# Deep Correctness & Robustness Audit — 2026-06-10

Stochastic parallel re-audit (10 agents, surface-partitioned) targeting the known soft-spot
classes: **error-swallowing, not-wired features, env-ordering, loop/termination, concurrency,
resource leaks, silent data loss**. Each finding survived adversarial self-refutation against
call sites. Lint/style/`%w` nits excluded by construction.

Branch `tabula-rasa` @ HEAD `0e453c7a`.

---

## Root-cause cluster: "shell turns never answer" (the named open UX bug)

No single hang in the completion-gate critic — the gate is a hard one-shot (`completionAttempts >= 1`,
fails open on a broken critic). The symptom is produced by **three independent mechanisms**, any of
which alone reproduces "after a shell command the agent gives no real answer":

1. **[H4] shell_exec can literally hang past its own timeout** — orphan grandchild keeps the stdout
   pipe open, `cmd.Wait()` blocks forever (no `WaitDelay`). The turn never returns. → primary mechanical cause.
2. **[H5] On large shell output the exit-code/stderr footer is truncated away** — the agent reasons
   over head-only stdout, can't see the failure, and the critic grades a failed run `DONE` or loops. → "lies about success" variant.
3. **[M-a / M-b] completion-critic + finalize bypass `streamWithOpenRetry`, and a veto re-surfaces the
   vetoed hand-off prose on the budget trip** — the gate's whole purpose (reject "here's a script, you
   run it") is defeated on the budget-exhaustion path; the user gets exactly the vetoed non-answer. → "hands off instead of answering" variant.

Fixing H4 + H5 + M-b together is the highest-leverage work in this report.

---

## HIGH

### H1 — SSE backpressure drops protocol BOUNDARY frames → conformant client rejects the whole stream
`internal/agui/server.go:240-258` + `internal/agui/fanout.go:112-143`
- **class:** silent data loss (protocol corruption)
- A slow client fills the cap-64 buffer; only `RUN_STARTED/FINISHED/ERROR` are non-droppable. The
  `default:` arm drops `TEXT_MESSAGE_START`, `TOOL_CALL_START`, `REASONING_START` too. Dropping a START
  while delivering its CONTENT/END orphans the delta; the AG-UI SDK's public `ValidateSequence` rejects
  the entire turn ("cannot add content to message that was not started").
- `TestFanoutSlowSubscriberDropped` only asserts `len <= want` + first/last lifecycle — it never
  re-validates the surviving sub-sequence, so the blind spot is untested.
- **fix:** make `isLifecycleFrame` treat ALL START/END/RESULT/CUSTOM frames as non-droppable; only the
  repeatable delta frames (`*_CONTENT`, `TOOL_CALL_ARGS`, `STATE_DELTA`) may drop.

### H2 — Turn errors are swallowed from the user (Telegram): bare "Stato: errore", never the reason
`internal/channels/telegram/renderer.go:81-101`
- **class:** error-swallowing
- The renderer switches only on `TextMessageContent/End` + `RunFinished`; there is **no `RunErrorEvent`
  case**, so the error text (`err.Error()` — model unavailable, tool/DB error, `EnsureConversation` fail)
  is dropped. The status pane only flips `failed=true` → a tiny glyph, and in-flight tools stay on 🟡.
- Golden `testdata/statuspane_run_error.golden` is the full user-visible output for an errored turn; the
  `serve_channels.go:145` comment claiming "the user sees a generic ❌ Errore" is stale — that string
  renders nowhere.
