# Audit: internal/runner

**Verdict:** needs-work — two real correctness bugs (partial-write in `SubmitAnswers`, wire-invalid history after `Stop` with active pauses), one transient goroutine accumulation in `waitWorkers`, plus a minor auto-title coverage gap on the fast path.

**Counts:** critical 0 / high 1 / medium 2 / low 2

---

## Findings

### [HIGH][BUG] runner-1: `SubmitAnswers` partial-write leaves wire-invalid history on `injectAnswer` failure

**Location:** `internal/runner/runner_resume.go:113-120`

**Confidence:** high

**Detail:**
`MarkResumedBatch` is called atomically (one DB transaction) marking all tokens as resumed. If that succeeds but the subsequent per-token `injectAnswer` loop fails midway (e.g. transient `AppendTurn` error), the DB state and conversation_turns diverge:

- `paused_states.resumed_at` is set for ALL tokens (atomically, irreversibly).
- `conversation_turns` has RoleTool answers only for the tokens processed before the failure.
- The remaining tokens' tool_call_ids are left unanswered in the history.

On the next `Turn(convID, nil)` resume, `LoadManagedHistory` reconstructs this partial history. The LLM request carries an assistant message with N tool_calls but fewer than N `tool` responses — a wire-invalid request per OpenAI spec. The model may re-emit `ask_user` for the unanswered calls, creating a duplicate-question loop.

```go
// runner_resume.go:113
if err := r.pause.MarkResumedBatch(ctx, batch); err != nil { // atomic DB update
    return 0, fmt.Errorf("submit answers: %w", err)
}
for token, resp := range answers {
    if err := r.injectAnswer(ctx, pendings[token], resp); err != nil {
        return 0, err // partial write: MarkResumedBatch already committed
    }
}
```

**Suggested fix:**
Inject all `conversation_turns` RoleTool answers BEFORE calling `MarkResumedBatch`. This way, if the injection loop fails, `paused_states` rows are still pending and the caller can retry. Alternatively, wrap the entire operation (inject all + mark batch) in a single DB transaction via `db.WithTx`, which requires the conversation store and pause store to share the same transaction handle.

---

### [MEDIUM][BUG] runner-2: `Stop` leaves wire-invalid history when active pauses exist

**Location:** `internal/runner/runner_resume.go:198-210`

**Confidence:** high

**Detail:**
`Stop` calls `AutoResolveForConversation` which marks `paused_states` rows as auto-resolved but does NOT inject matching `RoleTool` turns into `conversation_turns`. In contrast, `cancelConversation` (called by `SubmitAnswer` with the `cancel` action) injects a `cancelledContent` RoleTool answer for each pending before auto-resolving.

When a conversation is force-terminated (e.g. the CLI REPL exits on EOF while a pause is active), `defer Stop()` fires and only resolves the `paused_states` rows. The `flushPause` defer in `Turn` has already written the assistant tool_call turn(s) to `conversation_turns`. So the history contains orphaned assistant/tool_calls messages with no matching tool responses.

If the same conversation is later resumed via `aura chat resume <id>`, `Turn(convID, nil)` loads this wire-invalid history and the LLM request fails or loops.

