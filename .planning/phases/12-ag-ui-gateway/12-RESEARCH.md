# Phase 12: AG-UI Gateway - Research

**Researched:** 2026-06-06
**Domain:** SSE event-protocol transport over Aura's in-process agent Event stream (Go 1.26.4; AG-UI community Go SDK)
**Confidence:** HIGH

## Summary

Phase 12 wraps Aura's existing in-process `iter.Seq2[*agent.Event, error]` stream in the AG-UI protocol over SSE. The hard work was already de-risked by live spikes 014-016 (2026-06-06): the SDK module pin is resolved, the full event surface enumerated, and an HTTP `POST /agent/run` SSE round-trip was executed end-to-end against the **real** `internal/agent` types in `sources/016-agui-sse-roundtrip/main.go`. The translator is a **pure function** — `internal/agent` is never imported by `internal/agui` and is never touched (D-17 verified live). This is a thin transport adapter, not a refactor.

The scope (operator decision, this session) is **8a + minimal 8b**: the critical-path translator/types/fanout/client PLUS a loopback-only HTTP server (`server.go` + `aura serve` wiring) with `POST /agent/run` (SSE) and `GET /threads/<id>/messages`. Auth, the Dojo conformance suite, `aura chat --via-agui`, and non-loopback bind remain DEFERRED (amendment #35). The four success criteria all reduce to concrete `curl`/`go.mod`/CI-grep observables.

Two facts dominate the plan and must not be re-litigated: (1) the SDK pin is a **pseudo-version literal** `v0.0.0-20260514093510-e9e910b230b9` — the original amendment-#6 "40-hex SHA in go.mod" gate can NEVER match (invalid go.mod syntax), so the CI gate greps the pseudo-version literal (amendment #56). (2) The real `(*LlmAgent).Run` emits **per-token chunk Events** (`chunkEvent`, `llm_agent.go:418`), not whole-content Events like the spike's synthetic stream — so the translator must coalesce consecutive non-empty deltas into one TEXT_MESSAGE_START/CONTENT*/END message run, keyed on a message-id lifecycle, NOT emit START/CONTENT/END per delta.

**Primary recommendation:** Build `internal/agui/{types,translator,client,fanout,server}.go` as a pure translator over `iter.Seq2[*agent.Event, error]` using the spike-pinned SDK; mount the HTTP server into the existing `aura serve` daemon (it already exists for the scheduler — Phase 12 adds an `http.Server` alongside the scheduler tick loop); enforce the `agent ⇸ agui` boundary with a `go list -deps` grep script wired as a new CI job (depguard is NOT enabled and adding it is heavier than the one-line script).

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| Event → AG-UI wire translation | API/Backend (`internal/agui`) | — | Pure transport adapter; consumes `agent.Event`, emits `events.Event`. Never touches agent runtime (D-17 boundary). |
| SSE framing + flush + backpressure | API/Backend (`internal/agui/server.go`) | — | The SDK `sse.SSEWriter` owns framing/escaping/flush; server owns the buffered-channel backpressure + drop policy. |
| `POST /agent/run` request lifecycle | API/Backend (`server.go`) | Orchestration (`runner.Runner`) | HTTP handler parses `RunAgentInput`, resolves the thread, drives `Runner.Turn`, streams the translated events. |
| `GET /threads/<id>/messages` | API/Backend (`server.go`) | Persistence (`conversations.Store.LoadHistory`) | Read path: rehydrate persisted turns → `MESSAGES_SNAPSHOT`. No agent run. |
| Thread → conversation resolution | Persistence (`conversations.Store`) | — | `threadId` == `conversations.id`; `Get`/`LoadHistory` already exist. Missing thread → 404. |
| Resume mapping (interrupt → pause token) | Orchestration (`runner.Runner`) | Persistence (`askuser.Store`) | `RunAgentInput.Resume[]` → `Runner.SubmitAnswers` (existing seam). Translator surfaces the interrupt; server maps it back. |
| `aura serve` daemon lifecycle | CLI (`cmd/aura/serve.go`) | API/Backend | Server hosts ON the existing serve daemon (graceful SIGTERM drain already wired for the scheduler). |
| Boundary enforcement (`agent ⇸ agui`) | CI (new script + job) | — | `go list -deps ./internal/agent/...` must not contain `internal/agui`. |

## Standard Stack

### Core
| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| `github.com/ag-ui-protocol/ag-ui/sdks/community/go` | `v0.0.0-20260514093510-e9e910b230b9` (pseudo-version) | AG-UI event constructors, `Validate()`, `RunAgentInput`/`Interrupt`/`ResumeEntry` types, `sse.SSEWriter` | Official community Go SDK; the protocol Aura ratified as 2026 transport standard (amendment #35). Spike-pinned + spike-proven. `[CITED: spike-findings-Aura/references/agui-gateway.md]` |
| `net/http` (stdlib) | Go 1.26.4 | HTTP server, `POST /agent/run` + `GET /threads/<id>/messages` routing (`http.ServeMux` pattern routing, e.g. `"POST /agent/run"`) | stdlib; the spike's mux used Go 1.22+ method-pattern routing. No router dep needed. `[VERIFIED: spike 016 main.go]` |
| `github.com/google/uuid` | `v1.6.0` (already direct) | `runId` minting (`run-` prefix), `messageId` generation | Already Aura's shared dep; the SDK also depends on it (shared, no version conflict). `[VERIFIED: go.mod + spike blueprint]` |

### SDK Packages Used
| Package | Symbols | Use |
|---------|---------|-----|
| `pkg/core/events` | `NewRunStartedEvent`, `NewTextMessageStartEvent(id, ...WithRole)`, `NewTextMessageContentEvent(id, delta)`, `NewTextMessageEndEvent(id)`, `NewToolCallStartEvent(callID, name)`, `NewToolCallArgsEvent(callID, delta)`, `NewToolCallEndEvent(callID)`, `NewToolCallResultEvent(msgID, callID, content)`, `NewStateDeltaEvent([]JSONPatchOperation)`, `NewRunFinishedEventWithOptions(thread, run, WithSuccessOutcome()/WithInterruptOutcome([]types.Interrupt))`, `NewRunErrorEvent(msg)`, `NewReasoning*Event`, `JSONPatchOperation`, per-event `.Validate()`, `.Type()` | Constructors auto-attach `timestamp`; `TOOL_CALL_RESULT` auto-injects `"role":"tool"`. `[VERIFIED: spike 015 main.go]` |
| `pkg/core/types` | `RunAgentInput` (hand-written `UnmarshalJSON`, camelCase+snake_case), `Message`, `Interrupt{ID,Reason,Message,ToolCallID,ResponseSchema,ExpiresAt}`, `ResumeEntry{InterruptID,Status,Payload}`, `ResumeStatusResolved`/`ResumeStatusCancelled` | Input parsing + interrupt/resume contract. `[VERIFIED: spike 015 main.go]` |
| `pkg/encoding/sse` | `NewSSEWriter()`, `.WithLogger(slog)`, `.WriteEventWithType(ctx, w, ev, string(ev.Type()))` | SSE framing: emits `event: TYPE` + `id: TYPE_<ms>` + `data: {json}` + blank line. `http.ResponseWriter` satisfies its flusher seam. `[VERIFIED: spike 016 main.go]` |

### Supporting
| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| `go.uber.org/goleak` | `v1.3.0` (already) | The SSE pump goroutine + server lifecycle leak discipline (PRD §Test discipline #3) | `goleak.VerifyTestMain` in `server_test.go` TestMain — mandatory for the buffered-channel pump. |
| `pgregory.net/rapid` | `v1.3.0` (already, test-only) | Property-based translator test over the 21 emitted event types | `translator_test.go` — feed random `[]*agent.Event` sequences, assert valid AG-UI output + `Validate()` passes on each. |

### Transitive Deps the SDK Brings
| Dep | Note |
|-----|------|
| `github.com/sirupsen/logrus v1.9.3` | Imported by `core/events/decoder.go`; links into the binary even though Aura's **emit** path never logs through it (the SSE writer uses slog). Tolerated — the client-side decode path is never used server-side. `[CITED: agui-gateway.md Constraints]` |
| `github.com/google/uuid v1.6.0` | Already Aura's — shared, no conflict. |

**Installation (two `go get`s — the second is mandatory for transitive sums when nothing imports the module yet):**
```bash
go get github.com/ag-ui-protocol/ag-ui/sdks/community/go@e9e910b230b9329c905e31ca024b4114dedf7918
go get github.com/ag-ui-protocol/ag-ui/sdks/community/go/pkg/core/events@v0.0.0-20260514093510-e9e910b230b9
```
The first build otherwise fails with `missing go.sum entry` for logrus. `[CITED: agui-gateway.md "How to Build It" step 1]`

**Version verification:** `go list -m github.com/ag-ui-protocol/ag-ui/sdks/community/go` currently returns `not a known dependency` (the spike reverted the `go get` at session end). After Phase 12's `go get`, `go.mod` must record the literal `v0.0.0-20260514093510-e9e910b230b9`. The repo has **no subdir tags** (`sdks/community/go/vX.Y.Z`), so a pseudo-version is the ONLY possible resolution forever — this is why the CI gate greps the pseudo-version literal, not a SHA. `[VERIFIED: go list -m (not present), agui-gateway.md Constraints]`

### Alternatives Considered
| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| AG-UI community Go SDK | Hand-rolled event structs | Rejected — the SDK's `RunAgentInput.UnmarshalJSON` (dual camel/snake), per-event `Validate()`, and `SSEWriter` framing are exactly what Phase 12 needs; hand-rolling re-implements protocol details the SDK got right (halves `types.go` from ~80 to ~40 LOC). `[CITED: agui-gateway.md step 4]` |
| `http.ServeMux` method-pattern routing | chi / gorilla/mux | Rejected — only 2 routes, stdlib `"POST /agent/run"` pattern routing (Go 1.22+) is sufficient and adds zero deps; matches the existing codebase's no-router posture. |
| Pseudo-version pin | `@latest` / floating | FORBIDDEN by amendment #6 intent (no floating); the pin is immutable via go.sum. CI greps the literal. |

## Package Legitimacy Audit

> The single external package is the AG-UI community Go SDK. slopcheck could not be run in this environment (pip/network constrained), so per the graceful-degradation rule the package is tagged below with its provenance and gated for verification at install time.

| Package | Registry | Age | Downloads | Source Repo | slopcheck | Disposition |
|---------|----------|-----|-----------|-------------|-----------|-------------|
| `github.com/ag-ui-protocol/ag-ui/sdks/community/go` | Go module proxy (GitHub) | commit 2026-05-14 | n/a (Go modules) | github.com/ag-ui-protocol/ag-ui | not run | **Approved with provenance** — spike-pinned to an immutable commit + go.sum, ratified as the project's transport standard in PRD amendment #35, executed live in spikes 014-016 against real Aura types. NOT a hallucinated/slopsquatted name. |

**Packages removed due to slopcheck [SLOP] verdict:** none
**Packages flagged as suspicious [SUS]:** none

The SDK's legitimacy is established by three independent live spikes (module resolves + builds + events construct/marshal/validate, full SSE round-trip) rather than by registry heuristics. The pseudo-version pin + go.sum makes the exact bytes immutable — a supply-chain swap would change the hash and break the build. The planner should still keep the install behind the standard post-edit `go build`/`go vet` gate.

## Architecture Patterns

### System Architecture Diagram

```
                          ┌─────────────────────────────────────────────┐
  HTTP client             │              aura serve (daemon)            │
 (curl / Dojo / SPA)      │   ┌──────────────┐    ┌───────────────────┐ │
       │                  │   │ scheduler    │    │  http.Server      │ │
       │ POST /agent/run  │   │ tick loop    │    │  (NEW, Phase 12)  │ │
       ├─────────────────────▶│ (existing)   │    │  127.0.0.1:9080   │ │
       │                  │   └──────────────┘    └─────────┬─────────┘ │
       │                  └──────────────────────────────────┼──────────┘
       │                                                     │
       │              ┌──────────────────────────────────────▼─────────────────┐
       │              │  internal/agui/server.go  (NEW)                         │
       │              │   1. parse types.RunAgentInput (SDK UnmarshalJSON)      │
       │              │   2. resolve threadId → conversations.Get (404 if none) │
       │              │   3. map Resume[] → Runner.SubmitAnswers (if present)   │
       │              │   4. mint runId = "run-"+uuidv4                         │
       │              └───────────────────┬────────────────────────────────────┘
       │                                  │  agentSeq := Runner.Turn(ctx, convID, userMsg)
       │                                  │  iter.Seq2[*agent.Event, error]
       │              ┌───────────────────▼────────────────────────────────────┐
       │              │  internal/agui/translator.go  (NEW, PURE)               │
       │              │   Translate(threadID, runID, seq) →                     │
       │              │     iter.Seq2[events.Event, error]                      │
       │              │   - RUN_STARTED first                                   │
       │              │   - coalesce per-token chunk Events into                │
       │              │       TEXT_MESSAGE_START / CONTENT* / END runs          │
       │              │   - ToolInvocation start → TOOL_CALL_START(+ARGS)       │
       │              │   - ToolInvocation end   → TOOL_CALL_END + RESULT       │
       │              │   - StateDelta map → STATE_DELTA (sorted keys)          │
       │              │   - AwaitingInput → RUN_FINISHED(interrupt) outcome     │
       │              │   - err → RUN_ERROR, stop                               │
       │              │   - RUN_FINISHED(success) last                          │
       │              └───────────────────┬────────────────────────────────────┘
       │                                  │  for ev := range Translate(...)
       │              ┌───────────────────▼────────────────────────────────────┐
       │  SSE stream  │  pkg/encoding/sse.SSEWriter.WriteEventWithType          │
       │◀─────────────│   (buffered-channel pump, cap 64, drop+WARN on slow)    │
       │              └─────────────────────────────────────────────────────────┘
       │
       │ GET /threads/<id>/messages
       └─────────────▶ server.go → conversations.LoadHistory → MESSAGES_SNAPSHOT (single SSE/JSON)

  BOUNDARY (CI-enforced): internal/agent  ⇸  internal/agui   (one-way: agui imports agent, NEVER reverse)
  fanout.go: in-process Fanout wraps iter.Seq2 → N subscriber channels (Phase 13 Telegram consumer; not on the HTTP path)
```

### Recommended Project Structure
```
internal/agui/
├── types.go          # ~40 LOC — Aura-semantic validation only (threadId=existing UUID,
│                     #          messages-non-empty); SDK owns RunAgentInput parsing
├── translator.go     # ~180 LOC — PURE iter.Seq2[*agent.Event,error] → iter.Seq2[events.Event,error]
├── fanout.go         # ~80 LOC — Fanout struct: wrap iter.Seq2, distribute to N subscriber chans
├── client.go         # ~80 LOC — (8a) in-process subscriber helper; reasoning dual-field map
├── server.go         # ~200 LOC — HTTP server: POST /agent/run (SSE) + GET /threads/<id>/messages
├── translator_test.go# property-based (rapid) over 21 emitted types + golden fixtures (spike 015)
└── server_test.go    # integration: goleak TestMain + httptest round-trip + GET messages

cmd/aura/serve.go     # +~80 LOC diff — mount http.Server alongside the scheduler; --bind guard
internal/config/config.go  # +AURA_AGUI_* fields (port, cors-permissive, bind)
scripts/agui_boundary_check.sh  # NEW — go list -deps grep gate (CI job)
```

### Pattern 1: Pure translator over the real Run signature
**What:** A function `Translate(threadID, runID string, seq iter.Seq2[*agent.Event, error]) iter.Seq2[events.Event, error]` that maps Aura Events 1:N to AG-UI events with lifecycle framing.
**When to use:** This is THE phase deliverable. It imports `internal/agent` (for the Event type) and the SDK; nothing imports it back.
**Example (spike-proven seed — the working ~60-LOC core):**
```go
// Source: spike-findings-Aura/sources/016-agui-sse-roundtrip/main.go (translate())
func translate(threadID, runID string, seq iter.Seq2[*agent.Event, error]) iter.Seq2[events.Event, error] {
    return func(yield func(events.Event, error) bool) {
        if !yield(events.NewRunStartedEvent(threadID, runID), nil) { return }
        for ev, err := range seq {
            if err != nil { yield(events.NewRunErrorEvent(err.Error()), nil); return }
            if ti := ev.Actions.ToolInvocation; ti != nil { /* TOOL_CALL_START/ARGS/END/RESULT */ continue }
            if len(ev.Actions.StateDelta) > 0 { /* STATE_DELTA, sorted keys */ continue }
            if ev.LLMResponse != nil && ev.LLMResponse.Content != "" { /* TEXT_MESSAGE_START/CONTENT/END */ }
        }
        yield(events.NewRunFinishedEventWithOptions(threadID, runID, events.WithSuccessOutcome()), nil)
    }
}
```
**CRITICAL DIVERGENCE from the spike:** the spike's `synthSeq()` emits ONE whole-content Event per assistant message (so START/CONTENT/END collapse cleanly per Event). The REAL `(*LlmAgent).Run` (`llm_agent.go:418`, via `consume`→`chunkEvent`) emits **one Event per streamed token delta**, each with `LLMResponse.Content == "<delta>"` and no `FinishReason`. Emitting START/CONTENT/END per delta would produce one single-token message per token. The production translator MUST run a small state machine: open a TEXT_MESSAGE_START on the FIRST non-empty delta of a contiguous assistant run (mint a `messageId`), emit TEXT_MESSAGE_CONTENT per non-empty delta, and close TEXT_MESSAGE_END when the run ends (a tool call, a final Event with `FinishReason`, a state delta, or stream end interrupts the text run). The final Event (`finalEvent`/`finalizeEvent`, carries `FinishReason`) and the per-delta chunks BOTH carry `LLMResponse.Content`; coalesce so the final answer is not double-streamed. See Pitfall 1.

### Pattern 2: SSE serving via the SDK writer
**What:** `sse.NewSSEWriter().WriteEventWithType(ctx, w, ev, string(ev.Type()))` per event; the writer does framing, JSON escaping, and per-event flush.
**Example:**
```go
// Source: spike 016 main.go
writer := sse.NewSSEWriter()
w.Header().Set("Content-Type", "text/event-stream")
w.Header().Set("Cache-Control", "no-cache")
for ev, _ := range translate(in.ThreadID, runID, agentSeq) {
    if err := writer.WriteEventWithType(r.Context(), w, ev, string(ev.Type())); err != nil { return }
}
```
Produces `event: TYPE` + `id: TYPE_<ms>` + `data: {json}` + blank line — exactly the PRD smoke framing (the `id:` line is a free `Last-Event-ID` reconnect hook). `WriteEvent` (no `-WithType`) is data-only (TS-SDK default); keep `event:` lines, but re-verify Dojo compat at any future Dojo work. `[VERIFIED: spike 016]`

### Pattern 3: Mount the HTTP server on the existing serve daemon
**What:** `aura serve` already runs the scheduler tick loop with graceful SIGTERM drain (`cmd/aura/serve.go`). Phase 12 adds an `http.Server` bound to `127.0.0.1:9080` that runs concurrently and is shut down in the same graceful path.
**When to use:** Success criterion 1 demands `aura serve` + `curl`. Reuse `bootServe`→`bootChatEnv` (it already builds the pool + Runner + registry the HTTP handler needs).
**Example skeleton:**
```go
// In bootServe / runServe: start the http.Server in a goroutine, Shutdown on ctx cancel.
srv := &http.Server{Addr: cfg.AGUIBind, Handler: agui.NewServer(env.run, env.conv).Mux()}
go func() { if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) { slog.Error(...) } }()
// on shutdown:
ctxShut, cancel := context.WithTimeout(context.Background(), 10*time.Second); defer cancel()
_ = srv.Shutdown(ctxShut)  // graceful drain of active SSE connections
```

### Pattern 4: Backpressure (buffered channel, cap 64, drop + WARN)
**What:** The SSE writer feeds a buffered channel (capacity 64). A slow client that fills it triggers a drop with a WARN log + a `RUN_ERROR` event if persistent — the Loop NEVER blocks.
**Why:** A blocking SSE write would back-pressure into `Runner.Turn` → `LlmAgent.Run`, stalling the agent on a slow HTTP reader. Channel + `select` with a default (drop) decouples them. Use `goleak` to prove the pump goroutine exits on client disconnect (`r.Context().Done()`).
`[CITED: prd.md Slice 8 Backpressure acceptance + agui-gateway.md]`

### Anti-Patterns to Avoid
- **START/CONTENT/END per token delta:** produces one message per token. Coalesce a contiguous assistant run into one message lifecycle (Pattern 1 divergence note).
- **Emitting empty deltas:** `NewTextMessageContentEvent(id, "").Validate()` → error ("delta field must not be empty"). Real LLM streams produce empty deltas; SKIP them. `[VERIFIED: spike 016 EDGE probe]`
- **Empty `messageId`/`toolCallId`:** both are `Validate()`-required. The translator OWNS id generation. `[VERIFIED: spike 016 EDGE probe]`
- **THINKING_* events:** deprecated aliases at this pin. Use REASONING_* (`REASONING_START`/`REASONING_MESSAGE_START`/`REASONING_MESSAGE_CONTENT`/`REASONING_MESSAGE_END`/`REASONING_END`). `[VERIFIED: spike 015 golden]`
- **Unsorted multi-key StateDelta in goldens:** map iteration order is non-deterministic — sort keys before building `[]JSONPatchOperation` so golden compares are stable. `[CITED: agui-gateway.md]`
- **Implementing the amendment-#6 40-hex-SHA CI gate as originally written:** 0 matches forever, falsely red. Grep the pseudo-version literal instead. `[CITED: amendment #56]`
- **`internal/agent` importing `internal/agui`:** the entire premise. One-way only; CI-enforced.

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| SSE framing (`event:`/`id:`/`data:`/blank line, flush) | Custom `fmt.Fprintf` framer | `sse.SSEWriter.WriteEventWithType` | The SDK handles escaping, per-event flush, and the `id:` reconnect hook; matches the PRD smoke byte-for-byte. |
| `RunAgentInput` JSON parsing | Hand-rolled decoder | SDK `types.RunAgentInput` `UnmarshalJSON` | Accepts BOTH camelCase and snake_case on every field; halves `types.go`. `[VERIFIED: spike 015]` |
| Event construction + validation | Struct literals | `events.New*Event(...)` + `.Validate()` | Constructors auto-attach timestamps and `role:tool`; `Validate()` is the contract that rejects empty ids/deltas. |
| Interrupt/resume contract | RoleTool-answers-in-messages design | `RunAgentInput.Resume []ResumeEntry` + `types.Interrupt.ResponseSchema` | Protocol-native; `status=cancelled` gives HITL cancel for free (supersedes PRD's old RoleTool design — amendment #56). |
| HTTP route dispatch | chi/gorilla | stdlib `http.ServeMux` method patterns | 2 routes; Go 1.22+ pattern routing. Matches existing no-router codebase posture. |
| Thread history read | New query | `conversations.Store.LoadHistory(ctx, convID)` | Already byte-identical, sidecar-rehydrating, and tested. `GET /threads/<id>/messages` is a thin projection over it. |
| Resume orchestration | New resume path | `runner.Runner.SubmitAnswers` + `Turn` | The Runner is already the SOLE paused_states writer and resolves resume as a fresh `Run` over rehydrated history (SC-4). |

**Key insight:** Nearly every hard part of this phase is already built — the SDK owns the protocol, `conversations.Store` owns persistence/read, `runner.Runner` owns the turn/resume orchestration, and `aura serve` owns the daemon lifecycle. Phase 12's genuinely new code is the **pure translator** (the only non-trivial logic, ~180 LOC) plus thin HTTP glue. Resist re-plumbing existing seams.

## Common Pitfalls

### Pitfall 1: Treating per-token chunk Events as whole-content Events
**What goes wrong:** The translator emits TEXT_MESSAGE_START/CONTENT/END per delta, producing one single-token AG-UI message per streamed token (and re-streaming the final answer because `finalEvent` ALSO carries `LLMResponse.Content`).
**Why it happens:** Spike 016's `synthSeq()` is synthetic — one whole-content Event per message. The real `(*LlmAgent).Run` streams per-token via `consume`→`chunkEvent` (`llm_agent.go:399-428`), then emits a `finalEvent`/`finalizeEvent` carrying the FULL answer + `FinishReason`. Both chunks and final carry `Content`.
**How to avoid:** Run a text-message state machine: open START on the first non-empty delta of a contiguous assistant run, CONTENT per non-empty delta, END when the run is interrupted (tool call, state delta, stream end, OR a final Event with `FinishReason != ""`). Decide ONE policy for the final Event: either (a) stream only the deltas and treat the final Event as END-only (preferred — avoids double text), or (b) suppress deltas and stream only the final Content. Document the choice; the `toolResultEvent`/`toolPreviewEvent` Events also carry `Content` + a `StateDelta{tool_call_id}` marker (`llm_agent_events.go:60-103`) — distinguish them from assistant prose by that marker so tool-result previews don't pollute the TEXT_MESSAGE stream.
**Warning signs:** A 200-token answer produces 200 AG-UI messages; the final answer appears twice in the stream.

### Pitfall 2: tool-result Events look like assistant text
**What goes wrong:** `toolResultEvent` and `toolPreviewEvent` set `LLMResponse.Content = run.Preview` (the tool output preview), so a naive `ev.LLMResponse.Content != ""` check streams raw tool output as assistant prose.
**Why it happens:** Aura overloads `LLMResponse.Content` for both assistant chunks AND tool-result previews; the disambiguator is `ev.Actions.StateDelta["tool_call_id"]` being set (`llm_agent_events.go:63-69`).
**How to avoid:** In the translator, branch on `ev.Actions.ToolInvocation` FIRST (the start/end lifecycle), and treat an Event carrying `Actions.StateDelta["tool_call_id"]` as a tool-result → TOOL_CALL_RESULT, NOT a TEXT_MESSAGE. Only Events with `LLMResponse.Content != ""` AND no `tool_call_id` state-delta marker are assistant prose.
**Warning signs:** Tool output (e.g. "12°C sereno") streams to the UI as the assistant's own message.

### Pitfall 3: The amendment-#6 CI grep can never match (already resolved, do not regress)
**What goes wrong:** A CI gate that greps `go.mod` for the 40-hex SHA `e9e910b230b9329c905e31ca024b4114dedf7918` matches 0 times forever (go.mod records the pseudo-version, not a raw SHA), failing the build falsely.
**Why it happens:** `require <path> <sha40>` is not valid go.mod syntax; an untagged subdir module resolves only to a pseudo-version.
**How to avoid:** Grep `go.mod` for the literal `v0.0.0-20260514093510-e9e910b230b9` (success criterion 4 explicitly says "CI greps the literal"). `[CITED: amendment #56]`
**Warning signs:** CI red on a clean go.mod with the correct pin.

### Pitfall 4: SSE pump goroutine leak on client disconnect
**What goes wrong:** The buffered-channel pump goroutine blocks forever writing to a full channel after the client disconnects, leaking under `goleak`.
**Why it happens:** No `select { case <-r.Context().Done(): return }` arm in the pump.
**How to avoid:** Every channel send in the pump is a `select` with a `ctx.Done()` arm AND a default (drop) arm. `goleak.VerifyTestMain` in `server_test.go` proves the pump exits on disconnect. Follow the `golang-concurrency` skill core principle #7 (always include `ctx.Done()` in select).
**Warning signs:** `server_test.go` goleak flags a lingering goroutine after `httptest` server close.

### Pitfall 5: Boundary gate must catch a TRANSITIVE import, not just a direct one
**What goes wrong:** A boundary check that greps `internal/agent/*.go` for `"internal/agui"` import lines misses a transitive violation (e.g. agent imports X which imports agui).
**Why it happens:** Source grep is shallow; the real invariant is the dependency CLOSURE.
**How to avoid:** Use `go list -deps ./internal/agent/...` and assert `internal/agui` is absent from the full transitive dep list. This is the same technique the codebase already uses to prove cycle-freedom (`go list -deps ./internal/agent/tools` has 0 `internal/skills` — STATE.md 11-02). A one-line script in `scripts/` wired as a CI job is lighter and more correct than enabling depguard.
**Warning signs:** A green source-grep gate while `go build` pulls agui into the agent package's closure.

### Pitfall 6: `--bind` to a non-loopback address under local-only privacy
**What goes wrong:** Binding the HTTP server to `0.0.0.0` or a public IP exposes an UNAUTHENTICATED agent endpoint (auth is out of scope for this phase).
**Why it happens:** The PRD's `identity_id != local → 403` is authorization WITHOUT authentication — not a security control until auth lands.
**How to avoid:** Default bind is `127.0.0.1:9080`. If a `--bind`/`AURA_AGUI_BIND` flag is exposed at all, it MUST fail-fast on a non-loopback address under `AURA_PRIVACY_MODE=local-only` (parse host → `IsLoopback`, mirroring the existing config boot-fail check — prd.md §AURA_PRIVACY_MODE / amendment #35 fix 2). Simplest compliant option: hardcode loopback for this phase, defer the flag.
**Warning signs:** `aura serve --bind 0.0.0.0:9080` boots without error under local-only mode.

## Code Examples

### Resolve thread + 404 (server.go)
```go
// conversations.Store.Get returns ErrConversationNotFound on a missing row.
conv, err := srv.conv.Get(r.Context(), in.ThreadID)
if errors.Is(err, conversations.ErrConversationNotFound) {
    http.Error(w, "thread not found", http.StatusNotFound); return
}
```
`[VERIFIED: internal/conversations/store.go:157-170]`

### GET /threads/<id>/messages → MESSAGES_SNAPSHOT
```go
// LoadHistory is byte-identical + sidecar-rehydrating; project llm.Message → events.Message.
hist, err := srv.conv.LoadHistory(r.Context(), threadID)   // []llm.Message
msgs := make([]events.Message, 0, len(hist))
for i, m := range hist { msgs = append(msgs, events.Message{ID: fmt.Sprintf("msg-%d", i+1), Role: m.Role, Content: m.Content, ToolCallID: m.ToolCallID}) }
snap := events.NewMessagesSnapshotEvent(msgs)
```
`[VERIFIED: store.go:363 LoadHistory + spike 015 NewMessagesSnapshotEvent signature]`

### Interrupt outcome from AwaitingInput
```go
// ev.Actions.AwaitingInput → types.Interrupt (map Question/ToolCallID/Options → ResponseSchema).
ai := ev.Actions.AwaitingInput
intr := types.Interrupt{
    ID: ai.ToolCallID, Reason: ai.Kind, Message: ai.Question, ToolCallID: ai.ToolCallID,
    ResponseSchema: map[string]any{"type":"object","properties":map[string]any{"answer":map[string]any{"type":"string"}}},
}
yield(events.NewRunFinishedEventWithOptions(threadID, runID, events.WithInterruptOutcome([]types.Interrupt{intr})), nil)
```
`[VERIFIED: agent/event.go AwaitingInput fields + spike 015 WithInterruptOutcome]`

### Env registration (config.go pattern)
```go
// Append to loadBase() Config literal — follows the existing envDefault/envBoolDefault pattern:
AGUIBind:           envDefault("AURA_AGUI_BIND", "127.0.0.1:9080"),
AGUICORSPermissive: envBoolDefault("AURA_AGUI_CORS_PERMISSIVE", false),
```
`[VERIFIED: internal/config/config.go:133-203 envDefault/envBoolDefault helpers]`

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| PRD "Loop has an `emitter` interface" + Subscribe/callback plumbing | Pure `mapToAGUI(*Event)` translator over `iter.Seq2` (range-over-func) | Slice 0.9 amendment | −100 LOC; no custom emitter plumbing. The translator consumes `(*LlmAgent).Run` directly. |
| PRD pin = 40-hex SHA in go.mod | Pseudo-version literal `v0.0.0-20260514093510-e9e910b230b9` | Amendment #56 (spike 014) | CI gate greps the literal; the SHA-grep was unsatisfiable. |
| Resume via RoleTool answers in `messages` matched on `tool_call_id` | Protocol-native `RunAgentInput.Resume []ResumeEntry` + `Interrupt.ResponseSchema` | Amendment #56 (spike 015) | `status=cancelled` = free HITL cancel; map `interruptId` → pause token. |
| Outcome `{interrupted, errored}` | Outcome `{success, interrupt}`; errors are RUN_ERROR events, not outcomes | Amendment #56 | Only two outcome literals; failure path is a distinct event. |
| THINKING_* reasoning events | REASONING_* family (exact names: START/MESSAGE_START/MESSAGE_CONTENT/MESSAGE_END/END) | This SDK pin | THINKING_* deprecated; serves amendment #33 dual-field reasoning. |
| PRD "~17-25 event types" | 28 active + 5 deprecated; Aura emits 21 | Spike 015 enumeration | Property tests target the 21 Aura emits, seeded from `golden-events.json`. |

**Deprecated/outdated:**
- THINKING_* events: replaced by REASONING_* at this pin.
- The 8b full deferral (amendment #35): SUPERSEDED by the operator scope decision this session — minimal 8b server IS in Phase 12. Dojo suite / `--via-agui` / non-loopback+auth stay deferred.

## Runtime State Inventory

> Not a rename/refactor/migration phase — this is a greenfield transport package. No stored data, live-service config, OS-registered state, secrets, or build artifacts carry a string this phase renames. The only persistence touch is READ-ONLY (`conversations.LoadHistory` for `GET /threads/<id>/messages`) and the existing `Runner.Turn` write path (unchanged). **None — verified by: new package `internal/agui`, no migration, no schema change, no env rename.**

## Project Constraints (from CLAUDE.md)

- **NO GOD CLASS >600 LOC.** All target files are well under (largest: server.go ~200). Enforced by `scripts/check-file-size.sh` (CI job already wired).
- **Post-edit validation:** after every Go edit run `go vet ./...`, `go build ./...`, `go test ./internal/agui/`, `go test -race ./internal/agui/`.
- **No skip-as-green in CI:** the server integration test (criterion 1/3) needs Postgres for conversations — it MUST actually run. Follow the existing `db_integration` env pattern (compose role DSNs `AURA_DB_URL`/`AURA_DB_MIGRATE_URL`, `CI=true` arms the no-skip guard). A sub-second "integration" runtime is a skip tell.
- **Coverage floor 85% owned-surface** (overrides PRD 75/60). Report combined unit+integration figure across the tag matrix.
- **Mutation spot-check ≥70%** on the phase's critical file — `translator.go` is the obvious candidate (the only non-trivial logic).
- **One slice = one commit** (or N sub-slice commits with atomicity notes). PRD-amendment commit FIRST if a gap is found (none expected — #56 already landed).
- **FOLLOW EXISTING PATTERNS:** consumer-declared narrow interfaces (D-A2-02), `envDefault`/`envBoolDefault` for config, hand-rolled subcommand switch (not cobra), `goleak.VerifyTestMain` per package with goroutines.
- **NO COMMENTS unless WHY is non-obvious.** The translator's chunk-coalescing and the tool-result-marker disambiguation are exactly the non-obvious cases that warrant a comment.
- **NEVER MODIFY TESTS TO PASS** unless the test is broken.
- **GIT PUSH DISCIPLINE / master-direct:** commit on the working branch; no push unless asked.

## Validation Architecture

> nyquist_validation is enabled (`.planning/config.json` workflow.nyquist_validation: true).

### Test Framework
| Property | Value |
|----------|-------|
| Framework | Go stdlib `testing` + `pgregory.net/rapid` (property) + `go.uber.org/goleak` (leak) + `net/http/httptest` (server) |
| Config file | none (go test); build tags `db_integration` for the server tier (Postgres-backed) |
| Quick run command | `go test ./internal/agui/` (unit + translator property) |
| Full suite command | `go test -tags db_integration -race -count=1 ./internal/agui/...` (server integration with Postgres up) |

### Phase Requirements → Test Map
| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| UX-01 (SC1) | `aura serve` + `curl POST /agent/run` streams RUN_STARTED/TEXT_MESSAGE_*/TOOL_CALL_*/RUN_FINISHED in SSE | integration | `go test -tags db_integration -run TestServeRunSSE ./internal/agui/` + live `scripts/agui_smoke.sh` (curl, FakeClient) | ❌ Wave 0 |
| UX-01 (SC2) | importing `internal/agui` from `internal/agent/` fails the build with explicit boundary error | static/CI | `bash scripts/agui_boundary_check.sh` (go list -deps grep) | ❌ Wave 0 |
| UX-01 (SC3) | `curl GET /threads/<id>/messages` after a run shows persisted history matching the SSE stream | integration | `go test -tags db_integration -run TestThreadMessages ./internal/agui/` | ❌ Wave 0 |
| UX-01 (SC4) | `go.mod` pins the pseudo-version literal `v0.0.0-20260514093510-e9e910b230b9` | static/CI | `grep -F 'v0.0.0-20260514093510-e9e910b230b9' go.mod` (in a CI step) | ❌ Wave 0 |
| UX-01 (translator) | 21 emitted Event types → valid AG-UI sequence; skip empty deltas; non-empty ids; sorted StateDelta keys | unit/property | `go test -run TestTranslatorProperty ./internal/agui/` (rapid, golden-events.json fixtures) | ❌ Wave 0 |
| UX-01 (leak) | SSE pump exits on client disconnect; server shutdown goleak-clean | unit | `go test -race -run TestServer ./internal/agui/` (goleak TestMain) | ❌ Wave 0 |

### Sampling Rate
- **Per task commit:** `go vet ./... && go build ./... && go test ./internal/agui/`
- **Per wave merge:** `go test -tags db_integration -race -count=1 ./internal/agui/...` + `bash scripts/agui_boundary_check.sh`
- **Phase gate:** full suite green + live `scripts/agui_smoke.sh` curl round-trip + `grep -F` go.mod literal + boundary gate + coverage_gate.sh ≥85% + translator.go mutation ≥70% before `/gsd-verify-work`.

### Wave 0 Gaps
- [ ] `internal/agui/translator_test.go` — property-based, covers UX-01 translator obligations (seed from `golden-events.json`)
- [ ] `internal/agui/server_test.go` — covers SC1/SC3 (httptest + Postgres), goleak TestMain for SC pump
- [ ] `scripts/agui_boundary_check.sh` — covers SC2 (`go list -deps ./internal/agent/...` must not contain `internal/agui`)
- [ ] `scripts/agui_smoke.sh` — live curl round-trip (FakeClient or operator OpenRouter), the SC1 CI/operator gate
- [ ] CI job additions in `.github/workflows/ci.yml`: (a) go.mod literal grep (SC4), (b) boundary check (SC2), (c) the agui db_integration tier in the integration-test job env (reuse the existing `AURA_DB_URL`/`AURA_DB_MIGRATE_URL`/`CI=true`)
- [ ] Framework install: none new — rapid + goleak + httptest already available

## Security Domain

> `security_enforcement` not present in config → treated as enabled. This phase opens a network surface, so security is in scope even though auth is deferred.

### Applicable ASVS Categories
| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V2 Authentication | no (deferred) | NONE this phase — endpoint is loopback-only, unauthenticated by design. The control is the loopback bind + fail-fast on non-loopback under local-only (compensating). |
| V3 Session Management | no | No sessions; each `POST /agent/run` is a fresh run keyed by threadId+runId. |
| V4 Access Control | partial | `identity_id != local → 403` is authorization-without-authentication (NOT a security control until auth lands — documented as such, amendment #35). Real control = loopback bind. |
| V5 Input Validation | yes | SDK `RunAgentInput.UnmarshalJSON` + Aura-semantic validation in `types.go` (threadId = existing conversation UUID via `conversations.Get`; messages-non-empty). Reject malformed JSON → 400. |
| V6 Cryptography | no | No crypto in this phase. |
| V7 Error Handling | yes | RUN_ERROR events carry `err.Error()` — ensure no DB DSN / API key / internal path leaks into the error string surfaced over the wire (the agent error slot already redacts API keys per D-28; verify tool/infra errors don't carry secrets). |
| V13 API/Web Service | yes | CORS permissive only behind `AURA_AGUI_CORS_PERMISSIVE` (default restrictive); loopback-only bind; no public egress. |

### Known Threat Patterns for {Go SSE HTTP server on loopback}
| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| Unauthenticated agent endpoint exposed beyond loopback | Elevation of Privilege | Default `127.0.0.1` bind; fail-fast on non-loopback `--bind` under `AURA_PRIVACY_MODE=local-only` (Pitfall 6); auth deferred but documented. |
| Slow-client SSE resource exhaustion (slowloris-style) | Denial of Service | Buffered channel cap 64 + drop-with-WARN + RUN_ERROR if persistent; never block the Loop. Server read/write timeouts on `http.Server`. |
| Info leak via RUN_ERROR message | Information Disclosure | Sanitize the error surfaced over the wire — never echo DSNs, keys, or internal paths. The agent path already structurally redacts API keys (D-28); audit the translator's `RunErrorEvent(err.Error())`. |
| Cross-thread access (reading another conversation's history) | Information Disclosure | `threadId` must resolve to an existing conversation; single-user `local` v1 (no cross-identity isolation needed yet, but `Get` is the chokepoint). |
| CSRF on `POST /agent/run` from a malicious local web page | Spoofing/Tampering | Restrictive CORS by default (permissive only behind the explicit dev env flag); loopback-only reduces blast radius. |
| Malformed/oversized request body | Tampering/DoS | `http.MaxBytesReader` on the request body; SDK `UnmarshalJSON` rejects bad shapes → 400. |

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | `NewTextMessageStartEvent` accepts a variadic `WithRole(...)` option (spike 016 uses `WithRole("assistant")`; spike 015 omits it) | Standard Stack / Pattern 1 | LOW — both spike calls compiled live; the option is genuinely variadic. Re-confirm signature on first build. |
| A2 | Coalescing per-token deltas into one message lifecycle is the correct semantics for AG-UI text streaming (vs. one message per Event) | Pitfall 1 | LOW-MED — AG-UI's TEXT_MESSAGE_CONTENT is explicitly a delta stream within one messageId (the SDK rejects empty deltas, confirming streaming intent). But the exact run-boundary policy (final Event END-only vs. suppress deltas) is a design choice the planner should lock. |
| A3 | The minimal 8b server can mount cleanly onto the existing `aura serve` scheduler daemon without a goroutine-lifecycle conflict | Pattern 3 / Validation | LOW — serve already owns a graceful ctx-cancel drain; adding an `http.Server` with `Shutdown` on the same ctx is the standard pattern. Verify goleak stays clean with BOTH the scheduler and the HTTP server running. |
| A4 | `GET /threads/<id>/messages` returning a single MESSAGES_SNAPSHOT (not an SSE stream) satisfies criterion 3 | Code Examples / Validation | LOW — criterion 3 says "shows the persisted turn history"; a JSON MESSAGES_SNAPSHOT body is the natural shape. Confirm whether the operator expects SSE framing or a plain JSON body for the GET. |
| A5 | A `go list -deps` grep script is the accepted boundary-gate mechanism (vs. enabling depguard) | Pitfall 5 / Validation | LOW — the codebase already proves cycle-freedom this exact way (STATE.md 11-02); depguard is not enabled and adding it is heavier. Planner may still prefer depguard for declarative clarity. |

**These assumptions are LOW-risk and spike-grounded; none block planning. A2/A4 are the two design choices the planner should lock explicitly.**

## Open Questions

1. **Text-message run-boundary policy (the one real design decision)**
   - What we know: Real Run streams per-token chunk Events AND a final Event carrying the full answer + FinishReason (`llm_agent.go` consume→chunkEvent then finalEvent).
   - What's unclear: Should the translator (a) stream deltas and emit END-only on the final Event, or (b) suppress deltas and emit one CONTENT from the final Event? Option (a) gives live token streaming (the PRD smoke shows token-per-token); option (b) is simpler but loses streaming.
   - Recommendation: Option (a) — stream deltas (matches the PRD smoke "delta token-per-token"), treat the final Event as END + the usage StateDelta, and DO NOT re-emit the final Content as a CONTENT event. Lock this in the plan.

2. **GET /threads/<id>/messages response shape: SSE or plain JSON?**
   - What we know: Criterion 3 wants the persisted history observable via curl; PRD lists MESSAGES_SNAPSHOT "su GET /threads/<id>/messages (rehydration UI client)".
   - What's unclear: whether the GET returns a single `data: {MESSAGES_SNAPSHOT json}` SSE frame or a plain `application/json` body.
   - Recommendation: plain `application/json` MESSAGES_SNAPSHOT body for the GET (it's a one-shot read, not a stream); reserve SSE for `POST /agent/run`. Cheap to change if the operator wants SSE.

3. **Does `POST /agent/run` actually drive a live agent turn, or replay/stream a pre-run?**
   - What we know: Criterion 1's curl includes `"messages":[...]` (a user message); the natural reading is "run the agent on this message and stream the result."
   - What's unclear: whether the server should append the user message and drive `Runner.Turn(ctx, convID, &userMsg)`, or only stream an already-running turn.
   - Recommendation: drive `Runner.Turn` with the last user message from `RunAgentInput.Messages` (the Runner persists it + drives a fresh agent over rehydrated history). This is the only behavior that makes the curl smoke produce TEXT_MESSAGE/TOOL_CALL events end-to-end.

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| Go toolchain | build/test | ✓ | 1.26.4 (go.mod) | — |
| AG-UI Go SDK module | translator/server | ✗ (reverted post-spike) | — | `go get` the pseudo-version pin (Phase 12 Wave 0 install step) |
| Postgres (compose) | server integration test (conversations) | ✓ (compose) | 17 | — (criterion 1/3 need it; no fallback — must run, no-skip-as-green) |
| OpenRouter API key | live `aura serve` + curl with a REAL agent run | operator-gated | — | FakeClient in the integration test; live curl is the operator Gate-3 checkpoint |
| `goleak`, `rapid`, `httptest` | tests | ✓ | go.mod / stdlib | — |

**Missing dependencies with no fallback:** none (the SDK is a `go get` away; Postgres is in compose).
**Missing dependencies with fallback:** OpenRouter key for the live curl smoke → FakeClient covers the automated tier; the live curl is the operator checkpoint (consistent with every prior phase's live Gate-3 pattern).

## Sources

### Primary (HIGH confidence)
- `spike-findings-Aura/references/agui-gateway.md` — the spike blueprint (pin recipe, SDK surface, translator obligations, SSE framing, resume mapping). Executed live 2026-06-06.
- `sources/016-agui-sse-roundtrip/main.go` — working translator + SSE server seed against REAL `internal/agent` types.
- `sources/015-agui-event-surface/main.go` + `golden-events.json` — verified wire shapes for all 21 emitted events + RunAgentInput camel/snake/resume parse.
- `sources/014-agui-sdk-module-pin/main.go` — module pin resolution + event construct/marshal/validate proof.
- `internal/agent/event.go`, `internal/agent/llm_agent.go`, `internal/agent/llm_agent_events.go`, `internal/agent/llm_agent_finalize.go` — the REAL Event emission pattern (per-token chunks + final Event; tool-result Content overload).
- `internal/conversations/store.go` — `Get`/`LoadHistory`/`ErrConversationNotFound` seams for thread resolution + GET messages.
- `internal/runner/runner.go`, `runner_resume.go`, `interfaces.go` — `Turn`/`SubmitAnswers` orchestration + resume contract.
- `cmd/aura/serve.go`, `chat.go`, `main.go` — the existing `aura serve` daemon, `bootChatEnv` composition root, subcommand dispatch.
- `internal/config/config.go` — `envDefault`/`envBoolDefault` env-registration pattern.
- `.golangci.yml`, `.github/workflows/ci.yml`, `Makefile`, `scripts/check-file-size.sh` — CI/gate infrastructure (no depguard; `go list -deps` boundary technique).
- `prd.md` §Slice 8 (lines 2689-2763) + amendments #35, #56 — the truth-source.
- `.planning/ROADMAP.md` Phase 12, `.planning/REQUIREMENTS.md` UX-01, `.planning/STATE.md` — phase scope, requirement, decisions.

### Secondary (MEDIUM confidence)
- `golang-concurrency` skill — SSE pump leak discipline (ctx.Done in select, only sender closes, goleak).

### Tertiary (LOW confidence)
- none — all claims are spike-verified or codebase-verified.

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH — SDK pinned + spike-proven (014/015/016); every constructor signature verified live.
- Architecture: HIGH — the translator, persistence, orchestration, and daemon seams all EXIST and were read directly; only the chunk-coalescing logic is genuinely new.
- Pitfalls: HIGH — Pitfalls 1/2 derived from reading the REAL emission code (not the synthetic spike); 3/5/6 from amendments + codebase conventions.
- Open questions: the 3 are design choices (run-boundary, GET shape, run-driving), not unknowns — all have clear recommendations.

**Research date:** 2026-06-06
**Valid until:** 2026-07-06 (30 days — the SDK pin is immutable; only Aura's internal Event shape could drift, and it is locked by D-17).
