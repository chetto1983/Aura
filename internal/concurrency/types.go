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
}

// Config holds all UserGate configuration per D-16.
type Config struct {
	// InboxSize is the buffered channel capacity per user. Default: 8.
	InboxSize int

	// EvictionThreshold is the duration of inactivity after which a user's actor is evicted. Default: 30min.
	EvictionThreshold time.Duration

	// SweepInterval is how often the InactivityTracker checks for stale users. Default: 60s.
	SweepInterval time.Duration

	// OnEvict is called when a user is evicted. Called from the InactivityTracker sweeper goroutine.
	// The UserGate has already cancelled the actor context and removed the user from internal maps.
	// The callback should persist conversation state and clean up external resources.
	OnEvict func(userID string)

	// OnOverflow is called when the inbox is full and the oldest entry is dropped.
	// Called from the Acquire call path (which runs in onMessage's goroutine, not the actor goroutine).
	// The callback should send a Telegram notice to the user per D-03.
	OnOverflow func(userID string)
}

// DefaultConfig returns a Config with defaults matching decisions.
func DefaultConfig() Config {
	return Config{
		InboxSize:         8,
		EvictionThreshold: 30 * time.Minute,
		SweepInterval:     60 * time.Second,
	}
}
