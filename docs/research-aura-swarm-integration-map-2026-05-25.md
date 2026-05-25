# Aura ↔ Wave-OH1 swarm integration map (audit before PRD)

**Date:** 2026-05-25
**Author:** codebase mapper (deep audit of existing swarm + agent surface vs OH1 sketch in `docs/openhuman-pattern-lift-2026-05-25.md` and `docs/aura-graph-tools-plan-2026-05-25.md` §5)
**Purpose:** identify EXACTLY which existing code OH1 (AGENTDEF + TIER + DELEGATE-TOOL + DEDUP guard) extends, replaces, deletes, or leaves alone. Numbers in §6/§7 inform whether the original ~1500 LOC estimate is realistic.

---

## 1. What Aura HAS today

### 1.1 swarm runtime — `internal/swarm/` (1158 LOC non-test)

| File | LOC | Public types / one-liner |
|---|---|---|
| `manager.go` | 290 | `Manager{runner, store, mu, maxActive, maxDepth, logger}`; interfaces `AgentRunner` (`Run(ctx, agent.Task) -> agent.Result, error`), `RunRunner`, `parentRunIDSetter` (`WithParentRunID(id) AgentRunner`), `TaskResultReader`, `LimitController`; `ManagerConfig`, `RunRequest{Goal, CreatedBy, Assignments[]}`, `RunResult{Run, Tasks[]}`. Hardcoded: `defaultMaxActive=2`, `defaultMaxDepth=1` (`manager.go:14-17`). Per-task semaphore enforces MaxActive; per-task `Assignment.Depth > maxDepth` rejection (`manager.go:166-174`). Stamps every child dispatch with identity actor + parent_run_id via `delegateAssignmentActor` (`manager.go:248-282`). |
| `plan.go` | 336 | `Plan{Goal, Roles[], Assignments[]}`, `PlanOptions{Roles, UserID, MaxAssignments}`, `RunSynthesis{RunID, Goal, Status, Summary, Metrics, Tasks}`, `RunSynthesisMetrics`, `TaskSynthesis`. Hardcoded role enum: `defaultPlanRoles = ["librarian","critic","researcher","skillsmith","synthesizer"]` (`plan.go:17`). `DefaultMaxPlanAssignments=6` (`plan.go:13`). `BuildPlan`, `PlanAssignments`, `SynthesizeRunResult`. Per-role hardcoded prompt templates in `rolePrompt(role,goal)` (`plan.go:228-243`). `RoleMaxToolCalls=100`, `RoleMaxToolResultChars=24000` (`plan.go:251-261`). |
| `nodespec.go` | 48 | `NodeSpec{Goal, Instruction, ToolAllowlist, MaxIterations, MaxToolCalls, BudgetSecs, OutputSchema, RiskTier, ParentRunID, AssignmentID}`. `RiskTier` enum: `"read_only"` \| `"write_proposal"` (`nodespec.go:31-40`). Hardcoded caps: `MaxIterations ≤ 10`, `BudgetSecs ≤ 300` (`nodespec.go:41-46`). One-liner: bounded-fanout dispatch spec; sibling primitive to the role-plan path, used by `HubBridge.Dispatch`. |
| `tool_policy.go` | 36 | `DirectWriteToolNames = ["wiki_page","source","task","file","agent_note"]` (`tool_policy.go:12-18`). `EnforceWriteProposalAllowlist(allowlist)` — fail-closed validator: must contain `propose_patch`, must NOT contain any direct-write tool. |
| `types.go` | 95 | `RunStatus` enum (`pending/running/completed/failed`), `TaskStatus` enum (same), `Run{ID,Goal,Status,CreatedBy,CreatedAt,UpdatedAt,CompletedAt,LastError}`, `Task{ID,RunID,ParentID,Role,Subject,Prompt,ToolAllowlist,Status,Depth,Attempts,BlockedBy,Result,ToolCalls,LLMCalls,TokensPrompt,TokensCompletion,TokensTotal,ElapsedMS,CreatedAt,StartedAt,CompletedAt,LastError}`, `Assignment{ParentID,Role,Subject,Prompt,SystemPrompt,ToolAllowlist,Depth,UserID,Temperature,MaxToolCalls,MaxToolResultChars,FinalizationTimeout,CompleteOnDeadline,ActorID,DelegatedCapabilities,DelegationConstraintsJSON}` (`types.go:64-81`). `Assignment.AgentTask()` projects to `agent.Task` (`types.go:83-95`). |
| `store.go` | 435 | `Store{db, owned, now, newID}` + interfaces `RunReader`, `RunWriter`, `TaskLister`, `TaskGetter`, `TaskReader`, `TaskWriter`, `Reader`, `Repository`. SQLite tables `swarm_runs` + `swarm_tasks`. `OpenStore(path)`, `NewStoreWithDB(db)`. CRUD + `MarkRunRunning/CompleteRun/FailRun`, `MarkTaskRunning/CompleteTask/FailTask`. Each row carries tool_allowlist as JSON. |
| `hub_bridge.go` | 87 | `HubBridge{hub, parentRunID}` implements `AgentRunner` AND adds `Dispatch(ctx, NodeSpec, principal) (childRunID, error)`. Built around `chat.InboundMessage{Channel: ChannelSwarm, Mode: DeliveryModeSilent, ParentRunID, ChannelData}`. `WithParentRunID(id)` returns shallow copy for child-run lineage. Synchronous `Run` reads back `run.Metadata["final_text"]`; async `Dispatch` returns immediately with child run ID. |

**Test surface (~1755 LOC)**: `hub_bridge_test.go`, `hub_e2e_test.go`, `manager_test.go`, `parent_child_integration_test.go`, `plan_test.go`, `store_test.go`, `tool_policy_test.go`, `write_proposal_integration_test.go`.

