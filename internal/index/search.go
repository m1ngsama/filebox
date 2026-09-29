package index

import (
	"context"
	"encoding/json"
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
	scope, where := visible, []any{}
	if q.Vol != "" {
		scope += ` AND f.vol = ?`
		where = append(where, q.Vol)
		if q.Under != "" && q.Under != "." {
			scope += ` AND f.path > ? AND f.path < ?`
			where = append(where, q.Under+"/", q.Under+"0")
		}
	}
	const cols = `SELECT f.vol, f.path, f.name, f.dir, f.size, f.mtime FROM files f`
	with := func(first any, rest ...any) []any { return append(append([]any{first}, where...), rest...) }
	if utf8.RuneCountInString(q.Text) < 3 {
		like := "%" + likeEscape(q.Text) + "%"
		return x.hits(ctx, cols+` WHERE f.path LIKE ? ESCAPE '\' AND f.name LIKE ? ESCAPE '\'`+scope+` ORDER BY f.mtime DESC LIMIT ?`,
			append([]any{like}, with(like, q.Limit)...)...)
	}
	phrase := `"` + strings.ReplaceAll(q.Text, `"`, `""`) + `"`
	ids, err := x.named(ctx, phrase)
	if err != nil {
		return nil, err
	}
	list, _ := json.Marshal(ids)
	first, arg := ` WHERE f.id IN (SELECT value FROM json_each(?))`, any(string(list))
	if len(ids) > candidates {
		first, arg = ` INDEXED BY files_mtime WHERE f.id IN (SELECT rowid FROM files_fts WHERE files_fts MATCH ?)`, `name : `+phrase
	}
	out, err := x.hits(ctx, cols+first+scope+` ORDER BY f.mtime DESC LIMIT ?`, with(arg, q.Limit)...)
	if err != nil || len(out) >= q.Limit {
		return out, err
	}
	more, err := x.hits(ctx, cols+` WHERE f.id IN (SELECT files_fts.rowid FROM files_fts JOIN files f ON f.id = files_fts.rowid
		WHERE files_fts MATCH ?`+scope+` ORDER BY files_fts.rowid DESC LIMIT ?) AND f.id NOT IN (SELECT value FROM json_each(?))
		ORDER BY f.mtime DESC LIMIT ?`,
		with(`path : `+phrase, candidates+len(ids), string(list), q.Limit-len(out))...)
	return append(out, more...), err
}

func (x *Index) named(ctx context.Context, phrase string) ([]int64, error) {
	rows, err := x.db.QueryContext(ctx, `SELECT rowid FROM files_fts WHERE files_fts MATCH ? LIMIT ?`, `name : `+phrase, candidates+1)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := []int64{}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (x *Index) hits(ctx context.Context, sql string, args ...any) ([]Hit, error) {
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
