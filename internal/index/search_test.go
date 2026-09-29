package index

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"testing"
	"time"
)

func (e *env) find(t *testing.T, q Query) []string {
	t.Helper()
	if q.Limit == 0 {
		q.Limit = 200
	}
	hits, err := e.x.Search(q)
	if err != nil {
		t.Fatalf("search %q: %v", q.Text, err)
	}
	var out []string
	for _, h := range hits {
		out = append(out, h.Vol+":"+h.Path)
	}
	return out
}

func TestTrigramAvailable(t *testing.T) {
	e := setup(t)
	var opts string
	if err := e.x.db.QueryRow(`SELECT group_concat(compile_options) FROM pragma_compile_options WHERE compile_options LIKE 'ENABLE_FTS5%'`).Scan(&opts); err != nil || opts == "" {
		t.Fatalf("fts5 not compiled in: %q %v", opts, err)
	}
	if _, err := e.x.db.Exec(`CREATE VIRTUAL TABLE temp.probe USING fts5(x, tokenize='trigram')`); err != nil {
		t.Fatal(err)
	}
}

func TestSearchFollowsWrites(t *testing.T) {
	e := setup(t)
	v, _ := e.vols.Get("v")
	t0 := time.Unix(1_700_000_000, 0)
	e.write(t, "Photos/holiday-2024.jpg", "x", t0)
	e.write(t, "Photos/beach.jpg", "x", t0.Add(time.Hour))
	e.write(t, "docs/holiday plan.txt", "x", t0.Add(2*time.Hour))
	e.write(t, "年度报告.pdf", "x", t0)
	e.scan(t)
	if got := e.find(t, Query{Text: "HOLIDAY"}); !slices.Equal(got, []string{"v:docs/holiday plan.txt", "v:Photos/holiday-2024.jpg"}) {
		t.Fatalf("holiday %v", got)
	}
	if got := e.find(t, Query{Text: "photos"}); !slices.Equal(got, []string{"v:Photos", "v:Photos/beach.jpg", "v:Photos/holiday-2024.jpg"}) {
		t.Fatalf("name hit must rank first, then mtime: %v", got)
	}
	if got := e.find(t, Query{Text: "报告"}); !slices.Equal(got, []string{"v:年度报告.pdf"}) {
		t.Fatalf("two-char %v", got)
	}
	if got := e.find(t, Query{Text: "年度报告"}); !slices.Equal(got, []string{"v:年度报告.pdf"}) {
		t.Fatalf("cjk trigram %v", got)
	}
	if got := e.find(t, Query{Text: "ot"}); !slices.Equal(got, []string{"v:Photos"}) {
		t.Fatalf("two-char matches the name only: %v", got)
	}

	os.Rename(filepath.Join(e.dir, "Photos"), filepath.Join(e.dir, "Album"))
	e.x.Rename(v, "Photos", "Album")
	if got := e.find(t, Query{Text: "holiday-2024"}); !slices.Equal(got, []string{"v:Album/holiday-2024.jpg"}) {
		t.Fatalf("after dir rename %v", got)
	}
	if got := e.find(t, Query{Text: "photos"}); len(got) != 0 {
		t.Fatalf("old path still found %v", got)
	}
	os.Rename(filepath.Join(e.dir, "Album/beach.jpg"), filepath.Join(e.dir, "docs/sand.jpg"))
	e.x.Rename(v, "Album/beach.jpg", "docs/sand.jpg")
	if got := e.find(t, Query{Text: "sand"}); !slices.Equal(got, []string{"v:docs/sand.jpg"}) {
		t.Fatalf("after move %v", got)
	}
	os.RemoveAll(filepath.Join(e.dir, "docs"))
	e.x.Touch(v, "docs")
	if got := e.find(t, Query{Text: "sand"}); len(got) != 0 {
		t.Fatalf("after delete %v", got)
	}
	e.write(t, "new/sandbox.txt", "x", t0)
	e.scan(t)
	if got := e.find(t, Query{Text: "sand"}); !slices.Equal(got, []string{"v:new/sandbox.txt"}) {
		t.Fatalf("after rescan %v", got)
	}
	var fts, files int
	e.x.db.QueryRow(`SELECT count(*) FROM files_fts WHERE files_fts MATCH '"jpg" OR "pdf" OR "txt" OR "new" OR "Album"'`).Scan(&fts)
	e.x.db.QueryRow(`SELECT count(*) FROM files`).Scan(&files)
	if fts != files {
		t.Fatalf("fts rows %d, files %d", fts, files)
	}
	if _, err := e.x.db.Exec(`INSERT INTO files_fts (files_fts, rank) VALUES ('integrity-check', 1)`); err != nil {
		t.Fatalf("fts out of sync: %v", err)
	}
}

