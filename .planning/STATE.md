# State

Date: 2026-05-10

Active milestone: v4.0 Production Hardening

Last closed milestone: v3.3 Runner Boundary & Health Hardening

Current branch: `master`

## Current Position

Phase: Not started (defining requirements)
Plan: —
Status: Defining requirements
Last activity: 2026-05-10 — Milestone v4.0 started

## Project Context

Aura is a second-brain Telegram assistant with an embedded React dashboard. 

- Compact agent loop in `internal/agentloop`; runtime events in `internal/agentruntime`.
- Four runtime toolsets: `default`, `compute`, `document`, `admin`.
- Default toolset is deliberately tiny: `search_memory` + `schedule_task`.
- Qdrant (`aura_memory_v1_compact`) is the canonical vector store; SQLite FTS provides exact/fallback search.
- Wiki pages are the durable memory format with `[[slug]]` links.
- Dashboard is served from Go-embedded React assets under `internal/api/dist`.

## Previous Milestones

- v1.2 Source Intake Closure (closed 2026-05-06)
- v1.3 Memory Consolidation And Quality (closed 2026-05-07)
- v3.1 Agent Runtime Stabilization (closed)
- v3.2 Runtime Diet (closed 2026-05-08)
- v3.3 Runner Boundary & Health Hardening (closed 2026-05-08)
- v4.0 MCP Marketplace (deferred — replanned as v4.0 Production Hardening)
