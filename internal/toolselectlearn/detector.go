// Package toolselectlearn is the tool-selection active-learning loop (D-06/D-07,
// spike 057) — the SECOND consumer of the shared internal/activelearn mechanism
// after the reasoning learner. It detects mis-routed turns cheaply (a shell/fs
// fallback tool was used, OR the used-tool != the ranker's top-1), labels the
// confident cases with the free ranker and escalates the low-margin tail to the
// existing DeepSeek router (the two-tier oracle), then persists confirmed
// (query-embedding -> tool) exemplars to :ToolSelectionExample so the tool_search
// ranker's per-tool centroids self-improve. All async, off the hot path: the runner
// calls Observe(request, usedTool) post-tool-execution and it never blocks the turn.
package toolselectlearn

import (
	"context"
	"strings"
)

// nsDelimiterStr is the "<namespace>__<tool>" delimiter from the mcptools
// namespacing (08.1-03), duplicated as a literal (NOT imported — package boundaries:
// toolselectlearn must not depend on internal/agent/mcptools). It mirrors the same
// literal in internal/agent/tools/search.go:120; a 2-char delimiter is a stable
// cross-package contract, not shared state.
const nsDelimiterStr = "__"

// bareTool returns a tool's bare name: the segment AFTER the last "__" namespace
// delimiter (whatsapp__send_message -> send_message), or the whole string when
// un-namespaced. Splitting on the LAST delimiter handles multi-segment namespaces.
func bareTool(name string) string {
	if i := strings.LastIndex(name, nsDelimiterStr); i >= 0 {
		return name[i+len(nsDelimiterStr):]
	}
	return name
}

// sameTool is SYMMETRIC tool identity: a bare name (send_message) and any namespaced
// form (whatsapp__send_message) are the same tool, and the comparison gives the SAME
// answer regardless of argument order. It splits BOTH names on nsDelimiterStr="__"
// and compares the bare segments — deliberately a bare-name equality, never a suffix
// test.
//
// RESEARCH risk #3 / spike-057: an order-sensitive suffix match (a is-a-suffix-of
// "__"+b, OR b is-a-suffix-of "__"+a) produced a spurious false positive — e.g.
// "search" is a suffix of "web_search" but they are different tools, and the suffix
// form also mis-handles two distinct namespaces sharing a bare name asymmetrically.
// The bare-name split fixed detector precision 0.78 -> 0.88.
func sameTool(a, b string) bool {
	if a == b {
		return true
	}
	return bareTool(a) == bareTool(b)
}

// fallbackTools are the generic "the model improvised" tools (the bitcoin
// skill->fs_glob->shell_exec chain): using one of these when a dedicated tool existed
// is the classic inefficiency signal. shell/fs-fallback detection alone is P0.88/R1.00
// and needs NO embedding (spike-057) — it directly encodes the bitcoin failure mode.
var fallbackTools = map[string]bool{
	"shell_exec": true, "shell_poll": true, "shell_kill": true,
	"fs_read": true, "fs_glob": true, "fs_grep": true, "skill": true,
	"text_response": true,
}

// isFallback reports whether the executed tool is a generic-improvisation fallback.
// It compares on the BARE name so a namespaced fallback (rare) is still caught.
func isFallback(usedTool string) bool {
	return fallbackTools[bareTool(usedTool)] || fallbackTools[usedTool]
}

// Ranker is the narrow seam the loop uses to ask the free semantic ranker two
// things about a request: (1) "what tool would you choose?" (top1, for the
// mis-route disagreement signal) and (2) "how confident are you?" (the top-2 margin,
// for the two-tier free-vs-escalate label gate). It is satisfied by the tool_search
// semindex ranker (the per-tool centroid bank). ok is false when the ranker is
// unwired or the bank is empty — then the detector uses only the embedding-free
// shell/fs heuristic, and the oracle escalates (no free label is trustworthy).
//
// Rank takes a ctx (CR-01): ranking embeds the deferred corpus + the request, and that
// I/O runs entirely off the turn goroutine on the learner's intake/activelearn workers,
// bounded by the learner's lifetime ctx so Close aborts any in-flight embed.
type Ranker interface {
	Rank(ctx context.Context, query string) (top1 string, margin float64, ok bool)
}

// top1 is a small helper so the detector can ask only for the best tool.
func top1Of(ctx context.Context, r Ranker, query string) (string, bool) {
	if r == nil {
		return "", false
	}
	t, _, ok := r.Rank(ctx, query)
	return t, ok
}

// isMisroute applies the spike-057 detection rule: a turn (request -> usedTool) is a
// mis-route candidate when a shell/fs-fallback tool was used OR the used-tool != the
// ranker's top-1 for the request (symmetric sameTool). The shell/fs branch needs no
// ranker; the disagreement branch is skipped when the ranker is unavailable (ok=false)
// rather than firing a spurious flag. An empty request or an empty usedTool is never a
// mis-route (no tool was executed, or no signal). It is called only from the intake
// worker (CR-01) — never on the turn goroutine — so the ranker's embed I/O is off the
// hot path; ctx is the learner lifetime, so Close cancels an in-flight rank.
func isMisroute(ctx context.Context, request, usedTool string, ranker Ranker) bool {
	if strings.TrimSpace(request) == "" || usedTool == "" {
		return false
	}
	if isFallback(usedTool) {
		return true
	}
	top1, ok := top1Of(ctx, ranker, request)
	if !ok {
		return false
	}
	return !sameTool(usedTool, top1)
}
