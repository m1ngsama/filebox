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
