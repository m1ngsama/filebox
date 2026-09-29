package index

import (
	"strings"
	"unicode/utf8"
)

type Hit struct {
	File
	Dir bool `json:"dir"`
}

type Query struct {
	Text, Vol, Under string
	Limit            int
}

const visible = ` AND NOT (f.path = '.trash' OR f.path GLOB '.trash/*' OR f.path = '.filebox' OR f.path GLOB '.filebox/*')`

func likeEscape(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

func (x *Index) Search(q Query) ([]Hit, error) {
	var (
		sql, order string
		args, tail []any
	)
	if utf8.RuneCountInString(q.Text) >= 3 {
		sql = `SELECT f.vol, f.path, f.name, f.dir, f.size, f.mtime FROM files_fts JOIN files f ON f.id = files_fts.rowid
			WHERE files_fts MATCH ?`
		args = []any{`"` + strings.ReplaceAll(q.Text, `"`, `""`) + `"`}
		order, tail = ` ORDER BY instr(lower(f.name), lower(?)) = 0, f.mtime DESC`, []any{q.Text}
	} else {
		sql = `SELECT f.vol, f.path, f.name, f.dir, f.size, f.mtime FROM files f
			WHERE f.path LIKE ? ESCAPE '\' AND f.name LIKE ? ESCAPE '\'`
		like := "%" + likeEscape(q.Text) + "%"
		args = []any{like, like}
		order = ` ORDER BY f.mtime DESC`
	}
	if q.Vol != "" {
		sql += ` AND f.vol = ?`
		args = append(args, q.Vol)
		if q.Under != "" && q.Under != "." {
			sql += ` AND f.path > ? AND f.path < ?`
			args = append(args, q.Under+"/", q.Under+"0")
		}
	}
	sql += visible + order + ` LIMIT ?`
	args = append(append(args, tail...), q.Limit)
	rows, err := x.db.Query(sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Hit{}
	for rows.Next() {
		var h Hit
		if err := rows.Scan(&h.Vol, &h.Path, &h.Name, &h.Dir, &h.Size, &h.Mtime); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}
