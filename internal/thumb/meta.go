package thumb

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/m1ngsama/filebox/internal/httpx"
)

type Meta struct {
	Width    int     `json:"width,omitempty"`
	Height   int     `json:"height,omitempty"`
	Duration float64 `json:"duration,omitempty"`
	Camera   string  `json:"camera,omitempty"`
	Lens     string  `json:"lens,omitempty"`
	Focal    string  `json:"focal,omitempty"`
	Aperture string  `json:"aperture,omitempty"`
	Shutter  string  `json:"shutter,omitempty"`
	ISO      string  `json:"iso,omitempty"`
	Taken    string  `json:"taken,omitempty"`
	GPS      string  `json:"gps,omitempty"`
}

type probed struct {
	Frames  []dims `json:"frames"`
	Streams []dims `json:"streams"`
	Format  struct {
		Duration string            `json:"duration"`
		Tags     map[string]string `json:"tags"`
	} `json:"format"`
}

type dims struct {
	Width  int               `json:"width"`
	Height int               `json:"height"`
	Tags   map[string]string `json:"tags"`
}

func (s *Service) ServeMeta(w http.ResponseWriter, r *http.Request, root *os.Root, rel string) {
	f, err := root.Open(rel)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || st.IsDir() {
		httpx.Fail(w, 400, "not a file")
		return
	}
	etag := fmt.Sprintf(`"m%x-%x"`, st.Size(), st.ModTime().UnixNano())
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "private, no-cache")
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	m := Meta{}
	if kind := Kind(rel); kind != "" && kind != "pdf" && s.FFprobe != "" {
		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		defer cancel()
		m = s.probe(ctx, f, kind != "video")
	}
	httpx.JSON(w, 200, m)
}

func (s *Service) probe(ctx context.Context, f *os.File, image bool) Meta {
	entries := "stream=width,height:format=duration:format_tags"
	args := []string{"-v", "error", "-protocol_whitelist", "file", "-select_streams", "v:0"}
	if image {
		entries += ":frame=width,height:frame_tags"
		args = append(args, "-read_intervals", "%+#1")
	}
	cmd := exec.CommandContext(ctx, s.FFprobe, append(args, "-show_entries", entries, "-of", "json", "/dev/fd/3")...)
	cmd.ExtraFiles = []*os.File{f}
	out, err := cmd.Output()
	var p probed
	if err != nil || json.Unmarshal(out, &p) != nil {
		return Meta{}
	}
	return p.meta()
}

func (p probed) meta() Meta {
	var m Meta
	for _, d := range append(p.Frames, p.Streams...) {
		if m.Width == 0 && d.Width > 0 {
			m.Width, m.Height = d.Width, d.Height
		}
	}
	m.Duration, _ = strconv.ParseFloat(p.Format.Duration, 64)
	if len(p.Frames) == 0 || len(p.Frames[0].Tags) == 0 {
		t := norm(p.Format.Tags)
		if c, err := time.Parse(time.RFC3339Nano, t["creation_time"]); err == nil {
			m.Taken = c.UTC().Format("2006-01-02 15:04:05") + " UTC"
		}
		m.GPS = iso6709(cmpOr(t["com.apple.quicktime.location.ISO6709"], t["location"]))
		m.Camera = camera(t["com.apple.quicktime.make"], t["com.apple.quicktime.model"])
		if m.Duration < 0.01 {
			m.Duration = 0
		}
		return m
	}
	m.Duration = 0
	t := norm(p.Frames[0].Tags)
	m.Camera = camera(t["Make"], t["Model"])
	m.Lens = cmpOr(t["LensModel"], t["0xA434"])
	if v, ok := rational(t["FocalLength"]); ok {
		m.Focal = trim(v) + " mm"
	}
	if v, ok := rational(t["FNumber"]); ok {
		m.Aperture = "f/" + trim(v)
	}
	if v, ok := rational(t["ExposureTime"]); ok && v > 0 {
		if v < 1 {
			m.Shutter = "1/" + strconv.Itoa(int(math.Round(1/v))) + " s"
		} else {
			m.Shutter = trim(v) + " s"
		}
	}
	m.ISO = strings.TrimSpace(cmpOr(t["ISOSpeedRatings"], t["PhotographicSensitivity"]))
	if d := cmpOr(t["DateTimeOriginal"], t["DateTime"]); len(d) >= 19 {
		m.Taken = strings.Replace(d[:10], ":", "-", 2) + d[10:19]
	}
	lat, okLat := dms(t["GPSLatitude"], t["GPSLatitudeRef"] == "S")
	lon, okLon := dms(t["GPSLongitude"], t["GPSLongitudeRef"] == "W")
	if okLat && okLon {
		m.GPS = fmt.Sprintf("%.6f, %.6f", lat, lon)
	}
	return m
}

func norm(tags map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range tags {
		if i := strings.LastIndexByte(k, '/'); i >= 0 {
			k = k[i+1:]
		}
		out[k] = strings.TrimSpace(v)
	}
	return out
}

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func camera(mk, model string) string {
	mk, model = strings.TrimSpace(mk), strings.TrimSpace(model)
	if mk != "" && strings.HasPrefix(strings.ToLower(model), strings.ToLower(mk)) {
		return model
	}
	return strings.TrimSpace(mk + " " + model)
}

func rational(s string) (float64, bool) {
	a, b, ok := strings.Cut(strings.TrimSpace(s), ":")
	if !ok {
		a, b, ok = strings.Cut(strings.TrimSpace(s), "/")
	}
	n, err := strconv.ParseFloat(strings.TrimSpace(a), 64)
	if err != nil {
		return 0, false
	}
	if !ok {
		return n, true
	}
	d, err := strconv.ParseFloat(strings.TrimSpace(b), 64)
	if err != nil || d == 0 {
		return 0, false
	}
	return n / d, true
}

func trim(v float64) string { return strconv.FormatFloat(math.Round(v*100)/100, 'f', -1, 64) }

func dms(s string, neg bool) (float64, bool) {
	parts := strings.Split(s, ",")
	if len(parts) != 3 {
		return 0, false
	}
	v := 0.0
	for i, p := range parts {
		x, ok := rational(p)
		if !ok {
			return 0, false
		}
		v += x / math.Pow(60, float64(i))
	}
	if neg {
		v = -v
	}
	return v, true
}

var iso6709re = regexp.MustCompile(`^([+-]\d+(?:\.\d+)?)([+-]\d+(?:\.\d+)?)`)

func iso6709(s string) string {
	m := iso6709re.FindStringSubmatch(s)
	if m == nil {
		return ""
	}
	lat, _ := strconv.ParseFloat(m[1], 64)
	lon, _ := strconv.ParseFloat(m[2], 64)
	return fmt.Sprintf("%.6f, %.6f", lat, lon)
}
