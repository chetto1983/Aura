# Phase 22: bug-fix - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-06-15
**Phase:** 22-bug-fix
**Areas discussed:** Validation bar, Operational defaults, Cross-pkg boundary, Execution strategy

---

## Area selection (gray-area triage)

| Option | Description | Selected |
|--------|-------------|----------|
| Validation bar | Automated done-bar vs live sign-off pass | ✓ (initial) |
| Operational defaults | New-knob values + posture | ✓ (2nd pass) |
| Cross-pkg boundary | NEEDS-CONFIRMATION + cross-package reach | ✓ (2nd pass) |
| Execution strategy | Codex vs GSD, commits, branch, waves | ✓ (2nd pass) |

**Notes:** User first picked only "Validation bar"; after seeing the SPEC-grounded
defaults for the other three, chose to discuss all three rather than accept defaults.

---

## Validation bar

| Option | Description | Selected |
|--------|-------------|----------|
| Automated + live spot-check | Automated hard gate + focused live spot-check on observable surface | |
| Automated done-bar only | Tests + race/goleak + coverage + mutation + CI, no live pass | |
| Full live sign-off pass | Phase 19/20 parity — full live real-agent E2E across the phase | ✓ |

**User's choice:** Full live sign-off pass.
**Notes:** Automated done-bar stays the per-finding gate; full live pass added on top as phase-close gate.

| Option (harness) | Description | Selected |
|--------|-------------|----------|
| Daemon + host chat + /metrics | aura serve + aura chat tool-trace + curl /metrics | |
| Add Telegram CDP too | Above + CDP Telegram round-trip (full 19/20 parity) | ✓ |
| You decide per finding | Planner right-sizes harness per finding | |

**User's choice:** Add Telegram CDP too. The live pass must also drive the full tool surface incl. GLM OCR multimodal (from the Operational-defaults discussion).

| Option (test discipline) | Description | Selected |
|--------|-------------|----------|
| Fail-before where deterministic | Fail-before/pass-after for deterministic; race-class via race/goleak green | |
| Strict fail-before for ALL | Every finding shown red pre-fix, incl. race-class | ✓ |
| Green-after is enough | Only require pass-after | |

**User's choice:** Strict fail-before for ALL.
**Notes:** Race-class "fail-before" satisfied by `go test -race`/`goleak` flagging the race/leak pre-fix, clean post-fix.

| Option (ledger location) | Description | Selected |
|--------|-------------|----------|
| docs/audit/ (parity) | 22-finding-ledger.md + 22-LIVE-SIGNOFF-<date>.md in docs/audit/ | ✓ |
| In the phase dir | Both under .planning/phases/22-bug-fix/ | |

**User's choice:** docs/audit/ (parity with 19-LIVE-SIGNOFF).

---

## Operational defaults

| Option (fs cap) | Description | Selected |
|--------|-------------|----------|
| 10 MiB, reject + page hint | stat-then-reject over 10 MiB, paging hint | ✓ (starting value) |
| 50 MiB, reject + page hint | More headroom | |
| Auto-page, no reject | Transparent first-chunk read | |

**User's choice:** Free-text — "here we can make some test on glm ocr and all tool in E2E."
**Notes:** Start at 10 MiB reject+hint, but treat the exact value as E2E-validated against the full tool surface incl. GLM OCR multimodal. Widens the live sign-off to drive all tools.

| Option (timeout flip) | Description | Selected |
|--------|-------------|----------|
| Document + boot-log | Apply flip, boot-log resolved value, migration note | ✓ (via E2E) |
| Hard-fail on 0 at boot | Refuse boot if 0 set | |
| Keep 0=infinite, new sentinel | No breaking change | |

**User's choice:** Free-text — "we can test on E2E."
**Notes:** Apply flip (0→default, -1→infinite) + boot-log; prove bounded-hang behavior in the E2E pass.

| Option (router) | Description | Selected |
|--------|-------------|----------|
| Opt-in OFF, abstain→Low (tunable) | SPEC R6 target wording | |
| Opt-in OFF, abstain→Medium | Safer abstain tier | |
| Keep router ON, just bound it | Embed breaker + ~2s cap, router stays default-ON | ✓ |

