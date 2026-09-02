---
phase: 49
slug: memory-tiers
status: validated
nyquist_compliant: false
wave_0_complete: false
created: 2026-08-31
revised: 2026-09-02
plans: 14
tasks: 32
---

# Phase 49 — Validation Strategy

## Test infrastructure and cadence

| Property | Contract |
|---|---|
| Framework | Go `testing`, `go.uber.org/goleak`, `pgregory.net/rapid`; Python `unittest`; live ArcadeDB build tag |
| Per-task gates | Named focused test, `go vet ./...`, `go build ./...`, touched-package unit, touched-package race |
| Live gates | `-race -tags=arcadedb_integration -count=1`; missing dependencies/evidence must fail, never skip-green |
| Final gates | no-skip authenticated running-Aura conversation plus Tempo/PostgreSQL/ArcadeDB inspection; `python scripts/agent_memory_eval.py --tier all`; WSL `make quality-full`; `scripts/coverage_docker.sh`; `make critical-mutation` |
| Quality thresholds | Full tagged coverage ≥85% plus package policy; critical mutation ≥70%; the three named running-Aura scenarios emit exactly 1/3/2 terminal answers and every one of the six per-response scores is >9.8 |

After every task, run its exact `<verify><automated>` command. All 32 commands are fail-fast with `set -euo pipefail`; focused Go gates execute `go test -list` and assert a positive named-test count before the test run, while Python evaluator-unit gates capture native `unittest` output and assert a positive `Ran N tests` count. After each wave, run the affected live tier. Before verify-work, run Plan 49-11-T3 exactly. Every verify has an explicit non-zero failure direction and rejects zero tests, skips, or empty evidence where applicable.

## Per-task verification map

