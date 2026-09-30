package app

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/m1ngsama/filebox/internal/api"
)

func body(s string) *strings.Reader { return strings.NewReader(s) }

func decode[T any](t *testing.T, w *httptest.ResponseRecorder) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(w.Body.Bytes(), &v); err != nil {
		t.Fatalf("decode %q: %v", w.Body.String(), err)
	}
	return v
}

type entries struct {
	Entries []struct {
		Name string
		Dir  bool
		Size int64
	}
}

func TestLogin(t *testing.T) {
	f := newTestApp(t)
	if w := f.do("GET", "/api/login", nil, "X-No-Auth", "1"); w.Code != 200 || strings.TrimSpace(w.Body.String()) != `{"single":true}` {
		t.Fatalf("login info %d %s", w.Code, w.Body)
	}
	w := f.do("POST", "/api/login", body(`{"password":"nope-nope"}`), "X-No-Auth", "1")
	if w.Code != 401 {
		t.Fatalf("bad pw = %d", w.Code)
	}
	w = f.do("POST", "/api/login", body(`{"password":"pw-pw-pw-pw"}`), "X-No-Auth", "1")
	if w.Code != 204 || !strings.Contains(w.Header().Get("Set-Cookie"), "fb_session=") {
		t.Fatalf("login %d %q", w.Code, w.Header().Get("Set-Cookie"))
	}
	if w := f.do("GET", "/api/me", nil); w.Code != 200 || !strings.Contains(w.Body.String(), `"v"`) {
		t.Fatalf("me %d %s", w.Code, w.Body)
	}
	if w := f.do("GET", "/api/nope", nil); w.Code != 404 || !strings.Contains(w.Body.String(), "error") {
		t.Fatalf("unknown api route %d %s", w.Code, w.Body)
	}
	f.App.DB.SetPassword("bob", "h")
	if w := f.do("GET", "/api/login", nil, "X-No-Auth", "1"); strings.TrimSpace(w.Body.String()) != `{"single":false}` {
		t.Fatalf("login info with two users %s", w.Body)
	}
	if w := f.do("POST", "/api/login", body(`{"password":"pw-pw-pw-pw"}`), "X-No-Auth", "1"); w.Code != 401 {
		t.Fatalf("nameless login with two users = %d", w.Code)
	}
	if w := f.do("POST", "/api/login", body(`{"name":"admin","password":"pw-pw-pw-pw"}`), "X-No-Auth", "1"); w.Code != 204 {
		t.Fatalf("named login %d", w.Code)
	}
}

func TestList(t *testing.T) {
	f := newTestApp(t)
	f.write(t, "b.txt", "12345")
	f.write(t, "a/x", "")
	f.write(t, ".trash/1/y", "")
	f.write(t, ".filebox/uploads/z", "")
	os.Symlink(t.TempDir(), filepath.Join(f.Dir, "escape"))
	w := f.do("GET", "/api/ls?vol=v&path=/", nil)
	got := decode[entries](t, w)
	var names []string
	for _, e := range got.Entries {
		names = append(names, e.Name)
	}
	if strings.Join(names, ",") != "a,b.txt" {
		t.Fatalf("names = %v", names)
	}
	if got.Entries[1].Size != 5 || !got.Entries[0].Dir {
		t.Fatalf("entries = %+v", got.Entries)
	}
	if w := f.do("GET", "/api/ls?vol=zz&path=/", nil); w.Code != 404 {
		t.Fatalf("unknown vol %d", w.Code)
	}
	if w := f.do("GET", "/api/ls?vol=v&path=/", nil, "X-No-Auth", "1"); w.Code != 401 {
		t.Fatalf("anon %d", w.Code)
	}
}

