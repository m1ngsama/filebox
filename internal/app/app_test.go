package app

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/m1ngsama/filebox/internal/auth"
	"github.com/m1ngsama/filebox/internal/db"
	"github.com/m1ngsama/filebox/internal/upload"
	"github.com/m1ngsama/filebox/internal/vol"
)

type fixture struct {
	App    *App
	H      http.Handler
	Dir    string
	Dir2   string
	Bearer string
	Cookie *http.Cookie
	UserID int64
}

func newTestApp(t *testing.T) *fixture {
	t.Helper()
	dir, dir2 := t.TempDir(), t.TempDir()
	vols, err := vol.Parse([]string{"v=" + dir, "w=" + dir2})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { vols.Close() })
	d, err := db.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	h, _ := auth.HashPassword("pw-pw-pw-pw")
	uid, _ := d.SetPassword("admin", h)
	a := auth.New(d)
	sess, _ := a.Login("admin", "pw-pw-pw-pw", "127.0.0.1")
	bearer, _ := a.NewAppToken(uid, "test", false)
	web := fstest.MapFS{
		"index.html":      {Data: []byte("<!doctype html>app")},
		"assets/app-1.js": {Data: []byte("js")},
	}
	ap := &App{Vols: vols, DB: d, Auth: a, Web: web, Uploads: &upload.Server{Vols: vols, Dir: t.TempDir()}}
	return &fixture{App: ap, H: ap.Handler(), Dir: dir, Dir2: dir2, Bearer: bearer,
		Cookie: &http.Cookie{Name: auth.CookieName, Value: sess}, UserID: uid}
}

func (f *fixture) do(method, url string, body io.Reader, hdr ...string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, url, body)
	noAuth := false
	for i := 0; i+1 < len(hdr); i += 2 {
		if hdr[i] == "X-No-Auth" {
			noAuth = true
			continue
		}
		r.Header.Set(hdr[i], hdr[i+1])
	}
	if !noAuth {
		r.AddCookie(f.Cookie)
	}
	w := httptest.NewRecorder()
	f.H.ServeHTTP(w, r)
	return w
}

func (f *fixture) write(t *testing.T, name, content string) {
	t.Helper()
	p := filepath.Join(f.Dir, name)
	os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestRawAuth(t *testing.T) {
	f := newTestApp(t)
	f.write(t, "a.txt", "hello")
	if w := f.do("GET", "/raw/v/a.txt", nil, "X-No-Auth", "1"); w.Code != 401 {
		t.Fatalf("anon = %d", w.Code)
	}
	if w := f.do("GET", "/raw/v/a.txt", nil); w.Code != 200 || w.Body.String() != "hello" {
		t.Fatalf("cookie = %d %q", w.Code, w.Body.String())
	}
	if w := f.do("GET", "/raw/v/a.txt", nil, "X-No-Auth", "1", "Authorization", "Bearer "+f.Bearer); w.Code != 200 {
		t.Fatalf("bearer = %d", w.Code)
	}
}

func TestRawEscapedPath(t *testing.T) {
	f := newTestApp(t)
	f.write(t, "d/中文 #?%.txt", "ok")
	w := f.do("GET", "/raw/v/d/%E4%B8%AD%E6%96%87%20%23%3F%25.txt", nil)
	if w.Code != 200 || w.Body.String() != "ok" {
		t.Fatalf("%d %q", w.Code, w.Body.String())
	}
	if w := f.do("GET", "/raw/v/.trash/x", nil); w.Code != 400 {
		t.Fatalf("reserved = %d", w.Code)
	}
}

func TestSPA(t *testing.T) {
	f := newTestApp(t)
	w := f.do("GET", "/files/v/deep/path", nil, "X-No-Auth", "1")
	if w.Code != 200 || w.Body.String() != "<!doctype html>app" {
		t.Fatalf("fallback %d %q", w.Code, w.Body.String())
	}
	if w.Header().Get("Content-Security-Policy") == "" {
		t.Error("SPA has no CSP")
	}
	w = f.do("GET", "/assets/app-1.js", nil, "X-No-Auth", "1")
	if w.Code != 200 || w.Header().Get("Cache-Control") != "public, max-age=31536000, immutable" {
		t.Fatalf("asset %d %q", w.Code, w.Header().Get("Cache-Control"))
	}
}

func TestCrossOriginBlocked(t *testing.T) {
	f := newTestApp(t)
	w := f.do("POST", "/raw/v/a.txt", nil, "Sec-Fetch-Site", "cross-site")
	if w.Code != 403 {
		t.Fatalf("cross-site POST = %d", w.Code)
	}
	if got := f.do("GET", "/", nil, "X-No-Auth", "1").Header().Get("Referrer-Policy"); got != "no-referrer" {
		t.Fatalf("Referrer-Policy %q", got)
	}
}
