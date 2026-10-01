package vol

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"regexp"
	"sort"
	"strings"
	"syscall"
)

const (
	UploadsDir  = ".filebox/uploads"
	JobsDir     = ".filebox/jobs"
	VersionsDir = ".filebox/versions"
	TmpDir      = ".filebox/tmp"
	TrashDir    = ".trash"
)

var ErrBadPath = errors.New("bad path")

type Volume struct {
	Name string
	Path string
	Root *os.Root
}

type Usage struct {
	Used  uint64 `json:"used"`
	Free  uint64 `json:"free"`
	Total uint64 `json:"total"`
	Type  uint64 `json:"-"`
}

func (v *Volume) Usage() (Usage, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(v.Path, &st); err != nil {
		return Usage{}, err
	}
	b := uint64(st.Bsize)
	used, free := (uint64(st.Blocks)-uint64(st.Bfree))*b, uint64(st.Bavail)*b
	return Usage{Used: used, Free: free, Total: used + free, Type: uint64(st.Type)}, nil
}

func (v *Volume) Device() (string, error) {
	fi, err := os.Stat(v.Path)
	if err != nil {
		return "", err
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return "", errors.New("no device id")
	}
	h := sha256.Sum256(fmt.Appendf(nil, "%d", uint64(st.Dev)))
	return hex.EncodeToString(h[:6]), nil
}

func (v *Volume) Free() (uint64, error) {
	u, err := v.Usage()
	return u.Free, err
}

type Set struct {
	m     map[string]*Volume
	names []string
}

func Parse(specs []string) (*Set, error) {
	s := &Set{m: map[string]*Volume{}}
	for _, spec := range specs {
		name, dir, ok := strings.Cut(spec, "=")
		if !ok || !ValidName(name) || Reserved(name) {
			s.Close()
			return nil, fmt.Errorf("invalid volume %q, want name=path", spec)
		}
		if _, dup := s.m[name]; dup {
			s.Close()
			return nil, fmt.Errorf("duplicate volume %q", name)
		}
		root, err := os.OpenRoot(dir)
		if err != nil {
			s.Close()
			return nil, fmt.Errorf("volume %q: %w", name, err)
		}
		s.m[name] = &Volume{Name: name, Path: dir, Root: root}
		s.names = append(s.names, name)
	}
	sort.Strings(s.names)
	return s, nil
}

func (s *Set) Get(name string) (*Volume, bool) {
	v, ok := s.m[name]
	return v, ok
}

func (s *Set) Names() []string { return s.names }

func (s *Set) All() []*Volume {
	out := make([]*Volume, len(s.names))
	for i, n := range s.names {
		out[i] = s.m[n]
	}
	return out
}

func (s *Set) Close() error {
	var errs []error
	for _, v := range s.m {
		if err := v.Root.Close(); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (s *Set) Resolve(volName, p string) (*Volume, string, error) {
	v, ok := s.m[volName]
	if !ok {
		return nil, "", fs.ErrNotExist
	}
	rel, err := v.Clean(p)
	if err != nil {
		return nil, "", err
	}
	return v, rel, nil
}

var reserved = [...]string{".filebox", TrashDir}

func Reserved(name string) bool { return name == reserved[0] || name == reserved[1] }

func (v *Volume) Clean(p string) (string, error) {
	c, err := Clean(p)
	if err != nil {
		return "", err
	}
	first, _, _ := strings.Cut(c, "/")
	for _, r := range reserved {
		if strings.EqualFold(first, r) {
			a, err1 := v.Root.Lstat(first)
			b, err2 := v.Root.Lstat(r)
			if err1 == nil && err2 == nil && os.SameFile(a, b) {
				return "", ErrBadPath
			}
		}
	}
	return c, nil
}

const purging = ".purge-"

func Purging(name string) bool { return strings.HasPrefix(name, purging) }

var trashID = regexp.MustCompile(`^[0-9]{13}-[0-9a-f]{8}$`)

func TrashID(name string) bool { return trashID.MatchString(name) }

func Shared(fi fs.FileInfo) bool {
	st, ok := fi.Sys().(*syscall.Stat_t)
	return ok && st.Nlink > 1
}

func (v *Volume) PurgeTrash(id string) (int64, error) {
	if !ValidName(id) || Purging(id) {
		return 0, ErrBadPath
	}
	claimed := path.Join(TrashDir, purging+id)
	if err := v.Root.Rename(path.Join(TrashDir, id), claimed); err != nil {
		return 0, err
	}
	var freed int64
	fs.WalkDir(v.Root.FS(), claimed, func(p string, d fs.DirEntry, err error) error {
		if err == nil && d.Type().IsRegular() {
			if fi, err := d.Info(); err == nil && !Shared(fi) {
				freed += fi.Size()
			}
		}
		return nil
	})
	return freed, v.Root.RemoveAll(claimed)
}

func (v *Volume) SweepPurges() {
	f, err := v.Root.Open(TrashDir)
	if err != nil {
		return
	}
	names, _ := f.Readdirnames(-1)
	f.Close()
	for _, n := range names {
		if Purging(n) && TrashID(strings.TrimPrefix(n, purging)) {
			v.Root.RemoveAll(path.Join(TrashDir, n))
		}
	}
}

func Junk(name string, dir bool) bool {
	if dir {
		return strings.EqualFold(name, ".Trashes")
	}
	return strings.HasPrefix(name, "._") || strings.EqualFold(name, ".DS_Store") || strings.EqualFold(name, "Thumbs.db") || strings.EqualFold(name, "desktop.ini")
}

func ValidName(name string) bool {
	return name != "" && name != "." && name != ".." && !strings.ContainsAny(name, "/\\\x00")
}

func Clean(p string) (string, error) {
	if strings.IndexByte(p, 0) >= 0 {
		return "", ErrBadPath
	}
	c := strings.TrimPrefix(path.Clean("/"+p), "/")
	if c == "" {
		return ".", nil
	}
	first, _, _ := strings.Cut(c, "/")
	if Reserved(first) {
		return "", ErrBadPath
	}
	return c, nil
}

func Move(r *os.Root, from, to string) error {
	err := r.Link(from, to)
	if err == nil {
		if err := r.Remove(from); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		return nil
	}
	if errors.Is(err, fs.ErrExist) {
		return err
	}
	if _, err := r.Lstat(to); err == nil {
		return fs.ErrExist
	}
	return r.Rename(from, to)
}

func Numbered(name string, i int) string {
	ext := path.Ext(name)
	return fmt.Sprintf("%s (%d)%s", strings.TrimSuffix(name, ext), i, ext)
}
