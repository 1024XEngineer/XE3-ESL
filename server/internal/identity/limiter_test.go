package identity

import (
	"testing"
	"time"
)

type mutableClock struct {
	now time.Time
}

func (c *mutableClock) Now() time.Time { return c.now }

func TestFixedWindowLimiterIsDeterministic(t *testing.T) {
	clock := &mutableClock{
		now: time.Date(2026, 7, 24, 12, 0, 0, 0, time.UTC),
	}
	limiter, err := NewFixedWindowLimiter(2, time.Minute, clock)
	if err != nil {
		t.Fatalf("new limiter: %v", err)
	}
	if !limiter.Allow("scope").Allowed || !limiter.Allow("scope").Allowed {
		t.Fatal("expected first two attempts to pass")
	}
	decision := limiter.Allow("scope")
	if decision.Allowed || decision.RetryAfter != time.Minute {
		t.Fatalf("unexpected limited decision: %#v", decision)
	}

	clock.now = clock.now.Add(30 * time.Second)
	decision = limiter.Allow("scope")
	if decision.Allowed || decision.RetryAfter != 30*time.Second {
		t.Fatalf("unexpected retry delay: %#v", decision)
	}

	clock.now = clock.now.Add(30 * time.Second)
	if !limiter.Allow("scope").Allowed {
		t.Fatal("expected new window to admit request")
	}
}

func TestLoginAccountScopeDoesNotContainEmail(t *testing.T) {
	key := accountRateLimitKey("Learner@Example.com")
	if key == "" || key == "login-account:learner@example.com" {
		t.Fatalf("unsafe account scope: %q", key)
	}
	if key != accountRateLimitKey(" learner@example.com ") {
		t.Fatal("canonical email variants must share a scope")
	}
}

func TestFixedWindowLimiterFailsClosedAtCapacityAndReusesExpiredSpace(t *testing.T) {
	clock := &mutableClock{now: time.Unix(1_000, 0)}
	limiter, err := NewFixedWindowLimiterWithCapacity(1, time.Minute, 3, clock)
	if err != nil {
		t.Fatalf("new limiter: %v", err)
	}
	for _, key := range []string{"one", "two", "three"} {
		if !limiter.Allow(key).Allowed {
			t.Fatalf("expected %q to be admitted", key)
		}
	}
	decision := limiter.Allow("high-cardinality-four")
	if decision.Allowed || decision.RetryAfter != time.Minute {
		t.Fatalf("capacity must fail closed: %#v", decision)
	}
	if len(limiter.entries) != 3 || len(limiter.expiry) != 3 {
		t.Fatalf("limiter exceeded capacity: %d/%d", len(limiter.entries), len(limiter.expiry))
	}

	clock.now = clock.now.Add(time.Minute)
	if !limiter.Allow("replacement").Allowed {
		t.Fatal("expired capacity was not reclaimed")
	}
	if len(limiter.entries) != 1 || len(limiter.expiry) != 1 {
		t.Fatalf("expired entries were not bounded: %d/%d", len(limiter.entries), len(limiter.expiry))
	}
}
