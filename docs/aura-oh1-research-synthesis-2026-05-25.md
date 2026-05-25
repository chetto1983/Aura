# Wave OH1 — 4-agent research synthesis (2026-05-25)

Closes the research circle on Wave OH1 (multi-agent foundation: AGENTDEF
+ TIER + DELEGATE-TOOL). Four parallel agents shipped: 1 deep audit of
openhuman, 1 sweep of other D:/tmp repos, 1 online production
ecosystem scan, 1 internal Aura swarm/agents integration map.

Source docs:
- `docs/research-openhuman-oh1-deep-2026-05-25.md` (field-level openhuman audit)
- `docs/research-tmp-other-multi-agent-2026-05-25.md` (codex, nanobot, hermes, cli-printing-press, claude-code leak)
- `docs/research-online-multi-agent-2026-05-25.md` (Anthropic, LangGraph, CrewAI, AutoGen, Pydantic AI, OpenAI Agents SDK)
- `docs/research-aura-swarm-integration-map-2026-05-25.md` (existing 1900 LOC mapped)

---

## 1. Verdict — ship OH1 as-designed with 6 concrete refinements

**The openhuman-derived OH1 design holds up against the 2024-2026
ecosystem.** Three independent confirmations:
- Anthropic's June-2025 Multi-Agent Research System post + Dec-2024
  "Building Effective Agents" both recommend the orchestrator-worker
  shape, flag same-tier delegation and unbounded recursion as
  production foot-guns.
- Claude Code's subagent YAML frontmatter (`name/description/model/tools/skills`
  + body=prompt) is an almost field-for-field analogue of AGENTDEF.
- codex (Apache-2.0), nanobot (MIT), and the leaked Claude Code prompt
  converge on named-archetype + per-archetype tool allowlist + sub-loop
  + parent-doesn't-see-child-trace.

What changes vs the original plan: **6 absorbable refinements** (4
schema-level, 2 sequencing-level) and **1 strategic re-ordering**
(REFLECTION hook ships BEFORE AGENTDEF as a fork-and-restrict, then
migrates to a `reflector` archetype later).

---

## 2. Schema refinements (OH1-S1)

### R1 — Tool synthesis schema is `{prompt, model?}` NOT `{task}`

- **Source**: openhuman `archetype_delegation.rs:22-37` (Agent #1 deep
  audit corrected the previous lift).
- **Why**: `prompt` (required) + `model` (optional per-call override)
  lets the chat tier pin a per-call exact model without editing the
  archetype TOML. Major UX leverage for ~1 line of schema.
- **Aura change**: `delegate_<id>` parameters become
  `{"prompt": string, "model": string?}` instead of the originally-
  planned `{"task": string}`.

### R2 — Fail-closed defaults: `inherit_*=false`, `AgentTier=Worker`

