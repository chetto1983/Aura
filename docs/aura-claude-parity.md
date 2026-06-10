# Aura ↔ Claude Code — tool parity roadmap

**Goal (loop spine):** close the capability gaps between Aura's agent loop and Claude Code's tool harness, one increment per `/loop` iteration. This file is the durable anchor — each iteration re-reads it to know *what's done / what's next / what's blocked on the user*.

**Started:** 2026-06-10. Ground truth: [internal/agent/llm_agent.go](../internal/agent/llm_agent.go) (run loop + `dispatch` + `runTool`), [internal/agent/tools/spec.go](../internal/agent/tools/spec.go) (Tool interface).

## Already at parity / ahead (no work)

- **At parity:** `fs_read/write/edit/grep/glob`, `web_search`, `web_fetch`, `shell_exec` (full host terminal), subagents (`swarm_spawn` parallel + `task` scheduler), deferred-tool loading (`tool_search`, BM25), MCP.
- **Aura ahead:** `skill` authors *and* executes skills; deferred-by-default manifest is more aggressive than Claude Code's; native cron scheduler (`task`) + KV-cache-stable manifest ordering + microcompact.
- **Error handling — partial parity:** a tool error is already fed back as a `RoleTool` observation, loop continues, no abort ([runTool:421-427](../internal/agent/llm_agent.go#L421)). What's missing is the *middleware* around it → P2.

## Parity items

Risk classes: **SAFE-ADDITIVE** (do autonomously in-loop) · **ARCHITECTURAL** (needs a design decision / user fork before code) · **BEHAVIORAL** (changes security/approval posture → user fork).

### P1 — Parallel read-only tool dispatch — ARCHITECTURAL — ⛔ BLOCKED (user fork)

- **Gap:** `dispatch` runs tool calls strictly one-at-a-time ([llm_agent.go:301](../internal/agent/llm_agent.go#L301)). Claude Code runs independent calls concurrently. Reading 5 files = 5× the wall-clock.
- **Target:** fan out **non-mutating** calls in one assistant message concurrently; keep writes / `shell_exec` / skill-mutations / `text_response` sequential. Append `RoleTool` results in the **original call order** (OpenAI wire contract) and preserve cache stability.
- **Fork for the user:** parallelizing changes the **dedup gate** semantics (`BeforeToolCall`/`AfterToolResult` are stateful, per-call — [llm_agent.go:341](../internal/agent/llm_agent.go#L341)) and the deterministic event-yield order. Trade determinism/dedup-simplicity for latency? Default proposal: parallelize only a *read-only prefix* run-group, dedup-check up front, append in order — minimal semantic change. **Needs a yes before code.**
- **DoD:** a turn with N read-only calls completes in ~max(call) not ~sum(call); dedup + wire-order + cache tests still green; `-race` clean.

### P2 — Tool-execution middleware ("the error hook") — SAFE-ADDITIVE — 🔄 IN PROGRESS

- **Gap:** no retry/backoff/middleware around `tool.Execute` ([runTool:421](../internal/agent/llm_agent.go#L421)), while the LLM stream already has `streamWithOpenRetry`.
- **Slice A (iteration 1, DONE):** typed retry — a **non-mutating** tool that fails with a transient `net.Error.Timeout` / `context.DeadlineExceeded` is retried (bounded, linear backoff) while the parent ctx is alive. Mutating tools never retried (at-most-once side effects). Transient decided by **type**, never by string-matching ([[feedback_no_regex_for_nlp]]). → `internal/agent/llm_agent_retry.go`.
- **Slice B (next):** an optional PostToolUse hook seam (e.g. auto-verify after a mutating write in a code dir) — additive, off by default.
- **DoD:** retry unit tests (transient→retried, mutating→not, permanent→not, budget cap, ctx-cancel) green + `-race`.

### P3 — Todo / plan scratchpad tool — ARCHITECTURAL (manifest/cache + new verb) — 💡 PROPOSED

- **Gap:** no structured working memory for long autonomous turns (swarm/cron, 25-step budget). Claude Code has TodoWrite + plan mode; Aura has a step counter + a one-shot recovery nudge.
- **Fork:** adding an always-on tool mutates the cache-stable manifest and adds a model verb (scope). Overlaps Phase 11f Task Canvas — decide build-vs-reuse. **Needs user steer.**

### P4 — Background `shell_exec` + poll — SAFE-ADDITIVE — ✅ DONE (it.2)

- **Gap:** `shell_exec` was synchronous; a 10-min build/download blocked the turn or died at the 120s cap. Claude Code backgrounds + Monitors.
- **Shipped:** `shell_exec` gains `"background": true` → returns a `shell_id` immediately; new deferred `shell_poll` (new-output-since-last + status `running|exited:<code>|killed`, optional regex filter — Claude Code's BashOutput) and `shell_kill` (KillBash). A shared `BackgroundShells` registry wired once at boot keeps jobs pollable across turns. Also fixed `shell_exec`'s stale `sandbox_exec` references. → [internal/agent/tools/shell_bg.go](../internal/agent/tools/shell_bg.go), commit `33ccab12`.
- **DoD met:** start→poll→complete, incremental read-off, filter, kill, error paths — all green with `-race`; golangci-lint 0 issues.

### P5 — Ungate in-box skill activation (self-extension parity) — BEHAVIORAL — ⛔ BLOCKED (user fork)

- **Gap:** model-authored skills land `pending_approval` and require a human approve ([[aura-self-extension-no-ceremony]]); Claude Code creates files with no gate.
- **Fork:** now that the container is the boundary (Phase 17, [[aura-full-host-terminal-primary]]), the HITL gate is arguably ceremony — but removing it changes the approval posture. **Needs explicit user go**, tied to the Phase-17 container model.

## Why the loop can't finish fully autonomously

P1, P3, P5 each carry a fork only the user can settle. The loop does the SAFE-ADDITIVE items (P2, P4) autonomously, then **stops and notifies** with the pending forks. It never cowboys an architectural/behavioral change unattended (project PRD-first discipline).

## Status log

- **2026-06-10 it.1** — spine created; P2 Slice A (typed tool-retry middleware) landed with tests. Next: P4 (background shell_exec) — safe-additive — then stop on the P1/P3/P5 forks.
- **2026-06-10 it.2** — P4 (background `shell_exec` + `shell_poll`/`shell_kill`) landed with tests, grounded in Claude Code's Bash/BashOutput/KillBash (`D:/tmp/system-prompts-and-models-of-ai-tools/Anthropic`). **Safe-additive work now exhausted.** Remaining: P1 (parallel dispatch), P3 (todo tool), P5 (ungate skill activation) — all user-forks — plus P2 Slice B (post-tool hook, also architectural). The loop now surfaces the forks and pauses for a decision.
