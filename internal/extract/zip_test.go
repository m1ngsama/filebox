package extract

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"runtime"
	"slices"
	"testing"
)

func open(t *testing.T, b []byte) *zip.Reader {
	t.Helper()
	z, err := Zip(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		t.Fatal(err)
	}
	return z
}

func TestNatural(t *testing.T) {
	got := []string{"p10.jpg", "P2.jpg", "p1.jpg", "p01b.jpg", "a/p3.jpg", "卷2/01.png", "卷10/01.png", "p001.jpg"}
	slices.SortStableFunc(got, Natural)
	want := []string{"a/p3.jpg", "p001.jpg", "p1.jpg", "p01b.jpg", "P2.jpg", "p10.jpg", "卷2/01.png", "卷10/01.png"}
	if !slices.Equal(got, want) {
		t.Fatalf("got %q", got)
	}
}

func TestImages(t *testing.T) {
	z := open(t, epubFile(t, [][2]string{
		{"ch/10.jpg", "j"}, {"ch/9.JPG", "j"}, {"__MACOSX/ch/._1.jpg", "x"}, {"ch/.hidden.png", "x"},
		{"ch/cover.svg", "<svg/>"}, {"ch/notes.txt", "t"}, {"ch/1.webp", "w"}, {"ch/", ""},
	}))
	var names []string
	for _, e := range Images(z) {
		names = append(names, e.Name)
	}
	if want := []string{"ch/1.webp", "ch/9.JPG", "ch/10.jpg"}; !slices.Equal(names, want) {
		t.Fatalf("got %q", names)
	}
}

func TestCover(t *testing.T) {
	pkg := func(meta, items, spine string) string {
		return `<?xml version="1.0"?><package xmlns="http://www.idpf.org/2007/opf"><metadata>` + meta + `</metadata><manifest>` +
			items + `</manifest><spine>` + spine + `</spine></package>`
	}
	book := func(opf string, extra ...[2]string) *zip.Reader {
		return open(t, epubFile(t, append([][2]string{{"META-INF/container.xml", containerXML}, {"OEBPS/content.opf", opf},
			{"OEBPS/Images/c%.jpg", "c"}, {"OEBPS/Images/other.png", "o"}, {"OEBPS/Images/v.svg", "<svg/>"}}, extra...)))
	}
	for name, tc := range map[string]struct {
		z    *zip.Reader
		want string
	}{
		"epub3 property": {book(pkg(`<meta name="cover" content="o"/>`,
			`<item id="o" href="Images/other.png"/><item id="c" href="Images/c%25.jpg" properties="cover-image"/>`, "")), "OEBPS/Images/c%.jpg"},
		"epub2 meta": {book(pkg(`<meta name="cover" content="o"/>`, `<item id="o" href="Images/other.png"/>`, "")), "OEBPS/Images/other.png"},
		"first spine image": {book(pkg("", `<item id="t" href="Text/t.xhtml"/><item id="s" href="Text/s.xhtml"/>`, `<itemref idref="t"/><itemref idref="s"/>`),
			[2]string{"OEBPS/Text/t.xhtml", `<html><body><p>title</p></body></html>`},
			[2]string{"OEBPS/Text/s.xhtml", `<html><body><svg><image xlink:href="../Images/other.png"/></svg></body></html>`}), "OEBPS/Images/other.png"},
		"svg only":  {book(pkg("", `<item id="v" href="Images/v.svg" properties="cover-image"/>`, "")), ""},
		"traversal": {book(pkg("", `<item id="x" href="../../../etc/passwd.jpg" properties="cover-image"/>`, "")), ""},
		"no opf":    {open(t, epubFile(t, [][2]string{{"a.jpg", "a"}})), ""},
	} {
		got := ""
		if e := Cover(tc.z); e != nil {
			got = e.Name
		}
		if got != tc.want {
			t.Errorf("%s: %q, want %q", name, got, tc.want)
		}
	}
}

func TestOpenGuards(t *testing.T) {
	var b bytes.Buffer
	w := zip.NewWriter(&b)
	f, _ := w.Create("zeros.jpg")
	f.Write(bytes.Repeat([]byte{0}, 8<<20))
	f, _ = w.CreateHeader(&zip.FileHeader{Name: "big.jpg", Method: zip.Store})
	f.Write(bytes.Repeat([]byte("0123456789abcdef"), 1<<16))
	f, _ = w.Create("ok.jpg")
	f.Write([]byte("fine"))
	w.Close()
	z := open(t, b.Bytes())
	if _, err := Open(z.File[0], 64<<20); !errors.Is(err, ErrSkipped) {
		t.Fatalf("ratio bomb: %v", err)
	}
	if _, err := Open(z.File[1], 1<<20-1); !errors.Is(err, ErrSkipped) {
		t.Fatalf("over limit: %v", err)
	}
	r, err := Open(z.File[2], 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := io.ReadAll(r); string(got) != "fine" {
		t.Fatalf("got %q", got)
	}
	r.Close()
	for _, bomb := range [][]byte{crafted(65541, func(dir int) uint32 { return uint32(dir) }, 0), []byte("not a zip")} {
		if _, err := Zip(bytes.NewReader(bomb), int64(len(bomb))); !errors.Is(err, ErrSkipped) {
			t.Fatalf("bomb opened: %v", err)
		}
	}
}

// A directory whose records carry 64 KiB of extra field and comment each: zip.NewReader would hold all of it.
func TestDirectoryFields(t *testing.T) {
	var b bytes.Buffer
	h := make([]byte, 46)
	binary.LittleEndian.PutUint32(h, 0x02014b50)
	binary.LittleEndian.PutUint16(h[28:], 8)
	binary.LittleEndian.PutUint16(h[30:], 65535)
	binary.LittleEndian.PutUint16(h[32:], 65535)
	pad := make([]byte, 8+65535+65535)
	const n = 2000
	for range n {
		b.Write(h)
		b.Write(pad)
	}
	end := make([]byte, 22)
	binary.LittleEndian.PutUint32(end, 0x06054b50)
	binary.LittleEndian.PutUint16(end[8:], n)
	binary.LittleEndian.PutUint16(end[10:], n)
	binary.LittleEndian.PutUint32(end[12:], uint32(b.Len()))
	b.Write(end)
	book := b.Bytes()
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	if _, err := Zip(bytes.NewReader(book), int64(len(book))); !errors.Is(err, ErrSkipped) {
		t.Fatalf("opened a %d MiB directory: %v", len(book)>>20, err)
	}
	runtime.ReadMemStats(&after)
	if n := after.TotalAlloc - before.TotalAlloc; n > 1<<20 {
		t.Fatalf("allocated %d bytes before rejecting", n)
	}
}
