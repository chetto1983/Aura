package conversations

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"time"

	"github.com/chetto1983/aura/internal/conversations/compaction_eval"
)

type rolloutStateStore interface {
	Load(context.Context, string) (RolloutState, error)
	Transition(context.Context, RolloutTransition) (RolloutState, error)
	Rollback(context.Context, RolloutRollback) (RolloutState, error)
}

// CompactionRolloutController applies evaluator decisions to authoritative durable state.
type CompactionRolloutController struct {
	store rolloutStateStore
	scope string
	now   func() time.Time
}

// NewCompactionRolloutController constructs a stateless controller for one deployment scope.
func NewCompactionRolloutController(store rolloutStateStore, scope string, now func() time.Time) *CompactionRolloutController {
	if now == nil {
		now = time.Now
	}
	return &CompactionRolloutController{store: store, scope: scope, now: now}
}

// Apply reloads current state and performs at most one expected-version transition.
func (c *CompactionRolloutController) Apply(ctx context.Context, d compaction_eval.Decision) (RolloutState, error) {
	if c == nil || c.store == nil || c.scope == "" {
		return RolloutState{}, errors.New("compaction rollout controller unavailable")
	}
	s, err := c.store.Load(ctx, c.scope)
	if err != nil {
		return RolloutState{}, err
	}
	if reason := rollbackReason(s, d); reason != "" {
		return c.store.Rollback(ctx, RolloutRollback{ScopeID: s.ScopeID, ExpectedVersion: s.Version, Evidence: evidenceFromDecision(d), ReasonCode: reason})
	}
	now := c.now()
	if now.Sub(s.StageStartedAt) < 24*time.Hour || d.EligibleAttempts < 1000 || !promotionPasses(d.Gates) {
		return s, nil
	}
	nextStage, percent := nextCanaryStage(s.Stage)
	if nextStage == "" {
		return s, nil
	}
	next := s
	next.Stage = nextStage
	next.StageStartedAt = now.UTC()
	next.EligibleAttempts = d.EligibleAttempts
	mode := "canary"
	if nextStage == "enabled" {
		mode = "enabled"
	}
	next.ActiveConfig = []byte(`{"mode":"` + mode + `","percent":` + strconv.Itoa(percent) + `,"recovery_drill_passed":true}`)
	return c.store.Transition(ctx, RolloutTransition{ExpectedVersion: s.Version, State: next, Evidence: evidenceFromDecision(d), ReasonCode: "promotion_gates_passed"})
}

func nextCanaryStage(current string) (string, int) {
	switch current {
	case "disabled", "shadow":
		return "canary_1", 1
	case "canary_1":
		return "canary_5", 5
	case "canary_5":
		return "canary_20", 20
	case "canary_20":
		return "canary_50", 50
	case "canary_50":
		return "enabled", 100
	default:
		return "", 0
	}
}

func rollbackReason(s RolloutState, d compaction_eval.Decision) string {
	if d.EvaluatorVersion != s.EvaluatorVersion {
		return "stale_evaluator_version"
	}
	if d.ScorerVersion != s.ScorerVersion || d.ConfigVersion != s.ConfigVersion || d.CorpusVersion != s.CorpusVersion {
		return "incompatible_evidence_version"
	}
	g := d.Gates
	switch {
	case g.CorruptEvidence > 0:
		return "corrupt_evidence"
	case g.AuthorityEscalations > 0 || g.L0Retention < 1:
		return "safety_gate_failed"
	case g.ToolPendingRetention < .99 || g.FactualDecisionRetention < .98 || g.ContinuationDelta < -0.02 || g.ContinuationConfidence < .95:
		return "continuation_gate_failed"
	case g.FailureRate > .02:
		return "failure_window_exceeded"
	case g.LatencyBreachMinutes >= 30:
		return "latency_window_exceeded"
	case g.RestoreRate > .01:
		return "restore_rate_exceeded"
	}
	return ""
}
func promotionPasses(g compaction_eval.Gates) bool {
	return g.L0Retention == 1 && g.AuthorityEscalations == 0 && g.ToolPendingRetention >= .99 && g.FactualDecisionRetention >= .98 && g.ContinuationDelta >= -.02 && g.ContinuationConfidence >= .95 && g.MedianReduction >= .4 && g.TargetAchievement >= .99 && g.P95ProactiveSeconds <= 8 && g.P95OverflowSeconds <= 15 && g.FailureRate <= .01 && g.CostRatio <= .15
}
func evidenceFromDecision(d compaction_eval.Decision) RolloutEvidence {
	return RolloutEvidence{ScopeID: d.ScopeID, Digest: d.EvidenceDigest, EvaluatorVersion: d.EvaluatorVersion, ScorerVersion: d.ScorerVersion, ConfigVersion: d.ConfigVersion, CorpusVersion: d.CorpusVersion, Snapshot: d.Snapshot}
}

