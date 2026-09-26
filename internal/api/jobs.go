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
	"syscall"
	"time"

	"github.com/m1ngsama/filebox/internal/vol"
)

type JobStatus struct {
	ID    string `json:"id"`
	Total int64  `json:"total"`
	Done  int64  `json:"done"`
	State string `json:"state"`
	Code  string `json:"code,omitempty"`
}

type job struct {
	total, done atomic.Int64
	mu          sync.Mutex
	state, code string
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
	return JobStatus{ID: id, Total: x.total.Load(), Done: x.done.Load(), State: x.state, Code: x.code}, true
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
		err := run(src, dst, srel, drel, id, move, x)
		x.mu.Lock()
		x.state, x.finished = "done", time.Now()
		if err != nil {
			x.state, x.code = "error", errorCode(err)
			slog.Error("job failed", "id", id, "err", err)
		}
		x.mu.Unlock()
	}()
	return id
}

func errorCode(err error) string {
	switch {
	case errors.Is(err, fs.ErrExist):
		return "exists"
	case errors.Is(err, fs.ErrNotExist):
		return "notfound"
	case errors.Is(err, syscall.ENOSPC) || errors.Is(err, syscall.EDQUOT):
		return "nospace"
	}
	return "internal"
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

var errSpecial = errors.New("symlink or special file")

func ClearStaging(vols *vol.Set) {
	for _, v := range vols.All() {
		v.Root.RemoveAll(vol.JobsDir)
	}
}

func run(src, dst *vol.Volume, srel, drel, id string, move bool, x *job) error {
	stage := path.Join(vol.JobsDir, id)
	if err := dst.Root.MkdirAll(stage, 0o700); err != nil {
		return err
	}
	defer dst.Root.RemoveAll(stage)
	tmp := path.Join(stage, "item")
	if err := copyTree(src, dst, srel, tmp, move, x); err != nil {
		return err
	}
	if err := place(dst.Root, tmp, drel); err != nil {
		return err
	}
	if move {
		return src.Root.RemoveAll(srel)
	}
	return nil
}

func place(r *os.Root, from, to string) error {
	err := r.Link(from, to)
	if err == nil || errors.Is(err, fs.ErrExist) {
		return err
	}
	if _, err := r.Lstat(to); err == nil {
		return fs.ErrExist
	}
	return r.Rename(from, to)
}

// Copies skip symlinks, since following them could copy data from outside the volume.
func copyTree(src, dst *vol.Volume, srel, drel string, strict bool, x *job) error {
	if fi, err := src.Root.Lstat(srel); err != nil {
		return err
	} else if strict && !fi.IsDir() && !fi.Mode().IsRegular() {
		return errSpecial
	}
	sfs := src.Root.FS()
	fs.WalkDir(sfs, srel, func(p string, d fs.DirEntry, err error) error {
		if err == nil && d.Type().IsRegular() {
			if fi, err := d.Info(); err == nil {
				x.total.Add(fi.Size())
			}
		}
		return nil
	})
	return fs.WalkDir(sfs, srel, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		target := path.Join(drel, strings.TrimPrefix(strings.TrimPrefix(p, srel), "/"))
		switch {
		case d.IsDir():
			return dst.Root.MkdirAll(target, 0o755)
		case d.Type().IsRegular():
			return copyFile(src.Root, dst.Root, p, target, x)
		case strict:
			return errSpecial
		}
		return nil
	})
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
