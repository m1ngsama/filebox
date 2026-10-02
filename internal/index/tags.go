package index

import (
	"database/sql"
	"io/fs"
	"strings"
)

type Tag struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Color string `json:"color"`
	Count int64  `json:"count"`
}

func (x *Index) Tags() ([]Tag, error) {
	rows, err := x.db.Query(`SELECT id, name, color, (SELECT count(*) FROM tagged WHERE tag = tags.id) FROM tags ORDER BY name COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Tag{}
	for rows.Next() {
		var t Tag
		if err := rows.Scan(&t.ID, &t.Name, &t.Color, &t.Count); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// NewTag returns the tag with this name, creating it when there is none.
func (x *Index) NewTag(name, color string) (Tag, error) {
	x.w.Lock()
	defer x.w.Unlock()
	t := Tag{Name: name, Color: color}
	err := x.db.QueryRow(`INSERT INTO tags (name, color) VALUES (?, ?) ON CONFLICT (name) DO UPDATE SET name = name RETURNING id, name, color`, name, color).Scan(&t.ID, &t.Name, &t.Color)
	return t, err
}

func (x *Index) EditTag(id int64, name, color string) error {
	x.w.Lock()
	defer x.w.Unlock()
	res, err := x.db.Exec(`UPDATE tags SET name = ?, color = ? WHERE id = ?`, name, color, id)
	if err != nil && strings.Contains(err.Error(), "UNIQUE") {
		return fs.ErrExist
	}
	return found(res, err)
}

func (x *Index) DeleteTag(id int64) error {
	x.w.Lock()
	defer x.w.Unlock()
	return found(x.db.Exec(`DELETE FROM tags WHERE id = ?`, id))
}

func (x *Index) TagItems(tag int64, vol string, paths []string, on bool) error {
	x.w.Lock()
	defer x.w.Unlock()
	tx, err := x.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var n int
	if err := tx.QueryRow(`SELECT count(*) FROM tags WHERE id = ?`, tag).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		return fs.ErrNotExist
	}
	q := `DELETE FROM tagged WHERE vol = ? AND path = ? AND tag = ?`
	if on {
		q = `INSERT OR IGNORE INTO tagged (vol, path, tag) VALUES (?, ?, ?)`
	}
	for _, p := range paths {
		if _, err := tx.Exec(q, vol, p, tag); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// TagsIn maps each direct child of dir to its tags.
func (x *Index) TagsIn(vol, dir string) (map[string][]int64, error) {
	lo, hi, cut := dir+"/", dir+"0", len(dir)+1
	if dir == "." {
		lo, hi, cut = "", "\xff", 0
	}
	rows, err := x.db.Query(`SELECT path, tag FROM tagged WHERE vol = ? AND path > ? AND path < ? ORDER BY tag`, vol, lo, hi)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string][]int64{}
	for rows.Next() {
		var p string
		var t int64
		if err := rows.Scan(&p, &t); err != nil {
			return nil, err
		}
		if name := p[cut:]; !strings.Contains(name, "/") {
			out[name] = append(out[name], t)
		}
	}
	return out, rows.Err()
}

func (x *Index) Tagged(tag int64) ([]Favorite, error) {
	rows, err := x.db.Query(`SELECT vol, path FROM tagged WHERE tag = ? ORDER BY vol, path`, tag)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Favorite
	for rows.Next() {
		var f Favorite
		if err := rows.Scan(&f.Vol, &f.Path); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

func found(res sql.Result, err error) error {
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fs.ErrNotExist
	}
	return nil
}
