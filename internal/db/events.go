package db

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	EventDownload    = "download"
	EventUpload      = "upload"
	EventLogin       = "login"
	EventLoginFailed = "login_failed"
	EventShareCreate = "share_create"
	EventShareEdit   = "share_edit"
	EventShareDelete = "share_delete"
	EventTokenCreate = "token_create"
	EventTokenRevoke = "token_revoke"

	MaxEvents     = 500_000
	maxSeen       = 1 << 16
	queueSize     = 4096
	flushInterval = 2 * time.Second
	pruneBatch    = 10_000
)

type Event struct {
	ID, At, UserID, ShareID     int64
	Kind, Visitor, Name, Target string
	Size                        int64
}

type visitorKey struct {
	mu  sync.Mutex
	day int64
	key []byte
}

func (v *visitorKey) at(day int64) []byte {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.key == nil || day > v.day {
		v.day, v.key = day, make([]byte, 32)
		rand.Read(v.key)
	}
	return v.key
}

func (d *DB) Visitor(ip string, now int64) string { return d.visitor(ip, now/86400) }

func (d *DB) visitor(ip string, day int64) string {
	a, err := netip.ParseAddr(ip)
	if err != nil {
		return ""
	}
	a = a.Unmap()
	if a.Is6() {
		a = netip.PrefixFrom(a, 64).Masked().Addr()
	}
	m := hmac.New(sha256.New, d.visitors.at(day))
	m.Write(a.AsSlice())
	return hex.EncodeToString(m.Sum(nil)[:8])
}

type item struct {
	ev   Event
	view bool
	done chan struct{}
}

var anonymous = map[string]bool{EventDownload: true, EventUpload: true, EventLoginFailed: true}

const anonymousKinds = `('download', 'upload', 'login_failed')`

type writer struct {
	d       *DB
	ch      chan item
	audit   chan item
	quit    chan struct{}
	exited  chan struct{}
	once    sync.Once
	dropped atomic.Int64
	mu      sync.Mutex
	day     int64
	seen    map[string]struct{}
}

func newWriter(d *DB) *writer {
	w := &writer{d: d, ch: make(chan item, queueSize), audit: make(chan item, 256), quit: make(chan struct{}), exited: make(chan struct{}), seen: map[string]struct{}{}}
	go w.run()
	return w
}

func (w *writer) send(it item) bool {
	select {
	case w.ch <- it:
		return true
	default:
		w.dropped.Add(1)
		return false
	}
}

func (w *writer) run() {
	defer close(w.exited)
	t := time.NewTicker(flushInterval)
	defer t.Stop()
	var batch []item
	for {
		select {
		case it := <-w.audit:
			batch = append(batch, it)
			if len(batch) < 512 {
				continue
			}
		case it := <-w.ch:
			batch = append(batch, it)
			if it.done == nil && len(batch) < 512 {
				continue
			}
			for len(w.audit) > 0 {
				batch = append(batch, <-w.audit)
			}
		case <-t.C:
		case <-w.quit:
			for len(w.ch) > 0 || len(w.audit) > 0 {
				select {
				case it := <-w.ch:
					batch = append(batch, it)
				case it := <-w.audit:
					batch = append(batch, it)
				}
			}
			w.write(batch)
			return
		}
		w.write(batch)
		batch = batch[:0]
	}
}

func (w *writer) write(batch []item) {
	if n := w.dropped.Swap(0); n > 0 {
		slog.Warn("activity log queue full, events dropped", "count", n)
	}
	var err error
	if len(batch) > 0 {
		err = w.insert(batch)
	}
	if err != nil {
		slog.Error("activity log", "events", len(batch), "err", err)
	}
	for _, it := range batch {
		if it.done != nil {
			close(it.done)
		}
	}
}

