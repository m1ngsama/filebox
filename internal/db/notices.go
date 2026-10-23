package db

// Notice is something that happened that one person should hear about inside filebox.
// Count is how many times it happened, except for disk_low, where it is the bytes left.
type Notice struct {
	ID    int64  `json:"id"`
	At    int64  `json:"at"`
	Kind  string `json:"kind"`
	Name  string `json:"name"`
	Vol   string `json:"vol"`
	Path  string `json:"path"`
	Share string `json:"share"`
	Count int64  `json:"count"`
	Read  bool   `json:"read"`
	Actor string `json:"actor"`
}

const keepNotices = 200

// AddNotice stores a notice and keeps only the newest few hundred per person.
func (d *DB) AddNotice(userID int64, n Notice) (Notice, error) {
	if n.Count == 0 {
		n.Count = 1
	}
	err := d.QueryRow(`INSERT INTO notices (user_id, at, kind, name, vol, path, share, count, actor) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?) RETURNING id`,
		userID, n.At, n.Kind, n.Name, n.Vol, n.Path, n.Share, n.Count, n.Actor).Scan(&n.ID)
	if err != nil {
		return n, err
	}
	_, err = d.Exec(`DELETE FROM notices WHERE user_id = ?1 AND id <= (SELECT id FROM notices WHERE user_id = ?1 ORDER BY id DESC LIMIT 1 OFFSET ?2)`, userID, keepNotices)
	return n, err
}

// Notices lists a person's newest notices, older than before when before is set.
func (d *DB) Notices(userID, before int64, limit int) ([]Notice, error) {
	rows, err := d.Query(`SELECT id, at, kind, name, vol, path, share, count, read, actor FROM notices
		WHERE user_id = ? AND (? = 0 OR id < ?) ORDER BY id DESC LIMIT ?`, userID, before, before, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Notice{}
	for rows.Next() {
		var n Notice
		if err := rows.Scan(&n.ID, &n.At, &n.Kind, &n.Name, &n.Vol, &n.Path, &n.Share, &n.Count, &n.Read, &n.Actor); err != nil {
			return nil, err
		}
		out = append(out, n)
	}
	return out, rows.Err()
}

func (d *DB) Unread(userID int64) (n int, err error) {
	err = d.QueryRow(`SELECT count(*) FROM notices WHERE user_id = ? AND read = 0`, userID).Scan(&n)
	return
}

// MarkRead marks the listed notices read, or all of them when ids is empty.
func (d *DB) MarkRead(userID int64, ids []int64) error {
	if len(ids) == 0 {
		_, err := d.Exec(`UPDATE notices SET read = 1 WHERE user_id = ? AND read = 0`, userID)
		return err
	}
	for _, id := range ids {
		if _, err := d.Exec(`UPDATE notices SET read = 1 WHERE user_id = ? AND id = ?`, userID, id); err != nil {
			return err
		}
	}
	return nil
}

// Admins lists the ids of everyone who runs the server.
func (d *DB) Admins() ([]int64, error) {
	rows, err := d.Query(`SELECT id FROM users WHERE role = 'admin' ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}
