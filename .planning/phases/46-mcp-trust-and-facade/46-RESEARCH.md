# Phase 46: MCP trust and facade - Research

**Researched:** 2026-08-17
**Domain:** MCP host policy (Go, `internal/agent/mcptools` + `internal/gateway`) and two forked MCP
sidecars (C#/.NET `aura-pim-mcp`, Python/FastMCP `whatsapp-mcp`)
**Confidence:** HIGH on the Aura-side mechanism (D-21/D-27, re-verified against `02a291530`); HIGH on
fork CI/publish mechanics (verified live via `gh api` against both fork repos); MEDIUM on the exact
in-fork curation diff shape (depends on discretion items CONTEXT.md leaves open); LOW on nothing —
every claim below was either read from code/API or is explicitly marked `[ASSUMED]`.

## Summary

CONTEXT.md (D-00..D-38) already resolved the architecture, the trust posture, and the classification
mechanism — this research does not re-open any of that. What it adds, beyond cheap re-verification of
the cited line numbers (all confirmed current), is the ground CONTEXT.md itself flagged as least
settled: **fork delivery mechanics**. Both forks were inspected live via `gh api` (not assumed): both
already carry an `aura-publish-image.yml` workflow on their Aura-owned branch (`aura/pim-sidecar`,
`aura/cockpit-connect`) that builds and pushes `ghcr.io/<repo>:sidecar` **and**
`ghcr.io/<repo>:${{ github.sha }}` (the full 40-char commit SHA — this is what D-23's `:<sha>` tag
means concretely) on every push to that branch touching build-relevant paths, or on manual dispatch.
This is a currently-idle-but-present piece of CI, not something to build.

One load-bearing discrepancy was found and was not in CONTEXT.md: **`whatsapp-mcp`'s
`aura/cockpit-connect` branch (the one `compose.yaml` mounts) currently registers only 12 of the 14
actions `bridge_risk.go`'s `trustedRecipeActions[whatsAppRecipeSource]` already lists** — `get_contact`
and `send_reaction` exist on the fork's own `main` branch but were never merged onto
`aura/cockpit-connect`. The Go-side risk table already accounts for a fork state that does not exist
yet on the branch Aura mounts. This does not break anything today (both are unused table entries; the
fail-closed default protects any tool that IS mounted but unlisted) but it directly affects how the
planner scopes the WhatsApp fork's plan: curating "the existing surface" without first reconciling
`aura/cockpit-connect` against `main` will silently narrow the curated tool to 12 actions while leaving
2 dead entries in the Go table, or the plan must explicitly include the merge as its first step.

The description-budget arithmetic (D-36) was independently re-derived from the numbers CONTEXT.md
already measured; the fixture (`internal/agent/tools/testdata/deferred_manifest.json`, 101,815 bytes
total) is confirmed current. The 4,096-byte `frameMCPDescription` cap and 16KB/128-property
`capSchemaDescriptions` caps were re-read at `bridge.go:26-30` and hold as cited.

**Primary recommendation:** plan Phase 46 as three tracks that land in one order — (1) the blocking PRD
amendment batch (D-05, D-08, D-09, TOOL-14, D-28..D-31) lands first as pure documentation, zero code;
(2) two fork-side plans (one per sidecar) whose commits land in the fork repos, each ending with the
`aura-publish-image` workflow producing a `:<sha>` tag — the WhatsApp plan's first task is reconciling
`aura/cockpit-connect` against `main`'s `get_contact`/`send_reaction` before curating; (3) one atomic
Aura-side commit (D-32) that flips both image pins in `compose.yaml`, re-keys `trustedRecipeActions` to
action-keyed, sets `Multiplexed: true` gated on classifier existence (D-34), adds the two
`multiplexedClassifiers` entries, and adds the mount-time drift-warning reconciliation (D-33) — all in
the same commit, because D-23's immutable pin is what makes a half-landed state impossible to reach.

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| MCP result trust framing | API/Backend (`internal/agent/mcptools`) | — | `newResult`/`frameMCPDescription` run host-side at bridge time, before the model ever sees text |
| Per-action risk classification | API/Backend (`internal/gateway`) | — | `classify`/`ValidateClassifiable` are the single in-process policy-enforcement point (PEP); no client or DB involvement |
| Tool surface curation (calendar/WhatsApp action set) | External service (forked MCP sidecar, C#/.NET and Python) | — | D-17: curation is server-owned by design; Aura's bridge stays generic |
| Always-loaded / deferred decision | API/Backend (`bridgePolicy.defaultDeferred`) | — | Pure host-side arithmetic over tool count, no persistence |
| Image pin / version selection | Database/Storage boundary is N/A here — Deploy config (`compose.yaml`) | — | Not a runtime tier; it is the join point between two repos, evaluated at `docker compose up` |
| `accountId` handle resolution | External service (fork) | — | D-20: the fork's own detail-tool schema, not host injection — MCP has no `accountId` concept at all |

## Standard Stack

No new library dependency is introduced by this phase. The relevant "stack" is what already exists at
each tier:

### Core
| Component | Version (verified) | Purpose | Why standard |
|---|---|---|---|
| `github.com/modelcontextprotocol/go-sdk` | v1.7.0 (`go.mod:27`, unchanged from 45.1) | MCP client the bridge sits on | Already the official SDK; Phase 45.1 completed the swap |
| `chetto1983/aura-pim-mcp` (fork of `MarimerLLC/calendar-mcp`), branch `aura/pim-sidecar` | HEAD verified live via `gh api` 2026-08-17 (branch exists, `aura-publish-image.yml` present) | Mail+calendar+contacts MCP server | Existing, already-mounted fork — this phase edits it, does not create it |
| `chetto1983/whatsapp-mcp`, branch `aura/cockpit-connect` | HEAD verified live via `gh api` 2026-08-17 | WhatsApp MCP server | Same — existing fork |

### Alternatives Considered
None — CONTEXT.md D-17/D-18 already closed this question (curation-in-fork over an Aura-side facade,
declarative config, or a namespace table). Re-litigating is explicitly out of scope per the task brief.

**Installation:** No `go get`/`npm install`/`pip install` — this phase edits existing Go files, an
existing C# project (`aura-pim-mcp`), and an existing Python FastMCP server (`whatsapp-mcp`).

## Package Legitimacy Audit

**Not applicable.** This phase installs no new external package in any ecosystem. Both MCP sidecars are
pre-existing forks already mounted and pinned in `compose.yaml`; the work is curation-diffs inside them,
not new dependencies. `slopcheck`/registry verification is skipped — nothing to gate.

## Fork Delivery Mechanics (primary research target)

### What already exists (verified live, not assumed)

Both forks were inspected directly against GitHub (`gh api repos/chetto1983/<repo>/contents/...`, not
training-data guesses about fork content):

- **`aura-pim-mcp`**, branch `aura/pim-sidecar`: workflows are `ci.yml`, `claude.yml`, `release.yml`
  (upstream cross-platform binary release, irrelevant to the sidecar image), and
  **`aura-publish-image.yml`** — present ONLY on this branch, not on `main` (confirmed: `main`'s
  workflow listing lacks it). `[VERIFIED: gh api, repos/chetto1983/aura-pim-mcp/contents/.github/workflows?ref=aura/pim-sidecar]`
- **`whatsapp-mcp`**, branch `aura/cockpit-connect`: same shape — `aura-publish-image.yml` present.
  `[VERIFIED: gh api]`
- **Both workflows are byte-for-byte the same pattern**: trigger on `workflow_dispatch` or `push` to
  the Aura branch touching build-relevant paths (`src/**`/`Dockerfile`/`Directory.Build.props` for
  the .NET fork; `whatsapp-bridge/**`/`whatsapp-mcp-server/**`/`Dockerfile`/`docker/entrypoint.sh` for
  the Python fork, plus the workflow file itself in both cases); `docker/build-push-action@v6` tags
  the built image **`ghcr.io/<owner>/<repo>:sidecar`** and **`ghcr.io/<owner>/<repo>:${{ github.sha }}`**
  — the full 40-hex-char commit SHA of the triggering push. This is exactly what D-23's `:<sha>` pin
  refers to concretely: no separate release process, no manual tag entry — pushing a curation commit
  to the Aura branch (or dispatching manually) produces the pinnable tag automatically, using the
  repo's own `GITHUB_TOKEN` (no PAT setup needed). `[VERIFIED: gh api, both repos' aura-publish-image.yml content]`
- **`compose.yaml`'s current defaults are the floating `:sidecar` tag** at line 818
  (`AURA_WHATSAPP_MCP_IMAGE`) and line 997 (`AURA_PIM_MCP_IMAGE`) — confirmed by direct read, matching
  D-23's citation exactly. `pull_policy: missing` on both (only pulls if absent locally — relevant for
  local dev loops, irrelevant once the compose default names an immutable SHA tag, since a new
  `:<sha>` never collides with a cached image).

### The finding CONTEXT.md did not have: WhatsApp fork branch drift

`bridge_risk.go`'s `trustedRecipeActions[whatsAppRecipeSource]` (14 entries: `list_chats`,
`list_messages`, `search_contacts`, `get_contact`, `get_chat`, `get_contact_chats`,
`get_direct_chat_by_contact`, `get_last_interaction`, `get_message_context`, `download_media`,
`send_audio_message`, `send_file`, `send_message`, `send_reaction`) was checked against the ACTUAL
`@mcp.tool()`-decorated functions in `whatsapp-mcp-server/main.py` on every relevant branch:

| Branch | `@mcp.tool()` count | Has `get_contact` | Has `send_reaction` |
|---|---|---|---|
| `aura/cockpit-connect` (the one `compose.yaml` mounts) | **12** | **no** | **no** |
| `main` (upstream, not mounted) | 14 | yes | yes |
| `luke/group-chat`, `luke/send-files-audio`, `luke/improved-date-filtering` | 9-11 | varies | varies |

`[VERIFIED: gh api, repos/chetto1983/whatsapp-mcp/contents/whatsapp-mcp-server/main.py?ref=<branch>,
grep on @mcp.tool()/def name, 2026-08-17]`

**Consequence for planning:** the Go-side risk table was written against a 14-action surface that does
not exist on the branch Aura actually mounts. This is harmless today (dead table entries; fail-closed
protects anything unlisted-but-mounted) but it means the WhatsApp fork plan cannot simply "curate the
existing 14 tools into one multiplexed tool" — it must either (a) start by merging/cherry-picking
`get_contact` and `send_reaction` from `main` onto `aura/cockpit-connect` so the curated surface matches
what the Go table already assumes, or (b) explicitly scope the curated `messages` tool to 12 actions and
prune the 2 dead entries from `trustedRecipeActions` in the same Aura-side commit. This is a genuine
open question the discretion section did not anticipate — flag it for the planner as a first-task
decision on the WhatsApp fork plan, not an assumption to silently resolve either way.

The calendar fork has no equivalent drift: `aura-pim-mcp`'s `Program.cs` (`aura/pim-sidecar` branch)
registers exactly 14 `.WithTools<>()` lines (`ListAccountsTool`, `GetEmailsTool`,
`GetEmailDetailsTool`, `SearchEmailsTool`, `SendEmailTool`, `ListCalendarsTool`,
`GetCalendarEventsTool`, `GetCalendarEventDetailsTool`, `CreateEventTool`, `RespondToEventTool`,
`UpdateEventTool`, `GetContactsTool`, `SearchContactsTool`, `GetContactDetailsTool`) matching
`trustedRecipeActions[calendarRecipeSource]`'s 14 keys 1:1. `[VERIFIED: gh api,
repos/chetto1983/aura-pim-mcp/contents/src/CalendarMcp.HttpServer/Program.cs?ref=aura/pim-sidecar]`

### Where the curation edit actually lands in each fork

- **`aura-pim-mcp`**: `Program.cs:121-134` is the exact list of `.WithTools<CalendarMcp.Core.Tools.X>()`
  registrations (`app.MapMcp()` at `:180` serves whatever is registered). D-26 (delete, don't just
  unadvertise) means these 14 individual tool classes stop being registered and a new
  `CalendarActionTool`-shaped class implementing the flat-union `action` dispatch (D-19) replaces them,
  calling into the same underlying provider methods the 14 classes already call. No local clone exists
  on this machine — the planner's fork plan must `git clone` (or the executor must, at execution time)
  `https://github.com/chetto1983/aura-pim-mcp` on branch `aura/pim-sidecar`, since `D:/tmp/` has clones
  of `hermes-agent`, `mcp-go-sdk`, `agent-memory-fork` etc. but not either sidecar fork.
- **`whatsapp-mcp`**: `whatsapp-mcp-server/main.py` holds the `@mcp.tool()` decorators (thin wrappers)
  and `whatsapp.py` holds the actual implementation each wrapper calls. The curated `messages` tool
  replaces the `@mcp.tool()` decorators with one dispatcher function carrying the `action` argument,
  calling straight into `whatsapp.py`'s existing functions — `whatsapp.py` itself does not need to
  change, only `main.py`'s registration surface (mirrors the calendar fork's shape: implementation
  layer untouched, registration layer collapsed).

