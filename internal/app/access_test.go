package app

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/m1ngsama/filebox/internal/auth"
	"github.com/m1ngsama/filebox/internal/db"
)

type person struct {
	id     int64
	hdr    []string
	bearer string
}

// member adds a team member with the given levels per volume and signs them in.
func (f *fixture) member(t *testing.T, name string, grants map[string]string) person {
	t.Helper()
	h, _ := auth.HashPassword("pw-pw-pw-pw")
	id, err := f.App.DB.CreateUser(name, h, db.RoleMember, grants)
	if err != nil {
		t.Fatal(err)
	}
	sess, err := f.App.Auth.Login(name, "pw-pw-pw-pw", "127.0.0.1", "")
	if err != nil {
		t.Fatal(err)
	}
	bearer, _ := f.App.Auth.NewAppToken(id, "t", false)
	return person{id, []string{"X-No-Auth", "1", "Cookie", auth.CookieName + "=" + sess}, bearer}
}

func (p person) do(f *fixture, method, url, b string, hdr ...string) (int, string) {
	w := f.do(method, url, strings.NewReader(b), append(append([]string{}, p.hdr...), hdr...)...)
	return w.Code, w.Body.String()
}

func (p person) dav(f *fixture, method, url, b string, hdr ...string) (int, string) {
	w := f.do(method, url, strings.NewReader(b), append([]string{"X-No-Auth", "1", "Authorization", "Basic " + b64("u:"+p.bearer)}, hdr...)...)
	return w.Code, w.Body.String()
}

