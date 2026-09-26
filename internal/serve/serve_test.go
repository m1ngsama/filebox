package serve

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func root(t *testing.T, files map[string]string) *os.Root {
	t.Helper()
	dir := t.TempDir()
	for n, c := range files {
		os.MkdirAll(filepath.Dir(filepath.Join(dir, n)), 0o755)
		os.WriteFile(filepath.Join(dir, n), []byte(c), 0o644)
	}
	r, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close() })
	return r
}

func TestRange(t *testing.T) {
	rt := root(t, map[string]string{"a.bin": "0123456789"})
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Range", "bytes=2-5")
	w := httptest.NewRecorder()
	File(w, req, rt, "a.bin", false)
	if w.Code != 206 || w.Body.String() != "2345" {
		t.Fatalf("%d %q", w.Code, w.Body.String())
	}
	etag := w.Header().Get("ETag")
	req = httptest.NewRequest("GET", "/", nil)
	req.Header.Set("If-None-Match", etag)
	w = httptest.NewRecorder()
	File(w, req, rt, "a.bin", false)
	if w.Code != 304 {
		t.Fatalf("conditional = %d", w.Code)
	}
}

func TestDangerousTypes(t *testing.T) {
	rt := root(t, map[string]string{"x.html": "<script>1</script>", "x.svg": "<svg/>", "x.txt": "<html>"})
	for _, n := range []string{"x.html", "x.svg"} {
		w := httptest.NewRecorder()
		File(w, httptest.NewRequest("GET", "/", nil), rt, n, false)
		h := w.Header()
		if !strings.HasPrefix(h.Get("Content-Disposition"), "attachment") ||
			!strings.Contains(h.Get("Content-Security-Policy"), "sandbox") ||
			h.Get("X-Content-Type-Options") != "nosniff" ||
			h.Get("Content-Type") != "application/octet-stream" {
			t.Errorf("%s headers: %v", n, h)
		}
	}
	w := httptest.NewRecorder()
	File(w, httptest.NewRequest("GET", "/", nil), rt, "x.txt", false)
	if !strings.HasPrefix(w.Header().Get("Content-Type"), "text/plain") {
		t.Errorf("txt was sniffed: %q", w.Header().Get("Content-Type"))
	}
}

func TestPDFNotSandboxed(t *testing.T) {
	rt := root(t, map[string]string{"a.pdf": "%PDF-1.4"})
	w := httptest.NewRecorder()
	File(w, httptest.NewRequest("GET", "/", nil), rt, "a.pdf", false)
	csp := w.Header().Get("Content-Security-Policy")
	if strings.Contains(csp, "sandbox") || !strings.Contains(csp, "frame-ancestors 'self'") {
		t.Fatalf("pdf csp %q", csp)
	}
}

func TestServeUnicodeName(t *testing.T) {
	rt := root(t, map[string]string{"中文 #1.txt": "hi"})
	w := httptest.NewRecorder()
	File(w, httptest.NewRequest("GET", "/", nil), rt, "中文 #1.txt", true)
	cd := w.Header().Get("Content-Disposition")
	if w.Code != 200 || !strings.Contains(cd, "filename*=utf-8''%E4%B8%AD%E6%96%87") {
		t.Fatalf("%d %q", w.Code, cd)
	}
}

func TestDirAndMissing(t *testing.T) {
	rt := root(t, map[string]string{"d/f": "x"})
	w := httptest.NewRecorder()
	File(w, httptest.NewRequest("GET", "/", nil), rt, "d", false)
	if w.Code != 400 {
		t.Errorf("dir = %d", w.Code)
	}
	w = httptest.NewRecorder()
	File(w, httptest.NewRequest("GET", "/", nil), rt, "nope", false)
	if w.Code != 404 {
		t.Errorf("missing = %d", w.Code)
	}
}
