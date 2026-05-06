# Aura Agent Orchestration & System Prompt Versioning Plan

## Summary

Build a Claude Code style orchestration layer for Aura where the main LLM still drives the loop, but the runtime gives it a versioned system prompt, a focused tool profile, and stronger guidance for skills, swarm, and Python sandbox.

This is not DOCX-specific and not an intent-classifier milestone. The goal is to make Aura reliably choose between direct tools, swarm delegation, and sandbox execution without exposing every capability at once.

## Key Changes

- Add versioned prompt composition starting at `aura-agent-v1`.
- Compose prompt modules for base behavior, runtime context, overlays, skills, swarm, sandbox, memory, file generation, security, and wiki proposals.
- Add runtime tool profiles:
  - `default`: everyday memory/wiki/source/tasks/dashboard-token tools.
  - `memory`: read-heavy wiki/source/conversation search with review-gated proposals.
  - `swarm_research`: broad read-only synthesis via AuraBot swarm.
  - `sandbox_compute`: Python sandbox for calculations, data transforms, charts, parser experiments, generated artifacts, and debug scripts.
  - `document`: skills plus memory/source/wiki, optional swarm evidence, and typed DOCX/XLSX/PDF tools.
  - `admin_review`: proposal/review surfaces only.
- Filter Telegram tool definitions by active profile instead of exposing every registered tool.
- Add skill preflight policy for matching installed skills before DOCX, PDF, XLSX, source extraction, sandbox/coding workflows, and future MCP/plugin capabilities.
- Log orchestration telemetry per turn: prompt version/hash/modules, active profile, exposed tools, called tools, skill read, swarm usage, sandbox usage, tokens, estimated tokens, and cost.

## Public Interfaces

- Settings:
  - `AURA_PROMPT_VERSION=aura-agent-v1`
  - `AURA_TOOL_PROFILE_MODE=auto|default|memory|swarm_research|sandbox_compute|document`
  - `AURA_ORCHESTRATION_LOG_LEVEL=summary|debug`
- Dashboard settings exposes these under the `agent` group.
- `cmd/debug_telegram_sandbox` reports prompt version, hash, modules, tool profile, exposed tools, called tools, skill/swarm/sandbox usage, token usage, estimated context tokens, and cost.

## Test Plan

- Unit tests cover prompt metadata, profile selection, profile tool boundaries, filtered tool definitions, orchestration settings, and production skill roots.
- Integration tests cover Telegram debug telemetry and filtered profile behavior.
- Live smoke prompts:
  - Repo summary document should use skill read, optional swarm evidence, and typed file creation.
  - Computed CSV/chart should use `execute_code` and persist artifacts.
  - Pipeline review should prefer swarm-first synthesis.
- Release gate:
  - `go test ./internal/conversation ./internal/telegram ./internal/tools ./internal/toolsets ./internal/settings ./internal/api -count=1`
  - `go test ./cmd/debug_telegram_sandbox ./cmd/debug_files -count=1`
  - `docker compose --profile test run --rm test`

## Assumptions And Defaults

- No separate intent model in this milestone.
- Swarm and sandbox are first-class orchestration routes, not rare fallback tools.
- Tool profiles are configurable but start with conservative built-in defaults.
- Prompt overlays remain operator-editable and are included in the prompt hash.
- Existing tool names stay stable.
- Review gates remain mandatory for skill/plugin/admin mutation.