### How the fork's new surface is proven from Aura's side (no cross-repo CI)

There is no CI job in Aura's own pipeline that can see into the fork repos' CI — GitHub Actions does
not span repos without a webhook or a manual `workflow_run` trigger neither fork currently has.
Proof of a fork's new curated surface happening correctly is therefore **live, from Aura's tree**, not
compiled: mount the pinned `:<sha>` image locally (`docker compose up aura-pim-mcp whatsapp`, or against
the CI stack once the pin lands), and let the existing `calendar_integration_test.go` /
`whatsapp_integration_test.go` (tags `calendar_integration` / `whatsapp_integration`,
`AURA_MCP_CALENDAR_SERVER_JSON` / `AURA_MCP_WHATSAPP_SERVER_JSON`) drive a real `tools/list` against
the running sidecar and assert the curated tool count (`calendar` → exactly 1 model-facing tool,
`messages` → exactly 1). These two tags are NOT in `AURA_COVERAGE_TAGS` (`db_integration` only,
`scripts/coverage_gate.sh:29`), so they run in CI (if wired — verify wiring, see Validation Architecture
below) but contribute no coverage percentage; they are proof-of-shape, not proof-of-coverage.

**What CI genuinely cannot check across the repo boundary:** whether a fork commit that has NOT yet been
pushed to `aura/pim-sidecar`/`aura/cockpit-connect` (i.e., still local or on a feature branch inside the
fork) compiles or passes the fork's own tests — that is the fork's own `ci.yml`, entirely outside
Aura's pipeline. The planner should treat "fork CI green" as a precondition recorded in the phase's
`VALIDATION.md`/SUMMARY (per D-25: "the phase records the fork SHAs"), not as something Aura's own CI
job can assert.

