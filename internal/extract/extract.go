package extract

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/binary"
	"encoding/xml"
	"errors"
	"io"
	"log/slog"
	"net/url"
	"os"
	"os/exec"
	"path"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/m1ngsama/filebox/internal/serve"
)

const (
	MaxText      = 1 << 20
	MaxDocument  = 512 << 20
	maxPages     = 2000
	maxEntries   = 10000
	maxRatio     = 100
	maxUnpacked  = 64 << 20
	markupBudget = 16 << 20
)

var (
	Timeout      = 2 * time.Minute
	ErrSkipped   = errors.New("extract: not indexable")
	probeTimeout = 10 * time.Second
)

var plain = map[string]bool{}

func init() {
	for _, e := range strings.Fields(`txt text md markdown mdown rst org adoc asciidoc tex bib csv tsv log
		json jsonl yaml yml toml ini cfg conf properties xml svg srt vtt ass lrc
		go py rb rs c h cc cpp cxx hpp hh m mm java kt kts scala groovy gradle swift cs fs vb dart
		js mjs cjs ts mts cts jsx tsx vue svelte astro css scss sass less
		sh bash zsh fish ps1 bat cmd lua pl pm php r jl ex exs erl hrl hs ml mli clj cljs el lisp scm
		sql graphql proto nix zig v d nim tf hcl mk cmake`) {
		plain["."+e] = true
	}
	plain["makefile"], plain["dockerfile"] = true, true
}

type Extractor struct {
	nice, pdftotext string
}

func New(ctx context.Context) *Extractor {
	x := &Extractor{}
	x.nice, _ = exec.LookPath("nice")
	ctx, cancel := context.WithTimeout(ctx, probeTimeout)
	defer cancel()
	if p, err := exec.LookPath("pdftotext"); err == nil && exec.CommandContext(ctx, p, "-v").Run() == nil {
		x.pdftotext = p
		slog.Info("content index: pdf", "tool", p)
	} else {
		slog.Info("content index: no pdftotext, pdf content off")
	}
	return x
}

func kind(name string) string {
	ext := strings.ToLower(path.Ext(name))
	if ext == "" {
		ext = strings.ToLower(path.Base(name))
	}
	switch {
	case ext == ".pdf":
		return "pdf"
	case ext == ".epub":
		return "epub"
	case ext == ".html" || ext == ".htm" || ext == ".xhtml":
		return "html"
	case plain[ext]:
		return "text"
	}
	return ""
}

func (x *Extractor) Handles(name string) bool {
	k := kind(name)
	return k != "" && (k != "pdf" || x.pdftotext != "")
}

func (x *Extractor) Kinds() []string {
	out := []string{".epub", ".html", ".htm", ".xhtml"}
	if x.pdftotext != "" {
		out = append(out, ".pdf")
	}
	for e := range plain {
		out = append(out, e)
	}
	return out
}

func (x *Extractor) Extract(ctx context.Context, root *os.Root, rel string) (string, error) {
	k := kind(rel)
	if k == "" || (k == "pdf" && x.pdftotext == "") {
		return "", ErrSkipped
	}
	// O_NONBLOCK keeps a FIFO swapped in since the scan from hanging the open.
	f, err := root.OpenFile(rel, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return "", err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return "", err
	}
	if !st.Mode().IsRegular() {
		return "", ErrSkipped
	}
	var w text
	switch k {
	case "text":
		b, err := io.ReadAll(io.LimitReader(f, MaxText))
		if err != nil {
			return "", err
		}
		if bytes.IndexByte(b[:min(len(b), 8192)], 0) >= 0 {
			return "", ErrSkipped
		}
		if len(b) == MaxText {
			b = wholeRunes(b)
		}
		w.write(serve.UTF8(b))
	case "html":
		err = markup(&w, io.LimitReader(f, markupBudget))
	case "epub":
		if st.Size() > MaxDocument {
			return "", ErrSkipped
		}
		err = epub(&w, f, st.Size())
	case "pdf":
		if st.Size() > MaxDocument {
			return "", ErrSkipped
		}
		err = x.pdf(ctx, &w, f)
	}
	if err != nil && !errors.Is(err, errFull) {
		return "", err
	}
	return w.String(), nil
}

func wholeRunes(b []byte) []byte {
	for i := len(b) - 1; i >= 0 && i >= len(b)-utf8.UTFMax; i-- {
		if utf8.RuneStart(b[i]) {
			if !utf8.FullRune(b[i:]) {
				return b[:i]
			}
			break
		}
	}
	return b
}

var errFull = errors.New("extract: text cap reached")

// Control bytes 0x02 and 0x03 delimit search highlights, so they must never reach the index.
type text struct {
	b       strings.Builder
	space   bool
	newline int
	last    rune
	pend    []byte
	full    bool
}

func (t *text) write(p []byte) error {
	if t.full {
		return errFull
	}
	for len(p) > 0 {
		r, n := utf8.DecodeRune(p)
		p = p[n:]
		if unicode.IsSpace(r) || unicode.IsControl(r) || r == utf8.RuneError {
			t.space = t.b.Len() > 0
			if r == '\n' {
				t.newline++
			}
			continue
		}
		if t.space && !(t.newline == 1 && unicode.Is(unicode.Han, t.last) && unicode.Is(unicode.Han, r)) {
			if t.b.Len()+1 > MaxText {
				t.full = true
				return errFull
			}
			t.b.WriteByte(' ')
		}
		t.space, t.newline = false, 0
		if t.b.Len()+utf8.RuneLen(r) > MaxText {
			t.full = true
			return errFull
		}
		t.b.WriteRune(r)
		t.last = r
	}
	return nil
}

