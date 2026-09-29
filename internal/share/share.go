package share

import (
	"crypto/rand"
	"encoding/base64"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/m1ngsama/filebox/internal/api"
	"github.com/m1ngsama/filebox/internal/auth"
	"github.com/m1ngsama/filebox/internal/db"
	"github.com/m1ngsama/filebox/internal/httpx"
	"github.com/m1ngsama/filebox/internal/render"
	"github.com/m1ngsama/filebox/internal/serve"
	"github.com/m1ngsama/filebox/internal/thumb"
	"github.com/m1ngsama/filebox/internal/upload"
	"github.com/m1ngsama/filebox/internal/vol"
)

type Service struct {
	DB      *db.DB
	Vols    *vol.Set
	Auth    *auth.Auth
	Uploads *upload.Server
	Thumbs  *thumb.Service
}

func (s *Service) Register(mux *http.ServeMux) {
	mux.Handle("GET /api/shares", s.Auth.RequireSession(http.HandlerFunc(s.list)))
	mux.Handle("POST /api/shares", s.Auth.RequireSession(http.HandlerFunc(s.create)))
	mux.Handle("PATCH /api/shares/{id}", s.Auth.RequireSession(http.HandlerFunc(s.edit)))
	mux.Handle("DELETE /api/shares/{id}", s.Auth.RequireSession(http.HandlerFunc(s.remove)))
	mux.HandleFunc("GET /s/{token}/info", s.info)
	mux.HandleFunc("POST /s/{token}/unlock", s.unlock)
	mux.HandleFunc("GET /s/{token}/ls", s.ls)
	mux.HandleFunc("GET /s/{token}/raw/{path...}", s.raw)
	mux.HandleFunc("GET /s/{token}/thumb/{path...}", s.thumb)
	mux.HandleFunc("GET /s/{token}/zip", s.zip)
	mux.HandleFunc("GET /s/{token}/render", s.render)
	mux.HandleFunc("GET /s/{token}/meta", s.meta)
	mux.HandleFunc("/s/{token}/upload/{rest...}", s.upload)
}

var modes = map[string]bool{"read": true, "upload": true, "drop": true}

const maxNote = 1000

func (s *Service) create(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Vol       string `json:"vol"`
		Path      string `json:"path"`
		Mode      string `json:"mode"`
		Password  string `json:"password"`
		ExpiresIn int64  `json:"expires_in"`
		Note      string `json:"note"`
		MaxUpload int64  `json:"max_upload"`
	}
	if err := httpx.Read(r, &in); err != nil || !modes[in.Mode] || in.ExpiresIn < 0 || in.MaxUpload < 0 || len(in.Note) > maxNote {
		httpx.Fail(w, 400, "bad request")
		return
	}
	v, rel, err := s.Vols.Resolve(in.Vol, in.Path)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	fi, err := v.Root.Stat(rel)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	if in.Mode != "read" && !fi.IsDir() {
		httpx.Fail(w, 400, "upload shares need a directory")
		return
	}
	p, _ := auth.From(r.Context())
	now := time.Now().Unix()
	if in.Password == "" && in.Note == "" && in.MaxUpload == 0 {
		if sh, err := s.DB.SameShare(p.UserID, v.Name, rel, in.Mode, in.ExpiresIn, now); err == nil {
			httpx.JSON(w, 200, map[string]any{"id": sh.ID, "token": sh.Token, "existing": true})
			return
		}
	}
	b := make([]byte, 16)
	rand.Read(b)
	sh := &db.Share{Token: base64.RawURLEncoding.EncodeToString(b), UserID: p.UserID, Vol: v.Name,
		Path: rel, Mode: in.Mode, CreatedAt: now, Note: in.Note, MaxUpload: in.MaxUpload}
	if in.ExpiresIn > 0 {
		sh.ExpiresAt = now + in.ExpiresIn
	}
	if in.Password != "" {
		if sh.PasswordHash, err = auth.HashPassword(in.Password); err != nil {
			httpx.Error(w, err)
			return
		}
	}
	if err := s.DB.InsertShare(sh); err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, 201, map[string]any{"id": sh.ID, "token": sh.Token})
}

func (s *Service) list(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.From(r.Context())
	shs, err := s.DB.ListShares(p.UserID)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	out := []map[string]any{}
	for _, sh := range shs {
		dir := false
		if v, ok := s.Vols.Get(sh.Vol); ok {
			fi, err := v.Root.Stat(sh.Path)
			dir = err == nil && fi.IsDir()
		}
		out = append(out, map[string]any{"dir": dir, "id": sh.ID, "token": sh.Token, "vol": sh.Vol, "path": sh.Path,
			"mode": sh.Mode, "has_password": sh.PasswordHash != "", "expires": sh.ExpiresAt,
			"created": sh.CreatedAt, "hits": sh.Hits, "views": sh.Views, "note": sh.Note, "max_upload": sh.MaxUpload})
	}
	httpx.JSON(w, 200, map[string]any{"shares": out})
}

