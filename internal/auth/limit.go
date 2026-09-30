package auth

import (
	"sync"
	"time"

	"github.com/m1ngsama/filebox/internal/db"
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

type Limited struct {
	Global bool
	Wait   time.Duration
}

func (e *Limited) Error() string {
	if e.Global {
		return "login paused"
	}
	return "too many attempts"
}

func (e *Limited) Is(target error) bool { return target == ErrRateLimited }

func key(ip string) string {
	if a, ok := db.Client(ip); ok {
		return a.String()
	}
	return ip
}

func (l *limiter) check(ip string, now time.Time) error {
	ip = key(ip)
	l.mu.Lock()
	defer l.mu.Unlock()
	if end := l.start.Add(time.Minute); l.budget > 0 && l.fails > l.budget && now.Before(end) {
		return &Limited{Global: true, Wait: end.Sub(now)}
	}
	if e := l.m[ip]; e != nil && now.Before(e.until) {
		return &Limited{Wait: e.until.Sub(now)}
	}
	return nil
}

func (l *limiter) fail(ip string, now time.Time) {
	ip = key(ip)
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
	ip = key(ip)
	l.mu.Lock()
	delete(l.m, ip)
	l.mu.Unlock()
}
