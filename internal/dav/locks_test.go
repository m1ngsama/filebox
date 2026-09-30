package dav

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/net/webdav"
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
	e.req(t, e.rw, "MKCOL", "/dav/v/src", "")
	e.req(t, e.rw, "PUT", "/dav/v/src2", "file")
	for _, m := range []struct{ method, from string }{{"COPY", "/dav/v/src"}, {"MOVE", "/dav/v/src2"}} {
		res, _ := e.req(t, e.rw, m.method, m.from, "", "Destination", e.srv.URL+"/dav/v/d", "Overwrite", "T")
		if res.StatusCode != 423 {
			t.Fatalf("%s over a locked destination member %d", m.method, res.StatusCode)
		}
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

func TestDavLocksLastAtMostAnHour(t *testing.T) {
	e := setup(t)
	e.req(t, e.rw, "PUT", "/dav/v/a.txt", "a")
	for _, tm := range []string{"", "Infinite", "Second-99999", "Infinite, Second-4100000000"} {
		res, body := e.req(t, e.rw, "LOCK", "/dav/v/a.txt", lockBody, "Timeout", tm)
		if res.StatusCode != 200 || !strings.Contains(body, "Second-3600") {
			t.Fatalf("Timeout %q: %d %s", tm, res.StatusCode, body)
		}
		e.req(t, e.rw, "UNLOCK", "/dav/v/a.txt", "", "Lock-Token", res.Header.Get("Lock-Token"))
	}
	l := newLocks()
	now := time.Now()
	tok, _ := l.Create(now, webdav.LockDetails{Root: "/v/d/f", Duration: -1})
	if got := l.covering(now.Add(59*time.Minute), "/v/d", nil); len(got) != 1 {
		t.Fatal("lock gone before an hour")
	}
	if got := l.covering(now.Add(61*time.Minute), "/v/d", nil); len(got) != 0 {
		t.Fatalf("infinite lock outlived an hour: %v %s", got, tok)
	}
}

func TestLockRecordSurvivesAFailedUnlock(t *testing.T) {
	l := newLocks()
	now := time.Now()
	tok, _ := l.Create(now, webdav.LockDetails{Root: "/v/d/f", Duration: time.Minute})
	release, err := l.Confirm(now, "/v/d/f", "", webdav.Condition{Token: tok})
	if err != nil {
		t.Fatal(err)
	}
	if err := l.Unlock(now, tok); err == nil {
		t.Fatal("unlock of a held lock succeeded")
	}
	if len(l.covering(now, "/v/d", nil)) != 1 {
		t.Fatal("record dropped after a failed unlock")
	}
	release()
	if l.Unlock(now, tok) != nil || len(l.covering(now, "/v/d", nil)) != 0 {
		t.Fatal("unlock after release")
	}
}

func TestDavOverwriteOfALockedDestination(t *testing.T) {
	e := setup(t)
	for _, m := range []string{"COPY", "MOVE"} {
		e.req(t, e.rw, "PUT", "/dav/v/doc.docx", "old")
		e.req(t, e.rw, "PUT", "/dav/v/tmp.docx", "new "+m)
		tok := e.lock(t, "/dav/v/doc.docx")
		if res, _ := e.req(t, e.rw, m, "/dav/v/tmp.docx", "", "Destination", e.srv.URL+"/dav/v/doc.docx", "Overwrite", "T"); res.StatusCode != 423 {
			t.Fatalf("%s over a locked file without its token %d", m, res.StatusCode)
		}
		if res, _ := e.req(t, e.rw, m, "/dav/v/tmp.docx", "", "Destination", e.srv.URL+"/dav/v/doc.docx", "Overwrite", "T",
			"If", "<"+e.srv.URL+"/dav/v/doc.docx> ("+tok+")"); res.StatusCode != 204 {
			t.Fatalf("%s by the lock holder %d", m, res.StatusCode)
		}
		if b, _ := os.ReadFile(filepath.Join(e.dir, "doc.docx")); string(b) != "new "+m {
			t.Fatalf("%s content %q", m, b)
		}
		if res, _ := e.req(t, e.rw, "PUT", "/dav/v/doc.docx", "after"); res.StatusCode != 201 && res.StatusCode != 204 {
			t.Fatalf("%s left the destination locked: PUT %d", m, res.StatusCode)
		}
		e.req(t, e.rw, "PUT", "/dav/v/tmp.docx", "again")
		tok = e.lock(t, "/dav/v/doc.docx")
		if res, _ := e.req(t, e.rw, m, "/dav/v/tmp.docx", "", "Destination", e.srv.URL+"/dav/v/doc.docx", "Overwrite", "T", "If", "("+tok+")"); res.StatusCode == 423 {
			t.Fatalf("%s with the destination token in an untagged list got 423", m)
		}
		e.req(t, e.rw, "UNLOCK", "/dav/v/doc.docx", "", "Lock-Token", tok)
	}
}

func TestDavLockHoldsThroughACaseAlias(t *testing.T) {
	e := setup(t)
	e.req(t, e.rw, "MKCOL", "/dav/v/Case", "")
	e.req(t, e.rw, "PUT", "/dav/v/Case/f.txt", "f")
	if _, err := os.Stat(filepath.Join(e.dir, "case")); err != nil {
		t.Skip("case-sensitive filesystem")
	}
	tok := e.lock(t, "/dav/v/Case/f.txt")
	if res, _ := e.req(t, e.rw, "DELETE", "/dav/v/case", ""); res.StatusCode != 423 {
		t.Fatalf("DELETE through a case alias %d", res.StatusCode)
	}
	if res, _ := e.req(t, e.rw, "DELETE", "/dav/v/case", "", "If", "<"+e.srv.URL+"/dav/v/Case/f.txt> ("+tok+")"); res.StatusCode != 204 {
		t.Fatalf("DELETE through a case alias with the token %d", res.StatusCode)
	}
	if res, _ := e.req(t, e.rw, "MKCOL", "/dav/v/Case", ""); res.StatusCode != 201 {
		t.Fatalf("MKCOL %d", res.StatusCode)
	}
	if res, _ := e.req(t, e.rw, "PUT", "/dav/v/Case/f.txt", "new"); res.StatusCode != 201 {
		t.Fatalf("lock survived a DELETE through its case alias: PUT %d", res.StatusCode)
	}
}
