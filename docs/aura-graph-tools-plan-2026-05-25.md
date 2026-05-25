# Aura — Graph + Tools Plan (2026-05-25)

Executable plan that converts the two pattern-lift audits
(`docs/graphify-pattern-lift-2026-05-25.md`, `docs/openhuman-pattern-lift-2026-05-25.md`)
into an atomic, sequenced story backlog with per-story acceptance criteria,
deep-refactor checklist, test strategy, and Codex/Ralph/interactive
classification.

This plan extends, **not replaces**, the locked roadmap in
`project_post_drift_phase_sequence_locked` (RAG+rerank → ToolSurface →
TokenJuice → AgentLoop). The four hot-fixes shipped earlier today
(commits `13d004d3 → 35348bee`) closed the *symptom* of "fa cagare il
grafo e i tool"; this plan closes the *root cause* and ships the missing
patterns the audits surfaced.

---

## 0. Status snapshot (2026-05-25)

**Shipped today** (4 fixes + 1 docs commit, all green through lefthook):

| SHA | Title | Lines |
|---|---|---|
| `13d004d3` | RRF score normalised to [0,1] | +63/-46 |
| `f877fd85` | god_nodes filter hub/operational/uncategorized | +186/-6 |
| `0bb00697` | search_memory slim format (drop curator dump) | +218/-195 |
| `35348bee` | create_document description teaches call shape | +49/-7 |
| `32ca07cc` | docs: graphify + openhuman pattern lift audits | +645 |

**RAG/search/wiki/registry test suite**: green (16s + 31s + 27s + 40s + 11s).
**Lefthook**: 0 dupl, 0 new lint, file-size cap respected on every commit.

**Not yet started** (this plan covers):

| Surface | State | Driver |
|---|---|---|
| `internal/wiki/{surprise,questions,diff,gaps,cluster,centrality}.go` | ABSENT | This plan |
| `internal/agent/agentdef/` | ABSENT | This plan |
| `internal/agent/governance/{microcompact,context_guard}.go` | ABSENT | This plan |
| `internal/agent/{toolpolicy,promptguard}/` | ABSENT | This plan |
| `internal/learning/{reflection,profile_render}.go` | ABSENT | This plan |
| `skill(action=create)` | ABSENT | This plan |
| `ORCHESTRATOR-PROMPT-DECISION-TREE` (AGENT.md edit) | PARTIAL | This plan |

---

## 1. Wave sequence — 6 waves, ~25 atomic stories

Per `feedback_wiki_is_bedrock` the graph/wiki layer is the bedrock; per
`feedback_aura_as_product` we keep max 2 waves staged at a time. Per
`feedback_per_module_deep_refactor_mandatory` every story includes the
deep-refactor checklist in the SAME commit.

```
WAVE G1 (graph fast wins)          ──┐
                                     │ STAGE NOW
WAVE OH2 (history hygiene)         ──┘

WAVE G2 (graph signal)             ──┐
                                     │ STAGE AFTER G1 ships
WAVE OH1 (multi-agent foundation)  ──┘

WAVE OH3 (memory & safety)         ──┐
                                     │ STAGE AFTER OH1 ships
WAVE G3 (clustering — discuss 1st) ──┘
```

Rationale for order:
- **G1 first**: cheapest, highest-readability, no dependencies. Closes
  the "graph fa cagare" complaint with concrete user-visible answers
  (graph_diff, gaps).
- **OH2 second**: finishes the cleanup the 4 hot-fixes started.
  Microcompact + tool-result-budget trailer + context-guard prevent the
  *next* batch of "agent burns tokens, model can't tell" bugs.
- **G2 third**: surprise score + suggested questions become possible
  once G1's confidence tags exist. Adds the "show me non-obvious links"
  surface.
- **OH1 fourth**: unlocks multi-agent scaffolding (AGENTDEF + TIER +
  DELEGATE-TOOL). Big lift but pure data + a loader → low blast radius.
- **OH3 fifth**: leverages OH1 (REFLECTION posts into archetype-specific
  USER.md via PROFILE-RENDER, CHANNEL-TOOL-POLICY needs AGENTDEF tier
  data).
- **G3 last**: clustering depends on a Louvain port (~400 LOC, needs
  discuss-phase to pick Go vs sidecar). Gates patterns 6/9/12 but they
  are LOW impact at current wiki size (45 pages).

Per-wave LOC + session estimates:

| Wave | Stories | LOC | Sessions | Codex? |
|---|---|---|---|---|
| G1 | 3 | ~250 | 1 | ✅ Codex |
| OH2 | 5 | ~700 | 2 | ✅ Codex |
| G2 | 3 | ~410 | 1-2 | ✅ Codex |
| OH1 | 4 | ~1500 | 3-4 | Mix (discuss + Codex) |
| OH3 | 7 | ~1700 | 3-4 | Mix (interactive prompts + Codex) |
| G3 | 3 | ~1000 | 2 (after discuss-phase) | Discuss → Codex |

Total: ~25 stories, ~5500 LOC, ~12-15 sessions.

---

## 2. Wave G1 — Graph fast wins (~250 LOC, 1 session, Codex)

Source: graphify patterns 4, 5, 7. The "no-dependencies, ship today"
trio. Per `feedback_one_module_per_slice` each ships as its own atomic
commit.

