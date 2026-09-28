package auth

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

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
	if _, err := a.Login("admin", "wrong", "1.1.1.1", ""); !errors.Is(err, ErrBadLogin) {
		t.Fatalf("err = %v", err)
	}
	tok, err := a.Login("admin", "correct horse", "1.1.1.1", "")
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
		a.Login("admin", "wrong", "2.2.2.2", "")
	}
	if _, err := a.Login("admin", "correct horse", "2.2.2.2", ""); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("err = %v, want rate limited", err)
	}
	if _, err := a.Login("admin", "correct horse", "3.3.3.3", ""); err != nil {
		t.Fatal("other IP was blocked")
	}
	c.t = c.t.Add(20 * time.Minute)
	if _, err := a.Login("admin", "correct horse", "2.2.2.2", ""); err != nil {
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
	a.DB.DeleteToken(uid, ts[0].ID, "app")
	if _, ok := a.App(r); ok {
		t.Fatal("revoked token accepted")
	}
	st, _ := a.Login("admin", "correct horse", "1.1.1.1", "")
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

	tok, _ := a.Login("admin", "correct horse", "1.1.1.1", "")
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

func dav(a *Auth, ip, tok string) int {
	h := a.RequireBasic(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	r := httptest.NewRequest("PROPFIND", "/dav/", nil)
	r.RemoteAddr = ip + ":1234"
	r.SetBasicAuth("me", tok)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w.Code
}

func TestBasicAndLoginLimitsSeparate(t *testing.T) {
	a, _, uid := setup(t)
	tok, _ := a.NewAppToken(uid, "dav", false)
	for range 5 {
		a.Login("admin", "wrong", "4.4.4.4", "")
	}
	if c := dav(a, "4.4.4.4", tok); c != 200 {
		t.Fatalf("valid token after bad web logins %d", c)
	}
	for range 6 {
		dav(a, "5.5.5.5", "fb_stale")
	}
	if c := dav(a, "5.5.5.5", "fb_stale"); c != 429 {
		t.Fatalf("bad basic attempts not limited: %d", c)
	}
	if c := dav(a, "5.5.5.5", tok); c != 200 {
		t.Fatalf("valid token throttled %d", c)
	}
	if _, err := a.Login("admin", "correct horse", "5.5.5.5", ""); err != nil {
		t.Fatalf("bad basic attempts blocked web login: %v", err)
	}
}

func TestGlobalFailureBudget(t *testing.T) {
	a, c, _ := setup(t)
	for i := range 21 {
		a.Login("admin", "wrong", "10.0.0."+strconv.Itoa(i), "")
	}
	if _, err := a.Login("admin", "correct horse", "10.0.1.1", ""); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("err = %v, want rate limited", err)
	}
	if a.LoginLimit("10.0.1.2") == nil {
		t.Fatal("passkey login not covered by the global budget")
	}
	if a.ShareLimit("10.0.1.2") != nil {
		t.Fatal("bad logins throttled share unlock")
	}
	c.t = c.t.Add(time.Minute)
	if _, err := a.Login("admin", "correct horse", "10.0.1.1", ""); err != nil {
		t.Fatalf("still limited after the window: %v", err)
	}
}

func TestLimiterFloodKeepsBans(t *testing.T) {
	l := newLimiter(0)
	now := time.Unix(1_000_000, 0)
	for range 5 {
		l.fail("6.6.6.6", now)
	}
	for i := range maxEntries + 10 {
		l.fail("f"+strconv.Itoa(i), now)
	}
	if l.check("6.6.6.6", now) == nil {
		t.Fatal("flood lifted an existing ban")
	}
	if len(l.m) > maxEntries {
		t.Fatalf("map grew to %d", len(l.m))
	}
	later := now.Add(time.Hour)
	l.fail("7.7.7.7", later)
	if len(l.m) != 1 {
		t.Fatalf("expired entries kept: %d", len(l.m))
	}
}

func TestSessionCookieRenewed(t *testing.T) {
	a, c, _ := setup(t)
	tok, _ := a.Login("admin", "correct horse", "1.1.1.1", "")
	h := a.RequireSession(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	serve := func() *http.Cookie {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, withCookie(tok))
		if w.Code != 200 {
			t.Fatalf("code %d", w.Code)
		}
		if cs := w.Result().Cookies(); len(cs) > 0 {
			return cs[0]
		}
		return nil
	}
	if ck := serve(); ck != nil {
		t.Fatalf("cookie re-sent without renewal: %+v", ck)
	}
	c.t = c.t.Add(2 * time.Hour)
	ck := serve()
	if ck == nil || ck.Name != CookieName || ck.Value != tok || ck.MaxAge != int(SessionTTL/time.Second) || ck.Path != "/" {
		t.Fatalf("renewed cookie %+v", ck)
	}
}

func TestAppTokenNeverUsed(t *testing.T) {
	a, c, uid := setup(t)
	tok, _ := a.NewAppToken(uid, "fresh", false)
	if ts, _ := a.DB.ListTokens(uid, "app"); len(ts) != 1 || ts[0].LastUsedAt != 0 {
		t.Fatalf("new token %+v", ts)
	}
	r := httptest.NewRequest("GET", "/", nil)
	r.Header.Set("Authorization", "Bearer "+tok)
	a.App(r)
	if ts, _ := a.DB.ListTokens(uid, "app"); ts[0].LastUsedAt != c.t.Unix() {
		t.Fatalf("used token %+v", ts)
	}
}

func TestRefuseSaysWhichLimit(t *testing.T) {
	a, c, _ := setup(t)
	refuse := func(err error) (string, string) {
		w := httptest.NewRecorder()
		Refuse(w, err)
		if w.Code != 429 {
			t.Fatalf("code %d", w.Code)
		}
		return w.Header().Get("Retry-After"), strings.TrimSpace(w.Body.String())
	}
	for range 5 {
		a.Login("admin", "wrong", "2.2.2.2", "")
	}
	_, err := a.Login("admin", "correct horse", "2.2.2.2", "")
	if h, b := refuse(err); h != "1" || b != `{"error":"too many attempts","retry_after":1}` {
		t.Fatalf("per-IP %q %s", h, b)
	}
	for i := range 21 {
		a.Login("admin", "wrong", "10.0.0."+strconv.Itoa(i), "")
	}
	c.t = c.t.Add(15 * time.Second)
	_, err = a.Login("admin", "correct horse", "10.0.1.1", "")
	if h, b := refuse(err); h != "45" || b != `{"error":"login paused","retry_after":45}` {
		t.Fatalf("global %q %s", h, b)
	}
	if err := a.ShareLimit("10.0.1.1"); err != nil {
		t.Fatalf("share unlock paused by bad logins: %v", err)
	}
}

func TestSessionUserAgentTruncatedOnRune(t *testing.T) {
	a, _, uid := setup(t)
	if _, err := a.Login("admin", "correct horse", "1.1.1.1", "x"+strings.Repeat("é", 200)+"\xff"); err != nil {
		t.Fatal(err)
	}
	ts, _ := a.DB.ListTokens(uid, "session")
	ua := ts[0].UserAgent
	if len(ua) != 255 || !utf8.ValidString(ua) {
		t.Fatalf("user agent %d bytes, valid %v", len(ua), utf8.ValidString(ua))
	}
}
