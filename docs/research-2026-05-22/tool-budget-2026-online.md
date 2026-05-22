# Tool-Call Budget Enforcement in AI Agents — 2026 State of Art

**Compiled:** 2026-05-22
**Scope:** Web research synthesis. NOT Aura-specific. Surfaces 2026 production-validated patterns.
**Trigger:** Aura thrashed 28 LLM calls / 33 tool calls / 180s on a single user query because a system-prompt rule was not enforced in code. Need code-level enforcement.

---

## 1. Anthropic Claude Agent SDK / Claude Code

**Source:** Anthropic official docs — `https://code.claude.com/docs/en/agent-sdk/agent-loop`

### What's enforced

| Option (Python / TS) | What it controls | Default |
|---|---|---|
| `max_turns` / `maxTurns` | Maximum tool-use round trips per session | **No limit** (unlimited) |
| `max_budget_usd` / `maxBudgetUsd` | Maximum USD cost before stopping | **No limit** |
| `effort` (`low`/`medium`/`high`/`xhigh`/`max`) | Reasoning depth per turn → token cost | model default; TS SDK = `high` |

Both `max_turns` and `max_budget_usd` default to **unlimited**. When either limit is hit, the SDK terminates the loop and returns a `ResultMessage` with subtype `error_max_turns` or `error_max_budget_usd`. The `result` field is absent on error subtypes — the user gets a structured error, not a synthesized answer. This is a **strict / fail-hard** policy by design.

### Recommended production config (Anthropic example)

```python
ClaudeAgentOptions(
    allowed_tools=["Read","Edit","Bash","Glob","Grep"],
    max_turns=30,          # "Prevent runaway sessions"
    effort="high",
)
```

The docs explicitly note: *"Without limits, the loop runs until Claude finishes on its own, which is fine for well-scoped tasks but can run long on open-ended prompts. **Setting a budget is a good default for production agents.**"*

### tool_choice: none

`tool_choice = {"type": "none"}` forces Claude to answer with text and no tool calls. Compatible with extended thinking. This is the documented hook for force-finalize patterns: strip tools, force a synthesis turn.

> Sources:
> - Claude Agent SDK loop docs — `https://code.claude.com/docs/en/agent-sdk/agent-loop`
> - Claude tool_choice docs — `https://docs.anthropic.com/en/docs/agents-and-tools/tool-use/implement-tool-use`
> - tool_choice cookbook — `https://github.com/anthropics/anthropic-cookbook/blob/main/tool_use/tool_choice.ipynb`

### "Writing tools for agents" / advanced tool use

The Anthropic `advanced-tool-use` post and the `effective-context-engineering-for-ai-agents` post do NOT prescribe per-tool budgets or iteration caps. They focus on **tool design** (minimum viable tool set, response_format enums, ≤25k token returns, search-not-list) so the agent has fewer reasons to thrash. Anthropic's stated position: design tools so the agent doesn't need to loop, then add `max_turns` as a safety rail. There is no "per-tool budget" feature in the official SDK.

> Sources:
> - Advanced tool use — `https://www.anthropic.com/engineering/advanced-tool-use`
> - Effective context engineering — `https://www.anthropic.com/engineering/effective-context-engineering-for-ai-agents`

---

## 2. OpenAI

### Assistants API (legacy v2)

| Parameter | Behavior |
|---|---|
| `max_prompt_tokens` | Truncates thread to this many tokens before each completion |
| `max_completion_tokens` | Caps total output tokens across all completions in the Run |
| `truncation_strategy` | `auto` or `last_messages` |

When `max_completion_tokens` is reached the Run terminates with status `incomplete` and details in `incomplete_details`. **No per-tool-call counter exists** — budget is purely token-based across the full Run lifecycle. An Assistant can have up to 128 tools attached.

> Source: `https://platform.openai.com/docs/assistants/deep-dive`

### Agents SDK (openai-agents-python)

