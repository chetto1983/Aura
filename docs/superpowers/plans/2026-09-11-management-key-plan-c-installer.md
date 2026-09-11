# Management Key — Plan C (Installer, Compose, `.env`) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** No OpenRouter credential, route or model travels through `create-aura`, `install.conf`, `.env` or `compose.yaml`; `.env.example` lists the required secrets and the values that differ from compose, and a test keeps it that way.

**Architecture:** `create-aura` stops asking for the model route and key and writes `install.conf` `format=2` (install dir, appliance, gvisor). `install.sh` accepts only `format=2` and writes no LLM key into `.env`. `compose.yaml` stops passing `OPENROUTER_API_KEY`, `AURA_OPENROUTER_MANAGEMENT_KEY` and `AURA_LLM_PROVIDER/MODEL/BASE_URL` to `aura` and `aura-ingest`, and `TELEGRAM_BOT_TOKEN` to `aura`: the daemon and `aura-media-index` read them from `aura.settings` (Plan A) or fall back to the code defaults, which equal the compose defaults they replace (`internal/llm/config.go:26-27`). `AURA_EMBED_DIMENSIONS` leaves the Settings allowlist. `.env.example` is rewritten by rule and guarded by a Go test.

**Tech Stack:** TypeScript + Vitest (`packages/create-aura`), Bash (`install.sh`, `install_config_test.sh`), Go 1.27, Docker Compose.

**Spec:** `docs/superpowers/specs/2026-09-10-management-key-onboarding-design.md` (Delivery item C). Plans A and B are landed.

## Global Constraints

- `master`, one commit per task, imperative subject + why, last line `Co-Authored-By: Claude Opus 5 (1M context) <noreply@anthropic.com>`. Each task commit ticks its boxes here.
- A task that touches `compose.yaml`, `install.sh` or another payload file runs `bash scripts/payload_manifest_gate.sh --write` and commits `scripts/payload_manifest.txt`. The gate hashes the file on disk: check the file is LF (`file compose.yaml`), or CI refuses a manifest the local gate accepted.
- `create-aura`: `cd packages/create-aura && npm test && npm run build`.
- Go: `go vet`, `go build ./...`, `go test` on touched packages, race in WSL.
- Scripts: `bash scripts/install_config_test.sh`.

## Measured before planning (2026-09-11, this checkout)

`.env.example` has 222 active names. 7 have no reader outside documentation: the `AURA_DOCUMENT_*` knobs other than `AURA_DOCUMENT_RETRIEVAL_CANDIDATES` (`DENSE_MAX_DISTANCE_RATIO`, `EMBED_BATCH_SIZE`, `LEXICAL_MIN_SCORE`, `MAX_ACTIVE_PASSAGES`, `MAX_PASSAGES_PER_VERSION`, `PIPELINE_LEASE_SEC`, `PIPELINE_MAX_ATTEMPTS`). 124 active lines equal the single default compose gives the same name. The spec's 11 and 69 were measured against the running containers' environment; this plan compares the file with compose, which is what a test can check. What the measurement does not show: whether an operator's own `.env` relies on one of those lines. It cannot change behaviour, because compose supplies the same value.

## Deviations from the spec

1. A line equal to its compose default is commented out, not deleted, so the explanation above it keeps its example. The guard reads active lines only.
2. `OPENROUTER_API_KEY`, `AURA_OPENROUTER_MANAGEMENT_KEY`, `AURA_LLM_BASE_URL`, `AURA_LLM_MODEL`, `TELEGRAM_BOT_TOKEN` and `AURA_EMBED_DIMENSIONS` leave `.env.example` by hand: they still have readers, so the guard alone would keep them.
3. `installLocal` and `installRemote` lose their `settings` parameter: it only carried the OpenRouter key into the redaction list, and nothing secret travels any more.
4. `musr_live_run_preconditions.sh` checks the management key in the store instead of `OPENROUTER_API_KEY`, because every identity's key is minted from it.

---

### Task 1: The installer asks for infrastructure only

**Files:** `packages/create-aura/src/{types,prompts,config-file,validation,cli,local,remote}.ts`, `src/messages/{en,it}.ts`; delete `src/modelroute.ts` and `src/__tests__/modelroute.test.ts`; tests `config-file`, `prompts`, `validation`, `cli`, `cli-test-support`, `cli_local_preflight`, `local`, `remote`; `package.json` (0.1.4 → 0.2.0: the config format changed); `README.md`; `scripts/install.sh`, `scripts/install_config_test.sh`, `scripts/payload_manifest.txt`.

**Produces:** `InstallSettings { installDir; appliance; gvisor }`; `collectSettings(prompt, t, installDir)`; `install.conf` = `format=2`, `install_dir_base64`, `appliance`, `gvisor`.

