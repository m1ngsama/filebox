package db

import "strings"

type Webhook struct {
	ID, UserID, CreatedAt, LastAt int64
	URL, Format, Secret, Lang     string
	LastStatus                    string
	Kinds                         []string
}

const webhookCols = `id, user_id, url, format, kinds, secret, lang, created_at, last_at, last_status`

type scanner interface{ Scan(...any) error }

func scanWebhook(s scanner) (Webhook, error) {
	var w Webhook
	var kinds string
	err := s.Scan(&w.ID, &w.UserID, &w.URL, &w.Format, &kinds, &w.Secret, &w.Lang, &w.CreatedAt, &w.LastAt, &w.LastStatus)
	if kinds != "" {
		w.Kinds = strings.Split(kinds, ",")
	}
	return w, err
}

// Webhooks lists one user's webhooks, or everyone's when userID is 0.
func (d *DB) Webhooks(userID int64) ([]Webhook, error) {
	rows, err := d.Query(`SELECT `+webhookCols+` FROM webhooks WHERE ?1 = 0 OR user_id = ?1 ORDER BY id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Webhook
	for rows.Next() {
		w, err := scanWebhook(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

func (d *DB) Webhook(userID, id int64) (Webhook, error) {
	w, err := scanWebhook(d.QueryRow(`SELECT `+webhookCols+` FROM webhooks WHERE user_id = ? AND id = ?`, userID, id))
	return w, notFound(err)
}

func (d *DB) AddWebhook(w Webhook, now int64) (int64, error) {
	var id int64
	err := d.QueryRow(`INSERT INTO webhooks (user_id, url, format, kinds, secret, lang, created_at) VALUES (?, ?, ?, ?, ?, ?, ?) RETURNING id`,
		w.UserID, w.URL, w.Format, strings.Join(w.Kinds, ","), w.Secret, w.Lang, now).Scan(&id)
	return id, err
}

func (d *DB) CountWebhooks(userID int64) (n int, err error) {
	err = d.QueryRow(`SELECT count(*) FROM webhooks WHERE user_id = ?`, userID).Scan(&n)
	return
}

func (d *DB) EditWebhook(userID, id int64, kinds []string) error {
	return one(d.Exec(`UPDATE webhooks SET kinds = ? WHERE user_id = ? AND id = ?`, strings.Join(kinds, ","), userID, id))
}

func (d *DB) DeleteWebhook(userID, id int64) error {
	return one(d.Exec(`DELETE FROM webhooks WHERE user_id = ? AND id = ?`, userID, id))
}

func (d *DB) WebhookResult(id, at int64, status string) {
	d.Exec(`UPDATE webhooks SET last_at = ?, last_status = ? WHERE id = ?`, at, status, id)
}
