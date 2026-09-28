package app

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/fxamacker/cbor/v2"

	"github.com/m1ngsama/filebox/internal/auth"
	"github.com/m1ngsama/filebox/internal/passkey"
)

const pkHost = "files.test"

var b64u = base64.RawURLEncoding

type softKey struct {
	key            *ecdsa.PrivateKey
	id, handle     []byte
	count          uint32
	origin, rpHost string
	noUV           bool
}

func newSoftKey(t *testing.T) *softKey {
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return &softKey{key: k, id: []byte(rand.Text()), origin: "https://" + pkHost, rpHost: pkHost}
}

func (k *softKey) clientData(typ, challenge string) []byte {
	b, _ := json.Marshal(map[string]any{"type": typ, "challenge": challenge, "origin": k.origin, "crossOrigin": false})
	return b
}

func (k *softKey) authData(flags byte, extra ...byte) []byte {
	if k.noUV {
		flags &^= 0x04
	}
	rp := sha256.Sum256([]byte(k.rpHost))
	b := append(rp[:], flags)
	b = binary.BigEndian.AppendUint32(b, k.count)
	return append(b, extra...)
}

func (k *softKey) create(t *testing.T, challenge string) json.RawMessage {
	pub, err := k.key.PublicKey.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	cose, _ := cbor.Marshal(map[int]any{1: 2, 3: -7, -1: 1, -2: pub[1:33], -3: pub[33:]})
	cred := append(make([]byte, 16), byte(len(k.id)>>8), byte(len(k.id)))
	cred = append(append(cred, k.id...), cose...)
	att, _ := cbor.Marshal(map[string]any{"fmt": "none", "attStmt": map[string]any{}, "authData": k.authData(0x45, cred...)})
	b, _ := json.Marshal(map[string]any{"id": b64u.EncodeToString(k.id), "rawId": b64u.EncodeToString(k.id), "type": "public-key",
		"response": map[string]string{"clientDataJSON": b64u.EncodeToString(k.clientData("webauthn.create", challenge)),
			"attestationObject": b64u.EncodeToString(att)},
		"clientExtensionResults": map[string]any{}})
	return b
}

func (k *softKey) get(t *testing.T, challenge string) json.RawMessage {
	cd := k.clientData("webauthn.get", challenge)
	ad := k.authData(0x05)
	h := sha256.Sum256(cd)
	digest := sha256.Sum256(append(ad, h[:]...))
	sig, err := ecdsa.SignASN1(rand.Reader, k.key, digest[:])
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(map[string]any{"id": b64u.EncodeToString(k.id), "rawId": b64u.EncodeToString(k.id), "type": "public-key",
		"response": map[string]string{"clientDataJSON": b64u.EncodeToString(cd), "authenticatorData": b64u.EncodeToString(ad),
			"signature": b64u.EncodeToString(sig), "userHandle": b64u.EncodeToString(k.handle)},
		"clientExtensionResults": map[string]any{}})
	return b
}

func withPasskeys(t *testing.T) (*fixture, *passkey.Service) {
	t.Helper()
	f := newTestApp(t)
	s, err := passkey.New(f.App.DB, f.App.Auth, []string{"https://" + pkHost})
	if err != nil {
		t.Fatal(err)
	}
	f.App.Passkeys = s
	f.H = f.App.Handler()
	return f, s
}

func (f *fixture) pk(method, url string, body any, c *http.Cookie) *httptest.ResponseRecorder {
	return f.pkFrom("", method, url, body, c)
}

func (f *fixture) pkFrom(ip, method, url string, body any, c *http.Cookie) *httptest.ResponseRecorder {
	var s string
	if body != nil {
		b, _ := json.Marshal(body)
		s = string(b)
	}
	r := httptest.NewRequest(method, "https://"+pkHost+url, strings.NewReader(s))
	if ip != "" {
		r.RemoteAddr = ip + ":1234"
	}
	if c != nil {
		r.AddCookie(c)
	}
	w := httptest.NewRecorder()
	f.H.ServeHTTP(w, r)
	return w
}

