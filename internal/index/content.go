package index

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"

	"github.com/m1ngsama/filebox/internal/extract"
	"github.com/m1ngsama/filebox/internal/vol"
)

const (
	contentBatch  = 256
	contentLimit  = 20
	snippetWords  = 64
	contentSchema = 2
	retryAfter    = 24 * 60 * 60
	maxTries      = 2
)

var settle = 2 * time.Second

const ext = `lower(substr(f.name, length(rtrim(f.name, replace(f.name, '.', '')))))`

const schema = `CREATE TABLE contents (
	id INTEGER PRIMARY KEY,
	size INTEGER NOT NULL,
	mtime INTEGER NOT NULL,
	ver INTEGER NOT NULL,
	status TEXT NOT NULL,
	tries INTEGER NOT NULL,
	at INTEGER NOT NULL,
	bytes INTEGER NOT NULL
);
CREATE VIRTUAL TABLE contents_fts USING fts5(body, tokenize='trigram');
CREATE VIRTUAL TABLE contents_cjk USING fts5(body, content='', contentless_delete=1, detail=none);`

type Progress struct {
	Done  int64 `json:"done"`
	Total int64 `json:"total"`
}

type ContentHit struct {
	File
	Snippet []string `json:"snippet"`
}

type content struct {
	db          *sql.DB
	w           sync.Mutex
	warned      atomic.Bool
	running     atomic.Bool
	done, total atomic.Int64
}

// content.db only holds what the extractor can rebuild, so any trouble opening it starts it over.
func (x *Index) OpenContent(p string) error {
	x.cmu.Lock()
	defer x.cmu.Unlock()
	c, err := openContent(p)
	if err != nil {
		slog.Warn("content index unusable, rebuilding", "path", p, "err", err)
		removeContent(p)
		if c, err = openContent(p); err != nil {
			return err
		}
	}
	x.cpath = p
	x.content.Store(c)
	return nil
}

func removeContent(p string) {
	for _, s := range []string{"", "-wal", "-shm"} {
		os.Remove(p + s)
	}
}

func (x *Index) CloseContent() error {
	x.cmu.Lock()
	c := x.content.Swap(nil)
	if x.stop != nil {
		x.stop()
	}
	x.cmu.Unlock()
	x.workers.Wait()
	if c == nil {
		return nil
	}
	c.w.Lock()
	defer c.w.Unlock()
	c.db.Exec(`DELETE FROM contents WHERE status = 'running'`)
	return c.db.Close()
}

func corrupt(err error) bool {
	var e *sqlite.Error
	return errors.As(err, &e) && (e.Code()&0xff == sqlite3.SQLITE_CORRUPT || e.Code()&0xff == sqlite3.SQLITE_NOTADB)
}

func (x *Index) failed(c *content, err error) {
	if !corrupt(err) {
		if !c.warned.Swap(true) {
			slog.Warn("content index", "err", err)
		}
		return
	}
	x.cmu.Lock()
	defer x.cmu.Unlock()
	if x.content.Load() != c {
		return
	}
	slog.Error("content index damaged, rebuilding", "path", x.cpath, "err", err)
	c.w.Lock()
	c.db.Close()
	c.w.Unlock()
	removeContent(x.cpath)
	fresh, err := openContent(x.cpath)
	if err != nil {
		slog.Error("content index off", "err", err)
		x.content.Store(nil)
		return
	}
	x.content.Store(fresh)
	x.poke()
}

func openContent(p string) (*content, error) {
	q := url.Values{"_pragma": {"journal_mode(WAL)", "busy_timeout(5000)", "synchronous(NORMAL)"}, "_txlock": {"immediate"}}
	dsn := url.URL{Scheme: "file", OmitHost: true, Path: p, RawQuery: q.Encode()}
	d, err := sql.Open("sqlite", dsn.String())
	if err != nil {
		return nil, err
	}
	var v int
	err = d.QueryRow(`PRAGMA user_version`).Scan(&v)
	switch {
	case err != nil:
	case v == 0:
		_, err = d.Exec(schema + fmt.Sprintf(`PRAGMA user_version = %d;`, contentSchema))
	case v != contentSchema:
		err = fmt.Errorf("content schema %d, want %d", v, contentSchema)
	}
	if err == nil {
		_, err = d.Exec(`UPDATE contents SET status = 'failed' WHERE status = 'running'`)
	}
	if err != nil {
		d.Close()
		return nil, err
	}
	return &content{db: d}, nil
}

