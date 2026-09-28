package identity

import (
	"sync"
	"time"
)

// limiter is a fixed-window counter keyed by string (IP, identifier, …). PRD uses
// Redis for this; Redis is optional in this deployment, and a single API instance
// makes an in-process limiter sufficient (D-14).
type limiter struct {
	mu      sync.Mutex
	limit   int
	window  time.Duration
	buckets map[string]*bucket
}

type bucket struct {
	count int
	reset time.Time
}

func newLimiter(limit int, window time.Duration) *limiter {
	return &limiter{limit: limit, window: window, buckets: map[string]*bucket{}}
}

// blocked reports whether key has reached its limit, and for how long it stays blocked.
func (l *limiter) blocked(key string) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	b, ok := l.buckets[key]
	if !ok || time.Now().After(b.reset) {
		return false, 0
	}
	if b.count >= l.limit {
		return true, time.Until(b.reset)
	}
	return false, 0
}

// hit records one event for key and reports whether the limit is now reached.
func (l *limiter) hit(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	if len(l.buckets) > 10_000 {
		for k, b := range l.buckets {
			if now.After(b.reset) {
				delete(l.buckets, k)
			}
		}
	}
	b, ok := l.buckets[key]
	if !ok || now.After(b.reset) {
		b = &bucket{reset: now.Add(l.window)}
		l.buckets[key] = b
	}
	b.count++
	return b.count >= l.limit
}

func (l *limiter) clear(key string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.buckets, key)
}
