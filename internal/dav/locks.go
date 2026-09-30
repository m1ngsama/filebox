package dav

import (
	"path"
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

func expiry(now time.Time, d time.Duration) time.Time {
	if d < 0 {
		return time.Time{}
	}
	return now.Add(d)
}

func (l *locks) Create(now time.Time, d webdav.LockDetails) (string, error) {
	tok, err := l.LockSystem.Create(now, d)
	if err == nil {
		l.mu.Lock()
		l.m[tok] = held{path.Clean("/" + d.Root), d.ZeroDepth, expiry(now, d.Duration)}
		l.mu.Unlock()
	}
	return tok, err
}

func (l *locks) Refresh(now time.Time, tok string, d time.Duration) (webdav.LockDetails, error) {
	ld, err := l.LockSystem.Refresh(now, tok, d)
	if err == nil {
		l.mu.Lock()
		if h, ok := l.m[tok]; ok {
			h.expiry = expiry(now, d)
			l.m[tok] = h
		}
		l.mu.Unlock()
	}
	return ld, err
}

func (l *locks) Unlock(now time.Time, tok string) error {
	l.mu.Lock()
	delete(l.m, tok)
	l.mu.Unlock()
	return l.LockSystem.Unlock(now, tok)
}

func under(p, root string) bool { return p == root || root == "/" || strings.HasPrefix(p, root+"/") }

func (l *locks) covering(now time.Time, p string) []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	var out []string
	for tok, h := range l.m {
		if !h.expiry.IsZero() && !now.Before(h.expiry) {
			delete(l.m, tok)
			continue
		}
		if under(h.root, p) || (!h.zeroDepth && under(p, h.root)) {
			out = append(out, tok)
		}
	}
	return out
}

func (l *locks) release(now time.Time, p string) {
	l.mu.Lock()
	var toks []string
	for tok, h := range l.m {
		if under(h.root, p) {
			toks = append(toks, tok)
		}
	}
	l.mu.Unlock()
	for _, tok := range toks {
		l.Unlock(now, tok)
	}
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
