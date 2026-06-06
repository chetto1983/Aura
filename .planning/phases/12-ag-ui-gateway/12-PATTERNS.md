# Phase 12: AG-UI Gateway - Pattern Map

**Mapped:** 2026-06-06
**Files analyzed:** 11 (5 new source + 2 new tests + 2 new scripts + 2 diffs)
**Analogs found:** 11 / 11 (every file has a strong in-repo analog; the only genuinely new logic is the translator state machine, and even that has a 1:1 consumer analog in `cmd/aura/chat_render.go`)

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|-------------------|------|-----------|----------------|---------------|
| `internal/agui/translator.go` | service (pure transform) | streaming (iter.Seq2 1:N) | `cmd/aura/chat_render.go` `renderRunnerTurn` | exact (same Event-stream consumer, same coalescing/disambiguation branches) |
| `internal/agui/types.go` | model (validation) | request-response | `internal/runner/interfaces.go` (narrow consumer types) + `internal/conversations` validation | role-match |
| `internal/agui/fanout.go` | service (pub-sub) | event-driven / pub-sub | `internal/llm/openai_compat/client.go` `Stream` (buffered-chan + select pump) | role-match (backpressure pump) |
| `internal/agui/client.go` | service (in-proc subscriber) | streaming | `internal/llm/openai_compat/client.go` `Stream` emit closure | role-match |
| `internal/agui/server.go` | controller (HTTP) | request-response + streaming (SSE) | `cmd/aura/serve.go` (daemon) + `internal/llm/openai_compat/client.go` (SSE direction-flip) | role-match |
| `internal/agui/translator_test.go` | test (property + golden) | — | `internal/swarm/swarm_property_test.go` (rapid) + `internal/llm/openai_compat/sse_test.go` (golden fixtures) | exact |
| `internal/agui/server_test.go` | test (integration + goleak) | — | `internal/llm/openai_compat/client_test.go` (httptest + disconnect + goleak baseline) + `internal/runner/main_test.go` (goleak TestMain) | exact |
| `cmd/aura/serve.go` (diff) | controller (CLI daemon) | request-response | self (`runServe`/`bootServe` — add `http.Server` alongside scheduler) | exact (in-place extension) |
| `cmd/aura/main.go` (no diff expected) | — | — | `serve` already dispatched (line 75) | n/a |
| `internal/config/config.go` (diff) | config | — | self (`loadBase` `envDefault`/`envBoolDefault` literal) | exact |
| `scripts/agui_boundary_check.sh` (new) | config (CI gate) | — | `scripts/check-file-size.sh` (git ls-files + loop + exit codes) | role-match (no `go list -deps` script exists yet — genuinely new technique) |
| `.github/workflows/ci.yml` (diff) | config (CI) | — | self (`integration-test` job env block; `cache-invariant` Postgres-free job for the grep/boundary gates) | exact |

## Pattern Assignments

### `internal/agui/translator.go` (service, streaming pure transform)

**Analog:** `cmd/aura/chat_render.go` (`renderRunnerTurn`, lines 30-79) — THE definitive analog. It is the existing in-process consumer of `runner.Turn`'s `iter.Seq2[*agent.Event, error]` and already implements the exact branch logic the translator must mirror (Pitfalls 1 & 2). The translator is the same consumer with AG-UI event emission swapped in for terminal rendering.

**Event-stream consumption + branch order** (`chat_render.go:36-77`) — copy the branch ORDER and disambiguation conditions verbatim; the translator just yields `events.Event` instead of writing prose:
```go
for ev, runErr := range seq {
    if runErr != nil { /* → RUN_ERROR, stop */ }
    if ev == nil { continue }
    if ev.Actions.AwaitingInput != nil { /* → RUN_FINISHED(interrupt), stop */ continue }
    if ev.LLMResponse == nil { continue }
    resp := ev.LLMResponse
    switch {
    case len(resp.ToolCalls) > 0 && !isTerminalToolCall(resp.ToolCalls):
        // → TOOL_CALL_START / ARGS / END  (NOT text_response — that is terminal prose)
    case resp.FinishReason != "":
        // final Event: close the text run (END-only — DO NOT re-emit Content as CONTENT)
        // + read usage off StateDelta for STATE_DELTA (usageFromStateDelta, line 156)
    case isToolResultPreview(ev):
        // → TOOL_CALL_RESULT  (NOT a TEXT_MESSAGE — Pitfall 2)
        continue
    case resp.Content != "":
        // streamed per-token chunk → TEXT_MESSAGE_START(once)/CONTENT(per delta)
    }
}
```

