package db

import (
	"strings"
	"testing"
)

func TestVisitorHash(t *testing.T) {
	d := open(t)
	a, b := d.Visitor("203.0.113.7"), d.Visitor("203.0.113.8")
	if len(a) != 16 || a == b || a != d.Visitor("203.0.113.7") || strings.Contains(a, "203") {
		t.Fatalf("visitor hashes %q %q", a, b)
	}
	if d.Visitor("") != "" {
		t.Fatal("empty IP hashed")
	}
	other := open(t)
	if other.Visitor("203.0.113.7") == a {
		t.Fatal("visitor key is not per install")
	}
}

func TestEventsViewOncePerDayAndPrune(t *testing.T) {
	d := open(t)
	uid, _ := d.SetPassword("admin", "h")
	s := &Share{Token: "tok", UserID: uid, Vol: "v", Path: "p", Mode: "read", CreatedAt: 1}
	d.InsertShare(s)
	day := int64(86400 * 100)
	for _, e := range []Event{
		{At: day + 10, ShareID: s.ID, Kind: EventView, Visitor: "a"},
		{At: day + 20, ShareID: s.ID, Kind: EventView, Visitor: "a"},
		{At: day + 30, ShareID: s.ID, Kind: EventView, Visitor: "b"},
		{At: day + 86400, ShareID: s.ID, Kind: EventView, Visitor: "a"},
		{At: day + 40, ShareID: s.ID, Kind: EventDownload, Visitor: "a", Name: "x.txt", Size: 3},
		{At: day + 50, UserID: uid, Kind: EventLogin},
		{At: day + 60, Kind: EventLoginFailed},
	} {
		if err := d.Log(e); err != nil {
			t.Fatal(err)
		}
	}
	got, _ := d.ShareByToken("tok")
	if got.Views != 3 {
		t.Fatalf("views = %d", got.Views)
	}
	all, err := d.Events(EventFilter{UserID: uid, Limit: 50})
	if err != nil || len(all) != 6 || all[0].Kind != EventLoginFailed || all[len(all)-1].Share != "v:/p" {
		t.Fatalf("events %+v %v", all, err)
	}
	if es, _ := d.Events(EventFilter{UserID: uid + 1, Limit: 50}); len(es) != 1 {
		t.Fatalf("another user sees %+v", es)
	}
	views, _ := d.Events(EventFilter{UserID: uid, ShareID: s.ID, Kinds: []string{EventView}, Limit: 2})
	if len(views) != 2 {
		t.Fatalf("filtered %+v", views)
	}
	if page, _ := d.Events(EventFilter{UserID: uid, Before: views[1].ID, Limit: 50}); len(page) == 0 || page[0].ID >= views[1].ID {
		t.Fatalf("paging %+v", page)
	}
	if n, _ := d.PruneEvents(day + 86400); n != 5 {
		t.Fatalf("pruned %d", n)
	}
	if left, _ := d.Events(EventFilter{UserID: uid, Limit: 50}); len(left) != 1 || left[0].At != day+86400 {
		t.Fatalf("after prune %+v", left)
	}
}
