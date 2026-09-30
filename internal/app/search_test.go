package app

import (
	"bytes"
	"context"
	"database/sql"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/m1ngsama/filebox/internal/extract"
)

func (f *fixture) search(t *testing.T, query string) []string {
	t.Helper()
	w := f.do("GET", "/api/search?"+query, nil)
	if w.Code != 200 {
		t.Fatalf("search %s: %d %s", query, w.Code, w.Body)
	}
	var out []string
	for _, e := range decode[recentList](t, w).Entries {
		out = append(out, e.Vol+":"+e.Path)
	}
	return out
}

func TestSearchAPI(t *testing.T) {
	f := newTestApp(t)
	f.write(t, "notes/plan.txt", "a")
	f.write(t, "planets.md", "a")
	f.App.Index.Scan(f.App.Vols)
	f.do("POST", "/api/mkdir", body(`{"vol":"w","path":"plans"}`))
	if got := f.search(t, "q=plan&vol=v"); !slices.Equal(got, []string{"v:planets.md", "v:notes/plan.txt"}) && !slices.Equal(got, []string{"v:notes/plan.txt", "v:planets.md"}) {
		t.Fatalf("vol v %v", got)
	}
	if got := f.search(t, "q=plan"); len(got) != 3 {
		t.Fatalf("all vols %v", got)
	}
	if got := f.search(t, "q=plan&vol=v&under=notes"); !slices.Equal(got, []string{"v:notes/plan.txt"}) {
		t.Fatalf("under %v", got)
	}
	if got := f.search(t, "q="+url.QueryEscape(`" OR *`)); len(got) != 0 {
		t.Fatalf("injection %v", got)
	}
	f.do("POST", "/api/mv", body(`{"src":{"vol":"v","path":"notes"},"dst":{"vol":"v","path":"archive"}}`))
	if got := f.search(t, "q=plan.txt"); !slices.Equal(got, []string{"v:archive/plan.txt"}) {
		t.Fatalf("after mv %v", got)
	}
	f.do("POST", "/api/rm", body(`{"vol":"v","paths":["archive"]}`))
	if got := f.search(t, "q=plan.txt"); len(got) != 0 {
		t.Fatalf("after rm %v", got)
	}
	for q, code := range map[string]int{"q=p": 400, "q=+p+": 400, "q=plan&vol=nope": 404, "q=plan&vol=v&under=.trash": 400, "q=plan&vol=v&under=../x": 200, "q=%00%00%00": 400, "q=ab%00cd": 400, "q=plan&under=notes": 400} {
		if w := f.do("GET", "/api/search?"+q, nil); w.Code != code {
			t.Errorf("%s: %d, want %d", q, w.Code, code)
		}
	}
	if w := f.do("GET", "/api/search?q=plan", nil, "X-No-Auth", "1", "Authorization", "Bearer "+f.Bearer); w.Code != 401 {
		t.Fatalf("app token search %d", w.Code)
	}
	if w := f.do("GET", "/api/search?q=plan", nil, "X-No-Auth", "1"); w.Code != 401 {
		t.Fatalf("anonymous search %d", w.Code)
	}
}

func TestSearchContentAPI(t *testing.T) {
	f := newTestApp(t)
	f.write(t, "notes/plan.txt", "ship the <b>content</b> index")
	f.App.Index.Scan(f.App.Vols)
	type result struct {
		Content []struct {
			Vol, Path, Name string
			Snippet         []string
		}
		Indexing *struct{ Done, Total int64 }
	}
	if r := decode[result](t, f.do("GET", "/api/search?q=content", nil)); r.Content == nil || len(r.Content) != 0 || r.Indexing != nil {
		t.Fatalf("disabled %+v", r)
	}
	cdb := filepath.Join(t.TempDir(), "content.db")
	if err := f.App.Index.OpenContent(cdb); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { f.App.Index.CloseContent() })
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		f.App.Index.Extract(ctx, f.App.Vols, extract.New(ctx))
		close(done)
	}()
	t.Cleanup(func() { cancel(); <-done })
	for deadline := time.Now().Add(15 * time.Second); ; time.Sleep(50 * time.Millisecond) {
		r := decode[result](t, f.do("GET", "/api/search?q="+url.QueryEscape("<b>content"), nil))
		if len(r.Content) == 1 {
			if c := r.Content[0]; c.Path != "notes/plan.txt" || c.Name != "plan.txt" || !slices.Equal(c.Snippet, []string{"ship the ", "<b>content", "</b> index"}) {
				t.Fatalf("hit %+v", c)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("content never indexed")
		}
	}
	if r := decode[result](t, f.do("GET", "/api/search?q=content&vol=w", nil)); len(r.Content) != 0 {
		t.Fatalf("other volume %+v", r)
	}

	d, err := sql.Open("sqlite", cdb)
	if err != nil {
		t.Fatal(err)
	}
	d.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`)
	d.Close()
	b, _ := os.ReadFile(cdb)
	os.WriteFile(cdb, append(b[:4096], bytes.Repeat([]byte{0xab}, len(b)-4096)...), 0o644)
	w := f.do("GET", "/api/search?q=plan.txt", nil)
	if r := decode[recentList](t, w); w.Code != 200 || len(r.Entries) != 1 {
		t.Fatalf("a damaged content index broke name search: %d %s", w.Code, w.Body)
	}
	if r := decode[result](t, f.do("GET", "/api/search?q=plan.txt", nil)); r.Content == nil {
		t.Fatalf("content must stay an empty list %+v", r)
	}
}