**Tool-result disambiguation marker** (`chat_render.go:114-117`) — this is the Pitfall-2 guard, copy the predicate exactly:
```go
func isToolResultPreview(ev *agent.Event) bool {
    _, ok := ev.Actions.StateDelta["tool_call_id"]
    return ok
}
```
`toolResultEvent`/`toolPreviewEvent` set `LLMResponse.Content = run.Preview` AND stamp `Actions.StateDelta["tool_call_id"]`. Branch on this marker → `TOOL_CALL_RESULT`, never `TEXT_MESSAGE_CONTENT`.

**Terminal-vs-activity tool-call disambiguation** (`chat_render.go:121-128`) — `text_response` is the loop-terminating tool whose args ARE the prose, not a tool call to surface:
```go
func isTerminalToolCall(calls []llm.ToolCall) bool {
    for i := range calls { if calls[i].Function.Name == "text_response" { return true } }
    return false
}
```

**Chunk-coalescing (Pitfall 1) — the genuinely new logic** the analog handles via a `strings.Builder` + `flushRemainder` (lines 31-34, 94-106). The translator's twist: instead of accumulating into a buffer, it must run a `messageId` lifecycle state machine — `TEXT_MESSAGE_START` on the FIRST non-empty delta of a contiguous assistant run, `TEXT_MESSAGE_CONTENT` per non-empty delta (SKIP empty deltas — `Validate()` rejects them, Anti-Pattern), `TEXT_MESSAGE_END` when the run ends (tool call / state-delta / final Event with `FinishReason` / stream end). The `flushRemainder` prefix-dedup logic (line 99 `strings.HasPrefix(finalAnswer, already)`) is the proof that the final Event's Content is a superset of the streamed deltas → the locked policy (OQ1 / A2) is **stream deltas, final Event = END-only, do NOT re-emit Content**.

**Spike-proven translator seed** (`spike-findings-Aura/sources/016-agui-sse-roundtrip/main.go` `translate()`, quoted in RESEARCH Pattern 1, lines 160-171) — the ~60-LOC skeleton with `events.NewRunStartedEvent` first / `NewRunFinishedEventWithOptions(...WithSuccessOutcome())` last. CRITICAL: the spike's `synthSeq()` emits one whole-content Event per message; the REAL `(*LlmAgent).Run` streams per-token. Use the spike for the RUN_STARTED/FINISHED framing and SDK constructor signatures; use `chat_render.go` for the per-token coalescing the spike does NOT exercise.

**Imports pattern** (`internal/agent/event.go:1-12`, `cmd/aura/chat_render.go:15-23`) — `internal/agui` imports `internal/agent` (Event type) + the SDK `pkg/core/events`,`pkg/core/types`. NEVER the reverse (CI-enforced boundary).

---

### `internal/agui/types.go` (model, request-response validation)

**Analog:** `internal/runner/interfaces.go` (lines 29-84) — the consumer-declared narrow-interface convention (D-A2-02 "accept interfaces, return structs"). `types.go` declares ONLY the narrow surface `server.go` consumes from `conversations.Store` + `runner.Runner`, satisfied implicitly by the concrete structs, so unit tests pass in-memory fakes.

**Narrow-interface pattern to copy** (`interfaces.go:34-48`):
```go
// ConversationStore is the narrow conversation surface the gateway consumes (D-A2-02).
// *conversations.Store satisfies it implicitly.
type ConversationStore interface {
    Get(ctx context.Context, conversationID string) (conversations.Conversation, error)
    LoadHistory(ctx context.Context, conversationID string) ([]llm.Message, error)
}
```

