# Codex patterns not yet lifted into Aura — research dump 2026-05-21

Source read: `D:/tmp/codex/codex-rs/` (Rust, OpenAI Codex CLI).
Read order: `tools/parallel.rs`, `tools/orchestrator.rs`, `tools/registry.rs`, `tools/router.rs`, `tools/mod.rs`, `session/turn.rs` (run_turn + try_run_sampling_request + drain_in_flight), `stream_events_utils.rs` (handle_output_item_done), `compact.rs`, `client.rs` (prompt_cache_key wiring), `utils/output-truncation/src/lib.rs`, `utils/string/src/truncate.rs`, `tools/handlers/tool_search.rs`, `tasks/regular.rs`, `context_manager/history.rs`.

Skipped patterns that Aura already has:
- Hard MaxIterations cap → Aura `MaxIterationsCeiling` in `internal/agent/loop.go` (line 30).
- Step X/N injected in system prompt → done in `internal/agent/promptplan.go`.
- Tool description audit / operational language → done in registry tests.
- Per-batch parallel tool execution → done in `internal/agent/executor.go:75` (`ExecuteToolCalls` fan-out via sync.WaitGroup).
- Terminal tool (`text_response`, `execute_code`, file gen) → done in `internal/agent/toolexec.go:39 IsTerminalTool`.
- BM25 tool_search → Aura has `tool_search` analog (different scoring but same shape).

---

## 1. Stream-time parallel tool dispatch via `FuturesOrdered`

### WHAT
Codex spawns each tool's execution future the instant the stream emits `OutputItemDone` for a tool call — it does NOT wait for the stream to finish. Tools begin running in parallel with each other AND with the rest of the LLM stream (which often still has reasoning items, more tool calls, or the `Completed` event coming). After the stream's `Completed` event, the loop calls `drain_in_flight` to await any still-running tool future before issuing the next sampling round.

Aura today: parallel execution happens AFTER the stream closes (`agent/executor.go:75`). LLM finishes streaming → loop calls `ExecuteToolCalls` → tools fan out in goroutines → all join → next iteration. The tools cannot overlap with later parts of the stream.

### WHERE
- `core/src/session/turn.rs:1750-1751` — declares the queue:
  ```rust
  let mut in_flight: FuturesOrdered<BoxFuture<'static, CodexResult<ResponseInputItem>>> =
      FuturesOrdered::new();
  ```
- `core/src/session/turn.rs:1813-1892` — for every `ResponseEvent::OutputItemDone` (a finalized stream item), build the tool future via `handle_output_item_done` and push it into the queue immediately:
  ```rust
  let output_result = match handle_output_item_done(&mut ctx, item, previously_streamed_item)
      .instrument(handle_responses).await { ... };
  if let Some(tool_future) = output_result.tool_future {
      in_flight.push_back(tool_future);
  }
  ```
- `core/src/stream_events_utils.rs:343-382` — the future is built but not awaited:
  ```rust
  let cancellation_token = ctx.cancellation_token.child_token();
  let tool_future: InFlightFuture<'static> = Box::pin(
      ctx.tool_runtime.clone().handle_tool_call(call, cancellation_token),
  );
  output.needs_follow_up = true;
  output.tool_future = Some(tool_future);
  ```
  The `Box::pin` here returns immediately — execution happens whenever the pollers next drive the future. In practice tokio's runtime steals work the moment `in_flight` is `.next()`'d or `tokio::spawn`'d underneath.
- `core/src/session/turn.rs:1678-1702` — drain after `Completed`:
  ```rust
  async fn drain_in_flight(in_flight, sess, turn_context) -> CodexResult<()> {
      while let Some(res) = in_flight.next().await {
          match res { Ok(response_input) => { sess.record_conversation_items(...) } Err(...) }
      }
  }
  ```
- `core/src/session/turn.rs:2164` — drain is called at the end of the sampling loop, just before the next round prompt is assembled.