## Architecture Patterns

### System Architecture Diagram

```
Model turn
   │
   ▼
tools.Registry.All()  ──boot──▶  gateway.ValidateClassifiable (panics if a Mutating+Multiplexed
   │                              tool has no multiplexedClassifiers entry — D-34 gates this on
   │                              classifier EXISTENCE, not on any `action` enum in the schema)
   │
   ▼ (per turn)
bridgedTool.Spec()  ──deferred?──▶ bridgePolicy.defaultDeferred()
   │                                  today: unconditional true
   │                                  D-27: true UNLESS modelFacing tool count ≤ 3 AND the
   │                                        global 2-slot budget is unspent (deterministic
   │                                        grant order, mount order)
   ▼ (if loaded, i.e. calendar / messages / a ≤3-tool self-minted server)
model dispatches calendar(action=X) or messages(action=Y)
   │
   ▼
gateway.classify(spec, rawArgs)
   │  spec.Multiplexed == true (set at bridge time, D-21/D-34) →
   │  multiplexedClassifiers["calendar"/"messages"](rawArgs)
   │     └─ unmarshals {action}, looks up trustedRecipeActions[recipeSource][action]
   │        (RE-KEYED from raw-tool-name to action-name by this phase)
   ▼
scoring.RiskTier (Safe / Normal / Risky / Destructive)
   │
   ▼
decide.go: only Destructive stops the turn for operator approval
   │
   ▼
bridgedTool.Execute → srv.CallToolText → the fork's HTTP MCP endpoint
   │                                        (fork routes action → underlying provider call,
   │                                         curation and provider dispatch both live here)
   ▼
newResult(): tools.NewResult + Provenance{Trust: TrustTrusted}  (no envelope, D-01)
   │
   ▼
aura.tool_invocations row (args_raw, result_preview, meta) — the live-evidence read surface
```

### Recommended structure of the Aura-side change (no new files needed)

```
internal/agent/mcptools/
├── bridge.go            # specFromToolDefWithPolicy: set Multiplexed (D-21, gated by D-34)
├── bridge_risk.go        # re-key trustedRecipeActions to action-keyed for calendar+whatsapp,
│                          # keep memory tool-keyed (D-35, documented mixed-key comment);
│                          # add mount-time reconciliation warn-log (D-33)
├── bridge_memory.go       # defaultDeferred(): add the count predicate (D-27) — or a small
│                          # new file bridge_deferral.go if this pushes bridge_memory.go's
│                          # LOC past the 600 threshold with headroom needed for D-33's log
internal/gateway/
├── classify.go           # add "calendar" and "messages" (or actual namespaced names,
│                          # e.g. "calendar" and "whatsapp__messages" — verify exact
│                          # namespaced spec.Name at bridge time) to multiplexedClassifiers
compose.yaml               # both image pins move from :sidecar to :<sha> (D-23), landing in
                            # the SAME commit as the above (D-32)
docs/superpowers/specs/
└── 2026-08-1X-mcp-curated-surface-design.md   # NEW: the contract doc (D-24), naming both
                                                 # curated tools' action sets, the flat-union
                                                 # shape (D-19), and the accountId fix (D-20)
```

