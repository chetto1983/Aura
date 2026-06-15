# Phase 20: scheduler-hardening-full-implementation - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-06-11
**Phase:** 20-scheduler-hardening-full-implementation
**Areas discussed:** Origin routing key, Origin-preference switch, Live-verify depth, Channel fan-out order, SPEC reconciliation

Mode: SPEC-locked (ambiguity 0.12) — discussion limited to implementation forks. User requested a deep D:/tmp + online industrial-pattern sweep before deciding; three research agents ran (1 local codebase/curated-sources sweep, 2 online comparison-table researchers). All four forks were locked to the research-recommended option.

---

## Fork 1 — Origin routing key (deleted-origin resilience)

| Option | Description | Selected |
|--------|-------------|----------|
| Snapshot identity_id | Capture stable `identity_id` at schedule time; deliver by it; dispatcher reads `task.IdentityID` directly (drops conv→identity dispatch seam); keep `origin_conversation_id` for context. Survives deleted origin conversation. | ✓ |
| SPEC default (resolve at dispatch) | Store only `origin_conversation_id`; resolve identity at dispatch via `IdentityForConversation`; deleted conv → NULL → degrade to notify route. | |

**User's choice:** Snapshot identity_id (Recommended)
**Notes:** Research provenance — transactional-outbox immutable-key-at-enqueue + Klaviyo's explicit snapshot-at-schedule mode. `conversation_id` is the wrong durability anchor (`ON DELETE SET NULL` → silent fallback). `cron.Task.IdentityID` already exists, so this is a snapshot, not a new column. Bonus: removes the dispatch-time conversation-lookup seam (R4 simplification). Refines SPEC R1/R4.

---

## Fork 2 — Origin-preference switch (kill-switch vs always-on)

| Option | Description | Selected |
|--------|-------------|----------|
| Env kill-switch, default ON | Add `AURA_SCHEDULER_PREFER_ORIGIN_CHANNEL` (default on via `envBoolDefault`); operator can revert to known-good route without recompile. | ✓ |
| Always-on once wired (SPEC default) | No new env; "nil deps = off, wired = on"; rollback = unwire at composition root (a dev action). | |

**User's choice:** Env kill-switch, default ON (Recommended)
**Notes:** Both researchers converged: minimal-shape generally favors always-on, BUT a delivery/routing regression fails *silently and user-visibly* — the exact Fowler ops/kill-switch case. Near-zero cost (convention + `envBoolDefault` helper already exist). Additive to SPEC (added to Constraints + Acceptance + env catalog follow-up).

---

## Live-verify depth

| Option | Description | Selected |
|--------|-------------|----------|
| Live both (force quiet window) | Live-verify Step 1 (CDP harness) + Step 2 by forcing the quiet-hours window to cover "now"; integration tests as regression guard. | ✓ |
| Live Step 1 + integration-test Step 2 | Live only the headline immediate path; Step 2 via DB+fake-channel integration test. | |
| Live both via natural quiet hours | Drive Step 2 through a real configured quiet-hours window (slower / time-of-day dependent). | |

**User's choice:** Live both (force quiet window) (Recommended)
**Notes:** Step 2 becomes cheaply live-testable by forcing the quiet window to "now". Honors the "probe must verify the artifact, not the reply" + no-skip-as-green discipline. Step 2 is a hard gate, not advisory.

---

## Channel fan-out order

| Option | Description | Selected |
|--------|-------------|----------|
| Deterministic order now | Replace map-iteration fan-out with a stable order (sort by name / explicit `Priority`); defer the per-identity preference engine. | ✓ |
| Defer entirely (SPEC default) | Keep first-delivers-wins over map iteration; revisit at 2nd Deliverer channel. | |

**User's choice:** Deterministic order now (Recommended)
**Notes:** Courier "Best Of" / Novu / AWS Pinpoint all use declared order, never map iteration. One stable sort. Per-identity preference engine deferred (YAGNI — Telegram is the only `Deliverer` today).

---

## SPEC reconciliation (process)

| Option | Description | Selected |
|--------|-------------|----------|
| Amend SPEC + write CONTEXT | Update 20-SPEC.md R1/R2/R4 + Constraints + Acceptance + new env, then write CONTEXT; commit together. | ✓ |
| CONTEXT only (planner reconciles) | Leave SPEC as-is; CONTEXT overrides; planner reconciles the stale R4 seam. | |

**User's choice:** Amend SPEC + write CONTEXT (Recommended)
**Notes:** Avoids handing the planner a contradiction (R4's now-removed `IdentityForConversation` seam). `20-SPEC.md` amended in the same discuss-phase commit.

## Claude's Discretion

- Exact env var name (`AURA_SCHEDULER_PREFER_ORIGIN_CHANNEL` proposed).
- Deterministic order via sort-by-name vs explicit `Priority()` (cheapest acceptable since unobservable with one channel).
- `deliverToOrigin` file placement under the ≤600-LOC rule.

## Deferred Ideas

- Per-identity channel preference engine (defer to a real 2nd live channel).
- True group-origin delivery (needs a conv→channel-address binding; breaks identity-keyed model).
- whatsapp/email as `Deliverer` channels (vs MCP self-send routes).
- PRD env-catalog entry for `AURA_SCHEDULER_PREFER_ORIGIN_CHANNEL` (phase-close housekeeping).
