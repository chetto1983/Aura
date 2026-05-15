# Phase 6 - Add the Tool Experience Loop: Current Scope Summary

Synthesised from `.planning/deep-refactor/Phase06_Tool_Experience_Loop/`
scaffolds + `prd.md` §6 Phase 6, 2026-05-15.

## A. PRD §6 Phase 6 verbatim

**Goal:** Aura improves from preventable tool-call failures instead of
repeating them.

**Steps:**

- define `ToolObservation` as the single result/error contract for inline
  and workflow-backed tools,
- classify tool results as ok, recoverable error, blocked error, fatal
  error, or cancelled,
- add a Tool Supervisor that enforces retry policy, redaction, idempotency,
  and retry budgets,
- inject recoverable error feedback into the same run,
- cap retries by run, tool, and error kind, and record why a retry was
  attempted or refused,
- route stateful, long-running, side-effecting, background, cron, outbound,
  source-ingest, memory-write, wiki-mutation, and swarm-spawn tools through
  durable workflow execution,
- require idempotency keys for every retryable side-effecting operation,
- require reconcile-first behavior for `side_effect_unknown`,
- persist tool attempts and outcomes as learning events,
- retrieve validated lessons for similar future tool calls,
- promote repeated lessons into memory, skills, or tool policy only after
  validation.

**Gate:**

- a recoverable tool error can be corrected in the same run,
- repeat failures are visible by tool and error kind,
- workflow-backed tool steps survive process restart and do not double-apply
  side effects,
- `side_effect_unknown` is never blindly retried,
- secrets and raw sensitive args are redacted from learning records,
- retrieved lessons are versioned against tool schema/version,
- no automatic prompt/code mutation happens without validation.

## B. Current scaffold state (pre-refresh)

| File | Status | Goal | Notes |
|---|---|---|---|
| plan.md | self-audited scaffold, not verified | Aura learns from preventable tool-call failures | Scope mirrors PRD; non-goals: no auto prompt mutation, no blind retry, no raw secrets |
| source.md | source audit complete, pending targeted rereads | PRD Phase 6 + AGENTS.md transaction boundaries + tool execution surface + storage/workflow code | Missing: current workflow/outbox map |
| benchmark.md | planned (no tests run) | Observation tests, retry budget tests, unknown side-effect reconciliation, redaction tests, full compile/vet/test | Empty actuals |
| progress.md | clean scaffold (2026-05-15) | n/a | Blocker: needs source map + verifier |

## C. Outstanding questions

From source.md "Missing Source Questions":

- Current workflow/outbox facilities must be mapped before implementation.
  → Partial answer in `docs/phase06-current-state-audit-2026-05-15.md`
  section A (no durable workflow exists today; cron + scheduler are
  in-process; conversation archive is the closest persistent surface).