### 1.2 swarm LLM-facing tools — `internal/agent/tools/swarm/` (737 LOC non-test)

| File | LOC | Public types / one-liner |
|---|---|---|
| `tools.go` | 587 | Four LLM tools: `SpawnAuraBotTool` (`tools.go:18`, `Name()="spawn_aurabot"`), `RunAuraBotSwarmTool` (`:22`, `Name()="run_aurabot_swarm"`), `ListSwarmTasksTool` (`:306`, `Name()="list_swarm_tasks"`), `ReadSwarmResultTool` (`:363`, `Name()="read_swarm_result"`). All four `VisibilityTier: VisibilityDeferred` (`:42, :158, :326, :383`) — hidden from default manifest, discoverable via tool_search. All four `RequiredCapability: identity.CapabilitySwarmSpawn`. **Role enum is hardcoded inline in `SpawnAuraBotTool.Parameters()`** at `tools.go:181`: `enum: ["librarian","critic","researcher","synthesizer","skillsmith"]`. `RunAuraBotSwarmTool` does NOT accept `roles[]` from the model — it forces `workerRolesForPolicy(nil, policy)` and ignores model-supplied roles (`:96-99`). Per-role identity-delegation: `applyIdentityDelegationToAssignments` walks each assignment and sets `DelegatedCapabilities = [CapabilityToolExecute]` + JSON constraints (`tools.go:275-304`). `roleSystemPrompt(role)` returns a single hardcoded string template (`tools.go:508-510`). |
| `delegation_policy.go` | 150 | `DelegationPolicy{MaxWorkers, Timeout, FinalizationTimeout, ChildMaxIterations, MaxResultChars, Finalization}` (`:18-25`). `Finalization` enum: `"aggregate"` \| `"no_tool_llm"` (`:14-16`). `DefaultDelegationPolicy()` → `{1, 25s, 4s, 100, 24000, "aggregate"}` (`:33-42`). `LoadDelegationPolicyFromEnv()` reads `SWARM_RESEARCH_MAX_WORKERS`, `SWARM_RESEARCH_TIMEOUT_MS`, `SWARM_RESEARCH_FINALIZATION_TIMEOUT_MS`, `SWARM_RESEARCH_CHILD_MAX_ITERATIONS`, `SWARM_RESEARCH_MAX_RESULT_CHARS`, `SWARM_RESEARCH_FINALIZATION` (`:44-65`). `Clamp()` keeps floors on capability knobs + ceilings on latency knobs (the openhuman-aligned "cap LATENCY+COST, not CAPABILITY" rule documented `:67-108`). `ValidateWorkerRoles(roles, catalog)` rejects yaml-slug stale aliases AND unknown roles via `WorkerCatalog.HasRole`. Concrete catalog `toolsetWorkerCatalog{}` delegates to `toolsets.RoleTools`. |

**Test surface (~666 LOC)**: `delegation_policy_test.go`, `tools_test.go`.

### 1.3 Role tool-allowlist source of truth — `internal/agent/tools/sets/toolsets.go`

| Item | Detail |
|---|---|
| `var rolePresets map[string][]string` (`toolsets.go:46-68`) | THE hardcoded role→tools map. Five entries: `librarian→[search,file,source]`, `critic→[search,file,source]`, `researcher→[web]`, `skillsmith→[file]`, `synthesizer→[search,file,source]`. This is what `swarm/plan.go::roleReadOnlyTools` and `swarmtools/tools.go::resolveRoleTools` both call via `toolsets.RoleTools(role)` (`:119-125`). |
| Named toolset enum (`:8-15`) | `ToolsetMemoryRead`, `ToolsetWikiReview`, `ToolsetSkillsRead`, `ToolsetWebResearch`, `ToolsetSandboxCode`, `ToolsetSchedulerSafe`. Independent abstraction used elsewhere (scheduler), not by swarm. |

### 1.4 Summarizer archetype — `internal/agent/agents/summarizer/`

| File | LOC | Detail |
|---|---|---|
| `prompt.go` | 7 | One `//go:embed SKILL.md` directive; exports `Prompt string`. That's it. No struct, no metadata, no `model.hint`, no `max_iterations`, no `tools.named[]`. |
| `SKILL.md` | 33 lines (Markdown) | YAML frontmatter `name: payload-summarizer`, `description`, `version: 1.0.0` + extraction-contract body. |

**How it's wired**: `governance.NewSubagentPayloadSummarizer(client, model, summarizer.Prompt, threshold, max)` (constructor at `internal/agent/governance/payload_summarizer.go:73`). Built ONCE per channel:
- `cmd/aura/web_chat.go:239-242` (web chat).
- `internal/channels/telegram/invocation_builder.go:50-53` (Telegram).

Both gated on `cfg.PayloadSummarizerEnabled` — the summarizer is currently NOT a swarm-runnable agent; it is a bare LLM call short-circuited around the swarm machinery. Trigger lives in `internal/agent/executor.go` (PayloadSummarizer field passed via `WithPayloadSummarizer`, `exec_helpers.go:61`).

**Recursive-dispatch prevention (R1)**: documented in `prd-completed-phase-ctx.json:85` — when the agent IS the summarizer, `PayloadSummarizer` field MUST be nil. Enforced TODAY at the cmd/aura wiring site by simply not wiring a `governance.NewSubagentPayloadSummarizer` into a "summarizer agent" — because no such agent exists in the swarm sense. The summarizer is purely a one-shot LLM detour, no looping.

### 1.5 Sub-loop entry point — `internal/agent/runtask.go` (103 LOC)

