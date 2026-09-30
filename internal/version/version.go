package version

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/m1ngsama/filebox/internal/db"
	"github.com/m1ngsama/filebox/internal/vol"
)

const (
	Upload    = "upload"
	WebDAV    = "webdav"
	Restore   = "restore"
	Recovered = "recovered"

	PerFile    = 50
	MinFreePct = 10
	guardGrace = 10 * time.Minute
	OrphanTTL  = 30 * 24 * time.Hour
	sidecar    = ".path"
)

var validID = regexp.MustCompile(`^[0-9]{1,19}-[0-9a-f]{16}$`)

type Version struct {
	ID      string `json:"id"`
	Vol     string `json:"-"`
	Path    string `json:"-"`
	Size    int64  `json:"size"`
	Mtime   int64  `json:"mtime"`
	Created int64  `json:"created"`
	Source  string `json:"source"`
	UserID  int64  `json:"-"`
}

type Store struct {
	DB    *db.DB
	Now   func() time.Time
	Usage func(*vol.Volume) (vol.Usage, error)
	mu    sync.Mutex
}

func (s *Store) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Store) usage(v *vol.Volume) (vol.Usage, error) {
	if s.Usage != nil {
		return s.Usage(v)
	}
	return v.Usage()
}

func File(id string) string { return path.Join(vol.VersionsDir, id) }

func ValidID(id string) bool { return validID.MatchString(id) }

func (s *Store) Capture(v *vol.Volume, rel, source string, user int64) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.guard(v, s.now().UnixMilli())
	id, err := s.capture(v, rel, source, user, false)
	if err == nil {
		s.prune(v, rel)
	}
	return id, err
}

var link = (*os.Root).Link

func noLink(err error) bool {
	return errors.Is(err, syscall.EXDEV) || errors.Is(err, syscall.EPERM) || errors.Is(err, syscall.ENOTSUP) ||
		errors.Is(err, syscall.EOPNOTSUPP) || errors.Is(err, syscall.EMLINK)
}

