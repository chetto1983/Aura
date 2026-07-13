// Package compaction_eval produces immutable, locale-neutral rollout evidence.
package compaction_eval

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
)

// Gates is the exact numerical promotion and rollback snapshot.
type Gates struct {
	L0Retention              float64 `json:"l0_retention"`
	ToolPendingRetention     float64 `json:"tool_pending_retention"`
	FactualDecisionRetention float64 `json:"factual_decision_retention"`
	ContinuationDelta        float64 `json:"continuation_delta"`
	ContinuationConfidence   float64 `json:"continuation_confidence"`
	MedianReduction          float64 `json:"median_reduction"`
	TargetAchievement        float64 `json:"target_achievement"`
	FailureRate              float64 `json:"failure_rate"`
	CostRatio                float64 `json:"cost_ratio"`
	P95ProactiveSeconds      float64 `json:"p95_proactive_seconds"`
	P95OverflowSeconds       float64 `json:"p95_overflow_seconds"`
	RestoreRate              float64 `json:"restore_rate"`
	AuthorityEscalations     int64   `json:"authority_escalations"`
	CorruptEvidence          int64   `json:"corrupt_evidence"`
	LatencyBreachMinutes     int64   `json:"latency_breach_minutes"`
}

// Input is one bounded cohort observation with immutable version identities.
type Input struct {
	ScopeID                                                       string `json:"scope_id"`
	EligibleAttempts                                              int64  `json:"eligible_attempts"`
	EvaluatorVersion, ScorerVersion, ConfigVersion, CorpusVersion string
	Eligibility, Censors, Strata                                  map[string]int64
	Gates                                                         Gates
}

// Decision is canonical evaluator evidence suitable for a durable CAS decision.
type Decision struct {
	ScopeID, EvidenceDigest                                       string
	EligibleAttempts                                              int64
	EvaluatorVersion, ScorerVersion, ConfigVersion, CorpusVersion string
	Eligibility, Censors, Strata                                  map[string]int64
	Gates                                                         Gates
	Snapshot                                                      []byte
}

// Evaluate validates and seals an observation without retaining mutable input maps.
func Evaluate(in Input) (Decision, error) {
	if in.ScopeID == "" || in.EvaluatorVersion == "" || in.ScorerVersion == "" || in.ConfigVersion == "" || in.CorpusVersion == "" || in.EligibleAttempts < 0 {
		return Decision{}, errors.New("incomplete compaction evaluator input")
	}
	// encoding/json sorts string map keys, giving a stable canonical snapshot for this closed schema.
	b, err := json.Marshal(in)
	if err != nil {
		return Decision{}, err
	}
	sum := sha256.Sum256(b)
	return Decision{ScopeID: in.ScopeID, EvidenceDigest: hex.EncodeToString(sum[:]), EligibleAttempts: in.EligibleAttempts, EvaluatorVersion: in.EvaluatorVersion, ScorerVersion: in.ScorerVersion, ConfigVersion: in.ConfigVersion, CorpusVersion: in.CorpusVersion, Eligibility: clone(in.Eligibility), Censors: clone(in.Censors), Strata: clone(in.Strata), Gates: in.Gates, Snapshot: append([]byte(nil), b...)}, nil
}

func clone(in map[string]int64) map[string]int64 {
	if in == nil {
		return map[string]int64{}
	}
	out := make(map[string]int64, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
