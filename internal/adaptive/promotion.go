package adaptive

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"strings"

	"github.com/google/uuid"
)

// EvaluationEnvironment identifies where promotion evidence was collected.
type EvaluationEnvironment string

// Evaluation environments separate non-serving spikes from production canaries.
const (
	EvaluationSpike            EvaluationEnvironment = "spike"
	EvaluationOffline          EvaluationEnvironment = "offline"
	EvaluationProductionCanary EvaluationEnvironment = "production_canary"
)

// EvaluatorKind identifies the source of an observed outcome.
type EvaluatorKind string

// Evaluator kinds distinguish eligible calibrated evidence from weak proxies.
const (
	EvaluatorDeterministic   EvaluatorKind = "deterministic"
	EvaluatorHuman           EvaluatorKind = "human"
	EvaluatorCalibratedJudge EvaluatorKind = "calibrated_judge"
	EvaluatorSelfReport      EvaluatorKind = "self_report"
	EvaluatorOperational     EvaluatorKind = "operational_proxy"
)

// CanaryArm identifies the randomized assignment served to a cohort member.
type CanaryArm string

// Canary arms compare the static champion with one candidate.
const (
	CanaryBaseline  CanaryArm = "baseline"
	CanaryCandidate CanaryArm = "candidate"
)

// CanaryObservation is one pre-assigned unit in a closed production cohort.
// CandidateProbability is the logged randomization propensity, not an inferred
// probability. Missing outcomes remain in the batch with OutcomeObserved=false
// so censoring cannot disappear from the promotion calculation.
type CanaryObservation struct {
	DecisionID           uuid.UUID
	Arm                  CanaryArm
	CandidateProbability float64
	OutcomeObserved      bool
	Success              bool
	Harm                 bool
	Evaluator            EvaluatorKind
	CalibrationID        string
}

// CanaryBatch is one closed, pre-registered randomized production cohort.
type CanaryBatch struct {
	Environment   EvaluationEnvironment
	PolicyVersion string
	ModelID       string
	CohortID      string
	CohortClosed  bool
	Observations  []CanaryObservation
}

// PromotionConfig contains the pre-registered statistical and safety bounds.
type PromotionConfig struct {
	MinPerArm              int
	MinEffectiveSampleSize float64
	MinimumQualityUplift   float64
	MaximumHarmIncrease    float64
	TotalAlpha             float64
	PlannedLooks           int
	Look                   int
	MaxCensorRate          float64
	MaxCensorImbalance     float64
}

// EstimateInterval reports a point estimate and its confidence bounds.
type EstimateInterval struct {
	Estimate float64
	Lower    float64
	Upper    float64
}

// ArmEvidence summarizes assignment, censoring, quality, and harm for one arm.
type ArmEvidence struct {
	Assigned     int
	Observed     int
	CensoredRate float64
	EffectiveN   float64
	QualityRate  float64
	HarmRate     float64
}

// PromotionReport is the immutable evidence evaluated before a policy promotion.
type PromotionReport struct {
	Environment    EvaluationEnvironment
	PolicyVersion  string
	ModelID        string
	CohortID       string
	EvidenceSHA256 string
	Eligible       bool
	Promote        bool
	Reasons        []string
	AlphaAtLook    float64
	Baseline       ArmEvidence
	Candidate      ArmEvidence
	QualityUplift  EstimateInterval
	HarmIncrease   EstimateInterval
}

type weightedArm struct {
	assigned   int
	observed   int
	sumWeight  float64
	sumWeight2 float64
	success    float64
	harm       float64
}

