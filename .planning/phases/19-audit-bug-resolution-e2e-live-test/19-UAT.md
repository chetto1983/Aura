---
status: complete
phase: 19-audit-bug-resolution-e2e-live-test
source: [19-01-SUMMARY.md, 19-02-SUMMARY.md, 19-03-SUMMARY.md, 19-04-SUMMARY.md, 19-05-SUMMARY.md, 19-06-SUMMARY.md, 19-07-SUMMARY.md, 19-08-SUMMARY.md, 19-09-SUMMARY.md, 19-10-SUMMARY.md, 19-11-SUMMARY.md]
started: 2026-06-10T13:42:49Z
updated: 2026-06-10T13:53:13Z
evidence: docs/audit/19-LIVE-SIGNOFF-2026-06-10.md (operator Davide, SIGNED OFF 2026-06-10)
---

## Current Test

[testing complete]

## Tests

### 1. Cold Start Smoke Test
expected: Kill any running daemon, apply migration 0013 to a clean DB, start aura fresh with .env present. Boots without errors; schema_migrations = version 13 and aura.pending_notifications exists; a primary op (chat turn or scheduler tick) works against live data; operator subcommands now see .env config (central godotenv.Load before dispatch).
result: pass
note: "Sign-off Preconditions table — migration 0013 applied (schema_migrations version 13, dirty=f), aura.pending_notifications present; fresh binary @ 0ab722e5 booted; live stack (PG/Neo4j/embed/agent-memory-mcp/STT/markitdown/TTS/SearXNG) healthy; live chat turns + real scheduler ticks ran against live data."

### 2. Shell Command That Orphans a Child + Huge Output (H4/H5/M-b/M-f)
expected: In `aura chat`, ask the agent to run a shell command that spawns an orphan grandchild (e.g. `( sleep 120 & )`) plus large stdout then `exit 7`. The turn returns a real terminal answer within seconds (no ~120s hang), the agent is aware of exit code 7 even though stdout was truncated (reserved-tail footer survives), and it never goes silent or tells you to "run it yourself". A `· shell_exec` tool-trace line appears.
result: pass
note: "Sign-off Task 1 — containerized paid agent: 55.8s wall-clock no hang with ( sleep 120 & ) orphan; agent reported exit code 7 / 'Fallito' and self-noted truncation at 1960B (reserved-tail footer survived); turn answered (no silence, no hand-off). Ground truth: tool_invocations = shell_exec×4 all returned; persisted assistant turn carries 7."

### 3. Mid-Stream SSE Failure Is Not a Complete Answer (H9)
expected: When an LLM stream cuts mid-response (parse error, or EOF with no finish_reason), the agent surfaces a stream error / retry rather than presenting the accumulated partial text as a finished answer.
result: pass
note: "Sign-off Task 1 — Layer-1 binding proof (testdata/premature_close.sse + EOF-without-finish regressions: terminal Chunk.Err emitted before close, main loop refuses partial-as-complete) + healthy-path live control (clean full terminal answer, no false complete). Deterministic live mid-stream cut not reproducible on demand against OpenRouter; Layer-1 is the binding evidence per plan."

### 4. Telegram Turn-Error Renders the Reason (H2)
expected: When a turn errors in Telegram (e.g. provider HTTP 400), the bot sends a sanitized failure message like "❌ Errore: llm: provider returned HTTP 400" (DSN/token redacted) — not just a bare "Stato: errore" status glyph with the reason swallowed.
result: pass
note: "Sign-off Task 2 — live bot with invalid model rendered '❌ Errore: llm: provider returned HTTP 400' (sanitized via agui.SanitizeString, no DSN/token/URL) in addition to the status glyph. Ground truth: rendered body screenshot D:/tmp/tg_h2err.png, zero mojibake."

### 5. Telegram Large-Document Conversion Failure Notifies the User (H3)
expected: Upload a 5–50 MB document that fails conversion. The bot replies "Conversione del documento non disponibile." instead of leaving you stranded on "📄 …elaborando…" forever.
result: pass
note: "Sign-off Task 2 — 6 MB corrupt .docx (async tier); bot log WARN async document convert failed (non-nil convErr) → rendered '📄 …elaborando…' then 'Conversione del documento non disponibile.' (convertFailMessage reached the user, not log-and-silent)."

### 6. /cancel During an ask_user Pause Cancels the Pause (M-e)
expected: While the agent is paused awaiting your answer (inline keyboard shown), sending `/cancel` cancels the pause — the paused_states row resolves with action=cancel — shows "Richiesta annullata.", and the inline keyboard is cleared, matching the "Annulla" button. With no pending pause, /cancel still cancels the in-flight turn as before.
result: pass
note: "Sign-off Task 2 — induced choice pause (paused_states token 019eb182…, resumed_at NULL = pending) → /cancel → DB resumed_answer={\"action\":\"cancel\"} + rendered 'Richiesta annullata.' + Pizza/Sushi keyboard retired (screenshot D:/tmp/tg_mecancel.png)."

