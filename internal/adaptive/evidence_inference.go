package adaptive

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/big"
	"slices"
	"time"
)

// ExactInferenceCaps bounds adversarial evidence computation.
type ExactInferenceCaps struct {
	MaxMembers     int
	MaxStrata      int
	MaxTailTerms   int
	MaxBigIntBits  int
	MaxMemoryBytes int64
	MaxWallTime    time.Duration
}

// DefaultExactInferenceCaps returns the frozen production resource limits.
func DefaultExactInferenceCaps() ExactInferenceCaps {
	return ExactInferenceCaps{
		MaxMembers:     5_000,
		MaxStrata:      5_000,
		MaxTailTerms:   5_000_000,
		MaxBigIntBits:  16_384,
		MaxMemoryBytes: 64 << 20,
		MaxWallTime:    10 * time.Second,
	}
}

// IntegerBounds contains conservative total-success bounds.
type IntegerBounds struct {
	Lower int
	Upper int
}

type inferenceBudget struct {
	caps      ExactInferenceCaps
	startedAt time.Time
	terms     int
}

// HypergeometricBounds inverts the strict inclusive upper tail exactly.
func HypergeometricBounds(
	ctx context.Context,
	population int,
	sampleSize int,
	observedSuccesses int,
	beta ExactRational,
	caps ExactInferenceCaps,
) (IntegerBounds, error) {
	budget := &inferenceBudget{caps: caps, startedAt: time.Now()}
	return hypergeometricBounds(ctx, population, sampleSize, observedSuccesses, beta, budget)
}

func hypergeometricBounds(
	ctx context.Context,
	population int,
	sampleSize int,
	observedSuccesses int,
	beta ExactRational,
	budget *inferenceBudget,
) (IntegerBounds, error) {
	if err := ctx.Err(); err != nil {
		return IntegerBounds{}, fmt.Errorf("exact inference cancelled: %w", err)
	}
	if err := validateHypergeometricInput(population, sampleSize, observedSuccesses, beta, budget.caps); err != nil {
		return IntegerBounds{}, err
	}
	if sampleSize == 0 {
		return IntegerBounds{Lower: 0, Upper: population}, nil
	}
	if sampleSize == population {
		return IntegerBounds{Lower: observedSuccesses, Upper: observedSuccesses}, nil
	}
	lower, err := invertHypergeometricLower(
		ctx, population, sampleSize, observedSuccesses, beta, budget,
	)
	if err != nil {
		return IntegerBounds{}, err
	}
	failureLower, err := invertHypergeometricLower(
		ctx, population, sampleSize, sampleSize-observedSuccesses, beta, budget,
	)
	if err != nil {
		return IntegerBounds{}, err
	}
	return IntegerBounds{Lower: lower, Upper: population - failureLower}, nil
}

func validateHypergeometricInput(
	population int,
	sampleSize int,
	observedSuccesses int,
	beta ExactRational,
	caps ExactInferenceCaps,
) error {
	if population < 0 || sampleSize < 0 || sampleSize > population ||
		observedSuccesses < 0 || observedSuccesses > sampleSize {
		return errors.New("invalid hypergeometric counts")
	}
	if beta.Cmp(MustExactRational(0, 1)) <= 0 ||
		beta.Cmp(MustExactRational(1, 1)) >= 0 {
		return errors.New("hypergeometric tail budget must be in (0,1)")
	}
	if population > caps.MaxMembers || caps.MaxTailTerms <= 0 ||
		caps.MaxBigIntBits <= 0 || caps.MaxMemoryBytes <= 0 || caps.MaxWallTime <= 0 {
		return errors.New("invalid or exceeded exact-inference caps")
	}
	return nil
}

