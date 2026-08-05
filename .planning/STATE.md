---
gsd_state_version: '1.0'
milestone: v2.1.0
milestone_name: HERMES-CLAUDE_PARITY
status: planning
last_updated: "2026-08-05T00:00:00.000Z"
last_activity: 2026-08-05
progress:
  total_phases: 8
  completed_phases: 0
  total_plans: 0
  completed_plans: 0
  percent: 0
---

# Project State

## Project Reference

See: .planning/PROJECT.md (updated 2026-08-05)

**Core value:** When Aura says she did something, she did it — and she can find what she knew.
**Current focus:** Phase 45 — Harness correctness (idempotency replay fix + memory-write guardrails)

## Current Position

Phase: 45 of 52 (1 of 8 in v2.1.0 HERMES-CLAUDE_PARITY) — Harness correctness
Plan: — (not yet planned)
Status: Ready to plan
Last activity: 2026-08-05 — ROADMAP.md created; 52 v1 requirements mapped across Phases 45-52 with 0 unmapped

Progress: [░░░░░░░░░░] 0%

## Performance Metrics

**Velocity:**
- Total plans completed: 0
- Average duration: — min
- Total execution time: 0 hours

**By Phase:**

| Phase | Plans | Total | Avg/Plan |
|-------|-------|-------|----------|
| - | - | - | - |

**Recent Trend:**
- Last 5 plans: —
- Trend: —

*Updated after each plan completion*

## Accumulated Context

### Decisions

Decisions are logged in PROJECT.md Key Decisions table. Recent decisions affecting current work:

- Roadmap creation: build order is F-1 idempotency fix (45) → MCP trust/facade (46) →
  tool-surface ceremony strip (47) → tool-surface un-defer/merges (48) → memory tiers (49) →
  context ladder (50, parallel-safe) → summarization spike (51) → milestone exit (52).
- Tool-surface work deliberately split across two phases (47, 48) rather than one — it
  touches live persisted state (COMPAT-01/02/03) and has distinct blast radii per PITFALLS.md.
- MEM-06 (PRD amendment extending #91) is a committed step inside Phase 49, landing before
  any reasoning-tier code commit — not a separate phase, since it's a within-phase sequencing
  rule (CLAUDE.md PRD-amendment-before-code), not a standalone deliverable.
- CTX-06 (Phase 51) is a spike, not an implementation phase — its output decides whether
  CTX-V2-01 (LLM summarization) gets promoted into a follow-on Phase 53 or a decimal
  insertion. Not pre-scheduled in this roadmap.
- Corrected REQUIREMENTS.md's v1 requirement count from a stated 51 to the actual 52 (direct
  count of unique REQ-IDs) during roadmap creation — a miscount at definition time, not a
  scope change.

### Pending Todos

None yet.

### Blockers/Concerns

- Phase 45's key-shape fix direction depends on an unverified fact (Pitfall 5): whether
  Aura's transport-retry path reuses the same `tool_call_id` on retry or mints a fresh one.
  Must be established empirically before choosing the fix direction — flagged for Phase 45's
  own planning/discussion, not resolved by research.
- Phase 48's un-defer step normally needs a tool-choice-accuracy eval harness per Pitfall 1,
  but ACC-02 (no new eval harness) supersedes that — Phase 48 must instead verify via a live
  before/after scenario comparison against `aura.tool_invocations`. Flagged so this
  substitution isn't lost during planning.

## Deferred Items

Items acknowledged and carried forward from previous milestone close:

| Category | Item | Status | Deferred At |
|----------|------|--------|-------------|
| Context | CTX-V2-01 (LLM summarization rung) | Conditional — promoted only if Phase 51's spike selects it | v2.1.0 requirements definition |
| Context | CTX-V2-02 (durable cross-restart anti-thrash state) | Deferred | v2.1.0 requirements definition |
| Tool surface | TOOL-V2-01 (merge fs_glob/fs_grep) | Deferred, blocked on telemetry | v2.1.0 requirements definition |
| Tool surface | TOOL-V2-02 (provider reasoning-block replay) | Deferred, not needed by current provider | v2.1.0 requirements definition |

## Session Continuity

Last session: 2026-08-05
Stopped at: ROADMAP.md, REQUIREMENTS.md (traceability), and STATE.md written for v2.1.0
Resume file: None
