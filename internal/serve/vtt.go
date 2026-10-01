package serve

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"regexp"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/traditionalchinese"
	xunicode "golang.org/x/text/encoding/unicode"

	"github.com/m1ngsama/filebox/internal/httpx"
	"github.com/m1ngsama/filebox/internal/vol"
)

const maxSubtitle = 8 << 20

var srtTime = regexp.MustCompile(`(?m)^(\s*\d+:\d{2}:\d{2}),(\d{1,3})\s*-->\s*(\d+:\d{2}:\d{2}),(\d{1,3})`)

func UTF16(b []byte) bool {
	return bytes.HasPrefix(b, []byte("\xff\xfe")) || bytes.HasPrefix(b, []byte("\xfe\xff"))
}

func UTF8(src []byte) []byte {
	if UTF16(src) {
		if b, err := xunicode.UTF16(xunicode.LittleEndian, xunicode.ExpectBOM).NewDecoder().Bytes(src); err == nil {
			return b
		}
	}
	src = bytes.TrimPrefix(src, []byte("\xef\xbb\xbf"))
	if utf8.Valid(src) {
		return src
	}
	bad, multi := 0, 0
	for p := src; len(p) > 0; {
		r, n := utf8.DecodeRune(p)
		if r == utf8.RuneError && n == 1 {
			bad++
		} else if n > 1 {
			multi++
		}
		p = p[n:]
	}
	if bad*100 <= len(src) || multi >= bad {
		return bytes.ToValidUTF8(src, nil)
	}
	for _, e := range []encoding.Encoding{simplifiedchinese.GB18030, traditionalchinese.Big5} {
		if b, err := e.NewDecoder().Bytes(src); err == nil {
			if n := bytes.Count(b, []byte("\ufffd")); n == 0 || n == 1 && bytes.HasSuffix(b, []byte("\ufffd")) {
				return b
			}
		}
	}
	b, _ := charmap.Windows1252.NewDecoder().Bytes(src)
	return b
}

func SRTToVTT(src []byte) []byte {
	src = UTF8(src)
	src = bytes.ReplaceAll(src, []byte("\r\n"), []byte("\n"))
	src = bytes.ReplaceAll(src, []byte("\r"), []byte("\n"))
	if bytes.HasPrefix(src, []byte("WEBVTT")) {
		return src
	}
	return append([]byte("WEBVTT\n\n"), srtTime.ReplaceAllFunc(src, func(m []byte) []byte {
		g := srtTime.FindSubmatch(m)
		ms := func(b []byte) string { return string(b) + strings.Repeat("0", 3-len(b)) }
		return fmt.Appendf(nil, "%s.%s --> %s.%s", g[1], ms(g[2]), g[3], ms(g[4]))
	})...)
}

func VTT(w http.ResponseWriter, r *http.Request, root *os.Root, rel string) {
	ext := strings.ToLower(path.Ext(rel))
	if ext != ".srt" && ext != ".vtt" {
		httpx.Fail(w, 400, "not a subtitle")
		return
	}
	f, err := vol.Open(root, rel)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || st.IsDir() {
		httpx.Fail(w, 400, "not a file")
		return
	}
	etag := fmt.Sprintf(`"v%x-%x"`, st.Size(), st.ModTime().UnixNano())
	h := w.Header()
	h.Set("ETag", etag)
	h.Set("Cache-Control", "private, no-cache")
	h.Set("Content-Type", "text/vtt; charset=utf-8")
	SafeHeaders(h, "text/vtt")
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	b, err := io.ReadAll(io.LimitReader(f, maxSubtitle))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	w.Write(SRTToVTT(b))
}
