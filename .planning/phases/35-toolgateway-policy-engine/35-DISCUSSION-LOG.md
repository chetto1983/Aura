# Phase 35: ToolGateway + Policy Engine - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-07-03
**Phase:** 35-toolgateway-policy-engine
**Areas discussed:** Reservation ledger, Mutating classification, Approve path, GATE-02 scope
**Method:** User selected all 4 gray areas + explicitly requested research on 2026 industrial patterns (web) and the curated `D:/tmp` repos before deciding. Three parallel `gsd-advisor-researcher` agents produced code-grounded comparison tables; the four forks were then confirmed in one pass. All four landed on the recommended (minimal-industrial) option.

---

## Reservation ledger + policy-decision storage (GATE-03/04)

| Option | Description | Selected |
|--------|-------------|----------|
| Reuse, zero migration | Reservation = synchronous pre-Execute `start` insert into existing `tool_invocations`; idempotency = existing UNIQUE index; verdict in `meta jsonb`. No migration 0026. | ✓ |
| Reuse + minimal ALTER 0026 | Same reuse + additive `decision text` (+ optional `principal`) column for first-class SQL-queryable audit evidence. | |
| New `tool_reservations` table | Dedicated mutable `reserved→committed/aborted` state machine beside the ledger. | |

**User's choice:** Reuse, zero migration.
**Notes:** Direct continuation of Phase-34 D-06/D-07 (the append-only ledger Aura already writes IS the durable-execution log; `:exec→:execrows` + the existing UNIQUE index give idempotency-replay). A new mutable table would manufacture the dual-write D-07 rejected. Real work = making the `start` insert synchronous *before* the mutating `Execute()` (today it's persisted reactively via the async event stream). Migration 0026 deferred to Phase 39.

---

## Mutating classification for action-multiplexed tools (skill/task/swarm_spawn)

| Option | Description | Selected |
|--------|-------------|----------|
| Hybrid: floor + de-escalate | `Mutating=true` floor on the 3 tools + gateway classifier de-escalating ONLY enumerated reads; unknown/parse-fail → Risky; `swarm_spawn` flat Risky; reuse `scoring`. Optional boot-guard. | ✓ |
| Conservative wholesale | Just `Mutating=true` on the 3; flat boolean, no arg peek. Over-gates `skill list`/`task list`. | |
| Action-aware only | Per-action tiers, no `Mutating` floor. Precise but unsafe as sole line (future unlisted tool under-gates). | |

**User's choice:** Hybrid (B-primary classification + A structural fallback).
**Notes:** Default always mutating; the classifier is a monotone de-escalator that only lowers explicitly-enumerated read actions. Ground-truth arg shapes confirmed in code: `skill`/`task` carry `action`; `swarm_spawn` carries only `goals` (flat Risky). `scoring.ComputeSkillTier` is mutation-only (feeding it `list` returns Risky) → read allowlist lives in the gateway table. Prerequisite for the reservation's crash-orphan reconciliation (can't safely reconcile a mutating orphan without a trustworthy flag).

---

## `approve` verdict runtime behavior (GATE-01)

| Option | Description | Selected |
|--------|-------------|----------|
| Route by responder-presence | Emit+record verdict; interactive (single principal) → reuse `ask_user` pause + approval center (persist-before-act, resume re-enters PEP); headless → deny-with-guidance (D-25); multi-identity interactive deferred to Phase 36. | ✓ |
| Deny-with-guidance only | `approve` → structured deny everywhere + recorded verdict; no pause wiring this phase. | |

**User's choice:** Route by responder-presence.
**Notes:** Every primitive (ask_user pause/resume, Phase-25 approval center, Phase-29 skills pause, D-25 headless auto-reject) already ships — the gateway only routes; no new UX. Default = deny unless an interactive responder is positively known (a mistaken pause of a headless run hangs it). Multi-identity interactive routing under `server_production` pulls Phase 36 (MUSR owner-scoping) → until then `approve` degrades to deny-with-guidance under production.

---

## GATE-02 (command-hook fail-closed) scope

| Option | Description | Selected |
|--------|-------------|----------|
| Verify-only + assert coverage | Treat GATE-02 as satisfied by the shipped `FailClosed` default + per-hook `fail_policy`; add tests proving timeout/crash/non-zero DENY; pin coverage. No new knob. | ✓ |
| Also add global env knob | Add a profile-level `AURA_COMMAND_HOOK_FAIL_POLICY` override on top of the per-hook JSON. | |

**User's choice:** Verify-only + assert coverage.
**Notes:** `commandHookFailPolicy` already defaults `FailClosed` with hardening tests; the requirement's "…or `AURA_COMMAND_HOOK_FAIL_POLICY`" is an `or` the default already meets. No new config surface.

## Claude's Discretion

- Gateway package name/shape (`internal/gateway` + `Decide` interface) + composition-root injection; ensuring **both** Execute call sites (`llm_agent_retry.go`, `llm_agent_pause.go`) route through the single PEP.
- Replay-fidelity contract (default: tolerate a missing F-040-GC'd sidecar → preview + `result expired`; don't extend retention).
- Optional `Multiplexed bool` Spec hint + `Registry.Validate` boot-guard.
- Grace-window constant (mirror `tmpTTL`); tx boundary of the reservation insert; `gateway_approval` ResumeContext wording; OTel span/overhead assertion.
- Under `dev`/`local_trusted` the gateway is a pure host-direct no-op (no recording) per Success Criterion 4.

## Deferred Ideas

- Multi-identity interactive approval routing → Phase 36 (MUSR + Authula).
- Migration 0026 + OTel metric path + `/readyz` + alert/dashboard YAML → Phase 39.
- Additive `ADD COLUMN decision text` for queryable audit evidence → only if governance later requires it.
- Sandbox routing of host shell/fs into per-identity container → Phase 37 (SBX).
- MCP governance limits → Phase 38.
- Verbatim exact-bytes replay (extend sidecar retention) → only if telemetry shows preview-replay insufficient.
- Declarative YAML/Rego/Cedar policy language + quorum approval → OUT (anti-feature; the Go table over `internal/scoring` is the locked form).