func invertHypergeometricLower(
	ctx context.Context,
	population int,
	sampleSize int,
	observedSuccesses int,
	beta ExactRational,
	budget *inferenceBudget,
) (int, error) {
	maximumTotal := population - sampleSize + observedSuccesses
	for totalSuccesses := observedSuccesses; totalSuccesses <= maximumTotal; totalSuccesses++ {
		tail, err := hypergeometricUpperTail(
			ctx,
			population,
			sampleSize,
			observedSuccesses,
			totalSuccesses,
			budget,
		)
		if err != nil {
			return 0, err
		}
		if tail.Cmp(beta) > 0 {
			return totalSuccesses, nil
		}
	}
	return 0, errors.New("hypergeometric confidence set is empty")
}

func hypergeometricUpperTail(
	ctx context.Context,
	population int,
	sampleSize int,
	observedSuccesses int,
	totalSuccesses int,
	budget *inferenceBudget,
) (ExactRational, error) {
	numerator := new(big.Int)
	maximum := min(sampleSize, totalSuccesses)
	for successes := observedSuccesses; successes <= maximum; successes++ {
		if err := budget.consume(ctx); err != nil {
			return ExactRational{}, err
		}
		failures := sampleSize - successes
		if failures < 0 || failures > population-totalSuccesses {
			continue
		}
		term := new(big.Int).Mul(
			new(big.Int).Binomial(int64(totalSuccesses), int64(successes)),
			new(big.Int).Binomial(int64(population-totalSuccesses), int64(failures)),
		)
		numerator.Add(numerator, term)
		if numerator.BitLen() > budget.caps.MaxBigIntBits {
			return ExactRational{}, errors.New("exact-inference bigint cap exceeded")
		}
	}
	denominator := new(big.Int).Binomial(int64(population), int64(sampleSize))
	if denominator.BitLen() > budget.caps.MaxBigIntBits {
		return ExactRational{}, errors.New("exact-inference bigint cap exceeded")
	}
	return NewExactRational(numerator, denominator)
}

func (budget *inferenceBudget) consume(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("exact inference cancelled: %w", err)
	}
	if time.Since(budget.startedAt) > budget.caps.MaxWallTime {
		return errors.New("exact-inference wall-time cap exceeded")
	}
	budget.terms++
	if budget.terms > budget.caps.MaxTailTerms {
		return errors.New("exact-inference tail-term cap exceeded")
	}
	estimatedMemory := int64(budget.caps.MaxBigIntBits/8) * 4
	if estimatedMemory > budget.caps.MaxMemoryBytes {
		return errors.New("exact-inference memory cap exceeded")
	}
	return nil
}

// BinaryATEStratum supplies observed binary counts after endpoint-specific missing substitution.
type BinaryATEStratum struct {
	AnalysisStratumID string
	Population        int
	Treated           int
	TreatedSuccesses  int
	ControlSuccesses  int
}

// BinaryATEStratumBounds contains exact potential-total bounds for one stratum.
type BinaryATEStratumBounds struct {
	AnalysisStratumID string
	Treatment         IntegerBounds
	Control           IntegerBounds
}

// ExactATEReport contains the challenger-minus-baseline finite-population bounds.
type ExactATEReport struct {
	Strata []BinaryATEStratumBounds
	Lower  ExactRational
	Upper  ExactRational
}