### WHY VALUABLE
On any turn where the model emits a tool call mid-stream and then keeps streaming reasoning/text/another tool call, Aura today blocks. Codex does not. Concretely:
- If the LLM emits `search_memory("X")` at output token ~120 and then keeps reasoning for another 2 seconds, Codex's `search_memory` is already executing during those 2 seconds. Aura's `search_memory` doesn't start until the stream closes.
- Multi-tool turns (e.g. `read_skill("xlsx") + search_memory("foo")` emitted as the model thinks) overlap their wall-clock with the stream itself.

For Aura's typical 8-12 s turn with 1-3 tool calls, expected wall-clock win: 1.5-3 s per turn on tool-call-heavy queries. Bigger when a slow tool (web_search, ingest_source) lands early in the stream.

### AURA PORT EFFORT — 3/5
Aura's loop is a single goroutine that calls `client.Stream(...)` and accumulates the full assistant message before invoking `ExecuteToolCalls`. The Go-idiomatic port:
- Change `llm.Client.Stream` to deliver structured events (`FunctionCallFinalized{id, name, args}`) on a channel rather than buffering the whole turn.
- The loop reads channel events. On each `FunctionCallFinalized`, spawn `go func() { resultChan <- e.executeOneTool(...) }()` — append to an in-flight slice or sync.WaitGroup.
- On stream close, `wg.Wait()` (the equivalent of `drain_in_flight`), then assemble the tool-result block for the next round.
- The cancellation contract has to be preserved: if the user cancels mid-stream, each in-flight tool's ctx must be cancelled.

Risk surface: progressive Telegram edits already rely on streaming the text portion. The reorder must not break ordering of text deltas vs tool-result placeholders in the UI. Aura currently emits "thinking" while tools run; with stream-time dispatch the UI gets the tool result back potentially BEFORE the stream finishes, so the "tools_executed" UI marker has to be tolerant of arrival order.

### AURA LOC DELTA
~150-220 LOC added in `internal/llm/client.go` (richer stream events) + `internal/agent/loop.go` (dispatch on event, wait at stream end). Some current sync-only logic in `executor.go` becomes dead and is removed (-40 LOC). Net: +130-180.

---

## 2. Parallel-friendly tool serialization via shared RwLock

### WHAT
Codex tags each tool with a `supports_parallel_tool_calls()` bit. The runtime acquires a shared read lock for parallel-friendly tools and a write lock for serialized tools. Multiple parallel-friendly tools run concurrently; a write-lock tool waits for the readers and then locks exclusively until done. This is one elegant primitive that subsumes "run these in parallel" AND "this one is dangerous, gate it" without two code paths.

Aura today: every tool in a batch runs in parallel unconditionally (executor.go ExecuteToolCalls). There is no per-tool "this must serialize" flag. So `execute_code`, `workspace_write` and `request_dashboard_token` race against each other and against benign reads.

### WHERE
- `core/src/tools/parallel.rs:31-37`:
  ```rust
  pub(crate) struct ToolCallRuntime {
      router: Arc<ToolRouter>,
      session: Arc<Session>,
      turn_context: Arc<TurnContext>,
      tracker: SharedTurnDiffTracker,
      parallel_execution: Arc<RwLock<()>>,
  }
  ```
- `core/src/tools/parallel.rs:88-118` — the gate:
  ```rust
  let supports_parallel = self.router.tool_supports_parallel(&call);
  ...
  let mut handle = AbortOnDropHandle::new(tokio::spawn(async move {
      let _guard = if supports_parallel {
          Either::Left(lock.read().await)
      } else {
          Either::Right(lock.write().await)
      };
      router.dispatch_tool_call_with_terminal_outcome(...).await
  }));
  ```
- `core/src/tools/router.rs:83-87` — the per-tool predicate:
  ```rust
  pub fn tool_supports_parallel(&self, call: &ToolCall) -> bool {
      self.registry.supports_parallel_tool_calls(&call.tool_name).unwrap_or(false)
  }
  ```

