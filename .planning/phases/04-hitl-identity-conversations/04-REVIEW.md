---
phase: 04-hitl-identity-conversations
reviewed: 2026-05-31T00:00:00Z
depth: standard
files_reviewed: 26
files_reviewed_list:
  - cmd/aura/chat.go
  - cmd/aura/chat_render.go
  - cmd/aura/chat_repl.go
  - cmd/aura/identity.go
  - cmd/aura/main.go
  - cmd/aura/paused_states.go
  - internal/agent/event.go
  - internal/agent/llm_agent.go
  - internal/agent/llm_agent_pause.go
  - internal/agent/tools/ask_user.go
  - internal/askuser/store.go
  - internal/config/config.go
  - internal/conversations/context.go
  - internal/conversations/orphan_scan.go
  - internal/conversations/store.go
  - internal/conversations/store_helpers.go
  - internal/conversations/tiktoken.go
  - internal/conversations/title.go
  - internal/db/tx.go
  - internal/identity/store.go
  - internal/llm/config.go
  - internal/runner/interfaces.go
  - internal/runner/runner.go
  - internal/runner/runner_persist.go
  - internal/runner/runner_resume.go
  - scripts/microcompact_smoke.sh
findings:
  critical: 2
  warning: 5
  info: 4
  total: 11
status: issues_found
---

# Phase 4: Code Review Report

**Reviewed:** 2026-05-31T00:00:00Z
**Depth:** standard
**Files Reviewed:** 26
**Status:** issues_found

## Summary

Phase 4 wires the HITL pause/resume + identity + conversation-persistence substrate. The DB layer is disciplined: every query is sqlc-parameterized (the locked FTS `content % $1` / `similarity(content,$1)` is bound, not concatenated), SQLSTATE-based error classification is used throughout (`isUniqueViolation` via `pgErr.Code`), the orphan scan applies the `Lstat` symlink guard before `RemoveAll`, `validateID` rejects path-traversal segments, `db.WithTx` is panic-safe, and the auto-title worker is a Runner-owned WaitGroup goroutine joined by `Stop` with a bounded drain. Cache-stability (never mutate `messages[0]`) and the byte-identical LoadHistory round-trip hold.

However, the **resume wire-correctness invariant (SC-4) is broken on two reachable paths**, both of which leave the persisted history with an assistant `ask_user` tool_call that has no matching `RoleTool` answer. Because `buildAgent` rehydrates that history verbatim and sends it to an OpenAI-compatible provider, the very next `Turn` produces a malformed request (a 400 from the provider): a dangling `tool_calls` entry with no responding `tool` message. These are the two BLOCKERs below. A third correctness issue (two independent `bufio.Reader`s over the same stdin) breaks pause answering under piped/scripted input. The remainder are robustness and quality findings.

## Critical Issues

### CR-01: `cancel` resume leaves a dangling `ask_user` tool_call → next Turn sends wire-invalid history

**File:** `internal/runner/runner_resume.go:55-76` (and `81-117` for the batch variant)
**Issue:** On `ActionCancel`, `SubmitAnswer` calls `AutoResolveForConversation` (which only writes `resumed_answer` rows in `aura.paused_states`) and returns **without injecting any `RoleTool` answer turn**. But the assistant `ask_user` tool_call turn was already persisted in `aura.conversation_turns` by `persistPause` (`runner_persist.go:74-118`). Auto-resolve never touches `conversation_turns`, so the assistant tool_call remains in history permanently with no responding tool message.

The REPL then continues: `renderAndAnswerPauses` returns `remaining == 0` (cancel auto-resolved every pending), so `runUserTurn` (`cmd/aura/chat_repl.go:88-94`) falls through to `driveTurn(ctx, d, nil)`. That calls `Turn(convID, nil)` → `LoadManagedHistory` → `buildAgent` → a fresh `agent.Run` whose `messages` contain an assistant message with `tool_calls:[{ask_user...}]` and **no matching `{role:"tool", tool_call_id:...}`**. OpenAI-compatible providers reject this with a 400. Worse, the dangling pair is durable — every *future* `Turn` on this conversation rehydrates the same invalid history, so cancel permanently corrupts the thread.

