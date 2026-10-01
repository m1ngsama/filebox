package serve

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
)

func unzip(t *testing.T, b []byte) map[string]string {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		c, _ := io.ReadAll(rc)
		rc.Close()
		out[f.Name] = string(c)
		if want := map[bool]uint16{true: zip.Store, false: zip.Deflate}[strings.HasSuffix(f.Name, ".jpg")]; !strings.HasSuffix(f.Name, "/") && f.Method != want {
			t.Errorf("%s method %d", f.Name, f.Method)
		}
	}
	return out
}

func keys(m map[string]string) []string {
	k := make([]string, 0, len(m))
	for n := range m {
		k = append(k, n)
	}
	slices.Sort(k)
	return k
}

func TestZip(t *testing.T) {
	outside := t.TempDir()
	os.WriteFile(filepath.Join(outside, "secret"), []byte("no"), 0o644)
	rt := root(t, map[string]string{
		"d/a.txt": "aaa", "d/sub/b.jpg": "jpg", "d/.env": "dot", "x/a.txt": "other", ".filebox/uploads/u": "tmp", "top.txt": "t",
	})
	dir := rt.Name()
	os.Symlink(filepath.Join(outside, "secret"), filepath.Join(dir, "d/escape"))
	os.Symlink(outside, filepath.Join(dir, "d/escapedir"))
	os.Symlink("..", filepath.Join(dir, "d/loop"))
	os.Symlink("a.txt", filepath.Join(dir, "d/alias.txt"))

	get := func(rels ...string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		Zip(w, httptest.NewRequest("GET", "/", nil), rt, rels, "", ZipName("vol", rels, ""))
		return w
	}
	w := get("d")
	if w.Code != 200 || w.Header().Get("Content-Length") != "" || w.Header().Get("Content-Type") != "application/zip" {
		t.Fatalf("%d %v", w.Code, w.Header())
	}
	if cd := w.Header().Get("Content-Disposition"); cd != `attachment; filename=d.zip` {
		t.Fatalf("disposition %q", cd)
	}
	got := unzip(t, w.Body.Bytes())
	want := []string{"d/", "d/.env", "d/a.txt", "d/alias.txt", "d/sub/", "d/sub/b.jpg"}
	if !slices.Equal(keys(got), want) || got["d/alias.txt"] != "aaa" {
		t.Fatalf("got %v", keys(got))
	}

	got = unzip(t, get("d/a.txt", "x/a.txt").Body.Bytes())
	if !slices.Equal(keys(got), []string{"a (1).txt", "a.txt"}) || got["a (1).txt"] != "other" {
		t.Fatalf("got %v", got)
	}

	w = get(".")
	got = unzip(t, w.Body.Bytes())
	if _, ok := got[".filebox/"]; ok || got["top.txt"] != "t" || got["d/a.txt"] != "aaa" {
		t.Fatalf("root %v", keys(got))
	}
	if cd := w.Header().Get("Content-Disposition"); cd != `attachment; filename=vol.zip` {
		t.Fatalf("disposition %q", cd)
	}

	for _, p := range []string{"d/escape", "d/escapedir"} {
		if w := get(p); w.Code == 200 {
			t.Fatalf("%s followed a symlink out of the volume", p)
		}
	}
	if w := get("missing"); w.Code != 404 {
		t.Fatalf("missing %d", w.Code)
	}
}

func TestZipName(t *testing.T) {
	for _, c := range []struct {
		rels       []string
		want, name string
	}{
		{[]string{"a/b"}, "", "b.zip"},
		{[]string{"a/b", "a/c"}, "", "a.zip"},
		{[]string{"b", "c"}, "", "vol.zip"},
		{[]string{"a/b", "a/c"}, "照片-2项", "照片-2项.zip"},
		{[]string{"a/b"}, "../evil", "b.zip"},
	} {
		if got := ZipName("vol", c.rels, c.want); got != c.name {
			t.Errorf("%v %q = %q, want %q", c.rels, c.want, got, c.name)
		}
	}
	w := httptest.NewRecorder()
	Zip(w, httptest.NewRequest("GET", "/", nil), root(t, map[string]string{"a": "x"}), []string{"a"}, "", "照片.zip")
	if cd := w.Header().Get("Content-Disposition"); !strings.Contains(cd, "filename*=utf-8''%E7%85%A7%E7%89%87.zip") {
		t.Fatalf("disposition %q", cd)
	}
}

