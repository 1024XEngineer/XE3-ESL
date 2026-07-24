package identity

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sync"
	"time"
)

const (
	identityRateLimitWindow   = 15 * time.Minute
	registrationAttemptsPerIP = 5
	loginAttemptsPerIP        = 30
	loginAttemptsPerAccount   = 10
)

type RateLimitDecision struct {
	Allowed    bool
	RetryAfter time.Duration
}

type RateLimiter interface {
	Allow(key string) RateLimitDecision
}

type rateLimitWindow struct {
	start time.Time
	count int
}

// FixedWindowLimiter is process-local admission control for expensive public
// Identity endpoints. The Repository remains responsible for database
// uniqueness. A future multi-instance deployment must replace this adapter
// behind RateLimiter with shared admission control.
type FixedWindowLimiter struct {
	mu      sync.Mutex
	limit   int
	window  time.Duration
	clock   Clock
	entries map[string]rateLimitWindow
}

func NewFixedWindowLimiter(
	limit int,
	window time.Duration,
	clock Clock,
) (*FixedWindowLimiter, error) {
	if limit < 1 || window <= 0 || clock == nil {
		return nil, errors.New("identity: invalid rate limiter configuration")
	}
	return &FixedWindowLimiter{
		limit:   limit,
		window:  window,
		clock:   clock,
		entries: make(map[string]rateLimitWindow),
	}, nil
}

func (l *FixedWindowLimiter) Allow(key string) RateLimitDecision {
	now := l.clock.Now()
	l.mu.Lock()
	defer l.mu.Unlock()

	entry, found := l.entries[key]
	if !found || !now.Before(entry.start.Add(l.window)) {
		l.pruneExpired(now)
		l.entries[key] = rateLimitWindow{start: now, count: 1}
		return RateLimitDecision{Allowed: true}
	}
	if entry.count >= l.limit {
		return RateLimitDecision{
			Allowed:    false,
			RetryAfter: entry.start.Add(l.window).Sub(now),
		}
	}
	entry.count++
	l.entries[key] = entry
	return RateLimitDecision{Allowed: true}
}

func (l *FixedWindowLimiter) pruneExpired(now time.Time) {
	for key, entry := range l.entries {
		if !now.Before(entry.start.Add(l.window)) {
			delete(l.entries, key)
		}
	}
}

type RateLimiters struct {
	RegistrationIP RateLimiter
	LoginIP        RateLimiter
	LoginAccount   RateLimiter
}

func NewDefaultRateLimiters(clock Clock) (RateLimiters, error) {
	registration, err := NewFixedWindowLimiter(
		registrationAttemptsPerIP,
		identityRateLimitWindow,
		clock,
	)
	if err != nil {
		return RateLimiters{}, err
	}
	loginIP, err := NewFixedWindowLimiter(
		loginAttemptsPerIP,
		identityRateLimitWindow,
		clock,
	)
	if err != nil {
		return RateLimiters{}, err
	}
	loginAccount, err := NewFixedWindowLimiter(
		loginAttemptsPerAccount,
		identityRateLimitWindow,
		clock,
	)
	if err != nil {
		return RateLimiters{}, err
	}
	return RateLimiters{
		RegistrationIP: registration,
		LoginIP:        loginIP,
		LoginAccount:   loginAccount,
	}, nil
}

func accountRateLimitKey(email string) string {
	canonical, err := NormalizeEmail(email)
	if err != nil {
		canonical = email
	}
	digest := sha256.Sum256([]byte(canonical))
	return "login-account:" + hex.EncodeToString(digest[:])
}