This directly violates the stated invariant: "resume must NOT silently re-run the LLM or duplicate the ask_user tool_call (SC-4)" and "mismatch = wire corruption."

**Fix:** On cancel, inject a terminating `RoleTool` answer keyed by the original `tool_call_id` (mirroring `autoTerminatedContent`) so the tool_call is answered before the loop stops, AND signal the REPL to NOT continue the loop. For example:
```go
if resp.Action == askuser.ActionCancel {
    if err := r.pause.AutoResolveForConversation(ctx, pending.ConversationID); err != nil {
        return 0, fmt.Errorf("submit answer (cancel): %w", err)
    }
    // Answer the dangling ask_user tool_call so rehydrated history stays wire-valid.
    if err := r.injectAnswer(ctx, pending, ResponseInput{
        Action: askuser.ActionCancel, Content: "user cancelled the request",
    }); err != nil {
        return 0, err
    }
    return r.remainingPending(ctx, pending.ConversationID)
}
```
and have the REPL treat a cancel as a turn termination (do not call `driveTurn(nil)` afterward). The same fix is required in `SubmitAnswers` (`runner_resume.go:96-101`).

### CR-02: Multi-pause in one round persists N separate assistant tool_call turns → wire-invalid interleaving on resume

**File:** `internal/runner/runner_persist.go:74-118` (`persistPause`) interacting with `internal/agent/llm_agent_pause.go:90-98` (`emitPauses`) and `internal/runner/runner_resume.go:119-141` (`injectAnswer`)
**Issue:** When the model emits two or more `ask_user` calls in a single assistant message, the agent rewrites its in-memory history to **one** assistant message carrying all the ask_user `tool_calls` (`llm_agent.go:175`, `pauseToolCalls`). But the Runner observes **one pause Event per call** (`emitPauses` yields per pause) and `persistEvent`→`persistPause` runs per Event, persisting a **separate assistant turn with a single tool_call for each pause**.

On resume, `injectAnswer` appends each `RoleTool` answer as its own turn after all the assistant turns are already written. The rehydrated message sequence becomes:
```
assistant{tool_calls:[A]}
assistant{tool_calls:[B]}
tool{tool_call_id:A}
tool{tool_call_id:B}
```
The OpenAI wire contract requires each assistant `tool_calls` message to be immediately followed by the `tool` responses for exactly those ids. The second assistant message (B) is followed by a tool message for A, not B — malformed. Even single-pause-per-turn is fine, but the multi-pause path (reachable: `validateOptions`/`detectPause` allow several ask_user calls in one batch, and `emitPauses` is explicitly built to emit several) produces an invalid transcript and a provider 400 on the continuation Turn.

**Fix:** Persist the assistant pause turn ONCE per round with ALL the round's ask_user tool_calls in a single `conversation_turns` row (accumulate the pauses in `turnTracker` and write the combined assistant turn after the round, or persist on the first pause Event and merge subsequent ones), so the rehydrated assistant message matches the agent's single-message rewrite. Ensure `injectAnswer` writes the tool answers grouped immediately after that single assistant turn. Add a 2-pause integration test asserting the rehydrated `messages` are wire-valid (assistant-with-N-tool_calls immediately followed by N tool messages).

## Warnings

### WR-01: Two independent `bufio.Reader`s over the same stdin drop buffered input on pause

