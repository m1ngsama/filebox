package api

import (
	"io/fs"
	"net/http"
	"path"

	"github.com/m1ngsama/filebox/internal/auth"
	"github.com/m1ngsama/filebox/internal/db"
	"github.com/m1ngsama/filebox/internal/httpx"
	"github.com/m1ngsama/filebox/internal/serve"
	"github.com/m1ngsama/filebox/internal/version"
	"github.com/m1ngsama/filebox/internal/vol"
)

func (a *API) versions(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	v, rel, err := a.Vols.Resolve(q.Get("vol"), q.Get("p"))
	if err == nil && rel == "." {
		err = vol.ErrBadPath
	}
	if err != nil {
		httpx.Error(w, err)
		return
	}
	xs, err := a.Versions.List(v.Name, rel)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, 200, map[string]any{"versions": xs})
}

func (a *API) version(volName, id string) (*vol.Volume, version.Version, error) {
	v, ok := a.Vols.Get(volName)
	if !ok {
		return nil, version.Version{}, fs.ErrNotExist
	}
	x, err := a.Versions.Get(v.Name, id)
	return v, x, err
}

func (a *API) versionRaw(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	v, x, err := a.version(q.Get("vol"), q.Get("id"))
	if err != nil {
		httpx.Error(w, err)
		return
	}
	serve.Named(w, r, v.Root, version.File(x.ID), path.Base(x.Path), q.Has("dl"))
}

type versionRef struct {
	Vol string `json:"vol"`
	ID  string `json:"id"`
}

func (a *API) versionRestore(w http.ResponseWriter, r *http.Request) {
	var in versionRef
	if err := httpx.Read(r, &in); err != nil {
		httpx.Fail(w, 400, "bad request")
		return
	}
	v, x, err := a.version(in.Vol, in.ID)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	p, _ := auth.From(r.Context())
	dst, prev, err := a.Versions.Restore(v, x.ID, p.UserID)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	a.Index.Touch(v, dst)
	a.did(r, db.Event{Kind: db.EventRevert, Vol: v.Name, Path: dst})
	httpx.JSON(w, 200, map[string]string{"path": dst, "prev": prev})
}

func (a *API) versionDelete(w http.ResponseWriter, r *http.Request) {
	var in versionRef
	if err := httpx.Read(r, &in); err != nil {
		httpx.Fail(w, 400, "bad request")
		return
	}
	v, x, err := a.version(in.Vol, in.ID)
	if err == nil {
		err = a.Versions.Delete(v, x.ID)
	}
	if err != nil {
		httpx.Error(w, err)
		return
	}
	w.WriteHeader(204)
}
