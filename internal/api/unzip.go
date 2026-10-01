package api

import (
	"archive/zip"
	"errors"
	"io"
	"io/fs"
	"os"
	"path"
	"strings"
	"syscall"
	"unicode/utf8"

	"golang.org/x/text/encoding/simplifiedchinese"

	"github.com/m1ngsama/filebox/internal/extract"
	"github.com/m1ngsama/filebox/internal/index"
	"github.com/m1ngsama/filebox/internal/vol"
)

var ErrUnsafeArchive = errors.New("archive is unsafe or unreadable")

// unzipReserve keeps this much of the volume free after an archive is unpacked.
const unzipReserve = 1 << 30

func (j *Jobs) Unzip(v *vol.Volume, rel, drel string) string {
	return j.launch(func(id string, x *job) error { return unzip(j.ix, v, rel, drel, id, x) })
}

// entryName turns an archive path into a safe relative path, or "" for entries to skip.
// Windows zips often store Chinese names in GBK without the UTF-8 flag.
func entryName(e *zip.File) string {
	n := e.Name
	if e.NonUTF8 || !utf8.ValidString(n) {
		if s, err := simplifiedchinese.GB18030.NewDecoder().String(n); err == nil {
			n = s
		}
	}
	n = strings.ReplaceAll(n, `\`, "/")
	n = strings.TrimSuffix(n, "/")
	for _, seg := range strings.Split(n, "/") {
		if seg == "__MACOSX" || vol.Junk(seg, false) {
			return ""
		}
	}
	if !fs.ValidPath(n) || n == "." {
		return ""
	}
	return n
}

func unzip(ix *index.Index, v *vol.Volume, rel, drel, id string, x *job) error {
	defer ix.Touch(v, drel)
	f, err := vol.Open(v.Root, rel)
	if err != nil {
		return err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return err
	}
	z, err := extract.Zip(f, fi.Size())
	if err != nil {
		return ErrUnsafeArchive
	}
	var total int64
	for _, e := range z.File {
		if !e.FileInfo().IsDir() {
			total += int64(min(e.UncompressedSize64, 1<<62))
		}
	}
	if free, err := v.Free(); err != nil || uint64(total)+unzipReserve > free {
		return syscall.ENOSPC
	}
	x.total.Store(total)
	stage := path.Join(vol.JobsDir, id)
	if err := v.Root.MkdirAll(stage, 0o700); err != nil {
		return err
	}
	defer v.Root.RemoveAll(stage)
	tmp := path.Join(stage, "item")
	if err := v.Root.Mkdir(tmp, 0o755); err != nil {
		return err
	}
	for _, e := range z.File {
		n := entryName(e)
		if n == "" || e.Mode()&fs.ModeSymlink != 0 {
			continue
		}
		to := path.Join(tmp, n)
		if e.FileInfo().IsDir() {
			if err := v.Root.MkdirAll(to, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := v.Root.MkdirAll(path.Dir(to), 0o755); err != nil {
			return err
		}
		if err := unpack(v.Root, e, f, to, x); err != nil {
			return err
		}
	}
	return place(v, tmp, drel, x)
}

func unpack(r *os.Root, e *zip.File, src io.ReaderAt, to string, x *job) error {
	in, err := extract.Detach(e, src, int64(min(e.UncompressedSize64, 1<<62)))
	if errors.Is(err, extract.ErrSkipped) && e.UncompressedSize64 == 0 {
		in, err = io.NopCloser(strings.NewReader("")), nil
	}
	if err != nil {
		return ErrUnsafeArchive
	}
	defer in.Close()
	out, err := r.OpenFile(to, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	n, err := io.Copy(out, counter{in, &x.done})
	if cerr := out.Close(); err == nil {
		err = cerr
	}
	if err == nil && n != int64(e.UncompressedSize64) {
		err = ErrUnsafeArchive
	}
	if err == nil {
		r.Chtimes(to, e.Modified, e.Modified)
	}
	return err
}
