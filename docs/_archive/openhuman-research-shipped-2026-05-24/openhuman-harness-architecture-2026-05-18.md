# OpenHuman agent harness — architecture analysis

**Date:** 2026-05-18
**Source studied:** `D:/tmp/openhuman/src/openhuman/agent/` (Rust, GPLv3 — concepts only, not code)
**Aura baseline:** `D:/Aura/internal/agent/`, `D:/Aura/internal/swarm/`
**Goal:** identify openhuman patterns that would make Aura's deferred Phase 8 multi-agent substrate (planner+executor, critic+review, hierarchical DAG, team-collaboration) "drop-in" rather than rewrites.

---

## 1. Core abstractions

| # | Abstraction | Responsibility (openhuman) | File:line | Aura analog |
|---|---|---|---|---|
| 1 | **Agent / Session** | Stateful conversation owner. Holds history, provider, tool registry, memory loader, per-turn budgets. `Agent::turn(user_message)` is the hot path. One Agent = one live conversation. | `agent/harness/session/mod.rs:35` (re-exports `Agent`), `agent/harness/session/turn.rs:62` (`Agent::turn`) | **Missing as a struct.** Aura has `internal/agent/state.go` (history) + `internal/agent/loop.go::runLoop` (stateless function) — no first-class "Agent" object that owns history + provider + tools + budgets together. `runtask.go::RunTask` rebuilds the wiring every call. |
| 2 | **AgentDefinition** | Pure data archetype: id, when_to_use, prompt source, model spec, tool scope, sandbox mode, max_iterations, max_result_chars, tier (chat/reasoning/worker), subagents list. Serialised as TOML files in `agents/<name>/agent.toml` or workspace overrides. | `agent/harness/definition.rs:37` (`AgentDefinition`), `agents/orchestrator/agent.toml`, `agents/planner/agent.toml`, `agents/critic/agent.toml` | **Missing.** Aura's `internal/swarm/plan.go` hardcodes 5 roles (librarian, critic, researcher, skillsmith, synthesizer) in `defaultPlanRoles` (`plan.go:17`) with prompts/toolsets baked into Go functions (`rolePrompt`, `roleReadOnlyTools`, `RoleMaxToolCalls`). No data-driven registry, no TOML, no workspace overrides. |
| 3 | **AgentDefinitionRegistry** | Singleton registry of definitions. Built-ins compiled in (`builtin_definitions.rs`); workspace TOMLs loaded at boot (`definition_loader.rs`); user TOMLs override built-ins by id. Loader runs static validators (`validate_tier_hierarchy`). | `agent/harness/definition.rs` (struct), `agents/loader.rs:validate_tier_hierarchy` | **Missing entirely.** No registry, no boot-time validation, no override mechanism. |
| 4 | **ToolDispatcher trait** | Abstracts tool-call dialect (Native function-calling / XML / P-Format). One dispatcher selected per provider. Same loop code drives Claude, GPT, Gemini, local Ollama. | `agent/dispatcher.rs:41` (`trait ToolDispatcher`), `dispatcher.rs:69` (`XmlToolDispatcher`), `dispatcher.rs:180` (`PFormatToolDispatcher`) | **Partial.** Aura's `internal/llm/client.go::Stream` accumulates tool-call fragments and only supports OpenAI-style native calls — no dialect abstraction. Switching to a model without native tool-calling would require rewriting the LLM client. |
| 5 | **ParentExecutionContext (task-local)** | Tokio task-local that lets any tool inside `Agent::turn` reach the parent's provider, full tool registry, model, memory, integrations, progress sink — without widening the `Tool` trait signature. The plumbing for sub-agent spawning. | `agent/harness/fork_context.rs:33` (`ParentExecutionContext`), `fork_context.rs:119` (`PARENT_CONTEXT` task-local) | **Missing.** Aura passes `ctx context.Context` everywhere with ad-hoc `WithUserID` helpers (`executor.go:188`). Swarm parent→child wiring is explicit in `Assignment` struct (`swarm/types.go:64`) — no implicit context inheritance, no shared provider Arc, no KV-cache reuse strategy. |
| 6 | **run_subagent** | Sub-agent execution entry. Reads `PARENT_CONTEXT`, resolves model (inherit/hint/exact), filters parent's tools per definition, builds narrow system prompt (omit_identity/memory/safety/skills sections), runs inner tool-call loop, returns ONE compact text result. Intra-sub-agent history never leaks back to parent. | `agent/harness/subagent_runner/ops.rs:219` (`run_subagent`), `ops.rs:296` (`run_typed_mode`) | **Partial.** Aura's `internal/swarm/manager.go::Run` dispatches `Assignment`s through an `AgentRunner` interface (`manager.go:19`), but the runner reconstructs everything from scratch — no KV-cache awareness, no implicit prompt-section stripping, no model-hint resolution. Each child rebuilds tool/state/client. |
| 7 | **ToolScope + tool filtering** | Per-archetype declarative tool visibility: `ToolScope::Named(list)` / `Wildcard` / `Skill(filter)` plus `disallowed_tools`, `extra_tools`, `skill_filter`. Resolved at spawn time to a `visible_tool_names: HashSet<String>` whitelist passed into `run_tool_call_loop`. | `agent/harness/definition.rs:106` (`tools: ToolScope`), `subagent_runner/ops.rs:417` (`filter_tool_indices`), `harness/tool_loop.rs:127` (whitelist check) | **Partial.** Aura's `internal/agent/executor.go:171` does have `allowlist []string` checked per dispatch, and `internal/agent/tools/toolsets` defines role tool lists (`internal/swarm/plan.go::roleReadOnlyTools`). But there's no shape that lets a non-orchestrator definition declare "wildcard minus these" or "everything in skill X" — flat allowlist only. |
| 8 | **PromptSource (Inline / File / Dynamic)** | Prompts are either inline TOML strings, file paths, or Rust functions that take a `PromptContext` and return rendered text. Lets per-archetype prompts use shared template fragments while staying data-driven. | `agent/harness/definition.rs:50-53` (`PromptSource`), `agents/orchestrator/prompt.rs`, `agents/planner/prompt.rs` | **Missing.** Aura's prompt overlays (`SOUL.md`, `AGENT.md`, `USER.md`, `TOOLS.md` from `PROMPT_OVERLAY_PATH`) are per-process globals, not per-archetype. Swarm role prompts live in Go string literals (`internal/swarm/plan.go:228-243`). |
| 9 | **agent_tier + spawn hierarchy** | Three-tier model: `chat` / `reasoning` / `worker`. Chat may spawn reasoning + worker; reasoning may spawn worker; worker may not spawn. Loader-time static check (`validate_tier_hierarchy`) refuses to boot a registry that violates the rule. Planned runtime depth-counter task-local. | `agent/harness/definition.rs` (`AgentTier` field), `agents/loader.rs:validate_tier_hierarchy`, `gitbooks/.../agent-harness.md:175-205` | **Missing.** Aura has `defaultMaxDepth = 1` (`internal/swarm/manager.go:16`), a hard cap of one delegation level. No tier concept, no static validation. Sub-agents simply cannot spawn (manager rejects depth > 1). |
| 10 | **StopHook trait (mid-turn)** | Policy-driven kill switches checked between iterations of the tool-call loop. Built-ins: `BudgetStopHook` (caps cumulative USD), `MaxIterationsStopHook`. Riding on a task-local (`CURRENT_STOP_HOOKS`) so they don't pollute the loop signature. | `agent/stop_hooks.rs:42` (`trait StopHook`), `stop_hooks.rs:73` (task-local), `harness/tool_loop.rs:170-225` (hook check) | **Partial.** Aura's `Options.BeforeLLM` (`loop.go:108`) is a single closure called once per iteration — same idea, but no trait, no composition, no separate budget vs. max-iterations decomposition. `MaxToolCalls`/`MaxIterations`/`MaxElapsed` are hardcoded checks inside `runLoop`. |
| 11 | **PostTurnHook trait** | After-turn fire-and-forget hooks. Receive a `TurnContext` snapshot (user msg, assistant reply, tool_calls with outcomes, duration, iteration count). Built-ins: archivist (memory distillation), learning (reflection/tool-tracker/user-profile), cost log, episodic memory indexing. Run via `tokio::spawn` — user sees the reply before any hook finishes. | `agent/hooks.rs:80` (`trait PostTurnHook`), `hooks.rs:16` (`TurnContext`) | **Missing.** Aura has `OnStats`/`OnToolEnd`/`OnLLMDelta` callbacks (`loop.go:111-119`) — but they fire inline, blocking the user, and don't carry the full turn snapshot. No post-turn archivist or learning loop. |
| 12 | **InterruptFence** | User-driven cancellation. Checked at safe points (before each tool exec, before each sub-agent spawn, before each provider call). Shared via `Arc` so child agents see the same flag. | `agent/harness/interrupt.rs` | **Partial.** Aura uses Go's `context.Context` cancellation, which works similarly but doesn't have explicit "safe points" — cancellation is checked wherever someone happened to call `ctx.Err()`. |
| 13 | **TriggerEnvelope + triage pipeline** | External events (webhooks, cron, Composio fires) flow through `run_triage` → `TriageDecision` (drop / notify / spawn_reactor / spawn_orchestrator) before hitting `Agent::turn`. Small local LLM classifies; cloud-LLM fallback on parse error. Decisions cached. | `agent/triage/mod.rs:34-46` (re-exports), `triage/evaluator.rs`, `triage/escalation.rs` | **Missing.** Aura's cron + Telegram webhook paths feed straight into the agent loop. No classification, no noise-drop, no notify-only path. |
| 14 | **Payload summarizer (dedicated sub-agent)** | Oversized tool results (>threshold tokens) get routed through a `summarizer` sub-agent before entering parent history. Compresses per an extraction contract that preserves identifiers and key facts. Circuit breaker disables after 3 consecutive failures. | `agent/harness/payload_summarizer.rs`, `subagent_runner/handoff.rs::HANDOFF_OVERSIZE_THRESHOLD_TOKENS` | **Missing.** Aura hard-truncates at `MaxToolResultChars` (`loop.go:122`) — no LLM-based compression, no preservation contract. |
| 15 | **TaskBoard (per-thread kanban)** | Persistent per-thread JSON board under `<workspace>/agent_task_boards/<hex(thread_id)>.json`. The `todowrite` tool mutates it; UI fetches/replaces via `threads.task_board_*` RPC. Cards have `id/title/status/notes/blocker/order/updated_at`. | `agent/task_board.rs:55` (`TaskBoard`), `task_board.rs:73` (`TaskBoardStore`) | **Missing.** Aura has SQLite `scheduled_tasks` but no per-thread agent-visible task board. The agent has no "shared notepad" to track multi-step work across delegations. |
| 16 | **EventBus / native-bus dispatch** | Agent turns can be invoked through `agent.run_turn` on the core event bus with an `AgentTurnRequest` carrying owned trait objects (provider, tool registry, streaming senders) — no serialization, no harness import for consumers. | `agent/bus.rs:48` (`AgentTurnRequest`), `bus.rs:33` (`AGENT_RUN_TURN_METHOD`) | **Missing.** Aura's channel layer (`internal/channels/telegram/`) calls the loop directly. No internal RPC layer; bridges build the call site themselves. |
| 17 | **Cost / TurnCost accumulator** | Every provider response carries authoritative `charged_amount_usd`. `TurnCost` sums input/output/cached tokens + USD across all provider calls inside one turn. Drives budget stop-hook + per-iteration progress events + end-of-turn cost line. | `agent/cost.rs::TurnCost` | **Partial.** Aura has `Stats.TokensTotal/CostUSD` (`loop.go:188`) — counted but not authoritative (no backend cost surface), not used to gate the loop. |

