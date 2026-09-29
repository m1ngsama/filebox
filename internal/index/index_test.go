package index

import (
	"bytes"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/m1ngsama/filebox/internal/db"
	"github.com/m1ngsama/filebox/internal/vol"
)

type env struct {
	x    *Index
	vols *vol.Set
	dir  string
	out  string
}

func setup(t *testing.T) *env {
	t.Helper()
	dir, out := t.TempDir(), t.TempDir()
	vols, err := vol.Parse([]string{"v=" + dir})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { vols.Close() })
	d, err := db.Open(filepath.Join(t.TempDir(), "i.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return &env{x: New(d), vols: vols, dir: dir, out: out}
}

func (e *env) write(t *testing.T, rel, data string, mtime time.Time) {
	t.Helper()
	p := filepath.Join(e.dir, rel)
	os.MkdirAll(filepath.Dir(p), 0o755)
	if err := os.WriteFile(p, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	os.Chtimes(p, mtime, mtime)
}

func (e *env) recent(t *testing.T) map[string]File {
	t.Helper()
	fs, err := e.x.Recent(1000)
	if err != nil {
		t.Fatal(err)
	}
	m := map[string]File{}
	for _, f := range fs {
		m[f.Path] = f
	}
	return m
}

func (e *env) scan(t *testing.T) {
	t.Helper()
	if err := e.x.Scan(e.vols); err != nil {
		t.Fatal(err)
	}
}

func TestScanReconciles(t *testing.T) {
	e := setup(t)
	t0 := time.Unix(1_700_000_000, 0)
	e.write(t, "a.txt", "a", t0)
	e.write(t, "d/b.txt", "bb", t0.Add(time.Hour))
	e.write(t, "d/c.txt", "c", t0.Add(2*time.Hour))
	if e.x.Ready() {
		t.Fatal("ready before first scan")
	}
	e.scan(t)
	if !e.x.Ready() {
		t.Fatal("not ready after scan")
	}
	fs, _ := e.x.Recent(10)
	if len(fs) != 3 || fs[0].Path != "d/c.txt" || fs[2].Path != "a.txt" || fs[1].Name != "b.txt" || fs[1].Size != 2 || fs[1].Vol != "v" {
		t.Fatalf("recent %+v", fs)
	}
	e.write(t, "a.txt", "aaaa", t0.Add(3*time.Hour))
	os.Remove(filepath.Join(e.dir, "d/b.txt"))
	e.write(t, "new.txt", "n", t0.Add(4*time.Hour))
	e.scan(t)
	m := e.recent(t)
	if len(m) != 3 || m["a.txt"].Size != 4 || m["a.txt"].Mtime != t0.Add(3*time.Hour).UnixMilli() {
		t.Fatalf("after rescan %+v", m)
	}
	if _, ok := m["d/b.txt"]; ok {
		t.Fatal("deleted file still indexed")
	}
	if _, ok := m["new.txt"]; !ok {
		t.Fatal("new file missing")
	}
}

func TestScanSkipsReservedAndSymlinks(t *testing.T) {
	e := setup(t)
	now := time.Now()
	e.write(t, ".filebox/uploads/x", "x", now)
	e.write(t, ".trash/1-a/f.txt", "x", now)
	e.write(t, "d/.trash/kept.txt", "x", now)
	os.WriteFile(filepath.Join(e.out, "secret.txt"), []byte("s"), 0o644)
	os.Symlink(e.out, filepath.Join(e.dir, "escape"))
	os.Symlink(filepath.Join(e.out, "secret.txt"), filepath.Join(e.dir, "secret.txt"))
	e.scan(t)
	m := e.recent(t)
	if len(m) != 1 || m["d/.trash/kept.txt"].Name != "kept.txt" {
		t.Fatalf("indexed %+v", m)
	}
}

func TestTouch(t *testing.T) {
	e := setup(t)
	v, _ := e.vols.Get("v")
	e.scan(t)
	e.write(t, "d/a.txt", "a", time.Now())
	e.write(t, "d/e/b.txt", "b", time.Now())
	e.write(t, "dx.txt", "x", time.Now())
	e.x.Touch(v, "d")
	e.x.Touch(v, "dx.txt")
	if m := e.recent(t); len(m) != 3 {
		t.Fatalf("after touch %+v", m)
	}
	os.RemoveAll(filepath.Join(e.dir, "d"))
	e.x.Touch(v, "d")
	if m := e.recent(t); len(m) != 1 || m["dx.txt"].Size != 1 {
		t.Fatalf("after removal %+v", m)
	}
	var nilIndex *Index
	nilIndex.Touch(v, "dx.txt")
}

func TestScanLargeTree(t *testing.T) {
	e := setup(t)
	const n = 20000
	for i := range n / 100 {
		d := filepath.Join(e.dir, fmt.Sprintf("d%03d", i))
		os.Mkdir(d, 0o755)
		for j := range 100 {
			os.WriteFile(filepath.Join(d, fmt.Sprintf("f%03d", j)), nil, 0o644)
		}
	}
	start := time.Now()
	e.scan(t)
	if d := time.Since(start); d > 30*time.Second {
		t.Fatalf("scan took %v", d)
	}
	var count int
	e.x.db.QueryRow(`SELECT count(*) FROM files WHERE dir = 0`).Scan(&count)
	if count != n {
		t.Fatalf("indexed %d files", count)
	}
	start = time.Now()
	e.scan(t)
	t.Logf("rescan of %d files took %v", n, time.Since(start))
}

func (e *env) paths(t *testing.T) []string {
	t.Helper()
	rows, err := e.x.db.Query(`SELECT path FROM files WHERE vol = 'v' ORDER BY path`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var p string
		rows.Scan(&p)
		out = append(out, p)
	}
	return out
}

func TestRenameRewritesRowsWithoutWalking(t *testing.T) {
	e := setup(t)
	v, _ := e.vols.Get("v")
	const n = 20000
	b := &batch{db: e.x.db, vol: "v"}
	b.add(row{path: "a", dir: true})
	b.add(row{path: "a/b", dir: true})
	for i := range n {
		b.add(row{path: fmt.Sprintf("a/b/f%05d", i), size: 1, mtime: 1})
	}
	for _, p := range []string{"a/bc", "a/b-x", "a/b.txt", "z.txt"} {
		b.add(row{path: p, size: 1, mtime: 2})
	}
	b.add(row{path: "c/old", size: 1, mtime: 3})
	if err := b.flush(); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	e.x.Rename(v, "a/b", "c")
	t.Logf("renamed %d rows in %v", n, time.Since(start))
	got := e.paths(t)
	if len(got) != n+6 || got[0] != "a" || got[1] != "a/b-x" || got[2] != "a/b.txt" || got[3] != "a/bc" || got[4] != "c" || got[5] != "c/f00000" || got[len(got)-1] != "z.txt" {
		t.Fatalf("after dir rename %d rows: %v ... %v", len(got), got[:6], got[len(got)-2:])
	}
	e.x.Rename(v, "a/bc", "a/bd")
	if got := e.paths(t); got[3] != "a/bd" || len(got) != n+6 {
		t.Fatalf("after file rename %v", got[:6])
	}
}

func TestScanKeepsUnreadableSubtree(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads everything")
	}
	e := setup(t)
	e.write(t, "locked/a.txt", "a", time.Now())
	e.write(t, "locked/deep/b.txt", "b", time.Now())
	e.write(t, "open.txt", "o", time.Now())
	e.scan(t)
	locked := filepath.Join(e.dir, "locked")
	os.Chmod(locked, 0)
	t.Cleanup(func() { os.Chmod(locked, 0o755) })
	var logs bytes.Buffer
	old := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, nil)))
	t.Cleanup(func() { slog.SetDefault(old) })
	os.Remove(filepath.Join(e.dir, "open.txt"))
	e.scan(t)
	if m := e.recent(t); len(m) != 2 || m["locked/deep/b.txt"].Name != "b.txt" {
		t.Fatalf("after rescan %+v", m)
	}
	if c := strings.Count(logs.String(), "level=WARN"); c != 1 || !strings.Contains(logs.String(), "path=locked") {
		t.Fatalf("logs %q", logs.String())
	}
}

func TestMovedToRoot(t *testing.T) {
	e := setup(t)
	for _, c := range []struct{ from, to, path, want string }{
		{"x", ".", "x", "."},
		{"x", ".", "x/f", "f"},
		{"x/y", ".", "x/y/z/f", "z/f"},
		{".", "x", "f", "x/f"},
		{".", "x", ".", "x"},
		{".", ".", "f", "f"},
		{"a", "b/c", "a/f", "b/c/f"},
	} {
		expr, args := moved(c.from, c.to)
		var got string
		if err := e.x.db.QueryRow(`SELECT `+expr+` FROM (SELECT ? AS path)`, append(args, c.path)...).Scan(&got); err != nil {
			t.Fatal(err)
		}
		if got != c.want {
			t.Errorf("moved(%q, %q) of %q = %q, want %q", c.from, c.to, c.path, got, c.want)
		}
	}
}
