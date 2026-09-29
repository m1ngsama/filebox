package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMetaStaysInShare(t *testing.T) {
	f := newTestApp(t)
	fake := filepath.Join(t.TempDir(), "ffprobe")
	os.WriteFile(fake, []byte("#!/bin/sh\nprintf '{\"frames\":[{\"width\":4,\"height\":3,\"tags\":{\"Model\":\"%s\"}}]}' \"$(cat <&3)\"\n"), 0o755)
	f.App.Thumbs.FFprobe = fake
	f.write(t, "pub/ok.jpg", "shared")
	f.write(t, "secret.jpg", "private")
	os.Symlink("../secret.jpg", filepath.Join(f.Dir, "pub/link.jpg"))
	if w := f.do("GET", "/api/meta?vol=v&p=secret.jpg", nil); w.Code != 200 || !strings.Contains(w.Body.String(), `"camera":"private"`) || !strings.Contains(w.Body.String(), `"width":4`) {
		t.Fatalf("owner meta %d %s", w.Code, w.Body)
	}
	if c, _, _ := anon(f, "GET", "/api/meta?vol=v&p=secret.jpg", ""); c != 401 {
		t.Fatalf("anonymous meta %d", c)
	}
	tok := mkShare(t, f, `{"vol":"v","path":"pub","mode":"read"}`)
	if c, b, _ := anon(f, "GET", "/s/"+tok+"/meta?p=ok.jpg", ""); c != 200 || !strings.Contains(b, `"camera":"shared"`) {
		t.Fatalf("share meta %d %s", c, b)
	}
	for _, p := range []string{"../secret.jpg", "link.jpg", "%2E%2E/secret.jpg"} {
		if c, b, _ := anon(f, "GET", "/s/"+tok+"/meta?p="+p, ""); c == 200 || strings.Contains(b, "private") {
			t.Fatalf("share meta escaped with %s: %d %s", p, c, b)
		}
	}
	one := mkShare(t, f, `{"vol":"v","path":"pub/ok.jpg","mode":"read"}`)
	if c, b, _ := anon(f, "GET", "/s/"+one+"/meta", ""); c != 200 || !strings.Contains(b, "shared") {
		t.Fatalf("file share meta %d %s", c, b)
	}
	if c, _, _ := anon(f, "GET", "/s/"+one+"/meta?p=../secret.jpg", ""); c == 200 {
		t.Fatal("file share meta of a sibling")
	}
	drop := mkShare(t, f, `{"vol":"v","path":"pub","mode":"drop"}`)
	if c, _, _ := anon(f, "GET", "/s/"+drop+"/meta?p=ok.jpg", ""); c != 403 {
		t.Fatalf("drop share meta %d", c)
	}
	pw := mkShare(t, f, `{"vol":"v","path":"pub","mode":"read","password":"open sesame"}`)
	if c, _, _ := anon(f, "GET", "/s/"+pw+"/meta?p=ok.jpg", ""); c != 401 {
		t.Fatalf("locked share meta %d", c)
	}
}
