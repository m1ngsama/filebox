package api

import (
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestSidecarsMatchTheirVideo(t *testing.T) {
	got := sidecars([]string{"a.mp4", "a.srt", "a.zh.vtt", "a.en.forced.srt", "b.srt", "notes.txt", "a.b.mkv", "a.b.srt", ".srt"})
	want := []string{"a.srt", "a.zh.vtt", "a.en.forced.srt", "a.b.srt"}
	if !slices.Equal(got, want) {
		t.Fatalf("%v", got)
	}
}

func TestListHidesJunk(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"a.mp4", "._a.mp4", ".DS_Store", "Thumbs.db", ".hidden"} {
		os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o644)
	}
	os.Mkdir(filepath.Join(dir, "sub"), 0o755)
	os.WriteFile(filepath.Join(dir, "sub", "._b"), nil, 0o644)
	os.WriteFile(filepath.Join(dir, "sub", "b"), nil, 0o644)
	root, _ := os.OpenRoot(dir)
	defer root.Close()
	w := httptest.NewRecorder()
	WriteList(w, httptest.NewRequest("GET", "/", nil), root, ".", nil)
	var got struct{ Entries []Entry }
	json.Unmarshal(w.Body.Bytes(), &got)
	var names []string
	for _, e := range got.Entries {
		names = append(names, e.Name)
		if e.Name == "sub" && (e.Items == nil || *e.Items != 1) {
			t.Fatalf("sub counts %v", e.Items)
		}
	}
	if !slices.Equal(names, []string{"sub", ".hidden", "a.mp4"}) {
		t.Fatalf("%v", names)
	}
}

func TestLyricsFoldUnderTheirSong(t *testing.T) {
	got := sidecars([]string{"晴天.mp3", "晴天.lrc", "orphan.lrc", "clip.mp4", "clip.lrc", "clip.zh.srt"})
	if want := []string{"晴天.lrc", "clip.zh.srt"}; !slices.Equal(got, want) {
		t.Fatalf("sidecars %v, want %v", got, want)
	}
}
