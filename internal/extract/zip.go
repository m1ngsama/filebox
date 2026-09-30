package extract

import (
	"archive/zip"
	"bufio"
	"cmp"
	"encoding/binary"
	"encoding/xml"
	"io"
	"math"
	"net/url"
	"path"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/net/html"
)

// Zip opens an archive only after its central directory passes the entry-count guard.
func Zip(f io.ReaderAt, size int64) (*zip.Reader, error) {
	if n := entries(f, size); n < 0 || n > maxEntries {
		return nil, ErrSkipped
	}
	z, err := zip.NewReader(f, size)
	if err != nil || len(z.File) > maxEntries {
		return nil, ErrSkipped
	}
	return z, nil
}

// Open inflates at most limit bytes of an entry whose directory sizes pass the ratio guard.
func Open(e *zip.File, limit int64) (io.ReadCloser, error) {
	if e == nil || e.FileInfo().IsDir() || limit <= 0 || e.UncompressedSize64 > uint64(limit) ||
		e.CompressedSize64 == 0 && e.UncompressedSize64 > 0 ||
		e.CompressedSize64 > 0 && e.UncompressedSize64/e.CompressedSize64 > maxRatio {
		return nil, ErrSkipped
	}
	r, err := e.Open()
	if err != nil {
		return nil, ErrSkipped
	}
	return struct {
		io.Reader
		io.Closer
	}{io.LimitReader(r, limit), r}, nil
}

var images = map[string]string{".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".png": "image/png", ".gif": "image/gif",
	".webp": "image/webp", ".avif": "image/avif", ".bmp": "image/bmp"}

// ImageType is the Content-Type of a raster image entry, or "" for anything else.
func ImageType(name string) string { return images[strings.ToLower(path.Ext(name))] }

func hidden(name string) bool {
	return strings.HasPrefix(name, "__MACOSX/") || strings.HasPrefix(path.Base(name), ".")
}

// Images lists the raster image entries in natural order.
func Images(z *zip.Reader) []*zip.File {
	var out []*zip.File
	for _, e := range z.File {
		if ImageType(e.Name) != "" && !hidden(e.Name) && !e.FileInfo().IsDir() {
			out = append(out, e)
		}
	}
	slices.SortStableFunc(out, func(a, b *zip.File) int { return Natural(a.Name, b.Name) })
	return out
}

// Natural orders names case-insensitively with digit runs compared by value.
func Natural(a, b string) int {
	x, y := a, b
	for x != "" && y != "" {
		dx, dy := digits(x), digits(y)
		if dx > 0 && dy > 0 {
			nx, ny := strings.TrimLeft(x[:dx], "0"), strings.TrimLeft(y[:dy], "0")
			if c := cmp.Or(cmp.Compare(len(nx), len(ny)), strings.Compare(nx, ny)); c != 0 {
				return c
			}
			x, y = x[dx:], y[dy:]
			continue
		}
		rx, sx := utf8.DecodeRuneInString(x)
		ry, sy := utf8.DecodeRuneInString(y)
		if c := cmp.Compare(unicode.ToLower(rx), unicode.ToLower(ry)); c != 0 {
			return c
		}
		x, y = x[sx:], y[sy:]
	}
	return cmp.Or(cmp.Compare(len(x), len(y)), strings.Compare(a, b))
}

func digits(s string) int {
	i := 0
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	return i
}

type container struct {
	Rootfiles []struct {
		Path string `xml:"full-path,attr"`
	} `xml:"rootfiles>rootfile"`
}

type opf struct {
	Metas []struct {
		Name    string `xml:"name,attr"`
		Content string `xml:"content,attr"`
	} `xml:"metadata>meta"`
	Items []struct {
		ID         string `xml:"id,attr"`
		Href       string `xml:"href,attr"`
		Properties string `xml:"properties,attr"`
	} `xml:"manifest>item"`
	Spine []struct {
		IDRef string `xml:"idref,attr"`
	} `xml:"spine>itemref"`
}

type book struct {
	files  map[string]*zip.File
	budget int64
	dir    string
	pkg    opf
}

func newBook(z *zip.Reader, budget int64) *book {
	b := &book{files: make(map[string]*zip.File, len(z.File)), budget: budget}
	for _, e := range z.File {
		b.files[e.Name] = e
	}
	var c container
	if r := b.read(b.files["META-INF/container.xml"]); r != nil {
		xml.NewDecoder(r).Decode(&c)
		r.Close()
	}
	if len(c.Rootfiles) > 0 {
		if r := b.read(b.files[c.Rootfiles[0].Path]); r != nil {
			d := xml.NewDecoder(r)
			d.Strict = false
			d.Decode(&b.pkg)
			r.Close()
			b.dir = path.Dir(c.Rootfiles[0].Path)
		}
	}
	return b
}

func (b *book) read(e *zip.File) io.ReadCloser {
	if b.budget <= 0 {
		return nil
	}
	r, err := Open(e, math.MaxInt64)
	if err != nil {
		return nil
	}
	return struct {
		io.Reader
		io.Closer
	}{&counted{r, &b.budget}, r}
}

