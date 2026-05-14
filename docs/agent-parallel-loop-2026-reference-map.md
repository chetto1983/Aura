# Agent Parallel Loop 2026 Reference Map

Last updated: 2026-05-14

Purpose: keep the sources behind Aura's swarm/parallel-agent decision easy to find during the deep refactor. This map supports ADR-033, ADR-035, and BG-009.

## Core Findings

The 2026 literature is not converging on "spawn a few fixed workers". It is converging on topology-aware orchestration: a parent run dynamically creates bounded child executors, chooses when to parallelize, controls what context and tools each child receives, aggregates structured outputs, and records orchestration traces so the policy can improve over time.

Claude Code's Agent Teams documentation adds an important product pattern:
subagents report only to the caller, while agent teams coordinate through a
shared task list and direct teammate messaging. Aura needs both modes. Simple
delegation is right when only the result matters; team collaboration is right
when agents must share findings, challenge each other, claim work, and converge.

For Aura, the important design move is to make swarm a flexible run graph with strict authority and traceability. A conservative MVP is fine, but the core model must not permanently collapse to one hardcoded, read-only fanout tool.

## Paper Map

| Source | What It Shows | Aura Use |
| --- | --- | --- |
| [Kimi K2.5: Visual Agentic Intelligence](https://arxiv.org/abs/2602.02276), submitted 2026-02-02 | Agent Swarm dynamically decomposes tasks into heterogeneous sub-problems and runs them concurrently. PARL uses a trainable orchestrator with frozen subagents, rewards both parallel exploration and subagent finish rate, measures critical steps by the longest parallel branch, and treats Agent Swarm as proactive context sharding. Appendix E.8 exposes `create_subagent` and `assign_task` tools plus explicit orchestrator/subagent step budgets. | Keep parent/child runs, bounded child contexts, structured child returns, critical-path metrics, and dynamic subagent creation as first-class concepts. Do not import PARL training into Aura core. |
| [AOrchestra: Automating Sub-Agent Creation for Agentic Orchestration](https://arxiv.org/abs/2602.03786), revised 2026-02-07 | Static roles and context-only subagents are too rigid. The paper models each agent as `(Instruction, Context, Tools, Model)` and has an orchestrator use `Delegate` and `Finish`; delegated executors return structured observations with summaries, artifacts, and errors. | Model child agents as parameterized NodeSpecs, not fixed Go roles. Tool/model/context grants are part of the child contract. |
| [AdaptOrch: Task-Adaptive Multi-Agent Orchestration](https://arxiv.org/abs/2602.16873), submitted 2026-02-18 | Orchestration topology is a first-class optimization target. Tasks decompose into dependency DAGs; DAG width, critical path depth, and coupling route to parallel, sequential, hierarchical, or hybrid execution. Synthesis uses consistency scoring and rerouting when parallel outputs conflict. | Represent swarm work as a run graph/DAG and allow multiple topology policies. Add explicit aggregation and conflict/escalation contracts. |
| [Reinforcement Learning for LLM-based Multi-Agent Systems through Orchestration Traces](https://arxiv.org/abs/2605.02801), submitted 2026-05-04 | RL for multi-agent systems must optimize spawn, delegate, communicate, aggregate, and stop. It proposes orchestration traces as temporal event graphs and names metrics such as parallelism efficiency, useful-agent utilization, protocol overhead, message redundancy, and error amplification. It also notes no explicit RL stopping method in its curated pool as of 2026-05-04. | Persist replayable orchestration traces before trying RL-like optimization. Treat stop policy as a product risk, not an afterthought. |
| [Swarm Skills](https://arxiv.org/abs/2605.10052), submitted 2026-05-11 | Multi-agent coordination can be packaged as portable assets with roles, workflow, execution bounds, dependencies, and evolution records. It scores patches by Effectiveness, Utilization, and Freshness, with simplify, rebuild, and rollback governance. | Store reusable coordination patterns as skills or skill-like assets, but keep Aura's validation/review gates. Do not let a bad first workflow silently mutate core behavior. |
| [Lemon Agent Technical Report](https://arxiv.org/abs/2602.07092), submitted 2026-02-06 | Uses a multi-agent orchestrator-worker system with hierarchical self-adaptive scheduling, macro worker allocation, micro tool parallelization, three-tier context management, and self-evolving semantic memory. | Separate macro swarm topology from child-local tool parallelism. Keep context compression, memory, and scheduling as layered concerns. |
| [Web2BigTable](https://arxiv.org/abs/2604.27221), submitted 2026-04-29 | Uses a bi-level architecture: upper orchestrator decomposes, lower workers execute in parallel, shared Markdown workboard with locks/tag partitions coordinates partial findings, and run-verify-reflect updates human-readable external memory. | Consider a shared workspace/artifact plane for parallel research, not peer-to-peer chat as the default. External memory should improve decomposition and execution over time. |
| [SkillX](https://arxiv.org/abs/2604.04804), revised 2026-04-19 | Distills experience into hierarchical planning, functional, and atomic skills; refines skills from execution feedback and expands coverage. | Aura's operational memory should distill traces into reusable coordination and tool-use patterns, not dump raw transcripts into context. |
| [Signals: Trajectory Sampling and Triage for Agentic Interactions](https://arxiv.org/abs/2604.00356), submitted 2026-04-01 | Cheap, model-free trajectory signals can triage which agent traces are informative for post-deployment improvement. Signals are metadata, not quality scores. | Add low-cost trace signals for loop detection, failure, stagnation, satisfaction, exhaustion, and redundant work before expensive evaluation. |

## Agent Team Reference

| Source | What It Shows | Aura Use |
| --- | --- | --- |
| [Claude Code Agent Teams](https://code.claude.com/docs/en/agent-teams) | Agent teams coordinate multiple independent sessions through a team lead, shared task list, mailbox, direct teammate messaging, task assignment/claiming, dependencies, file locking, plan approval, and hooks. The docs distinguish this from subagents, which only report back to the main agent. | Add a `team_collaboration` topology to Aura's RunGraph. Model `SharedTaskBoard` and `Mailbox` as durable coordination primitives. Agents may message each other by name and claim tasks, but every message, claim, dependency, plan approval, shutdown, and hook decision is a traceable event. |

## Additional Online Examples

| Source | What It Shows | Aura Use |
| --- | --- | --- |
| [AutoGen AgentChat teams](https://microsoft.github.io/autogen/stable/reference/python/autogen_agentchat.teams.html) | `RoundRobinGroupChat` publishes each participant's messages to the rest of the group; `SelectorGroupChat` uses a model to choose the next speaker; group chats require unique participant names, termination conditions, max-turn limits, and custom message/event types. | Adopt speaker policy as an explicit team topology setting. Every Aura team needs unique teammate names, max turns/messages, termination policy, and typed message/event records. Reject unbounded group chat. |
| [LangGraph handoffs](https://docs.langchain.com/oss/python/langchain/multi-agent/handoffs) | Agents are graph nodes and transfer control through handoff tools. The docs warn that passing full subagent history can confuse the receiver and bloat context, recommending focused handoff messages or summaries. | Adopt explicit handoff events and context filters. Aura teammate messages should carry scoped summaries/artifact handles, not raw full transcripts. |
| [LangChain router pattern](https://docs.langchain.com/oss/python/langchain/multi-agent/router) | Router handles lightweight deterministic classification; supervisor/subagent orchestration is for flexible multi-turn work. | Keep `router`, `delegation`, and `team_collaboration` separate in Aura. Do not use a team when a router or single worker is enough. |
| [OpenAI Agents SDK handoffs](https://openai.github.io/openai-agents-python/handoffs/) | Handoffs are tool calls; each destination is registered explicitly; handoff metadata can be schema-validated; input filters control what history the receiving agent sees. | Adopt schema-bound handoff metadata such as reason, priority, summary, and risk. Add per-edge context filters to RunGraph handoffs. |
| [OpenAI Agents SDK overview](https://developers.openai.com/api/docs/guides/agents) | The SDK is framed for applications that own orchestration, tool execution, state, approvals, traces, and evaluations. | Confirms Aura should own the local runtime and use SDK/framework ideas as patterns, not outsource core state. |
| [Google ADK multi-agent systems](https://adk.dev/agents/multi-agents/) | ADK composes parent/child agents, workflow agents, shared session state, LLM-driven transfer, explicit invocation, parallel fan-out/gather, generator-critic, and iterative refinement. It warns to use distinct shared-state keys to avoid races. | Adopt typed shared state slots and explicit write ownership. SharedTaskBoard is not a free scratchpad; task outputs and intermediate state need owned keys/artifact refs. |
| [Agent2Agent Task concept](https://agent2agent.info/docs/concepts/task/) | A2A defines stateful Tasks with status, message history, artifacts, and delegation/request-more-information behaviors. | Adopt the Task/Message/Artifact split for external interoperability and internal vocabulary. A2A is not enough for Aura's internal team runtime because it does not define shared task-board claiming or local authority. |
| [A2A core protocol guidance](https://agent2agent.info/specification/core/) | Recommends building around `Task` as the durable unit, treating `Message` as interaction payload and `Artifact` as output payload, with version governance and strict schema validation. | Reinforces Aura's durable task/message/artifact separation and schema-versioned coordination events. |
| [CrewAI crews](https://docs.crewai.com/en/concepts/crews) | Crews group agents, tasks, process flow, planning LLM, knowledge sources, checkpointing, and hierarchical manager coordination. | Adopt declarative role/task specs and checkpointing ideas. Reject opaque black-box crew execution as Aura's source of truth; Aura's run/events remain canonical. |
| [OpenAI Swarm](https://github.com/openai/swarm) | Educational framework using agents and handoffs; now replaced by the production Agents SDK. It keeps state client-side and does not store state between calls. | Keep handoff/routine simplicity as a pattern. Reject statelessness for Aura; team task and mailbox state must be durable. |
| [mcp-agent workflows](https://docs.mcp-agent.com/workflows/overview) | Provides composable parallel, router, evaluator-optimizer, orchestrator, and swarm patterns. | Adopt composable pattern vocabulary so Aura can combine team collaboration with evaluator/optimizer or router slices. |
| [mcp-agent deployment guide](https://docs.mcp-agent.com/cloud/use-cases/deploy-agents) | Production guidance emphasizes durability, retries, human input, workflow history, structured logs, OTEL, and artifact references for large payloads. | Reinforces ADR-034: durable coordination and artifact references beat dumping large payloads into logs or traces. |
| [Anthropic Building Effective Agents](https://www.anthropic.com/engineering/building-effective-agents) | Successful systems often use simple composable patterns: routing, parallelization, orchestrator-workers, and evaluator-optimizer. Orchestrator-workers are useful when subtasks cannot be predicted ahead of time. | Use agent teams only when coordination value beats overhead. Prefer simple workflows first, then dynamic team topology when task shape justifies it. |
| [Anthropic multi-agent research system](https://www.anthropic.com/engineering/built-multi-agent-research-system) | Lead agent coordinates 3-5 parallel subagents, subagents can use tools in parallel, and poor division of labor causes duplicate work. | Add duplicate-work detection, task-claiming discipline, and useful-agent utilization to team evals. |
| [OpenCode agents](https://dev.opencode.ai/docs/agents/) | OpenCode separates primary agents from subagents, ships build/plan/general/explore/scout/compaction/summary modes, and scopes actions through per-agent permissions. Task permission rules can remove denied subagents from the model-visible Task tool. | Adopt explicit mode, permission, and task-invocation policy in Aura agent specs. A model should only see teammate/subagent tools it is allowed to call; hidden/system agents such as compaction or summary are support services, not product memory. |
| [Gemini CLI subagents](https://github.com/google-gemini/gemini-cli/blob/main/docs/core/subagents.md) | Gemini subagents have focused prompts, specialized tool access, independent context loops, automatic or explicit invocation, isolated MCP servers, and subagent-specific policy rules. | Treat subagent/team member definitions as callable tools with descriptions, scoped context, isolated tools/MCP servers, max turns/time, and policy bindings. The invocation description is part of the routing contract. |
| [Gemini CLI extensions](https://google-gemini.github.io/gemini-cli/docs/extensions/) | Extensions package commands, prompts, MCP servers, and tool exclusions as installable assets with enable/disable/update lifecycle. | Package Aura coordination patterns, skills, and tool bundles as versioned assets, but keep runtime state, tasks, mailbox, and memory authority inside Aura. |
| [OpenAI Codex agent loop](https://openai.com/index/unrolling-the-codex-agent-loop/) | Codex builds each turn from tool schemas, sandbox/developer instructions, AGENTS files, skill metadata, and environment context before sampling the model and executing tool calls. | Keep Aura's loop explicit and reconstructable: inputs, tools, policies, context sources, and environment must be recorded as run metadata. AGENTS/skills are durable instruction sources, not an implicit hidden brain. |

## Local Evidence

| Path | Observation | Aura Use |
| --- | --- | --- |
| `D:/Aura/internal/swarm/types.go` | Current swarm has `Run`, `Task`, and `Assignment`, but the topology is essentially flat fanout. | Keep the useful persistence primitives, but evolve toward run graph nodes/edges. |
| `D:/Aura/internal/swarm/manager.go` | Manager runs assignments with a semaphore and depth cap, calling a runner directly. | Replace "manager owns fanout" with "supervisor executes a planned run graph through the normal run boundary". |
| `D:/Aura/internal/agent/tools/swarm/tools.go` | `run_aurabot_swarm` is currently wait-only and read-only, with roles constrained by policy. | Good first safety slice, not the final architecture ceiling. |
| `D:/Aura/.planning/wave3-agent-swarm/plan.md` | Wave 3 correctly adds phases, role parameter restoration, skill-driven routing, and proposal-only worker writes. It intentionally forbids recursion for that wave and targets older runner/tool paths. | Treat Wave 3 as evidence, not an executable queue. ADR-036 requires re-authoring it against Phase 8 RunGraph/team-collaboration before implementation. |
| `D:/tmp/hermes-agent/AGENTS.md` | Hermes exposes delegated tasks with `max_concurrent_children`, `max_spawn_depth`, role `leaf` vs `orchestrator`, and optional nested orchestration. | Adopt explicit spawn depth, child concurrency, and role capability classes. Do not copy non-durable delegation as-is. |
| `D:/tmp/nanobot/nanobot/agent/subagent.py` | Nanobot tracks background subagent phase, iteration, tool events, usage, stop reason, and errors. | Adopt phase/status visibility and session-scoped child lifecycle. |
| `D:/tmp/nanobot/nanobot/agent/tools/spawn.py` | Spawn tool refuses when the concurrency limit is reached and returns a visible status message. | Concurrency limits must be user-visible and model-visible, not silent clamps. |

## Adopted Architecture Direction

Aura swarm should become a policy-driven `RunGraph`:

- `SwarmRun`: parent run plus graph metadata, status, budgets, and trace IDs.
- `NodeSpec`: role/name, goal, instruction, curated context capsule, toolset/capability grant, model/provider, budgets, max iterations, output schema, artifact policy, risk tier, and allowed spawn depth.
- `EdgeSpec`: dependency, artifact-consumption, aggregation, critic/review, reroute, or cancellation edge.
- `Supervisor`: executes the graph, watches budgets, persists events, enforces capability grants, and routes child work through the same run/event/workflow contracts as chat and cron.
- `SharedTaskBoard`: optional team coordination surface with task state,
  dependencies, assignment, claim locks, plan-approval state, and completion
  hooks.
- `Mailbox`: optional durable addressed messaging between team members. Messages
  can be teammate-to-teammate or lead-to-teammate; broadcast is represented as
  one message per recipient.
- `AggregationContract`: defines how child outputs are merged, cited, verified, escalated, or rejected.
- `OrchestrationTrace`: append-only event graph for spawn, delegate,
  task-create, task-claim, task-complete, message/workspace update, tool call,
  return, aggregate, plan approval, stop, retry, cancellation, and budget
  events.

## Policy Tiers

Aura should support these tiers as architecture, even if early implementation enables only the first few:

- `direct`: no swarm; one loop handles the task.
- `fanout_read`: read-only parallel workers, bounded output, parent aggregates.
- `team_collaboration`: lead plus named teammates coordinate through a shared
  task board and mailbox, useful for research debates, competing hypotheses,
  cross-layer work, and review where agents must challenge each other.
- `plan_execute`: planner creates graph, executors return structured outputs.
- `critic_review`: executor plus critic/verifier path for risky synthesis.
- `artifact_build`: child creates or updates artifacts through proposal/workflow gates.
- `repair_loop`: child proposes fix, verifier checks, parent applies through normal authority.
- `hierarchical`: bounded recursive planning with explicit max depth, only for tasks whose graph justifies it.
- `hybrid`: layered DAG execution, parallel inside layers and sequential across dependent stages.

## Non-Lobotomy Requirements

- Do not make fixed hardcoded roles the final model.
- Do not make read-only-only workers the final model.
- Do not make `max_spawn_depth=1` a permanent architectural invariant.
- Do not dump child transcripts into parent context.
- Do not let child agents write durable state directly.
- Do not use unbounded peer-to-peer free chat as the core default. Teammate
  messaging must be addressed, scoped, durable, and traceable.
- Do not collapse team collaboration back into lead-only aggregation when agents
  need to share findings, challenge each other, or claim dependent work.
- Do not optimize for number of agents. Optimize for critical path, quality, useful-agent utilization, cost, and trace debuggability.
- Do not attempt RL or self-evolution before traces, evals, rollback, and review gates exist.

## Evaluation Hooks

Minimum swarm evaluation needs:

- final task success and citation/source faithfulness,
- critical-path wall time,
- total token/tool/cost budget,
- parallelism efficiency,
- useful-agent utilization,
- protocol overhead,
- duplicate/redundant work rate,
- error amplification,
- context returned to parent,
- stopped-too-early and stopped-too-late cases,
- aggregation conflict rate,
- teammate message overhead and useful-message ratio,
- task claim conflicts and blocked-task latency,
- plan approval reject/revise loops,
- child-output injection/safety failures,
- trace replay and trace triage signals.
