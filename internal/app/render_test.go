package app

import (
	"strings"
	"testing"
)

func TestRender(t *testing.T) {
	f := newTestApp(t)
	f.write(t, "docs/a.md", "# Hi\n\n![p](pic.png)\n\n<script>x</script>")
	f.write(t, "docs/pic.png", "png")
	f.write(t, "secret.md", "# secret")
	w := f.do("GET", "/api/render?vol=v&p=docs/a.md", nil)
	if b := w.Body.String(); w.Code != 200 || !strings.Contains(b, "<h1") || !strings.Contains(b, `src="/raw/v/docs/pic.png"`) || strings.Contains(b, "<script") {
		t.Fatalf("render %d %s", w.Code, b)
	}
	if c := w.Header().Get("Content-Security-Policy"); !strings.Contains(c, "sandbox") {
		t.Fatalf("csp %q", c)
	}
	if w := f.do("GET", "/api/render?vol=v&p=../x.md", nil); w.Code == 200 {
		t.Fatal("escaped volume")
	}
	if c, _, _ := anon(f, "GET", "/api/render?vol=v&p=docs/a.md", ""); c != 401 {
		t.Fatalf("anonymous render %d", c)
	}

	tok := mkShare(t, f, `{"vol":"v","path":"docs","mode":"read"}`)
	if c, b, _ := anon(f, "GET", "/s/"+tok+"/render?p=a.md", ""); c != 200 || !strings.Contains(b, `src="/s/`+tok+`/raw/pic.png"`) {
		t.Fatalf("share render %d %s", c, b)
	}
	for _, p := range []string{"../secret.md", "%2E%2E/secret.md", "/../secret.md"} {
		if c, b, _ := anon(f, "GET", "/s/"+tok+"/render?p="+p, ""); c == 200 || strings.Contains(b, "secret") {
			t.Fatalf("share render escaped with %s: %d %s", p, c, b)
		}
	}
	one := mkShare(t, f, `{"vol":"v","path":"docs/a.md","mode":"read"}`)
	if c, b, _ := anon(f, "GET", "/s/"+one+"/render", ""); c != 200 || strings.Contains(b, "pic.png\"") {
		t.Fatalf("file share render %d %s", c, b)
	}
	if c, _, _ := anon(f, "GET", "/s/"+one+"/render?p=../secret.md", ""); c == 200 {
		t.Fatal("file share rendered a sibling")
	}
	drop := mkShare(t, f, `{"vol":"v","path":"docs","mode":"drop"}`)
	if c, _, _ := anon(f, "GET", "/s/"+drop+"/render?p=a.md", ""); c != 403 {
		t.Fatalf("drop share render %d", c)
	}
}
