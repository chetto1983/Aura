# Aura v3.1 Agent Orchestration Validation

## 2026-05-07 Implementation Slice: Hooks, Profile Contracts, And Debug Harness

Status: partial pass. v3.1 remains active.

Implemented:

- Added orchestration profile decision metadata with `profile_select_reason`.
- Added profile capability contracts through `ProfileCards`, including access level, allowed tools, denied tools, positive cues, and required availability.
- Added orchestration lifecycle hook primitives and default hidden-tool policy.
- Wired lifecycle hook injection points into the Telegram path for profile selection, prompt composition, tool exposure, tool calls, and turn completion.
- Added trace redaction for API keys, bearer tokens, Telegram tokens, and secret settings values.
- Wired Telegram telemetry to record profile selection reason and hidden-tool rejection using the actual exposed LLM tool definitions, not the broader profile allowlist.
- Made explicit profile selection availability-aware, so unavailable `sandbox_compute`/`swarm_research` modes fall back instead of exposing dead tools.
- Kept `admin_review` review-only by removing `run_task_now` from that profile.
- Extended `cmd/debug_telegram_sandbox` with route expectation flags:
  - `-expect-profile`
  - `-expect-no-tools`
  - `-expect-skill-read`
  - `-expect-swarm`
  - `-expect-sandbox`
- Added `cmd/debug_orchestration` for non-live prompt route inspection without Telegram or LLM calls.
- Added `admin_review` to the dashboard/API enum for `AURA_TOOL_PROFILE_MODE`.

Verification:

```powershell
go test ./internal/orchestration -count=1
go test ./cmd/debug_telegram_sandbox -count=1
go test ./internal/telegram -count=1
go test ./cmd/debug_orchestration -count=1
go test ./internal/conversation ./internal/telegram ./internal/tools ./internal/toolsets ./internal/settings ./internal/api ./internal/orchestration ./cmd/debug_telegram_sandbox ./cmd/debug_files ./cmd/debug_orchestration -count=1
go run ./cmd/debug_orchestration -prompt "facciamo il punto di tutta la pipeline"
go run ./cmd/debug_orchestration -prompt "calcola un CSV con grafico revenue"
go run ./cmd/debug_orchestration -prompt "calcola un CSV" -mode sandbox_compute -sandbox=false
go test ./... -count=1
```

Measured route probes:

- Pipeline review prompt selected `swarm_research`, reason `matched swarm_research broad synthesis cues`, and exposed `run_aurabot_swarm`, `read_swarm_result`, `list_swarm_tasks`, and read-only memory/source/wiki tools.
- Computed CSV/chart prompt selected `sandbox_compute`, reason `matched sandbox_compute compute/artifact cues`, and exposed `execute_code`, sandbox helper tools, and source reads.
- Explicit `sandbox_compute` with sandbox unavailable fell back to `default` with an unavailable-runtime reason.

Remaining v3.1 gates:

- Full live Telegram route scorecard with configured DB model.
- Dashboard settings E2E for orchestration enum persistence if UI changes.
- Docker rebuild smoke.
- Final `.planning/ROADMAP.md` and `.planning/STATE.md` closeout after all strict gates pass.

## 2026-05-07 E2E Harness DB Settings Fix

Status: pass for sandbox route; swarm route still needs latency work.

Root cause:

- The Docker app correctly used DB-backed LLM settings from `/data/aura.db`.
- The local `cmd/debug_telegram_sandbox` harness loaded `data\.env`, then interpreted `DB_PATH=./aura.db` relative to `D:\Aura`, opening `D:\Aura\aura.db` instead of `D:\Aura\data\aura.db`.
- That stale local DB/env path made the debug smoke report the wrong model (`glm-5.1:cloud`) even while the live dashboard settings showed the DB model (`deepseek/deepseek-v4-flash` on OpenRouter).

Fix:

- `cmd/debug_telegram_sandbox` now resolves relative `DB_PATH` values from the directory containing the loaded `AURA_ENV_PATH`.
- The smoke output now prints `db_path`, `db_source_path`, and `db_write_live` so future E2E runs prove which database supplied settings.
- The harness now copies the configured DB to a temporary debug DB by default and writes smoke-test rows only there. Direct writes to the live DB require the explicit `-write-live-db` flag.

Operational repair during validation:

- A Docker restart exposed SQLite integrity errors in `/data/aura.db`.
- The damaged root pages mapped to derived tables: `embedding_cache` and `wiki_documents` FTS data.
- A byte-for-byte backup was saved under `data\repair-backup-20260507-123911`, plus the corrupt DB was preserved as `data\aura.corrupt-20260507-124152.db`.
- Recovered with SQLite `.recover`, then dropped/recreated only the derived `wiki_documents` FTS index.
- Settings, API token, and allowed-user counts matched before/after recovery (`settings=33`, `api_tokens=4`, `allowed_users=2`).
- Docker Aura restarted healthy afterward with database integrity ok.

