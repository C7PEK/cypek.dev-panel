package auth

import (
	"sync"
	"time"
)

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

func NewRateLimiter(limit int, window time.Duration) *RateLimiter {
	return &RateLimiter{Limit: limit, Window: window, buckets: map[string]*bucket{}, now: time.Now}
}

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

func (r *RateLimiter) Reset(key string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.buckets, key)
}

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