type begun struct {
	Ceremony string
	Options  struct {
		Challenge string
		User      struct{ ID string }
	}
}

func (f *fixture) register(t *testing.T, k *softKey, c *http.Cookie, name string) map[string]any {
	t.Helper()
	w := f.pk("POST", "/api/passkeys/register/begin", nil, c)
	if w.Code != 200 {
		t.Fatalf("register begin %d %s", w.Code, w.Body)
	}
	b := decode[begun](t, w)
	k.handle, _ = b64u.DecodeString(b.Options.User.ID)
	body := map[string]any{"ceremony": b.Ceremony, "name": name, "response": k.create(t, b.Options.Challenge)}
	if w := f.pk("POST", "/api/passkeys/register/finish", body, c); w.Code != 201 {
		t.Fatalf("register finish %d %s", w.Code, w.Body)
	}
	return body
}

func (f *fixture) assertion(t *testing.T, k *softKey) map[string]any {
	t.Helper()
	w := f.pk("POST", "/api/passkeys/login/begin", nil, nil)
	if w.Code != 200 {
		t.Fatalf("login begin %d %s", w.Code, w.Body)
	}
	b := decode[begun](t, w)
	return map[string]any{"ceremony": b.Ceremony, "response": k.get(t, b.Options.Challenge)}
}

func (f *fixture) finish(body map[string]any) *httptest.ResponseRecorder {
	return f.pk("POST", "/api/passkeys/login/finish", body, nil)
}

type passkeyList struct {
	Passkeys []struct {
		ID        int64
		Name      string
		Created   int64
		Last_used int64
	}
}

func TestPasskeysDisabledWithoutOrigin(t *testing.T) {
	f := newTestApp(t)
	if w := f.do("GET", "/api/passkeys/enabled", nil, "X-No-Auth", "1"); w.Code != 200 || !strings.Contains(w.Body.String(), `"enabled":false`) {
		t.Fatalf("enabled %d %s", w.Code, w.Body)
	}
	for _, c := range [][2]string{{"POST", "/api/passkeys/login/begin"}, {"POST", "/api/passkeys/login/finish"},
		{"POST", "/api/passkeys/register/begin"}, {"GET", "/api/passkeys"}, {"DELETE", "/api/passkeys/1"}} {
		if w := f.do(c[0], c[1], body("{}")); w.Code != 404 {
			t.Errorf("%s %s = %d", c[0], c[1], w.Code)
		}
	}
}

func TestPasskeyOriginMustMatch(t *testing.T) {
	f, _ := withPasskeys(t)
	if w := f.pk("GET", "/api/passkeys/enabled", nil, nil); !strings.Contains(w.Body.String(), `"enabled":true`) {
		t.Fatalf("enabled %s", w.Body)
	}
	if w := f.do("GET", "/api/passkeys/enabled", nil); !strings.Contains(w.Body.String(), `"enabled":false`) {
		t.Fatalf("unlisted host enabled %s", w.Body)
	}
	if w := f.do("POST", "/api/passkeys/login/begin", nil); w.Code != 404 {
		t.Fatalf("unlisted host begin %d", w.Code)
	}
	r := httptest.NewRequest("POST", "http://"+pkHost+"/api/passkeys/login/begin", nil)
	w := httptest.NewRecorder()
	f.H.ServeHTTP(w, r)
	if w.Code != 404 {
		t.Fatalf("plain http begin %d", w.Code)
	}
	for _, bad := range []string{"http://files.test", "https://1.2.3.4", "ftp://x", "https://a.test/path", "localhost", "https://a.test:443", "http://localhost:80"} {
		if _, err := passkey.New(f.App.DB, f.App.Auth, []string{bad}); err == nil {
			t.Errorf("origin %q accepted", bad)
		}
	}
	if _, err := passkey.New(f.App.DB, f.App.Auth, []string{"http://localhost:5298"}); err != nil {
		t.Errorf("localhost origin rejected: %v", err)
	}
}

