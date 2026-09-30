package dav

import (
	"cmp"
	"context"
	"crypto/rand"
	"encoding/xml"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
	"syscall"
	"time"

	"golang.org/x/net/webdav"

	"github.com/m1ngsama/filebox/internal/api"
	"github.com/m1ngsama/filebox/internal/auth"
	"github.com/m1ngsama/filebox/internal/index"
	"github.com/m1ngsama/filebox/internal/serve"
	"github.com/m1ngsama/filebox/internal/version"
	"github.com/m1ngsama/filebox/internal/vol"
)

func Handler(vols *vol.Set, a *auth.Auth, ix *index.Index, vs *version.Store) http.Handler {
	fsys := &FS{vols: vols, ix: ix, vs: vs}
	ls := newLocks()
	h := &webdav.Handler{
		Prefix:     "/dav",
		FileSystem: fsys,
		LockSystem: ls,
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
		if r.Method == "PROPFIND" && r.Header.Get("Depth") == "" {
			r.Header.Set("Depth", "1")
		}
		if r.Method == "PROPFIND" && r.Header.Get("Depth") == "infinity" {
			w.Header().Set("Content-Type", `application/xml; charset="utf-8"`)
			w.WriteHeader(http.StatusForbidden)
			io.WriteString(w, `<?xml version="1.0" encoding="utf-8"?><D:error xmlns:D="DAV:"><D:propfind-finite-depth/></D:error>`)
			return
		}
		// Browsers with cached Basic credentials would otherwise render uploaded HTML on this origin.
		serve.SafeHeaders(w.Header(), "")
		if r.Method == "LOCK" {
			clampTimeout(r.Header)
		}
		if r.Method == "PUT" {
			pb := &putBody{ReadCloser: r.Body, want: r.ContentLength}
			r.Body = pb
			r = r.WithContext(context.WithValue(r.Context(), putKey{}, pb))
		}
		removes := r.Method == "DELETE" || r.Method == "MOVE"
		src := path.Clean("/" + strings.TrimPrefix(r.URL.Path, "/dav"))
		var gone []string
		if removes {
			gone = append(gone, src)
		}
		if u, err := url.Parse(r.Header.Get("Destination")); err == nil && (r.Method == "MOVE" || r.Method == "COPY") && r.Header.Get("Overwrite") != "F" {
			gone = append(gone, path.Clean("/"+strings.TrimPrefix(u.Path, "/dav")))
		}
		have := submitted(r.Header.Get("If"))
		for _, p := range gone {
			for _, tok := range ls.covering(time.Now(), p, fsys.same) {
				if !have[tok] {
					http.Error(w, "locked", http.StatusLocked)
					return
				}
			}
		}
		release := func() {
			for _, p := range gone {
				ls.release(time.Now(), p, fsys.same)
			}
		}
		if r.Method != "COPY" && r.Method != "MOVE" {
			sw := &status{ResponseWriter: w}
			h.ServeHTTP(sw, r)
			if removes && sw.code == http.StatusNoContent {
				release()
			}
			return
		}
		ow := &overwrite{}
		if u, err := url.Parse(r.Header.Get("Destination")); err == nil && r.Header.Get("Overwrite") != "F" {
			ow.dst = strings.TrimPrefix(u.Path, "/dav")
			r = r.WithContext(context.WithValue(r.Context(), overwriteKey{}, ow))
		}
		sw := &status{ResponseWriter: w}
		h.ServeHTTP(sw, r)
		ok := sw.code == http.StatusCreated || sw.code == http.StatusNoContent
		if !ok && ow.undo != nil {
			if err := ow.undo(); err != nil {
				slog.Error("webdav: put back overwritten destination", "path", ow.dst, "err", err)
			}
		}
		if ok && r.Method == "COPY" && ix != nil {
			fsys.copyProps(r)
		}
		if ok {
			release()
		}
	}))
}

type status struct {
	http.ResponseWriter
	code int
}

func (s *status) WriteHeader(code int) {
	s.code = code
	s.ResponseWriter.WriteHeader(code)
}

func (f *FS) copyProps(r *http.Request) {
	u, err := url.Parse(r.Header.Get("Destination"))
	if err != nil {
		return
	}
	v1, r1, err1 := f.resolve(strings.TrimPrefix(r.URL.Path, "/dav"))
	v2, r2, err2 := f.resolve(strings.TrimPrefix(u.Path, "/dav"))
	if err1 == nil && err2 == nil && v1 != nil && v2 != nil {
		f.ix.CopyProps(v1, r1, v2, r2, r.Header.Get("Depth") != "0")
	}
}

