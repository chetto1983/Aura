# Wave OH3 Research — Production Landscape, Anti-Patterns, Observability

**Scout #4 of 4 · 2026-05-25**
**Scope:** CrewAI · AutoGen / AG2 · Letta · Inngest AgentKit · CopilotKit · MetaGPT · AWS Bedrock Multi-Agent · smolagents · anti-patterns · observability.
**Sibling reports:** Scout #2/#3 cover Strands, LangGraph, Kimi K2.5/K2.6, OpenAI Agents SDK. This document covers the *rest of the field* plus the cross-cutting concerns nobody else owns.

---

## 1. TL;DR — 5 actionable lessons for Wave OH3

1. **Hard-cap iteration count, hard-cap fan-out width, hard-cap shared-context bytes — all three, before shipping.** Every production post-mortem (Anthropic, CrewAI, AutoGen, n8n, $47K customer incidents) lands on a runaway loop / fan-out as the dominant cost-and-stability bug. Aura already has `AURA_AGENT_LOOP_MAX_STEPS=5`; OH3 needs a *swarm-wide* analog (max delegations per turn, max bytes any single agent can write to the blackboard per round, per-conversation token budget cap). The "$47K pattern" — one runaway feature exhausting feature budget → workspace budget → provider cap — is the production failure mode, not the loop itself.
2. **Don't replicate "shared message context" naively — it explodes tokens 15× and corrupts agents downstream.** Anthropic confirmed `~15×` token overhead for multi-agent vs. chat; the MAST taxonomy attributes 36.94% of failures to inter-agent misalignment driven by exactly this. For Aura, use **summarised handoff packets** (objective + output format + boundaries + relevant facts) instead of full transcripts — the Anthropic playbook. Letta's `memory_insert` (append-only) is the correct primitive for shared state; `memory_rethink` is unsafe under concurrency.
3. **The Cognition/Anthropic split is real and Aura sits on Cognition's side by default.** Cognition (`Don't Build Multi-Agents`) argues parallel subagents fragment context and ship inconsistent outputs; Anthropic argues you can win 90.2% over single-agent if you accept the 15× token cost and instrument heavily. Aura's hierarchical-tree-with-sync substrate from OH2 is the safer baseline; OH3 should treat *parallel peer-mesh* as opt-in per-`agentdef`, not default. Reserve mesh for read-only research tasks where divergent outputs can be merged by a critic (NOT write/refactor tasks where the Flappy Bird failure mode applies).
4. **Anti-pattern: "critic loops that tautologically PASS."** A critic agent running on the same model family as the worker amplifies shared biases instead of catching them (verified by `arxiv 2505.19477` and the SWE judge-bias audit). Mitigation for Aura: critics must use a *different* model OR an *external ground-truth* check (file diff, test run, structured tool result) — never just "another LLM call." This matches the Aura CLAUDE.md rule "validate with verified benchmarks, never only tool-call counts."
5. **OpenTelemetry GenAI agent spans (`gen_ai.agent.id`, `gen_ai.conversation.id`) exited experimental in early 2026 and are now supported by Langfuse / Phoenix / Datadog / Honeycomb — but the spec still has no `parent_agent_id` or `handoff` event.** Aura needs to emit OTel-compliant base attributes and add its own non-standard `aura.handoff.from`, `aura.handoff.to`, `aura.delegation.depth`, `aura.blackboard.version` attributes. Langfuse self-host fits Aura's "single Docker stack" architecture better than Arize Phoenix; both speak OTel. Without distributed tracing, post-incident debugging of a 5-agent fan-out is effectively impossible — Galileo and Red Hat both rank "no correlation IDs across agents" as a top-7 failure.

---

## 2. CrewAI (2026 state)

**Repo:** [crewAIInc/crewAI](https://github.com/crewaiinc/crewai) · **Docs:** [docs.crewai.com](https://docs.crewai.com/) · **Blog:** [crewai.com](https://crewai.com/).

### 2.1 Process types

| Process | Semantic | Swarm-like? |
|---------|----------|-------------|
| **Sequential** | Default; tasks execute one-after-another in declared order. | No |
| **Hierarchical** | A manager agent (auto-generated or user-supplied) decides which worker handles each task; can reassign and revise. | Closest to "swarm with router" |
| **Consensual** | All agents discuss a task and merge perspectives into one output. | Closest to "debate/blackboard" |

There is **no dedicated `Process.swarm`**; CrewAI's collaboration primitive is `allow_delegation=True` on an agent, which auto-mounts the `Delegate work to coworker` and `Ask question to coworker` tools ([Collaboration docs](https://docs.crewai.com/en/concepts/collaboration)).

### 2.2 Production-relevant problems (well documented)

The hierarchical process is the most-reported broken path in production:

- **[Issue #4783](https://github.com/crewAIInc/crewAI/issues/4783)** — "Hierarchical process delegation fails — manager agents cannot delegate to worker agents."
- **[Issue #2606](https://github.com/crewAIInc/crewAI/issues/2606)** — `DelegateWorkToolSchema` type-validation error: manager passes dict, schema expects string.
- **[Issue #1823](https://github.com/crewAIInc/crewAI/issues/1823)** — "coworker mentioned not found."
- **Production write-up: ["Why CrewAI's Manager-Worker Architecture Fails — and How to Fix It"](https://towardsdatascience.com/why-crewais-manager-worker-architecture-fails-and-how-to-fix-it/)** — "CrewAI executes all tasks sequentially, causing incorrect agent invocation, overwritten outputs, and inflated latency/token usage."
- **["The Delegation Ping-Pong"](https://azguards.com/technical/the-delegation-ping-pong-breaking-infinite-handoff-loops-in-crewai-hierarchical-topologies/)** — "Manager enters an infinite task-reassignment loop, rapidly burning through tokens, saturating the context window, and crashing the runtime via OOM."

### 2.3 Crew vs Flow

`Crew` is a single execution context of cooperating agents. `Flow` ([CrewAI Flows](https://crewai.com/crewai-flows)) is the deterministic outer orchestration layer — event-driven, state-machine, can chain multiple Crews. Flow shipped in late-2024 specifically because Crews alone could not deliver production reliability — same conclusion Cognition reached for Devin. Quote: *"Flows act as the deterministic backbone of an agentic system."*

### 2.4 Translation to Aura

- Aura is Go and won't lift CrewAI directly. The **lesson** to lift: a peer-mesh in OH3 needs an outer deterministic harness (a "Flow" equivalent) — the existing `internal/chat/agentloop.go` is the natural place, not a new subsystem.
- Avoid the "auto-generated manager that grabs worker tools" anti-pattern ([Issue #2054](https://github.com/crewAIInc/crewAI/issues/2054)) — if Aura's coordinator inherits worker tools, the coordinator stops delegating.
- Treat the delegation tool result as **structured JSON** with explicit success/failure, not a free-text "coworker mentioned not found" parse — CrewAI ate three production bugs from sloppy string matching here.

### 2.5 Minimal Crew with delegation (illustrative Python, for shape reference)

```python
from crewai import Agent, Task, Crew, Process