### Pattern: mixed-key risk table with a documented discriminator (D-35)

```go
// trustedRecipeActions is keyed by whatever discriminator that source's surface
// exposes: calendar and whatsapp are ACTION-keyed (their tools are multiplexed
// behind one curated action enum); memory stays RAW-TOOL-NAME-keyed (it is not
// merged — each memory_* call is its own MCP tool name). One table, one lookup
// site in classifyToolRisk, two key spaces documented here so a future reader
// does not assume uniformity that was never true.
var trustedRecipeActions = map[string]map[string]mcpActionClass{
    calendarRecipeSource: { /* keyed by action, e.g. "send_email": mcpActionDestructive */ },
    whatsAppRecipeSource: { /* keyed by action */ },
    mcp.SourceRecipeMemory: { /* keyed by raw tool name, e.g. "memory_forget" */ },
}
```

### Pattern: gate `Multiplexed` inference on classifier existence, not on schema shape (D-34)

```go
// specFromToolDefWithPolicy — do NOT set Multiplexed from "does the schema have
// an `action` property". That would make ValidateClassifiable panic at boot for
// ANY stranger's server whose schema happens to use an `action` argument — the
// opposite of Success Criterion 6. Multiplexed is true only when this specific
// tool's namespaced name already has an entry in classify.go's
// multiplexedClassifiers — i.e., only for the two curated tools Aura itself knows
// about; every other bridged tool (a self-minted or ad hoc mount) gets the
// generic Mutating/Destructive flat tier, which is exactly the fail-closed
// promise SC#6 makes for an unknown server.
spec.Multiplexed = isKnownMultiplexedMCPTool(spec.Name)
```

### Anti-Patterns to Avoid

- **A second risk table or a `classifyComms` reading curation data:** explicitly rejected (D-21). The
  existing `trustedRecipeActions` + `multiplexedClassifiers` machinery is the ONLY risk source; adding
  a parallel one for the two curated tools reintroduces exactly the class of bug D-21 fixes.
- **Deriving tiers from server-declared MCP annotations** (`ReadOnlyHint`/`DestructiveHint`) for the two
  curated tools: `explicitDestructive` is deliberately escalate-only (never de-escalate) — trusting a
  fork's own annotation to LOWER a tier would let the fork talk itself out of an approval gate.
- **Inferring `Multiplexed` from schema shape** (any `action` property): panics Aura's boot for a
  stranger's server (D-34's hazard, above).
- **A dual-key transition table for the landing-order cutover**: rejected (D-32) in favor of one atomic
  commit gated by the immutable pin — avoids a table that means two things depending on which pin is
  live, plus a forgettable cleanup commit.

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Curated tool surface for calendar/WhatsApp | An Aura-side `comms` facade tool, hide-list, or curation config | Curate inside each fork's own tool registration (`Program.cs` / `main.py`) | D-17: every MCP server Aura ships is a fork Aura controls; Aura's bridge stays generic for the NEXT mounted server too |
| Per-action risk gating for a multiplexed tool | A new classifier abstraction or a `bridgePolicy` namespace table | `multiplexedClassifiers` + `trustedRecipeActions`, both already shipped | D-21: same data, re-keyed — adding a second risk source is the exact bug this phase closes |
| Always-loaded tool priority | A `_meta`-declared hint, or an allowlist of tool names | Tool-count arithmetic (`modelFacing` count ≤ 3, global cap 2) | D-27: the MCP protocol has NO priority field (`mcp/protocol.go` inventory, `Tool` struct) — an annotation-derived rule was falsified before being proposed; a name-list is the exact declaration ceremony D-17 forbids |
| Cross-repo drift detection | A checked-in `tools/list` capture fixture regenerated on demand | Mount-time reconciliation: WARN-log any curated `action` enum value the mounted server doesn't advertise (D-33) | A fixture needs manual regeneration and goes stale silently; a live reconcile at every mount catches drift the moment it matters |

**Key insight:** every "don't hand-roll" here resolves to the same instinct D-17 named — a problem
solvable in the server (which Aura owns, being a fork) or with already-shipped Aura machinery should
never grow a new Aura-side abstraction.

## Common Pitfalls

### Pitfall 1: Landing the Aura-side re-key before the fork publishes
**What goes wrong:** if `trustedRecipeActions` is re-keyed from raw-tool-name to action-name before the
fork's curated tool actually exists, every raw tool call (`calendar__send_email`, etc., still the only
thing mounted) falls through to the fail-closed default (`return true, true`) because the table no
longer has raw-tool-name keys — every calendar/WhatsApp call, including reads, starts demanding
approval.
**Why it happens:** the table lookup key changes meaning; there is no transition state where both key
shapes work.
**How to avoid:** D-32's ordering — fork publishes first (image tag exists), then ONE Aura commit lands
the pin + re-key + `Multiplexed` + classifier entry together. Never split across commits.
**Warning signs:** a live scenario where a calendar READ (e.g. `list_calendars`) suddenly triggers an
approval prompt is the tell.

### Pitfall 2: Treating the WhatsApp Go-side table as ground truth for what to curate
**What goes wrong:** building the `messages` tool's action set purely from
`trustedRecipeActions[whatsAppRecipeSource]`'s 14 keys produces a curated tool advertising
`get_contact`/`send_reaction` that the mounted `aura/cockpit-connect` branch cannot actually serve
(404/method-not-found at call time).
**Why it happens:** the table was written against a broader surface (possibly `main`) than what is
mounted today — verified drift, see Fork Delivery Mechanics above.
**How to avoid:** the WhatsApp fork plan's first task must resolve this explicitly — merge the 2 missing
actions from `main`, or scope the curated tool (and the Go table) to the 12 that exist. Do not silently
assume either direction.
**Warning signs:** a live call to `messages(action=get_contact)` in the E2E scenario returns a
transport/method error instead of a result.