func TestPasskeyRoundTrip(t *testing.T) {
	f, _ := withPasskeys(t)
	k := newSoftKey(t)
	f.register(t, k, f.Cookie, "MacBook")
	l := decode[passkeyList](t, f.pk("GET", "/api/passkeys", nil, f.Cookie))
	if len(l.Passkeys) != 1 || l.Passkeys[0].Name != "MacBook" || l.Passkeys[0].Created == 0 || l.Passkeys[0].Last_used != 0 {
		t.Fatalf("%+v", l)
	}
	w := f.pk("POST", "/api/passkeys/register/begin", nil, f.Cookie)
	if !strings.Contains(w.Body.String(), `"excludeCredentials":[{"type":"public-key","id":"`+b64u.EncodeToString(k.id)) {
		t.Fatalf("existing credential not excluded: %s", w.Body)
	}
	if !strings.Contains(w.Body.String(), `"residentKey":"required"`) || !strings.Contains(w.Body.String(), `"userVerification":"required"`) {
		t.Fatalf("authenticator selection: %s", w.Body)
	}
	k.count = 1
	w = f.finish(f.assertion(t, k))
	if w.Code != 204 {
		t.Fatalf("login %d %s", w.Code, w.Body)
	}
	var sess *http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Name == auth.CookieName && c.HttpOnly && c.Secure {
			sess = c
		}
	}
	if sess == nil {
		t.Fatalf("no session cookie: %v", w.Header())
	}
	if w := f.pk("GET", "/api/me", nil, sess); w.Code != 200 || !strings.Contains(w.Body.String(), `"admin"`) {
		t.Fatalf("me %d %s", w.Code, w.Body)
	}
	l = decode[passkeyList](t, f.pk("GET", "/api/passkeys", nil, sess))
	if l.Passkeys[0].Last_used == 0 {
		t.Fatal("last_used not updated")
	}
	id := l.Passkeys[0].ID
	if w := f.pk("PATCH", "/api/passkeys/"+itoa(id), map[string]string{"name": strings.Repeat("长", 65)}, sess); w.Code != 400 {
		t.Fatalf("long rename %d", w.Code)
	}
	if w := f.pk("PATCH", "/api/passkeys/"+itoa(id), map[string]string{"name": strings.Repeat("长", 64)}, sess); w.Code != 204 {
		t.Fatalf("64-char rename %d", w.Code)
	}
	if w := f.pk("PATCH", "/api/passkeys/"+itoa(id), map[string]string{"name": "iPhone / Safari"}, sess); w.Code != 204 {
		t.Fatalf("rename %d %s", w.Code, w.Body)
	}
	if w := f.pk("PATCH", "/api/passkeys/"+itoa(id), map[string]string{"name": " "}, sess); w.Code != 400 {
		t.Fatalf("blank rename %d", w.Code)
	}
	if l := decode[passkeyList](t, f.pk("GET", "/api/passkeys", nil, sess)); l.Passkeys[0].Name != "iPhone / Safari" {
		t.Fatalf("%+v", l)
	}
	if w := f.pk("DELETE", "/api/passkeys/"+itoa(id), nil, sess); w.Code != 204 {
		t.Fatalf("delete %d", w.Code)
	}
	k.count = 2
	if w := f.finish(f.assertion(t, k)); w.Code != 401 {
		t.Fatalf("deleted passkey logged in: %d", w.Code)
	}
}

func TestPasskeyChallengeSingleUse(t *testing.T) {
	f, _ := withPasskeys(t)
	k := newSoftKey(t)
	reg := f.register(t, k, f.Cookie, "a")
	if w := f.pk("POST", "/api/passkeys/register/finish", reg, f.Cookie); w.Code != 400 {
		t.Fatalf("reused registration ceremony %d", w.Code)
	}
	k.count = 1
	a := f.assertion(t, k)
	if w := f.pkFrom("10.1.2.3", "POST", "/api/passkeys/login/finish", a, nil); w.Code != 204 {
		t.Fatalf("login %d %s", w.Code, w.Body)
	}
	if ts, _ := f.App.DB.ListTokens(f.UserID, "session"); ts[len(ts)-1].IP != "10.1.2.3" {
		t.Fatalf("passkey session %+v", ts[len(ts)-1])
	}
	if w := f.finish(a); w.Code != 401 {
		t.Fatalf("replayed assertion %d", w.Code)
	}
}

