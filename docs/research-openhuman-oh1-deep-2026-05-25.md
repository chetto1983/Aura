# openhuman → Aura Wave OH1 deep audit (2026-05-25)

Second-pass audit of `D:/tmp/openhuman` (Rust, GPLv3) focused on the three
Wave OH1 patterns: **AGENTDEF** (TOML registry), **TIER** (chat/reasoning/worker
spawn hierarchy + depth gate), **DELEGATE-TOOL** (synthesised `delegate_<id>`
tools per turn). The first lift
(`docs/openhuman-pattern-lift-2026-05-25.md`) sampled headlines; this audit
inspects implementation details the OH1 plan must encode before Codex
touches a single Go file.

All file/line citations are read-only breadcrumbs. **No openhuman code is
quoted verbatim beyond the 5-line cap.** Aura's Go port is clean-room from
these descriptions.

Sources audited (deep read, not sampled):

- `src/openhuman/agent/harness/definition.rs:1-668`
- `src/openhuman/agent/harness/definition_loader.rs:1-295`
- `src/openhuman/agent/agents/loader.rs:1-991`
- `src/openhuman/agent/harness/spawn_depth_context.rs:1-66`
- `src/openhuman/agent/agents/orchestrator/{agent.toml,prompt.md}`
- `src/openhuman/agent/agents/summarizer/{agent.toml,prompt.md}`
- `src/openhuman/agent/agents/researcher/{agent.toml,prompt.md}` (sampled)
- `src/openhuman/agent/agents/planner/agent.toml`
- `src/openhuman/tools/impl/agent/archetype_delegation.rs:1-138`
- `src/openhuman/tools/impl/agent/skill_delegation.rs:1-355`
- `src/openhuman/tools/impl/agent/dispatch.rs:1-161`
- `src/openhuman/tools/orchestrator_tools.rs:1-160` (synth call site)
- `src/openhuman/agent/harness/subagent_runner/{ops.rs,types.rs}`
- `src/openhuman/agent/harness/session/builder.rs:31-63` (dedup function)
- `src/openhuman/agent/harness/tool_loop.rs:138-170` (dedup call site)

Time-box hit at ~45 min: `harness/session/{builder,turn,runtime}.rs` were
sampled (call sites only); `agent/prompts/connected_identities.rs` was not
read this pass. Both flagged in §6 Open questions.

---

## 1. AGENTDEF — `AgentDefinition` struct, field inventory + defaults

### File / line

- Struct: `src/openhuman/agent/harness/definition.rs:36-214`
- Defaults module: `:495-517`
- Registry: `:534-664`
- Loader: `src/openhuman/agent/harness/definition_loader.rs:23-136`
- Built-in parse: `src/openhuman/agent/agents/loader.rs:248-270`

### Complete field inventory (everything that lands in a TOML file)