**File:** `cmd/aura/chat_repl.go:41` vs `cmd/aura/chat_repl.go:148`
**Issue:** `chatLoop` creates `reader := bufio.NewReader(d.in)` and reads the user line with `ReadString('\n')`. `promptForPause` creates a **second** `bufio.NewReader(d.in)` over the same `d.in`. `bufio.Reader` reads ahead in blocks (up to 4096 bytes), so when stdin is piped/scripted (the documented test harness, `replDeps.in`), the first reader buffers the rest of the input — including the pause answers — and the second reader, reading directly from the underlying stream, sees EOF or the wrong bytes. Pause answering then silently fails or reads garbage. Interactively it is latent (line-buffered TTY) but still incorrect.
**Fix:** Thread a single `*bufio.Reader` through `replDeps`/`runUserTurn`/`renderAndAnswerPauses`/`promptForPause` instead of constructing a new one per pause. Construct it once in `chatLoop` and pass it down.

### WR-02: Title truncation byte-slices UTF-8, can store invalid runes and split multibyte chars

**File:** `internal/conversations/title.go:80` (`content[:perTurnCap]`) and `internal/conversations/title.go:104` (`t[:titleMaxChars]`)
**Issue:** Both truncations slice a `string` by byte offset. A multibyte UTF-8 rune straddling the cap is split, producing an invalid-UTF-8 fragment. The `perTurnCap` cut feeds the LLM prompt (tolerable), but the `titleMaxChars` cut is written to `aura.conversations.title` via `SetTitleIfNull` and then rendered in `aura chat list` — an invalid-UTF-8 title can corrupt tabwriter output and is persisted durably.
**Fix:** Truncate on rune boundaries, e.g.
```go
if len(t) > titleMaxChars {
    r := []rune(t)
    if len(r) > titleMaxChars {
        t = strings.TrimSpace(string(r[:titleMaxChars]))
    }
}
```
(and the same for `perTurnCap`).

### WR-03: Auto-title worker reads `history` concurrently with no copy; future history mutation would race

**File:** `internal/runner/runner_resume.go:18-40` (`maybeAutoTitle`) vs `internal/runner/runner.go:158-191`
**Issue:** `maybeAutoTitle` launches a goroutine that closes over the `history []llm.Message` slice that `Turn` also passed into `buildAgent`/`NewLlmAgent`. Today this is safe only because `NewLlmAgent` copies into its own backing array (`llm_agent.go:68-69`) and `applyL1` returns a copy — so nothing mutates `history` after the goroutine starts. This is an undocumented invariant held by accident: any future change that mutates the `history` slice in-place after `maybeAutoTitle` returns (e.g. an optimization that appends to it) becomes a data race the `-race` detector would only catch if a title worker happens to overlap. The `cmd/aura/chat.go:331` `defer Stop` and the per-turn worker can also overlap across turns sharing the slice header.
**Fix:** Pass a defensive copy of `history` to the goroutine (`hist := append([]llm.Message(nil), history...)`) and document that the worker owns its snapshot, removing the implicit "nobody mutates history" coupling.

### WR-04: Budget-exhausted / escalate turn persists no assistant turn, leaving an unanswered user turn

**File:** `internal/runner/runner_persist.go:34-45` (`persistEvent`)
**Issue:** `persistEvent` only persists on `AwaitingInput` or `FinishReason != ""`. A budget trip (`terminalBudgetEvent`, `llm_agent.go:122`/`224`) emits a terminal Event with `Escalate=true` + `StateDelta` but **no `FinishReason`** and no `AwaitingInput`, so nothing is persisted. The user turn was already written (`appendUserTurn`), so the conversation ends with a user turn and no assistant reply. On the next `Turn`, history rehydrates ending in a user message with no assistant response — acceptable to the wire, but the operator sees a silently dropped answer and the cost/usage of the consumed steps is never recorded against the conversation aggregates.
**Fix:** Persist a synthetic assistant turn (or a system/marker turn) carrying the termination reason from `StateDelta["limit_hit"]` when a terminal escalate Event is observed, so the transcript and aggregates reflect the consumed work. At minimum, surface the termination to the REPL so it is not silently swallowed.

