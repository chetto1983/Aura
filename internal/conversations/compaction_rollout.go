package conversations

import (
	"context"
	"errors"
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
	if c.now().Sub(s.StageStartedAt) < 24*time.Hour || d.EligibleAttempts < 1000 || !promotionPasses(d.Gates) {
		return s, nil
	}
	next := s
	next.Stage = "canary_1"
	next.StageStartedAt = c.now().UTC()
	next.EligibleAttempts = d.EligibleAttempts
	next.ActiveConfig = []byte(`{"mode":"canary","percent":1,"recovery_drill_passed":true}`)
	return c.store.Transition(ctx, RolloutTransition{ExpectedVersion: s.Version, State: next, Evidence: evidenceFromDecision(d), ReasonCode: "promotion_gates_passed"})
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