Verification:

```powershell
go test ./cmd/debug_telegram_sandbox -count=1
go test ./internal/orchestration ./internal/telegram -count=1
go test ./... -count=1
$env:AURA_ENV_PATH='data\.env'; go run ./cmd/debug_telegram_sandbox -timeout 3m -expect-profile sandbox_compute -expect-tools execute_code -expect-sandbox
$env:AURA_HOST_PORT='18080'; docker compose up -d --build aura
Invoke-RestMethod -Uri http://127.0.0.1:18080/status
```

Measured live smoke:

- `db_source_path=data\aura.db`
- `db_write_live=false`
- `model=deepseek/deepseek-v4-flash`
- `base_url=https://openrouter.ai/api/v1`
- `tool_profile=sandbox_compute`
- `tools_called=execute_code`
- `sandbox_used=true`
- `estimated_context_tokens=4982`
- `elapsed_ms=19133`
- `token_usage_reported=false`; OpenRouter-compatible response did not include usage in this streamed run, so cost remained `0.000000` in Aura telemetry.

Dashboard/API settings check:

- `LLM_BASE_URL=https://openrouter.ai/api/v1`, source `db`.
- `LLM_MODEL=deepseek/deepseek-v4-flash`, source `db`.
- `LLM_API_KEY=(configured)`, source `db`.
- `AURA_TOOL_PROFILE_MODE=auto`.

Docker health after repair/rebuild:

- Aura container healthy on `127.0.0.1:18080`.
- `/status` reports `database: integrity ok`.

## 2026-05-07 Pyodide Sidecar And Storage Decision

Status: pass for runtime migration; v3.1 still needs final orchestration closure gates.

Implemented:

- Docker installs use a dedicated `pyodide` sidecar with `SANDBOX_RUNTIME_MODE=container` and `SANDBOX_RUNTIME_URL=http://pyodide:8787`.
- The Aura image no longer installs Node.js or copies `/app/runtime/pyodide`.
- The sidecar is based on `pyodide/pyodide-env` and overlays Aura's HTTP runner shim.
- `execute_code` now loads Pyodide packages from actual Python imports instead of loading the full office/data profile for every call.
- Dashboard settings expose `SANDBOX_RUNTIME_MODE` and `SANDBOX_RUNTIME_URL`.
- MongoDB was evaluated and deferred. SQLite remains canonical state; MongoDB is only a future optional adapter candidate for high-volume archives, swarm traces, or audit logs after metrics justify it.

Verification:

```powershell
docker compose build --no-cache pyodide
$env:AURA_HOST_PORT='18080'; docker compose up -d --force-recreate pyodide aura
Invoke-RestMethod -Uri http://127.0.0.1:18080/status
docker compose --profile test run --rm --no-deps test go run ./cmd/debug_sandbox -tool-smoke -runtime-url http://pyodide:8787 -timeout 3m
docker compose --profile test run --rm --no-deps test go run ./cmd/debug_sandbox -artifact-smoke -runtime-url http://pyodide:8787 -timeout 5m
go test ./... -count=1
npm --prefix web run i18n:check
npm --prefix web run build
docker compose config --quiet
```

Measured runtime:

- Tool smoke: `5050`, `elapsed_ms=4`.
- Artifact smoke: CSV and PNG persisted as source artifacts, `elapsed_ms=4934`.
- Full all-import `debug_sandbox -smoke` is not a sidecar release gate yet; it exceeded the 15 minute debug timeout because it intentionally loads the legacy full office/data profile.

## 2026-05-07 Hermes-Style Swarm Delegation Hardening

Status: pass for the focused swarm closure smoke. v3.1 remains active until the remaining full release gate is re-run.

Implemented:

- `swarm_research` parent profile now exposes only `run_aurabot_swarm`, `read_swarm_result`, and `list_swarm_tasks`.
- Parent Telegram loop finalizes immediately after a successful `run_aurabot_swarm` in `swarm_research`.
- Duplicate `run_aurabot_swarm` calls in one assistant turn are capped to the first execution.
- Debug smoke reports `terminal_swarm`, `swarm_finalization`, `post_swarm_tool_calls`, `elapsed_ms`, token metrics, and cost.
- Swarm delegation policy now clamps runtime-owned limits:
  - `SWARM_RESEARCH_MAX_WORKERS`, default `1`, hard max `3`.
  - `SWARM_RESEARCH_TIMEOUT_MS`, default `25000`, hard max `30000`.
  - `SWARM_RESEARCH_CHILD_MAX_ITERATIONS`, default `3`.
  - `SWARM_RESEARCH_MAX_RESULT_CHARS`, default `12000`.
  - `SWARM_RESEARCH_FINALIZATION=aggregate|no_tool_llm`.
