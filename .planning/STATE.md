# State

Date: 2026-05-08

Active milestone: none selected

Last closed milestone: v3.2 Runtime Diet

Current branch: `master`

## Current Truth

`master` is deployed to GitHub at `9b2c49a` (`debug smoke: map legacy runtime paths`).

Aura's runtime is now the Runtime Diet shape:

- compact agent loop in `internal/agentloop`;
- Aura-native event wrapper in `internal/agentruntime`;
- four runtime toolsets (`default`, `compute`, `document`, `admin`);
- bounded workspace file tools rooted at `/workspace` in Docker;
- legacy wiki/skill/proposal wrapper tools removed from the LLM surface/code;
- skills preserved as file-backed procedures under the runtime workspace;
- compact Retrieval Capsule injection only when the turn needs memory or production context;
- compact source/archive/proposal facts indexed in SQLite FTS and mirrored to Qdrant in `aura_memory_v1_compact`;
- adaptive batch embeddings for cold compact-memory mirror rebuilds;
- Docker image/context narrowed so `/app` no longer exposes the developer repository.

## Phase 08 Closure Evidence

Phase 08 Runtime Diet is closure-clean.

What changed:

- The user-facing `"Mi sono fermato"` fallback is gone; budget exhaustion finalizes from the last useful tool result.
- Profile/preflight taxonomy and old capability routing were deleted from live code.
- The old always-on Memory Pack path was replaced by the routing-aware `## Retrieval Capsule`.
- `search_memory` now uses wiki search plus compact source/archive/proposal retrieval instead of raw source/archive scans.
- Qdrant compact memory uses a separate collection from wiki vectors and merges with exact/FTS retrieval.
- Archive append and cleanup mirror into compact memory and Qdrant.
- Document generation no longer wanders through broad workspace/source/web tools when the capsule is sufficient.
- Tool definitions now carry concrete examples through Aura's `ToolDefinition` contract.
- Compact Qdrant sync runs as background maintenance instead of blocking startup.

Fresh verification:

- `go test ./internal/search ./internal/memoryindex ./internal/telegram -count=1`
- `go test ./...`
- `go build ./...`
- `docker compose up -d --build aura`
- `/status` returned `ok`
- container logs showed `compact memory vector mirror synced`, `vector_collection=aura_memory_v1_compact`, `vector_docs=487`, `vector_size=1024`
- `scripts/test-compact-memory-qdrant.ps1` passed against local Qdrant with compact facts plus graph-expanded nodes
- `cmd/debug_telegram_sandbox` document E2E passed with `toolset=document`, `retrieval_capsule_present=true`, `tools_called=create_docx`, `terminal_tool=create_docx`, `loop_steps=1`, `llm_calls=1`, `tool_calls=1`, `elapsed_ms=15400`

## Cleaned-Up Plan Map

- Phase 05 (`05-agent-simplification-god-class-refactor`): complete and historical.
- Phase 06 (`06-fs-first-wiki-skills-agent`): complete. Remaining items are follow-ups, not blockers.
- Phase 07 (`07-runtime-workspace-bootstrap-graph-cache`): complete. Runtime workspace, graph cache, historical Memory Pack benchmark, and live benchmarks are done.
- Phase 08 (`08-runtime-diet-embedding-retrieval`): complete. Runtime Diet gates are closed.
- Phase 04 (`04-agent-orchestration-system-prompt-versioning`): historical v3.1 orchestration plan. Use only as context for old blocker evidence.
- v4.0 MCP marketplace: planned and unblocked by Runtime Diet closure.

## Recommended Next Slice

Recommended next phase before or alongside v4.0: **Runner Boundary & Health Hardening**.

Goal:

- finish moving Telegram session persistence and terminal finalization behind the `agentruntime` event boundary;
- expose compact-memory/Qdrant mirror health in `/status` or `/api/health`;
- make debug smokes Docker-first by default, so they use runtime workspace paths without host overrides;
- validate broad non-document prompts, not only document generation.

Suggested acceptance:

- `go test ./internal/agentruntime ./internal/agentloop ./internal/orchestration ./internal/telegram ./cmd/debug_telegram_sandbox -count=1`;
- Docker `/status` reports compact memory mirror state or last sync result;
- broad project/status prompts stay under 30s without repeated file/source loops;
- document smoke remains one loop step when the capsule is sufficient.

## Deferred Follow-Ups

- Automatic wiki index/search/graph refresh after accepted workspace writes to wiki files.
- Skill manifest/cache refresh after accepted workspace writes to `SKILL.md`.
- Small review-gated Git toolset for Aura (`git_status`, `git_diff`, `git_log`, explicit-path `git_stage`, review-gated `git_commit`).
- Optional planning archive workflow after a real `.planning/MILESTONES.md` exists.
- v4.0 MCP marketplace implementation.