func TestPasskeyRegistrationCeremonyBoundToUser(t *testing.T) {
	f, _ := withPasskeys(t)
	h, _ := auth.HashPassword("bob-bob-bob")
	f.App.DB.SetPassword("bob", h)
	bob, _ := f.App.Auth.Login("bob", "bob-bob-bob", "10.9.9.9", "")
	bc := &http.Cookie{Name: auth.CookieName, Value: bob}
	k := newSoftKey(t)
	b := decode[begun](t, f.pk("POST", "/api/passkeys/register/begin", nil, f.Cookie))
	body := map[string]any{"ceremony": b.Ceremony, "name": "x", "response": k.create(t, b.Options.Challenge)}
	if w := f.pk("POST", "/api/passkeys/register/finish", body, bc); w.Code != 400 {
		t.Fatalf("foreign ceremony %d", w.Code)
	}
}

func TestPasskeyCeremonyExpires(t *testing.T) {
	f, s := withPasskeys(t)
	now := time.Now()
	s.Now = func() time.Time { return now }
	k := newSoftKey(t)
	f.register(t, k, f.Cookie, "a")
	k.count = 1
	a := f.assertion(t, k)
	now = now.Add(5*time.Minute + time.Second)
	if w := f.finish(a); w.Code != 401 {
		t.Fatalf("expired ceremony %d", w.Code)
	}
	b := decode[begun](t, f.pk("POST", "/api/passkeys/register/begin", nil, f.Cookie))
	now = now.Add(6 * time.Minute)
	body := map[string]any{"ceremony": b.Ceremony, "name": "x", "response": newSoftKey(t).create(t, b.Options.Challenge)}
	if w := f.pk("POST", "/api/passkeys/register/finish", body, f.Cookie); w.Code != 400 {
		t.Fatalf("expired registration %d", w.Code)
	}
}

func TestPasskeyForeignUser(t *testing.T) {
	f, _ := withPasskeys(t)
	f.register(t, newSoftKey(t), f.Cookie, "mine")
	id := decode[passkeyList](t, f.pk("GET", "/api/passkeys", nil, f.Cookie)).Passkeys[0].ID
	h, _ := auth.HashPassword("bob-bob-bob")
	f.App.DB.SetPassword("bob", h)
	bob, _ := f.App.Auth.Login("bob", "bob-bob-bob", "10.9.9.9", "")
	bc := &http.Cookie{Name: auth.CookieName, Value: bob}
	if l := decode[passkeyList](t, f.pk("GET", "/api/passkeys", nil, bc)); len(l.Passkeys) != 0 {
		t.Fatalf("bob sees %+v", l)
	}
	if w := f.pk("DELETE", "/api/passkeys/"+itoa(id), nil, bc); w.Code != 404 {
		t.Fatalf("foreign delete %d", w.Code)
	}
	if w := f.pk("PATCH", "/api/passkeys/"+itoa(id), map[string]string{"name": "x"}, bc); w.Code != 404 {
		t.Fatalf("foreign rename %d", w.Code)
	}
	if l := decode[passkeyList](t, f.pk("GET", "/api/passkeys", nil, f.Cookie)); len(l.Passkeys) != 1 || l.Passkeys[0].Name != "mine" {
		t.Fatalf("%+v", l)
	}
}

func TestPasskeySignCountRegression(t *testing.T) {
	f, _ := withPasskeys(t)
	k := newSoftKey(t)
	f.register(t, k, f.Cookie, "a")
	k.count = 5
	if w := f.finish(f.assertion(t, k)); w.Code != 204 {
		t.Fatalf("login %d %s", w.Code, w.Body)
	}
	k.count = 3
	if w := f.finish(f.assertion(t, k)); w.Code != 401 {
		t.Fatalf("regressed sign count %d", w.Code)
	}
}

