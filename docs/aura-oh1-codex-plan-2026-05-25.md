# Aura — Wave OH1 Codex execution plan (2026-05-25)

Codex-ready, GSD-free. Each commit below is a self-contained Codex
prompt: file paths, struct sketches, tests, acceptance criteria,
deep-refactor checklist. Drive via `codex exec --skip-git-repo-check
--sandbox workspace-write "<prompt>"` one commit at a time, OR Claude
inline in this session.

Anchors: `docs/aura-oh1-research-synthesis-2026-05-25.md` (synthesis),
`docs/research-aura-swarm-integration-map-2026-05-25.md` (integration
map), `docs/aura-graph-tools-plan-2026-05-25.md` §5 (master plan).

---

## 0. Locked decisions (the 7 OH1-S0 answers)

| # | Question | LOCKED |
|---|---|---|
| 1 | File format for archetypes | **JSON** (Aura ops convention, no new TOML dep — `grep BurntSushi/toml go.mod` = 0 hits) |
| 2 | Tool synth schema | **`{prompt: string, model?: string}`** (per-call model pin = high UX leverage for ~1 line) |
| 3 | Inherit semantics | **Whitelist** `inherit_*` (default `false`). Forward-compatible vs blacklist |
| 4 | Default tier on missing annotation | **Worker** (fail-closed; worker-cannot-have-subagents trips at boot) |
| 5 | DEDUP timing | **Folded into commit E** (collision regression test in same commit) |
| 6 | REFLECTION sequencing | **Pre-OH1 fork** (Wave-RFL ships first; migrate to `reflector` archetype in commit G) |
| 7 | swarm.Manager merge | **Via `swarm.Manager.Run`** (reuses ~500 LOC, bump `defaultMaxDepth` 1→3) |

Plus 4 hardcoded from research:
- **R7** Every synth tool description prepended with: `"Use only when direct response/direct tools are insufficient. "` (openhuman over-delegation lever).
- **R8** Channel-scrubber + announce template lifted from nanobot.
- **R6** Parent-id stack cycle detector.
- **R3** `MaxInputTokens` / `MaxOutputTokens` per archetype.

---

## 1. Commit sequence (10 atomic commits)

```
RFL-S1 → OH1-A → OH1-B → OH1-C → OH1-D → OH1-E → OH1-F → OH1-G → OH1-H → OH1-I
   200      400     150     80      80     600     80      30      0       50    LOC (net code)
```

Test LOC budget: ~470 across the whole wave. Each commit lefthook-green
(0 dupl, 0 lint, file-size ≤600 LOC, errcheck where applicable).

### Status snapshot (2026-05-25)

| Commit | Status | Hash | Notes |
|---|---|---|---|
| RFL-S1 | ✅ shipped | `d7bb7aa0` | `feat(learning): add post-turn reflection fork` — code +309/-1, test +156. Hook persistent in web + Telegram. |
| OH1-A | ✅ shipped | `02d390a7` | `feat(agent): add agentdef registry skeleton` — registry boots empty, no manifest delta. |
| OH1-B | ✅ shipped | `45c5fe02` | `feat(agent): migrate summarizer to agentdef` — first archetype; legacy `agents/summarizer/prompt.go` now a shim. |
| OH1-C | ✅ shipped | `d0b24989` | `feat(agent): warn on agentdef tier violations` — warn-only validator + `DetectCycle(rootID)`. |
| OH1-D | ✅ shipped | `a82f5284` | `feat(agent): enforce agentdef tier validation` — boot uses `EnforceTier: true`; summarizer still validates. |
| OH1-E | ✅ shipped | `aec6f61d` | `feat(agent): synthesize agentdef delegate tools` — delegate-tool synth + DEDUP + over-delegation prefix + `swarm.Manager` maxDepth 1→3 + Assignment extensions. |
| OH1-F | ✅ shipped | `13a59625` | `feat(agent): announce agentdef delegation` — channel scrubber + announce template for Telegram + web. |
| OH1-G | ✅ shipped | `52bd2c99` | `feat(agent): migrate reflection to reflector archetype` — builtin `reflector` + reflection hook uses AGENTDEF delegate runner with direct fallback parity. |
| OH1-H | ✅ closed (no-op) | _(plan-only closure; fill hash after commit)_ | Prompt surface already clean: `TOOLS.md` is retired/ignored and active/default prompt files do not mention `spawn_aurabot` / `run_aurabot_swarm`. |
| OH1-I | ⬜ pending (post-telemetry) | — | Remove deprecated swarm tools after 7+ days of zero invocations. |