---

## 2. Control flow walkthrough — single-agent turn

### OpenHuman (one orchestrator turn)

1. **External trigger → triage.** A webhook/cron/composio event lands. `run_triage` (small local LLM, `triage/evaluator.rs`) classifies it → `TriageDecision`. Only "spawn_orchestrator" reaches `Agent::turn`. User-typed messages skip triage and go straight to step 2.
2. **`Agent::turn(user_message)` enters** (`session/turn.rs:62`). On first turn: builds system prompt with identity/soul/memory/integrations/tools sections (KV-cache anchor); on subsequent turns: reuses byte-identical prompt verbatim.
3. **Memory context injection.** `MemoryLoader` queries the Memory Tree for chunks relevant to the new user message, attaches citations, splices them into the user-visible message (NOT the system prompt, to preserve KV-cache prefix).
4. **`ParentExecutionContext` snapshot built and installed as task-local.** Any tool running inside this turn — especially `spawn_subagent` — now has access to provider, full tool registry, model, memory, integrations.
5. **Inner tool-call loop enters** (`harness/tool_loop.rs:100 run_tool_call_loop`):
   - **Stop hooks fire** (budget, max-iterations).
   - **Context guard** checks total history vs. context window; if needed, microcompact/autocompact summarises older middle turns (system prompt + recent turns kept verbatim).
   - **Tool spec list assembled** = `tools_registry ∪ extra_tools`, filtered by `visible_tool_names: Option<HashSet<String>>`. Schema sent to provider on every call (not part of KV-cache prefix — can change mid-session for Composio connect/disconnect).
   - **Provider call** streams the response.
   - **Dispatcher parses** assistant text + tool calls (`ToolDispatcher::parse_response` handles Native/XML/P-Format dialects).
   - **If no tool calls → return final text** to `Agent::turn`.
   - **Tool calls execute** in parallel (`harness/tool_loop.rs` dispatch).
   - **Oversized results** → routed through the summarizer sub-agent (`payload_summarizer.rs`).
   - **Self-healing** for missing shell commands → spawns `tool_maker` sub-agent inline.
   - **Results appended** to history; loop iterates.
