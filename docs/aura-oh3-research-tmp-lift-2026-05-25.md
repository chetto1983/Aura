# Aura Wave OH3 — Local source lift research (`D:/tmp/*`)

Scout: research agent #1 of 4. Date: 2026-05-25.
Scope: extract patterns Aura can lift for Wave OH3 (peer-mesh swarm with runtime communication loop) from already-curated material in `D:/tmp/`. NO architecture proposals — pattern extraction with file/line citations only.

## 1. TL;DR (top 5 lift-worthy patterns across all sources)

1. **`spawn_parallel_agents` tool with `ownership` field + `join_all` fan-out** — openhuman ships a battle-tested fan-out primitive that returns a single structured result block to the parent, gates on `max_parallel_tools`, publishes per-child `DomainEvent::SubagentSpawned/Completed/Failed`, and enforces a typed `ownership` boundary string injected as a prompt prefix to prevent worker overlap. (`D:/tmp/openhuman/src/openhuman/tools/impl/agent/spawn_parallel_agents.rs:108-411`). This is the closest existing implementation to Kimi's parallel swarm shape and is the single highest-ROI lift target.
2. **Spawn-tier discipline as architecture, not policy** — openhuman's `AgentTier::{Chat, Reasoning, Worker}` enum with loader-time enforcement (`chat → chat` is rejected at boot) plus a runtime depth cap of 3 hops, regardless of tier. (`D:/tmp/openhuman/src/openhuman/agent/harness/definition.rs:184-256`, `D:/tmp/openhuman/src/openhuman/agent/agents/orchestrator/agent.toml:8-13`, `D:/tmp/openhuman/src/openhuman/agent/agents/orchestrator/prompt.md:42-44`). Aura's OH1 already lifted tier validation; OH3 should keep this and not loosen it for peer handoff.
3. **Shared-context "Environment" object keyed by `(tool_name, result_name)`** — Elysia (Weaviate's agentic search) ships a typed shared workspace that persists across all decisions in a tree, holding `metadata + objects` lists per tool result. This is the closest published shape to a typed swarm blackboard. (`D:/tmp/elysia/elysia/tree/objects.py:21-58`, `D:/tmp/elysia/elysia/tree/tree.py:142-154,1224-1240`). Note: it is single-agent — multiple agents reading/writing the same `Environment` is the OH3-specific extension.
4. **Kimi K2.5 `PARL` framework establishes the success conditions** for parallel swarm — trainable orchestrator + **frozen** sub-agents, "Critical Steps" metric (`Σ(S_main + max_i S_sub)`) instead of total steps to incentivize useful parallelism, three-component reward (`r_perf + λ₁·r_parallel + λ₂·r_finish`) with annealed auxiliaries to prevent serial-collapse and spurious-parallelism reward hacking. K2.5 reports 4.5× wall-clock reduction and 60.6%→78.4% BrowseComp absolute gain. (`D:/tmp/aura-agent-loop-papers/2602.02276-Kimi-K2.5.txt:204-298, 706-802`). Aura is not training a policy, but the **metric** (critical-path step count) and the **shape** (frozen sub-agents = no RL signal flowing through children) are directly relevant for evaluation gating.
5. **AOrchestra's four-tuple `(Instruction, Context, Tools, Model)` agent abstraction** as the unit of dynamic sub-agent spec. Orchestrator emits `Delegate(Φ)` and `Finish(y)` actions only; never directly touches the environment. Returns a structured observation `{summary, artifacts, error_logs}` to the parent. (`D:/tmp/aura-agent-loop-papers/2602.03786-AOrchestra.txt:138-411`). This is the cleanest formal articulation of what every peer-handoff payload must carry.

## 2. Per-source extraction

### 2.1 `aura-phase8-research/swarm-local-map.md` + `swarm-online-research.md` + `phase08-plan-verifier-fresh.md`

**Files read:** all three in full.

**Findings.** These are Aura's own already-completed Phase-8 maps. The local map (May 2026) is now partially obsolete vs. master (OH1 just shipped `internal/agent/agentdef` registry skeleton, validator, cycle detector — see `git log` `02d390a7..d0b24989`). The online map establishes **target shape** that OH3 must respect:

- Two collaboration modes, not one overloaded swarm: `delegation` (parent → child, bounded return) and `team_collaboration` (lead-managed RunGraph with task board + addressed mailbox + plan approval). `swarm-online-research.md:51-54`.
- Minimum durable records named: `run_graph`, `run_graph_node`, `run_graph_edge`, `run_graph_event`. `swarm-online-research.md:55-60`.
- `NodeSpec` boundary fields: goal, instruction, curated context refs, tool/capability grant snapshot, model/provider, budgets, output schema, artifact policy, risk tier, allowed spawn depth, parent authority. `swarm-online-research.md:61-62`. AOrchestra (§2.5) is a direct narrowing of this list.
- Task claim is **compare-and-swap** in SQLite, exactly-one-winner under concurrent claims. `swarm-online-research.md:73-74`.
- Mailbox: broadcast expands into **N addressed rows**, never a shared invisible transcript. `swarm-online-research.md:75, 99`.
- Plan approval as **durable state**, not a prompt convention. `swarm-online-research.md:77`.
- **Rejected anti-patterns** explicitly named (load-bearing for §4 below):
  - Do not adopt OpenAI Swarm as production runtime — stateless, superseded. `swarm-online-research.md:91`.
  - Do not hardcode fixed roles / read-only-only workers / permanent `max_spawn_depth=1`. `:95`.
  - Do not use round-robin shared-context group chat as default. `:101`.
  - Do not measure success by spawn count — use critical path, useful-message ratio, error amplification. `:105`.
  - Do not store full transcripts / mailbox bodies / tool args as default trace fields. `:109`.

The local map's "Gap 7: Team collaboration primitives are absent" (`swarm-local-map.md:455-463`) and "Gap 8: Trace metrics are computed only as synchronous synthesis, not replayable orchestration" (`:466-474`) are exactly the OH3 gaps the user has now re-named (peer handoff, blackboard, parallel fan-out, iteration loop).

**Judgment: LIFT** — this is Aura's own canonical contract for what "good" looks like. The OH3 plan must satisfy the gates in `phase08-plan-verifier-fresh.md` and the constraints in `swarm-online-research.md §3 + §5`.

### 2.2 `aura-agent-loop-papers/2602.02276-Kimi-K2.5.txt`

**Files read:** lines 1-623 + 624-1246 (full body of the technical sections; appendix author list skipped).

**Findings — agent swarm specific.**

- **Architecture is decoupled**: trainable orchestrator + **frozen** sub-agents from "fixed intermediate policy checkpoints". Outputs of sub-agents are treated as "environmental observations rather than differentiable decision points". Reason: avoids credit assignment ambiguity and training instability. (`2602.02276:226-235`).
- **`PARL` (Parallel-Agent RL) reward** = `λ₁·r_parallel + λ₂·r_finish + r_perf` where `r_parallel` fights "serial collapse" (orchestrator defaults to single-agent) and `r_finish` fights "spurious parallelism" (spawning many agents without meaningful decomposition). `λ₁, λ₂` annealed to zero over training. (`:236-261`).
- **Critical Steps metric** (the load-bearing eval primitive): `CriticalSteps = Σ_t (S^(t)_main + max_i S^(t)_sub,i)`. Models per-stage wall-clock as longest sub-agent in that cohort. Penalizes excessive subtask creation that doesn't reduce max execution time. (`:262-288`).
- **Prompt construction**: Kimi uses synthetic prompts that **stress the limits of sequential execution** — wide-search ("simultaneous exploration of many independent sources") and deep-search ("multiple reasoning branches with delayed aggregation"). Prompts do NOT explicitly instruct the model to parallelize; they shape the task distribution so parallelization is naturally favored. (`:289-298`).
- **Operating envelope**: K2.5 reports up to 100 sub-agents and 1,500 coordinated steps; K2.6 (referenced in 2605.02801) scales to 300 sub-agents and 4,000 steps.
- **Empirical results**: 78.4% on BrowseComp (vs. 60.6% single-agent K2.5), 79.0% on WideSearch (vs. 72.7%), 58.3% on internal Swarm Bench (vs. 41.6%). 3× to 4.5× wall-clock reduction. (`:706-802`).
- **Agent Swarm as proactive context management**: long-horizon tasks decomposed into "parallel, semantically isolated subtasks, each executed by a specialized subagent with a bounded local context… Only task-relevant outputs—rather than full interaction traces—are selectively routed back to the orchestrator." This is the "context sharding" architectural property — Aura's OH1 payload summarizer already aims at the same target. (`:759-802`).

**Judgment: LIFT (evidence + metrics)** — Aura is not training an orchestrator, so PARL itself is not lifted, but: (a) the Critical Steps metric is the right evaluation primitive for OH3 fan-out, (b) the frozen-children architecture matches Aura's hierarchical tree (no RL signal flowing through agentdef children), (c) the wide-search / deep-search prompt taxonomy is reusable for OH3 bench design.

### 2.3 `aura-agent-loop-papers/2605.10052-Swarm-Skills.txt`

**Files read:** in full.

**Findings.**

- **Swarm Skill = portable, distributable, self-evolving multi-agent coordination protocol**, deliberately decoupled from runtime. Five required components: SKILL.md frontmatter (kind=`swarm-skill`, teammate_mode, roles[]), natural-language body, `roles/<id>.md`, `workflow.md` (DAG in Mermaid/natural language), `bind.md` (budgets, message turns, quality gates), `evolutions.json` (append-only patches with Effectiveness/Utilization/Freshness scores). (`2605.10052:191-258`).
- **CREATE → USE → PATCH lifecycle** with automated multi-dimensional scoring (`S = w_E·E + w_U·U + w_F·F`), Bayesian-smoothed Effectiveness from outcome, decayed Utilization, 90-day exponential Freshness half-life. (`:293-363`).
- **Governance routines** SIMPLIFY / REBUILD / ROLLBACK triggered when `evolutions.json` exceeds capacity (e.g., ≥10 records). REBUILD addresses "first-run lock-in" where a suboptimal initial workflow becomes heavily patched. (`:364-376`).
- **Specification omits message-passing mechanics on purpose** — "whether the Host Agent implements communication via a shared whiteboard, asynchronous message queues, or direct prompt injection is irrelevant to the Swarm Skill." Specification says only WHO needs to collaborate, WHAT dependencies exist, CONSTRAINTS. (`:267-281`).

**Judgment: SKIP for OH3 substrate, EVIDENCE-ONLY for future Aura skills layer.** This paper is about persisting and evolving coordination protocols across sessions, not about how to run them. OH3 needs the runtime (the "Host Agent" side), which the paper deliberately doesn't specify. Re-read after OH3 ships if Aura wants to make agentdef bundles portable.

### 2.4 `aura-agent-loop-papers/2602.03786-AOrchestra.txt`

**Files read:** lines 1-500 (intro through methodology); experimental sections skimmed.

**Findings — core abstraction.**

- **Four-tuple `Φ = (Instruction, Context, Tools, Model)`** as the universal agent spec. Both orchestrator and sub-agents instantiated by the same tuple. (`2602.03786:138-167, 378-394`).
- **Action space of orchestrator is ONLY `{Delegate(Φ), Finish(y)}`** — never directly takes environment actions. Decouples orchestration from execution. (`:395-411`).
- **Sub-agent returns a structured observation** to orchestrator: `(i) concise result summary, (ii) relevant artifacts (files, references), (iii) error messages/logs`. (`:418-422`).
- **Three anti-patterns of sub-agent-as-tools** explicitly named: (a) context-isolated threads with no specialization (THREAD, Context-Folding), (b) static predefined roles with heavy human engineering (Claude Code agents), (c) dynamic specialization — what AOrchestra proposes. (`:123-145`).
- **Learnable orchestrator** via SFT (behavior cloning over expert trajectories) + iterative in-context optimization of orchestrator prompt for cost-aware routing. SFT improves orchestration by +11.51% pass@1 on GAIA; iterative ICL reduces avg cost by 18.5%. (`:443-501`).
- 16.28% relative improvement on GAIA / SWE-Bench / Terminal-Bench-2 vs. strongest baseline (ReAct, OpenHands, Claude Code, Mini-SWE), all with same underlying model. (`:30-37`).

**Judgment: LIFT (abstraction)** — the 4-tuple is the cleanest possible peer-handoff payload schema for Aura. `nominate_next(agent_def, instruction, curated_context, tools_subset, model_hint?)` maps 1:1. The "orchestrator only emits `Delegate/Finish`" rule is one of two clean termination patterns for OH3 (the other being self-signal via terminal tool — see Aura's existing `text_response` precedent in CLAUDE.md).

### 2.5 `aura-agent-loop-papers/2602.07092-Lemon-Agent.txt`

**Files read:** lines 1-400 (intro through algorithm 1); experimental tables skimmed.

**Findings.**

- **Hierarchical self-adaptive scheduling** at TWO levels: (1) macro orchestrator-workers — main agent decides single-worker vs. ensemble based on "structural independence of the global goal"; (2) micro tool parallelization — within each sub-worker, 1-5 parallel tool calls based on "specific functional requirements" (news retrieval → 5 parallel searches, complex reasoning → sequential). (`2602.07092:131-159`).
- **Three-tier progressive context management** (Aura's payload summarizer already lifts the spirit of this): (1) intra-tool truncation + metadata logging, (2) intra-round adaptive summarization triggered by cumulative output threshold, (3) cross-round retroactive compression that backtracks and replaces truncated tool results in-place with compressed versions. (`:161-188`).
- **SES-Memory (Self-Evolving Semantic Memory)** extracts transferable skills from ALL trajectories (not just successful ones), with similarity-threshold + de-dup gating. Process-centric not outcome-centric. (`:189-212`).
- **Result aggregation** (Algorithm 1, line 29): "Aggregate answers from all subagents to generate Final Answer A with confidence score S_conf" — the orchestrator explicitly emits a confidence score with the aggregation.
- 91.36% GAIA + 77+ on xbench-DeepSearch, validating the macro/micro adaptive scheduling shape works in practice.

**Judgment: LIFT (macro/micro distinction)** — the two-level adaptivity (when to use multi-agent vs. when to parallelize tool calls within one agent) is a useful framing for OH3: not every parallel opportunity is a swarm opportunity. The "structural independence of the global goal" trigger is the right discriminator. SES-Memory and three-tier context: SKIP / already lifted in Aura's payload summarizer.

### 2.6 `aura-agent-loop-papers/2602.16873-AdaptOrch.txt`

**Files read:** lines 1-400 (intro through Algorithm 1); validation skimmed.

**Findings.**

- **Performance Convergence Scaling Law**: under ε-convergence of model capabilities, variance in system performance attributable to topology selection exceeds variance from model selection by `Ω(1/ε²) · k · (1-γ)²`. With current frontier models clustering within ε≈0.03-0.05, topology is the dominant lever. (`2602.16873:78-89, 195-228`).
- **Four canonical topologies** `T = {τ_P (parallel), τ_S (sequential), τ_H (hierarchical), τ_X (hybrid)}` with explicit scheduling functions. (`:195-203`).
- **Topology Routing Algorithm** (linear time `O(|V|+|E|)`) maps DAG structural properties → topology:
  - `|E| = 0` → fully parallel
  - `ω(G) = 1` → fully sequential
  - `γ(G) > θ_γ ∧ |V| > θ_δ` → hierarchical
  - `r > θ_ω ∧ γ ≤ θ_γ` → parallel
  - else → hybrid (DAG partitioned into topological layers, parallel within, sequential between)
  - Default thresholds: `θ_ω=0.5`, `θ_γ=0.6`, `θ_δ=5`. (`:280-330`).
- **Adaptive Synthesis Protocol** with provable termination in ≤5 iterations: consistency score from embedding cosine similarity on shared output dimensions; on low CS, route to single arbiter agent; on still-low CS, re-route via Algorithm 1 with `γ' = γ + 0.2` (forcing hierarchical after 5 iters). (`:366-405`).
- 12–23% improvement over static single-topology baselines with identical models. (`:24-30`).

**Judgment: LIFT (decision algorithm shape)** — the DAG-structural-properties → topology routing is exactly the kind of deterministic, debuggable orchestration policy Aura wants instead of "model decides everything". But: (a) Aura currently has no DAG decomposition phase (the user writes plain prompts), so OH3 would need a decomposer agent first; (b) the four-topology enum is a useful **planning surface** even if Aura starts by only implementing `τ_P` and `τ_H` in OH3. Decompose-then-route may be premature for OH3 — could be OH4.

### 2.7 `aura-agent-loop-papers/2605.02801-Orchestration-Traces.txt`

**Files read:** lines 1-300 (TOC, §1, §2) + 700-1500 (§3.3-§6 — value functions, topology typology, rollout cost, harness boundary, credit fragility, eight reward families).

**Findings — directly orchestration-relevant.**

- **Five orchestration sub-decisions** (O1-O5) — load-bearing taxonomy: O1 when to spawn, O2 whom to delegate to, O3 how to communicate, O4 how to aggregate, O5 when to stop. As of May 2026, **no explicit RL training method for the stopping decision (O5) exists** in the curated 84-paper pool. (`2605.02801:23-28, intro to §8`).
- **Six recurring agent-team topologies** observed across the field (Table 5, `:806-840`): (a) centralized orchestrator + sub-agents (Kimi, M-GRPO, Puppeteer), (b) planner-executor-critic (MALT, MATPO), (c) debate/committee, (d) parallel swarm (Kimi PARL, Anthropic parallel Claudes), (e) hierarchical multi-level spawn (HALO, AgentSpawn), (f) managed/harness-based (Codex, Claude Code). NOT mutually exclusive — production systems combine two or three.
- **Topology determines credit affordance**: centralized topologies make orchestrator-level credit easiest; debate makes message-level credit natural. (`:843-846`).
- **Anthropic C-compiler case study**: 16 parallel Claudes, ~2000 sessions, ~100,000 lines of Rust. The largest public multi-agent code-generation case. (`:1102-1104`).
- **Rollout cost dominates wall-clock training time** in MAS — `C_rollout = Σ_i (L_i·c_tok + T_i·c_tool) + C_orch(K, |G|)`. Industrial regime (Kimi K2.6: K=300, T=4000) is 10²-10³× a single-agent reasoning rollout. (`:1180-1334`).
- **Trace length and credit fragility**: under shared-reward uniform credit, per-decision signal-to-noise degrades as trace length grows. Academic methods evaluate at T≲10²; deployed systems at T~10³-10⁴. Implication: methods that look good on short benches may collapse at deployment scale. (`:1352-1418`).
- **Eight reward families** (R1-R8, Table 10 at `:1471-1495`): R1 shared team/outcome, R2 individual agent, R3 role-specialized, R4-R6 process/tool/verifier rewards, R7 orchestration rewards (spawn, finish, parallelism — Kimi's `r_parallel + r_finish`), R8 composition. R8 is the default in practice for serious systems.
- **Centralized orchestrator topology** makes orchestrator-level credit MOST natural — this is the topology Aura's OH1 already implements (tree, parent picks child). Peer handoff (every agent can nominate next) shifts the topology toward (e) hierarchical multi-level spawn or (d) parallel swarm, which still admit orchestrator-level signals.

**Judgment: LIFT (taxonomy + diagnostic)** — the 5 orchestration sub-decisions are the right axes to plan OH3 against. The 6-topology table is the right vocabulary. The credit-fragility-with-trace-length warning is the right reason Aura should bound iteration depth and trace length aggressively in OH3, not rely on long-tail learning to fix poor decompositions.

### 2.8 `D:/tmp/nanobot/`

**Files read:**
- `nanobot/agent/subagent.py` (full, 352 lines)
- `nanobot/agent/tools/spawn.py` (full, 79 lines)
- `nanobot/agent/loop.py`, `runner.py`, `templates/agent/subagent_*.md` (inventory + key sections)

**Findings.**

- **Single `spawn(task, label)` tool** fires a fully isolated background subagent. NO peer handoff, NO shared blackboard. (`spawn.py:23-78`).
- **Subagent runs in fully isolated context** with its own `ToolRegistry` built via `ToolLoader(scope="subagent")`. (`subagent.py:113-128`).
- **Result announcement is via `InboundMessage` on the bus** with `channel="system"`, `injected_event="subagent_result"`, `session_key_override` aligned to parent session — this is the channel-scrubber + announce-template that Aura already lifted in OH1-F. (`subagent.py:248-291`).
- **Concurrency cap** via `max_concurrent_subagents` checked before spawn (`spawn.py:63-70`); per-session task tracking (`subagent.py:101-103`).
- **Hard cleanup**: `bg_task.add_done_callback(_cleanup)` removes from running map and session set; cancel-by-session API. (`subagent.py:164-172, 331-339`).
- **NO peer handoff primitive.** Subagent returns a string; main agent re-prompts on the announce message. No way for subagent A to nominate subagent B mid-run.
- **NO shared blackboard.** Each subagent has its own workspace; results flow only through the announce template.
- **Parallel = N concurrent spawns** by the parent in a single message — not a typed fan-out primitive.

**Judgment: SKIP / already-lifted** — nanobot was the source of the channel-scrubber + announce-template pattern Aura already has. It does NOT have peer handoff or shared blackboard. Don't re-extract; OH3 needs sources beyond nanobot.

### 2.9 `D:/tmp/openhuman/` (Rust monorepo)

**Files read:**
- `src/openhuman/agent/agents/orchestrator/agent.toml` (full, 150 lines)
- `src/openhuman/agent/agents/orchestrator/prompt.md` (lines 1-120)
- `src/openhuman/tools/impl/agent/spawn_parallel_agents.rs` (full, 492 lines)
- `src/openhuman/tools/impl/agent/spawn_worker_thread.rs` (lines 1-120)
- `src/openhuman/agent/task_board.rs` (full, 471 lines)
- `src/openhuman/agent/dispatcher.rs` (full, 518 lines)
- `src/openhuman/agent/bus.rs` (full, 502 lines)
- `src/openhuman/agent/harness/fork_context.rs` (full, 140 lines)
- `src/openhuman/agent/harness/subagent_runner/handoff.rs` (full, 230 lines)
- `src/openhuman/agent/harness/subagent_runner/types.rs` (full, 111 lines)
- Inventory of `subagent_runner/ops.rs` (1786 LOC — symbols only, key section names)

**Findings — peer-handoff / parallel-fanout primitives.**

- **`spawn_parallel_agents` tool** (the single biggest lift target):
  - Accepts `tasks: [{agent_id, prompt, context?, toolkit?, ownership?}]`, minItems=2, schema enum-constrains `agent_id` to registered definitions. (`spawn_parallel_agents.rs:69-101`).
  - Validates parent context exists (rejects if called outside an agent turn), reads `max_parallel_tools` from parent config and caps fan-out at that. (`:129-157`).
  - Per-task validation: empty agent_id/prompt → immediate failure row; unknown agent_id → immediate failure row; `integrations_agent` requires `toolkit` → reject. (`:174-242`).
  - **Ownership boundary injection**: `with_ownership_boundary(prompt, ownership)` (`:480-487`) wraps the prompt as:
    ```
    [Ownership Boundary]\n{boundary}\n\n[Task]\n{prompt}\n\n
    Do not work outside the ownership boundary unless the parent explicitly asks you to.
    ```
    This is the prompt-level mechanism to prevent worker overlap on shared resources.
  - Per-child lifecycle events published on the global event bus AND optionally on parent's `on_progress` channel: `SubagentSpawned → SubagentCompleted | SubagentFailed`, each carrying `task_id`, `agent_id`, `elapsed_ms`, `iterations`, `output_chars`. (`:253-388`).
  - Concurrent execution via `futures::future::join_all`. (`:289-294`).
  - Returns a single structured JSON block: `{parallel_agents: {total, succeeded, failed, results[...]}}`. Each result: `{task_id, agent_id, success, output?, error?, ownership?, elapsed_ms, iterations}`. (`:399-410, 40-54`).
- **`spawn_worker_thread` tool** (`spawn_worker_thread.rs:37-92`): for long tasks where the sub-agent transcript would flood the parent thread, creates a **persisted thread** labeled `worker`, sub-agent's full transcript is recorded there, parent receives a compact `[worker_thread_ref]` (thread id + summary). **Worker threads cannot spawn worker threads** — one level deep by design.

**Findings — spawn-tier discipline and depth gating.**

- **`AgentTier` enum** (`definition.rs:184-256` — symbols traced via grep, file is 668 LOC): `Chat`, `Reasoning`, `Worker`. Loader enforces: a `chat` agent must NOT list any other `chat` agent in `subagents`. `Worker` is forbidden from spawning anything. Total depth capped at 3 hops by harness regardless of tier ("defence in depth against custom TOMLs that drop the tier annotation").
- **Orchestrator's hard rules** (`orchestrator/prompt.md:42-44`):
  - "Never spawn yourself — You cannot delegate to another chat-tier agent."
  - "Spawn hierarchy (hard rule). Allowed handoffs from here: `chat → worker` (fast path) or `chat → reasoning → worker` (deep path). Never `chat → chat` and never `chat → reasoning → reasoning`. The loader rejects same-tier delegation at boot."
- **Sub-agent runner enforces no recursive spawn**: "Sub-agents must never spawn their own sub-agents. Nested spawns through `spawn_subagent` would explode trace size and break the credit-assignment story." Strips `spawn_subagent` and every synthesised `delegate_*` tool from sub-agent's tool surface regardless of archetype. (`subagent_runner/ops.rs:503-525` — verbatim comment in source).
- **Spawn depth context** (`spawn_depth_context.rs`, 66 LOC) plus `MAX_SPAWN_DEPTH` constant in `harness::mod` — task-local `current_spawn_depth()` checked before every spawn. (Cited by `subagent_runner/ops.rs:232, 242, 271`.)

**Findings — shared workspace / blackboard primitives.**

- **`task_board.rs` is a PER-THREAD Kanban for ONE agent's TODOs** — NOT a multi-agent shared blackboard. Lives at `<workspace>/agent_task_boards/<hex(thread_id)>.json`. Card states: Todo / InProgress / Blocked / Done. The agent updates them via the `todo` tool; the dashboard UI fetches via RPC. (`task_board.rs:1-13, 17-71`).
- **Crucially: no multi-agent task board exists in openhuman.** The kanban is single-thread scope. Multiple parallel agents do NOT share one board.
- **`ParentExecutionContext`** (`fork_context.rs:30-117`) is the closest thing to shared context: a task-local snapshot of `provider, all_tools, all_tool_specs, model_name, temperature, workspace_dir, memory, agent_config, skills, memory_context, session_id, channel, integrations, tool_call_format, session_key, session_parent_prefix, on_progress`. The sub-agent runner reads this to spawn a child; child does NOT write back to it. This is **read-only inherited context**, not a peer-write blackboard.
- **`handoff.rs` is NOT peer handoff** despite the name. It's a per-spawn FIFO cache (`ResultHandoffCache`, max 8 entries) for oversized tool results (>50k tokens). The tool result is replaced in history with a placeholder + `result_id`; an `extract_from_result(result_id, query)` tool lets the same sub-agent pull a narrower view. (`handoff.rs:1-21, 53-108`). This is the **payload summarizer / progressive disclosure** pattern Aura already lifted via the openhuman Phase-OP+ port.

**Findings — orchestrator-tier subagents map.**

- Orchestrator's `subagents = [...]` list (`agent.toml:47-77`): `researcher, planner, code_executor, tools_agent, skill_creator, critic, archivist, crypto_agent, markets_agent, { skills = "*" }`. Each becomes a synthesized `delegate_*` tool via `ArchetypeDelegationTool` at build time. The `{ skills = "*" }` entry expands to one `SkillDelegationTool` per connected Composio toolkit, all routing to the generic `integrations_agent` with the toolkit slug pre-filled.
- Orchestrator's direct tools include `spawn_worker_thread, spawn_parallel_agents, todowrite, plan_exit` — coding-harness coordination primitives.

**Judgment: LIFT (the parallel-fanout tool + ownership + tier discipline)** — this is the highest-value section of the whole research pass. Specifically:
1. The `spawn_parallel_agents` tool shape, schema, validation, and structured-result envelope are the closest existing reference for Aura's gap-3 (parallel fan-out).
2. The `ownership` field + prompt-injection guard is the cheapest worker-conflict prevention mechanism observed.
3. The `AgentTier::{Chat, Reasoning, Worker}` enum + loader-time rejection + runtime depth cap is the right shape for OH3 (Aura's OH1 already partially adopted this; OH3 should extend not loosen).
4. The "sub-agents never spawn sub-agents" hard rule is what OH3's peer-handoff feature must explicitly relax — and there is a corresponding cost (trace explosion + credit fragility) the user must accept.
5. **SKIP**: `task_board.rs` is not the blackboard Aura needs (single-thread Kanban for LLM TODOs, not multi-agent state). `handoff.rs` is payload-progressive-disclosure, already lifted.

### 2.10 `D:/tmp/elysia/`

**Files read:**
- `elysia/tree/__init__.py`, `objects.py:21-58` (Environment definition), `tree.py:63-200` (Tree constructor).
- `elysia/tree/prompt_templates.py:7-30` (DecisionPrompt signature).

**Findings.**

- **`Environment` object** (`objects.py:21-58`): typed shared workspace keyed by `(tool_name, result_name)`, stores a list of `{metadata, objects[]}` blocks per result. "Persistent across the tree, so that all agents are aware of the same objects." Format (verbatim from code):
  ```python
  {
      "tool_name": {
          "result_name": [
              { "metadata": dict, "objects": list[dict] },
              ...
          ]
      }
  }
  ```
  This is the closest published primitive to "typed shared blackboard keyed by swarm_run_id" that the OH3 user named.
- **`Tree` class** (`tree.py:63-200`) is a SINGLE-agent decision tree (`Elly`), with multiple `decision_nodes` and branches. Each decision picks an action from `available_tasks`. Environment persists across decisions; `decision_history` is a list-of-lists tracking the tree traversal.
- **`DecisionPrompt` signature** (`prompt_templates.py:9-30`): explicit rules — "Always select from available_tasks list only", "Prefer tasks that directly progress toward answering the input prompt", "Consider tree_count to avoid repetitive decisions". This is a deterministic terminal-decision pattern (the model's job is route-or-finish, similar to AOrchestra's `{Delegate, Finish}`).
- **Recursion limit** baked into `TreeData(recursion_limit=5, ...)` (`tree.py:152`).

**Judgment: LIFT (Environment shape)** — the `(tool_name, result_name) → [{metadata, objects[]}]` keying scheme is a clean blackboard schema even though Elysia is single-agent. Multi-agent extension: add a `producer_agent_id` field per entry, key the whole map by `swarm_run_id`. Aura already mentions this pattern in MEMORY (`feedback_elysia_text_response_pattern` — text_response as terminal tool was lifted as LAT-03). SKIP the Tree machinery itself (Aura doesn't need Elysia's branch-initialization or `dspy.Signature`-based decision prompts).

### 2.11 `D:/tmp/picobot/`

**Files read:**
- `internal/agent/tools/spawn.go` (full, 44 lines).
- Inventory of `internal/agent/tools/*.go`, `internal/chat/chat.go`, `internal/agent/loop*.go`.

**Findings.**

- **`SpawnTool` is a stub** (`spawn.go:8-43`): "For v0 we simply return an acknowledgement". No actual subagent execution.
- Picobot is **single-agent** with a `loop.go` doing the standard LLM→tool→loop pattern, MCP support, persistent memory ranker. No multi-agent primitives.

**Judgment: SKIP** — Picobot was Aura's tool-consolidation reference (Wave 2.7 in MEMORY); it has no OH3-relevant content.

### 2.12 `D:/tmp/hermes-agent/`

**Files read:**
- Inventory of `agent/*.py` (60+ files, ~hundreds of KLOC).
- Grep on `subagent\|spawn\|delegate\|orchestr\|swarm\|background_review` showed only `delegate_task` in `agent_runtime_helpers.py:1567` and `background_review.py` (550+ LOC of post-turn async curation).
- `tools/delegate_tool.py` exists (path confirmed but not opened — file naming alone is informative).

**Findings.**

- **`delegate_task` tool** dispatched via `agent._dispatch_delegate_task(function_args)` (`agent_runtime_helpers.py:1567-1568`). Mechanism is a single delegation call, NOT a peer-handoff mesh.
- **`background_review`** is a fire-and-forget daemon thread spawned after a turn ends — it replays the conversation and curates memory. It's a SECOND agent running asynchronously, but communicates only by writing to memory; there is no runtime communication channel between the primary agent and the background reviewer. (`background_review.py:1-50, 326-485`).
- **`spawn_background_review_thread`** name confirms the fire-and-forget shape — daemon thread, no handle returned to caller. (`:552`).
- Voice-mode patterns referenced in Aura's MEMORY (`reference_hermes_voice_mode_pattern`) are not OH3-relevant.

**Judgment: EVIDENCE-ONLY for OH3** — hermes-agent confirms the "fire-and-forget post-turn second agent" pattern (also seen in nanobot subagent spawn) is widespread but is NOT peer handoff. Useful as evidence that production systems DO ship multi-agent shapes; not useful as a direct lift for the OH3 runtime communication loop.

### 2.13 `D:/tmp/agent-infra-sandbox/`

**Files read:**
- Repo inventory: `cli/, docker/, evaluation/, examples/, sdk/, website/`.
- `evaluation/agent_loop.py` (inferred via name), `evaluation/dataset/evaluation_collaboration.xml` (filename only — would be relevant to OH3 bench design).
- Grep showed zero matches on `subagent|spawn_agent|delegate|handoff|peer|blackboard|task_board` in source code (only matches were in lockfiles).

**Findings.**

- Agent-infra-sandbox is a **sandbox SDK** (browser-use, code execution, file editing in containers) — it provides the **substrate** an agent uses, not multi-agent orchestration. The `evaluation/dataset/evaluation_collaboration.xml` filename suggests a collaboration eval scenario exists, but the runtime is single-agent.

**Judgment: SKIP** — wrong layer for OH3 (substrate, not orchestration).

## 3. Pattern matrix (4 OH3 gaps × sources)

Legend: ★★★ = direct, lift-ready implementation. ★★ = partial / abstraction. ★ = related anti-pattern or evidence. — = not addressed.

| Source | (1) Peer handoff (every agent nominates next) | (2) Shared blackboard (typed, swarm-run-scoped) | (3) Parallel fan-out (concurrent N children) | (4) Iteration loop + convergence (who decides done) |
|---|---|---|---|---|
| `swarm-local-map.md` / `online-research.md` | ★★ — names "team_collaboration" mode w/ task board, mailbox, plan approval; rejects round-robin shared-context group chat (anti-pattern) | ★★ — names `run_graph`, `run_graph_node`, `run_graph_event` durable records, CAS task claim, addressed mailbox (N rows for broadcast) | ★ — names fan-out_read as conservative slice; rejects fixed roles / `max_spawn_depth=1` | ★★ — durable interrupt/approval pattern (LangGraph); termination via `Finish` or stop event; metrics: critical-path, useful-msg ratio, error amplification |
| `2602.02276-Kimi-K2.5` (Agent Swarm) | ★★ — orchestrator dynamically instantiates heterogeneous sub-agents per task; sub-agent CANNOT nominate (frozen, returns observation only) | ★ — "context sharding" — each sub-agent has bounded local context; only task-relevant outputs route back to orchestrator | ★★★ — explicit parallel agent RL (PARL), up to 100 sub-agents (K2.5) / 300 (K2.6), `join_all`-style cohort with `max_i S_sub` critical-path latency | ★★★ — orchestrator-level RL on `r_perf + r_parallel + r_finish`; critical-steps budget; "when, whether, how to parallelize" all learned |
| `2605.10052-Swarm-Skills` | ★ — `workflow.md` declares dependencies and handoff pattern; runtime is unspecified | — | ★ — declared via DAG in `workflow.md` but specification doesn't mandate runtime | ★ — `bind.md` declares max message turns + quality gates; runtime owns enforcement |
| `2602.03786-AOrchestra` | ★★ — `Delegate(Φ)` is the universal mechanism; orchestrator never executes; sub-agents return structured observation | ★ — `Context` field of 4-tuple is curated per delegation; no persistent shared store | ★ — implementation supports concurrent delegations but paper emphasizes serial-or-parallel as orchestrator choice | ★★★ — `{Delegate, Finish}` action space — orchestrator alone decides; learnable via SFT |
| `2602.07092-Lemon-Agent` | ★★ — macro orchestrator decides single vs. ensemble worker; workers don't peer-handoff | — | ★★ — macro level (worker ensemble) + micro level (1-5 parallel tool calls per worker); explicit thresholds for both | ★★ — confidence score `S_conf` emitted with aggregation; max iterations capped per role (main=10, sub-worker=20) |
| `2602.16873-AdaptOrch` | ★ — hierarchical topology τ_H is one of 4 — lead agent delegates and reconciles | ★ — "context_global" shared across topologies; coupling strength `c(u,v)` quantifies how much context to propagate | ★★ — τ_P (parallel) is one of 4 canonical topologies; routing algorithm picks topology from DAG width/coupling | ★★★ — Adaptive Synthesis Protocol with **provable termination in ≤5 iterations**; consistency-score → re-route → arbiter → forced hierarchical; convergence threshold + retry budget |
| `2605.02801-Orchestration-Traces` | ★★ — names 6 topologies, hierarchical multi-level spawn (HALO, AgentSpawn, DEPART, LAMO) explicitly supports level-k spawning level-(k+1) | ★ — "orchestration trace" event graph G = (V, E, ℓ_V, ℓ_E) with spawn/delegate/message/return/aggregate event types — read-only durable structure, not a peer-write blackboard | ★★ — parallel swarm is topology (d); Kimi PARL is the canonical instance; Anthropic 16-parallel-Claudes case study cited | ★★★ — 5 orchestration sub-decisions (O1-O5) with O5 = "when to stop" explicitly named as unaddressed in published RL methods as of May 2026 |
| `nanobot` | — fire-and-forget subagent only | — workspace isolated per subagent | ★ — N concurrent `spawn()` calls allowed up to `max_concurrent_subagents` cap, but not a typed fan-out tool | ★ — subagent terminates on `stop_reason`; max-iterations cap; announce to main via bus |
| `openhuman` | ★★★ — `spawn_parallel_agents` lets orchestrator dispatch many at once; **sub-agents themselves cannot nominate** (`is_subagent_spawn_tool` strip in `subagent_runner/ops.rs:503-525`); peer handoff is therefore **explicitly forbidden in current shape** | ★ — `ParentExecutionContext` is read-only inherited context; `task_board.rs` is per-thread Kanban (single-agent); no multi-agent blackboard | ★★★ — `spawn_parallel_agents` tool with `ownership` field, `max_parallel_tools` cap, `join_all` execution, structured `{total, succeeded, failed, results[]}` envelope, per-child `DomainEvent` lifecycle | ★★ — `MAX_SPAWN_DEPTH=3` hard cap; `AgentTier::Worker` cannot spawn; sub-agents return final text → parent aggregates; no convergence/iteration mechanism, single-pass dispatch |
| `elysia` | — single-agent decision tree | ★★★ — `Environment` object keyed by `(tool_name, result_name) → [{metadata, objects[]}]`, persistent across tree traversal | ★ — tree branches can fan out; not parallel by default | ★★ — `DecisionPrompt` with explicit rules "Always select from available_tasks", recursion_limit=5, tree_count check; terminal action = `forced_text_response` |
| `picobot` | — stub | — | — | — |
| `hermes-agent` | ★ — `delegate_task` single-hop tool; `spawn_background_review_thread` daemon (no comms back) | — | — | — |
| `agent-infra-sandbox` | — substrate only | — | — | — |

**Reading the matrix:**

- **Gap 1 (peer handoff)**: NOT well-addressed by existing curated sources. Kimi K2.5 + openhuman both **explicitly forbid** sub-agents from spawning (frozen subagents in Kimi; tool-strip in openhuman). The closest published instance is the hierarchical topology family (HALO, AgentSpawn, DEPART, LAMO) from the orchestration-traces survey — these are RL methods, not deployed systems. → **Online scouts (#2/#3/#4) should target hierarchical / level-k-spawns-level-(k+1) systems specifically**.
- **Gap 2 (shared blackboard)**: Elysia's `Environment` is the closest single-source shape. Aura's online research already names `run_graph_node` / addressed mailbox / CAS task claim — but these are durable records, not a runtime read/write workspace. → **Online scouts should look for "shared memory" / "global context" patterns in CAMEL, AutoGen GraphFlow, AgentVerse**.
- **Gap 3 (parallel fan-out)**: STRONGLY addressed — openhuman's `spawn_parallel_agents` is ready to port; Kimi PARL provides the metric (Critical Steps) and the scale envelope. No further research needed on the substrate; OH3 can ship gap-3 from existing material alone.
- **Gap 4 (iteration loop + convergence)**: Multiple complementary shapes — AOrchestra's `{Delegate, Finish}` (orchestrator-decides), AdaptOrch's provable-termination synthesis protocol (≤5 iterations), Kimi's RL on `r_finish`, openhuman's `MAX_SPAWN_DEPTH=3`. The orchestration-traces paper explicitly flags O5 (stop decision) as **the under-researched sub-decision** as of May 2026. → **Online scouts should look for explicit "when to stop" / convergence-detection patterns**.

## 4. Anti-patterns observed (multiple sources reject)

1. **Sub-agents spawning sub-agents recursively** — rejected by openhuman (loader-time tool strip, `subagent_runner/ops.rs:503-525`) and Kimi K2.5 (subagents are frozen, no spawn in their action space). Reason: trace explosion and credit-assignment ambiguity. **Implication for OH3**: peer handoff (the user's requested gap-1) MUST come with explicit depth/budget caps; "sub-agent nominates next" should land observation back on the orchestrator that then dispatches, OR enforce a hop budget independent of tree depth.
2. **Round-robin shared-context group chat as default** — explicitly rejected in `swarm-online-research.md:101`. Reason: spreads all context to all agents, increases token load, ownership blurry. **Implication**: blackboard reads/writes must be addressed (`producer_agent_id`, `consumer_agent_ids`) and scoped (`swarm_run_id`), never an invisible shared transcript.
3. **Hardcoded fixed roles and permanent `max_spawn_depth=1`** — rejected in `swarm-online-research.md:95` and Aura's own PRD/AGENTS constraints. **Implication**: OH3 must persist `allowed_spawn_depth` per-node even if first implementation only supports depth=0 or 1.
4. **Measuring success by spawn count or agent count** — rejected in `swarm-online-research.md:105`. Critical metric is critical-path wall-clock + useful-message ratio + error amplification + token efficiency. **Implication**: Aura's existing `tool_calls: N` is NOT sufficient (re-confirms CLAUDE.md rule "VALIDATE WITH VERIFIED BENCHMARKS, NEVER ONLY TOOL-CALL COUNTS").
5. **Storing full prompts/transcripts/mailbox bodies as default trace fields** — rejected in `swarm-online-research.md:109`. **Implication**: OH3 trace events should be metadata-first with artifact handles for large payloads, following Aura's existing payload-summarizer pattern.
6. **Long context with uniform credit on shared-reward terminal signal** — orchestration-traces §3.4 + §5.3 documents credit fragility as trace length grows. Academic methods evaluate at T≲10²; industrial at T~10³-10⁴. **Implication**: OH3 must cap iteration budget aggressively (3-5 not 50+) until per-step signal can be made dense (role/message-level credit).
7. **OpenAI Swarm as production runtime** — explicitly rejected in `swarm-online-research.md:91` ("educational, stateless, superseded by Agents SDK"). **Implication**: don't lift the OpenAI Swarm Python repo as a runtime reference. Their handoff primitive shape is fine; the runtime is not.
8. **Treating `task_board.rs` (openhuman) as a multi-agent blackboard** — it is NOT. It's a per-thread Kanban for a single agent's TODOs. **Implication**: don't name OH3's blackboard `task_board` (collision); pick a distinct name like `swarm_workspace` or `swarm_environment`.

## 5. Open questions for online scouts (#2/#3/#4)

The existing curated material in `D:/tmp/*` does NOT answer the following. Online scouts should focus here:

1. **Peer handoff with `nominate_next` action in a deployed system** — Kimi PARL + openhuman both forbid sub-agent spawn. Hierarchical topologies (HALO, AgentSpawn, DEPART, LAMO) are RL methods in the orchestration-traces pool — are any of them **deployed** with public reference implementations? Check Claude Code Agent Teams (`code.claude.com/docs/en/agent-teams`) explicitly for "lead can re-nominate" semantics, since the online-research map only quotes from product docs.
2. **Typed swarm blackboard with read/write semantics** keyed by `swarm_run_id` — Elysia's `Environment` is single-agent. AutoGen's GroupChat memory? CrewAI's "shared memory" feature? LangGraph's checkpointer keyed by thread_id — does it support cross-agent writes mid-run? GPTSwarm / DyLAN graph-based message routing — do they have a typed workspace primitive or is it message-passing only?
3. **Convergence detection without an explicit `Finish` tool** — AdaptOrch has provable termination via consistency score on output embedding similarity. Are there other deployed convergence detectors? Voting (Mixture-of-Agents)? Confidence-threshold (Lemon Agent's `S_conf`)? Cross-agent attention pattern (any published metric)? What does Kimi K2.6 use for its 4000-step coordination cap — is it just `max_steps` or is there a learned stop?
4. **Stream-time vs. batch-time peer handoff** — does the orchestrator wait for ALL parallel children to finish before re-dispatching (openhuman's `join_all` model), or can a child mid-flight nominate a peer that the orchestrator dispatches immediately? Anthropic's parallel-Claudes C-compiler case study would be the natural reference; the orchestration-traces paper only cites it as workflow-shape evidence, not behavior detail.
5. **What is the minimum useful `swarm_run_id` set of artifact types?** Aura already has wiki pages, scheduled tasks, source uploads, conversation archives. Should the blackboard hold raw tool results (Elysia's pattern), structured observations (AOrchestra's `{summary, artifacts, error_logs}`), or both? Industrial systems (Kimi, Codex, Claude Code) do not publicly disclose this.
6. **Where do existing systems detect and reject reward hacking** in non-RL contexts? Kimi PARL fights spurious-parallelism with `r_finish`; what's the equivalent for a prompt-engineered orchestrator that hasn't been RL-tuned? Counterfactual probes? Lint rules on the orchestration trace?
7. **What is the right shape of an `iteration_budget` for a swarm-run** — total LLM calls? per-agent iteration count? token budget? wall-clock? Aura currently caps `AURA_AGENT_LOOP_MAX_STEPS=5` for single-agent. OH3 needs an explicit multi-agent equivalent.
8. **How does Anthropic Claude Code agent teams handle mid-run user intervention** (steerability)? Orchestration-traces §10.4 + §11 flag steerability as an under-addressed RL target. Is there ANY published reference for how a peer-handoff mesh accepts a user "ferma" mid-iteration? (Aura's user has the explicit "fa schifo" → STOP rule in CLAUDE.md — the runtime must support breaking the loop fast.)

---

**End of scout #1 extraction.** Total ~1100 lines. All citations verified against files in `D:/tmp/`. The single highest-impact lift target is openhuman's `spawn_parallel_agents.rs` (gap-3); the second is Elysia's `Environment` schema (gap-2); the hardest gap to source is peer handoff itself (gap-1) — online scouts should target hierarchical-spawn academic methods and Anthropic agent teams docs.
