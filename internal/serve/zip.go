package serve

import (
	"archive/zip"
	"compress/flate"
	"context"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"mime"
	"net/http"
	"os"
	"path"
	"slices"
	"strings"

	"github.com/m1ngsama/filebox/internal/httpx"
	"github.com/m1ngsama/filebox/internal/vol"
)

var compressed = map[string]bool{}

func init() {
	for _, e := range strings.Fields("jpg jpeg png gif webp avif heic heif jxl mp4 m4v mov mkv webm avi flv wmv mpg mpeg ts " +
		"mp3 m4a aac flac ogg opus wma zip gz tgz bz2 xz zst 7z rar apk jar docx xlsx pptx odt ods odp epub pdf") {
		compressed["."+e] = true
	}
}

func ZipName(fallback string, rels []string, want string) string {
	if want = strings.TrimSuffix(want, ".zip"); vol.ValidName(want) {
		return want + ".zip"
	}
	name := path.Base(rels[0])
	if len(rels) > 1 {
		name = path.Base(path.Dir(rels[0]))
	}
	if name == "." {
		name = fallback
	}
	return name + ".zip"
}

func Zip(w http.ResponseWriter, r *http.Request, root *os.Root, rels []string, top, name string) bool {
	for _, rel := range rels {
		if _, err := root.Stat(rel); err != nil {
			httpx.Error(w, err)
			return false
		}
	}
	out := &counter{w: w}
	zw := zip.NewWriter(out)
	var fw *flate.Writer
	zw.RegisterCompressor(zip.Deflate, func(dst io.Writer) (io.WriteCloser, error) {
		if fw == nil {
			var err error
			fw, err = flate.NewWriter(dst, flate.BestSpeed)
			return fw, err
		}
		fw.Reset(dst)
		return fw, nil
	})
	z := &zipper{zw: zw, root: root, ctx: r.Context(), seen: map[string]bool{}}
	z.base, _ = root.Stat(".")
	h := w.Header()
	h.Set("Content-Type", "application/zip")
	h.Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
	h.Set("Cache-Control", "private, no-store")
	var err error
	for _, rel := range rels {
		arc := top
		if rel != "." {
			arc = z.unique(path.Base(rel))
		}
		if err = z.add(rel, arc, true); err != nil {
			break
		}
	}
	if err == nil {
		err = zw.Close()
	}
	switch {
	case err == nil:
		return true
	case r.Context().Err() != nil:
		return out.n > 0
	case out.n == 0:
		h.Del("Content-Disposition")
		h.Del("Cache-Control")
		httpx.Error(w, err)
		return false
	}
	slog.Warn("zip aborted", "err", err)
	panic(http.ErrAbortHandler)
}

type counter struct {
	w io.Writer
	n int64
}

func (c *counter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += int64(n)
	return n, err
}

type zipper struct {
	zw   *zip.Writer
	root *os.Root
	ctx  context.Context
	seen map[string]bool
	base fs.FileInfo
}

func (z *zipper) unique(name string) string {
	n := name
	for i := 1; z.seen[n]; i++ {
		n = vol.Numbered(name, i)
	}
	z.seen[n] = true
	return n
}

func (z *zipper) add(rel, arc string, top bool) error {
	if err := z.ctx.Err(); err != nil {
		return err
	}
	fi, err := z.root.Lstat(rel)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	if fi.Mode()&fs.ModeSymlink != 0 {
		if fi, err = z.root.Stat(rel); err != nil || (fi.IsDir() && !top) {
			return nil
		}
	}
	switch {
	case fi.IsDir():
		return z.dir(rel, arc, fi)
	case fi.Mode().IsRegular():
		return z.file(rel, arc, fi)
	}
	return nil
}

func (z *zipper) dir(rel, arc string, fi fs.FileInfo) error {
	if arc != "" {
		hdr := &zip.FileHeader{Name: arc + "/", Modified: fi.ModTime()}
		hdr.SetMode(fi.Mode())
		if _, err := z.zw.CreateHeader(hdr); err != nil {
			return err
		}
	}
	f, err := z.root.Open(rel)
	if err != nil {
		return err
	}
	names, err := f.Readdirnames(-1)
	st, serr := f.Stat()
	f.Close()
	if err == nil {
		err = serr
	}
	if err != nil {
		return err
	}
	top := z.base != nil && os.SameFile(st, z.base)
	slices.Sort(names)
	for _, n := range names {
		if top && vol.Reserved(n) {
			continue
		}
		if err := z.add(path.Join(rel, n), path.Join(arc, n), false); err != nil {
			return err
		}
	}
	return nil
}

func (z *zipper) file(rel, arc string, fi fs.FileInfo) error {
	f, err := z.root.Open(rel)
	if err != nil {
		return err
	}
	defer f.Close()
	hdr := &zip.FileHeader{Name: arc, Modified: fi.ModTime(), Method: zip.Deflate}
	if compressed[strings.ToLower(path.Ext(arc))] {
		hdr.Method = zip.Store
	}
	hdr.SetMode(fi.Mode())
	out, err := z.zw.CreateHeader(hdr)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, f)
	return err
}