**Freshness rule:** update this snapshot immediately after every atomic commit,
and mark the currently edited slice as in-flight before continuing.

**Next action:** run the post-G/H live probe from the Wave-OH1 ship gate, then
hold OH1-I until telemetry shows 7+ days of zero `spawn_aurabot` /
`run_aurabot_swarm` invocations.

---

## 2. RFL-S1 — REFLECTION-FORK (pre-OH1, ~200 LOC) [Codex] — ✅ shipped `d7bb7aa0`

**Goal**: ship hermes-style post-turn reflection NOW without coupling
to AGENTDEF. Migrates to `reflector` archetype in commit G.

**Files**:
- new `internal/learning/reflection_fork.go` (extraction prompt + LLM call)
- new `internal/learning/reflection_fork_test.go`
- edit `internal/agent/posthook.go` — register the new hook alongside
  `HeuristicPostTurnHook`

**Sketch**:
```go
// internal/learning/reflection_fork.go
package learning

type ReflectionExtract struct {
    Observations     []string `json:"observations"`
    Patterns         []string `json:"patterns"`
    UserPreferences  []string `json:"user_preferences"`
    UserReflections  []string `json:"user_reflections"`
}

type ReflectionHook struct {
    Client          llm.Client
    Model           string
    MaxPerSession   int        // default 5
    MinTurnTokens   int        // default 200 — skip trivial turns
    sessionCount    map[string]int
    mu              sync.Mutex
}

func (h *ReflectionHook) Apply(ctx context.Context, turn agent.TurnRecord, store *memoryindex.Store) []error {
    // 1. Throttle check (sessionCount, MaxPerSession)
    // 2. Token-floor check
    // 3. Build extraction prompt: ask LLM for JSON-only output
    // 4. Parse, validate, persist via store with kind="reflection"
}
```

**Extraction prompt (canonical, English only)**:
```
You are an extraction assistant. Read the conversation turn below and
emit STRICT JSON with four arrays:
  - observations: factual things you noticed about the user or context
  - patterns: recurring behaviors or preferences (not single events)
  - user_preferences: explicit or strongly-implied likes/dislikes
  - user_reflections: things the user said about themselves

Rules: JSON only. No prose. Empty arrays allowed. Max 3 items per array.
Each item ≤ 200 chars. Do NOT capture PII or third-party names.
```

**Tests** (5):
- `TestReflectionHook_HonorsMaxPerSession` — fires 5 times, 6th call no-op
- `TestReflectionHook_SkipsTrivialTurns` — turn with <MinTurnTokens skipped
- `TestReflectionHook_PersistsValidJSON` — round-trip valid extraction
- `TestReflectionHook_RejectsMalformedJSON` — LLM emits garbage → no write
- `TestReflectionHook_EmptyExtractionAccepted` — `{}` is valid no-op

**Acceptance**: hook registered in posthook; integration test with fake
LLM client persists 1 row in `memoryindex` per qualifying turn.

**Deep refactor**: lint clean, dupl clean, errcheck on LLM call.

---

## 3. OH1-A — AGENTDEF registry empty + loader (~400 LOC) [Codex] — ✅ shipped `02d390a7`

**Goal**: ship the registry skeleton with ZERO archetypes. Boot doesn't
break. Loader + validator + dedup logic in place ready for commit B.

