# Phase 08 Evidence

## 2026-05-08 Slice 1: Hot Path Cuts

Goal: remove default-turn ceremony while preserving Aura's useful second-brain capabilities.

Cuts made:

- Removed the user-facing budget fallback from `internal/agentloop/loop.go`. Budget exhaustion now returns the last successful tool result when one exists.
- Removed the `FallbackMessage` option and the `fallbackMessage` function.
- Stopped generic turns from running speculative embedding search or injecting a Memory Pack.
- Scoped graph context to graph/document routes and recent wiki log to log routes.
- Cleared stale search context every turn by always refreshing `SetSearchContext`, even with an empty string.
- Stopped loading and injecting the full skill manifest unless the prompt explicitly asks about skills/procedures.
- Stopped exposing AuraBot swarm in the default turn unless the prompt explicitly asks for swarm/subagents.
- Made post-turn summarizer opt-in by default: `SUMMARIZER_ENABLED=false`, `SUMMARIZER_MODE=off`.
- Made nightly auto-improve opt-in by default: `SANDBOX_AUTO_IMPROVE_MODE=off`, and startup no longer bootstraps `nightly-auto-improve` unless enabled.
- Startup cancels an existing `nightly-auto-improve` row when the mode is `off`, so old databases do not keep running the previous default.

Focused verification:

```powershell
go test ./internal/agentloop -count=1
go test ./internal/config ./internal/settings ./internal/api ./internal/telegram ./internal/orchestration ./internal/conversation -count=1
go test ./...
go build ./...
docker compose config --quiet
rg -n "Mi sono fermato|fallbackMessage|FallbackMessage" internal
```

Observed:

- `internal/agentloop` passed.
- focused config/settings/API/Telegram/orchestration/conversation tests passed.
- full `go test ./...` passed.
- `go build ./...` passed.
- `docker compose config --quiet` passed.
- the fallback string/function search returned no matches in `internal/`.

Not cut yet:

- `search_memory` remains, but ranking still needs Phase 08 Task 4/5.
- `internal/orchestration` profiles still exist, but swarm and skill manifest are no longer automatic in the live Telegram turn.
- `internal/conversation/swarm_prompt.go` remains for explicit swarm use and tests, but `handleConversation` no longer injects `SwarmTurnHint`.
