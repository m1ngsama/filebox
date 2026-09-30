package api

import (
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/m1ngsama/filebox/internal/auth"
	"github.com/m1ngsama/filebox/internal/db"
	"github.com/m1ngsama/filebox/internal/httpx"
	"github.com/m1ngsama/filebox/internal/index"
	"github.com/m1ngsama/filebox/internal/serve"
	"github.com/m1ngsama/filebox/internal/version"
	"github.com/m1ngsama/filebox/internal/vol"
)

type Entry struct {
	Name  string `json:"name"`
	Dir   bool   `json:"dir"`
	Size  int64  `json:"size"`
	Mtime int64  `json:"mtime"`
}

type Loc struct {
	Vol  string `json:"vol"`
	Path string `json:"path"`
}

func entry(name string, fi fs.FileInfo) Entry {
	e := Entry{Name: name, Dir: fi.IsDir(), Mtime: fi.ModTime().UnixMilli()}
	if !e.Dir {
		e.Size = fi.Size()
	}
	return e
}

func Stat(root *os.Root, rel string) (Entry, error) {
	fi, err := root.Stat(rel)
	if err != nil {
		return Entry{}, err
	}
	return entry(path.Base(rel), fi), nil
}

type API struct {
	Vols     *vol.Set
	DB       *db.DB
	Auth     *auth.Auth
	Jobs     *Jobs
	Index    *index.Index
	Versions *version.Store
}

func (a *API) Register(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/login", a.login)
	h := func(pattern string, fn http.HandlerFunc) { mux.Handle(pattern, a.Auth.RequireSession(fn)) }
	h("POST /api/logout", a.logout)
	h("GET /api/me", a.me)
	h("GET /api/ls", a.ls)
	h("GET /api/stat", a.stat)
	mux.Handle("GET /api/zip", a.Auth.RequireAny(http.HandlerFunc(a.zip)))
	h("GET /api/recent", a.recent)
	h("GET /api/search", a.search)
	h("GET /api/vols", a.vols)
	h("GET /api/size", a.size)
	h("GET /api/favorites", a.favorites)
	h("POST /api/favorites", a.star)
	h("POST /api/mkdir", a.mkdir)
	h("POST /api/mv", a.mv)
	h("POST /api/cp", a.cp)
	h("POST /api/rm", a.rm)
	h("GET /api/jobs/{id}", a.job)
	h("GET /api/trash", a.trashList)
	h("POST /api/trash/restore", a.trashRestore)
	h("POST /api/trash/empty", a.trashEmpty)
	h("POST /api/trash/delete", a.trashDelete)
	h("GET /api/versions", a.versions)
	h("GET /api/versions/raw", a.versionRaw)
	h("POST /api/versions/restore", a.versionRestore)
	h("POST /api/versions/delete", a.versionDelete)
	h("GET /api/tokens", a.tokens)
	h("POST /api/tokens", a.tokenNew)
	h("DELETE /api/tokens/{id}", a.tokenDel)
	h("GET /api/sessions", a.sessions)
	h("DELETE /api/sessions/{id}", a.sessionDel)
	h("POST /api/sessions/revoke-others", a.sessionsRevokeOthers)
	h("GET /api/activity", a.activity)
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) { httpx.Fail(w, 404, "not found") })
}

func (a *API) resolve(l Loc) (*vol.Volume, string, error) { return a.Vols.Resolve(l.Vol, l.Path) }

func (a *API) query(r *http.Request) (*vol.Volume, string, error) {
	q := r.URL.Query()
	return a.Vols.Resolve(q.Get("vol"), q.Get("path"))
}

func (a *API) login(w http.ResponseWriter, r *http.Request) {
	var in struct{ Name, Password string }
	if err := httpx.Read(r, &in); err != nil {
		httpx.Fail(w, 400, "bad request")
		return
	}
	if in.Name == "" {
		if ns, _ := a.DB.UserNames(); len(ns) == 1 {
			in.Name = ns[0]
		}
	}
	tok, err := a.Auth.Login(in.Name, in.Password, auth.ClientIP(r), r.UserAgent())
	switch {
	case errors.Is(err, auth.ErrRateLimited):
		auth.Refuse(w, err)
	case err != nil:
		httpx.Fail(w, 401, "wrong password")
	default:
		auth.SetCookie(w, r, auth.CookieName, tok, "/", auth.SessionTTL)
		w.WriteHeader(204)
	}
}

