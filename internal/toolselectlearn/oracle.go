package toolselectlearn

import (
	"context"
	"os"
	"strings"

	"github.com/chetto1983/aura/internal/toolselectstore"
)

// oracleEnv is the kill-switch for the PAID escalation tier (T-08.2-16, the
// no-paid-runs guardrail). AURA_TOOLSELECT_ORACLE=on|off; default ON. "off" makes
// the DeepSeek teacher a strict no-op so the loop labels ONLY the confident free
// cases (the ranker's high-margin top-1) and never spends a paid LLM call.
const oracleEnv = "AURA_TOOLSELECT_ORACLE"

// oracleEnabled reports whether the paid escalation tier is active. Any value other
// than an explicit "off" (case-insensitive) keeps the default ON, so a typo never
// silently disables learning's hard-case coverage; only a deliberate "off" gates it.
func oracleEnabled() bool {
	return !strings.EqualFold(strings.TrimSpace(os.Getenv(oracleEnv)), "off")
}

// confidentMargin is the free-vs-escalate gate: the ranker's free top-1 label is
// trusted only when its top-2 cosine margin is at least this wide. Below it the
// ranker is itself uncertain and the free label would amplify its own gravity-well
// bias (spike-057: the free ranker mislabels 3/7 hard mis-routes), so the low-margin
// tail escalates to the DeepSeek teacher. Hard-coded (correctness-bearing, spike-057).
const confidentMargin = 0.05

// Teacher is the narrow seam for the EXISTING DeepSeek router used as the escalation
// oracle (Req-8: no new sidecar). The runner supplies a concrete adapter that runs
// the router prompt via llm.Client (mirroring runner.reasoningOracle: MaxTokens:32,
// Temperature:0, ToolChoice:"none", Reasoning.Enabled=false). Label returns the
// confirmed tool name for the request, or ok=false on any failure/decline.
type Teacher interface {
	Label(ctx context.Context, request string) (tool string, ok bool)
}

// Saver persists one oracle-confirmed (query -> tool) example. *toolselectstore.Store
// satisfies it.
type Saver interface {
	Save(ctx context.Context, query, tool string, vec []float64) error
}

// ExampleLoader loads the full confirmed-example set for the Refresh re-fold.
// *toolselectstore.Store satisfies it (LoadExamples). The runner's Refresh hook
// type-asserts the Saver to this so the loop re-folds the per-tool centroids after a
// save without toolselectlearn importing toolselectstore.
type ExampleLoader interface {
	LoadExamples(ctx context.Context) ([]toolselectstore.LabeledVec, error)
}

// twoTierOracle adapts the tool-selection two-tier labeling + persistence into the
// label-agnostic activelearn.Oracle.LabelAndSave seam. The activelearn core observes
// a flagged turn opaquely; this adapter assigns the label:
//
//  1. FREE tier — when the ranker's top-1 for the request is CONFIDENT (margin >=
//     confidentMargin), trust it as the gold tool. Zero cost, deterministic.
//  2. ESCALATION tier — when the ranker is low-margin (or unavailable), escalate the
//     hard case to the DeepSeek Teacher, gated by AURA_TOOLSELECT_ORACLE. The free
//     ranker alone amplifies its bias (risk #8), so the escalation is MANDATORY for
//     the uncertain tail — but it is the existing router (no new sidecar) and is
//     paid-call-gated (margin-gated + async + kill-switched).
//
// On a committed label it persists (query, tool, vec) and returns saved=true so the
// activelearn core keeps the content hash (the same query is never relabeled). Any
// non-commit (no confident free label AND escalation off/failed, or a save error)
// returns false so activelearn deletes the hash and a later observation can retry.
type twoTierOracle struct {
	ranker  Ranker
	teacher Teacher
	saver   Saver
}

func (o twoTierOracle) LabelAndSave(ctx context.Context, query string, vec []float64) bool {
	tool, ok := o.label(ctx, query)
	if !ok || tool == "" {
		return false
	}
	return o.saver.Save(ctx, query, tool, vec) == nil
}

// label runs the two-tier policy and returns the confirmed tool (ok=false to decline).
func (o twoTierOracle) label(ctx context.Context, query string) (string, bool) {
	// Free tier: a confident ranker top-1 is the gold label (no paid call). This runs on
	// the activelearn worker (off the turn), so ctx is the worker lifetime — the ranking
	// embed is bounded by Close, never by the user turn.
	if o.ranker != nil {
		if top1, margin, ok := o.ranker.Rank(ctx, query); ok && top1 != "" && margin >= confidentMargin {
			return top1, true
		}
	}
	// Escalation tier: the low-margin / no-ranker tail. Paid — kill-switch gated.
	if o.teacher == nil || !oracleEnabled() {
		return "", false
	}
	return o.teacher.Label(ctx, query)
}

// compile-time assertion that twoTierOracle satisfies the activelearn seam without
// importing activelearn here (the LabelAndSave signature is the contract).
var _ interface {
	LabelAndSave(ctx context.Context, text string, vec []float64) bool
} = twoTierOracle{}
