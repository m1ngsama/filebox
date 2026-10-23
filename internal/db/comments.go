package db

// Comment is a note one person left on a file or folder for the others who can reach it.
type Comment struct {
	ID     int64  `json:"id"`
	At     int64  `json:"at"`
	UserID int64  `json:"-"`
	Author string `json:"author"`
	Body   string `json:"body"`
}

func (d *DB) AddComment(vol, path string, userID int64, body string, at int64) (Comment, error) {
	c := Comment{At: at, UserID: userID, Body: body}
	if err := d.QueryRow(`INSERT INTO comments (vol, path, user_id, body, at) VALUES (?, ?, ?, ?, ?) RETURNING id`, vol, path, userID, body, at).Scan(&c.ID); err != nil {
		return c, err
	}
	err := d.QueryRow(`SELECT name FROM users WHERE id = ?`, userID).Scan(&c.Author)
	return c, err
}

// Comments lists what was said about one item, oldest first; a removed member's comments stay without a name.
func (d *DB) Comments(vol, path string) ([]Comment, error) {
	rows, err := d.Query(`SELECT c.id, c.at, coalesce(c.user_id, 0), coalesce(u.name, ''), c.body FROM comments c
		LEFT JOIN users u ON u.id = c.user_id WHERE c.vol = ? AND c.path = ? ORDER BY c.id`, vol, path)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Comment{}
	for rows.Next() {
		var c Comment
		if err := rows.Scan(&c.ID, &c.At, &c.UserID, &c.Author, &c.Body); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// Comment finds one comment and the item it is on.
func (d *DB) Comment(id int64) (c Comment, vol, path string, err error) {
	err = d.QueryRow(`SELECT id, at, coalesce(user_id, 0), body, vol, path FROM comments WHERE id = ?`, id).Scan(&c.ID, &c.At, &c.UserID, &c.Body, &vol, &path)
	return c, vol, path, notFound(err)
}

func (d *DB) DeleteComment(id int64) error {
	return one(d.Exec(`DELETE FROM comments WHERE id = ?`, id))
}

// Commenters lists everyone who has said something about an item.
func (d *DB) Commenters(vol, path string) ([]int64, error) {
	rows, err := d.Query(`SELECT DISTINCT user_id FROM comments WHERE vol = ? AND path = ? AND user_id IS NOT NULL`, vol, path)
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