6. **Final assistant text returned** to `Agent::turn`.
7. **Post-turn hooks `tokio::spawn`'d** with `TurnContext` snapshot — archivist + learning + cost log + episodic memory indexing — user already saw the reply.

### Aura (current, for comparison)

1. **No triage layer.** Telegram webhook (`internal/channels/telegram/inbound.go`) builds `agent.Task` and calls into the loop directly.
2. **`runLoop` entered** (`internal/agent/loop.go:213`). System prompt assembled inline (`initialMessages`); no first-turn vs. subsequent-turn distinction → every turn rebuilds, every turn loses KV-cache prefix on the provider side.
3. **Per-iteration message governance** (`governance.Apply`, `loop.go:307`) applies `MaxToolResultChars`/microcompact heuristics. Closer to openhuman's context guard but simpler.
4. **`BeforeLLM` callback** (single closure, `loop.go:108`) — equivalent of one stop-hook slot.
5. **Tool defs assembled** via `toolPool` (`loop.go:298`) with `ToolResolver` for on-demand schema lookup. No per-agent filter beyond `executor.go:171`'s allowlist.
6. **LLM call** through `ChatClient.Chat` (`loop.go:21`).
7. **Tool calls parsed** by the LLM client itself (`internal/llm/client.go` accumulates fragments); no dispatcher abstraction.
8. **`agentExecutor.ExecuteToolCalls`** (`executor.go:72`) fans calls out in parallel goroutines; results wrapped via `WrapUntrustedToolResult`, capped, added to state.
9. **Final answer** returned synchronously from `runLoop`.
10. **No post-turn hooks.** Whatever happens after the user gets their reply is inline cost logging + Stats emission.