- **fix:** add a `case *events.RunErrorEvent:` that sends a sanitized user-facing failure message
  (reuse the HTTP path's `sanitizeErr`).

### H3 — Async (5–50 MB) document conversion failure swallowed → "elaborando…" then silence forever
`internal/channels/telegram/bot_dispatch.go:388-395`
- **class:** error-swallowing
- The async tier's callback logs `convErr` and `return`s with no user notice after already replying
  "📄 Documento ricevuto, lo sto elaborando…". The user waits indefinitely. (The sync ≤5 MB tier
  correctly surfaces `convertFailMessage`.)
- **fix:** on `convErr != nil`, send `convertFailMessage` via the captured sender.

### H4 — Timeout/cancel kills only the shell, not its children → orphan leak + hang past timeout
`internal/agent/tools/shell_exec.go:119,126`
- **class:** resource leak / loop-termination (the known MSYS gotcha, on the PRIMARY surface)
- `exec.CommandContext(...).Run()` with **no `SysProcAttr` process group** and **no `cmd.WaitDelay`**.
  On ctx expiry Go kills only the direct child (`bash`); grandchildren (`go run`, `python server.py`,
  `npm start`) are orphaned. Worse: stdout is a `strings.Builder` (internal `os.Pipe`), and with
  `WaitDelay`=0, `Wait()` blocks until every writer closes the pipe — including the orphaned
  grandchild. A grandchild that ignores the kill **hangs the call past its timeout**.
- Repo-wide: zero hits for `SysProcAttr`/`Setpgid`/`WaitDelay`/`Cancel`/`taskkill`.
  `TestShellExecTimesOut` only asserts the marker, never that the child died.
- **fix:** start a process group (`Setpgid` POSIX / `CREATE_NEW_PROCESS_GROUP` Win), kill the whole
  group via custom `cmd.Cancel`, and set `cmd.WaitDelay` (2–5 s) so `Wait()` force-closes inherited fds.

### H5 — Exit code + stderr silently dropped from the model's view on output > preview cap
`internal/agent/tools/shell_exec.go:139-146` + `internal/agent/tools/result.go:75-84`
- **class:** silent truncation / data loss (exit-code propagation defeated)
- The status marker, stderr block, and `[aura_shell …]`/`[exit code N]` footer are appended at the
  **tail**; `NewResult` truncates **head-first** (`content[:cut]`). For stdout > `AURA_CONTEXT_PREVIEW_CAP_BYTES`
  (2048), the model's RoleTool message = first 2 KB of stdout + a pager pointer; the exit code, stderr,
  and footer are gone. `Meta["exit_code"]` is set on the ledger but never injected into history. The
  critic (`criticResultCap=400`) is hit the same way → a large *failed* run can grade `DONE`.
- **fix:** reserve tail bytes for status+footer (and a stderr tail) that survive truncation, or truncate
  the stdout body before appending the always-keep footer.

### H6 — Quiet-hours-deferred notifications are permanently dropped, not deferred
`internal/cron/dispatch.go:154-156`
- **class:** silent data loss / not-wired
- A successful non-reminder run during `AURA_SCHEDULER_QUIET_HOURS` logs "deferred to window end" and
  returns — but `complete()` already wrote terminal state and the tick advanced `next_run_at`. There is
  **no** notification-state column, pending queue, or window-end flush (grep: no `notification_state`/
  `notified_at` anywhere). The notification is lost; the user never learns the job ran.
- **fix:** persist `notify_after` + a tick sweep that flushes after the window, or deliver immediately
  and correct the comments.

### H7 — "Undelivered → bound-retry on a later tick" is fiction; a failed self-send is never retried
`internal/cron/dispatch.go:158-160` + `internal/cron/notify.go:90`
- **class:** silent data loss / not-wired
- An MCP self-send failure logs "bound-retry on a later tick" (D-22) and moves on; the completed run is
  terminal and never re-selected. The stdout fallback is the only surviving copy. Same root defect as H6:
  a documented deferral/retry contract with no backing implementation.
- **fix:** persist undelivered-state + bounded re-attempt, or downgrade the comments to "best-effort,
  stdout only, no retry."

### H8 — microcompact `dropOldestPairs` drops turns 2-at-a-time → wire-invalid tool history → provider 500
`internal/conversations/context.go:237-263`
- **class:** silent truncation / data loss
- The function assumes strict `user/assistant` alternation and does `body = body[2:]` per iteration. But
  `role='tool'` turns are first-class persisted rows (`applyL1`), so a round is
  `assistant(tool_calls) → tool → tool → assistant`. A 2-stride drop slices mid-round, leaving an orphan
  `tool` turn at the head (no preceding `assistant` tool_call). `toMessages(reduced)` goes straight to the
  LLM, which rejects it → the turn 500s instead of compacting. Every `context_boundary_test.go` fixture
  uses only user/assistant bodies, so the tool case is untested.
- **fix:** drop by conversational boundary (advance to the next `RoleUser`), and skip a dangling
  `RoleTool` head after reducing.

### H9 — SSE parse error swallowed → a mid-stream transport failure is delivered as a complete answer
`internal/llm/openai_compat/client.go:145-166`
- **class:** error-swallowing / silent truncation
- `parseSSE` returns a real error on a malformed/truncated/reset mid-stream body, but the goroutine
  captures it only into the reasoning trace, then `close(out)` normally. `llm.Chunk` has **no error
  field**, so once the stream opens, every streaming error is structurally unreportable. The consumer
  accepts the partial accumulated text as the final answer (truncation notice only fires on
  `finish=="length"`, never on a missing/aborted finish). The orphaned fixture
  `testdata/premature_close.sse` (referenced by zero `.go` files) is the anticipated-but-unwired case.
- **fix:** add `Err error` to `llm.Chunk` and emit it before close, or treat a stream that ended with no
  `finish_reason` + non-nil parse error as a retryable infra failure. (Also covers M — clean EOF with no
  `finish_reason` in `sse.go:114-122`.)

### H10 — MCP "reconnect-on-use" is documented design but never implemented
`internal/agent/mcptools/bridge.go:35-60` + `internal/mcp/client.go`
- **class:** not-wired
- `bridgedTool` captures one `Server` at mount time; `Execute` calls `b.srv.CallTool` against a possibly-
  dead `stdin` forever. No re-spawn+re-init anywhere; the dead tool also keeps its boot-time description
  in the registry. The `Ping` primitive exists but is wired only to `aura mcp doctor`. The decided design
  (`docs/research/mcp-sidecar-lifecycle-study.md:43`: "lazy reconnect-on-use") only has its fail-soft-boot
  half implemented.
- **fix:** wrap the mounted `srv` in a reconnecting Server (re-open + `initialize` once on transport
  error, then retry; clean tool error on second failure) — or correct the docs/memory that claim it ships.

---

## MEDIUM

- **M-a — completion-critic + finalize + reasoning-router stream-opens bypass `streamWithOpenRetry`**
  `llm_agent_completion.go:94`, `llm_agent_finalize.go:217`, `llm_agent_reasoning.go:45`. A transient
  open blip the main loop would retry makes the critic fail-open (un-gated answer) or finalize to the
  Italian stub. Route all three through the shared bounded-retry helper. *(never-answer contributor)*
- **M-b — `gateCompletion` veto re-surfaces the vetoed hand-off prose on the budget trip**
  `llm_agent.go:259-266` + `llm_agent_finalize.go:117,202`. The veto appends the rejected
  "I wrote the script, you run it" prose as durable `RoleAssistant`; the next budget trip routes to
  `finalize`, which copies that history forward and re-emits it. The gate is defeated on the
  exhaustion path. Fix: on a content-stop veto, append only the feedback nudge, not the vetoed prose.
  *(never-answer contributor)*
- **M-c — Fanout (Telegram path) emits & traces UN-sanitized `RUN_ERROR`** `internal/agui/fanout.go:85-95`.
  The HTTP path redacts via `redactEvent`; the in-process Fanout path forwards `err.Error()` verbatim AND
  writes raw event JSON (DSN/token) into a reasoning trace. Trace leak is live today; user-facing leak is
  latent (status pane doesn't render `Message` yet). Sanitize at the translator boundary.
- **M-d — `lastUserMessage` silently drops structured/multimodal content** `internal/agui/server.go:318-328`.
  Only `string` content is accepted; a `[]InputContent` message is skipped → the turn drives over
  rehydrated history with no new user input, no 400. Reject explicitly or project text parts.
- **M-e — `/cancel` during a pending ask_user pause does NOT cancel the pause** `bot_dispatch.go:114-122`
  + `commands.go:167-177`. `/cancel` is intercepted before the HITL path; it only cancels an in-flight
  turn ctx (none exists during a pause). The `paused_states` row is orphaned and the inline keyboard
  stays live — divergent from the "Annulla" button. Route `/cancel` through `SubmitAnswer(…ActionCancel)`
  when `PendingFor` is non-empty.
- **M-f — shell stderr fully de-interleaved (all stderr after all stdout)** `shell_exec.go:123-125,294-300`.
  Two separate `strings.Builder`s destroy temporal correlation; combined with H5 the stderr block is also
  the truncated tail. Point both at one synchronized writer for terminal parity.
- **M-g — `ReschedulesOnRecovery` is a dead safety control** `internal/cron/recover.go:55-82`. The PRD's
  recovery invariant ("never auto-re-execute committed side-effects when the flag is false") is never
  consulted; `catchUpMissed` re-fires every overdue task. Benign today (only idempotent handlers are
  `false`), latent for any future side-effecting `false` handler. Wire the flag into `catchUpMissed`.
- **M-h — Shutdown mid-run completes on the cancelled ctx → run stuck `running` until the 90 s orphan
  window** `internal/cron/dispatch.go:120-134`. `CompleteRun(ctx,…)` with the signal-cancelled root ctx
  is rejected by pgx and only logged. Write terminal state on `context.WithoutCancel(ctx)` + a short
  deadline (mirror the HTTP-shutdown pattern at `serve.go:119`).
- **M-i — `aura mcp <sub>` reads `AURA_MCP_CONFIG` without loading `.env`** `internal/mcp/managed_config.go:88`.
  `godotenv.Load()` lives only in `config.Load*`/`llm.Load`; the `aura mcp` dispatch path calls neither
  (except `mcp tools`), so `.env`'s `AURA_MCP_CONFIG` is invisible and the operator edits the **default**
  `~/.aura/mcp/servers.json` while `aura serve` reads the intended file. **Root cause of a whole class**:
  there is no central `_ = godotenv.Load()` at `main()` start. One line closes it (also fixes the
  `aura mcp doctor` whatsapp-URL and `agent dry-run`/`swarm-demo` `AURA_LOOP_*`/`AURA_SWARM_MAX_DEPTH`
  LOW findings).
- **M-j — mcp-neo4j-cypher subprocess stderr captured into an unbounded buffer for the client's
  lifetime** `internal/knowledge/client.go:69,272-287`. `safeBuffer` never trims; `stderrTail()` reads
  only the last 200 B. A chatty/error-looping sidecar grows RSS without bound. Make it a bounded ring.

---

## LOW (informational)

- **L1 — host snippet exec (`aura skills snippet exec`) has no timeout/cancellation**
  `cmd/aura/skills_snippet.go:112` (`context.Background()`, no SIGINT) — a runaway snippet hangs forever
  on the operator CLI path; `shell_exec` (the model path) does enforce 120 s. Wrap with `WithTimeout` +
  `signal.NotifyContext`.
- **L2 — `Writer.Activate` / `Writer.DiscardPending` skip the `SanitizeName` chokepoint**
  `internal/skills/writer_activate.go:24`, `resume.go:66` — every sibling method guards before
  `os.RemoveAll`; these two don't. Not exploitable today (all live callers pre-validate). Add the guard.
- **L3 — ctx-cancel (vs deadline) reports opaque `signal: killed` / `timed_out:false` / `exit_code:-1`**
  `shell_exec.go:138-145` — the model can't distinguish operator-cancel from a crash. Add a
  `context.Canceled` branch with a distinct `[command cancelled]` status.
- **L4 — `SearchConversationTurns` has no status filter** `internal/conversations/store.go:482` —
  soft-deleted (`status='deleted'`) conversations' turns stay searchable. Latent (no command currently
  sets `StatusDeleted`; user-facing delete is a hard FK-cascade). Add `status <> 'deleted'` before any
  soft-delete path ships.
- **L5 — `DueTasks` `FOR UPDATE SKIP LOCKED` is a no-op** `internal/cron/store_runs.go:81-94` — runs on
  the autocommit pool, so the row lock is released the instant the SELECT returns. Correctness is held by
  the per-task `pg_try_advisory_lock`; the SKIP LOCKED is inert defense-in-depth that could mislead a
  maintainer. Drop it or run select+claim in one tx.
- **L6 — web_search/web_fetch don't reject empty required `query`/`url`** `web_search.go:76`,
  `web_fetch.go:65` — an empty query reaches SearXNG → "no results" instead of "query required". Graceful,
  just a weaker self-correction signal than the other tools give.
- **INFO — self-installed skill *bundled scripts* are not blocklist-scanned** (`loader.go:213-220` scans
  only SKILL.md body+description). Confirmed deliberate per the full-host-terminal trust model
  (amendment #50 / D-15c) — equivalent to the model writing+running its own `shell_exec` code. Logged so
  the trust boundary is explicit, not a defect.

---

## Confirmed RESOLVED / refuted (so the negative results are auditable)

- **`send_file` is fully wired end-to-end** (the prior canonical dead-tool defect): tool → `Meta["artifact"]`
  → `Actions.ArtifactDelta` → AG-UI `CustomEvent aura.artifact` → telegram `artifact.go` `SendDocument`,
  with the consumer actually subscribed in production. Every tool with a `ToolSpec` is registered in
  `buildBaseRegistry`; every deferred tool is BM25-indexed (incl. arg field names) and surfaceable by
  `tool_search`. No other dead tools found.
- **Env-ordering, the rest:** zero package-level/`init()` env reads exist in `internal/`; the live agent
  loop, `serve`, scheduler, telegram, and `task` all read env **after** `config.Load*`/godotenv. Only the
  non-`serve` operator subcommands (M-i + LOWs) miss `.env`.
- **Swarm:** max-depth `>=` is correct (no off-by-one); child ctx derives from parent (cancellation
  propagates); per-child errors land in their report slot (none dropped); siblings correctly not cancelled
  (D-02).
- **pgx lazy-error discipline** is applied (`rows.Err()` after loops, SQLSTATE re-classification);
  `WithTx` rollback discipline sound; askuser FIFO has the `token ASC` tiebreaker; sidecar orphan files
  self-heal on retry.
- **The completion gate cannot infinite-loop** (`completionAttempts >= 1` one-shot, fails open on broken
  critic, all trips reach `finalize()` which always yields a non-empty terminal event). The never-answer
  symptom is H4/H5/M-b, not a gate hang.