func TestVolumeAccess(t *testing.T) {
	f := newTestApp(t)
	f.write(t, "a.txt", "in v")
	f.write(t, "docs/plan.txt", "plan in v")
	os.WriteFile(filepath.Join(f.Dir2, "secret.txt"), []byte("in w"), 0o644)
	f.App.Index.Scan(f.App.Vols)

	alice := f.member(t, "alice", map[string]string{"v": "rw"})
	bob := f.member(t, "bob", map[string]string{"v": "r", "w": "r"})
	carol := f.member(t, "carol", nil)

	each := func(what string, method, url, b string, want map[string]int) {
		t.Helper()
		for who, p := range map[string]person{"alice": alice, "bob": bob, "carol": carol} {
			if c, body := p.do(f, method, url, b); c != want[who] {
				t.Errorf("%s as %s: %d, want %d (%s)", what, who, c, want[who], body)
			}
		}
	}

	each("list v", "GET", "/api/ls?vol=v&path=", "", map[string]int{"alice": 200, "bob": 200, "carol": 404})
	each("list w", "GET", "/api/ls?vol=w&path=", "", map[string]int{"alice": 404, "bob": 200, "carol": 404})
	each("raw v", "GET", "/raw/v/a.txt", "", map[string]int{"alice": 200, "bob": 200, "carol": 404})
	each("raw w", "GET", "/raw/w/secret.txt", "", map[string]int{"alice": 404, "bob": 200, "carol": 404})
	each("thumb w", "GET", "/thumb/w/secret.txt", "", map[string]int{"alice": 404, "bob": 404, "carol": 404})
	each("render w", "GET", "/api/render?vol=w&p=secret.txt", "", map[string]int{"alice": 404, "bob": 200, "carol": 404})
	each("zip w", "GET", "/api/zip?vol=w&p=secret.txt", "", map[string]int{"alice": 404, "bob": 200, "carol": 404})
	each("size w", "GET", "/api/size?vol=w&path=", "", map[string]int{"alice": 404, "bob": 200, "carol": 404})
	each("trash list w", "GET", "/api/trash?vol=w", "", map[string]int{"alice": 404, "bob": 200, "carol": 404})

	each("mkdir v", "POST", "/api/mkdir", `{"vol":"v","path":"d1"}`, map[string]int{"alice": 201, "bob": 403, "carol": 404})
	each("mkdir w", "POST", "/api/mkdir", `{"vol":"w","path":"d2"}`, map[string]int{"alice": 404, "bob": 403, "carol": 404})
	each("touch v", "POST", "/api/touch", `{"vol":"v","path":"n.txt"}`, map[string]int{"alice": 201, "bob": 403, "carol": 404})
	each("rm w", "POST", "/api/rm", `{"vol":"w","paths":["secret.txt"]}`, map[string]int{"alice": 404, "bob": 403, "carol": 404})
	each("move out of w", "POST", "/api/mv", `{"src":{"vol":"w","path":"secret.txt"},"dst":{"vol":"v","path":"stolen.txt"}}`, map[string]int{"alice": 404, "bob": 403, "carol": 404})
	each("copy out of w", "POST", "/api/cp", `{"src":{"vol":"w","path":"secret.txt"},"dst":{"vol":"v","path":"copy-of-secret.txt"}}`, map[string]int{"alice": 404, "bob": 403, "carol": 404})
	if c, _ := bob.do(f, "POST", "/api/cp", `{"src":{"vol":"w","path":"secret.txt"},"dst":{"vol":"v","path":"x"}}`); c != 403 {
		t.Errorf("a reader must not copy into a volume they cannot write: %d", c)
	}
	each("save v", "PUT", "/api/file?vol=v&path=a.txt", "x", map[string]int{"alice": 412, "bob": 403, "carol": 404})
	if _, err := os.Stat(filepath.Join(f.Dir, "stolen.txt")); err == nil {
		t.Error("a file left a volume its mover could not read")
	}
	if _, err := os.Stat(filepath.Join(f.Dir2, "secret.txt")); err != nil {
		t.Error("a reader removed a file")
	}

	md := func(vol string) string {
		return "vol " + b64(vol) + ",dir " + b64("") + ",filename " + b64("up.txt")
	}
	tus := func(p person, vol string) int {
		c, _ := p.do(f, "POST", "/upload/", "", "Tus-Resumable", "1.0.0", "Upload-Length", "0", "Upload-Metadata", md(vol))
		return c
	}
	if a, b, c := tus(alice, "v"), tus(bob, "v"), tus(carol, "v"); a != 201 || b != 403 || c != 404 {
		t.Errorf("upload to v: alice %d bob %d carol %d", a, b, c)
	}
	if a, b := tus(alice, "w"), tus(bob, "w"); a != 404 || b != 403 {
		t.Errorf("upload to w: alice %d bob %d", a, b)
	}

	if _, body := alice.do(f, "GET", "/api/search?q=plan", ""); !strings.Contains(body, "docs/plan.txt") {
		t.Errorf("alice cannot find her own file: %s", body)
	}
	if _, body := alice.do(f, "GET", "/api/search?q=secret", ""); strings.Contains(body, "secret.txt") {
		t.Errorf("search leaked a file from a volume alice cannot read: %s", body)
	}
	if _, body := bob.do(f, "GET", "/api/search?q=secret", ""); !strings.Contains(body, "secret.txt") {
		t.Errorf("bob cannot find a file on a volume he can read: %s", body)
	}
	if c, _ := carol.do(f, "GET", "/api/search?q=plan&vol=v", ""); c != 404 {
		t.Errorf("search inside a hidden volume: %d", c)
	}
	if _, body := carol.do(f, "GET", "/api/search?q=plan", ""); strings.Contains(body, "plan.txt") {
		t.Errorf("carol found a file: %s", body)
	}
	if _, body := alice.do(f, "GET", "/api/recent", ""); strings.Contains(body, "secret.txt") || !strings.Contains(body, "a.txt") {
		t.Errorf("recent for alice: %s", body)
	}
	if _, body := carol.do(f, "GET", "/api/recent", ""); strings.Contains(body, ".txt") {
		t.Errorf("recent for carol: %s", body)
	}
	if _, body := alice.do(f, "GET", "/api/usage", ""); strings.Contains(body, "secret.txt") {
		t.Errorf("usage leaked a file: %s", body)
	}
	if _, body := carol.do(f, "GET", "/api/usage", ""); strings.Contains(body, ".txt") || !strings.Contains(body, `"kinds":[]`) {
		t.Errorf("usage for carol: %s", body)
	}
	if _, body := alice.do(f, "GET", "/api/vols", ""); strings.Contains(body, `"name":"w"`) || !strings.Contains(body, `"name":"v"`) {
		t.Errorf("volume list for alice: %s", body)
	}

	me := func(p person) (string, string) { _, b := p.do(f, "GET", "/api/me", ""); return b, b }
	if b, _ := me(alice); !strings.Contains(b, `"admin":false`) || !strings.Contains(b, `"vols":["v"]`) || !strings.Contains(b, `"access":{"v":"rw"}`) {
		t.Errorf("me for alice: %s", b)
	}
	if b, _ := me(bob); !strings.Contains(b, `"access":{"v":"r","w":"r"}`) {
		t.Errorf("me for bob: %s", b)
	}
	if b, _ := me(carol); !strings.Contains(b, `"vols":[]`) {
		t.Errorf("me for carol: %s", b)
	}
	if w := f.do("GET", "/api/me", nil); !strings.Contains(w.Body.String(), `"admin":true`) || !strings.Contains(w.Body.String(), `"vols":["v","w"]`) {
		t.Errorf("me for the admin: %s", w.Body)
	}

	// shares
	if c, _ := alice.do(f, "POST", "/api/shares", `{"vol":"w","path":"secret.txt","mode":"read"}`); c != 404 {
		t.Errorf("share from a hidden volume: %d", c)
	}
	if c, _ := bob.do(f, "POST", "/api/shares", `{"vol":"v","path":"docs","mode":"upload"}`); c != 403 {
		t.Errorf("upload share from a read-only volume: %d", c)
	}
	c, body := bob.do(f, "POST", "/api/shares", `{"vol":"v","path":"docs","mode":"read"}`)
	if c != 201 {
		t.Fatalf("read share by a reader: %d %s", c, body)
	}
	tok := strings.Split(strings.Split(body, `"token":"`)[1], `"`)[0]
	if c, _, _ := anon(f, "GET", "/s/"+tok+"/info", ""); c != 200 {
		t.Errorf("share info %d", c)
	}
	f.App.DB.SetAccess(bob.id, "v", "")
	f.App.Auth.Forget(bob.id)
	if c, _, _ := anon(f, "GET", "/s/"+tok+"/info", ""); c != 404 {
		t.Errorf("a share must stop working when its maker loses the volume: %d", c)
	}
	f.App.DB.SetAccess(bob.id, "v", "r")
	f.App.Auth.Forget(bob.id)
	if c, _, _ := anon(f, "GET", "/s/"+tok+"/info", ""); c != 200 {
		t.Errorf("a share must work again when access returns: %d", c)
	}

	// favorites belong to each person
	if c, _ := alice.do(f, "POST", "/api/favorites", `{"vol":"v","paths":["a.txt"],"star":true}`); c != 204 {
		t.Errorf("star %d", c)
	}
	if c, _ := alice.do(f, "POST", "/api/favorites", `{"vol":"w","paths":["secret.txt"],"star":true}`); c != 404 {
		t.Errorf("star on a hidden volume %d", c)
	}
	if _, body := alice.do(f, "GET", "/api/favorites", ""); !strings.Contains(body, "a.txt") {
		t.Errorf("alice's favorites: %s", body)
	}
	if _, body := bob.do(f, "GET", "/api/favorites", ""); strings.Contains(body, "a.txt") {
		t.Errorf("bob sees alice's favorites: %s", body)
	}

	// WebDAV
	if c, body := alice.dav(f, "PROPFIND", "/dav/", "", "Depth", "1"); c != 207 || !strings.Contains(body, "/dav/v/") || strings.Contains(body, "/dav/w/") {
		t.Errorf("dav root for alice: %d %s", c, body)
	}
	if c, body := bob.dav(f, "PROPFIND", "/dav/", "", "Depth", "1"); c != 207 || !strings.Contains(body, "/dav/v/") || !strings.Contains(body, "/dav/w/") {
		t.Errorf("dav root for bob: %d %s", c, body)
	}
	if c, _ := carol.dav(f, "PROPFIND", "/dav/v/", "", "Depth", "1"); c != 404 {
		t.Errorf("dav list of a hidden volume: %d", c)
	}
	if c, _ := alice.dav(f, "GET", "/dav/w/secret.txt", ""); c != 404 {
		t.Errorf("dav read of a hidden volume: %d", c)
	}
	if c, _ := bob.dav(f, "GET", "/dav/w/secret.txt", ""); c != 200 {
		t.Errorf("dav read as a reader: %d", c)
	}
	for _, m := range []struct {
		method, url string
		hdr         []string
	}{
		{"PUT", "/dav/w/new.txt", nil},
		{"MKCOL", "/dav/w/dir", nil},
		{"DELETE", "/dav/w/secret.txt", nil},
		{"LOCK", "/dav/w/secret.txt", nil},
		{"PROPPATCH", "/dav/w/secret.txt", nil},
		{"MOVE", "/dav/w/secret.txt", []string{"Destination", "/dav/v/moved.txt"}},
		{"COPY", "/dav/w/secret.txt", []string{"Destination", "/dav/v/copied.txt"}},
	} {
		if c, _ := bob.dav(f, m.method, m.url, "x", m.hdr...); c != http.StatusForbidden && c != http.StatusNotFound {
			t.Errorf("dav %s as a reader: %d", m.method, c)
		}
	}
	if c, _ := alice.dav(f, "PUT", "/dav/v/dav.txt", "x"); c != 201 {
		t.Errorf("dav write as a writer: %d", c)
	}
	if c, _ := alice.dav(f, "PUT", "/dav/w/dav.txt", "x"); c != 403 && c != 404 {
		t.Errorf("dav write to a hidden volume: %d", c)
	}
	if _, err := os.Stat(filepath.Join(f.Dir2, "new.txt")); err == nil {
		t.Error("a reader wrote through WebDAV")
	}
	if _, err := os.Stat(filepath.Join(f.Dir, "copied.txt")); err == nil {
		t.Error("a reader copied out through WebDAV")
	}

	// the admin reaches everything, and losing access takes effect at once
	if c, _ := f.member(t, "dave", map[string]string{"v": "rw"}).do(f, "GET", "/api/ls?vol=v&path=", ""); c != 200 {
		t.Errorf("dave %d", c)
	}
	if w := f.do("GET", "/api/ls?vol=w&path=", nil); w.Code != 200 {
		t.Errorf("admin %d", w.Code)
	}
	f.App.DB.SetAccess(alice.id, "v", "r")
	f.App.Auth.Forget(alice.id)
	if c, _ := alice.do(f, "POST", "/api/mkdir", `{"vol":"v","path":"d9"}`); c != 403 {
		t.Errorf("a demoted writer still writes: %d", c)
	}
}