researcher = Agent(role="Researcher", goal="…", allow_delegation=False)
writer     = Agent(role="Writer",     goal="…", allow_delegation=True)
critic     = Agent(role="Critic",     goal="…", allow_delegation=True)

crew = Crew(
    agents=[researcher, writer, critic],
    tasks=[research_task, write_task, critique_task],
    process=Process.hierarchical,   # OR Process.sequential
    manager_llm="gpt-4.1",          # spawned manager
    max_iter=10,                    # HARD CAP — required in prod
)
```

---

## 3. AutoGen / AG2 (2026)

**Repos:** [microsoft/autogen](https://github.com/microsoft/autogen) (v0.4+) and [ag2ai/ag2](https://github.com/ag2ai/ag2) (community fork carrying the 0.2 API forward). **Docs:** [v0.4 AgentChat](https://microsoft.github.io/autogen/stable//user-guide/agentchat-user-guide/index.html).

### 3.1 Team types in 0.4 AgentChat

Per [v0.4 blog](https://www.microsoft.com/en-us/research/blog/autogen-v0-4-reimagining-the-foundation-of-agentic-ai-for-scale-extensibility-and-robustness/) and the [teams reference](https://microsoft.github.io/autogen/stable//reference/python/autogen_agentchat.teams.html):

| Team | Selection mechanism | Best for |
|------|---------------------|----------|
| `RoundRobinGroupChat` | Trivial round-robin | Toy / deterministic demos |
| `SelectorGroupChat` | LLM-based next-speaker selection over shared context | Dynamic collaboration |
| `Swarm` | Local tool-call handoff between agents (OpenAI Swarm pattern) | Decentralised peer mesh |
| `MagenticOneGroupChat` | The Magentic-One orchestrator from MS Research, now packaged as a team | Open-ended web/file tasks |

### 3.2 SelectorGroupChat next-speaker semantic

Per [docs](https://microsoft.github.io/autogen/stable//user-guide/agentchat-user-guide/selector-group-chat.html): an LLM (or a user-supplied function) selects the next speaker from the shared message context every turn. The selector sees the full conversation, every member's `description`, and the last message. *Anti-pattern observed:* `description` strings that are vague ("does research") collapse to whichever agent the selector picked last — leading to single-agent monoculture. Mitigation: description strings should be **discriminative** ("invoke ONLY for FX-quote retrieval; never for general web search").

### 3.3 Swarm: handoff via tool call (OpenAI Swarm port)

Per [Swarm docs](https://microsoft.github.io/autogen/stable//user-guide/agentchat-user-guide/swarm.html), each `AssistantAgent` declares a list of agents it can hand off to:

```python
travel_agent = AssistantAgent(
    "travel_agent",
    model_client=model_client,
    handoffs=["flights_refunder", "user"],
    system_message="You are a travel agent...",
)
flights_refunder = AssistantAgent(
    "flights_refunder",
    model_client=model_client,
    handoffs=["travel_agent", "user"],
    tools=[refund_flight],
    system_message="You are an agent specialized in refunding flights...",
)
termination = HandoffTermination(target="user") | TextMentionTermination("TERMINATE")
team = Swarm([travel_agent, flights_refunder], termination_condition=termination)
```

A handoff is a generated `HandoffMessage`; the receiving agent inherits **the entire shared message context**. This is the "share full context" Anthropic principle baked into the framework — and it is exactly what blows up tokens at scale.

### 3.4 Magentic-One

[Magentic-One](https://microsoft.github.io/autogen/stable//user-guide/agentchat-user-guide/magentic-one.html) is a generalist orchestrator with 4 pre-built agents (WebSurfer, FileSurfer, Coder, ComputerTerminal) and a `MagenticOneGroupChat` team type. It is the closest thing in MS-land to "off-the-shelf swarm." For Aura, the relevant insight is the *fixed agent roster* — Magentic-One doesn't spawn agents dynamically, which sidesteps the runaway-fan-out problem.

### 3.5 Documented bug: Swarm infinite loop in human-in-the-loop mode

[Issue #5831](https://github.com/microsoft/autogen/issues/5831) — "Infinite loop appears when using swarm with human-computer dialogue." Root cause: when the agent failed to understand the prompt and produced no handoff target, the framework fell into an infinite loop. **Mitigation for Aura:** any handoff tool must include an explicit `escalate_to_user` / `terminate` target so the model can always choose to stop.

### 3.6 Translation to Aura

- Aura's `agentdef` registry should reify the equivalent of `handoffs: [name]` — explicit allow-list of which agents an agentdef can hand off to. Without this, peer-mesh degenerates to all-to-all and the search space for "who's the next speaker" explodes.
- A Go port of `SelectorGroupChat` is small (~200 LOC). The LLM-as-selector pattern is appealing for OH3 but introduces a per-turn extra LLM call — budget for it explicitly.
- Always pair `Swarm`-style handoff with a `HandoffTermination` analog. The AutoGen team learnt this the hard way.

---

## 4. Letta (formerly MemGPT, 2026)

**Repos:** [letta-ai/letta](https://github.com/letta-ai/letta), [letta-ai/letta-code](https://github.com/letta-ai/letta-code) · **Docs:** [docs.letta.com](https://docs.letta.com/).

### 4.1 Multi-agent coordination primitives

Three built-in tools ([multi-agent docs](https://docs.letta.com/guides/agents/multi-agent/)):

| Tool | Semantic | Use when |
|------|----------|----------|
| `send_message_to_agent_and_wait_for_reply` | Synchronous RPC between agents | Strict sequential handoff |
| `send_message_to_agent_async` | Fire-and-forget; returns immediately | Parallel fan-out / notification |
| `send_message_to_agents_matching_all_tags` | Tag-based broadcast | Supervisor-worker pattern |

Letta docs explicitly warn: **don't attach both sync and async variants to the same agent** — "the agent becomes confused and uses the tool less reliably." This is an LLM-tool-selection anti-pattern Aura should mirror: a single semantic should have a single tool name.

### 4.2 Shared memory blocks — the most interesting primitive

From [shared memory docs](https://docs.letta.com/guides/agents/multi-agent-shared-memory) and the [tutorial](https://docs.letta.com/tutorials/shared-memory-blocks/):

Two or more agents can be attached to the **same memory block** via `block_ids`. Updates from one agent are immediately visible to the other — no message passing needed. This is the "blackboard" architecture (per [AutoGen group-chat blog](https://notes.muthu.co/2025/10/programming-multi-agent-conversations-with-autogen/), a modern reincarnation of 1980s blackboard systems).

Three update operations with different concurrency safety:

| Operation | Concurrency Safety | Aura analog |
|-----------|---------------------|-------------|
| `memory_insert` | **Safe** — append-only | preferred default |
| `memory_replace` | Mostly safe — fails if target changed (optimistic concurrency) | use for known-stable references |
| `memory_rethink` | **Unsafe** — last-writer-wins, lost updates | forbidden in multi-agent context |

### 4.3 Two-agent shared-memory example

```python
from letta_client import Letta
client = Letta(api_key="…")

