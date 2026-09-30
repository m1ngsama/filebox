package index

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"path"
	"sync"
	"sync/atomic"
	"time"

	"github.com/m1ngsama/filebox/internal/db"
	"github.com/m1ngsama/filebox/internal/vol"
)

const batchSize = 1000

const upsert = `INSERT INTO files (vol, path, dir, size, mtime) VALUES (?, ?, ?, ?, ?)
	ON CONFLICT (vol, path) DO UPDATE SET dir = excluded.dir, size = excluded.size, mtime = excluded.mtime
	WHERE dir != excluded.dir OR size != excluded.size OR mtime != excluded.mtime`

const subtree = `(vol = ? AND path = ?) OR (vol = ? AND path > ? AND path < ?)`

func under(vol, rel string) []any {
	if rel == "." {
		// No UTF-8 path sorts after the byte 0xff.
		return []any{vol, rel, vol, "", "\xff"}
	}
	return []any{vol, rel, vol, rel + "/", rel + "0"}
}

func moved(from, to string) (string, []any) {
	switch {
	case from == to:
		return `path`, nil
	case to == ".":
		return `CASE path WHEN ? THEN '.' ELSE substr(path, length(?) + 2) END`, []any{from, from}
	case from == ".":
		return `CASE path WHEN '.' THEN ? ELSE ? || '/' || path END`, []any{to, to}
	}
	return `? || substr(path, length(?) + 1)`, []any{to, from}
}

type File struct {
	Vol   string `json:"vol"`
	Path  string `json:"path"`
	Name  string `json:"name"`
	Size  int64  `json:"size"`
	Mtime int64  `json:"mtime"`
}

type pending struct {
	v   *vol.Volume
	rel string
}

type Index struct {
	db      *db.DB
	w       sync.Mutex
	mu      sync.Mutex
	touched map[pending]struct{}
	ready   atomic.Bool
}

func New(d *db.DB) *Index { return &Index{db: d} }

func (x *Index) Ready() bool { return x.ready.Load() }

func (x *Index) Touch(v *vol.Volume, rel string) {
	if x == nil {
		return
	}
	x.mu.Lock()
	if x.touched != nil {
		x.touched[pending{v, rel}] = struct{}{}
	}
	x.mu.Unlock()
	if err := x.sync(v, rel); err != nil {
		slog.Warn("index update", "vol", v.Name, "path", rel, "err", err)
	}
}

func (x *Index) Rename(v *vol.Volume, from, to string) {
	if x == nil {
		return
	}
	x.mu.Lock()
	if x.touched != nil {
		x.touched[pending{v, from}] = struct{}{}
		x.touched[pending{v, to}] = struct{}{}
	}
	x.mu.Unlock()
	if err := x.rename(v.Name, from, to); err != nil {
		slog.Warn("index rename", "vol", v.Name, "from", from, "to", to, "err", err)
	}
}

