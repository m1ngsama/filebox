package passkey

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"

	"github.com/m1ngsama/filebox/internal/auth"
	"github.com/m1ngsama/filebox/internal/db"
	"github.com/m1ngsama/filebox/internal/httpx"
)

const (
	ceremonyTTL   = 5 * time.Minute
	maxCeremonies = 1000
	maxPerIP      = 8
)

type Service struct {
	DB   *db.DB
	Auth *auth.Auth
	Now  func() time.Time

	wa      *webauthn.WebAuthn
	origins map[string]rp

	mu         sync.Mutex
	ceremonies map[string]ceremony
}

type ceremony struct {
	data    webauthn.SessionData
	userID  int64
	ip      string
	expires time.Time
}

type rp struct{ id, origin string }

var defaultPorts = map[string]string{"https": "443", "http": "80"}

func New(d *db.DB, a *auth.Auth, origins []string) (*Service, error) {
	s := &Service{DB: d, Auth: a, Now: time.Now, origins: map[string]rp{}, ceremonies: map[string]ceremony{}}
	for _, o := range origins {
		u, err := url.Parse(o)
		if err != nil || u.Host == "" || strings.TrimSuffix(u.Path, "/") != "" || u.RawQuery != "" ||
			!(u.Scheme == "https" || u.Scheme == "http" && u.Hostname() == "localhost") ||
			u.Port() == defaultPorts[u.Scheme] ||
			protocol.ValidateRPID(u.Hostname()) != nil {
			return nil, fmt.Errorf("bad origin %q: want https://host, or http://localhost for testing", o)
		}
		s.origins[u.Host] = rp{u.Hostname(), u.Scheme + "://" + u.Host}
	}
	if len(s.origins) == 0 {
		return s, nil
	}
	var all []string
	for _, p := range s.origins {
		all = append(all, p.origin)
	}
	wa, err := webauthn.New(&webauthn.Config{RPDisplayName: "filebox", RPOrigins: all})
	if err != nil {
		return nil, err
	}
	s.wa = wa
	return s, nil
}

func (s *Service) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/passkeys/enabled", func(w http.ResponseWriter, r *http.Request) {
		_, ok := s.rp(r)
		httpx.JSON(w, 200, map[string]bool{"enabled": ok})
	})
	if s.wa == nil {
		return
	}
	mux.HandleFunc("POST /api/passkeys/login/begin", s.loginBegin)
	mux.HandleFunc("POST /api/passkeys/login/finish", s.loginFinish)
	h := func(pattern string, fn http.HandlerFunc) { mux.Handle(pattern, s.Auth.RequireSession(fn)) }
	h("POST /api/passkeys/register/begin", s.registerBegin)
	h("POST /api/passkeys/register/finish", s.registerFinish)
	h("GET /api/passkeys", s.list)
	h("PATCH /api/passkeys/{id}", s.rename)
	h("DELETE /api/passkeys/{id}", s.remove)
}

func (s *Service) rp(r *http.Request) (rp, bool) {
	p, ok := s.origins[r.Host]
	return p, ok && (strings.HasPrefix(p.origin, "http:") || auth.IsHTTPS(r))
}

func (s *Service) Sweep() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.expire(s.Now())
}

func (s *Service) expire(now time.Time) {
	maps.DeleteFunc(s.ceremonies, func(_ string, c ceremony) bool { return !now.Before(c.expires) })
}

func (s *Service) put(data *webauthn.SessionData, userID int64, ip string) (string, bool) {
	now := s.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.expire(now)
	n, oldest := 0, ""
	for id, c := range s.ceremonies {
		if c.ip == ip {
			n++
			if oldest == "" || c.expires.Before(s.ceremonies[oldest].expires) {
				oldest = id
			}
		}
	}
	if n >= maxPerIP {
		delete(s.ceremonies, oldest)
	}
	if len(s.ceremonies) >= maxCeremonies {
		return "", false
	}
	id := rand.Text()
	s.ceremonies[id] = ceremony{data: *data, userID: userID, ip: ip, expires: now.Add(ceremonyTTL)}
	return id, true
}

