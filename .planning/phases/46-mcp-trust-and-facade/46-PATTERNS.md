# Phase 46: MCP trust and facade - Pattern Map

**Mapped:** 2026-08-17
**Files analyzed:** 20 (11 Aura-repo, 2 fork-repo, 1 new design doc, 6 Wave-0 test files, plus the
`prd.md`/`REQUIREMENTS.md`/`ROADMAP.md` amendment batch)
**Analogs found:** 18 / 20 (2 fork registration files have no Aura-side analog by construction —
see "No Analog Found")

**Scope note.** This phase spans three repositories: `D:/Repo/Aura` (this tree), `chetto1983/aura-pim-mcp`
(branch `aura/pim-sidecar`, C#/.NET), and `chetto1983/whatsapp-mcp` (branch `aura/cockpit-connect`,
Python/FastMCP). Neither fork has a local clone on this machine (RESEARCH.md "Environment
Availability"). For the two fork-side files, the "analog" is each fork's OWN existing registration
file (already read live via `gh api`, cited with line numbers), not anything in Aura's tree — do not
invent an Aura-side facade to imitate; D-17 forbids exactly that.

## File Classification

| New/Modified File | Repo | Role | Data Flow | Closest Analog | Match Quality |
|---|---|---|---|---|---|
| `prd.md` (amendment batch: D-05, D-08, D-09, TOOL-14, D-28/D-29) | Aura | config/doc | batch | `prd.md:6378-6420` Amendment #121 | exact |
| `.planning/REQUIREMENTS.md` MCP-02/04/05 rows (clean rewrite, D-31) | Aura | config/doc | batch | same file, MCPC-01..05 rows (`:131-135`) — clean-Complete-row shape | exact |
| `.planning/ROADMAP.md` §Phase 46 (clean rewrite, D-31) | Aura | config/doc | batch | same file, an already-clean phase section (e.g. §Phase 45) | role-match |
| `internal/agent/mcptools/bridge.go` — `specFromToolDefWithPolicy` (set `Multiplexed`, D-21/D-34) | Aura | service (bridge) | transform | same file, `specFromToolDefWithPolicy` itself (edit-in-place) | exact |
| `internal/agent/mcptools/bridge_risk.go` — re-key `trustedRecipeActions`, mount-time reconciliation (D-21/D-33/D-35) | Aura | service (policy) | transform | same file (edit-in-place); classifier shape from `internal/gateway/classify.go` | exact |
| `internal/agent/mcptools/bridge_memory.go` `defaultDeferred` OR new `bridge_deferral.go` (D-27 count rule) | Aura | service (policy) | transform | `internal/agent/mcptools/bridge_memory.go:22-25` (`defaultDeferred`), `bridge.go:66-100` (`refreshSpec` warn-on-change pattern) | exact |
| `internal/gateway/classify.go` — two new classifier fns + `multiplexedClassifiers` entries | Aura | service (classifier) | transform | same file, `classifySkill`/`classifyTask` | exact |
| `compose.yaml` — two image pins (D-23) | Aura | config | deploy | same file, Prometheus/Tempo/Grafana `@sha256` rows (`:1160,1190,1220`) — pattern for "how a pin reads", though D-23 chose `:<sha>` not digest | role-match |
| `docs/superpowers/specs/2026-08-1X-mcp-curated-surface-design.md` (NEW, D-24) | Aura | doc (design spec) | — | `docs/superpowers/specs/2026-06-16-calendar-pim-mcp-fork-design.md` | exact |
| `internal/gateway/classify_multiplexed_comms_test.go` (or extend `classify_test.go`) | Aura | test | unit | `internal/gateway/classify_test.go` (`TestClassifyTable`, `actionEnum` helper) | exact |
| `internal/agent/mcptools/bridge_deferral_test.go` (or extend `bridge_memory_policy_test.go`) | Aura | test | unit | `internal/agent/mcptools/bridge_memory_policy_test.go` | exact |
| `internal/gateway/guard_test.go` (extend, D-34 case) | Aura | test | unit | same file, `TestValidateClassifiableIgnoresNonMutatingMultiplexed` | exact |
| `internal/agent/mcptools/bridge_risk_test.go` (extend, action-keyed cases) | Aura | test | unit | same file, `TestTrustedRecipeRiskPolicyIsGraduated` + `TestMemoryRecipeCoversEveryServedTool` | exact |
| `internal/mcp/calendar_integration_test.go` (extend: curated tool count, no `accountId`) | Aura | test | integration | same file (edit-in-place) | exact |
| `internal/mcp/whatsapp_integration_test.go` (extend: curated tool count) | Aura | test | integration | same file (edit-in-place) | exact |
| `internal/mcp/calculator_integration_test.go` (extend: risk-tier assertion, SC#6) | Aura | test | integration | same file (edit-in-place) | exact |
| `whatsapp-mcp-server/main.py` — collapse 12(+2) `@mcp.tool()` decorators into one `messages` dispatcher | whatsapp-mcp fork | route/controller | request-response | fork's own `main.py` (registration layer); Go-side shape mirror: `internal/agent/tools/task.go` (flat-union `action` dispatch) | role-match (cross-language) |
| `Program.cs:121-134` — collapse 14 `.WithTools<X>()` registrations into one `CalendarActionTool` | aura-pim-mcp fork | route/controller | request-response | fork's own `Program.cs` (registration layer); Go-side shape mirror: `internal/agent/tools/task.go` | role-match (cross-language) |

## Pattern Assignments

### `internal/gateway/classify.go` — two new classifiers (calendar/messages)

**Analog:** same file, `classifySkill` (lines 93-110) and `classifyTask` (lines 131-151), plus the
`multiplexedClassifiers` map (lines 22-35) and `classify`'s dispatch (lines 53-64).

**The map to extend** (lines 31-35):
```go
var multiplexedClassifiers = map[string]func(json.RawMessage) scoring.RiskTier{
    "skill_manage": classifySkill,
    "task":         classifyTask,
    "swarm_spawn":  classifySwarmSpawn,
}
```
Add two more entries keyed by each curated tool's **namespaced `spec.Name`** — read from the actual
mount-time namespace in `internal/mcp/manager/catalog.go` (`BuiltInCatalog`) before writing the key;
do not guess it (RESEARCH Open Question 2 / Assumption A3).

**The classifier shape to copy exactly** (`classifySkill`, lines 93-110 — this is the fail-safe shape
both new classifiers must mirror, including the unrecognised-action branch):
```go
func classifySkill(raw json.RawMessage) scoring.RiskTier {
    var a struct {
        Action string `json:"action"`
    }
    if err := json.Unmarshal(raw, &a); err != nil {
        return scoring.Risky
    }
    if tier, ok := skillFixedTiers[a.Action]; ok {
        return tier
    }
    if skillScoredActions[a.Action] {
        return scoring.ComputeSkillTier(scoring.SkillAction(a.Action), "")
    }
    return scoring.Risky
}
```
For calendar/messages there is no `scoring.Compute*Tier` scored-action branch — every action's tier
comes straight from the re-keyed `trustedRecipeActions[calendarRecipeSource]` /
`[whatsAppRecipeSource]` map (now action-keyed, D-21), so the shape collapses to:
```go
func classifyCalendarAction(raw json.RawMessage) scoring.RiskTier {
    var a struct {
        Action string `json:"action"`
    }
    if err := json.Unmarshal(raw, &a); err != nil {
        return scoring.Risky
    }
    switch trustedRecipeActions[calendarRecipeSource][a.Action] {
    case mcpActionRead:
        return scoring.Safe
    case mcpActionMutate:
        return scoring.Normal
    case mcpActionDestructive:
        return scoring.Destructive
    default:
        return scoring.Risky // unrecognised action saturates upward — fail-safe (D-33)
    }
}
```
`classifyMessagesAction` (WhatsApp) is the same shape keyed on `whatsAppRecipeSource`. **Do not**
route these through `scoring.Compute*Tier` — `mcpActionClass` (bridge_risk.go) is a different,
already-graduated vocabulary; reuse it directly (D-21/D-35 forbid a second risk source).

**Dispatch site, unchanged** (lines 53-64) — new entries are picked up automatically:
```go
func classify(spec tools.Spec, rawArgs json.RawMessage) scoring.RiskTier {
    if fn, ok := multiplexedClassifiers[spec.Name]; ok {
        return fn(rawArgs)
    }
    if spec.Destructive {
        return scoring.Destructive
    }
    if spec.Mutating {
        return scoring.Normal
    }
    return scoring.Safe
}
```

---

### `internal/agent/mcptools/bridge_risk.go` — re-key `trustedRecipeActions`, D-33 reconciliation

**Analog:** same file, current shape (lines 26-83 for the table, 108-149 for `classifyToolRisk`).

**Current (raw-tool-name-keyed for all three sources)** — lines 26-83:
```go
var trustedRecipeActions = map[string]map[string]mcpActionClass{
    calendarRecipeSource: {
        "list_accounts":              mcpActionRead,
        // ...
        "send_email":                 mcpActionDestructive,
    },
    whatsAppRecipeSource: { /* 14 raw tool names */ },
    mcp.SourceRecipeMemory: { /* 10 raw tool names — STAYS this way, D-35 */ },
}
```

**Target shape (D-21/D-35 — mixed keys, documented):** calendar and whatsapp become **action-name**
keyed (matching the curated tool's `action` enum values, which may differ from the 14/14 raw tool
names above — Claude's discretion on exact action naming, CONTEXT.md); `mcp.SourceRecipeMemory`
**stays raw-tool-name keyed**, unchanged, because memory is not merged into one multiplexed tool.
Comment the mixed-key discriminator explicitly (RESEARCH "Pattern: mixed-key risk table"):
```go
// trustedRecipeActions is keyed by whatever discriminator that source's surface
// exposes: calendar and whatsapp are ACTION-keyed (their tools are multiplexed
// behind one curated action enum); memory stays RAW-TOOL-NAME-keyed (it is not
// merged — each memory_* call is its own MCP tool name). One table, one lookup
// site in classifyToolRisk, two key spaces documented here so a future reader
// does not assume uniformity that was never true.
```

**`classifyToolRisk`'s lookup site stays unchanged in shape** (lines 108-149) — only the key space
under it changes for two of the three sources:
```go
func classifyToolRisk(policy bridgePolicy, t *sdkmcp.Tool) (mutating, destructive bool) {
    if actions := trustedRecipeActions[policy.recipeSource]; actions != nil {
        if action, ok := actions[t.Name]; ok {   // t.Name is RAW tool name today; for
            // calendar/whatsapp the curated tool's single wire name means this lookup
            // moves to look up the ACTION from the unmarshalled call args instead of
            // t.Name — this is the seam classify.go's new classifier functions own,
            // NOT bridge_risk.go's mcpToolRisk (which classifies the SPEC once at
            // bridge time, before any per-call args exist). Keep mcpToolRisk's own
            // per-tool classification for the memory source unchanged.
```
**Important structural note the planner must resolve explicitly:** `mcpToolRisk`/`classifyToolRisk`
run once per **advertised tool** at spec-build time (`t *sdkmcp.Tool`, no call args). Once calendar/
whatsapp collapse to ONE curated tool each, this function no longer has an `action` to key on — the
per-action decision moves entirely to the new `gateway/classify.go` classifiers (which DO see
`rawArgs`). `trustedRecipeActions[calendarRecipeSource]`/`[whatsAppRecipeSource]` therefore becomes
a table **read only by the new gateway classifiers**, not by `classifyToolRisk`'s `t.Name` lookup —
`classifyToolRisk`'s recipe branch for calendar/whatsapp effectively goes dead once `Multiplexed` is
set (the spec's flat `Mutating`/`Destructive` become the safe generic floor gateway's `classify`
falls back to only for a tool `classify` doesn't recognize — but `classify` always recognizes it via
`multiplexedClassifiers`, so the flat spec bits are never read at dispatch time for these two tools).
`classifyToolRisk`'s memory branch is UNCHANGED (memory is not multiplexed).

**Mount-time reconciliation (D-33), new code — no direct precedent in this file, closest shape is the
`refreshSpec` warn-on-change pattern in `bridge.go:78-98`:**
```go
// Source: internal/agent/mcptools/bridge.go:78-91 — the shape to mirror for D-33's
// WARN-not-panic drift log (an unknown action in the curated tool's live schema enum
// vs. trustedRecipeActions' table).
if oldMutating != spec.Mutating {
    slog.Warn("mcp tool mutating flag changed on reconnect",
        "tool", spec.Name,
        "old_mutating", oldMutating,
        "new_mutating", spec.Mutating,
    )
}
```
D-33's reconciliation walks the curated tool's schema `action` enum (parsed the same way
`requiredArgsHint`/`requiredArgNames` in `bridge.go` parse `required`) and WARN-logs any action value
absent from `trustedRecipeActions[recipeSource]`, by name, at mount — never a panic (MCP mounts are
fail-soft by design, per CONTEXT.md's established pattern).

---

### `internal/agent/mcptools/bridge.go` — `specFromToolDefWithPolicy`, `Multiplexed` (D-21/D-34)

**Analog:** same file, the function itself (lines 168-182) plus the guard it must satisfy
(`internal/gateway/guard.go:22-38`).

**Current** (lines 168-182 — never sets `Multiplexed`):
```go
func specFromToolDefWithPolicy(namespace string, t *sdkmcp.Tool, policy bridgePolicy) tools.Spec {
    params, summary, description := specFieldsFromToolDef(t)
    mutating, destructive := mcpToolRisk(policy, t)
    spec := tools.Spec{
        Name:        namespacedName(namespace, t.Name),
        Summary:     summary,
        Description: description,
        Parameters:  params,
        Deferred:    policy.defaultDeferred(),
        Mutating:    mutating,
        Destructive: destructive,
    }
    applyMCPOperationMetadata(&spec)
    return spec
}
```

**D-34's gating rule (do NOT infer from schema shape):**
```go
// Source: 46-RESEARCH.md "Pattern: gate Multiplexed inference on classifier existence"
// specFromToolDefWithPolicy — do NOT set Multiplexed from "does the schema have
// an `action` property". That would make ValidateClassifiable panic at boot for
// ANY stranger's server whose schema happens to use an `action` argument — the
// opposite of Success Criterion 6. Multiplexed is true only when this specific
// tool's namespaced name already has an entry in classify.go's
// multiplexedClassifiers.
spec.Multiplexed = isKnownMultiplexedMCPTool(spec.Name)
```
`isKnownMultiplexedMCPTool` is new; it can be as simple as checking membership in a small constant
set of the two curated namespaced names, OR (cleaner, no duplicated list) exported from
`internal/gateway` and imported here — but `internal/gateway` already imports `internal/agent/tools`,
so check for an import cycle before choosing that direction; the fallback is a small local constant
set in `mcptools`, e.g. mirroring `calendarRecipeSource`/`whatsAppRecipeSource`'s const-pair style
(`bridge_risk.go:21-24`).

**The guard this must satisfy** (`internal/gateway/guard.go:22-38`, read verbatim, do not modify):
```go
func ValidateClassifiable(reg *tools.Registry) {
    for _, t := range reg.All() {
        spec := t.Spec()
        if spec.Mutating {
            validateOperationMetadata(spec)
        }
        if !spec.Mutating || !spec.Multiplexed {
            continue
        }
        if _, ok := multiplexedClassifiers[spec.Name]; !ok {
            panic(fmt.Sprintf(
                "gateway.ValidateClassifiable: mutating multiplexed tool %q has no per-action "+
                    "classifier — add it to multiplexedClassifiers, or the gateway will under-gate "+
                    "its actions", spec.Name))
        }
    }
}
```
This is exactly why D-32 requires the classifier map entry and the `Multiplexed: true` flip to land
in the **same commit**: setting `Multiplexed` without the matching `multiplexedClassifiers` entry
panics Aura's boot.

---

### `internal/agent/mcptools/bridge_memory.go` (or new `bridge_deferral.go`) — D-27 count rule

**Analog:** same file, `defaultDeferred` (lines 22-25), `modelFacing`/`memoryHiddenFromModel`
(lines 27-45).

**Current — unconditional:**
```go
// Every bridged MCP tool is deferred. tool_search keeps memory discoverable
// without carrying its full schemas in every model request.
func (bridgePolicy) defaultDeferred() bool {
    return true
}
```

**`modelFacing`, the counting basis D-27 builds on (lines 39-45) — count AFTER this filter:**
```go
func (p bridgePolicy) modelFacing(tool string) bool {
    if !p.memory {
        return true
    }
    _, hidden := memoryHiddenFromModel[tool]
    return !hidden
}
```

**D-27's rule to implement** (two code constants + one predicate, per CONTEXT.md's Discretion note —
CLAUDE.md forbids hard-coded env for this, and D-27 explicitly says these are constants, not env
vars):
```go
const maxAlwaysLoadedMCPTools = 3   // per-server: modelFacing tool count ceiling to earn a slot
const maxAlwaysLoadedMCPSlots = 2   // global: how many servers may hold a loaded slot at once

// defaultDeferred is no longer unconditional (D-27): a server whose modelFacing
// tool count is <= maxAlwaysLoadedMCPTools earns a loaded slot, up to the global
// maxAlwaysLoadedMCPSlots budget, granted in deterministic (mount) order. Overflow
// fails closed — every further qualifying server stays deferred and
// tool_search-discoverable (SC#6).
```
**Where the predicate lives and how it avoids `refreshSpec` churn** is explicitly Claude's discretion
(CONTEXT.md) — Pitfall 3 (RESEARCH.md) requires picking ONE of: freeze the deferred bit at mount time
(never recompute on reconnect), or extend `refreshSpec`'s existing warn-on-change pattern
(`bridge.go:78-98`, copy the `oldMutating != spec.Mutating` shape for a `oldDeferred != spec.Deferred`
case) to also warn instead of silently flip. Document whichever is chosen with the same rationale
weight the existing three `refreshSpec` warn-blocks carry.

**LOC headroom note:** `bridge_memory.go` is 79 lines today; `bridge.go` is 482/600. Either file has
room, but CLAUDE.md's ≤600 LOC ceiling plus "split into `<name>_<concern>.go` on touch" applies if the
count-predicate + global-budget bookkeeping (which needs SOME shared mutable state across mount calls,
e.g. a counter or a small registry-scoped struct) pushes either file close to the ceiling — a new
`bridge_deferral.go` is the natural split point, named for the concern exactly like `bridge_call.go`
and `bridge_risk.go` already are.

---

### `internal/agent/mcptools/bridge_call.go` — `newResult`, trust framing (reference only, D-01 — no
code change, cite for the amendment's evidence)

**Analog:** same file, `newResult` (lines 54-68) — already ratified, cited verbatim as the amendment's
evidence, not edited:
```go
func (b *bridgedTool) newResult(ctx context.Context, text string) (tools.ToolResult, error) {
    res, err := tools.NewResult(ctx, text)
    if err != nil {
        return tools.ToolResult{}, err
    }
    res.Provenance = &tools.ToolResultProvenance{
        Source: "mcp:" + b.Spec().Name,
        Trust:  tools.TrustTrusted,
    }
    return res, nil
}
```

---

### Cross-language schema shape for both curated fork tools — flat-union `action` (D-19)

**Analog:** `internal/agent/tools/task.go` — the closest EXISTING multiplexed-tool schema in Aura's
tree, and per D-19 ("Flat union keeps one familiar shape across every multiplexed tool Aura has") the
shape the fork's own JSON Schema for its curated tool should mirror, even though the fork is C#/Python:

**Wire-safety shape** (lines 104-125 — root object, ONLY `action` required, `action` carries a
property-level enum, per-action requirements live in field `description` strings, NO root
oneOf/anyOf/enum):
```json
{
  "type": "object",
  "properties": {
    "action": {"type": "string", "enum": ["schedule", "list", "cancel", "run_now"], "description": "..."},
    "schedule_kind": {"type": "string", "enum": ["at", "every", "cron"], "description": "Required when action=schedule. ..."}
  },
  "required": ["action"]
}
```
Carry this into the calendar/`messages` curated tool's own schema: one flat object, every field any
action needs declared optional at the schema root, the per-action requirement stated in that field's
`description`, `required: ["action"]` only. D-19 additionally requires: **never a bare `id`** —
`eventId`, `chatId`, `messageId`, `emailId` (Poke's ID discipline).

**Go-side dispatch shape** (lines 150-181 — `ActionRouter`, lazily built, dispatch by `action` string,
never panics on an unknown action):
```go
func (t *TaskTool) Execute(ctx context.Context, raw json.RawMessage) (ToolResult, error) {
    var head struct {
        Action string `json:"action"`
    }
    if err := json.Unmarshal(raw, &head); err != nil {
        return ToolResult{}, fmt.Errorf("task args: %w", err)
    }
    if head.Action == "" {
        return ToolResult{}, fmt.Errorf("task: action is required")
    }
    return t.actionRouter().Dispatch(ctx, head.Action, raw)
}
```
The fork's own dispatcher (C# `CalendarActionTool` / Python `messages` handler) is the same shape in
its own language: parse `action`, switch/dispatch to the existing provider-call method the current 14
individual tool classes / `@mcp.tool()` wrappers already call — **the implementation layer (provider
calls) does not change, only the registration/dispatch layer collapses** (RESEARCH.md, both forks).

---

### `internal/mcp/calendar_integration_test.go` / `whatsapp_integration_test.go` — extend for the
curated surface

**Analog:** same two files, current shape (calendar: full file above; whatsapp: full file above) —
this is the SC#1/#6 proof-of-shape tier, build-tag gated, no-skip-as-green.

**Gate helper to copy verbatim (already exists per file, keep it):**
```go
// Source: internal/mcp/calendar_integration_test.go:38-51
func calendarEndpointOrGate(t *testing.T) string {
    t.Helper()
    if v := strings.TrimSpace(os.Getenv("AURA_PIM_MCP_URL")); v != "" {
        return v
    }
    if port := strings.TrimSpace(os.Getenv("AURA_PIM_MCP_PORT")); port != "" {
        return "http://127.0.0.1:" + port + "/"
    }
    if strings.TrimSpace(os.Getenv("CI")) != "" {
        t.Fatal("AURA_PIM_MCP_URL (or _PORT) must be set under CI — a skipped calendar_integration tier is never a silent pass (CLAUDE.md no-skip-as-green)")
    }
    t.Skip("set AURA_PIM_MCP_URL (or AURA_PIM_MCP_PORT) + bring the aura-pim-mcp sidecar up to run the calendar_integration tier")
    return ""
}
```

**Kept/dropped tool-list assertion to REPLACE** (`calendar_integration_test.go:53-119` — today asserts
"exactly the 14 trimmed tools"; after this phase it must instead assert "exactly ONE curated tool
named `calendar`, action count == N"):
```go
// CURRENT (pre-46) — becomes stale once the fork collapses to 1 tool:
var keptCalendarTools = []string{
    "list_accounts", "get_emails", /* ...14 total... */
}
if len(advertised) != len(keptCalendarTools) {
    t.Errorf("PIM sidecar advertises %d tools, want exactly %d (trimmed surface); surface=%v", ...)
}
```
Replace with an assertion that `len(advertised) == 1` and `advertised[0].Name == "calendar"`, plus
(MCP-05/SC#4) a follow-up call chain: `list_calendars` (or `get_calendar_events`) → extract the
opaque reference the fork returns → `get_calendar_event_details` using ONLY that reference, asserting
no `accountId` key appears in the dispatched args. `whatsapp_integration_test.go` gets the parallel
treatment for `messages`.

**`calculator_integration_test.go`** (SC#6) — extend `TestCalculatorServerLive` (already exists,
lines 39-68) with a risk-tier assertion: mount, then assert the risk classification a mutating
`calculate`-adjacent action (if any) or the tool's own `Mutating`/`Destructive` bits read
`Mutating+Destructive` with no annotation present — the fail-closed default `mcpToolRisk` already
proves at unit level (`bridge_risk_test.go:172` `TestMCPToolRisk_NilAnnotationsFailsClosed`), now
proven end-to-end through a REAL unlisted mount.

---

### Unit test additions — the exhaustiveness/table-test idiom to copy

**Analog:** `internal/gateway/classify_test.go` — `actionEnum` helper (lines 33-55) + `TestClassifyTable`
(lines 57-72) — derives the action list FROM THE REAL SCHEMA rather than a hand-copied enum, so a
future action added to the schema is visible to the test:
```go
func actionEnum(t *testing.T, spec tools.Spec) map[string]bool {
    t.Helper()
    var schema struct {
        Properties struct {
            Action struct {
                Enum []string `json:"enum"`
            } `json:"action"`
        } `json:"properties"`
    }
    if err := json.Unmarshal(spec.Parameters, &schema); err != nil {
        t.Fatalf("parse %s schema: %v", spec.Name, err)
    }
    set := make(map[string]bool, len(schema.Properties.Action.Enum))
    for _, a := range schema.Properties.Action.Enum {
        set[a] = true
    }
    if len(set) == 0 {
        t.Fatalf("%s: no action enum in schema", spec.Name)
    }
    return set
}
```
This cannot be reused byte-for-byte for the curated calendar/messages tools, because their schema
lives in a REMOTE fork, not a local `Spec()` call — the planner's unit test instead hard-codes the
expected action set (there is no local `tools.Spec` to introspect until the tool is bridged live),
and the LIVE reconciliation (`whatsapp_integration_test.go`/`calendar_integration_test.go`) is what
actually reads the remote schema.

**Tripwire-style exhaustiveness test to copy** — `bridge_risk_test.go:117-134`
(`TestMemoryRecipeCoversEveryServedTool`), the closest existing precedent for D-33's mount-time
reconciliation warning, adapted to a TABLE test asserting `trustedRecipeActions[calendarRecipeSource]`
(now action-keyed) covers exactly the fork's documented action set, no more, no less:
```go
func TestMemoryRecipeCoversEveryServedTool(t *testing.T) {
    t.Parallel()
    served := []string{ /* the served tool names */ }
    table := trustedRecipeActions[mcp.SourceRecipeMemory]
    for _, name := range served {
        if _, ok := table[name]; !ok {
            t.Errorf("%s is served but unclassified — it silently becomes Destructive and stops the turn", name)
        }
    }
    for name := range table {
        if !slices.Contains(served, name) {
            t.Errorf("%s is classified but no longer served — the table is describing a server that is gone", name)
        }
    }
}
```

**Graduated-risk table test to copy** — `bridge_risk_test.go:74-111`
(`TestTrustedRecipeRiskPolicyIsGraduated`), same shape for the two re-keyed sources plus a case for
"unrecognised action fails closed":
```go
tests := []struct {
    name            string
    source          string
    tool            string   // becomes `action` after the re-key
    wantMutating    bool
    wantDestructive bool
}{
    {name: "calendar read", source: "recipe:calendar", tool: "get_emails"},
    {name: "calendar external send", source: "recipe:calendar", tool: "send_email", wantMutating: true, wantDestructive: true},
    {name: "unknown recipe tool fails closed", source: "recipe:calendar", tool: "new_side_effect", wantMutating: true, wantDestructive: true},
}
```

**Guard test to extend** — `internal/gateway/guard_test.go:75`
(`TestValidateClassifiableIgnoresNonMutatingMultiplexed`) is the closest existing case; add a sibling
proving D-34's actual hazard: a Mutating tool whose schema has an `action` property but `Multiplexed`
is deliberately left `false` (no classifier entry) boots cleanly and classifies via the generic
Mutating floor — never panics, never gets promoted.

---

## Shared Patterns

### Fail-closed on wiring, fail-soft on outages
**Source:** `internal/gateway/guard.go` (panic on missing classifier) vs. `internal/agent/mcptools/bridge.go`
`refreshSpec` (WARN-log on a changed flag, never panic).
**Apply to:** `bridge_risk.go`'s D-33 reconciliation (WARN, never panic — a fork rename must not stop
Aura booting) and `bridge.go`'s D-21/D-34 `Multiplexed` wiring (panic IS correct here — it is a static
Aura-side wiring gap, not an outage).

### Monotone saturate-upward classification, unrecognised → Risky/Destructive
**Source:** `internal/gateway/classify.go` (module docstring, lines 6-12); `classifySkill`/`classifyTask`'s
`return scoring.Risky` default branch; `bridge_risk.go`'s `mcpToolRisk` nil-Annotations `return true, true`.
**Apply to:** both new gateway classifiers (calendar/messages) and any dispatch touching
`trustedRecipeActions`'s re-keyed lookup — an unrecognised action NEVER de-escalates.

### Deferred-by-default, now earned by count
**Source:** `internal/agent/mcptools/bridge_memory.go:22-25` (`defaultDeferred`, today unconditional).
**Apply to:** the D-27 count predicate — same method signature, same call sites (`bridge.go:75`,
`bridge.go:176`), only the body's logic changes from a constant to an arithmetic check.

### `capSchemaDescriptions` / byte caps, load-bearing for the description budget
**Source:** `internal/agent/mcptools/bridge.go:203-346` (`capSchemaDescriptions`, `frameMCPSummary`,
`frameMCPDescription`).
**Apply to:** D-36's ~1.5–2KB merged-description budget for both curated tools — write the fork's
tool description to stay under `maxMCPDescriptionBytes` (4096B) WITH HEADROOM (target ~2KB), because
these two tools are now always-loaded and every byte is paid every turn; the existing cap machinery
still applies unchanged as the hard backstop, but the target is well inside it, not against it.

### PRD amendment house style — the shape D-05/D-08/D-09/TOOL-14 must match
**Source:** `prd.md:6378-6420`, Amendment #121 (the newest, and the one CONTEXT.md's own "what a
measurement does NOT prove" discipline is modeled on).
**Apply to:** every amendment this phase writes.
```
## §<Topic> (Amendment #NNN, YYYY-MM-DD)

> **Amendment #NNN (YYYY-MM-DD, Phase 46 planning, D-XX BLOCKING) — <one-line title
> naming exactly what changes>.**
>
> **What was measured, against this working tree at commit `<sha>`.** <concrete
> file:line citations, grep results, or gh api evidence — no assertions without a
> citation>.
>
> **What changes as a result.** <exact prose/requirement/roadmap text delta, naming
> the file:line it lands at>.
>
> **What this measurement does NOT prove.** <explicit scope boundary — what a
> later reader must NOT infer from this amendment>.
```
The OLDER single-paragraph style (`▶ Amendment #NNN (date — title).` one dense paragraph, e.g. #109-
#118) is still present throughout `prd.md` but is the SUPERSEDED house style — #121 is the current
one and the one to imitate, per its own self-description as the more recent, more rigorous shape and
per CLAUDE.md's explicit "cosa la misura NON dimostra" mandate.

### REQUIREMENTS.md clean-row rewrite shape (D-31)
**Source:** `.planning/REQUIREMENTS.md:131-135` (MCPC-01..05 — these ARE already the clean/Complete
shape: checkbox `[x]`, bold requirement name, plain current-state prose, historical corrections kept
as **bolded inline parentheticals** rather than strikethrough-and-append).
**Apply to:** MCP-02/04/05's rewrite — the CURRENT rows (lines 120-123, already read above) carry
`~~struck~~` text inline; D-31 replaces each with the MCPC-01..05 style: current text first, then a
`**Amended YYYY-MM-DD (...)**` clause folding in what changed, with the fully-superseded wording moved
to a dated footnote (D-31 makes the footnote MANDATORY — this relocates history, never deletes it).

### Design-doc precedent shape (D-24)
**Source:** `docs/superpowers/specs/2026-06-16-calendar-pim-mcp-fork-design.md` — 10 numbered
sections: `1. Background & motivation`, `2. Goals (locked via brainstorm)` + `Non-goals`,
`3. Architecture`, `4. Thin-fork changes`, `5. Trimmed tool surface`, `6. Aura-side integration`,
`7. Security & policy`, `8. Validation plan`, `9. Risks & open items`, `10. Decisions log`.
**Apply to:** the new `docs/superpowers/specs/2026-08-1X-mcp-curated-surface-design.md` — same ten
sections, scoped to BOTH forks' curated surfaces in one doc (action names, the flat argument union
per D-19, D-19's ID discipline, D-20's `accountId` handle fix), since D-24 explicitly says this is
the contract the fork commits implement and the artifact a reviewer reads without leaving this repo.

## No Analog Found

| File | Role | Data Flow | Reason |
|---|---|---|---|
| `whatsapp-mcp-server/main.py` (fork) | route/controller | request-response | No Aura-side analog exists by design (D-17) — the closest structural mirror is the fork's OWN current `main.py` registration layer (12 `@mcp.tool()` decorators on `aura/cockpit-connect`, verified live via `gh api`) plus Aura's `task.go` for the JSON-Schema SHAPE only, not the code |
| `Program.cs:121-134` (fork) | route/controller | request-response | Same reasoning — the fork's own current 14 `.WithTools<X>()` registrations (verified live via `gh api`) are the structural precedent; Aura's `task.go` supplies the schema-shape precedent only |

Both of these are genuinely fork-repo work with no Aura-tree equivalent to imitate beyond the
cross-language JSON-Schema idiom already captured above (flat union, `action` enum, D-19 ID
discipline) — this is the correct, D-17-mandated outcome, not a gap in the search.

## Metadata

**Analog search scope:** `internal/agent/mcptools/`, `internal/gateway/`, `internal/agent/tools/`,
`internal/mcp/*_integration_test.go`, `compose.yaml`, `prd.md`, `.planning/REQUIREMENTS.md`,
`.planning/ROADMAP.md`, `docs/superpowers/specs/`; fork repos inspected read-only via `gh api`
(already done by 46-RESEARCH.md, re-cited here rather than re-fetched).
**Files scanned:** ~24 (11 read in full, 8 grepped for structure, 5 cited from RESEARCH.md's own
`gh api` reads without re-fetching).
**Pattern extraction date:** 2026-08-17.
