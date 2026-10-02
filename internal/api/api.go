package api

import (
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path"
	"slices"
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
	Items *int   `json:"items,omitempty"`
	Bytes *int64 `json:"bytes,omitempty"`
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
	Origins  []string
}

func (a *API) Register(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/login", a.loginInfo)
	mux.HandleFunc("POST /api/login", a.login)
	h := func(pattern string, fn http.HandlerFunc) { mux.Handle(pattern, a.Auth.RequireSession(fn)) }
	h("POST /api/logout", a.logout)
	h("GET /api/me", a.me)
	h("GET /api/ls", a.ls)
	h("GET /api/stat", a.stat)
	mux.Handle("GET /api/zip", a.Auth.RequireAny(http.HandlerFunc(a.zip)))
	mux.Handle("GET /api/zip-entries", a.Auth.RequireAny(http.HandlerFunc(a.zipEntries)))
	mux.Handle("GET /api/zip-entry", a.Auth.RequireAny(http.HandlerFunc(a.zipEntry)))
	h("GET /api/recent", a.recent)
	h("GET /api/search", a.search)
	h("GET /api/vols", a.vols)
	h("GET /api/size", a.size)
	h("GET /api/favorites", a.favorites)
	h("POST /api/favorites", a.star)
	h("PUT /api/file", a.save)
	h("POST /api/mkdir", a.mkdir)
	h("POST /api/touch", a.touch)
	h("POST /api/unzip", a.unzip)
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
	h("POST /api/password", a.password)
	h("GET /api/activity", a.activity)
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) { httpx.Fail(w, 404, "not found") })
}

func (a *API) did(r *http.Request, e db.Event) {
	p, _ := auth.From(r.Context())
	e.At, e.UserID = time.Now().Unix(), p.UserID
	a.DB.Log(e)
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

func (a *API) loginInfo(w http.ResponseWriter, r *http.Request) {
	ns, err := a.DB.UserNames()
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, 200, map[string]bool{"single": len(ns) == 1})
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
	httpx.JSON(w, 200, map[string]any{"name": u.Name, "vols": a.Vols.Names(), "origins": append([]string{}, a.Origins...)})
}

func (a *API) ls(w http.ResponseWriter, r *http.Request) {
	v, rel, err := a.query(r)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	WriteList(w, r, v.Root, rel, func(es []Entry) {
		if !a.Index.Ready() {
			return
		}
		for i, e := range es {
			if e.Dir {
				if sz, err := a.Index.Size(v.Name, path.Join(rel, e.Name)); err == nil {
					es[i].Bytes = &sz.Size
				}
			}
		}
	})
	a.Index.Reconcile(v, rel)
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
	a.did(r, db.Event{Kind: db.EventCreate, Vol: v.Name, Path: rel})
	e, err := Stat(v.Root, rel)
	if err != nil {
		e = Entry{Name: path.Base(rel), Dir: true, Mtime: time.Now().UnixMilli()}
	}
	httpx.JSON(w, 201, e)
}

// touch creates an empty file, never over an existing one, for a note started in the browser.
func (a *API) touch(w http.ResponseWriter, r *http.Request) {
	var in Loc
	if err := httpx.Read(r, &in); err != nil {
		httpx.Fail(w, 400, "bad request")
		return
	}
	v, rel, err := a.resolve(in)
	if err == nil && (rel == "." || !vol.ValidName(path.Base(rel))) {
		err = vol.ErrBadPath
	}
	var f *os.File
	if err == nil {
		f, err = v.Root.OpenFile(rel, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	}
	if err != nil {
		httpx.Error(w, err)
		return
	}
	f.Close()
	a.Index.Touch(v, rel)
	a.did(r, db.Event{Kind: db.EventCreate, Vol: v.Name, Path: rel})
	e, err := Stat(v.Root, rel)
	if err != nil {
		e = Entry{Name: path.Base(rel), Mtime: time.Now().UnixMilli()}
	}
	httpx.JSON(w, 201, e)
}

// unzip unpacks an archive into a new folder beside it, named after the archive.
func (a *API) unzip(w http.ResponseWriter, r *http.Request) {
	var in Loc
	if err := httpx.Read(r, &in); err != nil {
		httpx.Fail(w, 400, "bad request")
		return
	}
	v, rel, err := a.resolve(in)
	if err == nil && !strings.EqualFold(path.Ext(rel), ".zip") {
		err = vol.ErrNotFile
	}
	if err != nil {
		httpx.Error(w, err)
		return
	}
	dir, base := path.Dir(rel), strings.TrimSuffix(path.Base(rel), path.Ext(rel))
	name := base
	for i := 2; ; i++ {
		if _, err := v.Root.Lstat(path.Join(dir, name)); errors.Is(err, fs.ErrNotExist) {
			break
		}
		name = fmt.Sprintf("%s %d", base, i)
	}
	drel := path.Join(dir, name)
	done := func() { a.did(r, db.Event{Kind: db.EventCreate, Vol: v.Name, Path: drel, Name: path.Base(rel)}) }
	httpx.JSON(w, 202, map[string]string{"job": a.Jobs.Unzip(v, rel, drel, done), "name": name})
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
		kind := db.EventMove
		if path.Dir(srel) == path.Dir(drel) {
			kind = db.EventRename
		}
		a.did(r, db.Event{Kind: kind, Vol: dst.Name, Path: drel, Name: srel})
		w.WriteHeader(204)
		return
	}
	done := func() {
		a.did(r, db.Event{Kind: db.EventMove, Vol: dst.Name, Path: drel, Name: srel, Target: src.Name})
	}
	httpx.JSON(w, 202, map[string]string{"job": a.Jobs.Start(src, dst, srel, drel, true, done)})
}