func (s *Store) capture(v *vol.Volume, rel, source string, user int64, byLink bool) (string, error) {
	fi, err := v.Root.Lstat(rel)
	if err != nil {
		return "", err
	}
	if !fi.Mode().IsRegular() {
		return "", fs.ErrInvalid
	}
	now := s.now()
	b := make([]byte, 8)
	rand.Read(b)
	id := strconv.FormatInt(now.UnixMilli(), 10) + "-" + hex.EncodeToString(b)
	if err := v.Root.MkdirAll(vol.VersionsDir, 0o700); err != nil {
		return "", err
	}
	if err := v.Root.WriteFile(File(id)+sidecar, []byte(rel), 0o600); err != nil {
		return "", err
	}
	keep := v.Root.Rename
	if byLink {
		keep = func(from, to string) error { return link(v.Root, from, to) }
	}
	if err := keep(rel, File(id)); err != nil {
		v.Root.Remove(File(id) + sidecar)
		return "", err
	}
	_, err = s.DB.Exec(`INSERT INTO versions (id, vol, path, size, mtime, created, source, user_id) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		id, v.Name, rel, fi.Size(), fi.ModTime().UnixMilli(), now.UnixMilli(), source, user)
	if err != nil {
		switch {
		case byLink:
			v.Root.Remove(File(id))
			v.Root.Remove(File(id) + sidecar)
		case vol.Move(v.Root, File(id), rel) == nil:
			v.Root.Remove(File(id) + sidecar)
		}
		return "", err
	}
	return id, nil
}

const columns = `id, vol, path, size, mtime, created, source, user_id`

func scan(rows *sql.Rows) ([]Version, error) {
	defer rows.Close()
	out := []Version{}
	for rows.Next() {
		var x Version
		if err := rows.Scan(&x.ID, &x.Vol, &x.Path, &x.Size, &x.Mtime, &x.Created, &x.Source, &x.UserID); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

func (s *Store) List(volName, rel string) ([]Version, error) {
	rows, err := s.DB.Query(`SELECT `+columns+` FROM versions WHERE vol = ? AND path = ? ORDER BY created DESC, id DESC`, volName, rel)
	if err != nil {
		return nil, err
	}
	return scan(rows)
}

func (s *Store) Get(volName, id string) (Version, error) {
	if !ValidID(id) {
		return Version{}, vol.ErrBadPath
	}
	rows, err := s.DB.Query(`SELECT `+columns+` FROM versions WHERE vol = ? AND id = ?`, volName, id)
	if err != nil {
		return Version{}, err
	}
	xs, err := scan(rows)
	if err != nil {
		return Version{}, err
	}
	if len(xs) == 0 {
		return Version{}, fs.ErrNotExist
	}
	return xs[0], nil
}

func (s *Store) Restore(v *vol.Volume, id string, user int64) (string, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	x, err := s.Get(v.Name, id)
	if err != nil {
		return "", "", err
	}
	dst, err := v.Clean(x.Path)
	if err != nil {
		return "", "", err
	}
	prev := ""
	if fi, err := v.Root.Lstat(dst); err == nil {
		if !fi.Mode().IsRegular() {
			return "", "", fs.ErrExist
		}
		if prev, err = s.swap(v, File(id), dst, Restore, user); err != nil {
			return "", "", err
		}
	} else if err := v.Root.MkdirAll(path.Dir(dst), 0o755); err != nil {
		return "", "", err
	} else if err := vol.Move(v.Root, File(id), dst); err != nil {
		return "", "", err
	}
	s.forget(v, id)
	return dst, prev, nil
}

func (s *Store) swap(v *vol.Volume, from, rel, source string, user int64) (string, error) {
	s.guard(v, s.now().UnixMilli())
	id, err := s.capture(v, rel, source, user, true)
	if err == nil {
		if err := v.Root.Rename(from, rel); err != nil {
			s.drop(v, id)
			return "", err
		}
		return id, nil
	}
	if !noLink(err) {
		return "", err
	}
	if id, err = s.capture(v, rel, source, user, false); err != nil {
		return "", err
	}
	if err := v.Root.Rename(from, rel); err != nil {
		s.unwind(v, id, rel)
		return "", err
	}
	return id, nil
}

func (s *Store) Replace(v *vol.Volume, tmp, rel, source string, user int64) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	fi, err := v.Root.Lstat(rel)
	switch {
	case err == nil && fi.IsDir():
		return "", &fs.PathError{Op: "replace", Path: rel, Err: syscall.EISDIR}
	case err != nil || !fi.Mode().IsRegular() || fi.Size() == 0 || vol.Junk(path.Base(rel), false):
		return "", v.Root.Rename(tmp, rel)
	}
	id, err := s.swap(v, tmp, rel, source, user)
	if err == nil {
		s.prune(v, rel)
	}
	return id, err
}

func (s *Store) Moved(v *vol.Volume, to string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows, err := s.DB.Query(`SELECT `+columns+` FROM versions WHERE vol = ? AND (path = ? OR (path > ? AND path < ?))`,
		v.Name, to, to+"/", to+"0")
	if err != nil {
		slog.Warn("versions after rename", "vol", v.Name, "path", to, "err", err)
		return
	}
	xs, _ := scan(rows)
	for _, x := range xs {
		v.Root.WriteFile(File(x.ID)+sidecar, []byte(x.Path), 0o600)
	}
}

func (s *Store) Revert(v *vol.Volume, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	x, err := s.Get(v.Name, id)
	if err != nil {
		return err
	}
	dst, err := v.Clean(x.Path)
	if err != nil {
		return err
	}
	if err := v.Root.RemoveAll(dst); err != nil {
		return err
	}
	if err := v.Root.Rename(File(id), dst); err != nil {
		return err
	}
	s.forget(v, id)
	return nil
}

func (s *Store) unwind(v *vol.Volume, id, dst string) {
	if err := vol.Move(v.Root, File(id), dst); err != nil {
		slog.Error("restore version", "vol", v.Name, "id", id, "err", err)
		return
	}
	s.forget(v, id)
}

func (s *Store) forget(v *vol.Volume, id string) {
	if _, err := s.DB.Exec(`DELETE FROM versions WHERE vol = ? AND id = ?`, v.Name, id); err != nil {
		slog.Warn("forget version", "vol", v.Name, "id", id, "err", err)
	}
	v.Root.Remove(File(id) + sidecar)
}

func (s *Store) Delete(v *vol.Volume, id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, err := s.Get(v.Name, id); err != nil {
		return err
	}
	return s.drop(v, id)
}

func (s *Store) drop(v *vol.Volume, id string) error {
	if err := v.Root.Remove(File(id)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	s.forget(v, id)
	return nil
}

func (s *Store) Prune(vols *vol.Set) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, v := range vols.All() {
		s.expire(v)
		s.prune(v, "")
	}
}

func (s *Store) expire(v *vol.Volume) {
	if _, err := v.Root.Lstat(vol.VersionsDir); err != nil {
		return
	}
	rows, err := s.DB.Query(`SELECT path, min(orphaned) FROM versions WHERE vol = ? GROUP BY path`, v.Name)
	if err != nil {
		slog.Warn("expire versions", "vol", v.Name, "err", err)
		return
	}
	type group struct {
		path  string
		since int64
	}
	var gs []group
	for rows.Next() {
		var g group
		if rows.Scan(&g.path, &g.since) == nil {
			gs = append(gs, g)
		}
	}
	rows.Close()
	now := s.now().UnixMilli()
	origins := trashed(v)
	for _, g := range gs {
		_, err := v.Root.Lstat(g.path)
		gone := errors.Is(err, fs.ErrNotExist) && !slices.ContainsFunc(origins, func(o string) bool { return g.path == o || strings.HasPrefix(g.path, o+"/") })
		switch {
		case !gone && g.since != 0:
			s.DB.Exec(`UPDATE versions SET orphaned = 0 WHERE vol = ? AND path = ?`, v.Name, g.path)
		case gone && g.since == 0:
			s.DB.Exec(`UPDATE versions SET orphaned = ? WHERE vol = ? AND path = ?`, now, v.Name, g.path)
		case gone && now-g.since > OrphanTTL.Milliseconds():
			xs, err := s.List(v.Name, g.path)
			if err != nil {
				continue
			}
			for _, x := range xs {
				s.drop(v, x.ID)
			}
		}
	}
}

func trashed(v *vol.Volume) []string {
	f, err := v.Root.Open(vol.TrashDir)
	if err != nil {
		return nil
	}
	ids, _ := f.Readdirnames(-1)
	f.Close()
	var out []string
	for _, id := range ids {
		if b, err := v.Root.ReadFile(path.Join(vol.TrashDir, id, ".origin")); err == nil {
			out = append(out, string(b))
		}
	}
	return out
}

func (s *Store) prune(v *vol.Volume, only string) {
	q, args := `SELECT `+columns+` FROM versions WHERE vol = ?`, []any{v.Name}
	if only != "" {
		q, args = q+` AND path = ?`, append(args, only)
	}
	rows, err := s.DB.Query(q+` ORDER BY path, created DESC, id DESC`, args...)
	if err != nil {
		slog.Warn("prune versions", "vol", v.Name, "err", err)
		return
	}
	xs, err := scan(rows)
	if err != nil {
		slog.Warn("prune versions", "vol", v.Name, "err", err)
		return
	}
	now := s.now().UnixMilli()
	for i := 0; i < len(xs); {
		j := i
		for j < len(xs) && xs[j].Path == xs[i].Path {
			j++
		}
		_, drop := Thin(xs[i:j], now)
		for _, x := range drop {
			s.drop(v, x.ID)
		}
		i = j
	}
	s.guard(v, now)
}

type bucket struct{ tier, n int64 }

func Thin(newestFirst []Version, now int64) (keep, drop []Version) {
	const hour, day = int64(time.Hour / time.Millisecond), int64(24 * time.Hour / time.Millisecond)
	seen := map[bucket]bool{}
	var recent []int
	for _, x := range newestFirst {
		age := now - x.Created
		var b bucket
		switch {
		case age < hour:
			b = bucket{0, -int64(len(keep)) - 1}
		case age < day:
			b = bucket{1, x.Created / hour}
		case age < 30*day:
			b = bucket{2, x.Created / day}
		default:
			b = bucket{3, x.Created / (7 * day)}
		}
		if seen[b] {
			drop = append(drop, x)
			continue
		}
		seen[b] = true
		if b.tier == 0 && len(keep) > 0 {
			recent = append(recent, len(keep))
		}
		keep = append(keep, x)
	}
	over := len(keep) - PerFile
	if over <= 0 {
		return keep, drop
	}
	evict := map[int]bool{}
	for i := len(recent) - 1; i >= 0 && over > 0; i-- {
		evict[recent[i]] = true
		over--
	}
	var kept []Version
	for i, x := range keep {
		if evict[i] {
			drop = append(drop, x)
		} else {
			kept = append(kept, x)
		}
	}
	if len(kept) > PerFile {
		drop = append(drop, kept[PerFile:]...)
		kept = kept[:PerFile]
	}
	return kept, drop
}

func (s *Store) guard(v *vol.Volume, now int64) {
	u, err := s.usage(v)
	if err != nil || u.Total == 0 || u.Free*100 >= u.Total*MinFreePct {
		return
	}
	need := int64(u.Total*MinFreePct/100 - u.Free)
	rows, err := s.DB.Query(`SELECT `+columns+` FROM versions WHERE vol = ? AND created < ? ORDER BY created, id`,
		v.Name, now-guardGrace.Milliseconds())
	if err != nil {
		return
	}
	old, err := scan(rows)
	if err != nil {
		return
	}
	for _, x := range old {
		if need <= 0 {
			return
		}
		freed := x.Size
		if fi, err := v.Root.Lstat(File(x.ID)); err == nil && shared(fi) {
			freed = 0
		}
		if s.drop(v, x.ID) == nil {
			need -= freed
		}
	}
}

func shared(fi fs.FileInfo) bool {
	st, ok := fi.Sys().(*syscall.Stat_t)
	return ok && st.Nlink > 1
}

func (s *Store) Recover(vols *vol.Set) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var errs []error
	for _, v := range vols.All() {
		if err := s.recover(v); err != nil {
			errs = append(errs, fmt.Errorf("versions of %s: %w", v.Name, err))
		}
	}
	return errors.Join(errs...)
}

func (s *Store) recover(v *vol.Volume) error {
	rows, err := s.DB.Query(`SELECT `+columns+` FROM versions WHERE vol = ?`, v.Name)
	if err != nil {
		return err
	}
	xs, err := scan(rows)
	if err != nil {
		return err
	}
	f, err := v.Root.Open(vol.VersionsDir)
	if errors.Is(err, fs.ErrNotExist) {
		if len(xs) > 0 {
			slog.Warn("versions folder missing, keeping its rows in case the disk is not mounted", "vol", v.Name, "rows", len(xs))
		}
		return nil
	}
	if err != nil {
		return err
	}
	names, err := f.Readdirnames(-1)
	f.Close()
	if err != nil {
		return err
	}
	present := map[string]bool{}
	for _, n := range names {
		present[n] = true
	}
	known := map[string]bool{}
	for _, x := range xs {
		if !present[x.ID] {
			slog.Warn("version file missing, dropping its row", "vol", v.Name, "id", x.ID)
			s.forget(v, x.ID)
			continue
		}
		kept, err1 := v.Root.Lstat(File(x.ID))
		live, err2 := v.Root.Lstat(x.Path)
		if err1 == nil && err2 == nil && os.SameFile(kept, live) {
			slog.Warn("version is the live file after a crash, dropping it", "vol", v.Name, "id", x.ID)
			s.drop(v, x.ID)
			continue
		}
		known[x.ID] = true
	}
	for _, n := range names {
		id, isSidecar := strings.CutSuffix(n, sidecar)
		switch {
		case !ValidID(id) || known[id]:
		case isSidecar && !present[id]:
			v.Root.Remove(File(n))
		case !isSidecar:
			if err := s.adopt(v, id); err != nil {
				slog.Warn("adopt version", "vol", v.Name, "id", id, "err", err)
			}
		}
	}
	return nil
}

func (s *Store) adopt(v *vol.Volume, id string) error {
	b, err := v.Root.ReadFile(File(id) + sidecar)
	if err != nil {
		return err
	}
	rel, err := v.Clean(string(b))
	if err != nil || rel == "." {
		return vol.ErrBadPath
	}
	fi, err := v.Root.Lstat(File(id))
	if err != nil {
		return err
	}
	if !fi.Mode().IsRegular() {
		return fs.ErrInvalid
	}
	ms, _ := strconv.ParseInt(id[:strings.IndexByte(id, '-')], 10, 64)
	_, err = s.DB.Exec(`INSERT INTO versions (id, vol, path, size, mtime, created, source) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		id, v.Name, rel, fi.Size(), fi.ModTime().UnixMilli(), ms, Recovered)
	if err == nil {
		slog.Info("adopted orphaned version", "vol", v.Name, "path", rel, "id", id)
	}
	return err
}
