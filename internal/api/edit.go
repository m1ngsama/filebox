package api

import (
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"sync"

	"github.com/m1ngsama/filebox/internal/auth"
	"github.com/m1ngsama/filebox/internal/httpx"
	"github.com/m1ngsama/filebox/internal/version"
	"github.com/m1ngsama/filebox/internal/vol"
)

const maxEdit = 8 << 20

var editing sync.Mutex

func stale(w http.ResponseWriter, fi os.FileInfo) {
	w.Header().Set("ETag", etag(fi))
	httpx.Fail(w, 412, "changed since it was opened")
}

func etag(fi os.FileInfo) string { return fmt.Sprintf(`"%x-%x"`, fi.Size(), fi.ModTime().UnixNano()) }

// save replaces a text file the browser edited; If-Match must name the copy the editor opened, so a change made meanwhile is never lost.
func (a *API) save(w http.ResponseWriter, r *http.Request) {
	v, rel, err := a.query(r)
	if err == nil && (rel == "." || vol.Reserved(path.Base(rel))) {
		err = vol.ErrBadPath
	}
	if err != nil {
		httpx.Error(w, err)
		return
	}
	fi, err := v.Root.Stat(rel)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	if !fi.Mode().IsRegular() {
		httpx.Error(w, vol.ErrNotFile)
		return
	}
	if r.Header.Get("If-Match") != etag(fi) {
		stale(w, fi)
		return
	}
	if err := v.Root.MkdirAll(vol.UploadsDir, 0o700); err != nil {
		httpx.Error(w, err)
		return
	}
	tmp := path.Join(vol.UploadsDir, "edit-"+rand.Text())
	f, err := v.Root.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, fi.Mode().Perm())
	if err != nil {
		httpx.Error(w, err)
		return
	}
	n, err := io.Copy(f, http.MaxBytesReader(w, r.Body, maxEdit))
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		v.Root.Remove(tmp)
		if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
			httpx.Fail(w, 413, "too large to edit")
			return
		}
		httpx.Error(w, err)
		return
	}
	editing.Lock()
	defer editing.Unlock()
	if now, err := v.Root.Stat(rel); err != nil || etag(now) != etag(fi) {
		v.Root.Remove(tmp)
		if err != nil {
			httpx.Error(w, err)
		} else {
			stale(w, now)
		}
		return
	}
	p, _ := auth.From(r.Context())
	if _, err := a.Versions.Replace(v, tmp, rel, version.Edit, p.UserID); err != nil {
		v.Root.Remove(tmp)
		httpx.Error(w, err)
		return
	}
	a.Index.Touch(v, rel)
	if fi, err = v.Root.Stat(rel); err != nil {
		httpx.Error(w, err)
		return
	}
	w.Header().Set("ETag", etag(fi))
	httpx.JSON(w, 200, map[string]any{"size": n, "mtime": fi.ModTime().UnixMilli()})
}
