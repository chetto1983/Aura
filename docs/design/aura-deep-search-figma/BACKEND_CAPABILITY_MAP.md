# Aura Backend Capability Map For Figma

Date: 2026-06-08

Purpose: expose Aura's real backend and control-plane functionality as Figma
design surfaces. This is the source-of-truth checklist for turning the current
Aura Deep Search board into a complete senior UI/UX design project.

## Design Principle

Aura's UI must not hide backend power behind generic chat bubbles. Every runtime
capability that can change state, access data, call tools, schedule work, mount
servers, install skills, or pause for approval needs a visible product surface
with status, provenance, risk, and recovery states.

## Capability Groups

| Group | Backend functionality | Figma design surface |
| --- | --- | --- |
| Runtime shell | `aura serve`, `aura shell`, `aura chat`, `aura agent dry-run`, shared registry boot, graceful daemon shutdown | App shell, run status, AG-UI health, registry status, terminal/debug entry points |
| Agent loop | `LlmAgent`, budget/dedup gates, `text_response`, reasoning chunks, tool-call lifecycle, completion gate after mutating tools | Run timeline, reasoning drawer, tool activity stream, finalization/error states |
| Tool registry | Non-deferred tools plus deferred `web_search`, `web_fetch`, `swarm_spawn`, MCP-mounted tools, `tool_search` discovery | Tool catalog, deferred-tool search, mounted vs blocked tools, manifest inspector |
| Conversations | `conversation_turns`, sidecar spill, auto-title, archive/delete/rename/search, context L1/L2/L2.5 rotation | Conversation list, search results, history inspector, sidecar pointer state, context budget meter |
| HITL and identity | `ask_user`, `paused_states`, FIFO priority, resume accept/decline/cancel, `identity` and `capability_grants` | Approval queue, clarification cards, resume-token drawer, identity/capability settings |
| Web tools | SearXNG search, domain filters, readable fetch, DNS pin, SSRF block, redirect validation, content/body caps | Search panel, source preview, blocked URL event, unsupported content state, safe reason copy |
| Execution tools | `sandbox_exec`, `shell_exec`, `fs_read`, `fs_write`, `fs_edit`, `fs_grep`, `fs_glob`, sidecar outputs | Execution console, host/sandbox distinction, file diff panel, output pager, mutating warning |
| Scheduler | `task` tool and CLI schedule/list/cancel/run_now/approve/runs/doctor; reminder, agent_job, backups, skill TTL sweep | Scheduler board, task approval state, run ledger, doctor panel, quiet-hours/notify controls |
| Skills and snippets | Loader, writer, active/pending/archived roots, create/update/delete gates, snippet save/restore/archive, audit ledger | Skills library, install/create gate, snippet lifecycle, audit table, always-on indicator |
| MCP manager | Managed config v2, recipes, profiles, trust, runtime, status/logs/doctor/tools, risk policy, Streamable HTTP | MCP settings, recipe install, profile selector, trust gate, doctor/tools table, redacted logs |
| Knowledge graph | Neo4j schema executor, `mcp-neo4j-cypher`, read/write Cypher, schema/index operations, graph evidence model | Graph explorer, Cypher preview, schema/status panel, node inspector, path evidence strip |
| Swarm | Deferred `swarm_spawn`, independent goal briefs, worker reports, depth/cap/budget guards | Worker report table, fan-out preflight, child status chips, synthesis panel |
| AG-UI transport | `POST /agent/run`, `GET /threads/{id}/messages`, SSE events, interrupts, state deltas, redaction, slow-client drops | Frontend transport contract, SSE timeline, interrupt resume form, error redaction states |
| Cache and quality | Stable prompt prefix, cache metrics/stats/audit, tool result preview cap, cache invariant | Cache health panel, model cost footer, context/cache metrics, QA gate checklist |
| Planned channels | Phase 13 Telegram/multimodal, setup API, artifact delivery, cancel/cost/search commands | Channel setup wizard, Telegram status, multimodal intake, artifact delivery states |
| Planned memory | Phase 15 document ingest, entity resolution, GraphRAG hybrid retrieval, agent journal/insights | Memory ingest, entity merge review, retrieval result composer, agent insight timeline |
| Planned packaging | Phase 17 fat container, installer, keyless boot, setup wizard handoff | Installer/onboarding flow, service health, missing-key state, upgrade retention checklist |

