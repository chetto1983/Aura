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
| 51-01-T1 | 51-01 | 1 | SWARM-03, SWARM-09 | T-51-01, T-51-02 | report delivered under `steer.SourceWorker`; rows identity-scoped, tested as `aura_app` | db_integration | `go test -tags=db_integration ./internal/swarm/ -run 'TestBackgroundDelegationEnqueues|TestDelegationClaimReclaim'` | ❌ W0 | ⬜ pending |
| 51-01-T2 | 51-01 | 1 | SWARM-03 | T-51-03 | claim loop runs workers through the single `runChild` path that carries `gateway.WithDelegatedDispatch` | unit | `go test ./cmd/aura/...` | ✅ | ⬜ pending |
| 51-01-T3 | 51-01 | 1 | SWARM-03 | T-51-01 | live tracer: turn returns early; the interim ~240s bound is recorded, never mistaken for D-03 | live E2E | checkpoint — `drive-sc.sh` precursor | ❌ W0 | ⬜ pending |
| 51-04-T2 | 51-04 | 1 | SWARM-07 | T-51-14 | run id + writer role host-derived; model has no field to lie in ON THE WRITE PATH; `memory_forget`'s source-scoped detachment (`tool_forget.go:69-74`, `RunID` as a FILTER) and the `toHits` read-back both still work | unit + race | `go test -race ./cmd/arcadedb-mcp/... ./internal/arcadedb/...` | ❌ W0 | ⬜ pending |
| 51-04-T3 | 51-04 | 1 | SWARM-07 | T-51-15, T-51-16 | worker supersede refused before any Command; real goroutine fan-out | race + arcadedb_integration | `go test -race -tags=arcadedb_integration ./internal/arcadedb/ -run TestConcurrentWorkerFactWrite` | ❌ W0 | ⬜ pending |
| 51-02-T2 | 51-02 | 2 | SWARM-03, SWARM-09 | T-51-06, T-51-07 | `source` round-trips; foreign `identity_id` invisible; as `aura_app`, >1s runtime | db_integration | `go test -tags=db_integration ./internal/steer/ -run 'TestPostgresSteerQueue|TestQueueTTLDerivedPerKind|TestConcurrentDrain'` | ❌ W0 | ⬜ pending |
| 51-02-T3 | 51-02 | 2 | SWARM-09 | T-51-08, T-51-09 | expired row leaves a trace in the same transaction (D-08); as `aura_app`, >1s runtime | db_integration + unit | `go test -tags=db_integration ./internal/steer/ -run TestExpireDue` | ❌ W0 | ⬜ pending |
| 51-03-F1 | 51-03 | 2 | SWARM-01 | T-51-11 | context text cannot forge a brief section marker | unit | `go test ./internal/swarm/ -run 'TestStructuredBrief|TestBriefContext'` | ❌ W0 | ⬜ pending |
| 51-03-F2 | 51-03 | 2 | SWARM-02 | T-51-12 | caps rendered from injected VALUES, never a hardcoded env name | unit | `go test ./internal/agent/tools/ -run TestSwarmSpawnSpecReflectsConfig` | ❌ W0 | ⬜ pending |
| 51-05-T1 | 51-05 | 3 | SWARM-04, SWARM-05 | T-51-18, T-51-19 | depth cap enforced by registry AND `checkDepth`; child never wider than parent | unit + race | `go test -race ./internal/swarm/ -run 'TestNestedDelegationSynchronous|TestSwarmDepthGuard'` | ❌ W0 (extend `swarm_test.go:464`) | ⬜ pending |
| 51-05-T2 | 51-05 | 3 | SWARM-08 | T-51-20 | re-entry guard keys on scope AND fingerprint on the new dispatch paths | unit (regression guard for `67d24aee4`) | `go test ./internal/agent/ -run TestDeriveToolOperationContext` | ✅ extend `idempotency_operation_test.go:209` | ⬜ pending |
| 51-06a-T2 | 51-06a | 3 | SWARM-06 | T-51-22, T-51-23, T-51-25, T-51-47 | fenced conditional UPDATE inside `CommitResumeBatch`'s transaction; a NULL-fence pause still resumes; as `aura_app`, >1s | db_integration | `go test -tags=db_integration ./internal/runner/ -run 'TestPerWorkerPauseFencing|TestWorkerPauseLazyExpiry|TestUnfencedPauseStillResumes'` | ❌ W0 | ⬜ pending |
| 51-10-T1 | 51-10 | 3 | SWARM-03 | T-51-38, T-51-41 | the SC#1 write: report recorded to the origin conversation before any push; a record failure blocks `succeeded` | unit | `go test ./internal/swarm/ -run 'TestDeliveryRecordsBeforePush|TestRecordFailureBlocksSucceeded'` | ❌ W0 | ⬜ pending |
| 51-10-T2 | 51-10 | 3 | SWARM-03, SWARM-09 | T-51-39, T-51-40, T-51-42 | tri-state honoured; a drained report is never pushed; the nudge is idempotent; as `aura_app`, >1s | db_integration + unit | `go test -tags=db_integration ./internal/swarm/ -run 'TestNudgeSkipsDrained|TestNudgeOnceUnderConcurrency'` | ❌ W0 | ⬜ pending |
| 51-06b-T1 | 51-06b | 4 | SWARM-06 | T-51-24, T-51-37 | worker opens its own attributed pause; the queue row parks non-claimable, atomically with the pause; as `aura_app`, >1s | db_integration | `go test -tags=db_integration ./internal/swarm/ -run 'TestWorkerOpensOwnPause|TestParkedRowNotClaimable'` | ❌ W0 | ⬜ pending |
| 51-06b-T2 | 51-06b | 4 | SWARM-06 | T-51-48, T-51-49, T-51-36 | **the worker CONTINUES past its question**; no pre-pause tool re-executed; promoted deferred tools survive the rebuild (via the persisted `tool_search` call/result pair re-read by `deriveActivated`, NOT a persisted list — `git diff --stat internal/agent/` must be empty); identity mismatch refused | db_integration + unit | `go test -tags=db_integration ./internal/swarm/ -run 'TestDelegationResumeContinuesWorker|TestUnparkExactlyOnce|TestResumeKeepsPromotedTools|TestSiblingPauseUnaffected'` | ❌ W0 | ⬜ pending |
| 51-06b-T3 | 51-06b | 4 | SWARM-06 | T-51-26 | pause expiry writes its trace atomically AND resolves the parked queue row; as `aura_app`, >1s | db_integration | `go test -tags=db_integration ./internal/runner/ -run 'TestExpireWorkerPauses|TestExpiredWorkerPauseResolvesQueueRow'` | ❌ W0 | ⬜ pending |
| 51-07-T1 | 51-07 | 4 | SWARM-10 | T-51-28, T-51-29 | path traversal rejected; foreign conversation returns 404 | unit + race | `go test -race ./internal/swarm/ -run TestTranscript && go test -race ./internal/agui/...` | ❌ W0 (`report_test.go` already covers the writer) | ⬜ pending |
| 51-07-T2 | 51-07 | 4 | SWARM-11 | — | amendment precedes implementation | manual-only | `git log --format='%h %cs %s' -- prd.md` | N/A | ⬜ pending |
| 51-09-T1 | 51-09 | 5 | SWARM-03, SWARM-04, SWARM-09 | T-51-43, T-51-44, T-51-45, T-51-46 | reap on inactivity not age; a stalled worker IS cancelled; boot refuses `idle >= lease`; no timer/goroutine leak | unit + race | `go test -race ./internal/swarm/ -run 'TestChildStaleness|TestWorkerStalledReport|TestStreamingWorkerNotReaped'` | ❌ W0 | ⬜ pending |
| 51-09-T2 | 51-09 | 5 | SWARM-03 | — | the retired knob has no reader, no catalog entry and no stale comment; the PRD records the retirement | gate | `grep -rn 'AURA_SWARM_CHILD_TIMEOUT_SEC' --include='*.go' --include='*.example' . | grep -v planning` returns empty | ✅ | ⬜ pending |
| 51-09-T3 | 51-09 | 5 | SWARM-03 | T-51-43, T-51-44 | live: a >4-minute delegation completes; a stalled one is reaped once; `idle == lease` refuses to boot | live E2E (checkpoint) | checkpoint on the running stack | ❌ W0 | ⬜ pending |
| 51-08-T1 | 51-08 | 9 | SWARM-01..10 | T-51-35 | verdicts from reassembled deltas, never an SSE grep | live driver | `bash -n .planning/phases/51-durable-delegation/drive-sc.sh` | ❌ W0 | ⬜ pending |
| 51-08-T2 | 51-08 | 9 | SWARM-01..10 | T-51-33, T-51-34 | SC#1–SC#5 scored >9.8, HEAD-fresh image; SC#1 asserted on a conversation the operator did NOT reopen; SC#4 asserted as "the worker continued" | live E2E (checkpoint) | `bash .planning/phases/51-durable-delegation/drive-sc.sh` | ❌ W0 | ⬜ pending |
| 51-11-T1 | 51-11 | 6 | SWARM-12 | T-51-51, T-51-52, T-51-54, T-51-56 | the durable record is a CARD, not raw report JSON; the full report is an owned thread-scoped asset; every cap counted in runes on a rune boundary | unit | `wsl.exe … go test ./internal/swarm/ -run TestDelegationRecordCard` | ❌ W0 | ⬜ pending |
| 51-11-T2 | 51-11 | 6 | SWARM-12, SWARM-10 | T-51-58 | each background worker owns a unique stable child id that `validatePathSegment` accepts, so two workers never share a transcript; one fan-out key per `swarm_spawn` call; the transcript self-terminates | unit | `wsl.exe … go test ./internal/swarm/ -run TestDelegationChildID` and `-run TestDelegationFanoutKey` | ❌ W0 | ⬜ pending |
| 51-11-T3 | 51-11 | 6 | SWARM-12, SWARM-09 | T-51-64, T-51-65, T-51-66 | ONE Telegram message per fan-out, never while a worker is non-terminal; all rows of the fan-out claimed in one conditional UPDATE, so two concurrent sweeps send once; as `aura_app`, >1s runtime | unit + db_integration | `wsl.exe … go test ./internal/swarm/ -run TestFanout` and `-tags=db_integration ./internal/steer/ -run TestPushDelegationResult` | ❌ W0 | ⬜ pending |
| 51-11-T4 | 51-11 | 6 | SWARM-10 | T-51-50, T-51-51, T-51-53 | `swarm_status` returns untrusted provenance; the job query carries the `identity_id` predicate and the tool scopes to the tool-call conversation; tail bounded at 100 events / 200 runes per event; as `aura_app`, >1s runtime | unit + db_integration | `wsl.exe … go test ./internal/agent/tools/ -run TestSwarmStatus` and `-tags=db_integration ./internal/documents/ -run TestListDelegationJobs` | ❌ W0 | ⬜ pending |
| 51-11-T5 | 51-11 | 6 | SWARM-12, SWARM-10 | T-51-52, T-51-55, T-51-65 | live: the card, the artifact, the ONE fan-out message AND the mid-flight silence before it, two distinct self-terminating transcripts, a fact-based progress answer — every verdict read from Postgres, the container filesystem or the device | live E2E (checkpoint) | checkpoint on the running stack; evidence in `live-check/envelope/RESULTS.md` | ❌ W0 | ⬜ pending |
| 51-12a-T1 | 51-12a | 7 | SWARM-12, SWARM-10 | T-51-57, T-51-58, T-51-59, T-51-67, T-51-68 | the 404 ladder is byte-identical across all four failure branches; the `child` parameter reaches the filesystem only through `validatePathSegment`; reasoning deltas absent from the bytes; a malformed line is skipped, not fatal; no goroutine leak | unit + race + goleak | `wsl.exe … go test -race ./internal/agui/ -run TestSwarmWorkerEvents` | ❌ W0 | ⬜ pending |
| 51-12a-T2 | 51-12a | 7 | SWARM-12 | T-51-62 | the multiplexed stream emits no transcript content (asserted against a planted sentinel) and re-emits only on state change; the queued `swarm_spawn` object and the synchronous array decode through ONE normalizer, everything else still degrades to the escaped raw panel | unit | `wsl.exe … go test ./internal/agui/ ./internal/agent/display/ -run TestSwarmWorkerStatus` and `-run TestDecodeSwarmSpawnPreview` | ❌ W0 | ⬜ pending |
| 51-12b-T1 | 51-12b | 8 | SWARM-12 | T-51-60, T-51-61, T-51-69 | the pane renders through the shipped part renderers only (no second frame mapping, no new markdown host); read-only by construction; the right rail is mutually exclusive, enforced in the hooks; no host-built bundle | unit (vitest) + lint | `cd web && npm run test -- --run src/chat/workers src/shell` | ❌ W0 | ⬜ pending |
| 51-12b-T2 | 51-12b | 8 | SWARM-12 | T-51-63 | exactly one status connection for the whole chip regardless of worker count, closed on unmount; no browser-side polling; colour is never the only status signal; every touch target a literal 44px | unit (vitest) + lint | `cd web && npm run test -- --run src/chat/displays src/chat/workers` | ❌ W0 | ⬜ pending |
| 51-12b-T3 | 51-12b | 8 | SWARM-12, SWARM-10 | T-51-60, T-51-63, T-51-65 | live in a real browser: the pane tails a running worker, the picker switches, devtools shows one EventSource per watched worker and one status stream (any repeating request is a FAIL), the fan-out message arrives once and not before | live E2E (checkpoint) | checkpoint on the running stack; evidence in `live-check/cockpit/RESULTS.md` | ❌ W0 | ⬜ pending |
| 51-12b-T4 | 51-12b | 8 | SWARM-11, SWARM-12 | T-51-70 | the PRD amendment is written AFTER the drive and cites its evidence path and its perimeter; the quality snapshot is re-attested for every matching glob | gate | `wsl.exe … bash scripts/quality_snapshot_gate.sh` | ❌ W0 | ⬜ pending |
| 51-08-T3 | 51-08 | 9 | SWARM-01..10 | — | snapshot re-attested; coverage floor ≥85% | gate | `bash scripts/quality_snapshot_gate.sh && bash scripts/coverage_docker.sh` | ✅ | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- [ ] `internal/agent/tools/swarm_spawn_schema_test.go` (or extend `swarm_spawn_test.go`) — SWARM-02's live-cap rendering
- [ ] `internal/swarm/brief_context_test.go` — SWARM-01's goal/context split
- [ ] `internal/swarm/delegation_queue_test.go` (`db_integration`, as `aura_app`) — SWARM-03/09 enqueue + claim/reclaim, exercising the **real `ClaimIngestionJobs` Go path**, closing spike 100's own stated gap
- [ ] `internal/runner/worker_pause_test.go` (`db_integration`, as `aura_app`) — SWARM-06's fencing column, including the NULL-fence case proving the shipped resume path is unchanged (51-06a)
- [ ] `internal/swarm/delegation_resume_test.go` (`db_integration`, as `aura_app`) — SWARM-06's RESUME leg (51-06b). The load-bearing case uses a scripted fake LLM client: a worker calls tool X, asks, pauses, and after the answer calls tool Y — asserting tool X was dispatched exactly ONCE in total and that work happened AFTER the answer
- [ ] `internal/swarm/delegation_delivery_test.go` (unit + one `db_integration` case) — SC#1's out-of-band conversation write and the absent-operator tri-state (51-10)
- [ ] `internal/swarm/child_staleness_test.go` (`-race`, `goleak`) — D-03's inactivity model (51-09): reset-keeps-alive, silence-reaps-once, `<=0` disables, no leaked timer on either path
- [ ] `internal/arcadedb/concurrent_fact_write_test.go` (`-race`, `arcadedb_integration`) — SWARM-07 with genuine goroutine fan-out, not sequential calls
- [ ] `internal/swarm/delegation_card_test.go` (unit) — SWARM-12's card, artifact pointer and the rune caps, including a multibyte-boundary case (51-11)
- [ ] `internal/swarm/delegation_fanout_test.go` (unit) + a `db_integration` tier as `aura_app` — the fan-out grouping key, the still-running refusal, the one-claim concurrency case and the ≤300-rune N-line budget (51-11)
- [ ] `internal/agent/tools/swarm_status_test.go` (unit, daemon-free fake reader) — SWARM-10's parent leg: not-found, clamp, empty tail, exact elapsed truncation, untrusted provenance (51-11)
- [ ] `internal/agui/server_swarm_events_test.go` (`-race`, `goleak`) — the 404 ladder asserted by byte-comparing all four failure bodies, replay order, tail pickup, terminal-marker stop, malformed-line skip, and the sentinel test proving the status mode carries no transcript text (51-12a)
- [ ] `web/src/chat/workers/workerStream.test.ts` plus vitest coverage for `useWorkerStatuses`, `WorkerPicker` keyboard navigation and the `SwarmReportTable` row matrix (51-12b)
- [ ] A phase-specific live driver script, modelled on `.planning/spikes/098-steer-carries-worker-result/drive.sh` and `.planning/spikes/099-worker-duration-and-progress/drive.sh`, covering SC#1–SC#5 (`internal/eval`'s harness stays deleted)
- [ ] Migration(s), **at most one per wave by construction, so two parallel executors can never take the same slot**: wave 2 → D-06/D-07's `aura.steer_queue`, including the `nudged_at` column plan 51-10 consumes without a migration of its own; wave 3 → D-12's `ALTER aura.paused_states ADD COLUMN` fencing id (51-06a); wave 4 → `aura.ingestion_jobs`'s status CHECK widened with `awaiting_input` (51-06b). D-10 needs **no** migration — it is a Go-only schema change. **The slot number is read from `ls internal/db/migrations/ | tail -1` at landing time, never copied from a document.**

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Operator keeps talking mid-delegation; the consolidated worker result lands in `aura.conversation_turns` when the work finishes | SWARM-03 / SC#1 | Spike 098 proved a mocked/SSE-grep test shows green while the model *semantically discards* the report. Only a live model run with the reasoning trace read reveals it (Pitfall 4). | Drive the real agent on the running stack; delegate; send a second operator message immediately; confirm the turn returned before the worker did; then read the model's own handling of the arriving report, not just the SSE frames. |
| A worker's question surfaces in the operator's channel naming the worker, and answering resumes that worker's line of work | SWARM-06 / SC#4 | Requires a real channel round-trip (cockpit or Telegram) and a real pause/resume across processes. | Nested delegation with an injected `ask_user`; answer on the origin channel; verify that worker **produced work after the answer** (a tool call or a report it had not produced before), that no pre-pause tool ran twice, and that no sibling pause was cross-resumed. A verdict of "the sibling did not move" does NOT close this row. |
| A backgrounded delegation that runs longer than the retired ~240s wall clock completes, and a stalled worker is still reaped | SWARM-03 / D-03 | The failure mode is a wall-clock cut-off a fast test never reaches, and a stall only a real upstream hang produces. Amendment #154 measured both against a live model. | Delegate real multi-tool work exceeding four minutes; assert completion, `succeeded`, and `attempt_count` unchanged. Separately point a worker at a hanging tool and assert one `stalled` report, one cancellation, and no second worker on the same goal. |
| A worker that promoted a deferred tool via `tool_search` before pausing can still call it after the resume | SWARM-06 / SWARM-08 | LibreChat hit exactly this and shipped `discoveredTools` for it. Aura's mechanism is different and already shipped — `NewLlmAgent` re-derives `activated`/`everLoaded` from the seeded history (`llm_agent_construct.go:38-39`) — so what is actually under test is that the persisted history kept the `tool_search` call/result pair. The failure is a runtime "unknown tool" that only appears on a real resume against a real deferred manifest. | Drive a worker to `tool_search` a deferred tool, ask, answer, then assert the post-resume dispatch of that same tool succeeds without a second `tool_search`. |
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
