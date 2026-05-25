# Multi-Agent Production Patterns — Online Research 2026-05-25

> Goal: validate Aura's planned **Wave OH1** (`AGENTDEF` + `TIER` + `DELEGATE-TOOL` + `DEDUP`) against the broader 2024-2026 multi-agent ecosystem before we commit ~1000-1500 LOC.
> Method: 45-min WebSearch + WebFetch sweep, 14 high-quality findings. Time-box hit.
> Convention: `REINFORCES` / `DIVERGENT` / `SKIP` per Aura's OH1 design contract.

---

## Top-3 + verdict (read this first)

1. **Anthropic's own June-2025 multi-agent post + Dec-2024 "Building Effective Agents" both REINFORCE the orchestrator-worker shape Aura's OH1 picked**, *and* explicitly warn against same-tier delegation and unbounded recursion. The 15× token cost they report comes from a research-heavy use case (web breadth); a single-user assistant like Aura should never approach that ratio if hops are capped at 3 and most turns stay at tier-1 chat. (Sources: Anthropic engineering blog; Anthropic research page.)
2. **Claude Code's subagent YAML-frontmatter pattern is the closest production analogue to Aura's `AGENTDEF` TOML** — almost field-for-field overlap (`name`, `description`, `model`, `tools`, `skills` body=prompt). This is the strongest single-source validation we have. It also pins down an under-specified OH1 question: parent's "when to delegate" is driven by the child's `description`, not the parent's prompt. (Source: code.claude.com docs.)
3. **The 3-tier (Chat/Reasoning/Worker) scheme is NOT industry-standard.** Production frameworks (LangGraph, ADK, CrewAI, OpenAI Agents SDK, Pydantic AI) all use **depth-only or role-name** hierarchies — no explicit tier enum. Tiers are an Aura-specific opinion. They're defensible because Aura runs on heterogeneous local+API models, but the **same-tier-rejection rule the validator enforces is the load-bearing bit, not the 3-name taxonomy itself**. Consider relaxing TIER to "model-class hint + max-depth + cycle detector" rather than a hard enum — see DIVERGENT note in §3.

**Verdict: openhuman-derived OH1 design HOLDS UP.** Three suggested adjustments (none reshape the design, all tighten it):

- (A) Add an explicit per-child **token-budget cap** field to `AGENTDEF` (`max_input_tokens` + `max_output_tokens`) — every production framework that tracked usage made this a first-class field (Pydantic AI `UsageLimits`, Anthropic blog stresses it, openhuman lacks it). Today OH1 only has `max_iterations` + `max_result_chars`; that's insufficient against runaway models.
- (B) Add **cycle detection** (parent-id stack) explicitly to the validator, not just the same-tier rule. Multiple frameworks (DACS paper, MAS-anti-patterns Medium 2025) call cyclic delegation out as the #1 silent-failure mode.
- (C) Soften TIER from a hard enum to an advisory model-class hint. The hard hop-cap and same-tier rejection rule are doing the real work; the 3 names are color.

If you don't take A/B/C: OH1 still ships and works for ~90% of cases. A and B are cheap. C is more invasive — defer to Davide.

---

## Findings (sorted: REINFORCES → DIVERGENT → SKIP)

### F1 — Anthropic "Building Effective Agents" (Dec 2024) — Workflows vs Agents distinction

- **Source**: https://www.anthropic.com/research/building-effective-agents
- **Year**: 2024-12
- **Pattern shape**: Anthropic defines five composable primitives (prompt-chaining, routing, parallelization, orchestrator-workers, evaluator-optimizer). They draw a hard line: **"workflows"** = predefined code paths over LLMs; **"agents"** = LLM dynamically directs its own loop. They explicitly recommend *starting with simple prompts and adding multi-step agentic systems only when simpler solutions fall short*. Frameworks "can make it tempting to add complexity when a simpler setup would suffice."
- **Aura applicability**: **REINFORCES** OH1 — the `subagents[]` per archetype is exactly the orchestrator-worker primitive. Aura's chat path stays single-loop; only the planner/reasoning archetype gains delegation. Matches Anthropic's "add complexity only when simpler solutions fall short."
- **Validates**: AGENTDEF (archetype = primitive shape), DELEGATE-TOOL (orchestrator-workers pattern), the decision to keep delegation opt-in per archetype rather than always-on.

