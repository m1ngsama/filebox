package live

import (
	"log/slog"
	"strconv"
	"time"

	"github.com/m1ngsama/filebox/internal/db"
)

const (
	downloadQuiet = time.Hour
	uploadQuiet   = 10 * time.Minute
)

// event turns logged activity into notices for the people it concerns: the owner of a share, and the admins.
func (h *Hub) event(e db.Event) {
	at := e.At
	if at == 0 {
		at = time.Now().Unix()
	}
	switch {
	case e.Kind == db.EventDownload && e.ShareID != 0 && e.UserID != 0:
		if h.fresh("d|"+strconv.FormatInt(e.ShareID, 10)+"|"+e.Visitor+"|"+e.Name, at, downloadQuiet) {
			go h.notice(e.UserID, db.Notice{At: at, Kind: "share_download", Name: e.Name, Share: e.Target})
		}
	case e.Kind == db.EventUpload && e.ShareID != 0 && e.UserID != 0:
		if h.fresh("u|"+strconv.FormatInt(e.ShareID, 10)+"|"+e.Name, at, uploadQuiet) {
			go h.notice(e.UserID, db.Notice{At: at, Kind: "share_upload", Name: e.Name, Vol: e.Vol, Path: e.Path, Share: e.Target})
		}
	case e.Kind == db.EventJoin:
		go h.admins(db.Notice{At: at, Kind: "joined", Name: e.Name}, e.UserID)
	}
}

// DiskLow tells the admins a volume is running short of space.
func (h *Hub) DiskLow(vol string, free int64) {
	h.admins(db.Notice{At: time.Now().Unix(), Kind: "disk_low", Vol: vol, Count: max(free, 1)}, 0)
}

func (h *Hub) admins(n db.Notice, except int64) {
	ids, err := h.DB.Admins()
	if err != nil {
		slog.Warn("notice", "err", err)
		return
	}
	for _, id := range ids {
		if id != except {
			h.notice(id, n)
		}
	}
}

// Notice stores a notice for one person and tells their open tabs.
func (h *Hub) Notice(userID int64, n db.Notice) { h.notice(userID, n) }

// Commented tells the tabs of everyone who can read an item that what was said about it changed.
func (h *Hub) Commented(vol, rel string) {
	h.mu.Lock()
	subs := make([]*sub, 0, len(h.subs))
	for s := range h.subs {
		subs = append(subs, s)
	}
	h.mu.Unlock()
	for _, s := range subs {
		if p, ok := h.Auth.Of(s.user); ok && p.Can(vol, false) {
			h.send(s, Event{Type: "comment", Vol: vol, Dir: rel})
		}
	}
}

func (h *Hub) notice(userID int64, n db.Notice) {
	n, err := h.DB.AddNotice(userID, n)
	if err != nil {
		slog.Warn("notice", "user", userID, "err", err)
		return
	}
	h.toUser(userID, Event{Type: "notice", Notice: &n})
	h.unread(userID)
}

// fresh drops a repeat of the same thing within quiet, so a busy link does not bury everything else.
func (h *Hub) fresh(key string, at int64, quiet time.Duration) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if last, ok := h.seen[key]; ok && at-last < int64(quiet/time.Second) {
		return false
	}
	if len(h.seen) > 1<<14 {
		clear(h.seen)
	}
	h.seen[key] = at
	return true
}
