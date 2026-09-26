package vol

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"sort"
	"strings"
	"syscall"
)

const (
	UploadsDir = ".filebox/uploads"
	JobsDir    = ".filebox/jobs"
	TrashDir   = ".trash"
)

var ErrBadPath = errors.New("bad path")

type Volume struct {
	Name string
	Path string
	Root *os.Root
}

func (v *Volume) Free() (uint64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(v.Path, &st); err != nil {
		return 0, err
	}
	return uint64(st.Bavail) * uint64(st.Bsize), nil
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
	rel, err := Clean(p)
	if err != nil {
		return nil, "", err
	}
	return v, rel, nil
}

func Reserved(name string) bool { return name == ".filebox" || name == TrashDir }

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