// ExactATEBounds applies the frozen treatment/control and stratum Bonferroni split.
func ExactATEBounds(
	ctx context.Context,
	strata []BinaryATEStratum,
	alpha ExactRational,
	caps ExactInferenceCaps,
) (ExactATEReport, error) {
	if len(strata) == 0 || len(strata) > caps.MaxStrata {
		return ExactATEReport{}, errors.New("exact ATE requires a bounded nonempty stratum set")
	}
	if alpha.Cmp(MustExactRational(0, 1)) <= 0 ||
		alpha.Cmp(MustExactRational(1, 1)) >= 0 {
		return ExactATEReport{}, errors.New("exact ATE alpha must be in (0,1)")
	}
	ordered := slices.Clone(strata)
	slices.SortFunc(ordered, func(left, right BinaryATEStratum) int {
		if left.AnalysisStratumID < right.AnalysisStratumID {
			return -1
		}
		if left.AnalysisStratumID > right.AnalysisStratumID {
			return 1
		}
		return 0
	})
	for index := 1; index < len(ordered); index++ {
		if ordered[index-1].AnalysisStratumID == ordered[index].AnalysisStratumID {
			return ExactATEReport{}, errors.New("duplicate analysis stratum id")
		}
	}
	beta, err := alpha.Quo(MustExactRational(int64(2*len(ordered)), 1))
	if err != nil {
		return ExactATEReport{}, err
	}
	budget := &inferenceBudget{caps: caps, startedAt: time.Now()}
	report := ExactATEReport{Strata: make([]BinaryATEStratumBounds, 0, len(ordered))}
	var population, treatmentLower, treatmentUpper, controlLower, controlUpper int64
	for _, stratum := range ordered {
		if stratum.Population <= 0 || stratum.Treated < 0 || stratum.Treated > stratum.Population {
			return ExactATEReport{}, errors.New("invalid exact ATE stratum counts")
		}
		if population+int64(stratum.Population) > int64(caps.MaxMembers) {
			return ExactATEReport{}, errors.New("exact ATE member cap exceeded")
		}
		control := stratum.Population - stratum.Treated
		treatmentBounds, err := hypergeometricBounds(
			ctx, stratum.Population, stratum.Treated, stratum.TreatedSuccesses, beta, budget,
		)
		if err != nil {
			return ExactATEReport{}, fmt.Errorf("treatment stratum %q: %w", stratum.AnalysisStratumID, err)
		}
		controlBounds, err := hypergeometricBounds(
			ctx, stratum.Population, control, stratum.ControlSuccesses, beta, budget,
		)
		if err != nil {
			return ExactATEReport{}, fmt.Errorf("control stratum %q: %w", stratum.AnalysisStratumID, err)
		}
		report.Strata = append(report.Strata, BinaryATEStratumBounds{
			AnalysisStratumID: stratum.AnalysisStratumID,
			Treatment:         treatmentBounds,
			Control:           controlBounds,
		})
		population += int64(stratum.Population)
		treatmentLower += int64(treatmentBounds.Lower)
		treatmentUpper += int64(treatmentBounds.Upper)
		controlLower += int64(controlBounds.Lower)
		controlUpper += int64(controlBounds.Upper)
	}
	if population == 0 {
		return ExactATEReport{}, errors.New("exact ATE population is empty")
	}
	report.Lower = MustExactRational(treatmentLower-controlUpper, uint64(population))
	report.Upper = MustExactRational(treatmentUpper-controlLower, uint64(population))
	return report, nil
}

type neumaierAccumulator struct {
	sum        float64
	correction float64
}

func (accumulator *neumaierAccumulator) add(value float64) error {
	if !finite(value) {
		return errors.New("nonfinite Neumaier input")
	}
	total := binary64Add(accumulator.sum, value)
	if !finite(total) {
		return errors.New("nonfinite Neumaier sum")
	}
	var delta float64
	if math.Abs(accumulator.sum) >= math.Abs(value) {
		delta = binary64Add(binary64Add(accumulator.sum, -total), value)
	} else {
		delta = binary64Add(binary64Add(value, -total), accumulator.sum)
	}
	accumulator.correction = binary64Add(accumulator.correction, delta)
	if !finite(accumulator.correction) {
		return errors.New("nonfinite Neumaier correction")
	}
	accumulator.sum = total
	return nil
}

func (accumulator neumaierAccumulator) result() (float64, error) {
	result := binary64Add(accumulator.sum, accumulator.correction)
	if !finite(result) {
		return 0, errors.New("nonfinite Neumaier result")
	}
	if result == 0 {
		return 0, nil
	}
	return result, nil
}

// NeumaierSum accumulates canonical finite binary64 values without FMA.
func NeumaierSum(values []float64) (float64, error) {
	var accumulator neumaierAccumulator
	for _, value := range values {
		if err := accumulator.add(value); err != nil {
			return 0, err
		}
	}
	return accumulator.result()
}

//go:noinline
func binary64Add(left, right float64) float64 {
	return left + right
}

func finite(value float64) bool {
	return !math.IsNaN(value) && !math.IsInf(value, 0)
}