func (s *Service) take(id string, userID int64) (webauthn.SessionData, bool) {
	s.mu.Lock()
	c, ok := s.ceremonies[id]
	delete(s.ceremonies, id)
	s.mu.Unlock()
	return c.data, ok && c.userID == userID && s.Now().Before(c.expires)
}

type user struct {
	db.User
	handle []byte
	rows   []db.Passkey
	creds  []webauthn.Credential
}

func (u *user) WebAuthnID() []byte                         { return u.handle }
func (u *user) WebAuthnName() string                       { return u.Name }
func (u *user) WebAuthnDisplayName() string                { return u.Name }
func (u *user) WebAuthnCredentials() []webauthn.Credential { return u.creds }

func (s *Service) load(u db.User) (*user, error) {
	fresh := make([]byte, 64)
	rand.Read(fresh)
	h, err := s.DB.WebAuthnID(u.ID, fresh)
	if err != nil {
		return nil, err
	}
	rows, err := s.DB.ListPasskeys(u.ID)
	if err != nil {
		return nil, err
	}
	out := &user{User: u, handle: h, rows: rows}
	for _, p := range rows {
		var c webauthn.Credential
		if err := json.Unmarshal([]byte(p.Credential), &c); err != nil {
			return nil, err
		}
		out.creds = append(out.creds, c)
	}
	return out, nil
}

func (s *Service) loadID(id int64) (*user, error) {
	u, err := s.DB.UserByID(id)
	if err != nil {
		return nil, err
	}
	return s.load(u)
}

type begun struct {
	Ceremony string `json:"ceremony"`
	Options  any    `json:"options"`
}

func (s *Service) loginBegin(w http.ResponseWriter, r *http.Request) {
	p, ok := s.rp(r)
	if !ok {
		httpx.Fail(w, 404, "not found")
		return
	}
	if err := s.Auth.LoginLimit(auth.ClientIP(r)); err != nil {
		auth.Refuse(w, err)
		return
	}
	opts, data, err := s.wa.BeginDiscoverableLogin(webauthn.WithLoginRelyingPartyID(p.id), webauthn.WithLoginOrigin(p.origin),
		webauthn.WithUserVerification(protocol.VerificationRequired))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	id, ok := s.put(data, 0, auth.ClientIP(r))
	if !ok {
		httpx.Fail(w, 429, "too many attempts")
		return
	}
	httpx.JSON(w, 200, begun{id, opts.Response})
}

func (s *Service) loginFinish(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.rp(r); !ok {
		httpx.Fail(w, 404, "not found")
		return
	}
	var in struct {
		Ceremony string          `json:"ceremony"`
		Response json.RawMessage `json:"response"`
	}
	readErr := httpx.Read(r, &in)
	tok, err := s.Auth.LoginWith(auth.ClientIP(r), r.UserAgent(), func() (int64, bool) {
		return s.verify(in.Ceremony, in.Response, readErr == nil)
	})
	switch {
	case errors.Is(err, auth.ErrRateLimited):
		auth.Refuse(w, err)
	case err != nil:
		httpx.Fail(w, 401, "unauthorized")
	default:
		auth.SetCookie(w, r, auth.CookieName, tok, "/", auth.SessionTTL)
		w.WriteHeader(204)
	}
}

func (s *Service) verify(id string, resp []byte, ok bool) (int64, bool) {
	data, found := s.take(id, 0)
	if !ok || !found {
		return 0, false
	}
	parsed, err := protocol.ParseCredentialRequestResponseBytes(resp)
	if err != nil {
		return 0, false
	}
	var u *user
	_, cred, err := s.wa.ValidatePasskeyLogin(func(_, handle []byte) (webauthn.User, error) {
		dbu, err := s.DB.UserByWebAuthnID(handle)
		if err == nil {
			u, err = s.load(dbu)
		}
		if err != nil {
			return nil, err
		}
		return u, nil
	}, data, parsed)
	if err != nil || cred.Authenticator.CloneWarning || !parsed.Response.AuthenticatorData.Flags.HasUserVerified() {
		return 0, false
	}
	i := slices.IndexFunc(u.rows, func(p db.Passkey) bool { return bytes.Equal(p.CredentialID, cred.ID) })
	b, err := json.Marshal(cred)
	if i < 0 || err != nil || s.DB.UsePasskey(u.rows[i].ID, string(b), s.Now().Unix()) != nil {
		return 0, false
	}
	return u.ID, true
}