`RunTask(ctx, RunTaskDeps, Task) -> Result`. The function the swarm runner adapter (`cmd/aura/adapters.go:90-99`) wraps. Pulls `MaxIterations`/`Timeout` from `deps` (per-call config snapshot), builds `Invocation` with `Tools: runTaskToolDefs(deps.Tools, allowlist)`, calls `Run(ctx, inv)` then `runLoop`. Hardcoded fallback `defaultMaxIterations = 100`, `defaultTimeout = 300s`, `defaultToolTimeout = 30s` (`task.go:14-26`). Sub-loop is the SAME `agent.runLoop` as the top-level loop — no separate "child loop" implementation. `DisableInBatchDedup: true` (`runtask.go:70`) is the only behavioural difference: background sub-loops allow same-tool multi-call.

`RunTaskDeps` (`task.go:57-88`) includes `PayloadSummarizer governance.PayloadSummarizer` field — comment explicitly says "MUST be nil for the summarizer sub-agent itself to prevent recursive dispatch (R1)" (`task.go:84-87`).

### 1.6 Top-level loop — `internal/agent/loop.go` (619 LOC)

`runLoop(ctx, client, executor, state, opts)`. Builds the per-turn tool pool from `opts.Tools` or `opts.ToolsProvider()` (`loop.go:145-151`). The `toolPool` (`internal/agent/pool.go`, 100 LOC) grows monotonically per turn via permissive-load: when the model emits a tool_call whose name is not in the pool, `pool.EnsureLoaded(name)` resolves it via the registry resolver and adds it. **This is the seam OH1's DELEGATE-TOOL hooks into**: synthesised `delegate_<id>` tool specs need to appear in either `opts.Tools` or be resolvable via `resolver`.

`MakeToolsProvider` (`internal/agent/toolsprovider.go:31-43`) returns a stateless closure that returns `defsForFn(coreNames)`. `AlwaysOnCore` defaults: `["search","source","wiki_page"]` (`toolsprovider.go:15`).

The loop already counts swarm usage: `case "run_aurabot_swarm": stats.SwarmUsed = true` (`loop.go:365`).

### 1.7 Identity capabilities — `internal/identity/types.go` (226 LOC)

`Capability string` enum (`types.go:43-58`). Twelve named values: `CapabilityAPIChat`, `CapabilityDashboardRead`, `CapabilityDashboardWrite`, `CapabilityToolExecute`, `CapabilitySkillsInstall`, `CapabilitySkillsDelete`, `CapabilitySettingsWrite`, `CapabilityCronCreate`, `CapabilityCronRun`, **`CapabilitySwarmSpawn`** (line 55), `CapabilityMemoryUserWrite`, `CapabilityWikiWrite`.

Helpers (`store_helpers.go:223-249`):
- `TelegramUserCapabilities()` → 6 standard capabilities (no `SwarmSpawn`).
- `TelegramOwnerCapabilities()` → 12 (includes `SwarmSpawn`).

`ActorType` enum (`types.go:28-34`) includes `ActorTypeSwarm`. The swarm manager uses it in `delegateAssignmentActor` (`manager.go:272`).

`ToolExecuteCapability(toolName)` (`types.go:60-66`) — every tool can be guarded by a finer-grained `Capability("tool.execute." + toolName)`. Today `CapabilitySwarmSpawn` is the coarse gate; this exists for per-tool gating but is not used by the swarm.

`Delegator` interface (`context.go:16-19`) is on the ctx and is what `Manager.delegateAssignmentActor` requires.

---

## 2. What OH1 ADDS that's NEW (no overlap)

These are clean lifts with zero conflict against existing code:

| New file/path | Purpose | Estimated LOC | Conflict risk |
|---|---|---|---|
| `internal/agent/agentdef/definition.go` | Pure `AgentDefinition` struct: `ID, DisplayName, WhenToUse, PromptSource (embed ref), ModelHint, Temperature, ToolsNamed[], SandboxMode, MaxIterations, MaxResultChars, OmitIdentity/Memory/Safety/Skills/Profile bool, Subagents []SubagentEntry, AgentTier`. TOML deserialiser tags. | ~120 | None — new package |
| `internal/agent/agentdef/loader.go` | Walks `embed.FS` (built-ins) then `runtime-workspace/agents/*.toml` (overrides). Returns `map[string]AgentDefinition`. | ~150 | None |
| `internal/agent/agentdef/registry.go` | `Registry{defs, mu}` + `Get(id)`, `All()`. Singleton wired at startup. | ~80 | None |
| `internal/agent/agentdef/validator.go` | Boot-time `ValidateHierarchy(defs)` — tier hops, subagent target existence, no slug collision. | ~120 | None |
| `internal/agent/agentdef/builtin/summarizer/{agent.toml, prompt.md}` | Lift the existing `SKILL.md` body into `prompt.md`; declare `agent_tier="worker"`, `max_iterations=1`, `tools.named=[]`, `temperature=0`. | ~50 (data) | None — new dir |
| `internal/agent/agentdef/builtin/orchestrator/{agent.toml, prompt.md}` | Optional second built-in declaring `subagents=["summarizer"]` so OH1-S3 has something to exercise. | ~40 (data) | None |
| `internal/agent/tools/registry/delegate.go` | The DELEGATE-TOOL synthesiser. Per-turn: for each `subagents[]` entry in active archetype, build one `delegate_<id>` `Tool` whose `Execute` spawns `RunTask` with the target archetype's prompt + filtered `Tools` + `MaxIterations` cap. | ~250-350 | Touches the manifest seam (see §3) but file itself is new |
| `internal/agent/agentdef/depth.go` | Context-local depth counter `WithDepth(ctx) / DepthFromContext`. Caps at 3. | ~40 | None |
| All test files | `definition_test.go`, `loader_test.go`, `registry_test.go`, `validator_test.go`, `delegate_test.go`, `depth_test.go`. | ~600 | None |