### G1-S1 — Confidence tags on wiki edges (~80 LOC)

- **Why**: precondition for G2-S1 surprise score and G2-S2 suggested_questions.
- **Touches**: `internal/wiki/writer.go`, `internal/wiki/page.go`
  (struct `RelatedRef` add `Confidence` field if absent), `propose_patch`
  flow at `internal/agent/tools/registry/propose_patch.go`.
- **Behavior**: when the LLM writes an edge from a recall hit, attach
  `confidence: EXTRACTED|INFERRED|AMBIGUOUS` based on score: ≥0.75 →
  EXTRACTED, 0.55-0.75 → INFERRED, <0.55 → AMBIGUOUS. User-typed
  `related:` entries default to EXTRACTED.
- **Acceptance**:
  - `wikiSubgraph.go:312` hard-coded `"EXTRACTED"` replaced with
    per-edge lookup.
  - New test: write a recall hit with score 0.42 → page frontmatter shows
    `confidence: AMBIGUOUS`.
  - Existing wiki tests stay green (back-compat: missing tag still reads
    as EXTRACTED).
  - Deep refactor: lint clean on `writer.go` + `propose_patch.go`,
    duplicate-find clean.

### G1-S2 — `search(action=diff)` for "what changed" (~120 LOC)

- **Why**: graphify pattern 4 (HIGH impact, TRIVIAL effort). Aura today
  can't answer "cosa è cambiato dall'ultima sessione" except by reading
  raw `git log`.
- **Touches**: new `internal/wiki/diff.go`, extend
  `internal/agent/tools/registry/search.go` action enum + dispatch,
  new delegate `internal/agent/tools/registry/wiki_diff.go`.
- **Behavior**: load a second GraphIndex from a historical git commit
  (default `HEAD~1`), compute set diff (new/removed nodes/edges),
  return JSON `{new_nodes, removed_nodes, new_edges, removed_edges, summary}`.
- **Acceptance**:
  - Action `diff` added to the `oneOf` block in `search.go::Parameters`.
  - Optional `since` arg: ISO-8601 timestamp OR git ref OR commit hash.
  - 2 tests: round-trip diff between two snapshots (synthetic git
    history); empty diff returns `{summary: "no changes"}`.
  - Deep refactor on `search.go` description: update the action enum
    line to mention `diff`.

### G1-S3 — `search(action=gaps)` for orphan pages (~50 LOC)

- **Why**: graphify pattern 7. Cheapest possible win for "wiki is
  bedrock" health.
- **Touches**: new `internal/wiki/gaps.go`, action wired into
  `search.go`.
