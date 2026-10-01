package app

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"golang.org/x/text/encoding/simplifiedchinese"
)

func archive(t *testing.T, add func(z *zip.Writer)) []byte {
	t.Helper()
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	add(z)
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func put(z *zip.Writer, h *zip.FileHeader, body string) {
	w, _ := z.CreateHeader(h)
	w.Write([]byte(body))
}

func unpacked(dir string) []string {
	var out []string
	filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err == nil && p != dir {
			rel, _ := filepath.Rel(dir, p)
			out = append(out, filepath.ToSlash(rel))
		}
		return nil
	})
	slices.Sort(out)
	return out
}

func TestUnzipStaysInsideAndReadsChineseNames(t *testing.T) {
	f := newTestApp(t)
	gbk, _ := simplifiedchinese.GBK.NewEncoder().String("报告/第一章.txt")
	data := archive(t, func(z *zip.Writer) {
		put(z, &zip.FileHeader{Name: "notes/a.txt", Method: zip.Deflate}, "a")
		put(z, &zip.FileHeader{Name: "../escape.txt"}, "x")
		put(z, &zip.FileHeader{Name: "/abs.txt"}, "x")
		put(z, &zip.FileHeader{Name: `..\..\win.txt`}, "x")
		put(z, &zip.FileHeader{Name: "__MACOSX/._a.txt"}, "x")
		put(z, &zip.FileHeader{Name: gbk, NonUTF8: true}, "gbk")
		link := &zip.FileHeader{Name: "link"}
		link.SetMode(os.ModeSymlink | 0o777)
		put(z, link, "/etc/passwd")
	})
	os.WriteFile(filepath.Join(f.Dir, "bundle.zip"), data, 0o644)
	os.Mkdir(filepath.Join(f.Dir, "bundle"), 0o755)
	w := f.do("POST", "/api/unzip", body(`{"vol":"v","path":"bundle.zip"}`))
	if w.Code != 202 || !strings.Contains(w.Body.String(), `"name":"bundle 2"`) {
		t.Fatalf("unzip %d %s", w.Code, w.Body)
	}
	if st := waitJob(t, f, decode[struct{ Job string }](t, w).Job); st != "done" {
		t.Fatalf("job %s", st)
	}
	if got, want := unpacked(filepath.Join(f.Dir, "bundle 2")), []string{"notes", "notes/a.txt", "报告", "报告/第一章.txt"}; !slices.Equal(got, want) {
		t.Fatalf("unpacked %v, want %v", got, want)
	}
	for _, p := range []string{"escape.txt", "win.txt", "../escape.txt"} {
		if _, err := os.Stat(filepath.Join(f.Dir, p)); err == nil {
			t.Fatalf("%s escaped the target", p)
		}
	}
}

func TestUnzipRefusesABombAndLeavesNothing(t *testing.T) {
	f := newTestApp(t)
	data := archive(t, func(z *zip.Writer) {
		put(z, &zip.FileHeader{Name: "ok.txt"}, "fine")
		put(z, &zip.FileHeader{Name: "bomb.bin", Method: zip.Deflate}, strings.Repeat("\x00", 8<<20))
	})
	os.WriteFile(filepath.Join(f.Dir, "bomb.zip"), data, 0o644)
	w := f.do("POST", "/api/unzip", body(`{"vol":"v","path":"bomb.zip"}`))
	if st := waitJob(t, f, decode[struct{ Job string }](t, w).Job); st != "error" {
		t.Fatalf("bomb job %s", st)
	}
	if _, err := os.Stat(filepath.Join(f.Dir, "bomb")); err == nil {
		t.Fatal("a half-unpacked folder was left behind")
	}
	if w := f.do("POST", "/api/unzip", body(`{"vol":"v","path":"bomb.txt"}`)); w.Code != 400 {
		t.Fatalf("not a zip %d", w.Code)
	}
}
