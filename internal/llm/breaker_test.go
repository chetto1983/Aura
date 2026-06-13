package llm

import (
	"errors"
	"testing"
	"time"
)

func TestBreakerOpensAfterThresholdAndResetsOnSuccess(t *testing.T) {
	now := time.Date(2030, 1, 1, 0, 0, 0, 0, time.UTC)
	b := NewBreaker(2, time.Minute)
	b.now = func() time.Time { return now }

	if err := b.Allow(); err != nil {
		t.Fatalf("fresh breaker should allow: %v", err)
	}
	b.Failure(errors.New("one"))
	if err := b.Allow(); err != nil {
		t.Fatalf("one failure should still allow: %v", err)
	}
	b.Failure(errors.New("two"))
	if err := b.Allow(); !errors.Is(err, ErrBreakerOpen) {
		t.Fatalf("threshold failure should open breaker, got %v", err)
	}

	now = now.Add(time.Minute + time.Second)
	if err := b.Allow(); err != nil {
		t.Fatalf("breaker should allow after cooldown: %v", err)
	}
	b.Success()
	if err := b.Allow(); err != nil {
		t.Fatalf("success should close breaker: %v", err)
	}
}

// TestNewDefaultBreakerUsesPolicyDefaults pins the single source of truth for the
// breaker policy (B-05): the shared Runner breaker and the agent fallback both build
// via NewDefaultBreaker so the threshold/cooldown live in one place. It opens after
// exactly DefaultBreakerThreshold consecutive failures.
func TestNewDefaultBreakerUsesPolicyDefaults(t *testing.T) {
	b := NewDefaultBreaker()
	for i := range DefaultBreakerThreshold - 1 {
		b.Failure(errors.New("transient"))
		if err := b.Allow(); err != nil {
			t.Fatalf("failure %d/%d must keep the breaker closed, got %v", i+1, DefaultBreakerThreshold, err)
		}
	}
	b.Failure(errors.New("threshold"))
	if err := b.Allow(); !errors.Is(err, ErrBreakerOpen) {
		t.Fatalf("the %dth failure must open the breaker, got %v", DefaultBreakerThreshold, err)
	}
}