**Total new LOC**: ~1450-1600 (matches the original 1500 estimate). **But ~25-30% of that overlaps with existing surface and should be subtracted via extension instead of duplication — see §3.**

---

## 3. What OH1 EXTENDS (existing surface gets augmented)

### 3.1 `swarm.Assignment` → optional `Archetype` field

**File**: `internal/swarm/types.go:64-81`.
**Change**: add `Archetype string` to `Assignment`. When non-empty, manager/runner uses it to (a) override the prompt template (skipping `rolePrompt(role,goal)`), (b) override the tool allowlist (skipping `toolsets.RoleTools(role)`), (c) override `MaxToolCalls/MaxToolResultChars` from the archetype config instead of the hardcoded `RoleMaxToolCalls=100`.
**Why this is "extends" not "replaces"**: legacy `Role`-driven path still works for back-compat. Only when `Archetype != ""` does the archetype branch kick in.
**LOC**: +1 field + ~30 LOC in `swarm/plan.go` to route to archetype when set + ~20 LOC in `swarmtools/tools.go::resolveRoleTools` for parallel path.
**Saves vs new-code path**: ~80 LOC (don't duplicate the manager.go semaphore + identity-delegation machinery).

### 3.2 `swarmtools.DelegationPolicy` ↔ `AgentDefinition` caps — fold OR keep separate?

**Files**: `internal/agent/tools/swarm/delegation_policy.go:18-25` (DelegationPolicy struct), `internal/agent/agentdef/definition.go` (planned).
**Conflict**: `DelegationPolicy.ChildMaxIterations`, `MaxResultChars`, `Finalization`, `Timeout` overlap directly with what an `AgentDefinition` carries (`MaxIterations`, `MaxResultChars`, `Sandbox`, no finalization yet).
**Recommended resolution**: KEEP THE TWO STRUCTS SEPARATE. `DelegationPolicy` is a *runtime knob* read from env (operator-controlled rate-limiter), `AgentDefinition` is a *design-time contract* (developer-shipped specialist spec). On delegate-tool execution, `applyDelegationPolicyToAssignments` (`tools.go:528-541`) already takes MIN of the two. Just extend it to also clamp against the archetype caps. Existing `Clamp()` doc (`delegation_policy.go:67-108`) explicitly says capability knobs have NO ceiling here — archetype provides the higher-fidelity cap.
**LOC**: ~20 LOC delta in `applyDelegationPolicyToAssignments`. No new struct, no migration.

### 3.3 `swarmtools.SpawnAuraBotTool` — hardcoded role enum → AGENTDEF lookup

**File**: `internal/agent/tools/swarm/tools.go:181`.
**Current**: `enum: ["librarian","critic","researcher","synthesizer","skillsmith"]`.
**Change**: when AGENTDEF registry is populated, `Parameters()` builds the enum dynamically from `registry.All()` filtered to `agent_tier="worker"`. Fallback to hardcoded list when registry is empty (boot-time safety).
**LOC**: ~30 LOC (helper + dynamic builder).
**Subtle constraint**: the LLM tool-call schema is provider-cached; the enum is sent every turn but should be stable per restart. ✅ archetype registry is stable per restart.

### 3.4 `identity.Capability` — does TIER need new values?

**File**: `internal/identity/types.go:43-58`.
**Decision**: NO. Tier (`Chat|Reasoning|Worker`) is an *archetype attribute*, not a *grant attribute*. The runtime depth gate enforces tier semantics at delegation time (loop.go check after the synthesised `delegate_<id>` is dispatched). The existing `CapabilitySwarmSpawn` is sufficient as the coarse "can spawn anything" grant.
**Alternative considered**: add `CapabilityDelegateReasoning` / `CapabilityDelegateWorker` for finer dashboard-level control. **Reject**: premature granularity per `feedback_aura_as_product` (gate on numbers, not vibes — single grant is fine until a user complains).
**LOC delta**: 0.

### 3.5 `swarm.NodeSpec.RiskTier` ↔ AGENTDEF `SandboxMode`

**File**: `internal/swarm/nodespec.go:31-40`.
**Today**: `RiskTier` is `"read_only"` \| `"write_proposal"`. `EnforceWriteProposalAllowlist` enforces the write-proposal pattern (must contain `propose_patch`, must NOT contain direct-write tools).
**Conflict**: openhuman's `SandboxMode` is `read_only|read_write|trusted` (richer). The Aura two-state is a STRICTER specialisation.
**Recommended resolution**: KEEP NodeSpec.RiskTier as-is. When AGENTDEF lands, the archetype's `sandbox_mode` MAPS to NodeSpec.RiskTier (`read_only|trusted → "read_only"`, `read_write → "write_proposal"` — yes this is asymmetric, but the proposal pattern is Aura's deliberate constraint per `feedback_per_module_deep_refactor_mandatory`). Document the mapping in `agentdef/definition.go` doc.
**LOC**: ~15 LOC in delegate.go to translate sandbox_mode → RiskTier on dispatch path.

### 3.6 `runtask.RunTaskDeps.PayloadSummarizer` — already exists as the recursive-prevention seam

**File**: `internal/agent/task.go:84-87` (field), `cmd/aura/web_chat.go:239` + `internal/channels/telegram/invocation_builder.go:50` (wiring).
**OH1 use**: when `delegate_<summarizer>` synthesised tool dispatches, the sub-loop's `RunTaskDeps.PayloadSummarizer` MUST be nil (R1 mitigation explicit in the field doc). The delegate.go synthesiser needs to thread a "is this the summarizer archetype" check and nil out the field. ~10 LOC.