### WHY VALUABLE
- Safety: `workspace_write`, `store_source`, `wiki_write`, `execute_code` should not interleave with each other or with reads. Today nothing guarantees this; Aura relies on the LLM not requesting conflicting calls in one batch.
- Performance: read-heavy tools (`read_memory`, `search_memory`, `read_skill`, `web_search`) stay fully parallel and get the read-lock fast path.

### AURA PORT EFFORT — 2/5
Trivial. Add `SupportsParallel() bool` to the tool interface (default true). In `executor.go ExecuteToolCalls`, two-phase the batch: separate `parallel := []`, `serial := []`. Run parallel ones in goroutines; await; then run serial ones one at a time. Or — closer to Codex — wrap the executor in an `sync.RWMutex` and acquire RLock vs Lock per call. Either is one PR.

### AURA LOC DELTA
~40-60 LOC. Mostly the interface bit + the executor switch. Two-three tools need explicit `SupportsParallel() bool { return false }` overrides.

---

## 3. Server-driven termination via `end_turn` flag

### WHAT
Codex does not need a hard MaxIterations cap to terminate well. The OpenAI Responses API returns an `end_turn` field on the `response.completed` event. If `end_turn == Some(false)`, the loop sets `needs_follow_up = true` (run another sampling round). Otherwise — even with no tool calls — the loop exits. Combined with "any tool call in the stream → needs_follow_up = true", this gives a clean two-signal termination contract that doesn't depend on iteration counting.

Aura today: terminates only on (a) `MaxIterations` reached, (b) empty assistant message with no tool calls, (c) a "terminal tool" was emitted. There's no server-side signal — Aura has to infer.

### WHERE
- `core/src/session/turn.rs:2012-2036` — the only thing that pushes follow-up beyond a tool call:
  ```rust
  ResponseEvent::Completed { response_id, token_usage, end_turn } => {
      ...
      if let Some(false) = end_turn {
          needs_follow_up = true;
      }
      ...
      break Ok(SamplingRequestResult { needs_follow_up, last_agent_message });
  }
  ```
- `core/src/session/turn.rs:300` — the final OR:
  ```rust
  let needs_follow_up = model_needs_follow_up || has_pending_input;
  ```
- `core/src/session/turn.rs:361` — the exit:
  ```rust
  if !needs_follow_up {
      last_agent_message = sampling_request_last_agent_message;
      let stop_outcome = run_turn_stop_hooks(...).await;
      ...
  }
  ```

### WHY VALUABLE
- Aura's LLM endpoint is OpenAI-compatible; if the upstream returns `end_turn`, Aura can adopt the same contract for free and drop iteration-count anxiety on long-form analytical turns.
- Today Aura caps at 20 iterations to prevent runaways. With `end_turn` Aura can keep a sanity cap but treat it as a true emergency brake, not the primary termination signal. This unblocks legitimately long multi-tool research turns that today get cut off.
- If the upstream does NOT return `end_turn` (some self-hosted gateways), Aura's existing inference (no tool call + non-empty assistant text → stop) is the natural fallback.

### AURA PORT EFFORT — 1/5
Trivial. The LLM client already parses the streaming `chat.completion.chunk` envelope. Add an optional `EndTurn *bool` to the final chunk parse, propagate to `loopResult`, OR `needs_follow_up |= (endTurn == Some(false))`.

### AURA LOC DELTA
~25-40 LOC across `internal/llm/client.go` (parse) and `internal/agent/loop.go` (consume).

---

## 4. Prompt cache key = thread ID (provider-side KV cache hint)

### WHAT
Codex sends `prompt_cache_key = thread_id` on every Responses API request. This is a cheap, deterministic signal for OpenAI's server-side prompt cache: requests with the same key from the same account reuse the cached KV state up to the divergence point. No client-side caching, no cache_control breakpoints, no manual placement — just a stable key per conversation.

Aura today: sends no cache hint. Every request is a cold-start from the provider's POV. With Aura's "every turn rebuilds the prompt from sliding window + overlays" pattern, the prefix (overlays + most of history) is identical turn to turn — the perfect cache target — but Aura never tells the provider.

