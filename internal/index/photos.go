package index

import (
	"context"
	"log/slog"
	"path"
	"strings"
	"time"

	"github.com/m1ngsama/filebox/internal/exif"
	"github.com/m1ngsama/filebox/internal/vol"
)

type Photo struct {
	File
	Taken int64 `json:"taken"`
	ID    int64 `json:"id"`
}

var exifKinds = map[string]bool{"jpg": true, "jpeg": true, "tif": true, "tiff": true, "dng": true, "cr2": true, "nef": true, "arw": true}

func media() string {
	var or []string
	for _, k := range []string{"image", "video"} {
		for _, e := range Kinds[k] {
			or = append(or, `f.name LIKE '%.`+e+`'`)
		}
	}
	return `(` + strings.Join(or, ` OR `) + `)`
}

// Date keeps every photo and video's capture time current, reading EXIF where there is one.
func (x *Index) Date(ctx context.Context, vols *vol.Set) {
	lowPriority()
	for {
		select {
		case <-ctx.Done():
			return
		case <-x.dates:
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(settle):
		}
		if !x.Ready() {
			continue
		}
		if n, err := x.DateAll(ctx, vols); err != nil && ctx.Err() == nil {
			slog.Warn("photo dates", "err", err)
		} else if n > 0 {
			slog.Info("photo dates", "files", n)
		}
	}
}

func (x *Index) DateAll(ctx context.Context, vols *vol.Set) (int, error) {
	if err := x.exec(x.db, `DELETE FROM taken WHERE NOT EXISTS (SELECT 1 FROM files f WHERE f.vol = taken.vol AND f.path = taken.path)`); err != nil {
		return 0, err
	}
	total := 0
	for ctx.Err() == nil {
		rows, err := x.db.QueryContext(ctx, `SELECT f.vol, f.path, f.size, f.mtime FROM files f
			LEFT JOIN taken t ON t.vol = f.vol AND t.path = f.path
			WHERE f.dir = 0 AND `+media()+` AND (t.id IS NULL OR t.size != f.size OR t.mtime != f.mtime) LIMIT 256`)
		if err != nil {
			return total, err
		}
		var batch []Photo
		for rows.Next() {
			var p Photo
			if err := rows.Scan(&p.Vol, &p.Path, &p.Size, &p.Mtime); err != nil {
				rows.Close()
				return total, err
			}
			batch = append(batch, p)
		}
		rows.Close()
		if len(batch) == 0 {
			return total, rows.Err()
		}
		for i := range batch {
			batch[i].Taken = taken(vols, batch[i].File)
		}
		if err := x.saveTaken(batch); err != nil {
			return total, err
		}
		total += len(batch)
	}
	return total, ctx.Err()
}

func taken(vols *vol.Set, f File) int64 {
	if v, ok := vols.Get(f.Vol); ok && exifKinds[strings.ToLower(strings.TrimPrefix(path.Ext(f.Path), "."))] {
		if r, err := vol.Open(v.Root, f.Path); err == nil {
			t, ok := exif.Taken(r, time.Local)
			r.Close()
			if ok {
				return t.UnixMilli()
			}
		}
	}
	return f.Mtime
}

func (x *Index) saveTaken(batch []Photo) error {
	x.w.Lock()
	defer x.w.Unlock()
	tx, err := x.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, p := range batch {
		if _, err := tx.Exec(`INSERT INTO taken (vol, path, size, mtime, at) VALUES (?, ?, ?, ?, ?)
			ON CONFLICT (vol, path) DO UPDATE SET size = excluded.size, mtime = excluded.mtime, at = excluded.at`,
			p.Vol, p.Path, p.Size, p.Mtime, p.Taken); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Photos pages through every photo and video, newest capture first; pass the last one seen to continue.
func (x *Index) Photos(ctx context.Context, after *Photo, limit int) ([]Photo, error) {
	q := `SELECT t.id, f.vol, f.path, f.name, f.size, f.mtime, t.at FROM taken t
		JOIN files f ON f.vol = t.vol AND f.path = t.path`
	args := []any{}
	if after != nil {
		q += ` WHERE (t.at, t.id) < (?, ?)`
		args = append(args, after.Taken, after.ID)
	}
	q += ` ORDER BY t.at DESC, t.id DESC LIMIT ?`
	rows, err := x.db.QueryContext(ctx, q, append(args, limit)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Photo{}
	for rows.Next() {
		var p Photo
		if err := rows.Scan(&p.ID, &p.Vol, &p.Path, &p.Name, &p.Size, &p.Mtime, &p.Taken); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}
