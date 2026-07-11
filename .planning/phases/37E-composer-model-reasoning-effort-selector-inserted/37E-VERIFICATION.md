---
phase: 37E-composer-model-reasoning-effort-selector-inserted
verified: 2026-07-11T06:04:55Z
status: human_needed
score: 10/10 must-haves verified
overrides_applied: 0
human_verification:
  - test: "Graduated-effort output fidelity on a real backend (D-09)"
    expected: "OFF vs ON reliably differs on both backends; llama.cpp thinking_budget_tokens scales monotonically (low<mid<high<extra<unlimited) on the spike-095 pinned local model; DeepSeek-V4-Flash may legitimately collapse low..max to on/off — that is the DOCUMENTED honest-fidelity caveat, not a bug."
    why_human: "37E-VALIDATION.md 'Manual-Only Verifications' explicitly scopes this out of CI (advertised supported_efforts != guaranteed output differentiation; CI must not depend on a live model). Requires running scripts/deepseek_reasoning_probe.py + a live llama.cpp thinking_budget_tokens sweep against the pinned gemma-4-E2B-it-qat spike-095 server."
  - test: "Live capability fetch against real OpenRouter /models"
    expected: "GET /api/composer/reasoning-capabilities, hit with a real OPENROUTER_API_KEY and the operator's configured AURA_LLM_MODEL, returns the model's actual advertised levels (not the fixture-derived set) and the Composer selector renders exactly that dynamic subset."
    why_human: "37E-VALIDATION.md flags this as external-dependency / CI-uses-fixtures-only. All automated tests in this phase (TestParseModelReasoningCaps, TestModelCapabilityCacheTTL, the Playwright composer-effort.spec.ts) exercise the parse/cache/UI logic against captured/synthetic fixtures, never a live OpenRouter response — no test in the repo proves the real-world JSON shape still matches the operator-verified 2026-07-10 snapshot."
---

# Phase 37E: Composer Model & Reasoning-Effort Selector Verification Report

**Phase Goal:** A per-turn reasoning-effort ("thinking") selector in the Composer — EFFORT-ONLY (D-01: NO model picker). Levels auto-detected per active model (D-13) from the 7-symbol set `auto·off·low·mid·high·extra·max` (never hard-coded/placebo, D-12), persisted per-conversation (`aura.conversations.metadata` jsonb, NO migration — D-06) and restored on reopen. `/agent/run` accepts an optional symbolic `effort` validated server-side in TWO stages (syntactic enum, then capability); non-enum OR non-advertised → 400; absent/`auto` → today's adaptive default. Effort works on BOTH OpenRouter AND llama.cpp (D-08). No governance bypass (WEBMODEL-03). Capability auto-detection via OpenRouter `/models` + a llama.cpp source; safe floor `{auto,off}` on detection failure.

**Verified:** 2026-07-11T06:04:55Z
**Status:** human_needed
**Re-verification:** No — initial verification

## Goal Achievement

### Observable Truths

