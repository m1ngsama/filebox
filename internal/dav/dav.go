package dav

import (
	"context"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path"
	"strings"
	"time"

	"golang.org/x/net/webdav"

	"github.com/m1ngsama/filebox/internal/auth"
	"github.com/m1ngsama/filebox/internal/serve"
	"github.com/m1ngsama/filebox/internal/vol"
)

func Handler(vols *vol.Set, a *auth.Auth) http.Handler {
	h := &webdav.Handler{
		Prefix:     "/dav",
		FileSystem: &FS{vols: vols},
		LockSystem: webdav.NewMemLS(),
		Logger: func(r *http.Request, err error) {
			if err != nil {
				slog.Debug("webdav", "method", r.Method, "path", r.URL.Path, "err", err)
			}
		},
	}
	return a.RequireBasic(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, _ := auth.From(r.Context())
		if p.ReadOnly && !readOnlyMethod(r.Method) {
			http.Error(w, "read-only token", http.StatusForbidden)
			return
		}
		if d := r.Header.Get("Depth"); r.Method == "PROPFIND" && (d == "" || d == "infinity") {
			w.Header().Set("Content-Type", `application/xml; charset="utf-8"`)
			w.WriteHeader(http.StatusForbidden)
			io.WriteString(w, `<?xml version="1.0" encoding="utf-8"?><D:error xmlns:D="DAV:"><D:propfind-finite-depth/></D:error>`)
			return
		}
		// Browsers with cached Basic credentials would otherwise render uploaded HTML on this origin.
		serve.SafeHeaders(w.Header(), "")
		h.ServeHTTP(w, r)
	}))
}

func readOnlyMethod(m string) bool {
	switch m {
	case "GET", "HEAD", "OPTIONS", "PROPFIND":
		return true
	}
	return false
}

type FS struct{ vols *vol.Set }

func (f *FS) resolve(name string) (*vol.Volume, string, error) {
	c := strings.TrimPrefix(path.Clean("/"+name), "/")
	if c == "" {
		return nil, ".", nil
	}
	vname, rest, _ := strings.Cut(c, "/")
	v, ok := f.vols.Get(vname)
	if !ok {
		return nil, "", os.ErrNotExist
	}
	rel, err := vol.Clean(rest)
	if err != nil {
		return nil, "", os.ErrNotExist
	}
	return v, rel, nil
}

func (f *FS) Mkdir(ctx context.Context, name string, perm os.FileMode) error {
	v, rel, err := f.resolve(name)
	if err != nil {
		return err
	}
	if v == nil || rel == "." {
		return os.ErrPermission
	}
	return v.Root.Mkdir(rel, perm)
}

func (f *FS) OpenFile(ctx context.Context, name string, flag int, perm os.FileMode) (webdav.File, error) {
	v, rel, err := f.resolve(name)
	if err != nil {
		return nil, err
	}
	if v == nil {
		if flag&(os.O_WRONLY|os.O_RDWR|os.O_CREATE|os.O_TRUNC) != 0 {
			return nil, os.ErrPermission
		}
		return &rootDir{vols: f.vols}, nil
	}
	fh, err := v.Root.OpenFile(rel, flag, perm)
	if err != nil {
		return nil, err
	}
	if rel == "." {
		return volRoot{fh}, nil
	}
	return fh, nil
}

func (f *FS) RemoveAll(ctx context.Context, name string) error {
	v, rel, err := f.resolve(name)
	if err != nil {
		return err
	}
	if v == nil || rel == "." {
		return os.ErrPermission
	}
	return v.Root.RemoveAll(rel)
}

func (f *FS) Rename(ctx context.Context, oldName, newName string) error {
	v1, r1, err := f.resolve(oldName)
	if err != nil {
		return err
	}
	v2, r2, err := f.resolve(newName)
	if err != nil {
		return err
	}
	if v1 == nil || v2 == nil || r1 == "." || r2 == "." || v1 != v2 {
		return os.ErrPermission
	}
	return v1.Root.Rename(r1, r2)
}

func (f *FS) Stat(ctx context.Context, name string) (os.FileInfo, error) {
	v, rel, err := f.resolve(name)
	if err != nil {
		return nil, err
	}
	if v == nil {
		return dirInfo{name: "/"}, nil
	}
	fi, err := v.Root.Stat(rel)
	if err != nil {
		return nil, err
	}
	if rel == "." {
		return named{fi, v.Name}, nil
	}
	return fi, nil
}

type named struct {
	fs.FileInfo
	name string
}

func (n named) Name() string { return n.name }

var started = time.Now()

type dirInfo struct{ name string }

func (d dirInfo) Name() string       { return d.name }
func (d dirInfo) Size() int64        { return 0 }
func (d dirInfo) Mode() fs.FileMode  { return fs.ModeDir | 0o555 }
func (d dirInfo) ModTime() time.Time { return started }
func (d dirInfo) IsDir() bool        { return true }
func (d dirInfo) Sys() any           { return nil }

type volRoot struct{ *os.File }

func (d volRoot) Readdir(n int) ([]fs.FileInfo, error) {
	fis, err := d.File.Readdir(n)
	out := fis[:0]
	for _, fi := range fis {
		if !vol.Reserved(fi.Name()) {
			out = append(out, fi)
		}
	}
	return out, err
}

type rootDir struct {
	vols *vol.Set
	done bool
}

func (d *rootDir) Close() error                   { return nil }
func (d *rootDir) Read([]byte) (int, error)       { return 0, io.EOF }
func (d *rootDir) Seek(int64, int) (int64, error) { return 0, nil }
func (d *rootDir) Write([]byte) (int, error)      { return 0, os.ErrPermission }
func (d *rootDir) Stat() (fs.FileInfo, error)     { return dirInfo{name: "/"}, nil }

func (d *rootDir) Readdir(n int) ([]fs.FileInfo, error) {
	if d.done {
		if n > 0 {
			return nil, io.EOF
		}
		return nil, nil
	}
	d.done = true
	var out []fs.FileInfo
	for _, v := range d.vols.All() {
		if fi, err := v.Root.Stat("."); err == nil {
			out = append(out, named{fi, v.Name})
		}
	}
	return out, nil
}
