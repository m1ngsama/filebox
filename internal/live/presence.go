package live

import (
	"net/http"
	"slices"
	"time"

	"github.com/m1ngsama/filebox/internal/auth"
	"github.com/m1ngsama/filebox/internal/httpx"
)

// A person counts as looking at a folder while a tab of theirs in front says so at least this often.
const presenceTTL = 70 * time.Second

type place struct {
	vol, dir string
	name     string
	at       time.Time
}

// here records where a person's open tab is, and tells everyone who can see that folder who else is in it.
func (h *Hub) here(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Vol string `json:"vol"`
		Dir string `json:"dir"`
	}
	if err := httpx.Read(r, &in); err != nil || len(in.Dir) > 4096 {
		httpx.Fail(w, 400, "bad request")
		return
	}
	p, _ := auth.From(r.Context())
	if in.Vol != "" && !p.Can(in.Vol, false) {
		httpx.Fail(w, 404, "not found")
		return
	}
	u, err := h.DB.UserByID(p.UserID)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	h.mu.Lock()
	old, had := h.places[p.UserID]
	if in.Vol == "" {
		delete(h.places, p.UserID)
	} else {
		h.places[p.UserID] = place{in.Vol, in.Dir, u.Name, time.Now()}
	}
	h.mu.Unlock()
	if had && (old.vol != in.Vol || old.dir != in.Dir) {
		h.present(old.vol, old.dir)
	}
	if in.Vol != "" {
		h.present(in.Vol, in.Dir)
	}
	w.WriteHeader(204)
}

// present tells the tabs that can see a folder who is in it now.
func (h *Hub) present(vol, dir string) {
	h.mu.Lock()
	people := []string{}
	for _, pl := range h.places {
		if pl.vol == vol && pl.dir == dir {
			people = append(people, pl.name)
		}
	}
	subs := make([]*sub, 0, len(h.subs))
	for s := range h.subs {
		subs = append(subs, s)
	}
	h.mu.Unlock()
	slices.Sort(people)
	for _, s := range subs {
		if p, ok := h.Auth.Of(s.user); ok && p.Can(vol, false) {
			h.send(s, Event{Type: "presence", Vol: vol, Dir: dir, People: people})
		}
	}
}

// sweep forgets people whose tabs stopped saying where they are.
func (h *Hub) sweep() {
	t := time.NewTicker(presenceTTL / 2)
	defer t.Stop()
	for {
		select {
		case <-h.done:
			return
		case <-t.C:
		}
		h.mu.Lock()
		var gone []place
		for id, pl := range h.places {
			if time.Since(pl.at) > presenceTTL {
				gone = append(gone, pl)
				delete(h.places, id)
			}
		}
		h.mu.Unlock()
		for _, pl := range gone {
			h.present(pl.vol, pl.dir)
		}
	}
}
