---
phase: 03
slug: llm-client-toolresult
status: draft
nyquist_compliant: true
wave_0_complete: false
created: 2026-05-30
---

# Phase 03 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | `go test` (Go stdlib `testing` + `net/http/httptest` + `go.uber.org/goleak` v1.3.0 + OTel `tracetest` in-memory recorder v1.44.0 + `pgregory.net/rapid` property tests) |
| **Config file** | none — Go convention (`go.mod`); golden fixtures in `internal/llm/openai_compat/testdata/*.sse` |
| **Quick run command** | `cd /mnt/d/Aura && go test ./internal/llm/... ./internal/agent/...` |
| **Full suite command** | `cd /mnt/d/Aura && go test -race ./internal/llm/... ./internal/agent/... ./cmd/aura/...` (+ `make quality-full` for the coverage gate) |
| **Estimated runtime** | ~25 seconds (deterministic tier, no network) |

---

## Sampling Rate

- **After every task commit:** Run `cd /mnt/d/Aura && go test ./internal/<touched-package>/ && go vet ./... && go build ./...` (Gate 2)
- **After every plan wave:** Run `cd /mnt/d/Aura && go test -race ./internal/llm/... ./internal/agent/... ./cmd/aura/...` (+ goleak)
- **Before `/gsd:verify-work`:** `make quality-full` green (owned-surface coverage ≥85% across the full tag matrix — overrides PRD 75/60 per CLAUDE.md), then manual `scripts/llm_smoke.sh` for the Req#11 live acceptance
- **Max feedback latency:** 30 seconds (deterministic tier)

---

