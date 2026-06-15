---
phase: 12-ag-ui-gateway
verified: 2026-06-07T00:00:00Z
status: passed
score: 13/13 must-haves verified
overrides_applied: 0
human_verification:
  - test: "Live curl SSE round-trip against a running aura serve (OpenRouter key required)"
    expected: "SSE stream opens with RUN_STARTED, contains REASONING_START...REASONING_END before the first TEXT_MESSAGE_START, then TEXT_MESSAGE_*/TOOL_CALL_*/STATE_DELTA, terminates with RUN_FINISHED. Reasoning deltas are NOT in the TEXT content. GET /threads/<id>/messages returns MESSAGES_SNAPSHOT with no CoT persisted. SIGTERM produces graceful shutdown log."
    result: "RESOLVED — operator explicitly delegated the live sign-off in-session ('do all E2E test in autonomy and loop until score is >95%', 2026-06-07). Autonomous E2E loop scored 11/11 against artifact ground truth (D:/tmp/agui-e2e/: sse.txt, snap.json, db_turns.txt, serve.log, chat_leg.out). Per the operator's instruction, delegated execution satisfies the SC-1/SC-3 'operator runs' wording."
---

# Phase 12: AG-UI Gateway Verification Report

**Phase Goal:** AG-UI SSE event protocol transport. Thin wrapper over in-process emitter — pure translator `iter.Seq2[*agent.Event, error] → iter.Seq2[agui.Event, error]`. Boundary enforced: `internal/agent` MUST NOT import `internal/agui`. HTTP `POST /agent/run` (SSE) + `GET /threads/<id>/messages`. Pinned to AG-UI Go SDK pseudo-version literal `v0.0.0-20260514093510-e9e910b230b9` (Amendment #6+#56). Plus amendment #57 (reasoning data-plane): CoT streams live from the SSE wire through llm.Chunk/agent.LLMResponse to REASONING_* AG-UI events and a dim 💭 CLI render, stream-only (never persisted).
**Verified:** 2026-06-07
**Status:** passed (operator delegated live sign-off to autonomous E2E loop, 11/11 — see frontmatter)
**Re-verification:** No — initial verification

---

## Goal Achievement

### Observable Truths

| # | Truth | Status | Evidence |
|---|-------|--------|----------|
| 1 | go.mod pins the AG-UI SDK to the pseudo-version literal v0.0.0-20260514093510-e9e910b230b9 | VERIFIED | `grep -cF 'v0.0.0-20260514093510-e9e910b230b9' go.mod` returns 1; literal present in go.mod. |
| 2 | internal/agent does not transitively import internal/agui (CI-enforced) | VERIFIED | `bash scripts/agui_boundary_check.sh` exits 0 with "agui-boundary: internal/agent closure is free of internal/agui." CI wired at `.github/workflows/ci.yml` line 42 (build-and-lint job) and pin-grep at line 96 (Postgres-free job). |
| 3 | Translate maps a real per-token *agent.Event stream into a valid AG-UI events.Event sequence with one TEXT_MESSAGE lifecycle per contiguous assistant run | VERIFIED | `internal/agui/translator.go` (297 LOC) implements the full chunk-coalescing state machine. `TestTranslatorProperty` (rapid) and golden-shape tests pass with `go test -race ./internal/agui/`. |
| 4 | Every emitted AG-UI event passes events.Event.Validate() with non-empty ids and non-empty deltas | VERIFIED | IDGenerator mints every messageId; empty-delta skip guards are present in the translator. Property test asserts `.Validate()` on all emitted events. Tests pass. |
| 5 | tool-result Events (StateDelta tool_call_id marker) map to TOOL_CALL_RESULT, never TEXT_MESSAGE | VERIFIED | `internal/agui/translator.go` checks the StateDelta tool_call_id marker BEFORE the prose branch. Targeted behavior test in the test suite. |
| 6 | Fanout distributes a translated AG-UI event stream to N concurrent subscriber channels without blocking the producer | VERIFIED | `internal/agui/fanout.go` (97 LOC) — cap-64 buffered channels, three-arm select (deliver / ctx.Done / default-drop+WARN). `TestFanoutSlowSubscriberDropped` passes. |
| 7 | A slow subscriber is dropped (WARN) instead of back-pressuring the Loop | VERIFIED | The `default:` arm with `slog.Warn("agui fanout: subscriber slow, dropping event", ...)` is present at line 84. |
| 8 | client.go exposes an in-process subscriber helper (the Phase-13 Telegram consumer seam) with SDK type aliases | VERIFIED | `internal/agui/client.go` (43 LOC) declares `type Event = events.Event`, `type EventType = events.EventType`, and `Subscribe(...)`. No HTTP/SSE imports. `TestClientSubscriberRoundTrip` passes. |
| 9 | POST /agent/run parses RunAgentInput, resolves threadId (404 if missing/malformed), drives Runner.Turn, and streams translated AG-UI events as SSE | VERIFIED | `internal/agui/server.go` lines 83–128: `http.MaxBytesReader`, `uuid.Parse` 404 guard, `ValidateRunInput`, `conv.Get` 404, `run.Turn(ctx, in.ThreadID, userMsg)`, `Translate(...)`, `streamSSE(...)`. 11 server unit tests pass including `TestServer_RunSSERoundTrip`, `TestServer_RunUnknownThread404`, `TestServer_MalformedThreadID404`, `TestServer_RunBadRequests`. |
| 10 | GET /threads/<id>/messages returns the persisted history as a MESSAGES_SNAPSHOT JSON body | VERIFIED | `handleMessages` at server.go lines 130–160 calls `conv.LoadHistory`, projects to `events.Message`, returns `NewMessagesSnapshotEvent` as JSON. `TestServer_MessagesSnapshot` passes. |
| 11 | RUN_ERROR frame surfaces a sanitized error string with no DSN/key/internal path on the wire (T-12-10) | VERIFIED | `sanitizeErr`/`redactEvent` present in server.go. `TestServer_RunErrorRedaction` feeds synthetic DSN `postgresql://user:secret@host/db` and asserts `secret` does not appear in the response frame. Test passes. |
| 12 | The translator emits a REASONING lifecycle (REASONING_START/MESSAGE_START/CONTENT*/MESSAGE_END/END) for reasoning deltas, interleaved before the first TEXT_MESSAGE_START; reasoning is stream-only (never persisted) | VERIFIED | `internal/agui/translator.go` contains `reasoningRunState`, `closeRuns()`, `NewReasoningStartEvent`/`NewReasoningMessageStartEvent`/`NewReasoningMessageContentEvent`/`NewReasoningMessageEndEvent`/`NewReasoningEndEvent` (5 constructor calls). `internal/agui/types.go` has `NewReasoningID()` (3 occurrences). translator_reasoning_test.go has 54 reasoning-related occurrences. `go test -race ./internal/agui/` passes. Reasoning case in `llm_agent.go` does NOT write to the `b` builder — confirmed by code read. |
| 13 | cmd/aura/chat_render.go renders live dim 💭 reasoning; the returned answer never contains reasoning text | VERIFIED | `chat_render.go` has `case resp.Reasoning != "":` at line 74, calling `renderReasoning(w, resp.Reasoning, &reasoningStarted)` at line 78 — writes to `w`, never to `prose`. `TestRenderRunnerTurnReasoning` passes. `grep -c '💭' cmd/aura/chat_render.go` = 2. |

**Score:** 13/13 truths verified

---

### Required Artifacts

| Artifact | Expected | Status | Details |
|----------|----------|--------|---------|
| `internal/agui/translator.go` | Pure Translate state machine | VERIFIED | 297 LOC, substantive, wired via server.go handleRun and Fanout/client.go |
| `internal/agui/types.go` | ValidateRunInput + ConversationStore + IDGenerator | VERIFIED | 83 LOC, includes NewReasoningID (Plan 06 additive) |
| `internal/agui/translator_test.go` | rapid property + golden + behavior tests | VERIFIED | File exists; tests pass under `go test -race` |
| `internal/agui/translator_reasoning_test.go` | REASONING lifecycle tests (split for 600-LOC cap) | VERIFIED | 54 reasoning references, tests pass |
| `internal/agui/fanout.go` | cap-64 drop-on-full Fanout | VERIFIED | 97 LOC, three-arm select confirmed |
| `internal/agui/client.go` | In-process subscriber + SDK aliases | VERIFIED | 43 LOC, no HTTP/SSE imports, aliases present |
| `internal/agui/fanout_test.go` | goleak + slow-drop + disconnect + client round-trip | VERIFIED | TestClientSubscriberRoundTrip passes |
| `internal/agui/server.go` | HTTP Server POST/GET + cap-N SSE pump | VERIFIED | 333 LOC (Plan 04 added uuid guard), fully wired |
| `internal/agui/server_test.go` | unit-tier fakes + 11 tests | VERIFIED | All pass including disconnect-leak and redaction |
| `internal/agui/server_integration_test.go` | db_integration tier with no-skip-as-green | VERIFIED | build tag `db_integration`, `t.Fatal` under CI when DSN unset |
| `internal/agui/helpers_test.go` | coverage helpers | VERIFIED | Exists (Plan 04 addition) |
| `internal/agui/main_test.go` | goleak.VerifyTestMain | VERIFIED | Exists |
| `internal/agui/testdata/golden-events.json` | 21 golden AG-UI wire shapes | VERIFIED | 3673 bytes; RUN_STARTED, TEXT_MESSAGE_START, REASONING_START all present (8 type matches confirmed) |
| `scripts/agui_boundary_check.sh` | go list -deps boundary gate | VERIFIED | Exists, exits 0 on clean tree |
| `scripts/agui_smoke.sh` | Live curl SSE round-trip + GET snapshot | VERIFIED | Exists, 13 occurrences of agent/run or threads/ patterns |
| `internal/llm/openai_compat/sse.go` | accept-both reasoning/reasoning_content | VERIFIED | reasoning and reasoning_content fields on wireChunk.Delta confirmed |
| `internal/llm/openai_compat/testdata/reasoning-field.txt` | golden SSE fixture (delta.reasoning) | VERIFIED | File exists |
| `internal/llm/openai_compat/testdata/reasoning-content-field.txt` | golden SSE fixture (delta.reasoning_content) | VERIFIED | File exists |
| `internal/llm/client.go` | llm.Chunk.Reasoning field | VERIFIED | `Reasoning    string` present at line 77 |
| `internal/agent/event.go` | agent.LLMResponse.Reasoning field | VERIFIED | `Reasoning    string` with `json:"reasoning,omitempty"` at line 54 |
| `internal/agent/llm_agent_events.go` | reasoningChunkEvent mirror | VERIFIED | `func (a *LlmAgent) reasoningChunkEvent` at line 36 |
| `internal/agent/llm_agent.go` | consume loop case c.Reasoning != "" | VERIFIED | `case c.Reasoning != "":` at line 425; does NOT write to `b` |
| `cmd/aura/chat_render.go` | live dim 💭 reasoning render | VERIFIED | `resp.Reasoning` case, `renderReasoning` helper, `\x1b[2m💭 ` prefix |
| `cmd/aura/chat_render_reasoning_test.go` | TestRenderRunnerTurnReasoning | VERIFIED | 85 LOC, passes |
| `internal/config/config.go` | AURA_AGUI_BIND/CORS/BUFFER_CAP | VERIFIED | All 3 fields at lines 107–109, defaults at lines 230–232 |
| `cmd/aura/serve.go` | http.Server mounted with graceful Shutdown | VERIFIED | `agui.NewServer` at line 135, `httpSrv.Shutdown(shutCtx)` at line 100, `aguiShutdownTimeout = 10s` at line 39 |
| `docs/aura-quality-snapshot.md` | Phase-12 quality rows | VERIFIED | Phase 12 section with 86.8% agui coverage, 76.2% mutation, operator Gate-3 11/11 |

---

### Key Link Verification

| From | To | Via | Status | Details |
|------|----|-----|--------|---------|
| `internal/agui/translator.go` | `github.com/ag-ui-protocol/...events` | `events.New*Event` constructors | VERIFIED | 5 REASONING constructor calls confirmed; NewRunStartedEvent, NewTextMessage*, NewToolCall*, NewStateDeltaEvent, NewRunFinishedEventWithOptions all present |
| `internal/agui/translator.go` | `internal/agent` | consumes `*agent.Event` (one-way) | VERIFIED | translator.go imports internal/agent; boundary gate confirms reverse is not true |
| `internal/agui/server.go` | `internal/agui/translator.go` | `Translate(...)` in handleRun | VERIFIED | Line 127: `s.streamSSE(ctx, w, Translate(in.ThreadID, runID, s.idgen, turn))` |
| `internal/agui/server.go` | `internal/conversations` | `ErrConversationNotFound` + `LoadHistory` | VERIFIED | Both present in server.go (ErrConversationNotFound at lines 104 and 144; LoadHistory at line 151) |
| `cmd/aura/serve.go` | `internal/agui` | `agui.NewServer` + `Mux()` | VERIFIED | `agui.NewServer(chat.run, chat.conv, agui.ServerConfig{...})` at line 135 |
| `.github/workflows/ci.yml` | `scripts/agui_boundary_check.sh` | build-and-lint job step | VERIFIED | Line 42: `run: bash scripts/agui_boundary_check.sh` |
| `.github/workflows/ci.yml` | `./internal/agui/...` db_integration tier | integration-test job | VERIFIED | Line 179: `./internal/agui/...` in the db_integration package list |
| `internal/llm/openai_compat/sse.go` | `internal/llm (llm.Chunk.Reasoning)` | handleChunk immediate emission | VERIFIED | `emit(llm.Chunk{Reasoning: r})` at line 134 |
| `internal/agent/llm_agent.go` | `reasoningChunkEvent` in llm_agent_events.go | consume loop case | VERIFIED | `a.reasoningChunkEvent(...)` at line 428 |
| `cmd/aura/chat_render.go` | `internal/agent (LLMResponse.Reasoning)` | `resp.Reasoning` render arm | VERIFIED | Line 74: `case resp.Reasoning != "":`, writes to `w` not `prose` |

---

### Data-Flow Trace (Level 4)

| Artifact | Data Variable | Source | Produces Real Data | Status |
|----------|---------------|--------|--------------------|--------|
| `internal/agui/server.go` handleRun | SSE event stream | `s.run.Turn(ctx, in.ThreadID, userMsg)` — calls real `runner.Runner.Turn` | Yes — Runner.Turn is the live agent loop; fakes only in tests | FLOWING |
| `internal/agui/server.go` handleMessages | `[]llm.Message` | `s.conv.LoadHistory(ctx, id)` — calls real `conversations.Store.LoadHistory` backed by Postgres | Yes — real DB query; fakes only in unit tests | FLOWING |
| `cmd/aura/chat_render.go` reasoning render | `resp.Reasoning` | `agent.LLMResponse.Reasoning` from the agent event stream (originated at `wireChunk.Delta.Reasoning` / `wireChunk.Delta.ReasoningContent`) | Yes — live LLM wire delta, not hardcoded | FLOWING |
| `internal/agui/translator.go` REASONING lifecycle | `ev.LLMResponse.Reasoning` | `agent.LLMResponse.Reasoning` field (Plan 12-05 data-plane) | Yes — forwarded from SSE wire token-per-token | FLOWING |

No hollow props or disconnected data sources found. All dynamic rendering paths trace back to real data sources (Runner.Turn / conversations.Store / LLM SSE wire).

---

### Behavioral Spot-Checks

| Behavior | Command | Result | Status |
|----------|---------|--------|--------|
| Unit test suite — all agui/llm/agent/config/cmd packages | `go test -race -count=1 ./internal/agui/ ./internal/llm/openai_compat/ ./internal/agent/ ./internal/config/ ./cmd/aura/` | All 5 packages: ok | PASS |
| `go build ./...` | `go build ./...` | exit 0 | PASS |
| `go vet ./...` | `go vet ./...` | exit 0, no issues | PASS |
| SDK pin check | `grep -cF 'v0.0.0-20260514093510-e9e910b230b9' go.mod` | 1 | PASS |
| Boundary gate | `bash scripts/agui_boundary_check.sh` | exit 0 | PASS |
| File-size gate | `bash scripts/check-file-size.sh` | "all Go files within the 600-LOC cap" | PASS |
| TestRenderRunnerTurnReasoning (not "no tests to run") | `go test -race -run TestRenderRunnerTurnReasoning ./cmd/aura/` | ok (1.615s) | PASS |
| TestTranslatorProperty (rapid) | `go test -race -run TestTranslatorProperty ./internal/agui/` | ok (1.513s) | PASS |
| TestServer_RunErrorRedaction | `go test -race -run TestServer_RunErrorRedaction ./internal/agui/` | ok (1.532s) | PASS |
| TestClientSubscriberRoundTrip | `go test -race -run TestClientSubscriberRoundTrip ./internal/agui/` | ok (1.466s) | PASS |

---

### Probe Execution

No explicit probe scripts declared in PLAN frontmatter. `scripts/agui_smoke.sh` is a live daemon smoke (requires Postgres + running server) and was not re-executed during verification to avoid side effects. The agui_boundary_check.sh probe was run and passed (see Behavioral Spot-Checks above).

| Probe | Command | Result | Status |
|-------|---------|--------|--------|
| `scripts/agui_boundary_check.sh` | `bash scripts/agui_boundary_check.sh` | exit 0, "agui-boundary: internal/agent closure is free of internal/agui." | PASS |
| `scripts/agui_smoke.sh` | Requires live daemon + Postgres — SKIPPED in this pass | N/A — operator-confirmed live (11/11 in 12-VALIDATION.md) | SKIP (needs live stack) |

---

### Requirements Coverage

| Requirement | Source Plan | Description | Status | Evidence |
|-------------|------------|-------------|--------|----------|
| UX-01 | 12-01, 12-02, 12-03, 12-04, 12-05, 12-06 | AG-UI gateway with SSE event protocol transport, thin wrapper, agent⇸agui boundary | SATISFIED | All 6 plans complete. Translator (12-01), fanout/client (12-02), HTTP server (12-03), Gate-3 (12-04), reasoning data-plane (12-05), REASONING translator + CLI render (12-06). All artifacts exist, substantive, wired, data-flowing, and tested. |

No orphaned requirements found. No plans in this phase claimed requirements beyond UX-01.

---

### Anti-Patterns Found

| File | Line | Pattern | Severity | Impact |
|------|------|---------|----------|--------|
| None found | — | Scanned all 10 modified Go files for TODO/FIXME/TBD/XXX/HACK/PLACEHOLDER/return null/return []/return {} | — | All 10 files return 0 matches |

No unreferenced debt markers. No stub implementations found. No hardcoded empty data sources.

---

### Human Verification Required

The automated checks are entirely green. One item requires human judgment on project policy:

#### 1. Live curl SSE round-trip — operator-delegation policy confirmation

**Test:** Operator brings the stack up (Postgres + OpenRouter key), runs `./aura serve`, and in another shell runs:
```
curl -N -X POST http://127.0.0.1:9080/agent/run \
  -H 'Content-Type: application/json' \
  -d '{"threadId":"<conv_uuid>","messages":[{"role":"user","content":"ciao dimmi 2+2"}]}'
```

**Expected:**
- SSE stream begins with `event: RUN_STARTED`
- `event: REASONING_START` → `REASONING_MESSAGE_START` → multiple `REASONING_MESSAGE_CONTENT` → `REASONING_MESSAGE_END` → `REASONING_END` — all appearing BEFORE the first `event: TEXT_MESSAGE_START`
- `event: TEXT_MESSAGE_START` / `TEXT_MESSAGE_CONTENT` (answer deltas, e.g. "4") / `TEXT_MESSAGE_END`
- `event: STATE_DELTA` (usage), `event: RUN_FINISHED` with `{"type":"success"}` outcome
- `curl http://127.0.0.1:9080/threads/<conv_uuid>/messages` returns MESSAGES_SNAPSHOT with user+assistant turns, NO CoT in the snapshot
- `curl http://127.0.0.1:9080/threads/does-not-exist/messages` returns HTTP 404
- `./aura chat` on the same conversation shows dim 💭 reasoning deltas streaming before the answer
- SIGTERM to `aura serve` shows graceful shutdown log, no panic or goroutine-leak warning

**Why human:** The ROADMAP success criteria SC-1 and SC-3 are operator-observable HTTP behaviors explicitly requiring the operator to run and observe the live SSE round-trip. The 12-VALIDATION.md Gate-3 sign-off was executed by an autonomous E2E loop that scored 11/11 against artifact ground-truth (sse.txt, db_turns.txt, snap.json in D:/tmp/agui-e2e/) — a strong proxy and accepted by the operator with the delegation. Whether that autonomous delegation satisfies the ROADMAP SC-1/SC-3 "operator runs and observes" gate is a project-policy call. If the 12-VALIDATION.md autonomous E2E delegation is accepted as equivalent, status upgrades to `passed`. If the policy requires a human to personally type the curl commands, this remains a checkpoint.

**Reference artifacts from the autonomous E2E run (D:/tmp/agui-e2e/):**
- `sse.txt` — full SSE stream with ordered REASONING lifecycle; REASONING_END precedes first TEXT_MESSAGE_START
- `snap.json` — MESSAGES_SNAPSHOT with no CoT
- `db_turns.txt` — assistant row len=21, CoT absent from all rows
- `serve.log` — "graceful shutdown complete", no panic
- `chat_leg.out` — 💭 reasoning deltas before answer, answer "**4**" plain, no mojibake

---

### Gaps Summary

No technical gaps found. All 13 observable truths verified in codebase. All required artifacts exist, are substantive, are wired, and data flows through them. 0 debt markers. 0 stub implementations. 0 missing key links.

The single `human_needed` item is not a technical gap — it is a project-policy question about whether the operator-delegated autonomous E2E sign-off satisfies the ROADMAP's "operator runs" wording. The technical implementation is complete and correct.

**Recommendation:** If the project policy accepts operator-delegated autonomous E2E loops as a valid Gate-3 sign-off mechanism (the pattern established in 12-VALIDATION.md), the status is effectively `passed`. Confirm and close.

---

_Verified: 2026-06-07_
_Verifier: Claude (gsd-verifier)_
