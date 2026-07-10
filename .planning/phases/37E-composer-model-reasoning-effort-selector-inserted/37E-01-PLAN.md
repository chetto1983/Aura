---
phase: 37E-composer-model-reasoning-effort-selector-inserted
plan: 01
type: execute
wave: 1
depends_on: []
files_modified:
  - .planning/REQUIREMENTS.md
  - .planning/ROADMAP.md
  - prd.md
autonomous: true
requirements: [WEBMODEL-01, WEBMODEL-02, WEBMODEL-03]
must_haves:
  truths:
    - "REQUIREMENTS.md WEBMODEL-01/02/03 describe an effort-only selector (model-selector clause removed) with capability-gated no-bypass"
    - "ROADMAP.md 37E success criteria describe effort-only + llama.cpp coverage + capability auto-detection; the stale 'no Max' wording is deleted"
    - "prd.md documents the composer effort selector, the /agent/run effort field, per-conversation metadata persistence, the OpenRouter /models capability source, and the honest backend-dependent fidelity caveat"
  artifacts:
    - path: ".planning/REQUIREMENTS.md"
      provides: "Amended WEBMODEL-01/02/03 (effort-only, 7-level capability-gated)"
      contains: "effort selector"
    - path: "prd.md"
      provides: "37E reasoning-effort selector section + env catalog entry AURA_LLM_PROVIDER"
      contains: "reasoning-effort"
  key_links:
    - from: ".planning/REQUIREMENTS.md"
      to: "prd.md"
      via: "WEBMODEL amendment consistency (effort-only, 7-level, capability-gated, no Max deletion)"
      pattern: "WEBMODEL-0[123]"
---

<objective>
Wave-1 PRD-amendment gate (D-11). This is a DOCS-ONLY plan and MUST land before any code — no implementation plan in this phase may execute until this commits.

Reconcile ROADMAP.md + REQUIREMENTS.md + prd.md to the resolved 37E scope: (a) drop the model-selector clause from WEBMODEL-01/02 (effort-only, D-01); (b) rewrite WEBMODEL-03 to the effort-enum + capability-gated no-bypass form (D-05/D-13); (c) add the llama.cpp coverage requirement (D-08); (d) add the capability-auto-detection + real-knobs-only requirements (D-12/D-13); (e) DELETE/supersede the stale D-09a "no Max" wording everywhere so all three docs reflect the 7-level capability-gated scale `auto·off·low·mid·high·extra·max` (D-02/D-09a-VOID); (f) document the composer effort selector, the `/agent/run` effort field, per-conversation `conversations.metadata` persistence, the OpenRouter `/models` capability source, and the honest backend-dependent fidelity caveat (D-09) in prd.md.

Purpose: PRD-first (absolute, CLAUDE.md). Deviation from the roadmap-chartered "model+effort" scope requires a PRD-amendment commit BEFORE implementation.
Output: Three amended docs, one atomic commit.
</objective>

<artifacts_this_phase_produces>
Consolidated symbol/artifact inventory for the whole 37E phase (executors of later plans consume these):
- **Go — internal/llm:** `ReasoningEffortMax` const (`"max"`); `ReasoningTargetKind` + `ReasoningTarget(provider, baseURL)`; `ReasoningCapability` struct; `ReasoningCapabilitySource` interface (`AllowedEfforts(ctx)`); `ModelCapabilityClient` (+ `NewModelCapabilityClient`, `ReasoningCapabilityFor`); `openRouterReasoningCaps`, `llamaCppReasoningCaps` impls; wire fields `ChatTemplateKwargs`, `ThinkingBudgetTokens` on `wireRequest`; effort→budget consts (512/2048/8192/16384/-1).
- **Go — internal/agent:** `ApplyFixedReasoning`; `BuildWithReasoningOverride`; generalized `IsReasoningTarget`; `LlmAgent.reasoningOverride` field + `LlmAgentConfig.ReasoningOverride`.
- **Go — internal/runner:** `WithReasoningOverride(ctx, llm.ReasoningEffort)` + `reasoningOverride(ctx)`.
- **Go — internal/agui:** `Effort` field on the `aura` run DTO (both structs); `parseEffortSymbol`; `SetReasoningCapabilitySource`; `handleReasoningCapabilities` + route `GET /api/composer/reasoning-capabilities`; `reasoningCapabilitiesDTO`.
- **Go — internal/conversations / db:** `UpdateConversationReasoningEffortForIdentity` sqlc query; `Store.UpdateReasoningEffortForIdentity`; `Conversation.ReasoningEffort` field; `metadata`→projection mapping in `conversationFromRow`.
- **Config/env:** new `AURA_LLM_PROVIDER` env + settings `AllowedKeys` entry (`openrouter|llamacpp`).
- **Web:** `web/src/chat/composer/useReasoningEffort.ts`, `useReasoningCapabilities.ts`; `fetchReasoningCapabilities` in `composer/api.ts`; `StreamRunOptions.effort`; `buildAuraRunBody` effort fold; Composer selector control + i18n `en`+`it` (7 labels + aria-label).
- **Fixtures:** `internal/llm/testdata/openrouter_models.json`, `internal/llm/testdata/llamacpp_props.json`.
</artifacts_this_phase_produces>

