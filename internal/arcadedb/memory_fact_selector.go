package arcadedb

// Which ONE fact a provenance attach lands on.
//
// A fact that is still valid is named by its `fact_key`, and that is the whole story for
// almost every write. A fact written ALREADY CLOSED has no key to be named by: the
// `FACT[fact_key]` index is UNIQUE, so the key can only ever belong to the currently-valid
// version, which is why activeFactKey stores nil for a fact whose valid_to is already past
// and why closing a fact nulls it.
//
// The consequence was a silent duplicate. `attachFactSource` looked a fact up by key, so
// for a historical write it always found nothing and always created another edge. Measured
// live on 2026-09-03: the same closed fact written twice -- identical subject, predicate,
// object, statement and window, differing only in its memory ids -- produced TWO edges, and
// `memory_facts_about` with `as_of` inside the window returned the same fact twice. The
// documented behaviour is the opposite: memory_batch's own upsert_fact says it "adds a fact
// or enriches an exact duplicate's provenance", and that is what the active path does.
//
// Re-running an import of historical records is an ordinary operation, and it multiplied
// rows without bound while splitting each fact's provenance across the copies -- the exact
// fragmentation the provenance merge exists to prevent.
//
// So the lookup becomes a parameter. The active selector keeps the key; the historical one
// matches on the fact's own content and window, which is the only identity a keyless row
// has. Everything downstream -- the transaction, the conflict retry, the source merge --
// is shared, because the two differ ONLY in how the row is found.

import (
	"maps"
	"time"
)

// factSelector names the single fact a provenance attach should land on.
type factSelector struct {
	// release frees an identity that must not block the lookup. Empty when there is
	// none to free.
	release string
	lookup  string
	params  map[string]any
	// label names the target in the give-up error, since a caller cannot read the
	// clause above from a message.
	label string
}

// bind completes the parameters with the instant the attach is running at.
func (s factSelector) bind(now time.Time) map[string]any {
	params := make(map[string]any, len(s.params)+1)
	maps.Copy(params, s.params)
	params["now"] = now.UTC().Format(time.RFC3339Nano)
	return params
}

// activeFactSelector names the still-valid fact holding this key, first releasing the key
// from a row that has since expired so it cannot mask the live one.
func activeFactSelector(factKey string) factSelector {
	return factSelector{
		release: "UPDATE " + factEdgeType + " SET fact_key = NULL WHERE fact_key = :fact_key AND " +
			"(expired_at IS NOT NULL OR (valid_to IS NOT NULL AND valid_to <= :now))",
		lookup: "fact_key = :fact_key AND expired_at IS NULL AND " +
			"(valid_to IS NULL OR valid_to > :now)",
		params: map[string]any{"fact_key": factKey},
		label:  "fact_key " + factKey,
	}
}

// historicalFactSelector names an already-closed fact by its content and its exact window.
//
// The window is part of the match, not a detail: two facts that say the same thing about
// different periods are different facts, and merging them would erase the distinction the
// bitemporal model exists to keep. Everything compared here is what the caller supplied, so
// a re-run of the same import matches and a genuinely different record does not.
func historicalFactSelector(fact Fact, validFrom, validTo time.Time) factSelector {
	return factSelector{
		lookup: "fact_key IS NULL AND expired_at IS NULL" +
			" AND predicate = :predicate AND statement = :statement" +
			" AND valid_from = " + rfc3339Instant("valid_from") +
			" AND valid_to = " + rfc3339Instant("valid_to") +
			" AND outV().name = :subject_name AND inV().name = :object_name",
		params: map[string]any{
			"predicate":    fact.Predicate,
			"statement":    fact.Statement,
			"valid_from":   validFrom.UTC().Format(time.RFC3339),
			"valid_to":     validTo.UTC().Format(time.RFC3339),
			"subject_name": fact.Subject,
			"object_name":  fact.Object,
		},
		label: "closed fact " + fact.Subject + " " + fact.Predicate + " " + fact.Object,
	}
}

// rfc3339Instant converts a bound RFC3339 parameter to a DATETIME for comparison.
//
// The conversion is not decoration. ArcadeDB coerces an RFC3339 string for the RANGE
// operators -- which is why the as_of filter works -- but NOT for equality: measured on
// 26.9.1 against the same two rows, `valid_from = "2025-01-01T00:00:00Z"` matched zero and
// `valid_from = "2025-01-01 00:00:00"` matched both, while `valid_from <= "2026-12-31T…Z"`
// matched every row correctly.
//
// The manual says why (arcadedb-docs, reference/managing-dates.adoc): the datetime format
// is `yyyy-MM-dd HH:mm:ss` BY DEFAULT and is changed with `ALTER DATABASE DATETIMEFORMAT`,
// while `date()` "converts dates to and from strings and dates, also uses custom formats".
// So writing the default format here would work today and break silently the day a
// database sets its own; naming the format at the call site cannot.
func rfc3339Instant(param string) string {
	return `date(:` + param + `, "yyyy-MM-dd'T'HH:mm:ss'Z'")`
}
