package api

import (
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/m1ngsama/filebox/internal/auth"
	"github.com/m1ngsama/filebox/internal/httpx"
)

// searchKeys are the parts of a search that can be saved: the words and the filters of search everywhere.
var searchKeys = map[string]bool{"q": true, "kind": true, "when": true, "size": true, "tag": true}

// cleanSearch keeps only known parts of a saved search, in a stable order.
func cleanSearch(raw string) (string, bool) {
	v, err := url.ParseQuery(raw)
	if err != nil || len(raw) > 500 {
		return "", false
	}
	out := url.Values{}
	for k, vs := range v {
		if !searchKeys[k] || len(vs) != 1 {
			return "", false
		}
		if vs[0] != "" {
			out.Set(k, vs[0])
		}
	}
	if len(out) == 0 {
		return "", false
	}
	return out.Encode(), true
}

func (a *API) searches(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.From(r.Context())
	ss, err := a.DB.Searches(p.UserID)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, 200, map[string]any{"searches": ss})
}

func (a *API) saveSearch(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Name   string `json:"name"`
		Params string `json:"params"`
	}
	if err := httpx.Read(r, &in); err != nil {
		httpx.Fail(w, 400, "bad request")
		return
	}
	in.Name = strings.TrimSpace(in.Name)
	params, ok := cleanSearch(in.Params)
	if n := utf8.RuneCountInString(in.Name); !ok || n == 0 || n > 64 || strings.ContainsFunc(in.Name, unicode.IsControl) {
		httpx.Fail(w, 400, "bad search")
		return
	}
	p, _ := auth.From(r.Context())
	s, err := a.DB.AddSearch(p.UserID, in.Name, params, time.Now().Unix())
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, 201, s)
}

func (a *API) dropSearch(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpx.Fail(w, 400, "bad id")
		return
	}
	p, _ := auth.From(r.Context())
	if err := a.DB.DeleteSearch(p.UserID, id); err != nil {
		httpx.Error(w, err)
		return
	}
	w.WriteHeader(204)
}
