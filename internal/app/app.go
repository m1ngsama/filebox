package app

import (
	"cmp"
	"crypto/sha256"
	"encoding/base64"
	"io"
	"io/fs"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path"
	"regexp"
	"strconv"
	"strings"

	"github.com/m1ngsama/filebox/internal/api"
	"github.com/m1ngsama/filebox/internal/auth"
	"github.com/m1ngsama/filebox/internal/dav"
	"github.com/m1ngsama/filebox/internal/db"
	"github.com/m1ngsama/filebox/internal/httpx"
	"github.com/m1ngsama/filebox/internal/index"
	"github.com/m1ngsama/filebox/internal/passkey"
	"github.com/m1ngsama/filebox/internal/render"
	"github.com/m1ngsama/filebox/internal/serve"
	"github.com/m1ngsama/filebox/internal/share"
	"github.com/m1ngsama/filebox/internal/stream"
	"github.com/m1ngsama/filebox/internal/thumb"
	"github.com/m1ngsama/filebox/internal/upload"
	"github.com/m1ngsama/filebox/internal/version"
	"github.com/m1ngsama/filebox/internal/vol"
)

const spaCSP = "default-src 'self'; img-src 'self' blob: data:; media-src 'self' blob:; " +
	"frame-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'"

// A book's sections render in blob: iframes that inherit this document's policy, so script-src must exclude /raw and /s.
const readerCSP = "default-src 'none'; style-src {assets} 'unsafe-inline' blob:; img-src blob: data:; font-src blob: data:; " +
	"media-src blob:; connect-src {api}; frame-src blob:; object-src 'none'; base-uri 'none'; frame-ancestors 'self'; form-action 'none'"

type App struct {
	Vols     *vol.Set
	DB       *db.DB
	Auth     *auth.Auth
	Web      fs.FS
	Uploads  *upload.Server
	Thumbs   *thumb.Service
	Stream   *stream.Service
	Passkeys *passkey.Service
	Index    *index.Index
	Versions *version.Store
	Origins  []string
}

func (a *App) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("GET /raw/{vol}/{path...}", a.Auth.RequireAny(http.HandlerFunc(a.raw)))
	mux.Handle("GET /thumb/{vol}/{path...}", a.Auth.RequireAny(http.HandlerFunc(a.thumb)))
	mux.Handle("GET /api/render", a.Auth.RequireAny(http.HandlerFunc(a.render)))
	mux.Handle("GET /api/meta", a.Auth.RequireAny(http.HandlerFunc(a.meta)))
	mux.Handle("GET /api/stream/index.m3u8", a.Auth.RequireAny(a.located(a.Stream.Playlist)))
	mux.Handle("GET /api/stream/seg", a.Auth.RequireAny(a.located(a.Stream.Segment)))
	(&api.API{Vols: a.Vols, DB: a.DB, Auth: a.Auth, Jobs: api.NewJobs(a.Index), Index: a.Index, Versions: a.Versions, Origins: a.Origins}).Register(mux)
	a.Passkeys.Register(mux)
	d := dav.Handler(a.Vols, a.Auth, a.Index, a.Versions)
	mux.Handle("/dav", d)
	mux.Handle("/dav/", d)
	mux.Handle("/upload/", a.Uploads.Handler("/upload/", a.userUploads()))
	(&share.Service{DB: a.DB, Vols: a.Vols, Auth: a.Auth, Uploads: a.Uploads, Thumbs: a.Thumbs, Stream: a.Stream}).Register(mux)
	mux.HandleFunc("POST /share-target", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/?share-target", http.StatusSeeOther)
	})
	mux.Handle("GET /reader", a.spa("reader.html"))
	mux.Handle("GET /s/{token}", a.spa("share.html"))
	mux.Handle("GET /s/{token}/{$}", a.spa("share.html"))
	mux.Handle("/", a.spa("index.html"))
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
			t, err := upload.TargetFor(v, dir, meta)
			t.Replace = meta["overwrite"] == "1"
			return t, err
		},
	}
}

