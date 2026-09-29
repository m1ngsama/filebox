package app

import (
	"encoding/json"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/m1ngsama/filebox/internal/db"
)

func mkShare(t *testing.T, f *fixture, js string) string {
	t.Helper()
	w := f.do("POST", "/api/shares", body(js))
	if w.Code != 201 {
		t.Fatalf("create share %d %s", w.Code, w.Body)
	}
	return decode[struct{ Token string }](t, w).Token
}

func anon(f *fixture, method, url, b string, hdr ...string) (int, string, string) {
	w := f.do(method, url, body(b), append([]string{"X-No-Auth", "1"}, hdr...)...)
	return w.Code, w.Body.String(), w.Header().Get("Set-Cookie")
}

func TestShareRead(t *testing.T) {
	f := newTestApp(t)
	f.write(t, "pub/a.txt", "hello")
	f.write(t, "secret.txt", "no")
	tok := mkShare(t, f, `{"vol":"v","path":"pub","mode":"read"}`)
	if c, b, _ := anon(f, "GET", "/s/"+tok+"/info", ""); c != 200 || !strings.Contains(b, `"dir":true`) {
		t.Fatalf("info %d %s", c, b)
	}
	if c, b, _ := anon(f, "GET", "/s/"+tok+"/ls?path=../../", ""); c != 200 || !strings.Contains(b, "a.txt") || strings.Contains(b, "secret") {
		t.Fatalf("ls %d %s", c, b)
	}
	if c, b, _ := anon(f, "GET", "/s/"+tok+"/raw/a.txt", ""); c != 200 || b != "hello" {
		t.Fatalf("raw %d %q", c, b)
	}
	if c, _, _ := anon(f, "GET", "/s/"+tok+"/raw/%2E%2E/secret.txt", ""); c == 200 {
		t.Fatal("escaped share via encoded ..")
	}
	if c, _, _ := anon(f, "POST", "/s/"+tok+"/upload/", "", "Tus-Resumable", "1.0.0", "Upload-Length", "1",
		"Upload-Metadata", "filename "+b64("x")); c != 403 {
		t.Fatalf("upload on read share %d", c)
	}
	for _, u := range []string{"/s/" + tok, "/s/" + tok + "/"} {
		if c, b, _ := anon(f, "GET", u, ""); c != 200 || b != "<!doctype html>share" {
			t.Fatalf("share page %s %d %q", u, c, b)
		}
	}
	list := decode[struct {
		Shares []struct {
			ID   int64
			Hits int64
		}
	}](t, f.do("GET", "/api/shares", nil)).Shares
	if len(list) != 1 || list[0].Hits != 1 {
		t.Fatalf("shares %+v", list)
	}
	if w := f.do("DELETE", "/api/shares/"+strconv.FormatInt(list[0].ID, 10), nil); w.Code != 204 {
		t.Fatalf("delete %d", w.Code)
	}
	if c, _, _ := anon(f, "GET", "/s/"+tok+"/info", ""); c != 404 {
		t.Fatalf("deleted share %d", c)
	}
}

func TestSharePassword(t *testing.T) {
	f := newTestApp(t)
	f.write(t, "p/a.txt", "x")
	tok := mkShare(t, f, `{"vol":"v","path":"p","mode":"read","password":"open sesame"}`)
	if c, b, _ := anon(f, "GET", "/s/"+tok+"/info", ""); c != 200 || !strings.Contains(b, `"locked":true`) {
		t.Fatalf("info %d %s", c, b)
	}
	if c, _, _ := anon(f, "GET", "/s/"+tok+"/ls", ""); c != 401 {
		t.Fatalf("locked ls %d", c)
	}
	if c, _, _ := anon(f, "POST", "/s/"+tok+"/unlock", `{"password":"wrong"}`); c != 401 {
		t.Fatalf("wrong pw %d", c)
	}
	c, _, cookie := anon(f, "POST", "/s/"+tok+"/unlock", `{"password":"open sesame"}`)
	if c != 204 || !strings.Contains(cookie, "Path=/s/"+tok+"/") {
		t.Fatalf("unlock %d %q", c, cookie)
	}
	val := strings.TrimPrefix(strings.SplitN(cookie, ";", 2)[0], "fb_share=")
	if c, _, _ := anon(f, "GET", "/s/"+tok+"/ls", "", "Cookie", "fb_share="+val); c != 200 {
		t.Fatalf("ls after unlock %d", c)
	}
	if c, _, _ := anon(f, "POST", "/s/"+tok+"/unlock", `{"password":"open sesame"}`, "Sec-Fetch-Site", "cross-site"); c != 403 {
		t.Fatalf("cross-site unlock %d", c)
	}
}