func (f *FS) same(a, b string) bool {
	v1, r1, err1 := f.resolve(a)
	v2, r2, err2 := f.resolve(b)
	if err1 != nil || err2 != nil || v1 == nil || v1 != v2 {
		return false
	}
	x, err1 := v1.Root.Lstat(r1)
	y, err2 := v2.Root.Lstat(r2)
	return err1 == nil && err2 == nil && os.SameFile(x, y)
}

func readOnlyMethod(m string) bool {
	switch m {
	case "GET", "HEAD", "OPTIONS", "PROPFIND":
		return true
	}
	return false
}

type FS struct {
	vols *vol.Set
	ix   *index.Index
	vs   *version.Store
}

type overwriteKey struct{}

type overwrite struct {
	dst  string
	undo func() error
}

func user(ctx context.Context) int64 {
	p, _ := auth.From(ctx)
	return p.UserID
}

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
	rel, err := v.Clean(rest)
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
	defer f.ix.Touch(v, rel)
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
	// PROPPATCH opens with a bare O_RDWR, which fails on directories.
	if flag == os.O_RDWR && f.ix != nil {
		flag = os.O_RDONLY
	}
	pb, put := ctx.Value(putKey{}).(*putBody)
	direct := put && flag&os.O_TRUNC != 0 && rel != "."
	if direct {
		if file, err := f.stage(ctx, pb, v, rel, perm); file != nil || err != nil {
			return file, err
		}
		flag = flag&^os.O_TRUNC | os.O_CREATE | os.O_EXCL
	}
	fh, err := v.Root.OpenFile(rel, flag, perm)
	if direct && errors.Is(err, fs.ErrExist) {
		return f.stage(ctx, pb, v, rel, perm)
	}
	if err != nil {
		return nil, err
	}
	var file webdav.File = fh
	switch {
	case rel == ".":
		file = volRoot{fh}
	case flag&(os.O_WRONLY|os.O_RDWR|os.O_CREATE|os.O_TRUNC) != 0:
		file = written{fh, func() { f.ix.Touch(v, rel) }}
	}
	if f.ix == nil {
		return file, nil
	}
	return propFile{file, f.ix, v, rel}, nil
}

type putKey struct{}

type putBody struct {
	io.ReadCloser
	want, n int64
	eof     bool
	err     error
}

func (b *putBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	b.n += int64(n)
	if err == io.EOF {
		b.eof = true
	} else if err != nil {
		b.err = err
	}
	return n, err
}

func (b *putBody) complete() bool { return b.eof && b.err == nil && (b.want < 0 || b.n == b.want) }

var errIncomplete = errors.New("upload body incomplete")

func (f *FS) stage(ctx context.Context, pb *putBody, v *vol.Volume, rel string, perm os.FileMode) (webdav.File, error) {
	parent, err := v.Root.Stat(path.Dir(rel))
	if err != nil || !parent.IsDir() {
		return nil, os.ErrNotExist
	}
	fi, err := v.Root.Lstat(rel)
	if err == nil && fi.IsDir() {
		return nil, &fs.PathError{Op: "open", Path: rel, Err: syscall.EISDIR}
	}
	exists := err == nil
	if exists {
		perm = fi.Mode().Perm()
	}
	if err := v.Root.MkdirAll(vol.TmpDir, 0o700); err != nil {
		return nil, err
	}
	if st, err := v.Root.Stat(vol.TmpDir); err == nil && !exists && !sameDevice(st, parent) {
		return nil, nil
	}
	tmp := path.Join(vol.TmpDir, rand.Text())
	fh, err := v.Root.OpenFile(tmp, os.O_RDWR|os.O_CREATE|os.O_EXCL, perm)
	if err != nil {
		return nil, err
	}
	commit := func(closed error) error {
		if closed != nil || !pb.complete() {
			v.Root.Remove(tmp)
			return cmp.Or(closed, errIncomplete)
		}
		if _, err := f.vs.Replace(v, tmp, rel, version.WebDAV, user(ctx)); err != nil {
			v.Root.Remove(tmp)
			return err
		}
		f.ix.Touch(v, rel)
		return nil
	}
	var file webdav.File = staged{fh, commit}
	if f.ix != nil {
		file = propFile{file, f.ix, v, rel}
	}
	return file, nil
}

var sameDevice = func(a, b fs.FileInfo) bool {
	sa, ok1 := a.Sys().(*syscall.Stat_t)
	sb, ok2 := b.Sys().(*syscall.Stat_t)
	return !ok1 || !ok2 || sa.Dev == sb.Dev
}

type staged struct {
	*os.File
	commit func(closed error) error
}

