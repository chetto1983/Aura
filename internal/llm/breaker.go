package llm

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

// ErrBreakerOpen marks calls rejected while the LLM circuit breaker is cooling down.
var ErrBreakerOpen = errors.New("llm breaker open")

// Breaker tracks consecutive retryable LLM failures and opens for a cooldown after
// the configured threshold is reached.
type Breaker struct {
	mu        sync.Mutex
	threshold int
	failures  int
	cooldown  time.Duration
	openUntil time.Time
	now       func() time.Time
}

// NewBreaker returns a circuit breaker with sane defaults for invalid threshold or
// cooldown values.
func NewBreaker(threshold int, cooldown time.Duration) *Breaker {
	if threshold <= 0 {
		threshold = 3
	}
	if cooldown <= 0 {
		cooldown = 30 * time.Second
	}
	return &Breaker{
		threshold: threshold,
		cooldown:  cooldown,
		now:       time.Now,
	}
}

// Allow returns nil when a request may proceed, or ErrBreakerOpen while the
// breaker is still cooling down.
func (b *Breaker) Allow() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.openUntil.IsZero() || !b.now().Before(b.openUntil) {
		return nil
	}
	return fmt.Errorf("%w until %s", ErrBreakerOpen, b.openUntil.UTC().Format(time.RFC3339))
}

// Success records a successful request and closes the breaker.
func (b *Breaker) Success() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.failures = 0
	b.openUntil = time.Time{}
}

// Failure records a failed request and opens the breaker when the threshold is met.
func (b *Breaker) Failure(error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.failures++
	if b.failures >= b.threshold {
		b.openUntil = b.now().Add(b.cooldown)
	}
}