### WR-05: `numericFromFloat` int64 mantissa overflow is silently truncated

**File:** `internal/conversations/store_helpers.go:151-161`
**Issue:** `scaled := f * 1e4; mantissa := big.NewInt(int64(scaled))`. For an absurd/garbage cost (`f` near or above 9.2e14, or `+Inf`/`NaN` propagated from a misbehaving provider usage figure), `int64(scaled)` overflows or is implementation-defined (Go: out-of-range float→int conversion yields an unspecified value). A poisoned `total_cost_usd` delta then folds into the aggregate via `total_cost_usd + $delta`. `numericFromFloat` always returns `nil` error despite the signature suggesting it can fail, so callers cannot detect the bad value.
**Fix:** Validate the input is finite and within `numeric(10,4)` range before constructing the mantissa, returning the (already-plumbed) error on violation:
```go
if math.IsNaN(f) || math.IsInf(f, 0) || f < -1e6 || f > 1e6 {
    return pgtype.Numeric{}, fmt.Errorf("cost %v out of numeric(10,4) range", f)
}
```
Use `math.Round` instead of the manual `±0.5` to avoid the boundary edge cases.

## Info

### IN-01: `usageFromStateDelta` / `anyInt` / `anyFloat` duplicated across two packages

**File:** `cmd/aura/chat_render.go:152-189` and `internal/runner/runner_persist.go:165-202`
**Issue:** The three helpers are byte-for-byte duplicated (the runner copy even documents the duplication "so the runner package does not import cmd/aura"). This violates the project "REUSABLE CODE / never duplicate; extract a helper" rule. Drift between the two copies would cause the persisted cost and the displayed cost footer to diverge.
**Fix:** Extract `usageFromStateDelta` into a small shared package (e.g. `internal/agent` next to where the StateDelta is produced, or `internal/llm`) and have both consumers call it.

### IN-02: Budget-trip turn loses the user message context if not persisted (see WR-04) — observability gap

**File:** `internal/runner/runner.go:189-191`
**Issue:** `maybeAutoTitle` is correctly skipped on pause (`!tr.paused`), but a budget-exhausted round still calls `maybeAutoTitle` with the pre-round `history` (no assistant turn was added), so a title may be generated from an incomplete exchange. Minor — best-effort title — but worth noting the title can be generated for a turn that produced no answer.
**Fix:** Also skip `maybeAutoTitle` when the round terminated via escalate/budget without a persisted assistant answer.

### IN-03: `chatNew` ignores its args; `aura chat new <anything>` silently no-ops the extra args

**File:** `cmd/aura/chat.go:255` (`func chatNew(_ []string)`)
**Issue:** `chatNew` discards its argument slice entirely. `aura chat new --foo` silently ignores `--foo` rather than erroring on an unknown flag. Low impact (the subcommand takes no options today) but inconsistent with the other subcommands that validate their args.
**Fix:** Either reject unexpected args with the usage line, or document that `new` takes none.

### IN-04: `detectPause` swallows non-sentinel validation errors silently during pause detection

**File:** `internal/agent/llm_agent_pause.go:71-82`
**Issue:** `detectPause` runs `tool.Execute` and only reports a pause when the error is the `*ErrAwaitingUserInput` sentinel; any *other* error (a malformed ask_user that fails validation) returns `(nil, false)` and the call falls through to normal dispatch — which re-executes `ask_user` in `runTool` and surfaces the same validation error as a `RoleTool` message. So a malformed `ask_user` is executed twice (once in detection, once in dispatch). `ask_user.Execute` is pure (no side effects), so this is harmless today, but the double-execution is a latent footgun if `ask_user` ever gains side effects, and it is undocumented.
**Fix:** Note the deliberate double-execution in the comment, or cache the detection result so dispatch reuses it rather than re-running `Execute`.

---

_Reviewed: 2026-05-31T00:00:00Z_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: standard_