- `max_turns` parameter on `Runner.run()` / `run_sync()` / `run_streamed()` — **default = 10** (`DEFAULT_MAX_TURNS`).
- Exceeding raises `MaxTurnsExceeded`. Pass `max_turns=None` to disable.
- Error handler keys: `"max_turns"`, `"model_refusal"` — let you return a controlled final output instead of raising.
- `as_tool(max_turns=...)` allows nested agents to have their own cap.
- `tool_choice="none"` available on the Responses API to force a final user-facing message without tool calls.

> Sources:
> - `https://openai.github.io/openai-agents-python/running_agents/`
> - `https://openai.github.io/openai-agents-python/ref/run/`
> - `https://github.com/openai/openai-agents-python/issues/844`

---

## 3. LangChain / LangGraph / DeepAgents

**Sources:**
- `https://docs.langchain.com/oss/python/langgraph/errors/GRAPH_RECURSION_LIMIT`
- `https://forum.langchain.com/t/how-to-cap-tool-and-sub-agent-calls-in-deepagents/1653`
- `https://github.com/langchain-ai/langgraph/issues/6731`

### Core mechanism: recursion_limit

- LangGraph default `recursion_limit` = **25 steps**.
- Recommended production range: **15–50**.
- Configured via `RunnableConfig`: `config={"recursion_limit": 100}`.
- Exceeding triggers `GRAPH_RECURSION_LIMIT` error.

### LangChain official guidance for DeepAgents (from forum reply by Pawel Twardziak)

1. **Global recursion_limit** — primary control.
2. **Explicit state counters** — `tool_calls`, `subagent_calls` fields on state, conditional edges that terminate when thresholds reached. "Belt-and-suspenders."
3. **Sub-agent local caps** — lower `recursion_limit` on child invocations, parent increments counter before delegation.

LangChain explicitly says: *"No per-tool budget mechanism is built-in; controls operate at the execution level rather than individual tool granularity."*

### Critical guidance

> *"Every cycle must rely on a strictly monotonic condition to continue — something in your state object must measurably increase (like a steps counter) or strictly decrease (like remaining token budget) with every single turn."*

> *"The most common reason for hitting a recursion limit isn't that your task is too complex — it's that your agent is stuck. The long-term fix is to improve your tool's error messages so the agent can reason its way out of the loop."*

---

## 4. AWS Strands Agents (Bedrock)

**Sources:**
- `https://aws.amazon.com/blogs/machine-learning/strands-agents-sdk-a-technical-deep-dive-into-agent-architectures-and-observability/`
- `https://strandsagents.com/docs/user-guide/concepts/agents/agent-loop/`
- `https://aws.amazon.com/blogs/opensource/introducing-strands-agents-1-0-production-ready-multi-agent-orchestration-made-simple/`

### Production config pattern recommended by AWS

```python
agent = Agent(
    model="claude-3-sonnet",
    tools=[...],
    max_iterations=5,        # Hard stop after 5 tool calls
    max_execution_time=30,   # 30-second wall-clock timeout
    token_limit=4000,        # Budget per request
)
```

- `MaxTokensReachedException` raised when model output truncated.
- AWS doctrine: *"Set conservative limits initially. You can always relax them based on actual usage patterns. Starting permissive and tightening later requires explaining cost spikes to stakeholders."*
- Observability emphasis: *"When max_iterations is consistently hit, either your tools aren't providing enough information per call, or your task complexity exceeds a single agent's capability."*

Strands' baseline `max_iterations` examples skew **very low (5)** compared to LangGraph (25) and OpenAI Agents (10).

---

## 5. Google ADK (Agent Development Kit)

**Sources:**
- `https://google.github.io/adk-docs/`
- `https://google.github.io/adk-docs/agents/workflow-agents/loop-agents/`

- `LoopAgent` has explicit `max_iterations` / `MaxIterations()` parameter.
- Example values across docs: **3, 5**.
- Loop terminates on `max_iterations` OR explicit `exit_loop` tool call by a sub-agent.
- **No global tool budget** across all agents; budgets are scoped per `LoopAgent`.

