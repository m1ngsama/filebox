package thumb

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	stdjpeg "image/jpeg"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func png(t *testing.T, ff, color string) []byte {
	t.Helper()
	p := filepath.Join(t.TempDir(), "c.png")
	if out, err := exec.Command(ff, "-v", "error", "-y", "-f", "lavfi", "-i", "color="+color+":s=64x48", "-frames:v", "1", p).CombinedOutput(); err != nil {
		t.Fatalf("fixture: %v %s", err, out)
	}
	b, _ := os.ReadFile(p)
	return b
}

func archive(t *testing.T, entries ...any) []byte {
	t.Helper()
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	for i := 0; i < len(entries); i += 2 {
		w, _ := z.Create(entries[i].(string))
		switch v := entries[i+1].(type) {
		case string:
			w.Write([]byte(v))
		case []byte:
			w.Write(v)
		}
	}
	z.Close()
	return b.Bytes()
}

func dominant(t *testing.T, b []byte) string {
	t.Helper()
	img, err := stdjpeg.Decode(bytes.NewReader(b))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	c := img.Bounds().Size()
	r, g, bl, _ := img.At(c.X/2, c.Y/2).RGBA()
	switch {
	case r > g && r > bl:
		return "red"
	case g > r && g > bl:
		return "green"
	}
	return "blue"
}

func TestCovers(t *testing.T) {
	ff, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not installed")
	}
	s, v, dir := setup(t, ff)
	s.format = jpeg
	red, blue, green := png(t, ff, "red"), png(t, ff, "blue"), png(t, ff, "0x00ff00")
	opf := `<?xml version="1.0"?><package xmlns="http://www.idpf.org/2007/opf"><manifest>
<item id="p" href="i/p.png"/><item id="c" href="i/c.png" properties="cover-image"/></manifest></package>`
	container := `<container><rootfiles><rootfile full-path="OEBPS/content.opf"/></rootfiles></container>`
	var ratio bytes.Buffer
	z := zip.NewWriter(&ratio)
	w, _ := z.Create("001.jpg")
	w.Write(make([]byte, 8<<20))
	z.Close()
	end := make([]byte, 22)
	binary.LittleEndian.PutUint32(end, 0x06054b50)
	binary.LittleEndian.PutUint16(end[10:], 60000)
	binary.LittleEndian.PutUint32(end[12:], 60000*46)
	files := map[string][]byte{
		"vol.cbz":            archive(t, "__MACOSX/._1.png", "junk", "p10.png", blue, "p2.png", red, "notes.txt", "x"),
		"book.epub":          archive(t, "META-INF/container.xml", container, "OEBPS/content.opf", opf, "OEBPS/i/p.png", red, "OEBPS/i/c.png", green),
		"coverless.epub":     archive(t, "META-INF/container.xml", container, "OEBPS/content.opf", `<package/>`, "OEBPS/i/p.png", red),
		"ratio.cbz":          ratio.Bytes(),
		"directory.cbz":      end,
		"notzip.cbz":         []byte("not a zip"),
		"series/10.png":      blue,
		"series/9.png":       red,
		"series/.hidden.png": green,
		"series/a.epub":      archive(t),
		"books/b.epub":       archive(t, "META-INF/container.xml", container, "OEBPS/content.opf", opf, "OEBPS/i/c.png", green),
		"books/a.cbz":        archive(t, "1.png", blue),
		"empty/readme.txt":   []byte("x"),
		"nested/inner/1.png": red,
		"nested/inner.png/x": []byte("dir named like an image"),
		"series/0.jpg/1.png": red,
	}
	for name, b := range files {
		os.MkdirAll(filepath.Dir(filepath.Join(dir, name)), 0o755)
		if err := os.WriteFile(filepath.Join(dir, name), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for rel, want := range map[string]string{
		"vol.cbz": "red", "book.epub": "green", "series": "red", "books": "blue",
		"coverless.epub": "", "ratio.cbz": "", "directory.cbz": "", "notzip.cbz": "", "empty": "", "nested": "",
	} {
		w := get(s, v, rel)
		if want == "" {
			if w.Code != 404 {
				t.Errorf("%s: %d, want 404", rel, w.Code)
			}
			continue
		}
		if w.Code != 200 || w.Header().Get("Content-Type") != "image/jpeg" {
			t.Errorf("%s: %d %q", rel, w.Code, w.Header().Get("Content-Type"))
			continue
		}
		if got := dominant(t, w.Body.Bytes()); got != want {
			t.Errorf("%s: %s, want %s", rel, got, want)
		}
	}
	if leftovers, _ := filepath.Glob(filepath.Join(s.Dir, "*", "*.src")); len(leftovers) > 0 {
		t.Fatalf("temporary files left: %v", leftovers)
	}
}

func TestCoverCachedPerFolder(t *testing.T) {
	ff, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not installed")
	}
	s, v, dir := setup(t, ff)
	s.format = jpeg
	os.MkdirAll(filepath.Join(dir, "empty"), 0o755)
	os.WriteFile(filepath.Join(dir, "empty/notes.txt"), []byte("x"), 0o644)
	os.MkdirAll(filepath.Join(dir, "art"), 0o755)
	os.WriteFile(filepath.Join(dir, "art/1.png"), png(t, ff, "red"), 0o644)
	for range 3 {
		if w := get(s, v, "empty"); w.Code != 404 {
			t.Fatalf("empty folder = %d", w.Code)
		}
		if w := get(s, v, "art"); w.Code != 200 {
			t.Fatalf("art folder = %d", w.Code)
		}
	}
	if len(s.covers) != 2 {
		t.Fatalf("cached %d folders", len(s.covers))
	}
	os.WriteFile(filepath.Join(dir, "empty/0.png"), png(t, ff, "blue"), 0o644)
	os.Chtimes(filepath.Join(dir, "empty"), time.Now().Add(time.Second), time.Now().Add(time.Second))
	if w := get(s, v, "empty"); w.Code != 200 {
		t.Fatalf("after the folder changed = %d", w.Code)
	}
}
