package render

import (
	"bytes"
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf8"
)

func md(t *testing.T, src string) string {
	t.Helper()
	return string(Render("a.md", []byte(src), linker("docs", "/raw/v/")).html)
}

func TestMarkdownGFM(t *testing.T) {
	out := md(t, "| a | b |\n|---|---|\n| 1 | 2 |\n\n- [x] done\n- [ ] todo\n\n~~old~~ https://example.com\n\n```go\nfunc main() {}\n```\n")
	for _, want := range []string{"<table>", "<td>1</td>", `type="checkbox"`, "checked", "<del>old</del>",
		`href="https://example.com"`, `<pre class="chroma">`, `<span class="kd">func</span>`} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in\n%s", want, out)
		}
	}
}

func TestMarkdownXSS(t *testing.T) {
	out := md(t, strings.Join([]string{
		`<script>alert(1)</script>`,
		`<img src=x onerror=alert(2)>`,
		`[click](javascript:alert(3))`,
		`[data](data:text/html;base64,PHNjcmlwdD5hbGVydCg0KTwvc2NyaXB0Pg==)`,
		`![pic](data:image/svg+xml;base64,PHN2ZyBvbmxvYWQ9YWxlcnQoNSk+)`,
		`<a href="https://x" onclick="alert(6)">x</a>`,
		`<iframe src="https://evil"></iframe>`,
		"```html\n<script>alert(7)</script>\n```",
	}, "\n\n"))
	for _, bad := range []string{"<script", "onerror", "javascript:", "data:", "onclick", "<iframe", "alert(1)", "alert(2)"} {
		if strings.Contains(strings.ToLower(out), bad) {
			t.Errorf("found %q in\n%s", bad, out)
		}
	}
	if !strings.Contains(out, `&lt;</span><span class="nt">script</span>`) {
		t.Errorf("fenced html should stay escaped text:\n%s", out)
	}
}

func TestRelativeLinksStayInsideTheRoot(t *testing.T) {
	cases := map[string]string{
		"![a](pic.png)":              `src="/raw/v/docs/pic.png"`,
		"![a](./img/a%20b.png)":      `src="/raw/v/docs/img/a%20b.png"`,
		"![a](../top.png)":           `src="/raw/v/top.png"`,
		"![a](/abs.png)":             `src="/raw/v/abs.png"`,
		"![a](/../../etc/passwd)":    `src="/raw/v/etc/passwd"`,
		"![a](https://x.test/a.png)": `src="https://x.test/a.png"`,
		"[n](other.md#top)":          `href="/raw/v/docs/other.md#top" target="_blank"`,
		"[n](#top)":                  `href="#top"`,
	}
	for src, want := range cases {
		if out := md(t, src); !strings.Contains(out, want) {
			t.Errorf("%s: want %s in %s", src, want, out)
		}
	}
	for _, src := range []string{"![a](../../passwd)", "![a](../../../x/../../y.png)"} {
		if out := md(t, src); strings.Contains(out, "src=") {
			t.Errorf("%s escaped the root: %s", src, out)
		}
	}
	if out := string(Render("a.md", []byte("![a](pic.png)"), linker(".", "")).html); strings.Contains(out, "pic.png\"") {
		t.Errorf("single-file shares must drop relative images: %s", out)
	}
}

func TestCodeFileHighlighted(t *testing.T) {
	out := string(Render("main.go", []byte("package main\n\n// hi <b>\n"), nil).html)
	if !strings.Contains(out, `<span class="kn">package</span>`) || !strings.Contains(out, "&lt;b&gt;") {
		t.Fatal(out)
	}
}

func TestTruncatedAndCached(t *testing.T) {
	dir := t.TempDir()
	big := strings.Repeat("line 你好\n", Limit/8)
	os.WriteFile(filepath.Join(dir, "big.txt"), []byte(big), 0o644)
	root, _ := os.OpenRoot(dir)
	defer root.Close()
	w := httptest.NewRecorder()
	Serve(w, httptest.NewRequest("GET", "/", nil), root, "big.txt", "/raw/v/")
	if w.Code != 200 || w.Header().Get("X-Truncated") != "1" || w.Header().Get("Content-Security-Policy") != CSP {
		t.Fatal(w.Code, w.Header())
	}
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("If-None-Match", w.Header().Get("ETag"))
	w2 := httptest.NewRecorder()
	Serve(w2, r, root, "big.txt", "/raw/v/")
	if w2.Code != 304 {
		t.Fatal(w2.Code)
	}
}

func TestCutKeepsInvalidBytesAndWholeRunes(t *testing.T) {
	gbk := bytes.Repeat([]byte{0xc4, 0xe3, 0xba, 0xc3}, Limit/4+10)
	start := time.Now()
	res := Render("a.txt", gbk, nil)
	if !res.truncated || time.Since(start) > 5*time.Second {
		t.Fatalf("truncated %v in %v", res.truncated, time.Since(start))
	}
	if got := cut(gbk, Limit); len(got) < Limit-utf8.UTFMax {
		t.Fatalf("non-UTF-8 prefix cut to %d bytes", len(got))
	}
	s := []byte(strings.Repeat("a", 10) + "\xff" + strings.Repeat("好", 10))
	if got := cut(s, 16); string(got) != string(s[:14]) {
		t.Fatalf("got %q", got)
	}
	if got := cut(s, 17); string(got) != string(s[:17]) {
		t.Fatalf("whole rune dropped: %q", got)
	}
}

