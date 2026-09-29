package serve

import (
	"archive/zip"
	"compress/flate"
	"context"
	"fmt"
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

func Zip(w http.ResponseWriter, r *http.Request, root *os.Root, rels []string, name string) {
	for _, rel := range rels {
		if _, err := root.Stat(rel); err != nil {
			httpx.Error(w, err)
			return
		}
	}
	h := w.Header()
	h.Set("Content-Type", "application/zip")
	h.Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
	h.Set("Cache-Control", "private, no-store")
	zw := zip.NewWriter(w)
	zw.RegisterCompressor(zip.Deflate, func(out io.Writer) (io.WriteCloser, error) { return flate.NewWriter(out, flate.BestSpeed) })
	z := &zipper{zw: zw, root: root, ctx: r.Context(), seen: map[string]bool{}}
	for _, rel := range rels {
		arc := ""
		if rel != "." {
			arc = z.unique(path.Base(rel))
		}
		if err := z.add(rel, arc); err != nil {
			if r.Context().Err() == nil {
				slog.Warn("zip aborted", "path", rel, "err", err)
			}
			return
		}
	}
	zw.Close()
}

type zipper struct {
	zw   *zip.Writer
	root *os.Root
	ctx  context.Context
	seen map[string]bool
}

func (z *zipper) unique(name string) string {
	ext := path.Ext(name)
	n := name
	for i := 1; z.seen[n]; i++ {
		n = fmt.Sprintf("%s (%d)%s", strings.TrimSuffix(name, ext), i, ext)
	}
	z.seen[n] = true
	return n
}

func (z *zipper) add(rel, arc string) error {
	if err := z.ctx.Err(); err != nil {
		return err
	}
	fi, err := z.root.Lstat(rel)
	if err == nil && fi.Mode()&fs.ModeSymlink != 0 {
		if fi, err = z.root.Stat(rel); err == nil && fi.IsDir() {
			return nil
		}
	}
	switch {
	case err != nil:
		return nil
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
		return nil
	}
	names, err := f.Readdirnames(-1)
	f.Close()
	if err != nil {
		return nil
	}
	slices.Sort(names)
	for _, n := range names {
		if rel == "." && vol.Reserved(n) {
			continue
		}
		if err := z.add(path.Join(rel, n), path.Join(arc, n)); err != nil {
			return err
		}
	}
	return nil
}

func (z *zipper) file(rel, arc string, fi fs.FileInfo) error {
	f, err := z.root.Open(rel)
	if err != nil {
		return nil
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
