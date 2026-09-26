package upload

import (
	"encoding/base64"
	"io"
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
	url string
	dir string
}

func setup(t *testing.T) *env {
	t.Helper()
	dir := t.TempDir()
	vols, err := vol.Parse([]string{"secretvol=" + dir})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { vols.Close() })
	s, err := New(vols)
	if err != nil {
		t.Fatal(err)
	}
	v, _ := vols.Get("secretvol")
	h := s.Handler("/up/", Policy{
		Owner: func(r *http.Request) (string, bool) {
			o := r.Header.Get("X-Owner")
			return o, o != ""
		},
		Resolve: func(r *http.Request, meta map[string]string) (Target, error) {
			return TargetFor(v, ".", meta)
		},
	})
	ts := httptest.NewServer(h)
	t.Cleanup(ts.Close)
	return &env{srv: s, url: ts.URL, dir: dir}
}

func meta(kv ...string) string {
	var parts []string
	for i := 0; i+1 < len(kv); i += 2 {
		parts = append(parts, kv[i]+" "+base64.StdEncoding.EncodeToString([]byte(kv[i+1])))
	}
	return strings.Join(parts, ",")
}

func (e *env) send(method, url string, body io.Reader, hdr ...string) *http.Response {
	r, _ := http.NewRequest(method, e.url+url, body)
	r.Header.Set("Tus-Resumable", "1.0.0")
	r.Header.Set("X-Owner", "alice")
	for i := 0; i+1 < len(hdr); i += 2 {
		if hdr[i+1] == "" {
			r.Header.Del(hdr[i])
		} else {
			r.Header.Set(hdr[i], hdr[i+1])
		}
	}
	w, err := http.DefaultClient.Do(r)
	if err != nil {
		panic(err)
	}
	io.Copy(io.Discard, w.Body)
	w.Body.Close()
	return w
}

func (e *env) do(method, url, body string, hdr ...string) *http.Response {
	return e.send(method, url, strings.NewReader(body), hdr...)
}

func (e *env) create(t *testing.T, length int, name string) string {
	t.Helper()
	w := e.do("POST", "/up/", "", "Upload-Length", strconv.Itoa(length), "Upload-Metadata", meta("filename", name))
	if w.StatusCode != 201 {
		t.Fatalf("create %d", w.StatusCode)
	}
	return w.Header.Get("Location")
}

func (e *env) patch(loc string, off int, data string) *http.Response {
	return e.do("PATCH", loc, data, "Upload-Offset", strconv.Itoa(off), "Content-Type", "application/offset+octet-stream")
}

func TestTusFlow(t *testing.T) {
	e := setup(t)
	loc := e.create(t, 10, "a.bin")
	if id, ok := strings.CutPrefix(loc, "/up/"); !ok || strings.Contains(id, "/") || strings.Contains(id, "secretvol") {
		t.Fatalf("location %q", loc)
	}
	if w := e.do("HEAD", loc, ""); w.Header.Get("Upload-Offset") != "0" || w.Header.Get("Upload-Length") != "10" || w.Header.Get("Upload-Metadata") != "" {
		t.Fatalf("head %v", w.Header)
	}
	if w := e.patch(loc, 0, "01234"); w.StatusCode != 204 || w.Header.Get("Upload-Offset") != "5" {
		t.Fatalf("patch1 %d %v", w.StatusCode, w.Header)
	}
	if w := e.patch(loc, 3, "xx"); w.StatusCode != 409 {
		t.Fatalf("stale offset %d", w.StatusCode)
	}
	if w := e.do("PATCH", loc, "x", "Upload-Offset", "5"); w.StatusCode != 400 {
		t.Fatalf("wrong content type %d", w.StatusCode)
	}
	if w := e.patch(loc, 5, "56789"); w.StatusCode != 204 || w.Header.Get("Upload-Offset") != "10" {
		t.Fatalf("patch2 %d", w.StatusCode)
	}
	if b, _ := os.ReadFile(filepath.Join(e.dir, "a.bin")); string(b) != "0123456789" {
		t.Fatalf("final %q", b)
	}
	if ents, _ := os.ReadDir(filepath.Join(e.dir, vol.UploadsDir)); len(ents) != 0 {
		t.Fatalf("upload files left: %d", len(ents))
	}
	if w := e.do("HEAD", loc, ""); w.StatusCode != 404 {
		t.Fatalf("head after finish %d", w.StatusCode)
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
		wg.Go(func() { codes[i] = e.patch(loc, 0, "hello").StatusCode })
	}
	wg.Wait()
	if codes[0]+codes[1] != 204+409 {
		t.Fatalf("codes %v", codes)
	}
	if w := e.patch(loc, 5, "world"); w.StatusCode != 204 {
		t.Fatalf("finish patch %d", w.StatusCode)
	}
	if b, _ := os.ReadFile(filepath.Join(e.dir, "c.bin")); string(b) != "helloworld" {
		t.Fatalf("content %q", b)
	}
}

