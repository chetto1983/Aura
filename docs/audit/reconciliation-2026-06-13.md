# Reconciliation — verified true status (2026-06-13)

The 2026-06-12 audit set (`risk-register.md`, `audit-index.json`, `re-audit-2026-06-12.md`) was written in the **morning**, listing all 40 findings as OPEN. Later that day commit **`ec7fe2f6 "fix(audit): close P1 items"`** (32 files, +803) and the 08.2 phase landed. Those audit docs never reflected the fixes, so this note **supersedes their Status columns** with a re-verification of every finding against the current `tabula-rasa` working tree.

## Method

Four read-only verification passes (reliability/loop · memory/context · ops/observability · security/tools/test) re-read the code for each finding against its **acceptance criterion** (not its presence), with `file:line` evidence and the asserting test where one exists. The headline durability fix **B-01** was additionally read down to its recovery-marker test. Every prior "CLOSED" was treated as a claim to falsify — consistent with the audit's own over-credit warning.

## Score

**6.5 → ~7.5.** All P1 go-live blockers are genuinely closed and tested (the audit predicted "closing the P1 set lands 7.5–8"). The score is held below 8 by the **22 OPEN + 6 PARTIAL P2/P3** items (no idle watchdog, per-turn breaker, microcompact answer-loss, no `/readyz`, no Windows CI, no fuzz/bench/mutation). Core loop ≈8.5; perimeter now ≈6.5.

## Tally (40 findings)

| Status | Count | IDs |
|---|---|---|
| **CLOSED** | 10 | B-01, B-02, B-03, B-04, O-01, O-02, D-01 (the P1 gate) · M-05, B-11, T-02 |
| **PARTIAL** | 6 | B-07, B-13, M-06, O-04, B-15, T-03 |
| **OPEN** | 22 | B-05, B-06, B-08, B-12, M-01, M-02, M-03, M-04, M-07, M-08, R-41, O-03, O-05, O-06, O-07, O-08, B-09, B-10, B-14, B-16, T-01, T-04 |
| **TRACKED** | 2 | R-26, R-40 |

## P1 gate — CLOSED & tested (`ec7fe2f6`, post-audit)

| ID | Verified evidence | Acceptance test (PASS) |
|---|---|---|
| B-01 | `conversations/store_helpers.go:302` repairs a dangling mutating call with a synthetic "previous result unknown… verify before re-running" marker instead of dropping it; write-ahead ordering enforced | `store_unit_test.go:154-188` (preserved+marked) · `runner_test.go:268` (`TestTurnPersistsAssistantToolCallsBeforeMutatingToolExecutes`) · `TestResume_NoSilentReRun_SC4` |
| B-02 | `swarm/runner_adapter.go:62` sets `Provenance{Source:"swarm",Trust:Untrusted}`; honored at `agent/trust.go:34` | `runner_adapter_test.go:61-65` |
| B-03 | per-thread in-flight guard in Runner, shared by AG-UI | `agui/server_p1_test.go` `TestServer_RunBusyThread409` |
| B-04 | `skill` tool schema states the true auto-activate policy; audit/alert fires on the ungated path | `TestSkillToolSchemaStatesActualAutoActivationPolicy`, `TestModelMutationBypassesGateExceptAlwaysOn`, `TestAuditActionFor` |
| O-01 | shared `obs.Init` (`internal/obs/init.go:47-79`) boots the tracer + JSON `slog` (service/version + secret redaction), called by `serve.go` and `chat_repl.go` | `TestInitInstallsJSONLoggerWithServiceAttrsAndTracerShutdown` |
| O-02 | `internal/agent/metrics.go` prom counters/histograms; `agui/server.go` mounts `GET /metrics` | `TestServerMetricsExposesPrometheus` |
| D-01 | non-root `Dockerfile` + hardened `aura` compose service (`read_only`, `cap_drop`, `mem_limit`, healthcheck) | `cmd/aura/container_artifacts_test.go` `TestProductionContainerArtifactsAreHardened` |

> The audit-index counted "8 P1"; the register carries **7** distinct P1 rows (B-04 folds the R-09 schema+alert pair). All 7 are closed.

## Other CLOSED / PARTIAL (P2/P3)

