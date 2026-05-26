# OH3 Research — Kimi K2.5/K2.6 + OpenAI Agents SDK (Scout #3)

Date: 2026-05-25
Author: Scout #3 of 4 for Wave OH3 (peer-mesh agent swarm with runtime communication loop)
Status: research dump — not a plan
Sources: local Kimi K2.5 paper (`D:/tmp/aura-agent-loop-papers/2602.02276-Kimi-K2.5.txt`), kimi.com blog + help, openai-agents-python docs + source, claude.com Agent SDK blog, third-party PARL repo.

---

## 1. TL;DR — top 5 patterns lift-worthy

1. **Kimi `create_subagent` + `assign_task` tool pair is the cleanest substrate primitive ever published.** Two tools, ~20 lines of schema, no framework. The orchestrator declares an archetype once (`create_subagent(name, system_prompt)`) and then dispatches N parallel jobs (`assign_task(agent, prompt)`) returning one message each. This maps 1:1 onto Aura's existing `tools/registry.Tool` interface and OH1's `agentdef` registry — port is mostly wiring.
2. **Sub-agents stay frozen; only the orchestrator is trained.** K2.5's PARL paper makes this the central design choice: subagent outputs are treated as **environmental observations**, not differentiable decisions. For Aura (no RL): the practical translation is "subagents are stateless workers that don't mutate orchestrator context" — exactly the OH1 hierarchical-tree contract. OH3 doesn't need to change subagent semantics; it needs to add parallel dispatch + result aggregation.
3. **OpenAI Agents SDK's `handoff` is the *anti-pattern* for our use case.** Handoff = transfer control, original agent does NOT come back. That collapses to a 1-of-N router, not a swarm. Aura wants `agents-as-tools` (Anthropic + OpenAI both call it that), where the orchestrator stays in control and tool-calls a worker that returns a result. This is what OH1 already does.
4. **Result aggregation = "one message back" — flat, no streaming, no mid-flight peer chat.** Both Kimi K2.5 paper and OpenAI `Agent.as_tool` converge on this: a subagent run is a synchronous black box that yields one final string/struct. Direct peer-to-peer mid-task chat is explicitly listed as future work in K2.5 ("introducing direct sub-agent communication" — kimi.com blog). For OH3 v1: do NOT build a peer-mesh chat bus. Build parallel fan-out + ordered result merge. Mesh chat is OH4 or later.
5. **Critical Steps metric is how Kimi judges parallelism — adopt it for Aura's bench.** Wall-clock per query is misleading once swarms exist. Critical Steps = orchestrator steps + max(subagent_steps_in_group). For Aura: a probe of an OH3 swarm should report both `total_tool_calls` (sum) and `critical_steps` (longest path). A swarm that runs 50 subagents at 1 tool each in parallel is **5 critical steps**, not 50. Without this metric Aura will misdiagnose parallelism wins as cost regressions.

---

## 2. Kimi K2.5 + K2.6 Agent Swarm — full architecture

### 2.1 Source map

| Source | URL / path | Authority |
|---|---|---|
| K2.5 tech report | `D:/tmp/aura-agent-loop-papers/2602.02276-Kimi-K2.5.txt` (arXiv 2602.02276v1) | canonical |
| K2.5 blog | https://www.kimi.com/blog/agent-swarm | marketing + Top-Tier UX |
| K2.6 blog | https://www.kimi.com/blog/kimi-k2-6 | scale upgrade announce |
| K2.6 help / beta access | https://www.kimi.com/help/agent/agent-swarm | tier gating + limits |
| InfoQ release writeup | https://www.infoq.com/news/2026/02/kimi-k25-swarm/ | independent recap |
| DataCamp tutorial | https://www.datacamp.com/tutorial/kimi-k2-agent-swarm-guide | user-facing examples |
| 3rd-party PARL Python | https://github.com/The-Swarm-Corporation/PARL | NOT official; useful as ref impl of reward shape |

### 2.2 Architecture (paper §3, Figure 3)