func TestTusPatchNotInterrupted(t *testing.T) {
	e := setup(t)
	loc := e.create(t, 10, "s.bin")
	pr, pw := io.Pipe()
	done := make(chan int)
	go func() {
		done <- e.send("PATCH", loc, pr, "Upload-Offset", "0", "Content-Type", "application/offset+octet-stream").StatusCode
	}()
	pw.Write([]byte("hel"))
	head := make(chan string)
	go func() { head <- e.do("HEAD", loc, "").Header.Get("Upload-Offset") }()
	pw.Write([]byte("lo"))
	pw.Close()
	if code := <-done; code != 204 {
		t.Fatalf("patch %d", code)
	}
	if off := <-head; off != "5" {
		t.Fatalf("head saw offset %s while a patch was running", off)
	}
}

func TestTusOverflow(t *testing.T) {
	e := setup(t)
	loc := e.create(t, 5, "o.bin")
	if w := e.patch(loc, 0, "12345678"); w.StatusCode != 413 {
		t.Fatalf("overflow %d", w.StatusCode)
	}
	if w := e.do("HEAD", loc, ""); w.Header.Get("Upload-Offset") != "0" {
		t.Fatalf("offset after overflow %q", w.Header.Get("Upload-Offset"))
	}
	if w := e.send("PATCH", loc, io.MultiReader(strings.NewReader("12345678")), "Upload-Offset", "0", "Content-Type", "application/offset+octet-stream"); w.StatusCode != 413 {
		t.Fatalf("chunked overflow %d", w.StatusCode)
	}
	if b, _ := os.ReadFile(filepath.Join(e.dir, "o.bin")); len(b) > 5 {
		t.Fatalf("kept %q beyond the declared length", b)
	}
}

func TestTusOwnerAndVersion(t *testing.T) {
	e := setup(t)
	loc := e.create(t, 5, "p.bin")
	if w := e.do("HEAD", loc, "", "X-Owner", "mallory"); w.StatusCode != 404 {
		t.Fatalf("other owner %d", w.StatusCode)
	}
	if w := e.do("DELETE", loc, "", "X-Owner", "mallory"); w.StatusCode != 404 {
		t.Fatalf("other owner delete %d", w.StatusCode)
	}
	if w := e.do("PATCH", "/up/../../x", "", "Upload-Offset", "0", "Content-Type", "application/offset+octet-stream"); w.StatusCode != 404 {
		t.Fatalf("bad id %d", w.StatusCode)
	}
	if w := e.do("PATCH", loc, "x", "Tus-Resumable", "", "Upload-Offset", "0", "Content-Type", "application/offset+octet-stream"); w.StatusCode != 412 {
		t.Fatalf("missing Tus-Resumable %d", w.StatusCode)
	}
	if w := e.do("POST", "/up/", "", "X-Owner", "", "Upload-Length", "1", "Upload-Metadata", meta("filename", "n")); w.StatusCode != 401 {
		t.Fatalf("no owner %d", w.StatusCode)
	}
	if w := e.do("DELETE", loc, ""); w.StatusCode != 204 {
		t.Fatalf("delete %d", w.StatusCode)
	}
	if w := e.do("HEAD", loc, ""); w.StatusCode != 404 {
		t.Fatalf("head after delete %d", w.StatusCode)
	}
}