func TestShareExpired(t *testing.T) {
	f := newTestApp(t)
	f.write(t, "e.txt", "x")
	f.App.DB.InsertShare(&db.Share{Token: "expiredtoken", UserID: f.UserID, Vol: "v", Path: "e.txt", Mode: "read", CreatedAt: 1, ExpiresAt: 2})
	if c, _, _ := anon(f, "GET", "/s/expiredtoken/info", ""); c != 410 {
		t.Fatalf("expired %d", c)
	}
}

func TestShareDrop(t *testing.T) {
	f := newTestApp(t)
	f.write(t, "inbox/existing.txt", "old")
	tok := mkShare(t, f, `{"vol":"v","path":"inbox","mode":"drop"}`)
	for _, p := range []string{"/ls", "/raw/existing.txt", "/thumb/existing.txt"} {
		if c, _, _ := anon(f, "GET", "/s/"+tok+p, ""); c != 403 {
			t.Errorf("drop %s = %d", p, c)
		}
	}
	c, _, _ := anon(f, "POST", "/s/"+tok+"/upload/", "", "Tus-Resumable", "1.0.0", "Upload-Length", "0",
		"Upload-Metadata", "filename "+b64("existing.txt")+",overwrite "+b64("1"))
	if c != 201 {
		t.Fatalf("drop upload %d", c)
	}
	if _, err := os.Stat(filepath.Join(f.Dir, "inbox/existing (1).txt")); err != nil {
		t.Fatal("drop overwrote or lost the upload")
	}
	if b, _ := os.ReadFile(filepath.Join(f.Dir, "inbox/existing.txt")); string(b) != "old" {
		t.Fatal("drop overwrote the existing file")
	}
	c, _, _ = anon(f, "POST", "/s/"+tok+"/upload/", "", "Tus-Resumable", "1.0.0", "Upload-Length", "0",
		"Upload-Metadata", "filename "+b64("e.txt")+",relativePath "+b64("../../e.txt"))
	if c != 400 {
		t.Fatalf("relativePath escape %d", c)
	}
	up := "/s/" + tok + "/upload/"
	w := f.do("POST", up, nil, "X-No-Auth", "1", "Tus-Resumable", "1.0.0", "Upload-Length", "2",
		"Upload-Metadata", "filename "+b64("d.txt"))
	loc := w.Header().Get("Location")
	if id, ok := strings.CutPrefix(loc, up); w.Code != 201 || !ok || strings.Contains(id, "/") {
		t.Fatalf("create %d %q", w.Code, loc)
	}
	if w := f.do("HEAD", loc, nil, "X-No-Auth", "1", "Tus-Resumable", "1.0.0"); w.Code != 200 || w.Header().Get("Upload-Metadata") != "" {
		t.Fatalf("head %d %v", w.Code, w.Header())
	}
	other := mkShare(t, f, `{"vol":"v","path":"inbox","mode":"upload"}`)
	if w := f.do("HEAD", strings.Replace(loc, tok, other, 1), nil, "X-No-Auth", "1", "Tus-Resumable", "1.0.0"); w.Code != 404 {
		t.Fatalf("upload visible through another share %d", w.Code)
	}
	if w := f.do("HEAD", "/upload/"+strings.TrimPrefix(loc, up), nil, "Tus-Resumable", "1.0.0"); w.Code != 404 {
		t.Fatalf("share upload visible to the user route %d", w.Code)
	}
	w = f.do("PATCH", loc, strings.NewReader("hi"), "X-No-Auth", "1", "Tus-Resumable", "1.0.0",
		"Upload-Offset", "0", "Content-Type", "application/offset+octet-stream")
	if b, _ := os.ReadFile(filepath.Join(f.Dir, "inbox/d.txt")); w.Code != 204 || string(b) != "hi" {
		t.Fatalf("patch %d %q", w.Code, b)
	}
}