func (a *API) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(auth.CookieName); err == nil {
		a.Auth.Logout(c.Value)
	}
	auth.SetCookie(w, r, auth.CookieName, "", "/", -1)
	w.WriteHeader(204)
}

func (a *API) me(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.From(r.Context())
	u, err := a.DB.UserByID(p.UserID)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, 200, map[string]any{"name": u.Name, "vols": a.Vols.Names()})
}

func (a *API) ls(w http.ResponseWriter, r *http.Request) {
	v, rel, err := a.query(r)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	WriteList(w, r, v.Root, rel)
}

func (a *API) stat(w http.ResponseWriter, r *http.Request) {
	v, rel, err := a.query(r)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	e, err := Stat(v.Root, rel)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, 200, e)
}

func (a *API) zip(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	v, ok := a.Vols.Get(q.Get("vol"))
	if !ok {
		httpx.Error(w, fs.ErrNotExist)
		return
	}
	var rels []string
	for _, p := range q["p"] {
		rel, err := v.Clean(p)
		if err != nil {
			httpx.Error(w, err)
			return
		}
		rels = append(rels, rel)
	}
	if len(rels) == 0 {
		httpx.Fail(w, 400, "bad path")
		return
	}
	serve.Zip(w, r, v.Root, rels, "", serve.ZipName(v.Name, rels, q.Get("name")))
}

func (a *API) mkdir(w http.ResponseWriter, r *http.Request) {
	var in Loc
	if err := httpx.Read(r, &in); err != nil {
		httpx.Fail(w, 400, "bad request")
		return
	}
	v, rel, err := a.resolve(in)
	if err == nil && rel == "." {
		err = fs.ErrExist
	}
	if err == nil {
		err = v.Root.Mkdir(rel, 0o755)
	}
	if err != nil {
		httpx.Error(w, err)
		return
	}
	a.Index.Touch(v, rel)
	w.WriteHeader(201)
}

type transfer struct {
	Src Loc `json:"src"`
	Dst Loc `json:"dst"`
}

func (a *API) prepare(w http.ResponseWriter, r *http.Request) (src, dst *vol.Volume, srel, drel string, ok bool) {
	var in transfer
	if err := httpx.Read(r, &in); err != nil {
		httpx.Fail(w, 400, "bad request")
		return
	}
	src, srel, err := a.resolve(in.Src)
	if err == nil {
		dst, drel, err = a.resolve(in.Dst)
	}
	if err != nil {
		httpx.Error(w, err)
		return
	}
	if srel == "." || drel == "." || (src == dst && (drel == srel || strings.HasPrefix(drel, srel+"/"))) {
		httpx.Fail(w, 400, "bad path")
		return
	}
	if _, err := src.Root.Lstat(srel); err != nil {
		httpx.Error(w, err)
		return
	}
	if _, err := dst.Root.Lstat(drel); err == nil {
		httpx.Error(w, fs.ErrExist)
		return
	}
	if fi, err := dst.Root.Stat(path.Dir(drel)); err != nil || !fi.IsDir() {
		httpx.Error(w, fs.ErrNotExist)
		return
	}
	return src, dst, srel, drel, true
}

func (a *API) mv(w http.ResponseWriter, r *http.Request) {
	src, dst, srel, drel, ok := a.prepare(w, r)
	if !ok {
		return
	}
	if src == dst {
		if err := vol.Move(src.Root, srel, drel); err != nil {
			httpx.Error(w, err)
			return
		}
		a.Index.Rename(src, srel, drel)
		w.WriteHeader(204)
		return
	}
	httpx.JSON(w, 202, map[string]string{"job": a.Jobs.Start(src, dst, srel, drel, true)})
}

func (a *API) cp(w http.ResponseWriter, r *http.Request) {
	src, dst, srel, drel, ok := a.prepare(w, r)
	if !ok {
		return
	}
	httpx.JSON(w, 202, map[string]string{"job": a.Jobs.Start(src, dst, srel, drel, false)})
}

func (a *API) rm(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Vol   string   `json:"vol"`
		Paths []string `json:"paths"`
	}
	if err := httpx.Read(r, &in); err != nil {
		httpx.Fail(w, 400, "bad request")
		return
	}
	type failure struct {
		Path   string `json:"path"`
		Status int    `json:"status"`
		Error  string `json:"error"`
	}
	type trashed struct {
		Path string `json:"path"`
		ID   string `json:"id"`
	}
	failed, done := []failure{}, []trashed{}
	for _, p := range in.Paths {
		v, rel, err := a.Vols.Resolve(in.Vol, p)
		id := ""
		if err == nil {
			id, err = Trash(v, rel, time.Now())
		}
		if err != nil {
			code, msg := httpx.Status(err)
			failed = append(failed, failure{p, code, msg})
			continue
		}
		done = append(done, trashed{p, id})
		a.Index.Touch(v, rel)
	}
	httpx.JSON(w, 200, map[string]any{"trashed": done, "failed": failed})
}

