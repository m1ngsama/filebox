package api

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"path"
	"sort"
	"time"

	"github.com/m1ngsama/filebox/internal/db"
	"github.com/m1ngsama/filebox/internal/httpx"
	"github.com/m1ngsama/filebox/internal/vol"
)

const originFile = ".origin"

type TrashItem struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Path    string `json:"path"`
	Dir     bool   `json:"dir"`
	Size    int64  `json:"size"`
	Deleted int64  `json:"deleted"`
}

func Trash(v *vol.Volume, rel string, now time.Time) (string, error) {
	if rel == "." {
		return "", vol.ErrBadPath
	}
	if _, err := v.Root.Lstat(rel); err != nil {
		return "", err
	}
	b := make([]byte, 4)
	rand.Read(b)
	id := fmt.Sprintf("%d-%s", now.UnixMilli(), hex.EncodeToString(b))
	dir := path.Join(vol.TrashDir, id)
	if err := v.Root.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	if err := v.Root.WriteFile(path.Join(dir, originFile), []byte(rel), 0o600); err != nil {
		v.Root.RemoveAll(dir)
		return "", err
	}
	if err := v.Root.Rename(rel, path.Join(dir, path.Base(rel))); err != nil {
		v.Root.RemoveAll(dir)
		return "", err
	}
	return id, nil
}

func Untrash(v *vol.Volume, id, rel string) error {
	dir := path.Join(vol.TrashDir, id)
	if err := v.Root.RemoveAll(rel); err != nil {
		return err
	}
	if err := v.Root.Rename(path.Join(dir, path.Base(rel)), rel); err != nil {
		return err
	}
	return v.Root.RemoveAll(dir)
}

func trashItem(v *vol.Volume, id string) (TrashItem, error) {
	if !vol.ValidName(id) {
		return TrashItem{}, vol.ErrBadPath
	}
	dir := path.Join(vol.TrashDir, id)
	origin, err := v.Root.ReadFile(path.Join(dir, originFile))
	if err != nil {
		return TrashItem{}, err
	}
	it := TrashItem{ID: id, Path: string(origin), Name: path.Base(string(origin))}
	fi, err := v.Root.Lstat(path.Join(dir, it.Name))
	if err != nil {
		return TrashItem{}, err
	}
	it.Dir, it.Size, it.Deleted = fi.IsDir(), fi.Size(), fi.ModTime().UnixMilli()
	if dfi, err := v.Root.Stat(dir); err == nil {
		it.Deleted = dfi.ModTime().UnixMilli()
	}
	return it, nil
}

func (a *API) trashList(w http.ResponseWriter, r *http.Request) {
	v, ok := a.Vols.Get(r.URL.Query().Get("vol"))
	if !ok {
		httpx.Error(w, fs.ErrNotExist)
		return
	}
	items := []TrashItem{}
	if f, err := v.Root.Open(vol.TrashDir); err == nil {
		des, _ := f.ReadDir(-1)
		f.Close()
		for _, de := range des {
			if vol.Purging(de.Name()) {
				continue
			}
			if it, err := trashItem(v, de.Name()); err == nil {
				items = append(items, it)
			}
		}
	}
	sort.Slice(items, func(i, j int) bool { return items[i].Deleted > items[j].Deleted })
	httpx.JSON(w, 200, map[string]any{"items": items})
}

func (a *API) trashRestore(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Vol string   `json:"vol"`
		ID  string   `json:"id"`
		IDs []string `json:"ids"`
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
	if in.IDs == nil {
		if err := a.restore(r, v, in.ID); err != nil {
			httpx.Error(w, err)
			return
		}
		w.WriteHeader(204)
		return
	}
	type failure struct {
		ID     string `json:"id"`
		Status int    `json:"status"`
		Error  string `json:"error"`
	}
	failed := []failure{}
	for _, id := range in.IDs {
		if err := a.restore(r, v, id); err != nil {
			code, msg := httpx.Status(err)
			failed = append(failed, failure{id, code, msg})
		}
	}
	httpx.JSON(w, 200, map[string]any{"failed": failed})
}

func (a *API) restore(r *http.Request, v *vol.Volume, id string) error {
	it, err := trashItem(v, id)
	if err != nil {
		return err
	}
	dst, err := v.Clean(it.Path)
	if err != nil {
		return err
	}
	if _, err := v.Root.Lstat(dst); err == nil {
		return fs.ErrExist
	}
	if err := v.Root.MkdirAll(path.Dir(dst), 0o755); err != nil {
		return err
	}
	dir := path.Join(vol.TrashDir, id)
	if err := vol.Move(v.Root, path.Join(dir, it.Name), dst); err != nil {
		return err
	}
	v.Root.RemoveAll(dir)
	a.Index.Touch(v, dst)
	a.did(r, db.Event{Kind: db.EventRestore, Vol: v.Name, Path: dst})
	return nil
}

func (a *API) trashDelete(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Vol string   `json:"vol"`
		IDs []string `json:"ids"`
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
	for _, id := range in.IDs {
		if !vol.ValidName(id) {
			httpx.Error(w, vol.ErrBadPath)
			return
		}
	}
	for _, id := range in.IDs {
		if _, err := v.PurgeTrash(id); err != nil && !errors.Is(err, fs.ErrNotExist) {
			httpx.Error(w, err)
			return
		}
	}
	w.WriteHeader(204)
}

func (a *API) trashEmpty(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Vol string `json:"vol"`
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
	f, err := v.Root.Open(vol.TrashDir)
	if errors.Is(err, fs.ErrNotExist) {
		w.WriteHeader(204)
		return
	}
	if err != nil {
		httpx.Error(w, err)
		return
	}
	names, err := f.Readdirnames(-1)
	f.Close()
	if err != nil {
		httpx.Error(w, err)
		return
	}
	for _, n := range names {
		if vol.Purging(n) {
			continue
		}
		if _, err := v.PurgeTrash(n); err != nil && !errors.Is(err, fs.ErrNotExist) {
			httpx.Error(w, err)
			return
		}
	}
	w.WriteHeader(204)
}