### Structurally different

- **Aura has no `Agent` struct.** State, executor, client, and options are wired by every caller (`RunTask`, swarm child, Telegram bridge). OpenHuman has one `Agent` that owns its loop, history, memory loader, tool registry, hook list, budget.
- **Aura has no task-local parent context.** Tool implementations cannot reach "the running agent's provider/tools" — they receive only `ctx.Context` + args. Swarm parent→child wiring goes through `Assignment` (explicit copy), not implicit inheritance.
- **Aura has no per-archetype prompt/tool/model definition.** Swarm roles are 5 hardcoded strings in Go. OpenHuman has 15+ TOML-defined archetypes plus user-extensible workspace TOMLs.
- **Aura has no tier / spawn-depth model beyond `MaxDepth=1`.** Hierarchical DAG is structurally impossible today — the manager rejects depth-2 spawns by default.
- **Aura has no triage / no post-turn hooks / no payload summarizer / no task-board / no event-bus dispatch.** All four are deferred Phase 8 territory but they're not load-bearing for B/C/D.

---

## 3. Multi-agent patterns enabled by the architecture

### Pattern B — Plan-Execute (planner produces plan, executor runs steps)

**OpenHuman:** Drop-in via the existing `planner` archetype (`agents/planner/agent.toml`) + `todowrite` / `plan_exit` direct tools.

