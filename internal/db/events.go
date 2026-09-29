package db

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"net/netip"
	"strings"
	"sync"
	"time"
)

const (
	EventView        = "view"
	EventDownload    = "download"
	EventUpload      = "upload"
	EventLogin       = "login"
	EventLoginFailed = "login_failed"
	EventShareCreate = "share_create"
	EventShareEdit   = "share_edit"
	EventShareDelete = "share_delete"
	EventTokenCreate = "token_create"
	EventTokenRevoke = "token_revoke"
)

type Event struct {
	ID, At, UserID, ShareID int64
	Kind, Visitor, Name     string
	Size                    int64
	Share                   string
}

type visitorKey struct {
	mu  sync.Mutex
	day int64
	key []byte
}

func (v *visitorKey) at(day int64) []byte {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.key == nil || v.day != day {
		v.day, v.key = day, make([]byte, 32)
		rand.Read(v.key)
	}
	return v.key
}

func (d *DB) Visitor(ip string) string { return d.visitor(ip, time.Now().Unix()/86400) }

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

func (d *DB) Log(e Event) error {
	if e.ShareID != 0 && e.UserID == 0 {
		d.QueryRow(`SELECT user_id FROM shares WHERE id = ?`, e.ShareID).Scan(&e.UserID)
	}
	tx, err := d.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	n, err := affected(tx.Exec(`INSERT OR IGNORE INTO events (at, user_id, kind, share_id, visitor, name, size) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		e.At, e.UserID, e.Kind, e.ShareID, e.Visitor, e.Name, e.Size))
	if err != nil {
		return err
	}
	if n > 0 && e.Kind == EventView {
		if _, err := tx.Exec(`UPDATE shares SET views = views + 1 WHERE id = ?`, e.ShareID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

type EventFilter struct {
	UserID, ShareID, Before int64
	Kinds                   []string
	Limit                   int
}

func (d *DB) Events(f EventFilter) ([]Event, error) {
	q := `SELECT e.id, e.at, e.user_id, e.share_id, e.kind, e.visitor, e.name, e.size,
		coalesce(s.vol || ':/' || iif(s.path = '.', '', s.path), '')
		FROM events e LEFT JOIN shares s ON s.id = e.share_id WHERE e.user_id IN (?, 0)`
	args := []any{f.UserID}
	if f.ShareID != 0 {
		q += ` AND e.share_id = ?`
		args = append(args, f.ShareID)
	}
	if len(f.Kinds) > 0 {
		q += ` AND e.kind IN (?` + strings.Repeat(`, ?`, len(f.Kinds)-1) + `)`
		for _, k := range f.Kinds {
			args = append(args, k)
		}
	}
	if f.Before > 0 {
		q += ` AND e.id < ?`
		args = append(args, f.Before)
	}
	q += ` ORDER BY e.id DESC LIMIT ?`
	args = append(args, f.Limit)
	rows, err := d.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Event
	for rows.Next() {
		var e Event
		if err := rows.Scan(&e.ID, &e.At, &e.UserID, &e.ShareID, &e.Kind, &e.Visitor, &e.Name, &e.Size, &e.Share); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (d *DB) PruneEvents(before int64) (int64, error) {
	return affected(d.Exec(`DELETE FROM events WHERE at < ?`, before))
}