func (w *writer) insert(batch []item) error {
	tx, err := w.d.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, it := range batch {
		e := it.ev
		switch {
		case it.done != nil:
			continue
		case it.view:
			if _, err := tx.Exec(`INSERT INTO share_views (share_id, day, n) VALUES (?, ?, 1)
				ON CONFLICT DO UPDATE SET n = n + 1`, e.ShareID, e.At/86400); err != nil {
				return err
			}
			if _, err := tx.Exec(`UPDATE shares SET views = views + 1 WHERE id = ?`, e.ShareID); err != nil {
				return err
			}
		default:
			if _, err := tx.Exec(`INSERT OR IGNORE INTO events (at, user_id, kind, share_id, visitor, name, target, size)
				VALUES (?1, iif(?2 = 0, coalesce((SELECT user_id FROM shares WHERE id = ?4), 0), ?2), ?3, ?4, ?5, ?6, ?7, ?8)`,
				e.At, e.UserID, e.Kind, e.ShareID, e.Visitor, e.Name, e.Target, e.Size); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

func (w *writer) stop() {
	w.once.Do(func() {
		close(w.quit)
		<-w.exited
	})
}

func (d *DB) Log(e Event) {
	if anonymous[e.Kind] {
		d.events.send(item{ev: e})
		return
	}
	select {
	case d.events.audit <- item{ev: e}:
	case <-d.events.exited:
		slog.Error("activity log closed, audit event lost", "kind", e.Kind)
	}
}

func (d *DB) View(shareID int64, visitor string, now int64) {
	w := d.events
	key := strconv.FormatInt(shareID, 10) + ":" + visitor
	w.mu.Lock()
	if day := now / 86400; day > w.day {
		w.day, w.seen = day, map[string]struct{}{}
	}
	_, dup := w.seen[key]
	full := len(w.seen) >= maxSeen
	if !dup && !full {
		w.seen[key] = struct{}{}
	}
	w.mu.Unlock()
	if !dup && !full {
		w.send(item{ev: Event{At: now, ShareID: shareID}, view: true})
	}
}

func (d *DB) Flush() {
	w := d.events
	select {
	case <-w.exited:
		return
	default:
	}
	done := make(chan struct{})
	select {
	case w.ch <- item{done: done}:
	case <-w.exited:
		return
	}
	select {
	case <-done:
	case <-w.exited:
	}
}

type EventFilter struct {
	UserID, ShareID, Before int64
	Kinds                   []string
	Limit                   int
}

func (d *DB) Events(f EventFilter) ([]Event, error) {
	q := `SELECT id, at, user_id, share_id, kind, visitor, name, target, size FROM events
		WHERE (user_id = ?1 OR (user_id = 0 AND ?1 = (SELECT min(id) FROM users)))`
	args := []any{f.UserID}
	if f.ShareID != 0 {
		q += ` AND share_id = ?`
		args = append(args, f.ShareID)
	}
	if len(f.Kinds) > 0 {
		q += ` AND kind IN (?` + strings.Repeat(`, ?`, len(f.Kinds)-1) + `)`
		for _, k := range f.Kinds {
			args = append(args, k)
		}
	}
	if f.Before > 0 {
		q += ` AND id < ?`
		args = append(args, f.Before)
	}
	q += ` ORDER BY id DESC LIMIT ?`
	args = append(args, f.Limit)
	rows, err := d.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Event
	for rows.Next() {
		var e Event
		if err := rows.Scan(&e.ID, &e.At, &e.UserID, &e.ShareID, &e.Kind, &e.Visitor, &e.Name, &e.Target, &e.Size); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (d *DB) PruneEvents(before int64, keep int) (int64, error) {
	var total int64
	del := func(q string, args ...any) error {
		for {
			n, err := affected(d.Exec(q, args...))
			total += n
			if err != nil || n < pruneBatch {
				return err
			}
		}
	}
	if err := del(`DELETE FROM events WHERE id IN (SELECT id FROM events WHERE at < ? LIMIT ?)`, before, pruneBatch); err != nil {
		return total, err
	}
	if _, err := d.Exec(`DELETE FROM share_views WHERE day < ?`, before/86400); err != nil {
		return total, err
	}
	var n int64
	if err := d.QueryRow(`SELECT count(*) FROM events WHERE kind IN ` + anonymousKinds).Scan(&n); err != nil {
		return total, err
	}
	for over := n - int64(keep); over > 0; over -= pruneBatch {
		m, err := affected(d.Exec(`DELETE FROM events WHERE id IN (SELECT id FROM events WHERE kind IN `+anonymousKinds+` ORDER BY id LIMIT ?)`, min(over, pruneBatch)))
		total += m
		if err != nil {
			return total, err
		}
	}
	return total, nil
}
