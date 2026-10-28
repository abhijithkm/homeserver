package auth

import (
	"sync"
	"time"
)

type attempt struct {
	fails       int
	windowStart time.Time
	lockedUntil time.Time
}

// Limiter throttles failed logins per key with exponential lockout. It keeps
// at most maxKeys entries and uses no background goroutine.
type Limiter struct {
	mu      sync.Mutex
	m       map[string]*attempt
	now     func() time.Time
	maxFail int
	window  time.Duration
	maxLock time.Duration
}

const maxKeys = 10000

// NewLimiter returns a limiter allowing maxFail failures per window.
func NewLimiter(maxFail int, window, maxLock time.Duration) *Limiter {
	return &Limiter{m: map[string]*attempt{}, now: time.Now, maxFail: maxFail, window: window, maxLock: maxLock}
}

// Allow reports whether key may try now, and how long to wait otherwise.
func (l *Limiter) Allow(key string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	a := l.m[key]
	if a == nil {
		return true, 0
	}
	if d := a.lockedUntil.Sub(l.now()); d > 0 {
		return false, d
	}
	return true, 0
}

// Fail records a failure.
func (l *Limiter) Fail(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	n := l.now()
	a := l.m[key]
	if a == nil {
		if len(l.m) >= maxKeys {
			l.purge(n)
			if len(l.m) >= maxKeys {
				return
			}
		}
		a = &attempt{windowStart: n}
		l.m[key] = a
	}
	if n.Sub(a.windowStart) > l.window {
		a.fails, a.windowStart = 0, n
	}
	a.fails++
	if a.fails >= l.maxFail {
		d := 15 * time.Second << uint(min(a.fails-l.maxFail, 10))
		if d > l.maxLock {
			d = l.maxLock
		}
		a.lockedUntil = n.Add(d)
	}
}

// Reset clears a key after a successful login.
func (l *Limiter) Reset(key string) {
	l.mu.Lock()
	delete(l.m, key)
	l.mu.Unlock()
}

func (l *Limiter) purge(n time.Time) {
	for k, a := range l.m {
		if n.Sub(a.windowStart) > l.window && n.After(a.lockedUntil) {
			delete(l.m, k)
		}
	}
}
