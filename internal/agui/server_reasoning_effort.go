package agui

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/chetto1983/aura/internal/llm"
	"github.com/chetto1983/aura/internal/runner"
)

// server_reasoning_effort.go carries the two-stage per-turn reasoning-effort governance the
// run handler applies (37E / WEBMODEL-02/03, D-05/D-13). It is split out of server.go (which
// keeps parseEffortSymbol + the SetReasoningCapabilitySource setter) so that file stays under
// the 600-LOC ceiling — the plan sanctions extracting the two-stage helper to a sibling.
//
// The control is the no-bypass enforcement (WEBMODEL-03): the client sends a symbol, never a
// ReasoningConfig. Stage 1 (syntactic) rejects any non-enum value; Stage 2 (capability) rejects
// a real-enum level the ACTIVE model does not advertise, and when detection failed collapses to
// the safe floor (only off/auto pass). A validated fixed level threads via
// runner.WithReasoningOverride; the original symbol is persisted owner-scoped so it restores on
// reopen (D-06/OQ-3).

// applyReasoningEffort runs the two stages, threads a validated fixed override onto ctx, and
// persists the symbol owner-scoped. It returns the (possibly override-carrying) ctx and ok. On
// any rejection it has ALREADY written the 400 and returns ok=false so handleRun stops; the
// caller MUST use the returned ctx (a fixed level rides it into runner.buildAgent).
//
// Stage 2 reads s.reasoningCaps from its in-memory TTL cache — NO per-request round-trip to the
// provider (T-37E-06-DOS: a flood cannot amplify into OpenRouter calls). When the source is
// unwired (nil) Stage 2 is skipped and a syntactically valid fixed level passes (graceful
// degrade — the endpoint separately reports the {auto,off} floor).
func (s *Server) applyReasoningEffort(ctx context.Context, w http.ResponseWriter, threadID, symbol string) (context.Context, bool) {
	// Stage 1 — SYNTACTIC: the 7-symbol enum (pure, no I/O). A non-enum value is the
	// enum-injection surface and is rejected 400 (T-37E-06-ENUM / WEBMODEL-03).
	effort, isFixed, ok := parseEffortSymbol(symbol)
	if !ok {
		http.Error(w, "invalid reasoning effort", http.StatusBadRequest)
		return ctx, false
	}
	// Stage 2 — CAPABILITY: a FIXED level must be advertised by the active model (D-13). When
	// detection failed only off/auto are safe (the graduated levels are unverifiable, D-12).
	if isFixed && s.reasoningCaps != nil {
		allowed, _, detected := s.reasoningCaps.AllowedEfforts(ctx)
		if detected {
			if !containsEffort(allowed, effort) {
				http.Error(w, "effort not supported by the active model", http.StatusBadRequest)
				return ctx, false
			}
		} else if effort != llm.ReasoningEffortNone {
			http.Error(w, "effort not verifiable; only off/auto available", http.StatusBadRequest)
			return ctx, false
		}
	}
	// Thread ONLY a fixed, validated level — auto/absent leaves ctx untouched so the agent runs
	// today's adaptive path (D-04, zero regression).
	if isFixed {
		ctx = runner.WithReasoningOverride(ctx, effort)
	}
	// Persist the ORIGINAL symbol (incl. auto/absent → "" hydrates back to auto, OQ-3) owner-
	// scoped. Ownership is already gated by GetForIdentity upstream, so a persist error or 0
	// rows-affected is logged, never fatal — the turn is the primary action, persistence is a
	// best-effort preference write (only the seven validated symbols ever reach it, T-37E-06-XSS).
	if _, err := s.conv.UpdateReasoningEffortForIdentity(ctx, threadID, scopedIdentityID(ctx), symbol); err != nil {
		slog.Warn("agui: persist reasoning effort", "err", err)
	}
	return ctx, true
}

// containsEffort reports whether the advertised set carries effort. It backs the Stage-2
// capability check; the set is small (≤6 levels) so a linear scan is right.
func containsEffort(set []llm.ReasoningEffort, effort llm.ReasoningEffort) bool {
	for _, e := range set {
		if e == effort {
			return true
		}
	}
	return false
}
