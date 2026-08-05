package documents

import (
	"testing"
	"time"
)

func TestExponentialRetryCapIsBounded(t *testing.T) {
	if got := exponentialRetryCap(time.Minute, 1); got != time.Minute {
		t.Fatalf("attempt 1 cap = %s", got)
	}
	if got := exponentialRetryCap(time.Minute, 3); got != 4*time.Minute {
		t.Fatalf("attempt 3 cap = %s", got)
	}
	if got := exponentialRetryCap(time.Minute, 100); got != maximumRetryBackoff {
		t.Fatalf("bounded cap = %s", got)
	}
}

func TestFullJitterStaysWithinCap(t *testing.T) {
	const cap = 25 * time.Millisecond
	for range 100 {
		got := fullJitter(cap)
		if got < 0 || got > cap {
			t.Fatalf("jitter = %s, cap = %s", got, cap)
		}
	}
}
