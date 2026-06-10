# Aura ↔ Claude Code — tool parity roadmap

**Goal (loop spine):** close the capability gaps between Aura's agent loop and Claude Code's tool harness, one increment per `/loop` iteration. This file is the durable anchor — each iteration re-reads it to know *what's done / what's next / what's blocked on the user*.

**Started:** 2026-06-10. Ground truth: [internal/agent/llm_agent.go](../internal/agent/llm_agent.go) (run loop + `dispatch` + `runTool`), [internal/agent/tools/spec.go](../internal/agent/tools/spec.go) (Tool interface).

## Already at parity / ahead (no work)

- **At parity:** `fs_read/write/edit/grep/glob`, `web_search`, `web_fetch`, `shell_exec` (full host terminal), subagents (`swarm_spawn` parallel + `task` scheduler), deferred-tool loading (`tool_search`, BM25), MCP.
- **Aura ahead:** `skill` authors *and* executes skills; deferred-by-default manifest is more aggressive than Claude Code's; native cron scheduler (`task`) + KV-cache-stable manifest ordering + microcompact. **Large tool output:** Aura auto-spills the full bytes to a sidecar file and returns a truncated preview + a `read_tool_output(tool_call_id, offset, limit)` pointer ([result.go NewResult, D-25](../internal/agent/tools/result.go)) — strictly ahead of Claude Code's Bash, which just truncates >30k chars and makes the agent re-run head/tail/grep to page (verified 2026-06-10).
- **Error handling — partial parity:** a tool error is already fed back as a `RoleTool` observation, loop continues, no abort ([runTool:421-427](../internal/agent/llm_agent.go#L421)). What's missing is the *middleware* around it → P2.

## Parity items

Risk classes: **SAFE-ADDITIVE** (do autonomously in-loop) · **ARCHITECTURAL** (needs a design decision / user fork before code) · **BEHAVIORAL** (changes security/approval posture → user fork).

### P1 — Parallel tool dispatch — ARCHITECTURAL — 🔄 NEXT (decided: FULL parallel, 2026-06-10)

- **Gap:** `dispatch` runs tool calls strictly one-at-a-time ([llm_agent.go:301](../internal/agent/llm_agent.go#L301)). Claude Code runs independent calls concurrently. Reading 5 files = 5× the wall-clock.
- **DECISION (2026-06-10): full parallel** — run all independent calls concurrently, accepting dedup/order semantic changes.
- **Implementation notes:** the iter.Seq2 `yield` is single-threaded — run `Execute()` in goroutines but SERIALIZE the yields + `RoleTool` history appends, emitting in **original call order** (wire contract: every tool_call_id gets a result). Keep the `text_response` terminal path + the dedup gate (dedup-check up front before launch — [llm_agent.go:341](../internal/agent/llm_agent.go#L341)). `-race` the whole loop; existing dispatch/pause/dedup/finalize tests stay green.
- **DoD:** a turn with N independent calls completes in ~max(call) not ~sum(call); wire-order + terminal + dedup + `-race` all green.

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

### P5 — Ungate in-box skill activation (self-extension parity) — BEHAVIORAL — 🔄 QUEUED (decided: ungate, 2026-06-10)

- **Gap:** model-authored skills land `pending_approval` and require a human approve ([[aura-self-extension-no-ceremony]]); Claude Code creates files with no gate.
- **DECISION (2026-06-10): ungate in-box.** Model-authored skills activate immediately inside the container (the boundary, Phase 17, [[aura-full-host-terminal-primary]]). Touch points: `internal/skills/writer.go` `WriteMutation` (pending → activate) + `internal/agent/tools/skill_write.go` (drops the `ErrAwaitingUserInput` pause); UPDATE the tests asserting `pending_approval` (requirement changed — justify in the commit). Keep the injection blocklist (prompt integrity, orthogonal).

## Loop state

Forks settled 2026-06-10 (P1 full-parallel, P5 ungate, P3 add). **Done:** P2, P4, P3. **Remaining authorized work:** P1 (full parallel dispatch — riskiest, its own focused iteration) then P5 (ungate skill activation). P2 Slice B (post-tool hook) optional. The loop proceeds through these with atomic per-item commits + `-race`.

## Status log

- **2026-06-10 it.1** — spine created; P2 Slice A (typed tool-retry middleware) landed with tests. Next: P4 (background shell_exec) — safe-additive — then stop on the P1/P3/P5 forks.
- **2026-06-10 it.2** — P4 (background `shell_exec` + `shell_poll`/`shell_kill`) landed with tests, grounded in Claude Code's Bash/BashOutput/KillBash (`D:/tmp/system-prompts-and-models-of-ai-tools/Anthropic`). **Safe-additive work now exhausted.** Remaining: P1 (parallel dispatch), P3 (todo tool), P5 (ungate skill activation) — all user-forks — plus P2 Slice B (post-tool hook, also architectural). The loop now surfaces the forks and pauses for a decision.
- **2026-06-10 it.3** — forks decided (P1 full-parallel, P5 ungate, P3 add). P3 `todo_write` landed (`51d07bf0`). Confirmed large-output spillover is **Aura-ahead**, not a gap (see "Aura ahead"). Next: P1 (full parallel — the big core-loop change, dedicated iteration) then P5 (ungate).
