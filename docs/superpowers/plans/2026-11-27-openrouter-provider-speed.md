# OpenRouter provider speed preferences Implementation Plan

**Status (2026-11-28, measured on the lab VM):** SUPERSEDED. Tasks 1-3 landed (b782503b5, 60530c1c3, 0422273e3). Task 4 ran on the VM (`docs/superpowers/verification/2026-11-28-openrouter-provider-speed.md`, PRD amendment #229): neither threshold moved the Telegram thread off Darkbloom, so the defaults stayed 0 and the operator's settings were reset. The operator then chose to replace the thresholds with a provider sort and a longer OpenRouter stream window; see `2026-11-28-openrouter-provider-sort.md`.

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Two cockpit settings make Aura send OpenRouter's `provider.preferred_min_throughput` and `provider.preferred_max_latency`, so a slow cheap provider is deprioritized and a fast one keeps the traffic.

**Architecture:** Two int fields on `llm.Config`, parsed from env like the other profile fields and hot-reloaded like `AURA_CONTEXT_COMPACTION_TRIGGER_PERCENT` (settings catalog → `hotLLMProfileKeys` → `primaryLLMRouteReloader.resolve`). The OpenRouter request builder adds them to the `provider` object it already sends. The cockpit shows them on the model-routing page.

**Tech Stack:** Go (openai-go v3 request options), React/TypeScript cockpit, Vitest.

**Spec:** `docs/superpowers/specs/2026-11-27-openrouter-provider-speed-design.md`

## Global Constraints

- Env names: `AURA_OPENROUTER_PREFERRED_MIN_THROUGHPUT` (tokens per second) and `AURA_OPENROUTER_PREFERRED_MAX_LATENCY_SEC` (seconds).
- Wire: inside `provider`, as plain numbers (OpenRouter reads a number as the p50): `{"provider":{"data_collection":"deny","preferred_min_throughput":N,"preferred_max_latency":M}}`. Docs: https://openrouter.ai/docs/features/provider-routing ("the `provider` object can contain the following fields", "Can be a number (applies to p50)").
- 0 or unset sends nothing: today's request, byte for byte. Negative values are refused.
- Only the OpenRouter route (`llm.ReasoningTarget(...) == llm.ReasoningTargetOpenRouter`) sends them; `data_collection: deny` stays in every OpenRouter request.
- Defaults stay 0 until the VM measurement of Task 4 says otherwise.
- No file over 600 LOC; `internal/llm/config.go` is at 559, so the parse goes through a small helper, not repeated blocks.

## Review Focus

- A value saved as text that is not a number (" 20 ", "abc", "-5"): the save must fail with the key named, never publish a profile that drops the field silently. Task 2's reloader test covers "abc" and "-5".
- A route change away from OpenRouter with the values still saved: the llama.cpp/ChatGPT/Ollama request must carry no `provider` key. Task 1's test covers a non-OpenRouter route.
- Clearing a value in the cockpit (delete the row): the reloader falls back to the boot value, as for the compaction trigger. Task 2's test covers the reset.
- Both set to 0 explicitly: no `preferred_*` keys, `data_collection` still present. Task 1.
- The cockpit field shown on a non-OpenRouter route: it is harmless (ignored), and its help text says it applies to OpenRouter only. Task 3.

---

### Task 1: The request carries the preferences

**Files:**
- Modify: `internal/llm/config.go` (two fields after `OpenRouterMiddleOut`, env names next to `envOpenRouterMiddleOut`, parse in `applyEnvOverrides`)
- Modify: `internal/llm/openai_compat/request.go:56-61`
- Test: `internal/llm/openai_compat/client_test.go`, `internal/llm/config_test.go`

**Interfaces:**
- Produces: `llm.Config.OpenRouterPreferredMinThroughput int`, `llm.Config.OpenRouterPreferredMaxLatencySec int`; constants `llm.EnvOpenRouterPreferredMinThroughput = "AURA_OPENROUTER_PREFERRED_MIN_THROUGHPUT"`, `llm.EnvOpenRouterPreferredMaxLatencySec = "AURA_OPENROUTER_PREFERRED_MAX_LATENCY_SEC"` (exported: Task 2 uses them).

- [ ] **Step 1: Write the failing tests**

In `client_test.go`, `TestOpenRouterProviderCarriesSpeedPreferences`, table-driven over the request body captured like the existing `session_id` test (line ~375):
- cfg min 20, latency 10 → `provider` == `{"data_collection":"deny","preferred_min_throughput":20,"preferred_max_latency":10}` (JSON numbers, compare after unmarshal as float64 20 and 10);
- cfg 0 and 0 → `provider` == `{"data_collection":"deny"}`, no `preferred_*` key;
- cfg min 20 only → only `preferred_min_throughput`;
- a llama.cpp route (BaseURL not OpenRouter) with min 20 → no `provider` key at all.

In `config_test.go`, `TestOpenRouterSpeedPreferencesFromEnv`: `t.Setenv` both to "20"/"10" → fields 20/10; "-1" → `load` returns an error naming the key; unset → 0.

- [ ] **Step 2: Run them to verify they fail**

Run (WSL): `go test ./internal/llm/ ./internal/llm/openai_compat/ -run 'SpeedPreferences' -count=1`
Expected: FAIL (fields undefined).

- [ ] **Step 3: Implement**

`config.go`: a helper `nonNegativeEnvInt(key string, field *int) error` used for both keys (the file is near the cap). `request.go`: replace the inline map with `openRouterProvider(cfg llm.Config) map[string]any` (same file), returning `data_collection: "deny"` plus each preference only when > 0.

- [ ] **Step 4: Run the tests, vet and race**

Run: `go vet ./internal/llm/... && go test -race -count=1 ./internal/llm/...`
Expected: PASS.

- [ ] **Step 5: Commit** `feat(llm): send OpenRouter's provider speed preferences when set`

### Task 2: The preferences are hot settings

**Files:**
- Modify: `internal/settings/settings.go` (catalog entries next to `AURA_CONTEXT_COMPACTION_TRIGGER_PERCENT`, `KindInt`, labels "OpenRouter: preferred minimum throughput (tokens/s)", "OpenRouter: preferred maximum latency (seconds)")
- Modify: `internal/agui/settings_api.go:41-54` (`hotLLMProfileKeys`)
- Modify: `cmd/aura/serve_settings.go` (`resolve`: start from `r.fallback` like the compaction trigger, then apply overrides through a non-negative parser; `EffectiveValue`: report both, "0" included)
- Modify: `.env.example` (both keys, commented, next to `AURA_LLM_OPENROUTER_MIDDLE_OUT` or the OpenRouter block, one line on what they do and the doc URL)
- Test: `cmd/aura/serve_settings_test.go`, `internal/agui/settings_api_applied_test.go`

**Interfaces:**
- Consumes: Task 1's fields and constants.

- [ ] **Step 1: Write the failing tests**

`serve_settings_test.go`, `TestReloaderAppliesOpenRouterSpeedPreferences`: overrides `{min:"20", latency:"10"}` → resolved cfg 20/10 and `EffectiveValue` returns "20"/"10"; override "abc" → error naming `AURA_OPENROUTER_PREFERRED_MIN_THROUGHPUT`; "-5" → error; a reset (key absent from overrides after a prior write) → back to the fallback value.

`settings_api_applied_test.go`: extend the hot-profile PUT test so a body with `"AURA_OPENROUTER_PREFERRED_MIN_THROUGHPUT":"20"` reaches `reloader.applied` and the store.

- [ ] **Step 2: Run them to verify they fail**

Run: `go test ./cmd/aura/ ./internal/agui/ -run 'SpeedPreferences|LLMProfile' -count=1`
Expected: FAIL.

- [ ] **Step 3: Implement** as listed under Files. Grep every `_test.go` with `rg -l hotLLMProfileKeys` and any tagged test counting the catalog before changing the lists.

- [ ] **Step 4: Run vet, the touched packages with race, and golangci-lint on them**

Run: `go vet ./... && go test -race -count=1 ./cmd/aura/ ./internal/agui/ ./internal/settings/ && golangci-lint run ./cmd/aura/ ./internal/agui/ ./internal/settings/ ./internal/llm/...`
Expected: PASS, 0 issues.

- [ ] **Step 5: Commit** `feat(settings): make the OpenRouter speed preferences hot settings`

### Task 3: The cockpit fields

**Files:**
- Modify: `web/src/settings/modelSettingsDefs.ts` (`SettingsKey` union; two `kind: 'int'` defs in `PRIMARY_SETTINGS` after `AURA_LLM_BASE_URL`, placeholder `'0'`, each with a `helpKey`)
- Modify: `web/src/settings/modelSettingsState.ts` (`HOT_LLM_PROFILE_KEYS`)
- Modify: `web/src/i18n/resources.settings.ts` (EN and IT labels and help)
- Test: the existing Vitest file covering `PRIMARY_SETTINGS`/`modelSettingsState` (find with `rg -l HOT_LLM_PROFILE_KEYS web/src`), or `web/src/settings/__tests__/modelSettingsDefs.test.ts` if none

Copy (EN / IT):
- label min: "OpenRouter: preferred min speed (tokens/s)" / "OpenRouter: velocità minima preferita (token/s)"
- label latency: "OpenRouter: preferred max latency (s)" / "OpenRouter: latenza massima preferita (s)"
- help (both): "OpenRouter only. Providers below this are tried last, never excluded. 0 sends nothing." / "Solo OpenRouter. I provider sotto soglia vengono provati per ultimi, mai esclusi. 0 non invia niente."

- [ ] **Step 1: Write the failing test**: both keys are in `PRIMARY_SETTINGS` with `kind: 'int'` and a `helpKey`, and both are hot (saved through the llm-profile batch, rendered "Si applica subito"/"Applies now").
- [ ] **Step 2: Run it to verify it fails**: `node node_modules/vitest/vitest.mjs run src/settings` (WSL, Node 24). Expected: FAIL.
- [ ] **Step 3: Implement** the defs, the hot list and the copy.
- [ ] **Step 4: Run** `npm run -s typecheck && npx oxlint --type-aware --max-warnings=0 src/settings src/i18n && node node_modules/vitest/vitest.mjs run src/settings` (WSL). Expected: PASS, 0 warnings.
- [ ] **Step 5: Commit** `feat(web): edit the OpenRouter speed preferences on the routing page`

### Task 4: Measure on the lab VM, then record it

**Files:**
- Create: `docs/superpowers/verification/2026-11-27-openrouter-provider-speed.md`
- Modify: `prd.md` (the OpenRouter routing paragraph of §6, a dated amendment with the measurement and what it does not show)
- Modify: this plan's status line, and the spec's defaults line only if the measurement sets non-zero defaults

- [ ] **Step 1:** Push (WSL recipe), wait for the VM updater to report the new `build_commit`.
- [ ] **Step 2:** Baseline: the Telegram request of `2026-11-27-llm-call-log.md` ("Cerca nella tua memoria cosa sai di ArcadeDB e riassumilo in una riga.") with both settings 0; record each `agent llm call end` (`provider`, `first_token_ms`, `duration_ms`, `cached_tokens`).
- [ ] **Step 3:** Set `AURA_OPENROUTER_PREFERRED_MIN_THROUGHPUT` from the cockpit (start at 20), repeat the turn, record; then latency (start at 10 s) with throughput 0, repeat, record. Cross-check each `response_id` on OpenRouter's Logs page (`?transaction=`), read-only.
- [ ] **Step 4:** If a threshold moved the call off Darkbloom, set that value as the shipped default in `internal/llm/config.go` with a test (red first) and a commit citing the verification file; otherwise leave 0 and say so. Put the VM settings back to what the operator had unless they ask to keep them.
- [ ] **Step 5:** Write the verification file (what was measured, what it does not prove: one model, one time of day, OpenRouter's 5-minute statistics, sticky routing with the thresholds set), the PRD amendment, the plan status; commit `docs(verification): OpenRouter speed preferences on the lab VM`; push.
