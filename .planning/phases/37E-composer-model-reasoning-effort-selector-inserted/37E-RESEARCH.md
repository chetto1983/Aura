# Phase 37E: Composer Reasoning-Effort Selector - Research

**Researched:** 2026-07-10
**Domain:** Per-turn reasoning-effort control (web Composer → `/agent/run` → provider-neutral `llm.ReasoningConfig` → OpenRouter + llama.cpp wire), per-conversation persistence
**Confidence:** HIGH (wire contract spike-settled; all code seams read + line-verified against HEAD)

## Summary

37E adds a GPT-style `off · low · mid · high · auto` reasoning-effort selector to the web Composer. The selector value rides `POST /agent/run` (an optional symbolic field on the existing `aura` envelope, exactly mirroring 37D's `aura.skill`), the server validates it against a fixed enum (invalid → 400, D-05), and a **fixed** level (off/low/mid/high) bypasses the adaptive-reasoning classifier and forces `req.Reasoning`; `auto` sends nothing and runs today's adaptive policy unchanged (D-04, zero regression). The value persists per-conversation in the existing `aura.conversations.metadata` jsonb column (no migration, D-06) and restores when the thread reopens.

The wire contract is **empirically settled** by two VALIDATED spikes and needs no re-probing: OpenRouter's OFF-switch (`reasoning:{effort:"none"}`) already works in Aura's current shape (spike 096) — **no OpenRouter wire change**; llama.cpp **ignores** Aura's `reasoning:{effort}` object entirely (spike 095), so the llama.cpp path is net-new — OFF = `chat_template_kwargs:{enable_thinking:false}`, graduated = `thinking_budget_tokens:512/2048/8192`. Graduated fidelity is **backend-dependent** (real on llama.cpp, effectively on/off on DeepSeek-V4-Flash) — the plan and UAT must state this honestly (D-09).

**Primary recommendation:** Thread the validated symbolic override from `handleRun` through a **ctx value** (`runner.WithReasoningOverride`) into `runner.buildAgent` → a new `LlmAgentConfig.ReasoningOverride` field → a new `ApplyFixedReasoning` builder seam that mirrors `ApplyAdaptiveReasoning`, gates on a **generalized** `IsReasoningTarget` (OpenRouter **or** llama.cpp), and sets `req.Reasoning` with `exclude` derived from `cfg.ShowReasoning` (D-10 parity). Add a target-aware branch in `openai_compat.buildWireRequest` keyed on a new neutral `llm.ReasoningTarget(provider, baseURL)` classifier. Persist via a new owner-scoped `jsonb_set` update query; hydrate by mapping `metadata` onto the `conversations.Conversation` projection (already SELECTed, currently dropped).

---

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions (research THESE, no alternatives)
- **D-01:** Effort-only selector; **the model dropdown is DROPPED**. Model selection stays in the Settings page (`AURA_LLM_MODEL`, operator-scoped, already shipped). Composer exposes ONE control: the reasoning-effort selector.
- **D-02:** Five levels, GPT-style: **`off · low · mid · high · auto`** (symbolic UI values). Client NEVER sends a raw provider reasoning payload.
- **D-03:** UI level → `llm.ReasoningConfig` server-side map — off→`ReasoningEffortNone`, low→`Low`, mid→`Medium`, high→`High`, auto→no override. `Exclude` set from `cfg.ShowReasoning` exactly as `ReasoningTier.reasoning()` does.
- **D-04:** **"auto" = Aura's existing adaptive policy, unchanged.** auto → Composer sends no override → `ApplyAdaptiveReasoning` runs as today. A fixed level bypasses the classifier and forces `req.Reasoning`.
- **D-05:** `POST /agent/run` accepts an optional symbolic effort from a fixed enum (`off|low|mid|high|auto`); server maps to `ReasoningConfig`; value outside enum → **400**. Absent/`auto` → today's adaptive default.
- **D-06:** Persisted **per-conversation, restored on reopen** (Claude parity). Default target: **`aura.conversations.metadata` jsonb — NO migration.** Typed column only if querying/indexing is truly needed (justify).
- **D-07:** New conversations default to **`auto`** → zero regression.
- **D-08:** Effort must take effect on **BOTH OpenRouter AND a local llama.cpp chat backend**. Generalize `IsOpenRouterReasoningTarget` to recognize llama.cpp AND branch `buildWireReasoning`. (Wire contract spike-settled — see Backend Wire Contract.)
- **D-09:** Reliable on every backend = **off vs. on vs. auto**. True low/mid/high gradation is **backend-dependent** (real on llama.cpp, on/off on DeepSeek-V4-Flash). Plan + UAT MUST state this; do NOT sell graduated effort as uniform.
- **D-09a (RESOLVED):** Ship **exactly `off/low/mid/high/auto`; `Max` is NOT added.** `auto` is the zero-regression default; an unlimited-budget `Max` (`thinking_budget_tokens:-1`) is a deferred idea.
- **D-10:** Selector controls **effort only**. CoT **visibility** stays governed by `AURA_SHOW_REASONING` / the `exclude` flag. Reuse `ReasoningTier.reasoning()`'s exclude handling.
- **D-11:** **PRD-amendment BEFORE any code** (Wave 1 = PRD-amendment gate): (a) amend WEBMODEL-01/02 to drop the model-selector clause; (b) amend WEBMODEL-03 to the effort-enum no-bypass form; (c) add the llama.cpp coverage requirement; (d) document the composer effort selector + `/agent/run` effort field + per-conversation persistence in `prd.md`. No implementation plan lands before it.

### Claude's Discretion
- **UI widget/placement** — segmented control vs. small dropdown/pill near the send button (GPT shows a compact pill). Keep it accessible (ARIA) and non-disruptive to Composer paste/drop/Enter-send (37D D-08/D-09 precedent).
- **Persistence mechanism** — `conversations.metadata` jsonb (recommended, no migration) vs. a typed column.
- **Override wire seam** — set `req.Reasoning` directly post-build vs. thread a per-turn tier through `buildRequest`. (This research recommends a hybrid — see Current-Code Seam Map §1.)
- **Label wording / i18n** — en+it parity for the five levels, CI-checked.

### Deferred Ideas (OUT OF SCOPE)
- Composer **model dropdown** (model lives in Settings, D-01).
- **Per-message** effort override (37E locks per-conversation, D-06).
- **`xhigh` / `minimal` levels + a reasoning-budget (MaxTokens) knob** — `ReasoningConfig` supports them; 37E exposes only the five levels.
- An **unlimited-budget `Max` level** (`thinking_budget_tokens:-1`) — distinct only on llama.cpp; deferred.
- **In-UI model-specific effort hints** ("this model collapses low→high") — D-09 documents the reality; a UI hint is a future nicety.
</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description (post-amendment intent) | Research Support |
|----|-------------------------------------|------------------|
| **WEBMODEL-01** | Composer exposes an **effort selector** (`off/low/mid/high/auto`); the choice is persisted per-conversation. (Model-selector clause **removed** by the Wave-1 PRD-amendment, D-01/D-11.) | Frontend mirror of 37D pinned-skill (`Composer.tsx` props + `usePinnedSkill` state model, adapted to per-conversation hydration); persistence via `conversations.metadata` (Seam Map §5). |
| **WEBMODEL-02** | `/agent/run` accepts an optional **effort** override validated server-side against the fixed enum (`off/low/mid/high/auto`); non-enum → 400; absent/`auto` → today's adaptive default (no regression). | `server_run_request.go` `aura.effort` decode mirrors `aura.skill`; `handleRun` validation → 400 (Seam Map §4). |
| **WEBMODEL-03** | No bypass of governance: the client sends a **symbol**, never a raw `ReasoningConfig` or arbitrary budget; the server owns the symbol→config map. Unit + e2e; coverage ≥85%. Architectural check: the LLM contract supports a per-request override (CONFIRMED — `Request.Reasoning` exists and is per-call). | `llm.ReasoningConfig` + `Request.Reasoning` already per-request (client.go); server-side enum gate is the no-bypass control (Threat Model). |
</phase_requirements>

## Project Constraints (from CLAUDE.md)

- **PRD-first (absolute):** No code before the Wave-1 PRD-amendment (D-11). The amendment reconciles ROADMAP.md + REQUIREMENTS.md (WEBMODEL-01..03) + prd.md.
- **Owned-surface coverage floor ≥85%** across the full tag matrix. The coverage gate runs **`db_integration neo4j_integration` ONLY** — there is no `docker_integration` CI job. **The llama.cpp wire branch MUST be exercised by a daemon-free pure unit test** (a `buildWireRequest` table test), never a container-gated one, or it contributes ZERO coverage and the floor fails.
- **No god class > 600 LOC.** `ExternalStoreChat.tsx` and `sseAdapter.ts` already sit at/near the 600-LOC cap — the 37D pattern extracted `auraRunBody.ts` and `usePinnedSkill.ts` to stay under; 37E MUST extract its effort state/hook similarly (`useReasoningEffort.ts`), never inline into those files.
- **Deferred-tool pattern:** N/A — 37E adds no LLM tool.
- **i18n en+it CI parity:** every new string (5 level labels + selector aria-label) in both `en` and `it`, parity CI-checked (37B/C/D precedent).
- **Direct-git commits** (the file-size hook takes ~66s via the gsd wrapper).
- **NEVER SUPPOSE / READ BEFORE EDIT:** the wire contract is spike-settled; do not re-derive it.

## Package Legitimacy Audit

**Not applicable — 37E installs NO new external packages.** All work is internal Go (`internal/llm`, `internal/agent`, `internal/runner`, `internal/agui`, `internal/conversations`, `internal/db`) plus existing web dependencies (`@assistant-ui/react`, `react-i18next`, `lucide-react` — all already in `web/package.json` and used by the Composer). No npm/Go module additions. The slopcheck / registry-verification gate is vacuously satisfied.

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| Effort selector UI + per-conversation hydration | Browser / Client (`web/src/chat`) | — | Pure presentation + a hydrated preference; no business logic client-side. |
| Symbol validation + enum→config governance | API / Backend (`internal/agui` `handleRun`) | — | D-05 no-bypass control MUST be server-side; client sends only a symbol. |
| Adaptive-vs-fixed decision + `req.Reasoning` assembly | API / Backend (`internal/agent` build seam) | — | The request is built per-turn inside the agent loop; exclude parity needs `cfg.ShowReasoning`. |
| Provider-neutral reasoning contract | API / Backend (`internal/llm`) | — | `ReasoningConfig` is already the neutral contract the agent loop never branches on. |
| Backend wire projection (OpenRouter vs llama.cpp) | API / Backend (`internal/llm/openai_compat`) | — | Wire-shape branch belongs at the wire layer, keyed on provider/baseURL. |
| Per-conversation persistence | Database (`aura.conversations.metadata` jsonb) | Backend store (`internal/conversations`) | Column exists; owner-scoped write + read-projection mapping needed. |

---

## Current-Code Seam Map

> Line numbers verified against HEAD 2026-07-10. Where CONTEXT.md's canonical_refs drifted, it is flagged.

### 1. The per-turn override SEAM (the crux — RECOMMENDED design)

**Problem:** the effort symbol enters at `handleRun` (HTTP layer) but must reach `req.Reasoning`, which is assembled deep inside the agent's per-turn loop (`llm_agent.go` → `builder.go`). Between them sit the `Runner` and a fresh-per-turn `LlmAgent`. The 37D `aura.skill` field did NOT need this threading (it only reframed the model user message inside `handleRun`); **effort is different — it must reach the request builder.**

**Why the existing `ReasoningTier` type is insufficient:** `prompt.ReasoningTier` has only `none/low/high` (`reasoning_policy.go:13-20`) — **there is no `medium` tier.** The five UI levels map to `llm.ReasoningEffort` (`none/low/medium/high`, `client.go:135-142`), which DOES have `medium`. So the fixed override must carry an `llm.ReasoningEffort`, **not** a `ReasoningTier`. Threading a `ReasoningTier` (CONTEXT option b, literal reading) would silently lose "mid".

**Recommended threading: ctx value → config field → new builder seam.** Rationale: the effort is orthogonal to message content, and `handleRun` already composes a `TurnWithModelUserMessage` split (`server.go:376-380`) for the skill/attachment case — adding Turn-variant methods for effort would combinatorially explode with that split. The codebase already threads per-turn request scope via ctx (`runner.WithThreadLockHeld` `server.go:358`, `tools.WithRequestID` `llm_agent.go:171`, `gateway.WithResponder` `runner.go:550`, `identityctx.WithIdentityID`). A ctx value composes cleanly with the existing message-split.

Concrete change set (six edits, all additive):

| # | File:line | Change |
|---|-----------|--------|
| 1a | `internal/agui/server_run_request.go:11-18` + `:29-34` | Add `Effort string \`json:"effort"\`` to BOTH the `Aura` struct and the `ext.Aura` decode struct (mirror `Skill` exactly). |
| 1b | `internal/agui/server.go:~322` (after `lastUserMessage`, before `Turn`) | Validate `req.Aura.Effort` → `effort, ok := parseEffortSymbol(...)`; `!ok` → `http.Error(w, "invalid reasoning effort", 400)`. On a **fixed** level, `ctx = runner.WithReasoningOverride(ctx, mappedEffort)`. `auto`/absent → no ctx mutation. Persist the symbol (see §5). |
| 1c | `internal/runner/runner.go` (new `runner_reasoning.go` or session file) | `WithReasoningOverride(ctx, llm.ReasoningEffort) context.Context` + `reasoningOverride(ctx) (llm.ReasoningEffort, bool)` — mirror `WithThreadLockHeld`/`threadLockHeld` in `runner_session.go`. |
| 1d | `internal/runner/runner.go:552-565` (`buildAgent`) | Read `reasoningOverride(ctx)`; pass into `agent.LlmAgentConfig{... ReasoningOverride: eff}`. |
| 1e | `internal/agent/llm_agent.go` (struct ~L102, config ~L130, `Run` L259-263, `buildRequest` L445-450) | Add `reasoningOverride llm.ReasoningEffort` field + config field. In `Run`, when the override is set (fixed), **skip** `adaptiveReasoningTier` and route `buildRequest` to the new fixed path; when unset/auto, today's adaptive path is byte-identical. |
| 1f | `internal/agent/prompt/builder.go` (new `BuildWithReasoningOverride`) + `reasoning_policy.go` (new `ApplyFixedReasoning`) | `ApplyFixedReasoning(req, provider, cfg, effort)` gates on the **generalized** `IsReasoningTarget` (§2) and sets `req.Reasoning = llm.ReasoningConfig{Effort: effort, Exclude: boolPtr(!cfg.ShowReasoning)}` — reusing the EXACT exclude logic of `ReasoningTier.reasoning()` (`reasoning_policy.go:78-88`), satisfying D-10. |

**Adaptive path stays OpenRouter-only (D-04 "unchanged").** `adaptiveReasoningTier` (`llm_agent_reasoning.go:24`) and `ApplyAdaptiveReasoning` (`reasoning_policy.go:39`) keep gating on `IsOpenRouterReasoningTarget`. Only the **fixed** path uses the generalized `IsReasoningTarget`. Consequence: on a llama.cpp backend with `auto`, no override is sent → llama.cpp's default thinking (ON) applies with no adaptive tiering. This matches "auto = the model self-adapts" and is a documented, acceptable limitation — NOT a regression (llama.cpp adaptive tiering never worked; the effort object was ignored).

**D-05 reconciliation ("server maps symbol → ReasoningConfig"):** the server owns the symbol→`llm.ReasoningEffort` map and the enum gate (the no-bypass control); the final `ReasoningConfig` assembly (adding `exclude`) happens one layer down in `ApplyFixedReasoning` because `exclude` is a **visibility** concern that must read `cfg.ShowReasoning` (D-10) — which the agui layer does not carry. This honors D-05's spirit (server owns effort authority, client sends only a symbol) and D-10 (exclude parity via cfg) simultaneously.

**Why NOT "set `req.Reasoning` directly post-build" (CONTEXT option a):** it would duplicate the `IsReasoningTarget` gate and the exclude derivation outside the existing `Apply*Reasoning` family, drifting from the one place that owns effort→wire projection. The recommended `ApplyFixedReasoning` is the symmetric sibling of `ApplyAdaptiveReasoning` — one family, one exclude rule.

### 2. Multi-backend target recognition

- **Current:** `prompt.IsOpenRouterReasoningTarget(provider, baseURL)` (`reasoning_policy.go:47-53`) — true only for `provider=="openrouter"` and a baseURL that is empty or contains `openrouter.ai`.
- **The wire layer needs the same knowledge:** `openai_compat.buildWireRequest` (`client.go:220`) holds `c.cfg.Provider` + `c.cfg.BaseURL` and must branch the wire shape. Importing the agent-layer `prompt` package from the low-level wire client is a layering smell.
- **Recommended generalization:** add a neutral classifier in `internal/llm` (the package both `prompt` and `openai_compat` already import):
  ```go
  // internal/llm/reasoning_target.go
  type ReasoningTargetKind int
  const (
      ReasoningTargetNone ReasoningTargetKind = iota
      ReasoningTargetOpenRouter
      ReasoningTargetLlamaCpp
  )
  func ReasoningTarget(provider, baseURL string) ReasoningTargetKind
  ```
  Then `prompt.IsOpenRouterReasoningTarget` becomes `ReasoningTarget(...) == llm.ReasoningTargetOpenRouter` (behavior-preserving), and a new `prompt.IsReasoningTarget` returns true for OpenRouter **or** LlamaCpp (used by the fixed path only).
- **KEY OPEN QUESTION — how is llama.cpp positively identified?** See Open Questions §OQ-1. Recognition must not misfire on the DGX/vLLM local path (which also emits `reasoning`, per `sse.go:22`). **Recommendation:** key on an explicit `cfg.Provider` value (`"llamacpp"`), NOT a baseURL heuristic. This requires adding a provider knob (env `AURA_LLM_PROVIDER` + settings `AURA_LLM_PROVIDER`) because `Provider` is currently only settable via `~/.aura/llm.json` (no env override exists — `applyEnvOverrides` sets Model/BaseURL but never Provider, `config.go:309-357`).

### 3. The llama.cpp wire branch

- **Branch point:** `openai_compat.buildWireRequest` (`client.go:220-241`) + `buildWireReasoning` (`client.go:243-253`). Currently unconditionally emits OpenRouter's nested `reasoning:{effort,max_tokens,exclude,enabled}` (`wireReasoning`, `client.go:84-89`).
- **`wireRequest` (client.go:71-82) gains two optional fields:**
  ```go
  ChatTemplateKwargs   map[string]any `json:"chat_template_kwargs,omitempty"`
  ThinkingBudgetTokens *int           `json:"thinking_budget_tokens,omitempty"`
  ```
- **`buildWireRequest` becomes target-aware** (make `buildWireReasoning` a method `(c *Client)` or pass the kind): switch on `llm.ReasoningTarget(c.cfg.Provider, c.cfg.BaseURL)`:
  - `OpenRouter` (or None): today's `Reasoning: buildWireReasoning(req.Reasoning)` — **UNCHANGED** (spike 096: OFF already works).
  - `LlamaCpp`: translate `req.Reasoning.Effort` → llama.cpp fields, leave `Reasoning` nil (the OpenRouter object is a NO-OP on llama-server, spike 095). See level→value table in Backend Wire Contract.
- **`exclude` has no llama.cpp wire representation** — on llama.cpp, CoT visibility is governed by the `--reasoning-format` server flag; the SSE reader already accepts both `reasoning` and `reasoning_content` (`sse.go:26-28`, `reasoningDelta` `:190-195`). This is consistent with D-10 (visibility is not the selector's job).
- **Constant location:** put the effort→budget table (512/2048/8192) as consts in `openai_compat` (it is a llama.cpp wire detail). Note in a comment they can be promoted to config (`AURA_LLM_LLAMACPP_THINKING_BUDGET_{LOW,MID,HIGH}`) if tuning is later needed — not for the first cut (spike-095 validated these exact values).

### 4. Server enum validation (mirror 37D's pinned-skill field)

- **Decode** (`server_run_request.go`): `Effort` added to both structs (§1a). 37D reference: `Skill string \`json:"skill"\`` at `:16` and `:32`, test at `server_run_request_test.go:20`.
- **Validate** (`server.go handleRun`, ~L322): a small pure helper (fully unit-testable, no I/O):
  ```go
  func parseEffortSymbol(s string) (llm.ReasoningEffort, bool, bool) // (effort, isFixed, ok)
  // "" or "auto" -> ("", false, true)   // valid, no override
  // "off"        -> (ReasoningEffortNone,   true, true)
  // "low"        -> (ReasoningEffortLow,    true, true)
  // "mid"        -> (ReasoningEffortMedium, true, true)
  // "high"       -> (ReasoningEffortHigh,   true, true)
  // anything else -> ("", false, false)  // -> 400
  ```
  `!ok` → `http.Error(w, "invalid reasoning effort", http.StatusBadRequest)` (D-05). Place validation AFTER the existing owner-scope `GetForIdentity` gate (`server.go:314`) so a foreign thread still 404s first (isolation before governance).
- **Note:** the run body carries `mid` (UI) but the enum maps to `medium` (the `llm.ReasoningEffort` vocab). Keep the UI/wire symbol `mid` and the internal effort `medium` distinct and documented.

### 5. Persistence wiring (`conversations.metadata` jsonb — NO migration)

**Read path (hydrate on open) — the projection currently DROPS metadata:**
- `metadata` is already SELECTed by `CreateConversation`, `GetConversation`, `GetConversationForIdentity`, `ListConversations`, `ListConversationsForIdentity` (`queries/conversations.sql:6,11,60,18,70`). **No read query change needed.**
- BUT `conversationFromRow` (`store_helpers.go:22-41`) does NOT map `metadata` onto the `Conversation` struct (`store.go:96-110` has no Metadata/ReasoningEffort field). **Add** a `ReasoningEffort string` field to `Conversation` and parse it out of `r.Metadata` (jsonb) in `conversationFromRow` (empty/absent → `""`, which the frontend hydrates as `auto`).
- Surfacing to the frontend is then FREE: `handleGetConversation` / `handleListConversations` (`conversations_api.go:51,48`) return the `Conversation` projection the SPA already consumes — the effort rides along.

**Write path — no update-metadata query exists today:**
- **Add** an owner-scoped query mirroring `RenameConversationForIdentity` (`queries/conversations.sql:93-98`):
  ```sql
  -- name: UpdateConversationReasoningEffortForIdentity :execrows
  UPDATE aura.conversations
  SET metadata = jsonb_set(COALESCE(metadata, '{}'::jsonb), '{reasoning_effort}', to_jsonb(sqlc.arg(effort)::text), true)
  WHERE id = sqlc.arg(id) AND identity_id = sqlc.arg(identity_id);
  ```
  `jsonb_set` merges the one key without clobbering other metadata. `:execrows` → 0 rows = not owned (403/404 split, mirrors the rename/delete pattern).
- **Add** store method `(*Store).UpdateReasoningEffortForIdentity(ctx, convID, identityID, effort string) (int64, error)` + expose it on the agui `ConversationStore` interface (`types.go:29-59`, alongside `RenameForIdentity` at `:49`).
- **Call site:** `handleRun`, after enum validation, persist the symbol (including `auto`, so switching back to auto is remembered). Owner id = `scopedIdentityID(ctx)` (already used at `server.go:314`).

**When to persist — planner decision (see OQ-2):** persist-on-send in `handleRun` is the minimal path (the effort already rides the run body). For strict Claude parity (persist a change even without a send), add a tiny dedicated `POST /api/conversations/{id}/reasoning-effort` the selector calls on change. Recommendation: ship persist-on-send first; add the dedicated PATCH only if UAT shows the change-without-send gap matters.

**Typed column NOT justified:** the effort is read once on thread open and never filtered, sorted, or aggregated. jsonb is correct (D-06 default). A typed column would need a migration for zero query benefit.

### 6. Frontend send-payload mirror + per-conversation restore

- **Wire serialization:** `web/src/chat/auraRunBody.ts` `buildAuraRunBody` folds `attachment_ids` + `skill` into the `aura` envelope. **Add** `...(opts.effort !== undefined && opts.effort !== 'auto' ? { effort: opts.effort } : {})` — send only fixed levels; `auto` omits (D-04/D-05 "absent/auto → no override").
- **Options type:** `StreamRunOptions` (`sseAdapter.ts:466-483`) gains `readonly effort?: string;` (mirror `skill?: string` at `:472`).
- **State model — DIFFERS from 37D:** the pinned skill is **per-turn** (`usePinnedSkill.ts`, cleared after send at `ExternalStoreChat.tsx:169`). Effort is **per-conversation, persisted**. Create `web/src/chat/composer/useReasoningEffort.ts` (mirror the `usePinnedSkill` lifted-seam shape to keep `ExternalStoreChat.tsx` under 600 LOC) that: (a) hydrates `effort` from the conversation DTO on `threadId` change (read the `reasoning_effort` field surfaced in §5, defaulting to `auto`); (b) exposes `effort`/`setEffort`; (c) does NOT clear on send.
- **`ExternalStoreChat.tsx`:** destructure `useReasoningEffort(threadId)`; pass `effort`/`onEffortChange` to `<Composer>` (mirror `pinnedSkill`/`onPinSkill` props at `:510-511`); include `effort` in the `streamRun({...})` call (mirror the `...(pinnedSkill !== null ? { skill: pinnedSkill.name } : {})` spread at `:151`). **Do NOT** clear effort after send.
- **`Composer.tsx`:** add the selector control near the Send affordance (Claude's discretion: a compact pill/segmented control). It reads `effort` and calls `onEffortChange`. ARIA: a labelled `role="radiogroup"`/segmented or a `<select>` with an aria-label; must NOT reclassify the input or break the existing `/`-picker combobox semantics (`:380-385`) or Enter-send/paste/drop (D-09 precedent). Add props to `ComposerProps` (`:48-55`).
- **i18n:** 5 level labels + the selector aria-label in `en` + `it` (parity CI-checked). `useTranslation` is already imported (`Composer.tsx:14`).

### Seam Map — canonical file:line index

| Concern | File:line (HEAD) | Note |
|---------|------------------|------|
| `ReasoningConfig` / `ReasoningEffort` vocab / `Request.Reasoning` / `Empty()` | `internal/llm/client.go:148-158 / 135-142 / 109 / 156` | `medium` exists here (not in `ReasoningTier`). |
| `ApplyAdaptiveReasoning` (auto path) | `internal/agent/prompt/reasoning_policy.go:38-43` | Gate stays OpenRouter-only (D-04). |
| `IsOpenRouterReasoningTarget` (generalize) | `internal/agent/prompt/reasoning_policy.go:47-53` | → refactor to call new `llm.ReasoningTarget`. |
| `ReasoningTier.reasoning()` (exclude rule to reuse) | `internal/agent/prompt/reasoning_policy.go:78-88` + `boolPtr:119` | D-10 exclude parity source. |
| `BuildWithReasoningTier` / `Build` / `buildBase` | `internal/agent/prompt/builder.go:99-104 / 90-94 / 106-119` | Add sibling `BuildWithReasoningOverride`. |
| `buildRequest` (adaptive-vs-plain selector) | `internal/agent/llm_agent.go:445-450` | Add the fixed-override branch. |
| adaptive tier compute in `Run` | `internal/agent/llm_agent.go:259-263` | Skip when a fixed override is set. |
| `adaptiveReasoningTier` (OpenRouter gate) | `internal/agent/llm_agent_reasoning.go:23-24` | Unchanged. |
| `wireRequest` / `wireReasoning` / `buildWireRequest` / `buildWireReasoning` | `internal/llm/openai_compat/client.go:71-82 / 84-89 / 220-241 / 243-253` | Add llama.cpp fields + target branch. |
| accept-both SSE reasoning | `internal/llm/openai_compat/sse.go:26-28 / 190-195` | Already handles local `reasoning`. |
| `handleRun` decode + owner gate + Turn | `internal/agui/server.go:291-392` (`GetForIdentity`:314, skill:342, mux:186) | Add effort validate + thread + persist. |
| `aura` envelope decode (mirror) | `internal/agui/server_run_request.go:11-39` | Add `Effort`. |
| `Runner.Turn` / `runTurn` / `buildAgent` / `NewLlmAgent` | `internal/runner/runner.go:297 / 315 / 537 / 552` | ctx-thread the override into config. |
| `TurnWithModelUserMessage` / `turnInput` (threading precedent) | `internal/runner/turn_model_context.go:19 / 11-15` | Pattern reference (ctx chosen over a new method). |
| `Conversation` projection (add effort) / `conversationFromRow` (map metadata) | `internal/conversations/store.go:96-110 / 131` + `store_helpers.go:22-41` | Metadata dropped today. |
| conversation queries (add update) | `internal/db/queries/conversations.sql` (rename@93 to mirror) | No update-metadata query exists. |
| `metadata jsonb` column | `internal/db/migrations/0005_conversations.up.sql:20` | Exists — NO migration (D-06). |
| `ConversationStore` interface (widen) | `internal/agui/types.go:29-59` (RenameForIdentity:49) | Add the effort-update method. |
| conversation routes / GET (hydration surface) | `internal/agui/conversations_api.go:47-60` (GET {id}:51) | Effort rides the existing DTO. |
| Settings owns model (D-01 confirm) | `internal/settings/settings.go:47-48` | `AURA_LLM_MODEL` + `AURA_LLM_BASE_URL` settings-scoped. |
| `/agent/run` auth mount | `cmd/aura/serve_webui.go:100 / ~326` | effort rides the SAME RequireAuth+capability route. |
| Frontend wire fold / options / state / composer | `web/src/chat/auraRunBody.ts:8-20` · `sseAdapter.ts:466-483` · `composer/usePinnedSkill.ts` · `ExternalStoreChat.tsx:151,169,510-511` · `Composer.tsx:48-55,370-405` | Mirror + per-conversation adaptation. |
| Live-proof harness | `scripts/deepseek_reasoning_probe.py` · `internal/llm/openai_compat/adaptive_reasoning_live_e2e_test.go` | **DRIFT:** CONTEXT.md §canonical_refs says `internal/agent/prompt/...` — the live e2e test actually lives in `internal/llm/openai_compat/`. |

---

## Backend Wire Contract

> Empirically settled — do NOT re-probe. Sources: spike 095 (llama.cpp, VALIDATED live on `gemma-4-E2B-it-qat`), spike 096 (OpenRouter/DeepSeek-V4-Flash, VALIDATED live).

### OpenRouter (UNCHANGED — Aura's current shape already handles OFF)

Aura emits `reasoning:{effort,max_tokens,exclude,enabled}` (`wireReasoning`, `client.go:84`). Spike 096 confirms:
- **OFF is reliable:** `reasoning:{effort:"none"}` **and** `reasoning:{enabled:false}` both drive `reasoning_tokens → 0` and answer directly. Aura's `off → ReasoningEffortNone` (D-03) is exactly right.
- **`exclude:true` gates visibility, not effort** (295 reasoning tokens still ran + billed with 0 CoT bytes) — confirms D-10.
- **Gradation is NOT reliable** on DeepSeek-V4-Flash: effort labels don't track (low 404 > high 303 > medium 264 reasoning tokens), and `reasoning.max_tokens` is **NOT a hard cap** (256 budget → 330 tokens). The cloud path is **effectively on/off** — the model self-scales.
- **Net effect: no OpenRouter wire change.** low/mid/high still emit `reasoning:{effort:...}` (forward-compatible with effort-trained cloud models like GPT-OSS/o-series), but they are **cosmetic on the default DeepSeek** — the UAT must say so (D-09).

### llama.cpp (NET-NEW — Aura's `reasoning:{effort}` object is a NO-OP)

Spike 095: llama-server **ignores** `reasoning:{effort:"none"}`, `reasoning:{effort:"high"}`, `reasoning:"off"`, and top-level `reasoning_effort` (all left `reasoning_content` at full length). Only two per-request fields work:
- **OFF:** `chat_template_kwargs:{enable_thinking:false}` (the only off-switch; needs `--jinja`).
- **Graduated:** `thinking_budget_tokens:N` — proven monotonic (64→214B, 128→347B, 256→612B, 1024→full). The llama.cpp webui's Low/Med/High = 512/2048/8192.

### Level → wire value (the 37E map)

| UI level (D-02) | `llm.ReasoningEffort` | OpenRouter wire (unchanged) | llama.cpp wire (net-new) |
|---|---|---|---|
| **off** | `ReasoningEffortNone` | `reasoning:{effort:"none", exclude:<!show>}` | `chat_template_kwargs:{enable_thinking:false}` |
| **low** | `ReasoningEffortLow` | `reasoning:{effort:"low", exclude:<!show>}` *(cosmetic on DeepSeek)* | `thinking_budget_tokens: 512` |
| **mid** | `ReasoningEffortMedium` | `reasoning:{effort:"medium", exclude:<!show>}` *(cosmetic)* | `thinking_budget_tokens: 2048` |
| **high** | `ReasoningEffortHigh` | `reasoning:{effort:"high", exclude:<!show>}` *(cosmetic)* | `thinking_budget_tokens: 8192` |
| **auto** | *(unset)* | *(no `reasoning` object → adaptive policy runs, OpenRouter-only)* | *(no reasoning fields → llama.cpp default thinking ON)* |

- `exclude:<!show>` = `boolPtr(!cfg.ShowReasoning)` (default `true` → hidden; cockpit stream sets showReasoning true at the translator, `server.go:391`).
- `Max` (`thinking_budget_tokens:-1`) is **NOT shipped** (D-09a).

### Server ops (document for 37E)

The llama.cpp graduated path is unlocked ONLY when llama-server runs **WITH `--jinja`** (else `chat_template_kwargs` is ignored) and **WITHOUT `--reasoning-budget`** (else per-request `thinking_budget_tokens` is locked out — llama.cpp discussion #21445; the gate is `if (reasoning_budget == -1 && body.contains("thinking_budget_tokens"))`). Validated local model: unsloth `gemma-4-E2B-it-qat` UD-Q4_K_XL (2.44 GB, GPU-fit 3606/4096 on the A2000 4 GB). Recommended launch (from spike 095): `-ngl 99 -c 4096 --temp 0 --jinja --reasoning-format auto`.

---

## Validation Architecture

> nyquist_validation is enabled (no `workflow.nyquist_validation:false` override found). This section drives VALIDATION.md.

### Test Framework

| Property | Value |
|----------|-------|
| Go framework | stdlib `testing` + table-driven; race under WSL/CI; tags `db_integration neo4j_integration` (the ONLY coverage-gate tiers — no `docker_integration` job). |
| Go quick run | `go test ./internal/llm/... ./internal/agent/... ./internal/agui/... ./internal/conversations/...` |
| Go full/coverage | `bash scripts/coverage_docker.sh` (stack up) → owned-surface floor ≥85% |
| Web unit | `vitest` (`web/`), React Testing Library — existing 37B/C/D convention |
| Web e2e | Playwright (`web/**/*.spec.ts`) |
| i18n parity | existing en+it CI parity check |

### Phase Requirements → Test Map

| Req | Behavior | Test type | Automated command | Exists? |
|-----|----------|-----------|-------------------|---------|
| WEBMODEL-02 | `parseEffortSymbol`: 5 valid symbols map correctly; unknown → `!ok`; `""`/`auto` → valid-no-override | unit (pure) | `go test ./internal/agui/ -run TestParseEffortSymbol` | ❌ Wave 0 |
| WEBMODEL-02 | `handleRun`: invalid `aura.effort` → **400**; absent/`auto` → 200 no override; foreign thread still 404 BEFORE effort | unit (httptest + fake Runner) | `go test ./internal/agui/ -run TestHandleRunEffort` | ❌ Wave 0 |
| WEBMODEL-03 | `aura.effort` decodes alongside `aura.skill`/`attachment_ids` (mirror `server_run_request_test.go:16`) | unit | `go test ./internal/agui/ -run TestDecodeRunAgentRequest` | ⚠️ extend existing |
| WEBMODEL-01 | **llama.cpp wire branch as a PURE builder test** (DAEMON-FREE): `buildWireRequest` with `Provider=llamacpp` + each effort → asserts `chat_template_kwargs:{enable_thinking:false}` (off) / `thinking_budget_tokens:512/2048/8192` (low/mid/high) / no reasoning fields (auto); and `Provider=openrouter` → today's `reasoning:{effort}` UNCHANGED | unit (pure, table) | `go test ./internal/llm/openai_compat/ -run TestBuildWireRequestReasoningTarget` | ❌ Wave 0 |
| WEBMODEL-01 | `llm.ReasoningTarget(provider,baseURL)` classifier: openrouter / llamacpp / none | unit (pure) | `go test ./internal/llm/ -run TestReasoningTarget` | ❌ Wave 0 |
| WEBMODEL-01 | `ApplyFixedReasoning`: sets `req.Reasoning{Effort,Exclude}` on a reasoning target, no-op off-target, exclude honors `cfg.ShowReasoning` (D-10) | unit (pure) | `go test ./internal/agent/prompt/ -run TestApplyFixedReasoning` | ❌ Wave 0 |
| WEBMODEL-01 | override-vs-auto seam: fixed override skips `adaptiveReasoningTier` and sets `req.Reasoning`; unset → adaptive path byte-identical | unit (agent, fake client) | `go test ./internal/agent/ -run TestReasoningOverride` | ❌ Wave 0 |
| WEBMODEL-01 | persistence round-trip: write effort via `UpdateReasoningEffortForIdentity`, read back via `GetForIdentity` → `Conversation.ReasoningEffort`; owner-scope 0-rows for a foreign id | integration (`db_integration`) | `go test -tags db_integration ./internal/conversations/ -run TestReasoningEffortRoundTrip` | ❌ Wave 0 |
| WEBMODEL-01/03 | selector renders 5 options, shows hydrated value, calls onEffortChange; does not break `/`-picker or Enter-send | web unit | `vitest run Composer` | ❌ Wave 0 |
| WEBMODEL-01 | `buildAuraRunBody` folds `effort` for fixed levels, omits for `auto` | web unit | `vitest run auraRunBody` | ⚠️ extend existing |
| WEBMODEL-01 | e2e: pick effort → send fires with `aura.effort` → reopen thread → selector restored | web e2e | `playwright test composer-effort` | ❌ Wave 0 |

### Sampling Rate (Nyquist)

- **Per task commit:** the touched package's `go test` (+ `-race` on Go pkgs) and `vitest run <module>`.
- **Per wave merge:** `go test ./internal/{llm,agent,agui,runner,conversations}/...` + `db_integration` for persistence + `vitest run` + `playwright test` for the composer.
- **Phase gate:** `bash scripts/coverage_docker.sh` green (≥85% owned-surface) + full web suite green before `/gsd-verify-work`.

### Live-proof harness (extend, do NOT gate CI on a live model)

- Existing: `scripts/deepseek_reasoning_probe.py` (the OpenRouter probe) + `internal/llm/openai_compat/adaptive_reasoning_live_e2e_test.go` (the OpenRouter live regression, env-gated — skips without a key). Spikes 095/096 are the frozen ground truth.
- **37E extension:** add a **build-tagged, env-gated** llama.cpp live e2e analog (working name `llamacpp_reasoning_live_e2e_test.go`, tag e.g. `llamacpp_live`) that, when a local llama-server URL is set, asserts OFF drives `reasoning_content → 0` and the three budgets scale monotonically — mirroring the spike-095 matrix as a regression. **It MUST be tag-isolated so CI never depends on a live GPU model** (CI has no local llama.cpp; the deepseek live test already models this env-gate pattern).

### Nyquist sampling risks (MUST carry into UAT — D-09)

- **Cloud gradation is untestable as graduation** — on DeepSeek-V4-Flash the effort labels don't track and `max_tokens` is not a cap (spike 096). Assert **on/off only** on the OpenRouter path (reasoning_tokens > 0 for on, == 0 for off); do NOT assert low < mid < high on the cloud path — that test would be flaky by design.
- **Graduated fidelity is asserted ONLY against a budget-capable target** — the monotonic `thinking_budget_tokens` scaling is asserted in the pure wire test (the request SHAPE) and in the tag-isolated local live test (the runtime EFFECT), never on the cloud path.
- **The pure wire-shape test is load-bearing for coverage** — because `docker_integration`/live-tagged tests contribute ZERO coverage in the gate (CLAUDE.md rule), the daemon-free `buildWireRequest` table test is what actually covers the llama.cpp branch. If it is skipped in favor of a live-only test, the owned-surface floor silently drops.

### Wave 0 gaps

- [ ] `internal/agui/*_test.go` — `TestParseEffortSymbol`, `TestHandleRunEffort` (extend `server_run_request_test.go` for the decode).
- [ ] `internal/llm/reasoning_target_test.go` — `TestReasoningTarget`.
- [ ] `internal/llm/openai_compat/*_test.go` — `TestBuildWireRequestReasoningTarget` (the daemon-free llama.cpp branch — coverage-load-bearing).
- [ ] `internal/agent/prompt/*_test.go` — `TestApplyFixedReasoning`.
- [ ] `internal/agent/*_test.go` — `TestReasoningOverride` (override-vs-auto seam).
- [ ] `internal/conversations/*_test.go` (`db_integration`) — `TestReasoningEffortRoundTrip`.
- [ ] `web/src/chat/composer/__tests__/` — selector unit + `useReasoningEffort` hydration; extend `auraRunBody` test.
- [ ] `web/**/*.spec.ts` — composer-effort e2e (pick → send → reopen → restored).
- [ ] Optional tag-isolated `internal/llm/openai_compat/llamacpp_reasoning_live_e2e_test.go`.

---

## Threat Model Inputs (ASVS L1)

Injection surface = the new `aura.effort` field on `POST /agent/run`.

| Pattern | STRIDE | Standard mitigation (in-scope for 37E) |
|---------|--------|----------------------------------------|
| **Enum-injection bypassing governance** — a crafted `effort` value tries to smuggle a non-enum knob (e.g. a raw provider string, `xhigh`, `minimal`, a JSON object) | Tampering / EoP | `parseEffortSymbol` accepts ONLY `{off,low,mid,high,auto}`; anything else → **400** (D-05). The client sends a **symbol**, never a `ReasoningConfig` — the server owns symbol→effort. This IS the WEBMODEL-03 no-bypass control. Unit-tested on the 400 path. |
| **Oversized / negative thinking-budget on llama.cpp** — an attacker tries to force `thinking_budget_tokens:-1` (unlimited) or a huge N for DoS | Denial of Service | The client never sends a raw budget; the server maps a **fixed symbol → a fixed const** (512/2048/8192) in the wire layer. `-1`/`Max` is NOT reachable (D-09a). No path lets a request choose an arbitrary N. |
| **Effort override crossing user isolation on a shared conversation** — B sets/persists effort on A's thread | Info Disclosure / Tampering | `handleRun` resolves ownership via `GetForIdentity` (`server.go:314`) and 404s a foreign thread BEFORE the effort is applied or persisted; the persistence write is owner-scoped (`UpdateReasoningEffortForIdentity` with `identity_id` predicate + RLS backstop, mirroring `RenameForIdentity`). Add a cross-identity deny test on the persist path. |
| **Persisted effort as a stored-value vector** — a bad value reaches `metadata` and later renders in the SPA | Tampering / XSS | Only the 5 validated symbols are ever persisted (validation precedes the write); `metadata` is written via parameterized `jsonb_set` (no string concatenation); the frontend renders the effort as a controlled selector value, not raw HTML. |
| **Body-size / malformed DoS** | DoS | `handleRun` already caps the body at `maxRunBodyBytes` (1 MiB, `server.go:28,292`) and 400s a malformed decode — the effort field inherits this. |

No new authn/authz surface: the effort field rides the existing `POST /agent/run` behind `RequireAuth` + `RequireCapability(agent.run)` (`serve_webui.go:100,~326`). ASVS V5 (input validation) is the primary applicable category and is satisfied by the enum gate; V4 (access control) is inherited from the existing owner-scoping.

---

## Open Questions / Risks

- **OQ-1 (BLOCKING for the llama.cpp branch) — how is a llama.cpp backend positively identified at request time?** `Provider` defaults to `"openrouter"` and is currently settable ONLY via `~/.aura/llm.json` (no env override — `applyEnvOverrides` sets Model/BaseURL but never Provider). A baseURL heuristic is fragile: the DGX/vLLM local path also emits `reasoning` (`sse.go:22`) and is also non-openrouter.ai. **Recommendation:** add an explicit provider knob — env `AURA_LLM_PROVIDER` + a settings entry `AURA_LLM_PROVIDER` in `AllowedKeys` (`settings.go:46`) — and key `llm.ReasoningTarget` on `Provider == "llamacpp"`. This lets the operator switch the whole backend (model + base URL + provider) from the Settings page, consistent with D-01. The planner must lock this recognition rule before the wire branch is coded.
- **OQ-2 — persist-on-send vs. persist-on-change.** D-06 wants restore-on-reopen (Claude parity). Persist-on-send (in `handleRun`) is minimal but loses a change the user makes without sending. A dedicated `POST /api/conversations/{id}/reasoning-effort` closes that gap. **Recommendation:** ship persist-on-send; add the PATCH only if UAT shows the gap matters. Planner decides.
- **OQ-3 — sending `auto` on the wire vs. omitting it.** Run semantics want `auto` = absent = no override (D-05). Persistence wants `auto` storable (to reset a prior `high`). **Resolution baked into §5/§6:** the run body OMITS `auto` (pure D-05), and persistence stores the current symbol including `auto` — via persist-on-send writing the symbol the composer holds, or via the OQ-2 PATCH. Confirm the composer always knows its current symbol (it does — it is the hydrated state).
- **RISK — `auto` on a llama.cpp default backend yields no adaptive tiering.** Because the adaptive path stays OpenRouter-only (D-04 "unchanged"), a llama.cpp operator on `auto` gets llama.cpp's default (thinking always ON), not Aura's tiered policy. This is consistent with "auto = self-adapt" and is NOT a regression (the effort object was already ignored on llama.cpp), but the UAT/PRD should note it so it is not mistaken for a bug.
- **RISK — 600-LOC caps.** `ExternalStoreChat.tsx` (~519 LOC) and `sseAdapter.ts` (at the cap) are near the limit; the effort state/hook MUST be extracted (`useReasoningEffort.ts`), mirroring the 37D `usePinnedSkill.ts`/`auraRunBody.ts` extractions. `reasoning_policy.go` (119 LOC) and `builder.go` (119 LOC) have headroom for `ApplyFixedReasoning`/`BuildWithReasoningOverride`.
- **DRIFT (documented) — live e2e path.** CONTEXT.md §canonical_refs cites `internal/agent/prompt/adaptive_reasoning_live_e2e_test.go`; the file actually lives at `internal/llm/openai_compat/adaptive_reasoning_live_e2e_test.go`. Use the real path.
- **CONTRADICTION with a spike recommendation (resolved by D-09/D-09a) — do NOT "unify by token budget".** Spike 095's write-up recommended defining levels by token budget and using `reasoning.max_tokens` to unify both backends. Spike 096 **refuted** that: DeepSeek does NOT honor `reasoning.max_tokens` as a cap. CONTEXT.md D-09 and this research follow the refutation — the cloud path stays effort-string/on-off; only llama.cpp uses `thinking_budget_tokens`. The planner must not resurrect the token-budget-unification idea.

---

## Environment Availability

| Dependency | Required by | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| OpenRouter (`OPENROUTER_API_KEY`) | Default backend; the `off/low/mid/high` OpenRouter path + the existing live e2e | ✓ (operator has it) | — | Existing e2e env-gate skips without it |
| Local llama-server (`gemma-4-E2B-it-qat`, `--jinja`, no `--reasoning-budget`) | D-08 graduated path + the OPTIONAL tag-isolated live test | ✗ in CI, available on the operator's A2000 box | server-cuda ≥ spike-095 image | **CI never depends on it** — the daemon-free `buildWireRequest` test covers the branch; the live test is tag-isolated + env-gated |
| Postgres (`aura.*`, `db_integration`) | persistence round-trip test | ✓ (WSL/CI stack) | 11 migrations | — |
| Go 1.26 toolchain + `-race` | all Go unit/integration | ✓ (WSL primary) | — | — |
| Node/vitest/Playwright | web unit + e2e | ✓ (`web/`) | existing | — |

**No blocking missing dependency.** The only non-CI dependency (a live llama-server) is intentionally kept off the CI path; the wire branch is proven by a pure test and frozen by spike 095.

---

## Assumptions Log

| # | Claim | Section | Risk if wrong |
|---|-------|---------|---------------|
| A1 | `Provider == "llamacpp"` is the right recognition key (vs a baseURL heuristic), and adding an `AURA_LLM_PROVIDER` env+settings knob is acceptable | Seam Map §2, OQ-1 | If the operator's llama.cpp deployment sets provider differently, the wire branch won't fire; mitigated by making it an explicit, documented knob the planner locks. `[ASSUMED]` |
| A2 | The effort→budget constants (512/2048/8192) belong in `openai_compat` as consts for the first cut (not config) | Seam Map §3 | Low — spike-095-validated; promotable to config later without a contract change. `[VERIFIED: spike 095]` for the values; `[ASSUMED]` for the location. |
| A3 | Persist-on-send in `handleRun` is sufficient for the D-06 restore-on-reopen acceptance (the dedicated change-without-send PATCH is optional) | Seam Map §5, OQ-2 | If UAT demands strict Claude change-without-send persistence, add the PATCH endpoint (small, additive). `[ASSUMED]` |
| A4 | Surfacing `reasoning_effort` on the existing `Conversation` DTO is an acceptable hydration channel (no new GET endpoint) | Seam Map §5/§6 | Low — the SPA already consumes this projection on open. `[VERIFIED: conversations_api.go + queries]` |
| A5 | ctx-threading the override (vs a new `Turn*` interface method) is the cleaner composition with the existing `TurnWithModelUserMessage` split | Seam Map §1 | If the team prefers an explicit method, a `reasoningOverrideRunner` optional interface (mirror `modelUserMessageRunner`) is the alternative; both are non-breaking. `[ASSUMED]` |

---

## Sources

### Primary (HIGH confidence)
- Spike 095 `.planning/spikes/095-llama-cpp-reasoning-effort-wire-contract/README.md` — VALIDATED llama.cpp per-request contract (`enable_thinking:false`, `thinking_budget_tokens:N`; OpenRouter object ignored; `--jinja` + no `--reasoning-budget`).
- Spike 096 `.planning/spikes/096-openrouter-reasoning-effort-wire-contract/README.md` — VALIDATED OpenRouter/DeepSeek contract (OFF reliable two ways; gradation + `max_tokens` NOT reliable; refutes token-budget unification).
- CONTEXT.md D-01..D-11, D-09a — user decisions (truth-source).
- Read + line-verified HEAD code: `internal/llm/{client.go,config.go}`, `internal/llm/openai_compat/{client.go,sse.go}`, `internal/agent/prompt/{reasoning_policy.go,builder.go,reasoning_classifier.go}`, `internal/agent/{llm_agent.go,llm_agent_reasoning.go}`, `internal/agui/{server.go,server_run_request.go,conversations_api.go,types.go}`, `internal/runner/{runner.go,turn_model_context.go}`, `internal/conversations/{store.go,store_helpers.go}`, `internal/db/{queries/conversations.sql,migrations/0005_conversations.up.sql}`, `internal/settings/settings.go`, `cmd/aura/serve_webui.go`, `web/src/chat/{Composer.tsx,ExternalStoreChat.tsx,auraRunBody.ts,sseAdapter.ts,composer/usePinnedSkill.ts}`.
- `.claude/skills/spike-findings-Aura/SKILL.md` — reasoning-effort-selector finding (backend-dependent gradation; net-new llama.cpp branch).

### Secondary (MEDIUM confidence)
- `.planning/phases/37D-composer-skill-picker/37D-CONTEXT.md` — the send-payload / `aura` envelope / ARIA / i18n / ≥85% + Playwright pattern to mirror.
- llama.cpp discussion #21445 (via spike 095) — `thinking_budget_tokens` gating on the server flags.

### Tertiary (LOW confidence)
- None. The wire contract is spike-VALIDATED; the code seams are read, not assumed.

## Metadata

**Confidence breakdown:**
- Backend wire contract: HIGH — two VALIDATED live spikes, frozen.
- Code seam map: HIGH — every touch point read + line-verified against HEAD.
- Override-seam recommendation: MEDIUM-HIGH — a clean composition, but ctx-vs-method and llama.cpp recognition (OQ-1) are planner locks.
- Persistence: HIGH — column + read queries confirmed; the missing write query + dropped projection are precisely identified.
- Validation architecture: HIGH — maps to real, runnable commands; coverage-gate rule (daemon-free wire test) accounted for.

**Research date:** 2026-07-10
**Valid until:** 2026-08-09 (30 days — stable internal seams; the only external truth is the spike-frozen wire contract).


---
---

# Pass 2 Addendum — Capability Auto-Detection (D-12 / D-13) + 7-Level Set

> **Appended 2026-07-10 after two operator directives grew the phase.** This addendum is ADDITIVE — every pass-1 finding above (Seam Map, override-seam recommendation, persistence, frontend, threat model) STILL STANDS. Where a pass-1 table conflicts with this addendum, **this addendum governs.**

## Supersession Map (what pass-1 content this revises)

| Pass-1 location | Status | Superseded by |
|-----------------|--------|---------------|
| `<user_constraints>` **D-02** (5 levels `off/low/mid/high/auto`) | **SUPERSEDED** | Revised D-02 — **7 levels** `auto·off·low·mid·high·extra·max` (§P2.1) |
| `<user_constraints>` **D-03** (5-row level map) | **SUPERSEDED** | Revised D-03 — adds `extra→xhigh`, `max→max`/`-1` (§P2.1) |
| `<user_constraints>` **D-05** (single-stage enum → 400) | **EXTENDED** | Revised D-05 — **two-stage** (enum + capability) → 400 (§P2.5) |
| Backend Wire Contract "Level → wire value" table | **SUPERSEDED** | 7-level table (§P2.1) |
| Seam Map §1 `parseEffortSymbol` (5 symbols) | **SUPERSEDED** | 7 symbols (§P2.4) |
| Seam Map §6 `buildAuraRunBody` fold + i18n (5 labels) | **EXTENDED** | 6 fixed levels folded + 7 labels; **selector is now capability-driven, not a static list** (§P2.6) |
| Open Questions — **D-09a** ("Max is NOT added") | **STALE / CONTRADICTED** | Revised D-02 explicitly ADDS `extra`+`max` and supersedes the 5-level set. D-09a (CONTEXT line 63) predates the 2026-07-10 revision — the PRD-amendment (D-11) MUST delete/supersede it (§P2.OQ). |

## P2.1 — Revised level set & wire map (7 levels)

**Full vocabulary (D-02 revised):** `auto · off · low · mid · high · extra · max`. `auto` is Aura's adaptive default (retained). The set actually **shown/honored is auto-detected per active model** (D-13) — never the full 7 unconditionally, never a placebo (D-12).

| UI level | `llm.ReasoningEffort` | OpenRouter wire (unchanged shape) | llama.cpp wire (spike 095) |
|---|---|---|---|
| **auto** | *(unset)* | no `reasoning` object → adaptive policy (OpenRouter-only) or model `default_effort` | no reasoning fields → llama.cpp default |
| **off** | `ReasoningEffortNone` | `reasoning:{effort:"none"}` / `{enabled:false}` | `chat_template_kwargs:{enable_thinking:false}` |
| **low** | `ReasoningEffortLow` | `reasoning:{effort:"low"}` | `thinking_budget_tokens: 512` |
| **mid** | `ReasoningEffortMedium` | `reasoning:{effort:"medium"}` | `thinking_budget_tokens: 2048` |
| **high** | `ReasoningEffortHigh` | `reasoning:{effort:"high"}` | `thinking_budget_tokens: 8192` |
| **extra** | `ReasoningEffortXHigh` (**exists**) | `reasoning:{effort:"xhigh"}` | `thinking_budget_tokens: 16384` *(planner-tunable)* |
| **max** | `ReasoningEffortMax` (**NET-NEW const**) | `reasoning:{effort:"max"}` | `thinking_budget_tokens: -1` (unlimited) |

`exclude` on the OpenRouter rows is `boolPtr(!cfg.ShowReasoning)` exactly as before (D-10). The OpenRouter wire SHAPE is still unchanged — `buildWireReasoning` already serializes `Effort: string(r.Effort)`, so `xhigh`/`max` project automatically once the `max` const exists (§P2.4). The llama.cpp branch (pass-1 Seam Map §3) gains `extra`/`max` budget rows.

## P2.4 — Vocab reconciliation (RESEARCH item D)

Read `internal/llm/client.go:135-142` — the `ReasoningEffort` const block is `xhigh · high · medium · low · minimal · none`.

- **`extra → xhigh`: the const ALREADY EXISTS** (`ReasoningEffortXHigh = "xhigh"`, client.go:136). No change.
- **`max`: NET-NEW.** Add `ReasoningEffortMax ReasoningEffort = "max"` to the const block. `"max"` is OpenRouter's own token (in `supported_efforts`), so it serializes 1:1 on the cloud wire; on llama.cpp the wire branch maps it to `thinking_budget_tokens:-1`.
- **`minimal`** stays (harmless, unused by 37E; not in the UI set and not in OpenRouter's `supported_efforts`).
- **Symbol vs. effort:** keep the UI/wire symbols (`mid`, `extra`, `max`) distinct from the internal `llm.ReasoningEffort` (`medium`, `xhigh`, `max`) in the server map. `parseEffortSymbol` (§P2.5) owns the translation.
- **Direct-set path unaffected:** `ReasoningConfig.Empty()` (client.go:156) already returns false for any non-empty `Effort`, so a `max`/`xhigh` override flows through the existing seam.

## P2.2 — Capability Auto-Detection subsystem (RESEARCH items A + B)

### The "before" picture (why this is net-new)

- `internal/llm/models.go` is a **HARD-CODED** `modelCapabilityTable` (vision/audio) — exactly the anti-pattern D-13 forbids for reasoning. Do NOT extend it with a reasoning column.
- `internal/llm/prices.go` `defaultPrices()` is likewise a HARD-CODED seed — there is **no existing OpenRouter `/models` fetch anywhere in the codebase.** The capability client is fully net-new.
- **Reusable:** `normalizeModelID(model)` (models.go:32) strips the `:nitro`/`:flash` routing suffix so `AURA_LLM_MODEL="deepseek/deepseek-v4-flash:nitro"` resolves to the `/models` list key `deepseek/deepseek-v4-flash`. Reuse it verbatim as the cache key.

### A. OpenRouter models-capability client

**How Aura talks to OpenRouter today:** `openai_compat.Client` (client.go:35-65) holds `httpClient *http.Client` + `cfg` and POSTs `cfg.BaseURL+"/chat/completions"` with `Authorization: Bearer cfg.APIKey` + `cfg.Headers` (attribution). It is **streaming-only**; there is no non-streaming GET. The models fetch is a **new, separate lightweight client** in `internal/llm` (respects ≤600 LOC — put it in a new file `internal/llm/model_reasoning_caps.go`, or a tiny subpackage `internal/llm/modelcaps` if it grows; NOT inside `openai_compat`, which is the wire/streaming layer).

**Fetch + parse (verified-live shape, D-13 / operator probe 2026-07-10):**
```go
// internal/llm/model_reasoning_caps.go  (package llm)

// ReasoningCapability is one model's advertised reasoning surface from GET /models.
type ReasoningCapability struct {
    SupportedEfforts []ReasoningEffort // subset of {max,xhigh,high,medium,low,none}
    DefaultEffort    ReasoningEffort   // "" when absent
    DefaultEnabled   bool
    Mandatory        bool              // true => reasoning cannot be turned off ("off" invalid)
    SupportedParams  []string          // reasoning / include_reasoning / reasoning_effort
}

// wire DTO for GET {BaseURL}/models  (CONFIRM exact nesting against a captured fixture — Wave 0)
type openRouterModelsResponse struct {
    Data []struct {
        ID        string `json:"id"`
        Reasoning *struct {
            SupportedEfforts []string `json:"supported_efforts"`
            DefaultEffort    string   `json:"default_effort"`
            DefaultEnabled   bool     `json:"default_enabled"`
            Mandatory        bool     `json:"mandatory"`
        } `json:"reasoning"`
        SupportedParameters []string `json:"supported_parameters"`
    } `json:"data"`
}

type ModelCapabilityClient struct {
    cfg        Config        // reuses BaseURL + APIKey + Headers
    httpClient *http.Client
    ttl        time.Duration
    now        func() time.Time         // injectable clock for cache-expiry tests
    mu         sync.Mutex
    cache      map[string]ReasoningCapability // keyed by normalizeModelID(id)
    fetchedAt  time.Time
    ok         bool
}

func NewModelCapabilityClient(cfg Config, ttl time.Duration) *ModelCapabilityClient

// ReasoningCapabilityFor fetches+caches the /models list on a cold/expired cache and
// returns the active model's advertised capability. ok=false when the model is absent
// or the fetch failed (caller shows the safe fallback set, §P2.H). NEVER per-turn: the
// TTL cache serves handleRun and the capability endpoint from memory.
func (c *ModelCapabilityClient) ReasoningCapabilityFor(ctx context.Context, model string) (ReasoningCapability, bool, error)
```

**Endpoint:** `GET {cfg.BaseURL}/models` (i.e. `https://openrouter.ai/api/v1/models`). The full list is large (~400+ models, multi-MB) → **warm once at boot + refresh on a long TTL** (recommend 6–24h; capabilities change rarely and the active model is boot-stable because `AURA_LLM_MODEL` changes take effect only on restart — settings_api.go). Cap the response body (defensive) and parse with `json.Decoder`. **Per-endpoint fallback:** if the top-level `/models` object lacks `reasoning` for the active model, probe `GET /api/v1/models/{author}/{slug}/endpoints` for per-endpoint detail (a secondary, single-model fetch) — flag as a planner option, not required for v1.

**Active-model key:** resolve the effective `AURA_LLM_MODEL` via the existing settings→env resolver (`effectiveSettingValue(ctx, "AURA_LLM_MODEL")`, settings_api.go:263) OR simply use `cfg.Model` (boot-resolved), then `normalizeModelID(...)`. Confirm the `/models` `id` is the base id, not the `:nitro` routing variant (the fixture capture will show this).

### B. Local llama.cpp capability source

**`GET {baseURL}/props` returns** (verified against the official llama.cpp server README): `default_generation_settings`, `total_slots`, `model_path`, **`chat_template`** (raw Jinja2), **`chat_template_caps`** (capability flags parsed from `common/jinja/caps.h`), `modalities` (e.g. `{"vision":false}`), `media_marker`, `build_info`, `is_sleeping`. It does **NOT** expose whether the server was launched with `--jinja` or `--reasoning-budget` — so `/props` alone **cannot confirm** the graduated `thinking_budget_tokens` path is unlocked.

**Recommendation (ties to pass-1 OQ-1 — positive llama.cpp identification):** derive the local capability set from **explicit config** (`AURA_LLM_PROVIDER=llamacpp`, the new knob from pass-1 Seam Map §2) **plus the documented spike-095 ops contract** (`--jinja` on, `--reasoning-budget` off → the full graduated set `{auto,off,low,mid,high,extra,max}` is spike-validated). Optionally **narrow** via a best-effort `/props` probe: if `chat_template_caps` exposes a thinking/reasoning capability flag AND it is false, drop the graduated levels to `{auto,off}`. If `/props` is unreachable or the flag name is unknown, **trust the provider config + ops contract** (the operator who sets `provider=llamacpp` is asserting the spike-095 launch config).

> **Wave-0 verification task:** the exact `chat_template_caps` field that signals thinking support is undocumented — capture a real `/props` from the pinned spike-095 `server-cuda` image (`gemma-4-E2B-it-qat`, `--jinja`) into `testdata/llamacpp_props.json` and confirm the flag name. Do NOT block the design on it — the provider+ops-contract fallback is authoritative; `/props` is a best-effort narrowing.

```go
// internal/llm/model_reasoning_caps.go (or a llamacpp_caps.go sibling)
type llamaCppPropsResponse struct {
    ChatTemplate     string          `json:"chat_template"`
    ChatTemplateCaps map[string]any  `json:"chat_template_caps"` // parse defensively; look for a thinking/reasoning flag
    Modalities       map[string]bool `json:"modalities"`
}
```

### Unifying seam (used by both the endpoint and the validator)

```go
// internal/llm — the neutral seam; selected at boot by llm.ReasoningTarget(cfg.Provider, cfg.BaseURL).
type ReasoningCapabilitySource interface {
    // AllowedEfforts returns the internal ReasoningEffort set the ACTIVE model advertises
    // + its default; detected=false => the caller shows the safe fallback set (§P2.H).
    AllowedEfforts(ctx context.Context) (efforts []ReasoningEffort, deflt ReasoningEffort, detected bool)
}
```
Two impls: `openRouterReasoningCaps` (wraps `ModelCapabilityClient`, maps `supported_efforts`→`[]ReasoningEffort`, honors `mandatory`/`default_effort`) and `llamaCppReasoningCaps` (provider+ops-contract, optional `/props` narrowing). The `auto` symbol is prepended by the endpoint/UI layer (it is not a `ReasoningEffort`).

## P2.C — Capability endpoint to the Composer (RESEARCH item C)

Mirror the 37D composer read route (`GET /api/composer/skills`, mounted behind **plain `RequireAuth`** via `registerComposerRoutes` in `composer_api.go` — NOT `governance.read`; ordinary identities must reach it).

- **Route:** `GET /api/composer/reasoning-capabilities` (add to `registerComposerRoutes`).
- **DTO:**
  ```go
  type reasoningCapabilitiesDTO struct {
      Levels   []string `json:"levels"`    // UI symbols the active model allows, e.g. ["auto","off","low","mid","high"]
      Default  string   `json:"default"`   // "auto"
      Backend  string   `json:"backend"`   // "openrouter" | "llamacpp"
      Detected bool     `json:"detected"`  // false => safe fallback set shown (UI can hint "capabilities unverified")
  }
  ```
  The handler maps the source's `[]llm.ReasoningEffort` (none/low/medium/high/xhigh/max) → UI symbols (off/low/mid/high/extra/max), prepends `auto`, and — when the model's reasoning is `mandatory` — OMITS `off`.
- **Setter-injection:** `func (s *Server) SetReasoningCapabilitySource(src llm.ReasoningCapabilitySource)` on the agui `Server` (mirror `SetSettingsStore`, settings_api.go:53). Wired by the daemon composition root (`serve`) after `NewServer`, alongside the other `Set*` calls. When nil → the handler returns the **safe fallback** (`levels:["auto","off"], detected:false`), never 503 (the composer must degrade, 37D D-09).
- **Identity-scoping:** the capability is a property of the process-global ACTIVE MODEL (operator-scoped, one per deployment), identical for every authenticated identity — so `RequireAuth` (no anonymous) is the only gate needed; there is no per-identity model to leak. Return ONLY the allowed symbols + default + backend kind — never the full models list, the model id, base URLs, or the API key (Threat §P2.G).

## P2.5 — Two-stage server validation (RESEARCH item E, revised D-05)

`handleRun` (pass-1 Seam Map §4) now runs BOTH stages, in order, after the owner-scope `GetForIdentity` gate:

```go
// Stage 1 — SYNTACTIC (pure, no I/O): the 7-symbol enum.
effort, isFixed, ok := parseEffortSymbol(req.Aura.Effort)
//  "" | "auto"                      -> ("", false, true)      valid, no override
//  "off"                            -> (ReasoningEffortNone,   true, true)
//  "low"|"mid"|"high"|"extra"|"max" -> (Low|Medium|High|XHigh|Max, true, true)
//  anything else                    -> ("", false, false)     -> 400
if !ok { http.Error(w, "invalid reasoning effort", 400); return }

// Stage 2 — CAPABILITY (cached, cheap): the fixed level must be advertised by the ACTIVE model.
if isFixed && s.reasoningCaps != nil {
    allowed, _, detected := s.reasoningCaps.AllowedEfforts(ctx) // in-memory TTL cache; NO per-request OpenRouter round-trip
    if detected {
        if !contains(allowed, effort) { http.Error(w, "effort not supported by the active model", 400); return } // D-13
    } else {
        if effort != ReasoningEffortNone { http.Error(w, "effort not verifiable; only off/auto available", 400); return } // safe floor, D-12
    }
}
// then thread + persist exactly as pass-1 Seam Map §1/§5.
```

- **Same cached source as the endpoint** — the `/models` fetch is TTL-cached in `ModelCapabilityClient`, so Stage 2 is an in-memory lookup, never a live round-trip per turn (the no-per-turn-fetch requirement).
- **`mandatory` models:** when `AllowedEfforts` reflects `mandatory:true`, `off` is not in `allowed` → a client that sends `off` gets 400. Correct (the model always reasons).
- **This is the WEBMODEL-03 no-bypass control, tightened:** the client sends a symbol; the server rejects both non-enum values AND real-enum-but-not-advertised values → the UI can never request a placebo (D-12).

## P2.6 — Frontend: dynamic, capability-driven selector (extends pass-1 Seam Map §6)

- **New read hook** `web/src/chat/composer/useReasoningCapabilities.ts` — `GET /api/composer/reasoning-capabilities` (mirror `useComposerSkills.ts`), returns `{levels, default, detected}`. On fetch failure → `{levels:['auto','off'], detected:false}` (degrade, never break — 37D D-09).
- **Selector renders `levels` DYNAMICALLY** — NOT a hard-coded 7. This is the D-13 core: the Composer shows exactly what the active model advertises. `useReasoningEffort(threadId)` (pass-1 §6) still owns the per-conversation persisted value; on hydrate, clamp a stored value not in `levels` back to `auto` (a model change between sessions must not show a now-unsupported level).
- **`buildAuraRunBody`** (auraRunBody.ts): fold `effort` for the **six** fixed levels, omit `auto`: `...(opts.effort && opts.effort !== 'auto' ? { effort: opts.effort } : {})`.
- **i18n:** up to **7** labels (`auto/off/low/mid/high/extra/max`) in en+it (CI parity). Suggested it: `auto/off/basso/medio/alto/extra/max` (the operator's Claude reference used `Basso/Medio/Alto/Extra/Max`).
- **`StreamRunOptions`** gains `effort?: string` (unchanged from pass-1).

## Validation Architecture — Capability-Detection additions (RESEARCH item F)

Append to the pass-1 `## Validation Architecture`. **All daemon-free unit tests** (CLAUDE.md coverage gate: the gate runs `db_integration neo4j_integration` only — these must be pure `go test` to count toward the ≥85% floor; NONE may be container- or live-gated).

| Req | Behavior | Test type | Command | Exists? |
|-----|----------|-----------|---------|---------|
| WEBMODEL-01/03 | OpenRouter `/models` JSON parse from a **captured fixture** → `supported_efforts`/`default_effort`/`mandatory`; `supported_efforts`→`ReasoningEffort` clamp (ignore unknown tokens) | unit (pure) | `go test ./internal/llm/ -run TestParseModelReasoningCaps` | ❌ Wave 0 |
| WEBMODEL-01 | cache TTL: cold fetch (fake `http.RoundTripper`), warm hit (no 2nd call), expiry re-fetch (injected clock) | unit (pure) | `go test ./internal/llm/ -run TestModelCapabilityCacheTTL` | ❌ Wave 0 |
| WEBMODEL-01 | `normalizeModelID` maps `deepseek/...:nitro` → the `/models` base key | unit (pure) | `go test ./internal/llm/ -run TestReasoningCapKey` | ⚠️ reuse models_test.go |
| WEBMODEL-01 | llama.cpp `/props` parse (fixture) + provider+ops-contract fallback when `/props` absent/unknown flag | unit (pure) | `go test ./internal/llm/ -run TestLlamaCppReasoningCaps` | ❌ Wave 0 |
| WEBMODEL-03 | **two-stage 400**: fixed level ∉ advertised set → 400; advertised → 200; `mandatory` + `off` → 400; `detected==false` + graduated → 400; `off`/`auto` always pass | unit (httptest + **fake** `ReasoningCapabilitySource`) | `go test ./internal/agui/ -run TestHandleRunEffortCapability` | ❌ Wave 0 |
| WEBMODEL-01 | capability endpoint: maps efforts→UI symbols, prepends `auto`, omits `off` when mandatory, nil source → safe fallback (not 503) | unit (httptest) | `go test ./internal/agui/ -run TestReasoningCapabilitiesEndpoint` | ❌ Wave 0 |
| WEBMODEL-01 | `useReasoningCapabilities` fetch + degrade; selector renders detected levels only; clamp unsupported stored value → auto | web unit | `vitest run reasoningCapabilities` | ❌ Wave 0 |
| WEBMODEL-01 | e2e: model advertising `{none,low,high}` shows exactly those (+auto) — not the full 7 | web e2e | `playwright test composer-effort-caps` | ❌ Wave 0 |

- **CI never depends on live OpenRouter:** the models client's HTTP layer is tested via an injected `http.RoundTripper` returning a **captured fixture** (`testdata/openrouter_models.json` — Wave-0 task: `curl -s https://openrouter.ai/api/v1/models | jq '.data |= .[0:4]' > testdata/openrouter_models.json`, keep one reasoning model, one `mandatory`, one no-reasoning). The `handleRun`/endpoint tests use a **fake `ReasoningCapabilitySource`**. No test hits the network.
- **Fixture-capture is Wave 0** for BOTH `openrouter_models.json` and `llamacpp_props.json` (the exact `chat_template_caps` flag). The parse tests are written against the real shapes, not hand-invented JSON.

## Threat Model Inputs — Capability-Detection additions (RESEARCH item G)

Append to the pass-1 `## Threat Model Inputs`.

| Pattern | STRIDE | Mitigation |
|---------|--------|-----------|
| **OpenRouter `/models` unavailable / slow** (new external dependency) | DoS / Availability | The fetch is TTL-cached + warmed at boot, never per-turn; on failure the source returns `detected=false` → the selector shows the safe floor `{auto,off}` and Stage-2 rejects graduated levels (400). The feature degrades, never breaks (D-13 fallback). Body-size-cap the response. |
| **Malicious / garbage `supported_efforts`** (a hostile or buggy upstream injects a non-vocab effort) | Tampering | Parse defensively: map each token through a strict `{max,xhigh,high,medium,low,none}` allowlist and DROP unknowns — a garbage token can never enter the validator's allowed set. The response is TLS + the operator's own key (same trust boundary as `/chat/completions`). |
| **Stale capability (cache lag)** | — (accepted) | Advertised support is best-effort (D-13); a long TTL may lag a model's capability change by hours. Acceptable — the wire knob is still real (D-12); worst case a just-added level is briefly unavailable until refresh. Documented, not a vuln. |
| **Capability endpoint info-leak** | Info Disclosure | `RequireAuth`-gated (no anonymous); returns ONLY the allowed UI symbols + default + backend kind — never the full models list, the model id, base URLs, or the API key. In multi-user, omitting the model id avoids leaking the operator's model choice to a non-operator identity. |
| **Capability-check bypass** (client claims a level the model lacks) | EoP / Tampering | Stage-2 server validation (§P2.5) rejects any fixed level not in the active model's advertised set → 400. The client's selector is advisory; the server is authoritative. This is the D-12/D-13 no-placebo enforcement. |

ASVS: still primarily **V5 (input validation)** — now two-stage. The new external dependency adds a **V1/V10 (communication / malicious-input-from-a-dependency)** consideration, handled by the defensive parse + allowlist clamp + fail-safe fallback.

## Open Questions / Risks — Capability-Detection additions (RESEARCH item H + more)

- **OQ-4 (fallback UX when detection fails) — RECOMMENDATION + open point.** On any detection failure (OpenRouter unreachable, parse error, cold-cache fetch fail, `/props` unreachable), the selector shows the **universally-real floor: `{auto, off}`** (both spike-validated on both backends) with `detected:false` so the UI can show a subtle "capabilities unverified" hint. Stage-2 rejects graduated levels while undetected (400). **Open point for the planner:** should the **llama.cpp** fallback be WIDENED to the full graduated `{auto,off,low,mid,high,extra,max}` when `AURA_LLM_PROVIDER=llamacpp` is EXPLICITLY configured (spike-095 validated the whole scale) even if the `/props` probe fails? Recommendation: **yes** — an explicit `provider=llamacpp` is the operator asserting the spike-095 launch config, so trust it; keep `{auto,off}` only for the "unknown backend" case. Confirm at plan time.
- **OQ-5 (D-09a is STALE — MUST be reconciled in the PRD-amendment).** CONTEXT line 63 (D-09a) says "37E ships EXACTLY `off/low/mid/high/auto`; `Max` is NOT added." The revised D-02 (line 22, same date, marked "REVISED 2026-07-10") explicitly ADDS `extra`+`max` and says it "Supersedes the earlier GPT-style 5-level." **D-09a directly contradicts the revised D-02 and is stale.** The Wave-1 PRD-amendment (D-11, which "ALSO covers D-12 and D-13") must delete/supersede D-09a so the plan and PRD agree on the 7-level set. Flag prominently — a planner reading D-09a literally would ship the wrong set.
- **OQ-6 (exact `/models` nesting + `/props` cap flag) — Wave-0 fixture capture, not a blocker.** The operator live-verified the field NAMES (`reasoning.supported_efforts`, `default_effort`, `default_enabled`, `mandatory`, `supported_parameters`); the exact JSON nesting (`data[].reasoning...` vs `data[].top_provider...`) and the `chat_template_caps` thinking-flag name must be pinned from CAPTURED fixtures at Wave 0 (§Validation). Low risk — the shape is authoritatively verified; only the byte-exact fixture remains.
- **OQ-7 (mandatory/default_enabled semantics).** Some models advertise `mandatory:true` (reasoning cannot be disabled) or `default_enabled:false`. The endpoint omits `off` for mandatory models and the validator rejects `off` (400). Confirm the desired UX: hide `off` vs. show-it-disabled. Recommendation: hide (the selector shows only real options — D-12/D-13 consistency).
- **RISK — model-switch staleness.** `AURA_LLM_MODEL` changes take effect on restart (settings overlay at boot); the capability source is keyed on the boot model. If a future change makes model switching hot, the capability cache must be invalidated on switch. Not a 37E concern (model is boot-stable today) but note it so the cache isn't assumed hot-swappable.

## Phase Sizing Signal (RESEARCH item I)

37E roughly **doubled** vs. pass 1. The work now spans five cohesive verticals:

1. **PRD-amendment gate** (D-11, now also covering D-12/D-13 + the D-09a reconciliation) — Wave 1, blocks all code.
2. **Effort engine** — `ReasoningEffortMax` const, generalized `llm.ReasoningTarget`, `AURA_LLM_PROVIDER` knob, the override seam (ctx→config→`ApplyFixedReasoning`), the llama.cpp wire branch. (pass-1 §1–3 + §P2.1/§P2.4)
3. **Capability-detection subsystem** — `ModelCapabilityClient` (fetch + parse + TTL cache), llama.cpp `/props` source, the `ReasoningCapabilitySource` seam, fixture capture. (§P2.2) — a self-contained NEW external-dependency vertical.
4. **Governance + endpoint** — two-stage `handleRun` validation, `GET /api/composer/reasoning-capabilities`, setter-injection. (§P2.5/§P2.C)
5. **UI + persistence** — dynamic capability-driven selector, per-conversation persist/hydrate (pass-1 §5/§6), i18n (7), vitest + Playwright.

**Estimate as ONE phase:** ~7–9 plans across **4 waves** (W1 PRD-amendment; W2 effort engine [vocab, target recognition, wire branch, override seam]; W3 capability subsystem + endpoint + two-stage validation + persistence; W4 frontend + e2e). That is a large-but-coherent phase.

**Recommendation — split at the capability-endpoint boundary if the plan-count exceeds ~8 or any wave exceeds ~4 parallel plans:**
- **37E-a "reasoning-effort engine + capability detection"** (verticals 1–4): all backend, testable daemon-free + `db_integration`, ships the `/agent/run` effort field + the capability endpoint + persistence store. Independently valuable and fully UAT-able via curl.
- **37E-b "composer effort selector"** (vertical 5): the dynamic UI + e2e, consuming the endpoint from 37E-a.

The two are tightly coupled (the UI is meaningless without the endpoint; the endpoint is only end-to-end-provable with the UI), so a single phase is defensible. **Lean: split into 37E-a / 37E-b** — the capability-detection subsystem (a net-new external dependency with its own cache, fixtures, and failure modes) is a clean seam and shipping the engine first de-risks the UI. Final call is the planner's against the team's per-phase plan-count norm.

## Pass-2 Sources

### Primary (HIGH)
- Operator live probe of `GET https://openrouter.ai/api/v1/models` (2026-07-10, relayed by the coordinator) — per-model `reasoning.{supported_efforts,default_effort,default_enabled,mandatory}` + `supported_parameters`. `[VERIFIED: operator live probe 2026-07-10]` (byte-exact fixture to capture at Wave 0).
- llama.cpp server README `/props` schema (WebFetch, official ggml-org repo) — `chat_template`, `chat_template_caps`, `modalities`, `default_generation_settings`; does NOT expose launch flags. `[CITED: github.com/ggml-org/llama.cpp/blob/master/tools/server/README.md]`
- Read HEAD: `internal/llm/{models.go,prices.go,client.go}` (no existing models fetch; hard-coded tables; `normalizeModelID`; `ReasoningEffort` lacks `max`), `internal/agui/settings_api.go` (endpoint + setter-injection + `effectiveSettingValue` pattern), `internal/llm/openai_compat/client.go` (streaming-only HTTP client).
- Spikes 095/096 (unchanged) — the frozen wire contract; llama.cpp budgets 512/2048/8192/-1.

### Secondary (MEDIUM)
- OpenRouter reasoning-tokens blog + models overview (WebSearch) — confirms `supported_parameters` includes `reasoning`/`reasoning_effort` and that `reasoning_effort` takes high/medium/low with medium default. `[CITED: openrouter.ai/blog/announcements/reasoning-tokens-for-thinking-models]`
- llama.cpp discussion #21445 (via spike 095) — `thinking_budget_tokens` gating on launch flags.

## Pass-2 Assumptions Log

| # | Claim | Risk if wrong |
|---|-------|---------------|
| A6 | `/models` nesting is `data[].reasoning.supported_efforts` | Field names operator-verified; nesting pinned by the Wave-0 fixture. Low. `[ASSUMED nesting; VERIFIED names]` |
| A7 | llama.cpp capability is safely derived from `provider=llamacpp` + the spike-095 ops contract (with `/props` as best-effort narrowing) | If `/props` later exposes a reliable thinking flag, prefer it; the provider+contract path is the robust floor. `[ASSUMED]` |
| A8 | A 6–24h TTL is acceptable (active model is boot-stable) | If model switching becomes hot, add cache invalidation on switch (OQ / RISK). `[ASSUMED]` |
| A9 | `ReasoningEffortMax = "max"` serializes 1:1 on the OpenRouter wire (it is OpenRouter's own token) | Verified as an OpenRouter `supported_efforts` token; the wire already does `Effort: string(r.Effort)`. `[VERIFIED: operator probe]` |
| A10 | The capability is process-global (operator-scoped), so the endpoint needs only `RequireAuth` (no per-identity leak) | True while there is one active model per deployment (today). `[VERIFIED: config/settings model is global]` |

**Pass-2 confidence:** HIGH for the code seams (settings_api mirror, models.go reuse, /props schema, vocab gap) and the enforcement design; MEDIUM-HIGH for the exact remote JSON nesting (names verified, byte-exact fixture pending Wave 0). The wire contract stays frozen — `reasoning.max_tokens` is NOT resurrected (D-12).
