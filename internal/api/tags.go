package api

import (
	"io/fs"
	"net/http"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/m1ngsama/filebox/internal/httpx"
	"github.com/m1ngsama/filebox/internal/vol"
)

var tagColors = map[string]bool{"": true, "red": true, "orange": true, "yellow": true, "green": true, "teal": true, "blue": true, "purple": true, "pink": true, "gray": true}

type tagIn struct {
	Name  string `json:"name"`
	Color string `json:"color"`
}

func (in *tagIn) valid() bool {
	in.Name = strings.Join(strings.Fields(in.Name), " ")
	n := utf8.RuneCountInString(in.Name)
	return n > 0 && n <= 64 && tagColors[in.Color] && !strings.ContainsFunc(in.Name, unicode.IsControl)
}

func tagID(r *http.Request) int64 {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	return id
}

func (a *API) tags(w http.ResponseWriter, r *http.Request) {
	ts, err := a.Index.Tags()
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, 200, map[string]any{"tags": ts})
}

func (a *API) tagNew(w http.ResponseWriter, r *http.Request) {
	var in tagIn
	if err := httpx.Read(r, &in); err != nil || !in.valid() {
		httpx.Fail(w, 400, "bad tag")
		return
	}
	t, err := a.Index.NewTag(in.Name, in.Color)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, 201, t)
}

func (a *API) tagEdit(w http.ResponseWriter, r *http.Request) {
	var in tagIn
	if err := httpx.Read(r, &in); err != nil || !in.valid() {
		httpx.Fail(w, 400, "bad tag")
		return
	}
	if err := a.Index.EditTag(tagID(r), in.Name, in.Color); err != nil {
		httpx.Error(w, err)
		return
	}
	w.WriteHeader(204)
}

func (a *API) tagDel(w http.ResponseWriter, r *http.Request) {
	if err := a.Index.DeleteTag(tagID(r)); err != nil {
		httpx.Error(w, err)
		return
	}
	w.WriteHeader(204)
}

func (a *API) tagItems(w http.ResponseWriter, r *http.Request) {
	fs, err := a.Index.Tagged(tagID(r))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, 200, map[string]any{"entries": a.located(fs)})
}

func (a *API) tagApply(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Vol   string   `json:"vol"`
		Paths []string `json:"paths"`
		On    bool     `json:"on"`
	}
	if err := httpx.Read(r, &in); err != nil {
		httpx.Fail(w, 400, "bad request")
		return
	}
	v, ok := a.Vols.Get(in.Vol)
	if !ok {
		httpx.Error(w, fs.ErrNotExist)
		return
	}
	rels := make([]string, 0, len(in.Paths))
	for _, p := range in.Paths {
		rel, err := vol.Clean(p)
		if err == nil && rel == "." {
			err = vol.ErrBadPath
		}
		if err == nil && in.On {
			_, err = v.Root.Lstat(rel)
		}
		if err != nil {
			httpx.Error(w, err)
			return
		}
		rels = append(rels, rel)
	}
	if err := a.Index.TagItems(tagID(r), v.Name, rels, in.On); err != nil {
		httpx.Error(w, err)
		return
	}
	w.WriteHeader(204)
}
