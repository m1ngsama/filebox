package upload

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/m1ngsama/filebox/internal/vol"
)

type env struct {
	srv *Server
	h   http.Handler
	dir string
}

func setup(t *testing.T) *env {
	t.Helper()
	dir := t.TempDir()
	vols, err := vol.Parse([]string{"v=" + dir})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { vols.Close() })
	s := &Server{Vols: vols, Dir: t.TempDir()}
	v, _ := vols.Get("v")
	h := s.Handler("/up/", Policy{
		Owner: func(r *http.Request) (string, bool) {
			o := r.Header.Get("X-Owner")
			return o, o != ""
		},
		Resolve: func(r *http.Request, meta map[string]string) (Target, error) {
			return TargetFor(v, ".", meta)
		},
	})
	return &env{srv: s, h: h, dir: dir}
}

func meta(kv ...string) string {
	var parts []string
	for i := 0; i+1 < len(kv); i += 2 {
		parts = append(parts, kv[i]+" "+base64.StdEncoding.EncodeToString([]byte(kv[i+1])))
	}
	return strings.Join(parts, ",")
}

func (e *env) do(method, url, body string, hdr ...string) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, url, strings.NewReader(body))
	r.Header.Set("Tus-Resumable", "1.0.0")
	r.Header.Set("X-Owner", "alice")
	for i := 0; i+1 < len(hdr); i += 2 {
		r.Header.Set(hdr[i], hdr[i+1])
	}
	w := httptest.NewRecorder()
	e.h.ServeHTTP(w, r)
	return w
}

func (e *env) create(t *testing.T, length int, name string) string {
	t.Helper()
	w := e.do("POST", "/up/", "", "Upload-Length", strconv.Itoa(length), "Upload-Metadata", meta("filename", name))
	if w.Code != 201 {
		t.Fatalf("create %d %s", w.Code, w.Body)
	}
	return w.Header().Get("Location")
}

func (e *env) patch(loc string, off int, data string) *httptest.ResponseRecorder {
	return e.do("PATCH", loc, data, "Upload-Offset", strconv.Itoa(off), "Content-Type", "application/offset+octet-stream")
}

func TestTusFlow(t *testing.T) {
	e := setup(t)
	loc := e.create(t, 10, "a.bin")
	if !strings.HasPrefix(loc, "/up/") {
		t.Fatalf("location %q", loc)
	}
	if w := e.do("HEAD", loc, ""); w.Header().Get("Upload-Offset") != "0" || w.Header().Get("Upload-Length") != "10" {
		t.Fatalf("head %v", w.Header())
	}
	if w := e.patch(loc, 0, "01234"); w.Code != 204 || w.Header().Get("Upload-Offset") != "5" {
		t.Fatalf("patch1 %d %v", w.Code, w.Header())
	}
	if w := e.patch(loc, 3, "xx"); w.Code != 409 {
		t.Fatalf("stale offset %d", w.Code)
	}
	if w := e.do("PATCH", loc, "x", "Upload-Offset", "5"); w.Code != 415 {
		t.Fatalf("wrong content type %d", w.Code)
	}
	if w := e.patch(loc, 5, "56789"); w.Code != 204 || w.Header().Get("Upload-Offset") != "10" {
		t.Fatalf("patch2 %d", w.Code)
	}
	if b, _ := os.ReadFile(filepath.Join(e.dir, "a.bin")); string(b) != "0123456789" {
		t.Fatalf("final %q", b)
	}
	if w := e.do("HEAD", loc, ""); w.Code != 404 {
		t.Fatalf("head after finish %d", w.Code)
	}
}

func TestTusUnicodeFilename(t *testing.T) {
	e := setup(t)
	loc := e.create(t, 2, "中文 #1 %.mkv")
	e.patch(loc, 0, "ok")
	if b, err := os.ReadFile(filepath.Join(e.dir, "中文 #1 %.mkv")); err != nil || string(b) != "ok" {
		t.Fatalf("%q %v", b, err)
	}
}

func TestTusNameCollision(t *testing.T) {
	e := setup(t)
	os.WriteFile(filepath.Join(e.dir, "a.txt"), []byte("old"), 0o644)
	l1, l2 := e.create(t, 1, "a.txt"), e.create(t, 1, "a.txt")
	var wg sync.WaitGroup
	for _, l := range []string{l1, l2} {
		wg.Add(1)
		go func() { defer wg.Done(); e.patch(l, 0, "n") }()
	}
	wg.Wait()
	for _, n := range []string{"a.txt", "a (1).txt", "a (2).txt"} {
		if _, err := os.Stat(filepath.Join(e.dir, n)); err != nil {
			t.Errorf("missing %s", n)
		}
	}
	if b, _ := os.ReadFile(filepath.Join(e.dir, "a.txt")); string(b) != "old" {
		t.Fatal("existing file overwritten")
	}
}

