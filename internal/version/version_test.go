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
	e.s = &Store{DB: d, Now: func() time.Time { return e.now }, Usage: func(*vol.Volume) (vol.Usage, error) {
		return vol.Usage{Total: 1000, Free: 900}, nil
	}}
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

func TestThin(t *testing.T) {
	const min = int64(time.Minute / time.Millisecond)
	now := int64(1_800_000_000_000)
	var xs []Version
	for i := range 2000 {
		xs = append(xs, Version{ID: string(rune(i)), Created: now - int64(i)*10*min})
	}
	keep, drop := Thin(xs, now)
	if len(keep)+len(drop) != len(xs) || len(keep) < 40 || len(keep) > 45 {
		t.Fatalf("kept %d dropped %d", len(keep), len(drop))
	}
	for i := range 6 {
		if keep[i].ID != xs[i].ID {
			t.Fatal("dropped a version from the last hour")
		}
	}
	day := 24 * 60 * min
	hourly := 0
	for _, x := range keep {
		if a := now - x.Created; a >= 60*min && a < day {
			hourly++
		}
	}
	if hourly < 22 || hourly > 24 {
		t.Fatalf("%d versions from the last day", hourly)
	}
	var burst []Version
	for i := range 3 * PerFile {
		burst = append(burst, Version{Created: now - int64(i)*1000})
	}
	if keep, _ = Thin(burst, now); len(keep) != PerFile || keep[0].Created != now {
		t.Fatalf("cap kept %d", len(keep))
	}
	var old []Version
	for i := range 120 {
		old = append(old, Version{Created: now - 40*day - int64(i)*day})
	}
	keep, _ = Thin(old, now)
	if len(keep) < 17 || len(keep) > 18 {
		t.Fatalf("weekly thinning kept %d of 120 days", len(keep))
	}
}

func TestPruneOnCapture(t *testing.T) {
	e := setup(t)
	start := e.now
	for i := range 30 {
		e.now = start.Add(time.Duration(i) * 5 * time.Minute)
		e.capture(t, "a.txt", "x")
	}
	xs, _ := e.s.List("v", "a.txt")
	young := 0
	for _, x := range xs {
		if e.now.UnixMilli()-x.Created < time.Hour.Milliseconds() {
			young++
		}
	}
	if young != 12 || len(xs) >= 30 {
		t.Fatalf("%d versions, %d from the last hour", len(xs), young)
	}
	e.now = start.Add(48 * time.Hour)
	e.s.Prune(e.vols)
	if n := e.count(t, "a.txt"); n != 1 {
		t.Fatalf("after two days %d versions", n)
	}
	f, _ := os.Open(filepath.Join(e.dir, vol.VersionsDir))
	names, _ := f.Readdirnames(-1)
	f.Close()
	if len(names) != 2 {
		t.Fatalf("pruned bytes or sidecars left behind: %d files", len(names))
	}
}

func TestSpaceGuard(t *testing.T) {
	e := setup(t)
	e.capture(t, "a.txt", "0123456789")
	e.now = e.now.Add(time.Hour)
	e.capture(t, "b.txt", "0123456789")
	e.now = e.now.Add(time.Hour)
	e.capture(t, "c.txt", "0123456789")
	e.write(t, "live.txt", "keep")
	free := uint64(85)
	e.s.Usage = func(*vol.Volume) (vol.Usage, error) { return vol.Usage{Total: 1000, Free: free}, nil }
	e.now = e.now.Add(time.Minute)
	e.capture(t, "d.txt", "0123456789")
	if e.count(t, "a.txt") != 0 || e.count(t, "b.txt") != 0 || e.count(t, "c.txt") != 1 || e.count(t, "d.txt") != 1 {
		t.Fatal("guard did not drop the oldest versions first")
	}
	if e.read("live.txt") != "keep" {
		t.Fatal("guard touched a live file")
	}
	free = 10
	e.now = e.now.Add(10 * time.Minute)
	e.s.Prune(e.vols)
	if e.count(t, "c.txt") != 0 || e.count(t, "d.txt") != 1 {
		t.Fatal("guard did not respect the grace period")
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
