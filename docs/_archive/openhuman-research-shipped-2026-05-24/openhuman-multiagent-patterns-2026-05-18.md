# OpenHuman multi-agent patterns — extracted for Aura Phase 8 evaluation

**Source tree:** `D:/tmp/openhuman/` (Rust, MIT-licensed AI-shell, target of acquisition by an AI tools company)
**Analysis date:** 2026-05-18
**Scope of interest:** Phase 8 "multi-agent substrate" patterns deferred in Aura's roadmap (`docs/phase-mm-synthesis-2026-05-17.md` notwithstanding — that's multimodal; Phase 8 is the substrate piece).

## Inventory of `openhuman/examples/`

The directory is essentially **empty for multi-agent purposes**. Only one file lives there:

| filename | one-line purpose | language | pattern (A-G) | reusable for Aura? |
|---|---|---|---|---|
| `mouse_smoke.rs` | Manual smoke test for humanized cursor `MouseTool` (#682) — verifies cursor curves vs. teleports | Rust | None (single-tool sanity) | No |

**Conclusion on `examples/`:** there is no example-as-pattern-library here. The real prior art is in `src/openhuman/agent/`. The remainder of this document operates on that.

## Map of the real surface area (`src/openhuman/agent/`)

| Path | Purpose |
|---|---|
| `harness/session/turn.rs` | `Agent::turn` lifecycle — the entry per user message/webhook/cron |
| `harness/tool_loop.rs` | Inner tool-call loop (provider call → parse → execute → loop, ≤10 iters default) |
| `harness/subagent_runner/ops.rs` | `run_subagent` — the function that spawns a typed sub-agent |
| `harness/subagent_runner/types.rs` | `SubagentRunOptions`, `SubagentRunOutcome`, `SubagentMode::Typed` |
| `harness/fork_context.rs` | `ParentExecutionContext` task-local — Arc-shared parent state propagated to children |
| `harness/definition.rs` | `AgentDefinition`, `AgentTier`, `ModelSpec` |
| `harness/payload_summarizer.rs` | Oversized tool-result → `summarizer` sub-agent detour |
| `harness/self_healing.rs` | Missing-command interceptor → `ToolMaker` sub-agent polyfills |
| `harness/interrupt.rs` | `InterruptFence` — Ctrl+C cancellation propagated via Arc to all children |
| `harness/tool_filter.rs` | Composio-toolkit action ranking (verb/token overlap heuristic, no LLM) |
| `agents/<archetype>/agent.toml + prompt.md` | One TOML+MD pair per archetype: orchestrator, planner, researcher, critic, code_executor, archivist, summarizer, tool_maker, integrations_agent, trigger_triage, trigger_reactor, welcome, morning_briefing, crypto_agent, skill_creator, tools_agent, help |
| `agents/loader.rs` | Registry merge (built-ins + workspace TOMLs), tier validation at boot |
| `tools/impl/agent/spawn_parallel_agents.rs` | Tool: fanout N sub-agents concurrently, join results |
| `tools/impl/agent/spawn_worker_thread.rs` | Tool: spawn child in a dedicated persisted thread (decoupled from parent transcript) |
| `tools/impl/agent/delegate.rs` | Tool: `delegate(agent, prompt, context)` — one-shot specialist call |
| `tools/impl/agent/plan_exit.rs` | Tool: `plan_exit { plan }` emits `[plan_exit]` marker for plan→build mode switch |
| `triage/` | External-trigger classification: small local LLM → drop / notify / spawn-reactor / spawn-orchestrator |

## Patterns found

### A. Multi-agent fanout (parent dispatches N children, collects results)

- **Implements:** YES
- **File:line:** `src/openhuman/tools/impl/agent/spawn_parallel_agents.rs:108-411` (`SpawnParallelAgentsTool::execute`). The orchestrator agent registers it as a direct tool (`agents/orchestrator/agent.toml:111`).
- **Algorithmic shape:**
  - Tool input: `{ tasks: [{ agent_id, prompt, context?, toolkit?, ownership? }] }`, schema enforces `minItems: 2`.
  - Validates each task (known `agent_id` in registry, required `toolkit` for `integrations_agent`, non-empty prompt).
  - Caps fanout to `parent.agent_config.max_parallel_tools` (rejects with explicit error if exceeded).
  - Wraps each prompt with a `[Ownership Boundary]\n…\n\n[Task]\n…` prefix when `ownership` is supplied (a *prompt-level* contract — no schema enforcement).
  - Builds futures via `run_subagent(definition, prompt, SubagentRunOptions{toolkit_override, context, task_id, …})`, runs them under `futures::future::join_all`.
  - Emits lifecycle events on a domain event bus (`SubagentSpawned`/`Completed`/`Failed`) and to a progress sink for UI streaming.
  - Aggregates `{ total, succeeded, failed, results: [{ task_id, agent_id, success, output?, error?, ownership?, elapsed_ms, iterations }] }` into a single pretty-JSON tool result for the parent.
- **What Aura would need to port:** Aura already has the substrate (`internal/swarm/manager.go`, `internal/swarm/plan.go` with `Assignment`/`Plan`/`RunResult`, plus the tool wrappers in `internal/agent/tools/swarm/tools.go`). The missing pieces vs openhuman:
  1. Explicit `ownership` boundary prompt prefix (a 3-line helper).
  2. Per-task `context` injection separate from prompt (Aura's swarm currently routes everything through `Assignment.Subject`/`Goal`).
  3. The `max_parallel_tools` cap exists in spirit (`MaxActive` in `manager.go`); the difference is it's a global manager cap, not a per-tool-call cap.
- **Effort:** **Small.** This is mostly already shipped. ~1 story to add ownership-boundary helper + per-task context.

### B. Plan-Execute (planner agent produces a plan, executor runs each step)

- **Implements:** YES — and the contract is explicit.
- **File:line:**
  - Planner archetype: `agents/planner/agent.toml` (read-only, `agent_tier = "reasoning"`, `[model] hint = "reasoning"`, tools = `file_read/grep/glob/list/todowrite/plan_exit/web_fetch/memory_recall/web_search_tool/parallel_search/parallel_chat/parallel_research/stock_*/composio_list_*/composio_execute`).
  - Planner prompt: `agents/planner/prompt.md` — emits a *strict JSON DAG schema*: `{ root_goal, context_gathered, nodes: [{ id, description, agent_id, depends_on, acceptance_criteria }] }` (max 8 nodes, no cycles).
  - Handoff marker: `tools/impl/agent/plan_exit.rs:19` defines `pub const PLAN_EXIT_MARKER: &str = "[plan_exit]"`. The planner calls `plan_exit { plan }`; the harness grep-detects the marker on the result and (per code comment) is meant to switch into "build mode". As of the source, the mode switch is **not yet wired** — the marker is a stable boundary the orchestrator's harness can recognize, but the actual plan-mode → build-mode runner is referenced as "follow-up" (issue #1205, #1208).
  - Executor side: any worker-tier agent in the plan's `nodes[].agent_id` list runs via `run_subagent`. The orchestrator drives the loop: it receives the planner's JSON, then issues parallel/serial `delegate_*` or `spawn_parallel_agents` calls based on `depends_on`.
- **Algorithmic shape:**
  - Orchestrator (chat tier) sees the user goal.
  - Decision tree (`agents/orchestrator/prompt.md:13-37`) routes to `delegate_plan` when "complex multi-step decomposition is required".
  - Planner (reasoning tier, separate model) gathers context via `memory_recall`/`web_search_tool`, emits JSON DAG, calls `plan_exit { plan: <JSON> }`.
  - Orchestrator receives `[plan_exit]\n<plan>`, parses, walks `nodes`:
    - Independent nodes (empty `depends_on`) → `spawn_parallel_agents` batch.
    - Dependent nodes wait for their predecessors' outputs to land in context.
- **What Aura would need to port:**
  1. **Planner archetype** as a Go struct + prompt: input = user goal, output = a JSON DAG with the exact schema above. The "agent_id" enum maps to Aura's existing roles (`librarian/critic/researcher/skillsmith/synthesizer`) — possibly extend with `code_executor`-equivalent later.
  2. **Plan-exit boundary marker** (trivial — a constant + a tool stub).
  3. **DAG walker in the orchestrator path** that consumes the planner's JSON and produces serial/parallel `swarm.RunRequest` calls based on `depends_on`. Aura's `swarm.Manager` already runs a flat batch; the DAG walker needs to (a) topologically sort, (b) feed predecessor outputs into successor prompts.
  4. **`plan_mode` boolean on the agent loop** (off by default). When the planner runs as a sub-agent its tools are read-only (matching `sandbox_mode = "read_only"`).
- **Effort:** **Medium.** ~3-5 stories. The hardest part is the DAG walker with dependency-aware context injection; the planner archetype is mostly prompt engineering.

### C. Critic-Review (worker produces draft, critic reviews and corrects)

- **Implements:** YES, but as a **standalone read-only worker**, not as an automatic loop.
- **File:line:**
  - Critic archetype: `agents/critic/agent.toml` (`sandbox_mode = "read_only"`, `[model] hint = "agentic"`, tools = `read_diff/run_linter/run_tests/file_read`, `max_iterations = 5`).
  - Critic prompt: `agents/critic/prompt.md` — a fixed checklist (security → correctness → style → tests → SOUL.md compliance). Output is text findings; the critic does not edit code.
  - Orchestrator routes via `delegate_critic` (`agents/orchestrator/agent.toml:53`, prompt step "If code review is requested").
- **Algorithmic shape:**
  - Worker (`code_executor`) writes/changes code in workspace.
  - Orchestrator (or planner-generated DAG) issues `delegate_critic`.
  - Critic reads the diff via `read_diff`, runs `run_linter`/`run_tests`, returns a prioritized findings list.
  - Orchestrator (or the user) decides whether to loop back to `code_executor` with the findings. **No automatic retry loop** — the model decides per turn.
- **What Aura would need to port:**
  1. A critic role prompt template. Aura already has a `critic` role-slot in `swarm.defaultPlanRoles` (`internal/swarm/plan.go:17`) — only the prompt is missing.
  2. Optional: an explicit "draft → review → revise" mini-pattern in the orchestrator prompt that says *"after `code_executor` produces a change, call `delegate_critic`, then if `critic.severity >= medium`, loop back once"*. openhuman does NOT wire this — it's left to model judgment.
- **Effort:** **Small.** ~1-2 stories (prompt + acceptance criteria + a "critic findings ingest" path that re-prompts the writer). The hardest part is making the retry loop deterministic, not the role itself.

### D. Hierarchical DAG (tree of agents, each can spawn sub-agents)

- **Implements:** PARTIAL — a **bounded** hierarchy with hard-coded depth ≤2 (orchestrator → planner → workers) and **leaves cannot spawn**.
- **File:line:**
  - Tier model: `harness/definition.rs` — `AgentTier::{Chat, Reasoning, Worker}`. Encoded in each `agent.toml` as `agent_tier = "…"`.
  - Allowed transitions (gitbooks `agent-harness.md:174-203`): `chat → {reasoning, worker}`, `reasoning → worker`, `worker → nothing`.
  - Enforcement (loader-time, static): `agents/loader.rs:validate_tier_hierarchy()` rejects a registry where a `chat` agent lists another `chat` in `subagents`, or where a `worker` lists any subagents.
  - Runtime depth gate (planned, **not shipped**): `gitbooks/agent-harness.md:200-204` says a `task-local` depth counter capping chains at `MAX_SPAWN_DEPTH = 3` is "the planned defence-in-depth layer".
- **Algorithmic shape:**
  - Static: every archetype declares `agent_tier` and `subagents`. Loader builds the registry and refuses to boot on tier violations.
  - Dynamic (today): `chat → reasoning → worker` chains work; recursion is prevented by archetype design (planner does not list itself; workers list nothing).
  - Dynamic (planned): a `tokio::task_local` counter on `run_subagent` would refuse to spawn at depth 3.
  - Worker threads (`spawn_worker_thread`) carry their **own** depth-1 cap inside the tool: a worker thread cannot itself call `spawn_worker_thread` (enforced by the tool's permission_level gate plus an explicit prompt rule in `orchestrator/prompt.md:69-70`).
- **What Aura would need to port:**
  1. **AgentTier-like enum** on Aura's role definitions (currently roles are flat strings in `defaultPlanRoles`). Add `Tier` field to `swarm.Assignment` and `Manager` config.
  2. **Loader-time tier validation** — rejected at registry construction, not at spawn time.
  3. **Runtime depth task-local** — context.Context value chain in Go (`ctx = context.WithValue(ctx, depthKey, prev+1)`). Aura's `swarm.Manager` already has `defaultMaxDepth = 1` (`internal/swarm/manager.go:17`) so the substrate is in place; what's missing is **declaring** which roles are leaves vs. inner nodes.
- **Effort:** **Small.** ~1-2 stories. The biggest implementation lift is updating `defaultPlanRoles` and the prompt fragment that tells each role what it may spawn.

### E. Hybrid DAG (DAG with conditional branches based on outputs)

- **Implements:** **NO** — not in the substrate. openhuman's planner emits a static DAG; conditional branching is left to the orchestrator's per-turn model judgment (which is *not* a DAG in the formal sense, just a tool-call loop with an LLM reasoning over results).
- **File:line:** Closest thing is the trigger-triage pipeline (`triage/`), which is conditional (`drop / notify / spawn_reactor / spawn_orchestrator`) but on *external triggers*, not on intra-plan node outputs.
- **Algorithmic shape (what's missing):** A planner that emits `{ if: <predicate on node_X.output>, then: [nodes], else: [nodes] }` would be the canonical Hybrid DAG shape. openhuman could be extended this way — the JSON schema in `agents/planner/prompt.md` would gain a `branch` node type — but no such thing exists today.
- **What Aura would need:** This is *not* a port; it would be a new design. Honestly, the value is questionable: the standard answer in agentic systems is "the orchestrator re-plans after each batch lands", which gives you conditional behaviour without the schema complexity.
- **Effort:** **Large** if pursued formally. **Zero** if you take openhuman's stance ("re-plan after each batch"). Recommend the latter.

### F. Self-correction loop (agent retries with feedback from its own output)

- **Implements:** YES, in **one specific form** — the self-healing interceptor for missing shell commands.
- **File:line:** `harness/self_healing.rs:1-100`. `SelfHealingInterceptor::detect_missing_command()` pattern-matches the tool error against `MISSING_CMD_PATTERNS` (`"command not found"`, `": not found"`, …), extracts the missing command name, and if attempts < `MAX_HEAL_ATTEMPTS = 2`, spawns a `ToolMaker` sub-agent with a polyfill prompt; on success the parent retries the original call.
- **Algorithmic shape:**
  - `agent_turn` executes a tool.
  - Tool returns `is_error: true` with stderr.
  - Interceptor's `detect_missing_command` returns `Some(cmd_name)` if the pattern matches.
  - Interceptor builds a prompt → spawns `tool_maker` → polyfill script lands in `polyfill_dir/cmd_name`.
  - Loop retries the original tool call.
  - Per-command counter prevents infinite loops (cap at 2).
- **What Aura would need to port:**
  1. This is a **specific** instance of self-correction (missing command → polyfill). Aura doesn't have a Python sandbox use-case mature enough to justify it directly.
  2. The **general pattern** — "intercept tool error, spawn a sub-agent to repair, retry" — is widely useful. The minimum viable version for Aura would be a retry-with-feedback hook on `loop.go` that, on tool failure, re-prompts the parent with the error and a "consider whether the call can be repaired" instruction. This is **already implicit** in Aura's loop (the next iteration sees the tool error and the LLM decides what to do); making it explicit would just add a budgeted retry counter.
- **Effort:** **Small** for the general pattern (1 story). **Medium** if you want a missing-command-specific polyfill flow (you'd need a `ToolMaker`-equivalent + a polyfill cache).

### G. Tool-as-agent (a tool that internally runs an LLM agent loop)

- **Implements:** YES — the **payload summarizer** is exactly this.
- **File:line:** `harness/payload_summarizer.rs`. When a tool result exceeds `threshold_tokens` (default 500k), the harness routes it through a `summarizer` sub-agent (`agents/summarizer/`, model hint `"summarization"`) before appending to parent history. The summarizer is a real `run_subagent` call with its own tool loop, but from the orchestrator's perspective it's invisible — the parent sees a compressed string.
- **Also:** `integrations_agent` is tool-as-agent at a coarser grain — the orchestrator calls `delegate_to_integrations_agent(toolkit: "gmail")` and a Composio specialist agent runs inside, picking among Gmail's ~50 actions.
- **Algorithmic shape:**
  - Tool execution returns a value.
  - Wrapper checks token count.
  - Over threshold → spawn typed sub-agent (`summarizer`) with the payload as task prompt.
  - Sub-agent's output replaces the raw payload in parent history.
  - Failures circuit-break after 3 consecutive (per-session), falling back to truncation.
- **What Aura would need to port:**
  1. **`internal/agent/payload_summarizer.go`** — a wrapper around `swarm.Manager.RunOne` (or a direct LLM call) keyed off a per-tool token-count threshold.
  2. **A `summarizer` role** in `defaultPlanRoles` and a dedicated prompt (compression-preserving identifiers + key facts).
  3. **A circuit breaker** (3 consecutive failures → disable for session).
- **Effort:** **Small-Medium.** ~2 stories. Aura's current behaviour is to hard-truncate; replacing that with a summarizer detour is a strict improvement and a useful safety net independent of Phase 8.

## Agent harness architecture (gitbooks narrative)

`gitbooks/developing/architecture/agent-harness.md` is the canonical narrative. Key conceptual moves:

- **Boundary:** the harness owns "tool-call loop, sub-agent dispatch, trigger-triage pipeline, hook surface". It does **not** own provider HTTP transport, tool implementations, prompt-section assembly, or memory storage — those are composed in. (Aura draws the same boundaries: `internal/llm/`, `internal/tools/`, `internal/wiki/`, `internal/storage/*` are all composed by the agent loop.)
- **Turn lifecycle:** `Agent::turn` = resume transcript → build system prompt (first turn only — KV-cache stability) → inject memory context → enter tool-call loop → post-turn hooks (background). The system prompt is **immutable across turns** to preserve the inference backend's KV-cache prefix; dynamic context (memory recall) is appended as user-visible content instead. (Aura: same idea, looser enforcement — see `internal/agent/loop.go` system prompt assembly.)
- **Why multi-agent (the design philosophy):** "A single agent that knows everything also has a system prompt the size of a small book." Splitting work across specialists gives each child a narrow prompt + filtered tool registry + the option of a cheaper model. Crucially: **sub-agent histories never leak back to the parent** — the parent sees ONE compact tool result.
- **In-scope for the substrate:** tool dispatch dialect abstraction (Native/XML/PFormat), context guard (microcompact/autocompact), stop hooks (mid-turn policy), post-turn hooks (background distillation), payload summarizer detour, self-healing, interrupt fence, cost accounting, sandbox-mode task-local, fork-context task-local.
- **Out of scope:** explicit DAG execution engine (hierarchy is enforced at the tier level + via prompt rules, not via a graph executor), conditional plan branching, multi-agent debate (no critic-retry-critic loop), tool-result caching across agents (each spawn is independent).
- **The trigger-triage pipeline (`triage/`)** is a separate concern: external triggers (webhooks, crons, Composio events) hit a small local-LLM classifier that returns `drop / notify / spawn_reactor / spawn_orchestrator`. Aura has an analogue in `internal/channels/cron/` (cron-triggered tasks), but without a triage classifier — every cron fires straight into a full agent turn. This is a known Aura gap.

## `tool_loop.rs` review

The OpenHuman main loop (`harness/tool_loop.rs:100-300`) and Aura's `internal/agent/loop.go` are conceptually identical: provider call → parse → tool execute → loop, with a max-iterations safety cap (default 10). Differences worth borrowing:

1. **Per-iteration progress events** (`AgentProgress::IterationStarted/Completed`, `tool_loop.rs:171-183`) emitted on a `tokio::mpsc` channel. Aura emits some progress (Telegram editor throttle in `internal/channels/telegram/outbound.go`) but no structured "iteration N of M" event. A Go equivalent — `progress chan<- AgentProgress` parameter on `loop.go.Run` — would clean up streaming UI work. **Effort: small.**
2. **Stop-hook surface** (`tool_loop.rs:170-200`): a `Vec<dyn StopHook>` checked between iterations, each returning `Continue` or `Stop{reason}`. Built-ins: budget cap (USD), max-iterations override, custom kill-switches. Aura has no such surface — budget enforcement is implicit (token-count checks scattered in `internal/agent/governance/`). A pluggable `StopHook` slice would unify them. **Effort: small.**
3. **Tool-call dialect abstraction** (`ToolDispatcher` trait, three impls: Native/XML/PFormat). Aura is hard-wired to OpenAI-native via `internal/llm/client.go`. If we ever want to use a model that does only XML/PFormat tool-calls (cheap local models, some open-source releases), this trait would be the right shape. **Effort: medium. Defer until needed.**
4. **`payload_summarizer` parameter on the loop** (`tool_loop.rs:115`): `Option<&dyn PayloadSummarizer>`. Cleanly composes the "tool-as-agent" detour into the loop without polluting tool implementations. Aura should adopt this shape if it ships pattern G. **Effort: small.**
5. **`extra_tools` parameter** (`tool_loop.rs:114`): per-turn synthesised tools spliced alongside the persistent registry. Used so `delegate_*` tools are constructed fresh per turn from the active agent's `subagents` field. Aura builds the tool list once at startup; if we ship the planner archetype, per-turn `delegate_*` synthesis becomes useful. **Effort: small.**
6. **What Aura already does better:** Aura's wiki integration is tighter than openhuman's memory layer (openhuman's "Memory Tree" is a separate ingest-then-RAG store; Aura's wiki *is* the memory and uses Git for provenance). Aura's `phantom_guard.go` is a substrate-level safety net openhuman lacks.

## Per-pattern adoption recommendation for Aura

Aura's current substrate as of 2026-05-18:
- `internal/swarm/` — Plan, Assignment, Manager, Repository, parent/child run lineage, parent_child_integration_test.go.
- `internal/agent/tools/swarm/` — LLM-facing tool wrappers (delegation_policy.go, tools.go).
- `internal/channels/cron/` — delegated child agent runs on schedule.
- Existing roles in `defaultPlanRoles`: `librarian/critic/researcher/skillsmith/synthesizer`.
- Phase-R = fanout (read-only) shipped. Phase-S = `write_proposal` shipped.

| Pattern | ROI for second-brain single-user | Effort | Aura already has? |
|---|---|---|---|
| A. Fanout | **High** — needed for "research this topic across N sources in parallel" | Small (mostly shipped) | YES — Phase-R |
| B. Plan-Execute | **High** — needed for "refactor wiki section X" / "build the H1 2026 yearly review" | Medium | Partial — has `Plan` struct, missing JSON-DAG planner archetype + walker |
| C. Critic-Review | **Medium** — useful for self-correcting wiki writes (`temperature=0` plus a critic pass) | Small | Partial — has `critic` role slot, missing prompt + retry loop |
| D. Hierarchical DAG (tier-bounded) | **Medium** — caps recursion blast radius | Small | Partial — has `defaultMaxDepth=1`, missing tier annotations on roles |
| E. Hybrid DAG (conditional branches) | **Low** — re-planning per batch covers 95% of cases | Large (formal) / Zero (re-plan) | NO — and recommend NOT building |
| F. Self-correction (general retry-with-feedback) | **Medium** — useful safety net for tool failures | Small | Implicit only — explicit hook would clarify |
| F'. Self-healing polyfill (specific) | **Low** — Aura's exec sandbox is Python-only and constrained | Medium | NO — and don't need yet |
| G. Tool-as-agent (payload summarizer) | **High** — Aura currently hard-truncates oversized tool results; this is a strict win | Small-Medium | NO — and should be built independent of Phase 8 |

## Bottom-line answer

**Phase 8 substrate is "easier than expected" — PARTIAL YES.**

Rationale:
- **A (fanout)** and **D (tier-bounded hierarchy)** are essentially already shipped — Aura's `internal/swarm/` is a Go-shaped version of openhuman's `subagent_runner`. Only prompt-level wiring (ownership boundary, tier annotations on roles) is missing.
- **G (tool-as-agent / payload summarizer)** is the **highest-ROI low-effort win** and is independent of any concrete workload. It improves Aura *today* by replacing hard-truncation with intelligent compression on oversized tool outputs. Build it next, regardless of Phase 8.
- **B (Plan-Execute)** is the substantive substrate work. openhuman shows it's tractable: a planner archetype with a strict JSON DAG schema, a `plan_exit` marker, and an orchestrator-side DAG walker. The unsolved part (in openhuman too) is the **plan-mode → build-mode runtime mode switch**; both projects defer it. For Aura the pragmatic shape is: planner produces JSON, orchestrator parses and serially fires `swarm.Manager.Run` batches, no separate "build mode" needed.
- **C (Critic-Review)** is mostly a prompt and a retry budget — small. **F (general self-correction)** is similarly cheap once you accept it as "retry with the error in context, capped at N attempts".
- **E (Hybrid DAG)** is the only deferred-territory pattern that's genuinely hard, and openhuman's stance (re-plan after each batch, don't model conditionals in the DAG) is the right answer. Skip.

**Concrete starter story set** if you want to act on this:
1. **US-P8-G** — Payload summarizer (tool-as-agent), pattern G. Independent win, ship now.
2. **US-P8-A** — Add `ownership` boundary + per-task `context` to `spawn_parallel_agents` Go tool. Tiny polish.
3. **US-P8-D** — Add `Tier` field to `swarm.Assignment` + loader-time validation. Prevents accidental recursion when role list grows.
4. **US-P8-B1** — Planner archetype prompt + JSON-DAG schema validator. Read-only role.
5. **US-P8-B2** — DAG walker in orchestrator path: topo-sort, predecessor-output injection, calls `swarm.Manager.Run` batch-per-level.
6. **US-P8-C** — Critic prompt + "after worker, if critic flags severity≥medium, retry-once" loop in plan walker.
7. **US-P8-F** — Generalised tool-retry hook on `loop.go` with budgeted attempts.

Total estimate: ~7 stories, mostly small, one medium (B2 DAG walker). With Aura's existing substrate this is **2-3 sessions**, not the "needs a concrete recurring workload" months of design work the original Phase 8 scoping implied. The workload requirement was real for **E (Hybrid DAG)** specifically — and the recommendation is to skip E entirely. Once that constraint is dropped, the rest is mechanical.