| Task ID | Wave | Requirements | Secure behavior | Primary automated target | Status |
|---|---:|---|---|---|---|
| 49-01-T1 | 1 | MEM-06 | isolated Amendment #201 plus six-path ancestry/non-proofs | repository diff-tree/path logs + Go gates | covered |
| 49-01-T2 | 1 | TOOL-05 | one retrieval surface; evaluator cannot skip empty evidence | `TestMemorySurfacePolicy_`, evaluator unit + Go gates | covered |
| 49-02-T1 | 2 | MEM-01 | typed eligible projection and idempotent graph fragment | `Test(ProjectionTurnEligibility|ConversationSchemaStatements)` + Go gates | covered |
| 49-02-T2 | 2 | MEM-01 | authoritative paging/edit/delete/rebuild contract | conversation projection live + Go gates | unit+race only; live tier NOT run |
| 49-06-T1 | 2 | HARN-05 | final-state validation and complete rollback | `TestMemoryBatch_(FinalStateTracer|RollbackFirstError|IdempotentReplay)` + Go gates | covered |
| 49-06-T2 | 2 | HARN-05 | whole-decision conflict retry and no partial state | `TestMemoryBatch_(ConflictRetry|LateRollback|CrossIdentity|IdempotentReplay|NoPartialObserver)` + Go gates | covered |
| 49-07-T1 | 3 | MEM-01 | EnsureMemorySchema registers complete conversation schema | `TestEnsureMemorySchemaRegistersConversationSchema` + Go gates | covered |
| 49-07-T2 | 3 | MEM-01 | projection offered only after source commit | `Test(ConversationProjectionPostCommit|ConversationProjectionFailSoft|ChatBootMemoryProjection)` + Go gates | covered |
| 49-07-T3 | 3 | MEM-01 | bounded crash replay converges without deleting authority | `Test(ConversationProjectionCrashRecovery|ConversationProjectionBootReconcile|ConversationProjectionPeriodicReconcile)` + Go gates | covered |
| 49-03-T1 | 4 | MEM-02, TOOL-05 | native `vector.fuse`; tier effective path separate from query/entity/fallback backend path; response/OTel equality | `Test(MemoryRecallMixedTierTracer|MemoryRecallVectorFuse|MemoryRecallBackendPath|MemoryRecallAbstains)` + Go gates | covered (live fixed) |
| 49-03-T2 | 4 | MEM-02 | unsigned/untrusted bounded cursor revalidates identity | `Test(MemoryRecallModeContract|RecallCursor|MemoryRecallWindow)` + Go gates | covered |
| 49-08-T1 | 5 | MEM-02 | fresh host-only active-source carrier per call | `Test(SessionIDFromContext|RecallContextHeaders)` + Go gates | covered |
| 49-08-T2 | 5 | MEM-02, TOOL-05 | server decodes only exclusion and revalidates ownership | `Test(RecallContextHeaders|MemoryRecallActiveSourceHeader|MemoryRecallSuppressesActiveConversation)` + Go gates | covered |
| 49-04-T1 | 6 | MEM-03 | EnsureMemorySchema registers reasoning schema; amendment isolation plus ancestry only for already-committed protected paths, permitting untouched future paths | `Test(EnsureMemorySchemaRegistersReasoningSchema|ReasoningSchemaStatements)` + intermediate ancestry + Go gates | covered |
| 49-04-T2 | 6 | MEM-03, CTX-05 | explicit-owner reasoning only; bounded/redacted fields | `Test(ReasoningRecallExplicitOnly|ReasoningToolMetadataBounded|ReasoningRecallIdentity)` + Go gates | covered |
| 49-04-T3 | 6 | MEM-03 | exact success=30d, failed/cancelled=7d | `Test(ReasoningRetentionPolicy|ReasoningTerminalExpiry)` + Go gates | covered |
| 49-13-T1 | 6 | MEM-02, TOOL-05 | live mixed recall excludes active/foreign sources; query/entity/fallback proves hybrid/graph/lexical separate from tier contribution | `TestAgentMemoryMCPLive_(MixedTierRecall|BackendPath)` + Go gates | unit+race only; live tier NOT run |
| 49-13-T2 | 6 | MEM-02, TOOL-05 | response and OTel separately agree on effective/backend paths and counts for query/entity/fallback | evaluator unit + `--tier mixed_tier_recall` + Go gates | covered |
| 49-12-T1 | 7 | MEM-03 | authorized provider-visible post-commit trace; amendment isolation plus ancestry for already-committed protected paths only | `TestReasoningGraphTracer` + intermediate ancestry + Go gates | covered (was false-green) |
| 49-12-T2 | 7 | MEM-03, CTX-05 | bounded tool metadata/TOUCHED; retry discard | `Test(ReasoningGraphRetryDiscard|ReasoningGraphToolMetadata)` + Go gates | covered (was false-green) |
| 49-09-T1 | 8 | MEM-03 | production lifecycle applies exact 30d/7d TTL | `Test(ReasoningRetentionWorker|ReasoningRetentionBoot|ReasoningRetentionClose)` + Go gates | covered |
| 49-09-T2 | 8 | MEM-03, MEM-06 | source deletion dominates TTL and deletes whole graph | live `DeletionPrecedence|ExpiryDeleteRace` + Go gates | unit+race only; live tier NOT run |
| 49-09-T3 | 8 | CTX-05 | graph-resident reasoning absent from automatic context | live `ExplicitIsolation|FailedCancelledRetention`, history test + Go gates | unit+race only; live tier NOT run |
| 49-05-T1 | 9 | AUTO-03, CTX-05 | exact upsert/write/patch AcceptedCapture producers | `Test(AcceptedCaptureProducer|MemoryUpsertAcceptedCapture|DurableArtifactAcceptedCapture)` + Go gates | covered |
| 49-05-T2 | 9 | AUTO-03 | ordered watermark barrier; discard/stop safety | `Test(MemoryCaptureQueueOrder|MemoryCaptureTerminalBarrier|MemoryCaptureRetryDiscard|MemoryCaptureStop)` + Go gates | covered |
| 49-10-T1 | 10 | AUTO-03, CTX-05 | idempotent direct provenance and source defense | `TestAcceptedCapture_(Tracer|Idempotent|Retry|SourceDefense)` + Go gates | covered |
| 49-10-T2 | 10 | AUTO-03 | temporal contradictions and principal-only supersession | `TestAcceptedCapture_(Contradiction|WorkerAuthority|PrincipalAuthority|ProvenanceEnrichment)` + Go gates | covered |
| 49-14-T1 | 11 | AUTO-03 | one bounded production queue and truthful close | `Test(MemoryCaptureBoot|MemoryCaptureClose|MemoryCaptureSinkFailure)` + Go gates | covered |
| 49-14-T2 | 11 | AUTO-03, CTX-05 | real structured events durable before completion | live `TestMemoryCaptureLive_(ExplicitUserEvent|DurableArtifactEvent|TerminalBarrier)` + Go gates | unit+race only; live tier NOT run |
| 49-11-T1 | 12 | HARN-05 | bounded identity-free public batch/risk schema | `Test(MemoryBatchTool|MemoryBatchRisk|MemorySurfacePolicy_)` + Go gates | covered |
| 49-11-T2 | 12 | HARN-05 | live rollback/concurrency/replay has no partial state | live `TestMemoryBatchLive_` and published batch route + Go gates | unit+race only; live tier NOT run |
| 49-11-T3 | 12 | all | final non-empty six-path ancestry; exact 1/3/2 terminal-answer counts across the three named authenticated Aura scenarios; six unique observed-to-scored response IDs; every per-response score >9.8; correlated Tempo/PG/ArcadeDB; coverage/mutation | exact Plan 49-11-T3 command and report assertion | **FAILING (live)** |

