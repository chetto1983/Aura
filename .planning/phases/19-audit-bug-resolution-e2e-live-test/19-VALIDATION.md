---
phase: 19
slug: audit-bug-resolution-e2e-live-test
status: planned
nyquist_compliant: true
wave_0_complete: false
created: 2026-06-10
---

# Phase 19 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.
> Detailed two-layer design lives in `19-RESEARCH.md` §"Validation Architecture".
> Layer 1 = fails-before/passes-after regression for EVERY finding (CI-committed).
> Layer 2 = real paid-agent + real-user-prompt live repro for user-observable findings
> (operator sign-off gate, NOT CI-automated — recorded in `docs/audit/19-LIVE-SIGNOFF-2026-06-10.md`).

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | go test (Go 1.26.4) |
| **Config file** | none — standard `go test`; integration via build tags |
| **Quick run command** | `go vet ./... && go build ./... && go test ./internal/<touched-package>/` |
| **Full suite command** | `go test -race ./...` then `make quality-full` (WSL, stack up) |
| **Estimated runtime** | unit ~seconds/pkg; full race+coverage matrix ~minutes |

**Integration tiers (no-skip-as-green — `t.Fatal` under `$CI` when env unset):**
- `-tags db_integration` (+ `AURA_DB_URL`/`AURA_DB_MIGRATE_URL`) — H6/H7 durable notification state (migration 0013), M-h shutdown-ctx terminal write, L4 search status filter.
- `-tags neo4j_integration` — M-j knowledge sidecar stderr ring (if exercised live).
- Cross-platform: H4 process-group/`WaitDelay` regression MUST pass on BOTH Linux (WSL/CI) and Windows (w64devkit) — first `//go:build` OS-split in `internal/`.

---

## Sampling Rate

- **After every task commit:** Run quick command for the touched package (`go vet` + `go build` + `go test ./internal/<pkg>/`, add `-race` for concurrency-touched packages: shell_exec/tools, agui, cron, llm, agent, mcptools, knowledge).
- **After every plan wave:** Run `go test -race ./...` across touched packages.
- **Before `/gsd-verify-work`:** Full `make quality-full` green (coverage floor ≥85% owned surface) + every Layer-1 regression green in CI.
- **Max feedback latency:** quick run < ~30s per package.

---

## Per-Task Verification Map