### 3.7 `agent.MakeToolsProvider` + `toolPool` — manifest synthesis seam

**Files**: `internal/agent/toolsprovider.go:31-43`, `internal/agent/pool.go`, `internal/agent/loop.go:145-151`.
**OH1 hook**: per-turn, BEFORE `runLoop` builds `toolPool`, the synthesised `delegate_<id>` definitions from `delegate.go::SynthForArchetype(activeArchetypeID)` get prepended to `opts.Tools`. The cleanest hook is to wrap `opts.ToolsProvider` (closure) so the seam stays in the channel-level `invocation_builder.go` and the agent package stays archetype-unaware.
**LOC**: ~40 LOC channel-side wrap + 0 LOC in agent/ (provider closure pattern already abstracts this).

---

## 4. What OH1 REPLACES (delete + rewrite)

### 4.1 `internal/agent/agents/summarizer/prompt.go` — collapses to a stub OR is deleted

**File**: `internal/agent/agents/summarizer/prompt.go` (7 LOC) + `SKILL.md` (33 lines).
**Replacement**: the same content moves to `internal/agent/agentdef/builtin/summarizer/{agent.toml, prompt.md}`.
**Migration path (back-compat, see §6 Commit B)**: keep `prompt.go` as a re-export shim that reads `agentdef.Registry.Get("summarizer").Prompt()`. Both `cmd/aura/web_chat.go:240` and `internal/channels/telegram/invocation_builder.go:51` keep importing `summarizer.Prompt`; no caller changes. Commit G (later) deletes the shim once nothing imports it.

### 4.2 `swarmtools.SpawnAuraBotTool` / `RunAuraBotSwarmTool` — DEPRECATE, don't delete (yet)

**Files**: `internal/agent/tools/swarm/tools.go:18-302` (~280 LOC of `Spawn` + `RunSwarm`).
**OH1 says replace them with synthesised `delegate_<id>` tools per turn**.
**But**: these are CALLABLE from the LLM today (registered via `cmd/aura/app.go:503-507`), have ~411 LOC of tests (`tools_test.go`), AND have `VisibilityTier: VisibilityDeferred` so the LLM only sees them on `tool_search`. Behavioural deprecation is safe; outright delete breaks tests that gate the swarm capability.
**Recommended path**: Commit F marks them deprecated in description, leaves them callable. Commit G removes after telemetry confirms zero invocations for N days.
**Critical constraint**: `ListSwarmTasksTool` / `ReadSwarmResultTool` (`tools.go:306-414`) DO NOT GO AWAY — they remain the only way to observe swarm state from the LLM. OH1's delegate-tool returns the child's text inline, but the underlying `swarm_runs` / `swarm_tasks` SQLite rows still exist and are still observable. Keep them as-is.

### 4.3 Hardcoded `defaultPlanRoles` in `swarm/plan.go:17`

**File**: `internal/swarm/plan.go:17`.
**Replacement**: `BuildPlan` (`plan.go:77`) accepts an archetype-registry-derived role list. Today it's `defaultPlanRoles = ["librarian","critic","researcher","skillsmith","synthesizer"]`. OH1 keeps the constant as a FALLBACK for when AGENTDEF is empty (Commit A scenario) — but `PlanOptions` grows an `Archetypes []string` field that wins when set.
**LOC**: -1 hardcoded line + ~40 LOC in `BuildPlan` for the dual-path.

### 4.4 Hardcoded role→tool map `rolePresets` in `toolsets.go:46-68`

**Files**: `internal/agent/tools/sets/toolsets.go:46-68` (the data), `tools.go:491-506` (consumer).
**Replacement**: when archetype X exists in AGENTDEF, `toolsets.RoleTools(X)` returns the archetype's `tools.named[]`. Fall back to hardcoded `rolePresets` when registry has no entry.
**LOC**: ~25 LOC in `RoleTools` to consult the registry first.

---

## 5. What stays UNTOUCHED

These work today, work after OH1, and OH1 has no reason to modify them:

| Surface | File | Why untouched |
|---|---|---|
| Swarm SQLite schema | `internal/swarm/store.go:115-329` | `swarm_runs` + `swarm_tasks` tables stay as-is. Archetype name fits in existing `role` column. No schema migration needed. |
| Parent-run-id lineage | `swarm/manager.go:138-144`, `hub_bridge.go:23-32` | Already in place; archetypes inherit it. |
| Identity actor delegation | `swarm/manager.go:248-282` | `delegateAssignmentActor` already creates a child actor per assignment with capability constraints; archetype-driven dispatch reuses this verbatim. |
| `NodeSpec.Dispatch` + write_proposal allowlist | `swarm/nodespec.go`, `swarm/tool_policy.go`, `hub_bridge.go:59-87` | The bounded-fanout primitive is orthogonal to the role-plan path; OH1 lives in the role-plan path. NodeSpec stays. |
| `ListSwarmTasksTool` / `ReadSwarmResultTool` | `swarmtools/tools.go:306-414` | LLM-facing observability tools. Still useful for any swarm run, archetype-driven or not. |
| Swarm parent-child integration tests | `swarm/parent_child_integration_test.go`, `hub_e2e_test.go` | Test the manager + bridge contract, not the role enum. Pass through unchanged. |
| Capability enum machinery | `identity/types.go:43-58`, `identity/store_*.go` | Per §3.4, no new capability values. |
| `summarizer` payload trigger (LLM short-circuit) | `internal/agent/governance/payload_summarizer.go` + `executor.go` hook | The summarizer-as-payload-interceptor is a separate code path from the summarizer-as-archetype that OH1 introduces. Both can co-exist: the LLM-short-circuit path runs ONE-SHOT inside `governance.MaybeSummarize`; the AGENTDEF path runs as a SWARM DISPATCH via `delegate_summarizer`. They don't intersect. (R1 stays prevented because the short-circuit path is gated on `cfg.PayloadSummarizerEnabled` and doesn't reach the agent loop.) |
| `governance.MicroCompactHistory` / `ScrubOrphanToolCalls` | `internal/agent/governance/` | History-shaping passes that don't care about archetypes. |
| `toolPool.EnsureLoaded` permissive-load | `internal/agent/pool.go:70-99` | The seam that lets synthesised `delegate_<id>` tools resolve on first call. No change needed. |
| All MCP tool wiring | `internal/agent/tools/registry/mcp.go` | Independent code path. |
| All `cmd/aura/app.go` wiring outside the swarm block (lines 471-522) | Most of `app.go` | Only the swarm block needs the archetype-registry constructor. |

