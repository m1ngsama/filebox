package thumb

import (
	"encoding/json"
	"testing"
)

func TestMetaFromExif(t *testing.T) {
	var p probed
	json.Unmarshal([]byte(`{"frames":[{"width":4032,"height":3024,"tags":{
		"Make":"Apple","Model":"iPhone 6s","ExifIFD/ExposureTime":"      1:50     ","ExifIFD/FNumber":"     11:5      ",
		"ExifIFD/ISOSpeedRatings":"     32","ExifIFD/DateTimeOriginal":"2019:04:28 10:23:28","ExifIFD/FocalLength":"     83:20     ",
		"ExifIFD/0xA434":"iPhone 6s back camera 4.15mm f/2.2","GPSInfo/GPSLatitudeRef":"S",
		"GPSInfo/GPSLatitude":"     29:1      ,      49:1      ,     611:100    ","GPSInfo/GPSLongitudeRef":"E",
		"GPSInfo/GPSLongitude":"    121:1      ,      34:1      ,     730:100    "}}],"streams":[{"width":4032,"height":3024}],"format":{"duration":"0.04"}}`), &p)
	got := p.meta()
	want := Meta{Width: 4032, Height: 3024, Camera: "Apple iPhone 6s", Lens: "iPhone 6s back camera 4.15mm f/2.2", Focal: "4.15 mm",
		Aperture: "f/2.2", Shutter: "1/50 s", ISO: "32", Taken: "2019-04-28 10:23:28", GPS: "-29.818364, 121.568694"}
	if got != want {
		t.Fatalf("\n got %+v\nwant %+v", got, want)
	}
}

func TestMetaFromVideo(t *testing.T) {
	var p probed
	json.Unmarshal([]byte(`{"streams":[{"width":1920,"height":1080}],"format":{"duration":"12.5","tags":{
		"creation_time":"2024-05-01T08:00:00.000000Z","com.apple.quicktime.location.ISO6709":"+29.8350+121.5685+144.921/",
		"com.apple.quicktime.make":"Apple","com.apple.quicktime.model":"iPhone 15"}}}`), &p)
	got := p.meta()
	want := Meta{Width: 1920, Height: 1080, Duration: 12.5, Camera: "Apple iPhone 15", Taken: "2024-05-01 08:00:00 UTC", GPS: "29.835000, 121.568500"}
	if got != want {
		t.Fatalf("\n got %+v\nwant %+v", got, want)
	}
}
