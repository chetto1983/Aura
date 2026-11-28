# OpenRouter provider sort and route timeouts Implementation Plan

**Status (2026-11-28, measured on the lab VM):** DONE. Tasks 1-3 landed (c656f2590, 15bde6498, 33e905569, plus the Prettier fix 943e476bb). Task 4 ran on the VM at 031fdc60c (`docs/superpowers/verification/2026-11-28-openrouter-provider-sort.md`, PRD amendment #230). On `tencent/hy4-flash`, the operator's model by then, every sort value went to Tencent in 4-7 s a call, so the default stays empty and the operator's settings were reset. Open:
- whether `sort` moves a fresh session;
- whether the 90 s window lets a real fallback finish.

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** A hot cockpit choice sends OpenRouter's `provider.sort`, replacing the two speed thresholds. On the OpenRouter route the stream idle window and call timeout default to 90 s and 180 s, so OpenRouter's fallback can answer.

**Architecture:**
- The sort is one string field on `llm.Config`, parsed and hot-reloaded on the thresholds' path, which this plan removes: settings catalog → `hotLLMProfileKeys` → `primaryLLMRouteReloader.resolve`. `openRouterProvider` sends it.
- The timeouts get `…Configured` flags, like `ContextWindowConfigured`, and two `Config` methods. The methods return the configured value, or the route's default when unset. Every reader calls the methods.

**Tech Stack:** Go (openai-go v3 request options), React/TypeScript cockpit, Vitest.

**Spec:** `docs/superpowers/specs/2026-11-28-openrouter-provider-sort-design.md`

## Global Constraints

- Env and setting name: `AURA_OPENROUTER_PROVIDER_SORT`.
  - Accepted values: `""`, `price`, `throughput`, `latency`, trimmed and lower-cased.
  - Anything else is refused, with the key named in the error.
- Wire: `{"provider":{"data_collection":"deny","sort":"throughput"}}`. An empty value sends no `sort` key. Only the OpenRouter route sends a `provider` object, as today.
- Route defaults when the env and file leave them unset:
  - OpenRouter (`llm.ReasoningTarget(...) == llm.ReasoningTargetOpenRouter`): idle 90 s, total 180 s;
  - every other route: 60 s and 120 s.
  - An explicit `AURA_LLM_STREAM_IDLE_TIMEOUT_SEC` or `AURA_LLM_TOTAL_TIMEOUT_SEC` wins on every route. An explicit idle 0 still disables the watchdog.
- Remove, with no residue (`git grep` returns nothing outside `docs/` and `prd.md`):
  - `AURA_OPENROUTER_PREFERRED_*`;
  - `OpenRouterPreferred*`;
  - `nonNegativeEnvInt` and `nonNegativeLLMSetting`, if they are left without a caller;
  - `config_openrouter_speed_test.go`, replaced by the sort test;
  - the i18n keys `openRouterMinThroughput`, `openRouterMaxLatency` and `openRouterProviderSpeed`.
- Default sort stays empty until Task 4 measures otherwise.
- No file over 600 LOC.

## Review Focus

- **A hot route switch.** Cloud→Local with no timeout env must give 60/120 on the next call, and back to Cloud 90/180. Task 2 covers this in the reloader test.
- **The sort saved, then the route switched to llama.cpp.** No `provider` key, as today. Task 1's request test keeps the non-OpenRouter row.
- **A sort value with a different case or spaces** (" Throughput "). It is accepted as `throughput`. Task 1's config test and Task 2's reloader test cover it.
- **The chatgpt and anthropic clients.** They read the idle window through the method and so keep 60 s. Task 2's config test covers both targets.
- **The cockpit choice set back to empty.** One PUT with `""` and no `sort` on the wire. Task 3 covers the PUT; Task 1 covers the wire.

---

### Task 1: The sort replaces the thresholds

**Files:**
- Modify: `internal/llm/config.go`, `internal/llm/openai_compat/request.go`, `internal/settings/settings.go`, `internal/agui/settings_api.go`, `cmd/aura/serve_settings.go`, `.env.example`
- Test: `internal/llm/openai_compat/client_request_sdk_test.go`, `internal/llm/config_test.go` (`clearLLMEnv`), `cmd/aura/serve_settings_test.go`, `internal/agui/settings_api_applied_test.go`
- Delete: `internal/llm/config_openrouter_speed_test.go`; Create: `internal/llm/config_openrouter_sort_test.go`

**Interfaces:**
- Produces:
  - `llm.Config.OpenRouterProviderSort string`;
  - `llm.EnvOpenRouterProviderSort = "AURA_OPENROUTER_PROVIDER_SORT"`;
  - `llm.ParseOpenRouterProviderSort(value string) (string, error)`, used by the env parse and the reloader.

- [ ] **Step 1: Write the failing tests**
  - `TestOpenRouterProviderCarriesSort`, which replaces `TestOpenRouterProviderCarriesSpeedPreferences`:
    - `throughput` → `{"data_collection":"deny","sort":"throughput"}`;
    - `""` → `{"data_collection":"deny"}`;
    - a llama.cpp route with `latency` → no `provider` key.
  - `TestOpenRouterProviderSortFromEnv`:
    - `" Latency "` → `latency`;
    - unset → `""`;
    - `"fastest"` → an error naming the key.
  - `TestPrimaryLLMRouteReloaderResolvesOpenRouterProviderSort`, which replaces the speed reloader test:
    - an override of `throughput` resolves, and `EffectiveValue` reports it;
    - a reset falls back to the boot value;
    - `"fastest"` is refused with the key named.
  - Update `TestHandlePutLLMProfileAcceptsLoopBudgetAndTrigger` to put `AURA_OPENROUTER_PROVIDER_SORT":"throughput"` in place of the two thresholds.
- [ ] **Step 2: Run them to verify they fail**
  - Run (WSL): `go test ./internal/llm/... ./cmd/aura/ ./internal/agui/ -run 'ProviderSort|AcceptsLoopBudgetAndTrigger' -count=1`
  - Expected: FAIL (undefined `OpenRouterProviderSort`).
- [ ] **Step 3: Implement**
  - Swap the two threshold fields, constants and parses for the sort.
  - Catalog: `KindString`, label "OpenRouter: sort providers by".
  - `.env.example`: one commented line with the three values, replacing the threshold block.
  - Delete the threshold helpers that are left without a caller.
- [ ] **Step 4: Run** `go vet ./... && go build ./... && go test -race -count=1 ./internal/llm/... ./cmd/aura/ ./internal/agui/ ./internal/settings/`
  - Expected: PASS.
  - Then `git grep -n "OPENROUTER_PREFERRED\|OpenRouterPreferred" -- ':!docs' ':!prd.md' ':!web'`, expected empty.
- [ ] **Step 5: Commit** `feat(llm): sort OpenRouter's providers instead of the speed thresholds`.

### Task 2: OpenRouter's timeouts let its fallback answer

**Files:**
- Modify:
  - `internal/llm/config.go` (flags, route defaults, two methods, and the corrected watchdog comment);
  - `internal/llm/openai_compat/client.go`, `internal/llm/chatgpt/client.go`, `internal/llm/anthropic/client.go`;
  - `internal/agent/llm_agent.go`, `internal/agent/llm_agent_finalize.go`;
  - `internal/runner/runner_context.go`, `internal/llm/model_reasoning_caps.go`.
- Test: `internal/llm/config_route_timeouts_test.go` (new), `cmd/aura/serve_settings_test.go`

**Interfaces:**
- Produces:
  - `llm.Config.StreamIdleTimeoutConfigured bool` and `llm.Config.TotalTimeoutConfigured bool`, set wherever the file or the env sets the value;
  - `func (c Config) StreamIdleTimeout() time.Duration`;
  - `func (c Config) CallTimeout() time.Duration`.

- [ ] **Step 1: Write the failing tests**
  - `TestRouteTimeoutsFollowTheRoute`, table-driven:
    - the OpenRouter route, unset → 90 s / 180 s;
    - llama.cpp, Ollama, ChatGPT and Anthropic routes, unset → 60 s / 120 s;
    - env idle `30` and total `100` on OpenRouter → 30 s / 100 s;
    - env idle `0` on OpenRouter → 0, the watchdog disabled.
  - `TestPrimaryLLMRouteReloaderSwitchesRouteTimeouts`:
    - a boot config on OpenRouter with nothing configured;
    - an override of `AURA_LLM_BASE_URL` to a llama.cpp URL with `AURA_LLM_PROVIDER=llamacpp` → `CallTimeout()` 120 s, `StreamIdleTimeout()` 60 s.
- [ ] **Step 2: Run them to verify they fail**
  - Run (WSL): `go test ./internal/llm/ ./cmd/aura/ -run 'RouteTimeouts' -count=1`
  - Expected: FAIL (undefined methods).
- [ ] **Step 3: Implement**
  - Add the methods and flags.
  - Switch every reader to the methods: `git grep -n "TotalTimeoutSec)\|StreamIdleTimeoutSec)"` outside config.go must be empty afterwards.
  - Fix the comment at `config.go:35-40`: OpenRouter can send nothing while it waits on a provider (verification 2026-11-28).
- [ ] **Step 4: Run** `go vet ./... && go build ./... && go test -race -count=1 ./internal/llm/... ./internal/agent/ ./internal/runner/ ./cmd/aura/`
  - Expected: PASS.
- [ ] **Step 5: Commit** `fix(llm): give OpenRouter's fallback time to answer`.

### Task 3: The cockpit's sort choice

**Files:**
- Modify:
  - `web/src/settings/modelSettingsDefs.ts`: an optional `options: readonly { value: string; labelKey: string }[]` on `SettingDef`; one def replaces the two threshold defs;
  - `web/src/settings/SettingField.tsx`: a def with `options` renders `components/ui/select.tsx`;
  - `web/src/settings/modelSettingsState.ts`;
  - `web/src/i18n/resources.settings.ts`.
- Test: `web/src/settings/__tests__/ModelSettingsPanel.openrouter.test.tsx`

Copy:

| | EN | IT |
|---|---|---|
| Label | "OpenRouter: sort providers by" | "OpenRouter: ordina i provider per" |
| Option `""` | "OpenRouter default" | "Predefinito di OpenRouter" |
| Option `price` | "Price" | "Prezzo" |
| Option `throughput` | "Throughput" | "Velocità" |
| Option `latency` | "Latency" | "Latenza" |
| Help | "OpenRouter only. Tries providers in this order instead of its price-weighted default. Throughput is not :nitro: it does not admit priority-tier endpoints." | "Solo OpenRouter. Prova i provider in quest'ordine invece del predefinito pesato sul prezzo. Velocità non è :nitro: non ammette gli endpoint priority." |

- [ ] **Step 1: Write the failing test**
  - Name: 'saves the provider sort through the hot-profile batch', replacing the speed test.
  - Choose "Throughput" in the select labelled "OpenRouter: sort providers by".
  - Save → exactly one PUT, `/api/settings/llm-profile`, body `{"settings":{"AURA_OPENROUTER_PROVIDER_SORT":"throughput"}}`.
- [ ] **Step 2: Run it to verify it fails**
  - Run (WSL): `node node_modules/vitest/vitest.mjs run src/settings/__tests__/ModelSettingsPanel.openrouter.test.tsx`
  - Expected: FAIL (no such label).
- [ ] **Step 3: Implement**
  - Add the option type, the select rendering and the copy.
  - Remove the two threshold defs and their copy.
- [ ] **Step 4: Run** `npm run -s typecheck`, `npx oxlint src/settings src/i18n` and `node node_modules/vitest/vitest.mjs run src/settings`.
  - Expected: all green.
- [ ] **Step 5: Commit** `feat(web): choose how OpenRouter sorts its providers`.

### Task 4: Measure on the lab VM, then record it

**Files:**
- Create: `docs/superpowers/verification/2026-11-28-openrouter-provider-sort.md`
- Modify: `prd.md` (a dated amendment after #229), this plan's status line

- [ ] **Step 1:** Push (WSL recipe). Wait for the VM's `build_commit`.
- [ ] **Step 2:** On the same Telegram thread, send "Cerca nella tua memoria cosa sai di ArcadeDB e riassumilo in una riga." three times:
  - with sort `throughput`;
  - with sort `latency`;
  - with sort empty.
  - Record each `agent llm call end` and each `agent llm call error`.
  - Cross-check every generation on OpenRouter's Logs page (provider responses, routing time), read-only.
- [ ] **Step 3:** Ship a non-empty default only when a value took the thread off Darkbloom at a lower turn time, with a test first and a commit citing the verification file. Otherwise the default stays empty. Reset the VM's sort to what the operator had, unless they ask to keep it.
- [ ] **Step 4:** Write the verification file. Say what it does not prove: one model, one thread and session, one hour, cost per call.
  - Write the PRD amendment and this plan's status.
  - Commit `docs(verification): OpenRouter provider sort on the lab VM`, then push.
