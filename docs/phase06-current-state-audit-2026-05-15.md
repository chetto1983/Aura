# Phase 6 — Tool Experience Loop: Current-State Audit

Date: 2026-05-15
Status: read-only ground-truth audit for the Phase-J planner.
Goal of Phase 6 (per `prd.md` §6): "Aura improves from preventable tool-call failures instead of repeating them."

Every claim is cited at `file:line`. No speculation about future shape — that is reserved for the planning slice.

---

## A. Preventable failures Aura already mitigates

| # | Failure mode | Mitigated? | Mechanism | Citation |
|---|---|---|---|---|
| 1 | In-batch dedupe (same `(name, args)` twice in the same LLM response) | Yes | `DedupeToolCalls` keeps first call, returns later identicals as duplicates; can be disabled via `Options.DisableInBatchDedup` | `internal/agent/dedupe.go:12-25`; `internal/agent/loop.go:399-408` |
| 2 | Sticky cross-iteration dedupe (same call repeated on a later iteration of the same Run) | Partial | `seenToolCalls` map keyed by `name + canonical(args)` rejects identical reissue UNLESS the prior result text begins with `"Error:"` (retryable) — see `IsRetryableToolResult` | `internal/agent/loop.go:218,430`; `internal/agent/loop.go:63-69` |
| 3 | Per-tool budget cap (`MaxCallsPerTool`) | Yes | When `toolCallExecutions[name] >= max`, call is diverted to duplicate path with a stub | `internal/agent/loop.go:112,433-437`; stub text at `loop.go:624-626` |
| 4 | Total tool-call budget (`MaxToolCalls`) | Yes | Global counter `globalToolCallsExecuted`; when reached, remaining calls in the batch get a `budgetCapToolResult` stub AND the loop flips to `finalizing` mode for the next iteration with `tools = nil` so the LLM must produce final prose | `internal/agent/loop.go:98,221,438-442,519-530`; finalizing branch at `loop.go:281-283`; stub at `loop.go:628-630`; final instruction at `loop.go:632-634` |
| 5 | LLM-level retry on TRANSIENT errors | Yes | `RetryClient.Send` / `.Stream` jittered exponential backoff, default `MaxRetries=5`, base 1s capped 30s, restores caller temperature on each retry | `internal/llm/retry.go:73-108`; `Stream` `retry.go:116-151`; defaults `retry.go:38-47` |
| 6 | LLM-level retry on CONTENT errors (schema-validation, parse failures) | Yes | `MaxContentRetries=3`, staircase `ContentTemperatures=[0.0, 0.3, 0.7]`, validation nudge appended as `system` message; no backoff sleep | `internal/llm/retry.go:97-105,140-148`; nudge builder `retry.go:174-183` |
| 7 | Phantom-tool detection (assistant claims it used a tool it didn't invoke this turn) | Yes | `PhantomToolGuard.LooksPhantom` requires (a) tool name appears as bare word outside backticks AND (b) past-tense first-person performative verb within 120 chars BEFORE the name; up to `RetriesAllowed()` (default 1) corrections per turn via injected user-side `CorrectionText()` | `internal/agent/phantom_guard.go:90-123`; performative-window 120 at `phantom_guard.go:131`; `loop.go:327-346` wiring; verbs list `phantom_guard.go:222-245`; correction text `phantom_guard.go:262-273` |
| 8 | Orphan tool_result (tool message whose `tool_call_id` matches no assistant `tool_calls`) | Yes | `governance.dropOrphanToolResults` removes them pre-LLM | `internal/agent/governance/governance.go:83-113` |
| 9 | Missing tool_result (assistant announced N calls, fewer results follow) | Yes | `governance.backfillMissingToolResults` inserts `[Tool result unavailable — call was interrupted or lost]` stub | `internal/agent/governance/governance.go:117-165` |
| 10 | Stale-context bloat from repeated `file` / `execute_code` / `execute_shell` / `web` / `search_memory` results | Yes | `governance.microcompactToolResults` keeps the most recent `MicrocompactKeepRecent=10`, replaces older copies with `[<name> result omitted from context]` if `len >= MicrocompactMinChars=2000` | `internal/agent/governance/governance.go:32-62,169-212` |
| 11 | Oversized single tool result | Yes | `governance.truncateOversizedToolResults` caps each tool message at `DefaultMaxToolResultChars=24000` bytes; rune-safe cut; appends `\n…[truncated by runtime]` | `internal/agent/governance/governance.go:42-50,215-246` |
| 12 | Empty-pool / tool-not-found (LLM names a tool that's not in this turn's pool but IS in the registry) | Yes | Permissive `toolPool.EnsureLoaded` calls `resolver(name)` on every announced call; if resolved, adds to pool before dispatch | `internal/agent/pool.go:85-120`; wiring `loop.go:393-397` |
| 13 | `tool_search` result preloading | Yes | `toolPool.AbsorbToolSearchResult` parses `hits[].name` from JSON envelope and pre-loads each, so the next iteration sees the tools in its `tools` array | `internal/agent/pool.go:122-152`; wiring `loop.go:495-503` |
| 14 | Hard tool-not-found / not-allowed at dispatch | Yes | Executor returns `FormatFatalToolError("tool %q is not allowed for this agent")` or `"tool registry unavailable"`; registry itself returns `"tool not found"` when unknown | `internal/agent/executor.go:124-128`; `internal/agent/tools/registry/registry.go:272-273` |
| 15 | Action-enum tools called without an `action` field | Yes | `ActionRequiredError` returns a self-correcting error: lists valid actions, guesses from arg keys via `GuessActionFromArgs`, embeds a ready-to-copy retry JSON | `internal/agent/tools/registry/action_error.go:46-86` |
| 16 | Action-enum tools called with an unknown action | Yes | `UnknownActionError` uses Levenshtein distance ≤3 (`closestActionMatch`) to suggest the closest valid action and embeds retry JSON | `internal/agent/tools/registry/action_error.go:88-138` |
| 17 | Outer-context cancellation while a tool is in flight | Yes | `executor.ExecuteToolCalls` selects on `ctx.Done()`; unfinished calls receive `FormatToolError(ctx.Err())` | `internal/agent/executor.go:94-102` |
| 18 | Parallel-tool data race on shared arguments map | Yes | `cloneToolArgs` shallow-clones LLM-supplied args before fan-out | `internal/agent/executor.go:71-86,155-164` |
| 19 | Prompt-injection in tool output | Yes | All tool output passes through `WrapUntrustedToolResult` before reaching state | `internal/agent/executor.go:84`; duplicate-stub variant `loop.go:517` |
| 20 | `ask_user` exclusive batch semantics (model emits ask_user + other tools simultaneously) | Yes | `findAskUserCall` truncates the batch to only ask_user both at state-record time and at dispatch time | `internal/agent/loop.go:368-371,453-455,732-739` |
| 21 | Per-tool wall-clock cap | Yes | Registry imposes 5-min `defaultToolExecTimeout` when caller didn't set one; executor also applies `e.toolTimeout` | `internal/agent/tools/registry/registry.go:261,289-293`; `internal/agent/executor.go:131-135` |
| 22 | Credential leakage in tool-failure log lines | Yes | `redactToolError` strips URL credentials and base64 blobs before `logger.Warn` (full err still goes to the LLM via `FormatToolError`) | `internal/agent/executor.go:146,168-181` |

What is NOT mitigated automatically (deliberately):

- The runtime does NOT suggest "retry" or classify fatality in tool-result text. From `internal/agent/tools/registry/error.go:48-50`: "tools report what happened, the LLM decides what to do. The runtime neither suggests 'retry' nor classifies fatality — both proved to inflate loop iterations against tools that simply needed different arguments." `FormatToolError` and `FormatFatalToolError` produce the same wire format (`error.go:60,69-70`); the distinction is callsite-intent only.

---

## B. Error-classification vocabulary already shipped

`classifyToolError` (`internal/agent/tools/registry/error.go:13-41`) maps a Go `error` to a low-cardinality label suitable for structured logging. It is the ONLY classification surface in the tools subsystem today.

| Class | Trigger | Citation |
|---|---|---|
| `""` (empty) | `err == nil` | `error.go:14-16` |
| `timeout` | `errors.Is(err, context.DeadlineExceeded)` OR message contains `timed out`/`timeout` | `error.go:17-19,29-30` |
| `cancelled` | `errors.Is(err, context.Canceled)` | `error.go:20-22` |
| `not_found` | message contains `not found` / `does not exist` / `no such` | `error.go:25-26` |
| `validation` | message contains `validation` / `invalid` / `must be` / `must not` / `is required` | `error.go:27-28` |
| `permission` | message contains `unauthorized` / `forbidden` / `denied` / `not allowed` | `error.go:31-32` |
| `blocked` | message contains `blocked` / `ssrf` / `refusing to dial` | `error.go:33-34` |
| `rate_limited` | message contains `rate limit` / `too many requests` | `error.go:35-36` |
| `io` | message contains `i/o` / `io error` / `read` / `write` | `error.go:37-38` |
| `error` | catch-all fallback | `error.go:40` |

Inline LLM-facing hints emitted by `specificHint` (separate from the class label):

| Trigger | Hint text | Citation |
|---|---|---|
| `is a directory` in message | "Hint: this path is a directory. Use list_files to enumerate it, then read individual files." | `error.go:78-79` |
| Shell failure mentioning `syntax error`/`redirection`/`sh:` | "Hint: /bin/sh is dash, not bash. For non-trivial shell logic use execute_code with Python." | `error.go:80-84` |

The class label is used at two emission sites only:

1. `internal/agent/tools/registry/registry.go:308` — `r.logger.Warn("tool failed", "tool", name, "elapsed", elapsed, "error_class", classifyToolError(err))`.
2. The label is NOT propagated into `run_events` payloads, NOT surfaced to the LLM, NOT aggregated, and NOT consulted by any other code path. Verified by full-tree search: `Grep "classifyToolError"` returns only the definition, the README listing, the test file, and the single emission at `registry.go:308`.

The LLM additionally has a coarser binary failure signal via the `chat.lifecyclePayload` mapping at `internal/chat/hub.go:435-439`: `EventToolEnd` with `success=false` is persisted to `run_events` with `error_class: "tool_failed"` (literal string, not the 10-label vocabulary above).

---

## C. What persists across runs

### C.1 `run_events` table

Schema at `internal/db/migrations/migrations.go:341-365`:

```
run_events(
  id, run_id, parent_run_id, seq, type, schema_version,
  actor_id, causation_id, correlation_id, idempotency_key,
  payload_json, redaction_level, created_at
)
```

Tool-failure-relevant rows:

- `EventToolStart` events: payload carries `tool`, `tool_call_id`, `arg_keys` (KEYS only, no values — `internal/chat/hub.go:433-434`).
- `EventToolEnd` events: payload carries `tool`, `tool_call_id`, `success` (bool), `elapsed_ms`; on failure, `error_class: "tool_failed"` is added; result text is replaced with `result_redaction: "preview_omitted"` (`internal/chat/hub.go:435-442`).

What `run_events` does NOT record:

- The fine-grained `classifyToolError` label (timeout/validation/not_found/...). Only the boolean `success=false` collapsed to the literal `"tool_failed"`.
- The actual error message text.
- The argument values.
- Any structured tool input — only `arg_keys` (the names of the keys).

The `durableRunEvent` filter at `internal/chat/hub.go:343-350` lists which event types reach `run_events`: `EventRunStarted, EventToolStart, EventToolEnd, EventMessageDone, EventUsage, EventDone, EventError, EventCancelled`. So every tool start/end is persisted, with the redaction shape above.

### C.2 `conversations` archive table

Schema at `internal/db/migrations/migrations.go:91-109`:

```
conversations(
  id, chat_id, user_id, turn_index, role, content,
  tool_calls, tool_call_id,
  llm_calls, tool_calls_count, elapsed_ms, tokens_in, tokens_out,
  created_at
)
```

Per `internal/conversation/archive_turns.go:46-80`: every loop message (user, assistant, tool) is appended as one row. Assistant rows that contain `ToolCalls` get the marshaled JSON into the `tool_calls` column (`archive_turns.go:64-71`). Tool rows carry the full result text in `content` and the `tool_call_id` for join.

This is the richest passive store of tool history Aura has today: the FULL turn-by-turn ledger including the LLM-facing error text, replayable for analysis. Indexed on `(chat_id, turn_index)` and `(user_id, created_at)` (`migrations.go:108-109`).

### C.3 Aggregation / lessons-learned layer

None. Full-tree search confirms:

- `Grep "lesson|learn|feedback|past_failure|prior_failure"` over `internal/` returns 10 files, all unrelated to tool-call learning: `delegation_policy.go` (swarm), `runtime_settings.go` / `config.go` (feature flags), `search.go` (storage), `untrusted.go`/`phantom_guard.go` (prompt-injection guard), wiki schema/parser, PDF/files. There is no table, no in-memory cache, and no code path that reads past tool-failure rows back into the prompt.
- Per-Run `Stats` (`internal/agent/loop.go:154-179`) has `DuplicateToolCall bool`, `ToolCalls int`, `ToolCallsExecuted int`, `PhantomToolDetections int`, `PhantomToolCorrected int` — counters only, no per-tool-per-error-class breakdown. Persisted to `runs.stats_json` (`internal/chat/hub.go:443-446,468-483`) but never read back to inform future calls.
- `seenToolCalls`, `seenToolCallsResult`, `toolCallExecutions` are all local maps reset per `runLoop` invocation (`loop.go:218-221`). No cross-Run state.

---

## D. What is missing for "Aura improves from preventable tool-call failures"

The audit above shows the runtime can DETECT and REDIRECT a single failure within ONE turn (dedupe, budget caps, phantom guard, governance repair) and can DEFLECT some failures with self-correcting hints (`ActionRequiredError`, `UnknownActionError`, `specificHint`). But "improves" — meaning the same shape of failure becomes LESS likely on the NEXT turn or the NEXT run — has no implementation today.

Concrete gaps:

| # | Gap | Evidence it's missing |
|---|---|---|
| D-1 | No cross-turn / cross-run learning. Same arg-shape failure repeats indefinitely. | No reader of `conversations.tool_calls` JSON or `run_events.type='tool_end' AND payload.success=false` exists in `internal/`. The cross-tree grep for `lesson|learn|past_failure` returned zero hits in the agent / tools paths. |
| D-2 | No pre-call "have we seen this fail before?" check. The LLM sees the system prompt + tools manifest + conversation history; it does NOT see any structured summary of prior failures of the tool it's about to invoke. | `loop.go:264-291` is the pre-LLM section — only governance transform + tool-pool snapshot run; no prior-failure injection. `BeforeLLM` (`loop.go:114,264-271`) is the only injection hook and is used for budget-stop signals (`stop bool`), not for failure-context augmentation. |
| D-3 | No automatic argument-correction suggestion based on prior outcomes. The action-enum self-correctors (`ActionRequiredError`, `UnknownActionError`) are STATIC — they guess from the current call's arg keys only, not from "last time you called `file` with `path=X` it returned `is a directory`". | `action_error.go:58-86,92-117` show the guesser uses only the supplied args + a static `[]ActionHint` table. No history lookup. |
| D-4 | No "tool experience" structure, table, or memory layer. | DB schema enumerated above (`migrations.go`). The only failure-shaped persistence is the boolean `success=false` per `EventToolEnd` row — no rollup table, no per-(tool, error_class) counters, no per-(tool, arg-key-signature) statistics. |
| D-5 | The `classifyToolError` vocabulary (10 labels) is currently a one-way log emission, not a key. It is NOT stored in `run_events.payload_json` and is NOT joinable back to a `(tool, class, count)` view. | `registry.go:308` is the only caller. The label never reaches `chat.lifecyclePayload` (`hub.go:435-442`) which hardcodes `"tool_failed"`. |
| D-6 | Phantom-tool corrections are not durable. `Stats.PhantomToolDetections` / `PhantomToolCorrected` reset per Run; there's no cross-run signal that "this user's tone tends to phantom around tool X" or that "this tool's name shows up in didactic prose a lot". | `loop.go:154-179` — counters only; not persisted to `run_events`. |
| D-7 | LLM-level retry (`internal/llm/retry.go`) is a TRANSIENT-vs-CONTENT-vs-PERMANENT classifier, but its classification is consumed entirely in-process: `Classify(err)` result is used immediately to dispatch and dropped. No record that "this prompt + this model produces CONTENT errors 80% of the time at temperature 0". | `retry.go:80-107` shows the bucket is used and lost; no emission of the bucket to logging or persistence. |
| D-8 | The `[<name> result omitted from context]` microcompact stub (`governance.go:202-206`) drops the error class along with the body, even when the dropped result was an error. After compaction, future iterations cannot tell that the prior tool result was a failure. | `microcompactToolResults` replaces content unconditionally if `len >= minChars` (`governance.go:194-205`); no special handling for `"Error:"` prefix. |
| D-9 | No surface to enumerate "tools that have failed in this thread" or "tools that have failed for this principal". The data is in `conversations.tool_calls` and `run_events`, but no Go reader, no API endpoint, no SQL view aggregates it. | `internal/api/` — no `/api/tool-failures` or similar endpoint. `Grep "tool_failures|tool_outcomes"` returns zero hits. |

---

## E. What we can build on (don't reinvent)

| Building block | Why it's the right shape | Citation |
|---|---|---|
| `classifyToolError` 10-label vocabulary | Already stable, low-cardinality, doesn't leak LLM-controlled values (a CLAUDE.md constraint). Natural primary key for a `(tool, class)` rollup. Augmenting it to be emitted into `run_events.payload_json` is a one-line change at `internal/chat/hub.go:435-442` plus a feed-through from `executor.go:140-148` (where the error is currently formatted for the LLM but the class is not captured). | `internal/agent/tools/registry/error.go:13-41` |
| `conversations` archive (`CONV_ARCHIVE_ENABLED`) | Already persists the full turn-by-turn ledger including the LLM-facing error text in `content` for `role='tool'` rows, plus `tool_calls` JSON on the announcing `role='assistant'` row. A passive scanner can rebuild any failure history without changing the writer. Indexed for `(chat_id, turn_index)` lookup. | `internal/db/migrations/migrations.go:91-109`; `internal/conversation/archive_turns.go:46-80` |
| `run_events` event-sourced log | Append-only, has `seq`, `correlation_id`, `idempotency_key`, indexed `(type, created_at)`. A `EventToolEnd` with `success=false` is already emitted per failed tool call. Extending `lifecyclePayload` to add a structured `error_class` field is non-breaking (existing readers ignore unknown keys). Index `idx_run_events_type_created` already supports range queries like "all `tool_end` failures in last 7d". | `internal/db/migrations/migrations.go:341-365`; `internal/chat/hub.go:343-350,433-457` |
| `PhantomToolGuard` shape | The pattern is: register a side-channel struct on `Options`, run a heuristic on EVERY iteration, emit structured logs (`agentloop: phantom_tool_detected`) when it fires, inject a corrective message into state via the `PhantomCorrector` interface assertion. This is the proven shape for "before next tool call, surface relevant prior failures": a `Options.PriorFailureBriefing` field, a `PriorFailureBriefer` interface assertion, log line `agentloop: prior_failure_briefed`. | `internal/agent/phantom_guard.go:38-65`; wiring `loop.go:327-346,356-364` |
| `toolPool` permissive load | The pre-dispatch hook `pool.EnsureLoaded(call.Name)` (`loop.go:393-397`) runs after the LLM announces a tool but BEFORE the executor runs. A "warn the model about prior failures of THIS tool with THIS arg-key shape" check can hook in here without touching the LLM call site. | `internal/agent/pool.go:85-120` |
| `BeforeTool` / `BeforeLLM` callbacks | Existing extension points for per-call decisions (`BeforeTool` can return `ToolCallDecision{Skip: true, Result: "..."}`) or for pre-LLM diversions (`BeforeLLM` returns `(message, stop)`). A "you're about to call X but the last 3 calls to X failed with `validation` — try Y instead" can ride on `BeforeTool` without a new hook. | `loop.go:84,113-114,264-271,420-429,644-666` |
| Governance pre-LLM transform chain | `governance.Apply` is the canonical place to mutate the message list before each LLM call. Adding a `prependPriorFailureSummary(messages, threadID)` step at the end of `governance.Apply` would inject a synthetic `system` (or `user`) message naming relevant prior failures. Keeps the failure-context shape consistent with how truncation/backfill already work. | `internal/agent/governance/governance.go:67-79` |
| `appendValidationNudge` shape from `llm/retry.go` | Demonstrates the idiom of appending a `system` message to the LLM's existing prompt at runtime to deliver a structured correction. Same shape works for "you previously called `tool=X args.shape=[a,b]` and it returned `validation`. Avoid that shape." | `internal/llm/retry.go:174-183` |

---

## Summary

What's there: per-turn defense in depth — dedupe, budget caps, phantom guard, governance repair, action self-correctors, LLM retry with content-temperature staircase, `WrapUntrustedToolResult`, oversized-result truncation, microcompact rolling window, classify-then-redact logging.

What's not: any code path that reads past failures back into a future turn's context. The data is there (`conversations.tool_calls` JSON, `run_events` rows tagged `tool_failed`), the vocabulary is there (`classifyToolError`'s 10 labels), and the injection patterns are there (`PhantomToolGuard` style + `governance.Apply` chain + `BeforeTool` callback). What's missing is the connective tissue: a reader, an aggregation key, and a pre-LLM briefing step. Phase 6 is precisely the slice that connects these existing pieces.
