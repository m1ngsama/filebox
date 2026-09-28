package app

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/m1ngsama/filebox/internal/auth"
	"github.com/m1ngsama/filebox/internal/db"
	"github.com/m1ngsama/filebox/internal/index"
	"github.com/m1ngsama/filebox/internal/passkey"
	"github.com/m1ngsama/filebox/internal/thumb"
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
	sess, _ := a.Login("admin", "pw-pw-pw-pw", "127.0.0.1", "")
	bearer, _ := a.NewAppToken(uid, "test", false)
	web := fstest.MapFS{
		"index.html":         {Data: []byte("<!doctype html>app")},
		"assets/app-1.js":    {Data: []byte("js")},
		"assets/app-1.js.br": {Data: []byte("js-br")},
		"assets/app-1.js.gz": {Data: []byte("js-gz")},
		"assets/app-1.css":   {Data: []byte("css")},
	}
	up, err := upload.New(vols)
	if err != nil {
		t.Fatal(err)
	}
	pk, err := passkey.New(d, a, nil)
	if err != nil {
		t.Fatal(err)
	}
	ix := index.New(d)
	up.Index = ix
	ap := &App{Vols: vols, DB: d, Auth: a, Web: web, Uploads: up, Thumbs: thumb.New("", t.TempDir()), Passkeys: pk, Index: ix}
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
	for _, c := range []struct{ accept, body, enc string }{
		{"", "js", ""},
		{"gzip, deflate", "js-gz", "gzip"},
		{"gzip, deflate, br, zstd", "js-br", "br"},
		{"br;q=0, gzip", "js-gz", "gzip"},
		{"identity", "js", ""},
	} {
		w = f.do("GET", "/assets/app-1.js", nil, "X-No-Auth", "1", "Accept-Encoding", c.accept)
		h := w.Header()
		if w.Code != 200 || w.Body.String() != c.body || h.Get("Content-Encoding") != c.enc ||
			h.Get("Vary") != "Accept-Encoding" || !strings.HasPrefix(h.Get("Content-Type"), "text/javascript") ||
			h.Get("Cache-Control") != "public, max-age=31536000, immutable" {
			t.Errorf("Accept-Encoding %q: %d %q %v", c.accept, w.Code, w.Body.String(), h)
		}
	}
	w = f.do("GET", "/assets/app-1.css", nil, "X-No-Auth", "1", "Accept-Encoding", "br, gzip")
	if w.Body.String() != "css" || w.Header().Get("Content-Encoding") != "" || w.Header().Get("Vary") != "" {
		t.Errorf("uncompressed asset %q %v", w.Body.String(), w.Header())
	}
	w = f.do("GET", "/", nil, "X-No-Auth", "1", "Accept-Encoding", "br, gzip")
	if w.Header().Get("Content-Encoding") != "" || w.Body.String() != "<!doctype html>app" {
		t.Errorf("index %q %v", w.Body.String(), w.Header())
	}
}

func TestSPAPolicyHashesInlineScripts(t *testing.T) {
	if got := spaPolicy([]byte(`<script type="module" src="/a.js"></script>`)); got != spaCSP {
		t.Fatalf("no inline script: %q", got)
	}
	got := spaPolicy([]byte("<head><script>alert(1)</script></head>"))
	if !strings.HasSuffix(got, "; script-src 'self' 'sha256-bhHHL3z2vDgxUt0W3dWQOrprscmda2Y5pLsLg4GF+pI='") {
		t.Fatalf("inline script: %q", got)
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