func openFiles(t *testing.T) int {
	es, err := os.ReadDir("/dev/fd")
	if err != nil {
		t.Skip("no /dev/fd")
	}
	return len(es)
}

func TestZipClientGone(t *testing.T) {
	big := strings.Repeat("0123456789abcdef", 1<<16)
	files := map[string]string{}
	for _, n := range []string{"a", "b", "c", "d", "e", "f", "g", "h"} {
		files["d/"+n+".bin"] = big
	}
	rt := root(t, files)
	done := make(chan struct{})
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer close(done)
		Zip(w, r, rt, []string{"d"}, "", "d.zip")
	}))
	defer ts.Close()
	runtime.GC()
	before, routines := openFiles(t), runtime.NumGoroutine()
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, "GET", ts.URL, nil)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	io.ReadFull(res.Body, make([]byte, 4096))
	cancel()
	res.Body.Close()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("handler still running after the client left")
	}
	http.DefaultClient.CloseIdleConnections()
	ts.CloseClientConnections()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && (openFiles(t) > before || runtime.NumGoroutine() > routines) {
		time.Sleep(20 * time.Millisecond)
	}
	if n := openFiles(t); n > before {
		t.Fatalf("open files %d > %d", n, before)
	}
	if n := runtime.NumGoroutine(); n > routines {
		t.Fatalf("goroutines %d > %d", n, routines)
	}
}

func TestZipSymlinkedFolder(t *testing.T) {
	rt := root(t, map[string]string{"real/a.txt": "a"})
	os.Symlink("real", filepath.Join(rt.Name(), "link"))
	w := httptest.NewRecorder()
	Zip(w, httptest.NewRequest("GET", "/", nil), rt, []string{"link"}, "", "link.zip")
	if got := keys(unzip(t, w.Body.Bytes())); !slices.Equal(got, []string{"link/", "link/a.txt"}) {
		t.Fatalf("got %v", got)
	}
}

func TestZipUnreadable(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root reads everything")
	}
	rt := root(t, map[string]string{"d/a.jpg": strings.Repeat("x", 1<<20), "d/b.txt": "b", "c.txt": "c"})
	os.Chmod(filepath.Join(rt.Name(), "d/b.txt"), 0)
	os.Chmod(filepath.Join(rt.Name(), "c.txt"), 0)
	w := httptest.NewRecorder()
	Zip(w, httptest.NewRequest("GET", "/", nil), rt, []string{"c.txt"}, "", "c.zip")
	if w.Code != 403 || w.Header().Get("Content-Disposition") != "" {
		t.Fatalf("unreadable before any bytes: %d %v", w.Code, w.Header())
	}
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		Zip(w, r, rt, []string{"d"}, "", "d.zip")
	}))
	defer ts.Close()
	res, err := http.Get(ts.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if _, err := io.ReadAll(res.Body); err == nil {
		t.Fatal("a zip missing a file ended cleanly")
	}
}

func TestZipBackslashNamesStayInside(t *testing.T) {
	rt := root(t, map[string]string{`d/..\..\evil.bat`: "x"})
	w := httptest.NewRecorder()
	Zip(w, httptest.NewRequest("GET", "/", nil), rt, []string{"d"}, "", "d.zip")
	for _, n := range unzip(t, w.Body.Bytes()) {
		if strings.Contains(n, `\`) {
			t.Fatalf("entry %q keeps a backslash", n)
		}
	}
}
