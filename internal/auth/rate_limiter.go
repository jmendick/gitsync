package auth

import (
	"net/http"
	"sync"
	"time"
)

type RateLimiter struct {
	attempts map[string]*attemptInfo
	mu       sync.RWMutex
	// Maximum login attempts before lockout
	maxAttempts int
	// How long to keep tracking attempts
	windowDuration time.Duration
	// How long to lock out an IP after max attempts
	lockoutDuration time.Duration
}

type attemptInfo struct {
	attempts     int
	firstAttempt time.Time
	lockedUntil  time.Time
}

func NewRateLimiter(maxAttempts int, windowDuration, lockoutDuration time.Duration) *RateLimiter {
	return &RateLimiter{
		attempts:        make(map[string]*attemptInfo),
		maxAttempts:     maxAttempts,
		windowDuration:  windowDuration,
		lockoutDuration: lockoutDuration,
	}
}

func (rl *RateLimiter) cleanup() {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	for ip, info := range rl.attempts {
		// Remove entries older than window duration
		if now.Sub(info.firstAttempt) > rl.windowDuration && now.After(info.lockedUntil) {
			delete(rl.attempts, ip)
		}
	}
}

func (rl *RateLimiter) isLocked(ip string) bool {
	rl.mu.RLock()
	defer rl.mu.RUnlock()

	if info, exists := rl.attempts[ip]; exists {
		return time.Now().Before(info.lockedUntil)
	}
	return false
}

func (rl *RateLimiter) recordAttempt(ip string) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()

	// Create new attempt info if not exists
	if _, exists := rl.attempts[ip]; !exists {
		rl.attempts[ip] = &attemptInfo{
			attempts:     1,
			firstAttempt: now,
		}
		return true
	}

	info := rl.attempts[ip]

	// Reset if window has expired
	if now.Sub(info.firstAttempt) > rl.windowDuration {
		info.attempts = 1
		info.firstAttempt = now
		info.lockedUntil = time.Time{}
		return true
	}

	// Increment attempts
	info.attempts++

	// Lock if max attempts exceeded
	if info.attempts >= rl.maxAttempts {
		info.lockedUntil = now.Add(rl.lockoutDuration)
		return false
	}

	return true
}

func (rl *RateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Clean up old entries periodically
		go rl.cleanup()

		ip := r.RemoteAddr

		// Check if IP is locked out
		if rl.isLocked(ip) {
			http.Error(w, "Too many login attempts. Please try again later.", http.StatusTooManyRequests)
			return
		}

		// Record the attempt
		if !rl.recordAttempt(ip) {
			http.Error(w, "Too many login attempts. Please try again later.", http.StatusTooManyRequests)
			return
		}

		next.ServeHTTP(w, r)
	})
}
