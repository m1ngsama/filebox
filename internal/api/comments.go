package api

import (
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/m1ngsama/filebox/internal/auth"
	"github.com/m1ngsama/filebox/internal/db"
	"github.com/m1ngsama/filebox/internal/httpx"
)

const maxComment = 2000

var mention = regexp.MustCompile(`@([^\s@,.;:!?，。；：！？、]+)`)

// commented finds an item on a volume the caller may read, which is all it takes to talk about it.
func (a *API) commented(r *http.Request, vol, p string) (string, error) {
	v, rel, err := a.resolve(r, Loc{vol, p}, false)
	if err != nil {
		return "", err
	}
	if _, err := v.Root.Lstat(rel); err != nil {
		return "", err
	}
	return rel, nil
}

func (a *API) comments(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	rel, err := a.commented(r, q.Get("vol"), q.Get("path"))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	cs, err := a.DB.Comments(q.Get("vol"), rel)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	p, _ := auth.From(r.Context())
	out := []map[string]any{}
	for _, c := range cs {
		out = append(out, map[string]any{"id": c.ID, "at": c.At, "author": c.Author, "body": c.Body, "mine": c.UserID == p.UserID, "removable": c.UserID == p.UserID || p.Admin})
	}
	httpx.JSON(w, 200, map[string]any{"comments": out})
}

func (a *API) comment(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Vol  string `json:"vol"`
		Path string `json:"path"`
		Body string `json:"body"`
	}
	if err := httpx.Read(r, &in); err != nil {
		httpx.Fail(w, 400, "bad request")
		return
	}
	in.Body = strings.TrimSpace(in.Body)
	if n := utf8.RuneCountInString(in.Body); n == 0 || n > maxComment || strings.ContainsFunc(in.Body, func(c rune) bool { return unicode.IsControl(c) && c != '\n' && c != '\t' }) {
		httpx.Fail(w, 400, "bad comment")
		return
	}
	rel, err := a.commented(r, in.Vol, in.Path)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	p, _ := auth.From(r.Context())
	before, _ := a.DB.Commenters(in.Vol, rel)
	c, err := a.DB.AddComment(in.Vol, rel, p.UserID, in.Body, time.Now().Unix())
	if err != nil {
		httpx.Error(w, err)
		return
	}
	if a.Live != nil {
		a.Live.Commented(in.Vol, rel)
		a.tell(in.Vol, rel, c, before)
	}
	httpx.JSON(w, 201, map[string]any{"id": c.ID, "at": c.At, "author": c.Author, "body": c.Body, "mine": true, "removable": true})
}

// tell lets the people named in a comment know, and everyone who already took part in the conversation,
// as long as they can still read the item.
func (a *API) tell(vol, rel string, c db.Comment, before []int64) {
	as, err := a.DB.Accounts()
	if err != nil {
		return
	}
	byName := map[string]int64{}
	for _, u := range as {
		byName[strings.ToLower(u.Name)] = u.ID
	}
	kinds := map[int64]string{}
	for _, m := range mention.FindAllStringSubmatch(c.Body, -1) {
		if id, ok := byName[strings.ToLower(m[1])]; ok {
			kinds[id] = "mention"
		}
	}
	for _, id := range before {
		if _, ok := kinds[id]; !ok {
			kinds[id] = "comment"
		}
	}
	delete(kinds, c.UserID)
	name := rel[strings.LastIndexByte(rel, '/')+1:]
	for id, kind := range kinds {
		if p, ok := a.Auth.Of(id); ok && p.Can(vol, false) {
			a.Live.Notice(id, db.Notice{At: c.At, Kind: kind, Name: name, Vol: vol, Path: rel, Actor: c.Author})
		}
	}
}

func (a *API) uncomment(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpx.Fail(w, 400, "bad id")
		return
	}
	c, vol, rel, err := a.DB.Comment(id)
	if err == nil {
		err = auth.Allow(r.Context(), vol, false)
	}
	if err != nil {
		httpx.Error(w, err)
		return
	}
	if p, _ := auth.From(r.Context()); c.UserID != p.UserID && !p.Admin {
		httpx.Fail(w, 403, "forbidden")
		return
	}
	if err := a.DB.DeleteComment(id); err != nil {
		httpx.Error(w, err)
		return
	}
	if a.Live != nil {
		a.Live.Commented(vol, rel)
	}
	w.WriteHeader(204)
}

// people lists who can read a volume, so a comment there can name them.
func (a *API) people(w http.ResponseWriter, r *http.Request) {
	vol := r.URL.Query().Get("vol")
	if err := auth.Allow(r.Context(), vol, false); err != nil {
		httpx.Error(w, err)
		return
	}
	as, err := a.DB.Accounts()
	if err != nil {
		httpx.Error(w, err)
		return
	}
	names := []string{}
	for _, u := range as {
		if p, ok := a.Auth.Of(u.ID); ok && p.Can(vol, false) {
			names = append(names, u.Name)
		}
	}
	httpx.JSON(w, 200, map[string]any{"people": names})
}
