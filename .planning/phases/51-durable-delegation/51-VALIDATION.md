---
phase: 51
slug: durable-delegation
# status lifecycle: draft (seeded by plan-phase) → validated (set by validate-phase §6)
# audit-milestone §5.5 distinguishes NOT-VALIDATED (draft) from PARTIAL (validated + nyquist_compliant: false) (#2117)
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-08-27
---

# Phase 51 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.
> Seeded by `/gsd-plan-phase` from `51-RESEARCH.md` §Validation Architecture.
> The Per-Task Verification Map is filled by the planner; task IDs do not exist yet.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | Go `testing` + `gotestsum`; `db_integration` build tag for real-Postgres tests; `arcadedb_integration` for the graph. Precedent: `internal/documents/jobs_store_test.go`, `jobs_worker_test.go`, `integration_pool_helper_test.go` |
| **Config file** | `internal/documents/integration_pool_helper_test.go` (DSN/pool bootstrap pattern to copy) |
| **Quick run command** | `go test ./internal/swarm/... ./internal/steer/... ./internal/agent/...` |
| **Full suite command** | `go build ./... && go vet ./... && go test ./... && go test -race ./internal/agent/... ./internal/gateway/... ./internal/swarm/... ./internal/runner/... ./internal/steer/... ./internal/documents/...` |
| **Estimated runtime** | ~180 seconds (unit + race); `db_integration` matrix adds ~120s with the stack up |

**Identity discipline (CLAUDE.md, non-negotiable):** every `db_integration` test here MUST run as
`aura_app`, never as the superuser `aura` role. `aura` is superuser+BYPASSRLS, so a hand-run as
`aura` produces a FALSE GREEN on any identity-scoping bug in the new delegation rows — which carry
`identity_id` exactly like `aura.ingestion_jobs` does today. Copy the coverage gate's
`aura_app`/`aura_migrate` DSN composition; verify with `bash scripts/coverage_docker.sh` before
trusting a green run.

---

## Sampling Rate

- **After every task commit:** targeted unit tests for the package just touched — `go test ./internal/<pkg>/`
- **After every plan wave:** the full suite command above (build + vet + test + race on the seven touched packages)
- **Before `/gsd-verify-work`:** full suite green AND the full `db_integration` matrix green as `aura_app`
- **Phase gate:** a fresh live-driven scenario per SC#1–SC#5 against the running stack, scored per CLAUDE.md's >9.8 bar. **A green test suite does not close this phase** (ACC-01/ACC-02 policy, established Phase 45).
- **Max feedback latency:** 180 seconds

---

## Per-Task Verification Map