| TOML key | Type | Default | Notes |
|---|---|---|---|
| `id` | string (required) | — | Unique archetype id. Slug-shaped (`[a-z_]`). Folder name must match. |
| `when_to_use` | string (required) | — | LLM-visible description for delegation. Becomes the `description()` of the synthesised `delegate_<id>` tool, prefixed with "Use only when direct response/direct tools are insufficient." |
| `display_name` | string | `None` (falls back to `id`) | UI label. Diverges from `delegate_name` on purpose. |
| `[system_prompt]` | enum table | empty inline placeholder | `[system_prompt] inline = "…"` OR `[system_prompt] file = "<rel-path>"`. Built-ins replace this with a function-driven `Dynamic` variant at boot (TOML can't construct `Dynamic`; serde error). User TOMLs that omit it OR leave inline empty are **rejected by the loader** with `bail!("missing system_prompt")` at `definition_loader.rs:108-117`. |
| `omit_identity` | bool | **`true`** | Strip parent's identity section from the rendered prompt. |
| `omit_memory_context` | bool | **`true`** | Strip parent's memory context. |
| `omit_safety_preamble` | bool | **`true`** | Strip global safety preamble. Tests in `agents/loader.rs:427-440` pin `code_executor`/`tool_maker`/`skill_creator` to `false` because they execute code → safety preamble must stay on. |
| `omit_skills_catalog` | bool | **`true`** | Strip skills catalog. |
| `omit_profile` | bool | **`true`** | Strip `PROFILE.md` (onboarding enrichment output). Default `true` = sub-agents stay lean; only `orchestrator` opts in with `false`. |
| `omit_memory_md` | bool | **`true`** | Strip `MEMORY.md` (archivist-curated long-term memory). **KV-cache contract documented at `definition.rs:84-91`**: archivist writes that land mid-session do NOT retroactively update the in-flight prompt; picked up only on next session. This invariant matters for Aura because Aura's wiki page is also "frozen per session" by analogy. |
| `[model]` | enum table | `Inherit` (parent's model) | `[model] hint = "chat"` (or `reasoning`, `agentic`, `summarization`), OR `[model] exact = "model-name"`, OR omit → `Inherit`. Hint resolves via `format!("{hint}-v1")` at `definition.rs:429-435` unless a `RouterProvider` overrides. |
| `temperature` | f64 | **`0.4`** | `defaults::subagent_temperature`. |
| `[tools]` | enum table | `Wildcard` (= all parent tools) | `[tools] wildcard = {}` OR `[tools] named = ["a", "b"]`. **Empty `named = []` means zero tools**, NOT wildcard — see `trigger_triage` test at `loader.rs:316-334`. |
| `disallowed_tools` | `Vec<String>` | `[]` | Explicit blocklist applied after `tools` scope. Used by `tools_agent` to disallow `polymarket`/`kalshi` so they route exclusively through `markets_agent`. |
| `skill_filter` | `Option<String>` | `None` | Restricts to tools whose name starts with `{skill}__` (note: double underscore). |
| `extra_tools` | `Vec<String>` | `[]` | "Also include these by name" hook. Subject to `disallowed_tools`. Vestigial — used to be a bypass for a removed `category_filter`. Aura can probably skip this field on day 1. |
| `max_iterations` | usize | **`8`** | Per-spawn cap. Orchestrator is `15`, summarizer is `1`, archivist is `3`, trigger_triage is `2`. |
| `max_result_chars` | `Option<usize>` | `None` (no cap) | Truncation cap applied AFTER the sub-agent returns, BEFORE the result is handed back to the parent as a tool_result. Researcher/planner set `8000`. Char-count not byte-count; `ops.rs:289-308` finds the cap-th `char_indices` offset to avoid splitting a multi-byte rune, then appends `"\n[...truncated]"`. |
| `timeout_secs` | `Option<u64>` | `None` | Wall-clock cap. Not consumed by `run_subagent` in the file I read — likely consumed at a higher layer; flagged as Open Question. |
| `sandbox_mode` | enum | **`None`** | `none` / `read_only` / `sandboxed`. `read_only` mode is the gate that makes `composio_execute` reject Write/Admin slugs (`code_executor` keeps `Sandboxed`, `planner`/`critic`/`morning_briefing` use `ReadOnly`). Installed as task-local `with_current_sandbox_mode` for the whole sub-agent run (`ops.rs:271-283`). |
| `background` | bool | `false` | Marker for async-spawned agents. Only `archivist = true` today. Doesn't appear to gate anything in the loop — pure metadata. |
| `subagents` | `Vec<SubagentEntry>` | `[]` | The delegation surface — see §3. |
| `delegate_name` | `Option<String>` | `None` (defaults to `delegate_{id}`) | Tool-name override. Researcher uses `"research"` so the synthesised tool becomes `delegate_research`. Markets_agent uses `"do_prediction_markets"` → `delegate_do_prediction_markets`. |
| `agent_tier` | enum | **`Worker`** (least powerful, by `#[derive(Default)]` on the enum) | `chat` / `reasoning` / `worker`. Default = `worker` is **load-bearing**: missing tier on a user override defaults to leaf-executor, which is the safest fail-closed behaviour. |
| `source` | enum (skip serde) | `Builtin` | Stamped at load time. `Builtin` for `loader.rs::parse_builtin`, `File(PathBuf)` for `definition_loader.rs::load_file`. Used by `agent::list_definitions` RPC. |

### Validation rules

**Loader-time** (`definition_loader.rs:103-118`):

- Bail on parse error (TOML syntax / serde shape mismatch) — but only for the
  single file; sibling files keep loading. **Loader is lenient by design**: a
  broken specialist never breaks boot, only its own load fails (warn log).
- Bail on empty `system_prompt` for file-loaded definitions. Built-ins
  bypass this because their TOMLs intentionally ship without `system_prompt`
  (the dynamic prompt builder is injected at `loader.rs:255-256`).

**Registry-time** (`definition.rs:559-586` — `AgentDefinitionRegistry::load`):

- Same-id overrides: user TOML beats built-in **silently** (`insert` replaces
  in `by_id` HashMap; logged at info level). No "blocked by built-in"
  protection — by design, the user can shadow `code_executor` with their own.
- After every user-TOML override merge, **re-runs `validate_tier_hierarchy`**
  on the full merged set. A user TOML that breaks the tier contract aborts
  the registry build with an `anyhow!` chained error. This is critical —
  the validator runs TWICE (once on builtins at boot, again after user
  overrides). Aura must mirror this.

**Tier validation** (`agents/loader.rs:189-245` — `validate_tier_hierarchy`):

- Iterates every definition; for each entry in `def.subagents`:
  - If entry is `SubagentEntry::Skills(_)` → **skip the check entirely**
    (skill wildcards always route to `integrations_agent`, a worker, via a
    single collapsed delegation tool — not subject to tier-mismatch).
  - If parent tier is `Worker` and entry is any `AgentId(_)` →
    **bail** ("workers are leaf executors").
  - Resolve child by id; if unknown → **continue (don't bail)**. Unknown
    ids are a separate concern handled by runtime spawn resolution and
    existing `subagents` integrity tests. Don't mask it as a tier error.
  - If `(parent, child)` is `(Chat, Chat)` → bail.
  - If `(parent, child)` is `(Reasoning, Reasoning)` → bail.
  - All other combos pass.

Note what's **NOT** validated:

- Cycle detection beyond same-tier (Worker → Chat → Worker would pass, the
  spawn-depth gate is the only guard against runtime cycles).
- Tool-name collisions across delegate names + skill tools (caught at
  runtime by dedup at `session/builder.rs:44`).
- TOML positional gotcha (`subagents = [...]` must come before any `[table]`
  header — once a table opens, top-level keys are consumed by it). This is a
  TOML-spec gotcha, not a validation; surfaces as "unexpected enum variant
  in ToolScope" parse error. **Aura adaptation**: this gotcha is irrelevant
  if we use JSON (Aura already uses `mcp.json`); TOML's positional parsing
  is the trade-off for consistency with openhuman. The OH1 plan currently
  says "TOML for consistency with openhuman lift" — reconsider, JSON sidesteps
  this entire class of authoring footgun.

### Boot vs runtime loading

- Boot: `AgentDefinitionRegistry::init_global(workspace)` at
  `definition.rs:635-650`. Uses `OnceLock` — subsequent calls are no-ops.
  Reload mechanism is named (`reload_global`) but I didn't read its body
  this pass — likely re-builds and atomic-swaps. Aura would need an
  equivalent for hot-reload of `agents/*.json` if we want runtime reload
  without restart (probably out of scope for OH1-S1).
- `builtins_only()` for tests: doesn't load any workspace files. Aura's
  equivalent will be a `NewBuiltinsRegistry()` constructor.

### Aura adaptation note (Rust → Go)

| Openhuman idiom | Aura Go analog |
|---|---|
| `#[derive(Deserialize)] struct AgentDefinition` | `type AgentDefinition struct {…}` with `toml:"…"` (or `json:"…"`) struct tags. Use `github.com/pelletier/go-toml/v2` (already pulled in by `aura-init-models`? — verify; if not, JSON wins). |
| `#[serde(default = "defaults::true_")]` | Go has no built-in field defaults. Use a `(def *AgentDefinition) ApplyDefaults()` method called immediately after unmarshal in `loader.go`. **CRITICAL**: openhuman's defaults for `omit_*` are all `true` — Aura must replicate this exactly or sub-agents will inherit the parent's full prompt and double the token cost. |
| `#[serde(untagged)] enum SubagentEntry { AgentId(String), Skills(SkillsWildcard) }` | Go has no untagged enums. Two options: (a) custom `UnmarshalJSON/UnmarshalTOML` that tries string-first, then table; (b) ship as a struct with one of two fields populated (`ID *string` or `Skills *string`). Option (a) matches openhuman semantics; option (b) is more idiomatic Go but changes the TOML shape. Pick (a). |
| `tokio::task_local!` for `CURRENT_SPAWN_DEPTH` | Go `context.Context` with a typed key. `context.WithValue(ctx, spawnDepthKey{}, depth+1)`. Read via `CurrentSpawnDepth(ctx) int`. **No global state** — depth is per-request, which is what we want. Aura already plumbs `context.Context` through `agent.Run` so this is a one-line addition. |
| `OnceLock<AgentDefinitionRegistry>` global singleton | `sync.Once` + package-level `var globalRegistry *AgentDefinitionRegistry`. Or: avoid the singleton entirely by passing the registry through `runtime.Config`. **Recommended**: pass through Config — singletons make testing harder and Aura already injects runtime config everywhere. |
| `anyhow::bail!("…")` | `fmt.Errorf("…")` or `errors.New(…)`. Wrap with `%w` for the chain. |
| `tracing::warn!` / `tracing::info!` | `zap` via `internal/logging.Default()`. Use field-style logging not Printf. |

### Integration friction with Aura's existing `internal/swarm/`

Aura's swarm package already has `NodeSpec` (`internal/swarm/nodespec.go:11-22`)
with fields `Goal/Instruction/ToolAllowlist/MaxIterations/MaxToolCalls/
BudgetSecs/OutputSchema/RiskTier/ParentRunID/AssignmentID`. **NodeSpec is a
per-dispatch unit, not an archetype** — it's openhuman's `SubagentRunOptions`,
not `AgentDefinition`. The OH1 plan correctly says "extends not replaces"
but the friction points are:

1. **`RiskTier` enum overlap with `AgentTier`**: `swarm.RiskTier` is
   `read_only|write_proposal` (data-flow risk); openhuman's `agent_tier` is
   `chat|reasoning|worker` (capability tier). Different axes — both should
   coexist. Recommend naming the new field `Tier` in `AgentDefinition` and
   keeping `RiskTier` on `NodeSpec` to make the distinction obvious. **Do
   NOT collapse them.**
2. **`ToolAllowlist []string` already exists on `NodeSpec` and
   `Assignment`** (`internal/swarm/types.go:46,70`). Openhuman's
   `tools.named[]` should map to the SAME field — when an archetype is
   dispatched via swarm.Manager, the archetype's `Tools.Named` becomes
   the `Assignment.ToolAllowlist`. This is the integration seam.
3. **`MaxIterations` exists on both** — `NodeSpec.MaxIterations` (cap 10)
   and `AgentDefinition.max_iterations` (default 8). Resolution rule:
   per-spawn `NodeSpec.MaxIterations` overrides `AgentDefinition.MaxIterations`
   when non-zero. Mirror openhuman's `SubagentRunOptions` semantics.
4. **`Manager.maxDepth` default = 1** (`manager.go:17`) vs openhuman's
   `MAX_SPAWN_DEPTH = 3`. The OH1 plan says "depth cap fires at 3" but
   Aura's existing default is 1. **Pick a number and document it** — bumping
   from 1 to 3 is a behavioural change that should be in the OH1-S2 commit
   body, not silent.
5. **`swarm.Manager` runs in-process via `AgentRunner` interface** — there
   is no equivalent to openhuman's `current_parent()` task-local + event-bus
   `SubagentSpawned` events. The OH1-S3 DELEGATE-TOOL implementation must
   decide whether to (a) reuse swarm.Manager as the dispatch substrate
   (recommended: less code, atomic with existing depth/concurrency tracking)
   or (b) build a parallel dispatch path in `internal/agent/agentdef/`.
   Recommend (a) — wire `delegate_<id>` tools to `swarm.Manager.Run` with
   a single-element `Assignments` slice.

---

## 2. AGENTDEF — Definition loader behaviour

### File / line

- `src/openhuman/agent/harness/definition_loader.rs:1-136`

### File discovery order

Order matters for "last-write wins" overrides; openhuman's order is:

1. `<workspace>/agents/*.toml` — workspace-local, loaded FIRST.
2. `~/.openhuman/agents/*.toml` (or `$OPENHUMAN_HOME/agents/*.toml` if set)
   — user-global fallback, loaded SECOND **only if the directory wasn't
   already loaded** (the `seen_dirs` Vec at `:25` prevents double-load when
   workspace == home).
3. Loader returns flat `Vec<AgentDefinition>`. **Custom overrides built-ins**
   later in `AgentDefinitionRegistry::load` (`definition.rs:559-586`) by
   re-inserting into the HashMap (replaces in place, doesn't grow `order`).

So precedence is: **user-global → workspace → built-in**, with
**later-loaded wins**. Workspace beats home-global because workspace is
loaded earlier into the Vec, but then `reg.insert(def)` is called per-entry
in registry order, and the LAST insert wins because HashMap. Actually no —
re-reading `:561-569`: the iteration is `for def in custom`, where `custom`
is the workspace's flat Vec (which includes BOTH dirs in load order:
workspace first, then home). So **the LAST file in the iteration wins on
id collision**, which means **home overrides workspace if both have the
same id**. This is counter-intuitive — Aura should document explicitly
which level wins.

**Aura adaptation**: probably want **project > user > home > builtin** with
explicit precedence, not load-order-dependent. Pick: pass an ordered slice
of dirs into the loader, document it, write a test that pins the order.

### Validation error format

- TOML parse errors: `toml::from_str` returns a `de::Error` that **includes
  line + column** via `serde::de::Error::custom`. The `with_context!` at
  `:106-107` adds the file path. So a malformed TOML produces something
  like `parsing /path/to/notion.toml as AgentDefinition TOML: invalid type:
  string "foo", expected struct AgentDefinition at line 3 column 5`.
  Aura's `pelletier/go-toml/v2` produces equivalent line+col errors.
- Per-file errors are **logged warn, skipped, not bubbled** (`:81-87`).
  This is the lenient-loader contract. The whole `load_from_workspace`
  function only returns `Err` on I/O errors reading the directory itself.
- Empty-prompt rejection is a `bail!` (returned `Err`), caught by the same
  warn-and-skip path in `load_dir`.

### Override resolution rule (re-stated for clarity)

- Within the file system: alphabetical (whatever `fs::read_dir` returns —
  on most platforms order is unspecified, **Aura must sort deterministically**
  to avoid different boot results on Linux vs Windows).
- Within the workspace: each file is `push`ed onto the Vec in iteration
  order.
- Workspace dir + home dir: workspace iterated first, home second.
- Built-ins vs custom: custom overrides built-in on id collision.
- Across custom files: **last file wins** because of HashMap insert
  semantics — but this is fragile and undocumented. **Aura must improve on
  this**: fail-hard on duplicate ids across multiple custom files (force
  the user to rename), or document the precedence explicitly.

### Aura adaptation note

- Sort files deterministically before iteration: `sort.Strings(filenames)`.
- Consider failing on duplicate id within the same scope (workspace OR
  home), but allowing scope-level override (home id can override workspace
  id intentionally).
- Mirror the lenient-per-file behaviour — one broken `agents/foo.json`
  must not block boot.

### Integration friction

None — `internal/swarm/` doesn't have a TOML/JSON registry today.
`internal/skills/` does have a discovery pattern (filesystem walk + JSON
parse) — mirror its directory-walk + lenient-error idiom.

---

## 3. TIER + DEPTH GATE — `AgentTier` enum + `validate_tier_hierarchy` +
spawn-depth task-local

### Files / lines

- Enum: `definition.rs:235-265` (`AgentTier::{Chat, Reasoning, Worker}`,
  `#[default] Worker`, `as_str()` for error messages).
- Validator: `agents/loader.rs:189-245`.
- Depth task-local: `harness/spawn_depth_context.rs:1-66` (`MAX_SPAWN_DEPTH
  = 3`, `current_spawn_depth() -> usize`, `with_spawn_depth(depth, future)`).
- Depth firing point: `harness/subagent_runner/ops.rs:232-248` — gate
  `attempted_depth > MAX_SPAWN_DEPTH` returns `SubagentRunError::SpawnDepthExceeded`
  **before** any work happens.

### Exact contract enforced

Validation rules (boot-time, hard-fail):

- `Worker` parent + any `AgentId` entry in `subagents` → bail. (Skill
  wildcards exempt.)
- `Chat` parent + `Chat` child → bail.
- `Reasoning` parent + `Reasoning` child → bail.
- `Chat` + `Reasoning` → allowed.
- `Chat` + `Worker` → allowed (fast path).
- `Reasoning` + `Worker` → allowed.
- Unknown child id → continue (delegated to runtime).

Runtime rules:

- Depth counter increments per `run_subagent` call (`ops.rs:233`).
- `attempted_depth > MAX_SPAWN_DEPTH (3)` aborts the spawn before any LLM
  call. So chains can go up to depth 3 inclusive: chat (turn) → reasoning
  (depth 1) → worker (depth 2) → worker (depth 3) is the deepest legal
  chain. Note: depth counts SUB-AGENT calls, not the root turn — depth 0
  is the root user-facing agent.
- Depth is installed via `with_spawn_depth(attempted_depth, async { … })`
  wrapping `run_typed_mode`, so the tokio task-local naturally propagates
  through every awaited sub-call.

### Defaults that matter

- `AgentTier::default() = Worker` — load-bearing fail-closed default. A user
  TOML that drops `agent_tier` becomes a worker (leaf). Combined with the
  validator's "worker can't have subagents" rule, this means user TOMLs
  that ship subagents WITHOUT declaring a tier **fail the registry build**.
  Good UX — forces explicit tier declaration when needed.

### Aura adaptation note

- Go enum: `type AgentTier int` with `const ( TierChat / TierReasoning / TierWorker )`
  + `(t AgentTier) String() string` + a custom `UnmarshalTOML` that maps
  `"chat"/"reasoning"/"worker"` strings to the enum (zero-value = Worker).
- Depth via `context.Context`:

  ```go
  type spawnDepthKey struct{}
  func CurrentSpawnDepth(ctx context.Context) int {
      if v, ok := ctx.Value(spawnDepthKey{}).(int); ok { return v }
      return 0
  }
  func WithSpawnDepth(ctx context.Context, d int) context.Context {
      return context.WithValue(ctx, spawnDepthKey{}, d)
  }
  ```

- Depth gate fires in the equivalent of `run_subagent` — for Aura this is
  inside `swarm.Manager.Run` OR a new wrapper at the dispatch site of the
  synthesised `delegate_<id>` tool. **The depth gate MUST fire before
  swarm.Manager creates any DB rows** — otherwise the cap-violated dispatch
  leaks a partially-initialised task into the store.

### Integration friction with `internal/swarm/`

- Existing `Manager.maxDepth = 1` (`manager.go:17`) caps at 1 globally. The
  OH1 plan must either:
  - **Bump default to 3** to match openhuman semantics. Backwards-incompat
    for anyone relying on the 1-level cap.
  - **Add a per-archetype depth cap that overrides the global**. More
    surface area but additive.
- Recommend: bump default to 3, expose `MaxDepth int` on `ManagerConfig`
  (already done), keep the depth-cap gate in the swarm.Manager dispatch
  path so both the synthesised delegate tool AND the existing swarm tools
  enforce the same cap. **Single source of truth**.
- The `Task.Depth int` field already exists at `internal/swarm/types.go:48`
  — wire the spawn-depth context value into that field at task creation.

---

## 4. DELEGATE-TOOL — synthesised `delegate_<id>` tools per turn

### Files / lines

- Tool struct: `tools/impl/agent/archetype_delegation.rs:6-77`
- Dispatch helper: `tools/impl/agent/dispatch.rs:10-121`
- Skill-wildcard collapsed tool: `tools/impl/agent/skill_delegation.rs:1-207`
- Synthesis call site: `tools/orchestrator_tools.rs:76-160` (`collect_orchestrator_tools`)
- Per-turn manifest build: `harness/tool_loop.rs:138-170`
- Dedup function: `harness/session/builder.rs:31-63` + sub-agent assembly's
  own dedup at `subagent_runner/ops.rs:329-365`

### Schema of a synthesised `delegate_<id>` tool

The first lift summarised this as `{task: string}`. **It's actually
`{prompt: string, model?: string}`** (`archetype_delegation.rs:22-37`):

- `prompt` (required, string): "Clear instruction for what to do. Include
  all relevant context — the sub-agent has no memory of your conversation."
- `model` (optional, string): per-call exact model override.

Aura should mirror BOTH fields — `prompt` (not `task`) and the optional
`model`. The `model` override hits `dispatch.rs:62-67` then `:86` to
populate `SubagentRunOptions::model_override`. Useful for "delegate this
sub-task to a stronger model just this once" without editing the agent
TOML.

Permission/category:

- `permission_level() = Execute`
- `category() = System`

### Tool name resolution

- Default: `format!("delegate_{}", target.id)` (`orchestrator_tools.rs:117-119`).
- Override: `target.delegate_name` if set (e.g. researcher's `"research"` →
  `delegate_research`, markets_agent's `"do_prediction_markets"` →
  `delegate_do_prediction_markets`).
- **Description prefix is always**: `"Use only when direct response/direct
  tools are insufficient. {when_to_use}"` (`orchestrator_tools.rs:125-128`).
  This is the "direct-first bias" baked into the tool's LLM-visible
  description — every synthesised delegate tool nags the LLM to try direct
  tools first. Aura must replicate this exact prefix (or write its own with
  the same effect) — it's the SINGLE biggest behavioural lever openhuman
  uses to stop the chat tier from over-delegating.

### Skill-wildcard collapsed delegation tool

- `SubagentEntry::Skills(SkillsWildcard{ skills: "*" })` collapses to a SINGLE
  `delegate_to_integrations_agent` tool whose `toolkit` arg is an `enum`
  over connected toolkit slugs (`skill_delegation.rs:93-115`).
- Returns `None` (no tool synthesised) when zero toolkits connected
  (`:50-53`). The orchestrator schema literally has no `delegate_to_integrations_agent`
  when the user has no integrations.
- Tool description enumerates connected toolkits inline (`:63-81`) — so
  the LLM sees the list of available integrations in the tool description,
  not as a separate prompt section.
- Aura analog: not directly applicable — Aura's MCP servers are the
  equivalent of Composio toolkits, but MCP tools surface as individual
  `mcp_<server>_<tool>` entries today. The collapsed delegation pattern
  could be a future optimization but is NOT a Wave OH1 requirement.

### Synthesis call site — where + when

- `collect_orchestrator_tools` is called from
  `harness/session/builder` at agent-build time, with the orchestrator's
  own definition + the global registry + the current list of connected
  Composio integrations (`orchestrator_tools.rs:24-27`).
- For each `SubagentEntry::AgentId(id)`:
  - Skip `id == "summarizer"` — runtime-only, never exposed to the LLM
    (`orchestrator_tools.rs:100-107`). **Aura needs this skip-list pattern**
    for any runtime-only agent (e.g. a future Aura summarizer or page-rewriter
    that the model must not call directly).
  - Lookup target in registry; unknown id → warn + skip, orchestrator still
    builds (`:108-114`).
  - Resolve tool name (delegate_name override or default).
  - Build `ArchetypeDelegationTool` and `Box<dyn Tool>` it into the result.

### Sub-loop execution semantics

When the LLM calls a synthesised `delegate_<id>` tool:

1. `ArchetypeDelegationTool::execute(args)` (`archetype_delegation.rs:47-77`):
   - Trim & validate `prompt` is non-empty → tool_error if blank.
   - Extract optional `model` override.
   - Call `dispatch_subagent(agent_id, tool_name, prompt, None, model_override)`.
2. `dispatch_subagent` (`dispatch.rs:10-121`):
   - Resolves agent definition from global registry.
   - Generates a UUID `sub-<uuid>` task_id.
   - Publishes `DomainEvent::SubagentSpawned` to event bus (telemetry +
     channel-bridge to frontend).
   - Optionally pushes `AgentProgress::SubagentSpawned` to the parent's
     `on_progress` mpsc sink (for live web UI updates).
   - Calls `run_subagent(definition, prompt, options)`.
3. `run_subagent` (`subagent_runner/ops.rs:221-323`):
   - Spawn-depth gate: `current + 1 > MAX_SPAWN_DEPTH` → bail with
     `SpawnDepthExceeded`.
   - Wraps the sub-loop in TWO task-locals: `with_spawn_depth(attempted)`
     + `with_current_sandbox_mode(def.sandbox_mode)` (sub-agent's sandbox
     overrides parent's during the sub-loop; restored on return).
   - Box-pins the inner state machine (stack-overflow workaround under
     coverage instrumentation — issue #2234). Aura's Go equivalent
     doesn't have this concern, goroutines start with small stacks that
     grow.
   - Calls `run_typed_mode` which builds the narrow sub-agent prompt,
     filters tools to `tools.Named` (subject to `disallowed_tools`),
     dedupes specs by name (separate path from main-agent dedup), runs
     the LLM loop up to `def.max_iterations` calls.
   - **Truncates output to `def.max_result_chars`** (char-count, not byte;
     find the cap-th `char_indices` offset, truncate, append
     `"\n[...truncated]"`).
   - Returns `SubagentRunOutcome { task_id, agent_id, output, iterations,
     elapsed, mode }`.
4. Result → `ToolResult::success(outcome.output)` back to the parent
   model as the synthesised tool's tool_result.

**Critical: the parent never sees the child's tool calls.** Only the
final `output` string. This is how openhuman keeps the orchestrator's
context small even when a worker did 8 web fetches.

### Error handling

- Child runner failure → `ToolResult::error(format!("{tool_name} failed:
  {message}"))` + publishes `SubagentFailed` event (`dispatch.rs:110-119`).
  The parent LLM sees a tool_result that's flagged as error and can
  decide to retry, route elsewhere, or escalate to the user.
- Max-iter exceeded inside the child → `SubagentRunError::MaxIterationsExceeded(n)`
  → bubbles up as the same error tool_result. The orchestrator's prompt
  explicitly handles this: "If a sub-agent fails after retries, explain
  what happened clearly."

### Aura adaptation note

- Synthesised tool struct: `type DelegateTool struct { Name, AgentID,
  Description string; Manager swarm.RunRunner; Registry *AgentDefinitionRegistry }`.
- Implements `tools.Tool` (or whatever the equivalent interface is in
  `internal/agent/tools/registry/`).
- `Parameters()` returns:

  ```go
  map[string]any{
      "type": "object", "required": []string{"prompt"},
      "properties": map[string]any{
          "prompt": map[string]any{"type": "string", "description": "…"},
          "model":  map[string]any{"type": "string", "description": "Optional model override…"},
      },
  }
  ```

- `Execute(ctx, args)` validates prompt, increments depth, calls
  `swarm.Manager.Run` with a single-element `Assignments` slice whose
  `SystemPrompt` = rendered agent prompt, `Prompt` = the LLM-provided
  `prompt`, `ToolAllowlist` = archetype's tools.Named, `MaxToolCalls` =
  archetype's max_iterations.
- After completion: apply `MaxResultChars` truncation (use Go `[]rune`
  conversion for char-count semantics).
- Description prefix: hard-code the "Use only when direct response/direct
  tools are insufficient. " prefix per archetype. This is the load-bearing
  "direct-first bias" string.
- Skip-list pattern: define a `runtimeOnlyArchetypes = map[string]bool{}`
  and skip those at synthesis time. Day 1 it's empty; future runtime-only
  archetypes (e.g. an Aura `payload_summarizer` archetype) add themselves.

### Integration friction with `internal/swarm/`

- **Massive overlap**: `swarm.Manager.Run` already does most of what
  openhuman's `run_subagent` does — spawns a child via `AgentRunner`,
  applies `ToolAllowlist`, tracks `Depth`, persists `Task` to store.
- The ADDITIONAL pieces openhuman has that Aura's swarm doesn't:
  - Per-spawn `model_override`: Aura's `Assignment` has no model override
    field today. Add `ModelOverride *string` or similar.
  - `max_result_chars` truncation post-completion: Aura's `Assignment`
    has `MaxToolResultChars int` which is per-tool, not for the
    sub-agent's FINAL output. Add a sub-agent-output-cap field.
  - The `omit_*` sections of the system prompt rendering: completely
    new for Aura — none of the swarm code touches prompt assembly today.
    This belongs in `internal/agent/agentdef/render.go` (new file).
  - DomainEvent emission for telemetry: Aura uses zap structured logs,
    not an event bus. Probably skip; revisit if/when we need
    real-time UI updates of sub-agent progress.

---

## 5. DEDUP-VISIBLE-TOOL-SPECS — `dedup_visible_tool_specs`

### Files / lines

- Function: `harness/session/builder.rs:31-63` (`pub(crate) fn
  dedup_visible_tool_specs(specs: Vec<ToolSpec>) -> Vec<ToolSpec>`).
- Sub-agent assembly mirror: `harness/subagent_runner/ops.rs:329-365`
  (`fn dedup_tool_specs_by_name(agent_id, specs)`).
- Call site: `harness/tool_loop.rs:138-170`.

### Dedup key + conflict policy

- Key: **`spec.name` only** (not name+description hash). HashMap `<String>`
  insert; first-occurrence wins (`:48-54`).
- Dropped specs are logged at `warn` level (main path, `:55-60`) or
  `debug` level (sub-agent path, `:357-363`) with the dropped names.
- Sub-agent path additionally tracks `agent_id` in the log line.

### Why two implementations?

- Main path dedups across `tools_registry + extra_tools` for the orchestrator
  turn (`tool_loop.rs:146-153`).
- Sub-agent path runs INSIDE the sub-loop and dedupes across `dynamic_tools
  + parent.all_tool_specs[allowed_indices]` (`ops.rs:868-895`). Note ops.rs
  calls `dedup_visible_tool_specs` AND THEN `dedup_tool_specs_by_name`
  sequentially (`:891` + `:895`) — defence in depth, both paths are
  idempotent so running both is cheap.

### Trigger conditions

- Strict providers (Anthropic, openhuman cloud) **HTTP-400** on duplicate
  tool names. OpenAI silently accepts. Bug originally hidden until #1710's
  per-role routing started fanning to Anthropic.
- Common collision sources:
  - Researcher's `delegate_name = "research"` shadowing a same-named
    skill tool.
  - Dynamic Composio action tools (e.g. `GMAIL_SEND_EMAIL`) colliding
    with parent registry entries when the agent's `AllowedAll` scope
    includes a same-named skill tool.
- Aura today is at low risk because: (a) tool name space is small (~14
  action-enum tools), (b) MCP tools are namespaced `mcp_<server>_<tool>`
  which avoids most collisions. The risk LANDS the moment Wave OH1-S3
  ships `delegate_<id>` synthesis, because user TOMLs can set
  `delegate_name` to anything — including the name of an existing action
  tool. **The dedup must ship together with OH1-S3, not after**, otherwise
  the first user who names a delegate `wiki_page` will get a silent
  shadowing bug.

### Aura adaptation note

- Single function: `func DedupToolsByName(specs []tools.ToolDefinition)
  []tools.ToolDefinition` in `internal/agent/tools/registry/manifest.go`
  (or wherever the manifest is assembled).
- Use `map[string]struct{}` as the seen-set; iterate in input order;
  log at `WARN` level with the dropped names list.
- **Behavioural choice**: openhuman keeps the FIRST occurrence. Aura
  should do the same to preserve the orchestrator-tools synthesis order
  (delegate tools added FIRST take priority over MCP-late-arrivals with
  the same name).
- Add a single test that mirrors openhuman's: input `[a, b, a, c, b]` →
  output `[a, b, c]`.

### Integration friction

None — no existing dedup in Aura's manifest path today. The Wave OH1 plan
(OH1-S4, ~30 LOC) is sized correctly. Implementation is mechanical.

---

## 6. Concrete file/line table for the Go port

| openhuman field/concept | Aura Go field/file |
|---|---|
| `AgentDefinition` struct | `internal/agent/agentdef/definition.go::AgentDefinition` |
| `AgentDefinition::id` | `AgentDefinition.ID string` (json/toml: `"id"`) |
| `AgentDefinition::when_to_use` | `AgentDefinition.WhenToUse string` |
| `AgentDefinition::display_name` | `AgentDefinition.DisplayName string` (omitempty) |
| `AgentDefinition::system_prompt` (enum) | `AgentDefinition.SystemPrompt PromptSource` with `Inline string` OR `File string` (one populated) |
| `AgentDefinition::omit_identity/memory_context/safety_preamble/skills_catalog/profile/memory_md` | `OmitIdentity/OmitMemoryContext/OmitSafetyPreamble/OmitSkillsCatalog/OmitProfile/OmitMemoryMD bool` (all default `true` via ApplyDefaults) |
| `AgentDefinition::model` (enum) | `AgentDefinition.Model ModelSpec` with `Inherit bool` / `Exact string` / `Hint string` |
| `AgentDefinition::temperature` | `AgentDefinition.Temperature float64` (default 0.4) |
| `AgentDefinition::tools` (enum) | `AgentDefinition.Tools ToolScope` with `Wildcard bool` / `Named []string` |
| `AgentDefinition::disallowed_tools` | `AgentDefinition.DisallowedTools []string` |
| `AgentDefinition::skill_filter` | `AgentDefinition.SkillFilter string` (omitempty) |
| `AgentDefinition::extra_tools` | SKIP for Aura day 1 (vestigial in openhuman) |
| `AgentDefinition::max_iterations` | `AgentDefinition.MaxIterations int` (default 8) |
| `AgentDefinition::max_result_chars` | `AgentDefinition.MaxResultChars int` (0 = no cap) |
| `AgentDefinition::timeout_secs` | `AgentDefinition.TimeoutSecs int` (0 = no cap) |
| `AgentDefinition::sandbox_mode` | `AgentDefinition.SandboxMode SandboxMode` (None / ReadOnly / Sandboxed) |
| `AgentDefinition::background` | SKIP for Aura day 1 (pure metadata in openhuman) |
| `AgentDefinition::subagents` | `AgentDefinition.Subagents []SubagentEntry` (custom UnmarshalJSON for untagged sum) |
| `AgentDefinition::delegate_name` | `AgentDefinition.DelegateName string` (omitempty; default = "delegate_" + ID) |
| `AgentDefinition::agent_tier` | `AgentDefinition.Tier AgentTier` (default Worker — critical fail-closed default) |
| `AgentDefinition::source` | `AgentDefinition.Source DefinitionSource` with `Builtin bool` / `File string` |
| `SubagentEntry::AgentId(String)` | `SubagentEntry.ID string` (mutually exclusive with Skills) |
| `SubagentEntry::Skills(SkillsWildcard)` | `SubagentEntry.Skills string` ("*" only initially) |
| `ModelSpec::Inherit` (default) | `ModelSpec{}` zero-value resolves to "inherit" |
| `ModelSpec::Exact(name)` | `ModelSpec.Exact string` |
| `ModelSpec::Hint(hint)` | `ModelSpec.Hint string`, `Resolve(parent) string` returns `hint + "-v1"` |
| `ToolScope::Wildcard` (default) | `ToolScope{Wildcard: true}` |
| `ToolScope::Named(Vec<String>)` | `ToolScope{Named: []string{...}}` (mutually exclusive) |
| `SandboxMode::{None, ReadOnly, Sandboxed}` | `SandboxMode int` with `SandboxNone / SandboxReadOnly / SandboxSandboxed` consts |
| `AgentTier::{Chat, Reasoning, Worker}` | `AgentTier int` with `TierChat / TierReasoning / TierWorker` consts; `String()` for error msgs; `UnmarshalText` for TOML/JSON |
| `AgentDefinitionRegistry` | `internal/agent/agentdef/registry.go::Registry` (drop the global singleton; pass through Config) |
| `Registry::builtins_only()` | `NewBuiltinsRegistry() *Registry` |
| `Registry::load(workspace)` | `Load(workspace string, opts LoadOptions) (*Registry, error)` |
| `Registry::insert(def)` | `(r *Registry) Insert(def AgentDefinition)` |
| `Registry::get(id)` | `(r *Registry) Get(id string) (AgentDefinition, bool)` |
| `Registry::list()` | `(r *Registry) List() []AgentDefinition` (insertion order) |
| `load_from_workspace(path)` | `internal/agent/agentdef/loader.go::LoadFromDirs(dirs ...string) ([]AgentDefinition, error)` |
| `load_file(path)` | `LoadFile(path string) (AgentDefinition, error)` |
| `validate_tier_hierarchy(defs)` | `ValidateTierHierarchy(defs []AgentDefinition) error` |
| `MAX_SPAWN_DEPTH = 3` | `internal/agent/agentdef/depth.go::MaxSpawnDepth = 3` (consider making this `swarm.Manager.maxDepth` to avoid two constants) |
| `current_spawn_depth()` | `CurrentSpawnDepth(ctx context.Context) int` |
| `with_spawn_depth(depth, future)` | `WithSpawnDepth(ctx context.Context, depth int) context.Context` |
| `SpawnDepthExceeded { attempted, max }` | `ErrSpawnDepthExceeded` sentinel + wrap with attempted/max in message |
| `ArchetypeDelegationTool` | `internal/agent/tools/registry/delegate.go::DelegateTool` |
| `dispatch_subagent(...)` | `(t *DelegateTool) Execute(ctx, args)` → routes via `swarm.Manager.Run` |
| `SkillDelegationTool` | SKIP for Wave OH1 (MCP not consolidated); revisit when MCP tool count >50 |
| `collect_orchestrator_tools(def, registry, integrations)` | `internal/agent/agentdef/synth.go::SynthesiseDelegateTools(def AgentDefinition, registry *Registry) []tools.Tool` (no integrations arg day 1) |
| `dedup_visible_tool_specs(specs)` | `internal/agent/tools/registry/manifest.go::DedupToolsByName(specs []tools.ToolDefinition) []tools.ToolDefinition` |
| `SubagentRunOptions::model_override` | `swarm.Assignment.ModelOverride *string` (new field) |
| `SubagentRunOptions::task_id` | already `swarm.Task.ID` |
| `def.max_result_chars` post-completion truncation | inside swarm.Manager.Run wrapper, or DelegateTool.Execute post-call; use `[]rune` for char-count |

---

## 7. What the previous lift got wrong or simplified

1. **Tool argument schema is `{prompt, model?}` NOT `{task}`.** The
   first lift said "JSON schema is `{task: string}`" — actually `task` is
   never the field name. It's `prompt` (required) plus an optional `model`
   override. The optional `model` is non-trivial: it lets the orchestrator
   say "for this specific delegation, use the bigger model" without editing
   the archetype TOML. **Aura plan must use `prompt`/`model` exactly** —
   matches openhuman semantics and gives the orchestrator a per-call
   capability lever.
2. **Description prefix is the load-bearing direct-first bias.** The first
   lift mentioned `when_to_use` becomes the description. It missed that
   `collect_orchestrator_tools` ALWAYS prepends `"Use only when direct
   response/direct tools are insufficient. "` to every synthesised tool's
   description. This single string is one of the strongest behavioural
   levers in the system. Aura's OH1-S3 must replicate this prefix verbatim
   (or write its own with the same explicit "direct first" framing) or the
   orchestrator will over-delegate.
3. **The `omit_*` flag defaults are all `true`, not `false`.** First
   lift's "Concept" section described the flags accurately but didn't pin
   the defaults. Default = `true` = "strip everything from the parent
   prompt by default; the front-line agent opts back in by setting `false`."
   This is the opposite of the intuitive "additive" default and is
   load-bearing for sub-agent context size. Aura's `ApplyDefaults` MUST
   set these to `true` or every sub-agent will inherit the parent's full
   prompt and double the token cost.
4. **`AgentTier::default() = Worker`** is a fail-closed safety net. First
   lift didn't call this out. A user TOML that ships subagents WITHOUT
   declaring a tier becomes a worker, which then fails `validate_tier_hierarchy`
   because workers can't have subagents. Forces explicit tier declaration
   when needed.
5. **Validator runs TWICE** — once on built-ins at boot
   (`load_builtins` calls it) and once after merging user-TOML overrides
   in `Registry::load`. First lift's "loader rejects malformed overrides"
   undersold this — there's a structured double-check that catches a user
   TOML re-tiering a built-in into an invalid combination.
6. **Two dedup paths exist (main + sub-agent), running serially in some
   cases.** First lift mentioned the single dedup. There's a SECOND dedup
   inside `subagent_runner/ops.rs:329-365` because the sub-agent assembles
   its own tool list from a different source (parent's `all_tool_specs[
   allowed_indices]` + dynamic Composio tools). Aura's port can probably
   get away with ONE dedup at the manifest-build site, but document that
   any future "dynamic-tool injection" path must run the same dedup.
7. **`Wildcard` is the default `ToolScope`, NOT `Named([])`.** The
   distinction matters: `Named([])` means "zero tools" (used by
   `trigger_triage` to be a thinking-only agent). `Wildcard` means "all
   parent tools subject to disallowed_tools". Aura's `ApplyDefaults` must
   set `Tools.Wildcard = true` if neither variant was deserialised.
8. **Worker can't have subagents, but skill-wildcards are exempt.** First
   lift said "worker MUST NOT spawn at all". The validator's actual rule
   is "worker MUST NOT list any `AgentId` subagents" — skill wildcards
   (`{ skills = "*" }`) pass through because they collapse to a single
   delegation to `integrations_agent` (a worker). Aura's validator must
   mirror this carve-out IF we ever ship a skill-wildcard equivalent;
   for day 1 (no skill-wildcard) it's irrelevant.
9. **The summarizer is in BUILTINS but NOT exposed as a delegate tool.**
   `orchestrator_tools.rs:100-107` hard-skips `agent_id == "summarizer"`
   because the summarizer is dispatched directly by the runtime
   (when a tool result exceeds `summarizer_payload_threshold_tokens`), not
   by the LLM picking a `delegate_summarizer` tool. **Aura's
   `SynthesiseDelegateTools` needs an equivalent skip-list** — start with
   any future Aura archetype that's "runtime-only" (e.g. a wiki-rewriter
   archetype the model must not call directly).

---

## 8. GPLv3 isolation reminders — clean-room rewrite path

All openhuman code is GPLv3. Aura must not copy code; the **concepts**
documented above are folklore + universal expressions. Per-pattern path:

- **AGENTDEF struct**: define the Go struct from §6's mapping table.
  Field names + defaults are reverse-engineered from the audit, not copied.
  TOML/JSON tag mapping is mechanical Go idiom. No code lifted.
- **Loader**: directory walk, JSON/TOML parse, lenient-per-file error
  handling. This is `filepath.Walk` + `json.Unmarshal` + a `log.Warn` —
  pure stdlib idiom. No openhuman code structure to copy.
- **Tier enum + validator**: write `ValidateTierHierarchy` from the §3
  ruleset (5 rules: worker-no-subagents, chat-no-chat, reasoning-no-reasoning,
  unknown-id-skip, skill-wildcard-skip). Function shape is generic.
- **Depth gate**: Go's `context.Context` value pattern is idiomatic and
  unrelated to openhuman's `tokio::task_local!`. Different mechanism
  entirely; same SEMANTICS (per-task counter, max-3, fail-before-work).
- **DELEGATE-TOOL**: Aura already has a `tools.Tool` interface (or
  whatever the equivalent in `internal/agent/tools/registry/` is). The
  per-turn synthesis is a `for _, entry := range def.Subagents { tools =
  append(tools, ...) }` loop. The schema is published in §4. No openhuman
  code shape to copy.
- **DEDUP**: `for _, spec := range specs { if !seen[spec.Name] { … } }` is
  textbook Go. Five lines of code; can be written from the spec alone.
- **Direct-first description prefix**: "Use only when direct response/direct
  tools are insufficient. " is a 9-word English sentence. Aura can use it
  verbatim or paraphrase; either is fine — words aren't copyrightable
  individually. If paranoid, paraphrase: "Use only when no direct answer
  or direct tool can resolve the request. "

The mechanical risk in this lift is **structural similarity** — the openhuman
file layout (`agentdef/{definition.go,loader.go,registry.go,validator.go}`)
is the natural Go decomposition of the Rust module. That's not copying —
it's the same problem yielding the same solution. Aura's
`internal/agent/agentdef/` should land with original code from §6's mapping;
do NOT keep openhuman open in a side window while writing it.

---

## 9. Open questions (flagged for OH1-S0 discuss-phase)

1. **TOML vs JSON for `agents/*` files**. Openhuman uses TOML; Aura already
   uses JSON for `mcp.json`. TOML's positional-key gotcha (top-level
   `subagents = [...]` must come BEFORE any `[table]` header) is a real
   authoring footgun openhuman hit (`orchestrator/agent.toml:43-47` comment).
   JSON avoids the class entirely. Recommendation: **JSON** for consistency
   with `mcp.json` and to skip the footgun. The OH1-S0 discuss-phase
   should explicitly pick.
2. **Where do built-in archetypes live in the binary?** Openhuman uses
   `include_str!("orchestrator/agent.toml")` at compile time. Go's
   equivalent is `//go:embed`. Recommended layout:
   `internal/agent/agentdef/builtin/<id>/{agent.toml,prompt.md}` with a
   single `embed.FS` per archetype OR one `embed.FS` for the whole dir.
3. **Dynamic vs static prompt-rendering.** Openhuman has 3 prompt sources:
   `Inline` (TOML), `File` (path), `Dynamic` (Rust fn). Day-1 Aura
   probably only needs `Inline` + `File`. The `Dynamic` (Go function-driven)
   variant can wait until we have an archetype that needs to branch on
   runtime state (tools available, user profile loaded, etc.). Recommend
   ship with 2 variants, add 3rd when needed.
4. **`timeout_secs` enforcement layer.** I didn't trace where openhuman
   actually consumes `timeout_secs`. It's a field on the struct but
   `run_subagent` in the file I read doesn't apply it. Probably the
   higher-level dispatch path. For Aura: wire to `context.WithTimeout` at
   the `swarm.Manager.Run` entry point. Mechanical.
5. **`OH1-S0` discuss-phase MUST cover Aura's depth-cap migration**. Aura
   today defaults to MaxDepth=1; openhuman to MaxDepth=3. Bumping is a
   user-visible behavioural change (deeper chains = more LLM cost). Pick:
   bump default and ship as breaking-change in commit body, OR ship with
   `MaxDepth=1` default and document the upgrade path.
6. **Did NOT read this pass**: `harness/session/{builder,turn,runtime}.rs`
   (call sites of `dedup_visible_tool_specs` + manifest assembly),
   `agent/prompts/connected_identities.rs` (`omit_profile`/`omit_memory_md`
   render side). Both are CALLER-side; the struct + loader + validator
   captured here is the LIBRARY side, which is what OH1-S1 lifts.
   Render-side will become relevant for OH1-S2/S3 when the sub-agent's
   prompt actually gets assembled and shipped to the provider.

---

## 10. Top-3 implementation details that change the OH1 plan

1. **Tool schema is `{prompt, model?}` not `{task}`** — current plan
   §5.OH1-S3 says "JSON schema `{task: string}`". Must change to
   `{prompt: string (required), model?: string (optional)}`. The optional
   `model` override is a 1-line addition with major UX value (per-call
   model bump without editing the archetype TOML); skipping it would
   leave a feature on the table for no reason.
2. **All `omit_*` flags default to `true`, and `AgentTier` defaults to
   `Worker`** — the OH1 plan's OH1-S1 acceptance criteria says "default-
   model resolution" but doesn't pin the rest of the field defaults. The
   defaults are load-bearing for both context cost (omit_* defaults =
   true) and safety (tier default = worker forces fail-closed). Spell
   them out in the acceptance criteria; write a test per default.
3. **Dedup ships INSIDE OH1-S3, not after as OH1-S4** — current plan
   sequences OH1-S4 (~30 LOC dedup) after OH1-S3 (~500 LOC delegate
   synth). The day a user sets `delegate_name = "wiki_page"` and shadows
   the real action tool, the LLM will silently call the delegate
   thinking it's invoking the wiki action — until then no test catches
   the collision. Either merge S3 + S4 into one commit OR move S4 to
   land FIRST as defensive infrastructure. Recommended: merge into one
   commit, since dedup is too small to discuss-phase separately and the
   integration test for S3 should already exercise a collision.

Bonus item that didn't make top-3 but worth surfacing in OH1-S0:
**reconsider TOML vs JSON for `agents/*`** — the openhuman TOML
positional-key gotcha is real, JSON sidesteps it, and Aura already uses
JSON for `mcp.json`. The "TOML for consistency with openhuman lift"
rationale is weak when we're clean-rooming everything else.
