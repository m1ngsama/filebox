package stream

import (
	"bytes"
	"context"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestTranscodesSegmentsOnDemand(t *testing.T) {
	ff, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not installed")
	}
	dir := t.TempDir()
	if out, err := exec.Command(ff, "-v", "error", "-f", "lavfi", "-i", "testsrc2=s=640x360:r=25:d=10", "-f", "lavfi", "-i", "sine=d=10",
		"-c:v", "libx264", "-c:a", "aac", "-shortest", filepath.Join(dir, "clip.mp4")).CombinedOutput(); err != nil {
		t.Skipf("fixture: %v %s", err, out)
	}
	s := New(ff, filepath.Join(t.TempDir(), "stream"))
	if s.FFprobe == "" {
		t.Skip("ffprobe not installed")
	}
	s.Probe(context.Background())
	root, _ := os.OpenRoot(dir)
	defer root.Close()

	w := httptest.NewRecorder()
	s.Playlist(w, httptest.NewRequest("GET", "/api/stream/index.m3u8?vol=v&p=clip.mp4&q=720", nil), root, "clip.mp4")
	body := w.Body.String()
	if w.Code != 200 || strings.Count(body, "#EXTINF") != 3 || !strings.Contains(body, "#EXT-X-ENDLIST") || !strings.Contains(body, "seg?n=2&p=clip.mp4&q=720&vol=v") {
		t.Fatalf("%d %s", w.Code, body)
	}

	for _, n := range []string{"2", "0", "1"} {
		w = httptest.NewRecorder()
		s.Segment(w, httptest.NewRequest("GET", "/api/stream/seg?p=clip.mp4&q=720&n="+n, nil), root, "clip.mp4")
		if b := w.Body.Bytes(); w.Code != 200 || len(b) < 188 || b[0] != 0x47 || !bytes.Contains(b[:188*4], []byte{0x47}) {
			t.Fatalf("segment %s: %d, %d bytes", n, w.Code, len(b))
		}
	}

	w = httptest.NewRecorder()
	s.Segment(w, httptest.NewRequest("GET", "/api/stream/seg?p=clip.mp4&q=720&n=9", nil), root, "clip.mp4")
	if w.Code != 400 {
		t.Fatalf("segment past the end: %d", w.Code)
	}
}

func TestWaitingPlayerGetsTheEncoder(t *testing.T) {
	s := New("", t.TempDir())
	spawn := func(start, want, made int) *session {
		cmd := exec.Command("sleep", "30")
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { cmd.Process.Kill(); cmd.Wait() })
		ss := &session{dir: t.TempDir(), start: start, want: want, cmd: cmd, exited: make(chan struct{})}
		for i := start; i < made; i++ {
			os.WriteFile(segName(ss.dir, i), nil, 0o644)
		}
		return ss
	}
	busy, fresh := spawn(0, 3, 12), spawn(40, 40, 40)
	s.sessions = map[string]*session{"busy": busy, "fresh": fresh}

	s.pace()
	if !busy.stopped || fresh.stopped {
		t.Fatalf("while one player waits: busy stopped=%v, fresh stopped=%v", busy.stopped, fresh.stopped)
	}
	for i := 40; i < 44; i++ {
		os.WriteFile(segName(fresh.dir, i), nil, 0o644)
	}
	s.pace()
	if !busy.stopped {
		t.Fatal("busy encoder resumed with most of its buffer left")
	}
	busy.want = 8
	s.pace()
	if busy.stopped {
		t.Fatal("busy encoder stayed paused with its buffer running low")
	}
	busy.want = -10
	s.pace()
	if !busy.stopped {
		t.Fatal("encoder far ahead of its player kept running")
	}
}
