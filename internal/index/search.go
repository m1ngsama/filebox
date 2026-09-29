package index

import (
	"context"
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

const candidates = 5000

func (x *Index) Search(ctx context.Context, q Query) ([]Hit, error) {
	scope, args := visible, []any{}
	if q.Vol != "" {
		scope += ` AND f.vol = ?`
		args = append(args, q.Vol)
		if q.Under != "" && q.Under != "." {
			scope += ` AND f.path > ? AND f.path < ?`
			args = append(args, q.Under+"/", q.Under+"0")
		}
	}
	const cols = `SELECT f.vol, f.path, f.name, f.dir, f.size, f.mtime FROM files f`
	var sql string
	if utf8.RuneCountInString(q.Text) >= 3 {
		sql = cols + ` WHERE f.id IN (SELECT files_fts.rowid FROM files_fts JOIN files f ON f.id = files_fts.rowid
			WHERE files_fts MATCH ?` + scope + ` ORDER BY files_fts.rowid DESC LIMIT ?)
			ORDER BY instr(lower(f.name), lower(?)) = 0, f.mtime DESC LIMIT ?`
		args = append([]any{`"` + strings.ReplaceAll(q.Text, `"`, `""`) + `"`}, append(args, candidates, q.Text, q.Limit)...)
	} else {
		like := "%" + likeEscape(q.Text) + "%"
		sql = cols + ` WHERE f.path LIKE ? ESCAPE '\' AND f.name LIKE ? ESCAPE '\'` + scope + ` ORDER BY f.mtime DESC LIMIT ?`
		args = append([]any{like, like}, append(args, q.Limit)...)
	}
	rows, err := x.db.QueryContext(ctx, sql, args...)
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
