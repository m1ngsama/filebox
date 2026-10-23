package api

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"net/http"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/m1ngsama/filebox/internal/auth"
	"github.com/m1ngsama/filebox/internal/db"
	"github.com/m1ngsama/filebox/internal/httpx"
)

const (
	defaultInviteHours = 7 * 24
	maxInviteHours     = 30 * 24
)

// admin lets only an admin through.
func (a *API) admin(fn http.HandlerFunc) http.Handler {
	return a.Auth.RequireSession(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if p, _ := auth.From(r.Context()); !p.Admin {
			httpx.Fail(w, 403, "forbidden")
			return
		}
		fn(w, r)
	}))
}

func validUserName(s string) bool {
	n := utf8.RuneCountInString(s)
	return n >= 1 && n <= 32 && s == strings.TrimSpace(s) && !strings.ContainsFunc(s, unicode.IsControl) && !strings.ContainsAny(s, "/\\:")
}

// goodGrants checks a map of volume to level: every volume must be served, and every level real.
func (a *API) goodGrants(grants map[string]string, allowEmpty bool) bool {
	names := a.Vols.Names()
	for v, l := range grants {
		if l == "" && allowEmpty {
			continue
		}
		if !db.ValidLevel(l) || !slices.Contains(names, v) {
			return false
		}
	}
	return true
}

func (a *API) team(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.From(r.Context())
	now := time.Now().Unix()
	as, err := a.DB.Accounts()
	if err != nil {
		httpx.Error(w, err)
		return
	}
	users := []map[string]any{}
	for _, u := range as {
		users = append(users, map[string]any{"id": u.ID, "name": u.Name, "role": u.Role, "access": u.Access, "created": u.CreatedAt, "self": u.ID == p.UserID})
	}
	ins, err := a.DB.Invites(now)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	invites := []map[string]any{}
	for _, in := range ins {
		invites = append(invites, map[string]any{"id": in.ID, "kind": in.Kind, "user": in.UserName, "role": in.Role, "access": in.Grants, "created": in.CreatedAt, "expires": in.ExpiresAt})
	}
	httpx.JSON(w, 200, map[string]any{"users": users, "invites": invites, "vols": a.Vols.Names()})
}

func pathID(r *http.Request) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	return id, err == nil
}

func (a *API) teamEdit(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	var in struct {
		Role   *string           `json:"role"`
		Access map[string]string `json:"access"`
	}
	if !ok || httpx.Read(r, &in) != nil || (in.Role != nil && *in.Role != db.RoleAdmin && *in.Role != db.RoleMember) || !a.goodGrants(in.Access, true) {
		httpx.Fail(w, 400, "bad request")
		return
	}
	if _, _, err := a.DB.Access(id); err != nil {
		httpx.Error(w, err)
		return
	}
	if in.Role != nil {
		if err := a.DB.SetRole(id, *in.Role); errors.Is(err, db.ErrLastAdmin) {
			httpx.Fail(w, 409, "there must be at least one admin")
			return
		} else if err != nil {
			httpx.Error(w, err)
			return
		}
	}
	for v, l := range in.Access {
		if err := a.DB.SetAccess(id, v, l); err != nil {
			httpx.Error(w, err)
			return
		}
	}
	a.Auth.Forget(id)
	w.WriteHeader(204)
}

func (a *API) teamDel(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		httpx.Fail(w, 400, "bad id")
		return
	}
	if p, _ := auth.From(r.Context()); p.UserID == id {
		httpx.Fail(w, 400, "you cannot remove yourself")
		return
	}
	if err := a.DB.DeleteUser(id); errors.Is(err, db.ErrLastAdmin) {
		httpx.Fail(w, 409, "there must be at least one admin")
		return
	} else if err != nil {
		httpx.Error(w, err)
		return
	}
	a.Auth.Forget(id)
	w.WriteHeader(204)
}

