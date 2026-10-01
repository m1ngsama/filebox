package app

import (
	"bufio"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func fill(t *testing.T, dir string, n int) {
	t.Helper()
	os.MkdirAll(dir, 0o755)
	for i := range n {
		fh, err := os.Create(filepath.Join(dir, fmt.Sprintf("f%07d.jpg", i)))
		if err != nil {
			t.Fatal(err)
		}
		fh.Close()
	}
}

type sink struct {
	h       http.Header
	code, n int
	first   time.Duration
	start   time.Time
}

func (s *sink) Header() http.Header { return s.h }
func (s *sink) WriteHeader(c int)   { s.code = c }
func (s *sink) Flush()              {}
func (s *sink) Write(b []byte) (int, error) {
	if s.n == 0 {
		s.first = time.Since(s.start)
	}
	s.n += len(b)
	return len(b), nil
}

func streamed(t *testing.T, w *httptest.ResponseRecorder) map[string]bool {
	t.Helper()
	h := w.Header()
	if w.Code != 200 || h.Get("Content-Type") != "application/x-ndjson" || h.Get("ETag") != "" || h.Get("Cache-Control") != "no-store" {
		t.Fatalf("%d %v", w.Code, h)
	}
	var body io.Reader = w.Body
	if h.Get("Content-Encoding") == "gzip" {
		zr, err := gzip.NewReader(w.Body)
		if err != nil {
			t.Fatal(err)
		}
		body = zr
	}
	sc := bufio.NewScanner(body)
	sc.Buffer(nil, 1<<20)
	lines := 0
	seen := map[string]bool{}
	for sc.Scan() {
		var l struct {
			Entries []struct{ Name string }
		}
		if err := json.Unmarshal(sc.Bytes(), &l); err != nil {
			t.Fatal(err)
		}
		for _, e := range l.Entries {
			seen[e.Name] = true
		}
		lines++
	}
	if lines != 3 {
		t.Fatalf("%d lines", lines)
	}
	return seen
}

func TestListStreamsHugeFolder(t *testing.T) {
	f := newTestApp(t)
	fill(t, filepath.Join(f.Dir, "big"), 2500)
	f.write(t, "big/sub/x", "")
	f.write(t, "outside.txt", "")
	for _, enc := range []string{"", "gzip, br", "gzip;q=0"} {
		w := f.do("GET", "/api/ls?vol=v&path=big", nil, "Accept-Encoding", enc)
		if got := w.Header().Get("Content-Encoding"); (got == "gzip") != (enc == "gzip, br") {
			t.Fatalf("Accept-Encoding %q gave %q", enc, got)
		}
		if seen := streamed(t, w); len(seen) != 2501 || !seen["sub"] || !seen["f0002499.jpg"] {
			t.Fatalf("%q: seen %d", enc, len(seen))
		}
	}
	w := f.do("GET", "/api/ls?vol=v&path=big/sub", nil)
	if w.Header().Get("Content-Type") != "application/json" || !strings.HasPrefix(w.Header().Get("ETag"), `"`) {
		t.Fatalf("small folder %v", w.Header())
	}
	if w := f.do("GET", "/api/ls?vol=v&path=big/sub", nil, "If-None-Match", w.Header().Get("ETag")); w.Code != 304 {
		t.Fatalf("small folder revalidate %d", w.Code)
	}
	tok := mkShare(t, f, `{"vol":"v","path":"big","mode":"read"}`)
	for _, p := range []string{"", "../..", "/"} {
		seen := streamed(t, f.do("GET", "/s/"+tok+"/ls?path="+p, nil, "X-No-Auth", "1", "Accept-Encoding", "gzip"))
		if len(seen) != 2501 || seen["outside.txt"] || seen["big"] {
			t.Fatalf("share %q: seen %d", p, len(seen))
		}
	}
}

func TestListMemoryStaysBounded(t *testing.T) {
	n := 300_000
	if testing.Short() || os.Getenv("CI") != "" {
		n = 50_000
	}
	f := newTestApp(t)
	fill(t, filepath.Join(f.Dir, "big"), n)
	defer debug.SetGCPercent(debug.SetGCPercent(10))
	runtime.GC()
	var base, peak runtime.MemStats
	runtime.ReadMemStats(&base)
	var top atomic.Uint64
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		var m runtime.MemStats
		for {
			select {
			case <-stop:
				return
			case <-time.After(2 * time.Millisecond):
			}
			runtime.ReadMemStats(&m)
			if m.HeapAlloc > top.Load() {
				top.Store(m.HeapAlloc)
			}
		}
	}()
	r := httptest.NewRequest("GET", "/api/ls?vol=v&path=big", nil)
	r.AddCookie(f.Cookie)
	s := &sink{h: http.Header{}, start: time.Now()}
	f.H.ServeHTTP(s, r)
	elapsed := time.Since(s.start)
	close(stop)
	<-done
	runtime.ReadMemStats(&peak)
	grew := int64(max(top.Load(), peak.HeapAlloc)) - int64(base.HeapAlloc)
	t.Logf("%d entries: %d bytes, first byte %v, total %v, heap grew %d KiB", n, s.n, s.first, elapsed, grew>>10)
	if s.code != 0 && s.code != 200 || s.n < n*40 {
		t.Fatalf("code %d, %d bytes", s.code, s.n)
	}
	if grew > 4<<20 {
		t.Fatalf("heap grew %d KiB listing %d entries", grew>>10, n)
	}
}

func TestListShowsFolderSizesOnceIndexed(t *testing.T) {
	f := newTestApp(t)
	f.write(t, "big/a.bin", strings.Repeat("a", 1000))
	f.write(t, "big/deep/b.bin", strings.Repeat("b", 500))
	f.App.Index.Scan(f.App.Vols)
	w := f.do("GET", "/api/ls?vol=v&path=", nil)
	var got struct {
		Entries []struct {
			Name  string
			Bytes *int64
		}
	}
	json.Unmarshal(w.Body.Bytes(), &got)
	for _, e := range got.Entries {
		if e.Name == "big" && (e.Bytes == nil || *e.Bytes != 1500) {
			t.Fatalf("big folder bytes %v", e.Bytes)
		}
	}
	if w := f.do("GET", "/api/ls?vol=v&path=", nil); !strings.Contains(w.Body.String(), `"bytes":1500`) {
		t.Fatalf("listing %s", w.Body)
	}
}