**Files**:
- new `internal/agent/agentdef/definition.go` — struct
- new `internal/agent/agentdef/loader.go` — disk + embedded loader
- new `internal/agent/agentdef/registry.go` — in-memory registry
- new `internal/agent/agentdef/validator.go` — boot-time validation
- new `internal/agent/agentdef/*_test.go`
- edit `internal/agent/runtime.go` — wire `agentdef.NewRegistry(ctx, logger)` into boot

**Sketch**:
```go
// internal/agent/agentdef/definition.go
package agentdef

type AgentTier string
const (
    TierChat      AgentTier = "chat"
    TierReasoning AgentTier = "reasoning"
    TierWorker    AgentTier = "worker"  // default — fail-closed
)

type AgentDefinition struct {
    ID              string         `json:"id"`
    DisplayName     string         `json:"display_name,omitempty"`
    WhenToUse       string         `json:"when_to_use"`
    PromptPath      string         `json:"prompt_path,omitempty"` // relative to definition file
    PromptInline    string         `json:"prompt,omitempty"`      // alternative to PromptPath
    ModelHint       string         `json:"model_hint,omitempty"`
    Temperature     *float64       `json:"temperature,omitempty"`
    Tools           ToolScope      `json:"tools"`
    Subagents       []SubagentRef  `json:"subagents,omitempty"`
    Tier            AgentTier      `json:"tier,omitempty"` // default = Worker
    MaxIterations   int            `json:"max_iterations,omitempty"`
    MaxInputTokens  int            `json:"max_input_tokens,omitempty"`
    MaxOutputTokens int            `json:"max_output_tokens,omitempty"`
    MaxResultChars  int            `json:"max_result_chars,omitempty"`
    // Whitelist (default false for all)
    InheritIdentity bool `json:"inherit_identity,omitempty"`
    InheritMemory   bool `json:"inherit_memory,omitempty"`
    InheritSafety   bool `json:"inherit_safety,omitempty"`
    InheritSkills   bool `json:"inherit_skills,omitempty"`
    InheritProfile  bool `json:"inherit_profile,omitempty"`
    // Resolved at load time (not serialised)
    Prompt          string         `json:"-"`
    Source          string         `json:"-"` // "builtin" | "user"
}

type ToolScope struct {
    Named []string `json:"named,omitempty"` // explicit allowlist; empty = empty list (fail-closed)
}

type SubagentRef struct {
    ID           string `json:"id"`
    DelegateName string `json:"delegate_name,omitempty"` // default = "delegate_" + ID
}

// internal/agent/agentdef/loader.go
type Loader struct {
    BuiltinFS    embed.FS      // internal/agent/agentdef/builtin/*
    UserDir      string        // runtime-workspace/agents/
    Logger       *slog.Logger
}

func (l *Loader) Load(ctx context.Context) ([]AgentDefinition, error) {
    // 1. Walk BuiltinFS → parse each agent.json + read prompt.md
    // 2. Walk UserDir → parse user overrides
    // 3. User override beats builtin by ID (last-write-wins on user side)
    // 4. Apply defaults (Tier=Worker, all Inherit*=false, MaxIter=20, MaxResultChars=24000)
    // 5. Resolve PromptPath → Prompt (read prompt.md sibling)
    // 6. Return list, NOT registered yet
}

// internal/agent/agentdef/registry.go
type Registry struct {
    byID map[string]AgentDefinition
    mu   sync.RWMutex
}

func NewRegistry(ctx context.Context, loader *Loader, validator *Validator) (*Registry, error) {
    defs, err := loader.Load(ctx)
    if err != nil { return nil, err }
    if errs := validator.ValidateAll(defs); len(errs) > 0 {
        return nil, fmt.Errorf("agentdef: %d validation errors: %v", len(errs), errs)
    }
    reg := &Registry{byID: map[string]AgentDefinition{}}
    for _, d := range defs { reg.byID[d.ID] = d }
    return reg, nil
}

func (r *Registry) Get(id string) (AgentDefinition, bool) { ... }
func (r *Registry) IDs() []string { ... }

// internal/agent/agentdef/validator.go
type Validator struct {
    EnforceTier bool // false in commit C (warn-only), true in commit D
    Logger      *slog.Logger
}

func (v *Validator) ValidateAll(defs []AgentDefinition) []error {
    // 1. Slug collisions
    // 2. Empty ID / WhenToUse / Prompt
    // 3. Tier presence (warn or reject per EnforceTier)
    // 4. Subagent ID references exist in defs
    // 5. Same-tier delegation forbidden (warn or reject)
    // 6. Worker MUST NOT have subagents
}
```

