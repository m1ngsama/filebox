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

type Usage struct {
	Used  uint64 `json:"used"`
	Free  uint64 `json:"free"`
	Total uint64 `json:"total"`
}

func (v *Volume) Usage() (Usage, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(v.Path, &st); err != nil {
		return Usage{}, err
	}
	b := uint64(st.Bsize)
	return Usage{Used: (uint64(st.Blocks) - uint64(st.Bfree)) * b, Free: uint64(st.Bavail) * b, Total: uint64(st.Blocks) * b}, nil
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

func Numbered(name string, i int) string {
	ext := path.Ext(name)
	return fmt.Sprintf("%s (%d)%s", strings.TrimSuffix(name, ext), i, ext)
}