func (s *Service) edit(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	var in struct {
		Mode      *string `json:"mode"`
		Password  *string `json:"password"`
		ExpiresIn *int64  `json:"expires_in"`
		Note      *string `json:"note"`
		MaxUpload *int64  `json:"max_upload"`
	}
	if err != nil || httpx.Read(r, &in) != nil || (in.Mode != nil && !modes[*in.Mode]) || (in.ExpiresIn != nil && *in.ExpiresIn < 0) ||
		(in.MaxUpload != nil && *in.MaxUpload < 0) || (in.Note != nil && len(*in.Note) > maxNote) {
		httpx.Fail(w, 400, "bad request")
		return
	}
	p, _ := auth.From(r.Context())
	sh, err := s.DB.ShareByID(p.UserID, id)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	if in.Mode != nil && *in.Mode != sh.Mode {
		v, ok := s.Vols.Get(sh.Vol)
		if !ok {
			httpx.Fail(w, 404, "not found")
			return
		}
		if fi, err := v.Root.Stat(sh.Path); *in.Mode != "read" && (err != nil || !fi.IsDir()) {
			httpx.Fail(w, 400, "upload shares need a directory")
			return
		}
		sh.Mode = *in.Mode
	}
	if in.ExpiresIn != nil {
		sh.ExpiresAt = 0
		if *in.ExpiresIn > 0 {
			sh.ExpiresAt = time.Now().Unix() + *in.ExpiresIn
		}
	}
	if in.Note != nil {
		sh.Note = *in.Note
	}
	if in.MaxUpload != nil {
		sh.MaxUpload = *in.MaxUpload
	}
	if in.Password != nil {
		sh.PasswordHash = ""
		if *in.Password != "" {
			if sh.PasswordHash, err = auth.HashPassword(*in.Password); err != nil {
				httpx.Error(w, err)
				return
			}
		}
	}
	if err := s.DB.UpdateShare(sh, in.Password != nil); err != nil {
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
	p, _ := auth.From(r.Context())
	if err := s.DB.DeleteShare(p.UserID, id); err != nil {
		httpx.Error(w, err)
		return
	}
	w.WriteHeader(204)
}

type opened struct {
	sh  db.Share
	v   *vol.Volume
	dir bool
}

func (s *Service) open(w http.ResponseWriter, r *http.Request, allowed ...string) (opened, bool) {
	sh, err := s.DB.ShareByToken(r.PathValue("token"))
	if err != nil {
		httpx.Fail(w, 404, "not found")
		return opened{}, false
	}
	if sh.ExpiresAt != 0 && sh.ExpiresAt <= time.Now().Unix() {
		httpx.Fail(w, 410, "expired")
		return opened{}, false
	}
	v, ok := s.Vols.Get(sh.Vol)
	if !ok {
		httpx.Fail(w, 404, "not found")
		return opened{}, false
	}
	fi, err := v.Root.Stat(sh.Path)
	if err != nil {
		httpx.Fail(w, 404, "not found")
		return opened{}, false
	}
	if len(allowed) > 0 {
		if sh.PasswordHash != "" && !s.Auth.ShareUnlocked(r, sh.ID) {
			httpx.Fail(w, 401, "locked")
			return opened{}, false
		}
		ok := false
		for _, m := range allowed {
			ok = ok || m == sh.Mode
		}
		if !ok {
			httpx.Fail(w, 403, "forbidden")
			return opened{}, false
		}
	}
	return opened{sh, v, fi.IsDir()}, true
}

func (o opened) name() string {
	if o.sh.Path == "." {
		return o.v.Name
	}
	return path.Base(o.sh.Path)
}

func (o opened) root(p string) (*os.Root, string, func(), error) {
	rel, err := vol.Clean(p)
	if err != nil {
		return nil, "", nil, err
	}
	if !o.dir {
		if rel != "." {
			return nil, "", nil, fs.ErrNotExist
		}
		return o.v.Root, o.sh.Path, func() {}, nil
	}
	r, err := o.v.Root.OpenRoot(o.sh.Path)
	if err != nil {
		return nil, "", nil, err
	}
	return r, rel, func() { r.Close() }, nil
}

func (s *Service) info(w http.ResponseWriter, r *http.Request) {
	o, ok := s.open(w, r)
	if !ok {
		return
	}
	if o.sh.PasswordHash != "" && !s.Auth.ShareUnlocked(r, o.sh.ID) {
		httpx.JSON(w, 200, map[string]any{"locked": true, "mode": o.sh.Mode})
		return
	}
	out := map[string]any{"name": o.name(), "dir": o.dir, "mode": o.sh.Mode, "locked": false,
		"note": o.sh.Note, "expires": o.sh.ExpiresAt, "max_upload": o.sh.MaxUpload}
	if !o.dir {
		if e, err := api.Stat(o.v.Root, o.sh.Path); err == nil {
			out["size"] = e.Size
		}
	}
	httpx.JSON(w, 200, out)
}

func (s *Service) unlock(w http.ResponseWriter, r *http.Request) {
	o, ok := s.open(w, r)
	if !ok {
		return
	}
	ip := auth.ClientIP(r)
	if err := s.Auth.ShareLimit(ip); err != nil {
		auth.Refuse(w, err)
		return
	}
	var in struct {
		Password string `json:"password"`
	}
	if err := httpx.Read(r, &in); err != nil {
		httpx.Fail(w, 400, "bad request")
		return
	}
	if o.sh.PasswordHash == "" || !auth.CheckPassword(o.sh.PasswordHash, in.Password) {
		s.Auth.ShareFailed(ip)
		httpx.Fail(w, 401, "wrong password")
		return
	}
	tok, err := s.Auth.NewShareToken(o.sh.UserID, o.sh.ID)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	auth.SetCookie(w, r, auth.ShareCookie, tok, "/s/"+o.sh.Token+"/", 24*time.Hour)
	w.WriteHeader(204)
}

func (s *Service) ls(w http.ResponseWriter, r *http.Request) {
	o, ok := s.open(w, r, "read", "upload")
	if !ok {
		return
	}
	if !o.dir {
		httpx.Fail(w, 400, "not a directory")
		return
	}
	root, rel, done, err := o.root(r.URL.Query().Get("path"))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	defer done()
	es, err := api.List(root, rel)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, 200, map[string]any{"entries": es})
}

