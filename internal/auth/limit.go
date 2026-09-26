package auth

import (
	"sync"
	"time"
)

type limiter struct {
	mu sync.Mutex
	m  map[string]*entry
}

type entry struct {
	fails int
	until time.Time
}

func newLimiter() *limiter { return &limiter{m: map[string]*entry{}} }

func (l *limiter) allow(ip string, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	e := l.m[ip]
	return e == nil || !now.Before(e.until)
}

func (l *limiter) fail(ip string, now time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.m) > 10000 {
		l.m = map[string]*entry{}
	}
	e := l.m[ip]
	if e == nil {
		e = &entry{}
		l.m[ip] = e
	}
	e.fails++
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
