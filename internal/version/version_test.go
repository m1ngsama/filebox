package version

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/m1ngsama/filebox/internal/db"
	"github.com/m1ngsama/filebox/internal/index"
	"github.com/m1ngsama/filebox/internal/vol"
)

type env struct {
	s    *Store
	v    *vol.Volume
	vols *vol.Set
	dir  string
	now  time.Time
}

func setup(t *testing.T) *env {
	t.Helper()
	dir := t.TempDir()
	vols, err := vol.Parse([]string{"v=" + dir})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { vols.Close() })
	d, err := db.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	v, _ := vols.Get("v")
	e := &env{v: v, vols: vols, dir: dir, now: time.UnixMilli(1_800_000_000_000)}
	e.s = &Store{DB: d, Now: func() time.Time { return e.now }}
	return e
}

func (e *env) write(t *testing.T, rel, body string) {
	t.Helper()
	os.MkdirAll(filepath.Dir(filepath.Join(e.dir, rel)), 0o755)
	if err := os.WriteFile(filepath.Join(e.dir, rel), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func (e *env) read(rel string) string {
	b, _ := os.ReadFile(filepath.Join(e.dir, rel))
	return string(b)
}

func (e *env) capture(t *testing.T, rel, body string) string {
	t.Helper()
	e.write(t, rel, body)
	id, err := e.s.Capture(e.v, rel, Upload, 1)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func (e *env) count(t *testing.T, rel string) int {
	t.Helper()
	xs, err := e.s.List("v", rel)
	if err != nil {
		t.Fatal(err)
	}
	return len(xs)
}

func TestCaptureAndRestore(t *testing.T) {
	e := setup(t)
	id := e.capture(t, "a.txt", "old")
	if _, err := os.Stat(filepath.Join(e.dir, "a.txt")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("live file still there")
	}
	if e.read(vol.VersionsDir+"/"+id) != "old" {
		t.Fatal("bytes not moved")
	}
	e.write(t, "a.txt", "new")
	dst, prev, err := e.s.Restore(e.v, id, 1)
	if err != nil || dst != "a.txt" || prev == "" {
		t.Fatalf("restore %q %q %v", dst, prev, err)
	}
	if e.read("a.txt") != "old" || e.read(vol.VersionsDir+"/"+prev) != "new" {
		t.Fatal("restore did not swap contents")
	}
	xs, _ := e.s.List("v", "a.txt")
	if len(xs) != 1 || xs[0].ID != prev || xs[0].Source != Restore {
		t.Fatalf("versions %+v", xs)
	}
	if _, _, err := e.s.Restore(e.v, id, 1); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("restoring a used version: %v", err)
	}
	if err := e.s.Delete(e.v, prev); err != nil || e.count(t, "a.txt") != 0 {
		t.Fatalf("delete %v", err)
	}
	if _, err := os.Stat(filepath.Join(e.dir, vol.VersionsDir, prev)); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("deleted version kept its bytes")
	}
	for _, bad := range []string{"", "../a.txt", "1-zz", prev + "/x"} {
		if _, err := e.s.Get("v", bad); err == nil {
			t.Fatalf("id %q accepted", bad)
		}
	}
}

func TestRestoreAfterDelete(t *testing.T) {
	e := setup(t)
	id := e.capture(t, "d/a.txt", "old")
	os.RemoveAll(filepath.Join(e.dir, "d"))
	dst, prev, err := e.s.Restore(e.v, id, 1)
	if err != nil || prev != "" || dst != "d/a.txt" || e.read("d/a.txt") != "old" {
		t.Fatalf("restore %q %q %v", dst, prev, err)
	}
	id = e.capture(t, "d/b", "x")
	os.Mkdir(filepath.Join(e.dir, "d/b"), 0o755)
	if _, _, err := e.s.Restore(e.v, id, 1); !errors.Is(err, fs.ErrExist) {
		t.Fatalf("restore over a folder: %v", err)
	}
}

func TestCaptureRefusesSpecialFiles(t *testing.T) {
	e := setup(t)
	os.Mkdir(filepath.Join(e.dir, "d"), 0o755)
	os.Symlink("d", filepath.Join(e.dir, "l"))
	for _, rel := range []string{"d", "l", "missing"} {
		if _, err := e.s.Capture(e.v, rel, Upload, 1); err == nil {
			t.Fatalf("captured %s", rel)
		}
	}
}

func TestVersionsFollowRename(t *testing.T) {
	e := setup(t)
	ix := index.New(e.s.DB)
	e.capture(t, "dir/a.txt", "1")
	e.capture(t, "b.txt", "2")
	e.write(t, "dir/a.txt", "live")
	os.Rename(filepath.Join(e.dir, "dir"), filepath.Join(e.dir, "moved"))
	ix.Rename(e.v, "dir", "moved")
	if e.count(t, "moved/a.txt") != 1 || e.count(t, "dir/a.txt") != 0 {
		t.Fatal("versions did not follow a folder rename")
	}
	os.Rename(filepath.Join(e.dir, "moved/a.txt"), filepath.Join(e.dir, "b.txt"))
	ix.Rename(e.v, "moved/a.txt", "b.txt")
	if e.count(t, "b.txt") != 2 {
		t.Fatal("renaming onto a name dropped its history")
	}
}

func TestRecover(t *testing.T) {
	e := setup(t)
	e.capture(t, "kept.txt", "k")
	lost := e.capture(t, "lost.txt", "l")
	orphan := e.capture(t, "sub/orphan.txt", "o")
	e.s.DB.Exec(`DELETE FROM versions WHERE id = ?`, orphan)
	os.Remove(filepath.Join(e.dir, vol.VersionsDir, lost))
	stray := "1-0123456789abcdef"
	os.WriteFile(filepath.Join(e.dir, vol.VersionsDir, stray+".path"), []byte("x"), 0o600)
	if err := e.s.Recover(e.vols); err != nil {
		t.Fatal(err)
	}
	if e.count(t, "kept.txt") != 1 || e.count(t, "lost.txt") != 0 {
		t.Fatal("rows not reconciled")
	}
	xs, _ := e.s.List("v", "sub/orphan.txt")
	if len(xs) != 1 || xs[0].ID != orphan || xs[0].Source != Recovered || xs[0].Size != 1 || xs[0].Created != e.now.UnixMilli() {
		t.Fatalf("orphan not adopted: %+v", xs)
	}
	for _, n := range []string{lost + ".path", stray + ".path"} {
		if _, err := os.Stat(filepath.Join(e.dir, vol.VersionsDir, n)); !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("%s left behind", n)
		}
	}
	if _, _, err := e.s.Restore(e.v, orphan, 1); err != nil || e.read("sub/orphan.txt") != "o" {
		t.Fatalf("adopted version not restorable: %v", err)
	}
}
