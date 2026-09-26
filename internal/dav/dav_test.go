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
	"github.com/m1ngsama/filebox/internal/vol"
)

type env struct {
	srv    *httptest.Server
	dir    string
	rw, ro string
}

func setup(t *testing.T) *env {
	t.Helper()
	dir := t.TempDir()
	vols, err := vol.Parse([]string{"v=" + dir, "w=" + t.TempDir()})
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
	h := Handler(vols, a)
	mux.Handle("/dav", h)
	mux.Handle("/dav/", h)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return &env{srv: srv, dir: dir, rw: rw, ro: ro}
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
