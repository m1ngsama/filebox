package app

import (
	"io/fs"
	"net/http"
	"strings"

	"github.com/m1ngsama/filebox/internal/auth"
	"github.com/m1ngsama/filebox/internal/db"
	"github.com/m1ngsama/filebox/internal/httpx"
	"github.com/m1ngsama/filebox/internal/serve"
	"github.com/m1ngsama/filebox/internal/vol"
)

const spaCSP = "default-src 'self'; img-src 'self' blob: data:; media-src 'self' blob:; " +
	"frame-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'"

type App struct {
	Vols *vol.Set
	DB   *db.DB
	Auth *auth.Auth
	Web  fs.FS
}

func (a *App) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /raw/{vol}/{path...}", a.Auth.RequireAny(http.HandlerFunc(a.raw)))
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

func (a *App) raw(w http.ResponseWriter, r *http.Request) {
	v, rel, err := a.Vols.Resolve(r.PathValue("vol"), r.PathValue("path"))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	serve.File(w, r, v.Root, rel, r.URL.Query().Has("dl"))
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