// EvaluatePromotion applies a fixed, pre-registered sequence of Bonferroni-
// corrected looks. The Wilson/Newcombe bounds are conservative for two
// independently randomized arms: promotion needs both a quality lower bound
// above the declared threshold and a harm upper bound below its margin.
func EvaluatePromotion(batch CanaryBatch, cfg PromotionConfig) PromotionReport {
	report := PromotionReport{
		Environment: batch.Environment, PolicyVersion: batch.PolicyVersion,
		ModelID: batch.ModelID, CohortID: batch.CohortID,
	}
	if raw, err := json.Marshal(batch); err == nil {
		sum := sha256.Sum256(raw)
		report.EvidenceSHA256 = hex.EncodeToString(sum[:])
	}
	validatePromotionEnvelope(batch, cfg, &report)

	seen := make(map[uuid.UUID]struct{}, len(batch.Observations))
	var baseline, candidate weightedArm
	for i, observation := range batch.Observations {
		if observation.DecisionID == uuid.Nil {
			report.Reasons = append(report.Reasons, fmt.Sprintf("observation %d has no decision id", i))
		} else if _, exists := seen[observation.DecisionID]; exists {
			report.Reasons = append(report.Reasons, fmt.Sprintf("duplicate decision id %s", observation.DecisionID))
		} else {
			seen[observation.DecisionID] = struct{}{}
		}

		if observation.CandidateProbability <= 0 || observation.CandidateProbability >= 1 ||
			math.IsNaN(observation.CandidateProbability) {
			report.Reasons = append(report.Reasons, fmt.Sprintf("observation %d has invalid assignment propensity", i))
			continue
		}
		var arm *weightedArm
		var weight float64
		switch observation.Arm {
		case CanaryBaseline:
			arm = &baseline
			weight = 1 / (1 - observation.CandidateProbability)
		case CanaryCandidate:
			arm = &candidate
			weight = 1 / observation.CandidateProbability
		default:
			report.Reasons = append(report.Reasons, fmt.Sprintf("observation %d has invalid arm %q", i, observation.Arm))
			continue
		}
		arm.assigned++
		if !observation.OutcomeObserved {
			continue
		}
		switch observation.Evaluator {
		case EvaluatorDeterministic, EvaluatorHuman, EvaluatorCalibratedJudge:
			if strings.TrimSpace(observation.CalibrationID) == "" {
				report.Reasons = append(report.Reasons, fmt.Sprintf("observation %d has no evaluator calibration id", i))
			}
		case EvaluatorSelfReport, EvaluatorOperational:
			report.Reasons = append(report.Reasons, fmt.Sprintf("observation %d uses ineligible evaluator %s", i, observation.Evaluator))
		default:
			report.Reasons = append(report.Reasons, fmt.Sprintf("observation %d has unknown evaluator %q", i, observation.Evaluator))
		}
		arm.observed++
		arm.sumWeight += weight
		arm.sumWeight2 += weight * weight
		if observation.Success {
			arm.success += weight
		}
		if observation.Harm {
			arm.harm += weight
		}
	}

	report.Baseline = summarizeArm(baseline)
	report.Candidate = summarizeArm(candidate)
	validateArmEvidence(report.Baseline, report.Candidate, cfg, &report)
	if len(report.Reasons) > 0 {
		return report
	}
	report.Eligible = true

	report.AlphaAtLook = cfg.TotalAlpha / float64(cfg.PlannedLooks)
	z := math.Sqrt2 * math.Erfinv(1-report.AlphaAtLook)
	bqLow, bqHigh := wilson(report.Baseline.QualityRate, report.Baseline.EffectiveN, z)
	cqLow, cqHigh := wilson(report.Candidate.QualityRate, report.Candidate.EffectiveN, z)
	bhLow, bhHigh := wilson(report.Baseline.HarmRate, report.Baseline.EffectiveN, z)
	chLow, chHigh := wilson(report.Candidate.HarmRate, report.Candidate.EffectiveN, z)
	report.QualityUplift = EstimateInterval{
		Estimate: report.Candidate.QualityRate - report.Baseline.QualityRate,
		Lower:    cqLow - bqHigh,
		Upper:    cqHigh - bqLow,
	}
	report.HarmIncrease = EstimateInterval{
		Estimate: report.Candidate.HarmRate - report.Baseline.HarmRate,
		Lower:    chLow - bhHigh,
		Upper:    chHigh - bhLow,
	}

	qualityPass := report.QualityUplift.Lower > cfg.MinimumQualityUplift
	safetyPass := report.HarmIncrease.Upper <= cfg.MaximumHarmIncrease
	if !qualityPass {
		report.Reasons = append(report.Reasons,
			fmt.Sprintf("quality lower bound %.6f does not exceed %.6f", report.QualityUplift.Lower, cfg.MinimumQualityUplift))
	}
	if !safetyPass {
		report.Reasons = append(report.Reasons,
			fmt.Sprintf("harm upper bound %.6f exceeds %.6f", report.HarmIncrease.Upper, cfg.MaximumHarmIncrease))
	}
	report.Promote = qualityPass && safetyPass
	return report
}