func TestShareSingleFile(t *testing.T) {
	f := newTestApp(t)
	f.write(t, "one.txt", "solo")
	tok := mkShare(t, f, `{"vol":"v","path":"one.txt","mode":"read"}`)
	if c, _, _ := anon(f, "GET", "/s/"+tok+"/ls", ""); c != 400 {
		t.Fatalf("ls on file share %d", c)
	}
	if c, b, _ := anon(f, "GET", "/s/"+tok+"/raw/", ""); c != 200 || b != "solo" {
		t.Fatalf("raw %d %q", c, b)
	}
	if c, _, _ := anon(f, "GET", "/s/"+tok+"/raw/other", ""); c != 404 {
		t.Fatalf("raw sub-path on file share %d", c)
	}
	if c, _, _ := anon(f, "POST", "/s/"+tok+"/upload/", "", "Tus-Resumable", "1.0.0", "Upload-Length", "0",
		"Upload-Metadata", "filename "+b64("x")); c != 403 {
		t.Fatalf("upload to file share %d", c)
	}
	w := f.do("POST", "/api/shares", body(`{"vol":"v","path":"one.txt","mode":"upload"}`))
	if w.Code != 400 {
		t.Fatalf("upload share on a file %d", w.Code)
	}
}

func TestShareCreateValidation(t *testing.T) {
	f := newTestApp(t)
	for _, js := range []string{
		`{"vol":"v","path":"missing","mode":"read"}`,
		`{"vol":"v","path":"/","mode":"bogus"}`,
		`{"vol":"nope","path":"/","mode":"read"}`,
		`{"vol":"v","path":"/","mode":"read","expires_in":-5}`,
	} {
		if w := f.do("POST", "/api/shares", body(js)); w.Code < 400 {
			t.Errorf("%s → %d", js, w.Code)
		}
	}
}

func TestUploadStorageErrorHidden(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores directory permissions")
	}
	f := newTestApp(t)
	f.write(t, "inbox/.keep", "")
	tok := mkShare(t, f, `{"vol":"v","path":"inbox","mode":"drop"}`)
	up := filepath.Join(f.Dir, ".filebox/uploads")
	os.Chmod(up, 0o500)
	t.Cleanup(func() { os.Chmod(up, 0o700) })
	md := "vol " + b64("v") + ",dir " + b64("inbox") + ",filename " + b64("x")
	for _, url := range []string{"/s/" + tok + "/upload/", "/upload/"} {
		w := f.do("POST", url, nil, "Tus-Resumable", "1.0.0", "Upload-Length", "1", "Upload-Metadata", md)
		if b := w.Body.String(); w.Code < 300 || strings.Contains(b, f.Dir) || strings.Contains(b, ".filebox") || strings.Contains(b, "/v/") {
			t.Errorf("%s: %d %q", url, w.Code, b)
		}
	}
}

func TestShareUnlockNotReusedAfterDelete(t *testing.T) {
	f := newTestApp(t)
	f.write(t, "p/a.txt", "x")
	type created struct {
		ID    int64
		Token string
	}
	w := f.do("POST", "/api/shares", body(`{"vol":"v","path":"p","mode":"read","password":"first pw"}`))
	a := decode[created](t, w)
	_, _, cookie := anon(f, "POST", "/s/"+a.Token+"/unlock", `{"password":"first pw"}`)
	val := strings.TrimPrefix(strings.SplitN(cookie, ";", 2)[0], "fb_share=")
	if val == "" {
		t.Fatal("no unlock cookie")
	}
	if w := f.do("DELETE", "/api/shares/"+strconv.FormatInt(a.ID, 10), nil); w.Code != 204 {
		t.Fatalf("delete %d", w.Code)
	}
	b := decode[created](t, f.do("POST", "/api/shares", body(`{"vol":"v","path":"p","mode":"read","password":"second pw"}`)))
	if b.ID == a.ID {
		t.Errorf("share id %d reused", b.ID)
	}
	if c, _, _ := anon(f, "GET", "/s/"+b.Token+"/ls", "", "Cookie", "fb_share="+val); c != 401 {
		t.Fatalf("old unlock cookie opened the new share: %d", c)
	}
}