> One row per finding-fix task. Each "Automated Command" is the Layer-1 fails-before/passes-after
> regression; user-observable findings ALSO have a Layer-2 live row in the Manual-Only table below.

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| 19-01-T1 | 19-01 | 1 | H4, L3, M-f | T-19-01/02/03 | process-group kill, no orphan, no leaked Go err | unit+race (tools), cross-OS | `go test -run TestShellExecTimesOut ./internal/agent/tools/` (grandchild PID dead) | ❌ W0 | ⬜ pending |
| 19-01-T2 | 19-01 | 1 | H5 | — | exit-code/stderr footer survives truncation | unit (tools+agent) | `go test -run 'TestShellExec\|TestSideEffectDigest' ./internal/agent/tools/ ./internal/agent/` | ✅ | ⬜ pending |
| 19-03-T1 | 19-03 | 1 | H9 | T-19-06/07 | terminal Chunk.Err emitted before close | unit (openai_compat) | `go test -run 'TestPrematureClose\|TestParseSSE' ./internal/llm/openai_compat/` (consumes premature_close.sse) | ❌ W0 | ⬜ pending |
| 19-03-T2 | 19-03 | 1 | H9 | T-19-06 | main loop refuses partial-as-complete | unit+race (agent) | `go test -race -run 'TestStreamErr\|TestLoop' ./internal/agent/` | ✅ | ⬜ pending |
| 19-04-T1 | 19-04 | 1 | H1 | T-19-09 | only delta frames drop; survivors ValidateSequence-valid | unit+race (agui) | `go test -race -run TestFanoutSlowSubscriberDropped ./internal/agui/` | ✅ (rewrite) | ⬜ pending |
| 19-04-T2 | 19-04 | 1 | M-c, M-d | T-19-08(HIGH)/10 | Fanout RUN_ERROR + trace redacted; multimodal explicit | unit (agui) | `go test -run 'TestSanitize\|TestRunError\|TestMultimodal' ./internal/agui/` | ✅ | ⬜ pending |
| 19-06-T1 | 19-06 | 1 | H8 | T-19-14 | boundary drop, no orphan RoleTool head | unit (conversations) | `go test -run 'TestDropOldest\|TestContextBoundary' ./internal/conversations/` | ✅ (fixture) | ⬜ pending |
| 19-07-T1 | 19-07 | 1 | H6, H7 | T-19-16 | migration 0013 + sqlc role-separated | integration (db) | `go test -tags db_integration -run 'TestMigrat\|TestPendingNotif' ./internal/db/` | ❌ W0 | ⬜ pending |
| 19-07-T2 | 19-07 | 1 | H6, H7 | T-19-15/17 | deferred persisted+flushed; failed bounded-retried | integration (db, cron) | `go test -tags db_integration -run 'TestNotify\|TestSweep\|TestQuietHours' ./internal/cron/` | ❌ W0 | ⬜ pending |
| 19-09-T1 | 19-09 | 1 | H10 | T-19-21 | reconnect-on-use, description refresh, BM25 intact | unit+race (mcptools) | `go test -race -run 'TestReconnect\|TestBridge' ./internal/agent/mcptools/` | ✅ | ⬜ pending |
| 19-09-T2 | 19-09 | 1 | M-j | T-19-22 | bounded-ring stderr, stderrTail intact | unit (mcp, knowledge) | `go test -run 'TestSafeBuffer\|TestRing' ./internal/mcp/ ./internal/knowledge/` | ✅ | ⬜ pending |
| 19-10-T1 | 19-10 | 1 | M-i | T-19-24 | central godotenv.Load visible to subcommands | unit (cmd/aura, mcp) | `go test -run 'TestEnv\|TestManagedConfig' ./cmd/aura/ ./internal/mcp/` | ✅ | ⬜ pending |
| 19-10-T2 | 19-10 | 1 | L1, L2, L4, L6 | T-19-25/26/27/28 | snippet timeout; SanitizeName guard; status filter; empty-arg reject | unit + integration (db for L4) | `go test -tags db_integration -run 'TestSnippet\|TestActivate\|TestDiscard\|TestSearch\|TestWebSearch\|TestWebFetch' ./cmd/aura/ ./internal/skills/ ./internal/conversations/ ./internal/agent/tools/` | ✅ | ⬜ pending |
| 19-10-T3 | 19-10 | 1 | INFO | T-19-29 (accept) | trust boundary documented, NO scanner | doc | `test -f docs/audit/trust-boundary-info-2026-06-10.md` + loader.go unchanged | ✅ | ⬜ pending |
| 19-05-T1 | 19-05 | 2 | H2, H3 | T-19-11(HIGH)/12 | sanitized error render; async convert notice | unit (telegram) | `go test -run 'TestRunError\|TestStatusPane\|TestAsyncConvert' ./internal/channels/telegram/` | ✅ (golden) | ⬜ pending |
| 19-05-T2 | 19-05 | 2 | M-e | T-19-13 | /cancel cancels pause, keyboard cleared | unit (telegram) | `go test -run 'TestCancel\|TestHitl\|TestPause' ./internal/channels/telegram/` | ✅ | ⬜ pending |
| 19-08-T1 | 19-08 | 2 | M-g | T-19-18 | catchUpMissed consults ReschedulesOnRecovery | unit (cron) | `go test -run 'TestCatchUp\|TestReschedule' ./internal/cron/` | ✅ | ⬜ pending |
| 19-08-T2 | 19-08 | 2 | M-h, L5 | T-19-19/20 | detached terminal write; inert SKIP LOCKED dropped | integration (db, cron) | `go test -tags db_integration -run 'TestComplete\|TestShutdown\|TestDueTasks' ./internal/cron/` | ❌ W0 | ⬜ pending |
| 19-02-T1 | 19-02 | 3 | M-b | T-19-04 | veto appends only nudge, no resurface | unit (agent) | `go test -run 'TestGateCompletion\|TestVeto\|TestWireValid' ./internal/agent/` | ✅ | ⬜ pending |
| 19-02-T2 | 19-02 | 3 | M-a | T-19-05 | 3 stream-opens via streamWithOpenRetry | unit (agent) | `go test -run 'TestCompletion\|TestFinalize\|TestReasoning\|TestStreamRetry' ./internal/agent/` | ✅ | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky · File Exists: ❌ W0 = the test file/fixture is a Wave-0 dependency to create first.*

**Continuity:** every fix task (19-01..19-10, 19-02, 19-05, 19-08) has an automated Layer-1 verify; no 3 consecutive fix tasks lack an automated verify. 19-11's 3 tasks are the FINAL manual Layer-2 sign-off (Manual-Only table) — by design, NOT CI-automated (D-03).

---

