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
		st := decode[struct{ State, Error string }](t, w)
		if st.State != "running" {
			if st.Error != "" {
				t.Logf("job error: %s", st.Error)
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
	if w := f.do("POST", "/api/rm", body(`{"vol":"v","paths":["d/keep.txt"]}`)); w.Code != 204 {
		t.Fatalf("rm %d %s", w.Code, w.Body)
	}
	if _, err := os.Stat(filepath.Join(f.Dir, "d/keep.txt")); !os.IsNotExist(err) {
		t.Fatal("file still there")
	}
	items := decode[struct {
		Items []struct{ ID, Name, Path string }
	}](t, f.do("GET", "/api/trash?vol=v", nil)).Items
	if len(items) != 1 || items[0].Path != "d/keep.txt" || items[0].Name != "keep.txt" {
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
	f.do("POST", "/api/rm", body(`{"vol":"v","paths":["d"]}`))
	if w := f.do("POST", "/api/trash/empty", body(`{"vol":"v"}`)); w.Code != 204 {
		t.Fatalf("empty %d", w.Code)
	}
	if n := len(decode[struct{ Items []any }](t, f.do("GET", "/api/trash?vol=v", nil)).Items); n != 0 {
		t.Fatalf("trash has %d items after empty", n)
	}
	if w := f.do("POST", "/api/rm", body(`{"vol":"v","paths":["/"]}`)); w.Code != 400 {
		t.Fatalf("rm root %d", w.Code)
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
