package dav

import (
	"net/http"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/webdav"
)

type held struct {
	root      string
	zeroDepth bool
	expiry    time.Time
}

type locks struct {
	webdav.LockSystem
	mu sync.Mutex
	m  map[string]held
}

func newLocks() *locks { return &locks{LockSystem: webdav.NewMemLS(), m: map[string]held{}} }

const maxLock = time.Hour

func clamp(d time.Duration) time.Duration {
	if d < 0 || d > maxLock {
		return maxLock
	}
	return d
}

func clampTimeout(h http.Header) {
	d := time.Duration(-1)
	if v, ok := strings.CutPrefix(strings.TrimSpace(strings.Split(h.Get("Timeout"), ",")[0]), "Second-"); ok {
		if n, err := strconv.ParseUint(v, 10, 32); err == nil {
			d = time.Duration(n) * time.Second
		}
	}
	h.Set("Timeout", "Second-"+strconv.Itoa(int(clamp(d)/time.Second)))
}

func (l *locks) Create(now time.Time, d webdav.LockDetails) (string, error) {
	d.Duration = clamp(d.Duration)
	tok, err := l.LockSystem.Create(now, d)
	if err == nil {
		l.mu.Lock()
		l.m[tok] = held{path.Clean("/" + d.Root), d.ZeroDepth, now.Add(d.Duration)}
		l.mu.Unlock()
	}
	return tok, err
}

func (l *locks) Refresh(now time.Time, tok string, d time.Duration) (webdav.LockDetails, error) {
	d = clamp(d)
	ld, err := l.LockSystem.Refresh(now, tok, d)
	if err == nil {
		l.mu.Lock()
		if h, ok := l.m[tok]; ok {
			h.expiry = now.Add(d)
			l.m[tok] = h
		}
		l.mu.Unlock()
	}
	return ld, err
}

func (l *locks) Unlock(now time.Time, tok string) error {
	err := l.LockSystem.Unlock(now, tok)
	if err == nil {
		l.mu.Lock()
		delete(l.m, tok)
		l.mu.Unlock()
	}
	return err
}

func under(p, root string) bool { return p == root || root == "/" || strings.HasPrefix(p, root+"/") }

func within(p, root string, same func(a, b string) bool) bool {
	if under(p, root) {
		return true
	}
	if len(p) < len(root) || (len(p) > len(root) && p[len(root)] != '/') {
		return false
	}
	head := p[:len(root)]
	return same != nil && strings.EqualFold(head, root) && same(head, root)
}

func (l *locks) covering(now time.Time, p string, same func(a, b string) bool) []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []string
	for tok, h := range l.m {
		if !now.Before(h.expiry) {
			delete(l.m, tok)
			continue
		}
		if within(h.root, p, same) || (!h.zeroDepth && within(p, h.root, same)) {
			out = append(out, tok)
		}
	}
	return out
}

func (l *locks) below(p string, same func(a, b string) bool) []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	var toks []string
	for tok, h := range l.m {
		if within(h.root, p, same) {
			toks = append(toks, tok)
		}
	}
	return toks
}

func submitted(hdr string) map[string]bool {
	out := map[string]bool{}
	depth, not := 0, false
	for i := 0; i < len(hdr); i++ {
		switch c := hdr[i]; {
		case c == '(':
			depth++
		case c == ')':
			depth--
		case c == '<':
			j := strings.IndexByte(hdr[i:], '>')
			if j < 0 {
				return out
			}
			if depth > 0 && !not {
				out[hdr[i+1:i+j]] = true
			}
			i += j
			not = false
		case depth > 0 && strings.HasPrefix(hdr[i:], "Not"):
			not = true
			i += 2
		case c == '[':
			j := strings.IndexByte(hdr[i:], ']')
			if j < 0 {
				return out
			}
			i += j
			not = false
		}
	}
	return out
}
