package db

import (
	"database/sql"
	"errors"
	"fmt"

	_ "modernc.org/sqlite"
)

var ErrNotFound = errors.New("not found")

var migrations = []string{
	`CREATE TABLE users (
		id INTEGER PRIMARY KEY,
		name TEXT NOT NULL UNIQUE,
		password_hash TEXT NOT NULL,
		created_at INTEGER NOT NULL DEFAULT (unixepoch())
	);
	CREATE TABLE tokens (
		id INTEGER PRIMARY KEY,
		user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		kind TEXT NOT NULL,
		hash TEXT NOT NULL UNIQUE,
		label TEXT NOT NULL DEFAULT '',
		scope TEXT NOT NULL DEFAULT '',
		created_at INTEGER NOT NULL,
		last_used_at INTEGER NOT NULL DEFAULT 0,
		expires_at INTEGER NOT NULL DEFAULT 0
	);
	CREATE TABLE shares (
		id INTEGER PRIMARY KEY,
		token TEXT NOT NULL UNIQUE,
		user_id INTEGER NOT NULL REFERENCES users(id) ON DELETE CASCADE,
		vol TEXT NOT NULL,
		path TEXT NOT NULL,
		mode TEXT NOT NULL,
		password_hash TEXT NOT NULL DEFAULT '',
		expires_at INTEGER NOT NULL DEFAULT 0,
		created_at INTEGER NOT NULL,
		hits INTEGER NOT NULL DEFAULT 0
	);`,
}

type DB struct{ *sql.DB }

type User struct {
	ID                 int64
	Name, PasswordHash string
}

type Token struct {
	ID, UserID                       int64
	Kind, Hash, Label, Scope         string
	CreatedAt, LastUsedAt, ExpiresAt int64
}

type Share struct {
	ID                            int64
	Token                         string
	UserID                        int64
	Vol, Path, Mode, PasswordHash string
	ExpiresAt, CreatedAt, Hits    int64
}

func Open(path string) (*DB, error) {
	dsn := "file:" + path + "?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)&_pragma=synchronous(NORMAL)"
	s, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	d := &DB{s}
	if err := d.migrate(); err != nil {
		s.Close()
		return nil, err
	}
	return d, nil
}

func (d *DB) migrate() error {
	var v int
	if err := d.QueryRow(`PRAGMA user_version`).Scan(&v); err != nil {
		return err
	}
	for ; v < len(migrations); v++ {
		tx, err := d.Begin()
		if err != nil {
			return err
		}
		if _, err := tx.Exec(migrations[v]); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %d: %w", v+1, err)
		}
		if _, err := tx.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, v+1)); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

func notFound(err error) error {
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	return err
}

func (d *DB) SetPassword(name, hash string) (int64, error) {
	var id int64
	err := d.QueryRow(`INSERT INTO users (name, password_hash) VALUES (?, ?)
		ON CONFLICT(name) DO UPDATE SET password_hash = excluded.password_hash
		RETURNING id`, name, hash).Scan(&id)
	return id, err
}

func (d *DB) UserByName(name string) (User, error) {
	var u User
	err := d.QueryRow(`SELECT id, name, password_hash FROM users WHERE name = ?`, name).
		Scan(&u.ID, &u.Name, &u.PasswordHash)
	return u, notFound(err)
}

func (d *DB) UserByID(id int64) (User, error) {
	var u User
	err := d.QueryRow(`SELECT id, name, password_hash FROM users WHERE id = ?`, id).
		Scan(&u.ID, &u.Name, &u.PasswordHash)
	return u, notFound(err)
}

const tokenCols = `id, user_id, kind, hash, label, scope, created_at, last_used_at, expires_at`

func scanToken(r interface{ Scan(...any) error }) (Token, error) {
	var t Token
	err := r.Scan(&t.ID, &t.UserID, &t.Kind, &t.Hash, &t.Label, &t.Scope, &t.CreatedAt, &t.LastUsedAt, &t.ExpiresAt)
	return t, notFound(err)
}

func (d *DB) InsertToken(t *Token) error {
	return d.QueryRow(`INSERT INTO tokens (user_id, kind, hash, label, scope, created_at, last_used_at, expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?) RETURNING id`,
		t.UserID, t.Kind, t.Hash, t.Label, t.Scope, t.CreatedAt, t.LastUsedAt, t.ExpiresAt).Scan(&t.ID)
}

func (d *DB) TokenByHash(hash string, now int64) (Token, error) {
	return scanToken(d.QueryRow(`SELECT `+tokenCols+` FROM tokens
		WHERE hash = ? AND (expires_at = 0 OR expires_at > ?)`, hash, now))
}

func (d *DB) TouchToken(id, now, expiresAt int64) error {
	_, err := d.Exec(`UPDATE tokens SET last_used_at = ?, expires_at = ? WHERE id = ?`, now, expiresAt, id)
	return err
}

func (d *DB) DeleteToken(userID, id int64) error {
	_, err := d.Exec(`DELETE FROM tokens WHERE user_id = ? AND id = ?`, userID, id)
	return err
}

func (d *DB) DeleteTokenByHash(hash string) error {
	_, err := d.Exec(`DELETE FROM tokens WHERE hash = ?`, hash)
	return err
}

func (d *DB) ListTokens(userID int64, kind string) ([]Token, error) {
	rows, err := d.Query(`SELECT `+tokenCols+` FROM tokens WHERE user_id = ? AND kind = ? ORDER BY id`, userID, kind)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Token
	for rows.Next() {
		t, err := scanToken(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

const shareCols = `id, token, user_id, vol, path, mode, password_hash, expires_at, created_at, hits`

func scanShare(r interface{ Scan(...any) error }) (Share, error) {
	var s Share
	err := r.Scan(&s.ID, &s.Token, &s.UserID, &s.Vol, &s.Path, &s.Mode, &s.PasswordHash, &s.ExpiresAt, &s.CreatedAt, &s.Hits)
	return s, notFound(err)
}

func (d *DB) InsertShare(s *Share) error {
	return d.QueryRow(`INSERT INTO shares (token, user_id, vol, path, mode, password_hash, expires_at, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?) RETURNING id`,
		s.Token, s.UserID, s.Vol, s.Path, s.Mode, s.PasswordHash, s.ExpiresAt, s.CreatedAt).Scan(&s.ID)
}

func (d *DB) ShareByToken(token string, now int64) (Share, error) {
	return scanShare(d.QueryRow(`SELECT `+shareCols+` FROM shares
		WHERE token = ? AND (expires_at = 0 OR expires_at > ?)`, token, now))
}

func (d *DB) ListShares(userID int64) ([]Share, error) {
	rows, err := d.Query(`SELECT `+shareCols+` FROM shares WHERE user_id = ? ORDER BY id DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Share
	for rows.Next() {
		s, err := scanShare(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

func (d *DB) DeleteShare(userID, id int64) error {
	_, err := d.Exec(`DELETE FROM shares WHERE user_id = ? AND id = ?`, userID, id)
	return err
}

func (d *DB) HitShare(id int64) error {
	_, err := d.Exec(`UPDATE shares SET hits = hits + 1 WHERE id = ?`, id)
	return err
}

func (d *DB) Check() error {
	var res string
	if err := d.QueryRow(`PRAGMA quick_check`).Scan(&res); err != nil {
		return err
	}
	if res != "ok" {
		return fmt.Errorf("database integrity check: %s", res)
	}
	return nil
}