func TestShareUploadRelativePath(t *testing.T) {
	f := newTestApp(t)
	f.write(t, "inbox/.keep", "")
	tok := mkShare(t, f, `{"vol":"v","path":"inbox","mode":"upload"}`)
	up := "/s/" + tok + "/upload/"
	md := func(rp string) string { return "filename " + b64("x.txt") + ",relativePath " + b64(rp) }
	w := f.do("POST", up, nil, "X-No-Auth", "1", "Tus-Resumable", "1.0.0", "Upload-Length", "2", "Upload-Metadata", md("sub/deep/x.txt"))
	if w.Code != 201 {
		t.Fatalf("create %d", w.Code)
	}
	f.do("PATCH", w.Header().Get("Location"), strings.NewReader("ok"), "X-No-Auth", "1", "Tus-Resumable", "1.0.0",
		"Upload-Offset", "0", "Content-Type", "application/offset+octet-stream")
	if b, _ := os.ReadFile(filepath.Join(f.Dir, "inbox/sub/deep/x.txt")); string(b) != "ok" {
		t.Fatalf("stored %q", b)
	}
	for _, rp := range []string{"../x.txt", "sub/../../x.txt", "sub/.trash/x.txt", ".filebox/x.txt", "sub/.filebox/x.txt"} {
		if c, _, _ := anon(f, "POST", up, "", "Tus-Resumable", "1.0.0", "Upload-Length", "0", "Upload-Metadata", md(rp)); c != 400 {
			t.Errorf("%s → %d", rp, c)
		}
	}
}

func TestShareLockedInfoHidesName(t *testing.T) {
	f := newTestApp(t)
	f.write(t, "hidden-name.txt", "x")
	tok := mkShare(t, f, `{"vol":"v","path":"hidden-name.txt","mode":"read","password":"open sesame"}`)
	c, b, _ := anon(f, "GET", "/s/"+tok+"/info", "")
	var got map[string]any
	json.Unmarshal([]byte(b), &got)
	if c != 200 || len(got) != 2 || got["locked"] != true || got["mode"] != "read" {
		t.Fatalf("locked info %d %s", c, b)
	}
}

func TestShareUnlockAndLoginLimitsSeparate(t *testing.T) {
	setup := func() (unlock, login func(ip, pw string) int) {
		f := newTestApp(t)
		f.write(t, "p/a.txt", "x")
		tok := mkShare(t, f, `{"vol":"v","path":"p","mode":"read","password":"open sesame"}`)
		unlock = func(ip, pw string) int {
			return f.pkFrom(ip, "POST", "/s/"+tok+"/unlock", map[string]string{"password": pw}, nil).Code
		}
		login = func(ip, pw string) int {
			return f.pkFrom(ip, "POST", "/api/login", map[string]string{"name": "admin", "password": pw}, nil).Code
		}
		return
	}
	flood := func(try func(ip, pw string) int) {
		for i := range 30 {
			try("10.0.0."+strconv.Itoa(i/5), "wrong")
		}
	}
	unlock, login := setup()
	flood(unlock)
	if c := unlock("10.0.1.1", "open sesame"); c != 429 {
		t.Fatalf("share budget not enforced %d", c)
	}
	if c := login("10.0.0.0", "pw-pw-pw-pw"); c != 204 {
		t.Fatalf("admin login after bad share unlocks %d", c)
	}
	unlock, login = setup()
	flood(login)
	if c := login("10.0.1.1", "pw-pw-pw-pw"); c != 429 {
		t.Fatalf("login budget not enforced %d", c)
	}
	if c := unlock("10.0.0.0", "open sesame"); c != 204 {
		t.Fatalf("share unlock after bad logins %d", c)
	}
}

