package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEditSavesOverTheOpenedCopyOnly(t *testing.T) {
	f := newTestApp(t)
	f.write(t, "notes.md", "# old")
	tag := f.do("GET", "/raw/v/notes.md", nil).Header().Get("ETag")

	if w := f.do("PUT", "/api/file?vol=v&path=notes.md", body("# new"), "If-Match", `"0-0"`); w.Code != 412 || w.Header().Get("ETag") != tag {
		t.Fatalf("stale save %d %q", w.Code, w.Header().Get("ETag"))
	}
	w := f.do("PUT", "/api/file?vol=v&path=notes.md", body("# new"), "If-Match", tag)
	if w.Code != 200 || w.Header().Get("ETag") == tag {
		t.Fatalf("save %d %s", w.Code, w.Body)
	}
	if b, _ := os.ReadFile(filepath.Join(f.Dir, "notes.md")); string(b) != "# new" {
		t.Fatalf("content %q", b)
	}
	if v := f.versions(t, "notes.md"); len(v) != 1 {
		t.Fatalf("versions %v", v)
	}
	if w := f.do("PUT", "/api/file?vol=v&path=notes.md", body("again"), "If-Match", tag); w.Code != 412 {
		t.Fatalf("save over a newer copy %d", w.Code)
	}
	next := w.Header().Get("ETag")
	if w := f.do("PUT", "/api/file?vol=v&path=notes.md", strings.NewReader(strings.Repeat("x", 9<<20)), "If-Match", next); w.Code != 413 {
		t.Fatalf("huge save %d", w.Code)
	}
	if w := f.do("PUT", "/api/file?vol=v&path=.trash", body("x"), "If-Match", next); w.Code != 400 {
		t.Fatalf("reserved %d", w.Code)
	}
	if left, _ := filepath.Glob(filepath.Join(f.Dir, ".filebox", "uploads", "edit-*")); len(left) != 0 {
		t.Fatalf("staging left behind %v", left)
	}
}