| ID | Status | Verified note |
|---|---|---|
| M-05 | CLOSED | `context.go` `dropOldestRound` preserves the tail; undroppable oversized → `ErrContextWindowExceeded` (`context_unit_test.go`). Original design, predates the audit. |
| B-11 | CLOSED | `shell_exec.go` = **599** LOC — at the wire, satisfies "no file >600". No real split; trivially re-breaches on next touch. |
| T-02 | CLOSED | `TestSendFileCaptionASCIISanitized` covers `foldToASCII`. |
| O-04 | PARTIAL | Secret-redacting `slog` handler done (`obs/init.go:81`); **no `Config.Validate()` fail-fast** — empty `NEO4J_PASSWORD` still boots. |
| M-06 | PARTIAL | Boot-time `tmp/*` 24h sweep exists; **no periodic sweep, no reasoningtrace rotation/cap**. |
| B-07 | PARTIAL | Escalate-on-exhaustion terminates a budget-owning branch; **no test for `maxIter=0` + budget-owning** termination. |
| B-13 | PARTIAL | Typed `errors.As` checks present **plus a retained substring fallback**; no `io.ErrUnexpectedEOF`/`ECONNRESET` sentinels. |
| B-15 | PARTIAL | Bridged MCP tool desc/summary framed; **arg-schema field descriptions still unframed/uncapped**. |
| T-03 | PARTIAL | Each deferred tool's `Spec()` tested individually; **no single golden sweep** over all deferred specs. |

## Genuinely OPEN — the real remaining roadmap (22)

- **Reliability:** B-05 (per-turn breaker, `llm_agent.go:131`), B-06 (breaker-open → error slot not finalize), B-08 (no stream idle watchdog), B-12 (live partial-chunk replay, cosmetic), M-08 (`EnsureConversation` masks 23505).
- **Memory/context:** M-01 (microcompact destroys `ask_user` answers — `context.go` `applyL1` has no sidecar guard), M-02 (`SubmitAnswer` inject + mark are **separate** txns — `runner_resume.go:77`), M-03 (`hardCap<=0` returns raw history; the bug is **test-locked** in `context_boundary_test.go`), M-04 (sidecar spill outside the tx), M-07 (`anyInt` lacks `json.Number` in **both** copies — `runner_persist.go` + `chat_render.go`), R-41 (per-session tool state never evicted).
- **Ops/obs:** O-03 (otel no-op error handler), O-05 (no `/readyz`, `/healthz` PG-only), O-06 (SIGTERM hard-cancels turns), O-07 (no Windows CI lane), O-08 (spans `llm.request`-only).
- **Security:** B-09 (divergent `secretEnvKey` — `PRIVATE_KEY` redacts in MCP, not shell), B-10 (destructive gate off by default, no conservative defaults), B-14 (`Registry.Register` silent overwrite), B-16 (`fs_grep`/`fs_glob` no node/deadline cap).
- **Test apex:** T-01 (no fuzz, no agent-core mutation score, bench targets the wrong path), T-04 (`agenttest` still in the coverage denominator).

## Sequencing for the close-out

Reliability + memory correctness first (B-05/06, B-08, M-01, M-02, M-03, M-04), then ops/security (O-03/04/05/06/07/08, B-09/10/14/16), then test apex (T-01/03/04) and cleanups (M-07, B-12, M-08). Two need a design decision before coding: **M-03** (what `hardCap≤0` should do for small-window models) and **B-11** (whether to actually split the 599-LOC file or leave it at the wire).

## Closures landed 2026-06-13 (the PARTIAL set, TDD-first, one atomic commit each)

| ID | Commit | What landed |
|---|---|---|
| B-13 | `b5767c2a` | typed network sentinels (`io.ErrUnexpectedEOF`/`io.EOF`/`syscall.ECONNRESET`/`ECONNREFUSED`/`ETIMEDOUT`) as the primary retry classifier; substring table demoted to last-resort fallback |
| B-15 | `66e014a5` | recursive 512-byte cap on every MCP arg-schema `description`; `bridge_spec_test.go` split to hold the 600-LOC line |
| O-04 | `a6373124` | `Config.Validate()` fail-fast on empty DB DSN / `NEO4J_PASSWORD`, wired into the shared chat/serve boot before any connection opens |
| B-07 | `99ccf919` | no-progress guard terminates a `maxIter=0` loop whose budget-owning sub neither spends nor escalates; `terminalEventKind` carries the reason |
| M-06 | `a822b1b5` | reasoningtrace rotates to a `.1` backup at a byte cap (`AURA_REASONING_TRACE_MAX_BYTES`, default 8 MiB) — **part 1 only** |
| T-03 | `298d04ea` | golden well-formed-spec sweep over the 18 built-in tools (unique names, present summary/description, JSON-object params, deferred-richness) |

**Result: 5 PARTIAL → CLOSED.** M-06 advanced to rotation-done; its **periodic sidecar TTL sweep + archived-sidecar reclaim** half remains a background-worker + reclaim-policy feature deferred to its own pass. Updated tally: **15 CLOSED / 1 PARTIAL (M-06) / 22 OPEN / 2 TRACKED.**

## Reliability cluster — OPEN → CLOSED (2026-06-13, TDD-first, one atomic commit each)