**Aura-semantic validation** — RESEARCH says SDK owns `RunAgentInput.UnmarshalJSON` (camel+snake); `types.go` only adds threadId-non-empty + messages-non-empty checks. The validation-before-DB-round-trip posture is `conversations/store.go:157-170` (`Get` maps a missing row to `ErrConversationNotFound`); reuse that error for the 404, do not re-validate the UUID shape (`Get` already does via `parseUUID`).

---

### `internal/agui/fanout.go` (service, pub-sub) + `internal/agui/client.go` (service, in-proc subscriber)

**Analog:** `internal/llm/openai_compat/client.go` `Stream` (lines 108-130) — the canonical buffered-channel pump with `select{ case out<-: case <-ctx.Done(): }` non-blocking send, `defer close(out)` (sole sender closes), and a producer goroutine that drains the source and exits on ctx-cancel. This is EXACTLY the Pattern-4 backpressure shape (cap 64, drop+WARN, never block the Loop) and the Pitfall-4 leak discipline.

**Buffered-channel + select-with-ctx.Done pump** (`client.go:108-130`) — copy the structure; for fanout, add a `default:` arm (drop+WARN) since RESEARCH Pattern 4 wants drop-on-full (the LLM client blocks-or-cancels because it has one consumer; fanout must NOT block one slow subscriber):
```go
out := make(chan llm.Chunk, chunkBuffer)   // → cap 64 per RESEARCH
go func() {
    defer close(out)               // sole sender closes (golang-concurrency #5)
    defer func() { _ = resp.Body.Close() }()
    emit := func(ch llm.Chunk) bool {
        select {
        case out <- ch:
            return true
        case <-ctx.Done():         // → Pitfall 4: always a ctx.Done() arm
            return false
        }
    }
    ...
}()
return out, nil
```
For `fanout.go`, the drop policy adds a third `default:` arm:
```go
select {
case sub <- ev:
case <-ctx.Done():
    return
default:
    slog.Warn("agui fanout: subscriber slow, dropping event", "type", ev.Type())
}
```
`chunkBuffer` is the named cap constant convention (mirror as a package const `fanoutBuffer = 64`).

**Note:** `fanout.go` is NOT on the HTTP path (RESEARCH diagram line 134 — it serves the Phase 13 Telegram consumer). Keep it a standalone in-process `Fanout` struct wrapping `iter.Seq2` → N subscriber chans; `server.go` does NOT route through it.

---

### `internal/agui/server.go` (controller, request-response + SSE)

**Analog A (daemon lifecycle):** `cmd/aura/serve.go` (lines 49-97) — the graceful SIGTERM-drain daemon. Phase 12 adds an `http.Server` that shares `bootServe`'s composition root and is `Shutdown` on the same ctx-cancel (RESEARCH Pattern 3, Assumption A3).

**Daemon-mount skeleton** (extend `runServe`/`bootServe`, `serve.go:49-97`) — the existing `signal.NotifyContext(... SIGTERM)` + `defer env.close()` graceful path is reused; add per RESEARCH Pattern 3:
```go
srv := &http.Server{Addr: cfg.AGUIBind, Handler: agui.NewServer(env.run, env.conv).Mux()}
go func() { if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) { slog.Error(...) } }()
// on shutdown (after scheduler.Start returns):
ctxShut, cancel := context.WithTimeout(context.Background(), 10*time.Second); defer cancel()
_ = srv.Shutdown(ctxShut)
```

