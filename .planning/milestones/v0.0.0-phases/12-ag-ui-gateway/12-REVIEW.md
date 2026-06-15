---
phase: 12-ag-ui-gateway
reviewed: 2026-06-07T00:00:00Z
depth: standard
files_reviewed: 33
files_reviewed_list:
  - .github/workflows/ci.yml
  - cmd/aura/chat_render.go
  - cmd/aura/chat_render_reasoning_test.go
  - cmd/aura/serve.go
  - docs/aura-quality-snapshot.md
  - internal/agent/event.go
  - internal/agent/event_test.go
  - internal/agent/llm_agent.go
  - internal/agent/llm_agent_events.go
  - internal/agent/llm_agent_test.go
  - internal/agui/client.go
  - internal/agui/fanout.go
  - internal/agui/fanout_test.go
  - internal/agui/helpers_test.go
  - internal/agui/main_test.go
  - internal/agui/server.go
  - internal/agui/server_integration_test.go
  - internal/agui/server_test.go
  - internal/agui/testdata/golden-events.json
  - internal/agui/translator.go
  - internal/agui/translator_reasoning_test.go
  - internal/agui/translator_test.go
  - internal/agui/types.go
  - internal/config/config.go
  - internal/config/config_test.go
  - internal/llm/client.go
  - internal/llm/openai_compat/sse.go
  - internal/llm/openai_compat/sse_test.go
  - internal/llm/openai_compat/testdata/reasoning-content-field.txt
  - internal/llm/openai_compat/testdata/reasoning-field.txt
  - scripts/agui_boundary_check.sh
  - scripts/agui_smoke.sh
  - internal/runner/runner.go (cross-ref, persistence invariant)
findings:
  critical: 0
  warning: 6
  info: 5
  total: 11
status: issues_found
---

# Phase 12: Code Review Report

**Reviewed:** 2026-06-07
**Depth:** standard
**Files Reviewed:** 33
**Status:** issues_found

## Summary

