// Package live tells open browser tabs what changed as it happens, over Server-Sent Events,
// and keeps each person's notices: things they should hear about inside filebox.
package live

import (
	"encoding/json"
	"fmt"
	"net/http"
	"path"
	"strconv"
	"sync"
	"time"

	"github.com/m1ngsama/filebox/internal/auth"
	"github.com/m1ngsama/filebox/internal/db"
	"github.com/m1ngsama/filebox/internal/httpx"
)

const (
	settle     = 150 * time.Millisecond
	keepAlive  = 25 * time.Second
	perUser    = 16
	queue      = 256
	noticePage = 50
)

// Event is one message on the stream. Type is "fs" when a folder's listing changed, "notice" for a new notice,
// and "unread" when the count of unread notices changed.
type Event struct {
	Type   string     `json:"type"`
	Vol    string     `json:"vol,omitempty"`
	Dir    string     `json:"dir,omitempty"`
	Notice *db.Notice `json:"notice,omitempty"`
	Unread *int       `json:"unread,omitempty"`
	People []string   `json:"people,omitempty"`
}

type sub struct {
	user   int64
	ch     chan Event
	closed chan struct{}
	once   sync.Once
}

func (s *sub) kill() { s.once.Do(func() { close(s.closed) }) }

type Hub struct {
	DB   *db.DB
	Auth *auth.Auth

	mu      sync.Mutex
	subs    map[*sub]struct{}
	pending map[[2]string]bool
	timer   *time.Timer
	seen    map[string]int64
	places  map[int64]place
	done    chan struct{}
	stop    sync.Once
}

func New(d *db.DB, a *auth.Auth) *Hub {
	h := &Hub{DB: d, Auth: a, subs: map[*sub]struct{}{}, pending: map[[2]string]bool{}, seen: map[string]int64{}, places: map[int64]place{}, done: make(chan struct{})}
	d.Observe(h.event)
	go h.sweep()
	return h
}

// Stop ends every open stream, so a server shutting down need not wait for them.
func (h *Hub) Stop() { h.stop.Do(func() { close(h.done) }) }

func (h *Hub) Register(mux *http.ServeMux) {
	mux.Handle("GET /api/events", h.Auth.RequireSession(http.HandlerFunc(h.stream)))
	mux.Handle("GET /api/notices", h.Auth.RequireSession(http.HandlerFunc(h.notices)))
	mux.Handle("POST /api/notices/read", h.Auth.RequireSession(http.HandlerFunc(h.read)))
	mux.Handle("POST /api/presence", h.Auth.RequireSession(http.HandlerFunc(h.here)))
}

// Changed is told about a path filebox changed. The folder holding it, and the path itself when it is a folder,
// are announced once the burst of changes it belongs to has settled.
func (h *Hub) Changed(vol, rel string) {
	dir := path.Dir(rel)
	if dir == "." {
		dir = ""
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.pending[[2]string{vol, dir}] = true
	if rel != "." {
		h.pending[[2]string{vol, rel}] = true
	}
	if h.timer == nil {
		h.timer = time.AfterFunc(settle, h.flush)
	}
}

func (h *Hub) flush() {
	h.mu.Lock()
	batch := h.pending
	h.pending, h.timer = map[[2]string]bool{}, nil
	subs := make([]*sub, 0, len(h.subs))
	for s := range h.subs {
		subs = append(subs, s)
	}
	h.mu.Unlock()
	for _, s := range subs {
		p, ok := h.Auth.Of(s.user)
		if !ok {
			s.kill()
			continue
		}
		for k := range batch {
			if p.Can(k[0], false) {
				h.send(s, Event{Type: "fs", Vol: k[0], Dir: k[1]})
			}
		}
	}
}

// send never blocks: a tab too slow to keep up loses its stream, reconnects and reloads what it shows.
func (h *Hub) send(s *sub, e Event) {
	select {
	case s.ch <- e:
	default:
		s.kill()
	}
}

func (h *Hub) toUser(userID int64, e Event) {
	h.mu.Lock()
	var subs []*sub
	for s := range h.subs {
		if s.user == userID {
			subs = append(subs, s)
		}
	}
	h.mu.Unlock()
	for _, s := range subs {
		h.send(s, e)
	}
}

func (h *Hub) add(s *sub) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	n := 0
	for o := range h.subs {
		if o.user == s.user {
			n++
		}
	}
	if n >= perUser {
		return false
	}
	h.subs[s] = struct{}{}
	return true
}

func (h *Hub) remove(s *sub) {
	h.mu.Lock()
	delete(h.subs, s)
	h.mu.Unlock()
}

func (h *Hub) stream(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.From(r.Context())
	fl, ok := w.(http.Flusher)
	if !ok {
		httpx.Fail(w, 500, "streaming unsupported")
		return
	}
	s := &sub{user: p.UserID, ch: make(chan Event, queue), closed: make(chan struct{})}
	if !h.add(s) {
		httpx.Fail(w, 429, "too many open tabs")
		return
	}
	defer h.remove(s)
	hd := w.Header()
	hd.Set("Content-Type", "text/event-stream")
	hd.Set("Cache-Control", "no-store")
	hd.Set("X-Accel-Buffering", "no")
	w.WriteHeader(200)
	unread, _ := h.DB.Unread(p.UserID)
	fmt.Fprint(w, "retry: 3000\n\n")
	write(w, Event{Type: "unread", Unread: &unread})
	fl.Flush()
	tick := time.NewTicker(keepAlive)
	defer tick.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-h.done:
			return
		case <-s.closed:
			return
		case e := <-s.ch:
			write(w, e)
		case <-tick.C:
			fmt.Fprint(w, ": ping\n\n")
		}
		fl.Flush()
	}
}

func write(w http.ResponseWriter, e Event) {
	b, _ := json.Marshal(e)
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", e.Type, b)
}

func (h *Hub) notices(w http.ResponseWriter, r *http.Request) {
	p, _ := auth.From(r.Context())
	before, _ := strconv.ParseInt(r.URL.Query().Get("before"), 10, 64)
	ns, err := h.DB.Notices(p.UserID, before, noticePage)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	unread, err := h.DB.Unread(p.UserID)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	httpx.JSON(w, 200, map[string]any{"notices": ns, "unread": unread, "more": len(ns) == noticePage})
}

func (h *Hub) read(w http.ResponseWriter, r *http.Request) {
	var in struct {
		IDs []int64 `json:"ids"`
	}
	if err := httpx.Read(r, &in); err != nil || len(in.IDs) > 500 {
		httpx.Fail(w, 400, "bad request")
		return
	}
	p, _ := auth.From(r.Context())
	if err := h.DB.MarkRead(p.UserID, in.IDs); err != nil {
		httpx.Error(w, err)
		return
	}
	h.unread(p.UserID)
	w.WriteHeader(204)
}

func (h *Hub) unread(userID int64) {
	if n, err := h.DB.Unread(userID); err == nil {
		h.toUser(userID, Event{Type: "unread", Unread: &n})
	}
}
