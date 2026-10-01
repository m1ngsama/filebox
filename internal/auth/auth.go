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
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"

	"github.com/m1ngsama/filebox/internal/db"
	"github.com/m1ngsama/filebox/internal/httpx"
)

const (
	CookieName  = "fb_session"
	ShareCookie = "fb_share"
	SessionTTL  = 30 * 24 * time.Hour
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
	DB                *db.DB
	Now               func() time.Time
	lim, share, basic *limiter
}

func New(d *db.DB) *Auth {
	return &Auth{DB: d, Now: time.Now, lim: newLimiter(20), share: newLimiter(20), basic: newLimiter(0)}
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

func (a *Auth) issue(t db.Token, tok string, ttl time.Duration) error {
	now := a.Now()
	t.Hash, t.CreatedAt = Hash(tok), now.Unix()
	if ttl > 0 {
		t.ExpiresAt = now.Add(ttl).Unix()
	}
	return a.DB.InsertToken(&t)
}

func (a *Auth) Login(name, pw, ip, ua string) (string, error) {
	return a.LoginWith(ip, ua, func() (int64, bool) {
		u, err := a.DB.UserByName(name)
		hash := u.PasswordHash
		if err != nil {
			hash = dummyHash()
		}
		return u.ID, CheckPassword(hash, pw) && err == nil
	})
}

func truncate(s string, n int) string {
	s = strings.ToValidUTF8(s, "")
	if len(s) <= n {
		return s
	}
	for n > 0 && !utf8.RuneStart(s[n]) {
		n--
	}
	return s[:n]
}

func (a *Auth) LoginWith(ip, ua string, verify func() (userID int64, ok bool)) (string, error) {
	if err := a.lim.check(ip, a.Now()); err != nil {
		return "", err
	}
	uid, ok := verify()
	now := a.Now().Unix()
	ev := db.Event{At: now, UserID: uid, Kind: db.EventLogin, Visitor: a.DB.Visitor(ip, now)}
	if !ok {
		a.lim.fail(ip, a.Now())
		ev.Kind = db.EventLoginFailed
		a.DB.Log(ev)
		return "", ErrBadLogin
	}
	a.lim.ok(ip)
	a.DB.Log(ev)
	tok := base64.RawURLEncoding.EncodeToString(random(32))
	return tok, a.issue(db.Token{UserID: uid, Kind: "session", UserAgent: truncate(ua, 256), IP: ip}, tok, SessionTTL)
}

func (a *Auth) Logout(tok string) error { return a.DB.DeleteTokenByHash(Hash(tok)) }

var ErrWrongPassword = errors.New("wrong password")

// ChangePassword checks the current password under the login rate limit, so a borrowed unlocked browser cannot
// guess it faster than the sign-in page allows, then signs out every other session.
func (a *Auth) ChangePassword(userID, keep int64, current, next, ip string) error {
	now := a.Now()
	if err := a.lim.check(ip, now); err != nil {
		return err
	}
	u, err := a.DB.UserByID(userID)
	if err != nil || !CheckPassword(u.PasswordHash, current) {
		a.lim.fail(ip, now)
		return ErrWrongPassword
	}
	h, err := HashPassword(next)
	if err != nil {
		return err
	}
	if _, err := a.DB.SetPassword(u.Name, h); err != nil {
		return err
	}
	if _, err := a.DB.DeleteOtherSessions(userID, keep); err != nil {
		return err
	}
	a.DB.Log(db.Event{At: now.Unix(), UserID: userID, Kind: db.EventPassword, Visitor: a.DB.Visitor(ip, now.Unix())})
	return nil
}

func (a *Auth) NewAppToken(userID int64, label string, readOnly bool) (string, error) {
	tok := "fb_" + strings.ToLower(base32.StdEncoding.WithPadding(base32.NoPadding).EncodeToString(random(32)))
	scope := ""
	if readOnly {
		scope = "ro"
	}
	return tok, a.issue(db.Token{UserID: userID, Kind: "app", Label: label, Scope: scope}, tok, 0)
}

func (a *Auth) NewShareToken(userID, shareID int64) (string, error) {
	tok := base64.RawURLEncoding.EncodeToString(random(32))
	return tok, a.issue(db.Token{UserID: userID, Kind: "share", Scope: strconv.FormatInt(shareID, 10)}, tok, shareTTL)
}

func (a *Auth) lookup(tok, kind string) (t db.Token, ok, renewed bool) {
	if tok == "" {
		return
	}
	now := a.Now().Unix()
	t, err := a.DB.TokenByHash(Hash(tok), now)
	if err != nil || t.Kind != kind {
		return db.Token{}, false, false
	}
	switch {
	case kind == "session" && now-max(t.LastUsedAt, t.CreatedAt) > 3600:
		renewed = a.DB.TouchToken(t.ID, now, now+int64(SessionTTL/time.Second)) == nil
	case kind == "app" && now-t.LastUsedAt > 60:
		a.DB.TouchToken(t.ID, now, t.ExpiresAt)
	}
	return t, true, renewed
}

func (a *Auth) Session(r *http.Request) (Principal, bool) { return a.session(nil, r) }

func (a *Auth) session(w http.ResponseWriter, r *http.Request) (Principal, bool) {
	c, err := r.Cookie(CookieName)
	if err != nil {
		return Principal{}, false
	}
	t, ok, renewed := a.lookup(c.Value, "session")
	if renewed && w != nil {
		SetCookie(w, r, CookieName, c.Value, "/", SessionTTL)
	}
	return Principal{UserID: t.UserID, TokenID: t.ID}, ok
}

func (a *Auth) App(r *http.Request) (Principal, bool) {
	tok := ""
	if _, pw, ok := r.BasicAuth(); ok {
		tok = pw
	} else if b, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer "); ok {
		tok = b
	}
	t, ok, _ := a.lookup(tok, "app")
	return Principal{UserID: t.UserID, TokenID: t.ID, ReadOnly: t.Scope == "ro"}, ok
}

