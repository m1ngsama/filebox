package thumb

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

const tinyPDF = "%PDF-1.4\n1 0 obj<</Type/Catalog/Pages 2 0 R>>endobj\n2 0 obj<</Type/Pages/Kids[3 0 R]/Count 1>>endobj\n" +
	"3 0 obj<</Type/Page/Parent 2 0 R/MediaBox[0 0 200 100]>>endobj\ntrailer<</Root 1 0 R>>\n%%EOF\n"

func TestPDFThumb(t *testing.T) {
	for _, tool := range []string{"pdftoppm", "mutool"} {
		t.Run(tool, func(t *testing.T) {
			p, err := exec.LookPath(tool)
			if err != nil {
				t.Skip(tool + " not installed")
			}
			ff, err := exec.LookPath("ffmpeg")
			if err != nil {
				t.Skip("ffmpeg not installed")
			}
			s, v, dir := setup(t, ff)
			s.Probe(context.Background())
			if s.FFmpeg == "" {
				t.Skip("ffmpeg cannot encode thumbnails")
			}
			s.pdf = p
			os.WriteFile(filepath.Join(dir, "a.pdf"), []byte(tinyPDF), 0o644)
			if w := get(s, v, "a.pdf"); w.Code != 200 || w.Header().Get("Content-Type") != s.format.mime {
				t.Fatalf("%d %s", w.Code, w.Body)
			}
			if left, _ := filepath.Glob(filepath.Join(s.Dir, "*", "*.src*")); len(left) != 0 {
				t.Fatalf("left %v", left)
			}
		})
	}
}

func TestPDFWithoutTool(t *testing.T) {
	s, v, dir := setup(t, fakeFFmpeg(t, " VFS..D mjpeg MJPEG\n"))
	s.format = jpeg
	s.pdf = ""
	os.WriteFile(filepath.Join(dir, "a.pdf"), []byte(tinyPDF), 0o644)
	if w := get(s, v, "a.pdf"); w.Code != 404 {
		t.Fatalf("%d", w.Code)
	}
}

func TestHEICThumb(t *testing.T) {
	ff, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not installed")
	}
	s, v, dir := setup(t, ff)
	s.Probe(context.Background())
	if s.FFmpeg == "" || !s.heic {
		t.Skip("ffmpeg cannot decode tiled heic")
	}
	os.WriteFile(filepath.Join(dir, "a.HEIC"), probeHEIC, 0o644)
	if w := get(s, v, "a.HEIC"); w.Code != 200 {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
}

func TestHEICProbeRejectsBrokenDecoders(t *testing.T) {
	s, _, _ := setup(t, fakeFFmpeg(t, " VFS..D mjpeg MJPEG\n"))
	if s.decodesHEIC(context.Background()) {
		t.Fatal("heic enabled although ffmpeg wrote nothing")
	}
}
