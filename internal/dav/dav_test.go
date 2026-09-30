package dav

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/m1ngsama/filebox/internal/auth"
	"github.com/m1ngsama/filebox/internal/db"
	"github.com/m1ngsama/filebox/internal/index"
	"github.com/m1ngsama/filebox/internal/version"
	"github.com/m1ngsama/filebox/internal/vol"
)

type env struct {
	srv    *httptest.Server
	dir    string
	dir2   string
	rw, ro string
	vs     *version.Store
}

func setup(t *testing.T) *env {
	t.Helper()
	dir, dir2 := t.TempDir(), t.TempDir()
	vols, err := vol.Parse([]string{"v=" + dir, "w=" + dir2})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { vols.Close() })
	d, _ := db.Open(filepath.Join(t.TempDir(), "d.db"))
	t.Cleanup(func() { d.Close() })
	uid, _ := d.SetPassword("admin", "x")
	a := auth.New(d)
	rw, _ := a.NewAppToken(uid, "rw", false)
	ro, _ := a.NewAppToken(uid, "ro", true)
	mux := http.NewServeMux()
	vs := &version.Store{DB: d}
	h := Handler(vols, a, index.New(d), vs)
	mux.Handle("/dav", h)
	mux.Handle("/dav/", h)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return &env{srv: srv, dir: dir, dir2: dir2, rw: rw, ro: ro, vs: vs}
}

func (e *env) req(t *testing.T, tok, method, p string, body string, hdr ...string) (*http.Response, string) {
	t.Helper()
	r, _ := http.NewRequest(method, e.srv.URL+p, strings.NewReader(body))
	if tok != "" {
		r.SetBasicAuth("me", tok)
	}
	for i := 0; i+1 < len(hdr); i += 2 {
		r.Header.Set(hdr[i], hdr[i+1])
	}
	res, err := http.DefaultClient.Do(r)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(res.Body)
	res.Body.Close()
	return res, string(b)
}

func TestDavAuth(t *testing.T) {
	e := setup(t)
	res, _ := e.req(t, "", "PROPFIND", "/dav/", "", "Depth", "1")
	if res.StatusCode != 401 || res.Header.Get("WWW-Authenticate") == "" {
		t.Fatalf("anon %d", res.StatusCode)
	}
	res, _ = e.req(t, "fb_wrong", "PROPFIND", "/dav/", "", "Depth", "1")
	if res.StatusCode != 401 {
		t.Fatalf("bad token %d", res.StatusCode)
	}
}

func TestDavRootListsVolumes(t *testing.T) {
	e := setup(t)
	for _, p := range []string{"/dav", "/dav/"} {
		res, body := e.req(t, e.rw, "PROPFIND", p, "", "Depth", "1")
		if res.StatusCode != 207 || !strings.Contains(body, "/dav/v/") || !strings.Contains(body, "/dav/w/") {
			t.Fatalf("%s: %d %s", p, res.StatusCode, body)
		}
	}
	res, _ := e.req(t, e.rw, "MKCOL", "/dav/newvol", "")
	if res.StatusCode < 400 {
		t.Fatalf("MKCOL at root %d", res.StatusCode)
	}
	res, _ = e.req(t, e.rw, "DELETE", "/dav/v/", "")
	if res.StatusCode < 400 {
		t.Fatalf("DELETE volume root %d", res.StatusCode)
	}
}

func TestDavUnicode(t *testing.T) {
	e := setup(t)
	res, _ := e.req(t, e.rw, "PUT", "/dav/v/%E4%B8%AD%20%23%25.txt", "hi")
	if res.StatusCode != 201 {
		t.Fatalf("PUT %d", res.StatusCode)
	}
	if b, _ := os.ReadFile(filepath.Join(e.dir, "中 #%.txt")); string(b) != "hi" {
		t.Fatalf("disk %q", b)
	}
	res, body := e.req(t, e.rw, "GET", "/dav/v/%E4%B8%AD%20%23%25.txt", "")
	if res.StatusCode != 200 || body != "hi" {
		t.Fatalf("GET %d %q", res.StatusCode, body)
	}
	if !strings.Contains(res.Header.Get("Content-Security-Policy"), "sandbox") {
		t.Fatalf("GET without sandbox CSP")
	}
}

func TestDavHidesReserved(t *testing.T) {
	e := setup(t)
	os.MkdirAll(filepath.Join(e.dir, ".trash/1"), 0o755)
	os.MkdirAll(filepath.Join(e.dir, ".filebox/uploads"), 0o755)
	os.WriteFile(filepath.Join(e.dir, "seen.txt"), nil, 0o644)
	_, body := e.req(t, e.rw, "PROPFIND", "/dav/v/", "", "Depth", "1")
	if strings.Contains(body, ".trash") || strings.Contains(body, ".filebox") || !strings.Contains(body, "seen.txt") {
		t.Fatalf("listing %s", body)
	}
	res, _ := e.req(t, e.rw, "PUT", "/dav/v/.trash/x", "boom")
	if res.StatusCode < 400 {
		t.Fatalf("PUT into .trash %d", res.StatusCode)
	}
}

func TestDavReadOnly(t *testing.T) {
	e := setup(t)
	res, _ := e.req(t, e.ro, "PUT", "/dav/v/a.txt", "x")
	if res.StatusCode != 403 {
		t.Fatalf("ro PUT %d", res.StatusCode)
	}
	res, _ = e.req(t, e.ro, "PROPFIND", "/dav/v/", "", "Depth", "1")
	if res.StatusCode != 207 {
		t.Fatalf("ro PROPFIND %d", res.StatusCode)
	}
}

