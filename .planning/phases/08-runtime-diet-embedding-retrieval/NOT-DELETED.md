# Phase 08 Not Deleted

Date: 2026-05-08

This file lists the remaining Runtime Diet candidates that are intentionally kept after the deletion pass.

## Kept

- `run_aurabot_swarm`, `read_swarm_result`, `list_swarm_tasks`: kept as explicit tools only. Normal turns do not expose them unless the user asks for subagents/swarm work.
- `internal/conversation/summarizer` proposal types and `internal/api/summaries.go`: kept as the manual review queue for approved wiki memory. The automatic post-turn summarizer runtime was deleted.
- `summarizer_v{n}` read compatibility in wiki/proposal parsing: kept only to read historical memory pages. New proposal writes use `proposal_v1`.
- `cmd/debug_*` smoke utilities: kept as explicit developer commands. They are not production Telegram tools.
- `list_sources` / `read_source` / `web_search` / `web_fetch`: kept for default/admin/compute routes, but removed from the normal document route so document generation cannot wander through broad evidence expansion by default.
- `MaxToolIterations` and `AgentLoopMaxSteps`: kept as real runtime configuration. Toolset-specific hidden `MaxSteps` caps were deleted so the DB/dashboard setting is not silently ignored.

## Moved To Follow-Up

- Full runner boundary: `internal/agentruntime` exists and owns event emission, but Telegram still owns some session persistence and terminal finalization. Move that into the runner in the next phase.
- Compact mirror observability: logs show `aura_memory_v1_compact` sync status, but `/status` and `/api/health` do not yet expose the last compact-memory/Qdrant sync result.
- Docker-first debug ergonomics: `cmd/debug_telegram_sandbox` now maps legacy `./wiki` and `./skills` to `runtime-workspace`, but the next phase should make Docker/runtime paths the default mental model for all smoke commands.
