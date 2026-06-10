# Audit: internal/agui

**Verdict:** needs-work — one protocol-correctness bug in Fanout, one latent goroutine leak in the SSE pump, and one minor constant-duplication drift risk.

**Counts:** critical 0 / high 1 / medium 2 / low 1

---

## Findings

### [HIGH][BUG] agui-1: Fanout producer does not stop after converting a source error to RUN_ERROR

**Location:** `internal/agui/fanout.go:85-100`

**Confidence:** high

**Detail:**

After converting a source error to a `RunErrorEvent` (line 86) the producer goroutine falls through to the send loop and then — crucially — re-enters the `for ev, err := range f.source` loop. If the source generator yields additional events after the error (or any future source is written that does not terminate immediately after yielding an error), those events are delivered to subscribers AFTER `RUN_ERROR`, violating the AG-UI run-lifecycle protocol (`RUN_ERROR` must be terminal).

Compare to `streamSSE` (server.go:203-204) which does:
```go
s.pumpSend(ctx, out, events.NewRunErrorEvent(sanitizeErr(err)))
return  // ← stops the producer
```

The Fanout producer lacks the corresponding `return`:
```go
if err != nil {
    ev = events.NewRunErrorEvent(err.Error())
    // ← no return; falls through to send and back to the range loop
}
```

The existing test `TestFanoutSourceErrorYieldsRunError` does not catch this because `sourceError` terminates naturally after `yield(nil, err)` — the source function body ends, so the range loop exits anyway. The bug is latent: any source that yields `(nil, err)` without returning would trigger post-error delivery.

In current production wiring, `Fanout` is always given a `Translate(...)` output, which maps all errors to `(event, nil)` pairs and never propagates errors in the `err` slot. The error branch in the Fanout producer is therefore dead code in production. The bug only fires if Fanout is composed with a raw (non-Translate-wrapped) source.

**Suggested fix:**

```go
if err != nil {
    ev = events.NewRunErrorEvent(err.Error())
    // send RUN_ERROR then stop — it is a terminal frame
    for i, sub := range subs {
        send(ctx, sub, ev, i)
    }
    return
}
```

Or more simply, add `return` after the send loop when `err != nil` was the trigger.

---

### [MEDIUM][BUG] agui-2: Goroutine leak in streamSSE when write error does not cancel the request context

**Location:** `internal/agui/server.go:196-229`

**Confidence:** medium

**Detail:**

When `writer.WriteEventWithType(...)` returns an error (line 222), the consumer goroutine returns (line 223). The comment says "let the producer drain via ctx". For the producer goroutine to unwind, `ctx.Done()` must fire. In practice — a client disconnect — Go's HTTP server cancels `r.Context()`, so this usually works.

The leak scenario: a write error that is NOT caused by a client disconnect (e.g. a buggy ResponseWriter implementation, a test double that returns an error without cancelling ctx). In that case `ctx` stays live, and `pumpSend` for a lifecycle frame (blocking select with only `out <- ev` and `ctx.Done()`) blocks forever on a full channel that nobody is draining. The producer goroutine leaks.

The existing goroutine test (`TestServer_DisconnectClosesPump`) uses `cancel()` before `resp.Body.Close()`, so it always cancels ctx — it does not exercise the write-error-without-ctx-cancel path.

**Suggested fix:** When the consumer returns due to a write error, cancel the per-connection derived context so the producer is guaranteed to unwind regardless of whether the HTTP server cancels `r.Context()`:

```go
ctx, cancel := context.WithCancel(r.Context())
defer cancel()
// ... pass ctx to streamSSE; the deferred cancel fires when handleRun returns,
// covering both the success path and any write-error bail-out.
```

Alternatively: add a dedicated `done` channel that the consumer closes on return, and select on it in `pumpSend`.

---

### [MEDIUM][DEAD-CODE] agui-3: `artifactEventName` internal alias is a maintenance liability

**Location:** `internal/agui/translator.go:23-25`

**Confidence:** high

**Detail:**

```go
const ArtifactEventName = "aura.artifact"           // exported canonical name
const artifactEventName = ArtifactEventName          // package-internal alias
```

