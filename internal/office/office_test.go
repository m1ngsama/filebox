package office

import (
	"bytes"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestServe(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "memo.rtf"), []byte(`{\rtf1\ansi Quarterly memo\par}`), 0o644)
	os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("x"), 0o644)
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	var none *Converter
	if w := serve(none, root, "memo.rtf"); w.Code != 501 {
		t.Fatalf("without LibreOffice %d", w.Code)
	}
	c := New(t.TempDir())
	if c == nil {
		t.Skip("LibreOffice not installed")
	}
	if w := serve(c, root, "notes.txt"); w.Code != 400 {
		t.Fatalf("plain text %d", w.Code)
	}
	w := serve(c, root, "memo.rtf")
	if w.Code != 200 || !bytes.HasPrefix(w.Body.Bytes(), []byte("%PDF")) || w.Header().Get("Content-Security-Policy") != "sandbox" {
		t.Fatalf("convert %d %q", w.Code, w.Body.String()[:min(w.Body.Len(), 200)])
	}
	if again := serve(c, root, "memo.rtf"); again.Code != 200 || again.Body.Len() != w.Body.Len() {
		t.Fatalf("cached %d", again.Code)
	}
}

func serve(c *Converter, root *os.Root, rel string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	c.Serve(w, httptest.NewRequest("GET", "/", nil), root, rel)
	return w
}