### WHERE
- `core/src/client.rs:751`:
  ```rust
  let prompt_cache_key = Some(self.state.thread_id.to_string());
  ```
- `core/src/client.rs:765` — sent on every responses request:
  ```rust
  let request = ResponsesApiRequest {
      ...
      prompt_cache_key,
      ...
  };
  ```
- `core/src/client.rs:476-488` — also sent on the compaction request (so even the summarizer benefits).

### WHY VALUABLE
- Aura's typical prompt is ~6-12k tokens (overlays + sliding window + tool defs). Server-side cache hit ratio on the prefix could be 70-90% for second+ turns of the same conversation.
- On Anthropic-compatible endpoints, the analog is `cache_control: ephemeral` breakpoints; the principle is the same.
- For self-hosted endpoints (llama.cpp, vllm) this header is ignored — no harm.

### AURA PORT EFFORT — 1/5
Add a header / request field. The thread/conversation ID is already in the loop context.

### AURA LOC DELTA
~10-15 LOC in `internal/llm/client.go`. One change in `Stream` to set the field on the OpenAI-compatible request body.

---

## 5. Centralized output truncation policy (`TruncationPolicy::Bytes | Tokens` + middle-truncate)

### WHAT
Codex routes ALL tool output through a single truncation primitive that preserves a prefix + suffix on UTF-8 boundaries with a "…N tokens truncated…" marker in the middle. Two unit types (Bytes / Tokens) using the same approximation constant (4 bytes/token). Exec output adds metadata (exit code, wall time, total lines) before truncation so the model always sees the structural signal even when the body is gutted.

Aura today: per-call `limitToolContent(maxChars)` in `executor.go:99` — naive byte cap, head-only. No marker. No prefix/suffix preservation. No metadata block. The model can be surprised when output ends abruptly.

### WHERE
- `core/src/utils/string/src/truncate.rs:7-69` — the primitive:
  ```rust
  pub fn truncate_middle_chars(s: &str, max_bytes: usize) -> String {
      truncate_with_byte_estimate(s, max_bytes, /*use_tokens*/ false)
  }
  pub fn truncate_middle_with_token_budget(s: &str, max_tokens: usize) -> (String, Option<u64>) { ... }
  fn split_string(s: &str, beginning_bytes: usize, end_bytes: usize) -> (usize, &str, &str) {
      // walks char_indices, finds UTF-8-safe prefix_end and suffix_start
  }
  ```
- `core/src/tools/mod.rs:64-89` — exec wrapping:
  ```rust
  pub fn format_exec_output_for_model(exec_output: &ExecToolCallOutput, truncation_policy: TruncationPolicy) -> String {
      let duration_seconds = ((exec_output.duration.as_secs_f32()) * 10.0).round() / 10.0;
      let content = build_content_with_timeout(exec_output);
      let total_lines = content.lines().count();
      let formatted_output = truncate_text(&content, truncation_policy);
      let mut sections = Vec::new();
      sections.push(format!("Exit code: {}", exec_output.exit_code));
      sections.push(format!("Wall time: {duration_seconds} seconds"));
      if total_lines != formatted_output.lines().count() {
          sections.push(format!("Total output lines: {total_lines}"));
      }
      sections.push("Output:".to_string());
      sections.push(formatted_output);
      sections.join("\n")
  }
  ```
- `core/src/utils/output-truncation/src/lib.rs:79-138` — the per-`FunctionCallOutputContentItem` budget walker that allocates remaining budget across multiple text segments.

### WHY VALUABLE
- Middle-truncate keeps the head AND tail. Most tool outputs encode the salient info at the ends: command echo at the top, errors / "Total: N" at the bottom. Head-only truncation drops the bottom.
- The marker `…N tokens truncated…` is an explicit signal the model has been trained / RLHF'd to recognize as "more output exists". Aura's silent truncation makes the model hallucinate that the visible output is complete.
- The exec-output structural wrapper (`Exit code: / Wall time: / Total output lines: / Output:`) reliably tells the model whether it should ask to retry or proceed. Aura wraps with `WrapUntrustedToolResult` but doesn't include this structural block.

