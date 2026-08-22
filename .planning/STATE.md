---
gsd_state_version: 1.0
milestone: v2.1.0
milestone_name: HERMES-CLAUDE_PARITY
current_phase: 46
current_phase_name: mcp-trust-and-facade
status: blocked
stopped_at: "Phase 46 HALTED at dispatch: 46-02 Task 1 premise falsified by live measurement (see 46-HALT-2026-08-22.md)"
last_updated: "2026-08-22T10:15:53.414Z"
last_activity: 2026-08-22
last_activity_desc: "Phase 46 execution halted before wave 1 — WhatsApp mount premise falsified, replan pending"
progress:
  total_phases: 3
  completed_phases: 2
  total_plans: 26
  completed_plans: 17
---

# Project State

## Project Reference

See: .planning/PROJECT.md (updated 2026-08-05)

**Core value:** When Aura says she did something, she did it — and she can find what she knew.
**Current focus:** Phase 46 — mcp-trust-and-facade

## Current Position

Phase: 46 (mcp-trust-and-facade) — HALTED BEFORE WAVE 1 (no executor dispatched, no commits)
Status: Blocked — 46-02/46-08/46-09 rest on a falsified premise; see 46-HALT-2026-08-22.md
Phase 46 discussion are recorded in `46-CONTEXT.md` D-10..D-16 and in ROADMAP §45.1.
Last activity: 2026-08-22 — execution halted at dispatch; live measurement of the running sidecar contradicts 46-RESEARCH
requirements as falsified or already-shipped, and inserted Phase 45.1 ahead of it by operator
decision ("use mcp client native no bespoke").

Phase 46 is context-complete but **blocked on five amendments** (46-CONTEXT.md D-05..D-09) and on
45.1 landing first: every file it touches sits on the MCP client seam.
Last reconciliation: 2026-08-13 — this file's counts re-measured against ROADMAP.md (see
Session Continuity). No work was executed on that date.

Progress: [████████░░] 82%

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
- [Phase 45.1]: 45.1-04: trusted-recipe branch left byte-identical when adopting IdempotentHint/OpenWorldHint -- operator curation outranks a server hint, and wiring the new hints there would have re-tiered create_event, download_media and memory_upsert_fact, putting an approval prompt in front of ordinary work
- [Phase 45.1]: 45.1-04: in the fallback path destructiveHint:false alone no longer earns the mutating tier -- idempotentHint defaults false and openWorldHint defaults true, so a server must now declare the call repeatable or closed-world to earn it; an untrusted server must not talk itself out of an approval gate with the cheapest hint
- [Phase 45.1]: 45.1-04: completed inline by the orchestrator after three gsd-executor dispatches died to a 600s stream watchdog (one at launch with an empty transcript, two mid-run); operator authorised the deviation
- [Phase 45.1]: 45.1-06: elicitation_surface = decline-and-surface — the handler declines but delivers the ask on the operator's channel naming the server; no in-flight turn is blocked and no row is written to aura.paused_states. Option B (mint-and-wait) stays available later behind the same ElicitationConsent seam
- [Phase 45.1]: 45.1-06: elicitation timeout = 300s via its OWN env var AURA_MCP_ELICITATION_TIMEOUT_SEC — the operator asked to reuse the gateway approval default, but measurement showed no approval timeout/TTL exists anywhere (approvals are an async cross-turn ledger with nothing held waiting); the value was honoured, the source could not be
- [Phase 45.1]: 45.1-07: on protocol 2026-07-28 a server does NOT send elicitation/create mid-request -- the SDK refuses it outright and the live path is an InputRequests map fulfilled by clientMultiRoundTripMiddleware; the first test draft had it wrong and only a real in-memory CallTool revealed it
- [Phase 45.1]: 45.1-07: obs exposes emission ONLY through Boundary (outcome derived from the error), so the plan's seven-valued action counter was not buildable without widening a shared package; reused MCPCallsID with a new catalog operation value and put the finer action in the structured log
- [Phase 45.1]: 45.1-07: the consent surface is late-bound because MCP mounts run before the channels Registry exists; follows the existing cron.ChannelDeliverer pattern rather than reordering boot

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
- 45.1-08 must also run a mutation spot-check on internal/agent/mcptools/bridge_risk.go -- not run during 45.1-04; the file is at 100% per-function coverage and bridge_supervisor.go scored 99/99, but neither is a mutation score for this file
- 45.1-08 must also cover 45.1-07: no mutation spot-check on elicitation.go/elicitation_consent.go, and no LIVE E2E of a real mounted server issuing a real elicitation to a real channel -- the in-memory pair proves the protocol path, not a Telegram delivery
- Env catalog gap (found in 45.1-07, PRD-amendment shaped): the whole AURA_MCP_* family is uncatalogued in prd.md -- AURA_MCP_MOUNT_TIMEOUT and AURA_MCP_SHUTDOWN_TIMEOUT are absent, AURA_MCP_CALL_TIMEOUT_SEC appears only in amendment prose, and AURA_MCP_ELICITATION_TIMEOUT_SEC was deliberately not added alone

## Deferred Items

Items acknowledged and carried forward from previous milestone close:

| Category | Item | Status | Deferred At |
|----------|------|--------|-------------|
| Context | CTX-V2-01 (LLM summarization rung) | Conditional — promoted only if Phase 53's spike selects it | v2.1.0 requirements definition |
| Context | CTX-V2-02 (durable cross-restart anti-thrash state) | Deferred | v2.1.0 requirements definition |
| Tool surface | TOOL-V2-01 (merge fs_glob/fs_grep) | Deferred, blocked on telemetry | v2.1.0 requirements definition |
| Tool surface | TOOL-V2-02 (provider reasoning-block replay) | Deferred, not needed by current provider | v2.1.0 requirements definition |

## Session Continuity

Last session: 2026-08-17T07:52:25.550Z
Stopped at: Phase 46 context re-measured after 45.1 (D-23..D-38 added)
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

Resume file: .planning/phases/46-mcp-trust-and-facade/46-CONTEXT.md
Next action: `/gsd-discuss-phase 45`, then `/gsd-plan-phase 45`.
