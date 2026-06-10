# Audit: internal/runner

**Verdict:** needs-work — three real defects (one high, two medium) plus one goroutine-leak in the timeout path

**Counts:** critical 0 / high 1 / medium 2 / low 2

---

## Findings

### [HIGH][BUG] persistPause accumulates tracker state before DB write succeeds

**Location:** `internal/runner/runner_persist.go:207-208`
**Confidence:** high

`persistPause` unconditionally sets `tr.paused = true` and appends `ai` to `tr.pauses` at lines 207-208 before any fallible operation. If the subsequent DB insert (`r.pause.Insert`) fails — or even if `uuid.NewV7()` fails — the function returns an error but the tracker already has `paused=true` and the pause payload in `tr.pauses`.

The deferred `flushOnce` in `Turn` then runs (it is not gated on error; it fires on every return path). `flushPause` sees `len(tr.pauses) == 1` and writes an assistant `ask_user` tool_call turn into the conversation history via `r.Conv.AppendTurn`. No corresponding `paused_states` row exists (the insert failed). The conversation now contains an unanswerable assistant tool_call — the model will re-ask on every subsequent resume, creating an infinite pause loop. This is the same failure mode the defer comment (lines 253-261) warns about, triggered by the persist failure rather than a consumer early-return.

The failure requires a transient DB error on `r.pause.Insert` but is not exercised by any test (`pause.insertEr` is declared on `fakePauseStore` but never set in any runner test).

**Suggested fix:** Move `tr.paused = true` and `tr.pauses = append(...)` to after the `r.pause.Insert` call succeeds. If the insert fails, nothing is accumulated in the tracker and `flushPause` is correctly a no-op.

---

### [MEDIUM][BUG] Fast-path turns skip auto-title permanently

**Location:** `internal/runner/runner.go:215-231`
**Confidence:** high

When `fastReplyFor` matches (Italian greeting), `Turn` appends the user turn, persists the assistant fast-reply event, yields the two events, and returns — bypassing the agent loop and the `maybeAutoTitle` call at line 302. `maybeAutoTitle` is only reachable after the agent loop (`if !tr.paused { r.maybeAutoTitle(...) }`). Any conversation whose first assistant turn is a fast-reply will never have its title generated, regardless of how many subsequent turns occur, because the first turn to reach `seq >= 3` is the fast-path turn and `maybeAutoTitle` is never called for it.

Subsequent turns (with a real LLM reply) will call `maybeAutoTitle`, so the title will eventually be set once a non-greeting message is sent. The practical impact is that a conversation starting with "Ciao" and then actual work will be titled off the second reply's history (which may be missing the greeting context), not a permanent nil.

**Suggested fix:** Add a `maybeAutoTitle` call after the fast-path's `yield(ev, nil)`, passing the (minimal) history that is available. Alternatively, reload history after the fast-path persist.

---

### [MEDIUM][BUG] SubmitAnswers partial-inject leaves conversation history inconsistent on retry

**Location:** `internal/runner/runner_resume.go:112-121`
**Confidence:** high

`SubmitAnswers` first injects all RoleTool answer turns (`injectAnswer` loop, lines 112-115) and only then calls `MarkResumedBatch` (line 121). If `injectAnswer` partially succeeds (some tokens injected, the next one errors) or if `MarkResumedBatch` fails after all injections succeed, the paused_states rows remain "pending" while the conversation history already contains some (or all) injected `RoleTool` turns. A caller retry of `SubmitAnswers` with the same token set will append duplicate `RoleTool` turns for the already-injected tokens, producing a wire-invalid history (duplicate tool_call answers for the same `ToolCallID`).

The same issue exists in the single-answer path `SubmitAnswer` (lines 77-80): `injectAnswer` before `MarkResumed`.

**Suggested fix:** The simplest fix is idempotency: before injecting, check if a `RoleTool` turn with the given `ToolCallID` already exists in the history and skip the inject if so. A heavier fix is to move `MarkResumedBatch` before the inject loop so on partial failure the injected turns have no mark to retry against.

---

### [LOW][RACE/LEAK] waitWorkers spawns an orphaned goroutine on timeout

**Location:** `internal/runner/runner_resume.go:241-253`
**Confidence:** high

`waitWorkers` spawns a goroutine to call `r.wg.Wait()` and close `done`. When the `time.After` case fires (timeout path), `waitWorkers` returns `false` and the function exits. The spawned goroutine is still blocked on `r.wg.Wait()` with no cancellation mechanism — it remains live until all title workers eventually call `Done`. In the worst case this is `titleTimeout` (30 s) after `Stop` returns.

The code comment ("A separate goroutine signals completion so the bounded wait never leaks") is incorrect: the goroutine does leak for the remainder of the title workers' lifetime. The goleak test in `TestMain` does not catch this because the test releases the held worker after `Stop` returns.

In addition, `time.After` creates a `time.Timer` that cannot be garbage-collected until it fires; here it fires immediately on the timeout path, so there is no lasting timer leak — only the goroutine.

**Suggested fix:** Use `time.NewTimer` + `defer timer.Stop()` (minor resource improvement). For the goroutine, document the bounded lifetime explicitly or use a context to unblock `wg.Wait` — though `sync.WaitGroup` cannot be cancelled, so the goroutine lifetime is inherently bounded by the title workers.

---

### [LOW][BUG] EnsureConversation swallows the initial Get error unconditionally

**Location:** `internal/runner/runner.go:182-193`
**Confidence:** medium

When `r.Conv.Get` fails (line 183), the code proceeds to `NewConversationWithID` regardless of the error type. If `Get` failed due to a transient infrastructure error (connection reset, pool exhaustion) rather than a "not found" condition, `NewConversationWithID` will also fail for the same reason, then a second `Get` is attempted and also fails. The function then returns the `NewConversationWithID` error, discarding the original `Get` error which is more diagnostic. In production the distinction matters for alerting: a "conversation not found" vs a "db pool exhausted" scenario would show identical error strings.

This is not a correctness bug — the behavior is ultimately correct (error returned) — but the error chain loses the root cause.

**Suggested fix:** Check for `conversations.ErrConversationNotFound` specifically before proceeding to create: `if !errors.Is(err, conversations.ErrConversationNotFound) { return err }`.