shared_block = client.blocks.create(
    label="organization",
    description="Shared information between all agents.",
    value="Company policies...",
)
supervisor = client.agents.create(
    model="anthropic/claude-haiku-4-5-20251001",
    memory_blocks=[{"label": "persona", "value": "I am a supervisor"}],
    block_ids=[shared_block.id],
)
worker = client.agents.create(
    model="anthropic/claude-haiku-4-5-20251001",
    memory_blocks=[{"label": "persona", "value": "I am a worker"}],
    block_ids=[shared_block.id],   # same block — shared visibility
)
```

### 4.4 Translation to Aura

The Letta blackboard is the **closest production analog to what Wave OH3 needs**: agents communicating not by passing messages, but by mutating a versioned shared region. Aura already has SQLite + the wiki — both are blackboard candidates. Specific design rules to steal:

- **Distinguish `insert` from `replace` from `rethink` at the API level.** Append-only first; mutations require optimistic-concurrency tokens; full rewrites are an explicit override.
- **One channel per semantic.** Don't expose both `delegate_sync` and `delegate_async` to the same agentdef.
- **Tag-based broadcast** (`send_message_to_agents_matching_all_tags`) is a reasonable substrate for supervisor-fan-out patterns; Aura could reuse `agentdef.Tier` for the tag axis.

### 4.5 Letta architectural pivot to know about

Per [Letta V1 blog](https://www.letta.com/blog/letta-v1-agent): heartbeats and the original `send_message` tool from MemGPT are **deprecated in V1**. They moved to native reasoning + direct assistant generations. The lesson: tool-based agent control loops drift; **native reasoning loops with explicit terminate tokens win.** Aura's `text_response` terminal tool (per `feedback_check_tmp_sources_then_brainstorm_best`) is already aligned with this.

---

## 5. Other production-relevant systems (one paragraph each)

### 5.1 Inngest AgentKit

[inngest/agent-kit](https://github.com/inngest/agent-kit) · [agentkit.inngest.com](https://agentkit.inngest.com/overview). TypeScript-first; agents are composed into **Networks** with a **Router** (deterministic by default, can be LLM-backed) and a shared **Network State**. Built on Inngest's durable-execution engine — automatic retries, idempotency, persisted step results, "resume from failure" by default. The deterministic-router angle directly addresses the CrewAI hierarchical-process bug class. Notable 2026 addition: [`useAgent` React hook](https://www.inngest.com/blog/agentkit-useagent-realtime-hook) streams durable workflow results to the frontend. **Translation to Aura:** Aura is Go, not TypeScript, but the Network/Router/State decomposition is the right shape; Aura's existing `cmd/aura/app.go` HTTP wiring + SQLite checkpoint table could be the durability substrate.

### 5.2 CopilotKit CoAgents

[CopilotKit/CopilotKit](https://github.com/CopilotKit/CopilotKit) · [docs](https://docs.copilotkit.ai/langgraph). Frontend-focused; the multi-agent story is mostly **LangGraph in the backend, CopilotKit as the realtime UI bridge** ([CoAgents page](https://www.copilotkit.ai/blog/intermediate-state-coagent)). Notable: CopilotKit is the maintainer of [AG-UI](https://github.com/CopilotKit/CopilotKit) — an event protocol so any agent framework can stream intermediate state to a generic UI. **Translation to Aura:** the dashboard SPA could speak AG-UI eventually; not OH3 scope. The interesting primitive is *shared state between backend agents and the UI* — Aura's existing per-conversation SQLite archive + the `/api/conversations` endpoint already covers ~half of this.

### 5.3 MetaGPT

[FoundationAgents/MetaGPT](https://github.com/FoundationAgents/MetaGPT). The original "software-company-as-multi-agent-system" — PM/Architect/Engineer/QA roles with SOP-driven handoffs producing PRD/Design/Code/Tests from a one-line prompt. Core thesis: `Code = SOP(Team)`. In 2026 the same team relaunched as MGX (rebranded "Atoms") with end-to-end production websites in 5 minutes. **Failure mode observed in MAST (it's one of the 7 frameworks analysed):** SOPs hard-code the workflow, so any task off the trained distribution produces FM-2.3 task derailment. **Translation to Aura:** Aura already has skills + prompt overlays — that's the "soft SOP" equivalent. Don't hard-code role chains; use overlays + delegation allow-lists.

### 5.4 AWS Bedrock Multi-Agent Collaboration

[AWS docs](https://docs.aws.amazon.com/bedrock/latest/userguide/agents-multi-agent-collaboration.html) · [GA announcement](https://aws.amazon.com/blogs/machine-learning/amazon-bedrock-announces-general-availability-of-multi-agent-collaboration/). Supervisor pattern with two modes: **supervisor mode** (full plan-then-delegate) and **supervisor-with-routing** (simple requests routed directly to a single collaborator, complex ones fall back to full supervisor). The hybrid is the interesting design — recognising that not every query needs the full mesh — and is functionally a *fast-path classifier in disguise*. Aura already memorised this anti-pattern (`feedback_check_tmp_sources_then_brainstorm_best`); the AWS team papered over it by making both modes always-available, but the fast path remains a code smell. **Translation to Aura:** skip the dual-mode pattern; pick one. (Memory says: pick "tight loop, no router".) The supervisor model itself is fine — that's what OH2's hierarchical-tree already implements.

### 5.5 smolagents (HuggingFace)

[huggingface/smolagents](https://github.com/huggingface/smolagents). Code-as-action: the LLM emits Python instead of JSON tool calls; the framework sandboxes execution via E2B/Modal/Docker. Multi-agent: `agent.name` + `agent.description` are baked into the manager's system prompt, and the manager calls subagents as if they were tools ([multi-agent course](https://huggingface.co/learn/agents-course/unit2/smolagents/multi_agent_systems)). Inherently hierarchical — there is **no peer mesh primitive**, only manager-spawns-worker. That's a feature for safety (no recursive handoff explosion) but limits expressivity. **Translation to Aura:** code-as-action doesn't fit Aura's Go-server-orchestrating-LLM-via-JSON-tools shape, but the *manager-as-system-prompt-with-worker-descriptions* pattern is small and effective. Aura's `agentdef` registry already gives each agent a description; the next step is to *inject sibling descriptions into the coordinator's system prompt* so the coordinator can make informed delegation decisions.

---

## 6. Anti-pattern roundup — the most important section

These are ranked by **how likely they bite Aura in OH3**, not alphabetical. Each cites a real source.

### AP-1. Recursive / circular handoff infinite loop ★★★★★

> "When an agent failed to understand the prompt and produced no handoff target, the call fell into an infinite loop."
> — [microsoft/autogen #5831](https://github.com/microsoft/autogen/issues/5831)
> Also: [LibreChat #10412](https://github.com/danny-avila/LibreChat/discussions/10412), [CrewAI Delegation Ping-Pong](https://azguards.com/technical/the-delegation-ping-pong-breaking-infinite-handoff-loops-in-crewai-hierarchical-topologies/).

**Mitigation for Aura:** every agentdef declares an explicit `terminate` / `escalate_to_user` handoff target as a peer of any worker handoff; coordinator-level hard cap `max_delegation_depth=3` and `max_total_handoffs_per_turn=10`; emit `aura.delegation.depth` on every span and alert when ≥ depth-3 fires more than 5% of turns.

### AP-2. Shared-context bloat blowing the LLM window ★★★★★

> "If a root agent passes its full history to a sub-agent, and that sub-agent does the same, you trigger a context explosion."
> — [Google Developers blog](https://developers.googleblog.com/architecting-efficient-context-aware-multi-agent-framework-for-production/)
> Quantified: Anthropic ~15× tokens vs chat ([engineering blog](https://www.anthropic.com/engineering/built-multi-agent-research-system)); MemU 2026 measures 2% retention loss per step, <60% after 5 cycles ([sitepoint](https://www.sitepoint.com/ai-agent-memory-guide/)).

**Mitigation for Aura:** the handoff packet must be a **summary** with explicit slots (objective / output-format / boundaries / relevant-facts) — never the raw transcript. Anthropic playbook is canon here. Bound packet size at ~2K tokens per handoff; CI gate on it.

### AP-3. Cost explosion via parallel fan-out without budget gates ★★★★★

> "An agent that takes 50 turns on a complex task costs ~$0.90 per session, and running 100 of those per hour amounts to $90/hour, or over $2,100/day. A single session stuck in a loop running 500 turns instead of 50 multiplies these costs dramatically."
> — [RelayPlane blog](https://relayplane.com/blog/agent-runaway-costs-2026)
> The "$47K pattern": one runaway feature exhausts feature budget → workspace buffer → provider cap.
> — [Ravoid blog](https://ravoid.com/blog/ai-agent-budget-enforcement)

**Mitigation for Aura:** three-tier budget enforcement — per-turn ceiling (already exists), per-conversation ceiling (NEW), per-agentdef daily ceiling (NEW). Preflight gate at coordinator: refuse to spawn fan-out N>3 if remaining turn-budget < N × expected-subagent-cost. Surface running cost in the dashboard, not just totals — the $47K incident pattern needs a live alert curve, not a daily report.

### AP-4. Conflicting decisions from parallel subagents (Flappy Bird) ★★★★

> "Sub-agent 1 builds a Super Mario background while sub-agent 2 builds a non-game-asset bird, because neither shares the other's design context."
> — [Cognition: Don't Build Multi-Agents](https://cognition.ai/blog/dont-build-multi-agents)

**Mitigation for Aura:** for any task where outputs must compose (code, doc edits, multi-page writes), use **sequential** delegation with explicit dependencies, not parallel fan-out. Reserve parallel only for **read-only** research where divergent answers can be ranked by a critic. Mark this in `agentdef`: `composable_output: false` forces sequential.

### AP-5. Critic loop converges to tautological PASS ★★★★

> "If all agents in a loop share the same family of weaknesses, the loop can amplify confidence without improving truth—sometimes multi-agent systems just create several instances of the same bias talking to themselves in a more elaborate way."
> — [arxiv 2505.19477 "Judging with Many Minds"](https://arxiv.org/pdf/2505.19477)
> SWE judge-bias audit: [arxiv 2604.16790](https://arxiv.org/html/2604.16790v1).

**Mitigation for Aura:** critics must verify against an **external ground truth** (a tool call, a file diff, a test run, a structured DB read), never just "another LLM saying yes." This is the existing Aura CLAUDE.md rule "validate with verified benchmarks, never only tool-call counts" — generalize it from probes to runtime critic agents. Where a real-model judge is unavoidable, force a **different model family** from the worker (e.g., Sonnet judging a Haiku worker).

### AP-6. Agent stops asking for clarification / withholds information ★★★★

> MAST FM-2.2 "Fail to ask for clarification" + FM-2.4 "Information withholding" — combined ~12% of all observed failures across 7 frameworks.
> — [arxiv 2503.13657 (MAST)](https://arxiv.org/html/2503.13657v1)

**Mitigation for Aura:** Aura already has an `ask_user` tool — keep it; bias the prompt to use it (especially in coordinator). For inter-agent: handoff packet schema must have a `known_unknowns` field; a worker that returns `known_unknowns != []` triggers either re-prompt or escalation rather than silent best-effort.

### AP-7. No or incorrect verification of task completion ★★★★

> MAST FM-3.2 "No or incomplete verification" + FM-3.3 "Incorrect verification" — together with FM-3.1 (premature termination), the entire FC3 category accounts for 21.30% of failures.
> — [MAST paper](https://arxiv.org/pdf/2503.13657)

**Mitigation for Aura:** every multi-agent turn must end with an explicit **acceptance test** — either a structured schema match on the worker output, or a deterministic tool call (e.g., `file.read` confirms the page exists with expected frontmatter). The probe-style "verify the artifact, not the reply" rule from Aura's MEMORY applies here verbatim.

### AP-8. Lost context across handoffs ★★★

> "Critical details disappear when one model's reply exceeds another's context window, forcing the next agent to reason from incomplete information."
> — [Galileo 7-reasons](https://galileo.ai/blog/why-multi-agent-systems-fail) (#2)

**Mitigation for Aura:** persist handoff packets to SQLite (`handoff_log` table: from_agent, to_agent, packet_json, turn_id, timestamp) — `BLOB` if needed. On context window pressure, the next agent can replay the packet sequence instead of the full transcript. This is also the foundation for distributed-trace reconstruction.

### AP-9. Role confusion / boundary violation ★★★

> MAST FM-1.2 "Disobey role specification" — "agents overstep their defined responsibilities, potentially assuming other agents' roles." Combined with FM-2.3 task derailment, role drift is the single biggest specification failure category.
> — [MAST](https://arxiv.org/html/2503.13657v1)

**Mitigation for Aura:** agentdef role descriptions include **negative constraints** ("NEVER do X; if asked, hand off to Y"). The smolagents pattern of injecting sibling descriptions into the coordinator system prompt helps the coordinator route correctly; agentdef YAML should compile that injection block at boot.

### AP-10. Observability collapse (no single trace across N agents) ★★★

> "Non-deterministic parallel execution and opaque orchestration create failures appearing random; traditional debugging tools fail."
> — [Galileo](https://galileo.ai/blog/why-multi-agent-systems-fail) (#7)
> "Stack traces assume linear execution and breakpoints require repeatable state—both impossible in multi-agent workflows."

**Mitigation for Aura:** **mandatory before OH3 ships.** A single `trace_id` per Telegram turn carried through every LLM call, every tool call, every delegation. Emit OTel-compliant spans (next section). Without this, the post-incident question "why did the swarm answer wrong" is unanswerable.

### AP-11. Agent ignores another agent's input ★★

> MAST FM-2.5 "Ignored other agent's input" — agents disregard recommendations from peers.
> — [MAST](https://arxiv.org/html/2503.13657v1)

**Mitigation for Aura:** in handoff-return packets, REQUIRE the caller's next LLM turn to include a citation field referencing which specific outputs it used. Where the caller produces nothing that references the worker's output, log it as a `peer_input_ignored` event and reduce that peer's selection priority next turn.

### AP-12. Hard-coded SOP collapsing on off-distribution tasks ★★

> MetaGPT and ChatDev are the canonical examples — SOPs work brilliantly on the training distribution and fall over on anything else.
> — [MAST](https://arxiv.org/html/2503.13657v1) analysed both.

**Mitigation for Aura:** keep delegation **declarative** (agentdef YAML), not **procedural** (hard-coded chains in Go). Aura's existing skill+overlay pattern is already aligned.

### Already-known Aura anti-patterns (NOT to re-derive)

For completeness — these are already in Aura's MEMORY.md and CLAUDE.md and should NOT be redocumented in OH3:

- ❌ Fast-path classifier (`feedback_check_tmp_sources_then_brainstorm_best`, 4-source convergence).
- ❌ LLM-as-reranker / mini-LLM on CPU for retrieval (`feedback_minillm_cpu_not_viable_for_tool_retrieval`).
- ❌ Compaction at 100% context (Aura's existing rule).
- ❌ Regex on natural language (`feedback_no_regex_for_nlp`).
- ❌ Trusting tool-call count as quality metric (CLAUDE.md "validate with verified benchmarks").

---

## 7. Observability — OTel GenAI 2026 status + recommended Aura instrumentation

### 7.1 OTel GenAI semantic conventions — 2026 state

- **Status:** [Agent spans page](https://opentelemetry.io/docs/specs/semconv/gen-ai/gen-ai-agent-spans/) — client spans graduated from experimental in early 2026; agent spans still partially experimental ([Greptime overview](https://www.greptime.com/blogs/2026-05-09-opentelemetry-genai-semantic-conventions)). Vendor adoption: Datadog, Honeycomb, New Relic; framework adoption: LangChain, CrewAI, AutoGen, AG2 emit OTel-compliant spans natively or via instrumentation packages.
- **Attributes defined for agents:**
  - `gen_ai.agent.id` — unique identifier of the agent
  - `gen_ai.agent.name` — human-readable name
  - `gen_ai.agent.version`
  - `gen_ai.agent.description`
  - `gen_ai.conversation.id` — session/thread correlator
  - `gen_ai.workflow.name`
- **Span kinds:** `CLIENT` (remote agent invocation), `INTERNAL` (in-process invocation), `INTERNAL` for workflow spans.
- **Notable gaps (still unstandardised as of 2026-05):**
  - No `parent_agent_id` / hierarchical relationship attribute
  - No standard `handoff` / `delegation` event type
  - No standard `run_id` separate from trace context
  - No swarm-specific attributes (selector model, broadcast tag, blackboard version)

### 7.2 What a "good" multi-agent trace looks like

Per [Datadog parent-child vs span-links](https://www.datadoghq.com/blog/parent-child-vs-span-links-tracing/) and [Red Hat agentic tracing](https://developers.redhat.com/articles/2026/04/06/distributed-tracing-agentic-workflows-opentelemetry):

```
trace_id = abc123              ← one per Telegram turn
└── span: coordinator.turn (root, INTERNAL)
    ├── span: llm.call         (CLIENT, gen_ai.system=openai, model, tokens)
    │   └── event: tool_call_emitted (delegate_to=researcher)
    ├── span: agent.researcher.invoke (INTERNAL, parent=coordinator)
    │   ├── attr aura.handoff.from=coordinator
    │   ├── attr aura.handoff.to=researcher
    │   ├── attr aura.delegation.depth=1
    │   ├── attr aura.handoff.packet_size_tokens=487
    │   ├── span: llm.call (CLIENT)
    │   ├── span: tool.web.search (INTERNAL)
    │   └── span: tool.text_response (INTERNAL, terminal=true)
    ├── span: agent.writer.invoke (INTERNAL, parent=coordinator)
    │   └── … (parallel to researcher, sibling span)
    └── span: tool.text_response (INTERNAL, terminal=true)
