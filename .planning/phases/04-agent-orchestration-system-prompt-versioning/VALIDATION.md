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
