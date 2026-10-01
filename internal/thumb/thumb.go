package thumb

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/m1ngsama/filebox/internal/extract"
	"github.com/m1ngsama/filebox/internal/httpx"
	"github.com/m1ngsama/filebox/internal/vol"
)

const (
	maxFailed = 10000
	maxNames  = 10000
	maxCover  = 64 << 20
)

var probeTimeout = 10 * time.Second

// MaxPixels stops a small file that declares a huge picture from making a decoder allocate gigabytes; 120 MP still covers 100 MP cameras.
const MaxPixels = "120000000"

var errCanceled = errors.New("thumb: request canceled")

var kinds = map[string]string{
	".jpg": "image", ".jpeg": "image", ".png": "image", ".gif": "image", ".webp": "image",
	".bmp": "image", ".tif": "image", ".tiff": "image", ".avif": "image", ".heic": "heic", ".heif": "heic",
	".cr2": "raw", ".cr3": "raw", ".nef": "raw", ".arw": "raw", ".dng": "raw", ".pdf": "pdf",
	".mp4": "video", ".m4v": "video", ".mkv": "video", ".mov": "video", ".avi": "video",
	".webm": "video", ".ts": "video", ".flv": "video", ".wmv": "video", ".mpg": "video", ".mpeg": "video",
	".epub": "epub", ".cbz": "cbz",
	".mp3": "audio", ".m4a": "audio", ".aac": "audio", ".flac": "audio", ".ogg": "audio", ".opus": "audio", ".wav": "audio",
}

func Kind(name string) string { return kinds[strings.ToLower(path.Ext(name))] }

type format struct {
	ext, mime string
	args      []string
}

var (
	webp = format{"webp", "image/webp", []string{"-c:v", "libwebp", "-quality", "75", "-f", "webp"}}
	jpeg = format{"jpg", "image/jpeg", []string{"-c:v", "mjpeg", "-q:v", "5", "-pix_fmt", "yuvj420p", "-f", "mjpeg"}}
)

type Service struct {
	FFmpeg, FFprobe, Dir string

	format   format
	nice     string
	pdf      string
	exiftool string
	heic     bool

	sem      chan struct{}
	warm     chan struct{}
	probes   chan struct{}
	mu       sync.Mutex
	inflight map[string]chan struct{}
	failed   map[string]struct{}
	covers   map[string]string
}

func New(ffmpeg, dir string) *Service {
	if ffmpeg != "" && !filepath.IsAbs(ffmpeg) {
		if p, err := exec.LookPath(ffmpeg); err == nil {
			ffmpeg = p
		}
	}
	s := &Service{FFmpeg: ffmpeg, Dir: dir, format: webp, sem: make(chan struct{}, runtime.NumCPU()), warm: make(chan struct{}, 1), probes: make(chan struct{}, runtime.NumCPU()),
		inflight: map[string]chan struct{}{}, failed: map[string]struct{}{}, covers: map[string]string{}}
	s.nice, _ = exec.LookPath("nice")
	if ffmpeg != "" {
		if p := filepath.Join(filepath.Dir(ffmpeg), "ffprobe"); fileExists(p) {
			s.FFprobe = p
		}
	}
	return s
}

// Distro and Homebrew ffmpeg builds often lack libwebp.
func (s *Service) Probe(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	if s.FFmpeg == "" {
		slog.Info("thumbnails disabled: no -ffmpeg")
		return
	}
	out, err := exec.CommandContext(ctx, s.FFmpeg, "-hide_banner", "-encoders").Output()
	switch {
	case err != nil:
		slog.Error("thumbnails disabled: ffmpeg did not run", "ffmpeg", s.FFmpeg, "err", err)
		s.FFmpeg = ""
	case hasEncoder(out, "libwebp"):
		s.format = webp
		slog.Info("thumbnails: webp", "ffmpeg", s.FFmpeg)
	case hasEncoder(out, "mjpeg"):
		s.format = jpeg
		slog.Warn("thumbnails: ffmpeg has no libwebp encoder, using jpeg", "ffmpeg", s.FFmpeg)
	default:
		slog.Error("thumbnails disabled: ffmpeg has neither libwebp nor mjpeg", "ffmpeg", s.FFmpeg)
		s.FFmpeg = ""
	}
	if s.FFmpeg == "" {
		return
	}
	s.probeTools(ctx)
	marker := filepath.Join(s.Dir, "format")
	if b, err := os.ReadFile(marker); err == nil && string(b) == s.format.ext {
		return
	}
	s.purge()
	os.MkdirAll(s.Dir, 0o755)
	os.WriteFile(marker, []byte(s.format.ext), 0o644)
}

