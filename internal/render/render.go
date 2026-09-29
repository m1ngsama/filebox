package render

import (
	"bufio"
	"bytes"
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"
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
	p.AllowAttrs("target").Matching(regexp.MustCompile(`^_blank$`)).OnElements("a")
	return p
}()

func IsMarkdown(name string) bool {
	switch strings.ToLower(path.Ext(name)) {
	case ".md", ".markdown":
		return true
	}
	return false
}

var renders = make(chan struct{}, runtime.NumCPU())

type result struct {
	html      []byte
	truncated bool
	plain     string
}

const workerArg = "render-worker"

var (
	timeout = 5 * time.Second
	self, _ = os.Executable()
	nice, _ = exec.LookPath("nice")
)

var (
	mu       sync.Mutex
	cache    = map[string]result{}
	inflight = map[string]chan struct{}{}
	cached   int
)

const maxCache = 256 << 20

var maxOutput = 16 << 20

func Serve(w http.ResponseWriter, r *http.Request, root *os.Root, rel, rawPrefix string) {
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
	sum := sha256.Sum256(fmt.Appendf(nil, "%s\x00%s\x00%s\x00%d\x00%d", version, rawPrefix, rel, st.Size(), st.ModTime().UnixNano()))
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
	res, err := once(r.Context(), key, func() (result, error) { return slot(r.Context(), f, rel, rawPrefix) })
	if err != nil {
		if r.Context().Err() == nil {
			httpx.Error(w, err)
		}
		return
	}
	h.Set("Content-Type", "text/html; charset=utf-8")
	if res.truncated {
		h.Set("X-Truncated", "1")
	}
	if res.plain != "" {
		h.Set("X-Plain", res.plain)
	}
	w.Write(res.html)
}

func once(ctx context.Context, key string, run func() (result, error)) (result, error) {
	for {
		mu.Lock()
		if res, ok := cache[key]; ok {
			mu.Unlock()
			return res, nil
		}
		if ch, ok := inflight[key]; ok {
			mu.Unlock()
			select {
			case <-ch:
				continue
			case <-ctx.Done():
				return result{}, ctx.Err()
			}
		}
		ch := make(chan struct{})
		inflight[key] = ch
		mu.Unlock()
		res, err := run()
		mu.Lock()
		delete(inflight, key)
		close(ch)
		if err == nil {
			if cached+len(res.html) > maxCache {
				clear(cache)
				cached = 0
			}
			cache[key] = res
			cached += len(res.html)
		}
		mu.Unlock()
		return res, err
	}
}

func slot(ctx context.Context, f io.Reader, rel, rawPrefix string) (result, error) {
	select {
	case renders <- struct{}{}:
	case <-ctx.Done():
		return result{}, ctx.Err()
	}
	defer func() { <-renders }()
	src, err := io.ReadAll(io.LimitReader(f, Limit+1))
	if err != nil {
		return result{}, err
	}
	wctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	name, args := self, []string{workerArg, rel, rawPrefix}
	if nice != "" {
		name, args = nice, append([]string{"-n", "10", self}, args...)
	}
	cmd := exec.CommandContext(wctx, name, args...)
	cmd.Stdin = bytes.NewReader(src)
	cmd.WaitDelay = time.Second
	out, err := cmd.Output()
	if ctx.Err() != nil {
		return result{}, ctx.Err()
	}
	head, body, ok := bytes.Cut(out, []byte("\n"))
	if err != nil || !ok {
		return plain(src, "complex"), nil
	}
	t, reason, _ := strings.Cut(string(head), " ")
	return result{body, t == "1", strings.TrimPrefix(reason, "-")}, nil
}

// RunWorker must run before anything else in main and in TestMain of packages that render.
func RunWorker() {
	if len(os.Args) != 4 || os.Args[1] != workerArg {
		return
	}
	src, err := io.ReadAll(io.LimitReader(os.Stdin, Limit+1))
	if err != nil {
		os.Exit(1)
	}
	res := Render(path.Base(os.Args[2]), src, linker(path.Dir(os.Args[2]), os.Args[3]))
	t := "0"
	if res.truncated {
		t = "1"
	}
	w := bufio.NewWriter(os.Stdout)
	fmt.Fprintf(w, "%s %s\n", t, cmp.Or(res.plain, "-"))
	w.Write(res.html)
	if w.Flush() != nil {
		os.Exit(1)
	}
	os.Exit(0)
}

func plain(src []byte, reason string) result {
	truncated := len(src) > Limit
	if truncated {
		src = cut(src, Limit)
	}
	return result{policy.SanitizeBytes(fmt.Appendf(nil, "<pre>%s</pre>", html.EscapeString(string(src)))), truncated, reason}
}

func Render(name string, src []byte, link func(string) string) result {
	truncated := len(src) > Limit
	if truncated {
		src = cut(src, Limit)
	}
	var buf bytes.Buffer
	if IsMarkdown(name) {
		markdown(link).Convert(src, &buf)
	} else {
		highlight(&buf, lexers.Match(name), string(src))
	}
	if buf.Len() > maxOutput {
		r := plain(src, "large")
		r.truncated = truncated
		return r
	}
	return result{policy.SanitizeBytes(buf.Bytes()), truncated, ""}
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
		if u.Fragment != "" {
			return raw + strings.Join(segs, "/") + "#" + url.PathEscape(u.Fragment)
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
			d := string(n.Destination)
			n.Destination = []byte(l.link(d))
			if u := string(n.Destination); u != d && strings.HasPrefix(u, "/") {
				n.SetAttributeString("target", []byte("_blank"))
			}
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
