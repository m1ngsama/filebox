package app

import (
	"cmp"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strconv"
	"strings"

	"github.com/m1ngsama/filebox/internal/api"
	"github.com/m1ngsama/filebox/internal/auth"
	"github.com/m1ngsama/filebox/internal/dav"
	"github.com/m1ngsama/filebox/internal/db"
	"github.com/m1ngsama/filebox/internal/httpx"
	"github.com/m1ngsama/filebox/internal/index"
	"github.com/m1ngsama/filebox/internal/passkey"
	"github.com/m1ngsama/filebox/internal/serve"
	"github.com/m1ngsama/filebox/internal/share"
	"github.com/m1ngsama/filebox/internal/thumb"
	"github.com/m1ngsama/filebox/internal/upload"
	"github.com/m1ngsama/filebox/internal/vol"
)

const spaCSP = "default-src 'self'; img-src 'self' blob: data:; media-src 'self' blob:; " +
	"frame-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'"

type App struct {
	Vols     *vol.Set
	DB       *db.DB
	Auth     *auth.Auth
	Web      fs.FS
	Uploads  *upload.Server
	Thumbs   *thumb.Service
	Passkeys *passkey.Service
	Index    *index.Index
}

func (a *App) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /raw/{vol}/{path...}", a.Auth.RequireAny(http.HandlerFunc(a.raw)))
	mux.Handle("GET /thumb/{vol}/{path...}", a.Auth.RequireAny(http.HandlerFunc(a.thumb)))
	(&api.API{Vols: a.Vols, DB: a.DB, Auth: a.Auth, Jobs: api.NewJobs(a.Index), Index: a.Index}).Register(mux)
	a.Passkeys.Register(mux)
	d := dav.Handler(a.Vols, a.Auth, a.Index)
	mux.Handle("/dav", d)
	mux.Handle("/dav/", d)
	mux.Handle("/upload/", a.Uploads.Handler("/upload/", a.userUploads()))
	(&share.Service{DB: a.DB, Vols: a.Vols, Auth: a.Auth, Uploads: a.Uploads, Thumbs: a.Thumbs}).Register(mux)
	mux.Handle("GET /s/{token}", a.spa())
	mux.Handle("/", a.spa())
	return common(http.NewCrossOriginProtection().Handler(mux))
}

func common(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		h.ServeHTTP(w, r)
	})
}

func (a *App) userUploads() upload.Policy {
	return upload.Policy{
		Owner: func(r *http.Request) (string, bool) {
			p, ok := a.Auth.Session(r)
			if !ok {
				p, ok = a.Auth.App(r)
				ok = ok && !p.ReadOnly
			}
			return "user:" + strconv.FormatInt(p.UserID, 10), ok
		},
		Resolve: func(r *http.Request, meta map[string]string) (upload.Target, error) {
			v, dir, err := a.Vols.Resolve(meta["vol"], meta["dir"])
			if err != nil {
				return upload.Target{}, err
			}
			return upload.TargetFor(v, dir, meta)
		},
	}
}

func (a *App) raw(w http.ResponseWriter, r *http.Request) {
	v, rel, err := a.Vols.Resolve(r.PathValue("vol"), r.PathValue("path"))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	serve.File(w, r, v.Root, rel, r.URL.Query().Has("dl"))
}

func (a *App) thumb(w http.ResponseWriter, r *http.Request) {
	v, rel, err := a.Vols.Resolve(r.PathValue("vol"), r.PathValue("path"))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	a.Thumbs.Serve(w, r, v, rel)
}

var encodings = [...]struct{ name, ext string }{{"br", ".br"}, {"gzip", ".gz"}}

func (a *App) precompressed(w http.ResponseWriter, r *http.Request, p string) bool {
	accept := acceptEncodings(r.Header.Get("Accept-Encoding"))
	vary := false
	for _, e := range encodings {
		f, err := a.Web.Open(p + e.ext)
		if err != nil {
			continue
		}
		vary = true
		if !accept[e.name] {
			f.Close()
			continue
		}
		defer f.Close()
		st, err := f.Stat()
		rs, ok := f.(io.ReadSeeker)
		if err != nil || !ok {
			return false
		}
		h := w.Header()
		h.Add("Vary", "Accept-Encoding")
		h.Set("Content-Encoding", e.name)
		h.Set("Content-Type", cmp.Or(mime.TypeByExtension(path.Ext(p)), "application/octet-stream"))
		http.ServeContent(w, r, p, st.ModTime(), rs)
		return true
	}
	if vary {
		w.Header().Add("Vary", "Accept-Encoding")
	}
	return false
}

func acceptEncodings(h string) map[string]bool {
	m := map[string]bool{}
	for part := range strings.SplitSeq(h, ",") {
		name, params, _ := strings.Cut(part, ";")
		q := strings.TrimSpace(params)
		if v, ok := strings.CutPrefix(q, "q="); ok {
			if f, err := strconv.ParseFloat(v, 64); err != nil || f <= 0 {
				continue
			}
		}
		m[strings.ToLower(strings.TrimSpace(name))] = true
	}
	return m
}

func (a *App) spa() http.Handler {
	files := http.FileServerFS(a.Web)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			httpx.Fail(w, 405, "method not allowed")
			return
		}
		if p := strings.TrimPrefix(r.URL.Path, "/"); p != "" {
			if st, err := fs.Stat(a.Web, p); err == nil && !st.IsDir() {
				if strings.HasPrefix(p, "assets/") {
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
					if a.precompressed(w, r, p) {
						return
					}
				}
				files.ServeHTTP(w, r)
				return
			}
		}
		b, err := fs.ReadFile(a.Web, "index.html")
		if err != nil {
			http.Error(w, "frontend not built", 404)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("Content-Security-Policy", spaCSP)
		w.Write(b)
	})
}
