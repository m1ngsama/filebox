package upload

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/tus/tusd/v2/pkg/filestore"
	"github.com/tus/tusd/v2/pkg/handler"
	"github.com/tus/tusd/v2/pkg/memorylocker"
	xslog "golang.org/x/exp/slog"

	"github.com/m1ngsama/filebox/internal/httpx"
	"github.com/m1ngsama/filebox/internal/index"
	"github.com/m1ngsama/filebox/internal/version"
	"github.com/m1ngsama/filebox/internal/vol"
)

type Target struct {
	Vol       *vol.Volume
	Dir, Name string
	Base      string
	Replace   bool
	MaxSize   int64
}

type Policy struct {
	Owner   func(r *http.Request) (string, bool)
	Resolve func(r *http.Request, meta map[string]string) (Target, error)
}

func TargetFor(v *vol.Volume, dir string, meta map[string]string) (Target, error) {
	name := meta["filename"]
	if rp := meta["relativePath"]; rp != "" {
		for _, seg := range strings.Split(rp, "/") {
			if seg == ".." || vol.Reserved(seg) {
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

func Within(v *vol.Volume, base string, meta map[string]string) (Target, error) {
	t, err := TargetFor(v, base, meta)
	if err != nil {
		return t, err
	}
	t.Base = base
	return t, t.confine(false)
}

func (t Target) confine(mkdir bool) error {
	if t.Base == "" {
		if mkdir {
			return t.Vol.Root.MkdirAll(t.Dir, 0o755)
		}
		return nil
	}
	sr, err := t.Vol.Root.OpenRoot(t.Base)
	if err != nil {
		return err
	}
	defer sr.Close()
	rel := t.Dir
	if t.Base != "." {
		rel = "."
		if t.Dir != t.Base {
			rel = strings.TrimPrefix(t.Dir, t.Base+"/")
		}
	}
	if mkdir {
		if err := sr.MkdirAll(rel, 0o755); err != nil {
			return vol.ErrBadPath
		}
	}
	in, err := sr.Stat(rel)
	if !mkdir && errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return vol.ErrBadPath
	}
	if out, err := t.Vol.Root.Stat(t.Dir); err != nil || !os.SameFile(in, out) {
		return vol.ErrBadPath
	}
	return nil
}

const (
	keyBase    = "base"
	keyOwner   = "owner"
	keyDir     = "dir"
	keyName    = "filename"
	keyReplace = "replace"
)

var (
	errNoSpace  = handler.NewError("ERR_INSUFFICIENT_STORAGE", "insufficient storage", http.StatusInsufficientStorage)
	errFinalize = handler.NewError("ERR_FINALIZE", "cannot store the upload", http.StatusInternalServerError)
	errInternal = handler.NewError("ERR_INTERNAL", "internal error", http.StatusInternalServerError)
	errTooLarge = handler.NewError("ERR_TOO_LARGE", "file too large", http.StatusRequestEntityTooLarge)

	ErrTooLarge = errTooLarge
	ErrRevoked  = handler.NewError("ERR_REVOKED", "upload no longer allowed", http.StatusForbidden)
)

type Server struct {
	Now      func() time.Time
	Index    *index.Index
	Received func(owner, rel string, size int64)
	Allow    func(owner string, size int64) error
	Versions *version.Store

	vols     []*volume
	byKey    map[string]*volume
	finalize sync.Mutex
}

type volume struct {
	v      *vol.Volume
	h      *handler.Handler
	store  store
	locker locker
}

type creation struct {
	t     Target
	owner string
}

type creationKey struct{}

func New(vols *vol.Set) (*Server, error) {
	s := &Server{byKey: map[string]*volume{}}
	logger := xslog.New(xslog.NewTextHandler(os.Stderr, &xslog.HandlerOptions{Level: xslog.LevelWarn}))
	for _, v := range vols.All() {
		if err := v.Root.MkdirAll(vol.UploadsDir, 0o700); err != nil {
			return nil, err
		}
		key := volumeKey(v.Name)
		if _, dup := s.byKey[key]; dup {
			return nil, fmt.Errorf("volume %q: upload key collides with another volume, rename it", v.Name)
		}
		u := &volume{v: v, store: store{filestore.New(filepath.Join(v.Path, vol.UploadsDir))}, locker: locker{memorylocker.New()}}
		c := handler.NewStoreComposer()
		c.UseCore(u.store)
		c.UseTerminater(u.store)
		c.UseLocker(u.locker)
		h, err := handler.NewHandler(handler.Config{
			StoreComposer:   c,
			BasePath:        "/upload/" + v.Name + "/",
			DisableDownload: true,
			Cors:            &handler.CorsConfig{Disable: true},
			Logger:          logger,
			PreUploadCreateCallback: func(hook handler.HookEvent) (handler.HTTPResponse, handler.FileInfoChanges, error) {
				return create(key, hook)
			},
			PreFinishResponseCallback: func(hook handler.HookEvent) (handler.HTTPResponse, error) {
				kept, err := s.finish(u, hook.Upload)
				return replaced(kept), err
			},
		})
		if err != nil {
			return nil, err
		}
		u.h = h
		s.vols = append(s.vols, u)
		s.byKey[key] = u
	}
	return s, nil
}

func (s *Server) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func volumeKey(name string) string {
	h := sha256.Sum256([]byte(name))
	return hex.EncodeToString(h[:4])
}

func (s *Server) lookup(id string) (*volume, bool) {
	key, tail, _ := strings.Cut(id, "-")
	u, ok := s.byKey[key]
	if !ok || len(tail) != 26 || strings.Trim(tail, "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567") != "" {
		return nil, false
	}
	return u, true
}

func (s *Server) Handler(prefix string, p Policy) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodOptions {
			s.vols[0].h.ServeHTTP(w, r)
			return
		}
		owner, ok := p.Owner(r)
		if !ok {
			httpx.Fail(w, 401, "unauthorized")
			return
		}
		id := strings.TrimPrefix(r.URL.Path, prefix)
		ctx := r.Context()
		var u *volume
		if r.Method == http.MethodPost && id == "" {
			t, err := p.Resolve(r, handler.ParseMetadataHeader(r.Header.Get("Upload-Metadata")))
			if err != nil {
				httpx.Error(w, err)
				return
			}
			for _, c := range s.vols {
				if c.v == t.Vol {
					u = c
				}
			}
			ctx = context.WithValue(ctx, creationKey{}, creation{t, owner})
		} else if u, ok = s.lookup(id); ok && u.owner(ctx, id) != owner {
			u = nil
		}
		if u == nil {
			httpx.Fail(w, 404, "not found")
			return
		}
		if r.Method == http.MethodHead || r.Method == http.MethodPatch {
			size, kept, err := s.settle(ctx, u, id)
			var herr handler.Error
			if errors.As(err, &herr) {
				httpx.Fail(w, herr.HTTPResponse.StatusCode, herr.Message)
				return
			}
			if err != nil {
				httpx.Fail(w, 500, "internal error")
				return
			}
			if size >= 0 {
				n := strconv.FormatInt(size, 10)
				h := w.Header()
				h.Set("Tus-Resumable", "1.0.0")
				h.Set("Cache-Control", "no-store")
				h.Set("Upload-Offset", n)
				h.Set("Upload-Length", n)
				for k, v := range replaced(kept).Header {
					h.Set(k, v)
				}
				if r.Method == http.MethodHead {
					w.WriteHeader(200)
				} else {
					w.WriteHeader(204)
				}
				return
			}
		}
		r = r.Clone(ctx)
		r.URL.Path, r.URL.RawPath = "/"+id, ""
		u.h.ServeHTTP(&rewriter{ResponseWriter: w, prefix: prefix}, r)
	})
}