func TestTeamActivity(t *testing.T) {
	f := newTestApp(t)
	f.write(t, "a.txt", "a")
	os.WriteFile(filepath.Join(f.Dir2, "w.txt"), []byte("w"), 0o644)
	bob := f.member(t, "bob", map[string]string{"v": "rw"})
	carol := f.member(t, "carol", map[string]string{"w": "rw"})
	bob.do(f, "POST", "/api/mkdir", `{"vol":"v","path":"by-bob"}`)
	carol.do(f, "POST", "/api/mkdir", `{"vol":"w","path":"by-carol"}`)
	f.do("POST", "/api/mkdir", body(`{"vol":"v","path":"by-admin"}`))
	f.App.DB.Flush()
	type ev struct {
		Kind, Path, Actor string
	}
	list := func(p *person, q string) []ev {
		var b string
		if p == nil {
			b = f.do("GET", "/api/activity"+q, nil).Body.String()
		} else {
			_, b = p.do(f, "GET", "/api/activity"+q, "")
		}
		var r struct{ Events []ev }
		json.Unmarshal([]byte(b), &r)
		return r.Events
	}
	if es := list(&bob, "?kind=files"); len(es) != 1 || es[0].Path != "by-bob" || es[0].Actor != "" {
		t.Fatalf("bob's own activity %+v", es)
	}
	es := list(&bob, "?everyone=1")
	paths := map[string]string{}
	for _, e := range es {
		paths[e.Path] = e.Actor
	}
	if paths["by-bob"] != "bob" || paths["by-admin"] != "admin" || len(paths) != 2 {
		t.Fatalf("bob sees the team's changes on v, named, and nothing on w: %+v", es)
	}
	if es := list(&bob, "?everyone=1&kind=login"); len(es) != 0 {
		t.Fatalf("a member sees other people's sign-ins: %+v", es)
	}
	all := list(nil, "?everyone=1")
	seen := map[string]bool{}
	for _, e := range all {
		seen[e.Path] = true
	}
	if !seen["by-carol"] || !seen["by-bob"] || !seen["by-admin"] {
		t.Fatalf("the admin sees everyone's changes: %+v", all)
	}
	logins := list(nil, "?everyone=1&kind=login")
	who := map[string]bool{}
	for _, e := range logins {
		who[e.Actor] = true
	}
	if !who["bob"] || !who["carol"] {
		t.Fatalf("the admin sees everyone's sign-ins: %+v", logins)
	}
}
