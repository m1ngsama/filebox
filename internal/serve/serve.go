package serve

import (
	"fmt"
	"mime"
	"net/http"
	"os"
	"path"
	"regexp"
	"strings"

	"github.com/m1ngsama/filebox/internal/httpx"
	"github.com/m1ngsama/filebox/internal/vol"
)

// Distros ship different mime.types files; previews depend on these being stable.
func init() {
	for ext, t := range map[string]string{
		".txt": "text/plain; charset=utf-8", ".md": "text/plain; charset=utf-8", ".log": "text/plain; charset=utf-8",
		".mp4": "video/mp4", ".m4v": "video/mp4", ".webm": "video/webm", ".mkv": "video/x-matroska", ".mov": "video/quicktime",
		".mp3": "audio/mpeg", ".m4a": "audio/mp4", ".flac": "audio/flac", ".ogg": "audio/ogg", ".opus": "audio/ogg", ".wav": "audio/wav",
	} {
		mime.AddExtensionType(ext, t)
	}
}

var dangerous = map[string]bool{
	".html": true, ".htm": true, ".xhtml": true, ".shtml": true, ".svg": true, ".svgz": true,
	".xml": true, ".xsl": true, ".mht": true, ".mhtml": true,
}

var active = regexp.MustCompile(`javascript|ecmascript|jscript|livescript|css|json|wasm`)

// Inert turns any script, style or module type into text/plain, so a user file never runs on this origin.
func Inert(ct string) string {
	if mt, _, err := mime.ParseMediaType(ct); err == nil && active.MatchString(strings.ToLower(mt)) {
		return "text/plain; charset=utf-8"
	}
	return ct
}

func SafeHeaders(h http.Header, contentType string) {
	h.Set("X-Content-Type-Options", "nosniff")
	// Chrome will not render a PDF in a sandboxed document; nosniff already keeps it from being HTML.
	if strings.HasPrefix(contentType, "application/pdf") {
		h.Set("Content-Security-Policy", "frame-ancestors 'self'")
	} else {
		h.Set("Content-Security-Policy", "sandbox; frame-ancestors 'self'")
	}
}

func File(w http.ResponseWriter, r *http.Request, root *os.Root, rel string, download bool) {
	Named(w, r, root, rel, path.Base(rel), download)
}

func Named(w http.ResponseWriter, r *http.Request, root *os.Root, rel, name string, download bool) {
	f, err := vol.Open(root, rel)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		httpx.Error(w, err)
		return
	}
	if st.IsDir() {
		httpx.Fail(w, 400, "is a directory")
		return
	}
	ext := strings.ToLower(path.Ext(name))
	ct := mime.TypeByExtension(ext)
	if ct == "" || dangerous[ext] {
		ct = "application/octet-stream"
	}
	ct = Inert(ct)
	h := w.Header()
	h.Set("Content-Type", ct)
	SafeHeaders(h, ct)
	h.Set("ETag", fmt.Sprintf(`"%x-%x"`, st.Size(), st.ModTime().UnixNano()))
	h.Set("Cache-Control", "private, no-cache")
	if download || dangerous[ext] {
		h.Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": name}))
	}
	http.ServeContent(w, r, name, st.ModTime(), f)
}