func (a *Auth) ShareUnlocked(r *http.Request, shareID int64) bool {
	c, err := r.Cookie(ShareCookie)
	if err != nil {
		return false
	}
	t, ok, _ := a.lookup(c.Value, "share")
	return ok && t.Scope == strconv.FormatInt(shareID, 10)
}

func with(r *http.Request, p Principal) *http.Request {
	return r.WithContext(context.WithValue(r.Context(), ctxKey{}, p))
}

func (a *Auth) RequireSession(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, ok := a.session(w, r)
		if !ok {
			httpx.Fail(w, 401, "unauthorized")
			return
		}
		h.ServeHTTP(w, with(r, p))
	})
}

func (a *Auth) RequireAny(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, ok := a.session(w, r)
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
			if err := a.basic.check(ip, a.Now()); err != nil {
				w.Header().Set("Retry-After", strconv.Itoa(seconds(err)))
				http.Error(w, err.Error(), 429)
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
		if x := strings.TrimSpace(r.Header.Get("X-Real-IP")); x != "" {
			return x
		}
		if xs := strings.Split(r.Header.Get("X-Forwarded-For"), ","); strings.TrimSpace(xs[len(xs)-1]) != "" {
			return strings.TrimSpace(xs[len(xs)-1])
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

func (a *Auth) LoginLimit(ip string) error { return a.lim.check(ip, a.Now()) }
func (a *Auth) ShareLimit(ip string) error { return a.share.check(ip, a.Now()) }
func (a *Auth) ShareFailed(ip string)      { a.share.fail(ip, a.Now()) }

func seconds(err error) int {
	var l *Limited
	if !errors.As(err, &l) {
		return 1
	}
	return max(1, int((l.Wait+time.Second-1)/time.Second))
}

func Refuse(w http.ResponseWriter, err error) {
	s := seconds(err)
	w.Header().Set("Retry-After", strconv.Itoa(s))
	httpx.JSON(w, 429, map[string]any{"error": err.Error(), "retry_after": s})
}