---

## 6. Migration path — atomic commit sequence

Per `feedback_per_module_deep_refactor_mandatory` and the `feedback_one_module_per_slice` rule, each commit must be (a) atomic, (b) revertable, (c) NOT break the live loop. Sequence:

### Commit A — `feat(agentdef): registry + loader, zero archetypes shipped (US-OH1-S1a)`

- New package `internal/agent/agentdef/` with `definition.go`, `loader.go`, `registry.go`, `validator.go` + their tests.
- Zero TOML files under `builtin/`. Registry boots empty.
- Wiring: `cmd/aura/app.go` calls `agentdef.Load()` after store init; logs `archetypes_loaded=0`.
- **Tests**: (a) empty registry boots cleanly; (b) loader reads valid TOML round-trip; (c) malformed TOML rejected with line+col; (d) user-override beats built-in by slug; (e) slug collision rejected; (f) `omit_*` flags deserialise; (g) `subagents[]` deserialises; (h) `agent_tier` defaults to `Worker` when missing; (i) `ValidateHierarchy` rejects cycle.
- **Live-loop impact**: ZERO. Nothing reads the registry yet.

### Commit B — `feat(agentdef): migrate summarizer to TOML, prompt.go becomes re-export (US-OH1-S1b)`

- Create `internal/agent/agentdef/builtin/summarizer/{agent.toml, prompt.md}`. Body of `prompt.md` is the body of `SKILL.md` (drop YAML frontmatter — TOML carries the same metadata).
- `internal/agent/agents/summarizer/prompt.go` becomes:
  ```go
  package summarizer
  import "github.com/aura/aura/internal/agent/agentdef"
  var Prompt = agentdef.MustGet("summarizer").Prompt
  ```
  (Or a lazy getter to avoid init-order dependency.)
- **Tests**: (a) `summarizer.Prompt` returns identical bytes to old `//go:embed SKILL.md`; (b) `governance.NewSubagentPayloadSummarizer` still constructible; (c) end-to-end payload summarization test in `payload_summarizer_test.go` still passes.
- **Live-loop impact**: byte-for-byte identical at runtime. The OH1 sketch acceptance criterion in `aura-graph-tools-plan-2026-05-25.md:397-399` ("Existing summarizer at `internal/agent/agents/summarizer/prompt.go` keeps working unchanged") is satisfied by the shim.

### Commit C — `feat(agentdef): TIER enum + validator warns (US-OH1-S2a, warn-only)`

- Add `AgentTier string` enum (`Chat|Reasoning|Worker`) to `AgentDefinition`.
- `validator.go` checks tier-hop rules but emits `logger.Warn` instead of returning error.
- New built-in `orchestrator/agent.toml` declares `agent_tier="chat"`, `subagents=["summarizer"]`.
- **Tests**: (a) tier deserialises; (b) tier defaults to `Worker` when missing; (c) `chat→chat` triggers warn; (d) `worker.subagents` non-empty triggers warn; (e) valid `chat→reasoning→worker` triggers no warn.
- **Live-loop impact**: zero behavioural; warnings only. Operator can read logs to see drift.

### Commit D — `feat(agentdef): tier enforcement on (US-OH1-S2b, enforce)`

- Flip `validator.go` warns to errors.
- Loader rejects on hop violation; `cmd/aura/app.go` fails-fast at boot.
- **Tests**: same as Commit C but assert error returned instead of warning emitted.
- **Live-loop impact**: still zero — no archetype is dispatched yet, only loaded. But if Commit C exposed bad config, Commit D refuses to boot. This is the gate to ship before delegate-tool.

### Commit E — `feat(tools): synthesise delegate_<id> per-turn (US-OH1-S3)`

- New `internal/agent/tools/registry/delegate.go` (~300 LOC) + `delegate_test.go`.
- Wire synthesised tools into `invocation_builder.go` (Telegram) + `web_chat.go` (web): wrap existing `ToolsProvider` closure to prepend the active archetype's `delegate_<id>[]` slice.
- **Tests**: (a) synthesised tool name is `delegate_<id>` (or `delegate_name` override); (b) schema is `{task: string}`; (c) `Execute` spawns `RunTask` with target archetype's prompt + tools + max_iter + nil `PayloadSummarizer` if target is `summarizer`; (d) parent's `Stats.ToolCalls` reflects the delegate call but not the child's internal tool calls (parent doesn't see them); (e) tier+depth check rejects illegal dispatch; (f) DEDUP-VISIBLE-TOOL-SPECS guard (OH1-S4 folded in) keeps first occurrence on name collision.
- **Live probe**: `gsd-execute-phase` integration probe sends "summarize this large block" → orchestrator archetype dispatches `delegate_summarizer` → sub-loop returns compressed result → orchestrator includes it in final reply. Asserted at the chat-archive layer: parent run has tool_call `delegate_summarizer` with reply text matching the child's `swarm_tasks.result`.