func (s *Service) registerBegin(w http.ResponseWriter, r *http.Request) {
	p, ok := s.rp(r)
	if !ok {
		httpx.Fail(w, 404, "not found")
		return
	}
	pr, _ := auth.From(r.Context())
	u, err := s.loadID(pr.UserID)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	opts, data, err := s.wa.BeginRegistration(u,
		webauthn.WithRegistrationRelyingPartyID(p.id), webauthn.WithRegistrationOrigin(p.origin),
		webauthn.WithAuthenticatorSelection(protocol.AuthenticatorSelection{
			ResidentKey: protocol.ResidentKeyRequirementRequired, RequireResidentKey: protocol.ResidentKeyRequired(),
			UserVerification: protocol.VerificationRequired}),
		webauthn.WithExclusions(webauthn.Credentials(u.creds).CredentialDescriptors()))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	id, ok := s.put(data, pr.UserID, auth.ClientIP(r))
	if !ok {
		httpx.Fail(w, 429, "too many attempts")
		return
	}
	httpx.JSON(w, 200, begun{id, opts.Response})
}

func validName(n string) (string, bool) {
	n = strings.TrimSpace(n)
	return n, n != "" && utf8.RuneCountInString(n) <= 64
}

func (s *Service) registerFinish(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.rp(r); !ok {
		httpx.Fail(w, 404, "not found")
		return
	}
	var in struct {
		Ceremony string          `json:"ceremony"`
		Name     string          `json:"name"`
		Response json.RawMessage `json:"response"`
	}
	err := httpx.Read(r, &in)
	name, ok := validName(in.Name)
	if err != nil || !ok {
		httpx.Fail(w, 400, "bad request")
		return
	}
	pr, _ := auth.From(r.Context())
	data, ok := s.take(in.Ceremony, pr.UserID)
	var cred *webauthn.Credential
	u, err := s.loadID(pr.UserID)
	if ok && err == nil {
		var parsed *protocol.ParsedCredentialCreationData
		if parsed, err = protocol.ParseCredentialCreationResponseBytes(in.Response); err == nil {
			cred, err = s.wa.CreateCredential(u, data, parsed)
		}
	}
	if !ok || err != nil {
		httpx.Fail(w, 400, "invalid passkey")
		return
	}
	b, err := json.Marshal(cred)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	pk := &db.Passkey{UserID: pr.UserID, CredentialID: cred.ID, Credential: string(b), Name: name, CreatedAt: s.Now().Unix()}
	if err := s.DB.InsertPasskey(pk); err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, 201, map[string]int64{"id": pk.ID})
}

func (s *Service) list(w http.ResponseWriter, r *http.Request) {
	pr, _ := auth.From(r.Context())
	ps, err := s.DB.ListPasskeys(pr.UserID)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	out := []map[string]any{}
	for _, p := range ps {
		out = append(out, map[string]any{"id": p.ID, "name": p.Name, "created": p.CreatedAt, "last_used": p.LastUsedAt})
	}
	httpx.JSON(w, 200, map[string]any{"passkeys": out})
}

func (s *Service) rename(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	var in struct {
		Name string `json:"name"`
	}
	if err == nil {
		err = httpx.Read(r, &in)
	}
	name, ok := validName(in.Name)
	if err != nil || !ok {
		httpx.Fail(w, 400, "bad request")
		return
	}
	pr, _ := auth.From(r.Context())
	if err := s.DB.RenamePasskey(pr.UserID, id, name); err != nil {
		httpx.Error(w, err)
		return
	}
	w.WriteHeader(204)
}

func (s *Service) remove(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpx.Fail(w, 400, "bad id")
		return
	}
	pr, _ := auth.From(r.Context())
	if err := s.DB.DeletePasskey(pr.UserID, id); err != nil {
		httpx.Error(w, err)
		return
	}
	w.WriteHeader(204)
}
