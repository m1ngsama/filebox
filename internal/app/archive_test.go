package app

import (
	"archive/zip"
	"bytes"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func cbz(t *testing.T, stored bool, entries ...[2]string) string {
	t.Helper()
	var b bytes.Buffer
	z := zip.NewWriter(&b)
	for _, e := range entries {
		h := &zip.FileHeader{Name: e[0], Method: zip.Deflate}
		if stored {
			h.Method = zip.Store
		}
		w, err := z.CreateHeader(h)
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte(e[1]))
	}
	z.Close()
	return b.String()
}

type zipList struct {
	Entries []struct {
		Name string
		Size int64
	}
}

func names(l zipList) []string {
	var out []string
	for _, e := range l.Entries {
		out = append(out, e.Name)
	}
	return out
}

func TestZipEntries(t *testing.T) {
	f := newTestApp(t)
	f.write(t, "manga/vol 1.cbz", cbz(t, false,
		[2]string{"p10.jpg", "ten"}, [2]string{"p2.jpg", "two"}, [2]string{"../evil.jpg", "x"}, [2]string{"/abs.jpg", "x"},
		[2]string{"page.html", "<script>alert(1)</script>"}, [2]string{"art.svg", "<svg onload=alert(1)/>"}, [2]string{"__MACOSX/._p2.jpg", "x"}))
	f.write(t, "manga/big.cbz", cbz(t, true, [2]string{"huge.jpg", strings.Repeat("x", 64<<20+1)}))
	f.write(t, "manga/bomb.cbz", cbz(t, false, [2]string{"zeros.jpg", strings.Repeat("\x00", 16<<20)}))
	f.write(t, "manga/fake.cbz", "not a zip")
	f.write(t, "secret.cbz", cbz(t, false, [2]string{"s.jpg", "secret"}))
	q := func(p string, kv ...string) string {
		v := url.Values{"vol": {"v"}, "p": {p}}
		for i := 0; i+1 < len(kv); i += 2 {
			v.Set(kv[i], kv[i+1])
		}
		return v.Encode()
	}

	w := f.do("GET", "/api/zip-entries?"+q("manga/vol 1.cbz"), nil)
	if got := names(decode[zipList](t, w)); w.Code != 200 || !slices.Equal(got, []string{"p2.jpg", "p10.jpg"}) {
		t.Fatalf("images %d %v", w.Code, got)
	}
	w = f.do("GET", "/api/zip-entries?"+q("manga/vol 1.cbz", "all", ""), nil)
	if got := names(decode[zipList](t, w)); !slices.Equal(got, []string{"p10.jpg", "p2.jpg", "page.html", "art.svg", "__MACOSX/._p2.jpg"}) {
		t.Fatalf("all %v", got)
	}
	w = f.do("GET", "/api/zip-entry?"+q("manga/vol 1.cbz", "e", "p2.jpg"), nil)
	if w.Code != 200 || w.Body.String() != "two" || w.Header().Get("Content-Type") != "image/jpeg" || w.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatalf("entry %d %q %v", w.Code, w.Body, w.Header())
	}
	if etag := w.Header().Get("ETag"); f.do("GET", "/api/zip-entry?"+q("manga/vol 1.cbz", "e", "p2.jpg"), nil, "If-None-Match", etag).Code != 304 {
		t.Fatal("no 304 for a matching ETag")
	}
	for _, e := range []string{"page.html", "art.svg"} {
		w = f.do("GET", "/api/zip-entry?"+q("manga/vol 1.cbz", "e", e), nil)
		if ct, csp := w.Header().Get("Content-Type"), w.Header().Get("Content-Security-Policy"); w.Code != 200 || ct != "application/octet-stream" || !strings.Contains(csp, "sandbox") {
			t.Fatalf("%s served as %d %q %q", e, w.Code, ct, csp)
		}
	}
	for e, code := range map[string]int{"../evil.jpg": 400, "/abs.jpg": 400, "a/../p2.jpg": 400, "": 400, "missing.jpg": 404} {
		if w := f.do("GET", "/api/zip-entry?"+q("manga/vol 1.cbz", "e", e), nil); w.Code != code {
			t.Errorf("entry %q: %d, want %d", e, w.Code, code)
		}
	}
	for p, code := range map[string]int{"manga/big.cbz": 413, "manga/bomb.cbz": 413, "manga/fake.cbz": 415, "manga": 415, "../x.cbz": 404} {
		if w := f.do("GET", "/api/zip-entry?"+q(p, "e", "huge.jpg"), nil); p != "manga/bomb.cbz" && w.Code != code {
			t.Errorf("%s: %d, want %d", p, w.Code, code)
		}
	}
	if w := f.do("GET", "/api/zip-entry?"+q("manga/bomb.cbz", "e", "zeros.jpg"), nil); w.Code != 413 {
		t.Errorf("ratio bomb: %d", w.Code)
	}
	if w := f.do("GET", "/api/zip-entries?"+q("manga/vol 1.cbz"), nil, "X-No-Auth", "1"); w.Code != 401 {
		t.Fatalf("anonymous listing %d", w.Code)
	}

	tok := mkShare(t, f, `{"vol":"v","path":"manga","mode":"read"}`)
	if c, b, _ := anon(f, "GET", "/s/"+tok+"/zip-entries?p="+url.QueryEscape("vol 1.cbz"), ""); c != 200 || !strings.Contains(b, "p10.jpg") {
		t.Fatalf("share listing %d %s", c, b)
	}
	if c, b, _ := anon(f, "GET", "/s/"+tok+"/zip-entry?e=p10.jpg&p="+url.QueryEscape("vol 1.cbz"), ""); c != 200 || b != "ten" {
		t.Fatalf("share entry %d %q", c, b)
	}
	for _, p := range []string{"../secret.cbz", "%2E%2E/secret.cbz", "/secret.cbz"} {
		if c, b, _ := anon(f, "GET", "/s/"+tok+"/zip-entry?e=s.jpg&p="+p, ""); c == 200 || strings.Contains(b, "secret") {
			t.Fatalf("share escaped via %s: %d", p, c)
		}
	}
	os.Symlink("../secret.cbz", filepath.Join(f.Dir, "manga/link.cbz"))
	if c, _, _ := anon(f, "GET", "/s/"+tok+"/zip-entries?p=link.cbz", ""); c == 200 {
		t.Fatal("share listing followed a link out of the share")
	}
	file := mkShare(t, f, `{"vol":"v","path":"manga/vol 1.cbz","mode":"read"}`)
	if c, b, _ := anon(f, "GET", "/s/"+file+"/zip-entry?e=p2.jpg", ""); c != 200 || b != "two" {
		t.Fatalf("file share entry %d %q", c, b)
	}
	if c, _, _ := anon(f, "GET", "/s/"+file+"/zip-entries?p=other.cbz", ""); c == 200 {
		t.Fatal("file share listed another path")
	}
	drop := mkShare(t, f, `{"vol":"v","path":"manga","mode":"drop"}`)
	if c, _, _ := anon(f, "GET", "/s/"+drop+"/zip-entries?p="+url.QueryEscape("vol 1.cbz"), ""); c != 403 {
		t.Fatalf("drop share listing %d", c)
	}
}
