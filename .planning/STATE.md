# State

Date: 2026-05-08

Active milestone: Runtime Answer Discipline

Last closed milestone: v3.3 Runner Boundary & Health Hardening

Current branch: `master`

## Current Truth

`master` is deployed to GitHub at `072888e` (`docs: close runtime diet planning`).

Aura's runtime is now the Runtime Diet shape:

- compact agent loop in `internal/agentloop`;
- Aura-native event wrapper in `internal/agentruntime`;
- four runtime toolsets (`default`, `compute`, `document`, `admin`);
- bounded workspace file tools rooted at `/workspace` in Docker;
- legacy wiki/skill/proposal wrapper tools removed from the LLM surface/code;
- skills preserved as file-backed procedures under the runtime workspace;
- no user-text keyword router in the hot path: `AURA_TOOLSET_MODE=auto` uses the default toolset, while `compute`, `document`, and `admin` are explicit modes;
- the default toolset is deliberately tiny: `search_memory` plus `schedule_task`; `daily_briefing`, task list/cancel, source, web, workspace, admin, and swarm tools are explicit specialized surfaces;
- default `search_memory` is terminal, capped to three results, and finalized through a no-tool answer;
- compact memory stays available through `search_memory` instead of speculative keyword-triggered capsule injection;
- compact source/archive/proposal facts indexed in SQLite FTS and mirrored to Qdrant in `aura_memory_v1_compact`;
- adaptive batch embeddings for cold compact-memory mirror rebuilds;
- Docker image/context narrowed so `/app` no longer exposes the developer repository.

## Phase 08 Closure Evidence

Phase 08 Runtime Diet is closure-clean.

What changed:

- The user-facing `"Mi sono fermato"` fallback is gone; budget exhaustion finalizes from the last useful tool result.
- Profile/preflight taxonomy and old capability routing were deleted from live code.
- The old always-on Memory Pack path was replaced during v3.2 by routing-aware `## Retrieval Capsule` injection; v3.3 then removed the remaining user-text keyword router from the live hot path.
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

## Active Slice

Active phase: **Runtime Answer Discipline**.

Plan: `.planning/phases/11-runtime-answer-discipline/PLAN.md`

Goal:

- make Aura answer naturally on Telegram instead of exposing raw tool results;
- keep shell/code tools internal unless the user explicitly asks for diagnostics, commands, files, tests, installs, or raw output;
- recover from hidden-tool guesses through no-tool finalization rather than user-facing technical scolding;
- keep `AGENT.md` and `AGENTS.md` out of the system prompt while bootstrapping `SOUL.md`/`TOOLS.md` as runtime overlays.

First slice:

- add failing debug smoke gates for forbidden final fragments and forbidden tool calls;
- remove `execute_shell` from the default hot tool surface;
- route shell/code terminal results through no-tool synthesis except explicit raw-output requests;
- add a final-answer sanitizer that blocks `Evidence envelope`, `exit_code`, `elapsed_ms`, `source_id`, and similar internal markers.

Suggested acceptance:

- `Cosa sai di me?` uses memory but never prints raw evidence envelopes or hidden-tool errors;
- `Puoi scansionare le cartelle di rete?` does not call shell and answers conversationally;
- explicit raw commands can still use shell through `tool_search`;
- Docker smoke script `scripts/test-runtime-answer-discipline-smokes.ps1` passes.

Latest implementation slice:

- Debug smoke gates now assert forbidden tools/final fragments and shell-call minimums.
- `execute_shell` is no longer hot-visible; `execute_code`/`execute_shell` are intent-gated for explicit operational work.
- Terminal shell output is synthesized unless raw output was explicitly requested.
- Hidden-tool rejection and raw fallback paths finalize naturally instead of exposing tool errors or raw envelopes.
- Empty runtime overlay workspaces bootstrap `SOUL.md` and `TOOLS.md`; `AGENT.md` remains file-readable only.
- Verification passed: `go test ./internal/agentruntime ./internal/agentloop ./internal/tools ./internal/conversation ./internal/telegram ./cmd/debug_telegram_sandbox -count=1`, `go test ./...`, `go build ./...`, `go vet ./...`, and local `scripts/test-runtime-answer-discipline-smokes.ps1 -SkipRawShell`.

## Deferred v4.0 MCP Marketplace Slice

Active phase: **v4.0 MCP Marketplace And Autonomous Plugin Manager**.

Plan: `.planning/phases/v4.0-mcp-plugin-marketplace/PLAN.md`

Design: `.planning/phases/v4.0-mcp-plugin-marketplace/DESIGN.md`

Goal:

- make Aura's plugin system MCP-first, Docker-first, review-gated, and agent-auditable;
- sync the official MCP Registry without blocking startup;
- install MCP plugins as managed sidecars or remote HTTP connections;
- keep marketplace actions dashboard-reviewed and smoke-tested before tool exposure.

First slice:

- provider-agnostic mail adapter over approved MCP providers;
- canonical Aura mail contract instead of raw provider tool sprawl;
- initial candidates: `tecnologicachile/mail-mcp`, `aaronsb/google-workspace-mcp`, `navbuildz/gmail-mcp-server`, `littlebearapps/outlook-assistant`;
- enterprise database MCP as read-only business profile using `executeautomation/mcp-database-server`;
- frontend configuration starts in the existing `/mcp` route with tabs for Connectors, Installed, Health, Review Queue, and Raw MCP diagnostics;
- provider/account credentials must use connector-specific flows with secret references, not dozens of global runtime settings;
- mail send/delete/bulk mutation and database writes deferred to later reviewed slices.

Latest implementation slice:

- Real MCP mail wiring now uses existing `mcp.json` stdio config with `env`, not a parallel Aura plugin runtime.
- Provider manifests include accepted MCP server aliases; `mail-mcp` can bind to a configured server named `mail` or `mail-mcp`.
- `mcp.example.json` includes a read-only `mail` template for `tecnologicachile/mail-mcp` IMAP config.
- Verification passed: `go test ./internal/mcp ./internal/api ./internal/telegram -run "TestLoadServersValid|TestMCPProviderProbeMailMCPFindsConfiguredMailAlias|TestMCPProviderMail" -count=1`, `npm --prefix web run i18n:check`, `npm --prefix web run build`, `go test ./...`, `go build ./...`, and `go vet ./...`.

Previous implementation slice:

- `POST /mcp/providers/{id}/actions/probe` now probes connected MCP provider profiles.
- `mail-mcp` has read-only canonical `mail.search` and `mail.read` adapter endpoints over approved IMAP/EWS tools.
- Dashboard provider cards can run Probe and display ready/missing/blocked advertised capabilities.
- Mail send/delete/bulk mutation remains blocked and not agent-visible.
- Verification passed: `go test ./internal/api -run "TestMCPProviderMail|TestMCPProviders" -count=1`, `go test ./internal/api -count=1`, `npm --prefix web run i18n:check`, `npm --prefix web run build`, `go test ./...`, `go build ./...`, and `go vet ./...`.

Previous implementation slice:

- `GET /mcp/providers` returns read-only mail/database provider manifests.
- `/mcp` dashboard now renders Connectors, Installed, Health, Review Queue, and Raw MCP tabs.
- Raw MCP manual invoke remains diagnostic-only in the UI.
- Embedded dashboard assets were rebuilt.
- Verification passed: `go test ./internal/api -count=1`, `npm --prefix web run i18n:check`, `npm --prefix web run build`, `go test ./...`, and `go build ./...`.

Suggested acceptance:

- registry sync has a local cache and clear health;
- fake MCP install/enable/invoke/rollback works in Docker;
- approved MCP tools are exposed as stable `mcp_<server>_<tool>` names;
- dashboard shows Marketplace, Installed plugins, Health, and Review Queue.

## Phase 09 Closure Evidence

Runner Boundary & Health Hardening is closure-clean.

What changed:

- Telegram active session/context lifecycle moved behind `agentruntime.SessionStore`.
- Runtime snapshots and debug smoke counters now come from `agentruntime` events/results.
- Generic terminal finalization moved into `internal/agentruntime`, with Telegram keeping delivery only.
- Compact-memory Qdrant mirror health is tracked and exposed through `/status` plus API health rollup.
- User-text keyword routing, speculative retrieval capsule routing, and default swarm exposure were removed from the hot path.
- The default toolset was cut to `search_memory` and `schedule_task`; `daily_briefing` is now explicit admin/ops and covered by its own smoke.

Fresh verification:

- `go test ./internal/agentruntime ./internal/orchestration ./internal/telegram ./cmd/debug_telegram_sandbox -count=1`
- `go test ./internal/conversation ./...`
- `go build ./...`
- `docker compose config --quiet`
- `docker compose up -d --build aura`
- live `/status`: `status=ok`, `compact_memory` detail `collection=aura_memory_v1_compact docs=487 vector=1024`
- `scripts/test-runner-boundary-smokes.ps1`
- status smoke: `tool_calls_count=1`, `tools_called=search_memory`, `terminal_tool=search_memory`, `elapsed_ms=10401`
- memory smoke: `tool_calls_count=1`, `tools_called=search_memory`, `terminal_tool=search_memory`, `elapsed_ms=10195`
- document smoke: `tool_calls_count=1`, `tools_called=create_docx`, `terminal_tool=create_docx`, `elapsed_ms=3826`
- admin briefing smoke: `tool_calls_count=1`, `tools_called=daily_briefing`, `elapsed_ms=6095`

## Deferred Follow-Ups

- Automatic wiki index/search/graph refresh after accepted workspace writes to wiki files.
- Skill manifest/cache refresh after accepted workspace writes to `SKILL.md`.
- Small review-gated Git toolset for Aura (`git_status`, `git_diff`, `git_log`, explicit-path `git_stage`, review-gated `git_commit`).
- Optional planning archive workflow after a real `.planning/MILESTONES.md` exists.
- v4.0 MCP marketplace implementation.