ADK is the most aggressive about low default loops (3-5) and treats the iteration cap as a "safety net" rather than the primary control.

---

## 6. Production Failure Case Studies (H1 2026)

### The $47K LangChain ping-pong — gabrielanhaia / DEV.to

**Source:** `https://dev.to/gabrielanhaia/the-agent-that-spent-47k-on-itself-an-autonomous-loop-postmortem-3313`

- 2 LangChain agents (analyzer + verifier) entered a clarification ping-pong.
- **11 days**, **$47,000** total spend.
- Cost progression: Week 1 $127 → Week 2 $891 → Week 3 $6,240 → Week 4 $18,400.
- Root cause: *"No step cap. No per-conversation USD budget. No orchestrator deciding when the work was done."*
- Fix: **3 defenses — step cap, budget gate, loop detector**. *"You only need one of those four to catch a loop on iteration three. The team had zero."*
- Loop detector threshold: **input hash duplicate detection triggering at 2**.

### The $4,200 autonomous refactor — Sattyam Jain / Medium

**Source:** `https://medium.com/@sattyamjain96/the-agent-that-burned-4-200-in-63-hours-a-production-ai-postmortem-d38fd9586a85`

- Single agent retrying a rate-limited API endpoint.
- **63 hours** (Fri 22:14 → Mon AM), **$4,200**, **~300,000 tool calls** (~4,800/hour).
- Cost progression: hour 1 = $42 → hour 4 = $200 → hour 12 = $1,000.
- Missing guards: no financial ceiling, no wall-clock timeout, no recursion depth limit, no retry backoff, no boundary validation, no observability.
- 4 production fixes: tool-call firewall + budget guards (dollar/token/time/recursion) + attributed observability + deny-by-default tool access.

### Magicrails 14,000 list_files calls

Documented in multiple roundups (e.g. The Operator Collective). Same pattern: identical tool + identical args, no dedup.

### nanobot tool-loop cost analysis

**Source:** `https://github.com/HKUDS/nanobot/issues/2318`

- Identified: redundant history resend (`T × history_tokens`) costing **260K wasted tokens across 20 iterations**.
- Length-limit loops: `finish_reason=="length"` not handled → **$0.91 burned on one task**.
- Unbounded per-task spending: `max_iterations` alone allows **$2.70 per user message**.
- Proposed: `maxCostPerMessage`, `maxInputTokensPerLoop` config + detection of `finish_reason=="length"`.

### H1 2026 retrospective — Digital Applied

**Source:** `https://www.digitalapplied.com/blog/ai-incidents-h1-2026-retrospective-failure-modes-analysis`

- Tool-misuse cascades = **~25% of H1 2026 incidents**, second-largest category.
- Fastest-growing failure mode; projected **~30% in H2 2026**.
- Patterns: retry storms, runaway loops, MCP server outages cascading through agent retries.
- Primary detection: **cost anomaly panels**. Severity scales with detection delay (minor at minute 4, catastrophic at hour 4).
- Recommended posture: *"cost anomaly panels + tool quarantine."*

### Token consumption baseline — LeanOps

**Source:** `https://leanopstech.com/blog/agentic-ai-cost-runaway-token-budget-2026/`

- Agents burn **50x more tokens than chats**.
- At 50 steps: **30x multiplier**. At 200 steps: **>100x multiplier**.
- Per-developer monthly spend (30-team audit): median $480, p75 $980, p90 $1,650, p99 $4,200+.
- **62% of agent bills = re-sent context**, 14% tool definitions, 11% reasoning, 8% system prompts, 5% wasted retries.

---

## 7. Industry Patterns — Detection & Recovery

### StuckLoopDetection (3 patterns) — Kacperwlodarczyk / Medium

**Source:** `https://medium.com/@kacperwlodarczyk/stuckloopdetection-how-we-stopped-an-agent-burning-12-on-47-identical-calls-a12b5ea1f193`

Production library (`$12 burned on 47 identical calls`) catches three patterns:

