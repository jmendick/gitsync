package bandwidth

import (
	"sync"
	"time"
)

// rateLimiter implements a token bucket rate limiter
type rateLimiter struct {
	limit      int64
	tokens     float64
	lastRefill time.Time
	mu         sync.Mutex
}

var (
	ErrCongestionLimit = NewBandwidthError("congestion limit exceeded")
)

type BandwidthError struct {
	msg string
}

func (e *BandwidthError) Error() string {
	return e.msg
}

func NewBandwidthError(msg string) *BandwidthError {
	return &BandwidthError{msg: msg}
}

func newRateLimiter(limit int64) *rateLimiter {
	return &rateLimiter{
		limit:      limit,
		tokens:     float64(limit),
		lastRefill: time.Now(),
	}
}

func (r *rateLimiter) acquire(bytes int64) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	elapsed := now.Sub(r.lastRefill).Seconds()
	r.tokens = min(float64(r.limit), r.tokens+float64(r.limit)*elapsed)
	r.lastRefill = now

	if r.tokens < float64(bytes) {
		return NewBandwidthError("rate limit exceeded")
	}

	r.tokens -= float64(bytes)
	return nil
}

func (r *rateLimiter) setLimit(limit int64) {
	r.mu.Lock()
	r.limit = limit
	r.mu.Unlock()
}

func min(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}