*Filled by `gsd-planner`. Task IDs are assigned when PLAN.md files are written.*

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| 51-03-F1 | 51-03 | 2 | SWARM-01 | T-51-11 | context text cannot forge a brief section marker | unit | `go test ./internal/swarm/ -run 'TestStructuredBrief|TestBriefContext'` | ❌ W0 | ⬜ pending |
| 51-03-F2 | 51-03 | 2 | SWARM-02 | T-51-12 | caps rendered are operational limits, not secrets | unit | `go test ./internal/agent/tools/ -run TestSwarmSpawnSpecReflectsConfig` | ❌ W0 | ⬜ pending |
| 51-01-T1 | 51-01 | 1 | SWARM-03, SWARM-09 | T-51-01, T-51-02 | report delivered under `steer.SourceWorker`; rows identity-scoped, tested as `aura_app` | db_integration | `go test -tags=db_integration ./internal/swarm/ -run 'TestBackgroundDelegationEnqueues|TestDelegationClaimReclaim'` | ❌ W0 | ⬜ pending |
| 51-01-T2 | 51-01 | 1 | SWARM-03 | T-51-03 | claim loop carries `gateway.WithDelegatedDispatch` | unit | `go test ./cmd/aura/...` | ✅ | ⬜ pending |
| 51-01-T3 | 51-01 | 1 | SWARM-03 | T-51-01 | live tracer: turn returns early, report re-enters conversation | live E2E | checkpoint — `drive-sc.sh` precursor | ❌ W0 | ⬜ pending |
| 51-02-T2 | 51-02 | 2 | SWARM-03, SWARM-09 | T-51-06, T-51-07 | `source` round-trips; foreign `identity_id` invisible, as `aura_app` | db_integration | `go test -tags=db_integration ./internal/steer/ -run 'TestPostgresSteerQueue|TestQueueTTLDerivedPerKind|TestConcurrentDrain'` | ❌ W0 | ⬜ pending |
| 51-02-T3 | 51-02 | 2 | SWARM-09 | T-51-08, T-51-09 | expired row leaves a trace in the same transaction (D-08) | db_integration + unit | `go test -tags=db_integration ./internal/steer/ -run TestExpireDue` | ❌ W0 | ⬜ pending |
| 51-05-T1 | 51-05 | 3 | SWARM-04, SWARM-05 | T-51-18, T-51-19 | depth cap enforced by registry AND `checkDepth`; child never wider than parent | unit + race | `go test -race ./internal/swarm/ -run 'TestNestedDelegationSynchronous|TestSwarmDepthGuard'` | ❌ W0 (extend `swarm_test.go:464`) | ⬜ pending |
| 51-05-T2 | 51-05 | 3 | SWARM-08 | T-51-20 | re-entry guard keys on scope AND fingerprint on the new dispatch paths | unit (regression guard for `67d24aee4`) | `go test ./internal/agent/ -run TestDeriveToolOperationContext` | ✅ extend `idempotency_operation_test.go:209` | ⬜ pending |
| 51-06-T2 | 51-06 | 3 | SWARM-06 | T-51-22, T-51-23, T-51-25 | fenced conditional UPDATE inside `CommitResumeBatch`'s transaction, as `aura_app` | db_integration | `go test -tags=db_integration ./internal/runner/ -run 'TestPerWorkerPauseFencing|TestWorkerPauseLazyExpiry'` | ❌ W0 | ⬜ pending |
| 51-06-T3 | 51-06 | 3 | SWARM-06 | T-51-24, T-51-26 | sibling pauses independent; expiry writes a trace atomically | db_integration | `go test -tags=db_integration ./internal/runner/ -run 'TestWorkerPauseSiblingIndependence|TestExpireWorkerPauses'` | ❌ W0 | ⬜ pending |
| 51-04-T2 | 51-04 | 2 | SWARM-07 | T-51-14 | run id + writer role host-derived; model has no field to lie in | unit + race | `go test -race ./cmd/arcadedb-mcp/... ./internal/arcadedb/...` | ❌ W0 | ⬜ pending |
| 51-04-T3 | 51-04 | 2 | SWARM-07 | T-51-15, T-51-16 | worker supersede refused before any Command; real goroutine fan-out | race + arcadedb_integration | `go test -race -tags=arcadedb_integration ./internal/arcadedb/ -run TestConcurrentWorkerFactWrite` | ❌ W0 | ⬜ pending |
| 51-07-T1 | 51-07 | 4 | SWARM-10 | T-51-28, T-51-29 | path traversal rejected; foreign conversation returns 404 | unit + race | `go test -race ./internal/swarm/ -run TestTranscript && go test -race ./internal/agui/...` | ❌ W0 (`report_test.go` already covers the writer) | ⬜ pending |
| 51-07-T2 | 51-07 | 4 | SWARM-11 | — | amendment precedes implementation | manual-only | `git log --format='%h %cs %s' -- prd.md` | N/A | ⬜ pending |
| 51-08-T1 | 51-08 | 5 | SWARM-01..10 | T-51-35 | verdicts from reassembled deltas, never an SSE grep | live driver | `bash -n .planning/phases/51-durable-delegation/drive-sc.sh` | ❌ W0 | ⬜ pending |
| 51-08-T2 | 51-08 | 5 | SWARM-01..10 | T-51-33, T-51-34 | SC#1–SC#5 scored >9.8 on the running stack, HEAD-fresh image | live E2E (checkpoint) | `bash .planning/phases/51-durable-delegation/drive-sc.sh` | ❌ W0 | ⬜ pending |
| 51-08-T3 | 51-08 | 5 | SWARM-01..10 | — | snapshot re-attested; coverage floor ≥85% | gate | `bash scripts/quality_snapshot_gate.sh && bash scripts/coverage_docker.sh` | ✅ | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- [ ] `internal/agent/tools/swarm_spawn_schema_test.go` (or extend `swarm_spawn_test.go`) — SWARM-02's live-cap rendering
- [ ] `internal/swarm/brief_context_test.go` — SWARM-01's goal/context split
- [ ] `internal/swarm/delegation_queue_test.go` (`db_integration`, as `aura_app`) — SWARM-03/09 enqueue + claim/reclaim, exercising the **real `ClaimIngestionJobs` Go path**, closing spike 100's own stated gap
- [ ] `internal/runner/worker_pause_test.go` (`db_integration`, as `aura_app`) — SWARM-06's fencing column and per-worker pause
- [ ] `internal/arcadedb/concurrent_fact_write_test.go` (`-race`, `arcadedb_integration`) — SWARM-07 with genuine goroutine fan-out, not sequential calls
- [ ] A phase-specific live driver script, modelled on `.planning/spikes/098-steer-carries-worker-result/drive.sh` and `.planning/spikes/099-worker-duration-and-progress/drive.sh`, covering SC#1–SC#5 (`internal/eval`'s harness stays deleted)
- [ ] Migration(s): D-06 (Postgres steer/delegation-result queue table), D-12 (`ALTER aura.paused_states ADD COLUMN` fencing id). D-10 needs **no** migration — it is a Go-only schema change. **The slot number is read from `ls internal/db/migrations/ | tail -1` at landing time, never copied from a document.**

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Operator keeps talking mid-delegation; the consolidated worker result lands in `aura.conversation_turns` when the work finishes | SWARM-03 / SC#1 | Spike 098 proved a mocked/SSE-grep test shows green while the model *semantically discards* the report. Only a live model run with the reasoning trace read reveals it (Pitfall 4). | Drive the real agent on the running stack; delegate; send a second operator message immediately; confirm the turn returned before the worker did; then read the model's own handling of the arriving report, not just the SSE frames. |
| A worker's question surfaces in the operator's channel naming the worker, and answering resumes that worker's line of work | SWARM-06 / SC#4 | Requires a real channel round-trip (cockpit or Telegram) and a real pause/resume across processes. | Nested delegation with an injected `ask_user`; answer on the origin channel; verify exactly that worker resumed and no sibling pause was cross-resumed. |
| SWARM-11's PRD amendment precedes implementation | SWARM-11 | Process requirement, not a code behavior. | `git log --format='%h %cs %s'` — the amendment commit must predate the first implementation commit. |
| Crash-after-partial-side-effects for a delegation row | SWARM-09 | Unmeasured by spike 100 (its crash test SIGKILLed the daemon 2s in, before any tool dispatch). **Planner verdict: NOT a build task — it is a required live verdict.** Plan 51-08's checkpoint MUST state one of "exercised live" or "documented residual risk"; leaving it implied is prohibited. | If taken: kill a worker between side-effect and ledger write, then assert what the reclaim path reports. |
| The four uncovered items of the `docs/aura-quality-snapshot.md` "Swarm E2E" row | SWARM-03/SWARM-09 | `internal/eval`'s harness stays deleted, so mail/WhatsApp MCP read-back, the `<1.5×` timing ratio, the `≥90%` judge score and the no-over-spawn check are unrun. They are NOT retroactively closed by the design-gate spikes. | Either run them through `drive-sc.sh` or keep them listed as uncovered in the snapshot row — never silently claim them. |

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 180s
- [ ] Every `db_integration` run executed as `aura_app`, not `aura`
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