```
                       USER PROMPT
                            │
                            ▼
                ┌───────────────────────┐
                │  ORCHESTRATOR (K2.5)  │  ← trainable; sees full task
                │  tools: web/search/   │
                │  code + create_sub   │
                │  agent + assign_task │
                └─────┬─────────┬───────┘
                      │         │
            create_subagent     │
            (name, sys_prompt)  │
                      │         │
                      ▼         ▼
              ┌────────┐   ┌────────┐   ┌────────┐
              │ SubA   │   │ SubB   │ … │ SubN   │   ← FROZEN; isolated ctx
              │ web_   │   │ deep_  │   │ video_ │      max ~100 steps each
              │ search │   │ research│   │ analyze│
              └────┬───┘   └────┬───┘   └────┬───┘
                   │            │            │
                   └────────── ONE MESSAGE EACH ─────────┐
                                                          ▼
                              ┌─────────────────────────────┐
                              │  ORCHESTRATOR resumes,      │
                              │  merges results, optionally │
                              │  spawns another wave        │
                              └─────────────────────────────┘
```

Key invariants (verbatim from paper, line refs to local txt):

- "the model is equipped with **interfaces for sub-agent creation and task delegation**" (L57)
- "sub-agents are frozen and their **execution trajectories are excluded from the optimization objective**" (L58)
- "subagents maintain **independent working memories** and perform local reasoning without directly mutating or contaminating the global context" (L766-767)
- "**Only task-relevant outputs** — rather than full interaction traces — are selectively routed back to the orchestrator" (L768)
- "When the agent is done, it will return a **single message back to you**" (L1686, assign_task tool description)

### 2.3 Canonical tool schemas (paper Appendix E.8, L1656-1700)

```json
{
  "name": "create_subagent",
  "description": "Create a custom subagent with specific system prompt and name for reuse.",
  "parameters": {
    "type": "object",
    "properties": {
      "name":          {"type": "string", "description": "Unique name for this agent configuration"},
      "system_prompt": {"type": "string", "description": "System prompt defining the agent's role, capabilities, and boundaries"}
    },
    "required": ["name", "system_prompt"]
  }
}
```

```json
{
  "name": "assign_task",
  "description": "Launch a new agent. Usage notes:\n1. You can launch multiple agents concurrently whenever possible, to maximize performance;\n2. When the agent is done, it will return a single message back to you.",
  "parameters": {
    "type": "object",
    "properties": {
      "agent":  {"type": "string", "description": "Specify which created agent to use."},
      "prompt": {"type": "string", "description": "The task for the agent to perform"}
    },
    "required": ["agent", "prompt"]
  }
}
```

That's the entire user-facing API surface for swarm mode. Everything else is the model learning when to call them. Subagents inherit the base toolset (search, browser, code, shell) — they are NOT restricted to a curated subtool list; the system prompt does the restriction.

### 2.4 Spawning model — dynamic, not pre-declared

> "Within an agent swarm, subagents are **dynamically instantiated rather than pre-defined**. Through PARL, the orchestrator learns adaptive policies to create and schedule self-hosted subagents in response to evolving task structures and problem states." (L753-755)

The orchestrator decides at runtime: "for this query I want a `web_researcher`, a `data_analyzer`, and 30 `paragraph_summarizer` clones." There is no pre-declared archetype YAML; the system prompt IS the archetype, generated by the orchestrator in the same turn.

Reuse-across-tasks is in scope (`name` field is "unique name for this agent configuration ... for reuse across tasks") but the paper doesn't claim persistence across separate user sessions.

### 2.5 When to spawn vs when to tool-call directly

The paper is explicit that this is a **learned policy**, not a heuristic:

> "parallelism is not presumed to be inherently advantageous; decisions regarding whether, when, and how to parallelize are **explicitly learned** through environmental feedback and RL-driven exploration." (L221-222)

PARL's reward function (paper §3, L237-260):

```
r_PARL = λ₁·r_parallel + λ₂·r_finish + r_perf
```