**Tests** (8):
- `TestLoader_BuiltinEmpty_NoError` — empty embed.FS → empty list, no error
- `TestLoader_UserDirMissing_NoError` — non-existent user dir → only builtins
- `TestLoader_MalformedJSON_LineCol` — bad JSON → error includes file + offset
- `TestLoader_UserOverridesBuiltin_BySluq` — same ID → user wins
- `TestLoader_DefaultsApplied` — missing Tier → Worker; missing Inherit_ → false
- `TestValidator_RejectsSubagentsOnWorker` — Tier=Worker + Subagents → error
- `TestValidator_RejectsSameTierDelegation` — Chat → Chat → error
- `TestRegistry_GetReturnsCopy` — mutating returned def doesn't affect registry

**Acceptance**: `runtime.go` boots with empty registry, no behavior change.
Aura starts, Telegram works, no new tool appears in manifest.

**Deep refactor**: lint clean, errcheck on `fs.WalkDir`, file-size ≤600.

---

## 4. OH1-B — Migrate summarizer to AGENTDEF (~150 LOC) [Codex] — ✅ shipped `45c5fe02`

**Goal**: move the existing in-binary summarizer into the registry as
the first archetype. Byte-identical runtime.

**Files**:
- new `internal/agent/agentdef/builtin/summarizer/agent.json`
- new `internal/agent/agentdef/builtin/summarizer/prompt.md` (= current `SKILL.md` body)
- edit `internal/agent/agentdef/builtin/embed.go` (or inline embed.FS in package)
- edit `internal/agent/agents/summarizer/prompt.go` — becomes a shim that re-exports `agentdef.Registry().Get("summarizer").Prompt`
- delete `internal/agent/agents/summarizer/SKILL.md` (content moved)

**agent.json sketch**:
```json
{
  "id": "summarizer",
  "display_name": "Tool Result Summarizer",
  "when_to_use": "Compress a single oversized tool result while preserving identifiers, dates, numbers, paths, error codes verbatim.",
  "prompt_path": "prompt.md",
  "model_hint": "fast",
  "temperature": 0,
  "tools": { "named": [] },
  "tier": "worker",
  "max_iterations": 1,
  "max_input_tokens": 16384,
  "max_output_tokens": 4096,
  "max_result_chars": 8000,
  "inherit_safety": true
}
```

**Tests** (3):
- `TestSummarizerArchetype_LoadsAtBoot` — registry has "summarizer"
- `TestSummarizerArchetype_PromptByteIdentical` — registry.Get("summarizer").Prompt == old summarizer.Prompt
- `TestSummarizerArchetype_AppliesViaShim` — the legacy `summarizer.Prompt` symbol still resolves (back-compat for any caller)

**Acceptance**: existing payload_summarizer tests stay green; summarizer
behaviour byte-identical.

**Deep refactor**: delete `agents/summarizer/SKILL.md`; trim `prompt.go`
to ~10 LOC shim.

---

## 5. OH1-C — TIER + cycle detector validator WARN-only (~80 LOC) [Codex] — ✅ shipped `d0b24989`

**Goal**: validator runs the full rule set but only logs warnings.
Lets us see what would break before enforcement.

**Files**:
- edit `internal/agent/agentdef/validator.go` — flip `EnforceTier: false`
- edit `internal/agent/agentdef/validator_test.go` — assert log emission

**Sketch**: validator already exists from commit A. Commit C just adds
a runtime path that walks all loaded archetypes after boot + logs
violations without rejecting. Cycle-detection helper used in commit E
also lives here:

```go
// internal/agent/agentdef/cycle.go
func (r *Registry) DetectCycle(rootID string) []string {
    // Walk subagents[] starting at rootID; return cycle path or nil.
    // Used at LOAD time to flag, and at delegate-invocation time as
    // defense-in-depth (commit E).
}
```

**Tests** (5): same-tier warn, missing-tier warn, worker-with-subagents
warn, cycle warn, valid chain quiet.

**Acceptance**: boot logs warnings for summarizer (Worker, no
subagents → no warn). Aura still functional.

---

## 6. OH1-D — TIER enforcement turned on (~80 LOC) [Codex] — ✅ shipped `a82f5284`

**Goal**: flip warnings to errors. Boot fails fast on malformed user
TOMLs.

**Files**:
- edit `internal/agent/agentdef/validator.go` — `EnforceTier: true`
- edit `internal/agent/agentdef/validator_test.go` — flip warn assertions to error assertions

**Tests** (5): each violation REJECTS at boot. Summarizer must still
pass (proves migration in commit B left registry valid).

**Acceptance**: a fixture user.json with `subagents` + `tier=worker`
fails boot with structured error citing the archetype ID + rule
violated.

---

## 7. OH1-E — DELEGATE-TOOL synth + DEDUP + over-delegation prefix (~600 LOC) [Codex] — ✅ shipped `aec6f61d`

**Goal**: the biggest commit. Per-turn synthesis of `delegate_<id>`
tools, DEDUP against action tools, hardcoded over-delegation prefix,
extraction of `WithArchetypeDelegates` helper used by all 3 channel
builders.

**Files**:
- new `internal/agent/agentdef/delegate.go` — synth + helper + collision check
- new `internal/agent/agentdef/delegate_test.go`
- edit `internal/agent/tools/manifest.go` — dedup guard at emit time
- edit `internal/channels/telegram/invocation_builder.go:~36-80` — call helper
- edit `cmd/aura/web_chat.go:~200-275` — call helper
- edit `internal/swarm/types.go` — extend `Assignment` with `Archetype string`, `ModelOverride string`, `ParentChain []string`, `MaxInputTokens int`, `MaxOutputTokens int`
- edit `internal/swarm/manager.go` — bump `defaultMaxDepth = 3` (was 1); update test expectations
- edit `internal/swarm/hub_e2e_test.go` — bump MaxDepth assertion

**Sketch**:
```go
// internal/agent/agentdef/delegate.go
package agentdef

const overDelegationPrefix = "Use only when direct response/direct tools are insufficient. "

// DelegateRunner is the abstraction the synth function calls into.
// Implemented by an adapter around swarm.Manager.Run.
type DelegateRunner interface {
    Run(ctx context.Context, archetype string, prompt string, modelOverride string, parentChain []string) (string, error)
}

// WithArchetypeDelegates synthesises one tool per entry in the active
// archetype's Subagents[]. Each tool's Execute calls into runner with
// the target archetype + child sub-loop.
//
// The DEDUP guard fires when a synthesised name collides with an
// existing action tool name in `existing`. Resolution: skip the synth
// (action tool wins; log a warning), so a misconfigured user TOML
// never silently shadows wiki_page / search / file.
func WithArchetypeDelegates(
    activeArchetype string,
    reg *Registry,
    runner DelegateRunner,
    parentChain []string,
    existing []tools.Tool,
) []tools.Tool {
    parent, ok := reg.Get(activeArchetype)
    if !ok { return nil }
    existingNames := make(map[string]bool, len(existing))
    for _, t := range existing { existingNames[t.Name()] = true }

    out := make([]tools.Tool, 0, len(parent.Subagents))
    for _, sub := range parent.Subagents {
        target, ok := reg.Get(sub.ID)
        if !ok { continue }  // referential integrity already validated at boot
        name := sub.DelegateName
        if name == "" { name = "delegate_" + sub.ID }
        if existingNames[name] {
            slog.Warn("agentdef: delegate tool name collides with action tool — skipping synth",
                "delegate_name", name, "archetype", target.ID)
            continue
        }
        // Cycle gate: if target.ID already in parentChain, skip + log.
        if slices.Contains(parentChain, target.ID) {
            slog.Warn("agentdef: cycle detected — skipping synth",
                "target", target.ID, "chain", parentChain)
            continue
        }
        out = append(out, &delegateTool{
            name:        name,
            description: overDelegationPrefix + target.WhenToUse,
            target:      target,
            runner:      runner,
            parentChain: parentChain,
        })
    }
    return out
}

type delegateTool struct {
    name        string
    description string
    target      AgentDefinition
    runner      DelegateRunner
    parentChain []string
}

func (t *delegateTool) Name() string { return t.name }
func (t *delegateTool) Description() string { return t.description }
func (t *delegateTool) Parameters() map[string]any {
    return map[string]any{
        "type":     "object",
        "required": []string{"prompt"},
        "properties": map[string]any{
            "prompt": map[string]any{"type": "string", "description": "Task brief for the specialist."},
            "model":  map[string]any{"type": "string", "description": "Optional per-call model override."},
        },
    }
}
func (t *delegateTool) Execute(ctx context.Context, args map[string]any) (string, error) {
    prompt, _ := args["prompt"].(string)
    if strings.TrimSpace(prompt) == "" {
        return "", fmt.Errorf("%s: prompt required", t.name)
    }
    model, _ := args["model"].(string)
    chain := append(slices.Clone(t.parentChain), t.target.ID)
    return t.runner.Run(ctx, t.target.ID, prompt, model, chain)
}
```