- Orchestrator (chat tier) calls `delegate_plan`. `planner` runs as a sub-agent (`reasoning` tier, `read_only` sandbox), produces a structured plan as its single text result + writes a todo list via `todowrite` (`agents/orchestrator/agent.toml:130`, `agents/planner/agent.toml:46`).
- Orchestrator reads the plan, decides which downstream worker(s) to delegate each step to (`code_executor`, `researcher`, …), runs them in parallel via the same `spawn_subagent` mechanism.
- **Built-in primitives:** `AgentDefinition` (data-driven planner), `ToolScope::read_only`, `plan_exit` marker for plan→build mode boundary, `TaskBoard` for cross-delegation state.
- **New code required:** zero. The shape is already there. Files: `agents/planner/`, `agents/orchestrator/`, `harness/subagent_runner/ops.rs`.

### Pattern C — Critic-Review (worker, critic chain)

**OpenHuman:** Drop-in via `critic` archetype (`agents/critic/agent.toml`).

- Critic is a worker-tier sub-agent with `sandbox_mode = "read_only"`, narrow tools (`read_diff`, `run_linter`, `run_tests`, `file_read`), 5 iterations cap (`agents/critic/agent.toml:7`).
- Orchestrator calls `delegate_run_code` → gets diff. Then calls `delegate_review_code` (`critic`) → gets adversarial review. Then re-spawns `code_executor` if critic flagged anything. Loop is the orchestrator's decision, not the harness's.
- **Built-in primitives:** archetype, narrow tool scope, sandbox mode (`SandboxMode::ReadOnly` at `definition.rs:144`), result truncation via `max_result_chars`.
- **New code required:** zero for one-shot critique. For iterative refine, the orchestrator's prompt already encodes "Review results. Retry or adjust if needed" (`agents/orchestrator/prompt.md:11`).

### Pattern D — Hierarchical DAG (recursive sub-agents)

**OpenHuman:** Bounded but supported via the **three-tier hierarchy** (`agent_tier`).

- `chat → reasoning → worker` is the canonical deep path (`agents/planner/agent.toml:18`).
- `reasoning` agents (planner) decompose a goal into worker sub-tasks; orchestrator (chat) dispatches workers in parallel.
- **Hard cap:** `MAX_SPAWN_DEPTH = 3` (loader-time static check is live; runtime depth-counter task-local is documented as the planned defence-in-depth — `gitbooks/.../agent-harness.md:204`).
- **NOT a fully recursive DAG.** Workers are leaves by contract. A worker cannot spawn — `subagent_runner/ops.rs:445-456` strips `spawn_subagent` and every `delegate_*` tool from every sub-agent's tool surface unconditionally. The orchestrator is the only node that delegates.
- **What this means:** "Hierarchical DAG" in openhuman = "fan-out from chat through one reasoning hop into one parallel layer of workers", not "arbitrary nesting".
- **Built-in primitives:** `AgentTier` enum, `validate_tier_hierarchy`, the strip-spawn-tools guard at `subagent_runner/ops.rs:446`.
- **New code required:** zero for the bounded form. Truly recursive (e.g. orchestrator → orchestrator → orchestrator) is deliberately blocked.

---

## 4. What Aura is missing to enable Phase 8 easily

