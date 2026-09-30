package app

import (
	"bufio"
	"encoding/json"
	"fmt"
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

func TestListStreamsHugeFolder(t *testing.T) {
	f := newTestApp(t)
	fill(t, filepath.Join(f.Dir, "big"), 2500)
	f.write(t, "big/sub/x", "")
	w := f.do("GET", "/api/ls?vol=v&path=big", nil)
	tag := w.Header().Get("ETag")
	if w.Code != 200 || w.Header().Get("Content-Type") != "application/x-ndjson" || !strings.HasPrefix(tag, `W/"`) {
		t.Fatalf("%d %v", w.Code, w.Header())
	}
	sc := bufio.NewScanner(w.Body)
	sc.Buffer(nil, 1<<20)
	var lines int
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
	if len(seen) != 2501 || !seen["sub"] || !seen["f0002499.jpg"] || lines != 3 {
		t.Fatalf("seen %d, lines %d", len(seen), lines)
	}
	if w := f.do("GET", "/api/ls?vol=v&path=big", nil, "If-None-Match", tag); w.Code != 304 {
		t.Fatalf("revalidate %d", w.Code)
	}
	f.write(t, "big/new", "")
	if w := f.do("GET", "/api/ls?vol=v&path=big", nil, "If-None-Match", tag); w.Code != 200 || w.Header().Get("ETag") == tag {
		t.Fatalf("after add %d %s", w.Code, w.Header().Get("ETag"))
	}
	if w := f.do("GET", "/api/ls?vol=v&path=big/sub", nil); w.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("small folder %v", w.Header())
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