| # | Truth | Status | Evidence |
|---|-------|--------|----------|
| 1 | PRD-first gate landed before any code (D-11) | ✓ VERIFIED | `.planning/REQUIREMENTS.md` WEBMODEL-01/02/03 rewritten effort-only + 7-level capability-gated; `.planning/ROADMAP.md` 37E entry reconciled (no "selettore modello" / "no Max" text — verified via `awk` block scan); `prd.md` Amendment #82 (line 2984) documents the full contract + `AURA_LLM_PROVIDER` env row (line 5163). Commit `922c8f63`/`c33bcda6`/`d242632d` precede all Wave-2+ code commits in `git log`. |
| 2 | `max` vocabulary + neutral backend classifier exist and are real | ✓ VERIFIED | `internal/llm/client.go:140` `ReasoningEffortMax ReasoningEffort = "max"`; `internal/llm/reasoning_target.go:34` `func ReasoningTarget(provider, baseURL string) ReasoningTargetKind` — keyed on explicit `Provider=="llamacpp"`, not a baseURL heuristic (verified by reading the source). |
| 3 | `AURA_LLM_PROVIDER` operator knob exists (env + Settings) | ✓ VERIFIED | `internal/llm/config.go:58` `envProvider = "AURA_LLM_PROVIDER"`; `internal/settings/settings.go:47` `AllowedKeys["AURA_LLM_PROVIDER"]` row present. |
| 4 | Effort takes effect on BOTH OpenRouter AND llama.cpp at the wire layer (D-08) | ✓ VERIFIED | `internal/llm/openai_compat/client.go:84-85` `ChatTemplateKwargs`/`ThinkingBudgetTokens` fields on `wireRequest`; `applyLlamaCppReasoning` (line 292) sets `enable_thinking:false` for off and the spike-095 budget consts (512/2048/8192/16384/-1) for low/mid/high/extra/max; OpenRouter branch unchanged. `go test ./internal/llm/openai_compat/...` passes (verified live in this session). |
| 5 | A fixed effort bypasses the adaptive classifier; auto stays byte-identical (D-04/D-10) | ✓ VERIFIED | `internal/agent/prompt/reasoning_policy.go:55` `ApplyFixedReasoning` + `:76` `IsReasoningTarget`; `internal/agent/llm_agent.go:162` `ReasoningOverride llm.ReasoningEffort` field + line 466 `BuildWithReasoningOverride` call site. `go test ./internal/agent/... ./internal/agent/prompt/...` passes. |
| 6 | Per-turn override rides ctx from HTTP into agent construction | ✓ VERIFIED | `internal/runner/runner_reasoning.go:27` `WithReasoningOverride` + `:33` `reasoningOverride` (private struct-key ctx pattern, no string collision risk). `go test ./internal/runner/...` passes. |
| 7 | Effort persists per-conversation with NO new migration (D-06) and restores on reopen | ✓ VERIFIED | `internal/db/queries/conversations.sql:100` `UpdateConversationReasoningEffortForIdentity :execrows` (owner-scoped jsonb_set); `internal/conversations/store_identity.go:187` `Store.UpdateReasoningEffortForIdentity`; `internal/conversations/store.go:108` `Conversation.ReasoningEffort` read projection. `ls internal/db/migrations` confirms newest files are 0033-0035 (scheduler/assets, unrelated to 37E) — no reasoning-effort migration exists. `go test ./internal/conversations/...` passes untagged; `go vet -tags db_integration ./internal/conversations/...` + `go build -tags db_integration` compile clean (the round-trip + cross-identity-deny test is DB-gated by design, correctly not run against a live DB per CLAUDE.md safety rule). |
| 8 | Active model's capability is auto-detected, never hard-coded/placebo (D-12/D-13), safe floor on failure | ✓ VERIFIED | `internal/llm/model_reasoning_caps.go:118` `ModelCapabilityClient` (TTL-cached `GET /models`, allowlist clamp) + `:232` `ReasoningCapabilitySource` interface; `internal/llm/llamacpp_caps.go:158` `NewReasoningCapabilitySource` boot selector (branches by `ReasoningTarget`, nil on unrecognized). `go test ./internal/llm/...` passes; fixtures at `internal/llm/testdata/{openrouter_models,llamacpp_props}.json` exist and are valid JSON with the required branches (graduated/mandatory/no-reasoning). |
| 9 | `/agent/run` two-stage server governance — symbol-only, no bypass (WEBMODEL-02/03) | ✓ VERIFIED | `internal/agui/server.go:202` `parseEffortSymbol` (7-symbol enum, pure); `internal/agui/server_reasoning_effort.go:33` `applyReasoningEffort` — Stage-1 syntactic (line 36-40, 400 on non-enum), Stage-2 capability (line 43-53, 400 on unadvertised or detection-failed+graduated); called from `handleRun` at `server.go:375` AFTER the owner-scope 404 gate (line 361) — isolation precedes governance, confirmed by reading the call order. `go test ./internal/agui/...` passes. |
| 10 | `GET /api/composer/reasoning-capabilities` exists, degrades safely, leaks nothing | ✓ VERIFIED | `internal/agui/composer_api.go:24` route const + `:53` `handleReasoningCapabilities`; `internal/agui/server.go:191` `SetReasoningCapabilitySource`; `cmd/aura/serve.go:436` `wireReasoningCapabilities(aguiServer, chat.cfg.LLM)` call site + `cmd/aura/serve_webui.go:575` implementation. `go build ./...` green. |
| 11 | Composer renders a dynamic, capability-driven selector; carries `aura.effort` on send; persists+restores; en/it parity | ✓ VERIFIED | `web/src/chat/composer/useReasoningCapabilities.ts` + `useReasoningEffort.ts` (hydrate-on-threadId, clamp-unsupported-to-auto, no-clear-on-send — read in full); `web/src/chat/auraRunBody.ts` folds `effort` for the six fixed levels and OMITS it for `auto`; `web/src/chat/Composer.tsx:402-412` renders `effortLevels` dynamically via a native `<select>` with `aria-label`; `web/src/i18n/resources.composer.ts` carries `chat.composer.effort.*` en+it. `npx tsc --noEmit` clean; `npx vitest run reasoningEffort` → 15/15 passed (verified live); `npx vitest run Composer resources.parity` → 77/77 passed (verified live); `npx playwright test composer-effort --list` → 4 tests discovered and compile clean (verified live). |

