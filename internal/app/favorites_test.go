package app

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

type favList struct {
	Entries []struct {
		Vol, Path, Name string
		Dir, Missing    bool
	}
}

func (f *fixture) favs(t *testing.T) []string {
	t.Helper()
	var out []string
	for _, e := range decode[favList](t, f.do("GET", "/api/favorites", nil)).Entries {
		s := e.Vol + ":" + e.Path
		if e.Missing {
			s += " missing"
		}
		out = append(out, s)
	}
	slices.Sort(out)
	return out
}

func (f *fixture) expectFavs(t *testing.T, want ...string) {
	t.Helper()
	if got := f.favs(t); !slices.Equal(got, want) {
		t.Fatalf("favorites = %v, want %v", got, want)
	}
}

func TestFavoritesFollowMovesAndDeletes(t *testing.T) {
	f := newTestApp(t)
	f.write(t, "tree/a.txt", "a")
	f.write(t, "tree/sub/b.txt", "b")
	f.write(t, "c.txt", "c")
	f.App.Index.Scan(f.App.Vols)
	if w := f.do("POST", "/api/favorites", body(`{"vol":"v","paths":["tree","tree/sub/b.txt","c.txt"],"star":true}`)); w.Code != 204 {
		t.Fatalf("star %d %s", w.Code, w.Body)
	}
	f.expectFavs(t, "v:c.txt", "v:tree", "v:tree/sub/b.txt")
	for _, b := range []string{`{"vol":"v","paths":["nope.txt"],"star":true}`, `{"vol":"x","paths":["c.txt"],"star":true}`, `{"vol":"v","paths":[".trash"],"star":true}`, `{"vol":"v","paths":["/"],"star":true}`} {
		if w := f.do("POST", "/api/favorites", body(b)); w.Code/100 != 4 {
			t.Fatalf("%s: %d", b, w.Code)
		}
	}

	f.do("POST", "/api/mv", body(`{"src":{"vol":"v","path":"tree"},"dst":{"vol":"v","path":"t2"}}`))
	f.expectFavs(t, "v:c.txt", "v:t2", "v:t2/sub/b.txt")
	f.do("POST", "/api/mv", body(`{"src":{"vol":"v","path":"c.txt"},"dst":{"vol":"v","path":"t2/c2.txt"}}`))
	f.expectFavs(t, "v:t2", "v:t2/c2.txt", "v:t2/sub/b.txt")
	w := f.do("POST", "/api/mv", body(`{"src":{"vol":"v","path":"t2"},"dst":{"vol":"w","path":"t3"}}`))
	if st := waitJob(t, f, decode[struct{ Job string }](t, w).Job); st != "done" {
		t.Fatalf("cross-volume mv %s", st)
	}
	f.expectFavs(t, "w:t3", "w:t3/c2.txt", "w:t3/sub/b.txt")
	w = f.do("POST", "/api/cp", body(`{"src":{"vol":"w","path":"t3"},"dst":{"vol":"v","path":"copy"}}`))
	waitJob(t, f, decode[struct{ Job string }](t, w).Job)
	f.expectFavs(t, "w:t3", "w:t3/c2.txt", "w:t3/sub/b.txt")

	os.Remove(filepath.Join(f.Dir2, "t3/c2.txt"))
	f.expectFavs(t, "w:t3", "w:t3/c2.txt missing", "w:t3/sub/b.txt")
	f.do("POST", "/api/rm", body(`{"vol":"w","paths":["t3/sub"]}`))
	f.expectFavs(t, "w:t3", "w:t3/c2.txt missing")
	id := decode[struct{ Items []struct{ ID string } }](t, f.do("GET", "/api/trash?vol=w", nil)).Items[0].ID
	f.do("POST", "/api/trash/restore", body(`{"vol":"w","id":"`+id+`"}`))
	f.expectFavs(t, "w:t3", "w:t3/c2.txt missing")
	if w := f.do("POST", "/api/favorites", body(`{"vol":"w","paths":["t3/c2.txt","t3"],"star":false}`)); w.Code != 204 {
		t.Fatalf("unstar %d", w.Code)
	}
	f.expectFavs(t)
	if w := f.do("GET", "/api/favorites", nil, "X-No-Auth", "1", "Authorization", "Bearer "+f.Bearer); w.Code != 401 {
		t.Fatalf("app token %d", w.Code)
	}
}