| Δ | Missing / different in Aura | Effort to add | Phase 8 strictly requires? | Workaround |
|---|---|---|---|---|
| **D1** | **First-class `AgentDefinition` + registry.** TOML-loaded archetypes with prompt/tools/model/sandbox/tier/max_iter. | **Medium (1-2 sessions).** Define struct + TOML loader + boot-time registration. Mirror openhuman's `definition.rs` shape. Reuse Aura's existing `internal/agent/tools/sets` + `internal/swarm/plan.go` role definitions as the bootstrap content. | **Yes for B/C/D maintainability.** Without it every new specialist is a Go-code edit. | Hardcode 3-4 more roles in `plan.go` (current shape). Works for B/C, doesn't scale to D. |
| **D2** | **`Agent` struct that owns turn-local state.** Right now `runLoop` is stateless and every call rebuilds. KV-cache reuse is impossible. | **Medium (1 session).** Refactor `RunTask` to instantiate an `Agent{provider, tools, state, opts, hooks}` once, then call `agent.Turn(msg)` repeatedly. Existing `internal/agent/session.go` and `state.go` are the seed. | **Soft yes.** Without it, multi-turn delegations re-prefill the parent's prompt on every nested call → cost + latency multiplier. | Keep the stateless shape; just accept the cost. Most local LLMs don't bill, so this hurts cloud routing more than local. |
| **D3** | **Parent-execution-context plumbing.** Today Aura's swarm wiring copies prompts/tools through `Assignment`. There's no implicit inheritance, no shared provider Arc. | **Medium (1 session).** Go doesn't have task-locals natively but `context.Context` values + a `WithParent(ctx, *ParentSnapshot)` helper at `internal/agent/runtime.go` solves the same problem. The `internal/swarm/manager.go::AgentRunner` interface is the natural seam. | **Yes for cheap sub-agents.** Without shared Arcs, every child rebuilds tool registry / provider / state — wasteful at depth >1. | Continue passing everything explicitly. Verbose but functional. |
| **D4** | **Spawn-depth gate beyond `MaxDepth=1`.** Aura's manager rejects depth-2 spawns (`internal/swarm/manager.go:16`). Hierarchical DAG cannot exist today. | **Small (½ session).** Bump default `MaxDepth` to 3, add tier-based static validation in `internal/swarm/plan.go::normalizePlanRoles`, propagate depth in `Assignment.Depth` (already a field, `types.go:73`). | **Yes for D.** This is the literal blocker. | None — D requires this. |
| **D5** | **Tool-call dispatcher abstraction.** `internal/llm/client.go` only supports OpenAI-shape native function calls. | **Large (2 sessions).** Define `internal/llm/dispatcher.go` mirroring openhuman's `ToolDispatcher` trait, refactor `client.go` to dispatch through it, add Native + XML fallback impls. | **No.** Local LLMs Aura uses (qwen-coder, glm-4.5, etc.) have native tool-calling. Strictly optional for Phase 8. | Skip. Only needed when we want to run models without native tool-calling. |
| **D6** | **Stop-hooks + post-turn hooks as composable traits.** Aura has single-closure callbacks. | **Small (½ session).** Define `type StopHook interface { Check(state TurnState) Decision }` and `type PostTurnHook interface { OnTurn(ctx context.Context, tc TurnContext) }`; replace the closures in `Options`. | **Soft yes.** Critic-review (C) benefits from a built-in CriticHook that fires after a code_executor sub-agent returns and re-spawns if quality is low. Doable as orchestrator-prompt logic too. | Keep closures; bake critique loop into the orchestrator prompt. |
| **D7** | **Payload summarizer sub-agent.** Aura hard-truncates oversized tool results. | **Small (½ session) once D1 lands.** Add `summarizer` archetype; hook into `governance.Apply` to route oversize payloads through it before history append. | **No.** Aura's wiki / sources stay <24k chars per result usually. Skip for v1. | Keep truncation. |
| **D8** | **Per-archetype prompt-section stripping.** OpenHuman's `omit_identity/memory/safety/skills/profile` flags. | **Small.** Add a per-archetype prompt builder that takes flags and assembles only the requested overlays from `PROMPT_OVERLAY_PATH`. | **Yes for D efficiency.** A `worker` agent that inherits SOUL+AGENT+USER+TOOLS overlays for every spawn is ~3-5k tokens of waste per leaf call. | Bake the worker prompts inline (current shape). |
| **D9** | **TaskBoard for shared cross-delegation state.** | **Small (½ session).** SQLite-backed `agent_task_boards` table + `todowrite` tool. | **Soft yes for D.** Without a board the planner's output is just text — the orchestrator has no structured way to track which steps are done across multiple delegations. | Encode the plan in the orchestrator's prompt context; track inline. |