Task coverage: **32/32** tasks have an automated command, explicit `<fails_when>`, machine-checkable `<acceptance_criteria>`, and `<done>`.

## Requirement → evidence map

| Requirement | Plans | Final evidence |
|---|---|---|
| MEM-01 | 02, 07, 11 | schema registration, post-commit projection, crash convergence, final all-tier gate |
| MEM-02 | 03, 08, 13, 11 | native fusion, cursor bounds, host exclusion, live mixed recall |
| MEM-03 | 04, 12, 09, 11 | schema registration, production trace builder, exact retention/lifecycle, final gate |
| MEM-06 | 01, 04, 09, 11 | isolated amendment plus exact six protected-path ancestry |
| TOOL-05 | 01, 03, 08, 13, 11 | one retrieval surface; tier `effective_path` distinct from actual graph/hybrid/lexical backend `path`; query/entity/fallback response/OTel equality; abstention and evaluator |
| AUTO-03 | 05, 10, 14, 11 | exact producers, sink semantics, production barrier/live proof |
| CTX-05 | 04, 05, 09, 10, 12, 14, 11 | zero automatic reasoning reads and no reasoning-derived capture |
| HARN-05 | 06, 11 | final-state engine, public typed tool, live atomicity |

## Wave-0 and sign-off

- [ ] All named test files/cases absent at execution start are created RED before production changes.
- [ ] Live fixtures fail on missing ArcadeDB/identity/embedding/OTel evidence; no skip-green path.
- [ ] The real running-Aura gate drives authenticated `/agent/run` turns and correlates Tempo, RLS-scoped `aura.tool_invocations`, `aura.conversation_turns`, and ArcadeDB evidence; no MCP-only substitute is accepted.
- [ ] `running_aura_conversation.scenarios` has exactly `beyond_active_context_recall` (1 answer), `provider_visible_reasoning_exclusion_explicit_recall` (3 answers), and `durable_shell_file_capture_later_recall` (2 answers); observed terminal IDs and scored `responses` IDs are globally unique and form an exact per-scenario bijection.
- [ ] Evaluator unit fixtures prove a later response at most 9.8 fails even when a scenario aggregate remains high, and prove missing, duplicate, extra, or unscored response records fail.
- [ ] Same-wave file overlap audit is zero and every plan has fewer than ten modified files.
- [ ] Full tagged coverage is ≥85% with package-local policy green.
- [ ] Critical mutation is ≥70% and every one of the six terminal Aura answers has its own score strictly >9.8; averages and aggregate-only scores are rejected.
- [ ] Set `wave_0_complete: true`, `nyquist_compliant: true`, and `status: validated` only after evidence is recorded.

## Validation audit 2026-09-02

Executed against the live stack (`aura` at HEAD, ArcadeDB, PostgreSQL, Tempo), not compile-checked.

| Metric | Count |
|---|---|
| Named Go test targets in the map | 71 |
| Present in the tree | 71 |
| Gaps found | 4 |
| Resolved | 3 |
| Escalated / still open | 1 |

### What was measured green

