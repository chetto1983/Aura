# Non-openhuman multi-agent patterns under D:/tmp/ — audit (2026-05-25)

Survey of `D:/tmp/{hermes-agent, cli-printing-press, elysia, nanobot, codex, picobot,
system_prompts_leaks, agent-infra-sandbox, assistant-ui, aura-agent-loop-papers}` for
multi-agent / archetype-registry patterns relevant to Aura Wave OH1
(AGENTDEF + TIER + DELEGATE-TOOL). Concepts only — no GPL code copied;
all repos here are MIT / BSD / Apache-2.0.

Cross-references: `docs/aura-graph-tools-plan-2026-05-25.md` §5 (Wave OH1),
`docs/openhuman-pattern-lift-2026-05-25.md` (the canonical AGENTDEF source).

---

## 1. Headline summary

The non-openhuman corpus **REINFORCES the openhuman-derived OH1 design on every
core dimension** (per-archetype config registry, hierarchical spawn, tool-scoped
sub-loops, parent-doesn't-see-child-trace) and adds **three credible divergent
options Aura should at least name** in OH1-S0 discuss-phase before committing
the PRD: (a) codex's **config-layer-overlay** model instead of monolithic TOML
record (lower blast radius, harder for users to author); (b) hermes-agent's
**fork-and-restrict** background review pattern (no separate archetypes, just
a tool-whitelist on a forked self) which is conceptually one rung *below* the
full registry but ships in 1 file; (c) AOrchestra paper's **dynamic-tuple**
where the orchestrator concretizes `<Instruction, Context, Tools, Model>` per
step (no static archetypes at all). Conclusion: **stay the openhuman course**
for OH1 — codex/nanobot/Claude Code/Cowork production prompts all converge on
"named archetype + tool allowlist + sub-loop", which is exactly the OH1 shape.

---

## 2. Findings

### F1 — codex named-agent-role registry with built-in `default | explorer | worker`

- **Source**: `D:/tmp/codex/codex-rs/core/src/agent/role.rs:29-382`
  (`AgentRoleConfig`, `apply_role_to_config`, `spawn_tool_spec::build`,
  `built_in::configs` with explorer/worker hard-coded role TOMLs); supporting
  schema `D:/tmp/codex/codex-rs/core/config.schema.json` and per-role files
  `codex-rs/core/src/agent/builtins/{explorer.toml,awaiter.toml}`.
- **License**: Apache-2.0 — clean-room friendly.
- **Pattern shape**: Roles ship as built-in TOML strings embedded in the
  binary (`include_str!`) + user can override by id under codex_home. At
  spawn time the role TOML is loaded as a **config layer** placed at
  session-flag precedence — it does NOT replace the agent's config; it
  *overlays* keys the role declares while the caller's runtime provider and
  service tier remain sticky. `spawn_tool_spec::build` synthesises a single
  `spawn_agent`-style tool whose description enumerates available roles +
  their locked settings ("This role's model is set to gpt-5-mini and cannot
  be changed").
- **Compatibility with OH1**: **REINFORCES, with one divergent twist worth
  surfacing in OH1-S0.**
- **Reinforces**: built-in-in-binary + user-override-on-disk + TOML file
  format + role.description used as tool description + parent's per-call
  context (provider/tier) preserved across spawn. This is exactly the
  OH1 AGENTDEF shape derived from openhuman, validated independently by
  Anthropic's competition.
- **Divergent twist**: codex uses **config-layer composition** instead of a
  monolithic struct. A role TOML is a partial config that gets inserted into
  the `ConfigLayerStack` at session-flag precedence; missing keys inherit
  from the parent's effective config. openhuman's AGENTDEF is a complete
  record (all fields mandatory or defaulted). The codex approach is more
  composable (a role can declare *only* the model + reasoning_effort and
  inherit everything else), but it shifts complexity from "parse one struct"
  to "merge N layers" and requires Aura to first build a config-layer stack
  abstraction. Recommendation for OH1-S0: **stick with openhuman's full-
  struct AGENTDEF for Aura** (Aura's config surface is small, the layer
  machinery doesn't yet exist), but capture codex's pattern as the
  evolution path once Aura's config-overlay system grows up.

### F2 — codex thread-spawn tree + fork-from-history

- **Source**: `D:/tmp/codex/codex-rs/core/src/thread_manager.rs:170-1338`
  (`ThreadManager`, `spawn_subagent`, `list_agent_subtree_thread_ids`,
  `list_live_thread_spawn_edges`, `fork_thread*`, `SubAgentSource::ThreadSpawn{parent_thread_id, ...}`).
- **License**: Apache-2.0.
- **Pattern shape**: Every spawned subagent is itself a full Thread (own
  rollout, own state-db row, own SessionConfigured event). The parent-child
  edge is stored as `SessionSource::SubAgent(SubAgentSource::ThreadSpawn{parent_thread_id})`
  on the child; the runtime can walk live + persisted edges to materialise
  the full spawn tree (`list_agent_subtree_thread_ids` does Open + Closed
  edge traversal). `spawn_subagent` forks the parent's persisted history at
  the *Interrupted* boundary, so the child sees what the parent had seen up
  to that moment but writes to its own rollout.
- **Compatibility with OH1**: **REINFORCES OH1-S3 (DELEGATE-TOOL) +
  surfaces one missing dimension in the openhuman lift.**
- **Reinforces**: parent never sees child's tool calls (separate rollouts);
  child is a "real" agent with full event stream, not a degraded inner-loop
  shim. Same shape openhuman uses.
- **Adds**: **persisted parent-child edges** (`SubAgentSource::ThreadSpawn`)
  + a **subtree query API** (`list_agent_subtree_thread_ids`). The
  openhuman lift treats DELEGATE-TOOL as transient (parent waits, child
  returns text, done) — codex persists the relationship so the user can
  later reconstruct "everything that came out of session X" including all
  delegated subagents. **Recommendation**: OH1-S3 should add a
  `parent_session_id` column on Aura's `conversations` table and store
  delegated children with that linkage. Cost ~30 LOC, unlocks future
  "show me the full spawn tree" dashboard surface. Not on the OH1 critical
  path but should be in the same commit since the alternative is a
  schema-migration story later.

### F3 — Claude Code production prompt: `subagent_type` + `SendMessage` continue + background runs

- **Source**: `D:/tmp/system_prompts_leaks/Anthropic/claude-code.md:260-396`
  (full `Agent` tool spec); reinforced by
  `D:/tmp/system_prompts_leaks/Anthropic/claude-cowork.md:193` ("For
  particularly high-stakes work, Claude should use a subagent (Task tool)
  for verification.").
- **License**: leaked-prompts repo is MIT (the prompt itself is Anthropic
  copyright — we lift the *pattern*, not the text).
- **Pattern shape**: production `Agent({description, prompt, subagent_type?,
  model?, run_in_background?, isolation: "worktree"?})` tool. Four built-in
  archetypes — `Explore`, `general-purpose`, `Plan`, `statusline-setup` —
  each with an **explicit tool allowlist in the prompt** ("Tools: All tools
  except Agent, ExitPlanMode, Edit, Write, NotebookEdit"). The prompt
  hard-codes guidance Aura should mirror verbatim:
  - "Never delegate understanding" (don't push synthesis onto the child).
  - "Trust but verify" (an agent's summary describes intent, not what it
    actually did — check the diff).
  - "Brief the agent like a smart colleague who just walked into the room"
    (self-contained prompts; no implicit context).
  - "When you launch multiple agents for independent work, send them in a
    single message with multiple tool uses so they run concurrently" (the
    parallel-dispatch pattern Aura already does for native tools — extend
    to delegates).
  - `SendMessage(to=agent_id|name)` to resume a previously-spawned agent
    (continuity, not just one-shot delegation).
- **Compatibility with OH1**: **REINFORCES** every architectural choice in
  the openhuman lift.
- **Adds (worth lifting into OH1-S3 prompt + AGENT.md edit)**: the four
  guidance lines above. Pure prompt work, trivial LOC. The
  `SendMessage(to=agent_id)` resume-an-existing-spawn pattern is **not** in
  the openhuman lift and is a credible OH3 follow-up (sub-agent
  conversation memory) but should be explicitly **deferred** in OH1 — the
  one-shot delegate pattern is the MVP; persistent named children is the
  v2.

### F4 — nanobot SubagentManager: async background spawn + concurrency cap + status dashboard

- **Source**: `D:/tmp/nanobot/nanobot/agent/subagent.py:1-200`
  (`SubagentManager.spawn`, `SubagentStatus` dataclass with
  `phase/iteration/tool_events/usage`),
  `D:/tmp/nanobot/nanobot/agent/tools/spawn.py:1-79` (`SpawnTool`),
  `D:/tmp/nanobot/nanobot/config/schema.py:125` (`max_concurrent_subagents: int = Field(default=1, ge=1)`),
  `D:/tmp/nanobot/nanobot/templates/agent/subagent_system.md`,
  `D:/tmp/nanobot/nanobot/templates/agent/subagent_announce.md`,
  `D:/tmp/nanobot/nanobot/utils/subagent_channel_display.py` (channel-safe
  display scrubbing).
- **License**: MIT.
- **Pattern shape**: **single `spawn(task, label?)` tool**, not per-archetype
  `delegate_<id>` synthesis. The SubagentManager owns the async task pool
  (`asyncio.create_task`), enforces a `max_concurrent_subagents` cap (default
  1), and surfaces real-time status (`phase ∈ initializing|awaiting_tools|
  tools_completed|final_response|done|error`, iteration counter, tool_events
  list, usage dict, error string). On completion the result is wrapped in
  the `subagent_announce.md` template (`[Subagent '{label}' done] Task: ...
  Result: ... Summarize this naturally for the user. Keep it brief.`) and
  posted back as an `injected_event=subagent_result` message that the
  parent's next turn sees. Channels other than the parent see a scrubbed
  version (`scrub_subagent_announce_body` strips the model-only "Summarize
  this naturally" trailer + caps result at 800 chars for WebSocket replay).
- **Compatibility with OH1**: **REINFORCES core + DIVERGENT on the tool
  shape.**
- **Reinforces**: sub-loop with isolated ToolRegistry (`_build_tools`
  scope="subagent"), parent-doesn't-see-child-trace, dedicated subagent
  system prompt, configurable per-tier max-iter cap.
- **Divergent option for Aura**: nanobot exposes ONE `spawn` tool with a
  free-form `task` string — no per-archetype `delegate_summarizer /
  delegate_researcher` synthesis at all. The model picks "what kind of
  subagent" implicitly via the task wording. This is **strictly simpler**
  than OH1-S3 (no per-turn manifest mutation, no tool-name dedup, no
  `subagents[]` field to populate) and is what nanobot ships in production.
  Trade-off: the chat tier loses the prompt-side affordance ("a tool
  literally named `delegate_summarizer` exists, use it for this") which
  is the openhuman lift's reason for the per-archetype synthesis. **OH1-S0
  recommendation**: keep openhuman's per-archetype synthesis as the OH1
  decision (the affordance is real), but add `spawn(task)` as the **fallback
  generic delegate** for cases where no specialist matches — this is a
  ~50 LOC add on top of OH1-S3 and gives Aura an escape hatch.
- **Adds (free wins to absorb into OH1-S3 + OH3)**:
  - `max_concurrent_subagents` cap as a first-class config (default 1)
    so Aura doesn't go from "one big loop" to "fork bomb" overnight.
  - `SubagentStatus` dataclass + the 5-phase enum — directly informs how
    Aura should surface delegation in `/api/conversations` and the
    dashboard.
  - `subagent_announce.md` template + the **channel-safe scrubbing**
    (strip "Summarize this naturally" trailer for external channels but
    keep on-disk for LLM replay) — this is the right answer to "what does
    the Telegram user see when a delegate fires" and should be lifted
    verbatim into Aura's `internal/channels/telegram/outbound.go` when
    DELEGATE-TOOL ships.

### F5 — hermes-agent `spawn_background_review`: forked self with tool allowlist (NO archetypes)

- **Source**: `D:/tmp/hermes-agent/agent/background_review.py:1-200`
  (`_MEMORY_REVIEW_PROMPT`, `_SKILL_REVIEW_PROMPT`, `_COMBINED_REVIEW_PROMPT`),
  paired with `spawn_background_review_thread` in `run_agent.py`.
- **License**: MIT.
- **Pattern shape**: After every qualifying turn, hermes-agent forks the
  AIAgent into a daemon thread that replays the conversation snapshot with
  the same provider/model/credentials (so it hits the same prefix cache)
  but with a **runtime-enforced tool whitelist** restricted to memory +
  skill management tools. The review agent runs the `_COMBINED_REVIEW_PROMPT`
  that asks "should any skill/memory be saved or updated?" and writes
  directly to the memory + skill stores. Main conversation untouched.
  Critically: **no separate archetype config** — the fork is the same
  AIAgent with one parameter flipped (tool_whitelist).
- **Compatibility with OH1**: **DIVERGENT (worth naming as an alternative)
  + REINFORCES OH3-S1 REFLECTION-POSTTURN.**
- **Divergent (alternative to OH1)**: if Aura wanted to ship a "background
  reviewer" specialist *without* the full AGENTDEF substrate, this
  fork-and-restrict pattern is the cheapest possible delivery (~150 LOC
  total: the prompt + the tool-whitelist gate + the daemon thread).
  Trade-off: it only scales to 1-2 specialists before you're hand-coding
  the same pattern repeatedly; AGENTDEF is the principled solution.
  **OH1-S0 recommendation**: do NOT switch to fork-and-restrict, but use
  this pattern *inside* OH3-S1 REFLECTION-POSTTURN as the bridge —
  reflection ships first as a fork-and-restrict at OH3-S1 time, then
  migrates to a proper AGENTDEF-defined `reflector` archetype once OH1
  lands. This is the path of least resistance and unblocks OH3 from the
  OH1 critical path.
- **Reinforces OH3-S1**: the reflection prompt content (the
  `_COMBINED_REVIEW_PROMPT`) is **the most detailed production reference**
  we have for the "what should an auto-post-turn reflector actually ask"
  question. Three lifts directly applicable:
  - separate **MEMORY** review (who the user is) from **SKILL** review
    (how to do this class of task) — Aura should encode this split in
    `internal/learning/reflection.go` instead of one merged structured
    output.
  - **"Do NOT capture" allowlist** (environment-dependent failures,
    negative claims about tools, session-specific transient errors) —
    Aura's reflection prompt MUST include this or it will silently learn
    "browser tools don't work" the first time a sandbox is misconfigured.
  - the **preference order for skill updates** (UPDATE-LOADED →
    UPDATE-UMBRELLA → ADD-SUPPORT-FILE → CREATE-NEW) — directly
    transferable to Aura's wiki-page write strategy where the equivalent
    is UPDATE-EXISTING-PAGE → ADD-RELATED-LINK → CREATE-NEW-PAGE.

### F6 — AOrchestra (Feb 2026 paper): dynamic `<Instruction, Context, Tools, Model>` tuple

- **Source**: `D:/tmp/aura-agent-loop-papers/2602.03786-AOrchestra.txt`
  page 1 (abstract + introduction).
- **License**: arXiv preprint, CC-BY 4.0 (concepts freely citable).
- **Pattern shape**: instead of a static registry of named archetypes, the
  central orchestrator **concretizes** a 4-tuple
  `<Instruction, Context, Tools, Model>` per step on the fly: it curates
  task-relevant context, selects tools and models, and delegates to a
  freshly-instantiated specialist executor whose persona only exists for
  that one delegation. Authors report +16.28% relative improvement on
  GAIA / SWE-Bench / Terminal-Bench vs static baselines (with
  Gemini-3-Flash).
- **Compatibility with OH1**: **DIVERGENT (alternative architecture worth
  naming).**
- **Alternative sketch**: skip OH1-S1 (AGENTDEF registry) entirely and
  ship a `delegate(task, tools[], model?, max_iter?)` tool that takes
  the four-tuple as arguments. The orchestrator (Aura's chat tier) decides
  what tools/model/iter the specialist gets per call; there is no named
  archetype, no `subagents[]` list, no built-in TOMLs. **When this wins**:
  when the task surface is so diverse that pre-named archetypes are always
  wrong (e.g. a research platform where every task is a new shape). **When
  it loses**: when the prompt-side affordance of "tool literally named
  `delegate_summarizer` exists" actually steers the model — exactly Aura's
  case, where the chat tier has small + repeated task classes (summarise
  search hit, write a wiki page, do voice TTS) that benefit from named
  specialists. **OH1-S0 recommendation**: name AOrchestra explicitly as
  the "we considered + rejected" alternative in the discuss-phase output,
  with the trade-off captured: dynamic-tuple is more general but loses
  the prompt-side affordance and pushes the "what tools / what model"
  decision onto the orchestrator every call (more reasoning load).
  Stick with openhuman's AGENTDEF.

### F7 — Swarm-Skills (May 2026 paper): coordination protocols as portable assets

- **Source**: `D:/tmp/aura-agent-loop-papers/2605.10052-Swarm-Skills.txt`
  page 1-2 (abstract + figure 1).
- **License**: arXiv preprint, CC-BY 4.0.
- **Pattern shape**: extends Anthropic's Skill spec with multi-agent
  semantics. A Swarm Skill is a portable bundle of `{roles, workflows,
  execution_bounds, evolution_experience}`. The companion self-evolution
  algorithm distills successful execution trajectories into new Swarm
  Skills (CREATE) and patches existing ones (PATCH) based on a
  multi-dimensional score (Effectiveness × Utilization × Freshness).
- **Compatibility with OH1**: **REINFORCES the long-term direction +
  DEFERRED — not a Wave OH1 decision.**
- **Reinforces**: the field is converging on "portable role + workflow
  bundle" as the unit of multi-agent reuse; this is exactly the AGENTDEF
  TOML shape. Aura's `runtime-workspace/agents/*.toml` user-override slot
  could later become Swarm-Skill-compatible (the TOML is a strict subset
  of the spec) without redesign.
- **Deferred**: the *self-evolution* algorithm (auto-distill + auto-patch
  swarm skills with E×U×F scoring) is an OH3+ concern. Aura's Wave OH3
  REFLECTION + PROFILE-RENDER is the single-agent analogue; the
  multi-agent version (auto-promote a frequently-used delegation chain
  to a named saved workflow) is a Wave 4+ idea. **Capture as a roadmap
  pin**, do not absorb now.

### F8 — cli-printing-press: TIER means API tier (free/paid/enterprise), NOT agent tier — namespace collision

- **Source**: `D:/tmp/cli-printing-press/internal/generator/tier_routing_test.go:14-120`
  + `D:/tmp/cli-printing-press/internal/spec/spec.go` (`TierRoutingConfig`,
  `TierConfig`).
- **License**: MIT.
- **Pattern shape**: cli-printing-press generates per-API-tier HTTP
  clients (free → no auth, paid → api_key in query, enterprise →
  bearer_token), where the **endpoint** declares its tier and the
  generated client routes auth + rate-limiter per tier. This is a SaaS
  API tiering pattern, **not** an agent-tier pattern.
- **Compatibility with OH1**: **DOES NOT CONFLICT — but flag the
  vocabulary**.
- **Recommendation**: in Aura's OH1-S2 (TIER enum: Chat/Reasoning/Worker),
  use **`agent_tier`** as the struct field name (not `tier`) and
  **`AgentTier`** as the Go type (not `Tier`) so future Aura readers
  searching for "tier" don't conflate the two if Aura ever grows a
  subscription tier of its own. The openhuman lift already uses
  `AgentTier`; just hold the line in the Go translation.
- **Adjacent useful pattern (lift into OH1-S3, not into OH1-TIER)**:
  cli-printing-press's `mcpdesc.Compose` (`internal/mcpdesc/compose.go:1-100`)
  produces a baseline tool description from spec + signals + structural
  overrides, with an override sidecar (`mcp-descriptions.json`) taking
  precedence per-tool. This is the right shape for Aura's
  `delegate_<id>` tool descriptions: a generated baseline ("Delegate to
  the summarizer specialist. When to use: when a tool result is large
  + non-evidence text. Tools: tool_result, agent_note. Max iter: 6.")
  with a per-archetype prose override slot for hand-tuning. ~80 LOC add
  inside OH1-S3, picks up the cli-printing-press composability pattern
  cited in `project_cli_printing_press_eval_2026-05-15` memory.

### F9 — Multi-agent UX surface in assistant-ui

- **Source**: `D:/tmp/assistant-ui/packages/{agent-launcher,react-langgraph,react-a2a,react-ag-ui}/`
- **License**: MIT.
- **Pattern shape**: assistant-ui ships **separate runtime adapters per
  framework** (LangGraph, A2A, ag-ui). The thread model assumes one chat
  thread = one runtime; multi-agent is exposed by the underlying framework
  (LangGraph state graph events, A2A direct agent-to-agent), not by
  assistant-ui itself. `agent-launcher` is a 2-file package
  (`index.ts`, `launch.ts`) — Aura-relevant only as the proof that
  "launching a configured agent" is light enough to be a leaf SDK utility.
- **Compatibility with OH1**: **DOES NOT CONFLICT — no useful Aura UX
  pattern at this layer**. assistant-ui doesn't have a "delegated
  subagent showed up mid-chat" first-class component; the multi-agent UX
  is whatever the framework adapter renders.
- **See "Multi-agent UX patterns" section below** for what Aura should
  do instead.

---

## 3. Patterns checked + rejected (one-liner each)

- **picobot** (`internal/agent/context.go`, `internal/agent/tools/spawn.go`) —
  single-agent ContextBuilder, `SpawnTool` is a stub that just returns a
  string ("spawned: agent=X task=Y"). No archetype concept at all.
  Useful only as the "do not adopt this stub" baseline. (`internal/agent/tools/spawn.go:35-43`).
- **elysia** (`elysia/tree/tree.py`, `elysia/tree/util.py`,
  `elysia/api/api_types.py`) — uses a **DecisionTree** (`branch_initialisation
  ∈ default|one_branch|multi_branch|empty`) where the model picks a tool
  from a tree of options, not a registry of specialists. Single agent
  with structured tool-selection. Different paradigm (tree-of-tools vs
  tree-of-agents) — not a multi-agent reference. The `AssertedModule`
  retry-with-feedback pattern (`tree/util.py:152-200`) is interesting
  for tool-result validation but unrelated to OH1.
- **agent-infra-sandbox** (`sdk/python/agent_sandbox/`, `sdk/js/src/`) —
  pure sandbox-execution SDK (Python + JS). No archetype / role / tier
  concept. Only "agent" in filenames is the SDK namespace.
- **OpenAI gpt-5-thinking, gpt-5.5-thinking, codex/gpt-5.2-codex prompts**
  (`system_prompts_leaks/OpenAI/`) — all single-agent prompts. The OpenAI
  prompts use **harness-injected tool surface** rather than
  Claude-Code-style subagent_type. No transferable pattern beyond
  validating that Aura's single-agent default is the industry default.
- **hermes leaked prompt** (`system_prompts_leaks/Misc/hermes.md`) —
  the user-facing prompt is single-agent; multi-agent is delivered via
  the `autonomous-ai-agents` skill family (delegate to claude-code /
  codex / opencode external CLIs), which is the **external-orchestration**
  pattern, not the in-process subagent pattern OH1 designs. Not directly
  relevant.

---

## 4. Multi-agent UX patterns (for future dashboard work)

Aura today has no "delegation happened" surface. When OH1 ships, the user
will see a tool call in Telegram or the dashboard for `delegate_summarizer`
with a JSON arg — opaque and ugly. The corpus surfaces three patterns
worth combining:

1. **nanobot subagent_announce template + channel scrubbing**
   (`nanobot/templates/agent/subagent_announce.md` +
   `nanobot/utils/subagent_channel_display.py`). Persisted version shows
   `[Subagent 'label' done]\nTask: ...\nResult: ...\nSummarize this naturally
   for the user.` — that trailing instruction is for the LLM. The
   channel-display scrubber strips the trailer for external channels and
   caps the result body at 800 chars. **Lift verbatim** when OH1-S3 ships:
   Aura's Telegram + dashboard see "[Subagent summarizer]\n\n<800-char
   result body…>"; the LLM still sees the full announce with the
   summarise-naturally trailer. ~60 LOC, no new dep.

2. **nanobot 5-phase status enum**
   (`SubagentStatus.phase ∈ initializing|awaiting_tools|tools_completed|
   final_response|done|error`). When OH1-S3 spawns a delegate the
   dashboard sees real-time phase transitions, not just "in progress / done".
   Maps cleanly onto Aura's existing tool-card render in
   `web/src/components/ToolCard.tsx` — same widget, new phase enum.

3. **Claude Code background-runs convention**
   (`claude-code.md:280-283`: "When an agent runs in the background, you
   will be automatically notified when it completes — do NOT sleep, poll,
   or proactively check on its progress"). The dashboard equivalent: a
   delegated subagent's "card" stays expanded in the conversation,
   updates in place, and emits a Telegram notification on completion so
   the user knows to come back. Aura's progressive-edit Telegram
   throttle (~600ms) is already wired for this — just needs the
   end-of-spawn boundary marker.

Combined into one Aura OH3 follow-up story (~150 LOC for the dashboard
side + ~30 LOC for the Telegram boundary marker), this gives "the user
sees that delegation happened, knows what it produced, and isn't
confused by a `delegate_summarizer` JSON tool call leaking through".

---

## 5. Concrete OH1 plan deltas

Per-story adjustments distilled from the findings above. Each is small —
no story balloons past the existing OH1 estimates.

| OH1 story | Change | Source | LOC delta |
|---|---|---|---|
| OH1-S0 (discuss-phase) | Name codex config-layer + AOrchestra dynamic-tuple as explicit "considered + rejected" alternatives in `DISCUSS.md` (F1, F6). Capture Swarm-Skills as deferred roadmap pin (F7). | F1, F6, F7 | 0 |
| OH1-S1 (AGENTDEF) | No design change. Stay full-struct TOML. | — | 0 |
| OH1-S2 (TIER) | Use `agent_tier` / `AgentTier` (NOT `tier` / `Tier`) to avoid future namespace collision with subscription-tier work (F8). | F8 | 0 (naming) |
| OH1-S3 (DELEGATE-TOOL) | (a) Add `parent_session_id` column on `conversations` + write linkage on spawn (F2). (b) Add fallback generic `spawn(task)` tool alongside `delegate_<id>` synthesis (F4). (c) Add `max_concurrent_delegates` config (default 1) + status enum (F4). (d) Lift `subagent_announce.md` template + channel scrubber verbatim (F4 + Multi-agent UX §1). (e) Use `mcpdesc.Compose`-style baseline-plus-override description generator for `delegate_<id>` tools (F8 adjacent). (f) Lift four Claude Code guidance lines into AGENT.md ("never delegate understanding", "trust but verify", "brief like a smart colleague", "parallel-dispatch") (F3). | F2 F3 F4 F8 | +~250 (within OH1-S3 budget of 400-600) |
| OH1-S4 (DEDUP) | No change — codex/nanobot/Claude Code all dedup tool specs the same way. | — | 0 |

OH3-S1 REFLECTION-POSTTURN bridge: ship as fork-and-restrict
(F5 pattern) before AGENTDEF lands, then migrate to a proper `reflector`
archetype once OH1 ships. Unblocks OH3 from the OH1 critical path.
Encoding the MEMORY-vs-SKILL split + the "Do NOT capture" allowlist +
the preference order from F5 into the reflection prompt is now a hard
acceptance criterion.

---

## 6. Bottom line

The non-openhuman corpus REINFORCES the openhuman OH1 design. codex
(Anthropic's competitor at Apache-2.0), nanobot (MIT), and Claude Code's
shipped production prompt all converge on the same shape: **named
archetype + per-archetype tool allowlist + sub-loop + parent-doesn't-see-
child-trace**. Two credible alternatives exist (codex config-layer-overlay;
AOrchestra dynamic-tuple) but both lose on Aura's specific axes: small
repeated task surface where named affordances actually steer the chat
tier, and Aura's still-young config-overlay infrastructure. **Stick with
openhuman.** The findings yield small, additive improvements inside the
existing OH1-S3 LOC budget (parent_session_id linkage, generic-spawn
fallback, concurrency cap, status enum, announce template, baseline-
plus-override descriptions) plus four prompt lines lifted from Claude
Code that cost nothing to add and will materially improve OH1 ship
quality. OH3-S1 should ship first as a hermes-agent-style
fork-and-restrict so reflection isn't gated on OH1.
