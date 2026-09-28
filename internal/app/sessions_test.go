package app

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/m1ngsama/filebox/internal/auth"
)

type sessionList struct {
	Sessions []struct {
		ID        int64
		UserAgent string `json:"user_agent"`
		IP        string
		Current   bool
		LastUsed  int64 `json:"last_used"`
	}
}

func (f *fixture) as(c *http.Cookie, method, url string) *httptest.ResponseRecorder {
	saved := f.Cookie
	defer func() { f.Cookie = saved }()
	f.Cookie = c
	return f.do(method, url, nil)
}

func (f *fixture) login(t *testing.T, name, pw, ua string) *http.Cookie {
	t.Helper()
	w := f.do("POST", "/api/login", body(`{"name":"`+name+`","password":"`+pw+`"}`), "X-No-Auth", "1", "User-Agent", ua)
	if w.Code != 204 {
		t.Fatalf("login %d", w.Code)
	}
	for _, c := range w.Result().Cookies() {
		if c.Name == auth.CookieName {
			return c
		}
	}
	t.Fatal("no session cookie")
	return nil
}

func (f *fixture) sessions(t *testing.T, c *http.Cookie) sessionList {
	t.Helper()
	return decode[sessionList](t, f.as(c, "GET", "/api/sessions"))
}

func TestSessionList(t *testing.T) {
	f := newTestApp(t)
	c := f.login(t, "admin", "pw-pw-pw-pw", "Mozilla/5.0 "+strings.Repeat("x", 300))
	l := f.sessions(t, c).Sessions
	if len(l) != 2 {
		t.Fatalf("sessions %+v", l)
	}
	cur := l[1]
	if !cur.Current || l[0].Current {
		t.Fatalf("current flag %+v", l)
	}
	if len(cur.UserAgent) != 256 || !strings.HasPrefix(cur.UserAgent, "Mozilla/5.0 ") || cur.IP != "192.0.2.1" || cur.LastUsed == 0 {
		t.Fatalf("session %+v", cur)
	}
	if w := f.do("GET", "/api/sessions", nil, "X-No-Auth", "1", "Authorization", "Bearer "+f.Bearer); w.Code != 401 {
		t.Fatalf("app token listed sessions %d", w.Code)
	}
}

func TestSessionOwnership(t *testing.T) {
	f := newTestApp(t)
	h, _ := auth.HashPassword("bob-bob-bob")
	f.App.DB.SetPassword("bob", h)
	bob := f.login(t, "bob", "bob-bob-bob", "curl")
	bl := f.sessions(t, bob).Sessions
	if len(bl) != 1 || !bl[0].Current {
		t.Fatalf("bob sees %+v", bl)
	}
	mine := f.sessions(t, f.Cookie).Sessions[0].ID
	if code := f.as(bob, "DELETE", "/api/sessions/"+itoa(mine)).Code; code != 404 {
		t.Fatalf("foreign delete %d", code)
	}
	if code := f.as(f.Cookie, "DELETE", "/api/sessions/"+itoa(bl[0].ID)).Code; code != 404 {
		t.Fatalf("foreign delete %d", code)
	}
	if code := f.as(bob, "POST", "/api/sessions/revoke-others").Code; code != 204 {
		t.Fatalf("revoke-others %d", code)
	}
	if code := f.as(f.Cookie, "GET", "/api/me").Code; code != 200 {
		t.Fatalf("admin lost session to bob %d", code)
	}
}

func TestSessionDelete(t *testing.T) {
	f := newTestApp(t)
	c := f.login(t, "admin", "pw-pw-pw-pw", "ua")
	l := f.sessions(t, c).Sessions
	if code := f.as(c, "DELETE", "/api/sessions/"+itoa(l[0].ID)).Code; code != 204 {
		t.Fatalf("delete other %d", code)
	}
	if code := f.as(f.Cookie, "GET", "/api/me").Code; code != 401 {
		t.Fatalf("deleted session still works %d", code)
	}
	w := f.as(c, "DELETE", "/api/sessions/"+itoa(l[1].ID))
	if w.Code != 204 || !strings.Contains(w.Header().Get("Set-Cookie"), "Max-Age=0") {
		t.Fatalf("delete current %d %q", w.Code, w.Header().Get("Set-Cookie"))
	}
	if code := f.as(c, "GET", "/api/me").Code; code != 401 {
		t.Fatalf("current session survived %d", code)
	}
}

func TestSessionRevokeOthers(t *testing.T) {
	f := newTestApp(t)
	a := f.login(t, "admin", "pw-pw-pw-pw", "a")
	b := f.login(t, "admin", "pw-pw-pw-pw", "b")
	if code := f.as(b, "POST", "/api/sessions/revoke-others").Code; code != 204 {
		t.Fatalf("revoke-others %d", code)
	}
	for _, c := range []*http.Cookie{f.Cookie, a} {
		if code := f.as(c, "GET", "/api/me").Code; code != 401 {
			t.Fatalf("revoked session works %d", code)
		}
	}
	if l := f.sessions(t, b).Sessions; len(l) != 1 || !l[0].Current || l[0].UserAgent != "b" {
		t.Fatalf("left %+v", l)
	}
	if w := f.do("GET", "/raw/v/", nil, "X-No-Auth", "1", "Authorization", "Bearer "+f.Bearer); w.Code == 401 {
		t.Fatal("revoke-others dropped the app token")
	}
}
