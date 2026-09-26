package upload

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/m1ngsama/filebox/internal/httpx"
	"github.com/m1ngsama/filebox/internal/vol"
)

type Target struct {
	Vol       *vol.Volume
	Dir, Name string
}

type Policy struct {
	Owner   func(r *http.Request) (string, bool)
	Resolve func(r *http.Request, meta map[string]string) (Target, error)
}

func TargetFor(v *vol.Volume, dir string, meta map[string]string) (Target, error) {
	name := meta["filename"]
	if rp := meta["relativePath"]; rp != "" {
		for _, seg := range strings.Split(rp, "/") {
			if seg == ".." {
				return Target{}, vol.ErrBadPath
			}
		}
		sub, err := vol.Clean(path.Join(dir, path.Dir(rp)))
		if err != nil {
			return Target{}, err
		}
		dir, name = sub, path.Base(rp)
	}
	if !vol.ValidName(name) || (dir == "." && vol.Reserved(name)) {
		return Target{}, vol.ErrBadPath
	}
	return Target{Vol: v, Dir: dir, Name: name}, nil
}

type Server struct {
	Vols *vol.Set
	Dir  string
	Now  func() time.Time

	locks    sync.Map
	finalize sync.Mutex
}

type info struct {
	ID      string `json:"id"`
	Owner   string `json:"owner"`
	Vol     string `json:"vol"`
	Dir     string `json:"dir"`
	Name    string `json:"name"`
	Length  int64  `json:"length"`
	Created int64  `json:"created"`
}

func (s *Server) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Server) infoPath(id string) string { return filepath.Join(s.Dir, id+".json") }

func partPath(id string) string { return path.Join(vol.UploadsDir, id) }

func (s *Server) load(id string) (info, *vol.Volume, error) {
	var in info
	if len(id) != 32 || strings.Trim(id, "0123456789abcdef") != "" {
		return in, nil, fs.ErrNotExist
	}
	b, err := os.ReadFile(s.infoPath(id))
	if err != nil {
		return in, nil, fs.ErrNotExist
	}
	if err := json.Unmarshal(b, &in); err != nil {
		return in, nil, err
	}
	v, ok := s.Vols.Get(in.Vol)
	if !ok {
		return in, nil, fs.ErrNotExist
	}
	return in, v, nil
}

func (s *Server) lock(id string) func() {
	m, _ := s.locks.LoadOrStore(id, &sync.Mutex{})
	mu := m.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

func (s *Server) Handler(prefix string, p Policy) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Tus-Resumable", "1.0.0")
		if r.Method == http.MethodOptions {
			h.Set("Tus-Version", "1.0.0")
			h.Set("Tus-Extension", "creation,creation-with-upload,termination")
			w.WriteHeader(204)
			return
		}
		if r.Header.Get("Tus-Resumable") != "1.0.0" {
			h.Set("Tus-Version", "1.0.0")
			httpx.Fail(w, 412, "unsupported tus version")
			return
		}
		owner, ok := p.Owner(r)
		if !ok {
			httpx.Fail(w, 401, "unauthorized")
			return
		}
		id := strings.TrimPrefix(r.URL.Path, prefix)
		switch {
		case r.Method == http.MethodPost && id == "":
			s.create(w, r, prefix, owner, p)
		case id == "" || strings.Contains(id, "/"):
			httpx.Fail(w, 404, "not found")
		default:
			in, v, err := s.load(id)
			if err == nil && in.Owner != owner {
				err = fs.ErrNotExist
			}
			if err != nil {
				httpx.Error(w, err)
				return
			}
			switch r.Method {
			case http.MethodHead:
				s.head(w, in, v)
			case http.MethodPatch:
				s.patch(w, r, in, v)
			case http.MethodDelete:
				unlock := s.lock(id)
				s.remove(in, v)
				unlock()
				w.WriteHeader(204)
			default:
				httpx.Fail(w, 405, "method not allowed")
			}
		}
	})
}

func parseMeta(s string) map[string]string {
	m := map[string]string{}
	for _, kv := range strings.Split(s, ",") {
		k, v, _ := strings.Cut(strings.TrimSpace(kv), " ")
		if k == "" {
			continue
		}
		b, err := base64.StdEncoding.DecodeString(v)
		if err != nil {
			continue
		}
		m[k] = string(b)
	}
	return m
}

