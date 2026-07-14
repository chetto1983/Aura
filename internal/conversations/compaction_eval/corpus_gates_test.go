package compaction_eval

import (
	"sort"
	"testing"
)

// Section 17.13 numerical promotion gates (42-VALIDATION.md "Numerical
// promotion gates" table). These are the exact pass conditions; a corpus that
// cannot clear all of them is not eligible for rollout regardless of census size.
const (
	gateL0Retention              = 1.0
	gateUnresolvedLedgerRetained = 1.0
	gateToolPendingRetention     = 0.99
	gateFactualDecisionRetained  = 0.98
	gateContinuationDeltaMax     = 0.02 // no more than 2 percentage points below baseline
	gateMedianReductionMin       = 0.40
	gateTargetAchievementMin     = 0.99
	gateP95ProactiveSecondsMax   = 8.0
	gateP95OverflowSecondsMax    = 15.0
	gateFailureRateMax           = 0.01
	gateCostRatioMax             = 0.15
	gateRestoreRateMax           = 0.01
	gateLatencyBreachMinutesMax  = 30 // "latency breach/30m" auto-rollback threshold
)

func mean(vals []float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	var sum float64
	for _, v := range vals {
		sum += v
	}
	return sum / float64(len(vals))
}

func median(vals []float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	s := append([]float64(nil), vals...)
	sort.Float64s(s)
	mid := len(s) / 2
	if len(s)%2 == 1 {
		return s[mid]
	}
	return (s[mid-1] + s[mid]) / 2
}

// percentile is a linear-interpolation percentile (matches the widely-used
// "R-7" method) over p in [0,1].
func percentile(vals []float64, p float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	s := append([]float64(nil), vals...)
	sort.Float64s(s)
	if len(s) == 1 {
		return s[0]
	}
	rank := p * float64(len(s)-1)
	lo := int(rank)
	hi := lo + 1
	if hi >= len(s) {
		return s[len(s)-1]
	}
	frac := rank - float64(lo)
	return s[lo]*(1-frac) + s[hi]*frac
}

