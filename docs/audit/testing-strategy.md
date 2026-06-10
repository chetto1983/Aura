# Testing Strategy — Aura `internal/agent`

## Current state (measured live, 2026-06-10)

`go test ./internal/agent/... -count=1 -cover` — **all green**; `go test -race ./internal/agent/... -count=1` — **all 6 packages green**.

| Package | Coverage | Wall |
|---|---|---|
| `internal/agent` | **93.3%** | 7.8s |
| `internal/agent/agenttest` | 42.6%¹ | 0.1s |
| `internal/agent/mcptools` | 83.4% | 0.2s |
| `internal/agent/prompt` | 91.9% | 0.1s |
| `internal/agent/tools` | 87.5% | 4.1s |
| `internal/agent/workflow` | 92.4% | 0.2s |

¹ measurement artifact — `agenttest` mocks/fakeclient are consumed by `internal/agent`'s external tests, whose execution doesn't count toward `agenttest`'s own profile. Not a quality gap, but it drags the owned-surface total.

**Scale:** 68 `_test.go` files, ~13,600 LOC of test code, **406 top-level `Test*` functions**. Zero `Fuzz*`/`Benchmark*` in scope.

### Technique inventory

| Technique | Present | Evidence |
|---|---|---|
| Table-driven | Pervasive | `budget_test.go`, `tools/result_test.go`, `prompt/reasoning_policy_test.go` |
| Property-based (`pgregory.net/rapid`) | 6 sites | `budget_dedup_test.go:415`, `event_test.go:300`, `workflow/loop_test.go:527`, `tools/bm25_test.go:162,177`, `tools/result_test.go:146` + `testdata/rapid` |
| goleak | Package-wide | `main_test.go` `VerifyTestMain` in agent/tools/mcptools/workflow; per-test `VerifyNone` ×10 in `workflow/parallel_test.go` |
| Race detector | Yes | green; CI `ci.yml:68–69` runs `-race ./...` |
| Fakes/mocks | High quality | `agenttest/fakeclient.go` (scripted chunk turns, goleak-clean pre-closed channels), `agenttest/mocks.go` (4 mocks w/ compile-time `var _ agent.Agent`) |
| Real-concurrency proof | Yes | `llm_agent_parallel_test.go:22–48` barrier tool (structural, not timing) |
| White-box internal | Yes | `export_test.go` + 5 `*_internal_test.go` |
| Build-tag live tier | Yes | `live_finalize_test.go` (`//go:build live_finalize`, paid manual gate) |
| Fuzzing | **No** | zero `f.Fuzz` in scope |
| Snapshot/golden wire | **No** | `testdata/` holds only rapid failure caches |
| Mutation testing | Partial | budget.go 82.8% / budget_dedup.go 89.4% / skill_write.go 95.5%; **none for `llm_agent*.go`, `shell_exec.go`, `mcptools`, `workflow`** |

### Realism assessment

**Not "asilo nido" (toy) tests.** Fixtures mirror real wire shapes (streamed chunks with `finish_reason`, finalized tool-call IDs, trailing Usage chunks); shell tests spawn real processes and verify **grandchild PID death** after a timeout kill (`shell_exec_test.go:222–228`); wire-validity tests assert the orphan-tool_result invariant on recorded request history (`llm_agent_wire_validity_test.go:21`); cancellation mid-tool, retry exhaustion, and prefix stability across turns are all pinned.

## Findings

**[P1] The 85% coverage floor does not gate `internal/agent/tools`.** `scripts/coverage_gate.sh:44` filters `/internal/agent/tools/` out with a stale "pre-rewrite skeletons" rationale. The package (32 files incl. `shell_exec.go`, fs tools, web tools, skill tools — 87.5% today) is free to decay below 85% without failing `make coverage`. Highest-leverage finding; one-line fix.