func TestSearchInputIsLiteral(t *testing.T) {
	e := setup(t)
	now := time.Now()
	for _, n := range []string{`say "hi".txt`, `a*b.txt`, `NEAR(x y).txt`, `50%_off.txt`, `a OR b.txt`, `x-y:z.txt`, `back\slash.txt`} {
		e.write(t, n, "x", now)
	}
	e.scan(t)
	for q, want := range map[string]string{
		`"hi"`: `say "hi".txt`, `"`: `say "hi".txt`, `""`: "", `a*b`: `a*b.txt`, `*b`: `a*b.txt`, `NEAR(x`: `NEAR(x y).txt`,
		`NEAR`: `NEAR(x y).txt`, `%_`: `50%_off.txt`, `%`: `50%_off.txt`, `_o`: `50%_off.txt`, `a OR b`: `a OR b.txt`,
		`OR`: `a OR b.txt`, `y:z`: `x-y:z.txt`, `path:`: "", `\s`: `back\slash.txt`, `-y`: `x-y:z.txt`, `^a`: "",
	} {
		got := e.find(t, Query{Text: q})
		if want == "" && len(got) != 0 || want != "" && !slices.Equal(got, []string{"v:" + want}) {
			t.Errorf("search %q = %v, want %q", q, got, want)
		}
	}
}

func TestSearchScope(t *testing.T) {
	e := setup(t)
	now := time.Now()
	e.write(t, "a/report.txt", "x", now)
	e.write(t, "ab/report.txt", "x", now)
	e.write(t, "a/deep/report.md", "x", now)
	e.write(t, ".trash/1-x/report.txt", "x", now)
	e.write(t, ".filebox/uploads/report", "x", now)
	e.scan(t)
	e.x.db.Exec(`INSERT INTO files (vol, path, dir, size, mtime) VALUES ('v', '.trash/leak/report.txt', 0, 1, 1), ('w', 'report.doc', 0, 1, 1)`)
	all := e.find(t, Query{Text: "report"})
	sort.Strings(all)
	if !slices.Equal(all, []string{"v:a/deep/report.md", "v:a/report.txt", "v:ab/report.txt", "w:report.doc"}) {
		t.Fatalf("all %v", all)
	}
	if got := e.find(t, Query{Text: "report", Vol: "w"}); !slices.Equal(got, []string{"w:report.doc"}) {
		t.Fatalf("vol w %v", got)
	}
	got := e.find(t, Query{Text: "report", Vol: "v", Under: "a"})
	sort.Strings(got)
	if !slices.Equal(got, []string{"v:a/deep/report.md", "v:a/report.txt"}) {
		t.Fatalf("under a %v", got)
	}
	if got := e.find(t, Query{Text: "re", Vol: "v", Under: "a/deep"}); !slices.Equal(got, []string{"v:a/deep/report.md"}) {
		t.Fatalf("two-char under %v", got)
	}
	if got := e.find(t, Query{Text: "report", Limit: 2}); len(got) != 2 {
		t.Fatalf("limit %v", got)
	}
}

func BenchmarkSearch(b *testing.B) {
	e := setup(&testing.T{})
	bt := &batch{db: e.x.db, vol: "v"}
	words := []string{"holiday", "report", "invoice", "photo", "scan", "draft", "final", "backup", "music", "video"}
	for i := range 100_000 {
		bt.add(row{path: fmt.Sprintf("dir%03d/sub%02d/%s-%06d.%s", i%500, i%37, words[i%len(words)], i, []string{"jpg", "txt", "pdf", "mp4"}[i%4]), size: int64(i), mtime: int64(i)})
	}
	bt.flush()
	b.ResetTimer()
	for i := 0; b.Loop(); i++ {
		e.x.Search(Query{Text: []string{"holiday", "00123", "sub1", "zz", "report-0999"}[i%5], Limit: 200})
	}
}