**Adapter around swarm.Manager** (new file `internal/agent/agentdef/swarm_runner.go`):
```go
type SwarmRunner struct {
    Manager *swarm.Manager
    Reg     *Registry
}
func (r *SwarmRunner) Run(ctx context.Context, archetype, prompt, modelOverride string, chain []string) (string, error) {
    def, ok := r.Reg.Get(archetype); if !ok { return "", fmt.Errorf("unknown archetype %q", archetype) }
    a := swarm.Assignment{
        Archetype:          def.ID,
        Role:               def.ID, // back-compat with existing swarm.Manager
        Subject:            "delegate",
        Prompt:             prompt,
        SystemPrompt:       def.Prompt,
        ToolAllowlist:      def.Tools.Named,
        Temperature:        def.Temperature,
        MaxToolCalls:       def.MaxIterations,
        MaxToolResultChars: def.MaxResultChars,
        ModelOverride:      modelOverride,
        ParentChain:        chain,
        MaxInputTokens:     def.MaxInputTokens,
        MaxOutputTokens:    def.MaxOutputTokens,
    }
    return r.Manager.Run(ctx, a)
}
```

**Tests** (8):
- `TestSynth_NameDefault` — no DelegateName → `delegate_<id>`
- `TestSynth_NameOverride` — DelegateName set → wins
- `TestSynth_CollisionWithActionTool_SkipsSynth` — `delegate_search` vs existing `search` → no synth, log warn
- `TestSynth_DedupAtManifestEmit` — same name appears twice → first wins
- `TestSynth_CycleDetected_SkipsSynth` — target already in parentChain → no synth
- `TestSynth_OverDelegationPrefixOnEveryDescription` — prefix present
- `TestSynth_PromptSchemaRequired_ModelOptional` — JSON schema correct
- `TestSwarmRunner_PropagatesMaxInputTokens` — Assignment carries cap to swarm.Manager

**Acceptance**: live probe — chat tier with `summarizer` in subagents
sees `delegate_summarizer` in manifest; invoking it spawns sub-loop;
parent never sees child's tool calls.

**Deep refactor**: manifest.go dedup is the only change there; lint
clean; new `agentdef/delegate.go` ≤600 LOC; errcheck on runner.Run.

---

## 8. OH1-F — Channel-scrubber + announce template (~80 LOC) [Codex] — ✅ shipped `13a59625`

**Goal**: user-facing visibility when chat tier delegates. Telegram
thread and web chat both show the announce + final result, but NOT
the child's internal tool calls.

