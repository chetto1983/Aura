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
- Drop **leg1** (`r.contextBlock`, the profile) from `Runner.renderContextBlock` ([runner_context.go:63-67](../../internal/runner/runner_context.go)); narrow the owner-resolve guard to `if r.archivalRecaller != nil`. Remove `Runner.contextBlock`/`Deps.ContextBlock`/its `New()` assignment (`runner.go`) and the `ContextBlockProvider` type (`interfaces.go`).
- **Path corrected (the ADR draft was stale):** `profileContextProvider` and the injection wiring live in **`cmd/aura/`** (`serve_adapters.go:515-534`, `chat_boot.go:346`), NOT `internal/agent/`. Also retire the `aura cache-audit` Agent.md-cache-churn scenario (`cmd/aura/cache_audit.go:205,302-310`) — moot once nothing is injected.
- **Delete `internal/profile/*`** — but **two prerequisite relocations first** (mechanically required; the ADR draft omitted them, the seam-map caught them):
  - **`internal/idroot`** ← the repo's *only* per-identity path-traversal guard (`RootIdentityDir`/`ValidateIdentity`/`ErrInvalidIdentity`/`DefaultRoot`, D-20/D-21) currently lives in `profile/store.go` and is used by 6 non-onboarding consumers (`mcp`, `skills`, `objectstore`, `garageadmin`, `config`, `serve_provisioning`). Move verbatim before deletion.
  - **`internal/onboarding`** ← `profile.Preferences` + the Agent.md renderer are live domain types in the interview state machine. Move before deletion.
- Trim the `<profile_context>` block (`prompt.go:53-60`) — the `<memory>` D-03 block already covers recall.
- **Leg2** (archival-recall injection, `AURA_CONTEXT_MEMORY_RECALL`) stays **default-OFF exactly as it is** — not flipped on, not deleted. **Leg3** (always-on skills) untouched.

### 2.2 Rewrite onboarding to store in Neo4j via the memory MCP
- Replace the `ProfileWriter.WriteProfile` (Agent.md) seam — web (`agui/onboarding_provision.go` `StatusCompleted`/`StatusSkipped`/`persistProfile`) + Telegram (`profile_onboarding.go` `writeCompleted`/`writeSkipped`) — with **one shared narrow port** `internal/onboarding.ProfileMemoryStore { StoreConfirmed / StoreSkipped / Status }`, wired twice at the composition root (deep-refactor-on-touch — no two divergent mappers).
- **Storage is a deterministic Go mapping, NOT a second LLM call.** `session.Answers` is *already* fully structured (the interview ran per-step LLM extraction, `session.go:74-95`), and neither web nor Telegram holds a `Runner`. So map Answers → `memory_add_entity`/`memory_add_fact`/`memory_add_preference` deterministically (entities-first — the `03b9c7c2` shape) + `memory_store_message` the raw answers (safety net). This honors the LOCKED-(a) decision (the tools are pinned, not an LLM turn) and is cheaper + daemon-free-unit-testable. *(Supersedes the earlier "one LLM pass" wording — the LLM work already happens inside the interview.)*
- **🔴 Security — mandatory `user_identifier` scoping.** The concrete adapter lives in `cmd/aura` over `callMemoryToolText`, which does **NOT** auto-inject `user_identifier` (only the agent bridge `mcptools/bridge.go` does). Every write **and** the status read MUST set `args["user_identifier"] = identityID` (the **UUID**, `owner.ID`, never the name) — mirror `serve_recall.go:39` — or writes/reads land in the memory server's fail-open **global** scope and leak across tenants in MUSR mode. Enforced by a daemon-free adapter unit test.
- **Onboarding-complete flag** → **two** sentinel Facts (subject = `identityID`): `predicate=onboarding_completed` and `predicate=onboarding_skipped` (object = ISO timestamp), preserving the current 3-state `/api/onboarding/status` (`serve_onboarding.go`) which reads both `OnboardingCompleted` and `OnboardingSkipped`. Read via `memory_get_facts` scoped to `identityID`, scanned client-side. Fails open to `Required:true` on sidecar error (matches today's not-found). Retires `Metadata.OnboardingCompleted`/`Skipped`. No migration.
- `DraftAgentMD` is still **rendered** for the confirm-UX review (relocated renderer, §2.1) but no longer **persisted**.

### 2.4 Verified implementation plan — 7 atomic commits (workflow `wj76q1ls9`, 2026-07-21)
"Prove the new store, then tear down the old" — the build stays green and profile-capture never lapses:
1. **`internal/idroot`** — relocate the D-20/D-21 path guard + `DefaultRoot`, rewire 6 consumers (pure move).
2. **`internal/onboarding`** — relocate `Preferences` + the Agent.md renderer (pure move).
3. **Web onboarding → memory** — `ProfileMemoryStore` port + deterministic mapper + `cmd/aura` memory adapter (with `user_identifier`) + sentinel facts + status-via-`memory_get_facts`. Daemon-free unit tests for mapper/sentinel/scanner/scoping.
4. **Telegram onboarding → same port**, then **real E2E** on the live stack (Neo4j holds the identity-scoped profile; status flips; a later turn recalls a fact).
5. **Remove injection leg1** (`runner_context.go`/`runner.go`/`interfaces.go` + `cmd/aura` providers + cache-audit); re-drive the runner error-branch tests via leg2 so the 85% floor holds.
6. **Trim `<profile_context>`** (one-time `messages[0]` hash reset — no golden pin exists; update two `prompt_test.go` needles).
7. **Delete `internal/profile`** + the dead `aura profile` CLI — last, when every consumer is migrated; full `db_integration neo4j_integration` matrix ≥85% + final E2E + quality-snapshot re-attest.

**Carried risks:** fail-open scope leak (mitigated by mandatory `user_identifier` + test); coverage floor (integration-gated memory calls contribute zero → daemon-free unit tests required); status now depends on the memory sidecar (fails-open to re-prompt); best-effort partial-failure across N memory round-trips vs today's one atomic file write (idempotency leans on the server's dedup).