func TestShareThumbStaysInShare(t *testing.T) {
	f := newTestApp(t)
	fake := filepath.Join(t.TempDir(), "ffmpeg")
	os.WriteFile(fake, []byte("#!/bin/sh\nfor a; do last=$a; done\ncat <&3 > \"$last\"\n"), 0o755)
	f.App.Thumbs.FFmpeg = fake
	f.write(t, "pub/ok.jpg", "shared")
	f.write(t, "secret.jpg", "private")
	os.Symlink("../secret.jpg", filepath.Join(f.Dir, "pub/link.jpg"))
	tok := mkShare(t, f, `{"vol":"v","path":"pub","mode":"read"}`)
	if c, b, _ := anon(f, "GET", "/s/"+tok+"/thumb/ok.jpg", ""); c != 200 || b != "shared" {
		t.Fatalf("thumb inside the share %d %q", c, b)
	}
	if c, _, _ := anon(f, "GET", "/s/"+tok+"/raw/link.jpg", ""); c == 200 {
		t.Fatal("raw followed a link out of the share")
	}
	if c, b, _ := anon(f, "GET", "/s/"+tok+"/thumb/link.jpg", ""); c == 200 || strings.Contains(b, "private") {
		t.Fatalf("thumb followed a link out of the share: %d %q", c, b)
	}
	if w := f.do("GET", "/thumb/v/pub/link.jpg", nil); w.Code != 200 || w.Body.String() != "private" {
		t.Fatalf("owner thumb of a link inside the volume %d", w.Code)
	}
	file := mkShare(t, f, `{"vol":"v","path":"pub/ok.jpg","mode":"read"}`)
	if c, b, _ := anon(f, "GET", "/s/"+file+"/thumb/", ""); c != 200 || b != "shared" {
		t.Fatalf("file share thumb %d %q", c, b)
	}
}

func TestShareUploadStaysInShare(t *testing.T) {
	f := newTestApp(t)
	f.write(t, "pub/keep.txt", "k")
	os.Mkdir(filepath.Join(f.Dir, "private"), 0o755)
	os.Symlink("../private", filepath.Join(f.Dir, "pub/inbox"))
	for _, mode := range []string{"upload", "drop"} {
		tok := mkShare(t, f, `{"vol":"v","path":"pub","mode":"`+mode+`"}`)
		up := "/s/" + tok + "/upload/"
		create := func(rel string) (int, string) {
			w := f.do("POST", up, body(""), "X-No-Auth", "1", "Tus-Resumable", "1.0.0", "Upload-Length", "1",
				"Upload-Metadata", "filename "+b64(path.Base(rel))+",relativePath "+b64(rel))
			return w.Code, w.Header().Get("Location")
		}
		patch := func(loc string) int {
			return f.do("PATCH", loc, strings.NewReader("x"), "X-No-Auth", "1", "Tus-Resumable", "1.0.0",
				"Upload-Offset", "0", "Content-Type", "application/offset+octet-stream").Code
		}
		if c, _ := create("inbox/x.txt"); c != 400 {
			t.Fatalf("%s: upload through a link out of the share %d", mode, c)
		}
		if c, loc := create("sub/ok-" + mode + ".txt"); c != 201 || patch(loc) != 204 {
			t.Fatalf("%s: normal upload %d", mode, c)
		}
		if b, _ := os.ReadFile(filepath.Join(f.Dir, "pub/sub/ok-"+mode+".txt")); string(b) != "x" {
			t.Fatalf("%s: normal upload landed wrong: %q", mode, b)
		}
		os.Mkdir(filepath.Join(f.Dir, "pub/later"), 0o755)
		c, loc := create("later/y-" + mode + ".txt")
		if c != 201 {
			t.Fatalf("%s: create %d", mode, c)
		}
		os.Remove(filepath.Join(f.Dir, "pub/later"))
		os.Symlink("../private", filepath.Join(f.Dir, "pub/later"))
		if c := patch(loc); c == 204 {
			t.Fatalf("%s: finish wrote through a link swapped in after create", mode)
		}
		os.Remove(filepath.Join(f.Dir, "pub/later"))
	}
	if es, _ := os.ReadDir(filepath.Join(f.Dir, "private")); len(es) != 0 {
		t.Fatalf("files written outside the share: %v", es)
	}
}