// EvaluateOnce converts the persisted rolling windows into immutable evidence and applies it.
func (c *CompactionRolloutController) EvaluateOnce(ctx context.Context) (RolloutState, error) {
	s, err := c.store.Load(ctx, c.scope)
	if err != nil {
		return RolloutState{}, err
	}
	var failure struct {
		Rate float64 `json:"rate"`
	}
	var latency struct {
		P95Seconds    float64 `json:"p95_seconds"`
		BreachMinutes int64   `json:"breach_minutes"`
	}
	var restore struct {
		Rate float64 `json:"rate"`
	}
	var strata struct {
		L0Retention              float64 `json:"l0_retention"`
		ToolPendingRetention     float64 `json:"tool_pending_retention"`
		FactualDecisionRetention float64 `json:"factual_decision_retention"`
		ContinuationDelta        float64 `json:"continuation_delta"`
		ContinuationConfidence   float64 `json:"continuation_confidence"`
		MedianReduction          float64 `json:"median_reduction"`
		TargetAchievement        float64 `json:"target_achievement"`
		CostRatio                float64 `json:"cost_ratio"`
		P95OverflowSeconds       float64 `json:"p95_overflow_seconds"`
	}
	if json.Unmarshal(s.FailureWindow, &failure) != nil || json.Unmarshal(s.LatencyWindow, &latency) != nil || json.Unmarshal(s.RestoreWindow, &restore) != nil || json.Unmarshal(s.StratumSnapshots, &strata) != nil {
		return RolloutState{}, errors.New("corrupt rollout evaluation windows")
	}
	d, err := compaction_eval.Evaluate(compaction_eval.Input{ScopeID: s.ScopeID, EligibleAttempts: s.EligibleAttempts, EvaluatorVersion: s.EvaluatorVersion, ScorerVersion: s.ScorerVersion, ConfigVersion: s.ConfigVersion, CorpusVersion: s.CorpusVersion, Gates: compaction_eval.Gates{L0Retention: strata.L0Retention, ToolPendingRetention: strata.ToolPendingRetention, FactualDecisionRetention: strata.FactualDecisionRetention, ContinuationDelta: strata.ContinuationDelta, ContinuationConfidence: strata.ContinuationConfidence, MedianReduction: strata.MedianReduction, TargetAchievement: strata.TargetAchievement, CostRatio: strata.CostRatio, P95ProactiveSeconds: latency.P95Seconds, P95OverflowSeconds: strata.P95OverflowSeconds, FailureRate: failure.Rate, LatencyBreachMinutes: latency.BreachMinutes, RestoreRate: restore.Rate}})
	if err != nil {
		return RolloutState{}, err
	}
	return c.Apply(ctx, d)
}

// Run evaluates immediately and periodically until cancellation.
func (c *CompactionRolloutController) Run(ctx context.Context, interval time.Duration) error {
	if interval <= 0 {
		interval = time.Minute
	}
	for {
		if _, err := c.EvaluateOnce(ctx); err != nil {
			return err
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil
		case <-timer.C:
		}
	}
}
