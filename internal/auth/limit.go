package auth

import (
	"sync"
	"time"
)

const maxEntries = 10000

type limiter struct {
	mu     sync.Mutex
	m      map[string]*entry
	budget int
	start  time.Time
	fails  int
}

type entry struct {
	fails       int
	last, until time.Time
}

func newLimiter(budget int) *limiter { return &limiter{m: map[string]*entry{}, budget: budget} }

func (l *limiter) allow(ip string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.budget > 0 && l.fails > l.budget && now.Sub(l.start) < time.Minute {
		return false
	}
	e := l.m[ip]
	return e == nil || !now.Before(e.until)
}

func (l *limiter) fail(ip string, now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if now.Sub(l.start) >= time.Minute {
		l.start, l.fails = now, 0
	}
	l.fails++
	e := l.m[ip]
	if e == nil {
		if len(l.m) >= maxEntries {
			for k, x := range l.m {
				if now.After(x.until) && now.Sub(x.last) > 15*time.Minute {
					delete(l.m, k)
				}
			}
		}
		if len(l.m) >= maxEntries {
			return
		}
		e = &entry{}
		l.m[ip] = e
	}
	e.fails++
	e.last = now
	if e.fails >= 5 {
		d := time.Second << min(e.fails-5, 10)
		e.until = now.Add(min(d, 15*time.Minute))
	}
}

func (l *limiter) ok(ip string) {
	l.mu.Lock()
	delete(l.m, ip)
	l.mu.Unlock()
}