func TestShareDedupe(t *testing.T) {
	f := newTestApp(t)
	f.write(t, "d/a.txt", "x")
	first := mkShare(t, f, `{"vol":"v","path":"d","mode":"read","expires_in":604800}`)
	w := f.do("POST", "/api/shares", body(`{"vol":"v","path":"d","mode":"read","expires_in":604800}`))
	if got := decode[struct{ Token string }](t, w).Token; w.Code != 200 || got != first {
		t.Fatalf("identical share %d %s", w.Code, got)
	}
	for _, js := range []string{
		`{"vol":"v","path":"d","mode":"read","expires_in":86400}`,
		`{"vol":"v","path":"d","mode":"upload","expires_in":604800}`,
		`{"vol":"v","path":"d","mode":"read","expires_in":604800,"password":"pw"}`,
		`{"vol":"v","path":"d","mode":"read","expires_in":604800,"note":"hi"}`,
	} {
		if tok := mkShare(t, f, js); tok == first {
			t.Fatalf("%s reused the existing link", js)
		}
	}
	f.App.DB.Exec(`UPDATE shares SET created_at = created_at - 3600, expires_at = expires_at - 3600 WHERE token = ?`, first)
	if tok := mkShare(t, f, `{"vol":"v","path":"d","mode":"read","expires_in":604800}`); tok == first {
		t.Fatal("reused a link with an hour less than requested")
	}
	long := mkShare(t, f, `{"vol":"v","path":"d","mode":"upload","expires_in":2592000}`)
	if tok := mkShare(t, f, `{"vol":"v","path":"d","mode":"upload","expires_in":86400}`); tok == long {
		t.Fatal("reused a 30-day link for a 1-day request")
	}
	forever := mkShare(t, f, `{"vol":"v","path":"d","mode":"drop"}`)
	if w := f.do("POST", "/api/shares", body(`{"vol":"v","path":"d","mode":"drop"}`)); !strings.Contains(w.Body.String(), forever) {
		t.Fatalf("permanent link not reused %s", w.Body)
	}
	pw := mkShare(t, f, `{"vol":"v","path":"d","mode":"drop","password":"pw"}`)
	if w := f.do("POST", "/api/shares", body(`{"vol":"v","path":"d","mode":"drop"}`)); strings.Contains(w.Body.String(), pw) {
		t.Fatalf("password share reused %d %s", w.Code, w.Body)
	}
}

func shareID(t *testing.T, f *fixture, tok string) string {
	t.Helper()
	list := decode[struct {
		Shares []struct {
			ID    int64
			Token string
		}
	}](t, f.do("GET", "/api/shares", nil)).Shares
	for _, s := range list {
		if s.Token == tok {
			return strconv.FormatInt(s.ID, 10)
		}
	}
	t.Fatalf("share %s not listed", tok)
	return ""
}

func TestShareEdit(t *testing.T) {
	f := newTestApp(t)
	f.write(t, "d/a.txt", "x")
	f.write(t, "one.txt", "x")
	tok := mkShare(t, f, `{"vol":"v","path":"d","mode":"read","password":"old"}`)
	id := shareID(t, f, tok)
	_, _, cookie := anon(f, "POST", "/s/"+tok+"/unlock", `{"password":"old"}`)
	jar := strings.SplitN(cookie, ";", 2)[0]
	if c, _, _ := anon(f, "GET", "/s/"+tok+"/ls", "", "Cookie", jar); c != 200 {
		t.Fatalf("unlocked ls %d", c)
	}
	if w := f.do("PATCH", "/api/shares/"+id, body(`{"note":"from me","mode":"upload","expires_in":3600,"max_upload":10}`)); w.Code != 204 {
		t.Fatalf("edit %d %s", w.Code, w.Body)
	}
	if c, _, _ := anon(f, "GET", "/s/"+tok+"/ls", "", "Cookie", jar); c != 200 {
		t.Fatalf("session lost without a password change %d", c)
	}
	if w := f.do("PATCH", "/api/shares/"+id, body(`{"password":""}`)); w.Code != 204 {
		t.Fatalf("remove password %d", w.Code)
	}
	var n int
	f.App.DB.QueryRow(`SELECT count(*) FROM tokens WHERE kind = 'share' AND scope = ?`, id).Scan(&n)
	if n != 0 {
		t.Fatalf("%d share sessions survived the password removal", n)
	}
	c, b, _ := anon(f, "GET", "/s/"+tok+"/info", "")
	if c != 200 || !strings.Contains(b, `"locked":false`) || !strings.Contains(b, `"note":"from me"`) || !strings.Contains(b, `"mode":"upload"`) ||
		!strings.Contains(b, `"max_upload":10`) {
		t.Fatalf("info after edit %d %s", c, b)
	}
	if w := f.do("PATCH", "/api/shares/"+id, body(`{"password":"new"}`)); w.Code != 204 {
		t.Fatalf("set password %d", w.Code)
	}
	if c, b, _ := anon(f, "GET", "/s/"+tok+"/info", ""); !strings.Contains(b, `"locked":true`) {
		t.Fatalf("new password not applied %d %s", c, b)
	}
	file := mkShare(t, f, `{"vol":"v","path":"one.txt","mode":"read"}`)
	if w := f.do("PATCH", "/api/shares/"+shareID(t, f, file), body(`{"mode":"drop"}`)); w.Code != 400 {
		t.Fatalf("drop mode on a file %d", w.Code)
	}
	for _, js := range []string{`{"mode":"admin"}`, `{"expires_in":-1}`, `{"max_upload":-5}`} {
		if w := f.do("PATCH", "/api/shares/"+id, body(js)); w.Code != 400 {
			t.Fatalf("%s accepted %d", js, w.Code)
		}
	}
	if w := f.do("PATCH", "/api/shares/999", body(`{"note":"x"}`)); w.Code != 404 {
		t.Fatalf("missing share %d", w.Code)
	}
}

