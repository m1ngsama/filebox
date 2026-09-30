package index

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/m1ngsama/filebox/internal/extract"
	"github.com/m1ngsama/filebox/internal/vol"
)

const (
	contentBatch = 256
	contentLimit = 50
	snippetWords = 40
)

var settle = 2 * time.Second

const ext = `lower(substr(f.name, length(rtrim(f.name, replace(f.name, '.', '')))))`

type Progress struct {
	Done  int64 `json:"done"`
	Total int64 `json:"total"`
}

type ContentHit struct {
	File
	Snippet []string `json:"snippet"`
}

type content struct {
	ex          *extract.Extractor
	kinds       string
	wake        chan struct{}
	running     atomic.Bool
	done, total atomic.Int64
}

func (x *Index) poke() {
	if c := x.content.Load(); c != nil {
		select {
		case c.wake <- struct{}{}:
		default:
		}
	}
}

func (x *Index) Progress() *Progress {
	c := x.content.Load()
	if c == nil || !c.running.Load() {
		return nil
	}
	return &Progress{Done: c.done.Load(), Total: c.total.Load()}
}

func (x *Index) Extract(ctx context.Context, vols *vol.Set, ex *extract.Extractor) {
	kinds, _ := json.Marshal(ex.Kinds())
	c := &content{ex: ex, kinds: string(kinds), wake: make(chan struct{}, 1)}
	c.wake <- struct{}{}
	x.content.Store(c)
	for {
		select {
		case <-ctx.Done():
			return
		case <-c.wake:
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(settle):
		}
		if !x.Ready() {
			continue
		}
		start := time.Now()
		n, err := x.extractAll(ctx, vols, c)
		switch {
		case err != nil && ctx.Err() == nil:
			slog.Error("content index", "err", err)
		case n > 0:
			slog.Info("content index", "files", n, "took", time.Since(start).Round(time.Millisecond))
		}
	}
}

func (x *Index) DropContent() error {
	return x.exec(x.db, `DELETE FROM contents_fts; DELETE FROM contents;`)
}

type job struct {
	id          int64
	vol, path   string
	size, mtime int64
	text        string
	err         error
}

func (x *Index) extractAll(ctx context.Context, vols *vol.Set, c *content) (int, error) {
	eligible := ` FROM files f LEFT JOIN contents c ON c.id = f.id WHERE f.dir = 0 AND ` + ext + ` IN (SELECT value FROM json_each(?))` + visible
	stale := ` AND (c.id IS NULL OR c.size != f.size OR c.mtime != f.mtime)`
	var total, todo int64
	if err := x.db.QueryRowContext(ctx, `SELECT count(*), count(*) FILTER (WHERE c.id IS NULL OR c.size != f.size OR c.mtime != f.mtime)`+eligible, c.kinds).
		Scan(&total, &todo); err != nil || todo == 0 {
		return 0, err
	}
	c.total.Store(total)
	c.done.Store(total - todo)
	c.running.Store(true)
	defer c.running.Store(false)
	workers := max(1, runtime.NumCPU()/2)
	n := 0
	for last := int64(0); ; {
		rows, err := x.db.QueryContext(ctx, `SELECT f.id, f.vol, f.path, f.size, f.mtime`+eligible+stale+` AND f.id > ? ORDER BY f.id LIMIT ?`,
			c.kinds, last, contentBatch)
		if err != nil {
			return n, err
		}
		var jobs []*job
		for rows.Next() {
			j := &job{}
			if err := rows.Scan(&j.id, &j.vol, &j.path, &j.size, &j.mtime); err != nil {
				rows.Close()
				return n, err
			}
			jobs = append(jobs, j)
		}
		rows.Close()
		if err := rows.Err(); err != nil || len(jobs) == 0 {
			return n, err
		}
		last = jobs[len(jobs)-1].id
		next := make(chan *job)
		out := make(chan *job)
		var wg sync.WaitGroup
		for range min(workers, len(jobs)) {
			wg.Go(func() {
				for j := range next {
					if v, ok := vols.Get(j.vol); !ok {
						j.err = extract.ErrSkipped
					} else {
						j.text, j.err = c.ex.Extract(ctx, v.Root, j.path)
					}
					out <- j
				}
			})
		}
		go func() {
			for _, j := range jobs {
				next <- j
			}
			close(next)
			wg.Wait()
			close(out)
		}()
		var werr error
		for j := range out {
			if werr != nil || ctx.Err() != nil {
				continue
			}
			if j.err != nil && !errors.Is(j.err, extract.ErrSkipped) {
				slog.Debug("content extract", "vol", j.vol, "path", j.path, "err", j.err)
			}
			werr = x.store(j)
			c.done.Add(1)
			n++
		}
		if werr != nil {
			return n, werr
		}
		if err := ctx.Err(); err != nil {
			return n, err
		}
	}
}

func (x *Index) store(j *job) error {
	x.w.Lock()
	defer x.w.Unlock()
	tx, err := x.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM contents WHERE id = ?`, j.id); err != nil {
		return err
	}
	res, err := tx.Exec(`INSERT INTO contents (id, size, mtime, bytes) SELECT id, size, mtime, ? FROM files WHERE id = ? AND size = ? AND mtime = ?`,
		len(j.text), j.id, j.size, j.mtime)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 1 && j.text != "" {
		if _, err := tx.Exec(`INSERT INTO contents_fts (rowid, body) VALUES (?, ?)`, j.id, j.text); err != nil {
			return err
		}
	}
	return tx.Commit()
}