`go vet ./...`, `go build ./...`, unit tests across the seven touched packages, and
`-race` on `internal/runner`, `internal/arcadedb`, `cmd/arcadedb-mcp`. Python evaluator
units 52/52 (48 before this audit). The MEM-06 amendment gate passes: `Amendment #201`
(`f231f15b5`) changes only `prd.md` and is an ancestor of the earliest Phase 49 commit
touching all six protected paths.

### Gaps found and closed

**1. The >9.8 acceptance gate could not fail (`a2b1a3b45`).** `_scenario()` scored a
response `10.0` whenever the turn produced any text, discarding the `passed` argument its
callers compute from the real cross-store assertions — a dead parameter, and the reason
49-11-SUMMARY reports six 10.0 scores. The seam that produces the score had no test at
all; the evaluator suite only exercised report *evaluation*. Restoring `passed` and adding
`ScenarioScoringTest` (two cases RED against the old scoring) made the suite fail honestly.

**2. TOUCHED edges never existed in production (`47db212a9`).** `reasoningToolPolicies` was
keyed on bare tool names while the runtime emits MCP-namespaced ones, so every memory tool
was dropped from the reasoning graph before entity refs were extracted. Measured before:
`ReasoningTrace=7, ReasoningStep=7, ReasoningToolCall=1, TOUCHED=0` — the one recorded call
was `send_file`, the only native unprefixed tool. The unit fixtures used a name production
never emits, so the suite could not catch it. After the fix, measured live: `TOUCHED=2`,
`touched_edges: 1` in the report.

**3. `memory_recall` never told the model when to pass `mode` (`9b616887`).** It was the one
memory tool whose description described instead of instructing. Measured live: asked *"cosa
vedi delle conversazioni precedenti?"* the agent called it with a bare semantic `query`,
scored the seeded conversation at 0.016, and answered it had no record of previous
conversations. After the fix the same scenario scores 10.0 and the agent chooses
`{"mode":"recent",...}` unprompted. The calling machinery was never at fault: the same agent
passes `{"mode":"reasoning","trace_id":...}` correctly when the prompt names it.

**4. The recorded final gate was unexecutable (open).** 49-11-T3's command hardcoded
`wsl.exe --cd /mnt/d/Repo/Aura`, a path that does not exist on this host; `wsl.exe --cd`
returns 127, so under `set -euo pipefail` the gate aborted before `make quality-full`,
`scripts/coverage_docker.sh` and `make critical-mutation` — which is why 49-11-SUMMARY
records all three as PENDING. The path is corrected in the PLAN; **the three gates have
still not been executed**, so no coverage, package-policy or mutation figure is claimed here.

### Still failing (not closed)

`python scripts/agent_memory_eval.py --tier all` remains **FAIL, MRS=44.00**. Note the
script exits 0 on FAIL; the gate depends on the separate report assertion to catch it.

| Scenario | Score | Remaining cause |
|---|---|---|
| `beyond_active_context_recall` | 10.0 PASS | — |
| `provider_visible_reasoning_exclusion_explicit_recall` | 0.0 | `explicit_reasoning_recall` asserts `trace_id in result_preview`; the call is correct (`mode=reasoning`, matching `trace_id`) but the preview is truncated and `[REDACTED]` before that field |
| `durable_shell_file_capture_later_recall` | 0.0 | `accepted_capture` and `run_finished_before_recall` both hold and the answer names the right artifact, but `memory_recall` returned the *previous* run's artifact — the just-captured fact was not retrievable in that turn, so the answer came from active context |

The last row is a real product signal, not only an assertion weakness: capture is durable
(`accepted_capture: True`) but not yet recallable at the moment the scenario asks for it.

### What this audit does NOT show

No coverage, package-policy, mutation, goleak or `make quality-full` figure was produced —
those three gates were never run (gap 4). The `arcadedb_integration` live tier was not run
either: the six tasks whose primary target is a live test are marked accordingly and their
green status covers only the unit and race tiers. The live tiers were exercised on one host with one
provider; the three scenarios are not a statistical sample, and the two remaining failures
were reproduced across three consecutive runs but not isolated to a root cause.

**Approval:** withheld — the phase's own acceptance gate does not pass.