<execution_context>
@/home/user/Aura/.claude/get-shit-done/workflows/execute-plan.md
@/home/user/Aura/.claude/get-shit-done/templates/summary.md
</execution_context>

<context>
@.planning/phases/37E-composer-model-reasoning-effort-selector-inserted/37E-CONTEXT.md
@.planning/phases/37E-composer-model-reasoning-effort-selector-inserted/37E-RESEARCH.md
@.planning/ROADMAP.md
@.planning/REQUIREMENTS.md
@CLAUDE.md
</context>

<tasks>

<task type="auto">
  <name>Task 1: Amend REQUIREMENTS.md WEBMODEL-01/02/03 to the effort-only, capability-gated form</name>
  <files>.planning/REQUIREMENTS.md</files>
  <read_first>
    - .planning/REQUIREMENTS.md lines ~91-97 (the WEBMODEL block — current model+effort charter)
    - .planning/phases/37E-.../37E-CONTEXT.md D-01, D-02, D-05, D-08, D-12, D-13, D-09a (VOID)
    - The 37B/C/D requirement rows above (the amendment-style wording precedent)
  </read_first>
  <action>
    Rewrite the three WEBMODEL rows (currently at REQUIREMENTS.md ~95-97) to the resolved scope. WEBMODEL-01: "The Composer exposes a reasoning-effort selector (levels auto-detected per active model from the set `auto·off·low·mid·high·extra·max`); the choice is persisted per-conversation (`aura.conversations.metadata` jsonb, no migration) and restored on reopen." — DELETE the "model selector (populated from the configured backends)" clause (D-01). WEBMODEL-02: "`/agent/run` accepts an optional symbolic `effort` override; the server maps the symbol → `llm.ReasoningConfig` and validates it in two stages — syntactic enum, then capability (the level must be in the active model's advertised `supported_efforts`); a non-enum OR non-advertised value → 400; absent/`auto` → today's adaptive default (no regression). Effort takes effect on BOTH OpenRouter AND a local llama.cpp backend (D-08)." — remove all "model" language. WEBMODEL-03: "No bypass of governance: the client sends a SYMBOL, never a raw `ReasoningConfig`/budget/model; the server owns the symbol→config map and the capability gate. Every UI level maps to a REAL spike-validated wire knob (D-12) — no placebo/fabricated field. Unit + e2e; coverage ≥85%." Add a one-line honest-fidelity caveat referencing D-09 (advertised support ≠ guaranteed graduated fidelity; DeepSeek collapses to on/off). Keep the `*(Phase 37E)*` tags.
  </action>
  <acceptance_criteria>
    - REQUIREMENTS.md WEBMODEL-01 contains "reasoning-effort selector" and "per-conversation" and does NOT contain "model selector" / "populated from the configured backends".
    - WEBMODEL-02 contains "two stages" (or "syntactic" + "capability") and "llama.cpp" and "400".
    - WEBMODEL-03 contains "symbol" + "never" + "coverage ≥85%" and references D-12 (real knob / no placebo).
    - The section header/intro line no longer promises "model + reasoning-effort" as the delivered scope (effort-only).
    - grep -n 'auto·off·low·mid·high·extra·max' .planning/REQUIREMENTS.md returns the 7-level set at least once.
  </acceptance_criteria>
  <verify>
    <automated>grep -q "reasoning-effort selector" .planning/REQUIREMENTS.md && grep -q "llama.cpp" .planning/REQUIREMENTS.md && ! grep -iq "Composer exposes a model selector" .planning/REQUIREMENTS.md && echo OK</automated>
  </verify>
  <done>WEBMODEL-01/02/03 read as effort-only, 7-level, capability-gated, llama.cpp-covered, no-bypass; the stale model-selector clause is gone.</done>
