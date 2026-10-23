package db

import (
	"encoding/json"
	"errors"
)

const (
	InviteJoin  = "join"
	InviteReset = "reset"
)

// Invite is a one-time link: it lets someone join with a role and some access, or set a new password for an existing account.
type Invite struct {
	ID, UserID, CreatedBy, CreatedAt, ExpiresAt int64
	Kind, Role, Hash                            string
	Grants                                      map[string]string
	UserName                                    string
}

var ErrGone = errors.New("invite gone")

func (d *DB) AddInvite(in Invite) (int64, error) {
	grants, _ := json.Marshal(in.Grants)
	var uid any
	if in.UserID != 0 {
		uid = in.UserID
	}
	var id int64
	err := d.QueryRow(`INSERT INTO invites (hash, kind, user_id, role, grants, created_by, created_at, expires_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?) RETURNING id`,
		in.Hash, in.Kind, uid, in.Role, string(grants), in.CreatedBy, in.CreatedAt, in.ExpiresAt).Scan(&id)
	return id, err
}

type rowScanner interface{ Scan(...any) error }

const inviteCols = `i.id, i.hash, i.kind, coalesce(i.user_id, 0), coalesce(u.name, ''), i.role, i.grants, i.created_by, i.created_at, i.expires_at`

func scanInvite(r rowScanner) (Invite, error) {
	var in Invite
	var grants string
	err := r.Scan(&in.ID, &in.Hash, &in.Kind, &in.UserID, &in.UserName, &in.Role, &grants, &in.CreatedBy, &in.CreatedAt, &in.ExpiresAt)
	in.Grants = map[string]string{}
	json.Unmarshal([]byte(grants), &in.Grants)
	return in, err
}

// Invites lists the links still good at now, newest first.
func (d *DB) Invites(now int64) ([]Invite, error) {
	rows, err := d.Query(`SELECT `+inviteCols+` FROM invites i LEFT JOIN users u ON u.id = i.user_id WHERE i.expires_at > ? ORDER BY i.id DESC`, now)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Invite
	for rows.Next() {
		in, err := scanInvite(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, in)
	}
	return out, rows.Err()
}

func (d *DB) InviteByHash(hash string, now int64) (Invite, error) {
	in, err := scanInvite(d.QueryRow(`SELECT `+inviteCols+` FROM invites i LEFT JOIN users u ON u.id = i.user_id WHERE i.hash = ? AND i.expires_at > ?`, hash, now))
	return in, notFound(err)
}

func (d *DB) DeleteInvite(id int64) error {
	return one(d.Exec(`DELETE FROM invites WHERE id = ?`, id))
}

func (d *DB) PurgeInvites(now int64) (int64, error) {
	return affected(d.Exec(`DELETE FROM invites WHERE expires_at <= ?`, now))
}

// UseInvite spends a link in one step: it creates the member it promised, or sets the new password, and the link is gone either way.
// Grants naming a volume no longer served are the caller's to filter before. ErrConflict means the name is taken, and the link stays good.
func (d *DB) UseInvite(hash string, now int64, name, passwordHash string) (userID int64, in Invite, err error) {
	tx, err := d.Begin()
	if err != nil {
		return 0, in, err
	}
	defer tx.Rollback()
	in, err = scanInvite(tx.QueryRow(`SELECT `+inviteCols+` FROM invites i LEFT JOIN users u ON u.id = i.user_id WHERE i.hash = ? AND i.expires_at > ?`, hash, now))
	if err != nil {
		return 0, in, ErrGone
	}
	switch in.Kind {
	case InviteJoin:
		var taken int
		if err := tx.QueryRow(`SELECT count(*) FROM users WHERE name = ? COLLATE NOCASE`, name).Scan(&taken); err != nil {
			return 0, in, err
		}
		if taken > 0 {
			return 0, in, ErrConflict
		}
		if err := tx.QueryRow(`INSERT INTO users (name, password_hash, role) VALUES (?, ?, ?) RETURNING id`, name, passwordHash, in.Role).Scan(&userID); err != nil {
			return 0, in, err
		}
		for v, l := range in.Grants {
			if !ValidLevel(l) {
				continue
			}
			if _, err := tx.Exec(`INSERT INTO volume_access (user_id, vol, level) VALUES (?, ?, ?)`, userID, v, l); err != nil {
				return 0, in, err
			}
		}
	case InviteReset:
		userID = in.UserID
		if err := one(tx.Exec(`UPDATE users SET password_hash = ? WHERE id = ?`, passwordHash, userID)); err != nil {
			return 0, in, ErrGone
		}
		if _, err := tx.Exec(`DELETE FROM tokens WHERE user_id = ? AND kind = 'session'`, userID); err != nil {
			return 0, in, err
		}
	default:
		return 0, in, ErrGone
	}
	if _, err := tx.Exec(`DELETE FROM invites WHERE id = ?`, in.ID); err != nil {
		return 0, in, err
	}
	return userID, in, tx.Commit()
}