### Commit F — `chore(swarmtools): deprecate spawn_aurabot + run_aurabot_swarm in description (US-OH1-S5)`

- Description string prefixed with `[DEPRECATED — prefer delegate_<id> when available]`.
- Both tools still registered, still callable, still tested.
- Add log warning when invoked.
- **Tests**: existing `tools_test.go` passes unchanged. Add one test asserting deprecation warning appears in log.
- **Live-loop impact**: tools still work. Telemetry starts measuring usage decline.

### Commit G — `refactor(swarmtools): remove deprecated tools (US-OH1-S6)`

- ONLY when telemetry shows zero invocations for ≥30 days AND a follow-up bench confirms `delegate_<id>` covers the use case.
- Delete `SpawnAuraBotTool` + `RunAuraBotSwarmTool` (~280 LOC).
- Delete corresponding tests (~250 LOC of `tools_test.go`).
- Delete the `cmd/aura/app.go:503-507` registrations.
- Keep `ListSwarmTasksTool` + `ReadSwarmResultTool` + `delegation_policy.go` (still needed for synthesised-delegate dispatch).
- ALSO delete the `internal/agent/agents/summarizer/prompt.go` re-export shim from Commit B; update the two callers (`web_chat.go:240`, `invocation_builder.go:51`) to read from `agentdef.MustGet("summarizer").Prompt` directly.
- **Tests**: ~250 LOC of tests deleted; ~80 LOC of new tests cover `agentdef.MustGet("summarizer")` direct path.
- **Live-loop impact**: surface narrows. Per `feedback_per_module_deep_refactor_mandatory` this is the deep refactor that lands IN THE SAME PHASE, not as a "later cleanup".

**Commit-sequence LOC delta**:

| Commit | Adds | Removes | Net |
|---|---|---|---|
| A | +450 (code) + ~250 (tests) | 0 | +700 |
| B | +50 (TOML+md) + ~30 (shim+test) | 0 | +80 |
| C | +80 (tier) + ~80 (test) + +40 (orchestrator builtin) | 0 | +200 |
| D | +10 (warn→err) + ~30 (test) | 0 | +40 |
| E | +300 (delegate) + ~250 (test) + 40 (wire) | 0 | +590 |
| F | +30 (warn+desc) + 20 (test) | 0 | +50 |
| G | +60 (direct-path tests) | −330 (deprecated tools + tests) | −270 |
| **TOTAL** | **~1340 added** | **~330 removed** | **+1010** |

This is **~33% less** than the original 1500 LOC estimate, because (a) the summarizer archetype reuses existing payload-summarizer wiring, (b) AGENTDEF extends `Assignment` rather than replacing it, (c) the dedupe guard (~30 LOC) folds into Commit E for free, (d) the swarm SQLite schema needs no migration.

---

## 7. Concrete integration friction points

### F1 — `summarizer` is two different things post-OH1

- **`internal/agent/governance/payload_summarizer.go:99`** — short-circuit one-shot LLM detour for oversized tool results. NO swarm involvement.
- **`internal/agent/agentdef/builtin/summarizer/`** — full swarm archetype invokable via `delegate_summarizer` from the orchestrator.
- **Friction**: a future reader will think these duplicate. Add a doc paragraph in `agentdef/builtin/summarizer/agent.toml` header pointing to `governance/payload_summarizer.go` and saying "this archetype is invoked from the agent loop via the DELEGATE-TOOL synthesiser; the short-circuit path in governance/ is independent and gates on `cfg.PayloadSummarizerEnabled`."
- **Resolution**: documentation, not code. Land in Commit B.

### F2 — `swarmtools.DelegationPolicy.Clamp()` already docs the "cap LATENCY, not CAPABILITY" rule

- **File**: `internal/agent/tools/swarm/delegation_policy.go:67-108`.
- **Friction**: when AGENTDEF's `MaxIterations` is HIGHER than `DelegationPolicy.ChildMaxIterations` (env-set ceiling), which wins?
- **Resolution**: archetype is the design contract; env is the operator override. `applyDelegationPolicyToAssignments` already takes MIN. Keep that pattern in `delegate.go` synthesised tool dispatch. ~5 LOC.

### F3 — `swarm.Assignment.AgentTask()` (`types.go:83-95`) loses archetype info

- **File**: `internal/swarm/types.go:83-95`.
- **Friction**: `agent.Task` has no `Archetype` field. When `delegate.go` invokes `RunTask` it builds the `agent.Task` directly (bypassing Assignment) so this isn't an issue for delegate-path. BUT: if someone later wants to plumb archetype through `manager.Run`, the projection drops it.
- **Resolution**: `delegate.go` invokes `RunTask` DIRECTLY without going through `Manager.Run`. Document this explicitly in delegate.go header. (The Manager-orchestrated, multi-role plan path stays the way it is — that path uses `Assignment.Role`, not Archetype.)

### F4 — Per-turn `delegate_<id>` synthesis happens at `invocation_builder` layer, not `agent/`

- **File**: `internal/channels/telegram/invocation_builder.go:36-80`, `cmd/aura/web_chat.go:200-275`.
- **Friction**: the agent package stays archetype-unaware (good — keeps `runtime.go` clean). But it means TWO channel builders need the same wrap.
- **Resolution**: extract a `WithArchetypeDelegates(provider, registry, archetypeID)` helper in `agentdef/` consumed by both builders. ~25 LOC, prevents drift. Land in Commit E.

### F5 — `cmd/aura/app.go:483-485` swarm runner adapter

