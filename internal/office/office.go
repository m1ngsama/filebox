// Package office turns Word, Excel and PowerPoint files into PDF with LibreOffice so the browser can show them.
package office

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/m1ngsama/filebox/internal/httpx"
	"github.com/m1ngsama/filebox/internal/vol"
)

const (
	MaxInput = 100 << 20
	keep     = 1 << 30
	timeout  = 2 * time.Minute
)

var exts = map[string]bool{".doc": true, ".docx": true, ".odt": true, ".rtf": true, ".xls": true, ".xlsx": true, ".ods": true, ".ppt": true, ".pptx": true, ".odp": true}

func Supported(name string) bool { return exts[strings.ToLower(path.Ext(name))] }

var ErrFailed = errors.New("could not convert the document")

type Converter struct {
	bin, dir string
	isolate  []string
	nice     string
	sem      chan struct{}
	mu       sync.Mutex
	inflight map[string]chan struct{}
}

// New finds LibreOffice, or returns nil when it is not installed.
func New(dir string) *Converter {
	var bin string
	for _, n := range []string{"soffice", "libreoffice", "/Applications/LibreOffice.app/Contents/MacOS/soffice"} {
		if p, err := exec.LookPath(n); err == nil {
			bin = p
			break
		}
	}
	if bin == "" || os.MkdirAll(dir, 0o700) != nil {
		return nil
	}
	c := &Converter{bin: bin, dir: dir, sem: make(chan struct{}, 1), inflight: map[string]chan struct{}{}}
	c.nice, _ = exec.LookPath("nice")
	// Documents may come from anonymous drop shares; without a network they cannot fetch linked content.
	if u, err := exec.LookPath("unshare"); err == nil {
		args := []string{u, "--user", "--map-root-user", "--net", "--"}
		if exec.Command(args[0], append(args[1:], "true")...).Run() == nil {
			c.isolate = args
		}
	}
	if c.isolate == nil {
		slog.Info("office preview runs LibreOffice with network access", "bin", bin)
	}
	return c
}

func (c *Converter) Serve(w http.ResponseWriter, r *http.Request, root *os.Root, rel string) {
	if c == nil {
		httpx.Fail(w, 501, "office preview unavailable")
		return
	}
	if !Supported(rel) {
		httpx.Fail(w, 400, "not an office document")
		return
	}
	f, err := vol.Open(root, rel)
	if err != nil {
		httpx.Error(w, err)
		return
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() {
		httpx.Fail(w, 400, "not a file")
		return
	}
	if st.Size() > MaxInput {
		httpx.Fail(w, 413, "too large to preview")
		return
	}
	etag := fmt.Sprintf(`"o%x-%x"`, st.Size(), st.ModTime().UnixNano())
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	sum := sha256.Sum256(fmt.Appendf(nil, "%s\x00%d\x00%d", filepath.Join(root.Name(), rel), st.Size(), st.ModTime().UnixNano()))
	out, err := c.pdf(r.Context(), f, path.Ext(rel), hex.EncodeToString(sum[:16]))
	if err != nil {
		if r.Context().Err() == nil {
			slog.Warn("office preview", "path", rel, "err", err)
		}
		httpx.Fail(w, 422, ErrFailed.Error())
		return
	}
	w.Header().Set("ETag", etag)
	w.Header().Set("Cache-Control", "private, no-cache")
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Security-Policy", "sandbox")
	http.ServeFile(w, r, out)
}

func (c *Converter) pdf(ctx context.Context, src io.Reader, ext, key string) (string, error) {
	out := filepath.Join(c.dir, key+".pdf")
	for {
		if _, err := os.Stat(out); err == nil {
			now := time.Now()
			os.Chtimes(out, now, now)
			return out, nil
		}
		c.mu.Lock()
		wait, busy := c.inflight[key]
		if !busy {
			wait = make(chan struct{})
			c.inflight[key] = wait
		}
		c.mu.Unlock()
		if !busy {
			break
		}
		select {
		case <-wait:
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	defer func() {
		c.mu.Lock()
		close(c.inflight[key])
		delete(c.inflight, key)
		c.mu.Unlock()
	}()
	select {
	case c.sem <- struct{}{}:
		defer func() { <-c.sem }()
	case <-ctx.Done():
		return "", ctx.Err()
	}
	if err := c.convert(src, ext, out); err != nil {
		return "", err
	}
	c.trim()
	return out, nil
}

// convert outlives the request that asked for it, so a reload finds the PDF ready.
func (c *Converter) convert(src io.Reader, ext, out string) error {
	tmp, err := os.MkdirTemp(c.dir, "work-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	in := filepath.Join(tmp, "doc"+strings.ToLower(ext))
	f, err := os.OpenFile(in, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	_, err = io.Copy(f, io.LimitReader(src, MaxInput))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	args := []string{c.bin, "--headless", "--norestore", "--nologo", "--nolockcheck", "--nodefault",
		"-env:UserInstallation=file://" + filepath.ToSlash(filepath.Join(c.dir, "profile")), "--convert-to", "pdf", "--outdir", tmp, in}
	if c.nice != "" {
		args = append([]string{c.nice, "-n", "10"}, args...)
	}
	args = append(slices.Clone(c.isolate), args...)
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	cmd.Env = []string{"HOME=" + tmp, "PATH=/usr/bin:/bin", "LANG=C.UTF-8"}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	if b, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(b)))
	}
	return os.Rename(filepath.Join(tmp, "doc.pdf"), out)
}

func (c *Converter) trim() {
	es, _ := os.ReadDir(c.dir)
	type file struct {
		path string
		size int64
		used time.Time
	}
	var fs []file
	var total int64
	for _, e := range es {
		if fi, err := e.Info(); err == nil && strings.HasSuffix(e.Name(), ".pdf") {
			fs = append(fs, file{filepath.Join(c.dir, e.Name()), fi.Size(), fi.ModTime()})
			total += fi.Size()
		}
	}
	slices.SortFunc(fs, func(a, b file) int { return a.used.Compare(b.used) })
	for _, f := range fs {
		if total <= keep {
			break
		}
		os.Remove(f.path)
		total -= f.size
	}
}