- Model-provided worker roles are validated for stale aliases but the fast route uses the runtime-owned worker mix.
- Default fast worker mix is single-worker `librarian`; `synthesizer`, `researcher`, `critic`, and `skillsmith` remain available behind explicit runtime tuning.
- The `swarm_research` Telegram route now launches `run_aurabot_swarm` directly from the runtime instead of spending a first parent LLM call deciding to call the swarm.
- Swarm manager now persists completed task/run state with a non-cancelled context after worker deadlines, so partial/timeout-adjacent metrics are not lost.

Verification:

```powershell
go test ./internal/orchestration ./internal/swarm ./internal/swarmtools ./internal/telegram ./cmd/debug_orchestration ./cmd/debug_telegram_sandbox -count=1
go test ./... -count=1
go run ./cmd/debug_orchestration -prompt "facciamo il punto di tutta la pipeline Aura e dimmi cosa manca per chiudere v3.1"
$env:AURA_ENV_PATH='data\.env'; go run ./cmd/debug_telegram_sandbox -timeout 90s -no-validate -expect-profile swarm_research -expect-tools run_aurabot_swarm -expect-swarm -expect-terminal-swarm -expect-token-metrics -max-elapsed-ms 30000 -prompt "facciamo il punto di tutta la pipeline Aura e dimmi cosa manca per chiudere v3.1"
$env:AURA_HOST_PORT='18080'; docker compose up -d --build aura
Invoke-RestMethod -Uri http://127.0.0.1:18080/status
```

Measured deterministic route probe:

- `tool_profile=swarm_research`
- `profile_select_reason=matched swarm_research broad synthesis cues`
- `tools_exposed=run_aurabot_swarm,read_swarm_result,list_swarm_tasks`

Measured live smoke on configured DB model:

- `model=deepseek/deepseek-v4-flash`
- `base_url=https://openrouter.ai/api/v1`
- `tool_calls=run_aurabot_swarm`
- `tool_profile=swarm_research`
- `tools_exposed=list_swarm_tasks,read_swarm_result,run_aurabot_swarm`
- `terminal_swarm=true`
- `swarm_finalization=aggregate`
- `post_swarm_tool_calls=0`
- `duplicate_swarm_rejected=false`
- `worker_count=1`
- `worker_failures=0`
- `final=Run swarm_0c735eb08723b78f (completed): 1/1 completed, 0 failed, 0 running, 0 pending. Roles: librarian=completed.`
- `token_usage_reported=true`
- `tokens_prompt=1227`
- `tokens_completion=218`
- `tokens_total=1445`
- `cost_usd=0.002051`
- `elapsed_ms=25925`

Notes:

- The local debug harness still logs `QDRANT_URL is required` because Compose-only environment values are not present in `data\.env`; this does not affect the swarm delegation smoke, but the full Docker closure gate should run inside Compose or pass the Compose Qdrant env explicitly.
- Docker rebuild initially failed when `AURA_HOST_PORT` was not exported and Compose tried to bind `127.0.0.1:8080`; rerunning with `AURA_HOST_PORT=18080` recreated Aura successfully and `/status` returned `status=ok`.

Post-commit Docker E2E:

```powershell
$env:AURA_HOST_PORT='18080'; docker compose up -d --build aura
Invoke-RestMethod -Uri http://127.0.0.1:18080/status
$env:AURA_ENV_PATH='data\.env'; go run ./cmd/debug_telegram_sandbox -timeout 90s -no-validate -expect-profile swarm_research -expect-tools run_aurabot_swarm -expect-swarm -expect-terminal-swarm -expect-token-metrics -max-elapsed-ms 30000 -prompt "facciamo il punto di tutta la pipeline Aura e dimmi cosa manca per chiudere v3.1"
```

Measured after container rebuild:

- `/status`: `status=ok`, `version=3.0`.
- `tool_profile=swarm_research`
- `tool_calls=run_aurabot_swarm`
- `terminal_swarm=true`
- `post_swarm_tool_calls=0`
- `duplicate_swarm_rejected=false`
- `worker_count=1`
- `worker_failures=0`
- `token_usage_reported=true`
- `tokens_prompt=3217`
- `tokens_completion=1401`
- `tokens_total=4618`
- `cost_usd=0.008281`
- `elapsed_ms=21706`