Phase 12 (AG-UI Gateway, Slice 8b) ships a thin, well-disciplined SSE transport over the existing
agent runtime. I reviewed the translator, fanout, HTTP server, the reasoning data-plane (amendment
#57) end-to-end across `internal/llm/openai_compat/sse.go` → `internal/agent` Event → translator
→ SSE wire, plus the daemon wiring, config knobs, CI gate, and the boundary/smoke scripts.

The five named invariants from the brief all hold:

1. **Boundary (agent ⊅ agui)** — enforced by `scripts/agui_boundary_check.sh` via `go list -deps`
   (transitive closure, not a shallow grep) and wired as a CI step. Verified clean.
2. **Reasoning is stream-only** — confirmed at three layers: `sse.go` never folds reasoning into
   the accumulator; `llm_agent.consume` never writes reasoning to the returned text; and
   `runner_persist.go` `persistEvent` persists ONLY on `FinishReason != ""` (final Event, which
   carries `Content`, never `Reasoning`). Reasoning chunk Events fall through to `return nil`.
   Tested at every layer (`TestStream_ReasoningDualField`, `TestLlmAgent_ReasoningChunk_StreamOnly`,
   `TestRenderRunnerTurnReasoning`).
3. **No secret leak in error frames** — `sanitizeErr`/`redactEvent` strip DSN userinfo; the
   openai_compat client structurally never puts the API key in error strings (D-28). (Coverage gap
   on non-DSN credential URLs noted in WR-03.)
4. **Fanout never blocks the producer** — drop-on-full default arm in both `fanout.send` and
   `server.pumpSend`; sole-sender-closes discipline is goleak-tested.
5. **Files ≤600 LOC** — all reviewed source files pass (largest: `llm_agent.go` at 469).

No blockers. The findings below are robustness, fidelity, and defense-in-depth gaps. The two that
most deserve a fix before this is leaned on beyond loopback dev are WR-01 (terminal frames are
droppable under backpressure) and WR-02 (a latent iter.Seq2 contract violation on the error path).

## Warnings

### WR-01: SSE drop-on-full can silently drop the terminal RUN_FINISHED/RUN_ERROR frame

**File:** `internal/agui/server.go:170-219` (`streamSSE` / `pumpSend`)
**Issue:** `pumpSend` applies the drop-on-full policy uniformly to EVERY event, including the
terminal `RUN_FINISHED` and `RUN_ERROR` frames. If a slow client lets the cap-N buffer fill and a
terminal frame arrives while the buffer is full, the `default` arm drops it (WARN) and the producer
returns true and exits, closing the channel. The drain loop then sees the closed channel and returns
— the client receives a stream that never reached a terminal frame, and an AG-UI consumer that waits
for `RUN_FINISHED` hangs until its own timeout. "Never block the Loop" is the right tradeoff for
intermediate deltas, but dropping the run-lifecycle terminal is a protocol violation, not graceful
degradation. The same applies to `fanout.send` for the in-process Telegram seam.
**Fix:** Treat run-lifecycle frames as non-droppable. In `pumpSend`, when the buffer is full and the
event is a terminal/lifecycle frame, fall back to a bounded blocking send (still abortable on
ctx-cancel) instead of dropping:
```go
func (s *Server) pumpSend(ctx context.Context, out chan events.Event, ev events.Event) bool {
	if isLifecycleFrame(ev.Type()) { // RUN_STARTED / RUN_FINISHED / RUN_ERROR
		select {
		case out <- ev:
			return true
		case <-ctx.Done():
			return false
		}
	}
	select {
	case out <- ev:
		return true
	case <-ctx.Done():
		return false
	default:
		slog.Warn("agui server: SSE client slow, dropping event", "type", ev.Type())
		return true
	}
}
```
The Loop is still never blocked indefinitely (ctx-cancel always wins), but the terminal frame is no
longer lost. The existing `TestServer_DisconnectClosesPump` keeps passing (it cancels the ctx).

### WR-02: Translator error path can yield after a prior yield returned false (iter.Seq2 contract)

**File:** `internal/agui/translator.go:34-39`
**Issue:** Every other branch in `Translate` guards the close with `if !closeRuns() { return }`. The
error branch does not:
```go
if err != nil {
	_ = closeRuns()              // return value discarded
	yield(events.NewRunErrorEvent(err.Error()), nil)
	return
}
```
If `closeRuns()` yields a `TEXT_MESSAGE_END`/`REASONING_END` and the consumer returns false (stop),
the subsequent `yield(RUN_ERROR, ...)` is a yield-after-false — which `go test` flags as
"range function continued iteration after function returned false" and panics under a strict
consumer. The current SSE pump never returns false except on ctx-cancel (where `closeRuns` itself
would not be reached because the source has already been told to stop), so it is latent today, but
it is a real contract violation and the only un-guarded yield in the file.
**Fix:** Guard it like the rest:
```go
if err != nil {
	if !closeRuns() {
		return
	}
	yield(events.NewRunErrorEvent(err.Error()), nil)
	return
}
```

### WR-03: Secret redaction misses non-DSN credential URLs (bearer/basic-auth in arbitrary URLs)

**File:** `internal/agui/server.go:298` (`secretPattern`)
**Issue:** `secretPattern` only matches `postgres|mysql|mongodb|redis|amqp://...`. A tool/infra error
that embeds credentials in any other URL scheme — e.g. an HTTP MCP server, webhook, or proxy URL of
the form `https://user:password@host/...`, or a leaked `Bearer <token>` / `api_key=...` querystring —
passes through `sanitizeErr`/`redactEvent` to the wire unredacted. The agent path structurally
protects the OpenRouter key (D-28), but the server-side belt-and-suspenders is the stated control for
"tool/infra error strings the translator forwards" (the comment at server.go:295-297), and that class
includes far more than the five DB schemes.
**Fix:** Broaden the redaction to cover URL userinfo across schemes and common token shapes. For
example add a generic `(?i)[a-z][a-z0-9+.-]*://[^/\s:@]+:[^/\s@]+@` userinfo collapser and a
`(?i)(bearer\s+|api[_-]?key=|token=)\S+` token collapser, in addition to the existing DSN rule.
Keep the existing DSN-scheme behavior so the current tests stay green.

### WR-04: MESSAGES_SNAPSHOT drops assistant tool_calls (and ToolCall args) from projected history

**File:** `internal/agui/server.go:281-292` (`projectMessages`)
**Issue:** `projectMessages` copies only `Content` and `ToolCallID` from each persisted `llm.Message`.
A persisted assistant turn that carries `ToolCalls` (e.g. the combined ask_user pause turn written by
`runner_persist.go` `flushPause`, whose `Content` is empty and whose payload lives entirely in
`ToolCalls`) projects to an `events.Message` with empty content and no tool calls — the snapshot
silently loses that turn's information. `events.Message` has a `ToolCalls []ToolCall` field that is
not populated. A client rehydrating a paused thread via GET would see a content-less assistant
message and no record of the pending ask_user call.
**Fix:** Project `m.ToolCalls` onto `events.Message.ToolCalls` (mapping `llm.ToolCall` →
`types.ToolCall`). At minimum, document the omission if structured tool calls in the snapshot are
deliberately out of scope this phase — but as written it is a data-fidelity gap, not a documented
decision.

### WR-05: No OPTIONS/preflight handler — permissive CORS is unusable from a browser as wired

**File:** `internal/agui/server.go:73-78, 118-122`
**Issue:** When `AGUICORSPermissive` is enabled (the dev knob), `Access-Control-Allow-Origin: *` is
set only on the streaming 200 response of `POST /agent/run`. The mux registers only
`POST /agent/run` and `GET /threads/{id}/messages`; there is no `OPTIONS` route. A real browser
cross-origin POST with a non-simple `Content-Type: application/json` triggers a CORS preflight
`OPTIONS /agent/run`, which the ServeMux answers with 405 and no CORS headers — so the permissive
mode does not actually enable browser access, and the header is also absent on the 400/404/500 error
responses. The feature is half-wired: it neither fully works (no preflight) nor is it inert.
**Fix:** If permissive CORS is meant to be functional, register an `OPTIONS` handler (or CORS
middleware) that emits `Access-Control-Allow-Origin/Methods/Headers` and returns 204 for preflight,
and set the ACAO header on all responses (including errors) when the knob is on. If it is only ever
meant as a stop-gap, drop the half-feature to avoid a false sense that browser access is supported.

### WR-06: Fanout has no concurrency guard against Subscribe-after-Run / double-Run

**File:** `internal/agui/fanout.go:39-73`
**Issue:** `Subscribe` appends to `f.subs` and `Run` reads `f.subs` into a local and launches the
producer goroutine. The contract ("Subscribe before Run", "Run at most once") is documented but not
enforced. A caller that calls `Subscribe` after `Run` mutates the slice the producer already snapshotted
(silently never fed — best case) or, if it reallocates concurrently with the goroutine's read, is a
data race the race detector would only catch by luck of timing. A second `Run` launches a second
producer that double-closes the subscriber channels (panic: close of closed channel). The current
tests always follow the contract, so neither is exercised.
**Fix:** Make the misuse loud rather than silently/racily wrong. Guard with a `sync.Once`/started flag
in `Run` (panic or no-op on second call) and reject `Subscribe` after start (return a closed channel
or panic with a clear message). This is consistent with the codebase's "sharp-edges / footgun
detection" posture for in-process seams a later phase (Telegram) will consume.

## Info

### IN-01: `finalEvent` carries a dead `requestID` parameter

**File:** `internal/agent/llm_agent_events.go:156-164`
**Issue:** `finalEvent` takes `requestID string` then immediately discards it (`_ = requestID`). The
parameter is threaded through `dispatch`/`Run` purely to be ignored. Dead plumbing.
**Fix:** Drop the `requestID` parameter from `finalEvent` and its call sites; `newEvent` already
stamps `RequestID` from the `InvocationContext`.

### IN-02: `parseResult.usage` / `hasUsage` are package-private and only read by tests

**File:** `internal/llm/openai_compat/sse.go:38-42`
**Issue:** `parseSSE` returns a `parseResult` whose `usage`/`hasUsage` fields are asserted by the
wire tests but, in the production `Stream` path, usage is delivered as a trailing `Chunk{Usage}` (the
provider-neutral channel) — the returned `parseResult.usage` is not the production carrier. This is
fine and intentional, but the dual representation (channel Chunk vs return struct) is a mild
readability trap: a future reader may wire the wrong one. Worth a one-line comment clarifying that the
return-struct usage is a test/inspection convenience and the channel Chunk is the production carrier.
**Fix:** Add a clarifying comment on `parseResult` noting the channel `Chunk{Usage}` is the
production path; the struct field is for the deterministic fixture asserts.

### IN-03: Redundant manual `flusher.Flush()` after the SDK writer already flushes

**File:** `internal/agui/server.go:196-201`
**Issue:** `sse.SSEWriter.WriteEventWithType` already flushes after each frame (verified in the SDK:
it type-asserts the `http.ResponseWriter` to a flusher and calls `Flush`). The handler then flushes
again at lines 199-201. Harmless but redundant — two flushes per frame.
**Fix:** Remove the manual `flusher`/`Flush()` in `streamSSE` (and the unused `flusher, _ := ...`
assignment), relying on the SDK writer's flush; or keep it with a comment noting it is belt-and-
suspenders for writers the SDK does not flush.

### IN-04: `reasoningRunState` and `textRunState` are near-duplicate state machines

**File:** `internal/agui/translator.go:141-213`
**Issue:** `textRunState` and `reasoningRunState` share the same `idgen/msgID/open` shape and
near-identical `content`/`close` logic, differing only in which SDK constructor they call and the
extra REASONING START/END envelope. This is borderline against the project's "never duplicate; extract
a helper" rule, though the divergent lifecycle envelopes make a clean extraction non-trivial.
**Fix:** Optional — extract the shared `open`/`msgID`/lazy-START bookkeeping into a small embedded
helper, leaving each type to supply only its constructor closures. Low priority; the duplication is
small and the golden tests pin both shapes.

### IN-05: `serve.go` AG-UI failure is fail-soft but indistinguishable from a config error to the operator

**File:** `cmd/aura/serve.go:84-88`
**Issue:** If `ListenAndServe` fails because the AGUI bind port is already taken, the daemon logs
`agui http server stopped` at error level and keeps the scheduler running (intended fail-soft,
Pitfall 6). But the operator gets no actionable hint that the *gateway* is down while the rest of the
daemon is up — a `curl` to the port will simply refuse, and the single error line is easy to miss in a
busy log. Minor operability gap.
**Fix:** Include the bind address and a remediation hint in the error log (`"addr", env.cfg.AGUIBind`,
"is AURA_AGUI_BIND already in use?"), so the fail-soft state is diagnosable.

---

## Verification notes (invariants confirmed, no finding)

- **Boundary gate is a true closure check**, not a grep: `go list -deps ./internal/agent/... | grep -qx`
  on the fully-qualified agui package path. Wired as a CI step. Clean.
- **No-skip-as-green**: `internal/agui` `db_integration` tier is added to the CI `-p 1` integration
  run; `envOrSkip` `t.Fatal`s under `$CI`; `agui_smoke.sh` exits 2 under `$CI` when the DB env is
  unset. The degraded-leg smoke runs against a real daemon with a dummy key. All consistent with the
  project no-skip-as-green rule.
- **Reasoning never persisted**: traced `consume` (no write to `b`) → final Event (Content only) →
  `persistEvent` (only `FinishReason`/`AwaitingInput`/`ToolInvocation` branches persist; reasoning
  chunk Events return nil). Triple-tested.
- **Event JSON round-trip** (`event.go`) is byte-stable incl. the new `reasoning` field
  (omitempty fires; `TestEvent_EmptyReasoning_OmitsKey` / `TestEvent_LLMResponseReasoning_RoundTrips`).
- **Translator purity + lifecycle balance** is property-tested (`TestTranslatorProperty`,
  `assertReasoningQuartets`): RUN_STARTED-first, terminal RUN_FINISHED/RUN_ERROR, no empty deltas,
  balanced START/END, reasoning-fully-precedes-text. Solid coverage.
- **Goroutine discipline**: producer is sole sender + sole closer in both fanout and server pump;
  goleak TestMain + explicit NumGoroutine baseline tests guard the disconnect/cancel/source-end paths.

---

_Reviewed: 2026-06-07_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: standard_
