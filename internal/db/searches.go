package db

// Search is a search someone saved to run again from the sidebar; Params is its query string.
type Search struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	Params string `json:"params"`
}

const maxSearches = 30

func (d *DB) Searches(userID int64) ([]Search, error) {
	rows, err := d.Query(`SELECT id, name, params FROM searches WHERE user_id = ? ORDER BY id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Search{}
	for rows.Next() {
		var s Search
		if err := rows.Scan(&s.ID, &s.Name, &s.Params); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// AddSearch saves a search; ErrConflict when the person already keeps as many as allowed.
func (d *DB) AddSearch(userID int64, name, params string, now int64) (Search, error) {
	var n int
	if err := d.QueryRow(`SELECT count(*) FROM searches WHERE user_id = ?`, userID).Scan(&n); err != nil {
		return Search{}, err
	}
	if n >= maxSearches {
		return Search{}, ErrConflict
	}
	s := Search{Name: name, Params: params}
	err := d.QueryRow(`INSERT INTO searches (user_id, name, params, created) VALUES (?, ?, ?, ?) RETURNING id`, userID, name, params, now).Scan(&s.ID)
	return s, err
}

func (d *DB) DeleteSearch(userID, id int64) error {
	return one(d.Exec(`DELETE FROM searches WHERE user_id = ? AND id = ?`, userID, id))
}