**[P2] No Windows CI lane.** Every `ci.yml` job is `ubuntu-latest`. `shell_exec_windows.go` (`taskkillProcessMissing` 0.0% even on the Windows host), Git-Bash resolution (`shell_exec.go:363–376`), and cmd.exe degraded mode exist only behind `//go:build windows`; POSIX-only fixtures skip on a Git-Bash-less Windows box. Windows-vs-Unix divergence is pinned by nobody but the operator's laptop.

**[P2] MCP reconnect half-tested.** `bridge_reconnect.go`: the `CallTool` reconnect path is covered, but the `ListTools` reconnect branch (55.6%), `Close()` (0.0%, incl. `client==nil`), the double-fault case, and `reconnectLocked`'s post-reconnect `ListTools` failure are untested.

**[P2] `retryableStreamOpenError` tested only via its string-marker tail.** `llm_agent_stream_retry.go:57–75` (58.3%): the `net.Error.Timeout()` and both `url.Error` branches have no test; the marker list is Windows-leaning, so a Linux-deploy regression (dropping `connection reset`) would survive.

**[P2] `truncateTailBytes` duplicated and weakly tested in both copies.** `shell_exec.go:486` (37.5%) and `llm_agent_completion.go:200` (62.5%) — byte-identical; the `n<=0` and rune-boundary branches are uncovered in the shell copy. Also a reusable-code violation.

**[P2] No mutation scores for the loop core.** `llm_agent.go`, `llm_agent_finalize.go`, `shell_exec.go`, `bridge_reconnect.go` — the highest-blast-radius files — have no documented kill rates; the Gate-3 ≥70% requirement is unmet/undocumented for them.

**[P3] Sub-70% functions with real behavior:** `exitCodeFromMeta` 50% (non-int branch), `MountServer`/`MountManagedServer` 33–52% (spawn-failure and mount-rollback `Close()` untested — the "never half-register or leak a process" contract is comment-only), `foldToASCII` 23.5%, `resolveSchedule` 61.5%, `SwarmContext` 0% in-package.

**[P3] No fuzzing on the JSON-args boundary.** Every tool parses model-authored `json.RawMessage`; the truncated-args steering path has one fixture but is never fuzzed.

## Skip-as-green analysis

| Site | Condition | CI verdict |
|---|---|---|
| `live_finalize_test.go:72,88` | `OPENROUTER_API_KEY`/`SEARXNG_URL` unset | **Compliant** — skips locally, `t.Fatal` under `$CI`; behind the `live_finalize` tag, deliberately absent from CI. The exception done right. |
| `tools/shell_exec_test.go:278,299,436` + `shell_bg_test.go:92,115` | cmd.exe fallback (no POSIX bash) | **Run in CI** (ubuntu always has `/bin/sh`). **Latent risk:** if a Windows runner is added without Git Bash, 5 tests vanish silently — no `$CI` guard. |
| `tools/fs_fence_test.go:108` | `os.UserHomeDir` fails | Benign; unreachable on hosted runners. |
| `budget_test.go:294,318` | `int` width == int32 | Benign; correct 64-bit platform guard. |

**Net: no skip-as-green violation today.** The unit job runs the whole package matrix under `-race` with no env gating. The real falsely-green vector is the **coverage-gate filter (P1)** silently excluding `agent/tools` from the floor.

## Proposed test pyramid

```
        ╱ live (paid, tag-gated, manual)  ── live_finalize + a new mcp-live tier
       ╱  e2e / contract  ── golden pause-event serialization, wire-validity invariants
      ╱   integration  ── runner persistence/resume, MCP reconnect (fake server), shell real-proc
     ╱    property  ── truncateTailBytes, dedup, event round-trip, args-parse fuzz
    ╱     unit (table-driven)  ── the broad base, already strong
```

The base is healthy; the gaps are at **integration** (persistence/resume, MCP failure modes), **contract** (golden wire shapes), and **fuzz/mutation** (the model-authored-args boundary and the loop core).

### ~16 specific high-value tests to add

