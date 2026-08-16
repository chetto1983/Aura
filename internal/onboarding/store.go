package onboarding

import "context"

// OnboardingState is the 3-state gate: an operator who has never been asked, one who
// filled the form, and one who declined it. "Skipped" is not "completed" — the cockpit
// must stop asking either way, but only one of them means there are answers to read.
type OnboardingState struct {
	Completed bool
	Skipped   bool
	// Nudged records that a channel which cannot render the form has already pointed the
	// operator at it. Only channels read it; the cockpit's Required is Completed/Skipped.
	Nudged bool
}

// Store persists the submitted/skipped onboarding seed and reads the gate back. Both the
// web (agui) and Telegram paths depend on this one port; the concrete implementation is
// ProfileStore, over Postgres.
//
// It used to be called ProfileMemoryStore and its only implementation drove the
// agent-memory MCP: every answer became a bitemporal fact in the identity's ArcadeDB
// database, and "has this operator onboarded" was an MCP round trip on every page load.
// That put settings in the memory graph, where they competed for rank against everything
// the agent had actually learned, and it put the operator's timezone somewhere no code on
// the turn path could read — which is why the clock stayed UTC.
//
// The graph keeps what the agent LEARNS. This keeps what the operator DECLARED.
type Store interface {
	StoreConfirmed(ctx context.Context, identityID string, a Answers) error
	StoreSkipped(ctx context.Context, identityID string) error
	Status(ctx context.Context, identityID string) (OnboardingState, error)
	// MarkNudged records that the operator has been pointed at the form, so a channel
	// nudges once per operator rather than once per daemon restart.
	MarkNudged(ctx context.Context, identityID string) error
}