func TestTusConcurrentPatchSameUpload(t *testing.T) {
	e := setup(t)
	loc := e.create(t, 10, "c.bin")
	var wg sync.WaitGroup
	codes := make([]int, 2)
	for i := range codes {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			codes[i] = e.patch(loc, 0, "hello").Code
		}(i)
	}
	wg.Wait()
	var got204, got409 int
	for _, c := range codes {
		switch c {
		case 204:
			got204++
		case 409:
			got409++
		default:
			t.Fatalf("unexpected code %d", c)
		}
	}
	if got204 != 1 || got409 != 1 {
		t.Fatalf("codes %v", codes)
	}
	if w := e.patch(loc, 5, "world"); w.Code != 204 {
		t.Fatalf("finish patch %d", w.Code)
	}
	if b, _ := os.ReadFile(filepath.Join(e.dir, "c.bin")); string(b) != "helloworld" {
		t.Fatalf("content %q", b)
	}
}

func TestTusOverflow(t *testing.T) {
	e := setup(t)
	loc := e.create(t, 5, "o.bin")
	if w := e.patch(loc, 0, "12345678"); w.Code != 413 {
		t.Fatalf("overflow %d", w.Code)
	}
	if w := e.do("HEAD", loc, ""); w.Header().Get("Upload-Offset") != "0" {
		t.Fatalf("offset after overflow %q", w.Header().Get("Upload-Offset"))
	}
}

func TestTusOwnerAndVersion(t *testing.T) {
	e := setup(t)
	loc := e.create(t, 5, "p.bin")
	if w := e.do("HEAD", loc, "", "X-Owner", "mallory"); w.Code != 404 {
		t.Fatalf("other owner %d", w.Code)
	}
	r := httptest.NewRequest("HEAD", loc, nil)
	r.Header.Set("X-Owner", "alice")
	w := httptest.NewRecorder()
	e.h.ServeHTTP(w, r)
	if w.Code != 412 {
		t.Fatalf("missing Tus-Resumable %d", w.Code)
	}
	r = httptest.NewRequest("POST", "/up/", nil)
	r.Header.Set("Tus-Resumable", "1.0.0")
	r.Header.Set("Upload-Length", "1")
	w = httptest.NewRecorder()
	e.h.ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatalf("no owner %d", w.Code)
	}
}

func TestTusLimits(t *testing.T) {
	e := setup(t)
	if w := e.do("POST", "/up/", "", "Upload-Length", strconv.FormatInt(1<<62, 10), "Upload-Metadata", meta("filename", "big")); w.Code != 507 {
		t.Fatalf("no space %d", w.Code)
	}
	if w := e.do("POST", "/up/", "", "Upload-Length", "-1", "Upload-Metadata", meta("filename", "x")); w.Code != 400 {
		t.Fatalf("negative length %d", w.Code)
	}
	for _, bad := range []string{"../x", ".trash", "a/b", ""} {
		if w := e.do("POST", "/up/", "", "Upload-Length", "1", "Upload-Metadata", meta("filename", bad)); w.Code != 400 {
			t.Errorf("filename %q → %d", bad, w.Code)
		}
	}
	if w := e.do("POST", "/up/", "", "Upload-Length", "1", "Upload-Metadata", meta("filename", "e.txt", "relativePath", "../../escape/e.txt")); w.Code != 400 {
		t.Errorf("relativePath escape → %d", w.Code)
	}
	w := e.do("POST", "/up/", "", "Upload-Length", "0", "Upload-Metadata", meta("filename", "empty"))
	if w.Code != 201 {
		t.Fatalf("zero %d", w.Code)
	}
	if _, err := os.Stat(filepath.Join(e.dir, "empty")); err != nil {
		t.Fatal("zero-length upload not finalized")
	}
}

func TestTusFolder(t *testing.T) {
	e := setup(t)
	w := e.do("POST", "/up/", "", "Upload-Length", "1", "Upload-Metadata", meta("filename", "f.txt", "relativePath", "album/2024/f.txt"))
	e.patch(w.Header().Get("Location"), 0, "x")
	if _, err := os.Stat(filepath.Join(e.dir, "album/2024/f.txt")); err != nil {
		t.Fatal(err)
	}
}

func TestTusSweep(t *testing.T) {
	e := setup(t)
	loc := e.create(t, 10, "s.bin")
	e.patch(loc, 0, "123")
	e.srv.Sweep(24 * time.Hour)
	if w := e.do("HEAD", loc, ""); w.Code != 200 {
		t.Fatal("fresh upload swept")
	}
	e.srv.Now = func() time.Time { return time.Now().Add(25 * time.Hour) }
	e.srv.Sweep(24 * time.Hour)
	if w := e.do("HEAD", loc, ""); w.Code != 404 {
		t.Fatalf("stale upload survived %d", w.Code)
	}
	ents, _ := os.ReadDir(filepath.Join(e.dir, vol.UploadsDir))
	if len(ents) != 0 {
		t.Fatalf("part files left: %d", len(ents))
	}
}

func TestTusOptions(t *testing.T) {
	e := setup(t)
	r := httptest.NewRequest("OPTIONS", "/up/", nil)
	w := httptest.NewRecorder()
	e.h.ServeHTTP(w, r)
	if w.Code != 204 || w.Header().Get("Tus-Version") != "1.0.0" || !strings.Contains(w.Header().Get("Tus-Extension"), "creation") {
		t.Fatalf("%d %v", w.Code, w.Header())
	}
}