func TestPasskeyFailuresHitLimiter(t *testing.T) {
	f, _ := withPasskeys(t)
	k := newSoftKey(t)
	f.register(t, k, f.Cookie, "a")
	other := newSoftKey(t)
	other.handle = k.handle
	for range 5 {
		if w := f.finish(f.assertion(t, other)); w.Code != 401 {
			t.Fatalf("bad assertion %d", w.Code)
		}
	}
	if w := f.pk("POST", "/api/passkeys/login/begin", nil, nil); w.Code != 429 {
		t.Fatalf("begin after failures %d", w.Code)
	}
	if w := f.finish(map[string]any{"ceremony": "x", "response": json.RawMessage("{}")}); w.Code != 429 {
		t.Fatalf("finish after failures %d", w.Code)
	}
	if w := f.pk("POST", "/api/login", map[string]string{"name": "admin", "password": "pw-pw-pw-pw"}, nil); w.Code != 429 {
		t.Fatalf("password login after passkey failures %d", w.Code)
	}
}

func TestPasskeyRequiresUserVerification(t *testing.T) {
	f, _ := withPasskeys(t)
	if w := f.pk("POST", "/api/passkeys/login/begin", nil, nil); !strings.Contains(w.Body.String(), `"userVerification":"required"`) {
		t.Fatalf("login options %s", w.Body)
	}
	weak := newSoftKey(t)
	weak.noUV = true
	b := decode[begun](t, f.pk("POST", "/api/passkeys/register/begin", nil, f.Cookie))
	body := map[string]any{"ceremony": b.Ceremony, "name": "x", "response": weak.create(t, b.Options.Challenge)}
	if w := f.pk("POST", "/api/passkeys/register/finish", body, f.Cookie); w.Code != 400 {
		t.Fatalf("registration without UV %d", w.Code)
	}
	k := newSoftKey(t)
	f.register(t, k, f.Cookie, "a")
	k.count, k.noUV = 1, true
	if w := f.finish(f.assertion(t, k)); w.Code != 401 {
		t.Fatalf("assertion without UV %d", w.Code)
	}
	k.count, k.noUV = 2, false
	if w := f.finish(f.assertion(t, k)); w.Code != 204 {
		t.Fatalf("assertion with UV %d", w.Code)
	}
}

func TestPasskeyDuplicateCredential(t *testing.T) {
	f, _ := withPasskeys(t)
	k := newSoftKey(t)
	f.register(t, k, f.Cookie, "a")
	b := decode[begun](t, f.pk("POST", "/api/passkeys/register/begin", nil, f.Cookie))
	body := map[string]any{"ceremony": b.Ceremony, "name": "b", "response": k.create(t, b.Options.Challenge)}
	if w := f.pk("POST", "/api/passkeys/register/finish", body, f.Cookie); w.Code != 409 {
		t.Fatalf("duplicate credential %d %s", w.Code, w.Body)
	}
}

func TestPasskeyCeremonyFloodIsPerIP(t *testing.T) {
	f, _ := withPasskeys(t)
	k := newSoftKey(t)
	f.register(t, k, f.Cookie, "a")
	first := decode[begun](t, f.pkFrom("198.51.100.7", "POST", "/api/passkeys/login/begin", nil, nil))
	for range 1100 {
		if w := f.pkFrom("198.51.100.7", "POST", "/api/passkeys/login/begin", nil, nil); w.Code != 200 {
			t.Fatalf("flooding begin %d", w.Code)
		}
	}
	k.count = 1
	body := map[string]any{"ceremony": first.Ceremony, "response": k.get(t, first.Options.Challenge)}
	if w := f.pkFrom("198.51.100.7", "POST", "/api/passkeys/login/finish", body, nil); w.Code != 401 {
		t.Fatalf("evicted ceremony %d", w.Code)
	}
	k.count = 2
	if w := f.finish(f.assertion(t, k)); w.Code != 204 {
		t.Fatalf("other IP blocked by flood: %d %s", w.Code, w.Body)
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }
