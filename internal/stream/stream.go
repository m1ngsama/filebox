// Package stream transcodes videos on demand into HLS so they play smoothly over slow links and in browsers that lack the source codec.
package stream

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/m1ngsama/filebox/internal/httpx"
)

const (
	segment = 4.0
	ahead   = 15
	idle    = time.Minute
)

var heights = map[string]int{"1080": 1080, "720": 720, "480": 480}
var rates = map[int]string{1080: "5M", 720: "2.5M", 480: "1M"}

type Service struct {
	FFmpeg, FFprobe, Device string
	Dir                     string

	mu       sync.Mutex
	sessions map[string]*session
	infos    map[string]info
	slots    int
}

type info struct {
	Duration float64
	Height   int
}

type session struct {
	dir     string
	start   int
	want    int
	cmd     *exec.Cmd
	exited  chan struct{}
	last    time.Time
	stopped bool
}

func New(ffmpeg, dir string) *Service {
	s := &Service{FFmpeg: ffmpeg, Dir: dir, sessions: map[string]*session{}, infos: map[string]info{}, slots: max(1, runtime.NumCPU()/2)}
	if ffmpeg != "" {
		if p := filepath.Join(filepath.Dir(ffmpeg), "ffprobe"); fileExists(p) {
			s.FFprobe = p
		}
	}
	return s
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// Probe picks the VAAPI encoder when a render node accepts a test encode, and the software encoder otherwise.
func (s *Service) Probe(ctx context.Context) {
	os.RemoveAll(s.Dir)
	if s.FFmpeg == "" || s.FFprobe == "" {
		slog.Info("stream: transcoding off, no ffmpeg and ffprobe")
		s.FFmpeg = ""
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	nodes, _ := filepath.Glob("/dev/dri/renderD*")
	for _, d := range nodes {
		err := exec.CommandContext(ctx, s.FFmpeg, "-v", "error", "-init_hw_device", "vaapi=va:"+d, "-filter_hw_device", "va",
			"-f", "lavfi", "-i", "testsrc2=s=320x240:d=0.2", "-vf", "format=nv12,hwupload", "-c:v", "h264_vaapi", "-f", "null", "-").Run()
		if err == nil {
			s.Device = d
			slog.Info("stream: hardware transcoding", "device", d)
			break
		}
	}
	if s.Device == "" {
		slog.Info("stream: software transcoding")
	}
	go s.reap()
}

func (s *Service) Enabled() bool { return s.FFmpeg != "" }

func key(fi fs.FileInfo, q int) string {
	if st, ok := fi.Sys().(*syscall.Stat_t); ok {
		return fmt.Sprintf("%x-%x-%x-%x-%d", st.Dev, st.Ino, fi.Size(), fi.ModTime().UnixNano(), q)
	}
	return fmt.Sprintf("%s-%x-%x-%d", fi.Name(), fi.Size(), fi.ModTime().UnixNano(), q)
}

func quality(r *http.Request) (int, bool) {
	h, ok := heights[r.URL.Query().Get("q")]
	return h, ok
}

func (s *Service) open(w http.ResponseWriter, r *http.Request, root *os.Root, rel string) (*os.File, fs.FileInfo, int, bool) {
	if !s.Enabled() {
		httpx.Fail(w, 404, "transcoding off")
		return nil, nil, 0, false
	}
	q, ok := quality(r)
	if !ok {
		httpx.Fail(w, 400, "bad quality")
		return nil, nil, 0, false
	}
	f, err := root.Open(rel)
	if err != nil {
		httpx.Error(w, err)
		return nil, nil, 0, false
	}
	fi, err := f.Stat()
	if err != nil || !fi.Mode().IsRegular() {
		f.Close()
		httpx.Fail(w, 400, "not a file")
		return nil, nil, 0, false
	}
	return f, fi, q, true
}

func (s *Service) info(ctx context.Context, f *os.File, fi fs.FileInfo) (info, error) {
	k := key(fi, 0)
	s.mu.Lock()
	in, ok := s.infos[k]
	s.mu.Unlock()
	if ok {
		return in, nil
	}
	cmd := exec.CommandContext(ctx, s.FFprobe, "-v", "error", "-protocol_whitelist", "file", "-select_streams", "V:0",
		"-show_entries", "stream=height:format=duration", "-of", "json", "/dev/fd/3")
	cmd.ExtraFiles = []*os.File{f}
	out, err := cmd.Output()
	var p struct {
		Streams []struct{ Height int }    `json:"streams"`
		Format  struct{ Duration string } `json:"format"`
	}
	if err != nil || json.Unmarshal(out, &p) != nil || len(p.Streams) == 0 {
		return info{}, errors.New("not a video")
	}
	d, _ := strconv.ParseFloat(p.Format.Duration, 64)
	if d <= 0 {
		return info{}, errors.New("unknown duration")
	}
	in = info{Duration: d, Height: p.Streams[0].Height}
	s.mu.Lock()
	s.infos[k] = in
	s.mu.Unlock()
	return in, nil
}

// Playlist lists every segment up front so the player can seek anywhere; segments are made when asked for.
func (s *Service) Playlist(w http.ResponseWriter, r *http.Request, root *os.Root, rel string) {
	f, fi, _, ok := s.open(w, r, root, rel)
	if !ok {
		return
	}
	defer f.Close()
	in, err := s.info(r.Context(), f, fi)
	if err != nil {
		httpx.Fail(w, 415, err.Error())
		return
	}
	q := r.URL.Query()
	var b strings.Builder
	fmt.Fprintf(&b, "#EXTM3U\n#EXT-X-VERSION:3\n#EXT-X-PLAYLIST-TYPE:VOD\n#EXT-X-TARGETDURATION:%d\n#EXT-X-MEDIA-SEQUENCE:0\n#EXT-X-INDEPENDENT-SEGMENTS\n", int(math.Ceil(segment)))
	n := int(math.Ceil(in.Duration / segment))
	for i := range n {
		d := min(segment, in.Duration-float64(i)*segment)
		q.Set("n", strconv.Itoa(i))
		fmt.Fprintf(&b, "#EXTINF:%.3f,\nseg?%s\n", d, q.Encode())
	}
	b.WriteString("#EXT-X-ENDLIST\n")
	h := w.Header()
	h.Set("Content-Type", "application/vnd.apple.mpegurl")
	h.Set("Cache-Control", "private, no-cache")
	w.Write([]byte(b.String()))
}

func (s *Service) Segment(w http.ResponseWriter, r *http.Request, root *os.Root, rel string) {
	f, fi, q, ok := s.open(w, r, root, rel)
	if !ok {
		return
	}
	defer f.Close()
	n, err := strconv.Atoi(r.URL.Query().Get("n"))
	in, ierr := s.info(r.Context(), f, fi)
	if err != nil || ierr != nil || n < 0 || float64(n)*segment >= in.Duration {
		httpx.Fail(w, 400, "bad segment")
		return
	}
	path, err := s.segment(r.Context(), f, fi, in, q, n)
	if err != nil {
		if !errors.Is(err, context.Canceled) {
			slog.Warn("stream segment", "n", n, "err", err)
		}
		httpx.Fail(w, 503, "transcode failed")
		return
	}
	h := w.Header()
	h.Set("Content-Type", "video/mp2t")
	h.Set("Cache-Control", "private, max-age=3600")
	http.ServeFile(w, r, path)
}

func segName(dir string, n int) string { return filepath.Join(dir, fmt.Sprintf("seg%d.ts", n)) }

func (s *Service) segment(ctx context.Context, f *os.File, fi fs.FileInfo, in info, q, n int) (string, error) {
	k := key(fi, q)
	deadline := time.Now().Add(45 * time.Second)
	restarted := false
	for {
		s.mu.Lock()
		ss := s.sessions[k]
		if ss != nil {
			ss.last = time.Now()
			if p := segName(ss.dir, n); fileExists(p) {
				ss.want = max(ss.want, n)
				s.mu.Unlock()
				return p, nil
			}
		}
		live := ss != nil && !closed(ss.exited)
		if !live || n < ss.start || n > made(ss)+ahead {
			if restarted && (ss == nil || !live) {
				s.mu.Unlock()
				return "", errors.New("ffmpeg stopped before the segment was ready")
			}
			if _, err := s.restart(k, f, in, q, n); err != nil {
				s.mu.Unlock()
				return "", err
			}
			restarted = true
		} else {
			ss.want = max(ss.want, n)
			if ss.stopped {
				syscall.Kill(ss.cmd.Process.Pid, syscall.SIGCONT)
				ss.stopped = false
			}
		}
		s.mu.Unlock()
		if time.Now().After(deadline) {
			return "", errors.New("segment timed out")
		}
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
}

func made(ss *session) int {
	n := ss.start
	for fileExists(segName(ss.dir, n)) {
		n++
	}
	return n
}

func closed(c chan struct{}) bool {
	select {
	case <-c:
		return true
	default:
		return false
	}
}

// restart is called with s.mu held.
func (s *Service) restart(k string, f *os.File, in info, q, n int) (*session, error) {
	if old := s.sessions[k]; old != nil {
		s.kill(old)
		delete(s.sessions, k)
	}
	for len(s.sessions) >= s.slots {
		var lru string
		for kk, x := range s.sessions {
			if lru == "" || x.last.Before(s.sessions[lru].last) {
				lru = kk
			}
		}
		s.kill(s.sessions[lru])
		delete(s.sessions, lru)
	}
	dir, err := os.MkdirTemp(ensure(s.Dir), "s")
	if err != nil {
		return nil, err
	}
	h := min(q, in.Height)
	if h <= 0 {
		h = q
	}
	h -= h % 2
	start := float64(n) * segment
	args := []string{"-nostdin", "-v", "error"}
	video := []string{"-vf", fmt.Sprintf("scale=-2:%d,format=yuv420p", h), "-c:v", "libx264", "-preset", "veryfast", "-profile:v", "high"}
	if s.Device != "" {
		// Frames arrive in GPU memory when the hardware decodes the source and in system memory when it cannot; this chain takes both.
		args = append(args, "-init_hw_device", "vaapi=va:"+s.Device, "-filter_hw_device", "va", "-hwaccel", "vaapi", "-hwaccel_device", "va", "-hwaccel_output_format", "vaapi")
		video = []string{"-vf", fmt.Sprintf("format=nv12|vaapi,hwupload,scale_vaapi=w=-2:h=%d:format=nv12", h), "-c:v", "h264_vaapi"}
	}
	args = append(args, "-ss", strconv.FormatFloat(start, 'f', 3, 64), "-protocol_whitelist", "file", "-i", "/dev/fd/3",
		"-map", "0:V:0", "-map", "0:a:0?")
	args = append(args, video...)
	args = append(args, "-b:v", rates[q], "-maxrate", rates[q], "-bufsize", rates[q],
		"-force_key_frames", fmt.Sprintf("expr:gte(t,n_forced*%g)", segment), "-sc_threshold", "0",
		"-c:a", "aac", "-ac", "2", "-b:a", "160k",
		"-output_ts_offset", strconv.FormatFloat(start, 'f', 3, 64),
		"-f", "hls", "-hls_time", strconv.FormatFloat(segment, 'f', 0, 64), "-hls_list_size", "0", "-hls_segment_type", "mpegts",
		"-hls_flags", "temp_file+independent_segments", "-start_number", strconv.Itoa(n),
		"-hls_segment_filename", filepath.Join(dir, "seg%d.ts"), filepath.Join(dir, "index.m3u8"))
	cmd := exec.Command(s.FFmpeg, args...)
	cmd.ExtraFiles = []*os.File{f}
	if err := cmd.Start(); err != nil {
		os.RemoveAll(dir)
		return nil, err
	}
	ss := &session{dir: dir, start: n, want: n, cmd: cmd, exited: make(chan struct{}), last: time.Now()}
	go func() {
		cmd.Wait()
		close(ss.exited)
	}()
	s.sessions[k] = ss
	return ss, nil
}

func ensure(dir string) string {
	os.MkdirAll(dir, 0o755)
	return dir
}

func (s *Service) kill(ss *session) {
	if ss.cmd.Process != nil && !closed(ss.exited) {
		ss.cmd.Process.Kill()
		if ss.stopped {
			syscall.Kill(ss.cmd.Process.Pid, syscall.SIGCONT)
		}
	}
	go func() {
		<-ss.exited
		os.RemoveAll(ss.dir)
	}()
}

// reap pauses encoders that ran far ahead of the player, prunes played segments and ends idle sessions.
func (s *Service) reap() {
	for range time.Tick(500 * time.Millisecond) {
		s.mu.Lock()
		for k, ss := range s.sessions {
			if time.Since(ss.last) > idle {
				s.kill(ss)
				delete(s.sessions, k)
				continue
			}
			if closed(ss.exited) {
				continue
			}
			if made(ss) > ss.want+ahead && !ss.stopped {
				syscall.Kill(ss.cmd.Process.Pid, syscall.SIGSTOP)
				ss.stopped = true
			}
			for i := ss.start; i < ss.want-ahead; i++ {
				os.Remove(segName(ss.dir, i))
			}
		}
		s.mu.Unlock()
	}
}
