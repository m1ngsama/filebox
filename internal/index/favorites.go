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

// Favorites lists one user's starred items on the volumes given, newest first; nil vols means every volume.
func (x *Index) Favorites(userID int64, vols []string) ([]Favorite, error) {
	only, args := onlyVols("vol", vols)
	rows, err := x.db.Query(`SELECT vol, path, created FROM favorites WHERE user_id = ?`+only+` ORDER BY created DESC`, append([]any{userID}, args...)...)
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

func (x *Index) Star(userID int64, v string, paths []string, on bool) error {
	x.w.Lock()
	defer x.w.Unlock()
	tx, err := x.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	q := `DELETE FROM favorites WHERE user_id = ? AND vol = ? AND path = ?`
	if on {
		q = `INSERT OR IGNORE INTO favorites (user_id, vol, path, created) VALUES (?, ?, ?, ?)`
	}
	now := time.Now().UnixMilli()
	for _, p := range paths {
		args := []any{userID, v, p}
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
	for _, t := range []string{"favorites", "tagged", "events", "comments"} {
		err := x.exec(x.db, `UPDATE OR REPLACE `+t+` SET vol = ?, path = `+expr+` WHERE `+subtree,
			append(append([]any{dst.Name}, args...), under(src.Name, srel)...)...)
		if err != nil {
			slog.Warn("carry "+t, "from", src.Name+":"+srel, "to", dst.Name+":"+drel, "err", err)
		}
	}
}