func (a *API) recent(w http.ResponseWriter, r *http.Request) {
	limit, err := strconv.Atoi(r.URL.Query().Get("limit"))
	if err != nil || limit <= 0 {
		limit = 200
	}
	fs, err := a.Index.Recent(min(limit, 500))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, 200, map[string]any{"entries": fs, "scanning": !a.Index.Ready()})
}

func (a *API) search(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	in := index.Query{Text: strings.TrimSpace(q.Get("q")), Limit: 200}
	switch {
	case utf8.RuneCountInString(in.Text) < 2:
		httpx.Fail(w, 400, "query too short")
		return
	case strings.ContainsRune(in.Text, 0):
		httpx.Fail(w, 400, "bad query")
		return
	case q.Has("under") && q.Get("vol") == "":
		httpx.Fail(w, 400, "under needs vol")
		return
	}
	if n, err := strconv.Atoi(q.Get("limit")); err == nil && n > 0 {
		in.Limit = min(n, in.Limit)
	}
	if name := q.Get("vol"); name != "" {
		v, rel, err := a.Vols.Resolve(name, q.Get("under"))
		if err != nil {
			httpx.Error(w, err)
			return
		}
		in.Vol, in.Under = v.Name, rel
	}
	hits, err := a.Index.Search(r.Context(), in)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	content, err := a.Index.SearchContent(r.Context(), in)
	if err != nil {
		content = []index.ContentHit{}
	}
	httpx.JSON(w, 200, map[string]any{"entries": hits, "content": content, "indexing": a.Index.Progress(), "scanning": !a.Index.Ready()})
}

func (a *API) vols(w http.ResponseWriter, r *http.Request) {
	type usage struct {
		Name string `json:"name"`
		vol.Usage
	}
	out := []usage{}
	for _, v := range a.Vols.All() {
		u, err := v.Usage()
		if err != nil {
			slog.Warn("volume usage", "vol", v.Name, "err", err)
		}
		out = append(out, usage{v.Name, u})
	}
	httpx.JSON(w, 200, map[string]any{"vols": out})
}

func (a *API) size(w http.ResponseWriter, r *http.Request) {
	v, rel, err := a.query(r)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	s, err := a.Index.Size(v.Name, rel)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, 200, map[string]any{"size": s.Size, "files": s.Files, "scanning": !a.Index.Ready()})
}

type favorite struct {
	Entry
	Vol     string `json:"vol"`
	Path    string `json:"path"`
	Missing bool   `json:"missing"`
}

func (a *API) favorites(w http.ResponseWriter, r *http.Request) {
	fs, err := a.Index.Favorites()
	if err != nil {
		httpx.Error(w, err)
		return
	}
	out := []favorite{}
	for _, f := range fs {
		it := favorite{Entry: Entry{Name: path.Base(f.Path)}, Vol: f.Vol, Path: f.Path, Missing: true}
		if v, ok := a.Vols.Get(f.Vol); ok {
			if e, err := Stat(v.Root, f.Path); err == nil {
				it.Entry, it.Missing = e, false
			}
		}
		out = append(out, it)
	}
	httpx.JSON(w, 200, map[string]any{"entries": out})
}

func (a *API) star(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Vol   string   `json:"vol"`
		Paths []string `json:"paths"`
		Star  bool     `json:"star"`
	}
	if err := httpx.Read(r, &in); err != nil {
		httpx.Fail(w, 400, "bad request")
		return
	}
	rels := make([]string, 0, len(in.Paths))
	for _, p := range in.Paths {
		rel, err := vol.Clean(p)
		if err == nil && rel == "." {
			err = vol.ErrBadPath
		}
		if err == nil && in.Star {
			v, ok := a.Vols.Get(in.Vol)
			if !ok {
				err = fs.ErrNotExist
			} else {
				_, err = v.Root.Lstat(rel)
			}
		}
		if err != nil {
			httpx.Error(w, err)
			return
		}
		rels = append(rels, rel)
	}
	if err := a.Index.Star(in.Vol, rels, in.Star); err != nil {
		httpx.Error(w, err)
		return
	}
	w.WriteHeader(204)
}

