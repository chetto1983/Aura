---
phase: 03-llm-client-toolresult
verified: 2026-05-30T18:00:00Z
status: human_needed
score: 14/14 must-haves verified
overrides_applied: 0
human_verification:
  - test: "Run `bash scripts/llm_smoke.sh` with OPENROUTER_API_KEY set in .env against live OpenRouter"
    expected: "Clean streamed Italian prose from deepseek/deepseek-v4-flash:exacto, a current_time tool turn, and a cost footer of the form `· N tok (I in / O out) · $X.XXXXXX · Ys` with N > 0 and X > 0; no raw text_response JSON in output; exit 0"
    why_human: "Requires a live paid OPENROUTER_API_KEY and real network call; cannot be automated in CI per no-skip-as-green discipline. The SUMMARY reports this gate PASSED during the phase session (prose + current_time tool turn + footer like `· 1064 tok (1000 in / 64 out) · $0.000125 · 11.2s` observed). A re-run is required to complete formal sign-off if the verifier did not witness it directly."
  - test: "Run `AURA_COVERAGE_MIN=85 bash scripts/coverage_gate.sh` with the Docker stack up (Postgres + Neo4j)"
    expected: "Output ends with `ok` and a percentage >= 85% (SUMMARY reports 90.3% was observed this session)"
    why_human: "coverage_gate.sh requires the container stack (Postgres + Neo4j via `make neo4j-migrate`) to execute db_integration and neo4j_integration tiers. Without the stack the gate exits at 83.1% (the pre-existing infra packages dominate the denominator). The Phase 3 owned surface alone is 89.4%. The stack was confirmed up during the phase close per SUMMARY; formal re-run requires bringing the stack up."
  - test: "Mutation spot-check on `sse.go`, `accumulate.go`, `result.go`, and `llm_agent.go` using go-mutesting on WSL"
    expected: ">= 70% of mutants killed on each critical file per VALIDATION.md Manual-Only table"
    why_human: "go-mutesting runs only on WSL (only fork supporting go1.26); VALIDATION.md lists this as a Manual-Only verification item. Not run during this automated verification pass."
---

# Phase 3: LLM Client + ToolResult Verification Report

**Phase Goal:** LLM Client + ToolResult — OpenAI-compat handrolled client + ToolResult preview+sidecar + SSE streaming.
**Verified:** 2026-05-30T18:00:00Z
**Status:** human_needed
**Re-verification:** No — initial verification

