package adaptive

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"

	"github.com/google/uuid"
)

func eligibleCanaryBatch(baselineSuccess, candidateSuccess, baselineHarm, candidateHarm, perArm int) CanaryBatch {
	observations := make([]CanaryObservation, 0, perArm*2)
	for i := 0; i < perArm; i++ {
		observations = append(observations,
			CanaryObservation{
				DecisionID: uuid.NewSHA1(uuid.NameSpaceOID, []byte(fmt.Sprintf("baseline-%d", i))),
				Arm:        CanaryBaseline, CandidateProbability: 0.5, OutcomeObserved: true,
				Success: i < baselineSuccess, Harm: i < baselineHarm,
				Evaluator: EvaluatorDeterministic, CalibrationID: "rubric-v1",
			},
			CanaryObservation{
				DecisionID: uuid.NewSHA1(uuid.NameSpaceOID, []byte(fmt.Sprintf("candidate-%d", i))),
				Arm:        CanaryCandidate, CandidateProbability: 0.5, OutcomeObserved: true,
				Success: i < candidateSuccess, Harm: i < candidateHarm,
				Evaluator: EvaluatorDeterministic, CalibrationID: "rubric-v1",
			},
		)
	}
	return CanaryBatch{
		Environment:   EvaluationProductionCanary,
		PolicyVersion: "candidate-v1",
		ModelID:       "production-model",
		CohortID:      "closed-cohort-1",
		CohortClosed:  true,
		Observations:  observations,
	}
}

func conservativePromotionConfig() PromotionConfig {
	return PromotionConfig{
		MinPerArm: 200, MinEffectiveSampleSize: 180,
		MinimumQualityUplift: 0, MaximumHarmIncrease: 0.02,
		TotalAlpha: 0.05, PlannedLooks: 5, Look: 5,
		MaxCensorRate: 0.05, MaxCensorImbalance: 0.02,
	}
}

func TestPromotionGateRequiresProductionIndependentCalibratedEvidence(t *testing.T) {
	batch := eligibleCanaryBatch(130, 170, 4, 4, 200)
	batch.Environment = EvaluationSpike
	batch.Observations[0].Evaluator = EvaluatorSelfReport
	batch.Observations[1].CalibrationID = ""

	report := EvaluatePromotion(batch, conservativePromotionConfig())
	if report.Promote || report.Eligible {
		t.Fatalf("ineligible spike/self-scored batch promoted: %+v", report)
	}
	for _, want := range []string{"production canary", "self_report", "calibration"} {
		if !containsReason(report.Reasons, want) {
			t.Fatalf("reasons %v do not mention %q", report.Reasons, want)
		}
	}
}

func TestPromotionGatePassesLargeQualityWinWithinSafetyBound(t *testing.T) {
	// A strict 2pp harm margin with five planned looks needs a genuinely large
	// cohort; the gate must not manufacture certainty from a few hundred samples.
	batch := eligibleCanaryBatch(650, 850, 10, 10, 1000)
	report := EvaluatePromotion(batch, conservativePromotionConfig())
	if !report.Eligible || !report.Promote {
		t.Fatalf("strong safe candidate did not promote: %+v", report)
	}
	if report.QualityUplift.Lower <= 0 {
		t.Fatalf("quality lower bound = %f, want > 0", report.QualityUplift.Lower)
	}
	if report.HarmIncrease.Upper > 0.02 {
		t.Fatalf("harm upper bound = %f, want <= 0.02", report.HarmIncrease.Upper)
	}
}

func TestPromotionGateBlocksNoQualityGainAndSafetyRegression(t *testing.T) {
	noGain := eligibleCanaryBatch(160, 160, 3, 3, 260)
	noGainReport := EvaluatePromotion(noGain, conservativePromotionConfig())
	if noGainReport.Promote || noGainReport.QualityUplift.Lower > 0 {
		t.Fatalf("no-gain candidate promoted: %+v", noGainReport)
	}

	harm := eligibleCanaryBatch(130, 180, 2, 25, 260)
	harmReport := EvaluatePromotion(harm, conservativePromotionConfig())
	if harmReport.Promote || harmReport.HarmIncrease.Upper <= 0.02 {
		t.Fatalf("harmful candidate promoted: %+v", harmReport)
	}
}

func TestPromotionGateBlocksCensoredAndDuplicateEvidence(t *testing.T) {
	batch := eligibleCanaryBatch(170, 190, 2, 2, 260)
	for i := 0; i < 30; i++ {
		batch.Observations[i*2+1].OutcomeObserved = false
	}
	batch.Observations[3].DecisionID = batch.Observations[1].DecisionID

	report := EvaluatePromotion(batch, conservativePromotionConfig())
	if report.Promote || report.Eligible {
		t.Fatalf("censored/duplicate batch promoted: %+v", report)
	}
	if !containsReason(report.Reasons, "censor") || !containsReason(report.Reasons, "duplicate") {
		t.Fatalf("reasons = %v, want censor and duplicate failures", report.Reasons)
	}
}

func TestPromotionGateSeededMonteCarloControlsFalsePromotion(t *testing.T) {
	cfg := conservativePromotionConfig()
	cfg.MinPerArm = 100
	cfg.MinEffectiveSampleSize = 90
	const trials = 1000
	const perArm = 180
	rng := rand.New(rand.NewSource(105))
	falsePromotions := 0
	for trial := 0; trial < trials; trial++ {
		batch := randomizedCanaryBatch(rng, perArm, 0.65, 0.65, 0.01, 0.01, trial)
		if EvaluatePromotion(batch, cfg).Promote {
			falsePromotions++
		}
	}
	rate := float64(falsePromotions) / trials
	if rate > 0.05 {
		t.Fatalf("seeded null false-promotion rate = %.3f (%d/%d), want <= 0.05", rate, falsePromotions, trials)
	}
	t.Logf("seeded null false-promotion rate %.3f (%d/%d)", rate, falsePromotions, trials)
}

func randomizedCanaryBatch(rng *rand.Rand, perArm int, baselineQuality, candidateQuality, baselineHarm, candidateHarm float64, trial int) CanaryBatch {
	batch := eligibleCanaryBatch(0, 0, 0, 0, perArm)
	batch.CohortID = fmt.Sprintf("monte-carlo-%d", trial)
	for i := range batch.Observations {
		observation := &batch.Observations[i]
		switch observation.Arm {
		case CanaryBaseline:
			observation.Success = rng.Float64() < baselineQuality
			observation.Harm = rng.Float64() < baselineHarm
		case CanaryCandidate:
			observation.Success = rng.Float64() < candidateQuality
			observation.Harm = rng.Float64() < candidateHarm
		}
	}
	return batch
}

func containsReason(reasons []string, fragment string) bool {
	for _, reason := range reasons {
		if strings.Contains(reason, fragment) {
			return true
		}
	}
	return false
}
