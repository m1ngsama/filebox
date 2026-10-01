package app

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/m1ngsama/filebox/internal/auth"
	"github.com/m1ngsama/filebox/internal/db"
	"github.com/m1ngsama/filebox/internal/index"
	"github.com/m1ngsama/filebox/internal/passkey"
	"github.com/m1ngsama/filebox/internal/thumb"
	"github.com/m1ngsama/filebox/internal/upload"
	"github.com/m1ngsama/filebox/internal/version"
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
		"index.html":           {Data: []byte("<!doctype html>app")},
		"share.html":           {Data: []byte("<!doctype html>share")},
		"reader.html":          {Data: []byte("<!doctype html><script>var t=1</script>reader")},
		"sw.js":                {Data: []byte("self")},
		"manifest.webmanifest": {Data: []byte("{}")},
		"assets/app-1.js":      {Data: []byte("js")},
		"assets/app-1.js.br":   {Data: []byte("js-br")},
		"assets/app-1.js.gz":   {Data: []byte("js-gz")},
		"assets/app-1.css":     {Data: []byte("css")},
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
	vs := &version.Store{DB: d}
	up.Versions = vs
	ix.Moved = vs.Moved
	ap := &App{Vols: vols, DB: d, Auth: a, Web: web, Uploads: up, Thumbs: thumb.New("", t.TempDir()), Passkeys: pk, Index: ix, Versions: vs}
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
		if hdr[i] == "X-Host" {
			r.Host = hdr[i+1]
			continue
		}
		if hdr[i] == "X-Remote-Addr" {
			r.RemoteAddr = hdr[i+1]
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
			h.Get("Cache-Control") != "public, max-age=31536000, immutable" ||
			h.Get("Content-Length") != strconv.Itoa(len(c.body)) {
			t.Errorf("Accept-Encoding %q: %d %q %v", c.accept, w.Code, w.Body.String(), h)
		}
	}
	w = f.do("GET", "/assets/app-1.js", nil, "X-No-Auth", "1", "Accept-Encoding", "br", "Range", "bytes=1-2")
	if w.Code != 206 || w.Body.String() != "s-" || w.Header().Get("Content-Length") != "2" || w.Header().Get("Content-Encoding") != "br" {
		t.Errorf("range %d %q %v", w.Code, w.Body.String(), w.Header())
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
	if got := spaPolicy("index.html", []byte(`<script type="module" src="/a.js"></script>`)); got != spaCSP {
		t.Fatalf("no inline script: %q", got)
	}
	got := spaPolicy("index.html", []byte("<head><script>alert(1)</script></head>"))
	if !strings.HasSuffix(got, "; script-src 'self' 'sha256-bhHHL3z2vDgxUt0W3dWQOrprscmda2Y5pLsLg4GF+pI='") {
		t.Fatalf("inline script: %q", got)
	}
}

func TestReaderPolicyFollowsConfiguredOrigins(t *testing.T) {
	f := newTestApp(t)
	f.App.Origins = []string{"https://files.example.com", "http://localhost:5280"}
	f.H = f.App.Handler()
	script := func(hdr ...string) string {
		w := f.do("GET", "/reader", nil, hdr...)
		if w.Code != 200 {
			t.Fatalf("reader %d", w.Code)
		}
		csp := w.Header().Get("Content-Security-Policy")
		return csp
	}
	for _, c := range []struct {
		hdr  []string
		want string
	}{
		{[]string{"X-Host", "files.example.com"}, "https://files.example.com/assets/ 'unsafe-inline'"},
		{[]string{"X-Host", "127.0.0.1:9000", "X-Forwarded-Host", "files.example.com"}, "https://files.example.com/assets/ 'unsafe-inline'"},
		{[]string{"X-Host", "127.0.0.1:9000"}, "127.0.0.1:9000/assets/ https://files.example.com/assets/ http://localhost:5280/assets/ 'unsafe-inline'"},
		{[]string{"X-Host", "evil host"}, "https://files.example.com/assets/ http://localhost:5280/assets/ 'unsafe-inline'"},
	} {
		got := script(c.hdr...)
		if !strings.Contains(got, "style-src "+c.want) || !strings.Contains(got, "script-src "+strings.TrimSuffix(c.want, "'unsafe-inline'")+"'sha256-") {
			t.Errorf("%v: %s", c.hdr, got)
		}
		if strings.Contains(c.want, "127.0.0.1") != strings.Contains(got, "127.0.0.1") || strings.Contains(got, "evil") {
			t.Errorf("%v: request host leaked into the policy: %s", c.hdr, got)
		}
	}
}

func TestReaderPolicy(t *testing.T) {
	f := newTestApp(t)
	w := f.do("GET", "/reader", nil)
	csp := w.Header().Get("Content-Security-Policy")
	if w.Code != 200 || !strings.Contains(csp, "script-src example.com/assets/ 'sha256-") {
		t.Fatalf("reader %d %q", w.Code, csp)
	}
	for _, s := range []string{"default-src 'none'", "img-src blob: data:", "frame-src blob:", "frame-ancestors 'self'", "connect-src 'self'"} {
		if !strings.Contains(csp, s) {
			t.Errorf("reader policy misses %q: %q", s, csp)
		}
	}
	for _, s := range []string{"'self'/assets", "script-src 'self'", "'unsafe-eval'"} {
		if strings.Contains(csp, s) {
			t.Errorf("reader policy has %q: %q", s, csp)
		}
	}
	if w := f.do("GET", "/reader", nil, "X-Host", "evil host"); w.Code != 400 {
		t.Fatalf("bad host = %d", w.Code)
	}
	spa := f.do("GET", "/files/v/", nil).Header().Get("Content-Security-Policy")
	if spa != spaCSP || !strings.Contains(spa, "frame-ancestors 'none'") || strings.Contains(spa, "'unsafe-inline'") {
		t.Errorf("spa policy: %q", spa)
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

func TestPWAFiles(t *testing.T) {
	f := newTestApp(t)
	if w := f.do("GET", "/sw.js", nil, "X-No-Auth", "1"); w.Code != 200 || w.Header().Get("Cache-Control") != "no-cache" {
		t.Fatalf("sw.js %d %v", w.Code, w.Header())
	}
	if w := f.do("GET", "/manifest.webmanifest", nil, "X-No-Auth", "1"); w.Code != 200 || w.Header().Get("Content-Type") != "application/manifest+json" {
		t.Fatalf("manifest %d %v", w.Code, w.Header())
	}
	if w := f.do("POST", "/share-target", nil, "X-No-Auth", "1"); w.Code != 303 || w.Header().Get("Location") != "/?share-target" {
		t.Fatalf("share target without a service worker %d %v", w.Code, w.Header())
	}
}

func TestMissingAssetIs404(t *testing.T) {
	f := newTestApp(t)
	if w := f.do("GET", "/assets/gone-1.js", nil, "X-No-Auth", "1"); w.Code != 404 || strings.Contains(w.Body.String(), "doctype") {
		t.Fatalf("missing asset %d %q", w.Code, w.Body)
	}
}
