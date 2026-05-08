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

Closure update:

- Phase 08 later completed Task 4/5: `search_memory` now uses calibrated hybrid retrieval and compact source/archive/proposal facts instead of raw scans.
- The old profile/preflight taxonomy was physically deleted from live code. The remaining concept is the simpler runtime `Toolset`.
- The old swarm-routing prompt helper was deleted. Swarm remains only as explicit tools when exposed by the selected toolset.

## 2026-05-08 Closure Evidence

Phase 08 closed after Docker E2E.

Verification:

```powershell
go test ./internal/search ./internal/memoryindex ./internal/telegram -count=1
go test ./...
go build ./...
docker compose up -d --build aura
powershell -NoProfile -ExecutionPolicy Bypass -File scripts\test-compact-memory-qdrant.ps1 -QdrantUrl http://localhost:6333
go run ./cmd/debug_telegram_sandbox -timeout 120s -prompt "Crea un breve documento docx che riassume perche Picobot e veloce e Aura deve restare semplice." -expect-toolset document -expect-retrieval-capsule -expect-tools create_docx -expect-terminal-tool create_docx -expect-loop-steps-max 1 -max-elapsed-ms 30000
```

Observed:

- Docker Aura returned `/status` ok.
- Container logs showed compact Qdrant mirror sync: `vector_collection=aura_memory_v1_compact`, `vector_docs=487`, `vector_size=1024`.
- Qdrant compact-memory PoC returned compact facts plus graph-expanded nodes.
- Telegram document E2E passed with `loop_steps=1`, `llm_calls=1`, `tool_calls=1`, `tools_called=create_docx`, and `elapsed_ms=15400`.
