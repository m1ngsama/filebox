package index

import (
	"cmp"
	"context"
	"encoding/json"
	"path"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

type Hit struct {
	File
	Dir bool `json:"dir"`
}

type Query struct {
	Text, Vol, Under string
	Limit            int
	Kind             string
	After, MinSize   int64
}

// Kinds are the type filters search offers, by file extension; "dir" means folders.
var Kinds = map[string][]string{
	"dir":   nil,
	"image": {"jpg", "jpeg", "png", "gif", "webp", "avif", "heic", "heif", "bmp", "tif", "tiff", "svg", "dng", "cr2", "cr3", "nef", "arw"},
	"video": {"mp4", "m4v", "mkv", "mov", "avi", "webm", "ts", "flv", "wmv", "mpg", "mpeg"},
	"audio": {"mp3", "m4a", "aac", "flac", "wav", "ogg", "opus"},
	"doc":   {"pdf", "epub", "cbz", "txt", "md", "doc", "docx", "xls", "xlsx", "ppt", "pptx", "odt", "ods", "odp", "csv", "rtf", "pages", "numbers", "key"},
}

func (q Query) Filtered() bool { return q.Kind != "" || q.After > 0 || q.MinSize > 0 }

// Admits applies the filters to a content hit, which comes from a separate index.
func (q Query) Admits(f File) bool {
	if f.Mtime < q.After || f.Size < q.MinSize {
		return false
	}
	if q.Kind == "" {
		return true
	}
	ext := strings.ToLower(strings.TrimPrefix(path.Ext(f.Name), "."))
	return slices.Contains(Kinds[q.Kind], ext)
}

const visible = ` AND NOT (f.path = '.trash' OR f.path GLOB '.trash/*' OR f.path = '.filebox' OR f.path GLOB '.filebox/*' OR f.name GLOB '._*' OR f.name IN ('.DS_Store', 'Thumbs.db', 'desktop.ini'))`

func likeEscape(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

const candidates = 5000

func scoped(q Query) (string, []any) {
	scope, where := visible, []any{}
	if q.Vol != "" {
		scope += ` AND f.vol = ?`
		where = append(where, q.Vol)
		if q.Under != "" && q.Under != "." {
			scope += ` AND f.path > ? AND f.path < ?`
			where = append(where, q.Under+"/", q.Under+"0")
		}
	}
	if q.After > 0 {
		scope += ` AND f.mtime >= ?`
		where = append(where, q.After)
	}
	if q.MinSize > 0 {
		scope += ` AND f.dir = 0 AND f.size >= ?`
		where = append(where, q.MinSize)
	}
	if exts, ok := Kinds[q.Kind]; ok && q.Kind == "dir" {
		scope += ` AND f.dir = 1`
	} else if ok {
		var or []string
		for _, e := range exts {
			or = append(or, `f.name LIKE ?`)
			where = append(where, "%."+e)
		}
		scope += ` AND f.dir = 0 AND (` + strings.Join(or, ` OR `) + `)`
	}
	return scope, where
}

func (x *Index) Search(ctx context.Context, q Query) ([]Hit, error) {
	scope, where := scoped(q)
	const cols = `SELECT f.vol, f.path, f.name, f.dir, f.size, f.mtime FROM files f`
	with := func(first any, rest ...any) []any { return append(append([]any{first}, where...), rest...) }
	if q.Text == "" {
		order := `f.mtime DESC`
		if q.MinSize > 0 {
			order = `f.size DESC`
		}
		return x.hits(ctx, cols+` WHERE 1`+scope+` ORDER BY `+order+` LIMIT ?`, append(where, q.Limit)...)
	}
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
	set, arg := `SELECT value FROM json_each(?)`, any(string(list))
	first := ` WHERE f.id IN (` + set + `)`
	if len(ids) > candidates {
		set, arg = `SELECT rowid FROM files_fts WHERE files_fts MATCH ?`, `name : `+phrase
		first = ` INDEXED BY files_mtime WHERE f.id IN (` + set + `)`
	}
	out, err := x.hits(ctx, cols+first+scope+` ORDER BY f.mtime DESC LIMIT ?`, with(arg, q.Limit)...)
	if err != nil || len(out) >= q.Limit {
		return out, err
	}
	more, err := x.hits(ctx, cols+` WHERE f.id IN (SELECT files_fts.rowid FROM files_fts JOIN files f ON f.id = files_fts.rowid
		WHERE files_fts MATCH ?`+scope+` ORDER BY files_fts.rowid DESC LIMIT ?) AND f.id NOT IN (`+set+`)
		ORDER BY f.mtime DESC LIMIT ?`,
		with(`path : `+phrase, candidates+len(ids), arg, q.Limit-len(out))...)
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

func cjk(r rune) bool {
	return r == 'ー' || r == 'ｰ' || unicode.In(r, unicode.L, unicode.Nl) && unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana, unicode.Hangul)
}

func mark(r rune) bool { return unicode.In(r, unicode.Mn, unicode.Me) }

func bare(s string) string {
	return strings.Map(func(r rune) rune {
		if mark(r) {
			return -1
		}
		return r
	}, s)
}

func CJK(s string) bool {
	s = bare(s)
	for _, r := range s {
		if !cjk(r) {
			return false
		}
	}
	return s != ""
}

func grams(s string) string {
	var b strings.Builder
	prev := rune(-1)
	for _, r := range s {
		if mark(r) {
			continue
		}
		switch {
		case cjk(r) && prev >= 0:
			b.WriteRune(prev)
			b.WriteRune(r)
			b.WriteByte(' ')
		case prev >= 0:
			b.WriteRune(prev)
			b.WriteByte(' ')
		}
		prev = -1
		if cjk(r) {
			prev = r
		}
	}
	if prev >= 0 {
		b.WriteRune(prev)
	}
	return b.String()
}

func (x *Index) SearchContent(ctx context.Context, q Query) ([]ContentHit, error) {
	c := x.content.Load()
	if n := utf8.RuneCountInString(q.Text); c == nil || n < 3 && !CJK(q.Text) {
		return []ContentHit{}, nil
	}
	if q.Kind != "" && q.Kind != "doc" {
		return []ContentHit{}, nil
	}
	out, err := x.searchContent(ctx, c, q)
	if err != nil && ctx.Err() == nil {
		x.failed(c, err)
	}
	return slices.DeleteFunc(out, func(h ContentHit) bool { return !q.Admits(h.File) }), err
}

func (x *Index) searchContent(ctx context.Context, c *content, q Query) ([]ContentHit, error) {
	out := []ContentHit{}
	phrase := `"` + strings.ReplaceAll(q.Text, `"`, `""`) + `"`
	table, match, short := "contents_fts", phrase, utf8.RuneCountInString(q.Text) < 3
	if short {
		table, match = "contents_cjk", `"`+bare(q.Text)+`"`
		if utf8.RuneCountInString(bare(q.Text)) == 1 {
			match += "*"
		}
	}
	rows, err := c.db.QueryContext(ctx, `SELECT json_group_array(json_array(id, size, mtime)) FROM contents
		WHERE status = 'ok' AND id IN (SELECT rowid FROM `+table+` WHERE `+table+` MATCH ? ORDER BY rowid DESC LIMIT ?)`, match, candidates)
	if err != nil {
		return nil, err
	}
	var found string
	if rows.Next() {
		err = rows.Scan(&found)
	}
	rows.Close()
	if err != nil || rows.Err() != nil {
		return nil, cmp.Or(err, rows.Err())
	}
	scope, where := scoped(q)
	rows, err = x.db.QueryContext(ctx, `SELECT f.id, f.vol, f.path, f.name, f.size, f.mtime FROM json_each(?) j JOIN files f ON f.id = j.value ->> 0
		WHERE f.size = j.value ->> 1 AND f.mtime = j.value ->> 2`+scope+` ORDER BY f.mtime DESC LIMIT ?`,
		append(append([]any{found}, where...), min(q.Limit, contentLimit))...)
	if err != nil {
		return nil, err
	}
	byID := map[int64]int{}
	var ids []int64
	for rows.Next() {
		var id int64
		var h ContentHit
		if err := rows.Scan(&id, &h.Vol, &h.Path, &h.Name, &h.Size, &h.Mtime); err != nil {
			rows.Close()
			return nil, err
		}
		byID[id] = len(out)
		ids = append(ids, id)
		out = append(out, h)
	}
	rows.Close()
	if err := rows.Err(); err != nil || len(ids) == 0 {
		return out, err
	}
	list, _ := json.Marshal(ids)
	if short {
		rows, err = c.db.QueryContext(ctx, `SELECT rowid, iif(s > 1, '…', '') || substr(body, s, ?2), length(substr(body, s, ?2 + 1)) > ?2
		FROM (SELECT rowid, body, max(1, instr(body, ?1) - ?3) AS s FROM contents_fts WHERE rowid IN (SELECT value FROM json_each(?4)))`,
			q.Text, snippetWords, snippetWords/2, string(list))
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var id int64
			var s string
			var more bool
			if err := rows.Scan(&id, &s, &more); err != nil {
				return nil, err
			}
			if more {
				s += "…"
			}
			out[byID[id]].Snippet = marked(s, q.Text)
		}
		return out, rows.Err()
	}
	rows, err = c.db.QueryContext(ctx, `SELECT rowid, snippet(contents_fts, 0, char(2), char(3), '…', ?) FROM contents_fts
		WHERE contents_fts MATCH ? AND rowid IN (SELECT value FROM json_each(?))`, snippetWords, phrase, string(list))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var s string
		if err := rows.Scan(&id, &s); err != nil {
			return nil, err
		}
		out[byID[id]].Snippet = segments(s)
	}
	return out, rows.Err()
}

func segments(s string) []string {
	out := []string{}
	for {
		plain, rest, ok := strings.Cut(s, "\x02")
		out = append(out, plain)
		if !ok {
			return out
		}
		mark, after, _ := strings.Cut(rest, "\x03")
		out = append(out, mark)
		s = after
	}
}

func marked(s, q string) []string {
	out := []string{}
	for i, p := range strings.Split(s, q) {
		if i > 0 {
			out = append(out, q)
		}
		out = append(out, p)
	}
	return out
}
