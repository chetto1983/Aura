package arcadedb

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"net/http"
	"time"
)

// maxWriteConflictRetries bounds the retry loops below. ArcadeDB's own error
// for a page-level write conflict says "Please retry the operation" -- these
// helpers are that retry, kept unconditional and lock-free (no cross-
// statement transaction, no per-identity serializer) so N concurrent workers
// stay fully concurrent; only a losing attempt pays a few extra round trips.
// Extracted into their own file rather than grown inline in memory.go
// (already at 580/600 LOC -- CLAUDE.md's NO GOD CLASS), mirroring
// fact_authority.go's precedent of one narrow concern per file. Sized
// generously (not the minimum that happened to pass once): measured live
// with 8-way contention on one row (this phase's concurrent fan-out test),
// a low retry ceiling combined with a non-jittered backoff occasionally
// exhausted every attempt with 1 of 8 sources still unmerged.
const maxWriteConflictRetries = 10

// isTransientWriteConflict reports whether err is ArcadeDB's own signal that
// a concurrent writer raced this one and the caller should retry -- observed
// live under this phase's concurrent fan-out test in TWO distinct shapes,
// both meaning "my write did not take effect because someone else's did":
//
//   - 503 "Slot rebase not possible on page ... (concurrent change to the
//     same record). Please retry the operation" -- N goroutines creating
//     DIFFERENT edges from the SAME subject vertex at once
//     (TestConcurrentWorkerFactWriteDistinctActorsProduceDistinctFacts)
//   - 409 "Duplicated key [...] found on index 'FACT[fact_key]'" -- N
//     goroutines writing the SAME content (same factKey) at once, where
//     attachFactSource's own read-then-decide SELECT ran before the winner's
//     CREATE EDGE was yet visible, so BOTH judged "not found" and both
//     attempted CREATE (TestConcurrentWorkerFactWriteSameContentMergesIntoOneFact)
//
// Both are D-09's OWN scenario, not a new invariant: the row this call wants
// now genuinely exists, so a fresh attachFactSource read after a short
// backoff resolves it into a merge rather than a lost write. This is
// distinct from a 409 on the ENTITY unique index, which upsertEntityWithRetry
// handles separately with its own unconditional single retry.
func isTransientWriteConflict(err error) bool {
	var serverErr *ServerError
	if !errors.As(err, &serverErr) {
		return false
	}
	return serverErr.Status == http.StatusServiceUnavailable || serverErr.Status == http.StatusConflict
}

// writeConflictBackoff is "full jitter" (a random duration between 0 and a
// ceiling that grows with attempt), not a fixed delay. Measured live: N
// goroutines that started retrying at the same instant and share a fixed,
// non-jittered backoff formula wake up and retry at the SAME wall-clock
// offsets round after round, so they keep colliding with each other in
// lockstep instead of fanning out -- exactly the AWS architecture blog's
// well-documented "backoff without jitter" failure mode for optimistic
// retries against shared state.
func writeConflictBackoff(attempt int) time.Duration {
	ceiling := time.Duration(attempt) * 5 * time.Millisecond
	// #nosec G404 -- timing jitter for a retry backoff, not a secret or a
	// security decision; crypto/rand buys nothing here and only adds a
	// syscall to a hot retry path.
	return time.Duration(rand.Int64N(int64(ceiling) + 1))
}

// upsertEntityWithRetry retries an entity UPSERT statement exactly once on
// failure.
//
// Measured live under this phase's concurrent fan-out test (51-04,
// TestConcurrentWorkerFactWriteSameContentMergesIntoOneFact): N goroutines
// upserting the SAME not-yet-existing Entity name at once can all read "no
// match" before any of them commits, so more than one attempts the insert
// branch of UPDATE ... UPSERT, and every loser gets "http 409: Duplicated
// key ... found on index 'Entity[name]'" instead of the update it asked
// for -- ArcadeDB's UPSERT is not atomic across concurrent connections the
// way its single-statement syntax suggests. By the time this happens the
// row DOES exist (the winner created it), so the exact same statement
// retried once takes the update branch and succeeds. Same "optimistic
// write, recover once" shape createFactWithRetry uses below for the FACT
// edge instead of the Entity vertex -- this is not a new pattern, it is the
// same one applied one step earlier.
func (c *Client) upsertEntityWithRetry(ctx context.Context, statement string, params map[string]any) ([]map[string]any, error) {
	rows, err := c.Command(ctx, statement, params)
	if err == nil {
		return rows, nil
	}
	return c.Command(ctx, statement, params)
}

// createFactWithRetry attempts to create the FACT edge, falls back to
// attachFactSource on any failure (a peer may have already created this
// exact factKey -- D-09's existing dedup path, unchanged), and retries the
// whole create-or-attach sequence a bounded number of times when the
// failure is ArcadeDB's own transient-conflict signal rather than a
// permanent one. A permanent error (bad SQL, a genuine constraint the
// attach fallback also cannot resolve) returns immediately -- retrying that
// would just waste round trips on an error that cannot change.
func (c *Client) createFactWithRetry(
	ctx context.Context,
	statement string,
	params map[string]any,
	factKey string,
	source FactSource,
	now time.Time,
) error {
	var lastErr error
	for attempt := 0; attempt <= maxWriteConflictRetries; attempt++ {
		_, err := c.Command(ctx, statement, params)
		if err == nil {
			return nil
		}
		if attached, attachErr := c.attachFactSource(ctx, factKey, source, now); attachErr == nil && attached {
			return nil
		}
		lastErr = err
		if !isTransientWriteConflict(err) || attempt == maxWriteConflictRetries {
			return fmt.Errorf("arcadedb: create fact: %w", lastErr)
		}
		time.Sleep(writeConflictBackoff(attempt + 1))
	}
	return fmt.Errorf("arcadedb: create fact: %w", lastErr)
}