</task>

<task type="auto">
  <name>Task 2: Reconcile ROADMAP.md 37E section — effort-only success criteria + delete stale "no Max" / model wording</name>
  <files>.planning/ROADMAP.md</files>
  <read_first>
    - .planning/ROADMAP.md lines 544-560 (the Phase 37E entry — current model+effort success criteria)
    - 37E-CONTEXT.md D-09a (the VOID "no Max" resolution) + D-02 (7 levels supersedes)
  </read_first>
  <action>
    Rewrite the ENTIRE 37E roadmap entry (everything between `#### Phase 37E` and `#### Phase 37F`) to effort-only — the verify greps the whole block, so ALL model-selector framing must go, not just the Success Criteria. Specifically: (1) **Goal paragraph** (ROADMAP.md ~546) — rewrite from "modello + reasoning-effort ... selettore modello (popolato dai backend configurati) + un selettore effort ... validato contro l'allowlist dei backend configurati" to effort-only: a per-turn reasoning-effort selector whose levels are auto-detected per active model (D-13), effort-only (model stays in Settings, D-01), persisted per-conversation; DROP every "selettore modello"/"allowlist dei backend/modelli" phrase. (2) **Success Criteria** (~552-556): SC1 — Composer exposes a reasoning-effort selector whose levels are auto-detected per active model (no hard-coded list, no placebo); persists per-conversation and restores on reopen. SC2 — `/agent/run` accepts an optional symbolic `effort`, validated server-side in two stages (enum + capability); non-enum/non-advertised → 400; absent/`auto` → today's adaptive default; effort works on OpenRouter AND llama.cpp. SC3 — no governance bypass (symbol-only, server owns the map + capability gate); every level maps to a real wire knob; unit + e2e; coverage ≥85%. (3) **Design-forks paragraph** (~569): DELETE the "(a) sorgente della lista modelli — un nuovo endpoint … che espone i backend configurati" model-list-source fork entirely (model selection is out, D-01); keep the persistence fork but note it is RESOLVED to `conversations.metadata` jsonb (no migration, D-06); replace the effort-semantics fork with the resolved 7-level capability-gated mapping (D-02/D-13). Across the WHOLE entry, DELETE any "Max is NOT added"/"off/low/mid/high/auto only" text, replacing with the 7-level capability-gated scale `auto·off·low·mid·high·extra·max` (D-09a VOID). Update the "PRD-first" line to reference the effort-enum + capability requirements. Leave the `**Requirements:** WEBMODEL-01, WEBMODEL-02, WEBMODEL-03` line AND the plan list (added by the planner) intact.
  </action>
  <acceptance_criteria>
    - The 37E block (from `#### Phase 37E` through the line before `#### Phase 37F`) contains "reasoning-effort" and "capability"/"auto-detected" and "llama.cpp".
    - No occurrence of "Max is NOT added" or "off/low/mid/high/auto only" anywhere in the 37E entry.
    - No "selettore modello" anywhere in the 37E block — Goal paragraph, Success Criteria, AND the Design-forks paragraph (the "sorgente della lista modelli" fork is deleted). This matches the verify command, which greps the full block.
    - The `**Requirements:** WEBMODEL-01, WEBMODEL-02, WEBMODEL-03` line and the planner-added plan list are unchanged.
  </acceptance_criteria>
  <verify>
    <automated>awk '/#### Phase 37E/,/#### Phase 37F/' .planning/ROADMAP.md > /tmp/37e_roadmap.txt && ! grep -iq "Max is NOT added" /tmp/37e_roadmap.txt && ! grep -iq "selettore modello" /tmp/37e_roadmap.txt && grep -q "WEBMODEL-01, WEBMODEL-02, WEBMODEL-03" /tmp/37e_roadmap.txt && echo OK</automated>
  </verify>
  <done>The 37E roadmap entry describes an effort-only, capability-gated, llama.cpp-covered scope; the stale "no Max" and model-selector wording is deleted.</done>
</task>