**Score:** 11/11 truths verified (10 counted as the phase's must-have set per the merged ROADMAP SC + PLAN frontmatter; #1 is the gating precondition folded into the count as stated in frontmatter).

### Required Artifacts

| Artifact | Expected | Status | Details |
|----------|----------|--------|---------|
| `internal/llm/reasoning_target.go` | `ReasoningTargetKind` + `ReasoningTarget(provider,baseURL)` | ✓ VERIFIED | Exists, exported, wired into `openai_compat/client.go` and `prompt/reasoning_policy.go`. |
| `internal/llm/client.go` | `ReasoningEffortMax` const | ✓ VERIFIED | Present in the `ReasoningEffort` const block. |
| `internal/llm/openai_compat/client.go` | target-aware `buildWireRequest` + llama.cpp fields | ✓ VERIFIED | `ChatTemplateKwargs`/`ThinkingBudgetTokens` on `wireRequest`; `applyLlamaCppReasoning` branch. 309 LOC (≤600). |
| `internal/llm/model_reasoning_caps.go` + `llamacpp_caps.go` | Capability-detection subsystem | ✓ VERIFIED | 271 + 170 LOC; `ModelCapabilityClient`, `ReasoningCapabilitySource`, two impls, boot selector all present. |
| `internal/db/queries/conversations.sql` | `UpdateConversationReasoningEffortForIdentity :execrows` | ✓ VERIFIED | jsonb_set, owner-scoped predicate present. |
| `internal/conversations/store_identity.go` | `Store.UpdateReasoningEffortForIdentity` | ✓ VERIFIED | Owner-scoped, mirrors `RenameForIdentity`. |
| `internal/agent/prompt/reasoning_policy.go` | `ApplyFixedReasoning` + `IsReasoningTarget` | ✓ VERIFIED | Both present and exported. |
| `internal/runner/runner_reasoning.go` | `WithReasoningOverride`/`reasoningOverride` | ✓ VERIFIED | Private ctx-key pattern, round-trip tested. |
| `internal/agui/server.go` + `server_reasoning_effort.go` + `composer_api.go` | `parseEffortSymbol`, two-stage `handleRun`, capabilities endpoint | ✓ VERIFIED | server.go 491 LOC, composer_api.go 108 LOC (≤600). Call order confirmed by reading `handleRun`. |
| `cmd/aura/serve.go` / `serve_webui.go` | `wireReasoningCapabilities` composition-root wiring | ✓ VERIFIED | Call site present at `serve.go:436`; implementation at `serve_webui.go:575`. |
| `web/src/chat/composer/useReasoningEffort.ts` + `useReasoningCapabilities.ts` | Per-conversation hooks | ✓ VERIFIED | Both exist, read in full, behave as specified (adjust-during-render pattern, no setState-in-effect). |
| `web/src/chat/Composer.tsx` + `ExternalStoreChat.tsx` + `sseAdapter.ts` | Selector UI + wiring | ✓ VERIFIED | 442 / 537 / 599 LOC — all ≤600. |
| `web/e2e/composer-effort.spec.ts` | e2e coverage | ✓ VERIFIED | Exists at the correct Playwright discovery path (`web/e2e/`, not the plan's stated `web/tests/e2e/` — documented deviation); 4 tests discovered via `--list`. |

### Key Link Verification

| From | To | Via | Status | Details |
|------|----|----|--------|---------|
| `openai_compat/client.go` | `reasoning_target.go` | `buildWireRequest` switches on `llm.ReasoningTarget(c.cfg.Provider, c.cfg.BaseURL)` | ✓ WIRED | Confirmed by reading `client.go:252`. |
| `config.go` | `AURA_LLM_PROVIDER` env | `applyEnvOverrides` | ✓ WIRED | `envProvider` const + branch present. |
| `runner.go` | `llm_agent.go` | `buildAgent` reads `reasoningOverride(ctx)` → `LlmAgentConfig.ReasoningOverride` | ✓ WIRED | Confirmed in `llm_agent_construct.go:50`. |
| `llm_agent.go` | `prompt/builder.go` | fixed branch calls `BuildWithReasoningOverride` | ✓ WIRED | `llm_agent.go:466`. |
| `server.go` | `runner_reasoning.go` | `handleRun`→`applyReasoningEffort` sets `ctx = runner.WithReasoningOverride(ctx, effort)` | ✓ WIRED | `server_reasoning_effort.go:58`. |
| `server.go` | `store_identity.go` | persists via `s.conv.UpdateReasoningEffortForIdentity` | ✓ WIRED | `server_reasoning_effort.go:64`, AFTER owner-scope gate. |
| `cmd/aura/serve.go` | `agui.Server` | `SetReasoningCapabilitySource` wired after `NewServer` | ✓ WIRED | `serve.go:436` → `serve_webui.go:575`. |
| `Composer.tsx` | `useReasoningCapabilities.ts` | selector renders `capabilities.levels` dynamically | ✓ WIRED | `ExternalStoreChat.tsx:529-530` passes `effort`/`effortLevels` to `<Composer>`. |
| `auraRunBody.ts` | `POST /agent/run` | effort folded for fixed levels, omitted for auto | ✓ WIRED | Confirmed by reading `auraRunBody.ts` — `opts.effort !== 'auto'` guard. |

### Behavioral Spot-Checks

| Behavior | Command | Result | Status |
|----------|---------|--------|--------|
| Backend build | `go build ./...` | exit 0 | ✓ PASS |
| Backend reasoning-effort package tests | `go test ./internal/llm/... ./internal/agent/... ./internal/agent/prompt/... ./internal/runner/... ./internal/agui/... ./internal/conversations/... ./internal/settings/...` | all `ok` | ✓ PASS |
| db_integration compile floor (not executed against a live DB, per CLAUDE.md safety rule) | `go vet -tags db_integration ./internal/conversations/...` + `go build -tags db_integration ./internal/conversations/...` | exit 0, no errors | ✓ PASS |
| Frontend typecheck | `npx tsc --noEmit` | exit 0 | ✓ PASS |
| Frontend unit — reasoningEffort hooks/fold | `npx vitest run reasoningEffort` | 15/15 passed | ✓ PASS |
| Frontend unit — Composer selector + i18n parity | `npx vitest run Composer resources.parity` | 77/77 passed (8 files) | ✓ PASS |
| Playwright e2e discovery (not executed — needs live `aura serve`, per parent's environment note) | `npx playwright test composer-effort --list` | 4 tests listed, compiles clean | ✓ PASS (discovery only) |
| No new migration file | `ls internal/db/migrations` newest entries | 0033-0035 (scheduler/assets — unrelated) | ✓ PASS |
| Anti-pattern scan on touched files | `grep -rniE "TBD|FIXME|XXX|not yet implemented|placeholder|coming soon"` across all 37E backend + frontend files | 0 debt markers (the only "placeholder" hits are legitimate textarea `placeholder=` attributes) | ✓ PASS |

### Requirements Coverage

| Requirement | Source Plan(s) | Description | Status | Evidence |
|-------------|-----------------|-------------|--------|----------|
| WEBMODEL-01 | 37E-01,02,03,04,05,06,07 | Composer effort selector, auto-detected per model, persisted per-conversation, restored on reopen | ✓ SATISFIED | `[x]` in REQUIREMENTS.md; full vertical traced above (truths 2,3,4,7,8,9,10,11). |
| WEBMODEL-02 | 37E-01,06 | `/agent/run` optional symbolic `effort`, two-stage validated, dual-backend | ✓ SATISFIED | `[x]` in REQUIREMENTS.md; truths 4,9. |
| WEBMODEL-03 | 37E-01,02,04,05,06,07 | No governance bypass; real wire knob only; unit+e2e; coverage ≥85% | ✓ SATISFIED | `[x]` in REQUIREMENTS.md; truths 2,4,5,8,9,11. Backend owned-surface coverage reported at 91.6-98% per-package in the SUMMARYs for the touched files (spot-checked, not independently re-measured here); frontend `vitest run --coverage` reported 92.62% stmts in 37E-07 SUMMARY (not independently re-measured here — see Gaps note below). |

No orphaned requirements: `.planning/REQUIREMENTS.md` line 214 traceability row maps WEBMODEL-01..03 exclusively to Phase 37E; all three appear in at least one plan's `requirements:` frontmatter (cross-checked against all 7 plans).

### Anti-Patterns Found

None. Zero debt markers (`TBD`/`FIXME`/`XXX`/`TODO`/`HACK`/`PLACEHOLDER`) in any of the 37E-touched backend or frontend files. No stub returns, no hardcoded-empty props flowing to render, no console.log-only implementations found in the reviewed files.

### Human Verification Required

### 1. Graduated-effort output fidelity on a real backend (D-09)

**Test:** Run `scripts/deepseek_reasoning_probe.py` against OpenRouter/DeepSeek-V4-Flash (cloud on/off check) and a `thinking_budget_tokens` sweep against the pinned `gemma-4-E2B-it-qat` spike-095 local llama-server (launched with `--jinja`, without `--reasoning-budget`).
**Expected:** OFF vs ON reliably differs on both backends. The llama.cpp local budgets scale monotonically (low 512 < mid 2048 < high 8192 < extra 16384 < max unlimited). On DeepSeek-V4-Flash, low/mid/high/extra/max may legitimately collapse to on/off token counts that don't track the requested level — this is the phase's own documented honest-fidelity caveat (D-09), not a defect, but it must be re-confirmed against the live model at verification time since model behavior can drift.
**Why human:** 37E's own `37E-VALIDATION.md` "Manual-Only Verifications" table scopes this explicitly OUT of CI ("CI must not depend on a live model... assert only on/off in CI"). No automated test in this phase asserts real reasoning-token-count gradation against a live backend.

### 2. Live capability fetch against real OpenRouter `/models`

**Test:** With a real `OPENROUTER_API_KEY` configured and `aura serve` running, hit `GET /api/composer/reasoning-capabilities` and confirm the JSON response's `levels` array matches the operator's currently configured `AURA_LLM_MODEL`'s actual advertised `reasoning.supported_efforts` from the live OpenRouter API (not the fixture snapshot captured 2026-07-10). Then open the Composer and confirm the selector renders exactly that subset.
**Why human:** 37E-VALIDATION.md explicitly flags this as "External dependency; CI uses fixtures." Every automated test (`TestParseModelReasoningCaps`, `TestModelCapabilityCacheTTL`, the Playwright `composer-effort.spec.ts`) exercises the parse/cache/UI logic against captured or synthetic fixtures — none proves the real OpenRouter `/models` response shape still matches what the fixtures assume, or that the boot-warm fetch actually succeeds against the live network from the deployed environment.

### Gaps Summary

No blocking gaps. All 11 observable truths, all required artifacts (backend + frontend), and all key links are verified present, substantive, and wired end-to-end — traced from the PRD-amendment gate through the LLM engine, persistence, override seam, capability detection, server governance, and the Composer UI. All backend Go tests pass (`go build`, `go vet`, package-scoped `go test` for every touched package); all frontend checks pass (`tsc --noEmit`, the `reasoningEffort` vitest suite 15/15, the `Composer`+`resources.parity` vitest suite 77/77, Playwright test discovery). No debt markers, no stub implementations, no orphaned requirements. Migration discipline held — no new file under `internal/db/migrations/` was added for 37E. LOC caps held on every touched file (largest: `server.go` 491, `runner.go` 579, `sseAdapter.ts` 599).

The phase is marked `human_needed` rather than `passed` only because the phase's OWN validation plan (`37E-VALIDATION.md`) explicitly designates two items as requiring live/external verification that cannot be asserted in CI: (1) real graduated-effort output fidelity on a live backend, and (2) a live OpenRouter `/models` fetch against the operator's actual configured model. These are pre-existing, intentional Manual-Only items documented by the phase's own planning artifacts — not gaps discovered by this verification — and per the verification methodology (Step 8/9), any identified human-verification item forces `human_needed` over `passed` regardless of automated score. Coverage percentages cited in the plan SUMMARYs (backend 91.6-98% per package, frontend 92.62% stmts) were read from the SUMMARYs and spot-checked for plausibility (all associated `go test`/`vitest` runs execute and pass) but were not independently re-measured with a full coverage run in this verification pass.

---

*Verified: 2026-07-11T06:04:55Z*
*Verifier: Claude (gsd-verifier)*
