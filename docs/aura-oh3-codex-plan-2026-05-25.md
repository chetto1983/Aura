# Aura — Wave OH3 Codex execution plan (2026-05-25)

Codex-ready, GSD-free. Each commit is a self-contained Codex prompt: file
paths, struct sketches, tests, acceptance criteria, deep-refactor checklist.
Drive via `codex exec --skip-git-repo-check --sandbox workspace-write
"<prompt>"` one commit at a time, OR Claude inline.

**Anchors:**
- `docs/aura-oh3-research-tmp-lift-2026-05-25.md` — Scout #1: `D:/tmp/` pattern lift
- `docs/aura-oh3-research-strands-langgraph-2026-05-25.md` — Scout #2: Strands + LangGraph deep-dive
- `docs/aura-oh3-research-kimi-openai-2026-05-25.md` — Scout #3: Kimi K2.5/K2.6 + OpenAI Agents SDK
- `docs/aura-oh3-research-production-antipatterns-2026-05-25.md` — Scout #4: CrewAI/AutoGen/Letta + 12 anti-patterns + OTel
- **Shen et al. 2026 — "An Empirical Study of Multi-Agent Collaboration for Automated Research"** (arXiv 2603.29632v2) — controlled head-to-head benchmark of *Subagent mode* (Algorithm 1, what OH3 ships) vs *Agent Teams* (Algorithm 2, deferred to OH4) vs single-agent baseline on a fixed-budget ML optimization task. Direct empirical evidence for OH3's parallel-fan-out-first choice.
- `docs/aura-oh1-codex-plan-2026-05-25.md` — Wave OH1 substrate (precondition for OH3)

**Status (2026-05-25):** Wave OH1 is in flight via parallel Codex session (shipped: RFL-S1, OH1-A, OH1-B, OH1-C; in flight: OH1-D; pending: OH1-E, OH1-F, OH1-G). Wave OH3 is the natural successor and depends on OH1-E (delegate synth) shipping first. Do NOT start OH3 implementation until OH1-G ships and the Wave-OH1 ship gate is green.

---

## 0. Locked decisions (the 10 OH3-S0 answers)

The user's framing was "agenti che comunicano tra di loro a runtime in loop, come Strands Swarm + Kimi K2.5". 4 parallel scouts mapped this to production reality. The convergent verdict:

> The "loop where agents talk to each other" pattern is two distinct shapes —
> *parallel fan-out* (Kimi `create_subagent + assign_task`) and *sequential
> peer-mesh* (Strands `Swarm` sticky active-agent). Kimi/OpenAI/Anthropic v1
> all ship the first (tree). The second (mesh) is unshipped even in K2.6.
> Wave OH3 ships parallel fan-out, treats peer-mesh as OH4+.