- **Source**: openhuman `definition.rs::ApplyDefaults` (Agent #1) +
  online research §F12 anti-pattern "parent-history leak" (Agent #3).
- **Why**: openhuman uses `omit_*=true` defaults (blacklist semantics);
  the online research surfaces a cleaner shape: flip to `inherit_*=false`
  (whitelist semantics). Both fail-closed at the "subagent gets lean
  context" goal, but the whitelist is forward-compatible — adding a new
  context category in the future doesn't silently leak to existing
  archetypes.
- **Aura change**:
  - `AgentDefinition.InheritIdentity bool` (default `false`)
  - `AgentDefinition.InheritMemory bool` (default `false`)
  - `AgentDefinition.InheritSafety bool` (default `false`)
  - `AgentDefinition.InheritSkills bool` (default `false`)
  - `AgentDefinition.InheritProfile bool` (default `false`)
  - `AgentDefinition.Tier AgentTier` (default `Worker` — the most-
    restricted tier; user-shipped TOMLs missing tier fail boot via
    worker-cannot-have-subagents rule).
- **Note for back-compat**: the existing summarizer migration (OH1-S1
  commit B) must set `Inherit*=true` for the few sections it actually
  needs, otherwise its behavior changes silently.

### R3 — Add `max_input_tokens` / `max_output_tokens` per archetype

- **Source**: online research §F7 (Pydantic AI's `UsageLimits`).
- **Why**: per-archetype caps prevent runaway-model cost on a worker
  delegated from chat tier. Today `swarm.DelegationPolicy` has
  `MaxResultChars` and `ChildMaxIterations` but no token caps.
- **Aura change**: `AgentDefinition.MaxInputTokens int` (0 = inherit
  loop default), `AgentDefinition.MaxOutputTokens int` (0 = inherit).
  Enforced at the LLM client boundary in the sub-loop, returns
  structured "budget exhausted" error on overshoot.

### R4 — File format: TOML stays, but reopen JSON as discuss-phase option

- **Source**: openhuman positional-key footgun (`subagents=[…]` must
  precede `[table]` headers — Agent #1) + Aura already uses JSON for
  `mcp.json` (Agent #4).
- **Why**: TOML is openhuman's choice and a 1:1 lift, BUT Aura's
  existing ops convention is JSON. Reopening this is cheap because
  the loader is the only consumer (parsing layer swap is 50 LOC).
- **Aura discuss-phase question**: TOML for openhuman fidelity, OR
  JSON for Aura ops consistency. My recommendation: **JSON** — fewer
  formats, no positional footgun, matches `mcp.json`. Override
  during OH1-S0 if openhuman-fidelity argument carries.

---

## 3. Behavioral refinements (OH1-S3)

### R5 — Merge OH1-S4 (DEDUP) into OH1-S3 (DELEGATE-TOOL)

- **Source**: openhuman runs dedup in TWO places (main manifest + sub-
  agent assembly) because both paths can collide (Agent #1).
- **Why**: `delegate_<id>` synthesis happens per-turn and per-channel;
  a user-set `delegate_name` can silently shadow an action tool name
  (`wiki_page`, `search`, `file`). Shipping dedup as a follow-up commit
  leaves a window where collision is unguarded and breaks regressions
  retroactively.
- **Aura change**: OH1-S3 (DELEGATE-TOOL) absorbs the dedup guard +
  ships a collision regression test in the same commit. OH1-S4
  becomes obsolete — drop from the wave or repurpose for a different
  story (see R6).

### R6 — Add cycle detector via parent-id stack (~10 LOC)

- **Source**: online research §F9 (catches A→B→A cross-tier cycles
  the same-tier rule misses — Agent #3).
- **Why**: same-tier rejection blocks `chat→chat` and `reasoning→reasoning`
  but not `chat→worker→worker_2→chat` (cross-tier loop). Parent-id
  stack catches every cycle for ~10 LOC.
- **Aura change**: a stack of archetype IDs walked during delegate
  invocation; if the target ID is already in the stack, reject with
  structured cycle error. Add as the freed OH1-S4 (or fold into
  OH1-S3 if scope allows).

### R7 — Hardcoded over-delegation prefix on every synth tool

- **Source**: openhuman `orchestrator_tools.rs:125-128` — every
  synthesised `delegate_<id>` description starts with `"Use only
  when direct response/direct tools are insufficient. "` (Agent #1).
- **Why**: single biggest behavioural lever against over-delegation.
  Forces the chat tier to think "do I need this specialist?" before
  every call.
- **Aura change**: prepend this exact prefix to every synthesised
  delegate tool description. ~5 LOC inside the synth function.

### R8 — Channel-scrubber + announce template for Telegram/dashboard

- **Source**: nanobot `subagent_channel_display.py` + `subagent_announce.md`
  template (Agent #2).
- **Why**: when chat tier invokes `delegate_summarizer`, the user
  should SEE that delegation happened in the Telegram thread —
  otherwise the message-time jump is unexplained. Channel-scrubber
  removes the child's internal tool calls from the displayed thread
  but keeps the announce + final result visible.
- **Aura change**: small `internal/agent/delegate/announce.md` template
  + a render hook in `internal/channels/telegram/invocation_builder.go`
  + symmetric hook in `cmd/aura/web_chat.go`. ~80 LOC.

---

## 4. Strategic re-ordering — REFLECTION before AGENTDEF

- **Source**: Agent #2 strong recommendation, validated against
  hermes-agent's `background_review.py` pattern.
- **Why**: REFLECTION-POSTTURN (planned for Wave OH3) is currently
  blocked on AGENTDEF (needs a `reflector` archetype). But hermes
  ships REFLECTION as a fork-and-restrict — a post-turn hook that
  invokes the same LLM client with a restricted prompt, NO archetype
  machinery. Aura can ship REFLECTION value TODAY without waiting
  for OH1, then migrate to a proper `reflector` archetype after
  AGENTDEF lands.
- **Aura change**:
  - Pre-OH1: ship REFLECTION-FORK story (~200 LOC) — see synthesis §6
  - Post-OH1: migrate to `reflector` archetype (~30 LOC delta) when
    AGENTDEF is live
- **Sequencing**: REFLECTION-FORK becomes a new pre-OH1 wave
  (Wave-RFL) or absorbed into the existing RAG-PROT slice (closer
  in spirit). My recommendation: separate atomic story shipped
  BEFORE OH1-S0 discuss-phase, ~1 session.

---

## 5. Integration friction with existing swarm — F4+F6 are load-bearing

- **Source**: Agent #4 integration map.
- **Risk**: per-turn synthesis of `delegate_<id>` tool specs straddles
  3 callers — `internal/agent/` (main loop), `internal/channels/telegram/invocation_builder.go:36-80`,
  `cmd/aura/web_chat.go:200-275`. Without extracting a single helper
  upfront, synth logic will duplicate-and-drift (WhatsApp would be the
  third caller when that lands).
- **Aura change**: introduce `agentdef.WithArchetypeDelegates(...)`
  helper BEFORE the first synth call site. Mandatory in OH1-S3.
- **LOC impact**: ~30 LOC for the helper saves ~200 LOC of duplicated
  logic across 3 channel builders.

---

## 6. Real LOC budget — ~1480 net code + ~470 test (vs ~1500 original estimate)

Agent #4's map:
- AGENTDEF (S1) — ~600 LOC code + ~250 LOC tests
- TIER (S2) — ~150 LOC code + ~80 LOC tests
- DELEGATE-TOOL (S3, includes merged DEDUP + cycle detector + helper)
  — ~700 LOC code + ~140 LOC tests
- REFLECTION-FORK (pre-OH1) — ~200 LOC + ~80 LOC tests (separate
  story, not in OH1 budget)

Existing-code reuse saves ~500 LOC vs the naïve estimate:
- `swarm.Assignment` extends with `Archetype` field (NOT replaced)
- `SKILL.md` body reused verbatim under new TOML wrapper
- `DelegationPolicy` stays separate from `AgentDefinition` (operator-
  runtime vs designer-contract are deliberately different shapes)
- `swarm_runs`/`swarm_tasks` SQLite schema needs NO migration
- DEDUP folds into S3 for free

---

## 7. SEPARATE-STORY candidates (catalogued)

| Story ID | Lift | LOC | Source | When |
|---|---|---|---|---|
| **Pre-OH1** | REFLECTION-FORK (hermes-style fork-and-restrict, no archetype machinery) | ~200 | hermes-agent `background_review.py` | Ship NOW |
| Post-OH1 | REFLECTION-MIGRATE (turn fork into `reflector` archetype TOML) | ~30 | derives from R-fork | After OH1-S1 |
| OH1-bonus | Generic `spawn(task)` fallback alongside `delegate_<id>` (catch-all) | ~80 | nanobot `subagent.py` | Optional in OH1-S3 if scope allows |
| OH1-bonus | `max_concurrent_delegates` config (default 1) + 5-phase status enum | ~60 | nanobot | Optional; matters when multi-delegate becomes common |
| Post-OH1 | `parent_session_id` column + write linkage on conversations table | ~50 | codex thread-tree | Migration story after OH1 ships |
| Post-OH1 | Swarm-Skills (semantic skill swarm) lift | ~400 | arXiv 2605.10052 | Roadmap pin, gated on OH3 reflection |
| OH3 | 4 Claude Code prompt lines into AGENT.md | ~0 code | system_prompts_leaks | Ships with OH1-S1 prompt overlay edit |

---

## 8. Open questions for Davide (research-driven)

The 4 agents surfaced ~14 distinct open questions. Consolidating to
the 7 that genuinely need a human call before OH1-S0 discuss-phase:

| # | Question | My recommendation |
|---|---|---|
| 1 | **File format** — TOML (openhuman 1:1) or JSON (Aura ops convention)? | **JSON**. Aura already uses JSON for mcp.json + dashboard settings. No positional footgun. 50-LOC parser swap. |
| 2 | **Tool synth schema** — `{prompt, model?}` (openhuman) or `{prompt}` only? | **`{prompt, model?}`**. Per-call model pin is high UX leverage for 1 line of schema. |
| 3 | **Inherit semantics** — blacklist `omit_*` (openhuman) or whitelist `inherit_*` (online research)? | **Whitelist `inherit_*`**, default `false`. Forward-compatible when new context categories are added later. |
| 4 | **Default tier on missing annotation** — `Worker` (fail-closed) or `Chat` (back-compat)? | **`Worker`**. Force user TOMLs to declare intent; fail-closed at boot. |
| 5 | **DEDUP timing** — folded into OH1-S3 (Agent #1 recommendation) or kept as OH1-S4? | **Folded into S3**. Collision risk is real, regression test in same commit. |
| 6 | **REFLECTION sequencing** — fork-and-restrict pre-OH1 (Agent #2) or wait for OH1 to land then ship `reflector` archetype (original plan)? | **Pre-OH1 fork-and-restrict**, migrate post-OH1. Value lands TODAY without coupling. |
| 7 | **swarm.Manager merge** — wire `delegate_<id>` through existing `swarm.Manager.Run` (Agent #4) or new parallel dispatch path? | **Through existing Manager.Run**. Reuses ~500 LOC, single dispatch surface. Friction: Manager.maxDepth=1 → bump to 3 (behavioral change documented in commit body). |

---

## 9. Updated migration sequence — 9 atomic commits

Per Agent #4's 7-step path + R5 (DEDUP folded into S3) + R8 (announce
template) + the pre-OH1 REFLECTION addition:

```
RFL-S1 (pre-OH1) — REFLECTION-FORK hook                       [~200 LOC, 1 session]
   ↓
OH1-S0 — discuss-phase (no code)                              [INTERACTIVE]
   ↓
A — AGENTDEF registry empty + loader (zero archetypes)        [~400 LOC]
   ↓
B — Migrate summarizer to JSON (or TOML per S0) archetype     [~150 LOC, byte-identical runtime]
   ↓
C — TIER enum added, validator WARN-only                      [~80 LOC]
   ↓
D — TIER enforcement turned on + cycle detector + parent stack [~80 LOC]
   ↓
E — DELEGATE-TOOL synth + DEDUP + over-delegation prefix       [~600 LOC, merges old S3+S4]
   ↓
F — Channel-scrubber + announce template (Telegram + web)     [~80 LOC]
   ↓
G — Migrate REFLECTION-FORK → reflector archetype             [~30 LOC delta]
   ↓
H — Deprecate spawn_aurabot / run_aurabot_swarm in prompts    [~0 LOC code, docs only]
   ↓
I — Remove deprecated tools when telemetry shows zero use      [~50 LOC cleanup]
```

Total atomic commits: 9 (plus RFL-S1 prerequisite). Roughly 3-4
sessions of execution time. Each commit independently revertable.

---

## 10. Master plan delta

Update `docs/aura-graph-tools-plan-2026-05-25.md` §5 (Wave OH1):
- Replace the 4-story decomposition with the 9-commit sequence above
- Add the 6 R-refinements as "in-same-commit" requirements per relevant story
- Update LOC estimate 1500 → ~1480 net (corrected via Agent #4 reuse)
- Add the 7 open-questions table as discuss-phase input for OH1-S0
- Cross-reference this synthesis + the 4 source research docs
- Surface the REFLECTION-FORK pre-OH1 story as a new Wave-RFL section

That's the surgical edit. Master plan stays the source of truth for
execution; this synthesis is the research anchor.