All 14 automated must-haves are VERIFIED by live test execution. Three items require
human gate completion: the live OpenRouter smoke (ROADMAP SC#1), the global coverage
gate with the DB stack up, and the mutation spot-check.

---

## Goal Achievement

### Observable Truths

| # | Truth | Status | Evidence |
|---|-------|--------|---------|
| 1 | Real SSE streaming client parses text deltas + `finish_reason=stop` from a golden fixture | VERIFIED | `TestStream_TextStop` PASS; `text_stop.sse` fixture, assembles `"Ciao, come stai?"` |
| 2 | Tool-call delta accumulation merges ≥3 fragments into one `ToolCall` with JSON-valid args | VERIFIED | `TestAccumulate` PASS; `toolcall_multichunk.sse` (70 877 bytes, >64KiB); bufio.Reader (not Scanner) confirmed by the test assertion `len(tc.Function.Arguments) > 64KiB` |
| 3 | Ctx-cancel mid-stream aborts HTTP request within ~100ms, zero goroutine leak | VERIFIED | `TestStream_CancelMidStream` PASS under `-race` + goleak TestMain; channel closes within 200ms deadline; NumGoroutine returns to baseline |
| 4 | 429 response yields `HTTPError{StatusCode:429, RetryAfterSec}` with exactly 1 request (no retry) | VERIFIED | `TestStream_429NoRetry` PASS; `atomic.Int32` counter asserts server saw exactly 1 call |
| 5 | `llm.Config` load order: built-in default < .env < `~/.aura/llm.json` < `AURA_LLM_*` | VERIFIED | `TestConfigLoadOrder` (two sub-tests) + `TestConfigMissingKey` + `TestConfigMalformedNumericEnv` PASS; 4-tier precedence demonstrated with explicit file write and env override |
| 6 | `Tool.Execute` returns `(ToolResult, error)`; 100KB output leaves ≤2KiB preview+footer in history; sidecar holds full bytes | VERIFIED | `TestNewResult_LargeSpills` PASS; preview 2048+footer, sidecar 100 000 bytes; `TestNewResult_SmallNoSidecar` confirms no file written for ≤cap |
| 7 | `read_tool_output(offset, limit)` returns correct byte slice; unknown id hard-fails | VERIFIED | `TestReadToolOutput_ByteSlice` PASS (slice 50000-50100 of 100KB + footer); `TestReadToolOutput_UnknownID` PASS (error names the id) |
| 8 | Sidecar lives at `$AURA_RUN_DIR/conversations/<session_id>/<tool_call_id>.result` | VERIFIED | `TestSidecarLayout` PASS; exact path assertion + lazy dir creation |
| 9 | `LlmAgent.Run` yields ordered Events (tool_call → tool_result → final), terminates, race-clean | VERIFIED | `TestLlmAgent_EventOrder` PASS; `TestLlmAgent_SequentialToolCalls` PASS (two RoleTool results in c1/c2 order); goleak TestMain present in `internal/agent/main_test.go` |
| 10 | Budget trips: step/wallclock/dedup each emit a terminal Event with correct reason; steps ≤ cap | VERIFIED | `TestLlmAgent_StepCap_Trips` (reason=`max_steps`, fc.CallCount()<=3); `TestLlmAgent_WallclockCap_Trips` (reason=`wallclock`, 0 calls); `TestLlmAgent_DedupWindow_Trips` (reason=`dedup`) all PASS |
| 11 | `aura chat` REPL: 2 turns share session_id; second turn sees first in history; missing key fails cleanly | VERIFIED (automated) | `TestChat_TwoTurns_SharedSession` PASS (shared session, history seen, clean prose, non-zero cost footer); `TestChat_MissingKey` PASS; live gate is human item 1 |
| 12 | USD from OpenRouter `usage.cost` when present, else price table; unknown model → `n/a` never `$0` | VERIFIED | `TestCost` (3 sub-tests) PASS; `TestUsage` confirms provider cost `0.000123` threaded through; `TestCost/unknown_model_reports_na_not_zero` PASS |
| 13 | Exactly 1 `llm.request` OTel span per call with all 6 attributes + stable span_id; `req.Messages` byte-identical pre/post | VERIFIED | `TestSpan_LLMRequest` PASS (all 6 attrs: llm.model, llm.provider, llm.prompt_tokens, llm.completion_tokens, llm.cache_hit_tokens, aura.request_id; D-28: no api_key attr); `TestMessagesImmutable` PASS (client_test.go + agent_test.go); `TestLlmAgent_SecretRedaction` PASS |
| 14 | `current_time` returns RFC-3339 UTC; IANA tz returns correct offset; `messages[0]` carries no timestamp and is byte-stable | VERIFIED | `TestCurrentTime_DefaultUTC/_IANAOffset/_InvalidTZ` PASS; `TestPrompt_NoTimestamp` PASS (no date/clock-shaped substring in SystemPrompt); `TestPrompt_ByteStable` PASS (two reads byte-identical); `TestPrefixStable` PASS (messages[0] identical across turns in FC.Requests[0] vs [1]) |

**Score:** 14/14 truths verified (automated). 3 items require human gate completion.

---

### Required Artifacts

| Artifact | Expected | Status | Details |
|----------|----------|--------|---------|
| `internal/llm/config.go` | Config struct + Load 4-tier precedence | VERIFIED | 254 LOC; `func Load`, `func applyFileConfig`, `func applyEnvOverrides`, `ErrMissingAPIKey` sentinel |
| `internal/llm/prices.go` | Price table seeded with deepseek-v4-flash; `CostUSD` | VERIFIED | 42 LOC; `defaultPrices()` has `deepseek/deepseek-v4-flash:exacto`; `CostUSD` returns `n/a` for unknown model |
| `internal/llm/openai_compat/sse.go` | SSE parser with bufio.Reader, comment-skip, [DONE] sentinel | VERIFIED | 136 LOC; `parseSSE` with explicit comment: "NEVER bufio.Scanner — Pitfall #1" |
| `internal/llm/openai_compat/accumulate.go` | Tool-call delta accumulator by index | VERIFIED | 100 LOC; `accumulator.add` / `finalize`; args concatenate across fragments |
| `internal/llm/openai_compat/client.go` | `Client.Stream` handrolled; no SDK; ctx-cancel; no retry | VERIFIED | 146 LOC; `http.NewRequestWithContext`; `DisableKeepAlives:true` for goleak cleanliness |
| `internal/llm/openai_compat/httperror.go` | `HTTPError{StatusCode,RetryAfterSec,Body}` | VERIFIED | 54 LOC; Retry-After parsed on 429; body capped at 64KiB |
| `internal/agent/tools/spec.go` | `ToolResult{Preview,FullPath,Bytes,Truncated}` + `Tool.Execute (ToolResult, error)` | VERIFIED | Interface migrated; both `text_response` and `tool_search` adapted in one coupled commit |
| `internal/agent/tools/result.go` | `NewResult` ctx-injected spillover helper + path-traversal validation | VERIFIED | 142 LOC; `validateID` rejects `..` and path separators; D-29 degrade-clean write failure |
| `internal/agent/tools/read_tool_output.go` | Byte-ranged paging; `Deferred:false`; unknown-id hard-fail | VERIFIED | 95 LOC; schema text says "BYTES"; negative offset rejected; offset clamp to file size |
| `internal/agent/tools/current_time.go` | RFC-3339 UTC + IANA tz; `Deferred:false`; clock never in `messages[0]` | VERIFIED | 58 LOC; wall clock only in tool path; `Deferred:false` asserted by `TestCurrentTime_Deferred` |
| `internal/agent/llm_agent.go` | `LlmAgent` implements `Agent`; budget-gated loop; dispatch; consume() stop-propagation fixed | VERIFIED | 320 LOC; `var _ Agent = (*LlmAgent)(nil)` compile guard; consume() returns `stopped` flag; iter.Seq2 contract enforced |
| `internal/agent/tracing.go` | `NewTracerProvider` real exporter; per-call `llm.request` span; silent-drop OTLP | VERIFIED | 138 LOC; `otel.SetErrorHandler(no-op)` on otlp path; `newTracerProvider`/`NewTracerProvider` exported |
| `internal/agent/prompt.go` | Byte-stable `SystemPrompt` constant; "Always respond in Italian" directive; no timestamp | VERIFIED | 25 LOC; `systemMessage()` returns the constant, reads no clock |
| `cmd/aura/chat.go` | `aura chat` REPL; two-stage Ctrl+C; OTel flush on exit; missing-key clean error | VERIFIED | 230 LOC; `signal.NotifyContext`; `tp.Shutdown` deferred; `errors.Is(err, llm.ErrMissingAPIKey)` |
| `cmd/aura/chat_render.go` | Token-by-token prose; cost footer; tool-result skip guard | VERIFIED | 176 LOC; `isToolResultPreview` guards; `costFooter` calls `llm.CostUSD`; regression `TestRenderTurn_ToolResultNotRenderedAsProse` |
| `cmd/aura/config.go` | `aura config show/get/set` over `~/.aura/llm.json`; refuses `llm.api_key` set | VERIFIED | 270 LOC; T-03-01 redaction in `show`; `set` refuses `llm.api_key` |
| `scripts/llm_smoke.sh` | Manual real-OpenRouter gate; not in CI/Makefile | VERIFIED | 95 LOC; explicitly NOT in Makefile; exits 0 when key unset (documented skip) |
| `internal/llm/openai_compat/testdata/*.sse` | 5 golden fixtures for Req#1/#2/#3/#4 | VERIFIED | `text_stop.sse`, `toolcall_multichunk.sse` (70 877 bytes, >64KiB), `error_429.sse`, `premature_close.sse`, `length_truncation.sse` |

### Key Link Verification

| From | To | Via | Status | Details |
|------|----|-----|--------|---------|
| `internal/config/config.go` | `internal/llm.Load` | `config.Load` calls `llm.Load()` and assigns `Config.LLM` | VERIFIED | Confirmed in `config.go`; `AURA_OTEL_*` fields also present |
| `cmd/aura/chat.go` | `internal/llm/openai_compat.New` | `openai_compat.New(cfg.LLM)` in `runChat` | VERIFIED | Line 87 of `chat.go` |
| `internal/agent/llm_agent.go` | `internal/agent/tools.WithToolCallContext` | `tools.WithToolCallContext(ctx, a.sessionID, call.ID, a.runDir, a.previewCap)` in `runTool` | VERIFIED | Line 241 of `llm_agent.go` |
| `cmd/aura/main.go` | `tools.ReadToolOutput` + `tools.CurrentTime` | `buildRegistry()` registers both | VERIFIED | Lines 60-61 of `main.go` |
| `internal/agent/llm_agent.go` | `tracing.startLLMSpan` + `setSpanAttrs` | Called in `Run` loop before/after `client.Stream` | VERIFIED | Lines 129, 147 of `llm_agent.go` |
| `cmd/aura/chat.go` | `agent.NewTracerProvider` | `agent.NewTracerProvider(ctx, cfg.OtelExporter, cfg.OtelEndpoint)` in `runChat` | VERIFIED | Line 64 of `chat.go` |

### Data-Flow Trace (Level 4)

| Artifact | Data Variable | Source | Produces Real Data | Status |
|----------|---------------|--------|-------------------|--------|
| `chat.go:costFooter` | `usage llm.Usage` | `renderTurn` reads `StateDelta` from final Event; LlmAgent stamps usage from SSE `Usage` chunk | OpenRouter wire usage chunk → LlmAgent.consume → Event.StateDelta → `usageFromStateDelta` | FLOWING |
| `chat_render.go:renderTurn` | `resp.Content` | `LlmAgent.Run` → chunk Events → `agent.chunkEvent` | FakeClient in tests; real `openai_compat.Client.Stream` in production | FLOWING |
| `result.go:NewResult` | sidecar file | `os.WriteFile` to `filepath.Join(runDir, "conversations", ...)` | `t.TempDir()` in tests; `config.RunDir` in production | FLOWING |

### Behavioral Spot-Checks

| Behavior | Command | Result | Status |
|----------|---------|--------|--------|
| Req#1 SSE text-stop | `go test ./internal/llm/openai_compat/ -run TestStream_TextStop` | PASS | PASS |
| Req#2 tool-call accumulation | `go test ./internal/llm/openai_compat/ -run TestAccumulate` | PASS | PASS |
| Req#3 ctx-cancel ≤100ms, zero leak | `go test -race ./internal/llm/openai_compat/ -run TestStream_CancelMidStream` | PASS (0.04s) | PASS |
| Req#4 429 no-retry | `go test ./internal/llm/openai_compat/ -run TestStream_429NoRetry` | PASS | PASS |
| Req#5 load-order | `go test ./internal/llm/ -run TestConfigLoadOrder` | PASS | PASS |
| Req#7 unknown id hard-fail | `go test ./internal/agent/tools/ -run TestReadToolOutput_UnknownID` | PASS | PASS |
| Req#10 budget trips | `go test ./internal/agent/ -run 'TestLlmAgent_(StepCap|WallclockCap|DedupWindow)_Trips'` | PASS (3 tests) | PASS |
| Req#11 two-turn REPL | `go test ./cmd/aura/ -run TestChat_TwoTurns_SharedSession` | PASS | PASS |
| Req#12 cost table | `go test ./internal/llm/ -run TestCost` | PASS (3 sub-tests) | PASS |
| Req#13 OTel span attrs | `go test ./internal/agent/ -run TestSpan_LLMRequest` | PASS | PASS |
| Req#13 messages immutable | `go test ./internal/agent/ -run TestMessagesImmutable` | PASS | PASS |
| Req#14 prefix stable | `go test ./internal/agent/ -run TestPrefixStable` | PASS | PASS |
| D-28 secret redaction | `go test ./internal/agent/ -run TestLlmAgent_SecretRedaction` | PASS | PASS |
| Full suite with `-race` | `go test -race ./internal/llm/... ./internal/agent/... ./cmd/aura/...` | all ok | PASS |

### Probe Execution

No conventional `scripts/*/tests/probe-*.sh` probes in Phase 3. The functional equivalent is `scripts/llm_smoke.sh` — a manual live gate per VALIDATION.md and SUMMARY (not a CI probe).

| Probe | Command | Result | Status |
|-------|---------|--------|--------|
| `scripts/llm_smoke.sh` | `bash scripts/llm_smoke.sh` (requires live OPENROUTER_API_KEY) | PASS per SUMMARY (live run this session: clean prose + `· 1064 tok (1000 in / 64 out) · $0.000125 · 11.2s`); re-run not performed in this automated verification | HUMAN GATE |

### Requirements Coverage

| Requirement | Source Plan | Description | Status | Evidence |
|-------------|------------|-------------|--------|---------|
| CORE-01 | Plans 01-05 (all 5) | LLM client OpenAI-compat, ToolResult preview+sidecar, SSE streaming + ctx-cancel, OTel span per LLM call | SATISFIED | All 14 SPEC Req# acceptance criteria verified; REQUIREMENTS.md row `[x] CORE-01` at Phase 3 Complete |

**CORE-01 traceability note:** All 5 Phase-3 plans declare `requirements: [CORE-01]`. Plan 03 did not prematurely claim CORE-01 complete — the SUMMARY for Plan 03 lists only `Req#6/#7/#8/#14 + T-03-07/T-03-09`. CORE-01 as a whole-phase requirement is correctly marked complete only after Plan 05 closes (REQUIREMENTS.md checkbox updated at phase close). No traceability inconsistency found.

### Anti-Patterns Found

| File | Line | Pattern | Severity | Impact |
|------|------|---------|----------|--------|
| `cmd/aura/main.go` | 45 | `TODO: implemented by the agent-loop and CLI slices` | Info | `shell` and `serve` subcommands are out-of-scope stubs (Phase 12/future). Not Phase 3 work. No Phase 3 acceptance criteria reference these subcommands. Not a debt marker on Phase 3 deliverables. |

No `TBD`, `FIXME`, or `XXX` markers found in any Phase 3 file. No unreferenced debt markers.

### Coverage Assessment

| Surface | Measured (no DB stack) | Measured (Phase 3 owned) | Floor | Status |
|---------|----------------------|------------------------|-------|--------|
| `internal/llm` | 100% | 100% | 85% | PASS |
| `internal/llm/openai_compat` | 97.6% | 97.6% | 85% | PASS |
| `internal/agent` (Phase 3 files) | 96.4% | 96.4% | 85% | PASS |
| `internal/agent/tools` | 88.9% | 88.9% | 85% | PASS |
| Phase 3 owned surface total | 89.4% | 89.4% | 85% | PASS |
| Global `coverage_gate.sh` (needs stack) | 83.1% (no stack) | 90.3% (stack up, per SUMMARY) | 85% | HUMAN GATE |

`coverage_gate.sh` at 83.1% without the DB stack is a measurement artifact: the db/knowledge packages (not Phase 3 work) dominate the denominator and their integration tests skip when the stack is not up. The Phase 3 owned surface is 89.4% without the stack. With the stack, SUMMARY reports 90.3% global. The human gate captures the formal re-run requirement.

### Human Verification Required

#### 1. Live ROADMAP SC#1 Acceptance: `aura chat` against real OpenRouter

**Test:** With `OPENROUTER_API_KEY` set in `.env`, run `bash scripts/llm_smoke.sh` from the repo root.

**Expected:**
- Script exits 0
- A `›` REPL prompt appears, followed by clean Italian prose from `deepseek/deepseek-v4-flash:exacto`
- A `current_time` tool turn fires (visible as a dim `· current_time` line)
- A cost footer of the form `· N tok (I in / O out) · $X.XXXXXX · Ys` with N > 0 and X > 0 (not `n/a`) appears for each turn
- No raw `{"text":...}` JSON leaked into the prose
- Script prints `==> llm_smoke: PASS (streamed prose + non-zero token+USD footer, clean prose)`

**Why human:** Requires a live paid OPENROUTER_API_KEY and real network. Cannot be automated in CI (no-skip-as-green). Per SUMMARY, this gate was executed and passed this session with output including `· 1064 tok (1000 in / 64 out) · $0.000125 · 11.2s`. Accept the SUMMARY evidence or re-run at your discretion.

#### 2. Global Coverage Gate with Stack Up

**Test:** With Docker stack running (`make neo4j-migrate` complete, `.env` with `POSTGRES_PASSWORD`/`AURA_DB_URL`), run `AURA_COVERAGE_MIN=85 bash scripts/coverage_gate.sh`.

**Expected:** Output ends with `ok` and percentage `>= 85%`. SUMMARY reports 90.3%.

**Why human:** Requires Postgres + Neo4j container stack. Without the stack the gate exits at 83.1% (pre-existing infra packages dominate denominator). Phase 3 owned surface alone is 89.4% (no stack required).

#### 3. Mutation Spot-Check (WSL-only)

**Test:** On WSL, run:
```bash
cd /mnt/d/Aura
export PATH="$HOME/.local/bin:$HOME/go/bin:$PATH"
go-mutesting ./internal/llm/openai_compat/ ./internal/agent/tools/
```

**Expected:** >= 70% of mutants killed on `sse.go`, `accumulate.go`, `result.go` per VALIDATION.md Manual-Only table.

**Why human:** `go-mutesting` runs only on WSL (the only fork supporting go1.26). CLAUDE.md mandates this as a manual gate. Not performed during this automated verification pass.

### Gaps Summary

No automated gaps. All 14 SPEC requirement acceptance criteria are backed by real, executing, non-skipped tests that PASS. The three human items are manual gates that were either executed during the phase close session (smoke, coverage with stack) or are WSL-only (mutation). The `status: human_needed` reflects that these gates have not been formally re-verified in this automated pass.

**Root causes of human items:**
1. Live LLM gate is intentionally manual (paid API, non-deterministic) — design decision, not a gap.
2. Coverage gate requires the container stack — infrastructure dependency, not a code gap.
3. Mutation testing requires WSL — toolchain constraint, not a code gap.

---

_Verified: 2026-05-30T18:00:00Z_
_Verifier: Claude (gsd-verifier)_
