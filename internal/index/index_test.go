package index

import (
	"fmt"
	"os"
	"path/filepath"
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
