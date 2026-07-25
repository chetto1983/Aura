package adaptive

import (
	"errors"
	"time"
)

// EvidenceLook is one frozen UTC cutoff and exact alpha spend.
type EvidenceLook struct {
	Number          int
	Cutoff          time.Time
	Alpha           ExactRational
	CumulativeAlpha ExactRational
	PredecessorRule string
}

// NewFiveLookPlan validates and constructs the only v2 look schedule.
func NewFiveLookPlan(cutoffs []time.Time) ([]EvidenceLook, error) {
	if len(cutoffs) != 5 {
		return nil, errors.New("v2 evidence requires exactly five cutoffs")
	}
	alphas := []ExactRational{
		MustExactRational(1, 200),
		MustExactRational(1, 200),
		MustExactRational(1, 100),
		MustExactRational(1, 100),
		MustExactRational(1, 50),
	}
	cumulative := []ExactRational{
		MustExactRational(1, 200),
		MustExactRational(1, 100),
		MustExactRational(1, 50),
		MustExactRational(3, 100),
		MustExactRational(1, 20),
	}
	looks := make([]EvidenceLook, len(cutoffs))
	for index, cutoff := range cutoffs {
		if cutoff.Location() != time.UTC || !cutoff.Equal(cutoff.Truncate(time.Microsecond)) {
			return nil, errors.New("look cutoff must be canonical UTC microsecond time")
		}
		if index > 0 && !cutoffs[index-1].Before(cutoff) {
			return nil, errors.New("look cutoffs must be strictly increasing")
		}
		predecessorRule := "immediate_continue"
		if index == 0 {
			predecessorRule = "none"
		}
		looks[index] = EvidenceLook{
			Number:          index + 1,
			Cutoff:          cutoff,
			Alpha:           alphas[index],
			CumulativeAlpha: cumulative[index],
			PredecessorRule: predecessorRule,
		}
	}
	return looks, nil
}