**Analog B (SSE writing direction):** `internal/llm/openai_compat/client.go` (lines 80-130) READS SSE; `server.go` WRITES it. The framing is delegated to the SDK `sse.SSEWriter.WriteEventWithType` (RESEARCH Pattern 2, Don't-Hand-Roll) — do NOT hand-roll `fmt.Fprintf`. The `http.ResponseWriter` flusher seam mirrors the test server's `w.(http.Flusher).Flush()` (`client_test.go:97-99`).

**404 + thread resolution** (RESEARCH Code Examples, verified `conversations/store.go:157-170`):
```go
conv, err := srv.conv.Get(r.Context(), in.ThreadID)
if errors.Is(err, conversations.ErrConversationNotFound) {
    http.Error(w, "thread not found", http.StatusNotFound); return
}
```

**Driving the agent turn** (OQ3 recommendation, verified `runner/runner.go:177` + `cmd/aura/chat_render.go:36`) — `POST /agent/run` drives `Runner.Turn(ctx, convID, &userMsg)` with the last user message from `RunAgentInput.Messages`, then translates+streams the returned `iter.Seq2`. Resume maps `RunAgentInput.Resume[]` → `Runner.SubmitAnswers` (`runner/runner_resume.go:89`) BEFORE the `Turn`.

**GET /threads/<id>/messages** (RESEARCH Code Examples + OQ2 recommendation = plain JSON, verified `store.go:363` `LoadHistory`) — project `[]llm.Message` → `events.Message`, emit `NewMessagesSnapshotEvent` as an `application/json` body (one-shot read, not SSE).

**stdlib mux method-pattern routing** (RESEARCH Don't-Hand-Roll) — `http.ServeMux` with `"POST /agent/run"` + `"GET /threads/{id}/messages"`; no chi/gorilla (matches the no-router codebase posture; `main.go`/`serve.go` use no router).

---

### `internal/agui/translator_test.go` (test, property + golden)

**Analog A (rapid property harness):** `internal/swarm/swarm_property_test.go` (lines 1-89) — the exact `rapid.Check` + `goleak.VerifyNone` shape. Draw random `[]*agent.Event` sequences (length-bounded, like `rapid.IntRange(1,8)`), feed `Translate`, assert every emitted `events.Event` passes `.Validate()` + the sequence invariants (RUN_STARTED first, RUN_FINISHED last, non-empty ids/deltas, sorted StateDelta keys).

**Rapid harness skeleton to copy** (`swarm_property_test.go:18-24`):
```go
func TestTranslatorProperty(t *testing.T) {
    defer goleak.VerifyNone(t)
    rapid.Check(t, func(rt *rapid.T) {
        n := rapid.IntRange(1, 20).Draw(rt, "events")
        // build a random []*agent.Event mix (chunk / tool / state-delta / final / pause) ...
        // for ev := range Translate(threadID, runID, seqOf(evs)) { assert ev.Validate() }
    })
}
```

**Analog B (golden-fixture loading):** `internal/llm/openai_compat/sse_test.go` (lines 16-31) — the `os.ReadFile(filepath.Join("testdata", name))` deterministic replay helper. Seed `testdata/golden-events.json` from spike 015 (`sources/015-agui-event-surface/golden-events.json`); load + compare wire shapes for the 21 emitted types.

**Golden-load helper to copy** (`sse_test.go:16-31`):
```go
func loadGolden(t *testing.T, name string) []byte {
    t.Helper()
    data, err := os.ReadFile(filepath.Join("testdata", name))
    if err != nil { t.Fatalf("read fixture %s: %v", name, err) }
    return data
}
```

---

### `internal/agui/server_test.go` (test, integration + goleak)

**Analog A (goleak TestMain):** `internal/runner/main_test.go` (lines 15-17) — the per-package `goleak.VerifyTestMain(m)` convention. Add a `main_test.go` (or top of `server_test.go`) with the same TestMain so the SSE pump goroutine leak (Pitfall 4) is asserted.

**goleak TestMain to copy verbatim** (`runner/main_test.go:15-17`):
```go
func TestMain(m *testing.M) {
    goleak.VerifyTestMain(m)
}
```

**Analog B (httptest + client-disconnect + baseline goroutine assertion):** `internal/llm/openai_compat/client_test.go` (lines 92-144) — the canonical pattern for proving a streaming server/pump exits on client disconnect. It captures `runtime.NumGoroutine()` baseline, drives a stream, cancels, and asserts the channel closes + goroutine count returns to baseline. `server_test.go` flips this: `httptest.NewServer(agui.NewServer(...).Mux())`, `curl`-equivalent client, cancel mid-SSE, assert the pump goroutine exits (Pitfall 4 / SC pump).

**Disconnect-and-assert skeleton** (`client_test.go:92-125`):
```go
srv := httptest.NewServer(handler)
defer srv.Close()
baseline := runtime.NumGoroutine()
ctx, cancel := context.WithCancel(context.Background())
// ... receive first SSE event to confirm live ...
cancel()
// ... assert stream ends within ~100ms + goroutines return to baseline ...
```

**Integration tier (db_integration, no-skip-as-green):** the server test needs Postgres for `conversations` (SC1/SC3). Follow the `db_integration` build-tag + `envOrSkip`-that-`t.Fatal`s-under-`$CI` pattern (CLAUDE.md + `ci.yml:114-165` integration-test job). A sub-second runtime is a skip tell.

---

### `cmd/aura/serve.go` (diff — daemon mount)

**Analog:** self. Extend `bootServe` (line 81) to also build `agui.NewServer(...)` from the already-composed `chat.run` + `chat.conv`, and `runServe` (line 49) to start/Shutdown the `http.Server` in the existing graceful path. The `serveEnv` struct (line 39) embeds `*chatEnv` which already exposes `run` + `conv` — no new composition. `aura serve` is already dispatched in `main.go:75` (no `main.go` diff needed).

**`--bind` guard (Pitfall 6):** RESEARCH recommends hardcoding loopback for this phase (defer the flag). If a flag lands, fail-fast on non-loopback under `AURA_PRIVACY_MODE=local-only` (parse host → `net.IP.IsLoopback`) — mirror the existing config boot-fail posture.

---

### `internal/config/config.go` (diff — AURA_AGUI_* fields)

**Analog:** self (`loadBase`, lines 133-218). Append fields to the `Config` struct (near line 99) + the literal in `loadBase` (near line 217), using the existing `envDefault`/`envBoolDefault` helpers (lines 316-353). RESEARCH Code Examples (verified `config.go:133-203`):
```go
// struct fields (add to Config, follow the Web/Skills block comment convention):
AGUIBind           string // AURA_AGUI_BIND — loopback-only HTTP bind (Pitfall 6)
AGUICORSPermissive bool   // AURA_AGUI_CORS_PERMISSIVE — dev-only permissive CORS (default restrictive)
// loadBase() literal (append to the returned &Config{...}):
AGUIBind:           envDefault("AURA_AGUI_BIND", "127.0.0.1:9080"),
AGUICORSPermissive: envBoolDefault("AURA_AGUI_CORS_PERMISSIVE", false),
```
Naming follows `AURA_<DOMAIN>_<UNIT>` (CLAUDE.md §Env vars). Non-fatal defaults (a bad value falls back, never boots fatal — `envDefault`/`envBoolDefault` semantics).

---

### `scripts/agui_boundary_check.sh` (new — CI boundary gate, SC2)

**Analog:** `scripts/check-file-size.sh` (lines 1-58) — the shell-gate convention (shebang + `set -euo pipefail` + documented exit codes 0/1/2 + a clear failure message pointing at the rule). NO existing `go list -deps` script exists (`Grep scripts/ "go list"` = 0 hits), so the TECHNIQUE is new but the SCRIPT SHAPE is `check-file-size.sh`.

**Boundary-check core** (RESEARCH Pitfall 5 — assert the transitive CLOSURE, not a source grep):
```bash
set -euo pipefail
if go list -deps ./internal/agent/... | grep -q 'internal/agui'; then
  echo "BOUNDARY VIOLATION: internal/agent transitively imports internal/agui" >&2
  echo "The translator boundary is one-way (agui imports agent, NEVER reverse, D-17)." >&2
  exit 1
fi
echo "agui-boundary: internal/agent closure is free of internal/agui."
```
Mirror `check-file-size.sh`'s exit-code discipline + the friendly remediation line.

---

### `.github/workflows/ci.yml` (diff — 3 additions, SC2/SC4 + integration tier)

**Analog:** self. Three additions, each with an in-file analog:
1. **go.mod pin grep (SC4)** — add to a Postgres-free job (the `cache-invariant` job at line 68 is the template for a lightweight grep gate): `grep -F 'v0.0.0-20260514093510-e9e910b230b9' go.mod`. Pitfall 3: grep the pseudo-version LITERAL, never the 40-hex SHA.
2. **boundary check (SC2)** — `run: bash scripts/agui_boundary_check.sh` in the `build-and-lint` job (line 19, alongside `check-file-size.sh` at line 38-39 — same `bash scripts/*.sh` shape).
3. **agui db_integration tier** — add `./internal/agui/...` to the `integration-test` job's `go test -tags db_integration` step (line 165). The job already exports `AURA_DB_URL`/`AURA_DB_MIGRATE_URL`/`CI=true` (lines 122-142) — reuse them; keep `-p 1` (shared Postgres advisory-lock rule, line 165 comment).

## Shared Patterns

### Narrow consumer interfaces (D-A2-02)
**Source:** `internal/runner/interfaces.go:29-84`
**Apply to:** `internal/agui/types.go` + `internal/agui/server.go` — declare only the `conversations.Store`/`runner.Runner` methods the gateway calls; concrete structs satisfy implicitly; unit tests use in-memory fakes (supports the 85% floor without DB).

### Buffered-channel + select-with-ctx.Done pump (Pitfall 4)
**Source:** `internal/llm/openai_compat/client.go:108-130`
**Apply to:** `internal/agui/fanout.go`, `internal/agui/client.go`, and the `server.go` SSE pump — sole sender `defer close`s; every send is a `select` with a `<-ctx.Done()` arm (and a `default:` drop arm for fanout). `goleak` proves exit on disconnect.

### goleak per-package leak discipline
**Source:** `internal/runner/main_test.go:15-17` (TestMain) + `internal/swarm/swarm_property_test.go:19` (inline `defer goleak.VerifyNone(t)`)
**Apply to:** `internal/agui/server_test.go` (TestMain) + `translator_test.go` (inline in the rapid block).

### Error wrapping + sentinel mapping
**Source:** `internal/conversations/store.go:157-170` (`Get` → `ErrConversationNotFound` via `errors.Is`)
**Apply to:** `server.go` 404 path. RESEARCH Security V7: sanitize the error surfaced in `RunErrorEvent(err.Error())` — never echo DSN/key/internal path (the agent path structurally redacts keys per D-28; audit tool/infra errors).

### envDefault/envBoolDefault config registration
**Source:** `internal/config/config.go:316-353` (helpers) + the `loadBase` literal (lines 159-217)
**Apply to:** the `AURA_AGUI_*` field additions. Non-fatal fallbacks; `AURA_<DOMAIN>_<UNIT>` naming.

### Shell CI-gate shape (shebang + set -euo + exit codes + remediation line)
**Source:** `scripts/check-file-size.sh:1-58`
**Apply to:** `scripts/agui_boundary_check.sh`.

### Postgres-free CI grep/gate job
**Source:** `.github/workflows/ci.yml:68-94` (`cache-invariant` job: no docker, `CI: "true"`, runs a script + a negative-test script)
**Apply to:** the SC4 go.mod pin-grep step (lightweight, no stack). Consider a paired negative assertion (proves the grep isn't silently green) mirroring `cache_invariant_negative_test.sh` if cheap.

## No Analog Found

| File | Role | Data Flow | Reason |
|------|------|-----------|--------|
| (none) | — | — | Every Phase-12 file maps to a strong in-repo analog. The single net-new TECHNIQUE is `go list -deps` (boundary gate) — but the script SHAPE follows `check-file-size.sh`, and the invariant is the same closure-freedom proof STATE.md 11-02 already used manually for `internal/agent/tools` ⇸ `internal/skills`. The SDK module itself is new but is an external dep (`go get`), not a file Phase 12 authors. |

## Metadata

**Analog search scope:** `cmd/aura/`, `internal/agui/` (empty — greenfield), `internal/agent/`, `internal/runner/`, `internal/conversations/`, `internal/llm/openai_compat/`, `internal/swarm/`, `internal/config/`, `scripts/`, `.github/workflows/`, `go.mod`
**Files scanned (read in full or targeted):** 14 source/test/config + 1 workflow + targeted greps across ~20 more
**Key verified facts:** AG-UI SDK NOT yet in `go.mod` (Wave-0 `go get`); no existing `go list -deps` script; `runner.Turn` returns `iter.Seq2[*agent.Event, error]` (runner.go:177); `chat_render.go` is the 1:1 translator consumer analog; `openai_compat/client.go` Stream is the backpressure-pump analog; `serve.go` is the daemon-lifecycle analog.
**Pattern extraction date:** 2026-06-06
