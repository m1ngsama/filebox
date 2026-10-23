package db

import (
	"errors"
	"strings"
)

const (
	RoleAdmin  = "admin"
	RoleMember = "member"

	LevelRead  = "r"
	LevelWrite = "rw"
)

var ErrLastAdmin = errors.New("last admin")

// Account is a user with what they may reach.
type Account struct {
	ID        int64
	Name      string
	Role      string
	CreatedAt int64
	Access    map[string]string
}

func ValidLevel(l string) bool { return l == LevelRead || l == LevelWrite }

// Access returns the role of a user and the level they hold on each volume, which an admin holds on all.
func (d *DB) Access(userID int64) (role string, grants map[string]string, err error) {
	if err = d.QueryRow(`SELECT role FROM users WHERE id = ?`, userID).Scan(&role); err != nil {
		return "", nil, notFound(err)
	}
	grants = map[string]string{}
	rows, err := d.Query(`SELECT vol, level FROM volume_access WHERE user_id = ?`, userID)
	if err != nil {
		return "", nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var v, l string
		if err := rows.Scan(&v, &l); err != nil {
			return "", nil, err
		}
		grants[v] = l
	}
	return role, grants, rows.Err()
}

func (d *DB) Accounts() ([]Account, error) {
	rows, err := d.Query(`SELECT id, name, role, created_at FROM users ORDER BY role = 'admin' DESC, name COLLATE NOCASE`)
	if err != nil {
		return nil, err
	}
	var out []Account
	for rows.Next() {
		a := Account{Access: map[string]string{}}
		if err := rows.Scan(&a.ID, &a.Name, &a.Role, &a.CreatedAt); err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, a)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	byID := map[int64]*Account{}
	for i := range out {
		byID[out[i].ID] = &out[i]
	}
	grants, err := d.Query(`SELECT user_id, vol, level FROM volume_access`)
	if err != nil {
		return nil, err
	}
	defer grants.Close()
	for grants.Next() {
		var id int64
		var v, l string
		if err := grants.Scan(&id, &v, &l); err != nil {
			return nil, err
		}
		if a := byID[id]; a != nil {
			a.Access[v] = l
		}
	}
	return out, grants.Err()
}

// SetAccess grants a level on a volume, or takes the grant away when level is empty.
func (d *DB) SetAccess(userID int64, vol, level string) error {
	if level == "" {
		_, err := d.Exec(`DELETE FROM volume_access WHERE user_id = ? AND vol = ?`, userID, vol)
		return err
	}
	if !ValidLevel(level) {
		return ErrConflict
	}
	return one(d.Exec(`INSERT INTO volume_access (user_id, vol, level) SELECT id, ?2, ?3 FROM users WHERE id = ?1
		ON CONFLICT (user_id, vol) DO UPDATE SET level = excluded.level`, userID, vol, level))
}

func (d *DB) SetRole(userID int64, role string) error {
	tx, err := d.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if role != RoleAdmin {
		var others int
		if err := tx.QueryRow(`SELECT count(*) FROM users WHERE role = 'admin' AND id != ?`, userID).Scan(&others); err != nil {
			return err
		}
		if others == 0 {
			var cur string
			if err := tx.QueryRow(`SELECT role FROM users WHERE id = ?`, userID).Scan(&cur); err != nil {
				return notFound(err)
			}
			if cur == RoleAdmin {
				return ErrLastAdmin
			}
		}
	}
	if err := one(tx.Exec(`UPDATE users SET role = ? WHERE id = ?`, role, userID)); err != nil {
		return err
	}
	return tx.Commit()
}

// DeleteUser removes a user along with their sessions, app passwords, passkeys, shares and favorites.
func (d *DB) DeleteUser(id int64) error {
	tx, err := d.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var role string
	if err := tx.QueryRow(`SELECT role FROM users WHERE id = ?`, id).Scan(&role); err != nil {
		return notFound(err)
	}
	if role == RoleAdmin {
		var others int
		if err := tx.QueryRow(`SELECT count(*) FROM users WHERE role = 'admin' AND id != ?`, id).Scan(&others); err != nil {
			return err
		}
		if others == 0 {
			return ErrLastAdmin
		}
	}
	if _, err := tx.Exec(`DELETE FROM users WHERE id = ?`, id); err != nil {
		return err
	}
	return tx.Commit()
}

// CreateUser adds a member; ErrConflict when the name is taken.
func (d *DB) CreateUser(name, hash, role string, grants map[string]string) (int64, error) {
	tx, err := d.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	var taken int
	if err := tx.QueryRow(`SELECT count(*) FROM users WHERE name = ? COLLATE NOCASE`, name).Scan(&taken); err != nil {
		return 0, err
	}
	if taken > 0 {
		return 0, ErrConflict
	}
	var id int64
	if err := tx.QueryRow(`INSERT INTO users (name, password_hash, role) VALUES (?, ?, ?) RETURNING id`, name, hash, role).Scan(&id); err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return 0, ErrConflict
		}
		return 0, err
	}
	for v, l := range grants {
		if !ValidLevel(l) {
			return 0, ErrConflict
		}
		if _, err := tx.Exec(`INSERT INTO volume_access (user_id, vol, level) VALUES (?, ?, ?)`, id, v, l); err != nil {
			return 0, err
		}
	}
	return id, tx.Commit()
}

func (d *DB) SetPasswordByID(id int64, hash string) error {
	return one(d.Exec(`UPDATE users SET password_hash = ? WHERE id = ?`, hash, id))
}
