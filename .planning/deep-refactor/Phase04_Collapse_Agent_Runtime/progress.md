# Phase04 Progress

| Date | Actor | Change | Verification | Blockers | Deviations From Plan |
| --- | --- | --- | --- | --- | --- |
| 2026-05-15 | Codex | Recreated clean standalone Phase04 scaffold after phase-folder reset. | Local file contract only. | Needs call graph and verifier. | No old verification inherited. |
| 2026-05-15 | Claude | Scoped the remaining Phase 4 work and produced a Ralph-ready story breakdown (Phase-G). See "Migration plan" below. NO code changes this turn — the refactor crosses 5 files (~1200 LOC) and per `feedback_ralph_for_heavy_work` belongs in a Ralph driver, not in an interactive session. Once the operator kicks off Ralph with the Phase-G prd.json, the deletion is mechanical. | Inventory + plan only. | None. | None. |

## Current state (post Phase-F, HEAD = `b58dc1ef`)

`internal/agent/runner.go` (292 LOC) + `internal/agent/runner_test.go` (514 LOC) host the legacy `agent.Runner` type.

`Runner` is a thin stateful wrapper around `agent.Run`. Concretely:
1. Constructor `NewRunner(Config) (*Runner, error)` — stores LLM + tools + model + mutable limits.
2. `Limits()` / `UpdateLimits()` — exposed via `LimitController` interface to `internal/config/runtime_settings.go` so dashboard settings can mutate caps without a process restart.
3. `Run(ctx, Task) (Result, error)` — wraps a single bounded `agent.Run` call: builds messages from `Task`, applies `context.WithTimeout(r.timeout)`, constructs `Invocation`, calls `agent.Run`, wraps result into the `Runner.Result` shape (content extraction, `MaxIterationsHit` special-case message).

The everything-`agent.Run`-does-is-here property means the duplicate body in the audit is now small — `Runner.Run` is ~70 LOC of glue around a single `agent.Run` call.

## Live consumers (call graph)

| Caller | File:Line | Holds | What it actually does with the Runner |
|---|---|---|---|
| Main bot wiring | `cmd/aura/app.go:416` | `auraRunner *agent.Runner` | Builds once at startup, stashes in `Deps`, hands to web_chat + cron adapter + telegram bot wiring. |
| Web chat service | `internal/api/web_chat.go:20,40,69` | `s.runner *agent.Runner` | Per `/api/chat` request: builds `agent.Task` from session messages, calls `s.runner.Run(ctx, task)`. |
| Cron job dispatch | `cmd/aura/app.go:852` (adapter) + `internal/cron/dispatch.go:30` (interface) | `r *agent.Runner` (adapter wraps it) | Adapter implements `cron.JobRunner.RunJob` by translating `cron.JobRequest` → `agent.Task` and calling `r.Run`. |
| Telegram bot | `internal/telegram/deps.go:57,115` | `agentRunner *agent.Runner` in `Bot.deps` | Held but NOT called for the normal chat path (which goes through `channels/telegram.InvocationBuilder` → `agent.Run` directly). Possibly used by background ops only. **Audit recommended in US-G02.** |
| Settings applier | `internal/config/runtime_settings.go:55` | `runner config.AgentLimitController` (`*agent.Runner` satisfies via duck-typing) | Dashboard pushes new limits → calls `runner.UpdateLimits(...)`. |

Plus tests:
- `internal/agent/runner_test.go` (514 LOC) — the exhaustive Runner contract test.
- `internal/swarm/manager_test.go:132-133` — uses `manager.UpdateLimits/Limits` (Swarm has its own pair, unrelated).

## Migration plan (Phase-G Ralph queue)

The goal: delete `runner.go` + `runner_test.go` while keeping every consumer working. The shape is:

1. **US-G01 — Stateless `agent.RunTask`.** Add `internal/agent/runtask.go` exposing `func RunTask(ctx, llmClient, tools, model, reasoningEffort, phantomGuard, logger, task Task, limits TaskLimits) (Result, error)` that does what `Runner.Run` does — timeout wrap, build messages, construct `Invocation`, call `agent.Run`, wrap result. `Task`, `Result`, `Config`, `LimitController` stay in `runner.go` for now (compat). Cover the new function with a focused test that mirrors the Runner contract for one happy path.

