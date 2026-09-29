package db

import (
	"slices"
	"strconv"
	"strings"
	"testing"
)

func TestVisitorHash(t *testing.T) {
	d := open(t)
	a, b := d.visitor("203.0.113.7", 1), d.visitor("203.0.113.8", 1)
	if len(a) != 16 || a == b || a != d.visitor("203.0.113.7", 1) || strings.Contains(a, ".") {
		t.Fatalf("visitor hashes %q %q", a, b)
	}
	if d.visitor("203.0.113.7", 2) == a {
		t.Fatal("visitor key did not rotate with the day")
	}
	if day2 := d.visitor("203.0.113.7", 2); d.visitor("203.0.113.7", 1) != day2 || d.visitor("203.0.113.7", 2) != day2 {
		t.Fatal("a late request for the previous day rotated the key back")
	}
	if d.visitor("2001:db8:1:2::1", 2) != d.visitor("2001:db8:1:2:ffff::9", 2) || d.visitor("2001:db8:1:2::1", 2) == d.visitor("2001:db8:1:3::1", 2) {
		t.Fatal("IPv6 visitors are not keyed by their /64")
	}
	if d.visitor("::ffff:203.0.113.7", 2) != d.visitor("203.0.113.7", 2) {
		t.Fatal("mapped IPv4 differs from IPv4")
	}
	if d.visitor("", 2) != "" || d.visitor("not-an-ip", 2) != "" {
		t.Fatal("invalid IP hashed")
	}
	if open(t).visitor("203.0.113.7", 1) == a {
		t.Fatal("visitor key is shared between instances")
	}
	var n int
	d.QueryRow(`SELECT count(*) FROM sqlite_master WHERE name = 'settings'`).Scan(&n)
	if n != 0 {
		t.Fatal("a visitor key is persisted")
	}
}

func TestEventsBatchedAndPruned(t *testing.T) {
	d := open(t)
	uid, _ := d.SetPassword("admin", "h")
	other, _ := d.SetPassword("bob", "h")
	s := &Share{Token: "tok", UserID: uid, Vol: "v", Path: "p", Mode: "read", CreatedAt: 1}
	d.InsertShare(s)
	day := int64(86400 * 100)
	for _, at := range []int64{day + 10, day + 20} {
		d.View(s.ID, "a", at)
	}
	d.View(s.ID, "b", day+30)
	d.View(s.ID, "a", day+86400)
	for _, e := range []Event{
		{At: day + 40, ShareID: s.ID, Kind: EventDownload, Visitor: "a", Name: "x.txt", Size: 3},
		{At: day + 45, ShareID: s.ID, Kind: EventDownload, Visitor: "a", Name: "x.txt", Size: 3},
		{At: day + 50, UserID: uid, Kind: EventLogin},
		{At: day + 60, Kind: EventLoginFailed, Visitor: "z"},
		{At: day + 61, Kind: EventLoginFailed, Visitor: "z"},
	} {
		d.Log(e)
	}
	d.Flush()
	got, _ := d.ShareByToken("tok")
	var rows, perDay int
	d.QueryRow(`SELECT count(*), max(n) FROM share_views WHERE share_id = ?`, s.ID).Scan(&rows, &perDay)
	if got.Views != 3 || rows != 2 || perDay != 2 {
		t.Fatalf("views = %d, rows %d, max per day %d", got.Views, rows, perDay)
	}
	all, err := d.Events(EventFilter{UserID: uid, Limit: 50})
	if err != nil || len(all) != 3 || all[0].Kind != EventLoginFailed || all[2].UserID != uid {
		t.Fatalf("events %+v %v", all, err)
	}
	if es, _ := d.Events(EventFilter{UserID: other, Limit: 50}); len(es) != 0 {
		t.Fatalf("another user sees %+v", es)
	}
	dl, _ := d.Events(EventFilter{UserID: uid, ShareID: s.ID, Kinds: []string{EventDownload}, Limit: 1})
	if len(dl) != 1 {
		t.Fatalf("filtered %+v", dl)
	}
	if page, _ := d.Events(EventFilter{UserID: uid, Before: all[1].ID, Limit: 50}); len(page) != 1 || page[0].ID != all[2].ID {
		t.Fatalf("paging %+v", page)
	}
	d.Log(Event{At: day + 89999, UserID: uid, Kind: EventTokenCreate})
	for i := range 5 {
		d.Log(Event{At: day + 90000 + int64(i), ShareID: s.ID, Kind: EventDownload, Visitor: "v", Name: strconv.Itoa(i)})
	}
	d.Log(Event{At: day + 90009, UserID: uid, Kind: EventShareCreate})
	d.Flush()
	if n, _ := d.PruneEvents(day+86400, 3); n != 5 {
		t.Fatalf("pruned %d", n)
	}
	left, _ := d.Events(EventFilter{UserID: uid, Limit: 50})
	kept := []string{}
	for _, e := range left {
		kept = append(kept, e.Kind+e.Name)
	}
	slices.Sort(kept)
	if strings.Join(kept, " ") != "download2 download3 download4 share_create token_create" {
		t.Fatalf("after prune %v", kept)
	}
	d.QueryRow(`SELECT count(*) FROM share_views`).Scan(&rows)
	if rows != 1 {
		t.Fatalf("share_views after prune %d", rows)
	}
}

func TestEventQueueNeverBlocks(t *testing.T) {
	d := open(t)
	d.events.stop()
	for range queueSize + 10 {
		d.Log(Event{Kind: EventDownload})
	}
	if d.events.dropped.Load() != 10 {
		t.Fatalf("dropped %d", d.events.dropped.Load())
	}
}

func TestFlushAfterCloseReturns(t *testing.T) {
	d := open(t)
	d.Close()
	for range 100 {
		d.Flush()
	}
}

func TestAuditEventsSurviveAFlood(t *testing.T) {
	d := open(t)
	uid, _ := d.SetPassword("admin", "h")
	for i := range queueSize * 2 {
		d.Log(Event{At: 1, Kind: EventDownload, Visitor: strconv.Itoa(i)})
	}
	for range 300 {
		d.Log(Event{At: 2, UserID: uid, Kind: EventLogin})
	}
	d.Flush()
	var n int
	d.QueryRow(`SELECT count(*) FROM events WHERE kind = 'login'`).Scan(&n)
	if n != 300 {
		t.Fatalf("%d of 300 login events kept", n)
	}
}

func TestViewDedupeIsBounded(t *testing.T) {
	d := open(t)
	for i := range maxSeen + 5 {
		d.View(1, strconv.Itoa(i), 86400)
	}
	if len(d.events.seen) != maxSeen {
		t.Fatalf("seen %d", len(d.events.seen))
	}
	d.View(1, "x", 2*86400)
	if len(d.events.seen) != 1 {
		t.Fatal("seen set not reset on a new day")
	}
	d.View(1, "y", 86400)
	d.View(1, "x", 2*86400+5)
	if len(d.events.seen) != 2 {
		t.Fatalf("a late request for the previous day reset the set: %d", len(d.events.seen))
	}
}
