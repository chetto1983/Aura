package adaptive

import (
	"testing"
	"time"
)

func TestNewFiveLookPlan_FreezesUTCAlphasAndPredecessors(t *testing.T) {
	cutoffs := make([]time.Time, 5)
	for index := range cutoffs {
		cutoffs[index] = time.Date(2026, 8, index+1, 12, 0, 0, 0, time.UTC)
	}
	looks, err := NewFiveLookPlan(cutoffs)
	if err != nil {
		t.Fatalf("NewFiveLookPlan() error = %v", err)
	}
	wantAlpha := []ExactRational{
		MustExactRational(1, 200),
		MustExactRational(1, 200),
		MustExactRational(1, 100),
		MustExactRational(1, 100),
		MustExactRational(1, 50),
	}
	wantCumulative := []ExactRational{
		MustExactRational(1, 200),
		MustExactRational(1, 100),
		MustExactRational(1, 50),
		MustExactRational(3, 100),
		MustExactRational(1, 20),
	}
	for index, look := range looks {
		if look.Number != index+1 ||
			look.Alpha.Cmp(wantAlpha[index]) != 0 ||
			look.CumulativeAlpha.Cmp(wantCumulative[index]) != 0 {
			t.Fatalf("look[%d] = %#v", index, look)
		}
		wantRule := "immediate_continue"
		if index == 0 {
			wantRule = "none"
		}
		if look.PredecessorRule != wantRule {
			t.Fatalf("look[%d] predecessor = %q", index, look.PredecessorRule)
		}
	}
}

func TestNewFiveLookPlan_RejectsEqualityAndNonMicrosecondCutoffs(t *testing.T) {
	cutoff := time.Date(2026, 8, 1, 12, 0, 0, 0, time.UTC)
	if _, err := NewFiveLookPlan([]time.Time{cutoff, cutoff, cutoff, cutoff, cutoff}); err == nil {
		t.Fatal("NewFiveLookPlan() accepted equal cutoffs")
	}
	cutoffs := []time.Time{
		cutoff,
		cutoff.Add(time.Second),
		cutoff.Add(2 * time.Second),
		cutoff.Add(3 * time.Second),
		cutoff.Add(4*time.Second + time.Nanosecond),
	}
	if _, err := NewFiveLookPlan(cutoffs); err == nil {
		t.Fatal("NewFiveLookPlan() accepted a non-microsecond cutoff")
	}
}
