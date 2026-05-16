# Phase06 Plan - Add the Tool Experience Loop

Status: closed 2026-05-16 for the Phase-J in-scope tool-experience slice.
Durable workflow, idempotency, reconcile-first handling, and lesson promotion
remain deferred to Phase-K.

## Goal

Aura improves from preventable tool-call failures instead of repeating them.
Concretely: every tool result gets a typed `ToolObservation` outcome; failures
are persisted to a versioned `tool_attempts` table; the agent loop injects
"we have failed like this before" briefings before the next planning round;
operators see a structured `tool_warnings` channel. No automatic
prompt/code mutation — promoted lessons remain operator-reviewed.

## Audits informing this plan

- `docs/phase06-current-state-audit-2026-05-15.md` — 22 already-mitigated
  failure paths, 10 error labels in `classifyToolError`, `run_events` shape,
  conversation archive captures tool_calls JSON, gap list.
- `docs/phase06-industry-patterns-2026-05-15.md` — D:/tmp shortlist:
  ADOPT #1 nanobot per-turn signature throttle, ADOPT #2 Elysia per-tool
  error ledger, ADOPT #3 cli-printing-press `tool_warnings`. REJECT nanobot
  Dream consolidator (defer).
- `docs/phase06-scaffold-summary-2026-05-15.md` — PRD §6 Phase 6 verbatim.
- `docs/aura-main-loop-limits-audit.md` §5 — flags Phase 6 as the explicit
  fix for the 🟡 "Observation handling" + "In-run self-healing" partials.

## Scope (Phase-J Ralph queue, 7 stories)

In scope:

- typed `ToolObservation` outcome contract (5 buckets: ok / recoverable /
  blocked / fatal / cancelled),
