package index

import (
	"log/slog"
	"time"

	"github.com/m1ngsama/filebox/internal/vol"
)

type Favorite struct {
	Vol, Path string
	Created   int64
}

func (x *Index) Favorites() ([]Favorite, error) {
	rows, err := x.db.Query(`SELECT vol, path, created FROM favorites ORDER BY created DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Favorite
	for rows.Next() {
		var f Favorite
		if err := rows.Scan(&f.Vol, &f.Path, &f.Created); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

func (x *Index) Star(v string, paths []string, on bool) error {
	x.w.Lock()
	defer x.w.Unlock()
	tx, err := x.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	q := `DELETE FROM favorites WHERE vol = ? AND path = ?`
	if on {
		q = `INSERT OR IGNORE INTO favorites (vol, path, created) VALUES (?, ?, ?)`
	}
	now := time.Now().UnixMilli()
	for _, p := range paths {
		args := []any{v, p}
		if on {
			args = append(args, now)
		}
		if _, err := tx.Exec(q, args...); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Carry follows a cross-volume move with the item's favorites, tags and activity.
func (x *Index) Carry(src *vol.Volume, srel string, dst *vol.Volume, drel string) {
	if x == nil {
		return
	}
	x.db.Flush()
	expr, args := moved(srel, drel)
	for _, t := range []string{"favorites", "tagged", "events"} {
		err := x.exec(x.db, `UPDATE OR REPLACE `+t+` SET vol = ?, path = `+expr+` WHERE `+subtree,
			append(append([]any{dst.Name}, args...), under(src.Name, srel)...)...)
		if err != nil {
			slog.Warn("carry "+t, "from", src.Name+":"+srel, "to", dst.Name+":"+drel, "err", err)
		}
	}
}
