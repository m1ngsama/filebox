package index

import (
	"errors"
	"log/slog"

	"github.com/m1ngsama/filebox/internal/vol"
)

const (
	MaxPropSize = 64 << 10
	MaxProps    = 256
)

var (
	ErrPropTooLarge = errors.New("property value too large")
	ErrTooManyProps = errors.New("too many properties")
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
	for _, p := range ops {
		if len(p.XML) > MaxPropSize {
			return ErrPropTooLarge
		}
	}
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
	var n int
	if err := tx.QueryRow(`SELECT count(*) FROM dav_props WHERE vol = ? AND path = ?`, v.Name, rel).Scan(&n); err != nil {
		return err
	}
	if n > MaxProps {
		return ErrTooManyProps
	}
	return tx.Commit()
}

func (x *Index) CopyProps(src *vol.Volume, srel string, dst *vol.Volume, drel string, tree bool) {
	if x == nil {
		return
	}
	if err := x.copyProps(src, srel, dst, drel, tree); err != nil {
		slog.Warn("copy props", "from", src.Name+":"+srel, "to", dst.Name+":"+drel, "err", err)
	}
}

func (x *Index) copyProps(src *vol.Volume, srel string, dst *vol.Volume, drel string, tree bool) error {
	one := `vol = ? AND path = ?`
	from, to := []any{src.Name, srel}, []any{dst.Name, drel}
	fromWhere, toWhere := one, one
	if tree {
		fromWhere, toWhere, from, to = subtree, subtree, under(src.Name, srel), under(dst.Name, drel)
	}
	tx, err := x.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`DELETE FROM dav_props WHERE `+toWhere, to...); err != nil {
		return err
	}
	expr, args := moved(srel, drel)
	if _, err := tx.Exec(`INSERT OR REPLACE INTO dav_props (vol, path, ns, name, xml)
		SELECT ?, `+expr+`, ns, name, xml FROM dav_props WHERE `+fromWhere,
		append(append([]any{dst.Name}, args...), from...)...); err != nil {
		return err
	}
	return tx.Commit()
}