```

Aura is Go, so `go.opentelemetry.io/otel` is the right SDK. Existing zap logging stays (structured fields parallel the OTel attributes); the OTel export is additive, not replacing.

### 7.3 Recommended Aura instrumentation contract (OH3 minimum)

| Attribute | OTel-standard? | Required? | Notes |
|-----------|----------------|-----------|-------|
| `gen_ai.agent.id` | YES | YES | from agentdef registry |
| `gen_ai.agent.name` | YES | YES | from agentdef YAML |
| `gen_ai.conversation.id` | YES | YES | Telegram chat_id + turn epoch |
| `gen_ai.system` | YES | YES | "openai" (since LLM is OAI-compat) |
| `gen_ai.request.model` | YES | YES | |
| `gen_ai.usage.input_tokens` | YES | YES | |
| `gen_ai.usage.output_tokens` | YES | YES | |
| `aura.handoff.from` | NO (custom) | YES | source agentdef id |
| `aura.handoff.to` | NO (custom) | YES | target agentdef id |
| `aura.delegation.depth` | NO (custom) | YES | hard cap alarm at ≥3 |
| `aura.handoff.packet_size_tokens` | NO (custom) | YES | bound check |
| `aura.handoff.packet_sha256` | NO (custom) | NO | for replay |
| `aura.blackboard.version` | NO (custom) | YES if blackboard used | optimistic-concurrency token |
| `aura.budget.tokens_remaining` | NO (custom) | YES | per-conversation |

### 7.4 Backend choice

Per [Langfuse vs Phoenix comparison](https://www.zenml.io/blog/langfuse-vs-phoenix) and [Braintrust 2026 review](https://www.braintrust.dev/articles/best-llm-tracing-tools-2026):

| Backend | Pros for Aura | Cons |
|---------|---------------|------|
| **Langfuse** (self-hosted Postgres+Clickhouse) | Self-host fits Aura single-stack ethos; native OTel; framework-agnostic; nested span tree rendering | Extra DB sidecar (ClickHouse), more ops |
| **Arize Phoenix** (self-host or cloud) | OpenInference convention well-defined for agents; eval-first design with LLM-as-judge built in | Heavier UI; less Go ecosystem |
| **LangSmith** | Best LangGraph rendering | Aura isn't LangGraph; vendor lock |
| **Honeycomb / Datadog** | Pure OTel; no special semantics needed | $$ |

**Recommendation:** start with **vanilla OTel SDK + OTLP exporter** in Aura. Make the *backend* a Docker-compose-time choice. Default: Langfuse self-host (one more sidecar in `compose.yaml`, profile-gated). Phoenix is an option for users who want evals out-of-the-box.

### 7.5 What Aura already has that helps

- `internal/logging` (zap) — already structured, can be wrapped to dual-write to OTel.
- `conversations` SQLite archive — every turn already persisted; trace_id can be added as a column to retroactively join logs ↔ traces.
- `cmd/probe_chat` — once OTel is wired, every probe run can ship traces too, enabling regression diffs by trace shape, not just reply text.

---

## 8. Open questions for follow-up

1. **Does Aura's `agentdef` registry need a versioned schema migration *now* (before OH3) to add `handoffs: []`, `delegation_max_depth`, `composable_output`, `tags`?** Adding fields later is cheap; renaming is expensive — design these in one shot.
2. **Blackboard substrate: SQLite table vs wiki page vs in-memory map?** Letta uses a typed memory block. Aura already has the wiki — could a *transient blackboard wiki page* work, with `memory_insert` / `memory_replace` / `memory_rethink` mapped to existing wiki tools? Performance question: wiki writes hit Git, which is slow per op.
3. **OTel export overhead:** with parallel fan-out of N=5, emitting ~30 spans per turn, what's the latency budget for OTLP export? Should the export be async batched? Inngest-style durable execution if the export endpoint is down?
4. **Critic-with-different-model:** Aura currently uses a single LLM endpoint per conversation. Adding a second endpoint (different model family) for critics doubles config complexity. Worth it for accuracy gain?
5. **Cost dashboard real-time:** the "$47K pattern" requires live alerting, not daily reports. Where does this live in Aura's existing dashboard — `/api/budget`? — and what's the alert delivery channel (Telegram message to the user when 80% of daily cap hit)?
6. **Memory pressure under shared-blackboard concurrency:** Letta documents `memory_rethink` is unsafe. What happens in Aura if two agents touch the same wiki page simultaneously? The existing per-page mutex helps but doesn't solve the "lost intent" problem.
7. **MAST-style failure annotation in CI:** can a probe-test harness auto-classify failed runs into MAST's 14 categories (e.g., FM-1.5 "unaware of termination conditions" if `tool_calls >= max_steps`)? Would give CI dashboards a categorical breakdown without manual triage.

---

## Sources

### CrewAI
- [crewAIInc/crewAI repo](https://github.com/crewaiinc/crewai)
- [Collaboration docs](https://docs.crewai.com/en/concepts/collaboration)
- [CrewAI Flows](https://crewai.com/crewai-flows)
- [Issue #4783 — hierarchical delegation fails](https://github.com/crewAIInc/crewAI/issues/4783)
- [Issue #2606 — DelegateWorkTool type error](https://github.com/crewAIInc/crewAI/issues/2606)
- [Issue #1823 — coworker not found](https://github.com/crewAIInc/crewAI/issues/1823)
- [Issue #2054 — manager hijacks worker tools](https://github.com/crewAIInc/crewAI/issues/2054)
- [TDS — Why CrewAI's Manager-Worker Architecture Fails](https://towardsdatascience.com/why-crewais-manager-worker-architecture-fails-and-how-to-fix-it/)
- [AZ Guards — Delegation Ping-Pong](https://azguards.com/technical/the-delegation-ping-pong-breaking-infinite-handoff-loops-in-crewai-hierarchical-topologies/)

### AutoGen / AG2
- [microsoft/autogen repo](https://github.com/microsoft/autogen)
- [AutoGen 0.4 launch blog](https://www.microsoft.com/en-us/research/blog/autogen-v0-4-reimagining-the-foundation-of-agentic-ai-for-scale-extensibility-and-robustness/)
- [AgentChat user guide](https://microsoft.github.io/autogen/stable//user-guide/agentchat-user-guide/index.html)
- [SelectorGroupChat](https://microsoft.github.io/autogen/stable//user-guide/agentchat-user-guide/selector-group-chat.html)
- [Swarm](https://microsoft.github.io/autogen/stable//user-guide/agentchat-user-guide/swarm.html)
- [Magentic-One](https://microsoft.github.io/autogen/stable//user-guide/agentchat-user-guide/magentic-one.html)
- [Teams API reference](https://microsoft.github.io/autogen/stable//reference/python/autogen_agentchat.teams.html)
- [Group Chat design pattern](https://microsoft.github.io/autogen/stable//user-guide/core-user-guide/design-patterns/group-chat.html)
- [Issue #5831 — Swarm infinite loop with HITL](https://github.com/microsoft/autogen/issues/5831)

### Letta
- [letta-ai/letta repo](https://github.com/letta-ai/letta)
- [Multi-agent systems](https://docs.letta.com/guides/agents/multi-agent/)
- [Supervisor-worker pattern](https://docs.letta.com/tutorials/multi-agent/supervisor-worker/)
- [Shared memory](https://docs.letta.com/guides/agents/multi-agent-shared-memory)
- [Shared memory blocks tutorial](https://docs.letta.com/tutorials/shared-memory-blocks/)
- [Async cookbook](https://docs.letta.com/cookbooks/multi-agent-async)
- [Letta V1 blog (architecture pivot)](https://www.letta.com/blog/letta-v1-agent)

### Other systems
- [Inngest AgentKit overview](https://agentkit.inngest.com/overview)
- [inngest/agent-kit repo](https://github.com/inngest/agent-kit)
- [useAgent hook blog](https://www.inngest.com/blog/agentkit-useagent-realtime-hook)
- [CopilotKit/CopilotKit repo](https://github.com/CopilotKit/CopilotKit)
- [CopilotKit LangGraph docs](https://docs.copilotkit.ai/langgraph)
- [CoAgents blog](https://www.copilotkit.ai/blog/intermediate-state-coagent)
- [FoundationAgents/MetaGPT repo](https://github.com/FoundationAgents/MetaGPT)
- [AWS Bedrock Multi-Agent Collaboration docs](https://docs.aws.amazon.com/bedrock/latest/userguide/agents-multi-agent-collaboration.html)
- [AWS Bedrock GA announcement](https://aws.amazon.com/blogs/machine-learning/amazon-bedrock-announces-general-availability-of-multi-agent-collaboration/)
- [huggingface/smolagents repo](https://github.com/huggingface/smolagents)
- [smolagents multi-agent course](https://huggingface.co/learn/agents-course/unit2/smolagents/multi_agent_systems)

### Anti-patterns + post-mortems
- [Cognition: Don't Build Multi-Agents](https://cognition.ai/blog/dont-build-multi-agents)
- [Anthropic: How we built our multi-agent research system](https://www.anthropic.com/engineering/built-multi-agent-research-system)
- [Anthropic engineering blog reposted on bytebytego](https://blog.bytebytego.com/p/how-anthropic-built-a-multi-agent)
- [MAST paper (Why Do Multi-Agent LLM Systems Fail?)](https://arxiv.org/html/2503.13657v1) · [PDF](https://arxiv.org/pdf/2503.13657)
- [Galileo — 7 Reasons Multi-Agent Systems Fail](https://galileo.ai/blog/why-multi-agent-systems-fail)
- [Towards Data Science — The Multi-Agent Trap](https://towardsdatascience.com/the-multi-agent-trap/)
- [Augment Code — Why Multi-Agent LLM Systems Fail](https://www.augmentcode.com/guides/why-multi-agent-llm-systems-fail-and-how-to-fix-them)
- [LibreChat #10412 — recursive agent handoff](https://github.com/danny-avila/LibreChat/discussions/10412)
- [AtlanKnow — 13 Agent Harness Anti-Patterns](https://atlan.com/know/agent-harness-failures-anti-patterns/)
- [arxiv 2505.19477 — Judging with Many Minds (bias amplification)](https://arxiv.org/pdf/2505.19477)
- [arxiv 2604.16790 — Auditing LLM-as-a-Judge for SWE](https://arxiv.org/html/2604.16790v1)
- [arxiv 2604.03143 — TokenDance (KV-cache sharing)](https://arxiv.org/pdf/2604.03143)
- [Google Devs blog — Architecting context-aware multi-agent](https://developers.googleblog.com/architecting-efficient-context-aware-multi-agent-framework-for-production/)
- [SitePoint — Agent Memory 2026](https://www.sitepoint.com/ai-agent-memory-guide/)
- [Beam.ai — 6 Multi-Agent Orchestration Patterns](https://beam.ai/agentic-insights/multi-agent-orchestration-patterns-production)

### Cost / runaway incidents
- [RelayPlane — Agent Runaway Costs 2026](https://relayplane.com/blog/agent-runaway-costs-2026)
- [Ravoid — AI Agent Budget Enforcement ($47K pattern)](https://ravoid.com/blog/ai-agent-budget-enforcement)
- [MLflow — Prevent Runaway Agent Costs](https://mlflow.org/blog/agent-costs-mlflow-gateway)
- [RunCycles — $6 in 30 seconds three-line fix](https://runcycles.io/blog/runaway-demo-agent-cost-blowup-walkthrough)
- [LiteLLM — Agent Iteration Budgets (A2A)](https://docs.litellm.ai/docs/a2a_iteration_budgets)

### Observability
- [OTel GenAI agent spans spec](https://opentelemetry.io/docs/specs/semconv/gen-ai/gen-ai-agent-spans/)
- [Greptime — How OpenTelemetry Traces LLM/Agent/MCP](https://www.greptime.com/blogs/2026-05-09-opentelemetry-genai-semantic-conventions)
- [Zylos — OpenTelemetry AI Agent Observability](https://zylos.ai/research/2026-02-28-opentelemetry-ai-agent-observability)
- [Uptrace — OpenTelemetry for AI Systems 2026](https://uptrace.dev/blog/opentelemetry-ai-systems)
- [Arize OpenInference spec](https://arize-ai.github.io/openinference/spec/semantic_conventions.html)
- [Arize-ai/openinference repo](https://github.com/Arize-ai/openinference)
- [Langfuse — Trace IDs & Distributed Tracing](https://langfuse.com/docs/observability/features/trace-ids-and-distributed-tracing)
- [Langfuse vs Phoenix comparison (ZenML)](https://www.zenml.io/blog/langfuse-vs-phoenix)
- [Braintrust — Best LLM tracing tools 2026](https://www.braintrust.dev/articles/best-llm-tracing-tools-2026)
- [Datadog — Parent-child vs span links](https://www.datadoghq.com/blog/parent-child-vs-span-links-tracing/)
- [Red Hat — Distributed tracing for agentic workflows](https://developers.redhat.com/articles/2026/04/06/distributed-tracing-agentic-workflows-opentelemetry)
- [Agent Patterns — Distributed agent tracing](https://www.agentpatterns.tech/en/observability-monitoring/agent-tracing)