## Per-Task Verification Map

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| 03-02-01 | 02 | 2 | CORE-01 (Req#1) | T-03-06 | `[DONE]`/`:`-comment never reach json.Unmarshal | unit (golden) | `cd /mnt/d/Aura && go test ./internal/llm/openai_compat/ -run TestStream_TextStop` | ❌ W0 (+`text_stop.sse`) | ⬜ pending |
| 03-02-01 | 02 | 2 | CORE-01 (Req#2) | T-03-06 | oversized tool-arg line parsed, not truncated | unit (golden, property) | `cd /mnt/d/Aura && go test ./internal/llm/openai_compat/ -run TestAccumulate` | ❌ W0 (+`toolcall_multichunk.sse` >64KB) | ⬜ pending |
| 03-02-02 | 02 | 2 | CORE-01 (Req#3) | T-03-05 | ctx-cancel ≤100ms, zero goroutine leak | unit + `-race` + goleak | `cd /mnt/d/Aura && go test -race ./internal/llm/openai_compat/ -run TestStream_CancelMidStream` | ❌ W0 (+`premature_close.sse`) | ⬜ pending |
| 03-02-01 | 02 | 2 | CORE-01 (Req#4) | — | 429 → HTTPError, request count==1, no retry | unit (golden) | `cd /mnt/d/Aura && go test ./internal/llm/openai_compat/ -run TestStream_429NoRetry` | ❌ W0 (+`error_429.sse`) | ⬜ pending |
| 03-01-02 | 01 | 1 | CORE-01 (Req#5) | T-03-02 | empty key → clean non-panic error | unit | `cd /mnt/d/Aura && go test ./internal/llm/ -run TestConfigLoadOrder` | ❌ W0 | ⬜ pending |
| 03-03-01 | 03 | 1 | CORE-01 (Req#6) | T-03-08 | 100KB→≤2KiB preview+sidecar; ≤cap→no file; UTF-8 boundary | unit (property for UTF-8 boundary) | `cd /mnt/d/Aura && go test ./internal/agent/tools/ -run TestNewResult` | ❌ W0 | ⬜ pending |
| 03-03-02 | 03 | 1 | CORE-01 (Req#7) | T-03-07 / T-03-09 | unknown id hard-fail; path-traversal rejected; offset/limit clamped | unit | `cd /mnt/d/Aura && go test ./internal/agent/tools/ -run 'TestReadToolOutput\|TestSidecarPathTraversal'` | ❌ W0 | ⬜ pending |
| 03-03-01 | 03 | 1 | CORE-01 (Req#8) | T-03-07 | sidecar at `conversations/<session_id>/<tool_call_id>.result` | unit (filesystem assert) | `cd /mnt/d/Aura && go test ./internal/agent/tools/ -run TestSidecarLayout` | ❌ W0 | ⬜ pending |
| 03-04-02 | 04 | 3 | CORE-01 (Req#9) | T-03-11 | ordered Events; malformed args never panic | unit (fake Client) + `-race` + goleak | `cd /mnt/d/Aura && go test -race ./internal/agent/ -run TestLlmAgent_EventOrder` | ❌ W0 | ⬜ pending |
| 03-04-02 | 04 | 3 | CORE-01 (Req#10) | T-03-10 | budget step/wallclock/dedup → terminal Event, steps≤cap | unit (fake Client) | `cd /mnt/d/Aura && go test ./internal/agent/ -run 'TestLlmAgent_(StepCap\|WallclockCap\|DedupWindow)_Trips'` | ❌ W0 | ⬜ pending |
| 03-05-01 | 05 | 4 | CORE-01 (Req#11) | T-03-01 | 2-turn shared session_id; missing key clean error | unit (scripted stdin) + **manual smoke** | `cd /mnt/d/Aura && go test ./cmd/aura/ -run TestChat` | ❌ W0 | ⬜ pending |
| 03-01-02 | 01 | 1 | CORE-01 (Req#12 table) | — | `usage.cost`→exact; absent→table; unknown→`n/a` (never `$0`) | unit (3 golden usage fixtures) | `cd /mnt/d/Aura && go test ./internal/llm/ -run TestCost` | ❌ W0 | ⬜ pending |
| 03-02-01 | 02 | 2 | CORE-01 (Req#12 wire) | — | usage chunk: cached_tokens distinct from cache_write_tokens | unit (golden) | `cd /mnt/d/Aura && go test ./internal/llm/openai_compat/ -run TestUsage` | ❌ W0 | ⬜ pending |
| 03-04-01 | 04 | 3 | CORE-01 (Req#13) | T-03-01 | exactly 1 `llm.request` span/call, all attrs, no api_key; `req.Messages` byte-identical | unit (in-memory recorder) | `cd /mnt/d/Aura && go test ./internal/agent/ -run 'TestSpan\|TestMessagesImmutable'` | ❌ W0 | ⬜ pending |
| 03-03-02 | 03 | 1 | CORE-01 (Req#14 tool) | — | `current_time` RFC-3339 UTC + IANA tz; invalid tz → error | unit | `cd /mnt/d/Aura && go test ./internal/agent/tools/ -run TestCurrentTime` | ❌ W0 | ⬜ pending |
| 03-04-01 | 04 | 3 | CORE-01 (Req#14 prefix) | T-03-12 | `messages[0]` no timestamp & byte-stable across turns | unit | `cd /mnt/d/Aura && go test ./internal/agent/ -run 'TestPrompt\|TestPrefixStable'` | ❌ W0 | ⬜ pending |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Sampling Rate (concrete commands)

- **After every task commit:** `cd /mnt/d/Aura && go test ./internal/<touched-package>/ && go vet ./... && go build ./...`
- **After every plan wave:** `cd /mnt/d/Aura && go test -race ./internal/llm/... ./internal/agent/... ./cmd/aura/...`
- **Property tier (Req#2, Req#6):** `cd /mnt/d/Aura && go test ./internal/llm/openai_compat/ ./internal/agent/tools/ -run 'TestAccumulate|TestTruncatePreview'` (rapid)
- **Phase gate:** `cd /mnt/d/Aura && make quality-full` then `OPENROUTER_API_KEY=… bash scripts/llm_smoke.sh`

---

## Wave 0 Requirements

- [ ] `internal/llm/openai_compat/testdata/text_stop.sse` — golden fixture: `: OPENROUTER PROCESSING` comment + content deltas + trailing usage chunk + `[DONE]`, finish_reason `stop` (Req#1)
- [ ] `internal/llm/openai_compat/testdata/toolcall_multichunk.sse` — one tool call split across ≥3 `data:` chunks, accumulated single line >64KiB (stresses past the bufio.Scanner cap), finish_reason `tool_calls` (Req#2)
- [ ] `internal/llm/openai_compat/testdata/error_429.sse` — 429 + Retry-After error body (Req#4)
- [ ] `internal/llm/openai_compat/testdata/premature_close.sse` — stream cut mid-stream, no `[DONE]` (Req#3 cancel)
- [ ] `internal/llm/openai_compat/testdata/length_truncation.sse` — partial deltas + finish_reason `length` + usage + `[DONE]` (Req#21/Plan-04 truncation path)
- [ ] `internal/llm/openai_compat/{client_test.go,sse_test.go,accumulate_test.go,httperror_test.go}` — stubs for Req#1/#2/#3/#4/#12-wire
- [ ] `internal/llm/{config_test.go,prices_test.go}` — stubs for Req#5/#12-table
- [ ] `internal/agent/tools/{result_test.go,read_tool_output_test.go,current_time_test.go}` — stubs for Req#6/#7/#8/#14-tool (incl. `TestSidecarPathTraversal` for T-03-07)
- [ ] `internal/agent/{llm_agent_test.go,tracing_test.go,prompt_test.go}` — stubs for Req#9/#10/#13/#14-prefix
- [ ] `internal/agent/agenttest/fakeclient.go` — `FakeClient` implementing `llm.Client` (reuse existing mock AGENTS; do NOT duplicate them — Phase 2 D-07)
- [ ] `cmd/aura/{chat_test.go,config_test.go}` — stubs for Req#11
- [ ] `goleak.VerifyTestMain(m)` in `internal/llm/openai_compat/main_test.go`, `internal/agent/tools/main_test.go`, and `internal/agent/main_test.go` (preserve if a Phase-2 harness is already present; add only if absent)
- [ ] 3 usage-chunk variants as inline fixtures for Req#12 (cost-present / cost-absent / unknown-model)

> Co-located test discipline (W5): tasks marked `tdd="true"` author their test stubs + assertions inline in the same task (no separate Wave-0 stub task). The Wave 0 items above are authored *during* execution alongside the production code in the same coupled commit — `wave_0_complete: false` is correct until that execution lands.

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| `aura chat` live streamed prose + non-zero token+USD cost footer against real OpenRouter | CORE-01 (Req#11) / ROADMAP SC#1 | Needs a live `OPENROUTER_API_KEY` + network; explicitly NOT a CI gate (no-skip-as-green compliant — the deterministic tier in Plans 01-04 genuinely exercises every parse/loop/redaction/cost path with no network) | `export OPENROUTER_API_KEY=sk-or-…` then `bash scripts/llm_smoke.sh`; confirm a streamed reply + a footer with a non-zero token count and a USD figure (no `0 tok`, no `$n/a` for the known default model). See the Plan-05 human-verify checkpoint for the full interactive script. |
| Mutation spot-check ≥70% killed on the phase's critical file(s) (`sse.go` / `accumulate.go` / `result.go` / `llm_agent.go`) | CORE-01 | go-mutesting runs on WSL/Git-Bash (avito-tech fork) only; documented per CLAUDE.md Manual-Only table | `cd /d/Aura && go-mutesting ./internal/llm/openai_compat/ ./internal/agent/tools/ ./internal/agent/llm_agent.go` (PASS=killed). **Recorded 2026-05-30 (all ≥70%):** see the per-target table below. |

### Mutation scores (recorded 2026-05-30)

| Target (package / critical file) | Score | Killed / Total | Gate ≥0.70 |
|----------------------------------|-------|----------------|-----------|
| `internal/llm/openai_compat/` (sse.go + accumulate.go) | **0.721** | 62 / 86 | ✅ |
| `internal/agent/tools/` (result.go + read_tool_output.go + current_time.go) | **0.791** | 87 / 110 | ✅ |
| `internal/agent/llm_agent.go` (scoped — full-package run exceeds the timeout; the critical file is the gate target) | **1.000** | 70 / 70 | ✅ |

> Hardened by adding NEW test assertions only — zero production-code changes (commits `80c2c9cb`, `9e350ba4`). `accumulate.go` alone is 0.783; `llm_agent.go` kills all 70 mutants. Accepted equivalent mutants (not test-gamed): `current_time.go` timezone empty/`UTC` guard removals (`LoadLocation("")` and `LoadLocation("UTC")` both yield UTC → byte-identical output), the `validateID` `/`+`\` OR-branch removals on Windows (`os.IsPathSeparator` catches both), the `truncatePreview` `cut>0` loop bound (`cut` never reaches a continuation byte for valid UTF-8), and the `accumulate.go` `a.order++` increment (`firstSeen` is unused in `finalize`, which sorts by index — no observable effect).

### Mutation re-run (live, 2026-06-02 — autonomous UAT close, WSL go-mutesting, `GOFLAGS=-count=1`)

Re-measured PER CRITICAL FILE (the gate's actual unit — "≥70% killed on each critical file"). The earlier whole-package `internal/agent/tools/` number (0.791) is no longer apples-to-apples: the package now also carries `ask_user.go` (Phase 04) and `execute.go` (Phase 05). Per-file isolation is the faithful gate measurement.

| Critical file | Score | Killed / Total | Gate ≥0.70 |
|---------------|-------|----------------|-----------|
| `internal/llm/openai_compat/` (sse.go + accumulate.go) | **0.754** | 43 / 57 | ✅ |
| `result.go` | **0.727** | 16 / 22 | ✅ |
| `current_time.go` | **0.714** | 5 / 7 | ✅ |
| `read_tool_output.go` | **0.857** | 12 / 14 | ✅ |

> `read_tool_output.go` initially measured 0.429: its early-return error guards were each covered by a test, but the tests asserted only `err != nil` — removing a guard fell through to a *different* downstream error, so the mutant survived a bare error-presence check. Hardened by adding NEW exact-error-message assertions only (zero production change) so each guard's specific message (`read_tool_output args:`, `tool_call_id is required`, `missing tool-call context`, `read sidecar`, `no output for tool_call_id`, `is negative`, validateID `..`/`path separator`) is pinned; `TestReadToolOutput_NegativeOffset` now seeds a sidecar so the negative-offset guard is the branch actually reached. → 0.857.
>
> Remaining accepted-equivalent survivors (not test-gamed, no test can distinguish them): `read_tool_output.go` `start > total`→`>=` and `end > total`→`>=` (at the `==total` boundary both branches assign `total` → byte-identical output); `current_time.go` timezone `""`/`"UTC"` guard (both yield UTC).

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 30s
- [x] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