func (s *Server) create(w http.ResponseWriter, r *http.Request, prefix, owner string, p Policy) {
	length, err := strconv.ParseInt(r.Header.Get("Upload-Length"), 10, 64)
	if err != nil || length < 0 {
		httpx.Fail(w, 400, "bad Upload-Length")
		return
	}
	t, err := p.Resolve(r, parseMeta(r.Header.Get("Upload-Metadata")))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	if free, err := t.Vol.Free(); err != nil || uint64(length) > free {
		httpx.Error(w, httpx.ErrNoSpace)
		return
	}
	b := make([]byte, 16)
	rand.Read(b)
	in := info{ID: hex.EncodeToString(b), Owner: owner, Vol: t.Vol.Name, Dir: t.Dir, Name: t.Name,
		Length: length, Created: s.now().Unix()}
	if err := t.Vol.Root.MkdirAll(vol.UploadsDir, 0o700); err != nil {
		httpx.Error(w, err)
		return
	}
	f, err := t.Vol.Root.OpenFile(partPath(in.ID), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	f.Close()
	js, _ := json.Marshal(in)
	if err := os.MkdirAll(s.Dir, 0o700); err == nil {
		err = os.WriteFile(s.infoPath(in.ID), js, 0o600)
	}
	if err != nil {
		t.Vol.Root.Remove(partPath(in.ID))
		httpx.Error(w, err)
		return
	}
	if length == 0 {
		if err := s.finish(in, t.Vol); err != nil {
			httpx.Error(w, err)
			return
		}
	}
	w.Header().Set("Location", prefix+in.ID)
	w.Header().Set("Upload-Offset", "0")
	w.WriteHeader(201)
}

func (s *Server) head(w http.ResponseWriter, in info, v *vol.Volume) {
	fi, err := v.Root.Stat(partPath(in.ID))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	h := w.Header()
	h.Set("Upload-Offset", strconv.FormatInt(fi.Size(), 10))
	h.Set("Upload-Length", strconv.FormatInt(in.Length, 10))
	h.Set("Cache-Control", "no-store")
	w.WriteHeader(200)
}

func (s *Server) patch(w http.ResponseWriter, r *http.Request, in info, v *vol.Volume) {
	if r.Header.Get("Content-Type") != "application/offset+octet-stream" {
		httpx.Fail(w, 415, "bad content type")
		return
	}
	off, err := strconv.ParseInt(r.Header.Get("Upload-Offset"), 10, 64)
	if err != nil {
		httpx.Fail(w, 400, "bad Upload-Offset")
		return
	}
	defer s.lock(in.ID)()
	f, err := v.Root.OpenFile(partPath(in.ID), os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	fi, err := f.Stat()
	if err != nil {
		f.Close()
		httpx.Error(w, err)
		return
	}
	if fi.Size() != off {
		f.Close()
		w.Header().Set("Upload-Offset", strconv.FormatInt(fi.Size(), 10))
		httpx.Fail(w, 409, "offset mismatch")
		return
	}
	n, err := io.CopyN(f, r.Body, in.Length-off)
	if err == nil {
		if extra, _ := r.Body.Read(make([]byte, 1)); extra > 0 {
			f.Truncate(off)
			f.Close()
			httpx.Fail(w, 413, "more data than Upload-Length")
			return
		}
	}
	if cerr := f.Close(); err == nil || errors.Is(err, io.EOF) {
		err = cerr
	}
	if err != nil && !errors.Is(err, io.EOF) {
		if errors.Is(err, syscall.ENOSPC) {
			err = httpx.ErrNoSpace
		}
		httpx.Error(w, fmt.Errorf("upload %s: %w", in.ID, err))
		return
	}
	off += n
	if off == in.Length {
		if err := s.finish(in, v); err != nil {
			httpx.Error(w, err)
			return
		}
	}
	w.Header().Set("Upload-Offset", strconv.FormatInt(off, 10))
	w.WriteHeader(204)
}

func (s *Server) finish(in info, v *vol.Volume) error {
	s.finalize.Lock()
	defer s.finalize.Unlock()
	if err := v.Root.MkdirAll(in.Dir, 0o755); err != nil {
		return err
	}
	name, err := unique(v.Root, in.Dir, in.Name)
	if err != nil {
		return err
	}
	if err := v.Root.Rename(partPath(in.ID), path.Join(in.Dir, name)); err != nil {
		return err
	}
	os.Remove(s.infoPath(in.ID))
	s.locks.Delete(in.ID)
	return nil
}

func unique(root *os.Root, dir, name string) (string, error) {
	ext := path.Ext(name)
	base := strings.TrimSuffix(name, ext)
	for i := 0; i < 1000; i++ {
		n := name
		if i > 0 {
			n = fmt.Sprintf("%s (%d)%s", base, i, ext)
		}
		if _, err := root.Lstat(path.Join(dir, n)); errors.Is(err, fs.ErrNotExist) {
			return n, nil
		}
	}
	return "", fs.ErrExist
}

func (s *Server) remove(in info, v *vol.Volume) {
	v.Root.Remove(partPath(in.ID))
	os.Remove(s.infoPath(in.ID))
	s.locks.Delete(in.ID)
}

func (s *Server) Sweep(maxAge time.Duration) {
	cutoff := s.now().Add(-maxAge)
	known := map[string]bool{}
	if ents, err := os.ReadDir(s.Dir); err == nil {
		for _, e := range ents {
			id, ok := strings.CutSuffix(e.Name(), ".json")
			if !ok {
				continue
			}
			in, v, err := s.load(id)
			if err != nil {
				continue
			}
			unlock := s.lock(id)
			fi, err := v.Root.Stat(partPath(id))
			stale := err != nil || fi.ModTime().Before(cutoff)
			if stale {
				s.remove(in, v)
			}
			unlock()
			if !stale {
				known[id] = true
			}
		}
	}
	for _, v := range s.Vols.All() {
		f, err := v.Root.Open(vol.UploadsDir)
		if err != nil {
			continue
		}
		ents, _ := f.ReadDir(-1)
		f.Close()
		for _, e := range ents {
			name := e.Name()
			if known[name] {
				continue
			}
			unlock := s.lock(name)
			if fi, err := v.Root.Stat(partPath(name)); err == nil && fi.ModTime().Before(cutoff) {
				v.Root.Remove(partPath(name))
				s.locks.Delete(name)
			}
			unlock()
		}
	}
}
