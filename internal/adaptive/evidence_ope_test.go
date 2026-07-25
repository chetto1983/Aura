package adaptive

import (
	"encoding/hex"
	"testing"
)

func TestClusterSampler_MatchesFrozenZeroSeedVector(t *testing.T) {
	var seed [32]byte
	got, err := SampleClusterIndices(seed, 3, 12)
	if err != nil {
		t.Fatalf("SampleClusterIndices() error = %v", err)
	}
	want := []int{0, 0, 0, 2, 2, 1, 1, 0, 2, 1, 0, 2}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("indices = %v, want %v", got, want)
		}
	}
	if hex.EncodeToString(seed[:]) != "0000000000000000000000000000000000000000000000000000000000000000" {
		t.Fatal("seed mutated")
	}
}

func TestOutcomeDisposition_AppliesStrictQualityAndLookFiveTermination(t *testing.T) {
	tests := []struct {
		name     string
		input    DispositionInput
		want     OutcomeDisposition
		eligible bool
		write    bool
	}{
		{
			name: "quality equality continues",
			input: DispositionInput{
				LookNumber: 1, GateValid: true,
				QualityLower:         MustExactRational(1, 10),
				MinimumQualityUplift: MustExactRational(1, 10),
				HarmUpper:            MustExactRational(0, 1),
				MaximumHarmIncrease:  MustExactRational(0, 1),
			},
			want: OutcomeDispositionContinue, eligible: true, write: true,
		},
		{
			name: "passing bounds promote",
			input: DispositionInput{
				LookNumber: 2, GateValid: true,
				QualityLower:         MustExactRational(11, 100),
				MinimumQualityUplift: MustExactRational(1, 10),
				HarmUpper:            MustExactRational(1, 100),
				MaximumHarmIncrease:  MustExactRational(1, 100),
			},
			want: OutcomeDispositionPromote, eligible: true, write: true,
		},
		{
			name: "look five retains",
			input: DispositionInput{
				LookNumber: 5, GateValid: true,
				QualityLower:         MustExactRational(0, 1),
				MinimumQualityUplift: MustExactRational(1, 10),
				HarmUpper:            MustExactRational(0, 1),
				MaximumHarmIncrease:  MustExactRational(1, 10),
			},
			want: OutcomeDispositionRetainCanary, eligible: true, write: true,
		},
		{
			name:  "precondition writes nothing",
			input: DispositionInput{PreconditionFailure: true},
			want:  "", eligible: false, write: false,
		},
		{
			name:  "gate poison seals ineligible",
			input: DispositionInput{LookNumber: 1, GateValid: false},
			want:  OutcomeDispositionIneligible, eligible: false, write: true,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := DecideOutcomeDisposition(test.input)
			if got.Disposition != test.want || got.Eligible != test.eligible || got.Write != test.write {
				t.Fatalf("decision = %#v", got)
			}
		})
	}
}
