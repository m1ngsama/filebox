package api

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/m1ngsama/filebox/internal/vol"
)

type JobStatus struct {
	ID    string `json:"id"`
	Total int64  `json:"total"`
	Done  int64  `json:"done"`
	State string `json:"state"`
	Error string `json:"error,omitempty"`
}

type job struct {
	total, done atomic.Int64
	mu          sync.Mutex
	state, err  string
	finished    time.Time
}

type Jobs struct {
	mu sync.Mutex
	m  map[string]*job
}

func NewJobs() *Jobs { return &Jobs{m: map[string]*job{}} }

func (j *Jobs) Get(id string) (JobStatus, bool) {
	j.mu.Lock()
	x, ok := j.m[id]
	j.mu.Unlock()
	if !ok {
		return JobStatus{}, false
	}
	x.mu.Lock()
	defer x.mu.Unlock()
	return JobStatus{ID: id, Total: x.total.Load(), Done: x.done.Load(), State: x.state, Error: x.err}, true
}

func (j *Jobs) Start(src, dst *vol.Volume, srel, drel string, move bool) string {
	b := make([]byte, 8)
	rand.Read(b)
	id := hex.EncodeToString(b)
	x := &job{state: "running"}
	j.mu.Lock()
	for k, old := range j.m {
		old.mu.Lock()
		stale := old.state != "running" && time.Since(old.finished) > time.Hour
		old.mu.Unlock()
		if stale {
			delete(j.m, k)
		}
	}
	j.m[id] = x
	j.mu.Unlock()

	go func() {
		cerr, rootConflict := copyTree(src, dst, srel, drel, x)
		err := cerr
		if cerr == nil && move {
			err = src.Root.RemoveAll(srel)
		} else if cerr != nil && !rootConflict {
			dst.Root.RemoveAll(drel)
		}
		x.mu.Lock()
		x.state, x.finished = "done", time.Now()
		if err != nil {
			x.state, x.err = "error", err.Error()
			slog.Error("job failed", "id", id, "err", err)
		}
		x.mu.Unlock()
	}()
	return id
}

type counter struct {
	r io.Reader
	n *atomic.Int64
}

func (c counter) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n.Add(int64(n))
	return n, err
}

// Symlinks are skipped: following them could copy data from outside the volume.
func copyTree(src, dst *vol.Volume, srel, drel string, x *job) (err error, rootConflict bool) {
	sfs := src.Root.FS()
	fs.WalkDir(sfs, srel, func(p string, d fs.DirEntry, err error) error {
		if err == nil && d.Type().IsRegular() {
			if fi, err := d.Info(); err == nil {
				x.total.Add(fi.Size())
			}
		}
		return nil
	})
	err = fs.WalkDir(sfs, srel, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		target := path.Join(drel, strings.TrimPrefix(strings.TrimPrefix(p, srel), "/"))
		switch {
		case d.IsDir():
			if p == srel {
				err := dst.Root.Mkdir(target, 0o755)
				rootConflict = errors.Is(err, fs.ErrExist)
				return err
			}
			return dst.Root.MkdirAll(target, 0o755)
		case d.Type().IsRegular():
			err := copyFile(src.Root, dst.Root, p, target, x)
			if p == srel {
				rootConflict = errors.Is(err, fs.ErrExist)
			}
			return err
		}
		return nil
	})
	return err, rootConflict
}

func copyFile(sr, dr *os.Root, from, to string, x *job) error {
	in, err := sr.Open(from)
	if err != nil {
		return err
	}
	defer in.Close()
	fi, err := in.Stat()
	if err != nil {
		return err
	}
	out, err := dr.OpenFile(to, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, counter{in, &x.done}); err != nil {
		out.Close()
		dr.Remove(to)
		return err
	}
	if err := out.Close(); err != nil {
		dr.Remove(to)
		return err
	}
	return dr.Chtimes(to, fi.ModTime(), fi.ModTime())
}
