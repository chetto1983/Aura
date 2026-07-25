package adaptive

import (
	"context"
	"errors"
	"testing"
)

func TestHypergeometricBounds_RejectsStrictTailEquality(t *testing.T) {
	bounds, err := HypergeometricBounds(
		context.Background(),
		10,
		5,
		4,
		MustExactRational(1, 42),
		DefaultExactInferenceCaps(),
	)
	if err != nil {
		t.Fatalf("HypergeometricBounds() error = %v", err)
	}
	if bounds.Lower != 5 {
		t.Fatalf("lower = %d, want 5", bounds.Lower)
	}
}

func TestHypergeometricBounds_ReturnsVacuousAndCensusVectors(t *testing.T) {
	caps := DefaultExactInferenceCaps()
	vacuous, err := HypergeometricBounds(
		context.Background(),
		4,
		0,
		0,
		MustExactRational(1, 100),
		caps,
	)
	if err != nil {
		t.Fatalf("vacuous bounds error = %v", err)
	}
	if vacuous.Lower != 0 || vacuous.Upper != 4 {
		t.Fatalf("vacuous bounds = %#v", vacuous)
	}
	census, err := HypergeometricBounds(
		context.Background(),
		4,
		4,
		3,
		MustExactRational(1, 100),
		caps,
	)
	if err != nil {
		t.Fatalf("census bounds error = %v", err)
	}
	if census.Lower != 3 || census.Upper != 3 {
		t.Fatalf("census bounds = %#v", census)
	}
}

func TestExactATEBounds_RetainsOneArmStrataWithVacuousSide(t *testing.T) {
	report, err := ExactATEBounds(
		context.Background(),
		[]BinaryATEStratum{{
			AnalysisStratumID: "one-arm",
			Population:        2,
			Treated:           2,
			TreatedSuccesses:  2,
			ControlSuccesses:  0,
		}},
		MustExactRational(1, 20),
		DefaultExactInferenceCaps(),
	)
	if err != nil {
		t.Fatalf("ExactATEBounds() error = %v", err)
	}
	if report.Strata[0].Control.Lower != 0 || report.Strata[0].Control.Upper != 2 {
		t.Fatalf("control bounds = %#v", report.Strata[0].Control)
	}
	if report.Lower.Cmp(MustExactRational(0, 1)) != 0 ||
		report.Upper.Cmp(MustExactRational(1, 1)) != 0 {
		t.Fatalf("ATE = [%s,%s], want [0,1]", report.Lower, report.Upper)
	}
}

func TestNeumaierSum_PreservesCanonicalBinary64Compensation(t *testing.T) {
	got, err := NeumaierSum([]float64{1e16, 1, -1e16})
	if err != nil {
		t.Fatalf("NeumaierSum() error = %v", err)
	}
	if got != 1 {
		t.Fatalf("sum = %v, want 1", got)
	}
}

func TestHypergeometricBounds_Stops_When_ContextIsAlreadyCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := HypergeometricBounds(
		ctx,
		4,
		0,
		0,
		MustExactRational(1, 100),
		DefaultExactInferenceCaps(),
	)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("HypergeometricBounds() error = %v, want context.Canceled", err)
	}
}

func BenchmarkExactATEBounds_ManyTinyStrata(b *testing.B) {
	strata := make([]BinaryATEStratum, 1_000)
	for index := range strata {
		strata[index] = BinaryATEStratum{
			AnalysisStratumID: string(rune(index + 1)),
			Population:        1,
			Treated:           index % 2,
			TreatedSuccesses:  index % 2,
		}
	}
	caps := DefaultExactInferenceCaps()
	alpha := MustExactRational(1, 20)
	for b.Loop() {
		if _, err := ExactATEBounds(context.Background(), strata, alpha, caps); err != nil {
			b.Fatal(err)
		}
	}
}
