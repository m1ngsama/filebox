package app

import (
	"net/url"
	"slices"
	"testing"
)

func (f *fixture) search(t *testing.T, query string) []string {
	t.Helper()
	w := f.do("GET", "/api/search?"+query, nil)
	if w.Code != 200 {
		t.Fatalf("search %s: %d %s", query, w.Code, w.Body)
	}
	var out []string
	for _, e := range decode[recentList](t, w).Entries {
		out = append(out, e.Vol+":"+e.Path)
	}
	return out
}

func TestSearchAPI(t *testing.T) {
	f := newTestApp(t)
	f.write(t, "notes/plan.txt", "a")
	f.write(t, "planets.md", "a")
	f.App.Index.Scan(f.App.Vols)
	f.do("POST", "/api/mkdir", body(`{"vol":"w","path":"plans"}`))
	if got := f.search(t, "q=plan&vol=v"); !slices.Equal(got, []string{"v:planets.md", "v:notes/plan.txt"}) && !slices.Equal(got, []string{"v:notes/plan.txt", "v:planets.md"}) {
		t.Fatalf("vol v %v", got)
	}
	if got := f.search(t, "q=plan"); len(got) != 3 {
		t.Fatalf("all vols %v", got)
	}
	if got := f.search(t, "q=plan&vol=v&under=notes"); !slices.Equal(got, []string{"v:notes/plan.txt"}) {
		t.Fatalf("under %v", got)
	}
	if got := f.search(t, "q="+url.QueryEscape(`" OR *`)); len(got) != 0 {
		t.Fatalf("injection %v", got)
	}
	f.do("POST", "/api/mv", body(`{"src":{"vol":"v","path":"notes"},"dst":{"vol":"v","path":"archive"}}`))
	if got := f.search(t, "q=plan.txt"); !slices.Equal(got, []string{"v:archive/plan.txt"}) {
		t.Fatalf("after mv %v", got)
	}
	f.do("POST", "/api/rm", body(`{"vol":"v","paths":["archive"]}`))
	if got := f.search(t, "q=plan.txt"); len(got) != 0 {
		t.Fatalf("after rm %v", got)
	}
	for q, code := range map[string]int{"q=p": 400, "q=+p+": 400, "q=plan&vol=nope": 404, "q=plan&vol=v&under=.trash": 400, "q=plan&vol=v&under=../x": 200} {
		if w := f.do("GET", "/api/search?"+q, nil); w.Code != code {
			t.Errorf("%s: %d, want %d", q, w.Code, code)
		}
	}
	if w := f.do("GET", "/api/search?q=plan", nil, "X-No-Auth", "1", "Authorization", "Bearer "+f.Bearer); w.Code != 401 {
		t.Fatalf("app token search %d", w.Code)
	}
	if w := f.do("GET", "/api/search?q=plan", nil, "X-No-Auth", "1"); w.Code != 401 {
		t.Fatalf("anonymous search %d", w.Code)
	}
}
