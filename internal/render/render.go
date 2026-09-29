package render

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"regexp"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/alecthomas/chroma/v2"
	chtml "github.com/alecthomas/chroma/v2/formatters/html"
	"github.com/alecthomas/chroma/v2/lexers"
	"github.com/alecthomas/chroma/v2/styles"
	"github.com/microcosm-cc/bluemonday"
	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	"github.com/yuin/goldmark/parser"
	"github.com/yuin/goldmark/renderer"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"

	"github.com/m1ngsama/filebox/internal/httpx"
)

const Limit = 1 << 20

const version = "1"

const CSP = "default-src 'none'; img-src 'self'; style-src 'self'; sandbox"

var formatter = chtml.New(chtml.WithClasses(true))

var policy = func() *bluemonday.Policy {
	p := bluemonday.UGCPolicy()
	p.AllowAttrs("class").Matching(regexp.MustCompile(`^[a-z0-9 -]{1,64}$`)).OnElements("span", "pre", "code")
	p.AllowElements("input")
	p.AllowAttrs("type").Matching(regexp.MustCompile(`^checkbox$`)).OnElements("input")
	p.AllowAttrs("checked", "disabled").OnElements("input")
	p.AddTargetBlankToFullyQualifiedLinks(true)
	return p
}()

func IsMarkdown(name string) bool {
	switch strings.ToLower(path.Ext(name)) {
	case ".md", ".markdown", ".mdown", ".mkd":
		return true
	}
	return false
}

const maxDepth = 32

var renders = make(chan struct{}, 2)

type result struct {
	html      []byte
	truncated bool
	plain     bool
}

var (
	mu     sync.Mutex
	cache  = map[string]result{}
	cached int
)

const (
	maxCache  = 32 << 20
	maxOutput = 4 << 20
)

// raw is the URL prefix that serves files under the same root as rel; "" drops relative links.
func Serve(w http.ResponseWriter, r *http.Request, root *os.Root, rel, raw string) {
	f, err := root.Open(rel)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		httpx.Error(w, err)
		return
	}
	if st.IsDir() {
		httpx.Fail(w, 400, "is a directory")
		return
	}
	sum := sha256.Sum256(fmt.Appendf(nil, "%s\x00%s\x00%s\x00%d\x00%d", version, raw, rel, st.Size(), st.ModTime().UnixNano()))
	key := hex.EncodeToString(sum[:16])
	etag := `"` + key + `"`
	h := w.Header()
	h.Set("ETag", etag)
	h.Set("Cache-Control", "private, no-cache")
	h.Set("Content-Security-Policy", CSP)
	h.Set("X-Content-Type-Options", "nosniff")
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	mu.Lock()
	res, ok := cache[key]
	mu.Unlock()
	if !ok {
		src, err := io.ReadAll(io.LimitReader(f, Limit+1))
		if err != nil {
			httpx.Error(w, err)
			return
		}
		select {
		case renders <- struct{}{}:
		case <-r.Context().Done():
			return
		}
		res = Render(path.Base(rel), src, linker(path.Dir(rel), raw))
		<-renders
		mu.Lock()
		if cached+len(res.html) > maxCache {
			clear(cache)
			cached = 0
		}
		if _, dup := cache[key]; !dup {
			cache[key] = res
			cached += len(res.html)
		}
		mu.Unlock()
	}
	h.Set("Content-Type", "text/html; charset=utf-8")
	if res.truncated {
		h.Set("X-Truncated", "1")
	}
	if res.plain {
		h.Set("X-Plain", "1")
	}
	w.Write(res.html)
}

func Render(name string, src []byte, link func(string) string) result {
	truncated := len(src) > Limit
	if truncated {
		src = cut(src, Limit)
	}
	var buf bytes.Buffer
	plain := false
	switch {
	case IsMarkdown(name) && tooDeep(src):
		plain = true
		fmt.Fprintf(&buf, "<pre>%s</pre>", html.EscapeString(string(src)))
	case IsMarkdown(name):
		markdown(link).Convert(src, &buf)
	default:
		highlight(&buf, lexers.Match(name), string(src))
	}
	if buf.Len() > maxOutput {
		plain = true
		buf.Reset()
		fmt.Fprintf(&buf, "<pre>%s</pre>", html.EscapeString(string(src)))
	}
	return result{policy.SanitizeBytes(buf.Bytes()), truncated, plain}
}

