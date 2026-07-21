# Agent.md → agent-memory supersession — design + go-ahead (ADR)

Design date: 2026-07-21
Supersedes-parked-item: `docs/audit/HANDOFF-2026-07-20-wave1-memory.md` §3.
Related memories: `[[aura-l4-archival-memory-state]]`, `[[aura-fix-plan-wave0-status]]`, `[[debug-by-driving-the-agent]]`.

## 0. Locked scope (operator, 2026-07-21) — "No more, no less"

> **Remove Agent.md. Rewrite onboarding to let the LLM store all profile data in Neo4j via the dedicated agent-memory MCP.**

Two deliverables, nothing else. **Explicitly OUT of scope** (proposed, then cut): no always-injected profile block, no bounded-projection/token budget, no cache-boundary work, no controlled-category-vocabulary mandate, no new "standing-defaults" prompt line. Memory stays **pure D-03 pull-on-demand** — already the shipped doctrine ([prompt.go:12-13, 62-67](../../internal/agent/prompt.go)) — and the LLM stores freely through the memory MCP.

## 1. Why this is correct (evidence, not supposition)

- **Aura's own doctrine already is pull-on-demand.** `prompt.go:62-67` commands the agent to *search memory before answering* and *write durable facts proactively*. The messages[1] profile/recall **injection was a later, redundant second path** on top of that doctrine — removing it returns Aura to D-01/D-03.
- **The live agent already stores its profile itself, unprompted.** Conversation `03b9c7c2` (queried 2026-07-21, `aura-postgres` + `aura-neo4j`): in one turn (request `019f8115`) it emitted **4 entities then 5 facts, entities-first** — `Davide`(PERSON)/`PmSync`(ORGANIZATION)/`Caraglio`(LOCATION)/`Andrea`(PERSON), then `Davide lavora_per PmSync / risiede_a Caraglio / ruolo Programmatore / …`. It also used `memory_get_context`/`search` 5× and `get_facts`. Onboarding does not need to invent storage behavior — it triggers the behavior the agent already performs, over the collected answers.
- **Injection carries costs the tool path doesn't:** prefix-cache churn, the memory-poisoning surface (`[[l4-recall-injection-security-followup]]`), per-turn token cost, staleness. Dropping it is strictly simplifying.
- **Industrial grounding** (mem0, Honcho, agent-memory fork): the agent-native camp treats memory as tool-driven, not stuffed into context. (The always-inject camp — Poke/Letta/hermes — is the alternative Aura is deliberately *not* taking here.)

## 2. Design

### 2.1 Remove Agent.md
- Drop **leg1** (`r.contextBlock`, the profile) from `Runner.renderContextBlock` ([runner_context.go:52](../../internal/runner/runner_context.go)); retire `profileContextProvider` (`serve_adapters.go:515-534`) and its injection (`chat_boot.go:346`); **delete `internal/profile/*`**; trim the `<profile_context>` block (`prompt.go:53-60`) — the `<memory>` D-03 block already covers recall.
- **Leg2** (archival-recall injection, `AURA_CONTEXT_MEMORY_RECALL`) stays **default-OFF exactly as it is** — not flipped on, not deleted (out of "no more, no less"). **Leg3** (always-on skills) untouched.
- Re-verify each seam at plan time (never suppose); the line numbers above are from the 2026-07-20 handoff mapping.

### 2.2 Rewrite onboarding to store in Neo4j via the memory MCP
- Replace the `ProfileWriter.WriteProfile` (Agent.md) seam — web ×3 `onboarding_provision.go:425/442/549`, Telegram ×2 `profile_onboarding.go:259/271` — with a **single LLM pass over `session.Answers`** that stores the operator profile into Neo4j through the **dedicated agent-memory MCP**, using the existing `memory_add_entity`/`memory_add_fact`/`memory_add_preference` tools (entities first, so facts resolve by name). This is exactly the behavior observed in `03b9c7c2`. The existing exact-text + 0.95 dedup makes re-running onboarding idempotent.
  - A batched `memory_save_profile(entities[], facts[], preferences[])` fork tool is an **optional optimization** (one round-trip instead of N) — build it only if we want to compress the calls; not required for correctness.
- **Onboarding-complete flag** → a sentinel **Fact** (`predicate=onboarding_completed`, object = ISO timestamp) read by `/api/onboarding/status` (`serve_onboarding.go:318-336`), retiring `Metadata.OnboardingCompleted`. No migration.
- Also `store_message` the raw answers (lossy-extraction safety net).

### 2.3 Implementation choice — LOCKED (a)
Onboarding stores via the **existing `memory_add_entity`/`memory_add_fact`/`memory_add_preference` tools** — no fork change, no sidecar re-vendor/redeploy. A batched `memory_save_profile` fork tool is **deferred as a future optimization only** (build it later if the per-onboarding call count matters). Operator decision 2026-07-21.

## 3. Proposed PRD Amendment #87 (draft — ratify before code)

> **Amendment #87 / Slice 10 supersession (2026-07-21): Agent.md retired — the agent-memory graph is the profile store.**
> The static `Agent.md` profile (`internal/profile/`, injected as `<profile_context>` at messages[1]) is **deprecated and removed**: leg1 of `renderContextBlock`, `profileContextProvider` + its injection, and `internal/profile/*` are deleted; the `<profile_context>` prompt block is trimmed (the `<memory>` D-03 pull-on-demand doctrine already governs recall). The archival-recall injection leg (`AURA_CONTEXT_MEMORY_RECALL`) is unchanged and remains default-off.
> **Onboarding** is rewritten: instead of writing Agent.md, it performs one LLM pass over `session.Answers` and stores the operator profile (entities, facts, preferences) into Neo4j through the **dedicated agent-memory MCP** (existing `memory_add_*` tools; a batched `memory_save_profile` is an optional optimization). Raw answers are also `store_message`'d. Re-running onboarding is idempotent via the existing dedup. The **onboarding-complete flag** moves from `Metadata.OnboardingCompleted` to a sentinel Fact (`predicate=onboarding_completed`) read by `/api/onboarding/status` — a UX gate, not a "profile complete" claim (no migration).
> No new always-injected block, no projection budget, no cache-boundary change, no new env var. Rationale + live-behavior evidence: `docs/audit/agentmd-supersession-design-2026-07-21.md`.

## 4. Sources
Live stack (`aura-postgres` `tool_invocations`, `aura-neo4j` graph, conv `03b9c7c2`) · `prompt.go` D-01/D-03 memory doctrine · agent-memory fork write-path (`d:/tmp/agent-memory-fork`) · industrial survey (mem0, Honcho, Letta, hermes, Poke — `d:/tmp/{mem0,system-prompts-and-models-of-ai-tools}`).
