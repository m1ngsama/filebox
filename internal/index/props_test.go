package index

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

func (e *env) propPaths(t *testing.T) []string {
	t.Helper()
	rows, err := e.x.db.Query(`SELECT vol || ':' || path FROM dav_props ORDER BY 1`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var s string
		rows.Scan(&s)
		out = append(out, s)
	}
	return out
}

func TestPropsFollowFiles(t *testing.T) {
	e := setup(t)
	v, _ := e.vols.Get("v")
	now := time.Now()
	for _, p := range []string{"a/b/f", "a/bc/f", "g"} {
		e.write(t, p, "x", now)
	}
	set := func(rel string) {
		if err := e.x.PatchProps(v, rel, []Prop{{NS: "urn:x", Name: "k", XML: []byte("1")}}); err != nil {
			t.Fatal(err)
		}
	}
	for _, p := range []string{".", "a/b", "a/b/f", "a/bc", "a/bc/f", "g"} {
		set(p)
	}
	check := func(want ...string) {
		t.Helper()
		if got := e.propPaths(t); !slices.Equal(got, want) {
			t.Fatalf("props %v, want %v", got, want)
		}
	}
	os.Rename(filepath.Join(e.dir, "a/b"), filepath.Join(e.dir, "c"))
	e.x.Rename(v, "a/b", "c")
	check("v:.", "v:a/bc", "v:a/bc/f", "v:c", "v:c/f", "v:g")
	e.x.CopyProps(v, "c", v, "d", true)
	e.x.CopyProps(v, "g", v, "h", false)
	check("v:.", "v:a/bc", "v:a/bc/f", "v:c", "v:c/f", "v:d", "v:d/f", "v:g", "v:h")
	os.RemoveAll(filepath.Join(e.dir, "c"))
	e.x.Touch(v, "c")
	e.x.Touch(v, "g")
	check("v:.", "v:a/bc", "v:a/bc/f", "v:d", "v:d/f", "v:g", "v:h")
	os.MkdirAll(filepath.Join(e.dir, "d"), 0o755)
	e.scan(t)
	check("v:.", "v:a/bc", "v:a/bc/f", "v:d", "v:g")
	if err := e.x.PatchProps(v, "g", []Prop{{NS: "urn:x", Name: "k"}, {NS: "urn:x", Name: "missing"}}); err != nil {
		t.Fatal(err)
	}
	check("v:.", "v:a/bc", "v:a/bc/f", "v:d")
}