## Wave 0 Requirements

- [ ] Rewrite the 5 named false-green tests (D-04) so they fail-before/pass-after:
  `TestFanoutSlowSubscriberDropped` (re-validate surviving sub-sequence via AG-UI `events.ValidateSequence`, H1 — 19-04),
  `TestShellExecTimesOut` (assert child PID actually dead, H4 — 19-01),
  `context_boundary_test.go` fixtures (cover `assistant(tool_calls)→tool→tool→assistant` round, H8 — 19-06),
  consume orphan `testdata/premature_close.sse` (H9 — 19-03, do NOT delete),
  correct/remove stale `serve_channels.go:142` comment (H2 — 19-05).
- [ ] H4 cross-platform regression harness (`shell_exec_test.go` + the OS-split files) — grandchild-PID-death assertion on Linux + Windows (19-01).
- [ ] Migration `0013_pending_notifications` + sqlc queries + regenerated client for H6/H7 durable notification state (db_integration fixtures) (19-07).
- [ ] Extend `agenttest.FakeClient` to script an `Err` chunk (H9 + M-a transient-failure tests) (19-03).

*These Wave-0 deliverables are the test scaffolds/fixtures the Layer-1 regressions need; they are created inside the owning plan (the false-green rewrites land WITH their corresponding fix per D-04).*

---

## Manual-Only Verifications (Layer 2 — live operator sign-off, paid, NOT CI — plan 19-11)

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Shell turn answers after long/orphaning command | H4/H5/M-b | Real paid agent + real user prompt; live stack | `aura chat` host loop; run a command that spawns an orphan grandchild + large output; confirm a real terminal answer (not silence / not "run it yourself" / not false success) with exit-code awareness; `· shell_exec` trace = ground truth. |
| LLM mid-stream SSE failure surfaced, not delivered as a complete answer | H9 | Live transport failure | `aura chat`; induce/observe mid-stream cut; confirm error/retry, not a truncated "answer" (Layer-1 premature_close.sse is the binding proof if a deterministic live cut is not reproducible). |
| Telegram error turn shows the reason | H2 | CDP harness, real bot | `D:/tmp/tg_cdp.py`; trigger an errored turn; confirm sanitized failure message renders (not bare "Stato: errore"); DB = ground truth. |
| Telegram async doc-conversion failure notifies user | H3 | CDP harness | Send a 5–50 MB doc that fails conversion; confirm `convertFailMessage` arrives (not eternal "elaborando…"). |
| `/cancel` during a pending `ask_user` pause cancels the pause | M-e | CDP harness, HITL | Enter a pause, `/cancel`; confirm `paused_states` row cleared (DB) + keyboard retired. |
| Scheduler deferred/undelivered notification reaches the user | H6/H7 | Live cron tick | Apply 0013; schedule a job inside quiet hours / force a self-send failure; confirm window-end flush / bounded retry delivers (DB `pending_notifications` ground truth). |
| AG-UI slow client keeps a conformant stream | H1 | Deliberately-slow SSE client | Drive AG-UI SSE with a throttled subscriber; confirm `events.ValidateSequence` accepts the surviving turn. |

**Non-observable findings → Layer-1 regression-only (NO live row):** M-a (transient retry — internal), M-c/M-d (agui transport — covered by Layer-1; M-c trace leak asserted in unit), M-f (stderr interleave), M-g (recovery flag), M-h (shutdown-ctx run-state), M-i (env load), M-j (RSS bound), L1/L2/L3/L4/L5/L6, INFO (doc-only).

*Record live evidence in `docs/audit/19-LIVE-SIGNOFF-2026-06-10.md` (mirror Phase 13-10 / Phase 8 live sign-off pattern): visual body print + mojibake scan + DB/tool-trace ground-truth assertion (not r.Reply).*

---

## Validation Sign-Off

- [x] All fix tasks have `<automated>` verify or Wave 0 dependencies (19-11's manual sign-off tasks are in the Manual-Only table by design)
- [x] Sampling continuity: no 3 consecutive fix tasks without automated verify
- [x] Wave 0 covers all MISSING references (5 false-green rewrites + H4 cross-platform harness + 0013 migration + FakeClient.Err)
- [x] No watch-mode flags
- [x] Feedback latency < 30s (per-package quick run)
- [ ] Layer-2 live operator sign-off recorded for all user-observable findings (plan 19-11, at execution time)
- [x] `nyquist_compliant: true` set in frontmatter

**Approval:** planner-complete (nyquist map populated; live sign-off pending execution of 19-11)
