package app

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

type tab struct {
	events chan map[string]any
	cancel context.CancelFunc
}

// listen opens the event tab as a browser tab would, with the given session cookie.
func listen(t *testing.T, srv *httptest.Server, cookie string) *tab {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	req, _ := http.NewRequestWithContext(ctx, "GET", srv.URL+"/api/events", nil)
	req.Header.Set("Cookie", cookie)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		cancel()
		t.Fatal(err)
	}
	t.Cleanup(cancel)
	if res.StatusCode != 200 || !strings.HasPrefix(res.Header.Get("Content-Type"), "text/event-stream") {
		t.Fatalf("stream %d %s", res.StatusCode, res.Header.Get("Content-Type"))
	}
	s := &tab{events: make(chan map[string]any, 64), cancel: cancel}
	go func() {
		defer res.Body.Close()
		sc := bufio.NewScanner(res.Body)
		for sc.Scan() {
			if d, ok := strings.CutPrefix(sc.Text(), "data: "); ok {
				var m map[string]any
				json.Unmarshal([]byte(d), &m)
				s.events <- m
			}
		}
	}()
	return s
}

// next waits for an event that matches, skipping others.
func (s *tab) next(t *testing.T, match func(map[string]any) bool, wait time.Duration) (map[string]any, bool) {
	t.Helper()
	deadline := time.After(wait)
	for {
		select {
		case e := <-s.events:
			if match(e) {
				return e, true
			}
		case <-deadline:
			return nil, false
		}
	}
}

// drain throws away whatever arrived so far, so a later check sees only what comes next.
func (s *tab) drain(quiet time.Duration) {
	for {
		select {
		case <-s.events:
		case <-time.After(quiet):
			return
		}
	}
}

func isFS(vol, dir string) func(map[string]any) bool {
	return func(e map[string]any) bool {
		return e["type"] == "fs" && e["vol"] == vol && (e["dir"] == dir || (dir == "" && e["dir"] == nil))
	}
}

func TestLiveStreamTellsReadersWhatChanged(t *testing.T) {
	f := newTestApp(t)
	f.write(t, "docs/a.txt", "a")
	srv := httptest.NewServer(f.H)
	// open streams end first: cleanups run in reverse, so this one runs last
	t.Cleanup(srv.Close)
	admin := listen(t, srv, f.Cookie.String())
	bob := f.member(t, "bob", map[string]string{"v": "r"})
	carol := f.member(t, "carol", map[string]string{"w": "rw"})
	bs, cs := listen(t, srv, bob.hdr[3]), listen(t, srv, carol.hdr[3])
	if e, ok := admin.next(t, func(e map[string]any) bool { return e["type"] == "unread" }, time.Second); !ok || e["unread"] != float64(0) {
		t.Fatalf("a tab starts with the unread count: %v", e)
	}

	if w := f.do("POST", "/api/mkdir", body(`{"vol":"v","path":"docs/new"}`)); w.Code != 201 {
		t.Fatalf("mkdir %d", w.Code)
	}
	if _, ok := admin.next(t, isFS("v", "docs"), 2*time.Second); !ok {
		t.Fatal("the admin did not hear that docs changed")
	}
	if _, ok := bs.next(t, isFS("v", "docs"), 2*time.Second); !ok {
		t.Fatal("a reader of v did not hear that docs changed")
	}
	if e, ok := cs.next(t, func(e map[string]any) bool { return e["type"] == "fs" }, 500*time.Millisecond); ok {
		t.Fatalf("carol cannot read v but heard %v", e)
	}

	// a burst of changes to one folder settles into one event
	for _, n := range []string{"x1", "x2", "x3", "x4"} {
		f.do("POST", "/api/touch", body(`{"vol":"v","path":"docs/`+n+`"}`))
	}
	n := 0
	for {
		if _, ok := admin.next(t, isFS("v", "docs"), 400*time.Millisecond); !ok {
			break
		}
		n++
	}
	if n < 1 || n > 2 {
		t.Fatalf("four quick changes made %d events", n)
	}

	// losing access stops the events at once
	bs.drain(400 * time.Millisecond)
	f.do("PATCH", "/api/team/users/"+itoa(bob.id), body(`{"access":{"v":""}}`))
	f.do("POST", "/api/mkdir", body(`{"vol":"v","path":"after"}`))
	if e, ok := bs.next(t, func(e map[string]any) bool { return e["type"] == "fs" }, 600*time.Millisecond); ok {
		t.Fatalf("bob lost v but still heard %v", e)
	}

	if w := f.do("GET", "/api/events", nil, "X-No-Auth", "1"); w.Code != 401 {
		t.Fatalf("anonymous tab %d", w.Code)
	}
}

