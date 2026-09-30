package index

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/m1ngsama/filebox/internal/extract"
)

func (e *env) extractor() *content {
	ex := extract.New(context.Background())
	kinds, _ := json.Marshal(ex.Kinds())
	return &content{ex: ex, kinds: string(kinds), wake: make(chan struct{}, 1)}
}

func (e *env) pass(t *testing.T, c *content) int {
	t.Helper()
	e.x.content.Store(c)
	n, err := e.x.extractAll(context.Background(), e.vols, c)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func (e *env) body(t *testing.T, rel string) (string, bool) {
	t.Helper()
	var s string
	err := e.x.db.QueryRow(`SELECT body FROM contents_fts WHERE rowid = (SELECT id FROM files WHERE vol = 'v' AND path = ?)`, rel).Scan(&s)
	return s, err == nil
}

func (e *env) rows(t *testing.T) (contents, fts int) {
	t.Helper()
	e.x.db.QueryRow(`SELECT (SELECT count(*) FROM contents), (SELECT count(*) FROM contents_fts)`).Scan(&contents, &fts)
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
	c := e.extractor()
	if n := e.pass(t, c); n != 4 {
		t.Fatalf("extracted %d, want 4 (two texts, one empty, one binary)", n)
	}
	if got, _ := e.body(t, "notes/a.txt"); got != "the quick brown fox" {
		t.Fatalf("a.txt %q", got)
	}
	if n, f := e.rows(t); n != 4 || f != 2 {
		t.Fatalf("rows %d/%d, want 4 recorded and 2 with text", n, f)
	}
	if n := e.pass(t, c); n != 0 {
		t.Fatalf("second pass redid %d files", n)
	}

	e.write(t, "notes/a.txt", "a slow red fox", t0.Add(time.Hour))
	e.x.Touch(v, "notes/a.txt")
	if n := e.pass(t, c); n != 1 {
		t.Fatalf("change re-extracted %d", n)
	}
	if got, _ := e.body(t, "notes/a.txt"); got != "a slow red fox" {
		t.Fatalf("after change %q", got)
	}

	os.Rename(filepath.Join(e.dir, "notes"), filepath.Join(e.dir, "docs"))
	e.x.Rename(v, "notes", "docs")
	if n := e.pass(t, c); n != 0 {
		t.Fatalf("rename re-extracted %d", n)
	}
	if got, _ := e.body(t, "docs/b.md"); got != "# Plan ship the **content** index" {
		t.Fatalf("after rename %q", got)
	}

	os.RemoveAll(filepath.Join(e.dir, "docs"))
	e.x.Touch(v, "docs")
	if n, f := e.rows(t); n != 2 || f != 0 {
		t.Fatalf("after delete %d/%d", n, f)
	}
	os.Remove(filepath.Join(e.dir, "empty.txt"))
	os.Remove(filepath.Join(e.dir, "bin.txt"))
	e.scan(t)
	if n, f := e.rows(t); n != 0 || f != 0 {
		t.Fatalf("scan left %d/%d", n, f)
	}
}

func TestContentResumesAfterRestart(t *testing.T) {
	e := setup(t)
	t0 := time.Unix(1_700_000_000, 0)
	e.write(t, "a.txt", "alpha", t0)
	e.scan(t)
	if n := e.pass(t, e.extractor()); n != 1 {
		t.Fatal(n)
	}
	e.write(t, "b.txt", "beta", t0)
	e.x = New(e.x.db)
	e.scan(t)
	if n := e.pass(t, e.extractor()); n != 1 {
		t.Fatalf("restart extracted %d, want only the new file", n)
	}
	if err := e.x.DropContent(); err != nil {
		t.Fatal(err)
	}
	if n, f := e.rows(t); n != 0 || f != 0 {
		t.Fatalf("drop left %d/%d", n, f)
	}
}

func TestContentWorkerWakes(t *testing.T) {
	old := settle
	settle = 0
	t.Cleanup(func() { settle = old })
	e := setup(t)
	v, _ := e.vols.Get("v")
	e.write(t, "a.txt", "first words", time.Unix(1_700_000_000, 0))
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
	e.pass(t, e.extractor())

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
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := e.x.SearchContent(ctx, Query{Text: "quick", Limit: 10}); err == nil {
		t.Fatal("canceled search succeeded")
	}
}
