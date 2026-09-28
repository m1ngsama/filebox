package index

import (
	"log/slog"

	"github.com/m1ngsama/filebox/internal/vol"
)

type Prop struct {
	NS, Name string
	XML      []byte
}

func (x *Index) Props(v *vol.Volume, rel string) ([]Prop, error) {
	rows, err := x.db.Query(`SELECT ns, name, xml FROM dav_props WHERE vol = ? AND path = ?`, v.Name, rel)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Prop
	for rows.Next() {
		var p Prop
		if err := rows.Scan(&p.NS, &p.Name, &p.XML); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (x *Index) PatchProps(v *vol.Volume, rel string, ops []Prop) error {
	tx, err := x.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, p := range ops {
		if p.XML == nil {
			_, err = tx.Exec(`DELETE FROM dav_props WHERE vol = ? AND path = ? AND ns = ? AND name = ?`, v.Name, rel, p.NS, p.Name)
		} else {
			_, err = tx.Exec(`INSERT OR REPLACE INTO dav_props (vol, path, ns, name, xml) VALUES (?, ?, ?, ?, ?)`, v.Name, rel, p.NS, p.Name, p.XML)
		}
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (x *Index) CopyProps(src *vol.Volume, srel string, dst *vol.Volume, drel string, tree bool) {
	if x == nil {
		return
	}
	where, args := `vol = ? AND path = ?`, []any{src.Name, srel}
	if tree {
		where, args = subtree, under(src.Name, srel)
	}
	_, err := x.db.Exec(`INSERT OR REPLACE INTO dav_props (vol, path, ns, name, xml)
		SELECT ?, ? || substr(path, length(?) + 1), ns, name, xml FROM dav_props WHERE `+where,
		append([]any{dst.Name, drel, srel}, args...)...)
	if err != nil {
		slog.Warn("copy props", "vol", src.Name, "from", srel, "to", drel, "err", err)
	}
}
