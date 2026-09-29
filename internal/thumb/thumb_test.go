package thumb

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/m1ngsama/filebox/internal/vol"
)

func setup(t *testing.T, ffmpeg string) (*Service, *vol.Volume, string) {
	t.Helper()
	dir := t.TempDir()
	vols, err := vol.Parse([]string{"v=" + dir})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { vols.Close() })
	v, _ := vols.Get("v")
	return New(ffmpeg, t.TempDir()), v, dir
}

func get(s *Service, v *vol.Volume, rel string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	s.Serve(w, httptest.NewRequest("GET", "/", nil), v, rel)
	return w
}

// This machine's ffmpeg build may lack the libwebp encoder even when ffmpeg is installed.
func ffmpegWithWebP(t *testing.T) string {
	t.Helper()
	ff, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not installed")
	}
	out, err := exec.Command(ff, "-hide_banner", "-encoders").CombinedOutput()
	if err != nil || !strings.Contains(string(out), "libwebp") {
		t.Skip("ffmpeg has no libwebp encoder")
	}
	return ff
}

func TestKind(t *testing.T) {
	cases := map[string]string{"a.JPG": "image", "b.webp": "image", "c.mkv": "video", "d.mp4": "video", "e.txt": "", "f": ""}
	for n, want := range cases {
		if got := Kind(n); got != want {
			t.Errorf("Kind(%q) = %q", n, got)
		}
	}
}

func TestDisabled(t *testing.T) {
	s, v, dir := setup(t, "")
	os.WriteFile(filepath.Join(dir, "a.jpg"), []byte("x"), 0o644)
	if w := get(s, v, "a.jpg"); w.Code != 404 {
		t.Fatalf("%d", w.Code)
	}
}

func TestRender(t *testing.T) {
	ff := ffmpegWithWebP(t)
	s, v, dir := setup(t, ff)
	run := func(args ...string) {
		if out, err := exec.Command(ff, append([]string{"-v", "error", "-y"}, args...)...).CombinedOutput(); err != nil {
			t.Fatalf("fixture: %v %s", err, out)
		}
	}
	run("-f", "lavfi", "-i", "color=red:s=640x480", "-frames:v", "1", filepath.Join(dir, "red.png"))
	run("-f", "lavfi", "-i", "testsrc=duration=3:size=640x360:rate=10", "-c:v", "mpeg4", filepath.Join(dir, "clip.mp4"))
	os.WriteFile(filepath.Join(dir, "broken.jpg"), []byte("not an image"), 0o644)

	for _, n := range []string{"red.png", "clip.mp4"} {
		w := get(s, v, n)
		b := w.Body.Bytes()
		if w.Code != 200 || w.Header().Get("Content-Type") != "image/webp" || len(b) < 12 ||
			!bytes.Equal(b[:4], []byte("RIFF")) || !bytes.Equal(b[8:12], []byte("WEBP")) {
			t.Fatalf("%s: %d %q", n, w.Code, w.Header().Get("Content-Type"))
		}
	}
	cached, _ := filepath.Glob(filepath.Join(s.Dir, "*", "*.webp"))
	if len(cached) != 2 {
		t.Fatalf("cache files = %d", len(cached))
	}
	if w := get(s, v, "broken.jpg"); w.Code != 404 {
		t.Fatalf("broken = %d", w.Code)
	}
	s.FFmpeg = "/nonexistent" // a second attempt must come from the negative cache
	if w := get(s, v, "broken.jpg"); w.Code != 404 {
		t.Fatalf("broken again = %d", w.Code)
	}
	if w := get(s, v, "red.png"); w.Code != 200 {
		t.Fatal("cached thumbnail not served after ffmpeg vanished")
	}
}

func TestCancelWhileQueued(t *testing.T) {
	s, v, dir := setup(t, "/nonexistent-ffmpeg")
	p := filepath.Join(dir, "a.jpg")
	os.WriteFile(p, []byte("x"), 0o644)
	fi, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	key := cacheKey(v.Name, "a.jpg", fi.Size(), fi.ModTime().UnixNano())

	for range cap(s.sem) {
		s.sem <- struct{}{}
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r := httptest.NewRequest("GET", "/", nil).WithContext(ctx)
	w := httptest.NewRecorder()
	done := make(chan struct{})
	go func() {
		s.Serve(w, r, v, "a.jpg")
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Serve blocked past cancellation instead of returning promptly")
	}
	if w.Code != 404 {
		t.Fatalf("code = %d", w.Code)
	}
	if s.isFailed(key) {
		t.Fatal("cancellation was negative-cached")
	}

	for range cap(s.sem) {
		<-s.sem
	}

	if w := get(s, v, "a.jpg"); w.Code != 404 {
		t.Fatalf("second attempt code = %d", w.Code)
	}
	if !s.isFailed(key) {
		t.Fatal("genuine failure after cancellation was not negative-cached (render never ran)")
	}
}

func TestFailedCacheBounded(t *testing.T) {
	s, _, _ := setup(t, "/nonexistent-ffmpeg")
	for i := 0; i < maxFailed+1; i++ {
		s.markFailed(fmt.Sprintf("k%d", i))
	}
	s.mu.Lock()
	n := len(s.failed)
	s.mu.Unlock()
	if n >= maxFailed {
		t.Fatalf("failed cache not bounded: %d entries", n)
	}
}

func TestSymlinkEscape(t *testing.T) {
	ff := ffmpegWithWebP(t)
	s, v, dir := setup(t, ff)
	out := t.TempDir()
	os.WriteFile(filepath.Join(out, "x.png"), []byte("x"), 0o644)
	os.Symlink(filepath.Join(out, "x.png"), filepath.Join(dir, "x.png"))
	if w := get(s, v, "x.png"); w.Code == 200 {
		t.Fatal("thumbnail rendered through escaping symlink")
	}
}

func TestProtocolWhitelist(t *testing.T) {
	bin := t.TempDir()
	log := filepath.Join(bin, "args")
	for _, n := range []string{"ffmpeg", "ffprobe"} {
		script := "#!/bin/sh\necho " + n + " \"$@\" >> " + log + "\nexit 1\n"
		os.WriteFile(filepath.Join(bin, n), []byte(script), 0o755)
	}
	s, v, dir := setup(t, filepath.Join(bin, "ffmpeg"))
	os.WriteFile(filepath.Join(dir, "a.mp4"), []byte("x"), 0o644)
	get(s, v, "a.mp4")
	b, _ := os.ReadFile(log)
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(lines) != 2 {
		t.Fatalf("calls %q", b)
	}
	for _, l := range lines {
		if !strings.Contains(l, "-protocol_whitelist file ") || strings.Index(l, "-protocol_whitelist") > strings.Index(l, "/dev/fd/3") {
			t.Errorf("no whitelist before the input: %s", l)
		}
	}
}

func TestSymlinkEscapeQuiet(t *testing.T) {
	s, v, dir := setup(t, "/nonexistent-ffmpeg")
	out := t.TempDir()
	os.WriteFile(filepath.Join(out, "x.png"), []byte("x"), 0o644)
	os.Symlink(filepath.Join(out, "x.png"), filepath.Join(dir, "x.png"))
	var buf bytes.Buffer
	prev := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&buf, nil)))
	t.Cleanup(func() { slog.SetDefault(prev) })
	if w := get(s, v, "x.png"); w.Code != 404 {
		t.Fatalf("code %d", w.Code)
	}
	if strings.Contains(buf.String(), "ERROR") {
		t.Fatalf("logged %s", buf.String())
	}
}

