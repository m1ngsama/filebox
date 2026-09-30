package api

import (
	"archive/zip"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"runtime"
	"strconv"
	"strings"

	"github.com/m1ngsama/filebox/internal/extract"
	"github.com/m1ngsama/filebox/internal/httpx"
	"github.com/m1ngsama/filebox/internal/serve"
)

const maxZipEntry = 64 << 20

// Parsing a central directory is memory-heavy, so only NumCPU requests may hold a reader at once.
var readers = make(chan struct{}, runtime.NumCPU())

func valid(name string) bool { return fs.ValidPath(name) && !strings.Contains(name, "\\") }

func openZip(w http.ResponseWriter, r *http.Request, root *os.Root, rel string) (*zip.Reader, *os.File, fs.FileInfo, func(), bool) {
	select {
	case readers <- struct{}{}:
	case <-r.Context().Done():
		httpx.Fail(w, 503, "busy")
		return nil, nil, nil, nil, false
	}
	release := func() { <-readers }
	f, err := root.Open(rel)
	if err != nil {
		release()
		httpx.Error(w, err)
		return nil, nil, nil, nil, false
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
		release()
		httpx.Fail(w, 415, "not a readable zip archive")
		return nil, nil, nil, nil, false
	}
	return z, f, st, func() { f.Close(); release() }, true
}

// ZipEntries lists an archive's images in natural order, or with ?all every file in archive order.
func ZipEntries(w http.ResponseWriter, r *http.Request, root *os.Root, rel string) {
	z, _, _, done, ok := openZip(w, r, root, rel)
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
		if valid(e.Name) && !e.FileInfo().IsDir() {
			out = append(out, item{e.Name, e.UncompressedSize64})
		}
	}
	httpx.JSON(w, 200, map[string]any{"entries": out})
}

// ZipEntry streams one archive entry named by ?e, as its image type or as an inert download.
func ZipEntry(w http.ResponseWriter, r *http.Request, root *os.Root, rel string) {
	name := r.URL.Query().Get("e")
	if !valid(name) {
		httpx.Fail(w, 400, "bad entry")
		return
	}
	z, src, st, done, ok := openZip(w, r, root, rel)
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
	if e.UncompressedSize64 > maxZipEntry {
		httpx.Fail(w, 413, "entry too large")
		return
	}
	h := w.Header()
	tag := fmt.Sprintf(`"%x-%x-%x"`, st.Size(), st.ModTime().UnixNano(), e.CRC32)
	h.Set("ETag", tag)
	h.Set("Cache-Control", "private, no-cache")
	ct := extract.ImageType(name)
	if ct == "" {
		ct = "application/octet-stream"
	}
	h.Set("Content-Type", ct)
	serve.SafeHeaders(h, ct)
	if e.Method == zip.Store {
		if off, err := e.DataOffset(); err == nil {
			http.ServeContent(w, r, "", st.ModTime(), io.NewSectionReader(src, off, int64(e.UncompressedSize64)))
			return
		}
	}
	h.Set("Accept-Ranges", "none")
	if match(r.Header.Get("If-None-Match"), tag) {
		w.WriteHeader(304)
		return
	}
	rc, err := extract.Open(e, maxZipEntry)
	if err != nil {
		httpx.Fail(w, 413, "entry too large")
		return
	}
	defer rc.Close()
	h.Set("Content-Length", strconv.FormatUint(e.UncompressedSize64, 10))
	if r.Method != http.MethodHead {
		io.Copy(w, rc)
	}
}

func match(header, tag string) bool {
	for _, t := range strings.Split(header, ",") {
		if strings.TrimSpace(t) == tag || strings.TrimSpace(t) == "*" {
			return true
		}
	}
	return false
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
