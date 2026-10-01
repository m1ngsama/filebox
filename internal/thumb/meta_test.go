package thumb

import (
	"encoding/json"
	"net/http/httptest"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestMetaFromExif(t *testing.T) {
	var p probed
	json.Unmarshal([]byte(`{"frames":[{"width":4032,"height":3024,"tags":{
		"Make":"Apple","Model":"iPhone 6s","ExifIFD/ExposureTime":"      1:50     ","ExifIFD/FNumber":"     11:5      ",
		"ExifIFD/ISOSpeedRatings":"     32","ExifIFD/DateTimeOriginal":"2019:04:28 10:23:28","ExifIFD/FocalLength":"     83:20     ",
		"ExifIFD/0xA434":"iPhone 6s back camera 4.15mm f/2.2","GPSInfo/GPSLatitudeRef":"S",
		"GPSInfo/GPSLatitude":"     29:1      ,      49:1      ,     611:100    ","GPSInfo/GPSLongitudeRef":"E",
		"GPSInfo/GPSLongitude":"    121:1      ,      34:1      ,     730:100    "}}],"streams":[{"width":4032,"height":3024}]}`), &p)
	got := p.meta()
	want := Meta{Width: 4032, Height: 3024, Camera: "Apple iPhone 6s", Lens: "iPhone 6s back camera 4.15mm f/2.2", Focal: "4.15 mm",
		Aperture: "f/2.2", Shutter: "1/50 s", ISO: "32", Taken: "2019-04-28 10:23:28", GPS: "-29.818364, 121.568694"}
	if got != want {
		t.Fatalf("\n got %+v\nwant %+v", got, want)
	}
}

func TestMetaFromVideoFormat(t *testing.T) {
	var p probed
	json.Unmarshal([]byte(`{"streams":[{"width":1920,"height":1080}],"format":{"duration":"95.480000","tags":{"creation_time":"2024-10-03T14:20:05.000000Z"}}}`), &p)
	if got := p.meta(); got != (Meta{Width: 1920, Height: 1080, Duration: 95.48, Taken: "2024-10-03 14:20:05"}) {
		t.Fatalf("%+v", got)
	}
}

func TestMetaIgnoresCoverArt(t *testing.T) {
	ff, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not installed")
	}
	s, v, dir := setup(t, ff)
	if s.FFprobe == "" {
		t.Skip("ffprobe not installed")
	}
	cover, song := filepath.Join(dir, "c.png"), filepath.Join(dir, "song.mp4")
	for _, args := range [][]string{
		{"-f", "lavfi", "-i", "color=red:s=64x64", "-frames:v", "1", cover},
		{"-f", "lavfi", "-i", "sine=d=1", "-i", cover, "-map", "0", "-map", "1", "-c:a", "aac", "-c:v", "png", "-disposition:v", "attached_pic", song},
	} {
		if out, err := exec.Command(ff, append([]string{"-v", "error", "-y"}, args...)...).CombinedOutput(); err != nil {
			t.Skipf("fixture: %v %s", err, out)
		}
	}
	w := httptest.NewRecorder()
	s.ServeMeta(w, httptest.NewRequest("GET", "/", nil), v.Root, "song.mp4")
	if w.Code != 200 || strings.Contains(w.Body.String(), "width") {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
}

func TestMetaFromAudioTags(t *testing.T) {
	var p probed
	json.Unmarshal([]byte(`{"format":{"duration":"201.5","tags":{"TITLE":"晴天","ARTIST":"周杰伦","ALBUM":"叶惠美"}}}`), &p)
	if got := p.meta(); got != (Meta{Duration: 201.5, Title: "晴天", Artist: "周杰伦", Album: "叶惠美"}) {
		t.Fatalf("%+v", got)
	}
}

func TestAudioCoverThumbnail(t *testing.T) {
	ff, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not installed")
	}
	s, v, dir := setup(t, ff)
	s.format = jpeg
	cover := filepath.Join(dir, "c.png")
	for _, args := range [][]string{
		{"-f", "lavfi", "-i", "color=red:s=300x300", "-frames:v", "1", cover},
		{"-f", "lavfi", "-i", "sine=d=1", "-i", cover, "-map", "0", "-map", "1", "-c:a", "libmp3lame", "-c:v", "png", "-disposition:v", "attached_pic", "-metadata", "title=Song", filepath.Join(dir, "song.mp3")},
		{"-f", "lavfi", "-i", "sine=d=1", "-c:a", "libmp3lame", filepath.Join(dir, "bare.mp3")},
	} {
		if out, err := exec.Command(ff, append([]string{"-v", "error", "-y"}, args...)...).CombinedOutput(); err != nil {
			t.Skipf("fixture: %v %s", err, out)
		}
	}
	if w := get(s, v, "song.mp3"); w.Code != 200 || w.Header().Get("Content-Type") != "image/jpeg" {
		t.Fatalf("cover: %d %q", w.Code, w.Header().Get("Content-Type"))
	}
	if w := get(s, v, "bare.mp3"); w.Code != 404 {
		t.Fatalf("no cover: %d", w.Code)
	}
	if s.FFprobe == "" {
		return
	}
	w := httptest.NewRecorder()
	s.ServeMeta(w, httptest.NewRequest("GET", "/", nil), v.Root, "song.mp3")
	if !strings.Contains(w.Body.String(), `"title":"Song"`) || strings.Contains(w.Body.String(), "width") {
		t.Fatalf("%d %s", w.Code, w.Body)
	}
}