func fakeFFmpeg(t *testing.T, encoders string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "ffmpeg")
	os.WriteFile(p, []byte("#!/bin/sh\nprintf '%s' '"+encoders+"'\n"), 0o755)
	return p
}

func TestProbe(t *testing.T) {
	cases := []struct {
		encoders, ext string
		enabled       bool
	}{
		{" V....D libwebp              libwebp WebP image (codec webp)\n VFS..D mjpeg                MJPEG\n", "webp", true},
		{" VFS..D mjpeg                MJPEG (Motion JPEG)\n A....D libwebp_fake  audio\n", "jpg", true},
		{" A....D aac                  AAC\n", "", false},
	}
	for _, c := range cases {
		s, _, _ := setup(t, fakeFFmpeg(t, c.encoders))
		for _, n := range []string{"ab/x.webp", "ab/y.jpg"} {
			os.MkdirAll(filepath.Join(s.Dir, "ab"), 0o755)
			os.WriteFile(filepath.Join(s.Dir, n), []byte("x"), 0o644)
		}
		s.Probe(context.Background())
		left, _ := filepath.Glob(filepath.Join(s.Dir, "ab", "*"))
		if c.enabled && (len(left) != 1 || filepath.Ext(left[0]) != "."+c.ext) {
			t.Errorf("%s cache left %v", c.ext, left)
		}
		if c.enabled {
			stale := filepath.Join(s.Dir, "ab", "z.other")
			os.WriteFile(stale, []byte("x"), 0o644)
			s.Probe(context.Background())
			if !fileExists(stale) {
				t.Errorf("%s: cache walked again although the format did not change", c.ext)
			}
		}
		if (s.FFmpeg != "") != c.enabled || (c.enabled && s.format.ext != c.ext) {
			t.Errorf("%q: ffmpeg %q ext %q", c.encoders, s.FFmpeg, s.format.ext)
		}
	}
	s, _, _ := setup(t, "/nonexistent-ffmpeg")
	s.Probe(context.Background())
	if s.FFmpeg != "" {
		t.Fatal("unrunnable ffmpeg left enabled")
	}
}

func TestRenderJPEG(t *testing.T) {
	ff, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not installed")
	}
	s, v, dir := setup(t, ff)
	s.format = jpeg
	if out, err := exec.Command(ff, "-v", "error", "-y", "-f", "lavfi", "-i", "color=blue:s=640x480", "-frames:v", "1", filepath.Join(dir, "b.png")).CombinedOutput(); err != nil {
		t.Fatalf("fixture: %v %s", err, out)
	}
	w := get(s, v, "b.png")
	b := w.Body.Bytes()
	if w.Code != 200 || w.Header().Get("Content-Type") != "image/jpeg" || len(b) < 3 || !bytes.Equal(b[:3], []byte{0xff, 0xd8, 0xff}) {
		t.Fatalf("%d %q %d bytes", w.Code, w.Header().Get("Content-Type"), len(b))
	}
	if cached, _ := filepath.Glob(filepath.Join(s.Dir, "*", "*.jpg")); len(cached) != 1 {
		t.Fatalf("cache %v", cached)
	}
}

func TestProbeTimeout(t *testing.T) {
	p := filepath.Join(t.TempDir(), "ffmpeg")
	os.WriteFile(p, []byte("#!/bin/sh\nexec sleep 60\n"), 0o755)
	s, _, _ := setup(t, p)
	defer func(d time.Duration) { probeTimeout = d }(probeTimeout)
	probeTimeout = 200 * time.Millisecond
	start := time.Now()
	s.Probe(context.Background())
	if time.Since(start) > 5*time.Second || s.FFmpeg != "" {
		t.Fatalf("hung probe: %v, ffmpeg %q", time.Since(start), s.FFmpeg)
	}
}
