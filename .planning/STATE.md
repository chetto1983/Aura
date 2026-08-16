---
gsd_state_version: 1.0
milestone: v2.1.0
milestone_name: HERMES-CLAUDE_PARITY
current_phase: 45.1
current_phase_name: native-mcp-client
status: executing
stopped_at: Phase 45.1 context gathered
last_updated: "2026-08-16T17:38:04.760Z"
last_activity: 2026-08-16
last_activity_desc: Phase 46 discussed; 45.1 (native MCP client) inserted as its prerequisite
progress:
  total_phases: 3
  completed_phases: 1
  total_plans: 17
  completed_plans: 12
---

# Project State

## Project Reference

See: .planning/PROJECT.md (updated 2026-08-05)

**Core value:** When Aura says she did something, she did it — and she can find what she knew.
**Current focus:** Phase 45.1 — native-mcp-client

## Current Position

Phase: 45.1 (native-mcp-client) — EXECUTING
Plan: 2 of 8
Status: Ready to execute
Phase 46 discussion are recorded in `46-CONTEXT.md` D-10..D-16 and in ROADMAP §45.1.
Last activity: 2026-08-16 — Phase 45.1 execution resumed (wave continue)
requirements as falsified or already-shipped, and inserted Phase 45.1 ahead of it by operator
decision ("use mcp client native no bespoke").

Phase 46 is context-complete but **blocked on five amendments** (46-CONTEXT.md D-05..D-09) and on
45.1 landing first: every file it touches sits on the MCP client seam.
Last reconciliation: 2026-08-13 — this file's counts re-measured against ROADMAP.md (see
Session Continuity). No work was executed on that date.

Progress: [███████░░░] 71%

## Performance Metrics

**Velocity:**

- Total plans completed: 9
- Average duration: — min
- Total execution time: 0 hours

**By Phase:**

| Phase | Plans | Total | Avg/Plan |
|-------|-------|-------|----------|
| 45 | 9 | - | - |

**Recent Trend:**

- Last 5 plans: —
- Trend: —

*Updated after each plan completion*
**Per-Plan Metrics:**

| Plan | Duration | Tasks | Files |
|------|----------|-------|-------|
| Phase 45 P03 | 55min | 3 tasks | 3 files |
| Phase 45 P04 | 70min | 3 tasks | 6 files |
| Phase 45 P06 | 150min | 3 tasks | 7 files |

## Accumulated Context

### Decisions

Decisions are logged in PROJECT.md Key Decisions table. Recent decisions affecting current work:

- Roadmap creation: build order is F-1 idempotency fix (45) → MCP trust/facade (46) →
  tool-surface ceremony strip (47) → tool-surface un-defer/merges (48) → memory tiers (49) →
  context ladder (50, parallel-safe) → durable delegation (51) → mid-turn steering (52) →
  summarization spike (53) → milestone exit (54).

- Durable delegation (51) and mid-turn steering (52) were added to the milestone on
  2026-08-05 by `1844cbfd9`, after the original 8-phase roadmap was written — both are
  operator decisions with pre-existing approved designs under `docs/superpowers/specs/`,
  not fresh design work. They pushed the spike 51→53 and the milestone exit 52→54.

- Tool-surface work deliberately split across two phases (47, 48) rather than one — it
  touches live persisted state (COMPAT-01/02/03) and has distinct blast radii per PITFALLS.md.