func (u *volume) owner(ctx context.Context, id string) string {
	up, err := u.store.FileStore.GetUpload(ctx, id)
	if err != nil {
		return ""
	}
	info, err := up.GetInfo(ctx)
	if err != nil {
		return ""
	}
	return info.MetaData[keyOwner]
}

type rewriter struct {
	http.ResponseWriter
	prefix string
}

func (w *rewriter) WriteHeader(code int) {
	h := w.Header()
	if loc := h.Get("Location"); loc != "" {
		h.Set("Location", w.prefix+path.Base(loc))
	}
	h.Del("Upload-Metadata")
	w.ResponseWriter.WriteHeader(code)
}

func (w *rewriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func create(key string, hook handler.HookEvent) (handler.HTTPResponse, handler.FileInfoChanges, error) {
	var none handler.FileInfoChanges
	c, ok := hook.Context.Value(creationKey{}).(creation)
	if !ok {
		return handler.HTTPResponse{}, none, errors.New("upload created without a policy")
	}
	if c.t.MaxSize > 0 && (hook.Upload.SizeIsDeferred || hook.Upload.Size > c.t.MaxSize) {
		return handler.HTTPResponse{}, none, errTooLarge
	}
	if free, err := c.t.Vol.Free(); err != nil || uint64(hook.Upload.Size) > free {
		return handler.HTTPResponse{}, none, errNoSpace
	}
	md := handler.MetaData{keyOwner: c.owner, keyDir: c.t.Dir, keyName: c.t.Name}
	if c.t.Replace {
		md[keyReplace] = "1"
	}
	if c.t.Base != "" {
		md[keyBase] = c.t.Base
	}
	return handler.HTTPResponse{}, handler.FileInfoChanges{ID: key + "-" + rand.Text(), MetaData: md}, nil
}

func complete(ctx context.Context, u *volume, id string) (handler.FileInfo, bool) {
	up, err := u.store.FileStore.GetUpload(ctx, id)
	if err != nil {
		return handler.FileInfo{}, false
	}
	info, err := up.GetInfo(ctx)
	return info, err == nil && !info.SizeIsDeferred && info.Offset == info.Size
}

func replaced(kept string) handler.HTTPResponse {
	if kept == "" {
		return handler.HTTPResponse{}
	}
	return handler.HTTPResponse{Header: handler.HTTPHeader{"Upload-Replaced": kept}}
}

func (s *Server) settle(ctx context.Context, u *volume, id string) (int64, string, error) {
	lk, _ := u.locker.NewLock(id)
	if err := lk.Lock(ctx, func() {}); err != nil {
		return -1, "", err
	}
	defer lk.Unlock()
	info, ok := complete(ctx, u, id)
	if !ok {
		return -1, "", nil
	}
	kept, err := s.finish(u, info)
	return info.Size, kept, err
}

func (s *Server) finish(u *volume, info handler.FileInfo) (string, error) {
	s.finalize.Lock()
	defer s.finalize.Unlock()
	if s.Allow != nil {
		if err := s.Allow(info.MetaData[keyOwner], info.Size); err != nil {
			u.v.Root.Remove(path.Join(vol.UploadsDir, info.ID))
			u.v.Root.Remove(path.Join(vol.UploadsDir, info.ID+".info"))
			return "", err
		}
	}
	dir := info.MetaData[keyDir]
	err := Target{Vol: u.v, Dir: dir, Base: info.MetaData[keyBase]}.confine(true)
	name := info.MetaData[keyName]
	dst := path.Join(dir, name)
	kept := ""
	if err == nil && replaceable(u.v.Root, dst, info.MetaData[keyReplace] == "1") {
		kept, err = s.Versions.Capture(u.v, dst, version.Upload, userID(info.MetaData[keyOwner]))
	} else if err == nil {
		name, err = unique(u.v.Root, dir, name)
		dst = path.Join(dir, name)
	}
	if err == nil {
		err = vol.Move(u.v.Root, path.Join(vol.UploadsDir, info.ID), dst)
		if err != nil && kept != "" {
			s.Versions.Restore(u.v, kept, 0)
			kept = ""
		}
	}
	if err != nil {
		slog.Error("finalize upload", "id", info.ID, "err", err)
		return "", errFinalize
	}
	u.v.Root.Remove(path.Join(vol.UploadsDir, info.ID+".info"))
	s.Index.Touch(u.v, dst)
	if s.Received != nil {
		s.Received(info.MetaData[keyOwner], dst, info.Size)
	}
	return kept, nil
}

func userID(owner string) int64 {
	n, _ := strconv.ParseInt(strings.TrimPrefix(owner, "user:"), 10, 64)
	return n
}

func replaceable(root *os.Root, rel string, replace bool) bool {
	if !replace {
		return false
	}
	fi, err := root.Lstat(rel)
	return err == nil && fi.Mode().IsRegular()
}

func unique(root *os.Root, dir, name string) (string, error) {
	for i := 0; i < 1000; i++ {
		n := name
		if i > 0 {
			n = vol.Numbered(name, i)
		}
		if _, err := root.Lstat(path.Join(dir, n)); errors.Is(err, fs.ErrNotExist) {
			return n, nil
		}
	}
	return "", fs.ErrExist
}

func (s *Server) Sweep(maxAge time.Duration) {
	cutoff := s.now().Add(-maxAge)
	for _, u := range s.vols {
		f, err := u.v.Root.Open(vol.UploadsDir)
		if err != nil {
			continue
		}
		names, _ := f.Readdirnames(-1)
		f.Close()
		ids := map[string]bool{}
		for _, n := range names {
			ids[strings.TrimSuffix(n, ".info")] = true
		}
		for id := range ids {
			if u.stale(id, cutoff) {
				s.sweep(u, id, cutoff)
			}
		}
	}
}

func (u *volume) stale(id string, cutoff time.Time) bool {
	fi, err := u.v.Root.Stat(path.Join(vol.UploadsDir, id))
	if err != nil {
		fi, err = u.v.Root.Stat(path.Join(vol.UploadsDir, id+".info"))
	}
	return err == nil && fi.ModTime().Before(cutoff)
}

func (s *Server) sweep(u *volume, id string, cutoff time.Time) {
	lock, _ := u.locker.NewLock(id)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if lock.Lock(ctx, func() {}) != nil {
		return
	}
	defer lock.Unlock()
	if info, ok := complete(ctx, u, id); ok {
		s.finish(u, info)
		return
	}
	if u.stale(id, cutoff) {
		u.v.Root.Remove(path.Join(vol.UploadsDir, id))
		u.v.Root.Remove(path.Join(vol.UploadsDir, id+".info"))
	}
}

type store struct{ filestore.FileStore }

type upload struct{ handler.Upload }

type terminatable struct{ handler.TerminatableUpload }

func sanitize(err error) error {
	var herr handler.Error
	switch {
	case err == nil || errors.As(err, &herr):
		return err
	case httpx.NoSpace(err):
		return errNoSpace
	}
	slog.Error("upload storage", "err", err)
	return errInternal
}

func (s store) NewUpload(ctx context.Context, info handler.FileInfo) (handler.Upload, error) {
	up, err := s.FileStore.NewUpload(ctx, info)
	if err != nil {
		return nil, sanitize(err)
	}
	return upload{up}, nil
}

func (s store) GetUpload(ctx context.Context, id string) (handler.Upload, error) {
	up, err := s.FileStore.GetUpload(ctx, id)
	if err != nil {
		return nil, sanitize(err)
	}
	return upload{up}, nil
}

func (s store) AsTerminatableUpload(up handler.Upload) handler.TerminatableUpload {
	return terminatable{s.FileStore.AsTerminatableUpload(up.(upload).Upload)}
}

func (u upload) WriteChunk(ctx context.Context, offset int64, src io.Reader) (int64, error) {
	n, err := u.Upload.WriteChunk(ctx, offset, src)
	return n, sanitize(err)
}

func (u upload) FinishUpload(ctx context.Context) error { return sanitize(u.Upload.FinishUpload(ctx)) }

func (t terminatable) Terminate(ctx context.Context) error {
	return sanitize(t.TerminatableUpload.Terminate(ctx))
}

// tusd v2.10.1 races on its request body when a lock interrupts its holder, so locks wait instead.
type locker struct{ *memorylocker.MemoryLocker }

type lock struct{ l handler.Lock }

func (l locker) NewLock(id string) (handler.Lock, error) {
	lk, err := l.MemoryLocker.NewLock(id)
	return lock{lk}, err
}

func (l lock) Lock(ctx context.Context, _ func()) error { return l.l.Lock(ctx, func() {}) }

func (l lock) Unlock() error { return l.l.Unlock() }