func TestMkdirMv(t *testing.T) {
	f := newTestApp(t)
	if w := f.do("POST", "/api/mkdir", body(`{"vol":"v","path":"new"}`)); w.Code != 201 {
		t.Fatalf("mkdir %d %s", w.Code, w.Body)
	}
	if w := f.do("POST", "/api/mkdir", body(`{"vol":"v","path":"new"}`)); w.Code != 409 {
		t.Fatalf("mkdir dup %d", w.Code)
	}
	f.write(t, "f.txt", "x")
	f.write(t, "g.txt", "y")
	if w := f.do("POST", "/api/mv", body(`{"src":{"vol":"v","path":"f.txt"},"dst":{"vol":"v","path":"g.txt"}}`)); w.Code != 409 {
		t.Fatalf("mv onto existing %d", w.Code)
	}
	if w := f.do("POST", "/api/mv", body(`{"src":{"vol":"v","path":"new"},"dst":{"vol":"v","path":"new/inner"}}`)); w.Code != 400 {
		t.Fatalf("mv into itself %d", w.Code)
	}
	if w := f.do("POST", "/api/mv", body(`{"src":{"vol":"v","path":"f.txt"},"dst":{"vol":"v","path":"new/f.txt"}}`)); w.Code != 204 {
		t.Fatalf("mv %d %s", w.Code, w.Body)
	}
	if _, err := os.Stat(filepath.Join(f.Dir, "new/f.txt")); err != nil {
		t.Fatal(err)
	}
	if w := f.do("POST", "/api/mkdir", body(`{"vol":"v","path":"x"}`), "Sec-Fetch-Site", "cross-site"); w.Code != 403 {
		t.Fatalf("cross-site mkdir %d", w.Code)
	}
}