| # | Question | LOCKED | Source(s) |
|---|---|---|---|
| 1 | Primitive shape | **Parallel fan-out** (Kimi-style `create_subagent` + `assign_task`); peer-mesh deferred to OH4 | Scout #3 §2 (Kimi paper L753-767); Scout #1 §2.9 (openhuman); Scout #4 §3 (Cognition split); **Shen et al. Fig 3** (Subagents = lowest preflight-failure + crash rate under fixed budget) |
| 2 | Sub-agent semantics | **Frozen, isolated context, one message back** — sub-agents cannot mutate parent context | Scout #3 §2.2 (Kimi paper L766); Scout #1 §2.9 (openhuman `subagent_runner/ops.rs:503-525`) |
| 3 | Spawn schema | **Kimi 2-tool API verbatim** — `create_subagent(name, system_prompt)` + `assign_task(agent, prompt)`; ephemeral agentdef in registry, lives 1 turn | Scout #3 §2.3 (Kimi paper Appendix E.8) |
| 4 | Concurrency cap | **8 parallel children** (vs Kimi's 300) — mini-PC budget per `feedback_minipc_cpu_budget` | Scout #3 §5.4 (mini-PC constraint), Scout #1 §2.9 (openhuman `max_parallel_tools`) |
| 5 | Termination policy | **Strands `SwarmState.should_continue()` ported 1:1 to Go** — 5 orthogonal stops: max_handoffs, max_iterations, execution_timeout, repetitive_handoff_window, repetitive_handoff_min_unique_agents | Scout #2 §2.8 + §5 (Strands swarm.py L90-93) |
| 6 | Blackboard | **Typed Go struct per swarm-run with Letta-style ops** — `insert` (append-only safe), `replace` (optimistic-concurrency token), NO `rethink` (last-writer-wins forbidden in multi-agent) | Scout #4 §4.2 (Letta `memory_insert/replace/rethink`); Scout #2 §5 Gap-2 (LangGraph contract) |
| 7 | Composability | **`composable_output: false` agentdef field forces sequential** (no parallel fan-out for write/refactor tasks per AP-4 Flappy Bird) | Scout #4 §6 AP-4 (Cognition) |
| 8 | Cost gates | **3-tier budget enforcement**: per-turn / per-conversation / per-agentdef-daily. Preflight rejects fan-out N>3 when remaining-budget < N × expected-subagent-cost | Scout #4 §6 AP-3 (RelayPlane $47K pattern) |
| 9 | Observability | **OTel GenAI standard attrs + 6 custom Aura attrs**: `aura.handoff.from/to`, `aura.delegation.depth`, `aura.handoff.packet_size_tokens`, `aura.blackboard.version`, `aura.budget.tokens_remaining` | Scout #4 §7 (OTel GenAI spec 2026 gaps) |
| 10 | Critic isolation | **Any critic agentdef MUST use different model family OR external ground-truth tool call** — never "another same-family LLM saying yes" (per AP-5 tautological PASS) | Scout #4 §6 AP-5 (arxiv 2505.19477) |

**Hardcoded from cross-scout convergence (R1-R7):**
- **R1** Ownership boundary prompt-injection (lifted from openhuman `spawn_parallel_agents.rs:480-487`) on every fan-out child to prevent worker overlap.
- **R2** Handoff-packet schema — `{objective, output_format, boundaries, relevant_facts, known_unknowns}` (NOT raw transcript) per AP-2 + AP-6. Bound at 2k tokens; CI gate.
- **R3** `terminate` / `escalate_to_user` always present as a peer of every delegation tool (mitigation for AP-1 infinite handoff per autogen #5831).
- **R4** Critical Steps metric (Kimi paper L262-287): `Σ stages [orch_steps + max(sub_steps)]`. Logged in archive, exposed in dashboard, asserted by probes.
- **R5** Sub-agents never spawn sub-agents — `is_subagent_spawn_tool` strip (openhuman `subagent_runner/ops.rs:503-525`). OH1's cycle detector enforces tree shape; OH3 inherits.
- **R6** Single trace_id per Telegram turn carried through every LLM/tool/delegate call (mitigation for AP-10 observability collapse).
- **R7** **Diversify ephemeral-subagent system prompts per spawn** — mitigation for the "greedy local optimization fixation" failure mode empirically observed in Shen et al. §4 Fig 2 (subagents converged repeatedly on MLP expansion ratio 4×→0.75× because all spawns shared the same prompt template). Concrete rule: when the orchestrator emits N≥2 `assign_task` calls in one turn against the same ephemeral agentdef, OH3-C MUST require distinct `prompt` strings (Levenshtein distance ≥ 0.3 over their tokenized form). Identical-prompt fan-out → rejected with structured error suggesting prompt diversification. CI gate in OH3-H scenario #3 (5-parallel wide research) asserts the rule.

---

## 1. Commit sequence (8 atomic commits)

```
OH3-A → OH3-B → OH3-C → OH3-D → OH3-E → OH3-F → OH3-G → OH3-H
 150     250     250     250     150     300     200     150    LOC (net code)
```

Test LOC budget: ~600 across the whole wave. Each commit lefthook-green (0 dupl, 0 lint, file-size ≤600 LOC, errcheck where applicable). Each commit ships with `golangci-lint run <touched files>` clean and includes the deep-refactor checklist per CLAUDE.md.

### Status snapshot (2026-05-25 — pre-implementation)

| Commit | Status | Notes |
|---|---|---|
| OH3-A | ⬜ blocked on OH1-G | Ephemeral agentdef registry path |
| OH3-B | ⬜ blocked on OH3-A | `create_subagent` + `assign_task` tools (Kimi schemas) |
| OH3-C | ⬜ blocked on OH3-B | Parallel fan-out (`errgroup`) + ownership boundary + 8-cap |
| OH3-D | ⬜ blocked on OH3-B | Typed Blackboard (Letta `insert/replace` ops) |
| OH3-E | ⬜ blocked on OH3-C | `swarmpolicy.ShouldContinue()` (Strands 5-stop port) |
| OH3-F | ⬜ blocked on OH3-C, OH3-D | OTel GenAI spans + Critical Steps metric + custom attrs |
| OH3-G | ⬜ blocked on OH3-F | 3-tier budget + cost preflight |
| OH3-H | ⬜ blocked on OH3-G | Live Telegram probe + MAST failure tagging + ship gate |

**Trigger to start OH3:** Wave OH1 ship gate green (per `docs/aura-oh1-codex-plan-2026-05-25.md` §12) — specifically OH1-E delegate synth shipped and OH1-G reflector archetype landed.

---

## 2. OH3-A — Ephemeral agentdef registry path (~150 LOC) [Codex]

**Goal**: extend OH1's `agentdef.Registry` with an in-memory ephemeral path so the orchestrator can declare a transient archetype within a turn (Kimi's `create_subagent` pattern). The persistent registry from OH1 remains the source of truth; ephemeral defs live keyed by `(turn_id, name)` and are garbage-collected at turn end.

**Files**:
- new `internal/agent/agentdef/ephemeral.go` — `EphemeralStore` with `Register/Get/PurgeTurn`
- new `internal/agent/agentdef/ephemeral_test.go`
- edit `internal/agent/agentdef/registry.go` — `Registry.Lookup(turnID, name)` consults ephemeral first, then persistent
- edit `internal/agent/runtime.go` — wire `EphemeralStore.PurgeTurn(turnID)` into the turn-end hook

**Sketch**:
```go
// internal/agent/agentdef/ephemeral.go
package agentdef

type EphemeralStore struct {
    mu  sync.RWMutex
    byTurn map[string]map[string]AgentDefinition // turnID -> name -> def
}

func NewEphemeralStore() *EphemeralStore { ... }

func (s *EphemeralStore) Register(turnID string, def AgentDefinition) error {
    // Validate: tier=worker, max_iterations<=20, tools empty (inherits parent's whitelist)
    // Reject if same (turnID, name) already exists.
}

func (s *EphemeralStore) Get(turnID, name string) (AgentDefinition, bool) { ... }

func (s *EphemeralStore) PurgeTurn(turnID string) { ... }

// edit internal/agent/agentdef/registry.go
func (r *Registry) Lookup(turnID, name string) (AgentDefinition, bool) {
    if def, ok := r.ephemeral.Get(turnID, name); ok { return def, true }
    return r.Get(name) // existing persistent path
}
```

**Tests** (4):
- `TestEphemeralStore_RegisterAndGet`
- `TestEphemeralStore_RejectsDuplicate` — same (turnID, name) → error
- `TestEphemeralStore_PurgeTurn_RemovesAll`
- `TestRegistry_LookupPrefersEphemeral` — ephemeral wins over same-name persistent def

**Acceptance**: existing OH1 tests stay green; runtime boots with no manifest delta (ephemeral path is library-only, not yet exposed as a tool).

**Deep refactor (per CLAUDE.md)**: `golangci-lint run internal/agent/agentdef/...` clean; `dupl -t 60 internal/agent/agentdef/` no new cluster; file-size ≤600 LOC; errcheck on `EphemeralStore.Register`.

---

## 3. OH3-B — `create_subagent` + `assign_task` tools (~250 LOC) [Codex]

**Goal**: ship the Kimi 2-tool spawn API on top of the ephemeral registry. Tools are added to chat-tier archetypes via the same OH1 delegate-synth pipeline. Sub-agent execution reuses OH1's swarm.Manager.

**Files**:
- new `internal/agent/tools/swarm/create_subagent.go` — schema + Execute
- new `internal/agent/tools/swarm/assign_task.go` — schema + Execute
- new `internal/agent/tools/swarm/swarm_test.go`
- edit `internal/agent/agentdef/delegate.go` — chat-tier archetypes get both tools synthesized automatically
- edit `cmd/aura/app_wire.go` (or equivalent) — register the 2 tools in the central registry

**Sketch**:
```go
// internal/agent/tools/swarm/create_subagent.go
package swarm

const createSubagentDescription = "Use only when direct response/direct tools are insufficient. " +
    "Create a custom subagent with specific system prompt and name for reuse within this turn."

type createSubagentTool struct {
    reg      *agentdef.Registry
    ephem    *agentdef.EphemeralStore
}

func (t *createSubagentTool) Name() string { return "create_subagent" }
func (t *createSubagentTool) Description() string { return createSubagentDescription }
func (t *createSubagentTool) Parameters() map[string]any {
    return map[string]any{
        "type":     "object",
        "required": []string{"name", "system_prompt"},
        "properties": map[string]any{
            "name":          map[string]any{"type": "string", "description": "Unique name for this agent configuration"},
            "system_prompt": map[string]any{"type": "string", "description": "System prompt defining the agent's role, capabilities, and boundaries"},
        },
    }
}

func (t *createSubagentTool) Execute(ctx context.Context, args map[string]any) (string, error) {
    turnID := turnIDFromCtx(ctx) // injected by agent loop
    name, _ := args["name"].(string)
    sysPrompt, _ := args["system_prompt"].(string)
    if strings.TrimSpace(name) == "" || strings.TrimSpace(sysPrompt) == "" {
        return "", fmt.Errorf("create_subagent: name and system_prompt required")
    }
    def := agentdef.AgentDefinition{
        ID:            name,
        DisplayName:   name,
        Tier:          agentdef.TierWorker, // ephemeral defs are always Worker
        Prompt:        sysPrompt,
        WhenToUse:     "ephemeral subagent created at turn time",
        MaxIterations: 20,
        Tools:         agentdef.ToolScope{}, // inherits parent's whitelist via runner
        Source:        "ephemeral",
    }
    if err := t.ephem.Register(turnID, def); err != nil {
        return "", err
    }
    return fmt.Sprintf(`{"created": %q, "tier": "worker"}`, name), nil
}

// internal/agent/tools/swarm/assign_task.go
const assignTaskDescription = "Use only when direct response/direct tools are insufficient. " +
    "Launch a created subagent on a task. You can launch multiple agents concurrently by emitting multiple " +
    "assign_task tool calls in one turn — they execute in parallel. When the agent is done, it returns a single message."

type assignTaskTool struct {
    reg      *agentdef.Registry
    ephem    *agentdef.EphemeralStore
    runner   agentdef.DelegateRunner // existing from OH1-E
}

func (t *assignTaskTool) Parameters() map[string]any {
    return map[string]any{
        "type":     "object",
        "required": []string{"agent", "prompt"},
        "properties": map[string]any{
            "agent":  map[string]any{"type": "string", "description": "Specify which created agent to use"},
            "prompt": map[string]any{"type": "string", "description": "The task for the agent to perform"},
        },
    }
}

func (t *assignTaskTool) Execute(ctx context.Context, args map[string]any) (string, error) {
    turnID := turnIDFromCtx(ctx)
    name, _ := args["agent"].(string)
    prompt, _ := args["prompt"].(string)
    _, ok := t.reg.Lookup(turnID, name)
    if !ok {
        return "", fmt.Errorf("assign_task: unknown agent %q (did you call create_subagent first?)", name)
    }
    // R1: ownership boundary injection (openhuman pattern)
    wrappedPrompt := withOwnershipBoundary(prompt, name)
    // Delegate via OH1-E runner; parent_chain inherited from ctx
    return t.runner.Run(ctx, name, wrappedPrompt, "" /* modelOverride */, parentChainFromCtx(ctx))
}
```

**Tests** (6):
- `TestCreateSubagent_RegistersEphemeral`
- `TestCreateSubagent_RejectsEmptyName`
- `TestAssignTask_RunsRegisteredEphemeral` — round-trip through fake DelegateRunner
- `TestAssignTask_RejectsUnknownAgent` — clear error message
- `TestAssignTask_AppliesOwnershipBoundary` — prompt prefixed correctly
- `TestEphemeralPurgedAtTurnEnd` — second turn cannot reach first turn's defs

**Acceptance**: with a chat-tier archetype that lists `swarm` in its tool whitelist, Aura's chat surface gains `create_subagent` + `assign_task` in the LLM manifest. A live probe `"create a researcher subagent and assign it to find X"` results in 1 turn = 2 tool calls (create + assign), one final assistant message back.

**Deep refactor**: shared helper `withOwnershipBoundary(prompt, ownership)` in `swarm/common.go` so OH3-C reuses it; lint clean; `dupl` clean vs OH1's delegate.go.

---

## 4. OH3-C — Parallel fan-out via `errgroup` + 8-cap (~250 LOC) [Codex]

**Goal**: when the LLM emits multiple `assign_task` tool calls in one turn, execute them concurrently with `errgroup`, hard-capped at 8. Each child gets the ownership boundary. Result envelope follows openhuman's structured shape.

This commit is the GO-NATIVE answer to LangGraph's `Send` API + Pregel supersteps. We don't need Pregel because `errgroup` + channels handles it.

**Files**:
- new `internal/agent/tools/swarm/fanout.go` — `RunFanout(ctx, tasks, opts) (Envelope, error)`
- new `internal/agent/tools/swarm/fanout_test.go`
- edit `internal/agent/tools/swarm/assign_task.go` — when called from a turn where ≥2 `assign_task` were emitted, defer to `RunFanout` instead of synchronous execution
- edit `internal/chat/agentloop.go` — verify the existing parallel-tool-call path routes `assign_task` calls into the fanout dispatcher

**Sketch**:
```go
// internal/agent/tools/swarm/fanout.go
package swarm

const (
    MaxParallelChildren = 8 // mini-PC cap; do NOT bump without explicit user authorization
)

type FanoutTask struct {
    TaskID    string // generated by caller, sortable
    AgentName string
    Prompt    string
    Ownership string // R1: ownership boundary
}

type FanoutResult struct {
    TaskID     string  `json:"task_id"`
    AgentName  string  `json:"agent_name"`
    Success    bool    `json:"success"`
    Output     string  `json:"output,omitempty"`
    Error      string  `json:"error,omitempty"`
    Ownership  string  `json:"ownership,omitempty"`
    ElapsedMs  int64   `json:"elapsed_ms"`
    Iterations int     `json:"iterations"`
}

type FanoutEnvelope struct {
    Total     int            `json:"total"`
    Succeeded int            `json:"succeeded"`
    Failed    int            `json:"failed"`
    Results   []FanoutResult `json:"results"`
}

func RunFanout(ctx context.Context, tasks []FanoutTask, runner agentdef.DelegateRunner, parentChain []string) (FanoutEnvelope, error) {
    if len(tasks) == 0 { return FanoutEnvelope{}, fmt.Errorf("RunFanout: empty tasks") }
    if len(tasks) > MaxParallelChildren {
        return FanoutEnvelope{}, fmt.Errorf("RunFanout: %d > cap %d", len(tasks), MaxParallelChildren)
    }
    // R6: emit OTel span before fanout
    ctx, span := otel.Tracer("aura.swarm").Start(ctx, "swarm.fanout",
        trace.WithAttributes(attribute.Int("aura.fanout.children", len(tasks))))
    defer span.End()

    results := make([]FanoutResult, len(tasks))
    g, gctx := errgroup.WithContext(ctx)
    g.SetLimit(MaxParallelChildren)

    for i, t := range tasks {
        i, t := i, t
        g.Go(func() error {
            start := time.Now()
            wrappedPrompt := withOwnershipBoundary(t.Prompt, t.Ownership)
            out, err := runner.Run(gctx, t.AgentName, wrappedPrompt, "", parentChain)
            elapsed := time.Since(start).Milliseconds()
            if err != nil {
                results[i] = FanoutResult{TaskID: t.TaskID, AgentName: t.AgentName, Success: false, Error: err.Error(), Ownership: t.Ownership, ElapsedMs: elapsed}
                return nil // do NOT abort sibling tasks on single failure
            }
            results[i] = FanoutResult{TaskID: t.TaskID, AgentName: t.AgentName, Success: true, Output: out, Ownership: t.Ownership, ElapsedMs: elapsed}
            return nil
        })
    }
    _ = g.Wait() // errors swallowed into per-result Error field

    env := FanoutEnvelope{Total: len(tasks), Results: results}
    for _, r := range results {
        if r.Success { env.Succeeded++ } else { env.Failed++ }
    }
    return env, nil
}
```

**Tests** (7):
- `TestRunFanout_AllSucceed` — 3 fake children all return text
- `TestRunFanout_PartialFailure` — 1 of 3 fails; others' results still returned; envelope counts correct
- `TestRunFanout_RespectsHardCap` — 9 tasks → error
- `TestRunFanout_ConcurrentExecution` — fake runner has 200ms sleep; 5 tasks complete in <500ms (not 1000ms)
- `TestRunFanout_AppliesOwnershipBoundaryToEach`
- `TestRunFanout_CtxCancelPropagates` — parent ctx cancel → all children abort
- `TestAssignTask_BatchedIntoFanout` — when the agent loop emits 2+ `assign_task` calls in one turn, fanout dispatched (not serial)

**Acceptance**: live probe `"split this 4-source research across 4 parallel researcher subagents"` → 5 tool calls total (1 create + 4 assign), wall-clock ≈ slowest child (not sum), envelope returned with `total=4 succeeded=4 failed=0`.

**Deep refactor**: `golangci-lint run internal/agent/tools/swarm/...` clean; `dupl` clean vs OH1 delegate.go; file-size ≤600 LOC; errcheck on `runner.Run` calls.

---

## 5. OH3-D — Typed Blackboard with Letta-style ops (~250 LOC) [Codex]

**Goal**: typed shared workspace per swarm-run that supports `insert` (append-only safe) and `replace` (optimistic-concurrency via version token). **No `rethink`** — last-writer-wins is explicitly forbidden in multi-agent (Letta lesson per Scout #4 §4.2).

The blackboard is read by every sub-agent at task entry (Strands `_build_node_input()` pattern from Scout #2 §2.5) — serialized into the system prompt. Sub-agents write via two tools: `blackboard_insert` and `blackboard_replace`. They do NOT subscribe; reads are cold snapshots at entry.

**Files**:
- new `internal/agent/swarm/blackboard.go` — typed in-memory blackboard with version vector
- new `internal/agent/swarm/blackboard_test.go`
- new `internal/agent/tools/swarm/blackboard_tools.go` — `blackboard_insert` + `blackboard_replace` tool definitions
- edit `internal/agent/agentdef/delegate.go` — sub-agents get both blackboard tools in their tool set when blackboard is active
- edit `internal/agent/tools/swarm/assign_task.go` — at child task entry, serialize blackboard snapshot into system prompt

**Sketch**:
```go
// internal/agent/swarm/blackboard.go
package swarm

type Entry struct {
    Key             string    `json:"key"`
    Value           string    `json:"value"`     // JSON-encoded payload
    ProducerAgentID string    `json:"producer_agent_id"`
    Version         int64     `json:"version"`   // monotonic
    Timestamp       time.Time `json:"timestamp"`
}

type Blackboard struct {
    mu      sync.RWMutex
    swarmID string
    byKey   map[string][]Entry // append history per key
    version int64
}

func NewBlackboard(swarmID string) *Blackboard {
    return &Blackboard{swarmID: swarmID, byKey: map[string][]Entry{}}
}

// Insert is APPEND-ONLY SAFE under concurrency. Always succeeds.
func (b *Blackboard) Insert(producer, key, value string) Entry { ... }

// Replace is OPTIMISTIC-CONCURRENCY safe.
// Caller passes the version they read; if blackboard version has advanced, error.
// Returns the new entry on success.
func (b *Blackboard) Replace(producer, key, value string, expectedVersion int64) (Entry, error) {
    b.mu.Lock(); defer b.mu.Unlock()
    if b.version != expectedVersion {
        return Entry{}, fmt.Errorf("blackboard: version conflict (expected=%d, current=%d)", expectedVersion, b.version)
    }
    // proceed with replace
    ...
}

// Snapshot is cold-read-at-entry for serializing into sub-agent system prompts.
func (b *Blackboard) Snapshot() map[string][]Entry { ... }

func (b *Blackboard) Version() int64 { ... }
```

```go
// internal/agent/tools/swarm/blackboard_tools.go
type blackboardInsertTool struct{ bb *swarm.Blackboard; agentName string }

func (t *blackboardInsertTool) Parameters() map[string]any {
    return map[string]any{
        "type":     "object",
        "required": []string{"key", "value"},
        "properties": map[string]any{
            "key":   map[string]any{"type": "string", "maxLength": 128},
            "value": map[string]any{"type": "string", "maxLength": 8000},
        },
    }
}

func (t *blackboardInsertTool) Execute(...) (string, error) {
    entry := t.bb.Insert(t.agentName, key, value)
    return fmt.Sprintf(`{"key":%q,"version":%d,"appended":true}`, entry.Key, entry.Version), nil
}

type blackboardReplaceTool struct{ bb *swarm.Blackboard; agentName string }

func (t *blackboardReplaceTool) Parameters() map[string]any {
    return map[string]any{
        "type":     "object",
        "required": []string{"key", "value", "expected_version"},
        "properties": map[string]any{
            "key":              map[string]any{"type": "string"},
            "value":            map[string]any{"type": "string", "maxLength": 8000},
            "expected_version": map[string]any{"type": "integer", "description": "Version you observed; fails if blackboard advanced"},
        },
    }
}
```

**Snapshot serialization into sub-agent prompt** (in `assign_task.go`):
```go
// At child task entry, build augmented system prompt:
const blackboardHeader = "\n\n[Shared Workspace]\nOther agents have contributed:\n"
snapshot := bb.Snapshot()
if len(snapshot) > 0 {
    var b strings.Builder
    b.WriteString(blackboardHeader)
    // Render compact: key → latest value (with version + producer)
    for k, entries := range snapshot {
        latest := entries[len(entries)-1]
        fmt.Fprintf(&b, "- %s (v%d by %s): %s\n", k, latest.Version, latest.ProducerAgentID, latest.Value)
    }
    childSysPrompt = def.Prompt + b.String()
}
```

**Tests** (7):
- `TestBlackboard_Insert_AppendOnlyUnderConcurrency` — 100 goroutines insert, all succeed, history len=100
- `TestBlackboard_Replace_OptimisticConcurrencySuccess` — version match → entry replaced
- `TestBlackboard_Replace_OptimisticConcurrencyConflict` — version mismatch → error
- `TestBlackboard_Snapshot_ReturnsLatestPerKey`
- `TestBlackboard_NoRethinkAPI` — confirm there's no `Rethink()` method by reflection (regression guard)
- `TestBlackboardInsertTool_PersistsAndReturnsVersion`
- `TestAssignTask_SerializesBlackboardSnapshotIntoChildPrompt` — when blackboard has 2 entries, child's system prompt contains them

**Acceptance**: live probe with 3 parallel research subagents, each calling `blackboard_insert("source_<i>", "<finding>")` → orchestrator reads merged blackboard at next turn → final answer cites all 3. Bench: `blackboard.Version()` advances monotonically; no lost updates under concurrent inserts.

**Deep refactor**: lint clean, dupl clean, file size ≤600 LOC; mutex contention not pathological under 8 concurrent writers (bench in `_test.go` with `-bench`).

---

## 6. OH3-E — `swarmpolicy.ShouldContinue()` (~150 LOC) [Codex]

**Goal**: port Strands' `SwarmState.should_continue()` 1:1 to Go as a reusable policy struct. Used by the agent loop to decide whether to keep going after each turn. 5 orthogonal stop conditions.

This is the cleanest cross-framework convergence: Strands has it, LangGraph wishes it did, Aura inherits 5 production-validated guards for free.

**Files**:
- new `internal/agent/swarmpolicy/policy.go` — `Policy` struct + `ShouldContinue(history, elapsed) (bool, string)`
- new `internal/agent/swarmpolicy/policy_test.go`
- edit `internal/chat/agentloop.go` — at end of each turn, consult `Policy.ShouldContinue(handoffHistory, elapsedSinceStart)`; if false, break + log reason

**Sketch**:
```go
// internal/agent/swarmpolicy/policy.go
package swarmpolicy

import (
    "fmt"
    "time"
)

type Policy struct {
    MaxHandoffs                int           // default 20
    MaxIterations              int           // default 20
    ExecutionTimeout           time.Duration // default 15m
    NodeTimeout                time.Duration // default 5m
    RepetitiveHandoffWindow    int           // 0 = disabled
    RepetitiveHandoffMinUnique int
}

func DefaultPolicy() Policy {
    return Policy{
        MaxHandoffs:                20,
        MaxIterations:              20,
        ExecutionTimeout:           15 * time.Minute,
        NodeTimeout:                5 * time.Minute,
        RepetitiveHandoffWindow:    4,
        RepetitiveHandoffMinUnique: 2,
    }
}

// ShouldContinue returns (true, "") to continue, (false, reason) to stop.
// Direct port of Strands SwarmState.should_continue() (swarm.py:L90-93).
func (p Policy) ShouldContinue(history []string, elapsed time.Duration) (bool, string) {
    if p.MaxHandoffs > 0 && len(history) >= p.MaxHandoffs {
        return false, fmt.Sprintf("max_handoffs=%d reached", p.MaxHandoffs)
    }
    if p.MaxIterations > 0 && len(history) >= p.MaxIterations {
        return false, fmt.Sprintf("max_iterations=%d reached", p.MaxIterations)
    }
    if p.ExecutionTimeout > 0 && elapsed > p.ExecutionTimeout {
        return false, fmt.Sprintf("execution_timeout=%s exceeded", p.ExecutionTimeout)
    }
    if p.RepetitiveHandoffWindow > 0 && len(history) >= p.RepetitiveHandoffWindow {
        recent := history[len(history)-p.RepetitiveHandoffWindow:]
        unique := uniqueCount(recent)
        if unique < p.RepetitiveHandoffMinUnique {
            return false, fmt.Sprintf("repetitive: %d unique in last %d (ping-pong)", unique, p.RepetitiveHandoffWindow)
        }
    }
    return true, ""
}

func uniqueCount(xs []string) int {
    s := make(map[string]struct{}, len(xs))
    for _, x := range xs { s[x] = struct{}{} }
    return len(s)
}
```

**Tests** (6):
- `TestPolicy_HitMaxHandoffs`
- `TestPolicy_HitMaxIterations`
- `TestPolicy_HitExecutionTimeout`
- `TestPolicy_RepetitiveHandoffDetector_PingPong` — A→B→A→B with window=4, min=2 → stop
- `TestPolicy_RepetitiveHandoffDetector_HealthyChain` — A→B→C→D with window=4, min=2 → continue
- `TestPolicy_DefaultsApplied` — `Policy{}` does NOT stop (zero values disabled)

**Acceptance**: agent loop with adversarial fanout that handsoff to itself 30 times in a turn → stopped at iteration 20 with structured log reason; ping-pong A↔B for 6 turns → stopped at turn 5 (window=4) with reason `"repetitive: 1 unique in last 4 (ping-pong)"`.

**Deep refactor**: lint clean; file ~80 LOC of code + 70 LOC of tests; package isolation (no agent/agentdef import — pure policy).

---

## 7. OH3-F — OTel GenAI spans + Critical Steps metric + custom Aura attrs (~300 LOC) [Codex]

**Goal**: instrument every LLM call, tool call, and delegate invocation with OTel-compliant spans. Compute and log Kimi's Critical Steps metric per turn. Emit 6 custom Aura attributes for multi-agent visibility.

This is mandatory before OH3 ships (per AP-10 from Scout #4 §6 — observability collapse is unrecoverable post-incident).

**Files**:
- new `internal/observ/otel.go` — OTel SDK bootstrap (tracer provider, OTLP exporter, batch processor)
- new `internal/observ/attrs.go` — Aura attribute keys (constants)
- new `internal/observ/critical_steps.go` — `CriticalSteps` accumulator + `Compute()`
- new `internal/observ/otel_test.go`
- edit `internal/chat/agentloop.go` — wrap each turn in a root span, each LLM/tool/delegate in nested spans
- edit `internal/agent/tools/swarm/fanout.go` — emit fanout span with child count + per-child sibling spans
- edit `internal/agent/agentdef/delegate.go` — emit delegate span with handoff.from/to + delegation.depth
- edit `cmd/aura/app.go` — initialize OTel SDK at boot; gracefully no-op if `OTEL_EXPORTER_OTLP_ENDPOINT` unset
- edit `compose.yaml` — add `langfuse` sidecar gated behind `--profile langfuse` (no default-on)
- edit `internal/db/migrations/NNNN_add_trace_id.sql` — add `trace_id TEXT` column to `conversations` table

**Sketch**:
```go
// internal/observ/attrs.go
package observ

import "go.opentelemetry.io/otel/attribute"

// Standard OTel GenAI attributes (2026 spec)
const (
    AttrAgentID         = attribute.Key("gen_ai.agent.id")
    AttrAgentName       = attribute.Key("gen_ai.agent.name")
    AttrConversationID  = attribute.Key("gen_ai.conversation.id")
    AttrSystem          = attribute.Key("gen_ai.system")
    AttrRequestModel    = attribute.Key("gen_ai.request.model")
    AttrInputTokens     = attribute.Key("gen_ai.usage.input_tokens")
    AttrOutputTokens    = attribute.Key("gen_ai.usage.output_tokens")
)

// Custom Aura attributes (non-standard; pending OTel GenAI extension)
const (
    AttrHandoffFrom      = attribute.Key("aura.handoff.from")
    AttrHandoffTo        = attribute.Key("aura.handoff.to")
    AttrDelegationDepth  = attribute.Key("aura.delegation.depth")
    AttrHandoffPktTokens = attribute.Key("aura.handoff.packet_size_tokens")
    AttrBlackboardVer    = attribute.Key("aura.blackboard.version")
    AttrBudgetRemaining  = attribute.Key("aura.budget.tokens_remaining")
)

// internal/observ/critical_steps.go
type CriticalSteps struct {
    mu      sync.Mutex
    stages  []stage
}

type stage struct {
    orchSteps int
    subSteps  []int // one per parallel sub-agent in this stage
}

func (c *CriticalSteps) BeginStage()       { ... }
func (c *CriticalSteps) AddOrchStep()      { ... }
func (c *CriticalSteps) AddSubSteps(n int) { ... } // called once per child with their total step count

// Compute returns the Kimi Critical Steps metric:
//   Σ_stages [ orchSteps + max(subSteps) ]
func (c *CriticalSteps) Compute() int {
    c.mu.Lock(); defer c.mu.Unlock()
    total := 0
    for _, s := range c.stages {
        m := 0
        for _, n := range s.subSteps { if n > m { m = n } }
        total += s.orchSteps + m
    }
    return total
}
```

**Migration** (new file `internal/db/migrations/NNNN_add_trace_id.sql`):
```sql
ALTER TABLE conversations ADD COLUMN trace_id TEXT;
CREATE INDEX IF NOT EXISTS idx_conversations_trace_id ON conversations(trace_id);
```

**compose.yaml addition** (gated):
```yaml
langfuse:
  image: langfuse/langfuse:3
  profiles: ["langfuse"]
  environment:
    DATABASE_URL: ${LANGFUSE_DB_URL}
    NEXTAUTH_SECRET: ${LANGFUSE_SECRET}
    SALT: ${LANGFUSE_SALT}
  ports: ["3001:3000"]
  depends_on: [langfuse-db]
langfuse-db:
  image: postgres:16
  profiles: ["langfuse"]
  environment: { POSTGRES_USER: langfuse, POSTGRES_PASSWORD: ${LANGFUSE_DB_PASS}, POSTGRES_DB: langfuse }
  volumes: ["langfuse-pg:/var/lib/postgresql/data"]
volumes: { langfuse-pg: {} }
```

**Tests** (8):
- `TestOTel_NoExporterSet_NoOp` — bootstrap with no env → tracer provider is no-op, doesn't panic
- `TestOTel_BootstrapsWithEndpoint` — env set → real provider, real exporter, batch processor
- `TestCriticalSteps_SingleStageNoSubagents` — 5 orch steps, 0 sub → 5
- `TestCriticalSteps_SingleStageParallelSubs` — 5 orch + [3,7,2] subs → 5 + max(3,7,2) = 12
- `TestCriticalSteps_MultiStage` — [5 orch + [3,7]] then [2 orch + [1]] → (5+7)+(2+1) = 15
- `TestSpanAttrs_HandoffFromTo` — delegate span has both attrs set
- `TestMigration_AddsTraceIdColumn`
- `TestAgentLoop_RootSpanCarriesTraceIdToConversationsTable`

**Acceptance**: live Telegram turn with 3-parallel fan-out → Langfuse UI (when `--profile langfuse` enabled) shows nested span tree: root coordinator → llm call → fanout → 3 sibling agent spans → each with their own llm + tool spans → final tool. `conversations.trace_id` populated. `CriticalSteps` value logged per turn ≤ `total_tool_calls`.

**Deep refactor**: lint clean across new `internal/observ/`; OTel SDK is the ONLY new external dep — verify `go.mod` adds only `go.opentelemetry.io/otel` + `go.opentelemetry.io/otel/sdk` + `go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc`; no new replace directives.

---

## 8. OH3-G — 3-tier budget gates + cost preflight (~200 LOC) [Codex]

**Goal**: prevent the RelayPlane $47K-pattern runaway. Three independent ceilings: per-turn, per-conversation, per-agentdef-daily. Preflight gate refuses a fan-out of N children if remaining-turn-budget < N × expected-child-cost.

Per Scout #4 §6 AP-3: this is the most-cited production failure mode and the cheapest mitigation.

**Files**:
- new `internal/agent/budget/ledger.go` — 3-tier ledger backed by SQLite
- new `internal/agent/budget/ledger_test.go`
- new `internal/agent/budget/preflight.go` — `PreflightFanout(remaining, n, expectedPerChild) error`
- new `internal/db/migrations/NNNN_budget_ledger.sql` — `budget_usage` table
- edit `internal/agent/tools/swarm/fanout.go` — call preflight before dispatching
- edit `internal/chat/agentloop.go` — at every LLM call, increment per-turn + per-conversation + per-agentdef ledger; check ceilings
- edit `internal/api/dashboard.go` — expose `/api/budget` endpoint with live remaining (not daily aggregate)

**Sketch**:
```sql
-- migrations/NNNN_budget_ledger.sql
CREATE TABLE IF NOT EXISTS budget_usage (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    scope           TEXT NOT NULL CHECK (scope IN ('turn', 'conversation', 'agentdef_daily')),
    scope_key       TEXT NOT NULL,      -- turn_id | conversation_id | agentdef_id|YYYY-MM-DD
    tokens_used     INTEGER NOT NULL DEFAULT 0,
    tool_calls_used INTEGER NOT NULL DEFAULT 0,
    dollars_cents   INTEGER NOT NULL DEFAULT 0, -- track cost too
    updated_at      DATETIME DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(scope, scope_key)
);
CREATE INDEX IF NOT EXISTS idx_budget_scope_key ON budget_usage(scope, scope_key);
```

```go
// internal/agent/budget/ledger.go
package budget

type Ceiling struct {
    Tokens    int
    ToolCalls int
    Dollars   int // cents
}

type Ledger struct {
    db *sql.DB
}

type Usage struct {
    Tokens, ToolCalls, DollarsCents int
}

func (l *Ledger) Add(scope, key string, delta Usage) error { ... }
func (l *Ledger) Get(scope, key string) (Usage, error) { ... }

// Check returns nil if remaining > 0 across all dimensions, error otherwise.
func (l *Ledger) Check(scope, key string, ceiling Ceiling) error {
    u, _ := l.Get(scope, key)
    if ceiling.Tokens > 0 && u.Tokens >= ceiling.Tokens {
        return fmt.Errorf("%s ceiling exceeded: %d/%d tokens", scope, u.Tokens, ceiling.Tokens)
    }
    if ceiling.ToolCalls > 0 && u.ToolCalls >= ceiling.ToolCalls {
        return fmt.Errorf("%s ceiling exceeded: %d/%d tool_calls", scope, u.ToolCalls, ceiling.ToolCalls)
    }
    if ceiling.Dollars > 0 && u.DollarsCents >= ceiling.Dollars {
        return fmt.Errorf("%s ceiling exceeded: $%.2f / $%.2f", scope, float64(u.DollarsCents)/100, float64(ceiling.Dollars)/100)
    }
    return nil
}

// internal/agent/budget/preflight.go
func PreflightFanout(remaining Ceiling, n int, expectedPerChild Usage) error {
    if n <= 0 { return nil }
    needed := Usage{
        Tokens:       n * expectedPerChild.Tokens,
        ToolCalls:    n * expectedPerChild.ToolCalls,
        DollarsCents: n * expectedPerChild.DollarsCents,
    }
    if remaining.Tokens > 0 && needed.Tokens > remaining.Tokens {
        return fmt.Errorf("fanout preflight: %d children × %d tokens = %d > remaining %d",
            n, expectedPerChild.Tokens, needed.Tokens, remaining.Tokens)
    }
    // same for tool_calls + dollars
    return nil
}
```

**Defaults** (overridable in dashboard settings):
- Per-turn: 50k tokens, 50 tool_calls, $0.50
- Per-conversation: 500k tokens, 500 tool_calls, $5.00
- Per-agentdef-daily: configurable per archetype; defaults: chat unlimited, worker 2M tokens, ephemeral 100k tokens

**Tests** (8):
- `TestLedger_AddAccumulates`
- `TestLedger_CheckFailsOnExceeded`
- `TestPreflightFanout_AcceptsIfFits`
- `TestPreflightFanout_RejectsIfWouldOverrun`
- `TestPreflightFanout_NSmallerThanCeilingOK`
- `TestAgentLoop_IncrementsPerTurn`
- `TestAgentLoop_StopsOnPerConversationCeiling`
- `TestDashboard_LiveBudgetEndpoint` — `GET /api/budget?conversation_id=X` returns live remaining

**Acceptance**: probe with adversarial prompt that tries to fan out 8 children when only 2 children worth of budget remains → preflight rejects with structured error, agent loop continues with single delegate fallback. Dashboard `/api/budget` returns live remaining per dimension within 1s of last LLM call.

**Deep refactor**: lint clean; new migration tested in `cmd/debug_*` smoke; no breaking changes to existing conversations/api_tokens tables.

---

## 9. OH3-H — Ship gate: live Telegram probe + MAST failure tagging (~150 LOC) [Codex]

**Goal**: end-to-end smoke + structured probe that exercises the full Wave OH3 surface (ephemeral spawn + parallel fan-out + blackboard + budget + OTel) on a live Telegram conversation. Adds MAST taxonomy tagging to the probe harness so future regressions are categorical, not just pass/fail.

This is the ship gate. Without it, Wave OH3 doesn't merge.

**Files**:
- new `cmd/probe_swarm/main.go` — live probe runner
- new `cmd/probe_swarm/mast.go` — `ClassifyFailure(reply, traceID) MASTFailureMode`
- new `cmd/probe_swarm/scenarios.json` — 5 canonical scenarios (research/synth/critic/etc.)
- edit `Makefile` (or equivalent) — `make probe-swarm` target

**Sketch**:
```go
// cmd/probe_swarm/mast.go
package main

type MASTFailureMode string

const (
    FM_1_2_DisobeyRole         MASTFailureMode = "FM-1.2-disobey-role"
    FM_1_5_UnawareOfTermination MASTFailureMode = "FM-1.5-unaware-of-termination"
    FM_2_2_NoClarification     MASTFailureMode = "FM-2.2-no-clarification"
    FM_2_3_TaskDerailment      MASTFailureMode = "FM-2.3-task-derailment"
    FM_2_4_InfoWithholding     MASTFailureMode = "FM-2.4-info-withholding"
    FM_2_5_IgnoredPeerInput    MASTFailureMode = "FM-2.5-ignored-peer-input"
    FM_3_1_PrematureTermination MASTFailureMode = "FM-3.1-premature-termination"
    FM_3_2_NoVerification      MASTFailureMode = "FM-3.2-no-verification"
    FM_3_3_IncorrectVerif      MASTFailureMode = "FM-3.3-incorrect-verification"
    // ... 14 modes total per arxiv 2503.13657
)

func ClassifyFailure(reply string, trace *Trace) []MASTFailureMode {
    var modes []MASTFailureMode
    // Heuristics on trace structure:
    if trace.TotalToolCalls >= trace.Policy.MaxIterations {
        modes = append(modes, FM_1_5_UnawareOfTermination)
    }
    if trace.DelegationDepth > 3 { /* etc */ }
    if reply == "" || strings.Contains(reply, "couldn't process") {
        modes = append(modes, FM_3_1_PrematureTermination)
    }
    // ...
    return modes
}
```

**Canonical scenarios** (`scenarios.json`):
1. **single-agent baseline** — "summarize page X" → no fanout, no blackboard
2. **2-parallel research** — "find me 2 different perspectives on Y" → 2-child fanout, both insert to blackboard
3. **5-parallel wide research** — stress the 8-cap; assert exactly 5 ran in parallel
4. **adversarial ping-pong** — prompt designed to cause A↔B handoff loop → assert `should_continue` stops at window=4
5. **adversarial cost-blowout** — prompt designed to fan out 8 children with budget for only 2 → assert preflight rejects, fallback to single delegate

**Tests** (in CI):
- `make probe-swarm` runs all 5 scenarios against a real Aura container (compose test profile)
- Each scenario asserts: reply contains expected substring, latency within budget, critical_steps within bound, tool_calls within bound, NO unexpected MAST failure modes
- Trace artifact (OTLP JSON) saved per run to `D:/Aura/.probe-runs/oh3/<timestamp>/`

**Acceptance**: all 5 scenarios PASS strict mode (substring AND latency AND tool_count AND no-mast-failure) for 3 consecutive runs. Telegram screenshot of scenario #2 shows announce + final result (no child tool trace) per OH1-F scrubber.

**Deep refactor**: lint clean across new `cmd/probe_swarm/`; reuse `cmd/probe_chat` helpers where they apply (don't duplicate the Aura client setup).

---

## 10. Wave-OH3 ship gate

After OH3-H green:

- All 5 probe scenarios PASS strict for 3 consecutive runs (substring + latency + tool_count + no-MAST-failure)
- Live Telegram probe scenario #2 shows announce + final result, no child trace leakage
- `conversations.trace_id` populated on every turn for 7+ days of dogfood; no NULL outside ingestion startup window
- `critical_steps` metric in archive ≤ `total_tool_calls` invariant holds across all stored turns
- Dashboard `/api/budget` live; live alert fires when 80% of any ceiling reached
- Langfuse profile (when enabled) renders multi-agent spans with correct parent-child + sibling shape
- `MEMORY.md` index gains an OH3 closure entry referencing this plan + the 4 research docs
- `project_aura_dgx_spark_bundle_vision` memory updated: parallel multi-agent runtime is real
- `.planning/post-drift-2026-05-21/INDEX.md` updated: parallel multi-agent substrate landed (the bit beyond what OH1 closed)

---

## 11. How to drive

**Inline in Claude session**: I (or you) walk commit-by-commit through this doc. Each commit is small enough to fit one context-wise. Lefthook gates per commit.

**Via Codex CLI** (per memory `reference_codex_cli_pipe`):
```bash
codex exec --sandbox workspace-write --skip-git-repo-check "$(cat <<'EOF'
Read docs/aura-oh3-codex-plan-2026-05-25.md section "OH3-A".
Implement exactly that commit (files + sketch + tests). Run go vet + go test
on touched packages before committing. Use the commit body template at the
end of the section. Do NOT touch anything outside the files listed. Do NOT
touch files owned by Wave OH1 (internal/agent/agentdef/registry.go beyond
the documented Lookup addition).
EOF
)"
```

Switch sections per commit (`OH3-B`, `OH3-C`, etc.).

**Commit body template** (every commit):
```
<type>(<scope>): <subject>

Per docs/aura-oh3-codex-plan-2026-05-25.md commit <ID>.
Locked decisions: <which of the 10 OH3-S0 locks this commit touches>.
Research anchor: <one or more of the 4 scout docs that justify the design>.

Files: <list>
Tests added: <names>
LOC delta: code <X>, test <Y>

Anti-pattern guards: <which AP-N from Scout #4 §6 this commit mitigates>

<2-3 sentence rationale tying back to a scout finding>

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
```

---

## 12. Hold conditions (stop the wave)

Stop OH3 and re-plan if ANY of the following triggers:

- Any commit's lefthook fails twice in a row → STOP, escalate
- OH3-C fanout latency under 5-child load > 2× longest child (concurrency broken) → STOP, profile before fixing
- OH3-D blackboard shows lost updates under 8 concurrent writers in bench → STOP, mutex/sharding design wrong
- OH3-E `should_continue` allows known ping-pong fixture to run past iteration 5 → STOP, regression
- OH3-F OTel span tree shows wrong parent-child in Langfuse → STOP, span context propagation broken
- OH3-G preflight gate triggers >5% false-positive on canonical scenarios → STOP, tune expected-cost-per-child heuristics
- Any OH3-H probe test goes flaky (≥2/10 runs) → STOP, swarm timing/race condition unfixed
- Cognition-side warning materializes: a Wave-OH3 fanout produces incoherent merged output on a benign read-only task (Flappy Bird mode) → STOP, restrict to read-only contexts harder
- Cost ceiling alert from real user dogfood within first 7 days of OH3 ship → STOP, lower defaults

---

## 13. NOT in scope (Wave OH4 or later)

Surface-anchored to keep the wave bounded:

- **True peer-mesh handoff** (Strands `Swarm` sticky-active-agent style across turns) — Wave OH4. Requires durable `active_agent` per conversation + LangGraph-style sticky router. Cognition warning still applies; gate on user explicit request.
- **Agent-Teams-style sequential specialists** (Shen et al. 2603.29632v2 Algorithm 2) — Wave OH4. Blueprint: 3 fixed specialist roles + 6-turn relay over a shared worktree + **dedicated Engineer agent** that activates only on crash (circuit-breaker pattern). Shen et al. Fig 2 shows this produces "deep theoretical alignment necessary for complex architectural refactoring" but at higher fragility cost vs OH3's Subagent mode. Lift target when OH4 opens: the Engineer-on-crash circuit breaker + Git worktree isolation per agent + Structured Patch Contract (Aura already has `propose_patch`).
- **Mid-flight peer chat** between sub-agents (kimi.com explicit "future work") — Wave OH4+.
- **Cross-conversation blackboard** (Letta-style shared memory blocks across sessions) — Wave OH4+; depends on persistent blackboard substrate.
- **Persistent ephemeral agentdefs** across turns — current OH3 purges at turn end. Crossing turns requires durable storage + GC policy.
- **Langfuse default-on** — profile-gated only in OH3. Default-on requires ops doc + auth model.
- **A2A remote-agent handshake** (Strands issue #913 not yet shipped upstream) — Wave OH5+, depends on upstream contract.
- **100+ subagent scale** — mini-PC budget forbids. Move to Wave OH-DGX when DGX Spark bundle ships (`project_aura_dgx_spark_bundle_vision`).
- **RL-style reward training (PARL)** — Aura has no RL infra. The replacement is prompt-level "spawn when X" heuristics. Permanently out of scope without external training infrastructure.
- **Hybrid AdaptOrch topology routing** (DAG-structural-properties → topology) — premature without a DAG decomposition phase; Wave OH5+.
- **Critic-with-different-model** as default — OH3 ships the API but defaults to single endpoint. Different-model critics are user-config Wave OH4.
- **Hierarchical multi-level spawn** (level-k spawns level-(k+1) per HALO/AgentSpawn/DEPART/LAMO) — explicit anti-pattern per Scout #1 §2.9 (Kimi + openhuman both forbid). Will not be added.

---

## 14. Cross-cutting design rules (apply to every OH3 commit)

These are the user's standing rules from `CLAUDE.md` + memory, restated for OH3 emphasis:

1. **Single trace_id per Telegram turn**, propagated through every span. OH3-F enforces; every other commit consults.
2. **English-only prompts** (per `feedback_all_prompts_in_english_only`). Aura output to user is Italian via explicit directive, NOT mixed prose.
3. **No new regex on natural language** (per `feedback_no_regex_for_nlp`). Use structured tool/registry signals.
4. **No fast-path classifier** to bypass the agent loop. Spawn decisions stay inside the loop.
5. **Validate with verified benchmarks, not tool-call counts** — every OH3-H probe asserts ≥1 ground-truth fact (filesystem / DB / API artifact), not just `tool_calls ≥ N`.
6. **One slice = one commit**, no batching. Per `feedback_one_module_per_slice`.
7. **Deep refactor on touch** — every commit lints + dupl-checks + LOC-bounds the files it edits, per CLAUDE.md.
8. **Bugs found mid-OH3 are fixed in same session**, never deferred. Per `feedback_codex_more_precise_than_claude` + CLAUDE.md.

---

## 15. Provenance

This plan is the synthesis of 4 parallel scout reports run 2026-05-25:

- **Scout #1** (`docs/aura-oh3-research-tmp-lift-2026-05-25.md`): D:/tmp source extraction — confirmed openhuman ships fan-out (lift target), Kimi K2.5 paper ships frozen subagents, Elysia Environment is closest blackboard primitive, 8 anti-patterns converged across sources.
- **Scout #2** (`docs/aura-oh3-research-strands-langgraph-2026-05-25.md`): Strands Swarm + LangGraph Swarm deep-dive — Strands `should_continue()` is the cleanest portable termination; tool-injection model maps 1:1 to Aura's executor; LangGraph's Pregel doesn't port to Go.
- **Scout #3** (`docs/aura-oh3-research-kimi-openai-2026-05-25.md`): Kimi K2.5/K2.6 + OpenAI Agents SDK — Kimi ships 2-tool API verbatim (`create_subagent` + `assign_task`); subagents frozen; peer chat is future work; OpenAI `Agent.as_tool` is the right shape, `handoff` is anti-pattern for swarm.
- **Scout #4** (`docs/aura-oh3-research-production-antipatterns-2026-05-25.md`): CrewAI/AutoGen/Letta + 12 anti-patterns + OTel — RelayPlane $47K cost incident pattern, Cognition vs Anthropic split, Letta blackboard ops semantics, OTel GenAI 2026 status, MAST failure taxonomy.
- **Shen et al. 2026** ([arXiv 2603.29632v2](https://arxiv.org/html/2603.29632v2)): "An Empirical Study of Multi-Agent Collaboration for Automated Research" — controlled head-to-head benchmark of Subagent mode (OH3's choice) vs Agent Teams (deferred OH4) vs single-agent baseline on a fixed-budget ML optimization task. Provides the empirical backing for OH3 lock #1 (Subagents win on stability per Fig 3) AND surfaces R7 (greedy local optimization fixation when subagent prompts aren't diversified, §4 Fig 2). Also catalogs the Agent-Teams Algorithm 2 blueprint as the lift target for Wave OH4.

Total research artifacts: ~3,500 lines across 4 scout docs + 1 external paper. Total citations: ~120 URLs + ~30 line refs into local D:/tmp source + 1 controlled empirical study. This plan extracts ~700 lines of execution-ready instructions from that base.