### F2 — Anthropic Multi-Agent Research System (Jun 2025) — Production blueprint + cost numbers

- **Source**: https://www.anthropic.com/engineering/multi-agent-research-system
- **Year**: 2025-06
- **Pattern shape**: Lead Researcher (orchestrator) decomposes query, spawns parallel subagents with **isolated context windows**, each gets an objective + output format + tool/source guidance + scope boundaries. Subagents return only final summaries (context isolation). 90.2% lift on internal research eval vs single Opus 4. **15× tokens** vs single chat (agents alone are already 4×).
- **Aura applicability**: **REINFORCES** orchestrator-worker, **REINFORCES** context isolation per child, **REINFORCES** richly-described delegation (not just `task: string`).
- **Anti-patterns called out**: vague task descriptions cause duplicated work; over-spawning (50 subagents for trivial queries); tool-mismatch ("searching web for context that only exists in Slack"); source-quality bias toward SEO content farms.
- **Cost note**: 15× claim is for breadth-first research. Davide should NOT extrapolate that to Aura's typical chat turn — see Cost Reality Check §C.

### F3 — Claude Code subagents (2025) — Closest analogue to OH1 AGENTDEF

- **Source**: https://code.claude.com/docs/en/sub-agents
- **Year**: 2025 (active)
- **Pattern shape**: Subagents = Markdown file with **YAML frontmatter** + body-as-system-prompt. Required fields: `name`, `description`. Optional: `model` (alias `sonnet`/`opus`/`haiku` or full ID), `tools` (whitelist), `skills` (inject skill content at startup). Parent's delegation decision is driven by the child's `description` field — Claude reads all subagent descriptions and routes by intent match. Each subagent **runs in its own context window** and returns only the summary. Subagents work within a single session; for cross-session there's a separate "agent teams" concept.
- **Aura applicability**: **REINFORCES** AGENTDEF schema almost field-for-field. The only differences:
  - Claude Code uses YAML+Markdown (one file per agent); OH1 plans TOML (one registry file or per-archetype). Both are fine — see §4 (format choice).
  - Claude Code has a `skills:` inject field; OH1 doesn't yet — **consider adding** since Aura already has a skills layer.
  - Claude Code's `tools:` is a whitelist; OH1's `tools.named[]` matches exactly.