### AURA PORT EFFORT — 2/5
- Single Go file at `internal/agent/tools/truncate.go` (~80 LOC) with `TruncateMiddle(s string, maxBytes int) string` + token variant.
- Swap `limitToolContent` (executor.go:99) to use it.
- Add exec/file output wrapper in `agent/exec_helpers.go` / `internal/files/` that prepends the structural block.

### AURA LOC DELTA
+90-120 LOC for the truncate util and wrapper; -10 LOC from removing inline head-trim. Net: +80-110.

---

## 6. Per-tool sandbox / approval orchestrator with single-retry escalation

### WHAT
Codex wraps every tool dispatch in `ToolOrchestrator` (492 LOC). The flow:
1. Compute approval requirement from policy.
2. First attempt under the strictest sandbox.
3. If sandbox-denied, escalate (with re-approval if needed) and retry exactly once.
4. Approval cache prevents re-prompting within the same call_id.

Aura doesn't have approval orchestration (different threat model — Aura is single-user), but the **single-retry-on-sandbox-denial** is a clean primitive for any "try permissive, fall back to restrictive" or vice versa flow.

### WHERE
- `core/src/tools/orchestrator.rs:128-389` — the `run` function.
- Key shape: first attempt → if `SandboxErr::Denied` AND `tool.escalate_on_failure()` AND policy allows → second attempt with sandbox=None.

### WHY VALUABLE
- Aura's `execute_code` Python sandbox occasionally needs to escalate (e.g. install a package, write outside /workspace). Today the LLM has to figure that out across multiple turns. A retry-with-permissive-cap could collapse those into one tool call.
- The pattern generalizes to `web_search` (try cached → live fallback) and `read_source` (try Markdown → re-OCR fallback) but Aura currently dispatches those as separate LLM rounds.

### AURA PORT EFFORT — 4/5
Heavier than it looks. Aura's tools are flat function calls; adding an orchestrator means inserting a per-tool retry policy and a hook for "what's a recoverable failure". Worth considering for a future Phase but not a quick win.

### AURA LOC DELTA
+250-350 LOC for a minimal orchestrator + per-tool retry-policy hooks. Not urgent.

---

## 7. Inline auto-compaction triggered by token-window detection

### WHAT
Codex runs a real-time check after every sampling round: compare `active_context_tokens` against the model's per-window soft cap (`auto_compact_token_limit`) AND the hard context window. If exceeded AND a follow-up is needed, trigger an in-thread compaction (NOT a turn boundary): summarize history to a compaction item, swap it into the conversation, reset client_session, continue.

Aura today: 50-message sliding window in `internal/conversation`. No token-aware compaction. Long conversations eventually drop messages off the back of the window — abrupt, no summary.

### WHERE
- `core/src/session/turn.rs:301-358` — the check inline in the main loop:
  ```rust
  let token_status = auto_compact_token_status(sess.as_ref(), turn_context.as_ref()).await;
  let token_limit_reached = token_status.token_limit_reached;
  ...
  if token_limit_reached && needs_follow_up {
      let reset_client_session = match run_auto_compact(
          &sess, &turn_context, &mut client_session,
          InitialContextInjection::BeforeLastUserMessage,
          CompactionReason::ContextLimit,
          CompactionPhase::MidTurn,
      ).await { ... };
      if reset_client_session { client_session.reset_websocket_session(); }
      continue;
  }
  ```
- `core/src/session/turn.rs:655-705` — the budget computation supports two scopes (Total or BodyAfterPrefix) and tracks `auto_compact_window_ordinal` so each window's prefill cost is amortized.
- `core/src/compact.rs:46-47` — the compaction prompt:
  ```rust
  pub const SUMMARIZATION_PROMPT: &str = include_str!("../templates/compact/prompt.md");
  pub const SUMMARY_PREFIX: &str = include_str!("../templates/compact/summary_prefix.md");
  ```