**Suggested fix:**
Add a helper `injectCancelledAnswers` (analogous to `cancelConversation`'s injection loop) called from `Stop` before `AutoResolveForConversation`, or delegate to `cancelConversation` directly. The distinction between explicit cancel and force-stop at the wire level is irrelevant — both need matching RoleTool turns.

---

### [MEDIUM][RACE] runner-3: `waitWorkers` goroutine outlives the timeout it is meant to bound

**Location:** `internal/runner/runner_resume.go:216-228`

**Confidence:** high

**Detail:**
The internal comment "A separate goroutine signals completion so the bounded wait never leaks" is inaccurate. When `time.After(timeout)` fires (the `stopTimeout` path), `waitWorkers` returns `false`, but the goroutine launched at line 218 is still blocked on `r.wg.Wait()`. That goroutine will eventually exit when the title workers finish (bounded by `titleTimeout`), but during that window there is a goroutine accumulation that violates the "goleak-clean" claim in the doc comment.

```go
func (r *Runner) waitWorkers(timeout time.Duration) bool {
    done := make(chan struct{})
    go func() {          // <-- leaks until wg drains (up to titleTimeout after Stop returns)
        r.wg.Wait()
        close(done)
    }()
    select {
    case <-done:
        return true
    case <-time.After(timeout):
        return false     // <-- goroutine above is still running
    }
}
```

In `TestStop_WorkerTimeout`, the test sidesteps this by immediately closing `release` after `Stop` returns, so goleak (at package end) sees a clean state. In production, a timeout means `Stop` returns but a goroutine lingers for up to `titleTimeout` (30 s default).

**Suggested fix:**
Use `sync.WaitGroup.Wait` with a context-based cancellation or replace the inner goroutine with a channel that signals on `wg.Wait()`. The simplest fix is to document the known-transient behavior explicitly. A stricter fix is to use `sync.WaitGroup` with a `done` context: `ctx, cancel := context.WithTimeout(context.Background(), timeout); defer cancel()` and pass it to a goroutine that selects between `wg.Wait()` done and `ctx.Done()`, making the inner goroutine's exit deterministic.

---

### [LOW][NOT-WIRED] runner-4: Auto-title worker not fired on fast-path greeting replies

**Location:** `internal/runner/runner.go:207-223`

**Confidence:** high

**Detail:**
The fast-path branch (triggered when `fastReplyFor` matches a greeting like "ciao") returns early after persisting the assistant turn. `maybeAutoTitle` is never called in this branch. The `if !tr.paused { r.maybeAutoTitle(...) }` block is only reachable in the non-fast-path flow.

If a conversation has accumulated seq >= 3 turns and the user sends a greeting, the auto-title worker never fires. The title remains NULL despite the conversation qualifying for auto-titling.

This is likely intentional (greeting replies are poor title candidates), but:
1. The behavior is undocumented.
2. A follow-up non-greeting turn would correctly fire the title worker, so the title is not permanently lost — it's deferred to the next qualifying turn.

**Suggested fix:**
Add a comment explaining why `maybeAutoTitle` is skipped on the fast path. If the intent is to fire the title worker even on greeting turns, move `r.maybeAutoTitle(ctx, convID, history)` after the early return via a labeled block or a deferred call.

---

### [LOW][BUG] runner-5: `localIdentityName` duplicated between `runner` and `cmd/aura`

**Location:** `internal/runner/runner.go:34` and `cmd/aura/serve_channels.go:44`

**Confidence:** high

**Detail:**
The constant `localIdentityName = "local"` is independently defined in both packages. There is no shared package-level constant. If the identity name ever changes (e.g. a migration renames the seeded row), one definition could diverge silently. Both currently resolve to `"local"`, so there is no runtime bug today.

```go
// internal/runner/runner.go:34
const localIdentityName = "local"

// cmd/aura/serve_channels.go:44
const localIdentityName = "local"
```

**Suggested fix:**
Expose the constant from a shared location (e.g. `internal/identity` or a new `internal/identities` package) and import it in both locations. This is a minor refactor but eliminates the duplication risk.

---

## What was checked and found clean

- **Nil pointer derefs:** All event fields (`ev.LLMResponse`, `ev.Actions.AwaitingInput`, `ev.Actions.ToolInvocation`, `ti.StartedAt`, `ti.EndedAt`) are nil-checked before use. `r.toolInvocations == nil` is explicitly guarded with a warn-and-skip. `r.cacheMetrics == nil` returns an error (intentional hard failure).
- **Context propagation:** `context.WithoutCancel` is correctly applied in both `flushPause` (deferred) and the auto-title goroutine. No context is dropped inadvertently.
- **Error wrapping:** All `fmt.Errorf` calls use `%w`. No error is swallowed except for `_ = r.Conv.SetTitleIfNull(...)` (best-effort, documented).
- **Slice aliasing:** `buildAgent` calls `stripLeadingSystem(history)` which returns a sub-slice, but `NewLlmAgent` copies the elements via `append(hist, cfg.UserTurns...)`. The `maybeAutoTitle` goroutine independently copies history via `append([]llm.Message(nil), history...)` (WR-03). No aliasing race.
- **JSON handling:** `anyFloat` correctly handles `json.Number` (the UseNumber decode path for cost_usd). `anyInt` lacks `json.Number` but this is non-reachable in the Runner's direct in-memory path (token counts are native `int` from `usageStateDelta`).
- **Timer/ticker leaks:** `time.After` in `waitWorkers` does not leak the timer on Go 1.23+ (the module is on Go 1.26.4). The `context.WithTimeout` in `maybeAutoTitle` is correctly deferred-cancelled.
- **Goroutine tracking:** `r.wg.Add(1)` / `defer r.wg.Done()` pair in `maybeAutoTitle` is correct. The `wg` is the sole sync point for title workers.
- **Race on history slice:** History is loaded, then passed to `buildAgent` (copies into agent's internal slice) and `maybeAutoTitle` (copies snapshot). No concurrent mutation.
- **`EnsureConversation` TOCTOU:** The check-then-act pattern has a correct recovery path (re-Get after Create failure) for concurrent first-message races.
- **Dead code:** All unexported functions (`fastReplyFor`, `normalizeGreeting`, `fastReplyEvent`, `fastReplyChunkEvent`, `stripLeadingSystem`, `buildAgent`, `contextConfig`, `appendUserTurn`, `waitWorkers`, `maybeAutoTitle`, `persistEvent`, `persistToolInvocation`, `persistAssistantAnswer`, `cacheMetricParams`, `persistPause`, `flushPause`, `assistantAskUserToolCalls`, `pauseOptionsJSON`, `usageFromStateDelta`, `anyInt`, `anyFloat`, `timePtrValue`, `toolInvocationTimestamp`, `toResumeAnswer`, `remainingPending`, `injectAnswer`, `cancelConversation`) are all called within the package or by tests. No dead unexported symbols found.
- **`SubmitAnswers` cancel early-exit:** When a cancel token is encountered mid-loop, `cancelConversation` uses `p.ConversationID` (not the accumulated `convID`) — correct.
