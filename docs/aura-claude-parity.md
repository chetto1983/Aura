# Aura ↔ Claude Code — tool parity roadmap

**Goal (loop spine):** close the capability gaps between Aura's agent loop and Claude Code's tool harness, one increment per `/loop` iteration. This file is the durable anchor — each iteration re-reads it to know *what's done / what's next / what's blocked on the user*.

**Started:** 2026-06-10. Ground truth: [internal/agent/llm_agent.go](../internal/agent/llm_agent.go) (run loop + `dispatch` + `runTool`), [internal/agent/tools/spec.go](../internal/agent/tools/spec.go) (Tool interface).

## Already at parity / ahead (no work)

- **At parity:** `fs_read/write/edit/grep/glob`, `web_search`, `web_fetch`, `shell_exec` (full host terminal), subagents (`swarm_spawn` parallel + `task` scheduler), deferred-tool loading (`tool_search`, BM25), MCP.
- **Aura ahead:** `skill` authors *and* executes skills; deferred-by-default manifest is more aggressive than Claude Code's; native cron scheduler (`task`) + KV-cache-stable manifest ordering + microcompact. **Large tool output:** Aura auto-spills the full bytes to a sidecar file and returns a truncated preview + a `read_tool_output(tool_call_id, offset, limit)` pointer ([result.go NewResult, D-25](../internal/agent/tools/result.go)) — strictly ahead of Claude Code's Bash, which just truncates >30k chars and makes the agent re-run head/tail/grep to page (verified 2026-06-10).
- **Error handling — partial parity:** a tool error is already fed back as a `RoleTool` observation, loop continues, no abort ([runTool:421-427](../internal/agent/llm_agent.go#L421)). What's missing is the *middleware* around it → P2.

## Parity items

Risk classes: **SAFE-ADDITIVE** (do autonomously in-loop) · **ARCHITECTURAL** (needs a design decision / user fork before code) · **BEHAVIORAL** (changes security/approval posture → user fork).

### P1 — Parallel tool dispatch — ✅ DONE (it.4)

- **Gap (closed):** `dispatch` ran tool calls one-at-a-time; reading 5 files was 5× the wall-clock.
- **Shipped:** `dispatch()` runs the runnable batch's `Execute()` concurrently (`executeBatch`: inline for 1, goroutine fan-out for ≥2) while the dedup gate, `yield`s, and `RoleTool` appends stay serial in original call order; `text_response` handled as terminal after the batch (`runTerminal`). → [llm_agent_parallel.go](../internal/agent/llm_agent_parallel.go) + [llm_agent.go](../internal/agent/llm_agent.go), commit `aa6f36c8`.
- **DoD met:** a deterministic barrier test proves concurrency + call-order; full `internal/agent` suite green with `-race`; lint 0.

### P2 — Tool-execution middleware ("the error hook") — SAFE-ADDITIVE — 🔄 IN PROGRESS

- **Gap:** no retry/backoff/middleware around `tool.Execute` ([runTool:421](../internal/agent/llm_agent.go#L421)), while the LLM stream already has `streamWithOpenRetry`.
- **Slice A (iteration 1, DONE):** typed retry — a **non-mutating** tool that fails with a transient `net.Error.Timeout` / `context.DeadlineExceeded` is retried (bounded, linear backoff) while the parent ctx is alive. Mutating tools never retried (at-most-once side effects). Transient decided by **type**, never by string-matching ([[feedback_no_regex_for_nlp]]). → `internal/agent/llm_agent_retry.go`.
- **Slice B (next):** an optional PostToolUse hook seam (e.g. auto-verify after a mutating write in a code dir) — additive, off by default.
- **DoD:** retry unit tests (transient→retried, mutating→not, permanent→not, budget cap, ctx-cancel) green + `-race`.

### P3 — Todo / plan scratchpad tool — ✅ DONE (it.3)

- **Gap:** no structured working memory for long autonomous turns (swarm/cron, 25-step budget). Claude Code has TodoWrite; Aura had a step counter + a one-shot recovery nudge.
- **Shipped:** `todo_write` (non-deferred, session-scoped) — the model writes the full list each call (content/status/activeForm), one in_progress enforced. → [internal/agent/tools/todo.go](../internal/agent/tools/todo.go), commit `51d07bf0`. (Decided "add it" over deferring to Phase 11f Task Canvas.)

### P4 — Background `shell_exec` + poll — SAFE-ADDITIVE — ✅ DONE (it.2)

- **Gap:** `shell_exec` was synchronous; a 10-min build/download blocked the turn or died at the 120s cap. Claude Code backgrounds + Monitors.
- **Shipped:** `shell_exec` gains `"background": true` → returns a `shell_id` immediately; new deferred `shell_poll` (new-output-since-last + status `running|exited:<code>|killed`, optional regex filter — Claude Code's BashOutput) and `shell_kill` (KillBash). A shared `BackgroundShells` registry wired once at boot keeps jobs pollable across turns. Also fixed `shell_exec`'s stale `sandbox_exec` references. → [internal/agent/tools/shell_bg.go](../internal/agent/tools/shell_bg.go), commit `33ccab12`.
- **DoD met:** start→poll→complete, incremental read-off, filter, kill, error paths — all green with `-race`; golangci-lint 0 issues.

### P5 — Ungate in-box skill activation (self-extension parity) — ✅ DONE (it.5)

- **Gap (closed):** model-authored skills landed `pending_approval` + a human approve pause; Claude Code creates files with no gate.
- **Shipped:** `WriteMutation` flips the gate to false for the model actor (reusing the auto-activate branch → `StatusActive`); `writeAction` returns a normal result on non-pending status (pause kept only as a defensive fallback). Injection blocklist still runs on every path. → [internal/skills/writer.go](../internal/skills/writer.go) + [internal/agent/tools/skill_write.go](../internal/agent/tools/skill_write.go), commit `9cc10202`. Rationale: the whole agent runs inside the container boundary (Phase 17, [[aura-full-host-terminal-primary]]), so self-extension needs no human gate.
- **DoD met:** tool `TestActionCreateActivates` + a live `db_integration` `TestWriteMutationModelActorUngated` (model→active+materialized, cli→pending) verified against Postgres; `-race` + lint clean.

## Loop state

**🎯 PARITY COMPLETE (2026-06-10).** All five gaps closed: P1 parallel dispatch, P2 error-hook retry, P3 `todo_write`, P4 background shell, P5 ungate skills — plus large tool output (already Aura-ahead). The only remaining item, **P2 Slice B** (a PostToolUse hook seam), is an *enhancement*, not a parity gap; leave for a future session if wanted.

## Status log

- **2026-06-10 it.1** — spine created; P2 Slice A (typed tool-retry middleware) landed with tests. Next: P4 (background shell_exec) — safe-additive — then stop on the P1/P3/P5 forks.
- **2026-06-10 it.2** — P4 (background `shell_exec` + `shell_poll`/`shell_kill`) landed with tests, grounded in Claude Code's Bash/BashOutput/KillBash (`D:/tmp/system-prompts-and-models-of-ai-tools/Anthropic`). **Safe-additive work now exhausted.** Remaining: P1 (parallel dispatch), P3 (todo tool), P5 (ungate skill activation) — all user-forks — plus P2 Slice B (post-tool hook, also architectural). The loop now surfaces the forks and pauses for a decision.
- **2026-06-10 it.3** — forks decided (P1 full-parallel, P5 ungate, P3 add). P3 `todo_write` landed (`51d07bf0`). Confirmed large-output spillover is **Aura-ahead**, not a gap (see "Aura ahead"). Next: P1 (full parallel — the big core-loop change, dedicated iteration) then P5 (ungate).
- **2026-06-10 it.4** — P1 parallel tool dispatch landed (`aa6f36c8`): concurrent `Execute()`, serial dedup/yield/append in call order, terminal-last; deterministic barrier test + full `-race` green. Only P5 (ungate skills) remains of the authorized work.
- **2026-06-10 it.5** — P5 ungate in-box skill activation landed (`9cc10202`), verified live against Postgres. **Parity loop complete** — all 5 gaps closed + large-output Aura-ahead. Loop stopped.
