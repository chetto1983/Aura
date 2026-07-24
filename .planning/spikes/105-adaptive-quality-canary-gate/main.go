package main

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"time"

	"github.com/chetto1983/aura/internal/adaptive"
	"github.com/google/uuid"
)

const (
	seed       = int64(10520260724)
	trials     = 1000
	perArm     = 2500
	baselineQ  = 0.65
	candidateQ = 0.78
	baselineH  = 0.01
	candidateH = 0.01
	unsafeH    = 0.08
)

type scenarioResult struct {
	Name          string  `json:"name"`
	Trials        int     `json:"trials"`
	Promotions    int     `json:"promotions"`
	PromotionRate float64 `json:"promotion_rate"`
}

type proof struct {
	SchemaVersion  string                   `json:"schema_version"`
	CreatedAt      time.Time                `json:"created_at"`
	SubjectModelID string                   `json:"subject_model_id"`
	Seed           int64                    `json:"seed"`
	Trials         int                      `json:"trials_per_scenario"`
	PerArm         int                      `json:"assigned_per_arm"`
	Config         adaptive.PromotionConfig `json:"gate_config"`
	Scenarios      []scenarioResult         `json:"scenarios"`
	NegativeGates  map[string]bool          `json:"negative_gates"`
	Verdict        string                   `json:"verdict"`
	Scope          string                   `json:"scope"`
}

func main() {
	cfg := adaptive.PromotionConfig{
		MinPerArm: 2000, MinEffectiveSampleSize: 1900,
		MinimumQualityUplift: 0, MaximumHarmIncrease: 0.02,
		TotalAlpha: 0.05, PlannedLooks: 5, Look: 5,
		MaxCensorRate: 0.05, MaxCensorImbalance: 0.02,
	}
	rng := rand.New(rand.NewSource(seed))
	results := []scenarioResult{
		runScenario(rng, cfg, "null_equal_quality", baselineQ, baselineQ, baselineH, baselineH),
		runScenario(rng, cfg, "safe_quality_uplift", baselineQ, candidateQ, baselineH, candidateH),
		runScenario(rng, cfg, "quality_uplift_with_harm_regression", baselineQ, candidateQ, baselineH, unsafeH),
	}

	spikeBatch := batchFor(rand.New(rand.NewSource(seed+1)), 0, baselineQ, candidateQ, baselineH, candidateH)
	spikeBatch.Environment = adaptive.EvaluationSpike
	selfScored := batchFor(rand.New(rand.NewSource(seed+2)), 1, baselineQ, candidateQ, baselineH, candidateH)
	selfScored.Observations[0].Evaluator = adaptive.EvaluatorSelfReport
	duplicate := batchFor(rand.New(rand.NewSource(seed+3)), 2, baselineQ, candidateQ, baselineH, candidateH)
	duplicate.Observations[1].DecisionID = duplicate.Observations[0].DecisionID
	censored := batchFor(rand.New(rand.NewSource(seed+4)), 3, baselineQ, candidateQ, baselineH, candidateH)
	for i := 1; i < 201; i += 2 {
		censored.Observations[i].OutcomeObserved = false
	}

	output := proof{
		SchemaVersion:  "1.0",
		CreatedAt:      time.Now().UTC(),
		SubjectModelID: "synthetic-model-not-production",
		Seed:           seed,
		Trials:         trials,
		PerArm:         perArm,
		Config:         cfg,
		Scenarios:      results,
		NegativeGates: map[string]bool{
			"spike_evidence_rejected":         !adaptive.EvaluatePromotion(spikeBatch, cfg).Eligible,
			"self_report_rejected":            !adaptive.EvaluatePromotion(selfScored, cfg).Eligible,
			"duplicate_decision_rejected":     !adaptive.EvaluatePromotion(duplicate, cfg).Eligible,
			"differential_censoring_rejected": !adaptive.EvaluatePromotion(censored, cfg).Eligible,
		},
		Verdict: "VALIDATED_GATE_NOT_MODEL",
		Scope:   "Synthetic seeded trials validate false-promotion, power, harm, and poison behavior of the gate. They do not prove that any production model or adaptive policy improves Aura.",
	}
	if results[0].PromotionRate > 0.05 || results[1].PromotionRate < 0.8 ||
		results[2].Promotions != 0 {
		output.Verdict = "INVALIDATED"
	}
	for _, passed := range output.NegativeGates {
		if !passed {
			output.Verdict = "INVALIDATED"
		}
	}

	target := filepath.Join(".planning", "spikes", "105-adaptive-quality-canary-gate", "artifacts", "quality-canary-statistical-proof.json")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		panic(err)
	}
	file, err := os.Create(target)
	if err != nil {
		panic(err)
	}
	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(output); err != nil {
		_ = file.Close()
		panic(err)
	}
	if err := file.Close(); err != nil {
		panic(err)
	}
	fmt.Printf("wrote %s: %s\n", target, output.Verdict)
	for _, result := range results {
		fmt.Printf("%s promotion_rate=%.4f (%d/%d)\n", result.Name, result.PromotionRate, result.Promotions, result.Trials)
	}
}

func runScenario(rng *rand.Rand, cfg adaptive.PromotionConfig, name string, bq, cq, bh, ch float64) scenarioResult {
	promotions := 0
	for trial := 0; trial < trials; trial++ {
		if adaptive.EvaluatePromotion(batchFor(rng, trial, bq, cq, bh, ch), cfg).Promote {
			promotions++
		}
	}
	return scenarioResult{
		Name: name, Trials: trials, Promotions: promotions,
		PromotionRate: float64(promotions) / trials,
	}
}

func batchFor(rng *rand.Rand, trial int, baselineQuality, candidateQuality, baselineHarm, candidateHarm float64) adaptive.CanaryBatch {
	observations := make([]adaptive.CanaryObservation, 0, perArm*2)
	for i := 0; i < perArm; i++ {
		observations = append(observations,
			observation(trial, i, adaptive.CanaryBaseline, rng.Float64() < baselineQuality, rng.Float64() < baselineHarm),
			observation(trial, i, adaptive.CanaryCandidate, rng.Float64() < candidateQuality, rng.Float64() < candidateHarm),
		)
	}
	return adaptive.CanaryBatch{
		Environment:   adaptive.EvaluationProductionCanary,
		PolicyVersion: "candidate-v1",
		ModelID:       "synthetic-model-not-production",
		CohortID:      fmt.Sprintf("closed-%d", trial),
		CohortClosed:  true,
		Observations:  observations,
	}
}

func observation(trial, index int, arm adaptive.CanaryArm, success, harm bool) adaptive.CanaryObservation {
	key := fmt.Sprintf("%d:%d:%s", trial, index, arm)
	return adaptive.CanaryObservation{
		DecisionID: uuid.NewSHA1(uuid.NameSpaceOID, []byte(key)),
		Arm:        arm, CandidateProbability: 0.5, OutcomeObserved: true,
		Success: success, Harm: harm,
		Evaluator: adaptive.EvaluatorDeterministic, CalibrationID: "rubric-v1",
	}
}