func (s *Service) purge() {
	filepath.WalkDir(s.Dir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && filepath.Ext(p) != "."+s.format.ext {
			os.Remove(p)
		}
		return nil
	})
}

func hasEncoder(list []byte, name string) bool {
	for _, l := range strings.Split(string(list), "\n") {
		if f := strings.Fields(l); len(f) >= 2 && f[1] == name && strings.HasPrefix(f[0], "V") {
			return true
		}
	}
	return false
}

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }

func cacheKey(volName, rel string, size, mtime int64) string {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%s\x00%d\x00%d", volName, rel, size, mtime)))
	return hex.EncodeToString(sum[:])
}

func (s *Service) isFailed(key string) bool {
	s.mu.Lock()
	_, bad := s.failed[key]
	s.mu.Unlock()
	return bad
}

func (s *Service) markFailed(key string) {
	s.mu.Lock()
	if len(s.failed) >= maxFailed {
		s.failed = map[string]struct{}{}
	}
	s.failed[key] = struct{}{}
	s.mu.Unlock()
}

func (s *Service) place(volName, name string, fi fs.FileInfo) (key, out string) {
	key = cacheKey(volName, name, fi.Size(), fi.ModTime().UnixNano())
	return key, filepath.Join(s.Dir, key[:2], key+"."+s.format.ext)
}

// Warm renders the thumbnail of a file that just arrived, one at a time, so the first visit finds it ready.
func (s *Service) Warm(v *vol.Volume, rel string) {
	if s.FFmpeg == "" || !s.can(Kind(rel)) {
		return
	}
	go func() {
		s.warm <- struct{}{}
		defer func() { <-s.warm }()
		f, err := vol.Open(v.Root, rel)
		if err != nil {
			return
		}
		defer f.Close()
		fi, err := f.Stat()
		if err != nil || !fi.Mode().IsRegular() {
			return
		}
		key, out := s.place(v.Name, rel, fi)
		if fileExists(out) || s.isFailed(key) {
			return
		}
		if err := s.render(context.Background(), key, f, Kind(rel), out, 320); err != nil && !errors.Is(err, errCanceled) && !errors.Is(err, os.ErrNotExist) {
			s.markFailed(key)
		}
	}()
}

func (s *Service) Serve(w http.ResponseWriter, r *http.Request, v *vol.Volume, rel string) {
	s.ServeFrom(w, r, v.Root, rel, v.Name, rel)
}

func (s *Service) ServeFrom(w http.ResponseWriter, r *http.Request, root *os.Root, open, volName, rel string) {
	if s.FFmpeg == "" {
		httpx.Fail(w, 404, "no thumbnail")
		return
	}
	f, err := vol.Open(root, open)
	if err != nil {
		httpx.Fail(w, 404, "no thumbnail")
		return
	}
	defer func() { f.Close() }()
	fi, err := f.Stat()
	if err == nil && fi.IsDir() {
		name := s.cover(f, volName+"\x00"+rel, fi.ModTime().UnixNano())
		if name == "" {
			httpx.Fail(w, 404, "no thumbnail")
			return
		}
		child, err := vol.Open(root, path.Join(open, name))
		if err != nil {
			httpx.Fail(w, 404, "no thumbnail")
			return
		}
		f.Close()
		f = child
		open, rel = path.Join(open, name), path.Join(rel, name)
		fi, err = f.Stat()
		if err != nil {
			httpx.Fail(w, 404, "no thumbnail")
			return
		}
	}
	kind := Kind(rel)
	if !fi.Mode().IsRegular() || !s.can(kind) {
		httpx.Fail(w, 404, "no thumbnail")
		return
	}
	side, name := 320, rel
	if (kind == "image" || kind == "audio") && r.URL.Query().Has("large") {
		side, name = 2048, rel+"\x00large"
	}
	key, out := s.place(volName, name, fi)
	if !fileExists(out) {
		if s.isFailed(key) {
			httpx.Fail(w, 404, "no thumbnail")
			return
		}
		if err := s.render(r.Context(), key, f, kind, out, side); err != nil {
			if !errors.Is(err, errCanceled) && !errors.Is(err, os.ErrNotExist) {
				s.markFailed(key)
			}
			httpx.Fail(w, 404, "no thumbnail")
			return
		}
	}
	t, err := os.Open(out)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	defer t.Close()
	h := w.Header()
	h.Set("Content-Type", s.format.mime)
	h.Set("Cache-Control", "private, max-age=86400")
	h.Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, "", fi.ModTime(), t)
}

