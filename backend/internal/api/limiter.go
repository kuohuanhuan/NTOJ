package api

import (
	"sync"
	"time"
)

// limiter is a fixed-window in-memory rate limiter. State is per process,
// which is sufficient to blunt credential stuffing and form spam.
type limiter struct {
	mu      sync.Mutex
	buckets map[string]*bucket
	sweep   time.Time
}

type bucket struct {
	count int
	reset time.Time
}

func newLimiter() *limiter { return &limiter{buckets: map[string]*bucket{}} }

// allow reports whether another event for key fits in max events per window.
func (l *limiter) allow(key string, max int, window time.Duration) bool {
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	if now.After(l.sweep) {
		for k, b := range l.buckets {
			if now.After(b.reset) {
				delete(l.buckets, k)
			}
		}
		l.sweep = now.Add(time.Minute)
	}
	b := l.buckets[key]
	if b == nil || now.After(b.reset) {
		b = &bucket{reset: now.Add(window)}
		l.buckets[key] = b
	}
	b.count++
	return b.count <= max
}
