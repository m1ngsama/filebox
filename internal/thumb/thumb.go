package thumb

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
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

var kinds = map[string]string{
	".jpg": "image", ".jpeg": "image", ".png": "image", ".gif": "image", ".webp": "image",
	".bmp": "image", ".tif": "image", ".tiff": "image", ".heic": "image", ".avif": "image",
	".mp4": "video", ".m4v": "video", ".mkv": "video", ".mov": "video", ".avi": "video",
	".webm": "video", ".ts": "video", ".flv": "video", ".wmv": "video", ".mpg": "video", ".mpeg": "video",
}

func Kind(name string) string { return kinds[strings.ToLower(path.Ext(name))] }

type Service struct {
	FFmpeg, FFprobe, Dir string

	sem      chan struct{}
	mu       sync.Mutex
	inflight map[string]chan struct{}
	failed   sync.Map
}

func New(ffmpeg, dir string) *Service {
	s := &Service{FFmpeg: ffmpeg, Dir: dir, sem: make(chan struct{}, 2), inflight: map[string]chan struct{}{}}
	if ffmpeg != "" {
		if p := filepath.Join(filepath.Dir(ffmpeg), "ffprobe"); fileExists(p) {
			s.FFprobe = p
		}
	}
	return s
}

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }

func (s *Service) Serve(w http.ResponseWriter, r *http.Request, v *vol.Volume, rel string) {
	kind := Kind(rel)
	if s.FFmpeg == "" || kind == "" {
		httpx.Fail(w, 404, "no thumbnail")
		return
	}
	f, err := v.Root.Open(rel)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil || fi.IsDir() {
		httpx.Fail(w, 404, "no thumbnail")
		return
	}
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s\x00%s\x00%d\x00%d", v.Name, rel, fi.Size(), fi.ModTime().UnixNano())))
	key := hex.EncodeToString(sum[:])
	out := filepath.Join(s.Dir, key[:2], key+".webp")
	if !fileExists(out) {
		if _, bad := s.failed.Load(key); bad {
			httpx.Fail(w, 404, "no thumbnail")
			return
		}
		if err := s.render(key, f, kind, out); err != nil {
			s.failed.Store(key, struct{}{})
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
	h.Set("Content-Type", "image/webp")
	h.Set("Cache-Control", "private, max-age=86400")
	h.Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, "", fi.ModTime(), t)
}

// The source is handed to ffmpeg as fd 3 so it never resolves the path itself;
// resolving it would follow symlinks out of the volume.
func (s *Service) render(key string, src *os.File, kind, out string) error {
	s.mu.Lock()
	if ch, ok := s.inflight[key]; ok {
		s.mu.Unlock()
		<-ch
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

	s.sem <- struct{}{}
	defer func() { <-s.sem }()
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	args := []string{"-n", "19", s.FFmpeg, "-nostdin", "-v", "error", "-y"}
	if kind == "video" {
		if d := s.duration(ctx, src); d > 0 {
			args = append(args, "-ss", strconv.FormatFloat(d/10, 'f', 2, 64))
		}
	}
	tmp := out + ".tmp"
	args = append(args, "-i", "/dev/fd/3", "-frames:v", "1", "-vf", "scale='min(320,iw)':-2",
		"-c:v", "libwebp", "-quality", "75", "-f", "webp", tmp)
	src.Seek(0, io.SeekStart)
	cmd := exec.CommandContext(ctx, "nice", args...)
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
	cmd := exec.CommandContext(ctx, s.FFprobe, "-v", "error", "-show_entries", "format=duration", "-of", "csv=p=0", "/dev/fd/3")
	cmd.ExtraFiles = []*os.File{src}
	b, err := cmd.Output()
	if err != nil {
		return 0
	}
	d, _ := strconv.ParseFloat(strings.TrimSpace(string(b)), 64)
	return d
}
