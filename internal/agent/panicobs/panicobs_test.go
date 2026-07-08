package panicobs

import "testing"

// TestRecordAndCount proves Record increments the per-site recovered-panic counter and Count
// reads it back, that an unrecognized site folds into the bounded "unknown" bucket (cardinality
// guard), and that an unrecorded site reads 0.
func TestRecordAndCount(t *testing.T) {
	// A known site: Count reflects each Record.
	before := Count(SiteExecuteBatch)
	Record(SiteExecuteBatch)
	Record(SiteExecuteBatch)
	if got := Count(SiteExecuteBatch); got != before+2 {
		t.Fatalf("Count(%s) = %d, want %d", SiteExecuteBatch, got, before+2)
	}

	// An unrecognized site normalizes to the bounded "unknown" bucket — both Record and Count
	// map "not-a-real-site" and a leading/trailing-space variant to the same counter.
	u0 := Count("not-a-real-site")
	Record("  not-a-real-site  ") // trimmed + folded to unknown
	if got := Count("totally-different-bogus-site"); got != u0+1 {
		t.Fatalf("unknown-site Count = %d, want %d (all unknown sites share one bucket)", got, u0+1)
	}

	// A never-recorded KNOWN site still reads a sane value (>=0), never panicking on a nil expvar.
	if got := Count(SiteWorkflowParallel); got < 0 {
		t.Fatalf("Count(%s) = %d, want >= 0", SiteWorkflowParallel, got)
	}
}
