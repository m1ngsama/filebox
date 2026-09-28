package api

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/m1ngsama/filebox/internal/vol"
)

func TestPlaceAcrossDevices(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	vols, err := vol.Parse([]string{"a=" + a, "b=" + b})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { vols.Close() })
	va, _ := vols.Get("a")
	vb, _ := vols.Get("b")
	var appear func(r *os.Root, to string)
	exdev := func(r *os.Root, from, to string) error {
		if appear != nil {
			appear(r, to)
		}
		return &os.LinkError{Op: "rename", Old: from, New: to, Err: syscall.EXDEV}
	}
	oldLink, oldRename := link, rename
	link, rename = exdev, exdev
	t.Cleanup(func() { link, rename = oldLink, oldRename })

	os.MkdirAll(filepath.Join(a, "tree/sub"), 0o755)
	os.WriteFile(filepath.Join(a, "tree/x.txt"), []byte("x"), 0o644)
	os.WriteFile(filepath.Join(a, "tree/sub/y.txt"), []byte("y"), 0o644)
	os.WriteFile(filepath.Join(a, "f.txt"), []byte("f"), 0o644)
	if err := run(va, vb, "tree", "tree", "j1", true, &job{}); err != nil {
		t.Fatalf("move dir: %v", err)
	}
	if got, _ := os.ReadFile(filepath.Join(b, "tree/sub/y.txt")); string(got) != "y" {
		t.Fatalf("moved content %q", got)
	}
	if _, err := os.Stat(filepath.Join(a, "tree")); !os.IsNotExist(err) {
		t.Fatal("source survived the move")
	}
	x := &job{}
	if err := run(va, vb, "f.txt", "f.txt", "j2", false, x); err != nil {
		t.Fatalf("copy file: %v", err)
	}
	if d, n := x.done.Load(), x.total.Load(); d != 2 || n != 2 {
		t.Fatalf("progress %d/%d, want the fallback copy counted", d, n)
	}
	if got, _ := os.ReadFile(filepath.Join(b, "f.txt")); string(got) != "f" {
		t.Fatalf("copied content %q", got)
	}
	if ents, _ := os.ReadDir(filepath.Join(b, vol.JobsDir)); len(ents) != 0 {
		t.Fatalf("staging left: %d", len(ents))
	}

	appear = func(r *os.Root, to string) { r.WriteFile(to, []byte("theirs"), 0o644) }
	if err := run(va, vb, "f.txt", "g.txt", "j3", false, &job{}); errorCode(err) != "exists" {
		t.Fatalf("file raced: %v", err)
	}
	if got, _ := os.ReadFile(filepath.Join(b, "g.txt")); string(got) != "theirs" {
		t.Fatalf("raced file overwritten: %q", got)
	}
	os.MkdirAll(filepath.Join(a, "d"), 0o755)
	appear = func(r *os.Root, to string) { r.Mkdir(to, 0o755) }
	if err := run(va, vb, "d", "d", "j4", false, &job{}); errorCode(err) != "exists" {
		t.Fatalf("dir raced: %v", err)
	}
}
