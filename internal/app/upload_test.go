package app

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func b64(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }

func TestUploadRoute(t *testing.T) {
	f := newTestApp(t)
	os.Mkdir(filepath.Join(f.Dir, "in"), 0o755)
	md := "vol " + b64("v") + ",dir " + b64("in") + ",filename " + b64("x.txt") + ",relativePath " + b64("sub/x.txt")
	w := f.do("POST", "/upload/", nil, "Tus-Resumable", "1.0.0", "Upload-Length", "3", "Upload-Metadata", md)
	if w.Code != 201 {
		t.Fatalf("create %d %s", w.Code, w.Body)
	}
	w = f.do("PATCH", w.Header().Get("Location"), strings.NewReader("abc"), "Tus-Resumable", "1.0.0",
		"Upload-Offset", "0", "Content-Type", "application/offset+octet-stream")
	if w.Code != 204 {
		t.Fatalf("patch %d", w.Code)
	}
	if b, _ := os.ReadFile(filepath.Join(f.Dir, "in/sub/x.txt")); string(b) != "abc" {
		t.Fatalf("got %q", b)
	}
	w = f.do("POST", "/upload/", nil, "X-No-Auth", "1", "Tus-Resumable", "1.0.0", "Upload-Length", "3", "Upload-Metadata", md)
	if w.Code != 401 {
		t.Fatalf("anon %d", w.Code)
	}
	w = f.do("POST", "/upload/", nil, "X-No-Auth", "1", "Authorization", "Bearer "+f.Bearer,
		"Tus-Resumable", "1.0.0", "Upload-Length", "0", "Upload-Metadata", md)
	if w.Code != 201 {
		t.Fatalf("app token upload %d", w.Code)
	}
	ro, _ := f.App.Auth.NewAppToken(f.UserID, "ro", true)
	w = f.do("POST", "/upload/", nil, "X-No-Auth", "1", "Authorization", "Bearer "+ro,
		"Tus-Resumable", "1.0.0", "Upload-Length", "0", "Upload-Metadata", md)
	if w.Code != 401 {
		t.Fatalf("read-only token upload %d", w.Code)
	}
}

func TestUploadOverwriteIsForUsersOnly(t *testing.T) {
	f := newTestApp(t)
	os.WriteFile(filepath.Join(f.Dir, "r.txt"), []byte("old"), 0o644)
	md := "vol " + b64("v") + ",dir " + b64("/") + ",filename " + b64("r.txt") + ",overwrite " + b64("1")
	w := f.do("POST", "/upload/", nil, "Tus-Resumable", "1.0.0", "Upload-Length", "3", "Upload-Metadata", md)
	f.do("PATCH", w.Header().Get("Location"), strings.NewReader("new"), "Tus-Resumable", "1.0.0",
		"Upload-Offset", "0", "Content-Type", "application/offset+octet-stream")
	if b, _ := os.ReadFile(filepath.Join(f.Dir, "r.txt")); string(b) != "new" {
		t.Fatalf("got %q", b)
	}
}
