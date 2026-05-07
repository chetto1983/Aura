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
