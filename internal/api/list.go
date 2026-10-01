package api

import (
	"compress/gzip"
	"encoding/json"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"sort"
	"strings"

	"github.com/m1ngsama/filebox/internal/httpx"
	"github.com/m1ngsama/filebox/internal/vol"
)

const (
	lsChunk  = 1000
	maxDirs  = 200
	maxItems = 1000
)

// counted fills in child counts for small folders only; a listing never reads more than maxDirs extra directories.
func counted(root *os.Root, rel string, es []Entry) []Entry {
	dirs := 0
	for _, e := range es {
		if e.Dir {
			dirs++
		}
	}
	if dirs > maxDirs {
		return es
	}
	for i, e := range es {
		if !e.Dir {
			continue
		}
		f, err := root.Open(path.Join(rel, e.Name))
		if err != nil {
			continue
		}
		names, _ := f.Readdirnames(maxItems)
		f.Close()
		n := len(names)
		es[i].Items = &n
	}
	return es
}

func entries(root *os.Root, rel string, des []fs.DirEntry) []Entry {
	out := make([]Entry, 0, len(des))
	for _, de := range des {
		name := de.Name()
		if rel == "." && vol.Reserved(name) {
			continue
		}
		var fi fs.FileInfo
		var err error
		if de.Type()&fs.ModeSymlink != 0 {
			fi, err = root.Stat(path.Join(rel, name))
		} else {
			fi, err = de.Info()
		}
		if err != nil {
			continue
		}
		out = append(out, entry(name, fi))
	}
	return out
}

func sorted(es []Entry) []Entry {
	sort.Slice(es, func(i, j int) bool {
		if es[i].Dir != es[j].Dir {
			return es[i].Dir
		}
		return es[i].Name < es[j].Name
	})
	return es
}

func acceptsGzip(r *http.Request) bool {
	for c := range strings.SplitSeq(r.Header.Get("Accept-Encoding"), ",") {
		name, params, _ := strings.Cut(c, ";")
		if strings.EqualFold(strings.TrimSpace(name), "gzip") {
			q := strings.TrimRight(strings.ReplaceAll(params, " ", ""), "0")
			return q != "q=" && q != "q=0."
		}
	}
	return false
}

func WriteList(w http.ResponseWriter, r *http.Request, root *os.Root, rel string) {
	f, err := root.Open(rel)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	defer f.Close()
	des, err := f.ReadDir(lsChunk)
	if err != nil && err != io.EOF {
		httpx.Error(w, err)
		return
	}
	if len(des) < lsChunk {
		httpx.Tagged(w, r, map[string]any{"entries": counted(root, rel, sorted(entries(root, rel, des)))})
		return
	}
	h := w.Header()
	h.Set("Content-Type", "application/x-ndjson")
	h.Set("Cache-Control", "no-store")
	h.Set("Vary", "Accept-Encoding")
	var out io.Writer = w
	var gz *gzip.Writer
	if acceptsGzip(r) {
		h.Set("Content-Encoding", "gzip")
		gz, _ = gzip.NewWriterLevel(w, gzip.BestSpeed)
		defer gz.Close()
		out = gz
	}
	enc := json.NewEncoder(out)
	rc := http.NewResponseController(w)
	for err == nil {
		if len(des) > 0 {
			if enc.Encode(map[string][]Entry{"entries": entries(root, rel, des)}) != nil {
				return
			}
			if gz != nil && gz.Flush() != nil {
				return
			}
			rc.Flush()
		}
		des, err = f.ReadDir(lsChunk)
	}
	if err != io.EOF {
		_, msg := httpx.Status(err)
		enc.Encode(map[string]string{"error": msg})
	}
}
