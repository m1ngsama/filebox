package vol

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func newSet(t *testing.T) (*Set, string) {
	t.Helper()
	dir := t.TempDir()
	s, err := Parse([]string{"data=" + dir})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s, dir
}

func TestParse(t *testing.T) {
	if _, err := Parse([]string{"noequals"}); err == nil {
		t.Fatal("want error for missing =")
	}
	if _, err := Parse([]string{"a/b=" + t.TempDir()}); err == nil {
		t.Fatal("want error for slash in name")
	}
	d := t.TempDir()
	if _, err := Parse([]string{"a=" + d, "a=" + d}); err == nil {
		t.Fatal("want error for duplicate")
	}
	if _, err := Parse([]string{"a=/does/not/exist"}); err == nil {
		t.Fatal("want error for missing dir")
	}
	if _, err := Parse([]string{".filebox=" + t.TempDir()}); err == nil {
		t.Fatal("want error for reserved name .filebox")
	}
	if _, err := Parse([]string{".trash=" + t.TempDir()}); err == nil {
		t.Fatal("want error for reserved name .trash")
	}
	s, _ := newSet(t)
	if v, ok := s.Get("data"); !ok || v.Name != "data" {
		t.Fatal("Get failed")
	}
	if got := s.Names(); len(got) != 1 || got[0] != "data" {
		t.Fatalf("Names = %v", got)
	}
}

func TestClean(t *testing.T) {
	ok := map[string]string{
		"": ".", "/": ".", "a": "a", "/a/b/": "a/b", "a/../b": "b",
		"../../etc/passwd": "etc/passwd", "a/./b": "a/b", "中文 #?%.txt": "中文 #?%.txt",
		"sub/.trash": "sub/.trash",
	}
	for in, want := range ok {
		got, err := Clean(in)
		if err != nil || got != want {
			t.Errorf("Clean(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	for _, bad := range []string{"a\x00b", ".trash", "/.trash/x", ".filebox/uploads/x"} {
		if _, err := Clean(bad); !errors.Is(err, ErrBadPath) {
			t.Errorf("Clean(%q) err = %v; want ErrBadPath", bad, err)
		}
	}
}

func TestValidName(t *testing.T) {
	for _, n := range []string{"a.txt", "中文", "a b #1.mkv"} {
		if !ValidName(n) {
			t.Errorf("ValidName(%q) = false", n)
		}
	}
	for _, n := range []string{"", ".", "..", "a/b", "a\x00", "a\\b"} {
		if ValidName(n) {
			t.Errorf("ValidName(%q) = true", n)
		}
	}
}

func TestSymlinkEscapeBlocked(t *testing.T) {
	s, dir := newSet(t)
	outside := t.TempDir()
	os.WriteFile(filepath.Join(outside, "secret"), []byte("x"), 0o644)
	if err := os.Symlink(outside, filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	v, _ := s.Get("data")
	if _, err := v.Root.Open("link/secret"); err == nil {
		t.Fatal("symlink escape was allowed")
	}
	if _, err := v.Root.Open("/etc/passwd"); err == nil {
		t.Fatal("absolute path was allowed")
	}
}

func TestInsideSymlinkAllowed(t *testing.T) {
	s, dir := newSet(t)
	os.Mkdir(filepath.Join(dir, "real"), 0o755)
	os.WriteFile(filepath.Join(dir, "real", "f"), []byte("ok"), 0o644)
	os.Symlink("real", filepath.Join(dir, "alias"))
	v, _ := s.Get("data")
	b, err := v.Root.ReadFile("alias/f")
	if err != nil || string(b) != "ok" {
		t.Fatalf("inside symlink: %q %v", b, err)
	}
}

func TestResolve(t *testing.T) {
	s, _ := newSet(t)
	v, rel, err := s.Resolve("data", "/a/../b")
	if err != nil || v.Name != "data" || rel != "b" {
		t.Fatalf("%v %q %v", v, rel, err)
	}
	if _, _, err := s.Resolve("nope", "a"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("err = %v", err)
	}
	if _, _, err := s.Resolve("data", ".trash"); !errors.Is(err, ErrBadPath) {
		t.Fatalf("err = %v", err)
	}
}

func TestFree(t *testing.T) {
	s, _ := newSet(t)
	v, _ := s.Get("data")
	if n, err := v.Free(); err != nil || n == 0 {
		t.Fatalf("Free = %d, %v", n, err)
	}
}

func TestMoveNeverReplaces(t *testing.T) {
	dir := t.TempDir()
	r, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	r.WriteFile("a", []byte("a"), 0o644)
	r.WriteFile("b", []byte("b"), 0o644)
	r.Mkdir("d", 0o755)
	r.WriteFile("d/x", []byte("x"), 0o644)
	if err := Move(r, "a", "b"); !errors.Is(err, fs.ErrExist) {
		t.Fatalf("file over file: %v", err)
	}
	if err := Move(r, "d", "b"); !errors.Is(err, fs.ErrExist) {
		t.Fatalf("folder over file: %v", err)
	}
	if err := Move(r, "a", "c"); err != nil {
		t.Fatal(err)
	}
	if err := Move(r, "d", "e"); err != nil {
		t.Fatal(err)
	}
	for p, want := range map[string]string{"b": "b", "c": "a", "e/x": "x"} {
		if got, _ := r.ReadFile(p); string(got) != want {
			t.Errorf("%s = %q", p, got)
		}
	}
	for _, p := range []string{"a", "d"} {
		if _, err := r.Lstat(p); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("%s still there", p)
		}
	}
}