---

## 5. Bottom-line: Phase 8 substrate effort estimate (revised)

**Original PRD estimate:** 6-12 sessions, anchored to a concrete workload.

**Revised lower bound (adopt openhuman patterns wholesale):**
- D1 (AgentDefinition registry) — 1 session
- D2 (Agent struct, KV-cache prefix) — 1 session
- D3 (ParentExecutionContext via ctx.Value) — 1 session
- D4 (depth gate + tier check) — ½ session
- D8 (prompt-section stripping) — ½ session
- D9 (TaskBoard + todowrite) — ½ session
- Pattern B (planner+executor wiring with the new substrate) — 1 session
- Pattern C (critic+review wiring) — ½ session
- Pattern D (hierarchical DAG, 3-hop bounded) — 1 session
- E2E benchmark probe through `probe_chat` — 1 session

**Total: ~8 sessions.** Lower than the original 6-12 *only* by skipping D5/D6/D7 (dispatcher abstraction, hook traits, payload summarizer) which are nice-to-haves.

**Revised upper bound (re-design from scratch, no prior art):** 12-15 sessions. The PRD's 12 was about right for cold start.

**Recommended path:** 3-phase staircase.
- **Phase 8a (3 sessions):** D1+D2+D4 land the data-driven substrate. Aura keeps its current swarm shape but archetypes move to TOML and depth lifts to 3 with tier validation. Concrete deliverable: rewrite the existing 5 swarm roles as TOML archetypes, prove the loader + spawn path on master.
- **Phase 8b (3 sessions):** Patterns B + C ship on top of 8a. Add `planner` and `critic` archetypes; orchestrator (= current Aura agent) delegates via the existing swarm tools. Concrete deliverable: a probe_chat case that asks "plan and execute X with review" and verifies plan→exec→critic→exec chain in artifacts.
- **Phase 8c (2 sessions):** Pattern D (bounded 3-hop hierarchical DAG) + D9 TaskBoard. Concrete deliverable: marketing-research workload demonstration with reasoning→worker fan-out.

---

## Bottom-line for the user (200 words)

OpenHuman's harness is unusually clean prior art for what Phase 8 needs. The load-bearing patterns — `AgentDefinition` as TOML-loaded data, a parent-context task-local, three-tier spawn hierarchy with a hard depth cap, and pattern-coded archetypes (planner/critic/researcher) — are exactly the abstractions Aura's current `internal/swarm/plan.go` hardcodes in Go. Adopting them conceptually (the code is GPLv3 — concepts only) collapses Phase 8 from "build everything" to "refactor swarm into a registry + lift depth from 1 to 3 + add three TOML archetypes".

My revised estimate is **~8 sessions** for the full B+C+D triad, structured as a 3+3+2 staircase: substrate (TOML registry + Agent struct + tier/depth gate) → patterns B+C (planner, critic land on the new substrate) → pattern D (bounded 3-hop DAG + task board).

The two things openhuman teaches that I'd commit to even before Phase 8 starts: (1) **stop hardcoding swarm roles in Go** — that's the single biggest velocity tax we have; (2) **bound recursion via a tier+depth check at the loader, not just at the runtime manager** — cheaper than discovering a runaway DAG in production.

Skip D5/D6/D7 (dispatcher trait, hook traits, payload summarizer) — they're polish, not blockers.

---

*Source file: `D:/Aura/docs/openhuman-harness-architecture-2026-05-18.md`*