// goldmark is superlinear in block and bracket nesting and cannot be cancelled.
func tooDeep(src []byte) bool {
	parens := 0
	for line := range bytes.Lines(src) {
		level, indent := 0, 0
	prefix:
		for _, c := range line {
			switch c {
			case '>', '-', '*', '+', '.', ')':
				level++
			case ' ':
				indent++
			case '\t':
				indent += 4
			case '0', '1', '2', '3', '4', '5', '6', '7', '8', '9':
			default:
				break prefix
			}
		}
		if level+indent/4 > maxDepth {
			return true
		}
		if len(bytes.TrimSpace(line)) == 0 {
			parens = 0
		}
		for _, c := range line {
			switch c {
			case '(', '[':
				if parens++; parens > maxDepth {
					return true
				}
			case ')', ']':
				parens = max(0, parens-1)
			}
		}
	}
	return false
}

func cut(src []byte, n int) []byte {
	src = src[:n]
	if i := bytes.LastIndexByte(src, '\n'); i > 0 {
		return src[:i+1]
	}
	for i := len(src) - 1; i >= 0 && i >= len(src)-utf8.UTFMax; i-- {
		if utf8.RuneStart(src[i]) {
			if !utf8.FullRune(src[i:]) {
				return src[:i]
			}
			break
		}
	}
	return src
}

func highlight(w io.Writer, l chroma.Lexer, code string) {
	if l == nil {
		l = lexers.Fallback
	}
	it, err := chroma.Coalesce(l).Tokenise(nil, code)
	if err != nil {
		fmt.Fprintf(w, "<pre>%s</pre>", html.EscapeString(code))
		return
	}
	formatter.Format(w, styles.Fallback, it)
}

func linker(dir, raw string) func(string) string {
	return func(dest string) string {
		u, err := url.Parse(dest)
		if err != nil {
			return ""
		}
		if u.Scheme != "" || u.Host != "" || dest == "" || strings.HasPrefix(dest, "#") {
			return dest
		}
		if raw == "" {
			return ""
		}
		p := u.Path
		if strings.HasPrefix(p, "/") {
			p = path.Clean(p)[1:]
		} else {
			p = path.Join(dir, p)
		}
		if p == ".." || strings.HasPrefix(p, "../") {
			return ""
		}
		segs := strings.Split(p, "/")
		for i, s := range segs {
			segs[i] = url.PathEscape(s)
		}
		return raw + strings.Join(segs, "/")
	}
}

func markdown(link func(string) string) goldmark.Markdown {
	return goldmark.New(
		goldmark.WithExtensions(extension.GFM),
		goldmark.WithParserOptions(parser.WithASTTransformers(util.Prioritized(links{link}, 100))),
		goldmark.WithRendererOptions(renderer.WithNodeRenderers(util.Prioritized(fences{}, 100))),
	)
}

type links struct{ link func(string) string }

func (l links) Transform(doc *ast.Document, _ text.Reader, _ parser.Context) {
	ast.Walk(doc, func(n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		switch n := n.(type) {
		case *ast.Image:
			n.Destination = []byte(l.link(string(n.Destination)))
		case *ast.Link:
			n.Destination = []byte(l.link(string(n.Destination)))
		}
		return ast.WalkContinue, nil
	})
}

type fences struct{}

func (fences) RegisterFuncs(reg renderer.NodeRendererFuncRegisterer) {
	reg.Register(ast.KindFencedCodeBlock, func(w util.BufWriter, src []byte, n ast.Node, entering bool) (ast.WalkStatus, error) {
		if !entering {
			return ast.WalkContinue, nil
		}
		b := n.(*ast.FencedCodeBlock)
		var code strings.Builder
		for i := 0; i < b.Lines().Len(); i++ {
			seg := b.Lines().At(i)
			code.Write(seg.Value(src))
		}
		var l chroma.Lexer
		if lang := b.Language(src); lang != nil {
			l = lexers.Get(string(lang))
		}
		highlight(w, l, code.String())
		return ast.WalkSkipChildren, nil
	})
}
