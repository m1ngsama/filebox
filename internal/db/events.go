package db

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strings"
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

func (d *DB) Visitor(ip string) string {
	k, err := d.visitorKey()
	if err != nil || ip == "" {
		return ""
	}
	m := hmac.New(sha256.New, k)
	m.Write([]byte(ip))
	return hex.EncodeToString(m.Sum(nil)[:8])
}

func (d *DB) Log(e Event) error {
	if e.ShareID != 0 && e.UserID == 0 {
		d.QueryRow(`SELECT user_id FROM shares WHERE id = ?`, e.ShareID).Scan(&e.UserID)
	}
	if e.Kind != EventView {
		_, err := d.Exec(`INSERT INTO events (at, user_id, kind, share_id, visitor, name, size) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			e.At, e.UserID, e.Kind, e.ShareID, e.Visitor, e.Name, e.Size)
		return err
	}
	tx, err := d.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	n, err := affected(tx.Exec(`INSERT OR IGNORE INTO events (at, user_id, kind, share_id, visitor) VALUES (?, ?, ?, ?, ?)`,
		e.At, e.UserID, e.Kind, e.ShareID, e.Visitor))
	if err != nil {
		return err
	}
	if n > 0 {
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
