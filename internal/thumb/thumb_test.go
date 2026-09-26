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

	s.sem <- struct{}{}
	s.sem <- struct{}{}

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

	<-s.sem
	<-s.sem

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