func TestShareUploadLimit(t *testing.T) {
	f := newTestApp(t)
	f.write(t, "in/.keep", "")
	for _, mode := range []string{"upload", "drop"} {
		tok := mkShare(t, f, `{"vol":"v","path":"in","mode":"`+mode+`","max_upload":5}`)
		up := "/s/" + tok + "/upload/"
		if c, _, _ := anon(f, "POST", up, "", "Tus-Resumable", "1.0.0", "Upload-Length", "6", "Upload-Metadata", "filename "+b64("big")); c != 413 {
			t.Fatalf("%s: oversized upload %d", mode, c)
		}
		if c, _, _ := anon(f, "POST", up, "", "Tus-Resumable", "1.0.0", "Upload-Defer-Length", "1", "Upload-Metadata", "filename "+b64("big")); c < 400 {
			t.Fatalf("%s: deferred length upload %d", mode, c)
		}
		if c, _, _ := anon(f, "POST", up, "", "Tus-Resumable", "1.0.0", "Upload-Length", "5", "Upload-Metadata", "filename "+b64("ok")); c != 201 {
			t.Fatalf("%s: upload at the limit %d", mode, c)
		}
	}
}

func TestShareActivity(t *testing.T) {
	f := newTestApp(t)
	f.write(t, "d/a.txt", "hello")
	f.write(t, "in/.keep", "")
	tok := mkShare(t, f, `{"vol":"v","path":"d","mode":"read"}`)
	drop := mkShare(t, f, `{"vol":"v","path":"in","mode":"drop"}`)
	ip := func(a string) []string { return []string{"X-Remote-Addr", a + ":1234"} }
	for range 2 {
		anon(f, "GET", "/s/"+tok+"/info", "", ip("198.51.100.1")...)
	}
	anon(f, "GET", "/s/"+tok+"/info", "", ip("198.51.100.2")...)
	anon(f, "GET", "/s/"+tok+"/raw/a.txt?dl", "", ip("198.51.100.1")...)
	anon(f, "GET", "/s/"+tok+"/raw/a.txt", "", ip("198.51.100.1")...)
	anon(f, "GET", "/s/"+tok+"/raw/a.txt?dl", "", append(ip("198.51.100.3"), "Range", "bytes=0-0")...)
	anon(f, "GET", "/s/"+tok+"/raw/a.txt?dl", "", append(ip("198.51.100.4"), "Range", "bytes=0-")...)
	anon(f, "GET", "/s/"+tok+"/zip", "", ip("198.51.100.1")...)
	w := f.do("POST", "/s/"+drop+"/upload/", nil, "X-No-Auth", "1", "Tus-Resumable", "1.0.0", "Upload-Length", "2",
		"Upload-Metadata", "filename "+b64("r.txt"))
	f.do("PATCH", w.Header().Get("Location"), strings.NewReader("hi"), "X-No-Auth", "1", "Tus-Resumable", "1.0.0",
		"Upload-Offset", "0", "Content-Type", "application/offset+octet-stream")
	f.do("PATCH", "/api/shares/"+shareID(t, f, tok), body(`{"note":"n"}`))
	f.do("POST", "/api/login", body(`{"name":"admin","password":"wrong"}`), "X-No-Auth", "1")
	f.App.DB.Flush()
	type ev struct {
		Kind, Name, Target, Visitor string
		Size                        int64
	}
	got := decode[struct {
		Events []ev
		More   bool
	}](t, f.do("GET", "/api/activity", nil))
	kinds := []string{}
	byKind := map[string]ev{}
	for _, e := range got.Events {
		kinds = append(kinds, e.Kind)
		byKind[e.Kind] = e
		if strings.Contains(e.Visitor, "198.51") {
			t.Fatalf("raw IP stored: %+v", e)
		}
	}
	slices.Sort(kinds)
	if want := "download download download login login_failed share_create share_create share_edit upload"; strings.Join(kinds, " ") != want {
		t.Fatalf("events %v", kinds)
	}
	if e := byKind["upload"]; e.Name != "r.txt" || e.Size != 2 || e.Target != "v:/in" {
		t.Fatalf("upload event %+v", e)
	}
	dls := decode[struct{ Events []ev }](t, f.do("GET", "/api/activity?kind=download&share="+shareID(t, f, tok), nil)).Events
	if len(dls) != 3 || dls[0].Target != "v:/d" || !slices.ContainsFunc(dls, func(e ev) bool { return e.Name == "a.txt" && e.Size == 5 && e.Visitor != "" }) {
		t.Fatalf("downloads %+v", dls)
	}
	var rows int
	f.App.DB.QueryRow(`SELECT count(*) FROM events WHERE kind = 'view'`).Scan(&rows)
	if rows != 0 {
		t.Fatalf("%d view rows", rows)
	}
	if w := f.do("GET", "/api/activity?kind=bogus", nil); w.Code != 400 {
		t.Fatalf("bogus kind %d", w.Code)
	}
	if b := f.do("GET", "/api/shares", nil).Body.String(); !strings.Contains(b, `"views":2`) {
		t.Fatalf("views counter %s", b)
	}
}