func TestTusMetadataInjection(t *testing.T) {
	e := setup(t)
	w := e.do("POST", "/up/", "", "Upload-Length", "1", "Upload-Metadata", meta("filename", "m.txt", "owner", "mallory", "dir", ".."))
	if w.StatusCode != 201 {
		t.Fatalf("create %d", w.StatusCode)
	}
	loc := w.Header.Get("Location")
	if w := e.do("HEAD", loc, "", "X-Owner", "mallory"); w.StatusCode != 404 {
		t.Fatalf("injected owner %d", w.StatusCode)
	}
	e.patch(loc, 0, "x")
	if _, err := os.Stat(filepath.Join(e.dir, "m.txt")); err != nil {
		t.Fatal(err)
	}
}

func TestTusLimits(t *testing.T) {
	e := setup(t)
	if w := e.do("POST", "/up/", "", "Upload-Length", strconv.FormatInt(1<<62, 10), "Upload-Metadata", meta("filename", "big")); w.StatusCode != 507 {
		t.Fatalf("no space %d", w.StatusCode)
	}
	if w := e.do("POST", "/up/", "", "Upload-Length", "-1", "Upload-Metadata", meta("filename", "x")); w.StatusCode != 400 {
		t.Fatalf("negative length %d", w.StatusCode)
	}
	for _, bad := range []string{"../x", ".trash", "a/b", ""} {
		if w := e.do("POST", "/up/", "", "Upload-Length", "1", "Upload-Metadata", meta("filename", bad)); w.StatusCode != 400 {
			t.Errorf("filename %q → %d", bad, w.StatusCode)
		}
	}
	if w := e.do("POST", "/up/", "", "Upload-Length", "1", "Upload-Metadata", meta("filename", "e.txt", "relativePath", "../../escape/e.txt")); w.StatusCode != 400 {
		t.Errorf("relativePath escape → %d", w.StatusCode)
	}
	w := e.do("POST", "/up/", "", "Upload-Length", "0", "Upload-Metadata", meta("filename", "empty"))
	if w.StatusCode != 201 {
		t.Fatalf("zero %d", w.StatusCode)
	}
	if _, err := os.Stat(filepath.Join(e.dir, "empty")); err != nil {
		t.Fatal("zero-length upload not finalized")
	}
}

func TestTusFolder(t *testing.T) {
	e := setup(t)
	w := e.do("POST", "/up/", "", "Upload-Length", "1", "Upload-Metadata", meta("filename", "f.txt", "relativePath", "album/2024/f.txt"))
	e.patch(w.Header.Get("Location"), 0, "x")
	if _, err := os.Stat(filepath.Join(e.dir, "album/2024/f.txt")); err != nil {
		t.Fatal(err)
	}
}

func TestTusSweep(t *testing.T) {
	e := setup(t)
	loc := e.create(t, 10, "s.bin")
	e.patch(loc, 0, "123")
	up := filepath.Join(e.dir, vol.UploadsDir)
	os.WriteFile(filepath.Join(up, "orphan"), []byte("x"), 0o600)
	os.WriteFile(filepath.Join(up, "0-ORPHAN.info"), []byte("{}"), 0o600)
	e.srv.Sweep(24 * time.Hour)
	if ents, _ := os.ReadDir(up); len(ents) != 4 {
		t.Fatalf("fresh files swept: %d left", len(ents))
	}
	if w := e.do("HEAD", loc, ""); w.StatusCode != 200 {
		t.Fatal("fresh upload swept")
	}
	e.srv.Now = func() time.Time { return time.Now().Add(25 * time.Hour) }
	e.srv.Sweep(24 * time.Hour)
	if w := e.do("HEAD", loc, ""); w.StatusCode != 404 {
		t.Fatalf("stale upload survived %d", w.StatusCode)
	}
	if ents, _ := os.ReadDir(up); len(ents) != 0 {
		t.Fatalf("part files left: %d", len(ents))
	}
}

func TestTusOptions(t *testing.T) {
	e := setup(t)
	w := e.do("OPTIONS", "/up/", "", "Tus-Resumable", "", "X-Owner", "")
	ext := w.Header.Get("Tus-Extension")
	if w.StatusCode != 200 || w.Header.Get("Tus-Version") != "1.0.0" || !strings.Contains(ext, "creation") || !strings.Contains(ext, "termination") || strings.Contains(ext, "defer") {
		t.Fatalf("%d %v", w.StatusCode, w.Header)
	}
}
