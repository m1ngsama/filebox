package thumb

import (
	"archive/zip"
	"bytes"
	"context"
	_ "embed"
	"encoding/binary"
	"errors"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/m1ngsama/filebox/internal/extract"
)

//go:embed probe.heic
var probeHEIC []byte

var errNoTool = errors.New("thumb: no tool for this kind")

func (s *Service) can(kind string) bool {
	switch kind {
	case "image", "video", "raw", "epub", "cbz":
		return true
	case "pdf":
		return s.pdf != ""
	case "heic":
		return s.heic
	}
	return false
}

func (s *Service) command(ctx context.Context, name string, args ...string) *exec.Cmd {
	if s.nice != "" {
		return exec.CommandContext(ctx, s.nice, append([]string{"-n", "19", name}, args...)...)
	}
	return exec.CommandContext(ctx, name, args...)
}

func runs(ctx context.Context, name string, args ...string) string {
	p, err := exec.LookPath(name)
	if err != nil || exec.CommandContext(ctx, p, args...).Run() != nil {
		return ""
	}
	return p
}

func (s *Service) probeTools(ctx context.Context) {
	if s.pdf = runs(ctx, "pdftoppm", "-v"); s.pdf == "" {
		s.pdf = runs(ctx, "mutool", "-v")
	}
	if s.pdf != "" {
		slog.Info("thumbnails: pdf", "tool", s.pdf)
	} else {
		slog.Info("thumbnails: no pdftoppm or mutool, pdf thumbnails off")
	}
	if s.exiftool = runs(ctx, "exiftool", "-ver"); s.exiftool != "" {
		slog.Info("thumbnails: raw previews", "tool", s.exiftool)
	} else {
		slog.Info("thumbnails: no exiftool, raw thumbnails use ffmpeg")
	}
	s.heic = s.decodesHEIC(ctx)
	slog.Info("thumbnails: heic", "enabled", s.heic)
}

func (s *Service) decodesHEIC(ctx context.Context) bool {
	dir, err := os.MkdirTemp("", "filebox-probe-")
	if err != nil {
		return false
	}
	defer os.RemoveAll(dir)
	in, out := filepath.Join(dir, "p.heic"), filepath.Join(dir, "p.png")
	if os.WriteFile(in, probeHEIC, 0o600) != nil {
		return false
	}
	f, err := os.Open(in)
	if err != nil {
		return false
	}
	defer f.Close()
	if s.decode(ctx, f, out) != nil {
		return false
	}
	w, _ := pngWidth(out)
	return w == 1200
}

// Tile-grid HEIC decodes through a complex filtergraph, which cannot be combined with -vf.
func (s *Service) decode(ctx context.Context, src *os.File, out string) error {
	src.Seek(0, io.SeekStart)
	cmd := s.command(ctx, s.FFmpeg, "-nostdin", "-v", "error", "-y", "-protocol_whitelist", "file", "-i", "/dev/fd/3",
		"-frames:v", "1", "-c:v", "png", "-f", "image2", out)
	cmd.ExtraFiles = []*os.File{src}
	return cmd.Run()
}

func pngWidth(p string) (uint32, error) {
	f, err := os.Open(p)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	var h [24]byte
	if _, err := io.ReadFull(f, h[:]); err != nil || !bytes.HasPrefix(h[:], []byte("\x89PNG")) {
		return 0, errors.New("not a png")
	}
	return binary.BigEndian.Uint32(h[16:20]), nil
}

func (s *Service) prepare(ctx context.Context, kind string, src *os.File, out string) error {
	src.Seek(0, io.SeekStart)
	switch kind {
	case "heic":
		return s.decode(ctx, src, out)
	case "pdf":
		defer os.Remove(out + ".png")
		var cmd *exec.Cmd
		if strings.HasSuffix(s.pdf, "mutool") {
			cmd = s.command(ctx, s.pdf, "draw", "-q", "-o", out, "-w", "640", "-h", "640", "-F", "png", "/dev/fd/3", "1")
		} else {
			cmd = s.command(ctx, s.pdf, "-f", "1", "-l", "1", "-singlefile", "-scale-to", "640", "-png", "/dev/fd/3", strings.TrimSuffix(out, ".png"))
		}
		cmd.ExtraFiles = []*os.File{src}
		if err := cmd.Run(); err != nil {
			return err
		}
		if !strings.HasSuffix(s.pdf, "mutool") {
			return os.Rename(out+".png", out)
		}
		return nil
	case "epub", "cbz":
		return page(kind, src, out)
	case "raw":
		if s.exiftool != "" {
			for _, tag := range []string{"-PreviewImage", "-JpgFromRaw"} {
				if s.exif(ctx, src, tag, out) == nil {
					return nil
				}
				src.Seek(0, io.SeekStart)
			}
		}
		return s.decode(ctx, src, out)
	}
	return errNoTool
}

func (s *Service) exif(ctx context.Context, src *os.File, tag, out string) error {
	f, err := os.Create(out)
	if err != nil {
		return err
	}
	defer f.Close()
	cmd := s.command(ctx, s.exiftool, "-b", tag, "/dev/fd/3")
	cmd.ExtraFiles = []*os.File{src}
	cmd.Stdout = f
	var head [2]byte
	if err := cmd.Run(); err == nil {
		if _, err := f.ReadAt(head[:], 0); err == nil && head == [2]byte{0xff, 0xd8} {
			return nil
		}
	}
	os.Remove(out)
	return errNoTool
}

func page(kind string, src *os.File, out string) error {
	st, err := src.Stat()
	if err != nil {
		return err
	}
	z, err := extract.Zip(src, st.Size())
	if err != nil {
		return err
	}
	var e *zip.File
	if kind == "epub" {
		e = extract.Cover(z)
	} else if list := extract.Images(z); len(list) > 0 {
		e = list[0]
	}
	r, err := extract.Open(e, maxCover)
	if err != nil {
		return err
	}
	defer r.Close()
	f, err := os.Create(out)
	if err != nil {
		return err
	}
	if _, err = io.Copy(f, r); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}