| Pattern | Definition | Threshold |
|---|---|---|
| Repeated Identical | Same tool, same args, N consecutive turns | **Default 3** |
| A-B-A-B Alternating | Two tools cycling, each justifying the other | Not specified |
| No-Op Loop | Same call producing identical success result | Not specified |

Actions on detection:
- `action="warn"` (default) — triggers ModelRetry with *"try a different approach"* message.
- `action="error"` — raises `StuckLoopError`, halts immediately.

### nanobot N-strike (proposed, not all merged)

**Sources:**
- `https://heyferrante.com/ai-agent-frameworks-february-2026`
- nanobot issue tracker references in main repo

- Warn after **N=3** identical call sets → inject system message *"you are repeating yourself."*
- Hard stop after **M=5** identical call sets → strip `tool_calls` from the response, force final text answer.

### Inngest Utah harness — two-tier budget pressure

**Sources:**
- `https://github.com/inngest/utah`
- `https://www.inngest.com/blog/your-agent-needs-a-harness-not-a-framework`
- `https://github.com/NousResearch/hermes-agent/issues/414`

Default `maxIterations = 20`. Two-tier pressure messages injected into the LLM context (ephemeral, not persisted):

| Tier | When | Message |
|---|---|---|
| CAUTION | iterations >= maxIterations - **10** (i.e. 10 iters before end) | `[SYSTEM: Iteration N/M. Start wrapping up — respond with text soon.]` |
| WARNING | iterations >= maxIterations - **3** (last 3 iters) | `[SYSTEM: You are on iteration N of M. You MUST respond with your final answer NOW. Do not call any more tools.]` |

Hermes adapted as relative thresholds:
- `BUDGET_CAUTION_THRESHOLD = 0.7` (70% consumed)
- `BUDGET_WARNING_THRESHOLD = 0.9` (90% consumed)

Key insight: messages appended to `api_messages` (sent to API) NOT to persistent `messages` array, so session history stays clean.

### Hermes auto-continue (proposed)

**Source:** `https://github.com/NousResearch/hermes-agent/issues/16068`

When `max_turns` reached:
- **Current (strict):** stop the turn, ask model for final summary WITHOUT tools, print error to user.
- **Proposed (soft, opt-in):** append safe continuation user message, extend budget by one `max_turns` window, repeat up to `max_auto_continues` (default 3), fall back to summary-only mode if exhausted.

Hermes' framing: default disabled, explicit opt-in, hard-bounded — "safety-bounded soft extension rather than a hard error."

---

## 8. Academic Literature — 2026

### Budget-Aware Tool-Use (arXiv 2511.17006)

**Source:** `https://arxiv.org/abs/2511.17006` (Nov 2025, UCSB / Google / NYU)

First systematic study of budget-constrained agents. Key findings:

- **Naively granting larger budgets fails.** Standard ReAct *"saturates and fails to utilize additional tool budget."* On BrowseComp at budget=100, ReAct plateaus at **12.6% accuracy** while a budget-aware variant scales to **24.6%**.
- **Per-tool budgets > unified token budget** for "relevance, consistency, and practicability." The paper explicitly tracks `search` and `browse` calls separately.
- **BATS framework** mechanisms:
  1. **Budget-aware planning** — agent decomposes constraints into exploration vs. verification, maintains a dynamic checklist with step status + resource usage.
  2. **Budget-aware verification** — at proposed-answer time, decides CONTINUE or PIVOT based on remaining resources.
  3. **Trajectory compression** — replace raw trajectory with summaries to reduce token overhead.
- Observed call counts at budget=100: ReAct averages **14.24 search calls + 1.36 browse calls** per question, costing **9.9¢ unified cost**.
- BATS at budget=5 hits **29.8% accuracy** on BrowseComp-ZH (vs ReAct 30.7% at budget=200) — budget-aware agents do MORE with LESS.

### Anthropic "Effective context engineering for AI agents"

**Source:** `https://www.anthropic.com/engineering/effective-context-engineering-for-ai-agents`

