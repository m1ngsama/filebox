package auth

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/m1ngsama/filebox/internal/db"
)

type clock struct{ t time.Time }

func (c *clock) now() time.Time { return c.t }

func setup(t *testing.T) (*Auth, *clock, int64) {
	t.Helper()
	d, err := db.Open(filepath.Join(t.TempDir(), "a.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	h, _ := HashPassword("correct horse")
	uid, _ := d.SetPassword("admin", h)
	a := New(d)
	c := &clock{time.Unix(1_000_000, 0)}
	a.Now = c.now
	return a, c, uid
}

func withCookie(tok string) *http.Request {
	r := httptest.NewRequest("GET", "/", nil)
	r.AddCookie(&http.Cookie{Name: CookieName, Value: tok})
	return r
}

func TestLoginAndSession(t *testing.T) {
	a, c, uid := setup(t)
	if _, err := a.Login("admin", "wrong", "1.1.1.1"); !errors.Is(err, ErrBadLogin) {
		t.Fatalf("err = %v", err)
	}
	tok, err := a.Login("admin", "correct horse", "1.1.1.1")
	if err != nil {
		t.Fatal(err)
	}
	p, ok := a.Session(withCookie(tok))
	if !ok || p.UserID != uid {
		t.Fatal("session not accepted")
	}
	c.t = c.t.Add(29 * 24 * time.Hour)
	if _, ok := a.Session(withCookie(tok)); !ok {
		t.Fatal("session expired early")
	}
	c.t = c.t.Add(29 * 24 * time.Hour)
	if _, ok := a.Session(withCookie(tok)); !ok {
		t.Fatal("sliding renewal did not extend session")
	}
	a.Logout(tok)
	if _, ok := a.Session(withCookie(tok)); ok {
		t.Fatal("session survived logout")
	}
}

func TestRateLimit(t *testing.T) {
	a, c, _ := setup(t)
	for i := 0; i < 5; i++ {
		a.Login("admin", "wrong", "2.2.2.2")
	}
	if _, err := a.Login("admin", "correct horse", "2.2.2.2"); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("err = %v, want rate limited", err)
	}
	if _, err := a.Login("admin", "correct horse", "3.3.3.3"); err != nil {
		t.Fatal("other IP was blocked")
	}
	c.t = c.t.Add(20 * time.Minute)
	if _, err := a.Login("admin", "correct horse", "2.2.2.2"); err != nil {
		t.Fatalf("still blocked after backoff: %v", err)
	}
}

func TestAppTokens(t *testing.T) {
	a, _, uid := setup(t)
	tok, err := a.NewAppToken(uid, "laptop", true)
	if err != nil || len(tok) < 40 || tok[:3] != "fb_" {
		t.Fatalf("tok %q %v", tok, err)
	}
	r := httptest.NewRequest("PROPFIND", "/dav/", nil)
	r.SetBasicAuth("anything", tok)
	p, ok := a.App(r)
	if !ok || !p.ReadOnly {
		t.Fatalf("basic: %+v %v", p, ok)
	}
	r = httptest.NewRequest("GET", "/raw/x", nil)
	r.Header.Set("Authorization", "Bearer "+tok)
	if _, ok := a.App(r); !ok {
		t.Fatal("bearer rejected")
	}
	ts, _ := a.DB.ListTokens(uid, "app")
	a.DB.DeleteToken(uid, ts[0].ID)
	if _, ok := a.App(r); ok {
		t.Fatal("revoked token accepted")
	}
	// a session token must not work as an app token
	st, _ := a.Login("admin", "correct horse", "1.1.1.1")
	r.Header.Set("Authorization", "Bearer "+st)
	if _, ok := a.App(r); ok {
		t.Fatal("session token accepted as app token")
	}
}

func TestRequireBasic(t *testing.T) {
	a, _, _ := setup(t)
	h := a.RequireBasic(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("PROPFIND", "/dav/", nil))
	if w.Code != 401 || w.Header().Get("WWW-Authenticate") == "" {
		t.Fatalf("code %d hdr %q", w.Code, w.Header().Get("WWW-Authenticate"))
	}
}

func TestShareUnlock(t *testing.T) {
	a, _, uid := setup(t)
	tok, _ := a.NewShareToken(uid, 7)
	r := httptest.NewRequest("GET", "/s/x/ls", nil)
	r.AddCookie(&http.Cookie{Name: ShareCookie, Value: tok})
	if !a.ShareUnlocked(r, 7) {
		t.Fatal("unlock cookie rejected")
	}
	if a.ShareUnlocked(r, 8) {
		t.Fatal("cookie for share 7 unlocked share 8")
	}
}

func TestClientIP(t *testing.T) {
	r := httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "127.0.0.1:5555"
	r.Header.Set("X-Real-IP", "9.9.9.9")
	if ClientIP(r) != "9.9.9.9" {
		t.Fatal("loopback proxy header ignored")
	}
	r.RemoteAddr = "192.168.31.5:5555"
	if ClientIP(r) != "192.168.31.5" {
		t.Fatal("spoofed header trusted from LAN")
	}
}

func TestSetCookie(t *testing.T) {
	w := httptest.NewRecorder()
	r := httptest.NewRequest("GET", "/", nil)
	SetCookie(w, r, "name", "value", "/path", time.Hour)
	c := w.Result().Cookies()[0]
	if !c.HttpOnly || c.SameSite != http.SameSiteLaxMode || c.Path != "/path" || c.MaxAge != 3600 {
		t.Fatalf("cookie = %+v", c)
	}
	if c.Secure {
		t.Fatal("secure on plain http")
	}

	w = httptest.NewRecorder()
	r = httptest.NewRequest("GET", "/", nil)
	r.RemoteAddr = "127.0.0.1:1234"
	r.Header.Set("X-Forwarded-Proto", "https")
	SetCookie(w, r, "name", "value", "/", time.Hour)
	if c := w.Result().Cookies()[0]; !c.Secure {
		t.Fatal("not secure behind loopback https proxy")
	}

	w = httptest.NewRecorder()
	r = httptest.NewRequest("GET", "/", nil)
	SetCookie(w, r, "name", "value", "/", -1)
	if c := w.Result().Cookies()[0]; c.MaxAge >= 0 {
		t.Fatalf("MaxAge = %d, want negative", c.MaxAge)
	}
}

func TestRequireSession(t *testing.T) {
	a, _, uid := setup(t)
	var gotUID int64
	h := a.RequireSession(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, _ := From(r.Context())
		gotUID = p.UserID
	}))

	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "/", nil))
	if w.Code != 401 || w.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("code %d content-type %q", w.Code, w.Header().Get("Content-Type"))
	}

	tok, _ := a.Login("admin", "correct horse", "1.1.1.1")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, withCookie(tok))
	if w.Code != 200 || gotUID != uid {
		t.Fatalf("code %d uid %d", w.Code, gotUID)
	}
}

func TestRequireAnyBearerToken(t *testing.T) {
	a, _, uid := setup(t)
	tok, _ := a.NewAppToken(uid, "cli", false)
	var gotUID int64
	h := a.RequireAny(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, _ := From(r.Context())
		gotUID = p.UserID
	}))
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("Authorization", "Bearer "+tok)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	if w.Code != 200 || gotUID != uid {
		t.Fatalf("code %d uid %d", w.Code, gotUID)
	}
}