<task type="auto">
  <name>Task 3: Document the effort selector + capability source + persistence + env in prd.md</name>
  <files>prd.md</files>
  <read_first>
    - prd.md §Slice Q&A discipline → Q&A revision protocol (the amendment-commit convention)
    - prd.md §Caps & Limits → Indice completo env vars (where AURA_LLM_PROVIDER is catalogued)
    - 37E-RESEARCH.md Pass-2 §P2.1 (7-level wire map), §P2.2 (capability source), Threat Model Inputs
    - 37E-CONTEXT.md D-06 (persistence), D-08/D-09 (backends + honest fidelity), D-13 (capability auto-detection)
  </read_first>
  <action>
    Add a 37E amendment subsection to prd.md documenting: (1) the Composer reasoning-effort selector — symbolic 7-level set `auto·off·low·mid·high·extra·max`, auto-detected per active model (D-13), effort-only (model stays in Settings, D-01); (2) the `/agent/run` optional `aura.effort` symbolic field with two-stage server validation (enum + capability), symbol→`llm.ReasoningConfig` map owned by the server (D-05/D-12); (3) the level→wire map for BOTH backends — OpenRouter `reasoning:{effort:…}` (unchanged shape) and llama.cpp `chat_template_kwargs:{enable_thinking:false}` / `thinking_budget_tokens:512/2048/8192/16384/-1` (spike 095), plus the llama-server ops contract (`--jinja` ON, `--reasoning-budget` OFF); (4) per-conversation persistence in `aura.conversations.metadata` jsonb (NO migration — the column exists in 0005; explicitly state migration numbering is unchanged); (5) the OpenRouter `/models` `supported_efforts` capability source (TTL-cached, warmed at boot, safe-fallback `{auto,off}` on failure) + the `GET /api/composer/reasoning-capabilities` endpoint; (6) the HONEST fidelity caveat (D-09) — advertised support ≠ guaranteed graduated output; DeepSeek-V4-Flash collapses low..max to on/off; graduated fidelity is guaranteed only on budget-capable local models. Add `AURA_LLM_PROVIDER` (`openrouter|llamacpp`, default `openrouter`) to the env var index.
  </action>
  <acceptance_criteria>
    - prd.md contains a 37E reasoning-effort selector section referencing `aura.effort`, the 7-level set, and two-stage validation.
    - prd.md documents the llama.cpp `thinking_budget_tokens` map AND the `--jinja`/`--reasoning-budget` ops contract.
    - prd.md states persistence is `conversations.metadata` jsonb with NO new migration.
    - prd.md carries the D-09 honest-fidelity caveat (advertised ≠ guaranteed; DeepSeek on/off).
    - grep -q 'AURA_LLM_PROVIDER' prd.md succeeds.
  </acceptance_criteria>
  <verify>
    <automated>grep -q "AURA_LLM_PROVIDER" prd.md && grep -q "thinking_budget_tokens" prd.md && grep -qi "no.*migration\|nessuna migration\|migration.*unchanged" prd.md && grep -q "reasoning-capabilities" prd.md && echo OK</automated>
  </verify>
  <done>prd.md is the truth-source for the effort selector, its wire map, capability source, persistence, env knob, and honest fidelity caveat.</done>
</task>

</tasks>

<threat_model>
## Trust Boundaries
| Boundary | Description |
|----------|-------------|
| author → planning docs | Documentation-only change; no runtime trust boundary crossed this plan. |

## STRIDE Threat Register
| Threat ID | Category | Component | Disposition | Mitigation Plan |
|-----------|----------|-----------|-------------|-----------------|
| T-37E-01-DOC | Repudiation | PRD/roadmap/requirements drift | mitigate | Single atomic commit reconciling all three docs; downstream plans cite D-IDs so the amendment is traceable and the "no Max"/model-selector staleness cannot silently persist. |
| T-37E-01-SCOPE | Tampering (scope) | Stale D-09a "no Max" | mitigate | Task 2 explicitly greps-and-deletes the stale wording; verify asserts its absence — a later plan reading D-09a literally would ship the wrong level set. |

No package installs, no code, no network surface in this plan.
</threat_model>

<verification>
- All three docs consistent: effort-only, 7-level capability-gated, llama.cpp-covered, no-Max-wording-deleted, honest fidelity documented.
- No code file touched (this is the gate; code plans are Wave 2+).
</verification>

<success_criteria>
- REQUIREMENTS.md WEBMODEL-01/02/03, the ROADMAP.md 37E entry, and prd.md all describe the same effort-only, 7-level, capability-gated, dual-backend scope.
- The stale D-09a "no Max" and the model-selector clauses are gone.
- Amendment committed atomically before any Wave-2 code begins.
</success_criteria>

<output>
Create `.planning/phases/37E-composer-model-reasoning-effort-selector-inserted/37E-01-SUMMARY.md` when done.
</output>