### Pitfall 3: `refreshSpec` flipping deferral mid-conversation
**What goes wrong:** `bridge.go:66-100`'s `refreshSpec` recomputes `spec.Deferred` on every reconnect.
If D-27's count predicate is naively re-evaluated there too, a server's tool count crossing the
threshold across a reconnect (e.g. the fork briefly advertises a 4th tool during a bad deploy, then
corrects) flips the manifest mid-conversation, invalidating the KV cache prefix the model was relying
on.
**Why it happens:** `refreshSpec` already warns on `Mutating`/`Destructive` flag changes and required-arg
changes (existing pattern, lines 78-98) — deferral needs the same treatment or an explicit freeze, and
it is easy to wire the count check only at mount time and forget the reconnect path.
**How to avoid:** follow the existing warn-on-change pattern for deferral flips, or freeze the deferred
bit at mount time and never recompute it on `refreshSpec` (CONTEXT.md leaves this to Claude's
discretion — pick one and document the choice, don't leave it unaddressed).
**Warning signs:** a tool present in one turn's manifest silently vanishing (or appearing) later in the
same conversation with no user-visible mount/unmount event.

### Pitfall 4: COMPAT blast radius from deleting the 28 raw tool names (D-26)
**What goes wrong:** any persisted row in `aura.tool_invocations`, a paused approval
(`aura.paused_states`), or a scheduled `agent_job` referencing `calendar__send_email` or any of its 27
siblings has nothing to resolve against the moment the pin flips — those requirements (COMPAT-01/02/03)
are assigned to Phases 47/48, AFTER this phase removes the names.
**Why it happens:** D-26 is deliberately one-way (deleting, not merely unadvertising, per operator
decision) — dark code (still-callable-but-unadvertised handlers) is forbidden by CLAUDE.md.
**How to avoid:** this phase does not own the fix, but must not silently absorb or hide the blast radius.
Record it explicitly in the phase's SUMMARY/handoff so Phase 47/48 planning starts from a known list of
what breaks, not a rediscovery.
**Warning signs:** none observable within Phase 46 itself — the risk surfaces only when Phase 47/48
rehydrate old history or resume an old pause. Documentation, not code, is this phase's mitigation.

## Code Examples

### Verified pattern: fail-closed default for an unannotated/unlisted MCP tool
```go
// Source: internal/agent/mcptools/bridge_risk.go:130-133 (read live, 2026-08-17)
a := t.Annotations
if a == nil {
    return true, true
}
```

### Verified pattern: monotone saturate-upward classification never lowers below the mutating floor
```go
// Source: internal/gateway/classify.go:53-64
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

### Verified pattern: boot-time panic on a wiring gap, not a runtime under-gate
```go
// Source: internal/gateway/guard.go:22-38 (this is the guard D-34 must not break for
// an unknown server, and the one D-21's re-key must satisfy for the two curated tools)
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

### Fork publish workflow (identical shape in both forks, verified via `gh api`)
```yaml
# Source: aura-publish-image.yml, both chetto1983/aura-pim-mcp@aura/pim-sidecar
# and chetto1983/whatsapp-mcp@aura/cockpit-connect (read live 2026-08-17)
on:
  workflow_dispatch:
  push:
    branches: [aura/pim-sidecar]   # or aura/cockpit-connect
    paths: ['src/**', 'Dockerfile', ...]
jobs:
  publish:
    steps:
      - uses: docker/build-push-action@v6
        with:
          tags: |
            ghcr.io/${{ github.repository }}:sidecar
            ghcr.io/${{ github.repository }}:${{ github.sha }}
```

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|---------------|--------|
| Per-call MCP result fencing (`TrustUntrusted` envelope) | `TrustTrusted`, no envelope | `34b892512`, 2026-08-12 | Ratified this phase (D-05); no code |
| 28 raw calendar+WhatsApp MCP tools, deferred | 2 curated always-loaded multiplexed tools | This phase (MCP-04) | Manifest slot cost drops from ~56 raw defs (undeferred) to 2 curated tools at a ~2KB-each description budget (D-36) |
| `bridgePolicy.defaultDeferred()` unconditional `true` | `true` unless modelFacing-tool-count ≤ 3 and the 2-slot global budget is unspent | This phase (D-27) | First non-constant deferral rule in the bridge |
| Floating `:sidecar` image tag | Immutable `:<sha>` tag | This phase (D-23) | "Which tool surface is live" becomes answerable from `compose.yaml` alone |

**Deprecated/outdated:** Amendment #110's *"non-persisted, untrusted reference item"* framing for the
memory block is superseded by D-03/D-05, not restored.

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | The exact file `Program.cs` line range (121-134) and `main.py`/`whatsapp.py` split will still be current when the fork plan's executor actually clones and edits — verified 2026-08-17, could drift if either fork's `main`/Aura branch is pushed to before the plan executes | Fork Delivery Mechanics | Low — a fresh `gh api` or `git clone` re-read at execution time trivially re-confirms; the shape (registration list vs. implementation file) is a stable convention in both frameworks |
| A2 | Neither fork has any other CI job that would block a push to the Aura branch beyond `ci.yml` (not inspected in full — only `aura-publish-image.yml` was read in full) | Fork Delivery Mechanics | Low-Medium — if `ci.yml` has strict gates (e.g. required status checks, branch protection) the fork plan's push could be blocked; verify `ci.yml` content and any branch protection rules before executing the fork plans |
| A3 | The curated tool's exact namespaced `spec.Name` (e.g. `calendar` vs `calendar__calendar` vs `pim__calendar`) that must be the key in `multiplexedClassifiers` — depends on the namespace string used at `Mount()` time, not yet decided | Architecture Patterns | Medium — using the wrong key means the classifier entry never matches and `ValidateClassifiable` panics at boot; must be read from the actual mount-time namespace before writing the classifier registration |

## Open Questions

1. **Does the WhatsApp fork plan merge `get_contact`/`send_reaction` from `main`, or scope the curated
   tool to the 12 actions `aura/cockpit-connect` already has?**
   - What we know: both actions exist cleanly on `main`, isolated to `main.py`/`whatsapp.py` (no merge
     conflict expected against `aura/cockpit-connect`'s divergence, per file-level diff inspection).
   - What's unclear: whether `aura/cockpit-connect` has diverged from `main` in ways that make a clean
     cherry-pick non-trivial (not fully diffed here — only tool-registration presence was checked).
   - Recommendation: the fork plan's first task should attempt the merge and fall back to the 12-action
     scope (plus pruning the two dead Go-table entries) only if the merge proves non-trivial; either way,
     record the choice explicitly rather than defaulting silently.

2. **Exact key `multiplexedClassifiers` needs for each curated tool** (see A3 above).
   - Recommendation: read the actual namespace passed to `Mount()`/`mountWithAdvertisedPolicy` for the
     PIM and WhatsApp managed servers in `internal/mcp/manager/catalog.go` (`BuiltInCatalog`) before
     writing the classifier map entry — do not guess the namespace string.

3. **Where D-27's count predicate lives, and how `refreshSpec` avoids flipping deferral mid-turn**
   (explicitly left to Claude's discretion in CONTEXT.md) — Pitfall 3 above lays out the hazard; the
   planner must pick a concrete answer (freeze-at-mount vs. warn-on-reconnect-change) and record it, not
   leave it implicit in the diff.

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| `gh` CLI (GitHub) | Fork inspection, verifying branch/workflow state | ✓ | authenticated as chetto1983 (per user memory) | — |
| Local clone of `chetto1983/aura-pim-mcp` | Fork plan execution (editing `Program.cs`) | ✗ (not under `D:/tmp/`) | — | `git clone` at plan-execution time; no viable fallback, this IS the work |
| Local clone of `chetto1983/whatsapp-mcp` | Fork plan execution (editing `main.py`) | ✗ (not under `D:/tmp/`) | — | `git clone` at plan-execution time |
| .NET 10 SDK (for `aura-pim-mcp` local build/test) | Verifying the C# curation compiles before pushing | Not probed this session — `docs/superpowers/plans/2026-06-16-aura-pim-mcp-fork-design.md` documents WSL .NET 10 prerequisites from the original fork build | — | Rely on the fork's own `ci.yml` running in GitHub Actions if local .NET is unavailable |
| `docker compose` (local stack) | Mounting the pinned images for live E2E (SC#1/#2/#4/#6) | Assumed available (project runs on containerized stack per CLAUDE.md) | — | — |

**Missing dependencies with no fallback:**
- Local clones of both forks do not exist and must be created as the literal first step of each fork
  plan — this is expected, not a blocker, since the work product IS a commit in each fork repo.

**Missing dependencies with fallback:**
- Local .NET 10 toolchain — the fork's own `ci.yml` can substitute if WSL/.NET is unavailable locally,
  at the cost of a slower push-and-wait verification loop.

## Validation Architecture

### Test Framework
| Property | Value |
|----------|-------|
| Framework | Go `testing` + build tags (no third-party test framework) |
| Config file | none — tag-gated files under `internal/mcp/*_test.go`, `internal/agent/mcptools/*_test.go`, `internal/gateway/*_test.go` |
| Quick run command | `go test ./internal/agent/mcptools/... ./internal/gateway/...` (no tags — daemon-free unit tier) |
| Full suite command | `AURA_DB_URL=... AURA_DB_MIGRATE_URL=... go test -tags db_integration -p 1 ./internal/...` (mirrors `scripts/coverage_gate.sh`) |

### Phase Requirements → Test Map

| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|---------------------|-------------|
| MCP-01 | MCP descriptions carry no distrust prefix | unit | `go test ./internal/agent/mcptools/ -run TestFrameMCPDescription` | ✅ (`bridge_trust_test.go` or equivalent already covers `34b892512`'s shipped behavior — confirm exact test name at plan time) |
| MCP-02 | Fail-closed default for unannotated/unlisted tool; approval gate; namespacing panic-on-duplicate; schema byte caps | unit | `go test ./internal/agent/mcptools/ -run TestClassifyToolRisk` (fail-closed default), `go test ./internal/agent/mcptools/ -run TestCapSchemaDescriptions` | ✅ `bridge_risk_test.go`, byte-cap tests likely in `bridge_spec_test.go`/`bridge_edges_test.go` |
| MCP-03 | Trust unconditional across every mounted server | unit | `go test ./internal/agent/mcptools/ -run TestNewResult` (or wherever `TrustTrusted` is asserted) | ✅ `bridge_trust_test.go` |
| MCP-04 (SC#1, SC#2) | Two always-loaded multiplexed tools, per-action classification survives the merge | unit (classification logic) + live E2E (manifest + approval flow) | unit: `go test ./internal/gateway/ -run TestClassify` extended with calendar/messages cases; live: driven conversation, `aura.tool_invocations` query | ❌ Wave 0 — new test cases for the two new `multiplexedClassifiers` entries do not exist yet; new unit test asserting `bridgePolicy.defaultDeferred()`'s count arithmetic (D-27) does not exist yet |
| MCP-05 (SC#4) | `accountId` never in dispatched args for calendar calls | live E2E only (the fix is fork-side; Go-side has nothing to unit-test except "the fork doesn't require it", which is an integration-tier assertion) | `calendar_integration` tag test extended to assert no `accountId` in a `get_calendar_event_details` call built from a prior `list` result | ❌ Wave 0 — extend `calendar_integration_test.go` |
| TOOL-14 | Tiering axis change (frequency + count budget) documented and enforced | unit | `go test ./internal/agent/mcptools/ -run TestDefaultDeferred` (new) | ❌ Wave 0 |
| SC#3 | Every mounted server's descriptions render as ordinary text | unit (already covered by MCP-01 test) + live spot-check | same as MCP-01 | ✅ (unit), live spot-check has no dedicated automation (visual read of a live turn) |
| SC#6 | A new unlisted server (calculator fork) mounts with no code/config change, fail-closed at Mutating+Destructive | integration (build-tag) | `go test -tags calculator_integration ./internal/mcp/ -run TestCalculatorServerLive` | ✅ already exists (`calculator_integration_test.go`) — extend with a risk-tier assertion if not already present |

### Sampling Rate
- **Per task commit:** `go test ./internal/agent/mcptools/... ./internal/gateway/...` (daemon-free,
  seconds) — run after every edit to `bridge_risk.go`, `bridge.go`, `bridge_memory.go`, `classify.go`,
  `guard.go` per CLAUDE.md's post-edit validation rule (`go vet`, `go build`, `go test`,
  `go test -race` for touched packages).
- **Per wave merge:** `bash scripts/coverage_docker.sh` (full `db_integration`-tagged aggregate, the
  85%-floor gate) plus, if the stack is up, a manual `calendar_integration`/`whatsapp_integration` run
  against the newly-pinned images.
- **Phase gate:** full suite green (`make quality-full`) before `/gsd-verify-work`; additionally the
  live driven-conversation E2E (D-37) scored per CLAUDE.md's Definition of Done (>9.8), which no
  automated command can substitute for — see below.

### Wave 0 Gaps
- [ ] `internal/gateway/classify_test.go` (or a new `classify_multiplexed_comms_test.go`) — unit tests
  for the two new `multiplexedClassifiers` entries (calendar/messages) covering: a read action → Safe,
  a mutate action → Normal, a destructive action → Destructive, an unrecognised action → Risky
  (fail-safe, mirrors `classifySkill`/`classifyTask`'s pattern exactly).
- [ ] `internal/agent/mcptools/bridge_deferral_test.go` (or extend `bridge_memory_policy_test.go`) —
  unit tests for D-27's count predicate: a server with ≤3 model-facing tools and budget available →
  not deferred; a server with >3 → deferred; the 2-slot global cap exhausted → third qualifying server
  stays deferred even though it individually qualifies (this is the case most likely to be missed).
- [ ] `internal/gateway/guard_test.go` — extend with a case asserting D-34's gate: a tool whose schema
  carries an `action` property but has NO `multiplexedClassifiers` entry does NOT get `Multiplexed:
  true` inferred, and boots cleanly (proves SC#6's fail-closed-not-panic promise for a stranger's
  server).
- [ ] `internal/agent/mcptools/bridge_risk_test.go` — extend for the re-keyed (action-keyed)
  `trustedRecipeActions[calendarRecipeSource]`/`[whatsAppRecipeSource]`, replacing whatever raw-tool-name
  keyed cases exist today; keep `mcp.SourceRecipeMemory`'s raw-tool-name-keyed cases unchanged (D-35).
- [ ] Mount-time reconciliation (D-33) — a small unit test asserting an unknown action in a mounted
  curated tool's `action` enum produces a WARN log by name and does NOT panic boot.
- [ ] `calendar_integration_test.go` / `whatsapp_integration_test.go` — extend to assert the curated
  tool's action count and, for calendar, that no `accountId` argument is required by the detail-tool
  call built from a prior list result (MCP-05/SC#4's only integration-tier assertion point).

None of these six gaps are daemon-gated except the last (`*_integration` tag, requires the live
sidecar) — the first five are pure-function unit tests over already-daemon-free packages
(`internal/gateway`, `internal/agent/mcptools`), so they DO feed the `db_integration`-only 85% coverage
gate and must exist before the phase closes, per CLAUDE.md's "daemon/container-gated runtime code
needs daemon-free unit tests" rule (which applies here even though nothing in this phase is
container-gated — the rule's spirit, keeping the floor real, still applies to any new branch added to
already-covered files).

### Live evidence, per success criterion (the six SCs, mapped to signal/source/tier)

| SC | Observable signal | Where read from | Tier that can assert it | Daemon-free unit backing the gate |
|----|---|---|---|---|
| SC#1 (2 always-loaded multiplexed tools, curation visible in fork's own `tools/list`) | Live turn's rendered manifest shows exactly `calendar` and `messages` (or their namespaced names), not 28 raw tools; a direct `tools/list` call against the mounted sidecar shows the same curated set | OTel span / manifest render log; direct MCP `tools/list` response | Live E2E only — manifest composition is a runtime property, no unit test can assert "what the model actually saw this turn" | `bridge_deferral_test.go` (D-27's count arithmetic) is the unit-level proxy — it proves the RULE is correct, not that a specific live turn obeyed it |
| SC#2 (destructive action gates, read in the same tool doesn't) | `aura.tool_invocations` shows an approval-pending row for e.g. `calendar(action=send_email)` and a completed row with no approval step for `calendar(action=list_calendar_events)` in the same conversation | `aura.tool_invocations` (status, meta columns), approval ledger | Live E2E required for the end-to-end proof; `classify_multiplexed_comms_test.go` unit-proves the classification function in isolation | `classify_multiplexed_comms_test.go` (Wave 0 gap above) |
| SC#3 (descriptions render as ordinary text, no distrust framing) | Rendered tool description text in a live turn transcript contains no distrust marker/prefix | Transcript / prompt-render log | Unit-testable in full (`frameMCPDescription` is pure) — live spot-check is confirmatory only | Already exists (MCP-01 unit test) |
| SC#4 (`accountId` never in dispatched args for calendar calls) | `aura.tool_invocations.args_raw` for a `get_calendar_event_details` call contains no `accountId` key (or contains only the opaque reference the fork itself issued) | `aura.tool_invocations.args_raw` (jsonb/text inspection) | Integration tier (`calendar_integration`) can assert the fork's own schema no longer requires it; the LIVE dispatched-args proof needs a real turn | `calendar_integration_test.go` extension (Wave 0 gap) |
| SC#5 | **DELETED (D-07)** — do not attempt to prove; a plan that tries "results carry instruction-shaped text and are not acted on" is reintroducing a criterion the operator explicitly struck | — | — | — |
| SC#6 (new unlisted server usable with zero code, fail-closed at Mutating+Destructive) | Mounting `chetto1983/calculator-mcp-server` live produces a usable tool with no catalog/code change, and its risk tier reads Destructive for any mutating action with no annotation | `calculator_integration_test.go` output; `aura.tool_invocations` for a live-driven call | `calculator_integration` tag proves the mount-and-classify mechanics; the "no code change was needed" claim is a documentation/process assertion the driven conversation's narration must state explicitly (per D-38's caveat: the server IS referenced in Aura's tree via the test fixture, so the evidence must show the MOUNT needed nothing new, not that the server was unknown) | `calculator_integration_test.go` already exists; extend with an explicit risk-tier assertion if absent |

**D-37's evidence discipline applies across SC#1/#2/#4**: one driven conversation against the real
running stack, reading a calendar, sending something that trips the approval gate, and following an
event from listing through to detail — quote the actual `aura.tool_invocations` rows in
`VALIDATION.md`. A green test suite alone does not close this phase (CLAUDE.md Definition of Done);
the live scenario must be scored, and per the project's evidence policy this is read from OTel traces
and `aura.tool_invocations`, never inferred from test output.

## Security Domain

### Applicable ASVS Categories

| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V2 Authentication | no (MCP servers here are operator-mounted infra, not user-auth surfaces) | — |
| V3 Session Management | no | — |
| V4 Access Control | yes | `mcpToolRisk`'s fail-closed default + the model-blind approval gate (`approve.go`) — no tool schema exposes the approval mechanism to the model, closing a self-approval vector |
| V5 Input Validation | yes | `capSchemaDescriptions` (16KB schema / 128-property / per-arg byte caps) bounds a server-controlled schema before it reaches the model or `tool_search`; `classify`'s saturate-upward parse-failure handling (`json.Unmarshal` error → `Risky`, never `Safe`) |
| V6 Cryptography | no (no new crypto surface in this phase) | — |

### Known Threat Patterns for this stack

| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| A curated MCP tool's `action` enum drifting from what the fork actually serves (Fork Delivery Mechanics finding above) | Tampering / Denial of Service (of a specific action, not the whole mount) | D-33: WARN-log at mount time by action name; fail-closed at call time for an unrecognised action (same path `classify` already uses for unknown input) |
| Prompt injection via untrusted MCP result text now read as ordinary (trusted) text | Spoofing / Elevation of Privilege | Explicitly accepted residual risk by operator decision (D-01) — mitigated only by the surviving guardrails (fail-closed risk classification, model-blind approval gate, namespacing) and operator control over what gets mounted. Not this phase's problem to re-litigate. |
| A fork's schema flooding the model-facing manifest (large descriptions, many properties) | Denial of Service (context budget) | `capSchemaDescriptions` byte/property caps (already shipped); D-36's tight ~1.5-2KB merged-description budget for the two always-loaded tools specifically, since they are now paid every turn |
| A stranger's mounted server whose schema happens to use an `action` property tricking the classifier into treating it as a known multiplexed tool | Elevation of Privilege (wrong risk tier assigned) | D-34: `Multiplexed` inferred ONLY when a `multiplexedClassifiers` entry already exists for that exact tool name — an unknown server can never self-promote into a known classifier's tier |

## Sources

### Primary (HIGH confidence)
- `internal/agent/mcptools/bridge.go`, `bridge_risk.go`, `bridge_memory.go`, `bridge_call.go` — read
  in full 2026-08-17 at the current tree state (`02a291530`).
- `internal/gateway/classify.go`, `guard.go` — read in full 2026-08-17.
- `compose.yaml:780-1035` — read directly, confirms line numbers cited in CONTEXT.md D-23.
- `scripts/coverage_gate.sh` — read in full, confirms `AURA_COVERAGE_TAGS` default (`db_integration`
  only) and the anti-footgun live-DB guard.
- `internal/db/migrations/0011_tool_invocations.up.sql` — confirms `aura.tool_invocations` schema
  (`tool_name`, `args_raw`, `status`, `meta` columns) as the live-evidence read surface.
- GitHub API (`gh api repos/chetto1983/aura-pim-mcp/...`, `gh api repos/chetto1983/whatsapp-mcp/...`) —
  branch listings, workflow file contents, and tool-registration source files, all read live 2026-08-17.
  This is the primary new evidence this research adds beyond CONTEXT.md.

### Secondary (MEDIUM confidence)
- `docs/superpowers/specs/2026-06-16-calendar-pim-mcp-fork-design.md` — read in full as the design-doc
  precedent D-24 extends; dated 2026-06-16, describes the ORIGINAL 29→~16 trim, not this phase's
  14→1 multiplex (superseded shape, still the correct structural precedent for the NEW doc's format).

### Tertiary (LOW confidence)
None — every claim in this document is either read directly from code/API or explicitly logged in the
Assumptions table above.

## Metadata

**Confidence breakdown:**
- Standard stack / architecture: HIGH — nothing new to verify beyond re-confirming CONTEXT.md's cited
  line numbers, all of which matched.
- Fork delivery mechanics: HIGH on CI/publish mechanism (read live via `gh api`); MEDIUM on the exact
  in-fork diff shape (depends on open questions the planner must resolve, notably the WhatsApp branch
  drift finding).
- Pitfalls: HIGH — all four are either re-derived from code already read or a direct extension of a
  hazard CONTEXT.md already named (D-32, D-34).
- Validation Architecture: HIGH on the mapping (build tags, coverage tag set, and `aura.tool_invocations`
  schema all confirmed by direct read); MEDIUM on which exact existing test function names cover MCP-01
  /MCP-02/MCP-03 today (not individually enumerated — the planner should `grep` the exact test names
  before writing task-level verification steps).

**Research date:** 2026-08-17
**Valid until:** 14 days (fast-moving — depends on live fork repo state, which this research found to
have already drifted once; re-verify branch state with `gh api` before executing either fork plan if
more than a few days have passed)