- `core/templates/compact/prompt.md` — six-line context-checkpoint instruction.

### WHY VALUABLE
- Aura's sliding window is a blunt instrument. A 50-msg cap means a 200-msg deep-debug conversation loses the early premise. Token-budgeted compaction preserves the gist.
- The two-scope option (`Total` vs `BodyAfterPrefix`) lets Aura keep the overlays + initial-context "free" and only compact the user-assistant turns — i.e. the right thing to compact.

### AURA PORT EFFORT — 4/5
Non-trivial. Requires:
- Token estimator (Aura uses byte-based today; needs ~4-bytes-per-token approximation or a real tokenizer).
- A compaction trigger in `internal/agent/loop.go` before each LLM call.
- A summarization sub-call to the same LLM with the 6-line `compact/prompt.md` analog.
- History rewrite in `internal/conversation` that supports "replace messages [0..N) with a single summary item".

### AURA LOC DELTA
+400-550 LOC across `internal/conversation/`, `internal/agent/`, new `internal/conversation/compact.go`. Worth doing AFTER stream-time dispatch but before any work on long-horizon multi-day conversation state.

---

## 8. `prompt_cache_key` analog on compaction requests (the secondary win)

### WHAT
Codex sends `prompt_cache_key` not just on the main sampling request but also on the COMPACTION request (the LLM call that summarizes history). This means the summarizer's prefix (its own instruction + any shared headers) gets cached too.

### WHERE
- `core/src/client.rs:476-488` — compaction payload:
  ```rust
  let payload = ApiCompactionInput {
      model: &model,
      input: &input,
      instructions: &instructions,
      tools,
      parallel_tool_calls,
      reasoning,
      service_tier: service_tier.as_deref(),
      prompt_cache_key: prompt_cache_key.as_deref(),
      text,
  };
  ```

### WHY VALUABLE
Small absolute win but free — paired with pattern #4 and pattern #7. When Aura adds compaction, every compaction sub-call should reuse the same `prompt_cache_key` as the main thread.

### AURA PORT EFFORT — 1/5
Trivial follow-on if patterns #4 and #7 land. Same field, same value.

### AURA LOC DELTA
+3 LOC.

---

## 9. Surprising pattern — `AbortOnDropHandle` for cancellation

### WHAT
Codex wraps every spawned tool task in `tokio_util::task::AbortOnDropHandle`. If the future holding the handle is dropped (e.g. the loop is unwinding because of an error in another tool), the spawned task is automatically aborted. No explicit cleanup, no leaked goroutines.

### WHERE
- `core/src/tools/parallel.rs:10`:
  ```rust
  use tokio_util::task::AbortOnDropHandle;
  ```
- `core/src/tools/parallel.rs:112-113`:
  ```rust
  let mut handle: AbortOnDropHandle<Result<AnyToolResult, FunctionCallError>> =
      AbortOnDropHandle::new(tokio::spawn(async move { ... }));
  ```

### WHY VALUABLE
Aura uses `context.WithCancel` + `defer cancel()` to achieve the same effect, but spawned goroutines that don't check `ctx.Done()` will leak. Codex's pattern is automatic.

### AURA PORT EFFORT — 3/5
Not directly portable — Go doesn't have drop semantics. The Go-idiomatic equivalent is to require every tool to accept a `context.Context` and return promptly when `ctx.Done()` fires. Aura already does this for most tools; the gap is making it a hard contract (the registry refuses to register a tool that doesn't honor cancellation). Worth a code audit but not a code-change pattern lift.

### AURA LOC DELTA
0 LOC for new code; could be a test fixture that compiles each tool against a "respect ctx.Done within 100 ms" probe.

---

## 10. Surprising pattern — `terminal_outcome_reached` AtomicBool for race-free cancel-after-finish