func (a *API) job(w http.ResponseWriter, r *http.Request) {
	st, ok := a.Jobs.Get(r.PathValue("id"))
	if !ok {
		httpx.Error(w, fs.ErrNotExist)
		return
	}
	httpx.JSON(w, 200, st)
}

func (a *API) tokens(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.From(r.Context())
	ts, err := a.DB.ListTokens(p.UserID, "app")
	if err != nil {
		httpx.Error(w, err)
		return
	}
	out := []map[string]any{}
	for _, t := range ts {
		out = append(out, map[string]any{"id": t.ID, "label": t.Label, "readonly": t.Scope == "ro",
			"created": t.CreatedAt, "last_used": t.LastUsedAt})
	}
	httpx.JSON(w, 200, map[string]any{"tokens": out})
}

func (a *API) tokenNew(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Label    string `json:"label"`
		Readonly bool   `json:"readonly"`
	}
	if err := httpx.Read(r, &in); err != nil || strings.TrimSpace(in.Label) == "" {
		httpx.Fail(w, 400, "label required")
		return
	}
	p, _ := auth.From(r.Context())
	tok, err := a.Auth.NewAppToken(p.UserID, in.Label, in.Readonly)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	a.DB.Log(db.Event{At: time.Now().Unix(), UserID: p.UserID, Kind: db.EventTokenCreate, Name: in.Label})
	httpx.JSON(w, 201, map[string]string{"token": tok})
}

func (a *API) tokenDel(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpx.Fail(w, 400, "bad id")
		return
	}
	p, _ := auth.From(r.Context())
	label, err := a.DB.DeleteToken(p.UserID, id, "app")
	if err != nil {
		httpx.Error(w, err)
		return
	}
	a.DB.Log(db.Event{At: time.Now().Unix(), UserID: p.UserID, Kind: db.EventTokenRevoke, Name: label})
	w.WriteHeader(204)
}

func (a *API) sessions(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.From(r.Context())
	ts, err := a.DB.ListTokens(p.UserID, "session")
	if err != nil {
		httpx.Error(w, err)
		return
	}
	out := []map[string]any{}
	for _, t := range ts {
		out = append(out, map[string]any{"id": t.ID, "user_agent": t.UserAgent, "ip": t.IP,
			"created": t.CreatedAt, "last_used": max(t.LastUsedAt, t.CreatedAt), "current": t.ID == p.TokenID})
	}
	httpx.JSON(w, 200, map[string]any{"sessions": out})
}

func (a *API) sessionDel(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpx.Fail(w, 400, "bad id")
		return
	}
	p, _ := auth.From(r.Context())
	if _, err := a.DB.DeleteToken(p.UserID, id, "session"); err != nil {
		httpx.Error(w, err)
		return
	}
	if id == p.TokenID {
		auth.SetCookie(w, r, auth.CookieName, "", "/", -1)
	}
	w.WriteHeader(204)
}

func (a *API) sessionsRevokeOthers(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.From(r.Context())
	if _, err := a.DB.DeleteOtherSessions(p.UserID, p.TokenID); err != nil {
		httpx.Error(w, err)
		return
	}
	w.WriteHeader(204)
}

var activityKinds = map[string][]string{
	db.EventDownload: {db.EventDownload}, db.EventUpload: {db.EventUpload},
	"login": {db.EventLogin, db.EventLoginFailed},
	"share": {db.EventShareCreate, db.EventShareEdit, db.EventShareDelete},
	"token": {db.EventTokenCreate, db.EventTokenRevoke},
}

func (a *API) activity(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	p, _ := auth.From(r.Context())
	f := db.EventFilter{UserID: p.UserID, Limit: 51, Kinds: activityKinds[q.Get("kind")]}
	f.ShareID, _ = strconv.ParseInt(q.Get("share"), 10, 64)
	f.Before, _ = strconv.ParseInt(q.Get("before"), 10, 64)
	if q.Get("kind") != "" && f.Kinds == nil {
		httpx.Fail(w, 400, "bad kind")
		return
	}
	es, err := a.DB.Events(f)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	more := len(es) == f.Limit
	if more {
		es = es[:len(es)-1]
	}
	out := []map[string]any{}
	for _, e := range es {
		out = append(out, map[string]any{"id": e.ID, "at": e.At, "kind": e.Kind, "share_id": e.ShareID, "target": e.Target,
			"visitor": e.Visitor, "name": e.Name, "size": e.Size})
	}
	httpx.JSON(w, 200, map[string]any{"events": out, "more": more})
}
