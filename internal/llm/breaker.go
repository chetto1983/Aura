package llm

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

// ErrBreakerOpen marks calls rejected while the LLM circuit breaker is cooling down.
var ErrBreakerOpen = errors.New("llm breaker open")

// DefaultBreakerThreshold / DefaultBreakerCooldown are the single source of truth for
// the LLM circuit-breaker policy. The Runner mints ONE breaker with these (B-05,
// process-lifetime, shared across turns) and the agent's standalone fallback uses the
// same values, so the policy is never spelled twice.
const (
	DefaultBreakerThreshold = 3
	DefaultBreakerCooldown  = 30 * time.Second
)

// NewDefaultBreaker builds a breaker with the default policy (DefaultBreakerThreshold
// consecutive failures open it for DefaultBreakerCooldown).
func NewDefaultBreaker() *Breaker {
	return NewBreaker(DefaultBreakerThreshold, DefaultBreakerCooldown)
}

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