## Backend To Component Mapping

| Component family | Must represent |
| --- | --- |
| `Aura/Shell` | daemon health, active mode, AG-UI connection, scheduler state, registry/MCP warnings |
| `Aura/RunTimeline` | RUN_STARTED, reasoning, text, tool start/args/end/result, state delta, pause, run finished/error |
| `Aura/Tool` | name, source, deferred state, mutating flag, risk labels, call count, last error |
| `Aura/Approval` | target kind, question, options, priority, token, accept/decline/cancel, provenance |
| `Aura/WebSource` | URL, title, snippet/content, domain filter, cache, blocked reason, fetch warning |
| `Aura/Execution` | host vs sandbox, command/args, cwd/env, exit code, stdout/stderr, truncation, sidecar path |
| `Aura/Scheduler` | kind, schedule, next run, risk tier, status, run history, heartbeat, notify route |
| `Aura/Skill` | active/pending/archived/rejected, body hash, always flag, snippet host path, audit |
| `Aura/MCP` | server source, command/url, env health, trust class, runtime kind, profile, policy decision |
| `Aura/Graph` | node/edge/path/schema/query contracts, Cypher raw view, evidence/citation links |
| `Aura/Memory` | document/chunk/entity/episode/insight states, dedupe, confidence, retrieval provenance |

## Screen Additions Required

1. Backend capability map page or section with the groups above.
2. Runtime control-plane screen for daemon, AG-UI, registry, scheduler, cache,
   and tool ledger health.
3. MCP manager screen with recipes, profiles, trust, doctor, tools, and logs.
4. Skills governance screen with pending approvals and append-only audit.
5. Scheduler screen with task creation, pending approval, runs, and doctor.
6. Execution inspector for host shell, sandbox, filesystem, sidecar output, and
   mutating completion gate.
7. Web safety states for SSRF, redirect block, unsupported scheme/content, size
   cap, timeout, and SearXNG unavailable.
8. Memory/GraphRAG planned screen that can be implemented when Phase 15 starts.

## Source Files Read

- `.planning/ROADMAP.md`
- `cmd/aura/main.go`
- `cmd/aura/serve.go`
- `cmd/aura/chat.go`
- `cmd/aura/mcp.go`
- `cmd/aura/task.go`
- `cmd/aura/skills.go`
- `cmd/aura/web.go`
- `cmd/aura/neo4j.go`
- `cmd/aura/identity.go`
- `cmd/aura/paused_states.go`
- `cmd/aura/config.go`
- `internal/agent/event.go`
- `internal/agent/llm_agent.go`
- `internal/agent/tools/spec.go`
- `internal/agent/tools/task.go`
- `internal/agent/tools/skill.go`
- `internal/agent/tools/skill_write.go`
- `internal/agent/tools/swarm_spawn.go`
- `internal/agui/server.go`
- `internal/agui/translator.go`
- `internal/askuser/store.go`
- `internal/conversations/store.go`
- `internal/conversations/context.go`
- `internal/cron/store.go`
- `internal/cron/dispatch.go`
- `internal/cron/handlers/handler.go`
- `internal/mcp/managed_config.go`
- `internal/mcp/manager/catalog.go`
- `internal/mcp/manager/policy.go`
- `internal/mcp/manager/status.go`
- `internal/web/client.go`
- `internal/web/searxng.go`
- `internal/web/fetcher.go`
- `internal/web/ssrf.go`
- `internal/web/errors.go`
- `internal/knowledge/client.go`
- `internal/knowledge/schema.go`
- `internal/sandboxagent/client.go`
- `internal/swarm/*`
- `internal/toolinvocations/store.go`

## Figma Guidance Applied

The Figma project should use pages/sections for navigation and handoff, local
variables for inspectable tokens, slash-separated component names for asset
organization, component properties/variants for reusable states, and Ready for
Dev sections once each surface has backend mapping and QA states.