func (x *Index) rename(vol, from, to string) error {
	if from == to {
		return nil
	}
	if to == "." {
		return fmt.Errorf("rename %q onto the volume root", from)
	}
	x.w.Lock()
	defer x.w.Unlock()
	tx, err := x.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, t := range []string{"files", "dav_props", "favorites", "versions"} {
		if t != "versions" {
			if _, err := tx.Exec(`DELETE FROM `+t+` WHERE `+subtree, under(vol, to)...); err != nil {
				return err
			}
		}
		expr, args := moved(from, to)
		if _, err := tx.Exec(`UPDATE `+t+` SET path = `+expr+` WHERE `+subtree, append(args, under(vol, from)...)...); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (x *Index) Scan(vols *vol.Set) error {
	start := time.Now()
	x.mu.Lock()
	if x.touched != nil {
		x.mu.Unlock()
		return nil
	}
	x.touched = map[pending]struct{}{}
	x.mu.Unlock()
	n, err := x.scan(vols)
	x.mu.Lock()
	touched := x.touched
	x.touched = nil
	x.mu.Unlock()
	for p := range touched {
		x.sync(p.v, p.rel)
	}
	x.ready.Store(true)
	if err != nil {
		return err
	}
	slog.Info("index scan", "files", n, "took", time.Since(start).Round(time.Millisecond))
	return nil
}

type Size struct {
	Size  int64 `json:"size"`
	Files int64 `json:"files"`
}

func (x *Index) Size(vol, rel string) (Size, error) {
	q, args := `vol = ?`, []any{vol}
	if rel != "." {
		q, args = `vol = ? AND path > ? AND path < ?`, []any{vol, rel + "/", rel + "0"}
	}
	var s Size
	err := x.db.QueryRow(`SELECT coalesce(sum(size), 0), count(*) FILTER (WHERE dir = 0) FROM files WHERE `+q, args...).
		Scan(&s.Size, &s.Files)
	return s, err
}

func (x *Index) Recent(limit int) ([]File, error) {
	rows, err := x.db.Query(`SELECT vol, path, size, mtime FROM files WHERE dir = 0 ORDER BY mtime DESC LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []File{}
	for rows.Next() {
		var f File
		if err := rows.Scan(&f.Vol, &f.Path, &f.Size, &f.Mtime); err != nil {
			return nil, err
		}
		f.Name = path.Base(f.Path)
		out = append(out, f)
	}
	return out, rows.Err()
}

func (x *Index) scan(vols *vol.Set) (int, error) {
	ctx := context.Background()
	c, err := x.db.Conn(ctx)
	if err != nil {
		return 0, err
	}
	defer c.Close()
	if _, err := c.ExecContext(ctx, `CREATE TEMP TABLE IF NOT EXISTS seen (path TEXT PRIMARY KEY) WITHOUT ROWID`); err != nil {
		return 0, err
	}
	defer c.ExecContext(ctx, `DROP TABLE temp.seen`)
	n := 0
	for _, v := range vols.All() {
		if _, err := c.ExecContext(ctx, `DELETE FROM temp.seen`); err != nil {
			return n, err
		}
		b := &batch{db: c, w: &x.w, vol: v.Name, seen: true}
		var unreadable []string
		err := walk(v, ".", func(r row) error {
			if !r.dir {
				n++
			}
			return b.add(r)
		}, func(p string) { unreadable = append(unreadable, p) })
		if err == nil {
			err = b.flush()
		}
		for _, p := range unreadable {
			if err == nil {
				_, err = c.ExecContext(ctx, `INSERT OR IGNORE INTO temp.seen (path) SELECT path FROM files WHERE `+subtree, under(v.Name, p)...)
			}
		}
		if err == nil {
			err = x.exec(c, `DELETE FROM files WHERE vol = ? AND path NOT IN (SELECT path FROM temp.seen)`, v.Name)
		}
		x.mu.Lock()
		var touched []string
		for p := range x.touched {
			if p.v == v {
				touched = append(touched, p.rel)
			}
		}
		x.mu.Unlock()
		for _, p := range touched {
			if err == nil {
				_, err = c.ExecContext(ctx, `INSERT OR IGNORE INTO temp.seen (path) SELECT path FROM dav_props WHERE `+subtree, under(v.Name, p)...)
			}
		}
		if err == nil {
			err = x.exec(c, `DELETE FROM dav_props WHERE vol = ? AND path != '.' AND path NOT IN (SELECT path FROM temp.seen)`, v.Name)
		}
		if err != nil {
			return n, fmt.Errorf("index volume %s: %w", v.Name, err)
		}
	}
	names, _ := json.Marshal(vols.Names())
	for _, t := range []string{"files", "dav_props"} {
		if err == nil {
			err = x.exec(c, `DELETE FROM `+t+` WHERE vol NOT IN (SELECT value FROM json_each(?))`, string(names))
		}
	}
	return n, err
}

func (x *Index) sync(v *vol.Volume, rel string) error {
	fi, err := v.Root.Lstat(rel)
	if errors.Is(err, fs.ErrNotExist) {
		for _, t := range []string{"dav_props", "favorites"} {
			if err := x.exec(x.db, `DELETE FROM `+t+` WHERE `+subtree, under(v.Name, rel)...); err != nil {
				return err
			}
		}
	}
	if err != nil || (!fi.IsDir() && !fi.Mode().IsRegular()) {
		return x.exec(x.db, `DELETE FROM files WHERE `+subtree, under(v.Name, rel)...)
	}
	b := &batch{db: x.db, w: &x.w, vol: v.Name}
	if err := walk(v, rel, b.add, nil); err != nil {
		return err
	}
	return b.flush()
}

type row struct {
	path        string
	dir         bool
	size, mtime int64
}

func walk(v *vol.Volume, rel string, fn func(row) error, unreadable func(string)) error {
	return fs.WalkDir(v.Root.FS(), rel, func(p string, d fs.DirEntry, err error) error {
		switch {
		case err != nil && p == rel:
			return err
		case err != nil:
			slog.Warn("index skipped unreadable path", "vol", v.Name, "path", p, "err", err)
			if unreadable != nil {
				unreadable(p)
			}
			return nil
		case p == ".":
			return nil
		case path.Dir(p) == "." && vol.Reserved(p):
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		case !d.IsDir() && !d.Type().IsRegular():
			return nil
		}
		fi, err := d.Info()
		if err != nil {
			return nil
		}
		r := row{path: p, dir: d.IsDir(), mtime: fi.ModTime().UnixMilli()}
		if !r.dir {
			r.size = fi.Size()
		}
		return fn(r)
	})
}

type batch struct {
	db interface {
		BeginTx(context.Context, *sql.TxOptions) (*sql.Tx, error)
	}
	w    *sync.Mutex
	vol  string
	seen bool
	rows []row
}

type execer interface {
	ExecContext(context.Context, string, ...any) (sql.Result, error)
}

func (x *Index) exec(e execer, q string, args ...any) error {
	x.w.Lock()
	defer x.w.Unlock()
	_, err := e.ExecContext(context.Background(), q, args...)
	return err
}

func (b *batch) add(r row) error {
	b.rows = append(b.rows, r)
	if len(b.rows) >= batchSize {
		return b.flush()
	}
	return nil
}

func (b *batch) flush() error {
	if len(b.rows) == 0 {
		return nil
	}
	b.w.Lock()
	defer b.w.Unlock()
	tx, err := b.db.BeginTx(context.Background(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var top int64
	if err := tx.QueryRow(`SELECT coalesce(max(id), 0) FROM files`).Scan(&top); err != nil {
		return err
	}
	up, err := tx.Prepare(upsert)
	if err != nil {
		return err
	}
	var seen *sql.Stmt
	if b.seen {
		if seen, err = tx.Prepare(`INSERT OR IGNORE INTO temp.seen (path) VALUES (?)`); err != nil {
			return err
		}
	}
	for _, r := range b.rows {
		if _, err := up.Exec(b.vol, r.path, r.dir, r.size, r.mtime); err != nil {
			return err
		}
		if seen != nil {
			if _, err := seen.Exec(r.path); err != nil {
				return err
			}
		}
	}
	if _, err := tx.Exec(`INSERT INTO files_fts (rowid, name, path) SELECT id, name, path FROM files WHERE id > ?`, top); err != nil {
		return err
	}
	b.rows = b.rows[:0]
	return tx.Commit()
}
