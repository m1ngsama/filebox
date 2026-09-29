package thumb

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/m1ngsama/filebox/internal/httpx"
	"github.com/m1ngsama/filebox/internal/vol"
)

const maxFailed = 10000

var probeTimeout = 10 * time.Second

var errCanceled = errors.New("thumb: request canceled")

var kinds = map[string]string{
	".jpg": "image", ".jpeg": "image", ".png": "image", ".gif": "image", ".webp": "image",
	".bmp": "image", ".tif": "image", ".tiff": "image", ".heic": "image", ".avif": "image",
	".mp4": "video", ".m4v": "video", ".mkv": "video", ".mov": "video", ".avi": "video",
	".webm": "video", ".ts": "video", ".flv": "video", ".wmv": "video", ".mpg": "video", ".mpeg": "video",
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

	format format
	nice   string

	sem      chan struct{}
	mu       sync.Mutex
	inflight map[string]chan struct{}
	failed   map[string]struct{}
}

func New(ffmpeg, dir string) *Service {
	if ffmpeg != "" && !filepath.IsAbs(ffmpeg) {
		if p, err := exec.LookPath(ffmpeg); err == nil {
			ffmpeg = p
		}
	}
	s := &Service{FFmpeg: ffmpeg, Dir: dir, format: webp, sem: make(chan struct{}, 2),
		inflight: map[string]chan struct{}{}, failed: map[string]struct{}{}}
	s.nice, _ = exec.LookPath("nice")
	if ffmpeg != "" {
		if p := filepath.Join(filepath.Dir(ffmpeg), "ffprobe"); fileExists(p) {
			s.FFprobe = p
		}
	}
	return s
}

// Probe picks the thumbnail encoder; distro and Homebrew ffmpeg builds often lack libwebp.
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

func (s *Service) Serve(w http.ResponseWriter, r *http.Request, v *vol.Volume, rel string) {
	kind := Kind(rel)
	if s.FFmpeg == "" || kind == "" {
		httpx.Fail(w, 404, "no thumbnail")
		return
	}
	f, err := v.Root.Open(rel)
	if err != nil {
		httpx.Fail(w, 404, "no thumbnail")
		return
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil || fi.IsDir() {
		httpx.Fail(w, 404, "no thumbnail")
		return
	}
	key := cacheKey(v.Name, rel, fi.Size(), fi.ModTime().UnixNano())
	out := filepath.Join(s.Dir, key[:2], key+"."+s.format.ext)
	if !fileExists(out) {
		if s.isFailed(key) {
			httpx.Fail(w, 404, "no thumbnail")
			return
		}
		if err := s.render(r.Context(), key, f, kind, out); err != nil {
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
func (s *Service) render(ctx context.Context, key string, src *os.File, kind, out string) error {
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

	args := []string{"-nostdin", "-v", "error", "-y"}
	if kind == "video" {
		if d := s.duration(rctx, src); d > 0 {
			args = append(args, "-ss", strconv.FormatFloat(d/10, 'f', 2, 64))
		}
	}
	tmp := out + ".tmp"
	args = append(args, "-protocol_whitelist", "file", "-i", "/dev/fd/3", "-frames:v", "1", "-vf", "scale='min(320,iw)':-2")
	args = append(append(args, s.format.args...), tmp)
	src.Seek(0, io.SeekStart)
	name, argv := s.FFmpeg, args
	if s.nice != "" {
		name, argv = s.nice, append([]string{"-n", "19", s.FFmpeg}, args...)
	}
	cmd := exec.CommandContext(rctx, name, argv...)
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