func TestShareUploadRecheckedAtFinish(t *testing.T) {
	f := newTestApp(t)
	f.write(t, "in/.keep", "")
	tok := mkShare(t, f, `{"vol":"v","path":"in","mode":"drop"}`)
	id := shareID(t, f, tok)
	start := func(name string) string {
		w := f.do("POST", "/s/"+tok+"/upload/", nil, "X-No-Auth", "1", "Tus-Resumable", "1.0.0", "Upload-Length", "20",
			"Upload-Metadata", "filename "+b64(name))
		if w.Code != 201 {
			t.Fatalf("create %d", w.Code)
		}
		return w.Header().Get("Location")
	}
	send := func(loc string) int {
		return f.do("PATCH", loc, strings.NewReader(strings.Repeat("x", 20)), "X-No-Auth", "1", "Tus-Resumable", "1.0.0",
			"Upload-Offset", "0", "Content-Type", "application/offset+octet-stream").Code
	}
	big := start("big.bin")
	f.do("PATCH", "/api/shares/"+id, body(`{"max_upload":5}`))
	if c := send(big); c != 413 {
		t.Fatalf("upload over the new limit finished with %d", c)
	}
	if _, err := os.Stat(filepath.Join(f.Dir, "in/big.bin")); err == nil {
		t.Fatal("oversized upload was stored")
	}
	if ents, _ := os.ReadDir(filepath.Join(f.Dir, ".filebox/uploads")); len(ents) != 0 {
		t.Fatalf("partial upload left behind: %v", ents)
	}
	f.do("PATCH", "/api/shares/"+id, body(`{"max_upload":0}`))
	ok := start("ok.bin")
	if c := send(ok); c != 204 {
		t.Fatalf("upload under no limit %d", c)
	}
	allow := f.App.Uploads.Allow
	if allow("share:"+id, 1) != nil || allow("user:1", 1<<40) != nil {
		t.Fatal("allowed uploads refused")
	}
	f.do("PATCH", "/api/shares/"+id, body(`{"mode":"read"}`))
	if allow("share:"+id, 1) == nil {
		t.Fatal("finish allowed after the share became read-only")
	}
	f.do("DELETE", "/api/shares/"+id, nil)
	if allow("share:"+id, 1) == nil {
		t.Fatal("finish allowed after the share was deleted")
	}
}
