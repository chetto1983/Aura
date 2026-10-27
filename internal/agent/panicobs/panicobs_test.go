package panicobs

import (
	"expvar"
	"testing"
)

// recorded reads the published aura_agent_panic_total expvar for site, 0 when the site
// has never been recorded.
func recorded(site string) int64 {
	if v, ok := expRecoveredPanics.Get(site).(*expvar.Int); ok {
		return v.Value()
	}
	return 0
}

// TestRecord proves Record increments the per-site recovered-panic counter, and that an
// unrecognized site folds into the bounded "unknown" bucket (cardinality guard).
func TestRecord(t *testing.T) {
	before := recorded(SiteExecuteBatch)
	Record(SiteExecuteBatch)
	Record(SiteExecuteBatch)
	if got := recorded(SiteExecuteBatch); got != before+2 {
		t.Fatalf("recorded(%s) = %d, want %d", SiteExecuteBatch, got, before+2)
	}

	// "not-a-real-site" and its space-padded variant land in the one unknown bucket.
	u0 := recorded(siteUnknown)
	Record("  not-a-real-site  ")
	if got := recorded(siteUnknown); got != u0+1 {
		t.Fatalf("unknown-site count = %d, want %d (all unknown sites share one bucket)", got, u0+1)
	}
	if got := recorded("not-a-real-site"); got != 0 {
		t.Fatalf("an unknown site got its own label (%d): cardinality is unbounded", got)
	}
}