- [x] **Step 1: Tests.** `config-file.test.ts`: the serialized config is exactly those four lines; `prompts.test.ts`: `collectSettings` asks the appliance, gVisor and confirmation questions and nothing else; `install_config_test.sh`: a `format=2` file parses, a `format=1` file is refused as an unsupported format, an `llm_model_base64` key is refused as unknown, and `apply_install_config` no longer exists.
- [x] **Step 2: Run, watch them fail.**
- [x] **Step 3: Implement.** Drop the route, key and model prompts, `collectOllamaModel`, `modelroute.ts`, `createSshProbeRunner` and `shellQuote`, `validateBaseUrl`, `validateModelId`, their messages and error-code mappings, and the probe runner in `cli.ts`. In `install.sh`: `CFG_LLM_*` and `CFG_OPENROUTER_API_KEY`, their parse cases and line-break check, `apply_install_config` and its call, and the `OPENROUTER_API_KEY` line and `openrouter_key` variable of the fresh `.env` template go; `parse_install_config` accepts `format=2` only.
- [x] **Step 4: Run, watch them pass;** `npm run build`; regenerate the payload manifest.
- [x] **Step 5: Commit** `feat(installer): ask for infrastructure only, in install.conf format 2`.

---

### Task 2: Compose stops carrying the credentials and the route

**Files:** `compose.yaml` (the `aura` and `aura-ingest` environment blocks; the local-LLM comment that told the operator to set `AURA_LLM_*` in `.env`), `cmd/aura/container_artifacts_test.go`, `scripts/musr_live_run_preconditions.sh`, `scripts/telegram_e2e.sh`, `scripts/ingest_reconcile_e2e.sh`, `scripts/payload_manifest.txt`.

- [ ] **Step 1: Test.** `container_artifacts_test.go` drops the five assertions that pinned these lines and the `AURA_LLM_MODEL` default pattern, and asserts instead that no service passes `OPENROUTER_API_KEY`, `AURA_OPENROUTER_MANAGEMENT_KEY`, `AURA_LLM_PROVIDER`, `AURA_LLM_MODEL`, `AURA_LLM_BASE_URL` or `TELEGRAM_BOT_TOKEN`.
- [ ] **Step 2: Run, watch it fail.**
- [ ] **Step 3: Implement.** Remove the lines from compose. `telegram_e2e.sh` stops exporting `OPENROUTER_API_KEY` from `.env` (the host `aura serve` reads the store); `ingest_reconcile_e2e.sh` passes `AURA_LLM_*` and the key to its containers only when an `AURA_DOCUMENT_E2E_LLM_*` override asks for it; `musr_live_run_preconditions.sh` requires `AURA_OPENROUTER_MANAGEMENT_KEY` in the store.
- [ ] **Step 4: Run, watch it pass;** `bash -n` on the three scripts; regenerate the payload manifest.
- [ ] **Step 5: Commit** `feat(compose): stop passing the OpenRouter credentials and the route`.

---

### Task 3: `AURA_EMBED_DIMENSIONS` leaves the Settings

**Files:** `internal/settings/settings.go` and `settings_test.go`, `internal/agui/{settings_api_test,settings_api_applied_test,settings_api_validate}.go`, `web/src/settings/modelSettingsDefs.ts`, `web/src/i18n/resources.settings.ts`, `web/src/settings/__tests__/ModelSettingsPanel.applied.test.tsx`.

The embedding model file fixes the width and changing it breaks the vector index, so it stays an environment default read by the daemon (`config.go:416`) and the Python ingest (`services/ingest/app.py:37`), never a Settings row.

- [ ] **Step 1: Test.** `settings_test.go`: `AURA_EMBED_DIMENSIONS` is not allowed. The tests that used it as an example int or boot-bound row switch to another such key.
- [ ] **Step 2: Run, watch it fail.**
- [ ] **Step 3: Implement** the allowlist, Settings pane and copy removals.
- [ ] **Step 4: Run, watch them pass** (Go and web).
- [ ] **Step 5: Commit** `refactor(settings): take the embedding width out of the Settings`.

---

### Task 4: `.env.example` by rule, guarded

**Files:** `.env.example`, `cmd/aura/env_example_test.go` (new).

- [ ] **Step 1: Test.** `TestEnvExampleNamesHaveReaders` and `TestEnvExampleValuesDifferFromCompose`: every active `NAME=value` line names a variable some tracked code, compose file or script reads, and none repeats the single default compose gives that name.
- [ ] **Step 2: Run, watch it fail** (7 names without readers, 124 values equal to compose).
- [ ] **Step 3: Implement.** Delete the 7 dead names and the six names of deviation 2, rewriting their comments to point at the first-run setup and Settings; comment out every other line equal to its compose default.
- [ ] **Step 4: Run, watch it pass.**
- [ ] **Step 5: Commit** `refactor(env): keep .env.example to secrets and what differs from compose`.

---

### Task 5: Gates and push

- [ ] **Step 1:** `make quality` in WSL; `npm test` in `packages/create-aura`; `bash scripts/install_config_test.sh`; `bash scripts/payload_manifest_gate.sh`.
- [ ] **Step 2:** push, watch CI green.