**Files**:
- new `internal/agent/agentdef/announce.md` (template, English only)
- new `internal/agent/agentdef/announce.go` (render function)
- edit `internal/channels/telegram/invocation_builder.go` — invoke
  announce before delegate sub-loop, scrub child trace after
- edit `cmd/aura/web_chat.go` — same

**announce.md** (template):
```
{{- if .DisplayName -}}
🤝 Delegating to **{{.DisplayName}}**…
{{- else -}}
🤝 Delegating to **{{.Archetype}}**…
{{- end }}
{{- if .ModelOverride }} _(model: {{.ModelOverride}})_{{ end }}
```

**Scrubber rule**: child's tool_call/tool_result messages are NOT
appended to the parent channel's outbound stream. Only the child's
final assistant message (text) is appended, prefixed by the announce
template.

**Tests** (3):
- `TestAnnounce_RendersDisplayNameWhenSet`
- `TestAnnounce_FallsBackToArchetypeID`
- `TestScrubber_DropsChildToolCalls` — child emits 3 tool calls + 1 final text → parent stream gets announce + final text only

**Acceptance**: live Telegram probe — invoke `delegate_summarizer` on
a long source → thread shows announce + summary, no child tool trace.

---

## 9. OH1-G — Migrate REFLECTION-FORK → `reflector` archetype (~30 LOC delta) [Codex] — ✅ shipped `52bd2c99`

**Goal**: replace the fork-and-restrict from RFL-S1 with a proper
archetype-driven path.

**Files**:
- new `internal/agent/agentdef/builtin/reflector/agent.json`
- new `internal/agent/agentdef/builtin/reflector/prompt.md` (= the
  extraction prompt from RFL-S1)
- edit `internal/learning/reflection_fork.go` — body becomes a call to
  the swarm runner with `archetype: "reflector"`
- update tests to assert byte-identical extraction

**Acceptance**: existing reflection tests stay green; new test asserts
the archetype path produces identical JSON for the same fixture turn.

**Shipped evidence (2026-05-25)**:
- Code commit: `52bd2c99 feat(agent): migrate reflection to reflector archetype`.
- Ground truth: `internal/agent/agentdef/builtin/reflector/prompt.md` is the
  embedded prompt used by the direct fallback; `TestReflectorArchetype_*`
  asserts builtin metadata and prompt-byte identity.
- Slice QA: `TestReflectionHook_ReflectorArchetypeProducesIdenticalJSON`
  asserts delegate-runner output persists the same normalized JSON as the
  direct LLM fallback for the same fixture turn; malformed JSON remains a
  no-write negative case.
- Verification: targeted learning/agentdef/agent/cmd/aura/Telegram tests,
  touched-file `dupl`, touched-package `golangci-lint`, `git diff --check`,
  `go vet ./...`, `go build ./...`, and lefthook pre-commit passed. Full
  `go test ./...` only hit the known Python availability timeout under load;
  the failing `cmd/aura` and `internal/sandbox` tests passed isolated.

---

## 10. OH1-H — Deprecate spawn_aurabot in prompts (docs only) [INTERACTIVE] — ✅ closed (no-op)

**Files**:
- edit `runtime-workspace/AGENT.md` — remove references to
  `spawn_aurabot` / `run_aurabot_swarm`; mention `delegate_<id>` as
  the new path
- edit `runtime-workspace/TOOLS.md` — same

**Acceptance**: probe a few turns; LLM no longer attempts the old
tools. Telemetry counter on those tool invocations should trend to 0
over N days.

**Closure evidence (2026-05-25)**:
- No prompt edit was needed. `runtime-workspace/TOOLS.md` is absent, and
  `internal/conversation/overlay.go` documents that `TOOLS.md` was retired
  on 2026-05-24 and is not injected into the system prompt.
- `runtime-workspace/AGENT.md`, `internal/config/defaults/AGENT.md`,
  `internal/config/defaults/TOOLS.md`, and `internal/conversation/` contain
  zero `spawn_aurabot` / `run_aurabot_swarm` prompt references.
