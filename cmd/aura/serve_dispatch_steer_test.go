package main

import "testing"

// TestSchedulerSteerOrNilKeepsANilStoreNil: without the guard a nil store reaches cron as a
// non-nil interface and the first agent_job record panics on Push.
func TestSchedulerSteerOrNilKeepsANilStoreNil(t *testing.T) {
	t.Parallel()
	if got := schedulerSteerOrNil(nil); got != nil {
		t.Fatalf("schedulerSteerOrNil(nil) = %#v, want a nil interface", got)
	}
}
