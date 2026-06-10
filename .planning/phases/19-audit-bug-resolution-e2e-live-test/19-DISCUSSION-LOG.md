# Phase 19: Audit Bug Resolution + E2E Live Test - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-06-10
**Phase:** 19-audit-bug-resolution-e2e-live-test
**Areas discussed:** Finding scope / triage, Contract build vs downgrade, Live E2E validation bar, False-green test strategy

---

## Area selection

User selected all four offered gray areas (multiSelect) and added a freeform steer:
"we must debug with live agent (paid) with user prompt no baby sisttin" — which set the
direction for the Live E2E validation bar before it was discussed.

---

## Finding scope / triage

| Option | Description | Selected |
|--------|-------------|----------|
| HIGH + all MEDIUM + folded LOWs | All HIGH+MEDIUM; fold only M-i's free-rider env LOWs + L3; defer L1/L2/L4/L5/L6 + INFO | |
| Everything — HIGH+MED+all LOW | All HIGH + MEDIUM + every LOW; triage INFO; zero residue | ✓ |
| HIGH + cluster MEDIUMs only | All HIGH + only M-a/M-b/M-i; defer standalone MEDIUMs + all LOWs | |

**User's choice:** Everything — HIGH+MED+all LOW.
**Notes:** Total scope, zero audit residue. INFO (bundled-script scan) treated as
logged/accepted per trust-model amendment #50 unless overridden — user did not override.

---

## Contract build vs downgrade (H6/H7/H10/M-g)

| Option | Description | Selected |
|--------|-------------|----------|
| Build minimal-real, all four | Build reconnect wrapper + recovery-flag wire + scheduler notify/retry persistence | ✓ (via freeform) |
| Build decided/cheap, downgrade scheduler pair | Build H10+M-g; downgrade H6/H7 to honest comments, defer durable queue | |
| Downgrade all four, defer machinery | Correct every doc/comment, ship zero machinery | |

**User's choice (free-text):** "i want aura full functional no stupit quik win."
**Notes:** Interpreted as Build minimal-real, all four — the doc-downgrade cop-out is
rejected. "Minimal" governs shape (no supervisor/over-engineering), not ambition (must
actually function). H10 reconnect was already the decided design regardless.

---

## Live E2E validation bar

| Option | Description | Selected |
|--------|-------------|----------|
| Live-repro user-visible + regression for all | Regression test every finding; real-agent live before/after for every user-observable finding; non-observable = regression-only; live = operator sign-off gate | ✓ |
| Live-first everything, regression follows | Drive every applicable finding live first (incl. synthetic rigs), backfill regression | |
| Live smoke per cluster, not per finding | Regression every finding; one consolidated live smoke per cluster | |

**User's choice:** Live-repro user-visible + regression for all.
**Notes:** Pre-steered by the freeform "debug with live agent (paid) with user prompt, no
babysitting" — Layer 2 uses the real paid agent and real user prompts, not scripted
fixtures. Live pass is a required operator sign-off gate, not CI-automated (paid/manual stack).

---

## False-green test strategy

| Option | Description | Selected |
|--------|-------------|----------|
| Rewrite as broken + wire orphan fixture | Rewrite false-green assertions (justified per CLAUDE.md broken-test clause) + consume premature_close.sse | ✓ |
| Add new tests, leave old untouched | Conservative; leaves misleading tests as future traps | |
| Rewrite + delete truly-dead artifacts | Rewrite + prune, risking discard of staged fixtures | |

**User's choice:** Rewrite as broken + wire orphan fixture.
**Notes:** Targets fanout sub-sequence re-validation, shell-timeout child-death assertion,
context_boundary tool-round coverage, and wiring (not deleting) premature_close.sse.

---

## Claude's Discretion

- H9 streaming-contract approach (`llm.Chunk.Err` field vs treat-no-finish_reason-as-retryable)
- Wave sequencing (H4+H5+M-b cluster as candidate wave 1)
- Commit granularity (per finding vs per tight cluster)
- M-a/M-b stream-retry consolidation through `streamWithOpenRetry`

## Deferred Ideas

None — the phase deliberately absorbs the entire audit; no finding backlog carried forward.
