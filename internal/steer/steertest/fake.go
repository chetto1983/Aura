// Package steertest is a test-only, in-process Push+Drain double for the shared steer
// interfaces (internal/agui's steerPusher, internal/channels/telegram's SteerPusher,
// agent.SteerInbox). It exists ONLY to keep the route/dispatch/runner unit tests that
// exercise a REAL queue round-trip fast and DB-less after Phase 51 plan 02 (D-06) moved
// the shipped backing store to Postgres — production code MUST NOT import this package;
// *steer.PostgresStore is the sole production implementation (D-06's "two live
// implementations of one contract" was explicitly rejected, and that verdict is about
// what cmd/aura wires, not about what a _test.go file constructs for itself).
//
// Fake reproduces the deleted in-memory internal/steer.Inbox's exact FIFO/consume-once/
// cap semantics byte-for-byte, including its sentinel errors (reused directly from
// internal/steer, never redeclared) — so a test written against the old Inbox keeps
// asserting the same observable behavior after swapping its constructor call.
package steertest

import (
	"strings"
	"sync"

	"github.com/chetto1983/aura/internal/steer"
	"github.com/google/uuid"
)

const (
	defaultMax      = 32
	defaultMaxBytes = 32768
)

// Fake is the in-memory Push+Drain double. Zero value is not usable; build one with
// New.
type Fake struct {
	mu     sync.Mutex
	byConv map[string][]steer.Message
	max    int
	maxB   int
	closed bool
}

// New builds a Fake from the resolved caps, mirroring the deleted Inbox's own New: a
// non-positive cap resolves to the package default.
func New(cfg steer.Config) *Fake {
	max, maxB := cfg.Max, cfg.MaxBytes
	if max <= 0 {
		max = defaultMax
	}
	if maxB <= 0 {
		maxB = defaultMaxBytes
	}
	return &Fake{byConv: make(map[string][]steer.Message), max: max, maxB: maxB}
}

// Push validates in order — empty/whitespace, oversize, closed, queue-full — returning
// the SAME steer.Err* sentinels the shipped PostgresStore.Push does, so a caller
// exercising the refusal ladder (e.g. writeSteerRefusal) observes identical behavior.
func (f *Fake) Push(conv, source, text string) error {
	if strings.TrimSpace(text) == "" {
		return steer.ErrEmpty
	}
	if size := len([]byte(text)); size > f.maxB {
		return steer.ErrTooLarge
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return steer.ErrClosed
	}
	if len(f.byConv[conv]) >= f.max {
		return steer.ErrQueueFull
	}
	f.byConv[conv] = append(f.byConv[conv], steer.Message{ID: uuid.NewString(), Source: source, Text: text})
	return nil
}

// Drain pops and returns everything queued for conv, atomically clearing the slot — a
// second Drain on the same conv returns empty.
func (f *Fake) Drain(conv string) []steer.Message {
	f.mu.Lock()
	defer f.mu.Unlock()
	msgs := f.byConv[conv]
	delete(f.byConv, conv)
	return msgs
}

// Close stops accepting new Push calls (which return steer.ErrClosed from then on); it
// does not discard whatever is already queued.
func (f *Fake) Close() {
	f.mu.Lock()
	f.closed = true
	f.mu.Unlock()
}