The alias was introduced to avoid touching existing translator code after exporting the name. It is referenced only at line 110 of `translator.go` and in two test assertions in `translator_artifact_test.go`. All three references could use `ArtifactEventName` directly. The alias adds an invisible indirection — a reviewer reading line 110 sees `artifactEventName` and must check whether it matches `ArtifactEventName`. If the exported constant is ever renamed, the alias silently tracks it (since it's a const alias, not a string literal), so drift is not possible in Go. The maintenance risk is low but the alias is unnecessary complexity.

Grep confirms zero external references to `artifactEventName` (it is unexported and only appears in `internal/agui`).

**Suggested fix:** Replace the three uses of `artifactEventName` with `ArtifactEventName` and delete the alias constant.

---

### [LOW][BUG] agui-4: `urlUserinfoPattern` does not redact token-as-user with empty password

**Location:** `internal/agui/server.go:381`

**Confidence:** medium

**Detail:**

The pattern `(?i)([a-z][a-z0-9+.-]*://)[^/\s:@]+:[^\s@]+@` requires the password segment `[^\s@]+` to be non-empty (one or more chars). The common Git/GitHub pattern of embedding a PAT as the username with an empty password — `https://ghp_TOKEN:@github.com/owner/repo` — is not redacted:

```
Input:  "error cloning https://ghp_PAT12345:@github.com/owner/repo"
Output: "error cloning https://ghp_PAT12345:@github.com/owner/repo"
// ghp_PAT12345 is NOT redacted
```

This can appear in error strings from git operations, webhook callbacks, or MCP server configuration. The `secretPattern` does not cover `https://` URLs (only postgres/mysql/mongodb/redis/amqp schemes), so this slips through both passes.

The existing `TestSanitizeErr` test does not cover this case.

**Suggested fix:** Change `[^\s@]+` to `[^\s@]*` (zero-or-more) to match the empty-password form, OR add a separate pattern `(?i)([a-z][a-z0-9+.-]*://)[^/\s:@]+:@` for the `token:@host` shape.

---

## Clean

All other aspects checked and found clean:

- **nil dereference**: `ev.LLMResponse` is nil-guarded before every field access; `pumpSend` only called with non-nil events; `redactEvent` type-asserts before mutating.
- **error propagation**: All `conv.Get`/`conv.LoadHistory`/`run.SubmitAnswers` errors are checked; `json.Encoder.Encode` errors are logged (not silently dropped) on the messages endpoint.
- **race conditions**: `Fanout.subs` mutations are mutex-protected; the `started` atomic is consistent with the mutex; `Server.idgen` is read-only after construction; `redactEvent` mutates only events already dequeued by the sole consumer.
- **resource leaks**: `streamSSE` producer goroutine closes `out` via `defer close(out)`; Fanout producer closes all subscriber channels via `defer closeAll(subs)`; both unwind on `ctx.Done()`.
- **CORS logic**: `withCORS` sets ACAO before `next.ServeHTTP`, covering both error responses and the SSE stream.
- **loop-capture**: Go 1.26.4, not applicable.
- **`toolResultCallID` branch precedence**: The StateDelta `tool_call_id` key is always a single-key map (verified in `llm_agent_events.go`); no multi-key StateDelta with `tool_call_id` is ever produced, so the branch never silently drops other keys.
- **`closeRuns` short-circuit**: Both `rs` (reasoning) and `st` (text) state machines are mutually exclusive — proven by the `content` branches' `close(yield)` calls before opening the other; `&&` short-circuit is safe.
- **`tokenPattern` prefix extraction**: `strings.IndexAny(match, " =\t")` correctly identifies the separator for all three token forms (Bearer space, api_key=, token=) and produces the right prefix in each case.
- **Wiring**: `agui.NewServer` is mounted in `cmd/aura/serve.go`; `agui.NewFanout`/`Subscribe` are used in `internal/channels/telegram/agui_subscriber.go`; `agui.ArtifactEventName` is consumed in `internal/channels/telegram/artifact.go`.
