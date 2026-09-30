package dav

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/m1ngsama/filebox/internal/version"
	"github.com/m1ngsama/filebox/internal/vol"
)

func (e *env) versions(t *testing.T, rel string) []string {
	t.Helper()
	xs, err := e.vs.List("v", rel)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, x := range xs {
		b, err := os.ReadFile(filepath.Join(e.dir, vol.VersionsDir, x.ID))
		if err != nil {
			t.Fatalf("version %s: %v", x.ID, err)
		}
		if x.Source != version.WebDAV {
			t.Errorf("source %q", x.Source)
		}
		out = append(out, string(b))
	}
	return out
}

func TestDavPutKeepsVersion(t *testing.T) {
	e := setup(t)
	e.req(t, e.rw, "PUT", "/dav/v/a.txt", "")
	e.req(t, e.rw, "PUT", "/dav/v/a.txt", "one")
	res, _ := e.req(t, e.rw, "PUT", "/dav/v/a.txt", "two")
	if res.StatusCode != 201 {
		t.Fatalf("PUT %d", res.StatusCode)
	}
	if b, _ := os.ReadFile(filepath.Join(e.dir, "a.txt")); string(b) != "two" {
		t.Fatalf("content %q", b)
	}
	if got := e.versions(t, "a.txt"); len(got) != 1 || got[0] != "one" {
		t.Fatalf("versions %q", got)
	}
	e.req(t, e.rw, "DELETE", "/dav/v/a.txt", "")
	if got := e.versions(t, "a.txt"); len(got) != 1 {
		t.Fatalf("delete dropped versions %q", got)
	}
}

func TestDavMoveOverwriteKeepsVersion(t *testing.T) {
	e := setup(t)
	e.req(t, e.rw, "PUT", "/dav/v/doc.txt", "saved")
	e.req(t, e.rw, "PUT", "/dav/v/doc.txt.tmp", "draft")
	e.req(t, e.rw, "PUT", "/dav/v/doc.txt.tmp", "edited")
	res, _ := e.req(t, e.rw, "MOVE", "/dav/v/doc.txt.tmp", "", "Destination", e.srv.URL+"/dav/v/doc.txt", "Overwrite", "T")
	if res.StatusCode != 204 {
		t.Fatalf("MOVE %d", res.StatusCode)
	}
	if b, _ := os.ReadFile(filepath.Join(e.dir, "doc.txt")); string(b) != "edited" {
		t.Fatalf("content %q", b)
	}
	got := e.versions(t, "doc.txt")
	if len(got) != 2 || !strings.Contains(strings.Join(got, ","), "saved") || !strings.Contains(strings.Join(got, ","), "draft") {
		t.Fatalf("versions %q", got)
	}
	if got := e.versions(t, "doc.txt.tmp"); len(got) != 0 {
		t.Fatalf("versions left behind %q", got)
	}
	res, _ = e.req(t, e.rw, "MOVE", "/dav/v/doc.txt", "", "Destination", e.srv.URL+"/dav/v/renamed.txt")
	if res.StatusCode != 201 || len(e.versions(t, "renamed.txt")) != 2 || len(e.versions(t, "doc.txt")) != 0 {
		t.Fatalf("versions did not follow the rename: %d", res.StatusCode)
	}
}

func TestDavCopyOverwriteKeepsVersion(t *testing.T) {
	e := setup(t)
	e.req(t, e.rw, "PUT", "/dav/v/a.txt", "a")
	e.req(t, e.rw, "PUT", "/dav/v/b.txt", "b")
	res, _ := e.req(t, e.rw, "COPY", "/dav/v/a.txt", "", "Destination", e.srv.URL+"/dav/v/b.txt")
	if res.StatusCode != 204 {
		t.Fatalf("COPY %d", res.StatusCode)
	}
	if got := e.versions(t, "b.txt"); len(got) != 1 || got[0] != "b" {
		t.Fatalf("versions %q", got)
	}
	res, _ = e.req(t, e.rw, "COPY", "/dav/v/a.txt", "", "Destination", e.srv.URL+"/dav/v/b.txt", "Overwrite", "F")
	if res.StatusCode != 412 || len(e.versions(t, "b.txt")) != 1 {
		t.Fatalf("COPY without overwrite %d", res.StatusCode)
	}
}

func TestDavHidesVersions(t *testing.T) {
	e := setup(t)
	e.req(t, e.rw, "PUT", "/dav/v/a.txt", "one")
	e.req(t, e.rw, "PUT", "/dav/v/a.txt", "two")
	xs, _ := e.vs.List("v", "a.txt")
	if len(xs) != 1 {
		t.Fatalf("versions %v", xs)
	}
	_, body := e.req(t, e.rw, "PROPFIND", "/dav/v/", "", "Depth", "1")
	if strings.Contains(body, "versions") || strings.Contains(body, xs[0].ID) {
		t.Fatalf("listing %s", body)
	}
	for _, p := range []string{"/dav/v/.filebox/versions/" + xs[0].ID, "/dav/v/.filebox/versions/"} {
		if res, _ := e.req(t, e.rw, "GET", p, ""); res.StatusCode != 404 {
			t.Fatalf("GET %s %d", p, res.StatusCode)
		}
		if res, _ := e.req(t, e.rw, "PROPFIND", p, "", "Depth", "1"); res.StatusCode != 404 {
			t.Fatalf("PROPFIND %s %d", p, res.StatusCode)
		}
	}
}

func TestDavCurlUploadTwice(t *testing.T) {
	curl, err := exec.LookPath("curl")
	if err != nil {
		t.Skip("no curl")
	}
	e := setup(t)
	src := filepath.Join(t.TempDir(), "note.txt")
	for _, body := range []string{"first", "second"} {
		os.WriteFile(src, []byte(body), 0o644)
		out, err := exec.Command(curl, "-fsS", "-u", "me:"+e.rw, "-T", src, e.srv.URL+"/dav/v/note.txt").CombinedOutput()
		if err != nil {
			t.Fatalf("curl: %v %s", err, out)
		}
	}
	if got := e.versions(t, "note.txt"); len(got) != 1 || got[0] != "first" {
		t.Fatalf("versions %q", got)
	}
}
