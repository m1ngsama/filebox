package api

import (
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/m1ngsama/filebox/internal/auth"
	"github.com/m1ngsama/filebox/internal/db"
	"github.com/m1ngsama/filebox/internal/httpx"
	"github.com/m1ngsama/filebox/internal/notify"
)

// where hides the path and query of a webhook URL, which is where services keep their secrets.
func where(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Scheme + "://" + u.Host
}

func goodKinds(kinds []string) bool {
	if len(kinds) == 0 {
		return false
	}
	for _, k := range kinds {
		if !slices.Contains(notify.Kinds, k) {
			return false
		}
	}
	return true
}

func (a *API) webhooks(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.From(r.Context())
	hs, err := a.DB.Webhooks(p.UserID)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	out := []map[string]any{}
	for _, h := range hs {
		out = append(out, map[string]any{"id": h.ID, "url": where(h.URL), "format": h.Format, "kinds": h.Kinds, "lang": h.Lang,
			"signed": h.Secret != "", "created": h.CreatedAt, "last_at": h.LastAt, "last_status": h.LastStatus})
	}
	httpx.JSON(w, 200, map[string]any{"webhooks": out, "kinds": notify.Kinds, "max": notify.MaxHooks()})
}

func (a *API) webhookNew(w http.ResponseWriter, r *http.Request) {
	var in struct {
		URL    string   `json:"url"`
		Format string   `json:"format"`
		Kinds  []string `json:"kinds"`
		Secret string   `json:"secret"`
		Lang   string   `json:"lang"`
	}
	if err := httpx.Read(r, &in); err != nil || !notify.ValidURL(strings.TrimSpace(in.URL)) || !slices.Contains(notify.Formats, in.Format) ||
		!goodKinds(in.Kinds) || len(in.Secret) > 200 || (in.Lang != "en" && in.Lang != "zh") {
		httpx.Fail(w, 400, "bad request")
		return
	}
	p, _ := auth.From(r.Context())
	if n, err := a.DB.CountWebhooks(p.UserID); err != nil {
		httpx.Error(w, err)
		return
	} else if n >= notify.MaxHooks() {
		httpx.Fail(w, 409, "too many webhooks")
		return
	}
	id, err := a.DB.AddWebhook(db.Webhook{UserID: p.UserID, URL: strings.TrimSpace(in.URL), Format: in.Format, Kinds: in.Kinds, Secret: in.Secret, Lang: in.Lang}, time.Now().Unix())
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, 201, map[string]int64{"id": id})
}

func (a *API) webhookEdit(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	var in struct {
		Kinds []string `json:"kinds"`
	}
	if err != nil || httpx.Read(r, &in) != nil || !goodKinds(in.Kinds) {
		httpx.Fail(w, 400, "bad request")
		return
	}
	p, _ := auth.From(r.Context())
	if err := a.DB.EditWebhook(p.UserID, id, in.Kinds); err != nil {
		httpx.Error(w, err)
		return
	}
	w.WriteHeader(204)
}

func (a *API) webhookDel(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpx.Fail(w, 400, "bad id")
		return
	}
	p, _ := auth.From(r.Context())
	if err := a.DB.DeleteWebhook(p.UserID, id); err != nil {
		httpx.Error(w, err)
		return
	}
	w.WriteHeader(204)
}

func (a *API) webhookTest(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		httpx.Fail(w, 400, "bad id")
		return
	}
	p, _ := auth.From(r.Context())
	h, err := a.DB.Webhook(p.UserID, id)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	if err := a.Notify.Test(r.Context(), h); err != nil {
		httpx.Fail(w, 502, err.Error())
		return
	}
	w.WriteHeader(204)
}