func (b *book) href(dir, h string) string {
	h, _, _ = strings.Cut(h, "#")
	return path.Join(dir, unescape(h))
}

func (b *book) spine() []string {
	hrefs := map[string]string{}
	for _, it := range b.pkg.Items {
		hrefs[it.ID] = it.Href
	}
	var out []string
	for _, s := range b.pkg.Spine {
		if h, ok := hrefs[s.IDRef]; ok {
			out = append(out, b.href(b.dir, h))
		}
	}
	return out
}

func (b *book) image(name string) *zip.File {
	if e := b.files[name]; e != nil && ImageType(name) != "" {
		return e
	}
	return nil
}

// Cover finds an EPUB's cover image: the cover-image item, the cover meta, or the first image in the first spine documents.
func Cover(z *zip.Reader) *zip.File {
	b := newBook(z, 4<<20)
	cover := ""
	for _, m := range b.pkg.Metas {
		if m.Name == "cover" {
			cover = m.Content
		}
	}
	for _, it := range b.pkg.Items {
		if slices.Contains(strings.Fields(it.Properties), "cover-image") {
			if e := b.image(b.href(b.dir, it.Href)); e != nil {
				return e
			}
		}
	}
	for _, it := range b.pkg.Items {
		if it.ID == cover && cover != "" {
			if e := b.image(b.href(b.dir, it.Href)); e != nil {
				return e
			}
		}
	}
	for _, doc := range b.spine()[:min(3, len(b.spine()))] {
		if e := b.firstImage(doc); e != nil {
			return e
		}
	}
	return nil
}

func (b *book) firstImage(doc string) *zip.File {
	r := b.read(b.files[doc])
	if r == nil {
		return nil
	}
	defer r.Close()
	z := html.NewTokenizer(r)
	z.SetMaxBuf(markupBudget)
	for {
		switch z.Next() {
		case html.ErrorToken:
			return nil
		case html.StartTagToken, html.SelfClosingTagToken:
			name, more := z.TagName()
			if (string(name) != "img" && string(name) != "image") || !more {
				continue
			}
			for more {
				var k, v []byte
				k, v, more = z.TagAttr()
				if key := string(k); key == "src" || key == "href" || key == "xlink:href" {
					return b.image(b.href(path.Dir(doc), string(v)))
				}
			}
		}
	}
}

// zip.NewReader loads every central directory header whatever the end record claims, starting at either offset it may pick, so count from both.
func entries(f io.ReaderAt, size int64) int {
	tail := make([]byte, min(size, 22+65535))
	if _, err := f.ReadAt(tail, size-int64(len(tail))); err != nil {
		return -1
	}
	i := len(tail) - 22
	for ; i >= 0; i-- {
		if string(tail[i:i+4]) == "PK\x05\x06" && i+22+int(binary.LittleEndian.Uint16(tail[i+20:])) <= len(tail) {
			break
		}
	}
	if i < 0 {
		return -1
	}
	le := binary.LittleEndian
	if le.Uint16(tail[i+10:]) == 0xffff || le.Uint32(tail[i+12:]) == 0xffffffff || le.Uint32(tail[i+16:]) == 0xffffffff {
		return -1
	}
	records, dirSize, dirOff := int64(le.Uint16(tail[i+10:])), int64(le.Uint32(tail[i+12:])), int64(le.Uint32(tail[i+16:]))
	if records*46 > size || records*46 > dirSize || dirSize > maxDirectory {
		return -1
	}
	eocd := size - int64(len(tail)) + int64(i)
	n := 0
	for _, start := range []int64{eocd - dirSize, dirOff} {
		if start < 0 || start >= size {
			continue
		}
		n = max(n, headers(f, start, size))
	}
	return n
}

// zip.NewReader keeps every name, extra field and comment in memory, so their total is capped too.
func headers(f io.ReaderAt, start, size int64) int {
	le := binary.LittleEndian
	r := bufio.NewReader(io.NewSectionReader(f, start, size-start))
	var h [46]byte
	n, held := 0, int64(0)
	for ; n <= maxEntries; n++ {
		if _, err := io.ReadFull(r, h[:]); err != nil || le.Uint32(h[:]) != 0x02014b50 {
			return n
		}
		fields := int64(le.Uint16(h[28:])) + int64(le.Uint16(h[30:])) + int64(le.Uint16(h[32:]))
		if held += fields + 46; held > maxDirectory {
			return maxEntries + 1
		}
		if _, err := r.Discard(int(fields)); err != nil {
			return n
		}
	}
	return n
}

func unescape(h string) string {
	if u, err := url.PathUnescape(h); err == nil {
		return u
	}
	return h
}

type counted struct {
	r io.Reader
	n *int64
}

func (c *counted) Read(p []byte) (int, error) {
	if *c.n <= 0 {
		return 0, io.EOF
	}
	if int64(len(p)) > *c.n {
		p = p[:*c.n]
	}
	n, err := c.r.Read(p)
	*c.n -= int64(n)
	return n, err
}
