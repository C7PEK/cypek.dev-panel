package auth

import (
	"sync"
	"time"
)

// RateLimiter is a fixed-window counter keyed by an arbitrary string
// (client IP or lowercase username). Login attempts beyond Limit within
// Window are rejected until the window expires. In-memory on purpose: a panel
// restart resetting the counters is acceptable for a single-host deployment.
type RateLimiter struct {
	Limit  int
	Window time.Duration

	mu      sync.Mutex
	buckets map[string]*bucket
	now     func() time.Time
}

type bucket struct {
	count int
	reset time.Time
}

// NewRateLimiter allows limit attempts per window.
func NewRateLimiter(limit int, window time.Duration) *RateLimiter {
	return &RateLimiter{Limit: limit, Window: window, buckets: map[string]*bucket{}, now: time.Now}
}

// Allow records an attempt for key and reports whether it is within limits.
// Call it before the expensive check; call Reset on success.
func (r *RateLimiter) Allow(key string) (ok bool, retryAfter time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.now()
	r.sweep(now)
	b := r.buckets[key]
	if b == nil || now.After(b.reset) {
		b = &bucket{reset: now.Add(r.Window)}
		r.buckets[key] = b
	}
	b.count++
	if b.count > r.Limit {
		return false, b.reset.Sub(now)
	}
	return true, 0
}

// Reset clears the counter for key, e.g. after a successful login.
func (r *RateLimiter) Reset(key string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.buckets, key)
}

// sweep drops expired buckets; cheap enough to run on every call at this scale.
func (r *RateLimiter) sweep(now time.Time) {
	if len(r.buckets) < 1024 {
		return
	}
	for k, b := range r.buckets {
		if now.After(b.reset) {
			delete(r.buckets, k)
		}
	}
}