// TestCompactionCorpusNumericalGates aggregates the golden corpus's embedded
// expected-projection ground truth into the exact compaction_eval.Gates
// fields, seals it through Evaluate (the same seam a live rollout controller
// consumes), and asserts every Section 17.13 numerical promotion/rollback gate
// clears. Adversarial cases contribute only to the zero-accepted-escalation and
// zero-corrupt-evidence checks (they are boundary/injection fixtures, not
// eligible compaction attempts). No skip path exists: a corpus that cannot
// clear a gate fails this test, it never passes green with a weaker corpus.
func TestCompactionCorpusNumericalGates(t *testing.T) {
	golden := loadGoldenCorpus(t)
	adversarial := loadAdversarialCorpus(t)
	if len(golden) == 0 {
		t.Fatal("golden corpus is empty")
	}

	eligibility := map[string]int64{}
	censors := map[string]int64{}
	strata := map[string]int64{}

	var (
		l0Retained, ledgerResolved, toolPendingOK, factualOK, targetOK int
		failedCount, restoredCount                                     int
		authorityEscalations, corruptEvidence, latencyBreachMinutes    int64
		reductions, deltas, proactiveLatencies, overflowLatencies      []float64
		costRatios                                                     []float64
	)
	for _, c := range golden {
		eligibility[c.Eligibility]++
		censors[c.Censor]++
		strata[c.Stratum]++

		ep := c.ExpectedProjection
		if ep.L0Retained {
			l0Retained++
		}
		if ep.UnresolvedLedgerResolved {
			ledgerResolved++
		}
		if ep.ToolPendingRetained {
			toolPendingOK++
		}
		if ep.FactualDecisionRetained {
			factualOK++
		}
		if ep.TargetAchieved {
			targetOK++
		}
		if ep.Failed {
			failedCount++
		}
		if ep.Restored {
			restoredCount++
		}
		if ep.AuthorityEscalationAccepted {
			authorityEscalations++
		}
		if ep.CorruptEvidence {
			corruptEvidence++
		}
		latencyBreachMinutes += ep.LatencyBreachMinutes
		reductions = append(reductions, ep.ReductionRatio)
		deltas = append(deltas, ep.ContinuationConfidenceDelta)
		proactiveLatencies = append(proactiveLatencies, ep.ProactiveLatencySeconds)
		overflowLatencies = append(overflowLatencies, ep.OverflowLatencySeconds)
		costRatios = append(costRatios, ep.CostRatio)
	}
	for _, c := range adversarial {
		if c.ExpectedProjection.AuthorityEscalationAccepted {
			authorityEscalations++
		}
		if c.ExpectedProjection.CorruptEvidence {
			corruptEvidence++
		}
	}

	n := float64(len(golden))
	gates := Gates{
		L0Retention:              float64(l0Retained) / n,
		ToolPendingRetention:     float64(toolPendingOK) / n,
		FactualDecisionRetention: float64(factualOK) / n,
		ContinuationDelta:        mean(deltas),
		ContinuationConfidence:   0.95,
		MedianReduction:          median(reductions),
		TargetAchievement:        float64(targetOK) / n,
		FailureRate:              float64(failedCount) / n,
		CostRatio:                median(costRatios),
		P95ProactiveSeconds:      percentile(proactiveLatencies, 0.95),
		P95OverflowSeconds:       percentile(overflowLatencies, 0.95),
		RestoreRate:              float64(restoredCount) / n,
		AuthorityEscalations:     authorityEscalations,
		CorruptEvidence:          corruptEvidence,
		LatencyBreachMinutes:     latencyBreachMinutes,
	}

	in := Input{
		ScopeID:          "phase42-corpus-census",
		EligibleAttempts: int64(len(golden)),
		EvaluatorVersion: "compaction-eval-v1",
		ScorerVersion:    "compaction-scorer-v1",
		ConfigVersion:    "compaction-config-v1",
		CorpusVersion:    corpusVersion,
		Eligibility:      eligibility,
		Censors:          censors,
		Strata:           strata,
		Gates:            gates,
	}
	dec, err := Evaluate(in)
	if err != nil {
		t.Fatalf("Evaluate(census input): %v", err)
	}
	if dec.EvidenceDigest == "" {
		t.Fatal("Evaluate returned an empty evidence digest")
	}
	if dec.EligibleAttempts != int64(len(golden)) {
		t.Fatalf("Decision.EligibleAttempts = %d, want %d", dec.EligibleAttempts, len(golden))
	}

	ledgerRetention := float64(ledgerResolved) / n
	unresolvedLedgerRetained := float64(ledgerResolved) / n

	type gate struct {
		name string
		got  float64
		want float64
		max  bool // true: got must be <= want; false: got must be >= want
	}
	for _, g := range []gate{
		{"L0 retention", dec.Gates.L0Retention, gateL0Retention, false},
		{"unresolved ledger retention", unresolvedLedgerRetained, gateUnresolvedLedgerRetained, false},
		{"tool/pending retention", dec.Gates.ToolPendingRetention, gateToolPendingRetention, false},
		{"factual/decision retention", dec.Gates.FactualDecisionRetention, gateFactualDecisionRetained, false},
		{"continuation delta (2pp cap)", dec.Gates.ContinuationDelta, gateContinuationDeltaMax, true},
		{"median reduction", dec.Gates.MedianReduction, gateMedianReductionMin, false},
		{"target achievement", dec.Gates.TargetAchievement, gateTargetAchievementMin, false},
		{"p95 proactive seconds", dec.Gates.P95ProactiveSeconds, gateP95ProactiveSecondsMax, true},
		{"p95 overflow seconds", dec.Gates.P95OverflowSeconds, gateP95OverflowSecondsMax, true},
		{"failure rate", dec.Gates.FailureRate, gateFailureRateMax, true},
		{"cost ratio (median)", dec.Gates.CostRatio, gateCostRatioMax, true},
		{"restore rate", dec.Gates.RestoreRate, gateRestoreRateMax, true},
	} {
		if g.max && g.got > g.want+1e-9 {
			t.Errorf("%s = %.4f, want <= %.4f", g.name, g.got, g.want)
		}
		if !g.max && g.got < g.want-1e-9 {
			t.Errorf("%s = %.4f, want >= %.4f", g.name, g.got, g.want)
		}
	}
	if ledgerRetention < gateUnresolvedLedgerRetained-1e-9 {
		t.Errorf("ledger retention = %.4f, want >= %.4f", ledgerRetention, gateUnresolvedLedgerRetained)
	}
	if dec.Gates.AuthorityEscalations != 0 {
		t.Errorf("accepted authority escalations = %d, want 0", dec.Gates.AuthorityEscalations)
	}
	if dec.Gates.CorruptEvidence != 0 {
		t.Errorf("corrupt evidence rows = %d, want 0", dec.Gates.CorruptEvidence)
	}
	if dec.Gates.LatencyBreachMinutes >= gateLatencyBreachMinutesMax {
		t.Errorf("latency breach minutes = %d, want < %d (auto-rollback threshold)", dec.Gates.LatencyBreachMinutes, gateLatencyBreachMinutesMax)
	}

	t.Logf("Section 17.13 gates: l0=%.4f ledger=%.4f toolPending=%.4f factual=%.4f delta=%.4f medianReduction=%.4f target=%.4f p95proactive=%.2fs p95overflow=%.2fs failure=%.4f cost=%.4f restore=%.4f escalations=%d corrupt=%d latencyBreach=%dm",
		dec.Gates.L0Retention, ledgerRetention, dec.Gates.ToolPendingRetention, dec.Gates.FactualDecisionRetention,
		dec.Gates.ContinuationDelta, dec.Gates.MedianReduction, dec.Gates.TargetAchievement,
		dec.Gates.P95ProactiveSeconds, dec.Gates.P95OverflowSeconds, dec.Gates.FailureRate, dec.Gates.CostRatio,
		dec.Gates.RestoreRate, dec.Gates.AuthorityEscalations, dec.Gates.CorruptEvidence, dec.Gates.LatencyBreachMinutes)
}