func serve(t *testing.T, name, src string) (*httptest.ResponseRecorder, time.Duration) {
	t.Helper()
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, name), []byte(src), 0o644)
	root, _ := os.OpenRoot(dir)
	defer root.Close()
	w := httptest.NewRecorder()
	start := time.Now()
	Serve(w, httptest.NewRequest("GET", "/", nil), root, name, "/raw/v/")
	return w, time.Since(start)
}

func TestPathologicalMarkdownFallsBackToPlainText(t *testing.T) {
	var table strings.Builder
	table.WriteString(strings.Repeat("|a", 10000) + "\n" + strings.Repeat("|-", 10000) + "\n")
	for range 10000 {
		table.WriteString("|b\n")
	}
	for name, src := range map[string]string{
		"quotes":     strings.Repeat("> ", 300000) + "x",
		"lists":      strings.Repeat("- ", 300000) + "x",
		"links":      strings.Repeat("[a](", 50000) + "x",
		"wide table": table.String(),
		"emphasis":   strings.Repeat("*a_ ", 200000),
	} {
		w, took := serve(t, "a.md", src)
		t.Logf("%s: %d bytes, %v", name, len(src), took)
		if w.Header().Get("X-Plain") != "complex" || !strings.HasPrefix(w.Body.String(), "<pre>") || took > 10*time.Second {
			t.Errorf("%s: plain %q in %v", name, w.Header().Get("X-Plain"), took)
		}
	}
}

func TestOrdinaryMarkdownRenders(t *testing.T) {
	doc := "# Title\n\n" + strings.Repeat("-", 40) + "\n\nA long heading line here\n" + strings.Repeat("-", 38) + "\n\n" +
		strings.Repeat("*", 40) + "\n\n> a\n> > b\n\n- a\n  - b\n    - c\n\n" + strings.Repeat(" ", 140) + "```json\n{}\n```\n\n[x](y (z))\n"
	w, _ := serve(t, "a.md", doc)
	b := w.Body.String()
	if w.Header().Get("X-Plain") != "" || !strings.Contains(b, "<hr>") || !strings.Contains(b, "<h2>A long heading") || !strings.Contains(b, "<blockquote>") {
		t.Fatalf("plain %q:\n%s", w.Header().Get("X-Plain"), b)
	}
}

func TestRenderWaitsForASlotAndGivesUpWithTheClient(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.md"), []byte("# hi"), 0o644)
	root, _ := os.OpenRoot(dir)
	defer root.Close()
	for range cap(renders) {
		renders <- struct{}{}
	}
	defer func() {
		for range cap(renders) {
			<-renders
		}
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	w := httptest.NewRecorder()
	Serve(w, httptest.NewRequest("GET", "/", nil).WithContext(ctx), root, "a.md", "")
	if w.Body.Len() != 0 {
		t.Fatalf("rendered past a full semaphore: %s", w.Body)
	}
}

func TestHugeHighlightFallsBackToPlainText(t *testing.T) {
	defer func(n int) { maxOutput = n }(maxOutput)
	maxOutput = 64 << 10
	src := []byte(strings.Repeat("x := f(a, b) + 1 // c\n", 8<<10))
	res := Render("a.go", src, nil)
	if res.plain != "large" || !strings.HasPrefix(string(res.html), "<pre>") {
		t.Fatalf("plain %q, %d bytes", res.plain, len(res.html))
	}
}

func TestConcurrentRendersOfOneFileRunOnce(t *testing.T) {
	var runs atomic.Int32
	release := make(chan struct{})
	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			res, err := once(context.Background(), "same-key-"+t.Name(), func() (result, error) {
				runs.Add(1)
				<-release
				return result{html: []byte("x")}, nil
			})
			if err != nil || string(res.html) != "x" {
				t.Errorf("%v %q", err, res.html)
			}
		})
	}
	time.Sleep(100 * time.Millisecond)
	close(release)
	wg.Wait()
	if n := runs.Load(); n != 1 {
		t.Fatalf("rendered %d times", n)
	}
}

func TestBackstopFallbackIsNotCached(t *testing.T) {
	if backstop <= 5*time.Second {
		t.Skip("no CPU limit on this platform")
	}
	defer func(d time.Duration) { timeout = d }(timeout)
	timeout = time.Nanosecond
	w, _ := serve(t, "a.md", "# hi")
	if w.Header().Get("X-Plain") != "complex" {
		t.Fatalf("plain %q", w.Header().Get("X-Plain"))
	}
	timeout = backstop
	if w, _ := serve(t, "a.md", "# hi"); w.Header().Get("X-Plain") != "" {
		t.Fatal("a backstop kill was cached")
	}
}

func TestWorkerFailureIsAnUncachedError(t *testing.T) {
	defer func(s, n string) { self, nice = s, n }(self, nice)
	for _, bad := range []string{filepath.Join(t.TempDir(), "missing"), "/usr/bin/true"} {
		self, nice = bad, ""
		if w, _ := serve(t, "a.md", "# hi"); w.Code != 500 {
			t.Fatalf("%s: %d %s", bad, w.Code, w.Body)
		}
	}
	self, _ = os.Executable()
	if w, _ := serve(t, "a.md", "# hi"); w.Code != 200 || !strings.Contains(w.Body.String(), "<h1") {
		t.Fatalf("failure was cached: %d %s", w.Code, w.Body)
	}
}
