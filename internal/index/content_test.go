package index

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/m1ngsama/filebox/internal/extract"
)

func (e *env) open(t *testing.T) *content {
	t.Helper()
	if e.cdb == "" {
		e.cdb = filepath.Join(t.TempDir(), "content.db")
	}
	if err := e.x.OpenContent(e.cdb); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { e.x.CloseContent() })
	return e.x.content.Load()
}

func (e *env) pass(t *testing.T) int {
	t.Helper()
	n, err := e.x.extractAll(context.Background(), e.vols, e.x.content.Load(), extract.New(context.Background()))
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func (e *env) id(t *testing.T, rel string) int64 {
	t.Helper()
	var id int64
	e.x.db.QueryRow(`SELECT id FROM files WHERE vol = 'v' AND path = ?`, rel).Scan(&id)
	return id
}

func (e *env) body(t *testing.T, rel string) (string, bool) {
	t.Helper()
	var s string
	err := e.x.content.Load().db.QueryRow(`SELECT body FROM contents_fts WHERE rowid = ?`, e.id(t, rel)).Scan(&s)
	return s, err == nil
}

func (e *env) status(t *testing.T, rel string) (status string, tries int) {
	t.Helper()
	e.x.content.Load().db.QueryRow(`SELECT status, tries FROM contents WHERE id = ?`, e.id(t, rel)).Scan(&status, &tries)
	return
}

func (e *env) rows(t *testing.T) (contents, fts int) {
	t.Helper()
	e.x.content.Load().db.QueryRow(`SELECT (SELECT count(*) FROM contents), (SELECT count(*) FROM contents_fts)`).Scan(&contents, &fts)
	return
}

func TestContentFollowsFiles(t *testing.T) {
	e := setup(t)
	v, _ := e.vols.Get("v")
	t0 := time.Unix(1_700_000_000, 0)
	e.write(t, "notes/a.txt", "the quick brown fox", t0)
	e.write(t, "notes/b.md", "# Plan\n\nship the **content** index", t0)
	e.write(t, "photo.jpg", "\xff\xd8", t0)
	e.write(t, "empty.txt", "", t0)
	e.write(t, "bin.txt", "a\x00b", t0)
	e.scan(t)
	e.open(t)
	if n := e.pass(t); n != 4 {
		t.Fatalf("extracted %d, want 4 (two texts, one empty, one binary)", n)
	}
	if got, _ := e.body(t, "notes/a.txt"); got != "the quick brown fox" {
		t.Fatalf("a.txt %q", got)
	}
	for rel, want := range map[string]string{"notes/a.txt": "ok", "empty.txt": "empty", "bin.txt": "skipped"} {
		if got, _ := e.status(t, rel); got != want {
			t.Errorf("%s: %s, want %s", rel, got, want)
		}
	}
	if n, f := e.rows(t); n != 4 || f != 2 {
		t.Fatalf("rows %d/%d, want 4 recorded and 2 with text", n, f)
	}
	if n := e.pass(t); n != 0 {
		t.Fatalf("second pass redid %d files", n)
	}

	e.write(t, "notes/a.txt", "a slow red fox", t0.Add(time.Hour))
	e.x.Touch(v, "notes/a.txt")
	if got := e.findText(t, Query{Text: "quick"}); len(got) != 0 {
		t.Fatalf("stale content matched after the file changed %v", got)
	}
	if n := e.pass(t); n != 1 {
		t.Fatalf("change re-extracted %d", n)
	}
	if got, _ := e.body(t, "notes/a.txt"); got != "a slow red fox" {
		t.Fatalf("after change %q", got)
	}

	os.Rename(filepath.Join(e.dir, "notes"), filepath.Join(e.dir, "docs"))
	e.x.Rename(v, "notes", "docs")
	if n := e.pass(t); n != 0 {
		t.Fatalf("rename re-extracted %d", n)
	}
	if got, _ := e.body(t, "docs/b.md"); got != "# Plan ship the **content** index" {
		t.Fatalf("after rename %q", got)
	}

	os.RemoveAll(filepath.Join(e.dir, "docs"))
	e.x.Touch(v, "docs")
	e.pass(t)
	if n, f := e.rows(t); n != 2 || f != 0 {
		t.Fatalf("after delete %d/%d", n, f)
	}
	os.Remove(filepath.Join(e.dir, "empty.txt"))
	os.Remove(filepath.Join(e.dir, "bin.txt"))
	e.scan(t)
	e.pass(t)
	if n, f := e.rows(t); n != 0 || f != 0 {
		t.Fatalf("scan left %d/%d", n, f)
	}
}

func TestContentIgnoresReusedIDs(t *testing.T) {
	e := setup(t)
	v, _ := e.vols.Get("v")
	e.write(t, "a.txt", "first alpha", time.Unix(1_700_000_000, 0))
	e.write(t, "z.txt", "secret zebra words", time.Unix(1_700_000_000, 0))
	e.scan(t)
	e.open(t)
	e.pass(t)
	old := e.id(t, "z.txt")
	os.Remove(filepath.Join(e.dir, "z.txt"))
	e.x.Touch(v, "z.txt")
	e.write(t, "y.txt", "other words", time.Unix(1_700_000_500, 0))
	e.x.Touch(v, "y.txt")
	if e.id(t, "y.txt") != old {
		t.Skip("sqlite did not reuse the id")
	}
	if got := e.findText(t, Query{Text: "zebra"}); len(got) != 0 {
		t.Fatalf("a new file inherited old content %v", got)
	}
	e.pass(t)
	if got := e.findText(t, Query{Text: "other"}); len(got) != 1 {
		t.Fatalf("new file %v", got)
	}
}

func TestContentRetriesFailuresOnce(t *testing.T) {
	e := setup(t)
	e.write(t, "locked.txt", "hidden words", time.Unix(1_700_000_000, 0))
	os.Chmod(filepath.Join(e.dir, "locked.txt"), 0)
	t.Cleanup(func() { os.Chmod(filepath.Join(e.dir, "locked.txt"), 0o644) })
	if f, err := os.Open(filepath.Join(e.dir, "locked.txt")); err == nil {
		f.Close()
		t.Skip("running as root")
	}
	e.scan(t)
	c := e.open(t)
	age := func() { c.db.Exec(`UPDATE contents SET at = at - 25 * 3600`) }
	for i, want := range []struct {
		n     int
		tries int
	}{{1, 1}, {0, 1}, {1, 2}, {0, 2}} {
		if i == 2 || i == 3 {
			age()
		}
		if n := e.pass(t); n != want.n {
			t.Fatalf("pass %d extracted %d, want %d", i, n, want.n)
		}
		if s, tries := e.status(t, "locked.txt"); s != "failed" || tries != want.tries {
			t.Fatalf("pass %d: %s after %d tries", i, s, tries)
		}
	}
}

func TestContentCrashCountsAsFailure(t *testing.T) {
	e := setup(t)
	e.write(t, "a.txt", "alpha", time.Unix(1_700_000_000, 0))
	e.scan(t)
	c := e.open(t)
	if err := c.claim(&job{id: e.id(t, "a.txt"), size: 5, mtime: time.Unix(1_700_000_000, 0).UnixMilli()}); err != nil {
		t.Fatal(err)
	}
	e.x.CloseContent()
	e.open(t)
	if s, tries := e.status(t, "a.txt"); s != "failed" || tries != 1 {
		t.Fatalf("%s after %d tries", s, tries)
	}
	if n := e.pass(t); n != 0 {
		t.Fatalf("crashed file retried at once: %d", n)
	}
}

func TestContentReextractsOnNewVersion(t *testing.T) {
	e := setup(t)
	e.write(t, "a.txt", "alpha", time.Unix(1_700_000_000, 0))
	e.write(t, "b.txt", "beta", time.Unix(1_700_000_000, 0))
	e.scan(t)
	c := e.open(t)
	e.pass(t)
	c.db.Exec(`UPDATE contents SET ver = ver - 1 WHERE id = ?`, e.id(t, "a.txt"))
	if n := e.pass(t); n != 1 {
		t.Fatalf("version change re-extracted %d", n)
	}
}

func TestContentResumesAfterRestart(t *testing.T) {
	e := setup(t)
	t0 := time.Unix(1_700_000_000, 0)
	e.write(t, "a.txt", "alpha", t0)
	e.scan(t)
	e.open(t)
	if n := e.pass(t); n != 1 {
		t.Fatal(n)
	}
	e.x.CloseContent()
	e.write(t, "b.txt", "beta", t0)
	e.x = New(e.x.db)
	e.scan(t)
	e.open(t)
	if n := e.pass(t); n != 1 {
		t.Fatalf("restart extracted %d, want only the new file", n)
	}
}

func TestContentRebuildsABadFile(t *testing.T) {
	e := setup(t)
	e.write(t, "a.txt", "alpha", time.Unix(1_700_000_000, 0))
	e.scan(t)
	e.cdb = filepath.Join(t.TempDir(), "content.db")
	os.WriteFile(e.cdb, []byte("this is not a database, just some bytes that are long enough to look like a header"), 0o644)
	e.open(t)
	if n := e.pass(t); n != 1 {
		t.Fatal(n)
	}
	e.x.content.Load().db.Exec(`PRAGMA user_version = 99`)
	e.x.CloseContent()
	e.open(t)
	if n := e.pass(t); n != 1 {
		t.Fatalf("schema mismatch kept old rows: %d", n)
	}
}

func TestContentWorkerWakes(t *testing.T) {
	old := settle
	settle = 0
	t.Cleanup(func() { settle = old })
	e := setup(t)
	v, _ := e.vols.Get("v")
	e.write(t, "a.txt", "first words", time.Unix(1_700_000_000, 0))
	e.open(t)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		e.x.Extract(ctx, e.vols, extract.New(ctx))
		close(done)
	}()
	t.Cleanup(func() { cancel(); <-done })
	e.scan(t)
	wait := func(rel, want string) {
		t.Helper()
		for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
			if got, _ := e.body(t, rel); got == want {
				return
			}
		}
		t.Fatalf("%s never became %q", rel, want)
	}
	wait("a.txt", "first words")
	e.write(t, "b.txt", "uploaded later", time.Unix(1_700_000_100, 0))
	e.x.Touch(v, "b.txt")
	wait("b.txt", "uploaded later")
	for deadline := time.Now().Add(10 * time.Second); e.x.Progress() != nil; time.Sleep(10 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("idle worker still reports progress %+v", e.x.Progress())
		}
	}
}

