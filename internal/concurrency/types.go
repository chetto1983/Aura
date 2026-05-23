package concurrency

import (
	"context"
	"time"
)

// Entry is a uniform inbox entry type per D-08.
// No kind discriminator -- notifications and user messages use the same struct.
// Process is called by the per-user actor goroutine with the actor's context.
type Entry struct {
	Process func(ctx context.Context)

	// startedCh is set internally by UserGate.Acquire when QueueNoticeAfter > 0
	// and OnQueueNotice != nil. It is closed by runActor immediately before
	// invoking Process. The Acquire-spawned timer goroutine selects on this
	// channel to cancel the queue-notice fire if processing started in time.
	//
	// Unexported -- callers MUST leave this as the zero value when constructing
	// Entry literals; the gate manages the lifetime.
	startedCh chan struct{}
}

// Config holds all UserGate configuration per D-16.
type Config struct {
	// InboxSize is the buffered channel capacity per user. Default: 8.
	InboxSize int

	// EvictionThreshold is the duration of inactivity after which a user's actor is evicted. Default: 30min.
	EvictionThreshold time.Duration

	// SweepInterval is how often the InactivityTracker checks for stale users. Default: 60s.
	SweepInterval time.Duration

	// QueueNoticeAfter is the duration to wait after enqueueing a user message
	// before firing OnQueueNotice. If <= 0, the feature is disabled (no timer
	// goroutine is spawned, no notice is fired). When > 0 and OnQueueNotice is
	// non-nil, Acquire spawns a per-entry timer goroutine; the timer fires
	// OnQueueNotice(userID) iff the entry has not begun processing within
	// QueueNoticeAfter. TryAcquire does NOT spawn the timer (notifications
	// drop on overflow rather than queue-with-notice).
	QueueNoticeAfter time.Duration

	// OnEvict is called when a user is evicted. Called from the InactivityTracker sweeper goroutine.
	// The UserGate has already cancelled the actor context and removed the user from internal maps.
	// The callback should persist conversation state and clean up external resources.
	OnEvict func(userID string)

	// OnOverflow is called when the inbox is full and the oldest entry is dropped.
	// Called from the Acquire call path (which runs in onMessage's goroutine, not the actor goroutine).
	// The callback should send a Telegram notice to the user per D-03.
	OnOverflow func(userID string)

	// OnQueueNotice is called once per Acquire-enqueued entry that has waited
	// longer than QueueNoticeAfter without beginning processing. Called from a
	// gate-spawned timer goroutine. The callback MUST NOT block the gate; it
	// should hand off to a separate goroutine for any external I/O (Pitfall 4).
	OnQueueNotice func(userID string)
}