func (x *Index) poke() {
	select {
	case x.wake <- struct{}{}:
	default:
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
	x.cmu.Lock()
	if x.content.Load() == nil {
		x.cmu.Unlock()
		return
	}
	ctx, x.stop = context.WithCancel(ctx)
	x.workers.Add(1)
	x.cmu.Unlock()
	defer x.workers.Done()
	lowPriority()
	x.poke()
	for {
		select {
		case <-ctx.Done():
			return
		case <-x.wake:
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(settle):
		}
		c := x.content.Load()
		if c == nil {
			return
		}
		if !x.Ready() {
			continue
		}
		start := time.Now()
		n, err := x.extractAll(ctx, vols, c, ex)
		switch {
		case err != nil && ctx.Err() == nil:
			x.failed(c, err)
		case n > 0:
			slog.Info("content index", "files", n, "took", time.Since(start).Round(time.Millisecond))
		}
	}
}

type job struct {
	id          int64
	vol, path   string
	size, mtime int64
	tries       int
	text, grams string
	err         error
}

type state struct {
	size, mtime int64
	ver, tries  int
	status      string
	at          int64
}

func (x *Index) plan(ctx context.Context, c *content, ex *extract.Extractor) (jobs []*job, orphans []int64, total int64, err error) {
	known := map[int64]state{}
	rows, err := c.db.QueryContext(ctx, `SELECT id, size, mtime, ver, status, tries, at FROM contents`)
	if err != nil {
		return nil, nil, 0, err
	}
	for rows.Next() {
		var id int64
		var s state
		if err := rows.Scan(&id, &s.size, &s.mtime, &s.ver, &s.status, &s.tries, &s.at); err != nil {
			rows.Close()
			return nil, nil, 0, err
		}
		known[id] = s
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, nil, 0, err
	}
	kinds, _ := json.Marshal(ex.Kinds())
	rows, err = x.db.QueryContext(ctx, `SELECT f.id, f.vol, f.path, f.size, f.mtime FROM files f
		WHERE f.dir = 0 AND `+ext+` IN (SELECT value FROM json_each(?))`+visible+` ORDER BY f.id`, string(kinds))
	if err != nil {
		return nil, nil, 0, err
	}
	defer rows.Close()
	now := time.Now().Unix()
	for rows.Next() {
		j := &job{}
		if err := rows.Scan(&j.id, &j.vol, &j.path, &j.size, &j.mtime); err != nil {
			return nil, nil, 0, err
		}
		total++
		s, ok := known[j.id]
		delete(known, j.id)
		switch {
		case !ok || s.size != j.size || s.mtime != j.mtime || s.ver != extract.Version:
		case s.status == "failed" && s.tries < maxTries && s.at < now-retryAfter:
			j.tries = s.tries
		default:
			continue
		}
		jobs = append(jobs, j)
	}
	for id := range known {
		orphans = append(orphans, id)
	}
	return jobs, orphans, total, rows.Err()
}

func (c *content) write(fn func(tx *sql.Tx) error) error {
	c.w.Lock()
	defer c.w.Unlock()
	tx, err := c.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit()
}

func (c *content) drop(ids []int64) error {
	for len(ids) > 0 {
		n := min(len(ids), 1000)
		list, _ := json.Marshal(ids[:n])
		ids = ids[n:]
		err := c.write(func(tx *sql.Tx) error {
			if _, err := tx.Exec(`DELETE FROM contents WHERE id IN (SELECT value FROM json_each(?))`, string(list)); err != nil {
				return err
			}
			if _, err := tx.Exec(`DELETE FROM contents_fts WHERE rowid IN (SELECT value FROM json_each(?))`, string(list)); err != nil {
				return err
			}
			_, err := tx.Exec(`DELETE FROM contents_cjk WHERE rowid IN (SELECT value FROM json_each(?))`, string(list))
			return err
		})
		if err != nil {
			return err
		}
	}
	return nil
}

func (c *content) claim(j *job) error {
	return c.write(func(tx *sql.Tx) error {
		if _, err := tx.Exec(`DELETE FROM contents_fts WHERE rowid = ?`, j.id); err != nil {
			return err
		}
		if _, err := tx.Exec(`DELETE FROM contents_cjk WHERE rowid = ?`, j.id); err != nil {
			return err
		}
		_, err := tx.Exec(`INSERT OR REPLACE INTO contents (id, size, mtime, ver, status, tries, at, bytes) VALUES (?, ?, ?, ?, 'running', ?, ?, 0)`,
			j.id, j.size, j.mtime, extract.Version, j.tries+1, time.Now().Unix())
		return err
	})
}

func (c *content) store(ctx context.Context, j *job) error {
	if ctx.Err() != nil {
		return c.write(func(tx *sql.Tx) error {
			_, err := tx.Exec(`DELETE FROM contents WHERE id = ? AND status = 'running'`, j.id)
			return err
		})
	}
	status := "ok"
	switch {
	case errors.Is(j.err, extract.ErrSkipped):
		status = "skipped"
	case j.err != nil:
		status = "failed"
		slog.Warn("content extract", "vol", j.vol, "path", j.path, "err", j.err)
	case j.text == "":
		status = "empty"
	}
	return c.write(func(tx *sql.Tx) error {
		if status == "ok" {
			if _, err := tx.Exec(`INSERT INTO contents_fts (rowid, body) VALUES (?, ?)`, j.id, j.text); err != nil {
				return err
			}
			if j.grams != "" {
				if _, err := tx.Exec(`INSERT INTO contents_cjk (rowid, body) VALUES (?, ?)`, j.id, j.grams); err != nil {
					return err
				}
			}
		}
		_, err := tx.Exec(`UPDATE contents SET status = ?, bytes = ? WHERE id = ?`, status, len(j.text), j.id)
		return err
	})
}

func (x *Index) extractAll(ctx context.Context, vols *vol.Set, c *content, ex *extract.Extractor) (int, error) {
	jobs, orphans, total, err := x.plan(ctx, c, ex)
	if err != nil {
		return 0, err
	}
	if err := c.drop(orphans); err != nil {
		return 0, err
	}
	if len(jobs) == 0 {
		return 0, nil
	}
	c.total.Store(total)
	c.done.Store(total - int64(len(jobs)))
	c.running.Store(true)
	defer c.running.Store(false)
	workers := max(1, runtime.GOMAXPROCS(0)/2)
	n := 0
	for len(jobs) > 0 && ctx.Err() == nil {
		chunk := jobs[:min(len(jobs), contentBatch)]
		jobs = jobs[len(chunk):]
		next, out := make(chan *job), make(chan *job)
		var wg sync.WaitGroup
		for range min(workers, len(chunk)) {
			wg.Go(func() {
				lowPriority()
				for j := range next {
					if j.err = c.claim(j); j.err == nil {
						if v, ok := vols.Get(j.vol); !ok {
							j.err = extract.ErrSkipped
						} else {
							j.text, j.err = ex.Extract(ctx, v.Root, j.path)
							j.grams = grams(j.text)
						}
					}
					out <- j
				}
			})
		}
		go func() {
			for _, j := range chunk {
				next <- j
			}
			close(next)
			wg.Wait()
			close(out)
		}()
		var werr error
		for j := range out {
			if werr == nil {
				werr = c.store(ctx, j)
			}
			c.done.Add(1)
			n++
		}
		if werr != nil {
			return n, werr
		}
	}
	return n, ctx.Err()
}
