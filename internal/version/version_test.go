package version

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
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
	ix.Moved = e.s.Moved
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
	e.s.DB.Exec(`DELETE FROM versions`)
	if err := e.s.Recover(e.vols); err != nil || e.count(t, "b.txt") != 2 {
		t.Fatalf("sidecars did not follow the rename: %v", err)
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

func TestCapKeepsLongTermHistory(t *testing.T) {
	const minute, day = int64(time.Minute / time.Millisecond), int64(24 * time.Hour / time.Millisecond)
	now := int64(1_800_000_000_000)
	var xs []Version
	for i := range 60 {
		xs = append(xs, Version{ID: "m" + strconv.Itoa(i), Created: now - int64(i)*minute/2})
	}
	for i := range 10 {
		xs = append(xs, Version{ID: "d" + strconv.Itoa(i), Created: now - int64(i+2)*day})
	}
	for i := range 8 {
		xs = append(xs, Version{ID: "w" + strconv.Itoa(i), Created: now - 40*day - int64(i)*7*day})
	}
	keep, drop := Thin(xs, now)
	if len(keep) != PerFile || len(keep)+len(drop) != len(xs) {
		t.Fatalf("kept %d dropped %d", len(keep), len(drop))
	}
	ids := map[string]bool{}
	for _, x := range keep {
		ids[x.ID] = true
	}
	for i := range 10 {
		if !ids["d"+strconv.Itoa(i)] {
			t.Fatalf("daily version %d dropped", i)
		}
	}
	for i := range 8 {
		if !ids["w"+strconv.Itoa(i)] {
			t.Fatalf("weekly version %d dropped", i)
		}
	}
	if !ids["m0"] || !ids["m31"] || ids["m32"] {
		t.Fatal("did not keep the newest recent versions")
	}
	var weekly []Version
	for i := range 3 * PerFile {
		weekly = append(weekly, Version{ID: strconv.Itoa(i), Created: now - 40*day - int64(i)*7*day})
	}
	if keep, _ = Thin(append([]Version{{ID: "new", Created: now}}, weekly...), now); len(keep) != PerFile || keep[0].ID != "new" || keep[PerFile-1].ID != strconv.Itoa(PerFile-2) {
		t.Fatal("with only representatives left the oldest should go first")
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

func TestGuardRunsBeforeCapture(t *testing.T) {
	e := setup(t)
	id := e.capture(t, "old.txt", "0123456789")
	e.now = e.now.Add(time.Hour)
	e.s.Usage = func(*vol.Volume) (vol.Usage, error) { return vol.Usage{Total: 1000, Free: 95}, nil }
	os.Mkdir(filepath.Join(e.dir, "d"), 0o755)
	if _, err := e.s.Capture(e.v, "d", Upload, 1); err == nil {
		t.Fatal("captured a folder")
	}
	if _, err := os.Stat(filepath.Join(e.dir, vol.VersionsDir, id)); !errors.Is(err, fs.ErrNotExist) || e.count(t, "old.txt") != 0 {
		t.Fatal("guard did not run before a failing capture")
	}
}

func TestRecoverKeepsRowsWhenTheStoreIsMissing(t *testing.T) {
	e := setup(t)
	e.capture(t, "a.txt", "a")
	aside := filepath.Join(t.TempDir(), "versions")
	os.Rename(filepath.Join(e.dir, vol.VersionsDir), aside)
	if err := e.s.Recover(e.vols); err != nil || e.count(t, "a.txt") != 1 {
		t.Fatalf("rows dropped while the versions folder was missing: %v", err)
	}
	os.Rename(aside, filepath.Join(e.dir, vol.VersionsDir))
	if err := e.s.Recover(e.vols); err != nil || e.count(t, "a.txt") != 1 {
		t.Fatalf("after the folder came back: %v", err)
	}
}

func TestExpireVersionsOfDeletedFiles(t *testing.T) {
	e := setup(t)
	for _, p := range []string{"gone.txt", "trashed/in.txt", "back.txt", "live.txt"} {
		e.capture(t, p, "old")
	}
	e.write(t, "live.txt", "new")
	e.write(t, ".trash/1/.origin", "trashed")
	e.s.Prune(e.vols)
	e.now = e.now.Add(OrphanTTL / 2)
	e.write(t, "back.txt", "again")
	e.s.Prune(e.vols)
	os.Remove(filepath.Join(e.dir, "back.txt"))
	e.now = e.now.Add(OrphanTTL/2 + time.Hour)
	e.s.Prune(e.vols)
	for p, want := range map[string]int{"gone.txt": 0, "trashed/in.txt": 1, "back.txt": 1, "live.txt": 1} {
		if n := e.count(t, p); n != want {
			t.Errorf("%s has %d versions, want %d", p, n, want)
		}
	}
	e.now = e.now.Add(OrphanTTL + time.Minute)
	e.s.Prune(e.vols)
	if e.count(t, "back.txt") != 0 {
		t.Error("back.txt versions kept after it was gone for the whole period")
	}
}

func TestReplaceLinksThenRenamesOver(t *testing.T) {
	for _, fallback := range []bool{false, true} {
		e := setup(t)
		if fallback {
			link = func(*os.Root, string, string) error { return &os.LinkError{Op: "link", Err: syscall.EXDEV} }
			t.Cleanup(func() { link = (*os.Root).Link })
		}
		e.write(t, "a.txt", "old")
		before, _ := os.Stat(filepath.Join(e.dir, "a.txt"))
		e.write(t, ".filebox/tmp/new", "new")
		id, err := e.s.Replace(e.v, ".filebox/tmp/new", "a.txt", WebDAV, 1)
		if err != nil || id == "" {
			t.Fatalf("fallback %v: %q %v", fallback, id, err)
		}
		kept, _ := os.Stat(filepath.Join(e.dir, vol.VersionsDir, id))
		if e.read("a.txt") != "new" || e.read(vol.VersionsDir+"/"+id) != "old" || !os.SameFile(before, kept) {
			t.Fatalf("fallback %v: live %q, version %q", fallback, e.read("a.txt"), e.read(vol.VersionsDir+"/"+id))
		}
		if _, err := os.Stat(filepath.Join(e.dir, ".filebox/tmp/new")); !errors.Is(err, fs.ErrNotExist) {
			t.Fatalf("fallback %v: temp file left", fallback)
		}
		e.write(t, "a.txt", "newer")
		if _, prev, err := e.s.Restore(e.v, id, 1); err != nil || prev == "" || e.read("a.txt") != "old" || e.read(vol.VersionsDir+"/"+prev) != "newer" {
			t.Fatalf("fallback %v: restore %v", fallback, err)
		}
	}
}

func TestReplaceFailureKeepsLiveFile(t *testing.T) {
	e := setup(t)
	e.write(t, "a.txt", "old")
	if _, err := e.s.Replace(e.v, ".filebox/tmp/missing", "a.txt", WebDAV, 1); err == nil {
		t.Fatal("replaced from a missing temp file")
	}
	if e.read("a.txt") != "old" || e.count(t, "a.txt") != 0 {
		t.Fatal("failed replace touched the live file or kept a version")
	}
	f, _ := os.Open(filepath.Join(e.dir, vol.VersionsDir))
	names, _ := f.Readdirnames(-1)
	f.Close()
	if len(names) != 0 {
		t.Fatalf("left behind %v", names)
	}
}

func TestExpireOnlyWhenTheFileIsReallyGone(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root reads unreadable folders")
	}
	e := setup(t)
	e.capture(t, "locked/a.txt", "old")
	e.write(t, "locked/a.txt", "new")
	e.capture(t, "file.txt", "old")
	e.write(t, "file.txt", "new")
	e.s.DB.Exec(`UPDATE versions SET path = 'file.txt/under' WHERE path = 'file.txt'`)
	os.Chmod(filepath.Join(e.dir, "locked"), 0)
	t.Cleanup(func() { os.Chmod(filepath.Join(e.dir, "locked"), 0o755) })
	for range 2 {
		e.s.Prune(e.vols)
		e.now = e.now.Add(OrphanTTL + time.Hour)
	}
	os.Chmod(filepath.Join(e.dir, "locked"), 0o755)
	if e.count(t, "locked/a.txt") != 1 || e.count(t, "file.txt/under") != 1 {
		t.Fatal("expired versions whose path could not be checked")
	}
}