func validatePromotionEnvelope(batch CanaryBatch, cfg PromotionConfig, report *PromotionReport) {
	if batch.Environment != EvaluationProductionCanary {
		report.Reasons = append(report.Reasons, "promotion requires a production canary environment")
	}
	if strings.TrimSpace(batch.PolicyVersion) == "" {
		report.Reasons = append(report.Reasons, "policy version is required")
	}
	if strings.TrimSpace(batch.ModelID) == "" {
		report.Reasons = append(report.Reasons, "production model id is required")
	}
	if strings.TrimSpace(batch.CohortID) == "" || !batch.CohortClosed {
		report.Reasons = append(report.Reasons, "a closed, pre-registered cohort is required")
	}
	if cfg.MinPerArm <= 0 || cfg.MinEffectiveSampleSize <= 0 {
		report.Reasons = append(report.Reasons, "positive arm sample thresholds are required")
	}
	if cfg.TotalAlpha <= 0 || cfg.TotalAlpha >= 1 || cfg.PlannedLooks <= 0 ||
		cfg.Look <= 0 || cfg.Look > cfg.PlannedLooks {
		report.Reasons = append(report.Reasons, "invalid pre-registered sequential-look configuration")
	}
	if cfg.MaxCensorRate < 0 || cfg.MaxCensorRate >= 1 ||
		cfg.MaxCensorImbalance < 0 || cfg.MaxCensorImbalance >= 1 {
		report.Reasons = append(report.Reasons, "invalid censoring thresholds")
	}
}

func summarizeArm(arm weightedArm) ArmEvidence {
	out := ArmEvidence{Assigned: arm.assigned, Observed: arm.observed}
	if arm.assigned > 0 {
		out.CensoredRate = float64(arm.assigned-arm.observed) / float64(arm.assigned)
	}
	if arm.sumWeight > 0 {
		out.QualityRate = arm.success / arm.sumWeight
		out.HarmRate = arm.harm / arm.sumWeight
	}
	if arm.sumWeight2 > 0 {
		out.EffectiveN = arm.sumWeight * arm.sumWeight / arm.sumWeight2
	}
	return out
}

func validateArmEvidence(baseline, candidate ArmEvidence, cfg PromotionConfig, report *PromotionReport) {
	if baseline.Observed < cfg.MinPerArm || candidate.Observed < cfg.MinPerArm {
		report.Reasons = append(report.Reasons,
			fmt.Sprintf("insufficient observed outcomes per arm: baseline=%d candidate=%d", baseline.Observed, candidate.Observed))
	}
	if baseline.EffectiveN < cfg.MinEffectiveSampleSize || candidate.EffectiveN < cfg.MinEffectiveSampleSize {
		report.Reasons = append(report.Reasons,
			fmt.Sprintf("insufficient effective sample size: baseline=%.2f candidate=%.2f", baseline.EffectiveN, candidate.EffectiveN))
	}
	if baseline.CensoredRate > cfg.MaxCensorRate || candidate.CensoredRate > cfg.MaxCensorRate {
		report.Reasons = append(report.Reasons,
			fmt.Sprintf("censor rate exceeds bound: baseline=%.4f candidate=%.4f", baseline.CensoredRate, candidate.CensoredRate))
	}
	if math.Abs(baseline.CensoredRate-candidate.CensoredRate) > cfg.MaxCensorImbalance {
		report.Reasons = append(report.Reasons,
			fmt.Sprintf("censor imbalance exceeds bound: baseline=%.4f candidate=%.4f", baseline.CensoredRate, candidate.CensoredRate))
	}
}

func wilson(rate, effectiveN, z float64) (float64, float64) {
	if effectiveN <= 0 {
		return 0, 1
	}
	z2 := z * z
	denom := 1 + z2/effectiveN
	center := (rate + z2/(2*effectiveN)) / denom
	half := z * math.Sqrt((rate*(1-rate)+z2/(4*effectiveN))/effectiveN) / denom
	return math.Max(0, center-half), math.Min(1, center+half)
}
