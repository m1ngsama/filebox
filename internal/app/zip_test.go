package app

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func zipNames(t *testing.T, b []byte) []string {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, f := range zr.File {
		out = append(out, f.Name)
	}
	slices.Sort(out)
	return out
}

func TestZipRoute(t *testing.T) {
	f := newTestApp(t)
	f.write(t, "pub/a.txt", "a")
	f.write(t, "pub/in/b.txt", "b")
	f.write(t, "secret.txt", "s")
	w := f.do("GET", "/api/zip?vol=v&p=pub/a.txt&p=pub/in&name=pub-2", nil)
	if w.Code != 200 || w.Header().Get("Content-Disposition") != "attachment; filename=pub-2.zip" {
		t.Fatalf("%d %v", w.Code, w.Header())
	}
	if got := zipNames(t, w.Body.Bytes()); !slices.Equal(got, []string{"a.txt", "in/", "in/b.txt"}) {
		t.Fatalf("got %v", got)
	}
	if w := f.do("GET", "/api/zip?vol=v&p=pub", nil, "X-No-Auth", "1"); w.Code != 401 {
		t.Fatalf("anon %d", w.Code)
	}
	if w := f.do("GET", "/api/zip?vol=v&p=pub", nil, "X-No-Auth", "1", "Authorization", "Bearer "+f.Bearer); w.Code != 200 {
		t.Fatalf("app token %d", w.Code)
	}
	for p, code := range map[string]int{"../../../etc": 404, ".filebox": 400, "": 400} {
		q := "/api/zip?vol=v"
		if p != "" {
			q += "&p=" + p
		}
		if w := f.do("GET", q, nil); w.Code != code {
			t.Errorf("%q = %d, want %d", p, w.Code, code)
		}
	}
	os.MkdirAll(filepath.Join(f.Dir, ".trash/t1"), 0o755)
	os.WriteFile(filepath.Join(f.Dir, ".trash/t1/gone.txt"), []byte("g"), 0o644)
	os.Symlink(".", filepath.Join(f.Dir, "all"))
	whole := mkShare(t, f, `{"vol":"v","path":"","mode":"read"}`)
	for _, u := range []string{"/api/zip?vol=v&p=all", "/s/" + whole + "/zip?p=all", "/s/" + whole + "/zip"} {
		w := f.do("GET", u, nil)
		got := zipNames(t, w.Body.Bytes())
		if w.Code != 200 || !slices.ContainsFunc(got, func(n string) bool { return strings.HasSuffix(n, "pub/a.txt") }) {
			t.Fatalf("%s: %d %v", u, w.Code, got)
		}
		for _, n := range got {
			if strings.Contains(n, ".trash") || strings.Contains(n, ".filebox") {
				t.Fatalf("%s exposes %s", u, n)
			}
		}
	}
	if w := f.do("GET", "/api/zip?vol=nope&p=pub", nil); w.Code != 404 {
		t.Fatalf("unknown volume %d", w.Code)
	}
}

func TestShareZip(t *testing.T) {
	f := newTestApp(t)
	f.write(t, "pub/a.txt", "a")
	f.write(t, "pub/in/b.txt", "b")
	f.write(t, "secret.txt", "s")
	tok := mkShare(t, f, `{"vol":"v","path":"pub","mode":"read"}`)
	w := f.do("GET", "/s/"+tok+"/zip", nil, "X-No-Auth", "1")
	if w.Code != 200 || w.Header().Get("Content-Disposition") != "attachment; filename=pub.zip" {
		t.Fatalf("%d %v", w.Code, w.Header())
	}
	if got := zipNames(t, w.Body.Bytes()); !slices.Equal(got, []string{"pub/", "pub/a.txt", "pub/in/", "pub/in/b.txt"}) {
		t.Fatalf("got %v", got)
	}
	w = f.do("GET", "/s/"+tok+"/zip?p=in", nil, "X-No-Auth", "1")
	if got := zipNames(t, w.Body.Bytes()); !slices.Equal(got, []string{"in/", "in/b.txt"}) {
		t.Fatalf("got %v", got)
	}
	for _, p := range []string{"../secret.txt", "../../secret.txt"} {
		if c, _, _ := anon(f, "GET", "/s/"+tok+"/zip?p="+p, ""); c != 404 {
			t.Errorf("%s = %d", p, c)
		}
	}
	for _, p := range []string{"missing", "in/missing"} {
		if c, _, _ := anon(f, "GET", "/s/"+tok+"/zip?p="+p, ""); c != 404 {
			t.Errorf("%s = %d", p, c)
		}
	}
	if b := f.do("GET", "/api/shares", nil).Body.String(); !strings.Contains(b, `"hits":2`) {
		t.Fatalf("hits after two zips and two misses: %s", b)
	}
	if os.Getuid() != 0 {
		f.write(t, "pub/locked/x.txt", "x")
		os.Chmod(filepath.Join(f.Dir, "pub/locked/x.txt"), 0)
		if c, _, _ := anon(f, "GET", "/s/"+tok+"/zip?p=locked", ""); c != 403 {
			t.Fatalf("unreadable folder %d", c)
		}
		if b := f.do("GET", "/api/shares", nil).Body.String(); !strings.Contains(b, `"hits":2`) {
			t.Fatalf("a failed zip counted a hit: %s", b)
		}
		os.RemoveAll(filepath.Join(f.Dir, "pub/locked"))
	}
	os.Symlink("../secret.txt", filepath.Join(f.Dir, "pub/link.txt"))
	w = f.do("GET", "/s/"+tok+"/zip", nil, "X-No-Auth", "1")
	if got := zipNames(t, w.Body.Bytes()); slices.Contains(got, "pub/link.txt") {
		t.Fatalf("share zip followed a link out of the share: %v", got)
	}
	if c, _, _ := anon(f, "GET", "/s/"+tok+"/raw/link.txt", ""); c == 200 {
		t.Fatal("share raw followed a link out of the share")
	}
	if _, b, _ := anon(f, "GET", "/s/"+tok+"/ls", ""); strings.Contains(b, "link.txt") {
		t.Fatalf("share ls lists a link out of the share: %s", b)
	}
	drop := mkShare(t, f, `{"vol":"v","path":"pub","mode":"drop"}`)
	if c, _, _ := anon(f, "GET", "/s/"+drop+"/zip", ""); c != 403 {
		t.Fatalf("drop share zip %d", c)
	}
	locked := mkShare(t, f, `{"vol":"v","path":"pub","mode":"read","password":"pw"}`)
	if c, _, _ := anon(f, "GET", "/s/"+locked+"/zip", ""); c != 401 {
		t.Fatalf("locked share zip %d", c)
	}
	file := mkShare(t, f, `{"vol":"v","path":"pub/a.txt","mode":"read"}`)
	if c, _, _ := anon(f, "GET", "/s/"+file+"/zip?p=../secret.txt", ""); c != 404 {
		t.Fatalf("file share escape %d", c)
	}
}