func (a *API) cp(w http.ResponseWriter, r *http.Request) {
	src, dst, srel, drel, ok := a.prepare(w, r)
	if !ok {
		return
	}
	from := ""
	if src != dst {
		from = src.Name
	}
	done := func() { a.did(r, db.Event{Kind: db.EventCopy, Vol: dst.Name, Path: drel, Name: srel, Target: from}) }
	httpx.JSON(w, 202, map[string]string{"job": a.Jobs.Start(src, dst, srel, drel, false, done)})
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
		a.did(r, db.Event{Kind: db.EventTrash, Vol: v.Name, Path: rel})
	}
	httpx.JSON(w, 200, map[string]any{"trashed": done, "failed": failed})
}

func (a *API) recent(w http.ResponseWriter, r *http.Request) {
	limit, err := strconv.Atoi(r.URL.Query().Get("limit"))
	if err != nil || limit <= 0 {
		limit = 200
	}
	tz, _ := strconv.ParseInt(r.URL.Query().Get("tz"), 10, 64)
	out, runs, err := recentRuns(r.Context(), a.Index, min(limit, 500), -tz*60*1000)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, 200, map[string]any{"entries": out, "runs": runs, "scanning": !a.Index.Ready()})
}

func (a *API) search(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	in := index.Query{Text: strings.TrimSpace(q.Get("q")), Limit: 200, Kind: q.Get("kind")}
	in.After, _ = strconv.ParseInt(q.Get("after"), 10, 64)
	in.MinSize, _ = strconv.ParseInt(q.Get("min"), 10, 64)
	if _, ok := index.Kinds[in.Kind]; in.Kind != "" && !ok {
		httpx.Fail(w, 400, "bad kind")
		return
	}
	switch {
	case in.Text == "" && in.Filtered():
	case utf8.RuneCountInString(in.Text) < 2 && !index.CJK(in.Text):
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
		FS   string `json:"fs,omitempty"`
		vol.Usage
	}
	vs := a.Vols.All()
	us, devs := make([]vol.Usage, len(vs)), make([]string, len(vs))
	for i, v := range vs {
		var err error
		if us[i], err = v.Usage(); err != nil {
			slog.Warn("volume usage", "vol", v.Name, "err", err)
		}
		devs[i], _ = v.Device()
	}
	out := []usage{}
	for i, fs := range filesystems(devs, us) {
		out = append(out, usage{vs[i].Name, fs, us[i]})
	}
	httpx.JSON(w, 200, map[string]any{"vols": out})
}

const btrfs = 0x9123683e

func filesystems(devs []string, us []vol.Usage) []string {
	keys := slices.Clone(devs)
	for i, u := range us {
		for j := range i {
			if (devs[i] != "" && devs[j] == devs[i]) || (u.Type == btrfs && us[j].Type == btrfs && u.Size > 0 && us[j].Size == u.Size) {
				keys[i] = keys[j]
				break
			}
		}
	}
	return keys
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

func (a *API) password(w http.ResponseWriter, r *http.Request) {
	var in struct{ Current, Next string }
	if err := httpx.Read(r, &in); err != nil || len(in.Next) > 72 {
		httpx.Fail(w, 400, "bad request")
		return
	}
	if utf8.RuneCountInString(in.Next) < 8 {
		httpx.Fail(w, 400, "too short")
		return
	}
	p, _ := auth.From(r.Context())
	switch err := a.Auth.ChangePassword(p.UserID, p.TokenID, in.Current, in.Next, auth.ClientIP(r)); {
	case errors.Is(err, auth.ErrRateLimited):
		auth.Refuse(w, err)
	case errors.Is(err, auth.ErrWrongPassword):
		httpx.Fail(w, 403, "wrong password")
	case err != nil:
		httpx.Error(w, err)
	default:
		w.WriteHeader(204)
	}
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
	"login": {db.EventLogin, db.EventLoginFailed, db.EventPassword},
	"share": {db.EventShareCreate, db.EventShareEdit, db.EventShareDelete},
	"token": {db.EventTokenCreate, db.EventTokenRevoke},
	"files": {db.EventUpload, db.EventCreate, db.EventEdit, db.EventRevert, db.EventRename, db.EventMove, db.EventCopy, db.EventTrash, db.EventRestore},
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
	if q.Has("vol") {
		v, rel, err := a.query(r)
		if err != nil {
			httpx.Error(w, err)
			return
		}
		f.Vol, f.Path = v.Name, rel
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
			"visitor": e.Visitor, "name": e.Name, "size": e.Size, "vol": e.Vol, "path": e.Path})
	}
	httpx.JSON(w, 200, map[string]any{"events": out, "more": more})
}
