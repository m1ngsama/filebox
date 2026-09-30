package dav

import (
	"os"
	"path/filepath"
	"testing"
)

const lockBody = `<?xml version="1.0" encoding="utf-8"?><D:lockinfo xmlns:D="DAV:"><D:lockscope><D:exclusive/></D:lockscope><D:locktype><D:write/></D:locktype><D:owner>t</D:owner></D:lockinfo>`

func (e *env) lock(t *testing.T, p string) string {
	t.Helper()
	res, body := e.req(t, e.rw, "LOCK", p, lockBody, "Timeout", "Second-600")
	tok := res.Header.Get("Lock-Token")
	if res.StatusCode != 200 || tok == "" {
		t.Fatalf("LOCK %s: %d %s", p, res.StatusCode, body)
	}
	return tok
}

func TestDavDeleteRespectsLocksBelow(t *testing.T) {
	e := setup(t)
	e.req(t, e.rw, "MKCOL", "/dav/v/d", "")
	e.req(t, e.rw, "PUT", "/dav/v/d/c.txt", "c")
	tok := e.lock(t, "/dav/v/d/c.txt")
	if res, _ := e.req(t, e.rw, "DELETE", "/dav/v/d", ""); res.StatusCode != 423 {
		t.Fatalf("DELETE over a locked member %d", res.StatusCode)
	}
	if res, _ := e.req(t, e.rw, "MOVE", "/dav/v/d", "", "Destination", e.srv.URL+"/dav/v/e"); res.StatusCode != 423 {
		t.Fatalf("MOVE over a locked member %d", res.StatusCode)
	}
	if _, err := os.Stat(filepath.Join(e.dir, "d/c.txt")); err != nil {
		t.Fatal(err)
	}
	if res, _ := e.req(t, e.rw, "DELETE", "/dav/v/d", "", "If", "<"+e.srv.URL+"/dav/v/d/c.txt> ("+tok+")"); res.StatusCode != 204 {
		t.Fatalf("DELETE with the member's token %d", res.StatusCode)
	}
	if res, _ := e.req(t, e.rw, "MKCOL", "/dav/v/d", ""); res.StatusCode != 201 {
		t.Fatalf("MKCOL after delete %d", res.StatusCode)
	}
	if res, _ := e.req(t, e.rw, "PUT", "/dav/v/d/c.txt", "new"); res.StatusCode != 201 {
		t.Fatalf("PUT where a deleted lock was %d", res.StatusCode)
	}
}

func TestDavDeleteNeedsTheTargetsOwnToken(t *testing.T) {
	e := setup(t)
	e.req(t, e.rw, "PUT", "/dav/v/a.txt", "a")
	e.req(t, e.rw, "PUT", "/dav/v/other.txt", "o")
	a := e.lock(t, "/dav/v/a.txt")
	b := e.lock(t, "/dav/v/other.txt")
	if res, _ := e.req(t, e.rw, "DELETE", "/dav/v/a.txt", "", "If", "<"+e.srv.URL+"/dav/v/other.txt> ("+b+")"); res.StatusCode != 423 {
		t.Fatalf("DELETE tagged with another lock %d", res.StatusCode)
	}
	if res, _ := e.req(t, e.rw, "DELETE", "/dav/v/a.txt", "", "If", "(Not "+a+")"); res.StatusCode != 423 {
		t.Fatalf("DELETE with a negated token %d", res.StatusCode)
	}
	if _, err := os.Stat(filepath.Join(e.dir, "a.txt")); err != nil {
		t.Fatal(err)
	}
	if res, _ := e.req(t, e.rw, "DELETE", "/dav/v/a.txt", "", "If", "("+a+")"); res.StatusCode != 204 {
		t.Fatalf("DELETE with its own token %d", res.StatusCode)
	}
	if res, _ := e.req(t, e.rw, "PUT", "/dav/v/a.txt", "again"); res.StatusCode != 201 {
		t.Fatalf("PUT after delete %d", res.StatusCode)
	}
}
