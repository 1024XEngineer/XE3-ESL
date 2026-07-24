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
