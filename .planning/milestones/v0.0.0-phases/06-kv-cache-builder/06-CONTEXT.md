# Phase 6: KV Cache Builder - Context

**Gathered:** 2026-06-02
**Status:** Ready for planning

<domain>
## Phase Boundary

Lock in the **KV-cache discipline** — Layer L0 of the context-rot mitigation policy. Concretely this phase delivers:

1. A named `PromptBuilder` that owns prompt construction, guaranteeing `messages[0]` (system + alphabetically-sorted tool manifest) is **byte-identical turn-on-turn**, with a clean seam for the future `messages[1]` (Agent.md, Slice 10) and `messages[2]` (AgentInsight, Slice 11e) stability tiers.
2. Provider-aware `cache_control` injection (Anthropic `ephemeral` on system+tools — a no-op seam under the current OpenRouter provider but built now per PRD OQ3).
3. Per-turn cache-hit measurement persisted to Postgres, surfaced via `aura cache-stats --since=<window>`.
4. The cross-slice CI gate `scripts/cache_invariant_audit.sh` (amendment #16, Pitfall #3 P0) that gates every subsequent merge against prefix poisoning.

**Maps to:** PRD Slice 4 (CAP-03 "KV cache builder stable-prefix + provider-aware"). Note: ROADMAP labels the requirement CAP-04; PROJECT.md labels it CAP-03 — same slice, a numbering drift to reconcile during planning.

**NOT in scope:** `messages[1]`/`messages[2]` *content* (those ship with Slices 10 / 11e — this phase only builds the empty, forward-compat seam); runtime provider selection (Slice 13 `LLMRouter`); any change to the agentic loop semantics.
</domain>

<decisions>
## Implementation Decisions

### PromptBuilder shape
- **D-01:** **Extract a named `PromptBuilder` type** rather than leaving construction inline. The byte-stable `messages[0]` invariant *already holds* today (frozen `SystemPrompt` constant in `internal/agent/prompt.go`, alphabetical manifest in `tools/manifest.go:39`, read-only `Request.Messages`), but extraction gives a single chokepoint that the CI gate hooks and a clean seam for the future `messages[1]`/`messages[2]` tiers. The move MUST preserve the existing byte-identity invariant.
- **D-01a (planner constraint):** PRD targeted `internal/llm/prompt.go`, but the system prompt + tool manifest live in `internal/agent` / `internal/agent/tools`, and `internal/llm` **cannot import `internal/agent/tools` without an import cycle**. Therefore `PromptBuilder` almost certainly belongs in **`internal/agent`** (or a new `internal/agent/prompt` subpackage), NOT `internal/llm`. Planner must resolve the exact location against the dependency graph and record any deviation from the PRD file-targets as a PRD-amendment.

### Cache-stats storage
- **D-02:** **Persist per-turn metrics to Postgres** (new `aura.cache_metrics` table: turn, conv_id, ts, prompt_tokens, cached_tokens, cost). This makes the roadmap's `aura cache-stats --since=1h` a real time-windowed query. **Overrides PRD Slice 4 OQ2** ("in-memory only, stats are debug") — requires a **PRD-amendment commit before implementation** (PRD-first principle). New migration lands with this slice (next in the 0007+ sequence — confirm numbering against `prd.md §Persistence "Migration numbering"`).
- **D-02a:** Source data is *already shipped* — `llm.Usage{PromptTokens, CompletionTokens, CachedTokens, Cost}` is populated by the OpenAI-compat client (`prompt_tokens_details.cached_tokens` + OpenRouter `usage.cost`). The Tracker consumes the existing trailing `Usage` chunk; no wire-layer parsing work remains.

### Anthropic cache_control seam
- **D-03:** **Build the no-op provider-aware seam now** (`cache_anthropic.go` injecting `cache_control: {"type":"ephemeral"}` on system + tools; add a `ToolsCacheControl` field to `llm.Request` per PRD OQ3). Aligns with the PRD's already-"Proposto: SÌ" answer — **no PRD-amendment needed**. It is a pure no-op under the OpenRouter provider; the value is the provider-branch existing now so Slice 13's `LLMRouter` activates it when an Anthropic-direct target appears.
- **D-03a:** This breaks the current `internal/llm/client.go` design comment "the wire layer is unaware [of caching]" — update that comment in the same commit (DEEP REFACTOR ON TOUCH). The injection logic lives in the `PromptBuilder`/provider layer, not the raw `Stream` wire path.

### CI gate (`cache_invariant_audit.sh`)
- **D-04:** **Runtime-faithful gate, not synthetic.** The gate drives a real **20-turn `runner.Turn` loop against a deterministic stub LLM** and asserts `SHA-256(messages[0])` is constant across all 20 turns. Rationale: `messages[0]` is constant *by construction*, so a synthetic `PromptBuilder.Build()` hash is trivially green and catches nothing. The gate's actual job (amendment #16) is catching a *future* slice (1.8 microcompact, 7e, 10, 11e) that mutates the assembled prefix at runtime — only a real-loop replay exercises that path.
- **D-05:** **Stub LLM = extend `internal/agent/agenttest.FakeClient`** (`fakeclient.go` — an *importable* non-test package implementing `llm.Client`). It already **captures every request in a `Requests` field**, so the audit reads `FakeClient.Requests[n].Messages[0]` directly — no need to reach into runner internals. **Explicitly NOT** `cmd/aura/cmdfakes_test.go` (that is `package main` test-only and cannot be imported by a shipped subcommand).
- **D-06:** Operator entrypoint = a **hidden `aura cache-audit` subcommand** (mirrors the existing `agent.go`/`db.go`/`neo4j.go` subcommand pattern in `cmd/aura/`) that runs the 20-turn replay and prints the per-turn SHA-256 to stdout. `scripts/cache_invariant_audit.sh` is a thin wrapper that invokes it and `diff`s the hashes — satisfying roadmap SC#1 ("printed to stdout, asserted by the script") and SC#5 (CI fails with "messages[0] mutated at <site>"), while operators get a real tool. CI-wires into `.github/workflows/ci.yml` from this phase onward.
- **D-06a:** Fixtures = `scripts/fixtures/cache_invariant/turn-{01..20}.json` (growing-history replay turns). The audit must assert the hash covers *only* `messages[0]` today; design the hash function to accept an index set so amendment #11 can extend it to `[0],[1],[2]` once Slices 10/11e ship.

### Claude's Discretion
- Exact `aura.cache_metrics` column types / index strategy, and whether `cache-stats` aggregates client-side or via SQL `GROUP BY` — planner/researcher decide against `golang-database` patterns.
- The precise `PromptBuilder` package boundary (new subpackage vs. existing `internal/agent`) — subject to D-01a's import-cycle constraint.
- Fixture turn content (the synthetic conversation used for the 20-turn replay) — must be deterministic and exercise tool-call turns, not just text turns.
</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### PRD (truth-source)
- `prd.md` §"Slice 4 — KV cache builder" (≈L1469–1549) — goal, smoke, acceptance, file-targets, OQ1–4, commit template. **Note OQ2 is overridden by D-02** (PRD-amendment required before impl).
- `prd.md` §"Cross-cutting — KV cache invariant CI (amendment #16, Pitfall #3 P0)" (≈L1599–1626) — the canonical spec for `cache_invariant_audit.sh`: cacheable-prefix index contract (`messages[0]` = system+manifest, NO per-turn data/timestamps/ids), file targets, CI gate wiring.
- `prd.md` §"Cross-cutting — Context rot mitigation policy (amendment #21)" L0 row (≈L1559) — where this phase sits in the 5-layer policy; cache-hit target ≥80% on DeepSeek-V4.
- `prd.md` amendment #11 (forward-compat `messages[2]` AgentInsight) and amendment #30 / Seam D00 (config-driven endpoint+model, no hardcoded provider) — referenced by D-03/D-06a.

### Roadmap & requirements
- `.planning/ROADMAP.md` §"Phase 6: KV Cache Builder" — 5 success criteria (the machine-checkable acceptance for Gate 3). Reconcile CAP-03 vs CAP-04 labelling.
- `.planning/PROJECT.md` — CAP-03 requirement wording; Out-of-Scope (Slice 13 vLLM gated on GPU).

### Memory (project knowledge)
- `reference_aura_cache_poisoning_sites_2026-05-27` — 6 historical poisoning sites (pre-rewrite) mapped to file:line; the warning catalog the CI gate exists to prevent recurring.
- `reference_openrouter_provider_capabilities_2026-05-27` — DeepSeek-V4 Flash 80% cache via OpenAI-wire shape (confirms `prompt_tokens_details.cached_tokens` is the right field under OpenRouter, not native `prompt_cache_hit_tokens`).

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- `internal/agent/prompt.go` — `SystemPrompt` frozen constant + `systemMessage()`; the byte-stable `messages[0]` source. PromptBuilder wraps this.
- `internal/agent/tools/manifest.go` — `Render()` (alphabetical sort, `:39`) + `RenderToolDefs()` (cache-stability-load-bearing ordering, already documented). The manifest half of `messages[0]` is done.
- `internal/agent/agenttest/fakeclient.go` — `FakeClient` (importable, implements `llm.Client`, scripts turns, **captures `Requests`**). The stub for D-05's runtime-faithful gate.
- `internal/llm/openai_compat/sse.go` + `usage.go` — already parse `prompt_tokens_details.cached_tokens` + `usage.cost` and emit a trailing `llm.Usage` chunk. The Tracker's input is already wired.
- `internal/runner/runner.go` — `Runner.Turn` drives one LLM round over rehydrated history; the 20-turn replay loops this.
- `cmd/aura/{agent,db,neo4j}.go` — the established `case "<subcommand>"` dispatch pattern for `cache-stats` + hidden `cache-audit`.

### Established Patterns
- **`messages[0]` is constant by construction** — no history/clock/id dependency. Any drift = a bug introduced by a *different* code path; hence the runtime-faithful gate (D-04).
- **Wire layer is currently caching-unaware** (`Request` has no `cache_control`) — D-03 deliberately changes this; update the design comment on touch.
- **`AURA_<DOMAIN>_<UNIT>` env convention** + config-driven `AURA_LLM_BASE_URL`/model (Seam D00) — never hardcode provider/endpoint.

### Integration Points
- `LlmAgent`/`runner` swap inline `llm.Request{Messages: ...}` construction → `PromptBuilder.Build(...)` (PRD diff `-15/+10`).
- New `aura.cache_metrics` migration (next in 0007+ sequence) + sqlc query set, consumed by `aura cache-stats`.
- New `.github/workflows/ci.yml` step "cache invariant gate" invoking `scripts/cache_invariant_audit.sh`.

</code_context>

<specifics>
## Specific Ideas

- The gate must print **per-turn SHA-256 to stdout** and fail with an explicit `messages[0] mutated at <site>` message (roadmap SC#5 wording) — operator-facing, not just a Go test pass/fail.
- Hash function takes an **index set** (today `{0}`; future `{0,1,2}`) so amendment #11's extension to Agent.md/AgentInsight tiers needs no rewrite.
- Cache-hit acceptance is an **invariant** (byte-identity), never a **percentage threshold** in CI (PRD OQ4: hit-rate is provider-dependent and flaky — measured by `cache-stats`, not gated by the script).

</specifics>

<deferred>
## Deferred Ideas

- **`messages[1]` content (Agent.md profile)** — seam only this phase; content ships with Slice 10.
- **`messages[2]` content (cached AgentInsight)** — seam-aware hash only; content ships with Slice 11e (amendment #11).
- **Runtime provider selection / activating the Anthropic `ephemeral` path** — Slice 13 `LLMRouter`; this phase only builds the dormant seam.
- **Throwaway `chat-loop` REPL** (PRD smoke) — superseded by the already-shipped persisted `aura chat` REPL; the 20-turn replay lives in `cache-audit`, not a new REPL.

</deferred>

---

*Phase: 6-KV Cache Builder*
*Context gathered: 2026-06-02*
