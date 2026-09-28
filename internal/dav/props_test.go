package dav

import (
	"strings"
	"testing"
)

const setProps = `<?xml version="1.0" encoding="utf-8"?>
<D:propertyupdate xmlns:D="DAV:" xmlns:Z="urn:z">
<D:set><D:prop><Z:color>red</Z:color><Z:deep><Z:x xmlns:Z="urn:z">1</Z:x></Z:deep></D:prop></D:set>
<D:remove><D:prop><Z:never/></D:prop></D:remove>
</D:propertyupdate>`

const findProps = `<?xml version="1.0" encoding="utf-8"?>
<D:propfind xmlns:D="DAV:" xmlns:Z="urn:z"><D:prop><Z:color/><Z:deep/></D:prop></D:propfind>`

func (e *env) proppatch(t *testing.T, p, body string) string {
	t.Helper()
	res, out := e.req(t, e.rw, "PROPPATCH", p, body, "Content-Type", "application/xml")
	if res.StatusCode != 207 || !strings.Contains(out, "200 OK") || strings.Contains(out, "403") {
		t.Fatalf("PROPPATCH %s: %d %s", p, res.StatusCode, out)
	}
	return out
}

func (e *env) color(t *testing.T, p string) string {
	t.Helper()
	res, out := e.req(t, e.rw, "PROPFIND", p, findProps, "Depth", "0")
	if res.StatusCode != 207 {
		t.Fatalf("PROPFIND %s: %d %s", p, res.StatusCode, out)
	}
	if !strings.Contains(out, ">red<") {
		return ""
	}
	if !strings.Contains(out, `<x xmlns="urn:z">1</x>`) && !strings.Contains(out, `<Z:x xmlns:Z="urn:z">1</Z:x>`) {
		t.Fatalf("nested value lost: %s", out)
	}
	return "red"
}

func TestDavPropsRoundTrip(t *testing.T) {
	e := setup(t)
	e.req(t, e.rw, "PUT", "/dav/v/a.txt", "x")
	e.req(t, e.rw, "MKCOL", "/dav/v/d", "")
	for _, p := range []string{"/dav/v/a.txt", "/dav/v/d", "/dav/v/"} {
		e.proppatch(t, p, setProps)
		if e.color(t, p) != "red" {
			t.Fatalf("%s lost its props", p)
		}
	}
	e.proppatch(t, "/dav/v/a.txt", `<?xml version="1.0"?><D:propertyupdate xmlns:D="DAV:" xmlns:Z="urn:z">
<D:remove><D:prop><Z:color/><Z:deep/></D:prop></D:remove></D:propertyupdate>`)
	if e.color(t, "/dav/v/a.txt") != "" {
		t.Fatal("removed props still listed")
	}
	res, out := e.req(t, e.rw, "PROPPATCH", "/dav/", setProps)
	if res.StatusCode == 207 && strings.Contains(out, "200 OK") {
		t.Fatalf("virtual root accepted props: %s", out)
	}
}

func TestDavPropsFollowMoveCopyDelete(t *testing.T) {
	e := setup(t)
	e.req(t, e.rw, "MKCOL", "/dav/v/d", "")
	e.req(t, e.rw, "PUT", "/dav/v/d/f.txt", "x")
	e.req(t, e.rw, "MKCOL", "/dav/v/dx", "")
	for _, p := range []string{"/dav/v/d", "/dav/v/d/f.txt", "/dav/v/dx"} {
		e.proppatch(t, p, setProps)
	}
	do := func(method, from, to string, want int) {
		t.Helper()
		res, out := e.req(t, e.rw, method, from, "", "Destination", e.srv.URL+to)
		if res.StatusCode != want {
			t.Fatalf("%s %s %s: %d %s", method, from, to, res.StatusCode, out)
		}
	}
	has := func(ps ...string) {
		t.Helper()
		for _, p := range ps {
			if e.color(t, p) != "red" {
				t.Fatalf("%s has no props", p)
			}
		}
	}
	do("MOVE", "/dav/v/d", "/dav/v/e", 201)
	has("/dav/v/e", "/dav/v/e/f.txt", "/dav/v/dx")
	do("MOVE", "/dav/v/e", "/dav/w/e", 201)
	has("/dav/w/e", "/dav/w/e/f.txt")
	do("COPY", "/dav/w/e", "/dav/w/c", 201)
	has("/dav/w/e", "/dav/w/e/f.txt", "/dav/w/c", "/dav/w/c/f.txt")
	do("COPY", "/dav/w/e/f.txt", "/dav/v/g.txt", 201)
	has("/dav/v/g.txt")
	e.req(t, e.rw, "MKCOL", "/dav/v/d", "")
	if e.color(t, "/dav/v/d") != "" {
		t.Fatal("new collection inherited moved props")
	}
	e.proppatch(t, "/dav/v/d", setProps)
	for _, p := range []string{"/dav/w/e", "/dav/v/d"} {
		if res, _ := e.req(t, e.rw, "DELETE", p, ""); res.StatusCode != 204 {
			t.Fatalf("DELETE %s: %d", p, res.StatusCode)
		}
	}
	e.req(t, e.rw, "MKCOL", "/dav/w/e", "")
	e.req(t, e.rw, "PUT", "/dav/w/e/f.txt", "x")
	e.req(t, e.rw, "MKCOL", "/dav/v/d", "")
	for _, p := range []string{"/dav/w/e", "/dav/w/e/f.txt", "/dav/v/d"} {
		if e.color(t, p) != "" {
			t.Fatalf("%s kept props after DELETE", p)
		}
	}
	has("/dav/v/dx", "/dav/w/c")
}

func TestDavWin32Props(t *testing.T) {
	e := setup(t)
	e.req(t, e.rw, "PUT", "/dav/v/w.txt", "x")
	body := `<?xml version="1.0" encoding="utf-8" ?><D:propertyupdate xmlns:D="DAV:" xmlns:Z="urn:schemas-microsoft-com:"><D:set><D:prop>` +
		`<Z:Win32CreationTime>Mon, 28 Sep 2026 10:00:00 GMT</Z:Win32CreationTime>` +
		`<Z:Win32LastAccessTime>Mon, 28 Sep 2026 10:00:00 GMT</Z:Win32LastAccessTime>` +
		`<Z:Win32LastModifiedTime>Mon, 28 Sep 2026 10:00:00 GMT</Z:Win32LastModifiedTime>` +
		`<Z:Win32FileAttributes>00000020</Z:Win32FileAttributes>` +
		`</D:prop></D:set></D:propertyupdate>`
	out := e.proppatch(t, "/dav/v/w.txt", body)
	if n := strings.Count(out, "<Win32"); n != 4 {
		t.Fatalf("propstat names %d: %s", n, out)
	}
}
