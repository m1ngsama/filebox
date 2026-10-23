package app

import (
	"encoding/json"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/m1ngsama/filebox/internal/auth"
	"github.com/m1ngsama/filebox/internal/db"
)

func cookieOf(w *httptest.ResponseRecorder) string {
	for _, c := range w.Result().Cookies() {
		if c.Name == auth.CookieName {
			return auth.CookieName + "=" + c.Value
		}
	}
	return ""
}

func tokenOf(t *testing.T, body string) (string, int64) {
	t.Helper()
	var r struct {
		Token string
		ID    int64
	}
	if err := json.Unmarshal([]byte(body), &r); err != nil {
		t.Fatalf("decode %q: %v", body, err)
	}
	return r.Token, r.ID
}

func TestInviteJoinsAMemberWithTheGrantedAccess(t *testing.T) {
	f := newTestApp(t)
	f.write(t, "a.txt", "x")
	alice := f.member(t, "alice", map[string]string{"v": "rw"})

	if c, _ := alice.do(f, "GET", "/api/team", ""); c != 403 {
		t.Fatalf("a member reads the team: %d", c)
	}
	for _, p := range [][2]string{
		{"POST", "/api/team/invites"}, {"PATCH", "/api/team/users/1"}, {"DELETE", "/api/team/users/1"},
		{"POST", "/api/team/users/1/reset"}, {"DELETE", "/api/team/invites/1"},
	} {
		if c, _ := alice.do(f, p[0], p[1], `{"role":"admin","access":{}}`); c != 403 {
			t.Errorf("%s %s as a member: %d", p[0], p[1], c)
		}
	}
	if w := f.do("GET", "/api/team", nil, "X-No-Auth", "1"); w.Code != 401 {
		t.Fatalf("anonymous team: %d", w.Code)
	}
	for _, bad := range []string{
		`{"role":"owner","access":{}}`, `{"role":"member","access":{"nope":"r"}}`, `{"role":"member","access":{"v":"x"}}`,
		`{"role":"member","access":{"v":""}}`, `{"role":"member","access":{"v":"r"},"hours":721}`,
	} {
		if w := f.do("POST", "/api/team/invites", body(bad)); w.Code != 400 {
			t.Errorf("%s: %d", bad, w.Code)
		}
	}

	w := f.do("POST", "/api/team/invites", body(`{"role":"member","access":{"v":"r"},"hours":2}`))
	if w.Code != 201 {
		t.Fatalf("invite %d %s", w.Code, w.Body)
	}
	tok, _ := tokenOf(t, w.Body.String())
	if c, b, _ := anon(f, "GET", "/api/invite/nope", ""); c != 404 {
		t.Fatalf("unknown invite %d %s", c, b)
	}
	if c, b, _ := anon(f, "GET", "/api/invite/"+tok, ""); c != 200 || !strings.Contains(b, `"kind":"join"`) || !strings.Contains(b, `"v":"r"`) {
		t.Fatalf("invite info %d %s", c, b)
	}
	join := func(name, pw string) *struct {
		code   int
		cookie string
		body   string
	} {
		r := f.do("POST", "/api/invite/"+tok, body(`{"name":"`+name+`","password":"`+pw+`"}`), "X-No-Auth", "1")
		return &struct {
			code   int
			cookie string
			body   string
		}{r.Code, cookieOf(r), r.Body.String()}
	}
	if r := join("bob", "short"); r.code != 400 {
		t.Errorf("short password %d", r.code)
	}
	if r := join("bad/name", "long-enough-pw"); r.code != 400 {
		t.Errorf("bad name %d", r.code)
	}
	if r := join("ALICE", "long-enough-pw"); r.code != 409 {
		t.Errorf("a name taken in another case: %d", r.code)
	}
	r := join("bob", "long-enough-pw")
	if r.code != 204 || r.cookie == "" {
		t.Fatalf("join %d %q %s", r.code, r.cookie, r.body)
	}
	bob := person{hdr: []string{"X-No-Auth", "1", "Cookie", r.cookie}}
	if _, b := bob.do(f, "GET", "/api/me", ""); !strings.Contains(b, `"name":"bob"`) || !strings.Contains(b, `"access":{"v":"r"}`) || !strings.Contains(b, `"admin":false`) {
		t.Errorf("the new member: %s", b)
	}
	if c, _ := bob.do(f, "POST", "/api/mkdir", `{"vol":"v","path":"x"}`); c != 403 {
		t.Errorf("a reader writes: %d", c)
	}
	if r := join("carl", "long-enough-pw"); r.code != 404 {
		t.Errorf("a link can be used twice: %d", r.code)
	}
	if _, err := f.App.Auth.Login("bob", "long-enough-pw", "127.0.0.1", ""); err != nil {
		t.Errorf("the new member cannot sign in with the password: %v", err)
	}

	// an expired link
	f.App.DB.AddInvite(db.Invite{Hash: auth.Hash("old"), Kind: db.InviteJoin, Role: db.RoleMember, CreatedAt: 1, ExpiresAt: 2})
	if c, _, _ := anon(f, "GET", "/api/invite/old", ""); c != 404 {
		t.Errorf("expired link %d", c)
	}
	if n, _ := f.App.DB.PurgeInvites(time.Now().Unix()); n != 1 {
		t.Errorf("purged %d", n)
	}
}