- Verification: `rg -n "spawn_aurabot|run_aurabot_swarm" runtime-workspace internal\config\defaults internal\conversation`
  returned no matches; `go test ./internal/conversation -run "TestLoadPromptOverlayReadsKnownFiles|TestEnsurePromptOverlayDefaultsCreatesSoulOnly|TestIsOverlayFileName" -count=1`
  and `go test ./internal/config -run TestDefaultPromptDocsUseCurrentUnifiedToolNames -count=1`
  passed.

---

## 11. OH1-I — Remove deprecated tools (~50 LOC cleanup) [Codex, after telemetry] — ⬜ pending

**Trigger**: telemetry confirms zero invocations of
`spawn_aurabot` / `run_aurabot_swarm` across 7+ days.

**Files**:
- delete `internal/agent/tools/swarm/tools.go::SpawnAuraBotTool`
- delete `internal/agent/tools/swarm/tools.go::RunAuraBotSwarmTool`
- remove registry wiring in `cmd/aura/app_wire.go`
- delete corresponding tests

**Acceptance**: build green, lefthook green, no caller anywhere.

---

## 12. Wave-OH1 ship gate

After commit G ships:
- Live Telegram probe: long source → chat tier invokes
  `delegate_summarizer(prompt=..., model?=...)` → thread shows
  announce + final result → child trace not visible.
- No `400 duplicate tool name` provider errors ever.
- `parent_session_id` populated on every child run in `conversations`
  table (R from Agent #2 — verify in commit E or fold into G).
- `MEMORY.md` index gains an OH1 closure entry.
- `project_aura_dgx_spark_bundle_vision` memory updated: multi-agent
  is real.

---

## 13. How to drive

**Inline in Claude session**: I (or you) walk commit-by-commit through
this doc. Each commit is small enough to fit in one session
context-wise. Lefthook gates per commit.

**Via Codex CLI** (per memory `reference_codex_cli_pipe`):
```bash
codex exec --sandbox workspace-write --skip-git-repo-check "$(cat <<'EOF'
Read docs/aura-oh1-codex-plan-2026-05-25.md section "RFL-S1".
Implement exactly that commit (files + sketch + tests). Run
go vet + go test on touched packages before committing. Use the
commit body template at the end of the section. Do NOT touch
anything outside the files listed.
EOF
)"
```

Switch sections per commit (`OH1-A`, `OH1-B`, etc.).

**Commit body template** (every commit):
```
<type>(<scope>): <subject>

Per docs/aura-oh1-codex-plan-2026-05-25.md commit <ID>.
Locked decisions: <which of the 7 locks this commit touches>.

Files: <list>
Tests added: <names>
LOC delta: code <X>, test <Y>

<2-3 sentence rationale tying back to the synthesis recommendation>

Co-Authored-By: Claude Opus 4.7 (1M context) <noreply@anthropic.com>
```

---

## 14. Hold conditions (stop the wave)

- Any commit's lefthook fails on the second attempt → STOP, escalate
- `swarm.Manager` maxDepth bump 1→3 breaks an existing
  parent-child integration test → STOP, examine before fixing
  (Agent #4 friction-point F4 expects this is OK; verify)
- Any of the 8 OH1-E tests is flaky (≥2/10 runs) → STOP
- Live probe in commit E or F shows child trace leaking into Telegram
  → STOP, scrubber bug must be fixed before G ships

---

## 15. NOT in scope (Wave OH3 or later)

Surface-anchored to keep the wave bounded:
- Generic `spawn(task)` catch-all (nanobot pattern) — Wave OH3
- `max_concurrent_delegates` config + 5-phase status enum — Wave OH3
- `parent_session_id` SQLite migration — Wave OH3 (this wave just
  populates the field if column exists; migration is its own story)
- Swarm-Skills semantic skill swarm (arXiv 2605.10052) — roadmap pin
- Per-archetype telemetry dashboard widget — UI wave
- Multi-user channel-tool-policy — Wave OH3
