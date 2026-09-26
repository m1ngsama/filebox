package thumb

import (
	"bytes"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

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