// ffmpeg reads fd 3, never the path: resolving the path would follow symlinks out of the volume.
func (s *Service) render(ctx context.Context, key string, src *os.File, kind, out string, side int) error {
	s.mu.Lock()
	if ch, ok := s.inflight[key]; ok {
		s.mu.Unlock()
		select {
		case <-ch:
		case <-ctx.Done():
			return errCanceled
		}
		if !fileExists(out) {
			return os.ErrNotExist
		}
		return nil
	}
	ch := make(chan struct{})
	s.inflight[key] = ch
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		delete(s.inflight, key)
		s.mu.Unlock()
		close(ch)
	}()

	select {
	case s.sem <- struct{}{}:
	case <-ctx.Done():
		return errCanceled
	}
	defer func() { <-s.sem }()
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return err
	}
	rctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	if kind != "image" && kind != "video" && kind != "audio" {
		pre := out + ".src"
		defer os.Remove(pre)
		if err := s.prepare(rctx, kind, src, pre); err != nil {
			return err
		}
		f, err := os.Open(pre)
		if err != nil {
			return err
		}
		defer f.Close()
		src = f
	}

	args := []string{"-nostdin", "-v", "error", "-y"}
	if kind == "video" {
		if d := s.duration(rctx, src); d > 0 {
			args = append(args, "-ss", strconv.FormatFloat(d/10, 'f', 2, 64))
		}
	}
	tmp := out + ".tmp"
	args = append(args, "-max_pixels", MaxPixels, "-protocol_whitelist", "file", "-i", "/dev/fd/3", "-frames:v", "1", "-vf", fmt.Sprintf("scale='min(%d,iw)':-2", side))
	args = append(append(args, s.format.args...), tmp)
	src.Seek(0, io.SeekStart)
	cmd := s.command(rctx, s.FFmpeg, args...)
	cmd.ExtraFiles = []*os.File{src}
	if err := cmd.Run(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, out)
}

func (s *Service) duration(ctx context.Context, src *os.File) float64 {
	if s.FFprobe == "" {
		return 0
	}
	src.Seek(0, io.SeekStart)
	cmd := exec.CommandContext(ctx, s.FFprobe, "-v", "error", "-protocol_whitelist", "file", "-show_entries", "format=duration", "-of", "csv=p=0", "/dev/fd/3")
	cmd.ExtraFiles = []*os.File{src}
	b, err := cmd.Output()
	if err != nil {
		return 0
	}
	d, _ := strconv.ParseFloat(strings.TrimSpace(string(b)), 64)
	return d
}

// A folder's cover is its first image, else its first book, by name; only the first maxNames are read.
func (s *Service) cover(dir *os.File, key string, mtime int64) string {
	key = fmt.Sprintf("%s\x00%d", key, mtime)
	s.mu.Lock()
	name, ok := s.covers[key]
	s.mu.Unlock()
	if ok {
		return name
	}
	name = s.scan(dir)
	s.mu.Lock()
	if len(s.covers) >= maxFailed {
		s.covers = map[string]string{}
	}
	s.covers[key] = name
	s.mu.Unlock()
	return name
}

func (s *Service) scan(dir *os.File) string {
	list, _ := dir.ReadDir(maxNames)
	var image, book string
	for _, e := range list {
		n := e.Name()
		if strings.HasPrefix(n, ".") || e.IsDir() {
			continue
		}
		switch k := Kind(n); {
		case k == "image" && (image == "" || extract.Natural(n, image) < 0):
			image = n
		case (k == "epub" || k == "cbz" || k == "pdf" || k == "audio") && s.can(k) && (book == "" || extract.Natural(n, book) < 0):
			book = n
		}
	}
	return cmp.Or(image, book)
}