| ID | Commit | What landed |
|---|---|---|
| B-05 | `52c0565b` | breaker hoisted to a **process-lifetime Runner singleton** (`Deps.Breaker` → `Runner.breaker`, defaulted via `llm.NewDefaultBreaker`) and injected into every per-turn agent (`LlmAgentConfig.Breaker`) so a provider outage trips cross-turn protection — the per-turn breaker reset on each rebuild and never opened. Refactor-on-touch: `consume` split to `llm_agent_consume.go`. Tests: `TestInjectedBreakerIsSharedNotPerAgent` (agent mechanism), `TestRunnerInjectsSharedBreakerIntoEveryTurn` (Runner wiring), `TestNewDefaultBreakerUsesPolicyDefaults` (policy single-source). |
| B-06 | `c9515390` | breaker-open now routes to `finalize()` (a non-empty terminal Event via the deterministic stub digest) instead of the iter.Seq2 **error slot** — graceful degradation, not an infra failure. `finalize`'s own synthesis short-circuits the same open breaker and falls through to the stub, so the terminal is always non-empty. Test: `TestBreakerOpenRoutesToFinalize`. |
| B-08 | _(this commit)_ | per-read **stream idle watchdog** in the openai_compat client (`AURA_LLM_STREAM_IDLE_TIMEOUT_SEC`, default 60s, 0 disables). Resets on ANY bytes from the wire — data OR `: OPENROUTER PROCESSING` keep-alives — so a long reasoning phase never trips it; only a dead connection does. On a stall it cancels the read and emits a **retryable** `ErrStreamIdleTimeout`, which the agent's stream classifier retries once before surfacing. Tests: `TestStream_IdleTimeoutAbortsStall`, `TestStream_IdleTimeoutDisabledWhenZero`, `TestRetryableStreamOpenError_IdleTimeout` + config default/override. |

Running tally after this section: **18 CLOSED / 1 PARTIAL (M-06) / 19 OPEN / 2 TRACKED.**

## Memory-correctness cluster — OPEN → CLOSED (2026-06-13, TDD-first, one atomic commit each)

| ID | Commit | What landed |
|---|---|---|
| M-01 | `b4f09719` | L1 microcompact (`applyL1`) now rewrites a `RoleTool` turn to a `read_tool_output` pointer **only when it is sidecar-backed** (`isSidecarBacked`: a `[output truncated:` footer or a `ContentSidecarPath`). A non-spilled turn — an `ask_user` answer or a small inline result — has nowhere to page back from, so the old unconditional rewrite created a dead pointer that silently destroyed the content after ~evictAfter rounds (R-28). Test: `TestApplyL1_PreservesNonSidecarToolAnswers`; 5 pre-existing fixtures that evicted unrealistic large-non-footer turns were made sidecar-backed (production always spills large outputs with a footer). |
| M-02 | _(this commit)_ | **Gate-first reorder** in `SubmitAnswer`: `MarkResumed` (whose `RowsAffected==0` gate returns `ErrPauseNotFound`) runs BEFORE `injectAnswer`, so a duplicate/already-resumed token is rejected before a second answer turn is appended (the old order injected first → two tool results for one tool_call → wire-invalid round, R-27). Meets the AP-9 acceptance (retry → exactly one answer turn + `ErrPauseNotFound`). Per user decision the full single-tx was **downgraded to the reorder** (the cross-store tx would force re-wiring ~94 runner-fake touchpoints); the residual `MarkResumed`-then-`injectAnswer`-fail window is documented and deferred. Test: `TestSubmitAnswer_DuplicateResumeInjectsExactlyOneAnswer`. |
| M-03 | _(this commit)_ | **Nanobot-style small-window `hardCap` floor.** `ContextConfig.hardCap()` kept the SPEC Req#10 formula (`ContextWindow − max(MaxOutputTokens,20000) − 13000`) for normal/large windows but, when the formula is **non-positive** (a window below the ~33k fixed reservation — e.g. a Slice-13 local vLLM), now clamps to a positive `smallWindowHardCapFloor(ContextWindow)` = `ContextWindow/2` instead of `0`. Previously a `hardCap==0` made `applyContextLadder` return **raw history with L2/L2.5 protection entirely off** (R-29); the floor keeps L2.5 truncation active. Only a degenerate `ContextWindow<=0` still yields `0` (the sole remaining `hardCap==0` path; boot fail-fast is the seam to reject it). The change is a Req#10 semantics shift → PRD-amendment committed first (PRD-first). The pre-fix behavior was test-locked in `context_boundary_test.go` part (b) + `context_unit_test.go` `TestHardCap`, both repointed with justification to the window≤0 path. Tests: `TestLadder_SmallWindowFloor_ProtectsNotRaw`, `TestSmallWindowHardCapFloor` (new `context_smallwindow_test.go`). |

Running tally after this section: **21 CLOSED / 1 PARTIAL (M-06) / 16 OPEN / 2 TRACKED.**
