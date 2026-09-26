package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/m1ngsama/filebox/internal/db"
	"github.com/m1ngsama/filebox/internal/httpx"
)

const (
	CookieName  = "fb_session"
	ShareCookie = "fb_share"
	sessionTTL  = 30 * 24 * time.Hour
	shareTTL    = 24 * time.Hour
)

var (
	ErrBadLogin    = errors.New("bad login")
	ErrRateLimited = errors.New("rate limited")
)

type Principal struct {
	UserID, TokenID int64
	ReadOnly        bool
}

type ctxKey struct{}

func From(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(ctxKey{}).(Principal)
	return p, ok
}

type Auth struct {
	DB         *db.DB
	Now        func() time.Time
	lim, basic *limiter
}

func New(d *db.DB) *Auth {
	return &Auth{DB: d, Now: time.Now, lim: newLimiter(20), basic: newLimiter(0)}
}

func HashPassword(pw string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(pw), bcrypt.DefaultCost)
	return string(b), err
}

func CheckPassword(hash, pw string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(pw)) == nil
}

func Hash(tok string) string {
	s := sha256.Sum256([]byte(tok))
	return hex.EncodeToString(s[:])
}

var dummyHash = sync.OnceValue(func() string {
	h, _ := HashPassword(base64.RawURLEncoding.EncodeToString(random(32)))
	return h
})

func random(n int) []byte {
	b := make([]byte, n)
	rand.Read(b)
	return b
}

func (a *Auth) issue(userID int64, kind, label, scope, tok string, ttl time.Duration) error {
	now := a.Now()
	t := &db.Token{UserID: userID, Kind: kind, Hash: Hash(tok), Label: label, Scope: scope,
		CreatedAt: now.Unix(), LastUsedAt: now.Unix()}
	if ttl > 0 {
		t.ExpiresAt = now.Add(ttl).Unix()
	}
	return a.DB.InsertToken(t)
}

func (a *Auth) Login(name, pw, ip string) (string, error) {
	if !a.lim.allow(ip, a.Now()) {
		return "", ErrRateLimited
	}
	u, err := a.DB.UserByName(name)
	hash := u.PasswordHash
	if err != nil {
		hash = dummyHash()
	}
	if !CheckPassword(hash, pw) || err != nil {
		a.lim.fail(ip, a.Now())
		return "", ErrBadLogin
	}
	a.lim.ok(ip)
	tok := base64.RawURLEncoding.EncodeToString(random(32))
	return tok, a.issue(u.ID, "session", "", "", tok, sessionTTL)
}

func (a *Auth) Logout(tok string) error { return a.DB.DeleteTokenByHash(Hash(tok)) }

func (a *Auth) NewAppToken(userID int64, label string, readOnly bool) (string, error) {
	tok := "fb_" + strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(random(32)))
	scope := ""
	if readOnly {
		scope = "ro"
	}
	return tok, a.issue(userID, "app", label, scope, tok, 0)
}

func (a *Auth) NewShareToken(userID, shareID int64) (string, error) {
	tok := base64.RawURLEncoding.EncodeToString(random(32))
	return tok, a.issue(userID, "share", "", strconv.FormatInt(shareID, 10), tok, shareTTL)
}

func (a *Auth) lookup(tok, kind string) (db.Token, bool) {
	if tok == "" {
		return db.Token{}, false
	}
	now := a.Now().Unix()
	t, err := a.DB.TokenByHash(Hash(tok), now)
	if err != nil || t.Kind != kind {
		return db.Token{}, false
	}
	switch {
	case kind == "session" && now-t.LastUsedAt > 3600:
		a.DB.TouchToken(t.ID, now, now+int64(sessionTTL/time.Second))
	case kind == "app" && now-t.LastUsedAt > 60:
		a.DB.TouchToken(t.ID, now, t.ExpiresAt)
	}
	return t, true
}

func (a *Auth) Session(r *http.Request) (Principal, bool) {
	c, err := r.Cookie(CookieName)
	if err != nil {
		return Principal{}, false
	}
	t, ok := a.lookup(c.Value, "session")
	return Principal{UserID: t.UserID, TokenID: t.ID}, ok
}

func (a *Auth) App(r *http.Request) (Principal, bool) {
	tok := ""
	if _, pw, ok := r.BasicAuth(); ok {
		tok = pw
	} else if b, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "); ok {
		tok = b
	}
	t, ok := a.lookup(tok, "app")
	return Principal{UserID: t.UserID, TokenID: t.ID, ReadOnly: t.Scope == "ro"}, ok
}

func (a *Auth) ShareUnlocked(r *http.Request, shareID int64) bool {
	c, err := r.Cookie(ShareCookie)
	if err != nil {
		return false
	}
	t, ok := a.lookup(c.Value, "share")
	return ok && t.Scope == strconv.FormatInt(shareID, 10)
}

func with(r *http.Request, p Principal) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), ctxKey{}, p))
}

func (a *Auth) RequireSession(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, ok := a.Session(r)
		if !ok {
			httpx.Fail(w, 401, "unauthorized")
			return
		}
		h.ServeHTTP(w, with(r, p))
	})
}

func (a *Auth) RequireAny(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, ok := a.Session(r)
		if !ok {
			p, ok = a.App(r)
		}
		if !ok {
			httpx.Fail(w, 401, "unauthorized")
			return
		}
		h.ServeHTTP(w, with(r, p))
	})
}

func (a *Auth) RequireBasic(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if p, ok := a.App(r); ok {
			h.ServeHTTP(w, with(r, p))
			return
		}
		if _, _, sent := r.BasicAuth(); sent {
			ip := ClientIP(r)
			if !a.basic.allow(ip, a.Now()) {
				http.Error(w, "too many attempts", 429)
				return
			}
			a.basic.fail(ip, a.Now())
		}
		w.Header().Set("WWW-Authenticate", `Basic realm="filebox", charset="UTF-8"`)
		http.Error(w, "unauthorized", 401)
	})
}

func fromLoopback(r *http.Request) bool {
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func ClientIP(r *http.Request) string {
	if fromLoopback(r) {
		if x := r.Header.Get("X-Real-IP"); x != "" {
			return x
		}
	}
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	return host
}

func IsHTTPS(r *http.Request) bool {
	return r.TLS != nil || (fromLoopback(r) && r.Header.Get("X-Forwarded-Proto") == "https")
}

func SetCookie(w http.ResponseWriter, r *http.Request, name, value, path string, ttl time.Duration) {
	c := &http.Cookie{Name: name, Value: value, Path: path, HttpOnly: true,
		Secure: IsHTTPS(r), SameSite: http.SameSiteLaxMode, MaxAge: int(ttl / time.Second)}
	if ttl < 0 {
		c.MaxAge = -1
	}
	http.SetCookie(w, c)
}

func (a *Auth) Throttled(ip string) bool { return !a.lim.allow(ip, a.Now()) }
func (a *Auth) Failed(ip string)         { a.lim.fail(ip, a.Now()) }
