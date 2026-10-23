package index

import (
	"context"
	"sort"
	"strings"
)

type KindUsage struct {
	Kind  string `json:"kind"`
	Size  int64  `json:"size"`
	Files int64  `json:"files"`
}

type FolderUsage struct {
	Vol   string `json:"vol"`
	Path  string `json:"path"`
	Size  int64  `json:"size"`
	Files int64  `json:"files"`
}

type Usage struct {
	Kinds   []KindUsage   `json:"kinds"`
	Folders []FolderUsage `json:"folders"`
	Largest []File        `json:"largest"`
}

// kindCase names the kind of every file by its extension, with "other" for the rest.
func kindCase() string {
	var b strings.Builder
	b.WriteString(`CASE`)
	for _, k := range []string{"image", "video", "audio", "doc"} {
		var or []string
		for _, e := range Kinds[k] {
			or = append(or, `f.name LIKE '%.`+e+`'`)
		}
		b.WriteString(` WHEN ` + strings.Join(or, ` OR `) + ` THEN '` + k + `'`)
	}
	b.WriteString(` ELSE 'other' END`)
	return b.String()
}

// Usage totals what the files take by kind, which top-level folders hold the most and which single files are largest.
func (x *Index) Usage(ctx context.Context, folders, largest int) (Usage, error) {
	u := Usage{Kinds: []KindUsage{}, Folders: []FolderUsage{}, Largest: []File{}}
	rows, err := x.db.QueryContext(ctx, `SELECT `+kindCase()+` AS k, coalesce(sum(f.size), 0), count(*) FROM files f WHERE f.dir = 0`+visible+` GROUP BY k`)
	if err != nil {
		return u, err
	}
	for rows.Next() {
		var k KindUsage
		if err := rows.Scan(&k.Kind, &k.Size, &k.Files); err != nil {
			rows.Close()
			return u, err
		}
		u.Kinds = append(u.Kinds, k)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return u, err
	}
	sort.Slice(u.Kinds, func(i, j int) bool { return u.Kinds[i].Size > u.Kinds[j].Size })

	rows, err = x.db.QueryContext(ctx, `SELECT f.vol, substr(f.path, 1, instr(f.path, '/') - 1) AS top, coalesce(sum(f.size), 0) AS s, count(*)
		FROM files f WHERE f.dir = 0 AND instr(f.path, '/') > 0`+visible+` GROUP BY f.vol, top ORDER BY s DESC LIMIT ?`, folders)
	if err != nil {
		return u, err
	}
	for rows.Next() {
		var f FolderUsage
		if err := rows.Scan(&f.Vol, &f.Path, &f.Size, &f.Files); err != nil {
			rows.Close()
			return u, err
		}
		u.Folders = append(u.Folders, f)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return u, err
	}

	rows, err = x.db.QueryContext(ctx, `SELECT f.vol, f.path, f.size, f.mtime FROM files f WHERE f.dir = 0`+visible+` ORDER BY f.size DESC, f.id LIMIT ?`, largest)
	if err != nil {
		return u, err
	}
	defer rows.Close()
	for rows.Next() {
		var f File
		if err := rows.Scan(&f.Vol, &f.Path, &f.Size, &f.Mtime); err != nil {
			return u, err
		}
		f.Name = f.Path[strings.LastIndexByte(f.Path, '/')+1:]
		u.Largest = append(u.Largest, f)
	}
	return u, rows.Err()
}