func TestAdminEditsAndRemovesMembers(t *testing.T) {
	f := newTestApp(t)
	f.write(t, "a.txt", "x")
	bob := f.member(t, "bob", map[string]string{"v": "rw"})
	listed := decode[struct {
		Users []struct {
			ID   int64
			Name string
			Role string
			Self bool
		}
		Vols []string
	}](t, f.do("GET", "/api/team", nil))
	if len(listed.Users) != 2 || listed.Users[0].Name != "admin" || !listed.Users[0].Self || listed.Users[0].Role != "admin" || len(listed.Vols) != 2 {
		t.Fatalf("team %+v", listed)
	}
	adminID, bobID := listed.Users[0].ID, bob.id
	idp := func(id int64) string { return "/api/team/users/" + strconv.FormatInt(id, 10) }

	if c, _ := bob.do(f, "POST", "/api/mkdir", `{"vol":"v","path":"d"}`); c != 201 {
		t.Fatalf("bob writes %d", c)
	}
	for _, bad := range []string{`{"role":"owner"}`, `{"access":{"nope":"r"}}`, `{"access":{"v":"x"}}`} {
		if w := f.do("PATCH", idp(bobID), body(bad)); w.Code != 400 {
			t.Errorf("%s: %d", bad, w.Code)
		}
	}
	if w := f.do("PATCH", idp(999), body(`{"role":"admin"}`)); w.Code != 404 {
		t.Errorf("unknown user %d", w.Code)
	}
	if w := f.do("PATCH", idp(bobID), body(`{"access":{"v":"r","w":"rw"}}`)); w.Code != 204 {
		t.Fatalf("edit %d %s", w.Code, w.Body)
	}
	if c, _ := bob.do(f, "POST", "/api/mkdir", `{"vol":"v","path":"d2"}`); c != 403 {
		t.Errorf("a demotion must apply at once: %d", c)
	}
	if c, _ := bob.do(f, "POST", "/api/mkdir", `{"vol":"w","path":"d2"}`); c != 201 {
		t.Errorf("a grant must apply at once: %d", c)
	}
	f.do("PATCH", idp(bobID), body(`{"access":{"v":""}}`))
	if c, _ := bob.do(f, "GET", "/api/ls?vol=v&path=", ""); c != 404 {
		t.Errorf("a taken-away volume is still there: %d", c)
	}

	if w := f.do("PATCH", idp(adminID), body(`{"role":"member"}`)); w.Code != 409 {
		t.Errorf("demoting the last admin: %d", w.Code)
	}
	if w := f.do("DELETE", idp(adminID), nil); w.Code != 400 {
		t.Errorf("removing yourself: %d", w.Code)
	}
	if w := f.do("PATCH", idp(bobID), body(`{"role":"admin"}`)); w.Code != 204 {
		t.Errorf("promote %d", w.Code)
	}
	if c, _ := bob.do(f, "GET", "/api/team", ""); c != 200 {
		t.Errorf("a promoted admin cannot manage the team: %d", c)
	}
	if w := f.do("PATCH", idp(adminID), body(`{"role":"member"}`)); w.Code != 204 {
		t.Errorf("demote with another admin around: %d", w.Code)
	}
	if w := f.do("GET", "/api/team", nil); w.Code != 403 {
		t.Errorf("a demoted admin still manages the team: %d", w.Code)
	}
	// bob, now the only admin, removes the old one
	if c, _ := bob.do(f, "DELETE", idp(adminID), ""); c != 204 {
		t.Errorf("remove %d", c)
	}
	if w := f.do("GET", "/api/me", nil); w.Code != 401 {
		t.Errorf("a removed member's session still works: %d", w.Code)
	}
	if c, _ := bob.do(f, "DELETE", idp(bobID), ""); c != 400 {
		t.Errorf("last admin removing themselves: %d", c)
	}
}