Frames context as a *"finite resource with an 'attention budget' — scarce, directional, and deserving the same engineering discipline as any load-bearing system component."* No specific iteration caps prescribed; the position is that tool DESIGN (minimal set, robust to error, clear contracts, token-efficient returns) prevents thrash. Quote: *"Tools should be self-contained, robust to error, and extremely clear with respect to their intended use… tools promote efficiency, both by returning information that is token efficient and by encouraging efficient agent behaviors."*

### Agent Harness Engineering (Addy Osmani + MindStudio + awesome-harness-engineering)

**Sources:**
- `https://addyosmani.com/blog/agent-harness-engineering/`
- `https://www.mindstudio.ai/blog/9-components-production-agent-harness`
- `https://github.com/ai-boost/awesome-harness-engineering`

The 9-component production harness per MindStudio:
1. While-loop (with cap)
2. Context management (compact at 80–90% of window)
3. Skills + tools registry
4. Sub-agent management
5. Built-in skills
6. Session persistence
7. System prompt assembly
8. Lifecycle hooks (pre/post tool execution)
9. Permissions + safety

Quoted rule: *"Always cap the iteration count. An uncapped loop will run until it hits a context limit or your API budget."* Notes a **25-point benchmark swing between identical models running in different harnesses** — harness > model.

---

## 9. Strict vs. Graceful Budget Exhaustion — The Debate

This is genuinely **contested in 2026**. Three camps:

### Camp A — Fail Hard (Anthropic SDK, AWS Strands, OpenAI Agents default)

- Hit `max_turns` → return error subtype (`error_max_turns`) with no `result` field.
- User / orchestrator MUST handle it.
- Argument: bounded blast radius, no compounding cost from soft retries, forces operators to set realistic budgets.
- Anthropic SDK: *"the SDK returns a ResultMessage with… `error_max_turns`."*
- AWS Strands: raises `MaxTokensReachedException`, *"unrecoverable within the current loop."*

### Camp B — Force-Finalize (Hermes current, nanobot proposal)