func (s *Service) raw(w http.ResponseWriter, r *http.Request) {
	o, ok := s.open(w, r, "read", "upload")
	if !ok {
		return
	}
	root, rel, done, err := o.root(r.PathValue("path"))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	defer done()
	if r.URL.Query().Has("vtt") {
		serve.VTT(w, r, root, rel)
		return
	}
	if _, err := root.Stat(rel); err == nil {
		if rg := r.Header.Get("Range"); rg == "" || strings.HasPrefix(rg, "bytes=0-") {
			s.DB.HitShare(o.sh.ID)
		}
	}
	serve.File(w, r, root, rel, r.URL.Query().Has("dl"))
}

func (s *Service) zip(w http.ResponseWriter, r *http.Request) {
	o, ok := s.open(w, r, "read", "upload")
	if !ok {
		return
	}
	q := r.URL.Query()
	ps := q["p"]
	if len(ps) == 0 {
		ps = []string{""}
	}
	root, _, done, err := o.root("")
	if err != nil {
		httpx.Error(w, err)
		return
	}
	defer done()
	var rels []string
	for _, p := range ps {
		rel, err := vol.Clean(p)
		if err == nil && !o.dir && rel != "." {
			err = fs.ErrNotExist
		}
		if err != nil {
			httpx.Error(w, err)
			return
		}
		if !o.dir {
			rel = o.sh.Path
		}
		rels = append(rels, rel)
	}
	top := ""
	if o.dir {
		top = o.name()
	}
	if serve.Zip(w, r, root, rels, top, serve.ZipName(o.name(), rels, q.Get("name"))) {
		s.DB.HitShare(o.sh.ID)
	}
}

func (s *Service) render(w http.ResponseWriter, r *http.Request) {
	o, ok := s.open(w, r, "read", "upload")
	if !ok {
		return
	}
	root, rel, done, err := o.root(r.URL.Query().Get("p"))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	defer done()
	raw := ""
	if o.dir {
		raw = "/s/" + url.PathEscape(o.sh.Token) + "/raw/"
	}
	render.Serve(w, r, root, rel, raw)
}

func (s *Service) meta(w http.ResponseWriter, r *http.Request) {
	o, ok := s.open(w, r, "read", "upload")
	if !ok {
		return
	}
	root, rel, done, err := o.root(r.URL.Query().Get("p"))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	defer done()
	s.Thumbs.ServeMeta(w, r, root, rel)
}

func (s *Service) thumb(w http.ResponseWriter, r *http.Request) {
	o, ok := s.open(w, r, "read", "upload")
	if !ok {
		return
	}
	root, rel, done, err := o.root(r.PathValue("path"))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	defer done()
	key := rel
	if o.dir {
		key = path.Join(o.sh.Path, rel)
	}
	s.Thumbs.ServeFrom(w, r, root, rel, o.v.Name, key)
}

func (s *Service) upload(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		s.Uploads.Handler("/", upload.Policy{}).ServeHTTP(w, r)
		return
	}
	o, ok := s.open(w, r, "upload", "drop")
	if !ok {
		return
	}
	if !o.dir {
		httpx.Fail(w, 403, "not a directory")
		return
	}
	prefix := "/s/" + o.sh.Token + "/upload/"
	owner := "share:" + strconv.FormatInt(o.sh.ID, 10)
	s.Uploads.Handler(prefix, upload.Policy{
		Owner: func(*http.Request) (string, bool) { return owner, true },
		Resolve: func(r *http.Request, meta map[string]string) (upload.Target, error) {
			t, err := upload.Within(o.v, o.sh.Path, meta)
			t.MaxSize = o.sh.MaxUpload
			return t, err
		},
	}).ServeHTTP(w, r)
}
