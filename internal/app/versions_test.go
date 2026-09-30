package app

import (
	"slices"
	"strings"
	"testing"

	"github.com/m1ngsama/filebox/internal/version"
)

type versionList struct {
	Versions []struct {
		ID     string
		Size   int64
		Source string
	}
}

func (f *fixture) versions(t *testing.T, p string) []string {
	t.Helper()
	w := f.do("GET", "/api/versions?vol=v&p="+p, nil)
	if w.Code != 200 {
		t.Fatalf("versions %d %s", w.Code, w.Body)
	}
	var ids []string
	for _, x := range decode[versionList](t, w).Versions {
		ids = append(ids, x.ID)
	}
	return ids
}

func (f *fixture) keep(t *testing.T, p, old, cur string) string {
	t.Helper()
	f.write(t, p, old)
	v, _ := f.App.Vols.Get("v")
	id, err := f.App.Versions.Capture(v, p, version.Upload, f.UserID)
	if err != nil {
		t.Fatal(err)
	}
	f.write(t, p, cur)
	return id
}

func TestVersionsAPI(t *testing.T) {
	f := newTestApp(t)
	id := f.keep(t, "doc.txt", "v1", "v2")
	if got := f.versions(t, "doc.txt"); !slices.Equal(got, []string{id}) {
		t.Fatalf("list %v", got)
	}
	w := f.do("GET", "/api/versions/raw?vol=v&id="+id+"&dl", nil)
	if w.Code != 200 || w.Body.String() != "v1" || !strings.Contains(w.Header().Get("Content-Disposition"), "doc.txt") {
		t.Fatalf("raw %d %q %v", w.Code, w.Body, w.Header())
	}
	for u, code := range map[string]int{
		"/api/versions/raw?vol=v&id=../../doc.txt": 400,
		"/api/versions/raw?vol=w&id=" + id:         404,
		"/api/versions/raw?vol=x&id=" + id:         404,
		"/api/versions?vol=v&p=":                   400,
		"/api/versions?vol=v&p=.filebox/versions":  400,
	} {
		if w := f.do("GET", u, nil); w.Code != code {
			t.Errorf("%s = %d, want %d", u, w.Code, code)
		}
	}
	for _, u := range []string{"/api/versions?vol=v&p=doc.txt", "/api/versions/raw?vol=v&id=" + id} {
		if w := f.do("GET", u, nil, "X-No-Auth", "1"); w.Code != 401 {
			t.Errorf("anon %s = %d", u, w.Code)
		}
	}
	if w := f.do("POST", "/api/versions/restore", body(`{"vol":"v","id":"`+id+`"}`), "X-No-Auth", "1"); w.Code != 401 {
		t.Fatalf("anon restore %d", w.Code)
	}
	if w := f.do("POST", "/api/versions/delete", body(`{"vol":"v","id":"`+id+`"}`), "Origin", "https://evil.example", "Sec-Fetch-Site", "cross-site"); w.Code != 403 {
		t.Fatalf("cross-site delete %d", w.Code)
	}
	if w := f.do("POST", "/api/versions/delete", body(`{"vol":"v","id":"`+id+`"}`)); w.Code != 204 || len(f.versions(t, "doc.txt")) != 0 {
		t.Fatalf("delete %d", w.Code)
	}
}

func TestVersionsFollowTheFile(t *testing.T) {
	f := newTestApp(t)
	id := f.keep(t, "a/doc.txt", "v1", "v2")
	if w := f.do("POST", "/api/mv", body(`{"src":{"vol":"v","path":"a"},"dst":{"vol":"v","path":"b"}}`)); w.Code != 204 {
		t.Fatalf("mv %d", w.Code)
	}
	if !slices.Equal(f.versions(t, "b/doc.txt"), []string{id}) || len(f.versions(t, "a/doc.txt")) != 0 {
		t.Fatal("versions did not follow the move")
	}
	w := f.do("POST", "/api/rm", body(`{"vol":"v","paths":["b/doc.txt"]}`))
	if !slices.Equal(f.versions(t, "b/doc.txt"), []string{id}) {
		t.Fatal("deleting the file dropped its versions")
	}
	tid := decode[struct{ Trashed []struct{ ID string } }](t, w).Trashed[0].ID
	if w := f.do("POST", "/api/trash/restore", body(`{"vol":"v","id":"`+tid+`"}`)); w.Code != 204 {
		t.Fatalf("trash restore %d", w.Code)
	}
	w = f.do("POST", "/api/versions/restore", body(`{"vol":"v","id":"`+id+`"}`))
	if w.Code != 200 {
		t.Fatalf("restore %d %s", w.Code, w.Body)
	}
	if b := f.do("GET", "/raw/v/b/doc.txt", nil).Body.String(); b != "v1" {
		t.Fatalf("restored %q", b)
	}
}

func TestVersionsInvisible(t *testing.T) {
	f := newTestApp(t)
	id := f.keep(t, "doc.txt", "old", "new")
	f.App.Index.Scan(f.App.Vols)
	if b := f.do("GET", "/api/ls?vol=v&path=", nil).Body.String(); strings.Contains(b, ".filebox") || strings.Contains(b, id) {
		t.Fatalf("ls %s", b)
	}
	if got := f.search(t, "q=doc"); !slices.Equal(got, []string{"v:doc.txt"}) {
		t.Fatalf("search %v", got)
	}
	if got := f.search(t, "q="+id[:8]); len(got) != 0 {
		t.Fatalf("search by id %v", got)
	}
	if got := zipNames(t, f.do("GET", "/api/zip?vol=v&p=/", nil).Body.Bytes()); !slices.Equal(got, []string{"doc.txt"}) {
		t.Fatalf("zip %v", got)
	}
	for _, u := range []string{"/raw/v/.filebox/versions/" + id, "/api/ls?vol=v&path=.filebox/versions"} {
		if w := f.do("GET", u, nil); w.Code == 200 {
			t.Fatalf("%s reachable", u)
		}
	}
	tok := mkShare(t, f, `{"vol":"v","path":"","mode":"read"}`)
	if c, b, _ := anon(f, "GET", "/s/"+tok+"/ls?path=", ""); c != 200 || strings.Contains(b, ".filebox") || !strings.Contains(b, "doc.txt") {
		t.Fatalf("share ls %d %s", c, b)
	}
	for _, u := range []string{"/s/" + tok + "/raw/.filebox/versions/" + id, "/s/" + tok + "/ls?path=.filebox/versions"} {
		if c, _, _ := anon(f, "GET", u, ""); c == 200 {
			t.Fatalf("share %s reachable", u)
		}
	}
	if got := zipNames(t, f.do("GET", "/s/"+tok+"/zip", nil, "X-No-Auth", "1").Body.Bytes()); slices.ContainsFunc(got, func(n string) bool { return strings.Contains(n, "filebox") }) {
		t.Fatalf("share zip %v", got)
	}
}