- `r_parallel`: rewards subagent instantiation — counters "serial collapse" (defaulting to single-agent execution).
- `r_finish`: rewards completed subtasks — counters "fake parallelism" (orchestrator spawns 100 subagents that do nothing, just to inflate r_parallel).
- `r_perf`: task-level outcome reward — the only one that survives annealing.
- λ₁, λ₂ are **annealed to zero** over training so the final policy optimizes purely for `r_perf`. The shaping rewards are scaffolding, not the deployed objective.

**For Aura (non-RL substrate):** the equivalent is a static cost heuristic in the orchestrator prompt, e.g. "spawn a subagent when the subtask is independent of others AND has ≥3 tool calls AND/OR needs a different toolset." No training loop required for v1.

### 2.6 Critical Steps metric (paper §3, L262-287)

```
critical_steps = Σ_stages [ orch_steps(stage) + max_i(sub_steps_i(stage)) ]
```

This is the canonical "wall-clock if you had infinite cores" metric. The paper uses it as a resource constraint during training, but it's equally useful at eval time. **Aura should adopt this for OH3 probes** — see TL;DR #5.

### 2.7 Reported numbers (paper §5.2 + blogs)

| Metric | K2.5 (Jan 2026) | K2.6 (Apr/May 2026) |
|---|---|---|
| Max sub-agents | 100 | 300 |
| Max coordinated steps | 1,500 | 4,000 |
| Wall-clock speedup vs single-agent | up to 4.5× (WideSearch, 3-4.5× on Figure 8) | "4.5×" repeated in marketing |
| BrowseComp (Agent Swarm) | 78.4% (paper Table 6) | 86.3% (vs GPT-5.4's 78.4%, nerdleveltech) |
| WideSearch Item-F1 | 79.0% (beats Claude Opus 4.5 76.2%) | not disaggregated |
| Single-agent K2.5 BrowseComp | 60.6% (so +17.8% absolute from swarm) | — |

**Step-limit configs from paper L1702-1708 (canonical, not marketing):**

| Benchmark | Orchestrator steps | Sub-agent steps |
|---|---|---|
| BrowseComp | 15 | 100 each |
| WideSearch | 100 | 100 each |
| In-house bench | 100 | 50 each |

Caveat: "100 sub-agents" and "1,500 tool calls" are **soft caps observed during eval**, not hard architectural limits. The model can in principle spawn fewer or more. Marketing rounds these into headline numbers.

### 2.8 Shared context = there isn't one (in v1)

Critically: K2.5 paper describes shared context as **selective routing of outputs back to orchestrator**, not a blackboard:

> "Long-horizon tasks are decomposed into parallel, semantically isolated subtasks, each executed by a specialized subagent with a bounded local context. … these subagents maintain independent working memories and perform local reasoning without directly mutating or contaminating the global context of the central orchestrator." (L764-767)

The kimi.com blog explicitly lists **"introducing direct sub-agent communication"** as future work. So in shipped K2.5/K2.6:

- No blackboard read/write tool exposed to subagents.
- No mid-flight peer chat.
- Communication is request/response through the orchestrator.

This is a **tree, not a mesh.** Same shape as Aura OH1.

### 2.9 K2.6 deltas (vs K2.5)

From kimi.com/blog/kimi-k2-6 + kimi.com/help/agent/agent-swarm:

- **Scale**: 100→300 subagents, 1,500→4,000 steps.
- **Skills**: convert PDFs/xlsx/docx into reusable "Skills" (similar to Anthropic skills); K2.6 captures "structural and stylistic DNA."
- **Claw Groups** (research preview): multi-agent + multi-human collaboration with persistent cross-device memory. This is the first hint at the peer-mesh / shared-state direction.
- **3-dim reward**: "quality, genuine parallelism, task completion rates" — restated PARL rewards, no new shape disclosed.
- **Tier gating**: Allegretto ($39/mo) / Allegro ($99) / Vivace ($199). Free tier is excluded.

No new tool schemas published for K2.6.

### 2.10 Pricing & access

| Channel | K2.5 | K2.6 |
|---|---|---|
| API per 1M input tokens | $0.60 | $0.55 |
| API per 1M output tokens | $2.50 | $2.65 |
| Open weights | Yes (HuggingFace `moonshotai/Kimi-K2.5`, ~595 GB BF16, MIT-modified) | Yes (`moonshotai/Kimi-K2.6`) |
| Agent Swarm web UI | Top-Tier subscribers (kimi.com/agent-swarm) | Allegretto+ ($39+/mo) |
| Agent Swarm via API | Not clearly documented as a separate endpoint — likely just `create_subagent`/`assign_task` exposed as tools on the chat completions call | Same — no separate endpoint |

**Sub-agent calls are billed as additional context.** Each `assign_task` instantiates a new context window that bills against the same account. The kimi.com pricing page and lushbinary/avenchat reviews confirm "swarm tasks consume significantly more quota" — concretely, multiplied by N subagents × steps × output length. Order-of-magnitude: a 50-subagent BrowseComp run can hit 200k+ output tokens easily.

### 2.11 Known issues / anti-patterns

From paper + reviews + community posts:

| Issue | Source | Aura implication |
|---|---|---|
| **Serial collapse** | paper L247-248 | Without `r_parallel` shaping, orchestrator defaults to single-agent. Aura needs an explicit prompt-level nudge ("when X, prefer parallel") OR a structural limit on serial tool chains. |
| **Fake parallelism** | paper L249-251 | Orchestrator spawns useless subagents to inflate counts. Aura needs a per-subagent useful-output check or a result-merge guard. |
| **Observability gap** | kimik2ai pricing review; multiple blogs | "Most agent frameworks do not provide granular token accounting per sub-agent, per task, or per swarm run." Aura should log per-subagent token + tool-call counts from day 1. |
| **Drift over long horizons** | venturebeat | K2.5 reportedly stable to 200-300 sequential tool calls; longer is unverified. Aura should hard-cap subagent steps per spawn. |
| **Cost blowup** | lushbinary, eesel | 50 subagents at 100 steps each = 5000 tool calls = order of $1-5 per query at K2.5 rates. Aura needs a per-turn cost ceiling. |
| **No peer chat in v1** | kimi.com blog "future work" | Don't try to lift mid-flight peer communication; it's not in the shipped model. |

---

## 3. OpenAI Swarm → OpenAI Agents SDK

### 3.1 Status

- **OpenAI Swarm** (Oct 2024, github.com/openai/swarm): officially deprecated; "Educational framework exploring ergonomic, lightweight multi-agent orchestration."
- **OpenAI Agents SDK** (Mar 2025, github.com/openai/openai-agents-python): production replacement. Latest as of 2026-05-19: v0.17.3.
- **Languages**: Python (`openai-agents-python`) and TypeScript (`openai-agents-js`). **No Go SDK.** Aura cannot wire directly; we lift the patterns.

### 3.2 Primitives (from openai.github.io/openai-agents-python/)

| Primitive | What it is |
|---|---|
| `Agent` | LLM + instructions + tools + handoffs + guardrails |
| `Runner` | Executes an agent run (`Runner.run()` / `Runner.run_streamed()`) |
| `Tool` | Function exposed to the agent (function-tool, hosted tool, or MCP) |
| `Handoff` | Tool that transfers control to another agent; **original agent does NOT resume** |
| `Agent.as_tool()` | Wrap an agent as a tool; manager keeps control, gets result back |
| `Guardrail` | Input/output validation hook |
| `Session` | Conversation history store (Redis/SQLAlchemy/MongoDB/Dapr backends) |
| `RunContextWrapper` | Per-run mutable app state passed to tools/lifecycle |
| Tracing | Built-in spans for debugging |

### 3.3 Handoff vs agents-as-tools — semantic comparison

| | Handoff | Agents as tools |
|---|---|---|
| Control return | NO — new agent takes over the run | YES — manager keeps control |
| LLM-visible shape | tool named `transfer_to_<agent>` | tool named after the wrapped agent |
| Conversation history | full history passed by default (configurable via `input_filter`) | only the `tool_input` is passed |
| Context isolation | shared run + shared `RunContextWrapper` | shared run, but nested `tool_input` separate |
| When to use | router pattern: triage → specialist takes the user dialogue | orchestrator pattern: manager fans out, merges, replies |

**For Aura OH3, agents-as-tools is the right primitive.** Handoff is for sequential customer-service routing (the "billing agent / refund agent / triage agent" example), not parallel swarm work.

### 3.4 Code example — agents as tools (closest to Kimi pattern)

```python
from agents import Agent, Runner

researcher = Agent(name="Researcher",
                   instructions="Find facts. Cite URLs.",
                   tools=[web_search_tool])

writer = Agent(name="Writer",
               instructions="Compose a polished paragraph from research notes.")

manager = Agent(
    name="Manager",
    instructions="You delegate research, then writing.",
    tools=[
        researcher.as_tool(tool_name="research", tool_description="Research a topic"),
        writer.as_tool(tool_name="write",      tool_description="Write a paragraph from notes"),
    ],
)

result = await Runner.run(manager, "Write 1 paragraph on Aura's wiki design.")
```

Parallel execution uses `asyncio.gather` over multiple `Runner.run()` calls (openai.github.io/openai-agents-python/multi_agent/, "Parallel execution via `asyncio.gather`"). Note: **parallelism is NOT automatic when the LLM emits multiple tool-calls in one turn** — the SDK awaits sequentially unless the user wires `asyncio.gather` explicitly. This is a docs gotcha; community discussion on github.com/openai/openai-agents-python issues confirms it.

### 3.5 Code example — handoff (the anti-pattern for swarm, the right pattern for routing)

```python
from agents import Agent, handoff
from agents.extensions.handoff_prompt import RECOMMENDED_PROMPT_PREFIX

billing  = Agent(name="Billing",  instructions="Handle billing questions.")
refund   = Agent(name="Refund",   instructions="Process refunds.")
triage   = Agent(
    name="Triage",
    instructions=f"{RECOMMENDED_PROMPT_PREFIX}\nRoute users to billing or refund.",
    handoffs=[billing, handoff(refund)],   # bare agent OR handoff()-wrapped
)

result = await Runner.run(triage, "I want a refund.")
# After the handoff fires, `refund` answers the user directly.
# `triage` does NOT see refund's reply; the run continues under refund.
```

Key semantic from openai.github.io/openai-agents-python/handoffs/:

- Handoff appears to the LLM as a tool `transfer_to_refund`.
- `input_filter` lets you trim history (default: full history forwarded).
- `on_handoff` callback fires server-side (logging, state mutation).
- `nest_handoff_history` can collapse prior turns into a summary block.
- `RECOMMENDED_PROMPT_PREFIX` is the official boilerplate to inject — without it, models routinely forget they have handoff tools.

### 3.6 Shared state model

- `RunContextWrapper.context`: per-run app state. **Not sent to the LLM.** Tools read/write it via `ctx.context`. Subagents called via `Agent.as_tool()` share the same wrapper by default — so this IS a usable blackboard within a single run.
- `Session`: persistent conversation history across runs (Redis/SQL/Mongo). Tied to a session_id, not shared across agents within one run.
- Tracing spans: built-in OpenTelemetry-style spans for every agent/tool call. The headline observability primitive missing from Kimi.

**Aura implication:** the OH1 hierarchical-tree already has a per-turn context bag (passed through `internal/agent`). OH3 can extend it with a typed `Blackboard` interface read/written by parallel subagents within one round, mirroring `RunContextWrapper.context`.

### 3.7 Realtime API + handoffs

`openai/openai-realtime-agents` (github) demonstrates handoffs over the Realtime API for voice agents:

- Native to Realtime API: WebRTC streaming, `conversation.item.create`, `response.create`, `session.update`.
- Userland: agent graph, `injectTransferTools()`, function-call routing, state-machine prompting.
- Voice model: `gpt-realtime-2`.

**Realtime API does NOT ship swarm primitives natively** — it ships streaming + session events. Swarm is implemented on top with the same Agents SDK primitives. No additional pattern to lift for Aura.

---

## 4. Anthropic 2026 multi-agent — brief

From claude.com/blog/building-agents-with-the-claude-agent-sdk + Augment Code + paddo.dev/claude-code-hidden-swarm:

- **Claude Agent SDK** (alongside Claude 4.6, Feb 2026): supports subagents primarily for (a) parallelization, (b) context isolation. "subagents use their own isolated context windows, and only send relevant information back to the orchestrator."
- **TeammateTool** (Claude Code v2.1.32+, behind `CLAUDE_CODE_EXPERIMENTAL_AGENT_TEAMS`): in-binary multi-agent orchestration. Patterns: **Leader, Swarm, Pipeline, Watchdog**. Disabled by default; not yet a public SDK primitive.
- **Communication**: tool-based + filesystem (shared storage). No explicit shared in-memory state object documented.
- **Same shape as Kimi/OpenAI**: tree of subagents-as-tools, no peer mesh in shipped product.

**For Aura**: nothing new to lift beyond what Kimi/OpenAI already give us. The TeammateTool pattern names (Leader/Swarm/Pipeline/Watchdog) are useful taxonomy for OH3+ planning but the implementation is closed.

---

## 5. Comparison vs Aura's current substrate (OH1)

### 5.1 Aura OH1 today

Per the conversation context: `internal/agent/agentdef` registry with sync hierarchical tree, depth ≤3, Chat→Worker delegate synth. Files touched: `validator.go`, `cycle.go`. No parallel dispatch yet.

### 5.2 Mapping Kimi primitives → Go on top of OH1

| Kimi primitive | Aura port | Effort |
|---|---|---|
| `create_subagent(name, sys_prompt)` tool | New tool in `internal/agent/tools/`, writes a transient agentdef into the registry (or a per-turn ephemeral table). Existing `agentdef` registry from OH1 is the storage. | **Small (~150 LOC)**. Schema is 2 fields. |
| `assign_task(agent, prompt)` tool | New tool that invokes `Runner.run(agentdef, prompt)` synchronously and returns the final assistant message. Internals = OH1's existing delegate executor. | **Small (~100 LOC)**. The hard part is already shipped. |
| Parallel fan-out across multiple `assign_task` calls in one turn | Aura already executes "independent tool calls in the same turn ... in parallel" per CLAUDE.md (agent loop section). Verify that parallelism applies to subagent-spawning tools too. | **Verification, possibly small wiring**. The parallel-tool-call path in `internal/chat/agentloop.go` likely already covers this; check that `assign_task` doesn't accidentally serialize via a shared lock. |
| Result aggregation = "one message back" | Each parallel `assign_task` returns one tool_result. Orchestrator's next LLM round sees all results in the message list (standard OpenAI tool_result protocol). | **Free**. This is just OpenAI tool-result semantics. |
| Step limits per subagent | OH1's existing per-turn `MaxIterations`/`MaxDepth` already cap this. Need a per-subagent override. | **Tiny config plumbing**. |
| Critical Steps metric | New telemetry in `internal/agent/metrics` (or wherever current loop stats live). Compute `orch_steps + max(sub_steps)` at run end; log + expose in dashboard. | **Small**. Adds ~50 LOC + a probe assertion in `cmd/probe_chat`. |
| PARL reward shaping | **N/A** — Aura has no RL training loop. Skip. Use prompt-level nudges instead ("when subtasks are independent, prefer parallel"). | **Zero code; just prompt**. |
| Shared blackboard | **NOT in Kimi v1**. Skip for OH3 v1. If we want it: model after OpenAI Agents SDK's `RunContextWrapper.context` — typed Go struct passed by ref through the call tree, with mutex. | **Future work (OH4+)**. |
| Peer-to-peer subagent chat | **NOT in Kimi v1.** Skip. Tree, not mesh. | **Out of scope for OH3.** |
| Open-weight model | N/A — Aura is model-agnostic via OpenAI-compatible HTTP. K2.5/K2.6 work if user wires `LLM_BASE_URL` to a hosted endpoint. | **Already supported**. |

### 5.3 Patterns that port cleanly

- **Two-tool spawn API** (`create_subagent` + `assign_task`). Directly add as two new entries in `internal/agent/tools/registry`. The K2.5 schemas above are usable as-is.
- **Synchronous "one message back" result contract**. Matches OpenAI tool_result. No new protocol.
- **Hierarchical tree, isolated subagent contexts**. Aura OH1 already does this; OH3 just adds the *parallel* axis.
- **Step-limit budgets per spawn**. Trivial config.
- **Critical Steps observability**. Cheap and high-value.

### 5.4 Patterns that need rethinking for Go

- **PARL training loop**: not portable; Aura has no RL infra. The replacement is a deterministic prompt-level "spawn when X" instruction + a static heuristic ("≥2 independent subtasks of ≥3 tool calls each").
- **Dynamic system prompts at runtime**: Kimi generates subagent system_prompts on the fly. Aura's agentdef registry is currently static (loaded at boot from `agentdef/*.toml`-ish files per OH1 commit messages). For OH3 we need an **ephemeral agentdef** path — register-and-forget within a single turn, not persisted to disk. New code: `internal/agent/agentdef/ephemeral.go` (~100 LOC, in-memory map keyed by turn_id).
- **300 subagents in flight**: physically infeasible on Aura's mini-PC budget (CPU ≤4 threads per process per `feedback_minipc_cpu_budget`). Realistic cap for OH3 v1: **8 parallel subagents**, hard-capped at OS level via a semaphore. Document this as a deliberate downscale, not a bug.
- **Python-only SDKs** (OpenAI Agents Python, third-party PARL): we lift patterns and tool schemas, not code.

### 5.5 Suggested OH3 v1 minimal slice

Three commits, each independently testable:

1. **OH3-A — Ephemeral agentdef** (`internal/agent/agentdef/ephemeral.go` + `create_subagent` tool): orchestrator can declare a transient archetype within a turn. Probe: log the registered ephemeral defs per turn.
2. **OH3-B — `assign_task` tool + parallel dispatch** (`internal/agent/tools/assign_task.go`): orchestrator dispatches one or many. Probe: a query that explicitly says "use 3 parallel research agents" — assert `critical_steps < total_tool_calls`.
3. **OH3-C — Critical-Steps metric + cost cap** (`internal/agent/metrics/critical_steps.go` + per-turn token ceiling): logged in archive, exposed in dashboard. Probe: assert that the metric appears in `conversations` table and dashboard renders it.

OH4+ (deferred): blackboard, peer chat, RL-style reward, persistent ephemeral defs across turns.

---

## 6. Open questions for other scouts / follow-up

1. **Has any other scout found an open-source implementation of `create_subagent`/`assign_task` as a server-side runtime?** PARL repo is training-only. We need to know if anyone (cli-printing-press? openhuman? D:/tmp candidates?) has shipped the runtime piece.
2. **What does Aura's current parallel-tool-call code path look like in `internal/chat/agentloop.go`?** CLAUDE.md claims "Independent tool calls in the same turn execute in parallel" — does that path already cover the case where one of those tool calls is itself an `assign_task` that spawns a subagent loop? Scout focused on the agent loop should confirm.
3. **What's the cost ceiling per turn we're willing to spend on a swarm?** K2.5 BrowseComp at 15 orch steps + 100 subagents × 100 sub-steps = 10,015 tool calls. Aura needs a hard `MAX_SUBAGENTS_PER_TURN` and `MAX_TOOL_CALLS_PER_TURN`. Suggested defaults: 8 and 50 respectively, given mini-PC constraints.
4. **Should subagents be allowed to use ALL tools, or a restricted subset?** Kimi paper: full tool access, system prompt restricts. OpenAI Agents SDK: `Agent.as_tool` lets the manager pre-restrict via the wrapped agent's own tool list. Aura's choice affects safety: if a `summarize_doc` subagent can call `execute_shell`, that's a privilege escalation surface.
5. **Does Aura want the OpenAI "agents-as-tools" shape (manager keeps control, gets result) or the Kimi "spawn + parallel + merge" shape (manager dispatches, awaits all)?** Functionally equivalent at the LLM API layer — both are tool_calls with tool_results. Aura should pick ONE pattern and stick to it in the system prompt for consistency. Recommend Kimi shape (more general; degenerates to OpenAI shape when N=1).
6. **TeammateTool's Leader/Swarm/Pipeline/Watchdog taxonomy** — should Aura adopt these as named patterns in code? Useful for documentation and reasoning, but adds complexity. Probably YES for the docs, NO for code structure (one substrate, four usage patterns).

---

## Sources

### Kimi K2.5 / K2.6 / PARL
- Local paper: `D:/tmp/aura-agent-loop-papers/2602.02276-Kimi-K2.5.txt` (canonical, all architecture detail above)
- https://www.kimi.com/blog/agent-swarm — K2.5 announcement
- https://www.kimi.com/blog/kimi-k2-6 — K2.6 deltas
- https://www.kimi.com/help/agent/agent-swarm — K2.6 Beta access / tier gating
- https://arxiv.org/html/2602.02276v1 — paper HTML mirror
- https://huggingface.co/moonshotai/Kimi-K2.5 — open weights
- https://huggingface.co/moonshotai/Kimi-K2.6 — open weights
- https://www.infoq.com/news/2026/02/kimi-k25-swarm/ — InfoQ writeup
- https://www.datacamp.com/tutorial/kimi-k2-agent-swarm-guide — user-facing tutorial
- https://the-decoder.com/moonshot-ai-releases-kimi-k2-5-claims-most-powerful-open-weight-model-with-100-agent-coordination/ — release recap
- https://venturebeat.com/orchestration/moonshot-ai-debuts-kimi-k2-5-most-powerful-open-source-llm-beating-opus-4-5 — drift/stability commentary
- https://github.com/The-Swarm-Corporation/PARL — 3rd-party reference impl (training only)
- https://kimik2ai.com/pricing/ — pricing tiers
- https://openrouter.ai/moonshotai/kimi-k2.5 + .6 — API token costs
- https://lushbinary.com/blog/kimi-k2-6-developer-guide-benchmarks-api-agent-swarm/ — independent dev guide
- https://avenchat.com/blog/kimi-k2-6-review — review w/ cost notes
- https://nerdleveltech.com/kimi-k2-6-300-agent-swarm-open-weight-frontier-coding — BrowseComp 86.3 number

### OpenAI Agents SDK / Swarm / Realtime
- https://github.com/openai/openai-agents-python — repo (v0.17.3, May 2026)
- https://openai.github.io/openai-agents-python/ — docs root
- https://openai.github.io/openai-agents-python/multi_agent/ — orchestration patterns
- https://openai.github.io/openai-agents-python/handoffs/ — handoff semantics
- https://openai.github.io/openai-agents-python/context/ — Context vs Session
- https://openai.github.io/openai-agents-python/sessions/ — Session backends
- https://github.com/openai/openai-realtime-agents — Realtime + handoffs ref impl
- https://github.com/openai/swarm — deprecated educational framework
- https://www.respan.ai/articles/openai-agents-sdk-vs-swarm — migration guide
- https://callsphere.ai/blog/openai-agents-sdk-2026-multi-agent-systems-handoffs-guardrails — 2026 overview
- https://developers.openai.com/cookbook/examples/agents_sdk/multi-agent-portfolio-collaboration/multi_agent_portfolio_collaboration — multi-agent example

### Anthropic
- https://claude.com/blog/building-agents-with-the-claude-agent-sdk — official SDK overview
- https://paddo.dev/blog/claude-code-hidden-swarm/ — TeammateTool reverse-engineering
- https://www.augmentcode.com/learn/ruflo-claude-code-multi-agent-orchestration — Ruflo / community orchestrators
- https://zylos.ai/research/2026-04-20-claude-agent-sdk-managed-agents-architecture — Anthropic Q2 2026 infrastructure analysis
- https://gist.github.com/kieranklaassen/4f2aba89594a4aea4ad64d753984b2ea — TeammateTool patterns guide (community)

### Comparative
- https://gurusup.com/blog/best-multi-agent-frameworks-2026 — framework landscape
- https://www.morphllm.com/ai-agent-framework — framework trade-offs
- https://till-freitag.com/en/blog/agent-swarm-architectures-compared — direct K2.5 vs OpenAI vs Anthropic comparison