2. **US-G02 — Audit telegram `Deps.agentRunner` usage.** Grep every method on `*telegram.Bot` that calls `b.deps.agentRunner.Run(...)`. If none, remove the field. If some, migrate each call site to `agent.RunTask`. Single commit, single file.

3. **US-G03 — Migrate `internal/api/web_chat.go`.** Replace `runner *agent.Runner` with the fields needed to call `RunTask` (LLM client + tool registry + model + reasoning effort + per-request limit reader). `webChatService.Chat` becomes one call to `agent.RunTask`. Update `NewWebChatService` signature; chase the 1 call site in `cmd/aura/app.go`.

4. **US-G04 — Migrate `cmd/aura/agentJobRunnerAdapter`.** Replace the adapter's stored `*agent.Runner` with the same field set as US-G03. Adapter method `RunJob` becomes one call to `agent.RunTask`.

5. **US-G05 — Re-wire `LimitController`.** With no `*Runner` left, `internal/config/runtime_settings.go::ApplyRuntimeSettings` has nothing to mutate. Options:
   - **(a)** Delete the `runner config.AgentLimitController` parameter entirely. Background-agent iteration/timeout caps are read fresh from `cfg.*` on every `agent.RunTask` call (read-the-config-on-each-invocation pattern). Pro: simplest. Con: live config changes only take effect on NEXT background agent invocation.
   - **(b)** Replace with a `config.AgentLimits` mutable struct (read-write atomic) that `cmd/aura` constructs and both the settings applier and `agent.RunTask` callers read.
   Recommended **(a)**: closest to how dashboard settings already propagate elsewhere.

6. **US-G06 — Delete `runner.go` + `runner_test.go`.** Drop both files. Update `cmd/aura/app.go:416` to remove the `auraRunner` construction (callers in US-G02/03/04 no longer need it). Update `internal/telegram/deps.go` to drop `agentRunner` if US-G02 didn't already. Verify `Deps.AgentRunner` field is gone everywhere. Final `go test ./...` clean.

7. **US-G07 — Update `internal/agent/README.md` + `docs/aura-main-loop-limits-audit.md`** to reflect the canonical-Run-only state. Move `Task` and `Result` types into a new `task.go` so the `agent.RunTask` API documents its contract.

## Why Ralph not in-session

- 5 files crossing 3 package boundaries (`agent`, `api`, `cron`, `telegram`, `config`).
- `LimitController` redesign is a small but real API change with 3 implementers.
- `runner_test.go` is 514 LOC of contract tests — every assertion needs to be remapped to the stateless function.
- Per `feedback_ralph_for_heavy_work` memory: refactor multi-step / >1h → Ralph (fresh ctx per iter), NON in-session.
- The 7 stories above are sized for one Ralph iter each; the queue is mechanical once written.

## Phase-G Closure (2026-05-15)

All 7 US-G stories shipped on master. `agent.Runner` is deleted. Every consumer
now calls `agent.RunTask`. docs and README refreshed.

| Story | Commit | What shipped |
|---|---|---|
| US-G01 | cf364f5a | Stateless `RunTask` helper + `RunTaskDeps` + 3 tests |
| US-G02 | da096351 | Dropped `telegram.Deps.agentRunner` (dead field) |
| US-G03 | 98e0b4f4 | Migrated `webChatService` off `*agent.Runner` |
| US-G04 | 1a3859f0 | Migrated `agentJobRunnerAdapter` off `*agent.Runner` |
| US-G05 | 37b674fb | Deleted `config.AgentLimitController` + dropped `runner` param from `ApplyRuntimeSettings` |
| US-G06 | 413277f7 | Deleted `runner.go` + `runner_test.go` (−806 LOC); added `task.go` |
| US-G07 | bd637695 | Refreshed `internal/agent/README.md` + limits audit §1 |

## Pointers

- Last Phase-F commit: `b58dc1ef`.
- Limits audit: `docs/aura-main-loop-limits-audit.md` §1 updated — Phase-G completion recorded.
- Ralph driver: `scripts/ralph/`. All US-G stories `passes: true`.
