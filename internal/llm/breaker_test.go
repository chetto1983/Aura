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
