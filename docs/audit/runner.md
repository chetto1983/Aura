# Audit: internal/runner

**Verdict:** needs-work — three real defects (two data-consistency bugs, one goroutine leak with misleading comment), one latent silent-data-loss path.

**Counts:** critical 0 / high 0 / medium 3 / low 1

---

## Findings

### [MEDIUM][BUG] `Stop` skips `AutoResolveForConversation` when inject fails — orphaned `paused_states` rows

**Location:** `internal/runner/runner_resume.go:220-236`
**Confidence:** high

`Stop` is documented to guarantee "zero unresolved rows after". The implementation:

```go
resolveErr := r.injectCancelledAnswers(ctx, convID)
if resolveErr == nil {
    resolveErr = r.pause.AutoResolveForConversation(ctx, convID)
}
```

If `injectCancelledAnswers` returns an error (e.g., an `AppendTurn` failure), `AutoResolveForConversation` is never called. The `paused_states` rows remain unresolved in the DB even though the conversation is forcefully stopped. Any subsequent `ListPending` call will see dangling rows, and future tooling that inspects pending counts gets a corrupted view. `cancelConversation` (line 164-172) has the same pattern — but there the caller re-surfaces the error and can retry. In `Stop`, the caller has no mechanism to complete the resolve.

**Suggested fix:** Always run `AutoResolveForConversation` regardless of inject failure. Accumulate both errors:

```go
resolveErr := r.injectCancelledAnswers(ctx, convID)
if arErr := r.pause.AutoResolveForConversation(ctx, convID); arErr != nil && resolveErr == nil {
    resolveErr = arErr
}
```

---

### [MEDIUM][BUG] `SubmitAnswers` inject loop is non-atomic — partial inject leaves history/state inconsistent on retry

**Location:** `internal/runner/runner_resume.go:112-129`
**Confidence:** high

The inject loop iterates a `map[string]ResponseInput` (non-deterministic order). If `injectAnswer` fails midway, some `RoleTool` turns are already appended to `conversation_turns` but `MarkResumedBatch` has not run, so the corresponding `paused_states` rows remain unresolved. The caller sees an error and may retry with the same full map — re-injecting already-persisted answers, creating duplicate `RoleTool` turns for the same `tool_call_id`. This produces a wire-invalid history (two tool-result messages for one tool_call) that confuses the model on resume.

```go
// Loop 2: inject — can fail at any point
for token, resp := range answers {  // non-deterministic order
    if err := r.injectAnswer(ctx, pendings[token], resp); err != nil {
        return 0, err  // some turns already appended; rows still unresolved
    }
}
// Loop 3: mark resolved — only reached if ALL injects succeed
if err := r.pause.MarkResumedBatch(ctx, batch); err != nil { ... }
```

**Suggested fix:** Track which tokens were successfully injected and skip them on error returns, or — more robustly — validate that each token has no existing `RoleTool` answer before injecting (idempotent upsert pattern). A simpler guard: add a `paused_states` unique constraint on `(conversation_id, tool_call_id)` so a duplicate inject is a no-op at the DB level.

---

### [MEDIUM][BUG] `anyInt` missing `json.Number` case — token counts silently zeroed on JSON-decoded StateDelta

**Location:** `internal/runner/runner_persist.go:334-345`
**Confidence:** medium

`usageFromStateDelta` calls `anyInt` for `prompt_tokens`, `completion_tokens`, and `cache_hit_tokens`. `anyFloat` (used for `cost_usd`) explicitly handles the `json.Number` case with this comment: *"the StateDelta is decoded with UseNumber (event.go), so cost_usd can arrive as a json.Number"*. `anyInt` does not:

```go
func anyInt(v any) int {
    switch n := v.(type) {
    case int:    return n
    case int64:  return int(n)
    case float64: return int(n)
    default:     return 0  // json.Number falls here → silently 0
    }
}
```

Currently, the runner only receives Events in-process from `LlmAgent.Run` (native `map[string]any` with `int` values), so this does not fire today. The risk activates if Events are JSON-round-tripped before `usageFromStateDelta` is called — e.g., an AG-UI replay path or a future serialized-event store. When it does fire, every token count is silently persisted as 0 while `cost_usd` is correctly decoded, resulting in a misleading cache/cost picture.

**Suggested fix:** Add `json.Number` (and `int32`) cases to `anyInt`:

```go
case int32:      return int(n)
case json.Number:
    i, err := n.Int64()
    if err != nil { return 0 }
    return int(i)
```

---

### [LOW][BUG] `waitWorkers` goroutine persists past timeout — comment claims otherwise

**Location:** `internal/runner/runner_resume.go:238-253`
**Confidence:** high

The comment says "the bounded wait never leaks". This is incorrect. When `time.After(timeout)` fires and `waitWorkers` returns `false`, the `go func() { r.wg.Wait(); close(done) }()` goroutine continues running, blocked on `r.wg.Wait()`, until all title workers drain. This is a temporary goroutine leak on every `Stop` timeout. The goroutine is self-healing (exits when workers eventually drain), so goleak only catches it if a test exits while workers are still running — and `TestStop_WorkerTimeout` correctly releases the worker after the assert, keeping goleak clean. In production a hung title worker would hold this goroutine open indefinitely (the title worker is already bounded by `titleTimeout`, so the max duration is `stopTimeout + titleTimeout`).

The design is acceptable (the goroutine is bounded), but the comment misrepresents the behaviour.

**Suggested fix:** Correct the comment: *"On timeout the wg.Wait goroutine persists until workers drain (bounded by their own titleTimeout); the goroutine is self-healing, not permanently leaked."* Optionally replace with a context-cancelled wg pattern to make it truly non-leaking.

---

## What was checked (no findings)

- **Nil-pointer paths:** `ev == nil` guard in `persistEvent`, `r.toolInvocations == nil` fast-exit, `r.cacheMetrics == nil` loud-error — all correct.
- **Context propagation:** `context.WithoutCancel` used correctly in both the title worker and `flushPause` defer to outlive the turn context.
- **Race on `r.wg`:** Only ever accessed from `maybeAutoTitle` (Add+goroutine) and `waitWorkers` (Wait). Correct WaitGroup usage; no races.
- **Fast-path (`runner_fastpath.go`):** Persist + chunk + final event yield sequence is correct; `yield(false)` guard respected.
- **`EnsureConversation` TOCTOU:** The concurrent-creator reconciliation (`Create → fail → re-Get`) is intentional and correctly documented.
- **`flushPause` defer:** Correctly uses `context.WithoutCancel(ctx)` so a turn-cancel cannot abort the durable write the resume depends on.
- **`stripLeadingSystem`:** Correct slice handling; does not alias.
- **`anyFloat` json.Number:** Correctly handled.
- **Dead code:** All unexported symbols are reachable from within the package. No dead exports identified.
- **Not-wired code:** All public methods (`Turn`, `Stop`, `SubmitAnswer`, `SubmitAnswers`, `PendingFor`, `EnsureConversation`, `NewConversation`, `NewConversationWithID`) are wired at `cmd/aura/chat.go` and/or `internal/agui/server.go`. `ResumeHook` is wired at `cmd/aura/serve_adapters.go:267`.
- **`cacheMetricParams(seq=0)`:** The real `conversations.Store.AppendAssistantTurnWithCacheMetric` allocates seq inside the transaction when `p.Seq <= 0`. Correct.
- **`SubmitAnswers` convID undefined:** All tokens are expected to belong to one conversation (API contract). The last-iteration `convID` is used only for `remainingPending` at the end, after all tokens are validated to belong to the same conversation in practice.
