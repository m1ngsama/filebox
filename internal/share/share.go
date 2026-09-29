package share

import (
	"crypto/rand"
	"encoding/base64"
	"io/fs"
	"net/http"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/m1ngsama/filebox/internal/api"
	"github.com/m1ngsama/filebox/internal/auth"
	"github.com/m1ngsama/filebox/internal/db"
	"github.com/m1ngsama/filebox/internal/httpx"
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
	mux.Handle("DELETE /api/shares/{id}", s.Auth.RequireSession(http.HandlerFunc(s.remove)))
	mux.HandleFunc("GET /s/{token}/info", s.info)
	mux.HandleFunc("POST /s/{token}/unlock", s.unlock)
	mux.HandleFunc("GET /s/{token}/ls", s.ls)
	mux.HandleFunc("GET /s/{token}/raw/{path...}", s.raw)
	mux.HandleFunc("GET /s/{token}/thumb/{path...}", s.thumb)
	mux.HandleFunc("GET /s/{token}/zip", s.zip)
	mux.HandleFunc("/s/{token}/upload/{rest...}", s.upload)
}

var modes = map[string]bool{"read": true, "upload": true, "drop": true}

func (s *Service) create(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Vol       string `json:"vol"`
		Path      string `json:"path"`
		Mode      string `json:"mode"`
		Password  string `json:"password"`
		ExpiresIn int64  `json:"expires_in"`
	}
	if err := httpx.Read(r, &in); err != nil || !modes[in.Mode] || in.ExpiresIn < 0 {
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
	b := make([]byte, 16)
	rand.Read(b)
	now := time.Now().Unix()
	sh := &db.Share{Token: base64.RawURLEncoding.EncodeToString(b), UserID: p.UserID, Vol: v.Name,
		Path: rel, Mode: in.Mode, CreatedAt: now}
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
		out = append(out, map[string]any{"id": sh.ID, "token": sh.Token, "vol": sh.Vol, "path": sh.Path,
			"mode": sh.Mode, "has_password": sh.PasswordHash != "", "expires": sh.ExpiresAt,
			"created": sh.CreatedAt, "hits": sh.Hits})
	}
	httpx.JSON(w, 200, map[string]any{"shares": out})
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
	sh, err := s.DB.ShareByToken(r.PathValue("token"), time.Now().Unix())
	if err != nil {
		httpx.Fail(w, 404, "not found")
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

func (o opened) sub(p string) (string, error) {
	rel, err := vol.Clean(p)
	if err != nil {
		return "", err
	}
	if rel == "." {
		return o.sh.Path, nil
	}
	if !o.dir {
		return "", fs.ErrNotExist
	}
	return path.Join(o.sh.Path, rel), nil
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
	out := map[string]any{"name": path.Base(o.sh.Path), "dir": o.dir, "mode": o.sh.Mode, "locked": false}
	if o.sh.Path == "." {
		out["name"] = o.v.Name
	}
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
	rel, err := o.sub(r.URL.Query().Get("path"))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	es, err := api.List(o.v.Root, rel)
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
	rel, err := o.sub(r.PathValue("path"))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	if _, err := o.v.Root.Stat(rel); err == nil {
		if rg := r.Header.Get("Range"); rg == "" || strings.HasPrefix(rg, "bytes=0-") {
			s.DB.HitShare(o.sh.ID)
		}
	}
	serve.File(w, r, o.v.Root, rel, r.URL.Query().Has("dl"))
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
	var rels []string
	for _, p := range ps {
		rel, err := o.sub(p)
		if err != nil {
			httpx.Error(w, err)
			return
		}
		rels = append(rels, rel)
	}
	s.DB.HitShare(o.sh.ID)
	serve.Zip(w, r, o.v.Root, rels, "", serve.ZipName(o.v.Name, rels, q.Get("name")))
}

func (s *Service) thumb(w http.ResponseWriter, r *http.Request) {
	o, ok := s.open(w, r, "read", "upload")
	if !ok {
		return
	}
	rel, err := o.sub(r.PathValue("path"))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	s.Thumbs.Serve(w, r, o.v, rel)
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
			return upload.TargetFor(o.v, o.sh.Path, meta)
		},
	}).ServeHTTP(w, r)
}