| # | Test | File target | Technique | Regression pinned |
|---|---|---|---|---|
| 1 | `TestReconnect_ListToolsTransportError` | `mcptools/bridge_test.go` | fake reconnecting client | `bridge_reconnect.go:38–46` |
| 2 | `TestReconnect_SecondCallFailsAfterReconnect` | `mcptools/bridge_test.go` | fake | error propagation at `:60` (no infinite loop / masked error) |
| 3 | `TestReconnectingServer_CloseIdempotentAndNilSafe` | `mcptools/bridge_test.go` | fake | `Close()` 0% + double-Close at shutdown |
| 4 | `TestMountServer_MountFailureClosesClient` | `mcptools/mount_test.go` | fake, colliding names | "never half-register or leak a process" |
| 5 | `TestRetryableStreamOpenError_NetAndURLErrors` | new internal test | table over typed errors | `stream_retry.go:58–74` (58→~100%) |
| 6 | `TestTruncateTailBytes_Property` (after folding) | `tools/result_test.go` | rapid | rune-boundary corruption in both copies |
| 7 | `TestShellExec_StderrTailReservedUnderCap` | `tools/shell_exec_test.go` | real proc + small cap | failing-command diagnostics survive truncation |
| 8 | `FuzzShellExecArgs` | new fuzz test | native fuzzing, truncated-JSON seed | args-parse panic-freedom + steering contract |
| 9 | `TestShellExec_TimeoutKeepsPreviousTrackedCwd` | `tools/shell_exec_test.go` | real proc | cwd corruption after a kill |
| 10 | `TestExitCodeFromMeta_NonIntAndMissing` | `event_test.go` | table | wrong exit code to AG-UI/Telegram |
| 11 | `TestPauseEvent_GoldenSerialization` | `llm_agent_pause_test.go` + golden | golden/contract | the pause wire shape consumed by `paused_states` |
| 12 | `TestDispatch_ParallelToolPanicIsolated` | `llm_agent_parallel_test.go` | barrier + panicking tool | siblings' tool_results not orphaned under concurrency |
| 13 | `TestDispatch_CancelMidParallelBatch_AllCallsAnswered` | `llm_agent_parallel_test.go` | barrier + cancel + goleak | every tool_call_id gets a RoleTool (provider 400 otherwise) |
| 14 | `TestPrefixStable_AcrossPauseResumeAndToolError` | `llm_agent_test.go` | extend prefix test | KV-cache invariant under non-happy paths |
| 15 | `TestSwarmContext_RoundTripAndAbsent` | new `swarm_context_test.go` | unit | adapter no-panic-on-absent-key |
| 16 | `TestFsEdit_EmptyOldStringRejected` | `tools/fs_edit_test.go` | table | the P1 file-corruption bug |
| 17 | `TestRunner_IntraTurnPersistedAcrossResume` | `internal/runner` test | integration | the P1 persistence-loss bug (after fix) |
| 18 | `TestMCPCall_TimesOutAndPoisonsTransport` | `internal/mcp` test | fake hung server + goleak | the P0 hang (after fix) |

## CI recommendations

1. **Remove `/internal/agent/tools/` from `scripts/coverage_gate.sh:44` now** (P1). Re-check `/internal/sandbox/` + `/internal/llm/client.go:` for the same staleness. Re-baseline: at 87.5% the floor still passes.
2. **Add a `windows-latest` lane** for the shell surface (`go test ./internal/agent/tools/ -run 'TestShellExec|TestBackgroundShell'`); install Git Bash explicitly or assert `shellIsCmdFallback()==false`; give the POSIX skips a `$CI` fatal guard.
3. **Wire the new fuzz target** as a seeded corpus-regression run in the unit job (mirror `FuzzSkillValidator` in skills.yml).
4. **Add the mutation row** for the loop-core files to `docs/aura-quality-snapshot.md` (run `go-mutesting` on them in WSL — the only Gate-3 metric this package has never reported).
5. **No change to the live tier** — `live_finalize` gating is exemplary; use it as the template if `mcptools` grows a live MCP-process tier.
