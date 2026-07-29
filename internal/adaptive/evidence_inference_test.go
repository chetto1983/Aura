package adaptive

import (
	"context"
	"errors"
	"math"
	"math/bits"
	"slices"
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

func TestExactATEBounds_IsPermutationInvariantAndRespectsCaps(t *testing.T) {
	strata := []BinaryATEStratum{
		{AnalysisStratumID: "b", Population: 2, Treated: 1, TreatedSuccesses: 1},
		{AnalysisStratumID: "a", Population: 2, Treated: 1, ControlSuccesses: 1},
	}
	want, err := ExactATEBounds(
		context.Background(), strata, MustExactRational(1, 20), DefaultExactInferenceCaps(),
	)
	if err != nil {
		t.Fatalf("ExactATEBounds() error = %v", err)
	}
	slices.Reverse(strata)
	got, err := ExactATEBounds(
		context.Background(), strata, MustExactRational(1, 20), DefaultExactInferenceCaps(),
	)
	if err != nil {
		t.Fatalf("permuted ExactATEBounds() error = %v", err)
	}
	if got.Lower.Cmp(want.Lower) != 0 || got.Upper.Cmp(want.Upper) != 0 {
		t.Fatalf("permuted bounds = [%s,%s], want [%s,%s]", got.Lower, got.Upper, want.Lower, want.Upper)
	}
	caps := DefaultExactInferenceCaps()
	caps.MaxTailTerms = 1
	if _, err := HypergeometricBounds(
		context.Background(), 10, 5, 4, MustExactRational(1, 42), caps,
	); err == nil {
		t.Fatal("HypergeometricBounds() ignored tail-term cap")
	}
	caps = DefaultExactInferenceCaps()
	caps.MaxBigIntBits = 1
	if _, err := HypergeometricBounds(
		context.Background(), 10, 5, 4, MustExactRational(1, 42), caps,
	); err == nil {
		t.Fatal("HypergeometricBounds() ignored bigint cap")
	}
}

func TestExactInference_RejectsMalformedCountsAlphaAndStrata(t *testing.T) {
	caps := DefaultExactInferenceCaps()
	if _, err := HypergeometricBounds(
		context.Background(), 2, 3, 0, MustExactRational(1, 20), caps,
	); err == nil {
		t.Fatal("HypergeometricBounds() accepted sample above population")
	}
	if _, err := HypergeometricBounds(
		context.Background(), 2, 1, 0, MustExactRational(0, 1), caps,
	); err == nil {
		t.Fatal("HypergeometricBounds() accepted zero beta")
	}
	invalidCaps := caps
	invalidCaps.MaxMemoryBytes = 0
	if _, err := HypergeometricBounds(
		context.Background(), 2, 1, 0, MustExactRational(1, 20), invalidCaps,
	); err == nil {
		t.Fatal("HypergeometricBounds() accepted invalid caps")
	}
	if _, err := ExactATEBounds(
		context.Background(), nil, MustExactRational(1, 20), caps,
	); err == nil {
		t.Fatal("ExactATEBounds() accepted empty strata")
	}
	if _, err := ExactATEBounds(
		context.Background(),
		[]BinaryATEStratum{{AnalysisStratumID: "a", Population: 1}},
		MustExactRational(1, 1),
		caps,
	); err == nil {
		t.Fatal("ExactATEBounds() accepted invalid alpha")
	}
	if _, err := ExactATEBounds(
		context.Background(),
		[]BinaryATEStratum{
			{AnalysisStratumID: "a", Population: 1},
			{AnalysisStratumID: "a", Population: 1},
		},
		MustExactRational(1, 20),
		caps,
	); err == nil {
		t.Fatal("ExactATEBounds() accepted duplicate strata")
	}
	if _, err := ExactATEBounds(
		context.Background(),
		[]BinaryATEStratum{{AnalysisStratumID: "a", Population: 0}},
		MustExactRational(1, 20),
		caps,
	); err == nil {
		t.Fatal("ExactATEBounds() accepted zero population stratum")
	}
}

func TestExactATEBounds_ExhaustiveTinyPotentialSchedulesAreConservative(t *testing.T) {
	const population = 3
	alpha := MustExactRational(1, 2)
	caps := DefaultExactInferenceCaps()
	for treatmentPotential := range 1 << population {
		for controlPotential := range 1 << population {
			covered := 0
			for assignment := range 1 << population {
				stratum := BinaryATEStratum{
					AnalysisStratumID: "tiny", Population: population,
					Treated: bits.OnesCount(uint(assignment)),
				}
				for member := range population {
					if assignment&(1<<member) != 0 {
						if treatmentPotential&(1<<member) != 0 {
							stratum.TreatedSuccesses++
						}
					} else if controlPotential&(1<<member) != 0 {
						stratum.ControlSuccesses++
					}
				}
				report, err := ExactATEBounds(context.Background(), []BinaryATEStratum{stratum}, alpha, caps)
				if err != nil {
					t.Fatal(err)
				}
				trueATE := MustExactRational(
					int64(bits.OnesCount(uint(treatmentPotential))-bits.OnesCount(uint(controlPotential))),
					population,
				)
				if report.Lower.Cmp(trueATE) <= 0 && report.Upper.Cmp(trueATE) >= 0 {
					covered++
				}
			}
			if covered < (1<<population)/2 {
				t.Fatalf(
					"potential schedules treatment=%03b control=%03b coverage=%d/8",
					treatmentPotential, controlPotential, covered,
				)
			}
		}
	}
}

func TestExactBernoulliConditioning_IsUniformWithinRealizedTreatedCount(t *testing.T) {
	for population := 1; population <= 6; population++ {
		counts := make(map[int]int)
		for assignment := 0; assignment < 1<<population; assignment++ {
			counts[bits.OnesCount(uint(assignment))]++
		}
		for treated, count := range counts {
			want := int(newtonBinomial(population, treated))
			if count != want {
				t.Fatalf("N=%d treated=%d assignments=%d, want %d", population, treated, count, want)
			}
		}
	}
}

func TestOutwardEightDecimal_UsesDirectedRounding(t *testing.T) {
	value := MustExactRational(-1, 3)
	if got := FormatLowerEightDecimals(value); got != "-0.33333334" {
		t.Fatalf("lower = %s", got)
	}
	if got := FormatUpperEightDecimals(value); got != "-0.33333333" {
		t.Fatalf("upper = %s", got)
	}
	if got := FormatLowerEightDecimals(MustExactRational(1, 8)); got != "0.12500000" {
		t.Fatalf("exact lower = %s", got)
	}
}

func TestNeumaierSum_NormalizesNegativeZeroAndRejectsOverflow(t *testing.T) {
	got, err := NeumaierSum([]float64{math.Copysign(0, -1)})
	if err != nil || math.Signbit(got) {
		t.Fatalf("sum = %v signbit=%v error=%v", got, math.Signbit(got), err)
	}
	if _, err := NeumaierSum([]float64{math.MaxFloat64, math.MaxFloat64}); err == nil {
		t.Fatal("NeumaierSum() accepted overflowing intermediate")
	}
}

func newtonBinomial(population, selected int) int64 {
	if selected < 0 || selected > population {
		return 0
	}
	result := int64(1)
	for factor := 1; factor <= selected; factor++ {
		result = result * int64(population-selected+factor) / int64(factor)
	}
	return result
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
