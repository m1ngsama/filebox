package db

import (
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"strconv"
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
	d.DeleteToken(uid, live.ID, "app")
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
	got, err := d.ShareByToken("tok")
	if err != nil || got.Path != "a/b" {
		t.Fatalf("%+v %v", got, err)
	}
	d.HitShare(s.ID)
	got, _ = d.ShareByToken("tok")
	if got.Hits != 1 {
		t.Fatalf("hits = %d", got.Hits)
	}
	exp := &Share{Token: "old", UserID: uid, Vol: "v", Path: "x", Mode: "read", CreatedAt: 1, ExpiresAt: 5}
	d.InsertShare(exp)
	if got, err := d.ShareByToken("old"); err != nil || got.ExpiresAt != 5 {
		t.Fatalf("expired share %+v %v", got, err)
	}
	list, _ := d.ListShares(uid)
	if len(list) != 2 {
		t.Fatalf("ListShares len = %d", len(list))
	}
	d.DeleteShare(uid, s.ID)
	if _, err := d.ShareByToken("tok"); !errors.Is(err, ErrNotFound) {
		t.Fatal("deleted share returned")
	}
}

func TestDeleteShareDropsUnlockTokens(t *testing.T) {
	d := open(t)
	uid, _ := d.SetPassword("admin", "h")
	s := &Share{Token: "tok", UserID: uid, Vol: "v", Path: ".", Mode: "read", CreatedAt: 1}
	d.InsertShare(s)
	d.InsertToken(&Token{UserID: uid, Kind: "share", Hash: "u1", Scope: strconv.FormatInt(s.ID, 10), CreatedAt: 1})
	d.InsertToken(&Token{UserID: uid, Kind: "share", Hash: "u2", Scope: "999", CreatedAt: 1})
	if err := d.DeleteShare(uid, s.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := d.TokenByHash("u1", 2); !errors.Is(err, ErrNotFound) {
		t.Fatal("unlock token of a deleted share survived")
	}
	if _, err := d.TokenByHash("u2", 2); err != nil {
		t.Fatal("unlock token of another share removed")
	}
}

func TestPurgeExpiredTokens(t *testing.T) {
	d := open(t)
	uid, _ := d.SetPassword("admin", "h")
	d.InsertToken(&Token{UserID: uid, Kind: "session", Hash: "old", CreatedAt: 1, ExpiresAt: 50})
	d.InsertToken(&Token{UserID: uid, Kind: "share", Hash: "old2", CreatedAt: 1, ExpiresAt: 99})
	d.InsertToken(&Token{UserID: uid, Kind: "session", Hash: "live", CreatedAt: 1, ExpiresAt: 500})
	d.InsertToken(&Token{UserID: uid, Kind: "app", Hash: "app", CreatedAt: 1})
	if n, err := d.PurgeTokens(100); err != nil || n != 2 {
		t.Fatalf("purged %d %v", n, err)
	}
	var left int
	d.QueryRow(`SELECT count(*) FROM tokens`).Scan(&left)
	if left != 2 {
		t.Fatalf("%d tokens left", left)
	}
}

func TestDeleteTokensOfKind(t *testing.T) {
	d := open(t)
	uid, _ := d.SetPassword("admin", "h")
	other, _ := d.SetPassword("bob", "h")
	d.InsertToken(&Token{UserID: uid, Kind: "session", Hash: "s1", CreatedAt: 1})
	d.InsertToken(&Token{UserID: uid, Kind: "session", Hash: "s2", CreatedAt: 1})
	d.InsertToken(&Token{UserID: uid, Kind: "app", Hash: "a1", CreatedAt: 1})
	d.InsertToken(&Token{UserID: other, Kind: "session", Hash: "s3", CreatedAt: 1})
	if n, err := d.DeleteTokens(uid, "session"); err != nil || n != 2 {
		t.Fatalf("deleted %d %v", n, err)
	}
	for h, want := range map[string]bool{"s1": false, "s2": false, "a1": true, "s3": true} {
		if _, err := d.TokenByHash(h, 2); (err == nil) != want {
			t.Errorf("%s present=%v", h, err == nil)
		}
	}
}

func TestMigrateFromV1(t *testing.T) {
	p := filepath.Join(t.TempDir(), "t.db")
	s, err := sql.Open("sqlite", "file:"+p)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{migrations[0], `PRAGMA user_version = 1`, `INSERT INTO users (name, password_hash) VALUES ('admin', 'h')`} {
		if _, err := s.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	s.Close()
	d, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	var v int
	d.QueryRow(`PRAGMA user_version`).Scan(&v)
	if v != len(migrations) {
		t.Fatalf("user_version = %d", v)
	}
	u, err := d.UserByName("admin")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.ListPasskeys(u.ID); err != nil {
		t.Fatal(err)
	}
}

func TestMigrateFromV2(t *testing.T) {
	p := filepath.Join(t.TempDir(), "t.db")
	s, err := sql.Open("sqlite", "file:"+p)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{migrations[0], migrations[1], `PRAGMA user_version = 2`,
		`INSERT INTO users (name, password_hash) VALUES ('admin', 'h')`,
		`INSERT INTO tokens (user_id, kind, hash, created_at) VALUES (1, 'session', 'x', 1)`} {
		if _, err := s.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	s.Close()
	d, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	var v int
	d.QueryRow(`PRAGMA user_version`).Scan(&v)
	if v != len(migrations) {
		t.Fatalf("user_version = %d", v)
	}
	ts, err := d.ListTokens(1, "session")
	if err != nil || len(ts) != 1 || ts[0].UserAgent != "" || ts[0].IP != "" {
		t.Fatalf("tokens %+v %v", ts, err)
	}
	tk := Token{UserID: 1, Kind: "session", Hash: "y", CreatedAt: 2, UserAgent: "ua", IP: "10.0.0.1"}
	if err := d.InsertToken(&tk); err != nil {
		t.Fatal(err)
	}
	if got, _ := d.TokenByHash("y", 0); got.UserAgent != "ua" || got.IP != "10.0.0.1" {
		t.Fatalf("token %+v", got)
	}
}

func TestPasskeys(t *testing.T) {
	d := open(t)
	uid, _ := d.SetPassword("admin", "h")
	other, _ := d.SetPassword("bob", "h")
	h1, err := d.WebAuthnID(uid, []byte("first"))
	if err != nil || string(h1) != "first" {
		t.Fatalf("%q %v", h1, err)
	}
	if h, _ := d.WebAuthnID(uid, []byte("second")); string(h) != "first" {
		t.Fatalf("handle changed to %q", h)
	}
	if u, err := d.UserByWebAuthnID([]byte("first")); err != nil || u.ID != uid {
		t.Fatalf("%+v %v", u, err)
	}
	if _, err := d.UserByWebAuthnID([]byte("nope")); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
	pk := &Passkey{UserID: uid, CredentialID: []byte{1, 2}, Credential: "{}", Name: "Mac", CreatedAt: 5}
	if err := d.InsertPasskey(pk); err != nil {
		t.Fatal(err)
	}
	if err := d.InsertPasskey(&Passkey{UserID: uid, CredentialID: []byte{1, 2}, Credential: "{}", Name: "dup", CreatedAt: 5}); !errors.Is(err, ErrConflict) {
		t.Fatal("duplicate credential id accepted")
	}
	if err := d.UsePasskey(pk.ID, `{"a":1}`, 9); err != nil {
		t.Fatal(err)
	}
	if err := d.RenamePasskey(other, pk.ID, "x"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign rename err = %v", err)
	}
	if err := d.DeletePasskey(other, pk.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign delete err = %v", err)
	}
	if err := d.RenamePasskey(uid, pk.ID, "Phone"); err != nil {
		t.Fatal(err)
	}
	ps, _ := d.ListPasskeys(uid)
	if len(ps) != 1 || ps[0].Name != "Phone" || ps[0].Credential != `{"a":1}` || ps[0].LastUsedAt != 9 || ps[0].CreatedAt != 5 {
		t.Fatalf("%+v", ps)
	}
	if err := d.DeletePasskey(uid, pk.ID); err != nil {
		t.Fatal(err)
	}
	if ps, _ := d.ListPasskeys(uid); len(ps) != 0 {
		t.Fatalf("%+v", ps)
	}
}

func TestUserNames(t *testing.T) {
	d := open(t)
	if ns, err := d.UserNames(); err != nil || len(ns) != 0 {
		t.Fatalf("%v %v", ns, err)
	}
	d.SetPassword("m1ng", "h")
	d.SetPassword("bob", "h")
	if ns, _ := d.UserNames(); len(ns) != 2 || ns[0] != "bob" || ns[1] != "m1ng" {
		t.Fatalf("%v", ns)
	}
}

func TestOpenOddPath(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "a?b#c%20d e")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "t.db")
	d, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if _, err := d.SetPassword("admin", "h"); err != nil {
		t.Fatal(err)
	}
	var mode string
	if err := d.QueryRow(`PRAGMA journal_mode`).Scan(&mode); err != nil || mode != "wal" {
		t.Fatalf("journal mode %q %v", mode, err)
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatal(err)
	}
}

func TestMigrateBackfillsSearch(t *testing.T) {
	p := filepath.Join(t.TempDir(), "t.db")
	s, err := sql.Open("sqlite", "file:"+p)
	if err != nil {
		t.Fatal(err)
	}
	qs := append([]string{}, migrations[:5]...)
	qs = append(qs, `PRAGMA user_version = 5`,
		`INSERT INTO files (vol, path, dir, size, mtime) VALUES ('v', 'Music', 1, 0, 1), ('v', 'Music/song.flac', 0, 9, 2)`)
	for _, q := range qs {
		if _, err := s.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	s.Close()
	d, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	var name string
	var size int64
	if err := d.QueryRow(`SELECT f.name, f.size FROM files_fts JOIN files f ON f.id = files_fts.rowid WHERE files_fts MATCH '"song"'`).Scan(&name, &size); err != nil || name != "song.flac" || size != 9 {
		t.Fatalf("backfilled %q %d %v", name, size, err)
	}
}

func TestOpenRebuildsSearchAfterOldBinaryWrites(t *testing.T) {
	p := filepath.Join(t.TempDir(), "t.db")
	d, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.Exec(`INSERT INTO files (vol, path, dir, size, mtime) VALUES ('v', 'kept.txt', 0, 1, 1), ('v', 'gone.txt', 0, 1, 1)`); err != nil {
		t.Fatal(err)
	}
	d.Close()
	if d, err = Open(p); err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if _, err := d.Exec(`DELETE FROM files WHERE path = 'gone.txt'`); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := d.QueryRow(`SELECT count(*) FROM files_fts WHERE files_fts MATCH '"kept"'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("kept.txt found %d times, %v", n, err)
	}
	if _, err := d.Exec(`INSERT INTO files_fts (files_fts, rank) VALUES ('integrity-check', 1)`); err != nil {
		t.Fatal(err)
	}
}