- **File**: `cmd/aura/app.go:483-485` + `cmd/aura/adapters.go:90-99`.
- **Friction**: `swarmRunnerAdapter.Run` builds `RunTaskDeps` via `newSwarmDepsGetter`. The deps it builds include `PayloadSummarizer` from telegram.Deps — when the swarm runs the summarizer archetype, R1 (recursive dispatch) fires unless the adapter zeros the field.
- **Resolution**: the delegate-tool path doesn't use this adapter (it calls `RunTask` directly with explicit nil for summarizer-target). The MANAGER path (the existing `spawn_aurabot` / `run_aurabot_swarm` tools, deprecated post-Commit F) still uses this adapter — and they don't dispatch to summarizer today, so no incremental risk. Add a guard anyway in `delegate.go`: `if target.ID == "summarizer" { deps.PayloadSummarizer = nil }`. ~3 LOC, documented as R1 mitigation.

### F6 — `toolPool.EnsureLoaded` + dynamic `delegate_<id>` resolver

- **File**: `internal/agent/pool.go:70-99`, `internal/agent/loop.go:373-377`.
- **Friction**: `delegate_summarizer` doesn't exist in the persistent tools registry; it's synthesised per-turn. If the model emits a tool_call for `delegate_summarizer` AFTER the pool seed, the permissive-load resolver in `EnsureLoaded` won't find it — there's no entry in the central registry.
- **Resolution**: make `delegate_<id>` always-on for the active archetype's subagents (prepend to `opts.Tools` at invocation-build time, NOT rely on resolver). The synthesised slice is small (typically 1-3 delegates per archetype), so prepending is cheap. ~5 LOC in the channel builder.

### F7 — Test file growth on swarm/tools/swarm/

- **File**: `internal/agent/tools/swarm/tools_test.go` is already 411 LOC (close to the 600 LOC cap from `feedback_per_module_deep_refactor_mandatory`).
- **Friction**: adding deprecation tests in Commit F pushes it past 600.
- **Resolution**: in Commit F, split `tools_test.go` into `spawn_aurabot_test.go`, `run_aurabot_swarm_test.go`, `list_read_swarm_test.go`. Each well under 200 LOC. Commit G deletes the first two. ~50 LOC test-file shuffle but no behavioural change.

---

## 8. Test changes required per commit

| Commit | New tests | Updated tests | Deleted tests |
|---|---|---|---|
| A | `agentdef/{definition,loader,registry,validator}_test.go` (~250 LOC, 9 cases) | None | None |
| B | `summarizer/prompt_compat_test.go` (~30 LOC, 1 case) | `payload_summarizer_test.go` minor — assert `summarizer.Prompt != ""` post-shim. | None |
| C | `agentdef/tier_test.go` (~80 LOC, 5 cases) | None | None |
| D | `agentdef/validator_enforce_test.go` (~30 LOC, 3 cases) | Update C's warn-assertions to err-assertions. | None |
| E | `tools/registry/delegate_test.go` (~250 LOC, 6 cases) + channel-side integration probe | `invocation_builder_test.go` + `web_chat_test.go` add archetype-wired build path | None |
| F | `tools/swarm/spawn_deprecated_test.go` (~20 LOC, 1 case asserting log warning) | Split `tools_test.go` into 3 files | None |
| G | `agentdef/registry_direct_use_test.go` (~60 LOC, 3 cases) | `web_chat.go` + `invocation_builder.go` test files updated to use `agentdef.MustGet("summarizer")` directly | `tools/swarm/spawn_aurabot_test.go` + `run_aurabot_swarm_test.go` deleted (~250 LOC) |

**Test LOC delta**: +720 added, ~250 deleted, **net +470 test LOC** (compared to ~870 net code LOC). Test-to-code ratio ~54% — appropriate for a substrate that 5 surfaces will eventually depend on.

---

## Summary

**Estimated final OH1 LOC**: **~1010 net** added (+~470 tests on top) vs the original sketch estimate of ~1500. The savings come from (a) `Assignment` extension instead of replacement (~80 LOC saved), (b) `summarizer` prompt body REUSED verbatim, only the metadata moves to TOML (~40 LOC saved), (c) `DelegationPolicy` stays separate (~80 LOC saved by NOT folding two configs), (d) SQLite schema unchanged (~150 LOC of migration code avoided), (e) DEDUP guard (OH1-S4) folds into Commit E as 30 LOC (~50 LOC saved by not making it a separate phase).

**Biggest integration risk**: **F4 + F6 combined** — the delegate-tool synthesis seam straddles `agent/`, `agentdef/`, AND both channel builders (`telegram/invocation_builder.go` + `cmd/aura/web_chat.go`). A naive implementation duplicates the synthesis logic in two places and drifts. Mitigation: extract `agentdef.WithArchetypeDelegates(provider, registry, archetypeID)` helper FIRST (~25 LOC, Commit E sub-task before the actual `delegate.go` writes); both builders consume it identically. If we skip this and inline the wrap, the next channel (whatsapp? per `project_target_architecture_diagram_2026-05-15`) will be the third place that needs the same wrap and the helper will be extracted under duress.

**Recommended next action**: **PROCEED with OH1-S0 (`/gsd-discuss-phase`)** before drafting the PRD. The discuss-phase should explicitly resolve the ambiguities surfaced in this audit: (1) keep DelegationPolicy separate (§3.2) — yes/no; (2) archetype enum source priority for `spawn_aurabot` (§3.3) — registry-first with hardcoded fallback, confirmed?; (3) sandbox_mode → RiskTier mapping (§3.5) — confirm asymmetry is intentional; (4) Commit F deprecation telemetry window — 30 days adequate or longer needed?; (5) F4 helper extraction — agreed to land in Commit E rather than delayed?. The PRD that comes out of discuss-phase will be implementation-ready precisely because these five questions were answered up front, not in flight.