- `tool_attempts` SQLite table (migration v10) with args hashed, never raw,
- in-run recoverable-error feedback injection,
- per-tool error ledger pre-LLM briefer (ADOPT #2 Elysia shape),
- per-(tool, error-class) retry budget + reason recording,
- structured `tool_warnings` operator channel (ADOPT #3 cli-printing-press
  shape),
- Phase 6 closure docs.

Deferred to Phase-K (transaction boundaries — orthogonal milestone):

- durable workflow execution for side-effecting tools (outbound, cron,
  source-ingest, memory-write, wiki-mutation, swarm-spawn),
- idempotency keys required for retryable side-effecting operations,
- reconcile-first behavior for `side_effect_unknown`,
- lesson promotion to memory / skills / policy (needs validated lessons
  first — the experience loop has to PRODUCE signal before it can be
  promoted).

The deferral is documented because the experience loop must EXIST and
generate validated lessons before any promotion is meaningful. Building
durable workflows + promotion infrastructure in the same phase risks
shipping cold pipes.

## Non-Goals

- No automatic prompt mutation. Lessons are visible to the operator and
  injected as pre-LLM context only.
- No automatic skill creation. Promotion is operator-reviewed.
- No raw secret/argument values in tool_attempts rows — SHA-256 hashes
  only, plus the existing redaction vocabulary from
  `internal/agent/tools/registry/error.go`.
- No new external dependency. Hand-rolled where the existing libs don't fit.

## PRD Coverage (post-Phase-J)

| PRD Item | Story | Status |
|---|---|---|
| ToolObservation contract (ok/recoverable/blocked/fatal/cancelled) | US-J01 | met |
| Tool Supervisor (retry, redaction, idempotency, budgets) | US-J03/J05 | met for retry/redaction/budgets; idempotency keys deferred |
| Recoverable error feedback injection same-run | US-J03 | met |
| Caps by run/tool/error-kind + reason recording | US-J05 | met |
| Persist tool attempts as learning events | US-J02 | met |
| Retrieve validated lessons for similar future calls | US-J04 | met via pre-LLM briefer |
| Durable workflow execution for side-effecting tools | Phase-K | deferred |
| Idempotency keys for retryable side-effects | Phase-K | deferred |
| Reconcile-first for side_effect_unknown | Phase-K | deferred |
| Promote validated lessons to memory/skills/policy | Phase-K | deferred |
| Secrets/raw args redacted in learning records | US-J02 | met |
| Lessons versioned against tool schema/version | US-J02 | met (tool_schema_hash) |
| No auto prompt/code mutation without validation | (non-goal) | locked |

## Ralph queue (Phase-J)

- **US-J01** — `ToolObservation` outcome contract.
  Add `internal/agent/tools/registry/observation.go` with `Outcome` typed
  enum (ok / recoverable / blocked / fatal / cancelled). Add a small
  classifier that maps the existing `classifyToolError` 10 labels into
  the 5 buckets. `executor.go` emits `ToolObservation{Outcome, Class,
  Reason, RedactedArgs}` per tool call to the existing OnEvent channel.
  Tests cover all 10 → 5 mappings + executor wiring. NO downstream
  consumer yet (next stories).

- **US-J02** — `tool_attempts` SQLite table + persistence.
  Migration v10: `tool_attempts(id, run_id, tool_name, attempt_n,
  outcome, class, reason, args_hash, args_redacted_keys, tool_schema_hash,
  started_at, ended_at)`. Args are SHA-256 hashed; redacted_keys is a
  JSON array of argument key names (no values). `tool_schema_hash` is a
  digest of `ToolDefinition.Parameters` so future lookups know which
  schema the failure was against (PRD: "lessons versioned against tool
  schema/version"). A new `internal/agent/tools/attempts/` package
  exposes `Repo.Record(ctx, ToolObservation) error` + `Repo.Recent(ctx,
  tool_name string, n int) ([]Attempt, error)`. Executor calls
  `Repo.Record` after each ToolObservation. Tests: migration on fresh +
  v9 DB, Record roundtrip, Recent ordering, no secret values in any row.

- **US-J03** — In-run recoverable-error feedback injection.
  When `Outcome == recoverable`, the loop injects a tool-message-style
  "self-heal hint" into the next LLM round (analog to nanobot's
  `appendValidationNudge` shape). The hint summarises the class + reason
  + last attempt's redacted args. Caps: max one hint per (tool, run);
  hint length ≤ 400 chars; only fires when retries remain in budget.
  Tests: recoverable error within budget → hint injected on next round;
  budget exhausted → no hint, observation marked fatal; non-recoverable
  outcomes never trigger the path.

- **US-J04** — Pre-LLM tool-experience briefer (ADOPT #2 Elysia shape).
  Before each LLM round in `loop.go`, fetch `Repo.Recent(tool_name, 3)`
  for every tool currently in the per-turn pool. Inject as a structured
  system-message capsule (≤ 200 chars per tool, ≤ 8 tools surfaced).
  Strictly read-only — the model SEES history but the loop NEVER auto-
  corrects arguments. Tests: pool with 3 tools, each with 1 prior
  failure → 3 entries in the capsule; pool of 12 → only top 8 by
  recency; empty history → no capsule injected (no noise).

- **US-J05** — Per-(tool, error-class) retry budget + reason recording.
  New `Options.RetryBudgets map[ErrorClass]int` on the agent loop.
  Default: recoverable=2, blocked=0, fatal=0, cancelled=0. When an
  attempt exhausts its class budget, the next attempt of the same
  (tool, class) is refused with a structured reason logged into
  `tool_attempts.reason` ("retry_budget_exhausted_for_class:X").
  Tests: budget=2 + 3 recoverable errors → 3rd refused; budget=0 +
  1 blocked → refused immediately.

- **US-J06** — `tool_warnings` operator channel (ADOPT #3
  cli-printing-press shape).
  New endpoint `GET /api/tool-warnings` returns recent failures grouped
  by (tool_name, class) with counts + last_seen. The endpoint reads
  `tool_attempts` and is operator-only (admin gate). Backed by
  `Repo.Aggregate(ctx, lookback time.Duration) ([]ToolWarning, error)`.
  Tests: 5 mixed failures across 2 tools → 2-row aggregate response;
  empty repo → empty array; bearer auth required (consistent with rest
  of /api).

- **US-J07** — Phase 6 closure docs.
  Append progress.md row with SHAs. Update benchmark.md actuals.
  Update prd.md §6 Phase 6 with "Phase 6 closed 2026-MM-DD (in-scope
  slice)" + a note that durable-workflow + idempotency + promotion are
  deferred to Phase-K. Update aura-main-loop-limits-audit.md §5 to flip
  Observation handling + In-run self-healing from 🟡 partial to ✅.

## Implementation Gate

Closed. No story was accepted without tests that exercise the actual loop path
(not mocks). The PRD §6 gates that fall within scope are:

- ✓ a recoverable tool error can be corrected in the same run (US-J03)
- ✓ repeat failures are visible by tool and error kind (US-J06)
- ✓ secrets and raw sensitive args are redacted from learning records (US-J02)
- ✓ lessons are versioned against tool schema/version (US-J02 schema_hash)
- ✓ no automatic prompt/code mutation happens without validation (non-goal)

Deferred-to-Phase-K gates (NOT required for Phase-J closure):

- workflow-backed tool steps survive process restart and do not double-apply
- `side_effect_unknown` is never blindly retried
