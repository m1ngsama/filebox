package extract

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"golang.org/x/text/encoding/simplifiedchinese"
)

func fixture(t *testing.T, files map[string][]byte) *os.Root {
	t.Helper()
	dir := t.TempDir()
	for name, b := range files {
		if err := os.WriteFile(filepath.Join(dir, name), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	r, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close() })
	return r
}

func epubFile(t *testing.T, entries [][2]string) []byte {
	t.Helper()
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	for _, e := range entries {
		w, err := z.Create(e[0])
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte(e[1]))
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

const containerXML = `<?xml version="1.0"?><container xmlns="urn:oasis:names:tc:opendocument:xmlns:container">
<rootfiles><rootfile full-path="OEBPS/content.opf" media-type="application/oebps-package+xml"/></rootfiles></container>`

const opfXML = `<?xml version="1.0"?><package xmlns="http://www.idpf.org/2007/opf"><manifest>
<item id="a" href="Text/ch%201.xhtml" media-type="application/xhtml+xml"/>
<item id="b" href="Text/ch2.xhtml" media-type="application/xhtml+xml"/>
</manifest><spine><itemref idref="b"/><itemref idref="a"/></spine></package>`

func TestPlainText(t *testing.T) {
	gbk, _ := simplifiedchinese.GB18030.NewEncoder().String("年度报告\n第二行 内容")
	r := fixture(t, map[string][]byte{
		"a.md":      []byte("\xef\xbb\xbf# Title\r\n\r\nSome  *markdown*\x02text\x03 here\n"),
		"gbk.txt":   []byte(gbk),
		"main.go":   []byte("package main\n\nfunc main() {}\n"),
		"Makefile":  []byte("all:\n\tgo build\n"),
		"bin.txt":   []byte("abc\x00def"),
		"photo.jpg": []byte("\xff\xd8"),
		"wrap.txt":  []byte("中文段落在这里\n换行继续 and English\nwords"),
	})
	x := &Extractor{}
	for name, want := range map[string]string{
		"a.md":     "# Title Some *markdown* text here",
		"gbk.txt":  "年度报告第二行 内容",
		"main.go":  "package main func main() {}",
		"Makefile": "all: go build",
		"wrap.txt": "中文段落在这里换行继续 and English words",
	} {
		got, err := x.Extract(context.Background(), r, name)
		if err != nil || got != want {
			t.Errorf("%s: %q %v, want %q", name, got, err, want)
		}
	}
	for _, name := range []string{"bin.txt", "photo.jpg", "missing.pdf"} {
		if _, err := x.Extract(context.Background(), r, name); !errors.Is(err, ErrSkipped) {
			t.Errorf("%s: %v, want skipped", name, err)
		}
	}
	if !x.Handles("README.MD") || x.Handles("x.pdf") || x.Handles("x.docx") {
		t.Fatal("handles")
	}
}

func TestTextCap(t *testing.T) {
	big := strings.Repeat("字", MaxText)
	r := fixture(t, map[string][]byte{"big.txt": []byte(big)})
	got, err := (&Extractor{}).Extract(context.Background(), r, "big.txt")
	if err != nil || len(got) > MaxText || len(got) < MaxText-3 || !utf8.ValidString(got) {
		t.Fatalf("len %d valid %v err %v", len(got), utf8.ValidString(got), err)
	}
}

func TestHTML(t *testing.T) {
	r := fixture(t, map[string][]byte{"p.html": []byte(`<!doctype html><html><head><title>T</title><style>p{}</style>
<script>var secret = 1</script></head><body><p>Hello&nbsp;<b>world</b> &amp; <i>more</i><br>next<p>para &lt;x&gt; <unclosed></body></html>`)})
	got, err := (&Extractor{}).Extract(context.Background(), r, "p.html")
	if want := "Hello world & more next para <x>"; err != nil || got != want {
		t.Fatalf("%q %v, want %q", got, err, want)
	}
}

func TestEPUB(t *testing.T) {
	book := epubFile(t, [][2]string{
		{"mimetype", "application/epub+zip"},
		{"META-INF/container.xml", containerXML},
		{"OEBPS/content.opf", opfXML},
		{"OEBPS/Text/ch 1.xhtml", `<html xmlns="http://www.w3.org/1999/xhtml"><head><title>c1</title></head><body><p>第二章的内容</p></body></html>`},
		{"OEBPS/Text/ch2.xhtml", `<html><body><h1>Chapter One</h1><p>It was a dark &amp; stormy night.</p></body></html>`},
	})
	noOPF := epubFile(t, [][2]string{{"a.html", "<p>fallback text</p>"}, {"b.css", "p{}"}})
	var bomb bytes.Buffer
	z := zip.NewWriter(&bomb)
	w, _ := z.Create("OEBPS/zeros.xhtml")
	w.Write(bytes.Repeat([]byte(" "), 32<<20))
	w, _ = z.Create("OEBPS/ok.xhtml")
	w.Write([]byte("<p>small</p>"))
	z.Close()
	many := epubFile(t, func() [][2]string {
		var e [][2]string
		for i := range maxEntries + 1 {
			e = append(e, [2]string{fmt.Sprintf("%d.html", i), "x"})
		}
		return e
	}())
	r := fixture(t, map[string][]byte{"b.epub": book, "n.epub": noOPF, "bomb.epub": bomb.Bytes(), "many.epub": many, "bad.epub": []byte("not a zip")})
	x := &Extractor{}
	for name, want := range map[string]string{
		"b.epub":    "Chapter One It was a dark & stormy night. 第二章的内容",
		"n.epub":    "fallback text",
		"bomb.epub": "small",
	} {
		got, err := x.Extract(context.Background(), r, name)
		if err != nil || got != want {
			t.Errorf("%s: %q %v, want %q", name, got, err, want)
		}
	}
	for _, name := range []string{"many.epub", "bad.epub"} {
		if _, err := x.Extract(context.Background(), r, name); !errors.Is(err, ErrSkipped) {
			t.Errorf("%s: %v, want skipped", name, err)
		}
	}
}

// A one-page PDF with the text "Hello filebox pdf" in Helvetica; pdftotext rebuilds the missing xref.
const onePage = `%PDF-1.4
1 0 obj << /Type /Catalog /Pages 2 0 R >> endobj
2 0 obj << /Type /Pages /Kids [3 0 R] /Count 1 >> endobj
3 0 obj << /Type /Page /Parent 2 0 R /MediaBox [0 0 300 100] /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> >> endobj
4 0 obj << /Length 52 >> stream
BT /F1 18 Tf 10 40 Td (Hello filebox pdf) Tj ET
endstream endobj
5 0 obj << /Type /Font /Subtype /Type1 /BaseFont /Helvetica >> endobj
trailer << /Root 1 0 R >>
%%EOF
`

func TestPDF(t *testing.T) {
	x := New(context.Background())
	if x.pdftotext == "" {
		t.Skip("no pdftotext")
	}
	r := fixture(t, map[string][]byte{"a.pdf": []byte(onePage), "broken.pdf": []byte("%PDF-1.4 garbage")})
	got, err := x.Extract(context.Background(), r, "a.pdf")
	if err != nil || got != "Hello filebox pdf" {
		t.Fatalf("%q %v", got, err)
	}
	if got, err := x.Extract(context.Background(), r, "broken.pdf"); err == nil && got != "" {
		t.Fatalf("broken pdf gave %q", got)
	}
}

func TestPDFTimeoutAndCap(t *testing.T) {
	dir := t.TempDir()
	slow := filepath.Join(dir, "slow")
	os.WriteFile(slow, []byte("#!/bin/sh\nexec sleep 30\n"), 0o755)
	flood := filepath.Join(dir, "flood")
	os.WriteFile(flood, []byte("#!/bin/sh\nexec yes 'lorem ipsum'\n"), 0o755)
	if _, err := exec.LookPath("yes"); err != nil {
		t.Skip("no yes")
	}
	r := fixture(t, map[string][]byte{"a.pdf": []byte(onePage)})
	old := Timeout
	Timeout = 300 * time.Millisecond
	t.Cleanup(func() { Timeout = old })
	start := time.Now()
	if _, err := (&Extractor{pdftotext: slow}).Extract(context.Background(), r, "a.pdf"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("slow: %v", err)
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Fatalf("timeout took %v", d)
	}
	Timeout = 10 * time.Second
	got, err := (&Extractor{pdftotext: flood}).Extract(context.Background(), r, "a.pdf")
	if err != nil || len(got) > MaxText || len(got) < MaxText-16 || !strings.HasPrefix(got, "lorem ipsum lorem") {
		t.Fatalf("flood: len %d err %v", len(got), err)
	}
}
