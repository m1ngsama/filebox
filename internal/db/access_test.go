package db

import (
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestFirstUserIsAdminAndLaterOnesAreMembers(t *testing.T) {
	d := open(t)
	a, _ := d.SetPassword("admin", "h")
	b, _ := d.SetPassword("bob", "h")
	if role, _, err := d.Access(a); err != nil || role != RoleAdmin {
		t.Fatalf("first user %q %v", role, err)
	}
	if role, grants, err := d.Access(b); err != nil || role != RoleMember || len(grants) != 0 {
		t.Fatalf("second user %q %v %v", role, grants, err)
	}
	if _, err := d.SetPassword("bob", "h2"); err != nil {
		t.Fatal(err)
	}
	if role, _, _ := d.Access(b); role != RoleMember {
		t.Fatal("changing a password changed the role")
	}
}

func TestGrantsAndGuards(t *testing.T) {
	d := open(t)
	admin, _ := d.SetPassword("admin", "h")
	id, err := d.CreateUser("eve", "h", RoleMember, map[string]string{"v": LevelRead, "w": LevelWrite})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.CreateUser("eve", "h", RoleMember, nil); !errors.Is(err, ErrConflict) {
		t.Fatalf("duplicate name: %v", err)
	}
	if _, err := d.CreateUser("bad", "h", RoleMember, map[string]string{"v": "x"}); !errors.Is(err, ErrConflict) {
		t.Fatalf("bad level: %v", err)
	}
	if _, err := d.UserByName("bad"); !errors.Is(err, ErrNotFound) {
		t.Fatal("a failed create left a user behind")
	}
	if _, g, _ := d.Access(id); g["v"] != "r" || g["w"] != "rw" {
		t.Fatalf("grants %v", g)
	}
	d.SetAccess(id, "v", LevelWrite)
	d.SetAccess(id, "w", "")
	if _, g, _ := d.Access(id); g["v"] != "rw" || len(g) != 1 {
		t.Fatalf("after change %v", g)
	}
	if err := d.SetAccess(999, "v", LevelRead); !errors.Is(err, ErrNotFound) {
		t.Fatalf("grant to nobody: %v", err)
	}
	as, err := d.Accounts()
	if err != nil || len(as) != 2 || as[0].Name != "admin" || as[1].Access["v"] != "rw" {
		t.Fatalf("accounts %+v %v", as, err)
	}

	if err := d.SetRole(admin, RoleMember); !errors.Is(err, ErrLastAdmin) {
		t.Fatalf("demoting the last admin: %v", err)
	}
	if err := d.DeleteUser(admin); !errors.Is(err, ErrLastAdmin) {
		t.Fatalf("deleting the last admin: %v", err)
	}
	if err := d.SetRole(id, RoleAdmin); err != nil {
		t.Fatal(err)
	}
	if err := d.SetRole(admin, RoleMember); err != nil {
		t.Fatalf("demoting one of two admins: %v", err)
	}
	if err := d.DeleteUser(id); !errors.Is(err, ErrLastAdmin) {
		t.Fatalf("deleting the only admin left: %v", err)
	}
	d.SetRole(admin, RoleAdmin)
	if err := d.DeleteUser(id); err != nil {
		t.Fatal(err)
	}
	if _, _, err := d.Access(id); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted user: %v", err)
	}
	var n int
	d.QueryRow(`SELECT count(*) FROM volume_access`).Scan(&n)
	if n != 0 {
		t.Fatalf("%d grants outlived their user", n)
	}
}

// Accounts that existed before roles keep reaching everything, and their favorites stay with the first of them.
func TestMigrationMakesExistingUsersAdmins(t *testing.T) {
	p := filepath.Join(t.TempDir(), "old.db")
	q := url.Values{"_pragma": {"foreign_keys(1)"}}
	raw, err := sql.Open("sqlite", (&url.URL{Scheme: "file", OmitHost: true, Path: p, RawQuery: q.Encode()}).String())
	if err != nil {
		t.Fatal(err)
	}
	before := slices.IndexFunc(migrations, func(m string) bool { return strings.Contains(m, "volume_access") })
	if before < 0 {
		t.Fatal("no roles migration")
	}
	for i := 0; i < before; i++ {
		if _, err := raw.Exec(migrations[i]); err != nil {
			t.Fatalf("migration %d: %v", i+1, err)
		}
	}
	raw.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, before))
	for _, s := range []string{
		`INSERT INTO users (name, password_hash) VALUES ('first', 'h'), ('second', 'h')`,
		`INSERT INTO favorites (vol, path, created) VALUES ('v', 'a.txt', 1), ('v', 'b.txt', 2)`,
	} {
		if _, err := raw.Exec(s); err != nil {
			t.Fatal(err)
		}
	}
	raw.Close()

	d, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	for _, name := range []string{"first", "second"} {
		u, _ := d.UserByName(name)
		if u.Role != RoleAdmin {
			t.Fatalf("%s is %q", name, u.Role)
		}
	}
	var owner int64
	var n int
	d.QueryRow(`SELECT min(user_id), count(*) FROM favorites`).Scan(&owner, &n)
	if first, _ := d.UserByName("first"); owner != first.ID || n != 2 {
		t.Fatalf("favorites belong to %d, %d kept", owner, n)
	}
	if id, _ := d.SetPassword("third", "h"); id == 0 {
		t.Fatal("no new user")
	} else if role, _, _ := d.Access(id); role != RoleMember {
		t.Fatalf("a user added later is %q", role)
	}
}