func newInviteToken() string {
	b := make([]byte, 24)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func (a *API) invite(w http.ResponseWriter, r *http.Request, in db.Invite, hours int) {
	p, _ := auth.From(r.Context())
	now := time.Now().Unix()
	tok := newInviteToken()
	in.Hash, in.CreatedBy, in.CreatedAt, in.ExpiresAt = auth.Hash(tok), p.UserID, now, now+int64(hours)*3600
	id, err := a.DB.AddInvite(in)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, 201, map[string]any{"id": id, "token": tok, "expires": in.ExpiresAt})
}

func (a *API) inviteNew(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Role   string            `json:"role"`
		Access map[string]string `json:"access"`
		Hours  int               `json:"hours"`
	}
	if httpx.Read(r, &in) != nil || (in.Role != db.RoleAdmin && in.Role != db.RoleMember) || !a.goodGrants(in.Access, false) || in.Hours < 0 || in.Hours > maxInviteHours {
		httpx.Fail(w, 400, "bad request")
		return
	}
	if in.Hours == 0 {
		in.Hours = defaultInviteHours
	}
	a.invite(w, r, db.Invite{Kind: db.InviteJoin, Role: in.Role, Grants: in.Access}, in.Hours)
}

func (a *API) teamReset(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		httpx.Fail(w, 400, "bad id")
		return
	}
	if _, _, err := a.DB.Access(id); err != nil {
		httpx.Error(w, err)
		return
	}
	a.invite(w, r, db.Invite{Kind: db.InviteReset, UserID: id, Role: db.RoleMember}, 24)
}

func (a *API) inviteDel(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(r)
	if !ok {
		httpx.Fail(w, 400, "bad id")
		return
	}
	if err := a.DB.DeleteInvite(id); err != nil {
		httpx.Error(w, err)
		return
	}
	w.WriteHeader(204)
}

// inviteInfo tells the person holding a link what it is for, before they commit to it.
func (a *API) inviteInfo(w http.ResponseWriter, r *http.Request) {
	in, ok := a.openInvite(w, r)
	if !ok {
		return
	}
	httpx.JSON(w, 200, map[string]any{"kind": in.Kind, "user": in.UserName, "role": in.Role, "access": in.Grants})
}

func (a *API) openInvite(w http.ResponseWriter, r *http.Request) (db.Invite, bool) {
	ip := auth.ClientIP(r)
	if err := a.Auth.ShareLimit(ip); err != nil {
		auth.Refuse(w, err)
		return db.Invite{}, false
	}
	in, err := a.DB.InviteByHash(auth.Hash(r.PathValue("token")), time.Now().Unix())
	if err != nil {
		a.Auth.ShareFailed(ip)
		httpx.Fail(w, 404, "not found")
		return db.Invite{}, false
	}
	return in, true
}

// inviteUse spends a link: it creates the account, or sets the new password, and signs the person in.
func (a *API) inviteUse(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name     string `json:"name"`
		Password string `json:"password"`
	}
	if httpx.Read(r, &body) != nil {
		httpx.Fail(w, 400, "bad request")
		return
	}
	in, ok := a.openInvite(w, r)
	if !ok {
		return
	}
	if utf8.RuneCountInString(body.Password) < 8 || (in.Kind == db.InviteJoin && !validUserName(body.Name)) {
		httpx.Fail(w, 400, "bad request")
		return
	}
	hash, err := auth.HashPassword(body.Password)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	uid, _, err := a.DB.UseInvite(auth.Hash(r.PathValue("token")), time.Now().Unix(), body.Name, hash)
	switch {
	case errors.Is(err, db.ErrConflict):
		httpx.Fail(w, 409, "name taken")
		return
	case errors.Is(err, db.ErrGone):
		httpx.Fail(w, 404, "not found")
		return
	case err != nil:
		httpx.Error(w, err)
		return
	}
	a.Auth.Forget(uid)
	if in.Kind == db.InviteJoin {
		a.DB.Log(db.Event{At: time.Now().Unix(), UserID: uid, Kind: db.EventJoin, Name: body.Name})
	}
	tok, err := a.Auth.LoginWith(auth.ClientIP(r), r.UserAgent(), func() (int64, bool) { return uid, true })
	if err != nil {
		httpx.Error(w, err)
		return
	}
	auth.SetCookie(w, r, auth.CookieName, tok, "/", auth.SessionTTL)
	w.WriteHeader(204)
}