**User's choice:** Keep router ON, just bound it.
**Notes:** Refines SPEC R6 target wording (no "default OFF"); R6 acceptance still holds ("one call then breaker-open, latency unaffected"). Preserves router routing quality. No SPEC amendment required.

| Option (MCP breaker) | Description | Selected |
|--------|-------------|----------|
| 3 fails / 30s / 500ms→30s / 10s | Standard resilient defaults | ✓ |
| More tolerant (5 / 60s) | Fewer false trips | |
| Researcher tunes from MCP norms | Capture shape, planner picks numbers | |

**User's choice:** 3 fails / 30s / 500ms→30s / 10s reconnect timeout, single-flight off-lock; env-tunable.

---

## Cross-pkg boundary

| Option (AG-041) | Description | Selected |
|--------|-------------|----------|
| Land it here | Wire Budget.WithDeadline at cmd/aura/agent.go in this phase | ✓ |
| Route the cmd/aura part out | Land only internal/agent plumbing, route wiring | |

**User's choice:** Land it here — the internal/agent fix is inert without the composition-root wiring.

| Option (AG-034) | Description | Selected |
|--------|-------------|----------|
| Confirm, then split | internal/agent event.go here; route pure-persistence redaction out | ✓ |
| Pull it all in here | Fix the whole thing incl. DB projection | |

**User's choice:** Confirm, then split.

| Option (pkg posture) | Description | Selected |
|--------|-------------|----------|
| internal/agent + named one-liners | Stay in internal/agent + cmd/aura AG-041; route true cross-pkg | ✓ |
| Fix small cross-pkg spillover too | Opportunistic adjacent fixes | |

**User's choice:** internal/agent + named one-liners. AG-028 + AG-043 land here; B-01/B-03/M-01 stay out; route others with ledger entries.

---

## Execution strategy

| Option (engine) | Description | Selected |
|--------|-------------|----------|
| Hybrid: Codex impl + I commit | GSD plans+gates; Codex implements; Claude reviews+commits | |
| Full GSD execute-phase | Wave-based gsd-executor subagents end-to-end | ✓ |
| Solo, in-session | Claude implements directly turn-by-turn | |

**User's choice:** Full GSD execute-phase. (Note: deviates from the usual Codex parallel-session pattern; chosen for the strict per-finding gate + ledger discipline.)

| Option (commits) | Description | Selected |
|--------|-------------|----------|
| One finding / tight cluster | Atomic, AG-### ref per commit | ✓ |
| Per-wave mega-commit | One commit per wave for velocity | |
| Strictly one finding per commit | Max granularity | |

**User's choice:** One finding / tight cluster per atomic commit with AG-### ref.

| Option (branch) | Description | Selected |
|--------|-------------|----------|
| Master-direct | Commit on master, no PR unless asked | ✓ |
| Phase branch | Dedicated branch merged at close | |

**User's choice:** Master-direct.

| Option (wave gate) | Description | Selected |
|--------|-------------|----------|
| Automated per wave, live at close | Each wave automated-green before next; live once at close | ✓ |
| Full gate per wave | Live sign-off subset at end of each wave | |
| Planner decides parallelization | Risk-first order as hint, planner composes | |

**User's choice:** Automated per wave, live at close.

---

## Claude's Discretion

- Exact `AURA_FS_MAX_READ_BYTES` value within the 10 MiB starting point (E2E-tuned).
- Final intra-wave parallelization + tight-cluster commit grouping (subject to /gsd-plan-phase).
- Exact MCP breaker env-var names (within `AURA_<DOMAIN>_<UNIT>`).

## Deferred Ideas

- Full multi-tenant security (AG-007 / AG-003 / AG-011 full slices) — future security phase.
- Other-package prior-cycle findings (B-01 / B-03 / M-01) — their own phases.
- OPS/deployment (prod container, /readyz, per-thread in-flight guard).
- AG-034 DB-projection redaction — routed out if it lives entirely in persistence.