func (a *App) raw(w http.ResponseWriter, r *http.Request) {
	v, rel, err := a.Vols.Resolve(r.PathValue("vol"), r.PathValue("path"))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	if r.URL.Query().Has("vtt") {
		serve.VTT(w, r, v.Root, rel)
		return
	}
	serve.File(w, r, v.Root, rel, r.URL.Query().Has("dl"))
}

func (a *App) render(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	v, rel, err := a.Vols.Resolve(q.Get("vol"), q.Get("p"))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	render.Serve(w, r, v.Root, rel, "/raw/"+url.PathEscape(v.Name)+"/")
}

func (a *App) located(fn func(http.ResponseWriter, *http.Request, *os.Root, string)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		v, rel, err := a.Vols.Resolve(q.Get("vol"), q.Get("p"))
		if err != nil {
			httpx.Error(w, err)
			return
		}
		fn(w, r, v.Root, rel)
	}
}

func (a *App) meta(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	v, rel, err := a.Vols.Resolve(q.Get("vol"), q.Get("p"))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	a.Thumbs.ServeMeta(w, r, v.Root, rel)
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
		h.Set("Content-Length", strconv.FormatInt(st.Size(), 10))
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

var inlineScript = regexp.MustCompile(`(?s)<script>(.*?)</script>`)

// The reader's scripts must come from /assets/ alone, which no host source but a path-bearing one can express.
func spaPolicy(page string, index []byte) string {
	csp, self := spaCSP, "'self'"
	if page == "reader.html" {
		csp, self = readerCSP, "{assets}"
	}
	if m := inlineScript.FindAllSubmatch(index, -1); m != nil {
		csp += "; script-src " + self
		for _, s := range m {
			sum := sha256.Sum256(s[1])
			csp += " 'sha256-" + base64.StdEncoding.EncodeToString(sum[:]) + "'"
		}
	}
	return csp
}

var badHost = regexp.MustCompile(`[^A-Za-z0-9.:\[\]-]`)

// A proxy may rewrite Host, so a configured origin wins: the one the request names, else all of them plus Host.
func (a *App) sources(r *http.Request) []string {
	var all []string
	if r.Host != "" && !badHost.MatchString(r.Host) {
		all = append(all, r.Host)
	}
	for _, o := range a.Origins {
		u, err := url.Parse(o)
		if err != nil || u.Host == "" {
			continue
		}
		if u.Host == r.Host || u.Host == r.Header.Get("X-Forwarded-Host") {
			return []string{o}
		}
		all = append(all, o)
	}
	return all
}

// The reader fetches only archive entries; a source path without a trailing slash matches that path exactly.
func readerSources(srcs []string, paths ...string) string {
	var out []string
	for _, s := range srcs {
		for _, p := range paths {
			out = append(out, s+p)
		}
	}
	return strings.Join(out, " ")
}

func (a *App) spa(page string) http.Handler {
	files := http.FileServerFS(a.Web)
	index, indexErr := fs.ReadFile(a.Web, page)
	csp := spaPolicy(page, index)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			httpx.Fail(w, 405, "method not allowed")
			return
		}
		if p := strings.TrimPrefix(r.URL.Path, "/"); p != "" {
			if st, err := fs.Stat(a.Web, p); err == nil && !st.IsDir() {
				switch {
				case strings.HasPrefix(p, "assets/"):
					w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
					if a.precompressed(w, r, p) {
						return
					}
				case p == "sw.js":
					w.Header().Set("Cache-Control", "no-cache")
				case path.Ext(p) == ".webmanifest":
					w.Header().Set("Content-Type", "application/manifest+json")
				}
				files.ServeHTTP(w, r)
				return
			}
		}
		if strings.HasPrefix(r.URL.Path, "/assets/") {
			http.NotFound(w, r)
			return
		}
		if indexErr != nil {
			http.Error(w, "frontend not built", 404)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		policy := csp
		if strings.Contains(policy, "{assets}") {
			srcs := a.sources(r)
			if len(srcs) == 0 {
				httpx.Fail(w, 400, "bad host")
				return
			}
			policy = strings.NewReplacer("{assets}", readerSources(srcs, "/assets/"),
				"{api}", readerSources(srcs, "/api/zip-entries", "/api/zip-entry", "/s/")).Replace(policy)
		}
		w.Header().Set("Content-Security-Policy", policy)
		w.Write(index)
	})
}
