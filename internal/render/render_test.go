package render

import (
	"bytes"
	"context"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
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
		"[n](other.md#top)":          `href="/raw/v/docs/other.md"`,
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

func TestDeepMarkdownFallsBackToPlainText(t *testing.T) {
	for name, src := range map[string]string{
		"quotes": strings.Repeat("> ", 300000) + "x",
		"lists":  strings.Repeat("- ", 30000) + "x",
		"links":  strings.Repeat("[a](", 50000) + "x",
	} {
		start := time.Now()
		res := Render("a.md", []byte(src), linker(".", "/raw/v/"))
		if !res.plain || !strings.HasPrefix(string(res.html), "<pre>") || time.Since(start) > 2*time.Second {
			t.Errorf("%s: plain %v in %v", name, res.plain, time.Since(start))
		}
	}
	nested := "> a\n> > b\n\n- a\n  - b\n    - c\n\n[x](y (z))\n"
	if res := Render("a.md", []byte(nested), linker(".", "/raw/v/")); res.plain {
		t.Fatal("ordinary nesting fell back to plain text")
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
