package identity

import (
	"container/heap"
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
	defaultRateLimitCapacity  = 10_000
)

type Clock interface {
	Now() time.Time
}

type SystemClock struct{}

func (SystemClock) Now() time.Time { return time.Now() }

type RateLimitDecision struct {
	Allowed    bool
	RetryAfter time.Duration
}

type RateLimiter interface {
	Allow(key string) RateLimitDecision
}

type rateLimitWindow struct {
	expiresAt time.Time
	count     int
}

type expiryEntry struct {
	key       string
	expiresAt time.Time
}

type expiryHeap []expiryEntry

func (h expiryHeap) Len() int           { return len(h) }
func (h expiryHeap) Less(i, j int) bool { return h[i].expiresAt.Before(h[j].expiresAt) }
func (h expiryHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *expiryHeap) Push(value any)    { *h = append(*h, value.(expiryEntry)) }
func (h *expiryHeap) Pop() any {
	old := *h
	last := old[len(old)-1]
	*h = old[:len(old)-1]
	return last
}

// FixedWindowLimiter bounds both CPU and memory under high-cardinality input.
// Expiration work is amortized O(log n), and a full live-key set fails closed.
type FixedWindowLimiter struct {
	mu       sync.Mutex
	limit    int
	window   time.Duration
	capacity int
	clock    Clock
	entries  map[string]rateLimitWindow
	expiry   expiryHeap
}

func NewFixedWindowLimiter(
	limit int,
	window time.Duration,
	clock Clock,
) (*FixedWindowLimiter, error) {
	return NewFixedWindowLimiterWithCapacity(
		limit,
		window,
		defaultRateLimitCapacity,
		clock,
	)
}

func NewFixedWindowLimiterWithCapacity(
	limit int,
	window time.Duration,
	capacity int,
	clock Clock,
) (*FixedWindowLimiter, error) {
	if limit < 1 || window <= 0 || capacity < 1 || clock == nil {
		return nil, errors.New("identity: invalid rate limiter configuration")
	}
	return &FixedWindowLimiter{
		limit:    limit,
		window:   window,
		capacity: capacity,
		clock:    clock,
		entries:  make(map[string]rateLimitWindow, capacity),
		expiry:   make(expiryHeap, 0, capacity),
	}, nil
}

func (l *FixedWindowLimiter) Allow(key string) RateLimitDecision {
	now := l.clock.Now()
	l.mu.Lock()
	defer l.mu.Unlock()

	l.expire(now)
	if entry, found := l.entries[key]; found {
		if entry.count >= l.limit {
			return RateLimitDecision{
				Allowed:    false,
				RetryAfter: positiveDuration(entry.expiresAt.Sub(now)),
			}
		}
		entry.count++
		l.entries[key] = entry
		return RateLimitDecision{Allowed: true}
	}

	if len(l.entries) >= l.capacity {
		retryAfter := l.window
		if len(l.expiry) > 0 {
			retryAfter = positiveDuration(l.expiry[0].expiresAt.Sub(now))
		}
		return RateLimitDecision{Allowed: false, RetryAfter: retryAfter}
	}

	entry := rateLimitWindow{expiresAt: now.Add(l.window), count: 1}
	l.entries[key] = entry
	heap.Push(&l.expiry, expiryEntry{key: key, expiresAt: entry.expiresAt})
	return RateLimitDecision{Allowed: true}
}

func (l *FixedWindowLimiter) expire(now time.Time) {
	for len(l.expiry) > 0 && !now.Before(l.expiry[0].expiresAt) {
		expired := heap.Pop(&l.expiry).(expiryEntry)
		entry, found := l.entries[expired.key]
		if found && entry.expiresAt.Equal(expired.expiresAt) {
			delete(l.entries, expired.key)
		}
	}
}

func positiveDuration(value time.Duration) time.Duration {
	if value <= 0 {
		return time.Nanosecond
	}
	return value
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