func (s staged) Close() error { return s.commit(s.File.Close()) }

func (f *FS) RemoveAll(ctx context.Context, name string) error {
	v, rel, err := f.resolve(name)
	if err != nil {
		return err
	}
	if v == nil || rel == "." {
		return os.ErrPermission
	}
	if ow, ok := ctx.Value(overwriteKey{}).(*overwrite); ok && ow.undo == nil && path.Clean("/"+ow.dst) == path.Clean("/"+name) {
		return f.setAside(ctx, ow, v, rel)
	}
	fi, err := v.Root.Lstat(rel)
	if err != nil {
		return err
	}
	defer f.ix.Touch(v, rel)
	if junk(fi) {
		return v.Root.RemoveAll(rel)
	}
	_, err = api.Trash(v, rel, time.Now())
	return err
}

func junk(fi fs.FileInfo) bool { return vol.Junk(fi.Name(), fi.IsDir()) }

func (f *FS) setAside(ctx context.Context, ow *overwrite, v *vol.Volume, rel string) error {
	fi, err := v.Root.Lstat(rel)
	if err != nil {
		return err
	}
	if junk(fi) {
		defer f.ix.Touch(v, rel)
		return v.Root.RemoveAll(rel)
	}
	if fi.Mode().IsRegular() {
		id, err := f.vs.Capture(v, rel, version.WebDAV, user(ctx))
		if err == nil {
			ow.undo = func() error {
				defer f.ix.Touch(v, rel)
				return f.vs.Revert(v, id)
			}
		}
		return err
	}
	id, err := api.Trash(v, rel, time.Now())
	if err == nil {
		ow.undo = func() error {
			defer f.ix.Touch(v, rel)
			return api.Untrash(v, id, rel)
		}
	}
	return err
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
	if v1 == nil || v2 == nil || r1 == "." || r2 == "." {
		return os.ErrPermission
	}
	if v1 != v2 {
		err := api.Transfer(f.ix, v1, v2, r1, r2, true)
		if errors.Is(err, api.ErrSourceLeft) {
			if ow, ok := ctx.Value(overwriteKey{}).(*overwrite); ok {
				ow.undo = nil
			}
			slog.Warn("webdav move", "from", oldName, "to", newName, "err", err)
		}
		return err
	}
	if err := vol.Move(v1.Root, r1, r2); err != nil {
		return err
	}
	f.ix.Rename(v1, r1, r2)
	return nil
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

type written struct {
	*os.File
	done func()
}

func (w written) Close() error {
	err := w.File.Close()
	w.done()
	return err
}

type propFile struct {
	webdav.File
	ix  *index.Index
	v   *vol.Volume
	rel string
}

func (f propFile) DeadProps() (map[xml.Name]webdav.Property, error) {
	ps, err := f.ix.Props(f.v, f.rel)
	if err != nil {
		return nil, err
	}
	m := make(map[xml.Name]webdav.Property, len(ps))
	for _, p := range ps {
		n := xml.Name{Space: p.NS, Local: p.Name}
		m[n] = webdav.Property{XMLName: n, InnerXML: p.XML}
	}
	return m, nil
}

func (f propFile) Patch(patches []webdav.Proppatch) ([]webdav.Propstat, error) {
	var ops []index.Prop
	st := webdav.Propstat{Status: http.StatusOK}
	for _, pp := range patches {
		for _, p := range pp.Props {
			op := index.Prop{NS: p.XMLName.Space, Name: p.XMLName.Local}
			if !pp.Remove {
				op.XML = append([]byte{}, p.InnerXML...)
			}
			ops = append(ops, op)
			st.Props = append(st.Props, webdav.Property{XMLName: p.XMLName})
		}
	}
	err := f.ix.PatchProps(f.v, f.rel, ops)
	switch {
	case err == nil:
		return []webdav.Propstat{st}, nil
	case !errors.Is(err, index.ErrPropTooLarge) && !errors.Is(err, index.ErrTooManyProps):
		return nil, err
	}
	bad := webdav.Propstat{Status: http.StatusInsufficientStorage}
	dep := webdav.Propstat{Status: http.StatusFailedDependency}
	for i, op := range ops {
		if op.XML != nil && (errors.Is(err, index.ErrTooManyProps) || len(op.XML) > index.MaxPropSize) {
			bad.Props = append(bad.Props, st.Props[i])
		} else {
			dep.Props = append(dep.Props, st.Props[i])
		}
	}
	if dep.Props == nil {
		return []webdav.Propstat{bad}, nil
	}
	return []webdav.Propstat{bad, dep}, nil
}

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
