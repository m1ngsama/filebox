package app

import (
	"net/http"
	"strings"
	"testing"

	"github.com/m1ngsama/filebox/internal/auth"
)

func TestChangePassword(t *testing.T) {
	f := newTestApp(t)
	other, _ := f.App.Auth.Login("admin", "pw-pw-pw-pw", "127.0.0.2", "phone")
	phone := (&http.Cookie{Name: auth.CookieName, Value: other}).String()
	if w := f.do("GET", "/api/me", nil, "X-No-Auth", "1", "Cookie", phone); w.Code != 200 {
		t.Fatalf("other session before the change %d", w.Code)
	}
	if w := f.do("POST", "/api/password", body(`{"current":"wrong-wrong","next":"brand-new-pw"}`)); w.Code != 403 {
		t.Fatalf("wrong current %d", w.Code)
	}
	if w := f.do("POST", "/api/password", body(`{"current":"pw-pw-pw-pw","next":"short"}`)); w.Code != 400 {
		t.Fatalf("short %d", w.Code)
	}
	if w := f.do("POST", "/api/password", body(`{"current":"pw-pw-pw-pw","next":"`+strings.Repeat("x", 73)+`"}`)); w.Code != 400 {
		t.Fatalf("over bcrypt's limit %d", w.Code)
	}
	if w := f.do("POST", "/api/password", body(`{"current":"pw-pw-pw-pw","next":"brand-new-pw"}`)); w.Code != 204 {
		t.Fatalf("change %d %s", w.Code, w.Body)
	}
	if w := f.do("GET", "/api/me", nil); w.Code != 200 {
		t.Fatalf("this session was signed out %d", w.Code)
	}
	r := f.do("GET", "/api/me", nil, "X-No-Auth", "1", "Cookie", phone)
	if r.Code != 401 {
		t.Fatalf("other session still in %d", r.Code)
	}
	if w := f.do("POST", "/api/login", body(`{"password":"pw-pw-pw-pw"}`), "X-No-Auth", "1"); w.Code != 401 {
		t.Fatalf("old password %d", w.Code)
	}
	if w := f.do("POST", "/api/login", body(`{"password":"brand-new-pw"}`), "X-No-Auth", "1"); w.Code != 204 {
		t.Fatalf("new password %d", w.Code)
	}
	limited := false
	for range 20 {
		if w := f.do("POST", "/api/password", body(`{"current":"guess-guess","next":"brand-new-pw"}`)); w.Code == 429 {
			limited = true
			break
		}
	}
	if !limited {
		t.Fatal("guessing the current password is not rate limited")
	}
}
