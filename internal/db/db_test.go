package db

import (
	"errors"
	"path/filepath"
	"testing"
)

func open(t *testing.T) *DB {
	t.Helper()
	d, err := Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { d.Close() })
	return d
}

func TestReopenKeepsVersion(t *testing.T) {
	p := filepath.Join(t.TempDir(), "t.db")
	d, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	d.SetPassword("admin", "h")
	d.Close()
	d, err = Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if _, err := d.UserByName("admin"); err != nil {
		t.Fatal(err)
	}
}

func TestUsers(t *testing.T) {
	d := open(t)
	id, err := d.SetPassword("admin", "h1")
	if err != nil {
		t.Fatal(err)
	}
	id2, _ := d.SetPassword("admin", "h2")
	if id != id2 {
		t.Fatal("upsert created a second user")
	}
	u, _ := d.UserByName("admin")
	if u.PasswordHash != "h2" {
		t.Fatalf("hash = %q", u.PasswordHash)
	}
	if _, err := d.UserByName("nobody"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
}

func TestTokens(t *testing.T) {
	d := open(t)
	uid, _ := d.SetPassword("admin", "h")
	live := &Token{UserID: uid, Kind: "app", Hash: "aa", Label: "phone", CreatedAt: 1, ExpiresAt: 0}
	dead := &Token{UserID: uid, Kind: "session", Hash: "bb", CreatedAt: 1, ExpiresAt: 50}
	d.InsertToken(live)
	d.InsertToken(dead)
	if _, err := d.TokenByHash("aa", 100); err != nil {
		t.Fatal(err)
	}
	if _, err := d.TokenByHash("bb", 100); !errors.Is(err, ErrNotFound) {
		t.Fatal("expired token returned")
	}
	d.TouchToken(dead.ID, 40, 200)
	if _, err := d.TokenByHash("bb", 100); err != nil {
		t.Fatal("touch did not extend expiry")
	}
	ts, _ := d.ListTokens(uid, "app")
	if len(ts) != 1 || ts[0].Label != "phone" {
		t.Fatalf("ListTokens = %+v", ts)
	}
	d.DeleteToken(uid, live.ID)
	if _, err := d.TokenByHash("aa", 100); !errors.Is(err, ErrNotFound) {
		t.Fatal("deleted token returned")
	}
	d.DeleteTokenByHash("bb")
	if _, err := d.TokenByHash("bb", 100); !errors.Is(err, ErrNotFound) {
		t.Fatal("deleted-by-hash token returned")
	}
}

func TestShares(t *testing.T) {
	d := open(t)
	uid, _ := d.SetPassword("admin", "h")
	s := &Share{Token: "tok", UserID: uid, Vol: "v", Path: "a/b", Mode: "read", CreatedAt: 1, ExpiresAt: 0}
	if err := d.InsertShare(s); err != nil {
		t.Fatal(err)
	}
	got, err := d.ShareByToken("tok", 10)
	if err != nil || got.Path != "a/b" {
		t.Fatalf("%+v %v", got, err)
	}
	d.HitShare(s.ID)
	got, _ = d.ShareByToken("tok", 10)
	if got.Hits != 1 {
		t.Fatalf("hits = %d", got.Hits)
	}
	exp := &Share{Token: "old", UserID: uid, Vol: "v", Path: "x", Mode: "read", CreatedAt: 1, ExpiresAt: 5}
	d.InsertShare(exp)
	if _, err := d.ShareByToken("old", 10); !errors.Is(err, ErrNotFound) {
		t.Fatal("expired share returned")
	}
	list, _ := d.ListShares(uid)
	if len(list) != 2 {
		t.Fatalf("ListShares len = %d", len(list))
	}
	d.DeleteShare(uid, s.ID)
	if _, err := d.ShareByToken("tok", 10); !errors.Is(err, ErrNotFound) {
		t.Fatal("deleted share returned")
	}
}