### 2.3 Implementation choice — LOCKED (a)
Onboarding stores via the **existing `memory_add_entity`/`memory_add_fact`/`memory_add_preference` tools** — no fork change, no sidecar re-vendor/redeploy. A batched `memory_save_profile` fork tool is **deferred as a future optimization only** (build it later if the per-onboarding call count matters). Operator decision 2026-07-21.

## 3. Proposed PRD Amendment #87 (draft — ratify before code)

> **Amendment #87 / Slice 10 supersession (2026-07-21): Agent.md retired — the agent-memory graph is the profile store.**
> The static `Agent.md` profile (`internal/profile/`, injected as `<profile_context>` at messages[1]) is **deprecated and removed**: leg1 of `renderContextBlock`, `profileContextProvider` + its injection (in `cmd/aura/`), and `internal/profile/*` are deleted; the `<profile_context>` prompt block is trimmed (the `<memory>` D-03 pull-on-demand doctrine already governs recall). Two symbols are relocated verbatim *before* the deletion (they are not Agent.md concerns): the per-identity path-traversal guard (D-20/D-21) → `internal/idroot`, and `Preferences` + the draft renderer → `internal/onboarding`. The archival-recall injection leg (`AURA_CONTEXT_MEMORY_RECALL`) is unchanged and remains default-off.
> **Onboarding** is rewritten: instead of writing Agent.md, a **deterministic Go mapping** of the already-structured `session.Answers` (the interview's per-step LLM extraction is the sole LLM work) stores the operator profile (entities, facts, preferences) into Neo4j through the **dedicated agent-memory MCP** (existing `memory_add_*` tools via one shared `ProfileMemoryStore` port; a batched `memory_save_profile` is an optional future optimization). Every write and the status read **must** carry `user_identifier=identityID` (fail-open global-scope leak otherwise). Raw answers are also `store_message`'d. Re-running onboarding is idempotent via the existing dedup. The **onboarding-complete flag** moves from `Metadata.OnboardingCompleted`/`Skipped` to sentinel Facts (`predicate=onboarding_completed`|`onboarding_skipped`) read by `/api/onboarding/status` — a UX gate, not a "profile complete" claim (no migration).
> No new always-injected block, no projection budget, no cache-boundary change, no new env var. Rationale + live-behavior evidence + 7-commit plan: `docs/audit/agentmd-supersession-design-2026-07-21.md`.

## 4. Sources
Live stack (`aura-postgres` `tool_invocations`, `aura-neo4j` graph, conv `03b9c7c2`) · `prompt.go` D-01/D-03 memory doctrine · agent-memory fork write-path (`d:/tmp/agent-memory-fork`) · industrial survey (mem0, Honcho, Letta, hermes, Poke — `d:/tmp/{mem0,system-prompts-and-models-of-ai-tools}`).