func waitJob(t *testing.T, f *fixture, id string) string {
	t.Helper()
	for i := 0; i < 100; i++ {
		w := f.do("GET", "/api/jobs/"+id, nil)
		st := decode[struct{ State, Code string }](t, w)
		if st.State != "running" {
			if st.Code != "" {
				t.Logf("job error: %s", st.Code)
			}
			return st.State
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("job did not finish")
	return ""
}

func TestCrossVolumeMoveAndCopy(t *testing.T) {
	f := newTestApp(t)
	f.write(t, "tree/a.txt", "aaa")
	f.write(t, "tree/sub/b.txt", "bb")
	w := f.do("POST", "/api/cp", body(`{"src":{"vol":"v","path":"tree"},"dst":{"vol":"v","path":"copy"}}`))
	if w.Code != 202 {
		t.Fatalf("cp %d %s", w.Code, w.Body)
	}
	if st := waitJob(t, f, decode[struct{ Job string }](t, w).Job); st != "done" {
		t.Fatalf("cp state %s", st)
	}
	if b, _ := os.ReadFile(filepath.Join(f.Dir, "copy/sub/b.txt")); string(b) != "bb" {
		t.Fatalf("copy content %q", b)
	}
	w = f.do("POST", "/api/mv", body(`{"src":{"vol":"v","path":"tree"},"dst":{"vol":"w","path":"moved"}}`))
	if w.Code != 202 {
		t.Fatalf("mv %d %s", w.Code, w.Body)
	}
	if st := waitJob(t, f, decode[struct{ Job string }](t, w).Job); st != "done" {
		t.Fatalf("mv state %s", st)
	}
	if b, _ := os.ReadFile(filepath.Join(f.Dir2, "moved/a.txt")); string(b) != "aaa" {
		t.Fatalf("moved content %q", b)
	}
	if _, err := os.Stat(filepath.Join(f.Dir, "tree")); !os.IsNotExist(err) {
		t.Fatal("source survived cross-volume move")
	}
}

func TestTrash(t *testing.T) {
	f := newTestApp(t)
	f.write(t, "d/keep.txt", "k")
	w := f.do("POST", "/api/rm", body(`{"vol":"v","paths":["d/keep.txt"]}`))
	rm := decode[struct {
		Trashed []struct{ Path, ID string }
		Failed  []any
	}](t, w)
	if w.Code != 200 || len(rm.Trashed) != 1 || rm.Trashed[0].Path != "d/keep.txt" || rm.Trashed[0].ID == "" || len(rm.Failed) != 0 {
		t.Fatalf("rm %d %s", w.Code, w.Body)
	}
	if _, err := os.Stat(filepath.Join(f.Dir, "d/keep.txt")); !os.IsNotExist(err) {
		t.Fatal("file still there")
	}
	items := decode[struct {
		Items []struct{ ID, Name, Path string }
	}](t, f.do("GET", "/api/trash?vol=v", nil)).Items
	if len(items) != 1 || items[0].Path != "d/keep.txt" || items[0].Name != "keep.txt" || items[0].ID != rm.Trashed[0].ID {
		t.Fatalf("items %+v", items)
	}
	f.write(t, "d/keep.txt", "new")
	if w := f.do("POST", "/api/trash/restore", body(`{"vol":"v","id":"`+items[0].ID+`"}`)); w.Code != 409 {
		t.Fatalf("restore over existing %d", w.Code)
	}
	os.Remove(filepath.Join(f.Dir, "d/keep.txt"))
	if w := f.do("POST", "/api/trash/restore", body(`{"vol":"v","id":"`+items[0].ID+`"}`)); w.Code != 204 {
		t.Fatalf("restore %d %s", w.Code, w.Body)
	}
	if b, _ := os.ReadFile(filepath.Join(f.Dir, "d/keep.txt")); string(b) != "k" {
		t.Fatalf("restored %q", b)
	}
	f.write(t, "gone.txt", "g")
	f.write(t, "stay.txt", "s")
	ids := decode[struct{ Trashed []struct{ ID string } }](t, f.do("POST", "/api/rm", body(`{"vol":"v","paths":["gone.txt","stay.txt"]}`))).Trashed
	if w := f.do("POST", "/api/trash/delete", body(`{"vol":"v","ids":["`+ids[0].ID+`","../d"]}`)); w.Code != 400 {
		t.Fatalf("delete with a bad id %d", w.Code)
	}
	if w := f.do("POST", "/api/trash/delete", body(`{"vol":"v","ids":["`+ids[0].ID+`"]}`)); w.Code != 204 {
		t.Fatalf("delete %d %s", w.Code, w.Body)
	}
	left := decode[struct{ Items []struct{ Name string } }](t, f.do("GET", "/api/trash?vol=v", nil)).Items
	if len(left) != 1 || left[0].Name != "stay.txt" {
		t.Fatalf("after delete %+v", left)
	}
	f.do("POST", "/api/rm", body(`{"vol":"v","paths":["d"]}`))
	if w := f.do("POST", "/api/trash/empty", body(`{"vol":"v"}`)); w.Code != 204 {
		t.Fatalf("empty %d", w.Code)
	}
	if n := len(decode[struct{ Items []any }](t, f.do("GET", "/api/trash?vol=v", nil)).Items); n != 0 {
		t.Fatalf("trash has %d items after empty", n)
	}
	f.write(t, "a.txt", "a")
	f.write(t, "c.txt", "c")
	w = f.do("POST", "/api/rm", body(`{"vol":"v","paths":["a.txt","/","missing","c.txt"]}`))
	type failure struct {
		Path   string
		Status int
		Error  string
	}
	got := decode[struct{ Failed []failure }](t, w)
	if w.Code != 200 || len(got.Failed) != 2 || got.Failed[0] != (failure{"/", 400, "bad path"}) || got.Failed[1] != (failure{"missing", 404, "not found"}) {
		t.Fatalf("partial rm %d %s", w.Code, w.Body)
	}
	for _, n := range []string{"a.txt", "c.txt"} {
		if _, err := os.Stat(filepath.Join(f.Dir, n)); !os.IsNotExist(err) {
			t.Fatalf("%s not trashed", n)
		}
	}
}

func TestTokensAPI(t *testing.T) {
	f := newTestApp(t)
	w := f.do("POST", "/api/tokens", body(`{"label":"ipad","readonly":true}`))
	if w.Code != 201 {
		t.Fatalf("new %d %s", w.Code, w.Body)
	}
	tok := decode[struct{ Token string }](t, w).Token
	f.write(t, "a", "1")
	if w := f.do("GET", "/raw/v/a", nil, "X-No-Auth", "1", "Authorization", "Bearer "+tok); w.Code != 200 {
		t.Fatalf("new token rejected %d", w.Code)
	}
	list := decode[struct {
		Tokens []struct {
			ID       int64
			Label    string
			Readonly bool
		}
	}](t, f.do("GET", "/api/tokens", nil)).Tokens
	var id int64
	for _, x := range list {
		if x.Label == "ipad" && x.Readonly {
			id = x.ID
		}
	}
	if id == 0 {
		t.Fatalf("list %+v", list)
	}
	if w := f.do("DELETE", "/api/tokens/"+strconv.FormatInt(id, 10), nil); w.Code != 204 {
		t.Fatalf("delete %d", w.Code)
	}
	if w := f.do("GET", "/raw/v/a", nil, "X-No-Auth", "1", "Authorization", "Bearer "+tok); w.Code != 401 {
		t.Fatalf("revoked token accepted %d", w.Code)
	}
}

func waitJobStatus(t *testing.T, jobs *api.Jobs, id string) api.JobStatus {
	t.Helper()
	for i := 0; i < 100; i++ {
		st, ok := jobs.Get(id)
		if !ok {
			t.Fatal("job not found")
		}
		if st.State != "running" {
			return st
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("job did not finish")
	return api.JobStatus{}
}

func TestJobDestinationConflict(t *testing.T) {
	f := newTestApp(t)
	f.write(t, "tree/a.txt", "aaa")
	v, ok := f.App.Vols.Get("v")
	if !ok {
		t.Fatal("volume v missing")
	}
	if err := v.Root.MkdirAll("copy", 0o755); err != nil {
		t.Fatal(err)
	}
	if err := v.Root.WriteFile("copy/marker.txt", []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	jobs := api.NewJobs(nil)
	id := jobs.Start(v, v, "tree", "copy", false)
	st := waitJobStatus(t, jobs, id)
	if st.State != "error" {
		t.Fatalf("state = %s", st.State)
	}
	if b, err := os.ReadFile(filepath.Join(f.Dir, "copy/marker.txt")); err != nil || string(b) != "keep" {
		t.Fatalf("pre-existing destination clobbered: %v %q", err, b)
	}
	if _, err := os.Stat(filepath.Join(f.Dir, "tree")); err != nil {
		t.Fatal("source removed on failed copy", err)
	}
}

func TestJobErrorCode(t *testing.T) {
	f := newTestApp(t)
	f.write(t, "a.txt", "a")
	f.write(t, "b.txt", "b")
	v, _ := f.App.Vols.Get("v")
	jobs := api.NewJobs(nil)
	st := waitJobStatus(t, jobs, jobs.Start(v, v, "a.txt", "b.txt", false))
	if st.State != "error" || st.Code != "exists" {
		t.Fatalf("status = %+v, want error with code exists", st)
	}
	st = waitJobStatus(t, jobs, jobs.Start(v, v, "gone.txt", "c.txt", false))
	if st.Code != "notfound" {
		t.Fatalf("status = %+v, want code notfound", st)
	}
}

func TestJobCopyCleansUpOnFailure(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: chmod 0 has no effect")
	}
	f := newTestApp(t)
	f.write(t, "bad/ok.txt", "x")
	f.write(t, "bad/secret.txt", "y")
	secret := filepath.Join(f.Dir, "bad/secret.txt")
	if err := os.Chmod(secret, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(secret, 0o644) })
	v, ok := f.App.Vols.Get("v")
	if !ok {
		t.Fatal("volume v missing")
	}
	jobs := api.NewJobs(nil)
	id := jobs.Start(v, v, "bad", "bad-copy", false)
	st := waitJobStatus(t, jobs, id)
	if st.State != "error" {
		t.Fatalf("state = %s", st.State)
	}
	if _, err := os.Stat(filepath.Join(f.Dir, "bad-copy")); !os.IsNotExist(err) {
		t.Fatalf("partial destination left behind: %v", err)
	}
}

func TestJobCopyNeverExposesPartialDestination(t *testing.T) {
	f := newTestApp(t)
	chunk := strings.Repeat("x", 256<<10)
	for i := range 100 {
		f.write(t, "big/"+strconv.Itoa(i), chunk)
	}
	v, _ := f.App.Vols.Get("v")
	w, _ := f.App.Vols.Get("w")
	jobs := api.NewJobs(nil)
	id := jobs.Start(v, w, "big", "big", false)
	for {
		ents, err := os.ReadDir(filepath.Join(f.Dir2, "big"))
		st, _ := jobs.Get(id)
		if err == nil && len(ents) != 100 {
			t.Fatalf("destination visible with %d of 100 files (state %s)", len(ents), st.State)
		}
		if st.State != "running" {
			if st.State != "done" {
				t.Fatalf("state %+v", st)
			}
			break
		}
	}
	if ents, _ := os.ReadDir(filepath.Join(f.Dir2, ".filebox/jobs")); len(ents) != 0 {
		t.Fatalf("staging left behind: %d", len(ents))
	}
}

func TestJobPlacementIsExclusive(t *testing.T) {
	f := newTestApp(t)
	f.write(t, "a.txt", "new")
	f.write(t, "tree/a.txt", "new")
	v, _ := f.App.Vols.Get("v")
	w, _ := f.App.Vols.Get("w")
	os.WriteFile(filepath.Join(f.Dir2, "a.txt"), []byte("foreign"), 0o644)
	os.Mkdir(filepath.Join(f.Dir2, "tree"), 0o755)
	jobs := api.NewJobs(nil)
	for _, p := range []string{"a.txt", "tree"} {
		st := waitJobStatus(t, jobs, jobs.Start(v, w, p, p, true))
		if st.State != "error" || st.Code != "exists" {
			t.Fatalf("%s: %+v", p, st)
		}
		if _, err := os.Stat(filepath.Join(f.Dir, p)); err != nil {
			t.Fatalf("%s: source removed", p)
		}
	}
	if b, _ := os.ReadFile(filepath.Join(f.Dir2, "a.txt")); string(b) != "foreign" {
		t.Fatalf("foreign file overwritten: %q", b)
	}
	if ents, err := os.ReadDir(filepath.Join(f.Dir2, "tree")); err != nil || len(ents) != 0 {
		t.Fatalf("foreign directory replaced: %v %d", err, len(ents))
	}
}

func TestJobMoveRefusesSymlinks(t *testing.T) {
	f := newTestApp(t)
	f.write(t, "tree/a.txt", "a")
	os.Symlink("a.txt", filepath.Join(f.Dir, "tree/link"))
	v, _ := f.App.Vols.Get("v")
	w, _ := f.App.Vols.Get("w")
	jobs := api.NewJobs(nil)
	st := waitJobStatus(t, jobs, jobs.Start(v, w, "tree", "tree", true))
	if st.State != "error" || st.Code != "internal" {
		t.Fatalf("status %+v", st)
	}
	if _, err := os.Lstat(filepath.Join(f.Dir, "tree/link")); err != nil {
		t.Fatal("source symlink lost")
	}
	if _, err := os.Stat(filepath.Join(f.Dir2, "tree")); !os.IsNotExist(err) {
		t.Fatalf("destination created: %v", err)
	}
	st = waitJobStatus(t, jobs, jobs.Start(v, w, "tree", "copy", false))
	if st.State != "done" {
		t.Fatalf("copy with symlink %+v", st)
	}
}

func TestClearStaging(t *testing.T) {
	f := newTestApp(t)
	f.write(t, ".filebox/jobs/dead/half.bin", "x")
	f.write(t, ".filebox/tmp/PARTIALPUT", "x")
	api.ClearStaging(f.App.Vols)
	for _, d := range []string{".filebox/jobs", ".filebox/tmp"} {
		if _, err := os.Stat(filepath.Join(f.Dir, d)); !os.IsNotExist(err) {
			t.Fatalf("stale %s kept: %v", d, err)
		}
	}
}

func TestListETag(t *testing.T) {
	f := newTestApp(t)
	f.write(t, "d/a.txt", "a")
	w := f.do("GET", "/api/ls?vol=v&path=d", nil)
	tag := w.Header().Get("ETag")
	if w.Code != 200 || len(tag) < 20 || tag[0] != '"' || w.Header().Get("Cache-Control") != "private, no-cache" {
		t.Fatalf("first ls %d %q %q", w.Code, tag, w.Header().Get("Cache-Control"))
	}
	for _, inm := range []string{tag, "W/" + tag, `"x", ` + tag, "*"} {
		if w := f.do("GET", "/api/ls?vol=v&path=d", nil, "If-None-Match", inm); w.Code != 304 || w.Body.Len() != 0 || w.Header().Get("ETag") != tag {
			t.Fatalf("If-None-Match %s: %d %q", inm, w.Code, w.Body)
		}
	}
	if w := f.do("GET", "/api/ls?vol=v&path=d", nil, "If-None-Match", `"stale"`); w.Code != 200 {
		t.Fatalf("stale tag %d", w.Code)
	}
	f.write(t, "d/a.txt", "ab")
	w = f.do("GET", "/api/ls?vol=v&path=d", nil, "If-None-Match", tag)
	if w.Code != 200 || w.Header().Get("ETag") == tag {
		t.Fatalf("after a size change %d %q", w.Code, w.Header().Get("ETag"))
	}
	if w := f.do("GET", "/api/ls?vol=v&path=d", nil, "If-None-Match", w.Header().Get("ETag")); w.Code != 304 {
		t.Fatalf("new tag %d", w.Code)
	}
	if w := f.do("GET", "/api/ls?vol=w&path=d", nil, "If-None-Match", tag); w.Code != 404 {
		t.Fatalf("other volume %d", w.Code)
	}
}