### 7. Scheduler Quiet-Hours Notification Deferred Then Flushed (H6)
expected: A job that completes inside quiet hours does not drop its notification — it's persisted as a pending_notifications row with notify_after = quiet-window end, and a later scheduler tick sweep delivers it once the window closes.
result: pass
note: "Sign-off Task 3 — live scheduler (15s tick, quiet hours covering now): in-window agent_job completed → durable pending_notifications row status=pending notify_after=13:15:00Z → after window, sweep delivered (status=delivered, updated_at=13:15:14Z) + stdout sink '[scheduler notify route=stdout] ok'. DB pending→delivered transition is ground truth."

### 8. Scheduler Failed Self-Send Is Bounded-Retried (H7)
expected: A notification whose MCP self-send fails is persisted as a failed row and retried on later ticks, bounded at 3 attempts (attempts 0→1→2→3), then stops — never silently lost, never retried forever.
result: pass
note: "Sign-off Task 3 — route=whatsapp with no WhatsApp MCP mounted → Notify failed (WARN MCP self-send failed) → durable pending_notifications row status=failed, sweep retried attempts 0→1→2→3 capped at 3 then stops. DB row status=failed attempts=3 last_error is ground truth."

### 9. AG-UI Slow Subscriber Keeps a Conformant Stream (H1)
expected: With a deliberately-slow SSE subscriber, only repeatable delta frames drop under backpressure; lifecycle/boundary frames (RUN_STARTED / RUN_FINISHED / message START/END) are never dropped, so the surviving sequence still passes AG-UI ValidateSequence.
result: pass
note: "Sign-off Task 3 — Layer-1 binding proof (rewritten TestFanoutSlowSubscriberDropped re-validates the surviving sub-sequence via the AG-UI SDK events.ValidateSequence, not just len<=want) + live control (AG-UI HTTP server up on the running stack; every live Telegram turn drove the in-process fanout→translator path conformantly). Deterministic cap-64 overflow not reproducible on demand; Layer-1 is the binding evidence per plan."

### 10. Dead MCP Tool Reconnects on Next Use (H10)
expected: If a stdio MCP sidecar's transport dies, the next tool call transparently reopens + re-initializes the client, refreshes the tool list/description, and retries once; a second consecutive failure returns a clean tool error (no hang, no permanently-dead tool). BM25 tool_search index stays intact.
result: pass
note: "Layer-1 regression-only (not in the D-03 live repro set). 19-09 reconnectingServer: typed mcp.ErrTransport classification, reopen+initialize+refresh tools/list, retry once, clean error on second failure, tool_search BM25 invalidate/refresh. Unit+race green (TestReconnect|TestBridge|TestBridgedTool)."

### 11. AG-UI Rejects Unsupported Multimodal Input (M-d)
expected: An AG-UI run request carrying structured/multimodal user content (not plain text) gets an explicit 400 rejection rather than silently replaying old history as if nothing happened.
result: pass
note: "Layer-1 regression-only (not in the D-03 live repro set). 19-04: run-input extraction returns explicit 400 for unsupported structured/multimodal content instead of replaying old history. Unit green (TestServer_RunBadRequests|TestLastUserMessage)."

### 12. Layer-1 Regression Matrix Green for Non-Observable Findings (H8, M-c/M-f/M-g/M-h/M-i/M-j, L1–L6, INFO)
expected: The committed fails-before/passes-after CI regressions for every non-user-observable finding pass — microcompact tool-round integrity (H8), Fanout/trace redaction (M-c), synchronized shell stderr interleave (M-f), ReschedulesOnRecovery recovery invariant (M-g), detached shutdown terminal write (M-h), central env load (M-i), bounded stderr ring (M-j), and the LOW trust-boundary fixes (L1 snippet timeout, L2 SanitizeName guard, L4 deleted-conversation search filter, L5 inert SKIP LOCKED dropped, L6 empty-arg reject) + INFO trust-boundary doc.
result: pass
note: "Sign-off Preconditions — Layer-1 matrix (19-01..19-10) green, full go build ./... clean. Per-finding regressions recorded in each SUMMARY: H8 (19-06), M-c/M-d (19-04), M-f (19-01), M-g/M-h/L5 (19-08), M-i/L1/L2/L4/L6/INFO (19-10), M-j (19-09). M-g/M-h fails-before/passes-after explicitly proven (19-08)."

## Summary

total: 12
passed: 12
issues: 0
pending: 0
skipped: 0
blocked: 0

## Gaps

[none — all findings resolved; live operator sign-off recorded in docs/audit/19-LIVE-SIGNOFF-2026-06-10.md]
