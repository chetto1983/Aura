package web

import (
	"context"
	"sync"
)

// perHostLimit caps in-flight fetches to a single host (D-36). A small fixed bound
// is enough for a single-user agent: it avoids hammering one origin, it is not an
// RPM rate limiter.
const perHostLimit = 2

// hostThrottle bounds concurrent fetches per host. Each host gets a buffered
// semaphore channel of perHostLimit tokens, created lazily under the mutex; the
// fetcher holds a token for the duration of one fetch (D-36, no RPM machinery).
type hostThrottle struct {
	mu sync.Mutex
	m  map[string]chan struct{}
}

func newHostThrottle() *hostThrottle {
	return &hostThrottle{m: make(map[string]chan struct{})}
}

// sem returns the host's semaphore, creating it on first use.
func (h *hostThrottle) sem(host string) chan struct{} {
	h.mu.Lock()
	defer h.mu.Unlock()
	s, ok := h.m[host]
	if !ok {
		s = make(chan struct{}, perHostLimit)
		h.m[host] = s
	}
	return s
}

// acquire blocks until a token is free for host or ctx is done. It returns a
// release func and ok=true on success; on ctx cancellation ok=false and the
// (no-op) release must not be deferred-called against a token that was never taken.
func (h *hostThrottle) acquire(ctx context.Context, host string) (release func(), ok bool) {
	s := h.sem(host)
	select {
	case s <- struct{}{}:
		return func() { <-s }, true
	case <-ctx.Done():
		return func() {}, false
	}
}