- MEM-06 (PRD amendment extending #91) is a committed step inside Phase 49, landing before
  any reasoning-tier code commit — not a separate phase, since it's a within-phase sequencing
  rule (CLAUDE.md PRD-amendment-before-code), not a standalone deliverable.

- CTX-06 (Phase 53) is a spike, not an implementation phase — its output decides whether
  CTX-V2-01 (LLM summarization) gets promoted into a follow-on integer phase after 54 or a
  decimal insertion before the Phase 54 exit gate. Not pre-scheduled in this roadmap.

- v1 requirement count: **77**, measured 2026-08-13 by direct count of unique REQ-IDs in
  REQUIREMENTS.md and cross-checked against ROADMAP.md's per-phase `**Requirements**:` lines
  (77 mapped, each to exactly one phase, 0 unmapped, 0 double-mapped). History: the count was
  corrected from a stated 51 to an actual 52 at definition time — a miscount, not a scope
  change — and then genuinely grew to 77 as delegation, steering and the follow-up
  tool-surface commits landed on 2026-08-05.

- [Phase 45]: Boot-time operation-metadata guard (D-09) checks the three Mutating-tool fields for EMPTINESS only, positioned inside ValidateClassifiable's existing reg.All() loop BEFORE the multiplexed-classifier continue, so non-multiplexed mutating tools are also covered
- [Phase 45]: 45-03's Task-2 span attributes are set inside execTool on the span already carried by ctx (stampReplayAttributes), the smaller of the two acceptable plumbing shapes named in the plan
- [Phase 45]: 45-04: RED commits ship a compiling identity-passthrough stub instead of an undefined symbol, because the pre-commit vet gate runs go vet ./... over the whole tree
- [Phase 45]: 45-04: llm_agent.go crossed the 590-LOC refactor threshold after both dedup call sites landed (583->592); both measured extraction candidates split into new llm_agent_round.go (roundBudget, recordRequestBuilt), landing at 556/70 for headroom against plans 45-05..08
- [Phase 45]: 45-04: three pre-existing test fixtures (fanout-cap, two panic-recovery tests) used byte-identical (name,args) calls in one message as a convenience fixture; D-12's new dedup now correctly collapses those, so each fixture was given distinct per-call args rather than weakening the new dedup
- [Phase 45]: 45-06: A refused correction (fact_key miss, or 0/>1 candidates) writes NOTHING -- no fact closed AND the new fact itself is not created, beyond the plan's literal 'closes nothing' text, to avoid orphaning or adding to an already-ambiguous candidate set
- [Phase 45]: 45-06: proseObjectRuneBound=80, measured live against the operator identity graph (longest legit Entity.name 36 runes, shortest measured prose violation 96 runes) rather than guessed
- [Phase 45.1]: 45.1-03: dark-code guard (darkcode_test.go) scans non-test .go files with comments stripped via go/scanner rather than a bare recursive grep -- this phase's own SDK-era wire-emulation test fixtures and doc comments legitimately contain deleted-symbol tokens (Mcp-Session-Id, notifications/initialized, decodeToolResult) that a literal substring scan would false-positive on
- [Phase 45.1]: 45.1-03: full aggregate coverage gate (scripts/coverage_docker.sh) not run -- a concurrent unrelated session (plan 45.1-04) held compose.yaml and bridge_risk.go dirty in the same checkout for the whole plan; per-package measurement (internal/mcp 90.5%, internal/agent/mcptools 92.1%) used as the honest substitute, aggregate gate deferred to when the tree is quiescent

### Pending Todos

None yet.

### Blockers/Concerns

- ~~Phase 45's key-shape fix direction depends on an unverified `tool_call_id` fact
  (Pitfall 5).~~ **Resolved 2026-08-05 by `657c9e383`**, from hermes
  (`agent/message_sanitization.py:536-566`, `run_agent.py:4601-4648`): do NOT key on
  `tool_call_id` at all. Providers reuse one id across a batch and strict providers —
  DeepSeek, Aura's default — reject duplicates, so uniqueness is a property the harness must
  enforce, not one it can rely on. The chosen direction is a per-turn **round ordinal** in
  the child operation key, discriminating at the round boundary the way hermes does.

- What remains open for Phase 45 is narrower and is a discuss/research item, not a blocker:
  confirm the round-ordinal shape against Aura's own dispatch loop before building (ROADMAP
  Phase 45 says so explicitly), and decide how far MEM-04/05 entity resolution goes here
  versus in Phase 49.

- Phase 48's un-defer step normally needs a tool-choice-accuracy eval harness per Pitfall 1,
  but ACC-02 (no new eval harness) supersedes that — Phase 48 must instead verify via a live
  before/after scenario comparison against `aura.tool_invocations`. Flagged so this
  substitution isn't lost during planning.

- 45.1-08 (phase close) must re-run bash scripts/coverage_docker.sh (full aggregate 85% owned-surface gate) once plan 45.1-04 lands and the tree is quiescent -- not run during 45.1-03 because a concurrent unrelated in-flight session held compose.yaml and internal/agent/mcptools/bridge_risk.go dirty in the same checkout

## Deferred Items

Items acknowledged and carried forward from previous milestone close:

| Category | Item | Status | Deferred At |
|----------|------|--------|-------------|
| Context | CTX-V2-01 (LLM summarization rung) | Conditional — promoted only if Phase 53's spike selects it | v2.1.0 requirements definition |
| Context | CTX-V2-02 (durable cross-restart anti-thrash state) | Deferred | v2.1.0 requirements definition |
| Tool surface | TOOL-V2-01 (merge fs_glob/fs_grep) | Deferred, blocked on telemetry | v2.1.0 requirements definition |
| Tool surface | TOOL-V2-02 (provider reasoning-block replay) | Deferred, not needed by current provider | v2.1.0 requirements definition |

## Session Continuity

Last session: 2026-08-16T17:37:21.587Z
Stopped at: Phase 45.1 context gathered
exited at its CONTEXT.md gate — Phase 45 has no CONTEXT.md, and discuss-phase must run as a
top-level command (nested invocation breaks AskUserQuestion, GSD #1009). No phase directory
was created and no planning agents were spawned.

While reporting that, STATE.md was found describing the milestone's *first* shape (8 phases,
45-52, 52 requirements) rather than its current one. ROADMAP.md and the REQUIREMENTS.md
traceability table were already correct; only the prose around them had drifted. Reconciled
on 2026-08-13: STATE.md frontmatter `total_phases` 8→10, current position 45-of-52→45-of-54,
build order extended with Phases 51/52 and the 53/54 renumber, requirement count re-measured
to 77, the `tool_call_id` blocker marked resolved by `657c9e383`, and the CTX-V2-01 deferral
re-pointed at Phase 53. REQUIREMENTS.md's two stale prose lines corrected to match.

Resume file: .planning/phases/45.1-native-mcp-client/45.1-CONTEXT.md
Next action: `/gsd-discuss-phase 45`, then `/gsd-plan-phase 45`.