func TestResetLinkSetsANewPasswordAndEndsOldSessions(t *testing.T) {
	f := newTestApp(t)
	bob := f.member(t, "bob", map[string]string{"v": "r"})
	w := f.do("POST", "/api/team/users/"+strconv.FormatInt(bob.id, 10)+"/reset", nil)
	if w.Code != 201 {
		t.Fatalf("reset %d %s", w.Code, w.Body)
	}
	tok, _ := tokenOf(t, w.Body.String())
	if c, b, _ := anon(f, "GET", "/api/invite/"+tok, ""); c != 200 || !strings.Contains(b, `"kind":"reset"`) || !strings.Contains(b, `"user":"bob"`) {
		t.Fatalf("info %d %s", c, b)
	}
	if c, _, _ := anon(f, "POST", "/api/invite/"+tok, `{"password":"short"}`); c != 400 {
		t.Errorf("short password %d", c)
	}
	r := f.do("POST", "/api/invite/"+tok, body(`{"password":"a-new-password"}`), "X-No-Auth", "1")
	if r.Code != 204 || cookieOf(r) == "" {
		t.Fatalf("use %d", r.Code)
	}
	if c, _ := bob.do(f, "GET", "/api/me", ""); c != 401 {
		t.Errorf("an old session survived a reset: %d", c)
	}
	if _, err := f.App.Auth.Login("bob", "pw-pw-pw-pw", "127.0.0.1", ""); err == nil {
		t.Error("the old password still works")
	}
	if _, err := f.App.Auth.Login("bob", "a-new-password", "127.0.0.1", ""); err != nil {
		t.Errorf("the new password does not: %v", err)
	}
	if _, b := (person{hdr: []string{"X-No-Auth", "1", "Cookie", cookieOf(r)}}).do(f, "GET", "/api/me", ""); !strings.Contains(b, `"name":"bob"`) {
		t.Errorf("signed in as %s", b)
	}
	if c, _, _ := anon(f, "POST", "/api/invite/"+tok, `{"password":"another-password"}`); c != 404 {
		t.Errorf("a reset link used twice: %d", c)
	}
	// revoking an unused link
	w = f.do("POST", "/api/team/invites", body(`{"role":"member","access":{}}`))
	tok2, id2 := tokenOf(t, w.Body.String())
	if w := f.do("DELETE", "/api/team/invites/"+strconv.FormatInt(id2, 10), nil); w.Code != 204 {
		t.Fatalf("revoke %d", w.Code)
	}
	if c, _, _ := anon(f, "GET", "/api/invite/"+tok2, ""); c != 404 {
		t.Errorf("a revoked link works: %d", c)
	}
}
