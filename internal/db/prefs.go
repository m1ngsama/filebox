package db

// Prefs is how a person likes filebox to look, kept with their account so it follows them to every device.
type Prefs struct {
	Accent  string `json:"accent,omitempty"`
	Density string `json:"density,omitempty"`
}

var (
	Accents   = map[string]bool{"": true, "blue": true, "teal": true, "green": true, "orange": true, "pink": true, "purple": true, "graphite": true}
	Densities = map[string]bool{"": true, "comfortable": true, "compact": true}
)

func (p Prefs) Valid() bool { return Accents[p.Accent] && Densities[p.Density] }

func (d *DB) Prefs(userID int64) (p Prefs, err error) {
	var raw string
	if err = d.QueryRow(`SELECT prefs FROM users WHERE id = ?`, userID).Scan(&raw); err != nil {
		return p, notFound(err)
	}
	err = jsonUnmarshal(raw, &p)
	return p, err
}

func (d *DB) SetPrefs(userID int64, p Prefs) error {
	raw, err := jsonMarshal(p)
	if err != nil {
		return err
	}
	return one(d.Exec(`UPDATE users SET prefs = ? WHERE id = ?`, raw, userID))
}