func (t *text) Write(p []byte) (int, error) {
	b := append(t.pend, p...)
	whole := wholeRunes(b)
	t.pend = append([]byte(nil), b[len(whole):]...)
	if err := t.write(whole); err != nil {
		return 0, err
	}
	return len(p), nil
}

func (t *text) brk() {
	if t.b.Len() > 0 {
		t.space, t.newline = true, 2
	}
}

func (t *text) String() string { return t.b.String() }

func (x *Extractor) pdf(ctx context.Context, w *text, f *os.File) error {
	ctx, cancel := context.WithTimeout(ctx, Timeout)
	defer cancel()
	args := []string{"-q", "-enc", "UTF-8", "-l", strconv.Itoa(maxPages), "/dev/fd/3", "-"}
	var cmd *exec.Cmd
	if x.nice != "" {
		cmd = exec.CommandContext(ctx, x.nice, append([]string{"-n", "19", x.pdftotext}, args...)...)
	} else {
		cmd = exec.CommandContext(ctx, x.pdftotext, args...)
	}
	cmd.ExtraFiles = []*os.File{f}
	cmd.Stdout = w
	cmd.WaitDelay = time.Second
	err := cmd.Run()
	switch {
	case w.full:
		return errFull
	case ctx.Err() != nil:
		return ctx.Err()
	}
	return err
}

var skipped = map[string]bool{"script": true, "style": true, "head": true, "template": true, "noscript": true}

var blocks = map[string]bool{"p": true, "div": true, "br": true, "li": true, "tr": true, "td": true, "th": true,
	"h1": true, "h2": true, "h3": true, "h4": true, "h5": true, "h6": true, "section": true, "article": true,
	"blockquote": true, "pre": true, "dt": true, "dd": true, "hr": true, "title": true, "figcaption": true}

func markup(w *text, r io.Reader) error {
	d := xml.NewDecoder(r)
	d.Strict = false
	d.AutoClose = xml.HTMLAutoClose
	d.Entity = xml.HTMLEntity
	d.CharsetReader = func(_ string, in io.Reader) (io.Reader, error) { return in, nil }
	hidden := 0
	for {
		tok, err := d.RawToken()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return nil
		}
		switch t := tok.(type) {
		case xml.StartElement:
			name := strings.ToLower(t.Name.Local)
			if skipped[name] {
				hidden++
			}
			if blocks[name] {
				w.brk()
			}
		case xml.EndElement:
			name := strings.ToLower(t.Name.Local)
			if skipped[name] && hidden > 0 {
				hidden--
			}
			if blocks[name] {
				w.brk()
			}
		case xml.CharData:
			if hidden == 0 {
				if err := w.write(t); err != nil {
					return err
				}
			}
		}
	}
}

type container struct {
	Rootfiles []struct {
		Path string `xml:"full-path,attr"`
	} `xml:"rootfiles>rootfile"`
}

type opf struct {
	Items []struct {
		ID   string `xml:"id,attr"`
		Href string `xml:"href,attr"`
	} `xml:"manifest>item"`
	Spine []struct {
		IDRef string `xml:"idref,attr"`
	} `xml:"spine>itemref"`
}

// zip.NewReader loads the whole central directory, so its entry count is checked first.
func entries(f io.ReaderAt, size int64) int {
	tail := make([]byte, min(size, 22+65535))
	if _, err := f.ReadAt(tail, size-int64(len(tail))); err != nil {
		return -1
	}
	i := bytes.LastIndex(tail, []byte("PK\x05\x06"))
	if i < 0 || len(tail)-i < 22 {
		return -1
	}
	return int(binary.LittleEndian.Uint16(tail[i+10:]))
}

func epub(w *text, f io.ReaderAt, size int64) error {
	if n := entries(f, size); n < 0 || n >= 0xffff || n > maxEntries {
		return ErrSkipped
	}
	z, err := zip.NewReader(f, size)
	if err != nil {
		return ErrSkipped
	}
	if len(z.File) > maxEntries {
		return ErrSkipped
	}
	files := make(map[string]*zip.File, len(z.File))
	for _, e := range z.File {
		files[e.Name] = e
	}
	budget := int64(maxUnpacked)
	read := func(e *zip.File) io.ReadCloser {
		if e == nil || e.CompressedSize64 == 0 && e.UncompressedSize64 > 0 ||
			e.CompressedSize64 > 0 && e.UncompressedSize64/e.CompressedSize64 > maxRatio || budget <= 0 {
			return nil
		}
		r, err := e.Open()
		if err != nil {
			return nil
		}
		return struct {
			io.Reader
			io.Closer
		}{&counted{r, &budget}, r}
	}
	var order []*zip.File
	var c container
	if r := read(files["META-INF/container.xml"]); r != nil {
		xml.NewDecoder(r).Decode(&c)
		r.Close()
	}
	if len(c.Rootfiles) > 0 {
		if r := read(files[c.Rootfiles[0].Path]); r != nil {
			var o opf
			d := xml.NewDecoder(r)
			d.Strict = false
			d.Decode(&o)
			r.Close()
			dir := path.Dir(c.Rootfiles[0].Path)
			hrefs := map[string]string{}
			for _, it := range o.Items {
				hrefs[it.ID] = it.Href
			}
			for _, s := range o.Spine {
				if h, ok := hrefs[s.IDRef]; ok {
					h, _, _ = strings.Cut(h, "#")
					if e := files[path.Join(dir, unescape(h))]; e != nil {
						order = append(order, e)
					}
				}
			}
		}
	}
	if len(order) == 0 {
		for _, e := range z.File {
			if kind(e.Name) == "html" {
				order = append(order, e)
			}
		}
	}
	for _, e := range order {
		r := read(e)
		if r == nil {
			continue
		}
		err := markup(w, r)
		r.Close()
		if err != nil {
			return err
		}
		w.brk()
	}
	return nil
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
