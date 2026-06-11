package channels

import "context"

// Deliverer is an OPTIONAL channel capability: a started Channel MAY also
// implement it. The Registry runtime-asserts ch.(Deliverer) and skips a channel
// that does not — zero registry change for a future channel.
//
// The tri-state return is load-bearing — the dispatch precedence (Phase 20 R4,
// plan 20-03) and the Registry fan-out (R2) both depend on it:
//
//	(false, nil) = not my user      → try the next channel
//	(true,  nil) = delivered        → stop
//	(false, err) = owns-but-failed  → stop, do NOT try siblings
//
// (false, err) must NOT be read as "try the next channel": that would let one
// identity's message leak to a second channel on a transient error (double-
// delivery). The contract travels with the type so every consumer honors it.
type Deliverer interface {
	Deliver(ctx context.Context, identityID, text string) (delivered bool, err error)
}