- **Validates**: AGENTDEF fields (`id/when_to_use/prompt/model_hint/tools.named[]`), the "description drives delegation" mechanism (which means we don't need a separate `route_keywords` field — the prompt body already does it).
- **Cost note**: Anthropic specifically pitches subagents as a cost-control mechanism: "route tasks to faster, cheaper models like Haiku." This validates `model_hint` per archetype.

### F4 — LangGraph supervisor + handoff tools (2025)

- **Source**: https://github.com/langchain-ai/langgraph-supervisor-py
- **Year**: 2025
- **Pattern shape**: `create_supervisor(agents=[...], model=..., prompt=..., output_mode=...)`. Handoff is implemented as a synthesised **tool** per child (`create_handoff_tool(agent_name=..., description=..., name=prefix+agent_name)`). Multi-level hierarchies built by nesting supervisors. **No explicit depth limit.** **No tool-name collision detection.** Latest LangChain guidance: prefer "supervisor pattern directly via tools" over the dedicated library — gives more context-engineering control.
- **Aura applicability**: **REINFORCES** DELEGATE-TOOL synthesis pattern. Aura's `delegate_<id>` per-turn synthesis is identical in spirit.
- **DIVERGENT note**: LangGraph synthesises handoff tools but doesn't dedup or bound them — Aura's DEDUP guard is something LangGraph users discover the hard way. Aura's OH1 is *better* on this dimension.
- **Validates**: per-turn synthesised `delegate_<id>` from `subagents[]`; per-tool description (= child's `when_to_use`); the `handoff_tool_prefix` field maps directly to Aura's `delegate_` prefix.

### F5 — Google ADK hierarchical agents (Apr 2025)

- **Source**: https://developers.googleblog.com/developers-guide-to-multi-agent-patterns-in-adk/
- **Year**: 2025
- **Pattern shape**: 8 patterns (Sequential, Coordinator/Dispatcher, Parallel Fan-out/Gather, Hierarchical Decomposition, Generator+Critic, Iterative Refinement, HITL, Composite). `LlmAgent` fields: `name`, `instruction`, `description`, `tools`, `output_key`, `sub_agents`. Two delegation modes: `AgentTool(sub_agent)` wraps child as tool; or `sub_agents=[...]` + AutoFlow. **Each agent has only one parent — DAG enforced by construction.** No explicit depth cap.
- **Aura applicability**: **REINFORCES** the field set (Aura's AGENTDEF has equivalent of every ADK field except `output_key`). **REINFORCES** "AgentTool wraps child as tool" = exactly Aura's DELEGATE-TOOL synthesis.
- **DIVERGENT note**: ADK enforces single-parent at the type level. Aura's OH1 plans `max-depth: 3` + same-tier validator but doesn't enforce single-parent. Consider adding: each child in a parent's `subagents[]` is only owned by that parent — easy invariant check.
- **Anti-patterns from ADK docs**: monolithic agents (single all-in-one bottleneck); over-complexity ("do not build a nested loop system on day one"); poor state management with non-descriptive keys.

### F6 — Pydantic AI multi-agent + UsageLimits (2025)

- **Source**: https://pydantic.dev/docs/ai/guides/multi-agent-applications/
- **Year**: 2025
- **Pattern shape**: Three delegation modes: **(a) agent-as-tool** (`@agent.tool` calling child `await child.run(..., usage=ctx.usage)`), **(b) programmatic handoff** (app code routes between agents), **(c) graph-based** via `pydantic_graph`. Critically, they recommend passing the **parent's accumulated `usage` object** down to the child so token costs aggregate into one number across the tree. No depth limit, but explicit `UsageLimits(request_limit, total_tokens_limit, tool_calls_limit)` enforcement.
- **Aura applicability**: **REINFORCES** delegate-as-tool pattern. **Strongly suggests adding token-budget fields to AGENTDEF** (currently OH1 has `max_iterations` + `max_result_chars` — both step/byte caps; Pydantic AI's `total_tokens_limit` is a true cost guardrail).
- **Validates**: tracking parent→child usage. Sketches **Adjustment-A** (token budget per archetype).

### F7 — Microsoft Magentic-One generalist orchestrator (Nov 2024, active 2025)

- **Source**: https://www.microsoft.com/en-us/research/articles/magentic-one-a-generalist-multi-agent-system-for-solving-complex-tasks/
- **Year**: 2024-11
- **Pattern shape**: 1 Orchestrator + 4 fixed specialist children (WebSurfer, FileSurfer, Coder, ComputerTerminal). Orchestrator maintains a **Task Ledger** (facts + guesses) and **Progress Ledger** (self-reflection per step). Model-agnostic (default GPT-4o). No tiers; no nesting (1-deep).
- **Aura applicability**: **REINFORCES** the *small fixed cast* of specialists pattern. Aura's planned built-in archetypes (chat / reasoning / worker subagents) match the Magentic shape. **DIVERGENT** on the ledger mechanism — Aura's OH1 has no equivalent of Task/Progress Ledger. **Alternative worth considering**: a lightweight `agent_note` is already in the tool surface; we could repurpose it as the orchestrator's scratch space. Wins when reasoning hops span multiple turns; not needed for single-turn delegation.
- **Cost note**: Magentic explicitly opted for "model-agnostic" — different models for different specialists (cost optimization). Validates Aura's `model_hint` per archetype.

### F8 — OpenAI Agents SDK handoff (Mar 2025, replaces Swarm)

- **Source**: https://cookbook.openai.com/examples/orchestrating_agents
- **Year**: 2025-03
- **Pattern shape**: Replaced experimental Swarm. Each agent has `instructions`, `model`, `tools`, and a **list of agents it can hand off to**. Handoff transfers control + conversation context. Primitives: Handoffs / Guardrails / Tracing. Locked to OpenAI models.
- **Aura applicability**: **DIVERGENT** — OpenAI's handoff is *transfer-of-control* (parent dies when child takes over), Aura's OH1 plans *spawn-and-await* (parent resumes after child returns). Aura's choice is the right one for a personal-assistant loop because the chat tier needs to wrap the child's output for the user. Skip OpenAI's hard-handoff model.
- **Notable**: Handoff is the SDK's #1 primitive (above guardrails and tracing). This is a strong signal that "how does control move between agents" needs to be a first-class type — not buried inside tool-call plumbing. Aura's `delegate_<id>` synthesis makes it visible; good.

### F9 — CrewAI Sequential vs Hierarchical Process (2025)

- **Source**: https://docs.crewai.com/en/concepts/crews
- **Year**: 2025 (active)
- **Pattern shape**: Agents defined in YAML (`role`, `goal`, `backstory`, tools, etc.). Two processes: **Sequential** (linear chain, no manager) vs **Hierarchical** (requires `manager_llm` or `manager_agent`, who delegates and validates). Production deployments use **CrewAI Flows** (event-driven) wrapping Crews for fine-grained control.
- **Aura applicability**: **REINFORCES** YAML/TOML configurable agent definitions. **REINFORCES** "manager agent is a separate role with its own LLM, not just a flag on a worker." Maps cleanly to Aura's `chat` tier as the manager.
- **DIVERGENT**: CrewAI builds tasks as first-class objects (`Task(description, agent, expected_output, ...)`). Aura's OH1 has no Task object — the orchestrator just makes a delegate tool call. CrewAI's Tasks make for richer audit trails; if Davide ever wants a Telegram `/tasks-history` view, the Task object is the seam.

### F10 — Anti-pattern: cyclic / recursive delegation (multi-source 2025)

- **Sources**: 
  - https://medium.com/@armankamran/anti-patterns-in-multi-agent-gen-ai-solutions-enterprise-pitfalls-and-best-practices-ea39118f3b70 (May 2025)
  - https://www.augmentcode.com/guides/multi-agent-ai-systems
  - https://www.augmentcode.com/guides/multi-agent-ai-production-requirements
- **Year**: 2025
- **Pattern shape**: Unbounded agent autonomy → infinite loops, cost blowouts, context collapse. Mitigations: explicit max-depth, **watchdog/supervisor agents**, **DAG visualization of execution**, per-role delegation limits + timeouts, termination conditions for swarms (max iterations, quality thresholds, timeout convergence).
- **Aura applicability**: **REINFORCES** OH1's max-depth 3 + same-tier rejection. **Suggests adding** explicit cycle detection — same-tier rule blocks A→A but not A→B→A across tiers. (Sketches Adjustment-B.)
- **Validates**: TIER validator. Hardens DELEGATE-TOOL.

### F11 — Anti-pattern: same-tier / peer-debate degradation (2025)

- **Sources**: 
  - https://arxiv.org/pdf/2509.05396 (Sep 2025: "Talk Isn't Always Cheap")
  - https://arxiv.org/pdf/2604.06091 (Social Dynamics paper)
  - https://www.augmentcode.com/guides/multi-agent-ai-systems
- **Year**: 2025
- **Pattern shape**: Same-tier peer debate often **degrades accuracy over time** — agents converge on hallucinated consensus, shift from correct to incorrect to favor agreement, infinite negotiation loops. Augment summary: "peer-to-peer is the least predictable pattern."
- **Aura applicability**: **STRONGLY REINFORCES** OH1's same-tier rejection rule. This is the load-bearing bit of the TIER design.
- **Implication for §3 DIVERGENT note**: this finding is why TIER is justified at all. Without the same-tier rule, peer-to-peer disaster mode opens up. With it, the 3 tier names are mostly cosmetic but the rule is essential.

### F12 — Context isolation + leakage prevention (2025)

- **Sources**: 
  - https://github.com/anthropics/claude-code/issues/10212 (independent context windows feature request)
  - https://arxiv.org/pdf/2604.07911 (DACS — Dynamic Attentional Context Scoping)
  - https://arxiv.org/html/2508.08322v1 (Context Engineering for Multi-Agent LLM)
- **Year**: 2025
- **Pattern shape**: Multi-agent systems where parent passes full history to child = privilege escalation, regulated-data leak, prompt-injection propagation. DACS reduces wrong-agent context contamination from 28-57% to 0-14%. ICLR-2025 finding: **LLMs cannot reliably separate instructions from data** — external architectural enforcement mandatory.
- **Aura applicability**: **REINFORCES** "each child gets isolated context window" (Anthropic blog F2 + Claude Code F3 confirm). **Suggests adding** to AGENTDEF: an `inherit_context: false` default + opt-in `inherit_context_keys: [...]` whitelist. Today OH1 has `omit_*` flags that subtract; flipping to a positive whitelist is safer.
- **Cost note**: isolating context is *cheaper* than sharing it (child's prompt = archetype prompt + task brief, not parent's entire history).

### F13 — TOML configuration with hot-reload (2025)

- **Sources**:
  - https://agent-ci.com/blog/2025/10/15/object-oriented-configuration-why-toml-is-the-only-choice/
  - https://docs.qodo.ai/qodo-documentation/qodo-gen/agent/workflows/agent-toml-file
  - https://developers.openai.com/codex/config-advanced
- **Year**: 2025
- **Pattern shape**: Multiple production agent systems (Qodo, OpenAI Codex CLI, agent-ci) use **TOML for agent definitions**. Hot-reload pattern: cache key includes content hash; gateway transparently rebuilds agent on change — no restart, no session reset. Rationale: TOML "brings object-oriented thinking to configuration" (vs YAML's nesting issues); Python ecosystem already standardized on TOML (`pyproject.toml`).
- **Aura applicability**: **REINFORCES** OH1's TOML choice over YAML/JSON. **Validates** built-in + user-override pattern (Codex CLI explicitly has overridable sub-agents, though limited to existing names — see `agentregistry-dev/agentregistry`).
- **Note**: YAML+Markdown (Claude Code F3) and TOML (Codex, Qodo) are both production-shipped. Picking one is style; both work. Aura's TOML choice is fine.

### F14 — Anti-pattern: Frameworks-too-early (Anthropic Dec 2024)

- **Source**: https://www.anthropic.com/research/building-effective-agents
- **Year**: 2024-12
- **Pattern shape**: Anthropic explicit warning: *"Start with simple prompts ... add multi-step agentic systems only when simpler solutions fall short."* Frameworks abstract away the wire-level details that you need to debug.
- **Aura applicability**: **REINFORCES** Aura's "thin in-house substrate, not LangGraph" approach. OH1 is ~1000-1500 LOC of carefully-typed Go, not a framework dependency.
- **Implication**: Whatever OH1 ships, it must NOT abstract away the LLM wire format. Today Aura's agent loop is direct OpenAI-compatible HTTP — keep it that way. Don't introduce a framework-style `Agent.run()` that hides the round trip.

---

## Anti-patterns to bake into OH1 design

Distilled from F2, F5, F10, F11, F12, F14:

| # | Anti-pattern | OH1 mitigation today | Gap |
|---|---|---|---|
| AP1 | Cyclic delegation (A→B→A) | Same-tier rejection rule | Doesn't catch cycles across tiers — **add cycle detector** (parent-id stack passed down). |
| AP2 | Same-tier peer debate | Same-tier rejection rule | Covered. |
| AP3 | Unbounded depth recursion | Max-depth 3 hop cap | Covered. |
| AP4 | Tool-name collision | DEDUP guard | Covered. |
| AP5 | Vague delegation task ("research X") | Per-archetype prompt template | Partial — **add structured task-brief schema** (objective + output-format + tool-hint + scope, per Anthropic F2). |
| AP6 | Over-spawning ("50 subagents for trivial query") | `max_iterations` per archetype | Partial — **add per-turn fan-out cap** (e.g. `max_subagents_per_turn: 3`). |
| AP7 | Context leakage / parent's full history into child | `omit_*` flags | Partial — flip to **whitelist**: `inherit_context: false` default + opt-in `inherit_keys: [...]`. |
| AP8 | Tool-mismatch (web search for Slack-only content) | Per-archetype `tools.named[]` | Covered. |
| AP9 | Unbounded token cost per child | `max_result_chars` (output bytes) | Insufficient — **add `max_input_tokens` + `max_output_tokens`** (Pydantic AI F6 + Anthropic F2). |
| AP10 | Monolithic agent (one big prompt) | Multiple archetypes | Architecturally avoided — confirm by not adding a "kitchen-sink" archetype. |
| AP11 | Framework abstracting wire details | OH1 is thin Go, not a framework | Covered — keep it thin. |
| AP12 | Multi-parent / DAG-not-tree | Implicit | **Add invariant**: child appears in at most one parent's `subagents[]` (ADK F5 enforces this by construction). |

---

## Cost reality check for Aura (single user, mini-PC + API hybrid)

The "**15× tokens**" headline from Anthropic's Jun-2025 post is **massively misleading for Aura's workload**. Decomposition:

1. **Anthropic's workload**: open-ended *breadth-first research* (15 parallel subagents searching the web for distinct facets of an open question). 90.2% lift justifies 15×.
2. **Aura's workload**: ~95% of turns are *chat/look-up/single-task* (per Davide's usage profile — Telegram second-brain, single user). For these, multi-agent is **strictly worse**: pure overhead, no parallelism benefit.

Cost model for Aura's planned tiers:

| Turn type | Hops | Cost vs single-agent | Worth it? |
|---|---|---|---|
| Chat (greeting, fact lookup, wiki read) | 1 (chat only, no delegate) | 1.0× | always |
| Reasoning (plan a multi-step task, complex Q&A) | 2 (chat → reasoning) | ~2-3× | sometimes — when planning beats one-shot |
| Worker fan-out (heavy research like Anthropic's case) | 3 (chat → reasoning → 3-5 workers parallel) | ~5-15× | rare — only when user explicitly asks for deep dive |

**Key implication**: OH1's design **must not** delegate by default. The chat tier handles a turn alone unless its prompt explicitly fires a `delegate_<id>` tool call. This is already the design (delegation is opt-in via archetype's `subagents[]`). **Reaffirm: never auto-delegate.**

**Second implication**: per-archetype `model_hint` lets Aura point the chat tier at a cheap fast model (local llama / Haiku) and the reasoning tier at a strong model (Sonnet 4.6 / Opus). This is the *primary* cost lever — bigger than tier count, bigger than max-iterations. Validate that OH1 actually wires `model_hint` through to the per-loop LLM client (today the agent loop hard-codes one client — check this is parameterized per-archetype).

**Third implication**: Aura ships on a mini-PC with shared CPU budget (per memory: `feedback_minipc_cpu_budget`). Worker fan-out that spawns 5 parallel sub-loops at once will saturate the CPU/network and degrade everything else. **Suggest**: per-archetype `max_subagents_per_turn` cap, default 1 or 2, override to higher only for explicit research archetype.

**Single-user vs enterprise**: enterprise multi-agent justifies 15× tokens because the dollar value of the task is in the hundreds. Aura's user value per turn is "Davide gets a faster answer" — measured in seconds saved, maybe cents of value. So the cost ceiling per turn is tiny. **Multi-agent for Aura pays off only when (a) parallelism actually shortens wall-clock latency Davide perceives, OR (b) the task is one Davide wouldn't otherwise do (deep research he'd skip). Not for routine chat.**

---

## Open design questions for Davide

These choices the research surfaces — need a human call.

**Q1. TIER as hard enum vs depth+model-hint only?**
The 3-tier names (Chat/Reasoning/Worker) are an Aura-specific opinion. Production frameworks use depth + role-name, no enum. The *rule* "no same-tier delegation" is essential (F11); the *names* are cosmetic.
- **Option A (current OH1)**: keep enum, keep same-tier validator. Crisp mental model; easy to audit; future-friendly if you ever add tier-specific routing.
- **Option B (advisory)**: tier becomes a model-class hint (cheap/medium/strong), depth is the hard guardrail, cycle detector replaces same-tier-only rule. More flexible; less opinionated; matches industry.
- **Recommendation**: ship A first (current plan), monitor; can collapse to B later. Cost of A is one validator function.

**Q2. Add token-budget fields to AGENTDEF?**
F6 (Pydantic AI) explicitly tracks `total_tokens_limit` per child; F2 (Anthropic) cost concern. OH1 today caps `max_iterations` (step count) + `max_result_chars` (output bytes) — neither catches an LLM that decides to think for 50k input tokens before responding.
- **Recommendation**: **yes, add `max_input_tokens` + `max_output_tokens`** (default 8k / 4k). Cheap to wire; saves real money on a runaway model.

**Q3. Whitelist context-inheritance or keep `omit_*` blacklist?**
F12 finding: parent-history-leak is the #1 production foot-gun. OH1's `omit_*` flags are subtractive (developer must remember to add each new flag).
- **Recommendation**: flip to additive — `inherit_context: false` default + `inherit_keys: [last_user_message, current_archetype_state]` opt-in. Whitelist > blacklist for security. One-line schema change.

**Q4. Skill injection field in AGENTDEF?**
Claude Code (F3) has `skills:` — declared skills are loaded into the subagent's context at startup. Aura already has a `skills` system + `internal/skills` runtime. Adding `skills.named[]` to AGENTDEF would let an archetype declare "I always want the X skill loaded" without burdening every prompt.
- **Recommendation**: defer to OH2 — not needed for foundation, but the design slot should exist. Add `skills: []` field now (no-op), wire later.

**Q5. Cycle detector implementation?**
Parent-id stack is trivial — pass `delegation_path: [archetype_id, ...]` through context, reject if `child.id in path`. ~10 LOC.
- **Recommendation**: **yes, ship in OH1**. Closes AP1. Doesn't conflict with same-tier rule (both fire).

**Q6. Per-turn fan-out cap?**
F2 anti-pattern: agents spawning 50 subagents. Mini-PC won't survive 5 concurrent worker loops.
- **Recommendation**: per-archetype `max_subagents_per_turn` field, default 1, raise to 3-5 for explicit research archetype. ~5 LOC + cap check at synthesis time.

**Q7. Task-brief schema for `delegate_<id>` arguments?**
Today plan is likely `task: string`. Anthropic (F2) explicitly says vague briefs cause duplicated work; structured briefs (objective + output-format + tool-hint + scope) work.
- **Recommendation**: **structured args**: `{objective: str, output_format: str (optional), scope_hint: str (optional), context_summary: str (optional)}`. Tool schema gets richer, model gets clearer instructions, post-mortems get clearer trails. Marginal cost.

**Q8. Task object for audit trail (CrewAI-style)?**
Optional but valuable if Telegram `/tasks` view ever wants to show "what did Aura delegate to whom, when, with what outcome."
- **Recommendation**: defer; the existing `conversations` archive captures tool-call JSON which has enough for now. Revisit when Davide wants a delegation dashboard.

---

## Sources

- [Anthropic — How we built our multi-agent research system](https://www.anthropic.com/engineering/multi-agent-research-system)
- [Anthropic — Building Effective Agents](https://www.anthropic.com/research/building-effective-agents)
- [Claude Code — Create custom subagents](https://code.claude.com/docs/en/sub-agents)
- [LangGraph supervisor (GitHub)](https://github.com/langchain-ai/langgraph-supervisor-py)
- [LangGraph hierarchical agent teams (tutorial)](https://langchain-ai.github.io/langgraph/tutorials/multi_agent/hierarchical_agent_teams/)
- [Google ADK — Developer's guide to multi-agent patterns](https://developers.googleblog.com/developers-guide-to-multi-agent-patterns-in-adk/)
- [Pydantic AI — Multi-agent applications](https://pydantic.dev/docs/ai/guides/multi-agent-applications/)
- [Microsoft Magentic-One announcement](https://www.microsoft.com/en-us/research/articles/magentic-one-a-generalist-multi-agent-system-for-solving-complex-tasks/)
- [OpenAI cookbook — Orchestrating Agents (Routines + Handoffs)](https://cookbook.openai.com/examples/orchestrating_agents)
- [CrewAI — Crews concept page](https://docs.crewai.com/en/concepts/crews)
- [HuggingFace smolagents (GitHub)](https://github.com/huggingface/smolagents)
- [Mastra TypeScript framework](https://mastra.ai/)
- [Anti-Patterns in Multi-Agent Gen AI Solutions (Kamran, May 2025)](https://medium.com/@armankamran/anti-patterns-in-multi-agent-gen-ai-solutions-enterprise-pitfalls-and-best-practices-ea39118f3b70)
- [Augment Code — Multi-Agent AI Systems guide](https://www.augmentcode.com/guides/multi-agent-ai-systems)
- [Augment Code — Multi-Agent AI Production Requirements](https://www.augmentcode.com/guides/multi-agent-ai-production-requirements)
- [Augment Code — Multi-Agent AI Security risks](https://www.augmentcode.com/guides/multi-agent-ai-security-risks-compliance-fixes)
- [Augment Code — AI Agent Loop Token Costs](https://www.augmentcode.com/guides/ai-agent-loop-token-cost-context-constraints)
- [Augment Code — Swarm vs Supervisor guide](https://www.augmentcode.com/guides/swarm-vs-supervisor)
- ["Talk Isn't Always Cheap" — multi-agent debate failure modes (Sep 2025)](https://arxiv.org/pdf/2509.05396)
- [Taxonomy of Hierarchical Multi-Agent Systems (2025)](https://arxiv.org/pdf/2508.12683)
- [DACS — Dynamic Attentional Context Scoping (2026)](https://arxiv.org/pdf/2604.07911)
- [Context Engineering for Multi-Agent LLM Code Assistants (Aug 2025)](https://arxiv.org/html/2508.08322v1)
- [Agent-CI — Why TOML for agent config](https://agent-ci.com/blog/2025/10/15/object-oriented-configuration-why-toml-is-the-only-choice/)
- [Qodo Gen — Agent TOML file](https://docs.qodo.ai/qodo-documentation/qodo-gen/agent/workflows/agent-toml-file)
- [OpenAI Codex CLI — Advanced config (subagent TOML)](https://developers.openai.com/codex/config-advanced)
- [Vellum — How to Build Multi-Agent AI Systems with Context Engineering](https://www.vellum.ai/blog/multi-agent-systems-building-with-context-engineering)
