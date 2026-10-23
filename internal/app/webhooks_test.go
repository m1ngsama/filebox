package app

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/m1ngsama/filebox/internal/db"
)

type receiver struct {
	*httptest.Server
	mu   sync.Mutex
	got  []map[string]any
	sigs []string
	raw  [][]byte
	fail bool
}

func newReceiver(t *testing.T) *receiver {
	r := &receiver{}
	r.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, q *http.Request) {
		b, _ := io.ReadAll(q.Body)
		r.mu.Lock()
		defer r.mu.Unlock()
		if r.fail {
			w.WriteHeader(500)
			return
		}
		var m map[string]any
		json.Unmarshal(b, &m)
		r.got = append(r.got, m)
		r.raw = append(r.raw, b)
		r.sigs = append(r.sigs, q.Header.Get("X-Filebox-Signature"))
	}))
	t.Cleanup(r.Close)
	return r
}

func (r *receiver) events(t *testing.T, n int) []map[string]any {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		r.mu.Lock()
		if len(r.got) >= n {
			defer r.mu.Unlock()
			return append([]map[string]any(nil), r.got...)
		}
		r.mu.Unlock()
		time.Sleep(10 * time.Millisecond)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	t.Fatalf("wanted %d notifications, got %d: %v", n, len(r.got), r.got)
	return nil
}

func TestWebhooks(t *testing.T) {
	f := newTestApp(t)
	rc := newReceiver(t)
	for _, bad := range []string{
		`{"url":"ftp://x/y","format":"json","kinds":["upload"],"lang":"en"}`,
		`{"url":"` + rc.URL + `","format":"xml","kinds":["upload"],"lang":"en"}`,
		`{"url":"` + rc.URL + `","format":"json","kinds":[],"lang":"en"}`,
		`{"url":"` + rc.URL + `","format":"json","kinds":["nope"],"lang":"en"}`,
		`{"url":"` + rc.URL + `","format":"json","kinds":["upload"],"lang":"fr"}`,
	} {
		if w := f.do("POST", "/api/webhooks", body(bad)); w.Code != 400 {
			t.Fatalf("%s: %d", bad, w.Code)
		}
	}
	w := f.do("POST", "/api/webhooks", body(`{"url":"`+rc.URL+`/secret/path?token=abc","format":"json","kinds":["upload","download"],"secret":"s3cret","lang":"en"}`))
	if w.Code != 201 {
		t.Fatalf("create %d %s", w.Code, w.Body)
	}
	id := decode[struct{ ID int64 }](t, w).ID
	list := decode[struct {
		Webhooks []struct {
			URL    string
			Signed bool
			Kinds  []string
		}
		Kinds []string
	}](t, f.do("GET", "/api/webhooks", nil))
	if len(list.Webhooks) != 1 || list.Webhooks[0].URL != rc.URL || !list.Webhooks[0].Signed || len(list.Kinds) == 0 {
		t.Fatalf("list must hide the path and the secret: %+v", list)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f.App.DB.Observe(f.App.Notify.Event)
	go f.App.Notify.Run(ctx)

	if w := f.do("POST", "/api/webhooks/"+itoa(id)+"/test", nil); w.Code != 204 {
		t.Fatalf("test %d %s", w.Code, w.Body)
	}
	got := rc.events(t, 1)
	if got[0]["event"] != "test" {
		t.Fatalf("test event %v", got[0])
	}
	mac := hmac.New(sha256.New, []byte("s3cret"))
	mac.Write(rc.raw[0])
	if rc.sigs[0] != "sha256="+hex.EncodeToString(mac.Sum(nil)) {
		t.Fatalf("signature %q", rc.sigs[0])
	}

	now := time.Now().Unix()
	f.App.DB.Log(db.Event{At: now, UserID: f.UserID, Kind: db.EventLogin})
	f.App.DB.Log(db.Event{At: now, UserID: f.UserID, Kind: db.EventUpload, Name: "a.txt", Size: 2048, Vol: "v", Path: "a.txt"})
	f.App.DB.Log(db.Event{At: now, UserID: f.UserID, ShareID: 7, Kind: db.EventDownload, Name: "b.txt", Visitor: "x"})
	f.App.DB.Log(db.Event{At: now + 5, UserID: f.UserID, ShareID: 7, Kind: db.EventDownload, Name: "b.txt", Visitor: "x"})
	got = rc.events(t, 3)
	time.Sleep(150 * time.Millisecond)
	got = rc.events(t, 3)
	if len(got) != 3 {
		t.Fatalf("unsubscribed login and a repeated download must not be sent: %v", got)
	}
	seen := map[string]string{}
	for _, g := range got[1:] {
		seen[g["event"].(string)] = g["text"].(string)
	}
	if seen["upload"] != "a.txt was uploaded (2.0 KB)" || seen["download"] != "b.txt was downloaded from a shared link" {
		t.Fatalf("texts %v", seen)
	}

	if w := f.do("PATCH", "/api/webhooks/"+itoa(id), body(`{"kinds":["login"]}`)); w.Code != 204 {
		t.Fatalf("edit %d", w.Code)
	}
	f.App.DB.Log(db.Event{At: now, UserID: f.UserID, Kind: db.EventLogin})
	if got = rc.events(t, 4); got[3]["event"] != "login" {
		t.Fatalf("after edit %v", got)
	}

	rc.mu.Lock()
	rc.fail = true
	rc.mu.Unlock()
	if w := f.do("POST", "/api/webhooks/"+itoa(id)+"/test", nil); w.Code != 502 {
		t.Fatalf("a failing receiver must show as 502, got %d", w.Code)
	}
	list2 := decode[struct {
		Webhooks []struct {
			LastStatus string `json:"last_status"`
		}
	}](t, f.do("GET", "/api/webhooks", nil))
	if list2.Webhooks[0].LastStatus != "HTTP 500" {
		t.Fatalf("status %+v", list2)
	}

	if w := f.do("DELETE", "/api/webhooks/"+itoa(id), nil); w.Code != 204 {
		t.Fatalf("delete %d", w.Code)
	}
	if w := f.do("POST", "/api/webhooks/"+itoa(id)+"/test", nil); w.Code != 404 {
		t.Fatalf("deleted %d", w.Code)
	}
}