func TestDavMove(t *testing.T) {
	e := setup(t)
	e.req(t, e.rw, "PUT", "/dav/v/a.txt", "x")
	res, _ := e.req(t, e.rw, "MOVE", "/dav/v/a.txt", "", "Destination", e.srv.URL+"/dav/v/b.txt")
	if res.StatusCode != 201 {
		t.Fatalf("MOVE %d", res.StatusCode)
	}
	if _, err := os.Stat(filepath.Join(e.dir, "b.txt")); err != nil {
		t.Fatal(err)
	}
}

func TestDavInfiniteDepth(t *testing.T) {
	e := setup(t)
	os.Mkdir(filepath.Join(e.dir, "sub"), 0o755)
	os.WriteFile(filepath.Join(e.dir, "sub/deep.txt"), nil, 0o644)
	for _, d := range []string{"infinity", ""} {
		r, _ := http.NewRequest("PROPFIND", e.srv.URL+"/dav/v/", nil)
		r.SetBasicAuth("me", e.rw)
		if d != "" {
			r.Header.Set("Depth", d)
		}
		res, err := http.DefaultClient.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(res.Body)
		res.Body.Close()
		if d == "" {
			if res.StatusCode != 207 || !strings.Contains(string(b), "/dav/v/sub/") || strings.Contains(string(b), "deep.txt") {
				t.Errorf("missing depth: %d %s", res.StatusCode, b)
			}
		} else if res.StatusCode != 403 || !strings.Contains(string(b), "propfind-finite-depth") {
			t.Errorf("depth %q: %d %s", d, res.StatusCode, b)
		}
	}
}

func TestDavReadOnlyMethods(t *testing.T) {
	e := setup(t)
	os.WriteFile(filepath.Join(e.dir, "a.txt"), []byte("x"), 0o644)
	dst := []string{"Destination", e.srv.URL + "/dav/v/b.txt"}
	cases := []struct {
		method, path string
		hdr          []string
		want         int
	}{
		{"PUT", "/dav/v/n.txt", nil, 403},
		{"MKCOL", "/dav/v/d", nil, 403},
		{"MOVE", "/dav/v/a.txt", dst, 403},
		{"COPY", "/dav/v/a.txt", dst, 403},
		{"DELETE", "/dav/v/a.txt", nil, 403},
		{"PROPPATCH", "/dav/v/a.txt", nil, 403},
		{"LOCK", "/dav/v/a.txt", nil, 403},
		{"GET", "/dav/v/a.txt", nil, 200},
		{"PROPFIND", "/dav/v/", []string{"Depth", "1"}, 207},
	}
	for _, c := range cases {
		if res, _ := e.req(t, e.ro, c.method, c.path, "", c.hdr...); res.StatusCode != c.want {
			t.Errorf("%s %s = %d, want %d", c.method, c.path, res.StatusCode, c.want)
		}
	}
	if _, err := os.Stat(filepath.Join(e.dir, "a.txt")); err != nil {
		t.Fatal("read-only token changed the volume")
	}
}

func TestDavMoveAcrossVolumes(t *testing.T) {
	e := setup(t)
	e.req(t, e.rw, "PUT", "/dav/v/x.txt", "x")
	os.MkdirAll(filepath.Join(e.dir, "d/sub"), 0o755)
	os.WriteFile(filepath.Join(e.dir, "d/sub/y.txt"), []byte("y"), 0o644)
	move := func(from, to string, hdr ...string) int {
		res, _ := e.req(t, e.rw, "MOVE", from, "", append([]string{"Destination", e.srv.URL + to}, hdr...)...)
		return res.StatusCode
	}
	if c := move("/dav/v/x.txt", "/dav/w/x.txt"); c != 201 {
		t.Fatalf("file MOVE %d", c)
	}
	if c := move("/dav/v/d", "/dav/w/d"); c != 201 {
		t.Fatalf("dir MOVE %d", c)
	}
	if b, _ := os.ReadFile(filepath.Join(e.dir2, "x.txt")); string(b) != "x" {
		t.Fatalf("moved file %q", b)
	}
	if b, _ := os.ReadFile(filepath.Join(e.dir2, "d/sub/y.txt")); string(b) != "y" {
		t.Fatalf("moved dir file %q", b)
	}
	for _, p := range []string{"x.txt", "d"} {
		if _, err := os.Stat(filepath.Join(e.dir, p)); !os.IsNotExist(err) {
			t.Fatalf("source %s survived", p)
		}
	}
	e.req(t, e.rw, "PUT", "/dav/v/x.txt", "new")
	if c := move("/dav/v/x.txt", "/dav/w/x.txt", "Overwrite", "F"); c != 412 {
		t.Fatalf("MOVE onto existing without overwrite %d", c)
	}
	if c := move("/dav/v/x.txt", "/dav/w/x.txt", "Overwrite", "T"); c != 204 {
		t.Fatalf("MOVE with overwrite %d", c)
	}
	if b, _ := os.ReadFile(filepath.Join(e.dir2, "x.txt")); string(b) != "new" {
		t.Fatalf("overwritten file %q", b)
	}
	os.MkdirAll(filepath.Join(e.dir, "l"), 0o755)
	os.Symlink("/etc/passwd", filepath.Join(e.dir, "l/link"))
	if c := move("/dav/v/l", "/dav/w/l"); c < 400 {
		t.Fatalf("MOVE of a tree with a symlink %d", c)
	}
	if _, err := os.Lstat(filepath.Join(e.dir, "l/link")); err != nil {
		t.Fatal("source with symlink damaged")
	}
	if _, err := os.Stat(filepath.Join(e.dir2, "l")); !os.IsNotExist(err) {
		t.Fatal("partial copy left behind")
	}
}