func TestNoticesForShareActivityAndNewMembers(t *testing.T) {
	f := newTestApp(t)
	f.write(t, "pub/a.txt", "hello")
	f.write(t, "inbox/.keep", "")
	srv := httptest.NewServer(f.H)
	// open streams end first: cleanups run in reverse, so this one runs last
	t.Cleanup(srv.Close)
	s := listen(t, srv, f.Cookie.String())

	tok := mkShare(t, f, `{"vol":"v","path":"pub","mode":"read"}`)
	for i := 0; i < 3; i++ {
		anon(f, "GET", "/s/"+tok+"/raw/a.txt?dl", "")
	}
	e, ok := s.next(t, func(e map[string]any) bool { return e["type"] == "notice" }, 2*time.Second)
	if !ok {
		t.Fatal("no notice for a download through a share")
	}
	if n := e["notice"].(map[string]any); n["kind"] != "share_download" || n["name"] != "a.txt" {
		t.Fatalf("notice %v", n)
	}
	if _, ok := s.next(t, func(e map[string]any) bool { return e["type"] == "unread" && e["unread"] == float64(1) }, time.Second); !ok {
		t.Fatal("no unread count after the notice")
	}

	drop := mkShare(t, f, `{"vol":"v","path":"inbox","mode":"drop"}`)
	if c, _, _ := anon(f, "POST", "/s/"+drop+"/upload/", "", "Tus-Resumable", "1.0.0", "Upload-Length", "0", "Upload-Metadata", "filename "+b64("from-a-stranger.txt")); c != 201 {
		t.Fatalf("drop upload %d", c)
	}
	if _, ok := s.next(t, func(e map[string]any) bool {
		n, _ := e["notice"].(map[string]any)
		return e["type"] == "notice" && n["kind"] == "share_upload" && n["name"] == "from-a-stranger.txt"
	}, 2*time.Second); !ok {
		t.Fatal("no notice for an upload into a drop box")
	}

	w := f.do("POST", "/api/team/invites", body(`{"role":"member","access":{}}`))
	inv, _ := tokenOf(t, w.Body.String())
	f.do("POST", "/api/invite/"+inv, body(`{"name":"newcomer","password":"long-enough-pw"}`), "X-No-Auth", "1")
	if _, ok := s.next(t, func(e map[string]any) bool {
		n, _ := e["notice"].(map[string]any)
		return e["type"] == "notice" && n["kind"] == "joined" && n["name"] == "newcomer"
	}, 2*time.Second); !ok {
		t.Fatal("no notice for the admin when someone joins")
	}

	list := decode[struct {
		Notices []struct {
			ID   int64
			Kind string
			Read bool
		}
		Unread int
	}](t, f.do("GET", "/api/notices", nil))
	if len(list.Notices) != 3 || list.Unread != 3 || list.Notices[0].Kind != "joined" {
		t.Fatalf("notices %+v", list)
	}
	if w := f.do("POST", "/api/notices/read", body(`{"ids":[`+itoa(list.Notices[2].ID)+`]}`)); w.Code != 204 {
		t.Fatalf("read one %d", w.Code)
	}
	if u := decode[struct{ Unread int }](t, f.do("GET", "/api/notices", nil)).Unread; u != 2 {
		t.Fatalf("unread after one %d", u)
	}
	f.do("POST", "/api/notices/read", body(`{}`))
	if u := decode[struct{ Unread int }](t, f.do("GET", "/api/notices", nil)).Unread; u != 0 {
		t.Fatalf("unread after all %d", u)
	}
	bob := f.member(t, "bob", map[string]string{"v": "r"})
	if _, b := bob.do(f, "GET", "/api/notices", ""); !strings.Contains(b, `"notices":[]`) {
		t.Fatalf("a member sees someone else's notices: %s", b)
	}
}
