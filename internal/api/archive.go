package api

import (
	"archive/zip"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"strconv"
	"strings"

	"github.com/m1ngsama/filebox/internal/extract"
	"github.com/m1ngsama/filebox/internal/httpx"
	"github.com/m1ngsama/filebox/internal/serve"
)

const maxZipEntry = 64 << 20

func clean(name string) bool {
	return name != "" && !strings.HasPrefix(name, "/") && !strings.Contains(name, "\\") && path.Clean(name) == name &&
		name != ".." && !strings.HasPrefix(name, "../")
}

func openZip(w http.ResponseWriter, root *os.Root, rel string) (*zip.Reader, fs.FileInfo, func(), bool) {
	f, err := root.Open(rel)
	if err != nil {
		httpx.Error(w, err)
		return nil, nil, nil, false
	}
	st, err := f.Stat()
	if err == nil && !st.Mode().IsRegular() {
		err = fs.ErrInvalid
	}
	var z *zip.Reader
	if err == nil {
		z, err = extract.Zip(f, st.Size())
	}
	if err != nil {
		f.Close()
		httpx.Fail(w, 415, "not a readable zip archive")
		return nil, nil, nil, false
	}
	return z, st, func() { f.Close() }, true
}

// ZipEntries lists an archive's images in natural order, or with ?all every file in archive order.
func ZipEntries(w http.ResponseWriter, r *http.Request, root *os.Root, rel string) {
	z, _, done, ok := openZip(w, root, rel)
	if !ok {
		return
	}
	defer done()
	type item struct {
		Name string `json:"name"`
		Size uint64 `json:"size"`
	}
	list := z.File
	if !r.URL.Query().Has("all") {
		list = extract.Images(z)
	}
	out := []item{}
	for _, e := range list {
		if clean(e.Name) && !e.FileInfo().IsDir() {
			out = append(out, item{e.Name, e.UncompressedSize64})
		}
	}
	httpx.JSON(w, 200, map[string]any{"entries": out})
}

// ZipEntry streams one archive entry named by ?e, as its image type or as an inert download.
func ZipEntry(w http.ResponseWriter, r *http.Request, root *os.Root, rel string) {
	name := r.URL.Query().Get("e")
	if !clean(name) {
		httpx.Fail(w, 400, "bad entry")
		return
	}
	z, st, done, ok := openZip(w, root, rel)
	if !ok {
		return
	}
	defer done()
	var e *zip.File
	for _, f := range z.File {
		if f.Name == name {
			e = f
			break
		}
	}
	if e == nil {
		httpx.Error(w, fs.ErrNotExist)
		return
	}
	rc, err := extract.Open(e, maxZipEntry)
	if err != nil {
		httpx.Fail(w, 413, "entry too large")
		return
	}
	defer rc.Close()
	h := w.Header()
	tag := fmt.Sprintf(`"%x-%x-%x"`, st.Size(), st.ModTime().UnixNano(), e.CRC32)
	h.Set("ETag", tag)
	h.Set("Cache-Control", "private, max-age=3600")
	if r.Header.Get("If-None-Match") == tag {
		w.WriteHeader(304)
		return
	}
	ct := extract.ImageType(name)
	if ct == "" {
		ct = "application/octet-stream"
	}
	h.Set("Content-Type", ct)
	serve.SafeHeaders(h, ct)
	h.Set("Content-Length", strconv.FormatUint(e.UncompressedSize64, 10))
	if r.Method != http.MethodHead {
		io.Copy(w, rc)
	}
}

func (a *API) zipEntries(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	v, rel, err := a.Vols.Resolve(q.Get("vol"), q.Get("p"))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	ZipEntries(w, r, v.Root, rel)
}

func (a *API) zipEntry(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	v, rel, err := a.Vols.Resolve(q.Get("vol"), q.Get("p"))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	ZipEntry(w, r, v.Root, rel)
}
