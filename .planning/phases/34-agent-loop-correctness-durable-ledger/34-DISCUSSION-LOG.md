# Phase 34: Agent-Loop Correctness + Durable Ledger - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-07-01
**Phase:** 34-agent-loop-correctness-durable-ledger
**Areas discussed:** HITL resume/pause durability, Sidecar security + lifecycle, Spilled-content search, Agent-facing loop behavior

**Method:** User selected all four gray areas and requested research of 2026 industrial patterns + the curated `D:/tmp` repos. Four parallel `gsd-advisor-researcher` agents produced code-grounded comparison tables (live Aura code + web + `D:/tmp/adk-go-study`, `D:/tmp/codex/codex-rs`). All four decisions locked on the research-recommended, minimal-industrial option.

---

## HITL resume/pause durability (LOOP-02/03/04 · F-004/029/030)

| Option | Description | Selected |
|--------|-------------|----------|
| Single transaction, no ledger | One `db.WithTx` spans claim+append (single & batch) + atomic pause exposure; no migration; idempotency = existing `WHERE resumed_at IS NULL` guard; matches ADK-Go `applyEvent`. Revises the roadmap goal (drops the ledger/migration). | ✓ |
| Build the durable ledger (migration) | New resume-ledger table + idempotency key + boot/periodic repair worker, per the roadmap text. | |

**User's choice:** Single transaction, no ledger.
**Notes:** The ledger/outbox family solves a dual-write problem Aura doesn't have here (all writes are single-pool). A single tx makes the "claimed-without-answer" orphan structurally impossible — reconciling it with a repair worker would be the "atomic bomb" project guidance rejects. Consequence: **no new migration in Phase 34**; roadmap goal text to be reconciled at plan time (D-07). External resume-relay-hook exactly-once deferred (only genuine dual-write).

---

## Sidecar security + crash-orphan lifecycle (LOOP-05/09 · F-005/040)

| Option | Description | Selected |
|--------|-------------|----------|
| Reconstruct + `os.Root` + age-grace GC | Fence reads by reconstructing path from `(runDir, convID, seq)` via `os.Root` (column = did-spill flag); fix both `loadTurns` + `loadBranchTurns`; crash-orphans via age-grace reconcile in the existing sweeper, scoped to `.content`. No migration/backfill; mirrors `read_tool_output`. | ✓ |
| Schema key column + temp/rename writes | Migrate `content_sidecar_path` to an explicit key + backfill; temp-file+rename write semantics. | |

**User's choice:** Reconstruct + `os.Root` + age-grace GC.
**Notes:** Two vulnerable reads to fix, not one (`loadTurns` + `loadBranchTurns`). Orphan sweep must be scoped strictly to the `.content` suffix so co-located `.result` tool-output sidecars survive. Temp+rename rejected — it trades a benign leak for a malignant unreadable-history failure mode and still needs a GC backstop.

---

## Spilled (>64 KiB) conversation content search (LOOP-10 · F-048)

| Option | Description | Selected |
|--------|-------------|----------|
| Document + assert exclusion | LOOP-10-blessed; one comment + one asserting test; short-preview upgrade path preserved. | ✓ |
| Add a searchable preview column | Migration + expression index; partial fidelity; edits the locked cross-slice search query. | |
| Sidecar tsvector index table | Net-new table + a second search paradigm bolted next to trigram. | |

**User's choice:** Document + assert exclusion.
**Notes:** Decisive fact — Aura's search is length-normalized trigram `similarity()`, so a >64 KiB spilled turn scores ~0 and wouldn't surface even if `content` were repopulated. Search infra buys ≈ nothing; spill is rare. Preserves a clean short-preview upgrade path if telemetry ever justifies it.

---

## Agent-facing loop behavior — terminal exclusivity + send_file (LOOP-01/06 · F-003/009)

| Option | Description | Selected |
|--------|-------------|----------|
| Hard-reject mixed step + deterministic send_file error | (a) `text_response` + any tool call → reject whole step, force replan (zero reliance on the incomplete `Mutating` flag; also fixes 2nd-`text_response` drop). (b) send_file outside workspace → deterministic unsupported error, remove dead approval route. | ✓ |
| Allow read-only siblings + wire send_file approval hook | (a) Allow read-only siblings — requires a tool-classification hardening project first or it's unsafe. (b) Build the ~225-LOC send-file approval ledger + resume hook. | |

**User's choice:** Hard-reject mixed step + deterministic send_file error.
**Notes:** Matches native semantics (Anthropic/OpenAI/LangGraph: any tool call ⇒ not final). Option B unsafe today — `skill`/`task`/`swarm_spawn` are unflagged and action-multiplexed, so "read-only siblings" would let `skill action=create` run beside a final answer. `send_file` approval-ledger rejected — no product requirement for cross-workspace egress; Codex's deterministic `Reject` branch is the precedent.

---

## Claude's Discretion

- Exact `ResumeCommitter` seam name/shape + composition-root pool injection; `os.Root` open cadence (per-load vs per-read); age-grace constant (mirror `tmpTTL`); wording of the documented search-exclusion.
- Optional code comment flagging the `skill`/`task`/`swarm_spawn` mutating-classification gap as a Phase-35 note (do NOT fix classification here — scope fence).

## Deferred Ideas

- Targeted `resume_relay_outbox` for the external resume-relay hook (only if it later proves non-idempotent).
- Complete/action-aware tool `Mutating` classification → Phase 35 ToolGateway.
- send_file cross-workspace egress approval subsystem → only on a real requirement.
- Short-preview / tsvector spilled-content search → future phase on telemetry.
- fsync file+dir power-loss durability → separate knob.
- Mutating-tool durable ledger reservation + per-profile runtime enforcement → Phase 35.
