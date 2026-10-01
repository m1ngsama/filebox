package api

import (
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
