package auth

import (
	"context"
	"io/fs"
	"time"

	"github.com/m1ngsama/filebox/internal/db"
)

const accessTTL = 30 * time.Second

type cached struct {
	role   string
	grants map[string]string
	at     time.Time
}

// grant fills in what the user behind a valid token may reach; a user who no longer exists gets nothing.
func (a *Auth) grant(p Principal, ok bool) (Principal, bool) {
	if !ok {
		return p, false
	}
	role, grants, err := a.accessOf(p.UserID)
	if err != nil {
		return Principal{}, false
	}
	p.Admin, p.Vols = role == db.RoleAdmin, grants
	return p, true
}

func (a *Auth) accessOf(userID int64) (string, map[string]string, error) {
	now := a.Now()
	if v, ok := a.access.Load(userID); ok {
		if c := v.(cached); now.Sub(c.at) < accessTTL {
			return c.role, c.grants, nil
		}
	}
	role, grants, err := a.DB.Access(userID)
	if err != nil {
		a.access.Delete(userID)
		return "", nil, err
	}
	a.access.Store(userID, cached{role, grants, now})
	return role, grants, nil
}

// Forget drops what is remembered about a user's access, after an admin changed it.
func (a *Auth) Forget(userID int64) { a.access.Delete(userID) }

// Of reports a user's role and the levels they hold, the way a request from them would see it.
func (a *Auth) Of(userID int64) (Principal, bool) { return a.grant(Principal{UserID: userID}, true) }

// Level is what the principal holds on a volume: "", "r" or "rw".
func (p Principal) Level(vol string) string {
	l := p.Vols[vol]
	if p.Admin {
		l = db.LevelWrite
	}
	if p.ReadOnly && l == db.LevelWrite {
		l = db.LevelRead
	}
	return l
}

func (p Principal) Can(vol string, write bool) bool {
	switch p.Level(vol) {
	case db.LevelWrite:
		return true
	case db.LevelRead:
		return !write
	}
	return false
}

// Volumes keeps the names the principal may read, in the order given.
func (p Principal) Volumes(all []string) []string {
	out := []string{}
	for _, v := range all {
		if p.Can(v, false) {
			out = append(out, v)
		}
	}
	return out
}

// Allow says whether the request's principal may read, or write, a volume. A volume they cannot read does not exist for them.
func Allow(ctx context.Context, vol string, write bool) error {
	p, ok := From(ctx)
	switch {
	case !ok || !p.Can(vol, false):
		return fs.ErrNotExist
	case write && !p.Can(vol, true):
		return fs.ErrPermission
	}
	return nil
}