### WHAT
The tool runtime tracks an `Arc<AtomicBool>` that gets set to true the moment the tool finishes its terminal action. If a cancellation arrives, the code checks this flag first: if the tool already completed, return the real output (don't replace with an "aborted" message). If not, abort the handle, await the join, and synthesize the abort message.

### WHERE
- `core/src/tools/parallel.rs:99-160`:
  ```rust
  let terminal_outcome_reached = Arc::new(AtomicBool::new(false));
  let dispatch_terminal_outcome_reached = Arc::clone(&terminal_outcome_reached);
  ...
  tokio::select! {
      res = &mut handle => res.map_err(Self::tool_task_join_error)?,
      _ = cancellation_token.cancelled() => {
          if terminal_outcome_reached.load(Ordering::Acquire) || handle.is_finished() {
              handle.await.map_err(Self::tool_task_join_error)?
          } else {
              ...
              handle.abort();
              match handle.await { ... }
          }
      },
  }
  ```
- And the flag is set inside `registry.rs:566-568`:
  ```rust
  if let Some(terminal_outcome_reached) = &terminal_outcome_reached {
      terminal_outcome_reached.store(true, Ordering::Release);
  }
  ```

### WHY VALUABLE
Solves the "cancel raced with tool's last 5ms of work" problem cleanly. Aura's pattern (ctx-based cancellation) doesn't have this race because Aura always waits for the tool to return, but if Aura ever moves to stream-time dispatch (pattern #1), it'll need this exact primitive: the loop is reading later stream events while a tool is in its terminal flush — a user-cancel arriving in between must not lose the work.

### AURA PORT EFFORT — 2/5
Go has `sync/atomic.Bool`. Same shape.

### AURA LOC DELTA
~30 LOC; only relevant if pattern #1 is adopted.

---

## Summary — top patterns by ROI (impact / effort)

ROI score = (impact rating 1-5) / (port effort rating 1-5).

| Pattern | Impact | Effort | ROI | Note |
|--------|-------:|------:|----:|------|
| #1 Stream-time parallel tool dispatch | 5 | 3 | 1.67 | Biggest latency win; requires stream restructure |
| #4 `prompt_cache_key = thread_id` | 4 | 1 | 4.0  | Free 70-90% cache hit on prefix |
| #3 Server-driven `end_turn` termination | 4 | 1 | 4.0  | Removes iteration-cap as primary gate |
| #5 Centralized middle-truncate + exec wrapper | 3 | 2 | 1.5  | Pairs with anti-hallucination work |
| #2 Parallel-friendly RwLock gate | 3 | 2 | 1.5  | Safety win for write-tools |
| #7 Inline auto-compaction | 5 | 4 | 1.25 | Required for long conversations |
| #8 cache_key on compaction call | 1 | 1 | 1.0  | Free if #4 + #7 done |
| #10 terminal_outcome_reached atomic | 2 | 2 | 1.0  | Only needed if #1 done |
| #6 Orchestrator with single-retry escalation | 3 | 4 | 0.75 | Architectural; not urgent |
| #9 AbortOnDropHandle equivalent | 2 | 3 | 0.67 | Audit, not code change |

---

## Top-3 by ROI

1. **#4 `prompt_cache_key = thread_id`** — 10-15 LOC, immediate cache benefit on every OpenAI-compatible request. No risk.
2. **#3 Server-driven `end_turn` termination** — 25-40 LOC, drops iteration cap from primary signal to emergency brake. Unblocks long analytical turns. Falls back gracefully when the server doesn't return the field.
3. **#1 Stream-time parallel tool dispatch** — 130-180 LOC, 1.5-3 s wall-clock saving on tool-heavy turns. Biggest absolute win but requires careful stream-event refactor in `internal/llm/client.go`. Should land AFTER #3 and #4 because those make the loop's termination contract cleaner.

Patterns #2 (RwLock gate) and #5 (truncate + exec wrapper) are good Phase-WIKI-Clean candidates — small, isolated, safety/quality wins that don't depend on the loop refactor.

Patterns #7 (compaction) and #6 (orchestrator) are bigger architectural moves; they should wait until the simpler ROI patterns ship.