func TestContentKindsMatchSQL(t *testing.T) {
	e := setup(t)
	for name, want := range map[string]string{"a.TXT": ".txt", "Makefile": "makefile", ".bashrc": ".bashrc", "x.tar.gz": ".gz"} {
		var got string
		if err := e.x.db.QueryRow(`SELECT `+ext+` FROM (SELECT ? AS name) f`, name).Scan(&got); err != nil || got != want {
			t.Errorf("%s: %q %v, want %q", name, got, err, want)
		}
	}
}

func (e *env) findText(t *testing.T, q Query) map[string][]string {
	t.Helper()
	if q.Limit == 0 {
		q.Limit = 200
	}
	hits, err := e.x.SearchContent(context.Background(), q)
	if err != nil {
		t.Fatalf("content search %q: %v", q.Text, err)
	}
	out := map[string][]string{}
	for _, h := range hits {
		out[h.Vol+":"+h.Path] = h.Snippet
	}
	return out
}

func TestSearchContent(t *testing.T) {
	e := setup(t)
	t0 := time.Unix(1_700_000_000, 0)
	e.write(t, "notes/a.txt", "The Quick brown fox jumps over the lazy dog", t0)
	e.write(t, "notes/evil.html", `<p>click &lt;script&gt;alert(1)&lt;/script&gt; for the quick win</p>`, t0.Add(time.Hour))
	e.write(t, "forged.txt", "quick \x02fake\x03 marker", t0)
	e.write(t, "年报.md", "二〇二四年度报告正文", t0)
	e.scan(t)
	if got := e.findText(t, Query{Text: "quick"}); len(got) != 0 {
		t.Fatalf("content search before the index is enabled %v", got)
	}
	e.open(t)
	e.pass(t)

	got := e.findText(t, Query{Text: "QUICK BROWN"})
	if s := got["v:notes/a.txt"]; len(got) != 1 || len(s) != 3 || s[0] != "The " || s[1] != "Quick brown" || !strings.HasPrefix(s[2], " fox jumps over") {
		t.Fatalf("snippet %#v", got)
	}
	got = e.findText(t, Query{Text: "script"})
	if s := got["v:notes/evil.html"]; len(s) != 5 || s[0] != "click <" || s[1] != "script" || s[2] != ">alert(1)</" {
		t.Fatalf("markup must come back as plain segments: %#v", got)
	}
	got = e.findText(t, Query{Text: "fake"})
	if s := got["v:forged.txt"]; len(s) != 3 || s[0] != "quick " || s[1] != "fake" || s[2] != " marker" {
		t.Fatalf("control bytes in content must not forge highlights: %#v", got)
	}
	if got := e.findText(t, Query{Text: "年度报告"}); len(got["v:年报.md"]) != 3 {
		t.Fatalf("cjk %#v", got)
	}
	if got := e.findText(t, Query{Text: "quick", Vol: "v", Under: "notes"}); len(got) != 2 {
		t.Fatalf("scope %#v", got)
	}
	if got := e.findText(t, Query{Text: "qu"}); len(got) != 0 {
		t.Fatalf("two runes cannot use the trigram index %#v", got)
	}
	for _, q := range []string{`" OR *`, `quick" OR "fox`, `NEAR(quick fox)`, `body : quick`, `quick*`, `^quick`, `-quick`, `quick AND fox`, `""""`, `(quick)`} {
		if _, err := e.x.SearchContent(context.Background(), Query{Text: q, Limit: 10}); err != nil {
			t.Errorf("%q: %v", q, err)
		}
	}
	if got := e.findText(t, Query{Text: `quick" OR "fox`}); len(got) != 0 {
		t.Fatalf("quotes must stay literal %#v", got)
	}
	v, _ := e.vols.Get("v")
	os.MkdirAll(filepath.Join(e.dir, ".trash"), 0o755)
	os.Rename(filepath.Join(e.dir, "forged.txt"), filepath.Join(e.dir, ".trash/forged.txt"))
	e.x.Rename(v, "forged.txt", ".trash/forged.txt")
	if got := e.findText(t, Query{Text: "fake"}); len(got) != 0 {
		t.Fatalf("trash leaked into content search %#v", got)
	}
	e.pass(t)
	if _, ok := e.body(t, ".trash/forged.txt"); ok {
		t.Fatal("content moved into the trash was kept")
	}
}

func TestSearchContentCancels(t *testing.T) {
	e := setup(t)
	line := strings.Repeat("filler words and more filler ", 60) + "needle\n"
	for i := range 20 {
		e.write(t, fmt.Sprintf("big%02d.txt", i), strings.Repeat(line, extract.MaxText/4/len(line)), time.Unix(1_700_000_000, 0))
	}
	e.scan(t)
	e.open(t)
	e.pass(t)
	start := time.Now()
	if hits, err := e.x.SearchContent(context.Background(), Query{Text: "needle", Limit: 20}); err != nil || len(hits) != 20 {
		t.Fatalf("%d hits, %v", len(hits), err)
	}
	full := time.Since(start)
	ctx, cancel := context.WithTimeout(context.Background(), full/10)
	defer cancel()
	start = time.Now()
	if _, err := e.x.SearchContent(ctx, Query{Text: "needle", Limit: 20}); err == nil {
		t.Fatal("search outlived its deadline")
	}
	if d := time.Since(start); d > full/2 {
		t.Fatalf("canceled search took %v of %v", d, full)
	}
}
