package app

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

type recentList struct {
	Entries []struct {
		Vol, Path, Name string
		Size            int64
	}
	Scanning bool
}

func (f *fixture) recent(t *testing.T) []string {
	t.Helper()
	var out []string
	for _, e := range decode[recentList](t, f.do("GET", "/api/recent", nil)).Entries {
		out = append(out, e.Vol+":"+e.Path)
	}
	return out
}

func (f *fixture) expectRecent(t *testing.T, want ...string) {
	t.Helper()
	got := f.recent(t)
	if !slices.Equal(slices.Sorted(slices.Values(got)), slices.Sorted(slices.Values(want))) {
		t.Fatalf("recent = %v, want %v", got, want)
	}
}

func TestRecentAPI(t *testing.T) {
	f := newTestApp(t)
	l := decode[recentList](t, f.do("GET", "/api/recent", nil))
	if !l.Scanning || l.Entries == nil || len(l.Entries) != 0 {
		t.Fatalf("before scan %+v", l)
	}
	f.write(t, "d/a.txt", "abc")
	if err := f.App.Index.Scan(f.App.Vols); err != nil {
		t.Fatal(err)
	}
	l = decode[recentList](t, f.do("GET", "/api/recent?limit=9999", nil))
	if l.Scanning || len(l.Entries) != 1 || l.Entries[0].Name != "a.txt" || l.Entries[0].Size != 3 {
		t.Fatalf("after scan %+v", l)
	}
	if w := f.do("GET", "/api/recent", nil, "X-No-Auth", "1", "Authorization", "Bearer "+f.Bearer); w.Code != 401 {
		t.Fatalf("app token read recent %d", w.Code)
	}
}

func TestRecentFollowsAPIWrites(t *testing.T) {
	f := newTestApp(t)
	f.write(t, "tree/a.txt", "a")
	f.write(t, "tree/sub/b.txt", "b")
	f.App.Index.Scan(f.App.Vols)
	f.do("POST", "/api/mkdir", body(`{"vol":"v","path":"empty"}`))
	f.expectRecent(t, "v:tree/sub/b.txt", "v:tree/a.txt")
	if w := f.do("POST", "/api/mv", body(`{"src":{"vol":"v","path":"tree"},"dst":{"vol":"v","path":"t2"}}`)); w.Code != 204 {
		t.Fatalf("mv %d", w.Code)
	}
	f.expectRecent(t, "v:t2/sub/b.txt", "v:t2/a.txt")
	w := f.do("POST", "/api/mv", body(`{"src":{"vol":"v","path":"t2"},"dst":{"vol":"w","path":"t3"}}`))
	if st := waitJob(t, f, decode[struct{ Job string }](t, w).Job); st != "done" {
		t.Fatalf("mv state %s", st)
	}
	f.expectRecent(t, "w:t3/sub/b.txt", "w:t3/a.txt")
	w = f.do("POST", "/api/cp", body(`{"src":{"vol":"w","path":"t3/a.txt"},"dst":{"vol":"v","path":"a.txt"}}`))
	waitJob(t, f, decode[struct{ Job string }](t, w).Job)
	if got := f.recent(t); len(got) != 3 {
		t.Fatalf("after cp %v", got)
	}
	f.do("POST", "/api/rm", body(`{"vol":"w","paths":["t3"]}`))
	f.expectRecent(t, "v:a.txt")
	id := decode[struct{ Items []struct{ ID string } }](t, f.do("GET", "/api/trash?vol=w", nil)).Items[0].ID
	if w := f.do("POST", "/api/trash/restore", body(`{"vol":"w","id":"`+id+`"}`)); w.Code != 204 {
		t.Fatalf("restore %d", w.Code)
	}
	if got := f.recent(t); len(got) != 3 {
		t.Fatalf("after restore %v", got)
	}
}

func TestRecentFollowsUpload(t *testing.T) {
	f := newTestApp(t)
	os.Mkdir(filepath.Join(f.Dir, "in"), 0o755)
	f.write(t, "old.txt", "o")
	old := time.Unix(1_700_000_000, 0)
	os.Chtimes(filepath.Join(f.Dir, "old.txt"), old, old)
	f.App.Index.Scan(f.App.Vols)
	md := "vol " + b64("v") + ",dir " + b64("in") + ",filename " + b64("x.txt")
	w := f.do("POST", "/upload/", nil, "Tus-Resumable", "1.0.0", "Upload-Length", "3", "Upload-Metadata", md)
	w = f.do("PATCH", w.Header().Get("Location"), strings.NewReader("abc"), "Tus-Resumable", "1.0.0",
		"Upload-Offset", "0", "Content-Type", "application/offset+octet-stream")
	if w.Code != 204 {
		t.Fatalf("patch %d", w.Code)
	}
	if got := f.recent(t); len(got) != 2 || got[0] != "v:in/x.txt" {
		t.Fatalf("recent = %v", got)
	}
}

func TestRecentFollowsWebDAV(t *testing.T) {
	f := newTestApp(t)
	f.App.Index.Scan(f.App.Vols)
	dav := func(method, p, b string, hdr ...string) int {
		return f.do(method, "/dav/"+p, strings.NewReader(b), append([]string{"X-No-Auth", "1", "Authorization", "Basic " + b64("u:"+f.Bearer)}, hdr...)...).Code
	}
	if c := dav("MKCOL", "v/d", ""); c != 201 {
		t.Fatalf("mkcol %d", c)
	}
	if c := dav("PUT", "v/d/a.txt", "hello"); c != 201 {
		t.Fatalf("put %d", c)
	}
	f.expectRecent(t, "v:d/a.txt")
	if c := dav("COPY", "v/d", "", "Destination", "/dav/v/e"); c != 201 {
		t.Fatalf("copy %d", c)
	}
	if c := dav("MOVE", "v/d", "", "Destination", "/dav/w/d"); c != 201 {
		t.Fatalf("move %d", c)
	}
	f.expectRecent(t, "v:e/a.txt", "w:d/a.txt")
	if c := dav("DELETE", "w/d", ""); c != 204 {
		t.Fatalf("delete %d", c)
	}
	f.expectRecent(t, "v:e/a.txt")
}
