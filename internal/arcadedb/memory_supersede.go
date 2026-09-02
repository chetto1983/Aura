package arcadedb

// The supersede concern: closing the fact(s) a correction names. Split out
// of memory.go, which sat at 495/600 LOC before this phase's three
// decisions (D-15/D-16/D-18) landed -- the same reasoning
// memory_provenance.go already applies in this directory to keep no single
// file approaching the 600-LOC no-god-class cap (CLAUDE.md).

import (
	"context"
	"fmt"
	"time"
)

// closeSupersededStatement ends the validity window of every still-valid fact
// with the same subject and predicate. It does not delete: the row stays
// queryable through `as_of`.
//
// The object is deliberately NOT in the WHERE clause -- the object is the thing
// that changed, so matching on it would mean the statement could never fire.
//
// Endpoints are read with outV()/inV(). The dotted `out.name` form returns NULL
// on an edge rather than failing, so getting this wrong is silent.
const closeSupersededStatement = "UPDATE " + factEdgeType + " SET valid_to = :valid_to, " +
	"expired_at = :expired_at, fact_key = NULL WHERE predicate = :predicate AND expired_at IS NULL " +
	"AND (valid_to IS NULL OR valid_to > :valid_to) " +
	"AND fact_key <> :fact_key AND outV().name = :subject_name"

// closeFactByKeyStatement ends the validity window of exactly the one fact
// named by target_fact_key (D-15). closeSupersededStatement's own
// `fact_key <> :fact_key` clause is self-exclusion -- never close the fact
// this call is about to create -- not a positive filter: it cannot select a
// target, so this is a new statement rather than a parameterization of the
// old one.
const closeFactByKeyStatement = "UPDATE " + factEdgeType + " SET valid_to = :valid_to, " +
	"expired_at = :expired_at, fact_key = NULL " +
	"WHERE fact_key = :target_fact_key AND expired_at IS NULL"

// SupersedeOutcome is the result of resolving what a Supersedes:true
// correction closes. Refused covers both entry points this package offers:
// an explicit TargetFactKey matching no still-valid fact (always 0-or-1,
// never ambiguous, since fact_key is UNIQUE-indexed), and the legacy
// subject+predicate path matching 0 or more than 1 distinct fact. In every
// refused case Closed is 0 and nothing was written -- the caller (UpsertFact)
// does not create the new fact either, so a refusal never adds to the
// ambiguity it could not resolve.
type SupersedeOutcome struct {
	Closed     int
	Refused    bool
	Reason     string
	Candidates []FactHit
}

// closeSuperseded resolves and closes the fact(s) fact.Supersedes names.
// fact.TargetFactKey set -> exact-match close (D-15). Empty -> the legacy
// subject+predicate path: resolve the candidate set first, then refuse on 0
// or >1 distinct matches, close on exactly 1 (D-16). Ambiguity is never
// resolved by guessing -- no recency tie-break, no ranking heuristic, no
// cardinality registry (Pitfall 6).
func (c *Client) closeSuperseded(
	ctx context.Context,
	fact Fact,
	factKey string,
	validFrom, now time.Time,
) (SupersedeOutcome, error) {
	if fact.TargetFactKey != "" {
		return c.closeByFactKey(ctx, fact.TargetFactKey, validFrom, now)
	}
	return c.closeByCandidateResolution(ctx, fact, factKey, validFrom, now)
}

// closeByCandidateResolution is the legacy path's ambiguity contract
// (D-16), replayed from F-2: 0 candidates refuses (nothing to supersede);
// exactly 1 closes it, behaviour identical to before D-16 for the
// single-valued case; more than 1 distinct candidate refuses with previews,
// naming supersedes_fact_key (Task 1's exact-match close) as the
// disambiguation path. "Distinct" means a different fact_key -- candidates
// are already one row per edge, so the count IS the distinct count.
func (c *Client) closeByCandidateResolution(
	ctx context.Context,
	fact Fact,
	factKey string,
	validFrom, now time.Time,
) (SupersedeOutcome, error) {
	candidates, err := c.candidatesForSupersede(ctx, fact.Subject, fact.Predicate, now)
	if err != nil {
		return SupersedeOutcome{}, err
	}
	switch len(candidates) {
	case 0:
		return SupersedeOutcome{
			Refused: true,
			Reason: fmt.Sprintf(
				"no still-valid fact matches subject %q and predicate %q; nothing to supersede",
				fact.Subject, fact.Predicate,
			),
			Candidates: candidates,
		}, nil
	case 1:
		// Fall through: exactly one target, close it below.
	default:
		return SupersedeOutcome{
			Refused: true,
			Reason: fmt.Sprintf(
				"%d facts match subject %q and predicate %q -- name one with supersedes_fact_key",
				len(candidates), fact.Subject, fact.Predicate,
			),
			Candidates: candidates,
		}, nil
	}
	rows, err := c.Command(ctx, closeSupersededStatement, map[string]any{
		"valid_to":     validFrom.UTC().Format(time.RFC3339),
		"expired_at":   now.UTC().Format(time.RFC3339),
		"predicate":    fact.Predicate,
		"subject_name": fact.Subject,
		"fact_key":     factKey,
	})
	if err != nil {
		return SupersedeOutcome{}, fmt.Errorf("arcadedb: close superseded fact: %w", err)
	}
	return SupersedeOutcome{Closed: countUpdated(rows)}, nil
}

// candidatesForSupersede resolves the still-valid facts an ambiguous
// Supersedes:true would consider. It reuses FactsAbout -- the read path
// Task 1's fact_key already rides through, factHitFromRow and all -- rather
// than a third query shape, then narrows client-side to the hits where
// subject is the edge's SOURCE: FactsAbout's WHERE matches either endpoint,
// but closeSupersededStatement only ever closes a fact where the named
// subject is outV(), and the candidate set must match exactly what a close
// would touch.
func (c *Client) candidatesForSupersede(
	ctx context.Context,
	subject, predicate string,
	asOf time.Time,
) ([]FactHit, error) {
	hits, err := c.FactsAbout(ctx, subject, predicate, c.memoryLimits().Results, asOf, factsAboutDirect)
	if err != nil {
		return nil, fmt.Errorf("arcadedb: resolve supersede candidates: %w", err)
	}
	candidates := make([]FactHit, 0, len(hits))
	for _, hit := range hits {
		if hit.Subject == subject {
			candidates = append(candidates, hit)
		}
	}
	return candidates, nil
}

// closeByFactKey closes exactly the one fact targetFactKey names. 0 rows
// updated is a refusal (D-15's must_haves truth: "a fact_key naming no
// still-valid fact is the 0-match case and refuses ... never a silent
// no-op success"), never a fallback to the broad match.
func (c *Client) closeByFactKey(
	ctx context.Context,
	targetFactKey string,
	validFrom, now time.Time,
) (SupersedeOutcome, error) {
	rows, err := c.Command(ctx, closeFactByKeyStatement, map[string]any{
		"valid_to":        validFrom.UTC().Format(time.RFC3339),
		"expired_at":      now.UTC().Format(time.RFC3339),
		"target_fact_key": targetFactKey,
	})
	if err != nil {
		return SupersedeOutcome{}, fmt.Errorf("arcadedb: close fact by key: %w", err)
	}
	if closed := countUpdated(rows); closed > 0 {
		return SupersedeOutcome{Closed: closed}, nil
	}
	return SupersedeOutcome{
		Refused: true,
		Reason:  fmt.Sprintf("fact_key %q does not name a still-valid fact", targetFactKey),
	}, nil
}