- At cap, strip `tool_calls` from the response and re-call the model with `tool_choice=none` so it MUST produce a text answer.
- Pros: user gets *something*, not a stack trace.
- Cons (Hermes #16068): *"the final summary request strips tools, so the agent cannot finish the task"* — answer is often shallow, can mislead.

### Camp C — Bounded Soft Continue (Hermes #16068 proposal, AI SDK loop-control)

- Append a safe continuation user message, extend the budget window, cap at N auto-continues (default 3), fall back to Camp B if exhausted.
- Used carefully: explicit opt-in, hard upper bound, prompt instructs model to stop on blockers / approvals / destructive actions.
- Best for long-running autonomous coding agents where the work was actually progressing.

**No 2026 consensus.** Anthropic + AWS + OpenAI default to Camp A; LangGraph defaults to Camp A (raises error); coding-agent harnesses (Hermes, Aider lineage) lean toward Camp B/C for UX reasons. The user-facing-product / "consumer agent" market is converging on Camp B (force-finalize) because end users can't action a `MaxTurnsExceeded` exception.

---

## 10. Per-Tool Budget vs. Flat Budget — 2026 Consensus

**Mostly flat-budget wins in shipped SDKs.** Per-tool budgets exist in research but are rare in production frameworks:

| System | Approach |
|---|---|
| Anthropic Agent SDK | Flat (`max_turns`) + USD (`max_budget_usd`) |
| OpenAI Agents | Flat (`max_turns` default 10) |
| LangGraph | Flat (`recursion_limit` default 25) |
| AWS Strands | Flat (`max_iterations` ~5) + wall-clock + tokens |
| Google ADK | Flat per-LoopAgent (`max_iterations` 3–5) |
| Inngest Utah | Flat (`maxIterations=20`) + pressure tiers |
| DeepAgents (LangChain) | Flat recursion_limit + optional explicit per-counter state |
| BATS / academic | **Per-tool budgets** (search vs browse) |

The arXiv 2511.17006 paper is the **strongest recent argument for per-tool budgets** — it shows that unified budgets cause specific failure modes (over-spending on cheap tools like search, under-spending on expensive tools like browse). The paper argues *"relevance, consistency, and practicability"* favor per-tool.

Production SDKs have **not yet adopted** per-tool budgets because the operator complexity is high and the win is marginal for general-purpose agents. Per-tool budgets show up in **vertical agents** (search agents, browse agents) where the tool taxonomy is small and stable.

---

## 11. Repeated-Query Throttling — Production-Validated?

**Yes, but the deployment pattern is "warn-then-strike," not "strict 2-strike."**

- StuckLoopDetection: **3 strikes** is the default for identical-call repetition.
- Nanobot proposal: **warn at 3, hard stop at 5** for identical call sets.
- $47K postmortem fix: input hash dedup triggering at **N=2**.
- The 2-strike pattern exists (postmortem fix) but is the AGGRESSIVE end of the spectrum. 3–5 strikes is more common.

Common code shape across these implementations:

```
hash = sha256(tool_name + canonical(args))
if hash_count[hash] >= warn_threshold:
    inject_system_message("you are repeating; try different approach")
if hash_count[hash] >= hard_threshold:
    block the call OR strip tool_calls and force final answer
```

The cheap-and-cheerful version is *"one dict lookup per tool call"* (StuckLoopDetection author's framing). This is genuinely cheap and the consensus is **always implement it** in any agent that calls tools >5 times.

A-B-A-B alternating loops and no-op loops also exist but are detected less commonly. StuckLoopDetection is the only public production library covering all 3 patterns.

---

## 12. Upper Bound on Tool Calls per Turn — Observed Production Numbers

Synthesizing the numbers above:

| Source | Cap recommended/observed |
|---|---|
| Anthropic Agent SDK example | `max_turns=30` (Anthropic docs) |
| OpenAI Agents SDK default | `max_turns=10` (DEFAULT_MAX_TURNS) |
| LangGraph default | `recursion_limit=25` |
| AWS Strands example | `max_iterations=5` |
| Google ADK examples | `max_iterations=3–5` |
| Inngest Utah default | `maxIterations=20` |
| Hermes default | `max_iterations=60` |
| nanobot observed cost-cliff | $2.70/message at default max_iterations |
| BATS paper saturation point | ReAct plateaus at **budget=100**, no further gains |

**Median sane production cap is ~10–30 tool calls per turn**, with vertical agents (Strands, ADK) going as low as 3–5 and coding agents (Hermes) going as high as 60. Above 30, you should be using sub-agents to split the work, not extending the cap.

The arXiv paper's observation that **ReAct plateaus at budget=100** is a strong signal that beyond ~30 calls per turn, you're spending money for negligible gain unless the agent is explicitly budget-aware.

---

## 13. Where the 2026 Consensus is Weak / Contested

1. **Per-tool vs flat budgets** — production frameworks all flat; research papers (arXiv 2511.17006) argue per-tool. Real-world per-tool implementations are rare and mostly in vertical agents.
2. **Fail-hard vs soft-continue at budget exhaustion** — Anthropic / AWS / OpenAI default to fail-hard; coding-agent harnesses (Hermes, Aider) want soft-continue; the user-facing market wants force-finalize. No winner.
3. **What system messages to inject under pressure** — Utah / Hermes have specific patterns but there's no consensus on the exact wording or thresholds (7 different threshold schemes documented).
4. **Wall-clock vs iteration vs token vs USD** — best-practice is "all four together" but most frameworks only ship 1–2. The $4,200 postmortem fixed *"dollar, token, time, AND recursion ceilings with human escalation"* — the OR-of-four is the production gold standard but rarely implemented.
5. **Whether to surface budget pressure to the model** — Utah / Hermes inject system messages; Anthropic SDK does NOT (just errors out); LangGraph does NOT. Real evidence on whether pressure messages help vs. just biasing the model toward premature termination is thin.

---

## 14. Summary Table — Recommended Production Caps (2026)

| Layer | Mechanism | Typical Value |
|---|---|---|
| Hard turn cap | `max_turns` / `max_iterations` / `recursion_limit` | 10–30 (general), 3–5 (vertical) |
| Hard USD cap | `max_budget_usd` per session | $0.10–$1.00 per user turn |
| Hard wall-clock cap | timeout | 30–60s (interactive), 600s (autonomous) |
| Hard token cap | `token_limit` or `max_completion_tokens` | model-dependent; 4–8K typical |
| Soft pressure tiers | inject system messages | CAUTION at 70%, WARNING at 90% |
| Same-call dedup | hash(tool, args) counter | warn at 3, hard stop at 5 |
| A-B-A-B detection | rolling window pattern match | trip at 3 cycles |
| Cost anomaly alert | rate vs rolling average | 3x baseline within 60s |
| Force-finalize | `tool_choice=none` final turn | when hard cap hit AND user-facing |
| Soft auto-continue | extend budget window | opt-in, max 3 extensions |

---

## 15. Sources

### Anthropic
- [Claude Agent SDK — Agent Loop](https://code.claude.com/docs/en/agent-sdk/agent-loop)
- [Implement tool use](https://docs.anthropic.com/en/docs/agents-and-tools/tool-use/implement-tool-use)
- [Effective context engineering for AI agents](https://www.anthropic.com/engineering/effective-context-engineering-for-ai-agents)
- [Advanced tool use](https://www.anthropic.com/engineering/advanced-tool-use)
- [tool_choice cookbook](https://github.com/anthropics/anthropic-cookbook/blob/main/tool_use/tool_choice.ipynb)

### OpenAI
- [Assistants API deep-dive](https://platform.openai.com/docs/assistants/deep-dive)
- [Agents SDK — running agents](https://openai.github.io/openai-agents-python/running_agents/)
- [Agents SDK — Runner](https://openai.github.io/openai-agents-python/ref/run/)
- [Function calling](https://platform.openai.com/docs/guides/function-calling)

### LangChain / LangGraph
- [GRAPH_RECURSION_LIMIT](https://docs.langchain.com/oss/python/langgraph/errors/GRAPH_RECURSION_LIMIT)
- [DeepAgents capping forum thread](https://forum.langchain.com/t/how-to-cap-tool-and-sub-agent-calls-in-deepagents/1653)
- [Infinite looping issue #6731](https://github.com/langchain-ai/langgraph/issues/6731)
- [Deep Agents docs](https://docs.langchain.com/oss/python/deepagents/overview)

### AWS / Google
- [Strands Agents SDK deep dive](https://aws.amazon.com/blogs/machine-learning/strands-agents-sdk-a-technical-deep-dive-into-agent-architectures-and-observability/)
- [Strands Agents 1.0](https://aws.amazon.com/blogs/opensource/introducing-strands-agents-1-0-production-ready-multi-agent-orchestration-made-simple/)
- [Strands Agent Loop docs](https://strandsagents.com/docs/user-guide/concepts/agents/agent-loop/)
- [Google ADK LoopAgent](https://google.github.io/adk-docs/agents/workflow-agents/loop-agents/)
- [Google ADK docs](https://google.github.io/adk-docs/)

### Postmortems / Case Studies
- [The Agent That Spent $47K — gabrielanhaia](https://dev.to/gabrielanhaia/the-agent-that-spent-47k-on-itself-an-autonomous-loop-postmortem-3313)
- [$4,200 in 63 Hours — Sattyam Jain](https://medium.com/@sattyamjain96/the-agent-that-burned-4-200-in-63-hours-a-production-ai-postmortem-d38fd9586a85)
- [$437 overnight — earezki](https://earezki.com/ai-news/2026-04-29-i-let-my-ai-agent-run-overnight-it-cost-437/)
- [AI Incidents H1 2026 Retrospective — Digital Applied](https://www.digitalapplied.com/blog/ai-incidents-h1-2026-retrospective-failure-modes-analysis)
- [Agentic Workflow Incident Response — Digital Applied](https://www.digitalapplied.com/blog/agentic-workflow-incident-response-playbook-2026)
- [AI Agents Burn 50x More Tokens — LeanOps](https://leanopstech.com/blog/agentic-ai-cost-runaway-token-budget-2026/)
- [10 Lessons From Agents That Crashed — Operator Collective](https://theoperatorcollective.org/blog/ai-agent-failures-lessons-crashes)

### Loop Detection / Throttling
- [StuckLoopDetection — Kacperwlodarczyk](https://medium.com/@kacperwlodarczyk/stuckloopdetection-how-we-stopped-an-agent-burning-12-on-47-identical-calls-a12b5ea1f193)
- [Why Your LangChain Agent Keeps Calling the Same Tool — gabrielanhaia](https://dev.to/gabrielanhaia/why-your-langchain-agent-keeps-calling-the-same-tool-in-a-loop-and-how-to-stop-it-57gk)
- [deer-flow repetitive tool loops](https://github.com/bytedance/deer-flow/issues/1055)
- [Infinite Agent Loop — Agent Patterns](https://www.agentpatterns.tech/en/failures/infinite-loop)

### Harness Engineering
- [Your Agent Needs a Harness — Inngest](https://www.inngest.com/blog/your-agent-needs-a-harness-not-a-framework)
- [Utah harness](https://github.com/inngest/utah)
- [Iteration Budget Pressure — Hermes #414](https://github.com/NousResearch/hermes-agent/issues/414)
- [Auto-continue at max iterations — Hermes #16068](https://github.com/NousResearch/hermes-agent/issues/16068)
- [Bounded auto-continue — Hermes #16004](https://github.com/NousResearch/hermes-agent/issues/16004)
- [9 Components of Production Agent Harness — MindStudio](https://www.mindstudio.ai/blog/9-components-production-agent-harness)
- [Agent Harness Engineering — Addy Osmani](https://addyosmani.com/blog/agent-harness-engineering/)
- [Agent Harnesses 2026 — htekdev](https://dev.to/htekdev/agent-harnesses-why-2026-isnt-about-more-agents-its-about-controlling-them-1f24)
- [awesome-harness-engineering](https://github.com/ai-boost/awesome-harness-engineering)
- [Token Budget Management in Claude Code — MindStudio](https://www.mindstudio.ai/blog/ai-agent-token-budget-management-claude-code)

### Academic
- [Budget-Aware Tool-Use (arXiv 2511.17006)](https://arxiv.org/abs/2511.17006)
- [Budget-Aware Tool-Use HTML](https://arxiv.org/html/2511.17006v1)
- [ContextBudget (arXiv 2604.01664)](https://arxiv.org/html/2604.01664)
- [ToolMisuseBench (arXiv 2604.01508)](https://arxiv.org/pdf/2604.01508)

### nanobot
- [Tool-loop cost roadmap — nanobot #2318](https://github.com/HKUDS/nanobot/issues/2318)
- [nanobot main repo](https://github.com/HKUDS/nanobot)
- [Claw Family agent framework comparison — Feb 2026](https://heyferrante.com/ai-agent-frameworks-february-2026)

### Cost / Observability
- [Cost Circuit Breaker — Fountaincity](https://fountaincity.tech/resources/blog/ai-agent-cost-circuit-breaker/)
- [4 Pillars of AI Agent Observability — NebulaGG](https://dev.to/nebulagg/ai-agent-observability-the-4-pillars-that-keep-your-agents-from-burning-2000-at-3-am-24cn)
- [Monitor AI Agent Costs Real-Time — Braincuber](https://www.braincuber.com/blog/how-to-monitor-ai-agent-costs-in-real-time)
- [Agent Observability for Production — Agentix](https://www.agentixlabs.com/blog/general/agent-observability-for-production-trace-tools-cost-and-safety-signals/)