- **Behavior**: walk `GraphIndex`, return slugs with `InDegree+OutDegree <= 1`
  excluding operational slugs (via existing `IsOperationalSlug` and
  the `IsAuxiliaryHubNode` predicate from today's commit `f877fd85`).
- **Acceptance**:
  - JSON `{count, slugs[], hint}`.
  - 1 test: diamond graph with 1 island → returns the island.
  - Reuses `IsAuxiliaryHubNode`; deep refactor: lint clean on touched
    files, no LOC drift on `godnodes.go`.

**Wave G1 ship gate**:
- All 3 stories merged, lefthook green per commit.
- Run live probe: ask Aura in Telegram "cosa è cambiato nella wiki
  oggi?" — answer must be substantive, cite ≥1 slug from `search(diff)`.
- Update `MEMORY.md` index with G1 closure entry.

---

## 3. Wave RAG-PROT — RAG-protected compaction slice (~600 LOC, 1 session, Codex)

**Replaces the old "Wave OH2 — History hygiene"** after pushback
2026-05-25 (per `feedback_check_tmp_sources_then_brainstorm_best`):
generic openhuman compaction risks corrupting evidence-class output
(SEED_CONTENT, SEED_SNIPPET, ground-truth bytes). The targeted slice
ships 5 stories — see `docs/aura-rag-compaction-slice-2026-05-25.md`
for the full design (openhuman source citations, Aura leak analysis,
acceptance criteria per story).

Headline: 4 user-stated requirements:
1. Skip LLM-summariser on `search(*)`/`wiki_page(read)`/`source(read)`
   — preserve SEED_CONTENT / SEED_SNIPPET / evidence verbatim
   (RAG-PROT-1).
2. Head-preserving budget on fresh tool results (openhuman trailer
   format) as backstop (RAG-PROT-3).
3. Markdown capsule as primary RAG/graph format — kill JSON wrappers
   that waste 30% of tokens on quoting (RAG-PROT-4).
4. Microcompact only on OLD tool results; the fresh one is by design
   in the keep_recent window (RAG-PROT-5).

Plus one wiring fix: `parent_task_hint` exists in
`payload_summarizer.go:99` but every caller passes `""` (RAG-PROT-2).

Per-story LOC + driver — see the slice doc.

### OH2-S1 — TOOLRESULT-BUDGET trailer (~40 LOC) [TRIVIAL]

- **Why**: openhuman P0 pattern. Aura already truncates but doesn't tell
  the model that there *was* more. Without the trailer the model
  hallucinates over silent cuts.
- **Touches**: `internal/agent/tools/registry/boundoutput.go`,
  `internal/agent/tools/registry/spilloutput.go`.
- **Behavior**: at cut, append
  `[… N bytes truncated by tool_result_budget — re-run with a narrower query …]`
  at a UTF-8 char boundary, reserving 256 bytes for the trailer so the
  marker itself is never truncated.
- **Acceptance**:
  - 3 tests: pass-through (under cap), exact-boundary cut, multibyte
    char near boundary (must not split a rune).
  - Existing `truncateForToolContext` users in `memory_search_format.go`
    benefit automatically.

### OH2-S2 — MICROCOMPACT placeholder substitution (~150 LOC)

- **Why**: openhuman P0. On heavy turns (10+ tool calls) Aura's history
  grows linearly with tool output; this caps it without paying for an
  LLM summarization round.
- **Touches**: new `internal/agent/governance/microcompact.go`, wired
  into the per-iteration check in `internal/agent/loop.go`.
- **Behavior**: when iteration ≥ N (default 6) AND token-usage >
  threshold (default 70% context), walk history backwards keeping the
  K most-recent (default 3) `role:tool` results intact and replacing
  body of older ones with `"[old tool result content cleared — re-run if needed]"`.
  Envelope stays so the `assistant_with_tool_calls ↔ tool_results`
  invariant holds and providers don't 400.
- **Acceptance**:
  - 5 tests: idempotent (second pass = no-op), K-recent untouched,
    envelope shape preserved, tool_call_id pairing intact, no-op below
    threshold.
  - Live probe: a 12-tool-call session has total token-in lower on
    iteration 8 than on iteration 6.

### OH2-S3 — CONTEXT-GUARD pre-call utilization check (~200 LOC)

- **Why**: openhuman P1. Today an over-budget call hits the provider,
  gets a 400, loop wastes a round. Pre-call guard avoids it.
- **Touches**: new `internal/agent/governance/context_guard.go`,
  called at the top of each iteration in `loop.go`.
- **Behavior**: tracks last `{input_tokens, output_tokens, context_window}`
  reported by provider. Before each call, estimate cost (history +
  draft tools + system overhead). Returns `{ok | compaction_needed |
  exhausted}`. Loop reacts: continue / run MICROCOMPACT / hard-stop
  with structured error.
- **Acceptance**:
  - 6 tests: under 0.90 → ok, between 0.90-0.95 → compaction_needed,
    over 0.95 → exhausted, circuit breaker after N consecutive
    compaction failures, model-context-window default fallback,
    structured error has a `reason` string.
  - Integration test: stub a 4K-context model + 5K history → first
    iteration triggers compaction.

### OH2-S4 — PAYLOAD-CONTRACT parent-task hint (~80 LOC)

- **Why**: openhuman P1 PARTIAL. Today summarizer is "generic", drops
  the field the user actually wanted. With parent-task hint the
  summary biases toward keeping what answers the task.
- **Touches**: `internal/agent/agents/summarizer/prompt.go`,
  `internal/agent/governance/payload_summarizer.go::MaybeSummarize`.
- **Behavior**: add `parentTaskHint string` parameter; if non-empty,
  inject into summarizer prompt as `Focus: keep verbatim any facts that
  answer "<hint>"`. Rewrite prompt to explicitly enumerate preserve-
  verbatim list (entity IDs, URLs, dates, numbers, paths, error codes).
- **Acceptance**:
  - 3 tests: hint propagates to prompt; failure modes (LLM error,
    empty output, output > input) all fall through to raw-truncation
    path; prompt contains the preserve-verbatim enumeration.
  - Live probe: summarize a 50KB MCP result with hint "give me the
    user emails" — emails preserved verbatim.

### OH2-S5 — TOKEN-BUDGET-PRE-DISPATCH history trim (~150 LOC)

- **Why**: openhuman P2 conditional → becomes HIGH the moment an MCP
  tool stuffs 80KB into a single tool result. 50-message-cap is no
  longer a useful proxy.
- **Touches**: `internal/agent/loop.go` (replaces / augments the existing
  50-message cap with a token-aware trim).
- **Behavior**: before each LLM call, estimate token cost (history +
  system + tool schemas + output reserve). If over `context_window -
  reserve`, drop messages from oldest non-system position until fit.
  Estimator: `len(text) / 4`. Reserve: 4K default, configurable.
- **Acceptance**:
  - 4 tests: under-budget → all messages kept; oldest-first eviction;
    system message preserved; tool_call/tool_result pairs evicted
    together (no orphan tool_use).
  - The existing `ScrubOrphanToolCalls` invariant from
    `internal/agent/governance/` still holds after a trim.

**Wave OH2 ship gate**:
- All 5 stories merged.
- Live probe (the one that originally triggered "fa cagare i tool"):
  ask "fammi un riepilogo PDF di quello che sai su di me". Expected:
  ≤4 tool calls (was 11), zero `truncated` markers without trailer,
  total wall-clock ≤30s (was 62s).

---

## 4. Wave G2 — Graph signal (~410 LOC, 1-2 sessions, Codex)

Source: graphify patterns 1, 2, 8. Requires G1-S1 confidence tags shipped.

### G2-S1 — `search(action=surprises)` (~150 LOC)

- **Why**: graphify pattern 1 (HIGH/SMALL). graphify users report this
  as the #1 reason they come back to the report. Aura today has no
  surface for "non-obvious connections".
- **Touches**: new `internal/wiki/surprise.go`, action wired into
  `search.go`.
- **Behavior**: walk edges, score each `(src, tgt)` pair on:
  (a) confidence weight (AMBIGUOUS=3, INFERRED=2, EXTRACTED=1 — needs
  G1-S1); (b) cross-`NodeMeta.Category` bonus; (c) peripheral→hub via
  `Degree()` (min≤2, max≥5); dedupe by pair, return top-K with one-line
  `why`.
- **Acceptance**:
  - Skips operational + auxiliary-hub via existing
    `IsAuxiliaryHubNode`.
  - 4 tests: confidence weighting, category-crossing bonus,
    peripheral-to-hub bonus, dedup-by-pair guarantee.
  - JSON shape: `[{src_slug, tgt_slug, score, confidence, why}]`.

### G2-S2 — `search(action=suggest_questions)` (~180 LOC)

- **Why**: graphify pattern 2 (HIGH/SMALL). Turns cold-start prompt into
  concrete questions tied to specific slugs.
- **Touches**: new `internal/wiki/questions.go`, action wired into
  `search.go`.
- **Behavior**: produce 4-5 questions across types: (1) AMBIGUOUS edges
  (needs G1-S1) → "what's the exact link between [[A]] and [[B]]?";
  (2) bridge nodes (uses existing `P99Degree`) → "why does X connect
  multiple categories?"; (3) god_nodes with many sources → "are the N
  links from X still accurate?"; (4) isolated nodes (uses G1-S3 logic)
  → "what connects A, B, C to the rest?".
- **Acceptance**:
  - 4 tests, one per type, on synthetic graphs.
  - JSON shape: `[{type, question, why, related_slugs[]}]`.
  - Top-K cap at 7 with deterministic ordering.

### G2-S3 — IDF exact-match boost in RRF (~80 LOC)

- **Why**: graphify pattern 8 (MEDIUM/SMALL). Fixes "user types slug
  literally, slug is at position 3" bug. RRF treats exact match as
  just-another-signal; this boosts it to always-top.
- **Touches**: `internal/storage/search/search.go` (`mergeHybridResults`
  + a new pre-search exact-slug check).
- **Behavior**: pre-search, compute `slug == normalized_query` (and
  prefix match). If hit, inject as `ScoreExact` group with weight that
  always tops fused score. IDF cache keyed on GraphIndex generation
  count; invalidate on `RefreshPage`/`RemoveNode`.
- **Acceptance**:
  - Test: query `davide-marchetto` returns slug `davide-marchetto` at
    position 1 even when 3 other pages mention "davide" in body.
  - IDF cache invalidation test (add page → cache miss → recompute).
  - Existing `TestMergeHybridResults*` tests stay green.

**Wave G2 ship gate**:
- Live probe: at session start ask "che domande mi consigli partendo
  dalla wiki?" — answer must be 4-5 concrete questions citing specific
  slugs.
- Update `MEMORY.md` index with G2 closure entry.

---

## 5. Wave OH1 — Multi-agent foundation (~1480 LOC, 3-4 sessions)

Source: openhuman AGENTDEF + TIER + DELEGATE-TOOL + DEDUP. **Must
discuss-phase first** — this is architectural.

> **Refined 2026-05-25 after 4-agent research.** See
> `docs/aura-oh1-research-synthesis-2026-05-25.md` for the full
> synthesis. Headline changes: tool synth schema is
> `{prompt, model?}` not `{task}`; defaults flipped to `Inherit*=false`
> + `Tier=Worker` (fail-closed whitelist); DEDUP folds into S3; cycle
> detector added; channel-scrubber announce template lifted from
> nanobot; REFLECTION-FORK ships PRE-OH1 (Wave-RFL) to unblock OH3.
> 7 open questions for discuss-phase consolidated in synthesis §8.

Research anchors:
- `docs/aura-oh1-research-synthesis-2026-05-25.md` (this wave's master)
- `docs/research-openhuman-oh1-deep-2026-05-25.md` (field-level audit)
- `docs/research-tmp-other-multi-agent-2026-05-25.md` (codex/nanobot/hermes)
- `docs/research-online-multi-agent-2026-05-25.md` (Anthropic/LangGraph/CrewAI)
- `docs/research-aura-swarm-integration-map-2026-05-25.md` (existing 1900 LOC map)

### Wave-RFL — REFLECTION-FORK (PRE-OH1, ~200 LOC, 1 session, Codex)

Ships hermes-agent-style fork-and-restrict post-turn hook BEFORE
AGENTDEF lands. Unblocks OH3 reflection value without coupling to the
archetype machinery. Migrates to a proper `reflector` archetype in
OH1-G (commit after AGENTDEF is live).

- **Touches**: new `internal/learning/reflection_fork.go` invoked from
  the existing `posthook.go` scaffold.
- **Behavior**: after qualifying turns, restricted LLM call extracts
  `{observations[], patterns[], user_preferences[], user_reflections[]}`
  as structured JSON. Per-session counter throttles cost.
- **Acceptance**: 5 tests + 1 integration. Cost throttle pinned.

### OH1-S0 — discuss-phase (no code) [INTERACTIVE]

Run `/gsd-discuss-phase` on the 7 questions from synthesis §8:
1. File format JSON (Aura ops convention) vs TOML (openhuman 1:1)?
2. Tool synth schema `{prompt, model?}` or `{prompt}` only?
3. Inherit semantics: blacklist `omit_*` or whitelist `inherit_*`?
4. Default tier on missing annotation: `Worker` or `Chat`?
5. DEDUP folded into S3 or kept as S4?
6. REFLECTION pre-OH1 fork or wait for `reflector` archetype?
7. Merge `delegate_<id>` into existing `swarm.Manager.Run` or parallel
   dispatch path?

Plus considered-rejected alternatives to record in DISCUSS.md so they
don't get re-litigated: codex config-overlay model, AOrchestra dynamic
`<Instruction, Context, Tools, Model>` tuple.

### OH1-A — AGENTDEF registry empty + loader (~400 LOC) [Codex]

- **Touches**: new `internal/agent/agentdef/{definition.go,loader.go,
  registry.go,validator.go}`. Loader supports the discuss-phase-chosen
  format (JSON default, TOML if S0 reopens).
- **Behavior**: ZERO archetypes registered, zero behavioural change.
  Boot picks up empty registry without breaking the loop. Pure-data
  +loader; unit-tested in isolation.
- **Acceptance**: 8 tests covering parse round-trip, malformed-input
  rejection with line+col, user override beats built-in, slug
  collision rejected, all `Inherit*` defaults `false`, `Tier` default
  `Worker`, `subagents[]` deserialises, MaxInputTokens/MaxOutputTokens
  honoured.
- **Schema refinements absorbed**: R2 (fail-closed whitelist defaults),
  R3 (`max_input_tokens`/`max_output_tokens`), R4 (file format from
  discuss).

### OH1-B — Migrate summarizer to AGENTDEF (~150 LOC) [Codex]

- **Touches**: new `internal/agent/agentdef/builtin/summarizer/
  {agent.json,prompt.md}` (or `.toml` per discuss). Existing
  `internal/agent/agents/summarizer/prompt.go` becomes a stub
  re-exporting from registry.
- **Behavior**: BYTE-IDENTICAL runtime to today. The summarizer's
  existing `Inherit*` settings must be explicitly `true` for sections
  it actually needs (because the new default is `false`).
- **Acceptance**: existing summarizer tests stay green; new test
  asserts byte-identical prompt rendering between old hardcoded path
  and new TOML/JSON path.

### OH1-C — TIER enum + validator WARN-only (~80 LOC) [Codex]

- **Touches**: `internal/agent/agentdef/validator.go` runs the tier-
  hierarchy check + the cycle detector (parent-id stack from R6) but
  only LOGS warnings; no rejection yet.
- **Behavior**: same-tier delegation, missing tier, cycles all log a
  warning during boot + at delegate-invocation time.
- **Acceptance**: 5 tests cover each violation class; assert log
  emission, assert delegation still proceeds.

### OH1-D — TIER enforcement turned on (~80 LOC) [Codex]

- **Touches**: same validator file as C, flip warnings to errors.
- **Behavior**: same-tier delegation, cycles, missing-tier-on-user-
  TOML all REJECT at boot or runtime.
- **Acceptance**: 5 tests assert each rejection path. Existing
  summarizer + any registered archetype must pass without changes
  (proves the migration in B left the registry valid).

### OH1-E — DELEGATE-TOOL synth + DEDUP + over-delegation prefix (~600 LOC) [Codex]

The largest single commit. Absorbs former OH1-S4 (DEDUP) per R5,
plus over-delegation prefix per R7, plus the `agentdef.WithArchetypeDelegates(...)`
helper per Agent #4 friction-point F4+F6.

- **Touches**:
  - new `internal/agent/agentdef/delegate.go` (synth function + helper)
  - `internal/agent/tools/manifest.go` (dedup guard at emit time)
  - `internal/channels/telegram/invocation_builder.go:36-80` (call
    `WithArchetypeDelegates` once)
  - `cmd/aura/web_chat.go:200-275` (same)
  - hook into `swarm.Manager.Run` per Q7 if discuss confirms merge
- **Behavior**: per turn, for each entry in active archetype's
  `subagents[]`, synthesise one tool. Schema is
  `{"prompt": string, "model"?: string}` per R1. Description
  prepended with hardcoded prefix from R7
  ("Use only when direct response/direct tools are insufficient. ").
  On invoke: sub-loop with target's prompt + filtered tools + per-
  archetype `max_iterations` + `max_input_tokens`/`max_output_tokens`
  + `max_result_chars`. Parent never sees child's tool calls.
- **Acceptance**: 8 tests: synth name, sub-loop execution, parent
  doesn't see child trace, dedup against action tool name collision,
  cycle detection at invoke time, prefix on description, prompt+model
  schema, max_input_tokens honoured.
- **Live probe**: chat tier delegates to summarizer worker, response
  returns within sub-loop's max-iter, telemetry shows correct
  `parent_session_id` (R from Agent #2).

### OH1-F — Channel-scrubber + announce template (~80 LOC) [Codex]

- **Touches**: new `internal/agent/delegate/announce.md` template +
  render hooks in `internal/channels/telegram/invocation_builder.go`
  and `cmd/aura/web_chat.go`.
- **Behavior**: when chat tier invokes `delegate_<id>`, render the
  announce template into the user-facing thread, run the child sub-
  loop, then surface ONLY the child's final result. Internal tool
  calls scrubbed.
- **Acceptance**: 3 tests — Telegram render, web render, scrubbing
  invariant (no child tool_use leaks).

### OH1-G — Migrate REFLECTION-FORK → `reflector` archetype (~30 LOC delta) [Codex]

- **Touches**: replaces `internal/learning/reflection_fork.go` body
  with `agentdef.WithArchetypeDelegates(...).Invoke("reflector", ...)`.
  Adds `internal/agent/agentdef/builtin/reflector/{agent.json,prompt.md}`.
- **Behavior**: same observable behaviour as Wave-RFL, now driven by
  archetype machinery.
- **Acceptance**: byte-identical extraction output for the same input
  turn; existing tests stay green.

### OH1-H — Deprecate `spawn_aurabot` / `run_aurabot_swarm` in prompts (~0 LOC code) [INTERACTIVE]

- Edit `runtime-workspace/AGENT.md` + `TOOLS.md` to remove references
  to the old swarm tools; recommend the new `delegate_<id>` surface.
  Old tools remain callable for back-compat until OH1-I.

### OH1-I — Remove deprecated tools (~50 LOC cleanup) [Codex, after telemetry]

- Drop `SpawnAuraBotTool` + `RunAuraBotSwarmTool` from registry.
- Trigger: telemetry confirms zero invocations across N days.

**Wave OH1 ship gate** (post OH1-G):
- Live probe in Telegram: chat tier asks Aura to summarise a long
  source → Aura invokes `delegate_summarizer(prompt=..., model?=...)`
  → response back with announce + final-only render → child trace
  not visible in thread.
- No `400 duplicate tool name` ever (DEDUP regression test holds).
- `parent_session_id` populated on every child run in
  `conversations` table.
- Update `MEMORY.md` index + `project_aura_dgx_spark_bundle_vision`
  context (multi-agent is now real).

### Real LOC after reuse (per Agent #4 map)

Total: **~1480 net code + ~470 test LOC** (was ~1500 estimate).
Savings: ~500 LOC vs naïve build because `swarm.Assignment` extends
with one field instead of getting replaced, SKILL.md body reused
verbatim, no SQLite migration, DEDUP folds into S3 for free.

---

## 6. Wave OH3 — Memory & safety (~1700 LOC, 3-4 sessions)

Source: openhuman REFLECTION-POSTTURN + PROFILE-RENDER +
PROMPT-INJECTION-GUARD + CHANNEL-TOOL-POLICY + STOP-HOOKS +
SKILL-CREATE + ORCHESTRATOR-PROMPT-DECISION-TREE.

### OH3-S1 — REFLECTION-POSTTURN hook (~500 LOC) [Codex]

- **Touches**: new `internal/learning/reflection.go`, wired into the
  existing `internal/agent/posthook.go` scaffold, persistence via
  `lessons` table (or new `learning_observations`).
- **Behavior**: after qualifying turns, build a small LLM prompt that
  emits `{observations[], patterns[], user_preferences[], user_reflections[]}`.
  Per-session counter throttles cost (cap 5/session). Stored in long-
  term memory namespaces.
- **Acceptance**: 6 tests + 1 integration (post-turn → row written).

### OH3-S2 — PROFILE-RENDER scheduled regen of USER.md (~400 LOC) [Codex]

- **Touches**: new `internal/learning/profile_render.go`, scheduled
  via existing `scheduler` package, atomic write to
  `runtime-workspace/USER.md`.
- **Behavior**: pulls last N user-prefs + reflections, renders markdown
  brief via LLM, atomic write. Per-archetype `omit_profile` (from
  AGENTDEF) controls injection — worker tier doesn't get the profile.
- **Acceptance**: 5 tests + scheduler integration test.

### OH3-S3 — PROMPT-INJECTION-GUARD heuristic detector (~250 LOC) [Codex]

- **Touches**: new `internal/agent/promptguard/detector.go`, invoked by
  `boundoutput.go` + `web.go` + source ingest before content enters
  history.
- **Behavior**: regex-with-score pipeline, threshold-based verdict
  `{allow|block|review}`. `review` wraps content in stronger
  untrusted-marker; `block` rejects entry. Logs to `/api/insights`.
- **Acceptance**: 8 tests (each rule + threshold combinations) + 1
  integration covering a known-injection PDF input.

### OH3-S4 — CHANNEL-TOOL-POLICY engine (~300 LOC) [Codex]

- **Touches**: new `internal/agent/toolpolicy/engine.go`, settings
  surface in `internal/api/settings.go`, manifest consumer hook.
- **Behavior**: per-tool `permission_level` (read/write/exec/admin) +
  per-channel `allowed_permission`. Session-build emits policy snapshot:
  block above, hide above-with-flag, allow below. Default empty channel-
  perms preserves today's behaviour.
- **Acceptance**: 5 tests; settings UI shows per-channel matrix.

### OH3-S5 — STOP-HOOKS interface refactor (~200 LOC) [Codex]

- **Touches**: refactor existing budget checks in `loop.go` into
  `internal/agent/stop_hooks.go` as default-installed hooks; add
  `WithStopHooks(ctx, hooks)` API.
- **Behavior**: `StopHook` interface (`Name()`, `Check()`). Built-ins:
  budget cap, max-iter ad-hoc, rate-limit. Per-call override doesn't
  mutate persistent config.
- **Acceptance**: 4 tests; existing budget tests pass against refactored
  surface.

### OH3-S6 — `skill(action=create)` (~150 LOC) [TRIVIAL, Codex]

- **Touches**: extend `internal/agent/tools/registry/skill.go`.
- **Behavior**: slugify name → canonicalise path (reject `..` and abs)
  → reject overwrite → write SKILL.md + create empty `scripts/`,
  `references/`, `assets/` → re-discover.
- **Acceptance**: 5 tests including path-traversal attempt.

### OH3-S7 — ORCHESTRATOR-PROMPT-DECISION-TREE in AGENT.md (~0 LOC code) [INTERACTIVE]

- **Touches**: `runtime-workspace/AGENT.md`, `runtime-workspace/TOOLS.md`.
- **Behavior**: rewrite routing section as numbered decision tree
  (1→6 branches), each branch names the action-enum tool that resolves
  it.
- **Acceptance**: re-run the existing bench matrix
  (`docs/aura-quality-snapshot.md`) — strict-pass count should improve
  or stay same; no regressions.

**Wave OH3 ship gate**:
- Bench matrix: strict-pass count ≥ baseline before OH3.
- Update `MEMORY.md` + `feedback_aura_as_product` annotation.

---

## 7. Wave G3 — Clustering (~300 LOC, 1 session, Codex)

Source: graphify pattern 3 (+ 6, 9, 12 dependent).

> **2026-05-25 update**: the Aura container already has Python (used by
> markitdown / whisper / piper sidecars). The Go-port-vs-Python-sidecar
> discuss-phase is therefore moot — we exec Python in-container with
> `networkx` + `python-louvain` (or `leidenalg` if BLAS is available).
> Scope drops from ~1000 LOC + new container to ~300 LOC adapter, 1
> session, no discuss-phase needed. The original three options are
> documented in `docs/aura-graph-tools-plan-2026-05-25.md` git history
> for posterity.

### G3-S1 — Python-backed clustering adapter (~200 LOC) [Codex]

- **Touches**: new `internal/wiki/cluster.go` (Go side) +
  `internal/wiki/cluster_python.py` (trusted in-container Python
  script invoked via `os/exec`). The script reads the graph as JSON
  on stdin (slugs + edges from `GraphIndex.Snapshot()`) and writes
  `{cluster_id: [slugs]}` on stdout.
- **Python deps**: `networkx` (already pulled in by markitdown
  transitively — verify in `Dockerfile`) + `python-louvain` (one
  `pip install` line). No Leiden unless we discover networkx's
  modularity layer is too weak — bench first.
- **Behavior**: serialise `GraphIndex` to JSON (~5KB at 45 pages,
  scales linearly), exec `python3 cluster.py`, parse stdout. Cache
  result keyed on `GraphIndex` content hash so re-clustering is
  free until the wiki changes. LLM-generated cluster labels stored
  separately in SQLite, also content-hash keyed.
- **Aura target surface**: `search(action="clusters")` returning
  `[{id, label, cohesion, size, top_slugs[]}]`. Filter out
  `IsAuxiliaryHubNode` slugs before clustering (consistent with
  god_nodes filter from commit `f877fd85`).
- **Acceptance**: 5 tests + 1 integration. Bench: clustering 45
  pages < 200ms, 500 pages < 2s.

### G3-S2 — Cross-community bonus in surprise score (~30 LOC)

- Re-enable signal (c) in G2-S1 deferred until clusters exist.

### G3-S3 — Low-cohesion bucket in suggest_questions (~50 LOC)

- Re-enable bucket (5) in G2-S2.

**Wave G3 ship gate**:
- Visible UX win: "mostrami le aree tematiche della mia wiki" returns
  4-6 meaningful clusters with labels.
- Cohesion score visible per cluster.
- Update `MEMORY.md` index.

---

## 8. Per-story deep-refactor checklist (NON-NEGOTIABLE)

Per `feedback_per_module_deep_refactor_mandatory` every story commit
must include the following in the SAME commit (not a separate cleanup
phase):

1. **Dead code removed** — `grep` callers in package + consumers; if
   zero, delete.
2. **Duplicates folded** — `dupl -t 60 <touched files>` shows zero new
   cluster.
3. **Legacy patterns translated** — no "TODO migrate later" left
   behind.
4. **Comments updated** — orphan docstrings on removed code deleted,
   stale references fixed.
5. **File size ≤600 LOC** — split with focused responsibilities if
   the touched file grew past.
6. **Tests aligned** — tests of removed code DELETED; tests of changed
   behaviour UPDATED to assert the new contract (never modified to
   pass).
7. **`~/go/bin/golangci-lint run <touched files>` clean** — no new
   warnings vs HEAD.
8. **Commit body lists**: files refactored + LOC delta (before → after)
   + dead code removed (function names + reason) + tests
   updated/removed.

The 4 hot-fixes shipped today (`13d004d3`, `f877fd85`, `0bb00697`,
`35348bee`) all followed this checklist — the commit bodies are the
template for the rest of this plan.

---

## 9. Validation gates (when to ship vs. hold)

After each wave's ship gate fires, before staging the next wave:

1. **Lefthook clean per commit** in the wave.
2. **`go test ./...` green** across the whole module.
3. **Live probe in Telegram** of the wave's headline behaviour
   (criterion stated per-wave above).
4. **`docs/aura-quality-snapshot.md`** updated with the new strict-pass
   counts (per `feedback_aura_as_product` — every wave gate is on
   numbers, not vibes).
5. **`MEMORY.md` index** updated with the wave closure entry.
6. **Codex parallel-session pattern** (per
   `feedback_codex_parallel_session_pattern`): if Codex was driving,
   review the mega commit + amend if needed.

Hold-condition triggers (don't stage next wave):
- Any test flakes ≥ 2/10 runs.
- Strict-pass count decreases vs prior wave baseline.
- User reports "fa cagare" on the surface that just shipped.
- File-size cap or dupl cap regression in main branch.

---

## 10. Risks + mitigations

| Risk | Mitigation |
|---|---|
| OH1 AGENTDEF blast radius — adding archetype config could break the existing single-agent loop | Discuss-phase OH1-S0 mandatory. Built-ins ship empty (1 archetype) and the loop is back-compat from day one. Each OH1 story is independently revertable. |
| G3 Louvain port quality unknown vs graphify's Leiden | Discuss-phase G3-S0 mandatory; bench against a hand-labelled clustering before merging. If quality is visibly worse, fall back to sidecar path. |
| OH3 REFLECTION cost — auto-LLM call per turn | Per-session counter throttle (default 5), opt-out env var, dry-run mode for first 50 turns. |
| MCP server count explosion making tool-filter-fuzzy critical | Tracked as TOOL-FILTER-FUZZY (P0 in openhuman lift, deferred from this plan); ship reactively when a 50+ tool MCP lands. |
| Codex drift on multi-story queues per `feedback_codex_over_ralph` | Default to Codex for ≤2-story commit batches; switch to interactive for ≥3-story Codex sessions. Always commit per concept, never mega-PR. |
| User parallel work colliding with hot-fix branches | Use `git stash --keep-index` pattern (proved out earlier today during Fix #4 commit) to isolate. |

---

## 11. Scheduling guidance

Per `feedback_codex_over_ralph` Codex is default for implementation;
Ralph reserved for bootstrap/cleanup batches. Per
`feedback_one_module_per_slice` prefer 1 story = 1 iter = 1 commit
with stop+verify between.

| Wave | Story | Driver | Rationale |
|---|---|---|---|
| G1 | S1, S2, S3 | Codex | Trivial-to-small atomic stories |
| OH2 | S1 | Codex | TRIVIAL, ~40 LOC |
| OH2 | S2, S3, S4, S5 | Codex | Small, well-bounded |
| G2 | S1, S2, S3 | Codex | Small, atomic |
| OH1 | S0 | Interactive | Architecture decision |
| OH1 | S1 | Codex | Medium, but pure data + loader |
| OH1 | S2, S4 | Codex | Small / TRIVIAL |
| OH1 | S3 | Codex with manual review | Medium, blast-radius into manifest builder |
| OH3 | S1, S2, S3, S4, S5, S6 | Codex | All small-to-medium |
| OH3 | S7 | Interactive | Prompt edit, requires bench replay |
| G3 | S0 | Interactive | Architecture decision |
| G3 | S1 | Codex if (a) / pause if (b) | Depends on S0 outcome |
| G3 | S2, S3 | Codex | Trivial follow-ups |

No story is sized for Ralph autonomous in this plan. Ralph is reserved
for: failure recovery loops, batch rename/sed operations, or any single
slice >1h wall-clock. None of the above fits.

---

## 12. Cross-references

- Locked roadmap (do not deviate without explicit user OK):
  `project_post_drift_phase_sequence_locked` memory.
- Source audits this plan derives from:
  - `docs/graphify-pattern-lift-2026-05-25.md`
  - `docs/openhuman-pattern-lift-2026-05-25.md`
- Quality dashboard the wave gates write to:
  `docs/aura-quality-snapshot.md` (per `feedback_aura_as_product`).
- Phase-WIKI-Clean (separate concern, not absorbed by this plan):
  `project_phase_wiki_clean_planned` memory — triggers on golangci-lint
  >5 findings OR dupl >3 cluster OR file >600 LOC. Run between G1 and
  G2 if any trigger fires.

---

## 13. First action

Stage Wave G1 PRD only (per `feedback_aura_as_product` max-2-staged
rule):

```bash
# 1. Create the phase directory + PRD
mkdir -p .planning/phases/wave-g1
# 2. Discuss-phase optional for G1 (stories are small + independent);
#    skip and go direct to plan-phase via /gsd-plan-phase
# 3. Write prd-wave-g1-staged.json with the 3 stories + acceptance
#    criteria above
# 4. Either run /gsd-execute-phase OR drive 3 atomic Codex commits
#    against the prd manually
```

Then once G1 ships, stage OH2 PRD (the natural pair to G1 — finishes
the today's hot-fix cleanup). Hold everything else until G1+OH2 are
both green.
