# Aura Deep Search UI/UX Spec

Date: 2026-06-04

This revision is altered after reading `weaviate/elysia-frontend` and
`pewdiepie-archdaemon/odysseus` source. The current source board is
`aura-deep-search-elysia-informed.svg`.

## What Changed

The previous Aura board treated evidence cards as the main UI primitive. Elysia's
frontend shows a stronger architecture: chat is only one mode of a richer
workspace. Results can become typed displays, merged tabs, raw/code views, detail
views, citations, feedback controls, and a decision-tree canvas.

Aura now adopts that structure while keeping Aura's product constraints:
self-hosted SearXNG, D:\tmp/local files, Neo4j MCP graph memory, MCP mounts,
sidecar artifacts, SSRF blocks, and minimal swarm worker reports.

This revision also fixes a design gap: Neo4j is no longer just a status label or
source tab. Aura gets a dedicated graph visualization mode for inspecting
entities, claims, sources, topics, agents, paths, edge types, Cypher, and graph
evidence provenance.

This revision additionally applies Odysseus frontend mechanics. Aura should not
copy Odysseus's broad personal-workspace feature set or playful visual
personality, but it should borrow the operator-OS mechanics: adaptive icon rail,
dockable/minimizable tool windows, command palette, slash actions, background
job feedback, early theme/density boot, and audited AI-driven UI events.

This revision also fills the missing control-plane surface for Aura itself:
managed MCP server configuration and skills install/lifecycle governance. These
are not hidden advanced settings. They are operator workflows because they
change what tools and instructions the agent can use.

This revision now also exposes Aura's backend capability map directly in the
Figma project. The design source is no longer only Elysia/Odysseus UX patterns;
it also includes Aura's own Go backend surfaces: runtime boot, event stream,
tool registry, conversations, HITL, web safety, execution, scheduler, skills,
MCP manager, Neo4j knowledge, swarm, AG-UI, planned channels, planned memory,
and packaging.

## Elysia Patterns Adopted

- Chat page mode switch: `Chat`, `Tree`, `Settings`, plus Aura's `Graph` and
  `Displays`.
- Query composer with visible running status and optional development route.
- Typed payload router. Aura display types should include `web_result`,
  `local_artifact`, `graph_chunk`, `swarm_report`, `mcp_message`,
  `table`, `chart`, `document`, `code`, and `system_event`.
- Merged result tabs when multiple source collections return the same display
  kind.
- Source-code/raw-data inspection for generated displays.
- Inline citation bubbles with hover previews and click-through to source detail.
- Paginated result groups with item count and items-per-page controls.
- Feedback controls after a completed answer: copy, strong positive, positive,
  negative, and duration.
- Tree canvas with selected path highlighting and a node-details drawer for
  reasoning, description, instruction, and metadata.
- Source explorer with `Table`, `Metadata`, and `Configuration` views.
- Explicit system event displays: warning, blocked URL, rate limit, suggestion,
  self-healing error.
- Neo4j result renderer that can switch from a tabular result to an interactive
  node-link graph with a selected-node inspector.

## Odysseus Patterns Adopted

- Static-first modular frontend: a shell can load focused modules for chat,
  sessions, tools, theme, rendering, background jobs, and accessibility.
- Adaptive icon rail. Full navigation can collapse to a narrow rail with
  active, minimized, running, warning, and completed states.
- Dockable tool windows. Research, Graph, Compare, Source Explorer, and Logs can
  minimize to dock chips, restore without losing state, and optionally dock to a
  screen edge.
- Command palette and slash actions. Operators can jump to a run, source, tool,
  or action without hunting through panels.
- Background job feedback. Long jobs expose running, queued, error, and
  completed state outside the modal that created them.
- AI `ui_control` events. Backend events can open a panel, highlight a source,
  toggle a safe control, or report a background job. These events must be
  allowlisted and auditable.
- Local theme, density, and route/PWA polish. User preferences should paint
  before app boot to avoid flash and preserve mobile installability.

## Aura MCP + Skills Patterns

- MCP configuration appears in Settings > Integrations with CLI parity for
  `install`, `add`, `list`, `doctor`, `tools`, `enable`, `disable`, and
  `remove`.
- Managed MCP config path is visible: `~/.aura/mcp/servers.json`. If
  `AURA_MCP_SERVERS_JSON` overrides it, the UI must show that source clearly.
- Recipe installs show provenance and defaults: `calculator`, `mail`, and
  `whatsapp`. Custom server add exposes command, args, env, and enabled state.
- Env values are editable but never displayed raw after save. Required,
  optional, missing, and redacted states should be visually distinct.
- MCP doctor is a first-class check that starts the server and lists tool
  counts before the agent gets access.
- MCP tools view shows advertised tools, mounted tools, and allowlist decisions.
  Mail and WhatsApp make denied/destructive tools explicit instead of silently
  hiding the policy.
- MCP mount failures are fail-soft runtime warnings. The design must show which
  servers failed and which tools remain mounted.
- Skill discovery and install are governed workflows, not a marketplace shelf.
  Catalog access is default-off until enabled by the operator.
- Skill install shows source, command, hash/content preview, risk tier,
  validation results, and destination state before activation.
- Skill mutation states are visible: active, pending, pending delete, archived,
  rejected, and audit-only.
- `skill.install`, `skill.create`, and `skill.update` are RISKY. `skill.delete`
  is DESTRUCTIVE. Both need explicit approval UI when the agent requests them.
- Pending skills live under `~/.aura/skills/pending/<name>/` and are not loaded
  until approved by `ask_user` resume or explicit CLI.
- Skill run/restore/archive actions must show capability scope, last used, use
  count, TTL/archive state, and audit trail.

## Aura Backend Capability Patterns

- Runtime is a product surface. `aura serve`, AG-UI health, scheduler state,
  registry validity, MCP mount warnings, and cache/tool ledgers need visible
  status rather than terminal-only feedback.
- The agent Event stream maps to a run timeline: reasoning chunks, text chunks,
  tool start/args/end/result, state deltas, pauses, finalization, and run errors.
- Tool registry state must show active, deferred, mutating, mounted, blocked,
  and failed tools. Deferred tools such as `web_search`, `web_fetch`, and
  `swarm_spawn` need search/discovery UI.
- Conversations need history, archive/delete/rename, FTS search, sidecar output
  pointers, context budget, cost/cache metrics, and auto-title behavior.
- `ask_user` is not just a chat message. Clarification, choice, approval,
  priority, resume token, accept/decline/cancel, and stale/auto-terminated
  states need an approval-center component.
- Web safety states must render stable backend error classes: blocked URL,
  redirect blocked, unsupported scheme, unsupported content type, response too
  large, timeout, HTTP error, extraction failed, and SearXNG unavailable.
- Execution tools must distinguish host shell, sandbox container, and filesystem
  operations. Mutating operations need warning/confirmation language and a
  post-run output/audit inspector.
- Scheduler design needs task creation, pending approval, active/cancelled rows,
  run history, heartbeat, doctor, quiet-hours deferral, and notification route.
- MCP design needs recipe/manual/source, profile membership, trust class,
  runtime kind, env redaction, auth posture, doctor/tools/logs, allow/deny/risk
  policy, and fail-soft mount warnings.
- Skills design needs active/pending/archived/audit tabs, validation results,
  snippet host path, restore/archive, always-on state, and immutable audit rows.
- Memory and channel screens can be designed now as planned surfaces, but they
  must be labelled as Phase 13/15/17 until implementation lands.

## Aura Design Direction

Aura becomes a dark operator cockpit. It borrows Elysia's dark, animated,
agentic feel, but the tone is more industrial and less decorative. The abstract
sphere idea is not copied into the primary product surface; Aura's trust model
is shown through source state, provenance, and execution structure.

The primary layout uses three zones:

- Left: product navigation, socket/stack health, recent investigations.
- Middle: chat stream and composer.
- Right: display workspace for typed payloads, citations, raw/code views, graph
  path previews, and source details.

The secondary layout uses tool windows:

- Primary modes stay in the top switch: `Chat`, `Tree`, `Graph`, `Displays`,
  `Settings`.
- Secondary tools can dock or minimize: `Research`, `Compare`, `Source
  Explorer`, `Logs`, `Theme`, and `Run Inspector`.
- Tool state must survive minimize/restore and must not steal the user's active
  chat, graph path, or source selection.

## Frames

### Frame 01 - Aura Chat + Display Workspace

Primary desktop surface.

Behavior:
- The top mode switch changes between Chat, Tree, Graph, Displays, and Settings.
- Running status appears above the composer.
- Result payloads are grouped into tabs by source collection.
- Display workspace can show result preview, source code, raw data, or selected
  source detail.
- Completed answers show citation bubbles and feedback controls.

### Frame 02 - Tree View + Reasoning Drawer

Decision trace surface.

Behavior:
- The selected path is highlighted.
- Unchosen paths remain visible but visually quiet.
- Selecting a node opens a right drawer with reasoning, description,
  instruction, and metadata.
- Aura should use this to explain agent/tool decisions, not to create a fake
  inter-agent chat graph.

### Frame 03 - Source Explorer

Source management and evidence preflight.

Behavior:
- `Table` view shows source rows with source type, mapping, confidence, and
  sortable metadata.
- `Metadata` view edits display mappings and summaries.
- `Configuration` view controls how source classes render.
- Warnings appear when local files are unprocessed or metadata is incomplete.

### Frame 04 - System Events

Safe operational feedback.

Behavior:
- Warnings, blocked URLs, rate limits, suggestions, and self-healing errors are
  display components, not plain text appended to the answer.
- SSRF blocks must show safe reasons only.
- Suggestions can become follow-up prompts.

### Frame 05 - Mobile

Single-column mobile surface.

Behavior:
- Mode switch stays in the header.
- Displays and Tree open as drawers.
- Source tabs remain reachable without horizontal overflow.
- Composer stays bottom-pinned.

### Frame 06 - Neo4j Graph Explorer

Graph evidence surface.

Behavior:
- `Graph` mode opens a dedicated Neo4j workspace rather than nesting graph data
  inside generic cards.
- The left panel contains natural-language graph query, Cypher preview, label
  filters, edge-type filters, save/export/copy actions, and saved graph views.
- The center canvas renders node-link paths. Node color encodes label family:
  source, claim, entity, agent, topic, and conflict. Node size can encode degree
  or confidence. Edge labels encode relationship type.
- The selected path is visually highlighted and also shown as a readable path
  strip below the canvas.
- The right inspector shows selected-node label, properties, degree/confidence,
  connected evidence, neighbor relationships, citations, and actions such as
  pin path, open source, show raw Cypher, and add note.
- Hover cannot be the only access path. On desktop, hover can preview. On mobile
  and keyboard, tap/focus opens the inspector drawer.
- Dense graphs should default to filtered evidence paths, not hairball views.
  Operators can expand neighbors intentionally.

### Frame 07 - Odysseus Operator OS Patterns

Workspace mechanics surface.

Behavior:
- Aura has an adaptive icon rail that can replace the wide sidebar when space is
  tight. Rail buttons show active, minimized, running, warning, and completed
  states.
- Deep Research, Compare, Graph, Source Explorer, and Logs are dockable tools.
  Opening a tool creates a restorable window; minimizing it creates a dock chip;
  closing it tears down transient state.
- Dock chips preserve state. A minimized research job keeps its progress,
  selected sources, and trace; a minimized compare run keeps pane streams and
  vote state.
- The command palette searches runs, sources, tools, and actions. Slash actions
  are visible accelerators, not a separate hidden product surface.
- Background jobs update both the rail and relevant dock chips.
- A `ui_control` event can request safe UI changes such as `open_panel`,
  `highlight_source`, `set_mode`, `focus_query`, or `show_job`. Events must be
  validated against an allowlist and recorded in the run log.
- Theme and density preferences apply before the app fully boots, including
  mobile theme-color/PWA metadata where applicable.

### Frame 08 - MCP Configuration + Skills Install

Configuration and governance surface.

Behavior:
- MCP server rows show source, command summary, enabled state, env health,
  doctor status, mounted tool count, and allowlist state.
- The add/edit MCP panel supports recipe install and custom stdio command entry.
  It must preview the equivalent CLI command and managed-config destination.
- Env editing uses redacted chips after save and warns when required recipe env
  variables still contain placeholders.
- Doctor and Tools are visible actions on each server. Doctor checks process
  start and tool listing. Tools shows advertised versus mounted tools.
- Disabling a server is reversible. Removing a server is a configuration
  mutation that should show a confirmation and audit row.
- Skill install starts from a source field or selected catalog item. The catalog
  must indicate whether external discovery is enabled.
- The install pipeline surfaces `--ignore-scripts`, sanitized env, post-install
  `SKILL.md` parse/validation, body cap, injection literal blocklist, and
  sanitized name/path validation.
- RISKY and DESTRUCTIVE skill actions enter an approval queue with source,
  command, diff/content preview, risk tier, resume token, and final destination.
- Active, pending, archived, and audit tabs are separate. Pending skills cannot
  be run or injected into prompts.
- Mobile uses the same approval queue as a drawer, not a reduced-risk shortcut.

## Component Inventory

- App shell
- Navigation rail
- Adaptive icon rail
- Dockable tool window
- Dock chip
- Command palette
- Slash-action autocomplete
- Conversation/investigation list item
- Mode switch
- Graph mode switch item
- Query composer
- Running status row
- Chat message
- Result display router
- Merged result tabs
- Source-code button
- Raw-data toggle
- Display pagination
- Typed display cards
- Citation bubble
- Feedback button group
- Tree canvas node
- Node details drawer
- Source explorer table
- Metadata mapping editor
- Neo4j graph query panel
- Neo4j graph canvas
- Neo4j schema/label legend
- Neo4j selected path strip
- Neo4j node inspector
- Cypher preview/raw Cypher drawer
- Background job card
- AI UI-control event card
- Theme/density selector
- System event card
- Mobile display drawer
- MCP server row
- MCP recipe install panel
- MCP custom command form
- MCP env redaction editor
- MCP doctor result card
- MCP tool allowlist table
- Skill source picker
- Skill install validation checklist
- Skill approval queue item
- Skill library tabs
- Skill audit event row
- Risk-tier badge
- Resume-token detail drawer

## Copy Contract

Allowed above-the-fold copy in Frame 01:

- Aura
- Chat
- Data
- Evaluation
- Settings
- SearXNG socket
- Neo4j MCP
- Local-only guard
- Investigations
- New run
- Chat
- Tree
- Graph
- Displays
- Ask Aura
- Compare Elysia frontend patterns with Aura Phase 9.
- Running
- Merged result payloads
- Online
- D:\tmp
- Graph
- Graph query
- Path visualization
- Selected node inspector
- Adaptive icon rail
- Command palette
- Dockable investigation tools
- Deep Research job
- Compare panes
- AI ui_control event stream
- Theme, density, and PWA boot
- MCP servers
- Doctor + tool allowlist
- Skills install
- Skills library + audit
- Approval center
- Source Code
- Raw data
- Display Workspace
- Payload router
- Paginated source cards
- Cited answer

Do not add marketing hero text, decorative badges, or tutorial paragraphs to the
primary product viewport.

## Implementation Model

Design the frontend around event payloads:

```text
Aura event -> display classifier -> chat renderer | display renderer | code/raw view | tree node | system event
```

The Odysseus-informed shell adds a second lane:

```text
Aura event -> shell event router -> safe UI control | background job state | dock/window state
```

The Aura configuration surface adds a governed control lane:

```text
Operator or agent request -> risk classifier -> pending config/skill state -> approval queue -> activation + audit
```

Suggested display type mapping:

- `web_search` result -> `web_result` display
- `web_fetch` markdown -> `document` display
- `sandbox_exec` sidecar output -> `code` or `local_artifact` display
- `swarm_spawn` child report -> `swarm_report` display
- Neo4j MCP result -> `graph_chunk`, `graph_path`, `graph_schema`, or `table`
  display
- ask_user pause -> `system_event` with `needs_user_input`
- SSRF block -> `system_event` with safe block reason
- research progress -> `background_job` with rail/dock update
- compare progress -> `background_job` with pane metrics
- safe UI request -> `ui_control` after allowlist validation
- MCP managed config -> `mcp_config`, `mcp_server`, `mcp_tool`,
  `mcp_doctor_result`, or `mcp_mount_warning`
- skill catalog result -> `skill_catalog_item`
- skill mutation request -> `skill_install_request`, `skill_pending_state`,
  `approval_gate`, or `skill_audit_event`

Suggested Neo4j visualization model:

```text
Neo4j MCP result -> graph normalizer -> nodes/edges/path summaries -> graph canvas | table | raw Cypher
```

Graph payload contract:

- `nodes`: stable id, labels, title, subtitle, properties, confidence, degree,
  source refs.
- `edges`: stable id, source id, target id, relationship type, weight,
  direction, evidence refs.
- `paths`: ordered node/edge ids plus summary title and conflict/support counts.
- `schema`: label counts, relationship counts, indexed properties, warnings.
- `query`: natural-language prompt, Cypher text, parameters, execution timing,
  result count, safety notes.

Rendering rules:

- Default to selected evidence paths with 20-80 nodes visible.
- Collapse high-degree neighbors behind expandable clusters.
- Preserve deterministic layout per query so repeated runs do not jump.
- Always pair canvas selection with a readable textual path and inspector.
- Offer table fallback for accessibility, export, and very large result sets.

Suggested UI-control contract:

- `open_panel`: panel id from an allowlist only.
- `highlight_source`: source id or DOM-safe internal target id only.
- `set_mode`: one of `chat`, `tree`, `graph`, `displays`, `settings`.
- `show_job`: background job id owned by the active run/user.
- `set_density`: one of `compact`, `operator`, `review`.
- `theme_preview`: token object validated by schema; no arbitrary CSS.

The model must never emit raw CSS selectors, scripts, URLs to execute, or
unbounded DOM mutations. UI-control events should be replayable from the run log
so debugging a session reconstructs what the operator saw.

Suggested MCP configuration model:

- `mcp_config`: config path, override source, server count, enabled count,
  warning count.
- `mcp_server`: name, source recipe/manual, command summary, env key states,
  enabled flag, last doctor status, mounted tool count.
- `mcp_tool`: server, advertised name, mounted boolean, allowlist reason,
  description first line, risk notes.
- `mcp_doctor_result`: server, started boolean, tool count, stderr summary,
  duration, remediation copy.
- `mcp_mount_warning`: server, error class, fail-soft state, affected tools.

Suggested skill governance model:

- `skill_catalog_item`: source, name, description, owner, provenance, catalog
  enabled flag.
- `skill_install_request`: source, command preview, content hash, risk tier,
  validation checklist, pending path.
- `skill_pending_state`: name, action, source, content hash, resume token,
  expires/cleanup time, status.
- `approval_gate`: target kind, target id/name, risk tier, question, options,
  resume context, gate recommended/taken.
- `skill_audit_event`: action, actor/source, name, hash, risk tier, approval
  source, timestamp.

Suggested backend capability model:

- `runtime_status`: daemon state, AG-UI bind, scheduler tick state, registry
  valid flag, MCP mounted/failed count, cache hit rate.
- `run_timeline_event`: type, request id, span id, message id, tool call id,
  author, branch, timestamp, preview, sidecar pointer.
- `tool_registry_entry`: name, summary, source, deferred, mutating, mounted,
  blocked reason, risk labels, last call status.
- `approval_item`: target kind, question, options, priority, token, source
  event, action taken, resolved at.
- `scheduler_task`: kind, schedule, next run, status, risk tier, notify route,
  last run status, heartbeat.
- `mcp_status`: server, profile, trust, runtime, auth, startup state, mounted
  tool count, blocked tool count, policy summary, last error.
- `skill_state`: name, type, status, always, content hash, pending path, audit
  count, last used.
- `web_safety_event`: error, reason, message, status code, source URL class,
  safe remediation copy.

## Important Non-Goals

- Do not copy Elysia's Weaviate collection model directly.
- Do not copy Elysia's abstract sphere as the main Aura dashboard decoration.
- Do not add swarm talk, join, mailbox, or sibling chat UI.
- Do not expose raw SearXNG backend parameters to the operator or model.
- Do not flatten every payload into generic cards; typed displays are the point.
- Do not render Neo4j as a decorative hairball. The graph view must answer an
  evidence/path question and keep source provenance visible.
- Do not copy Odysseus's personal-workspace sprawl into Aura. Aura should adopt
  dock/window mechanics only where they improve investigation flow.
- Do not let AI UI-control events become arbitrary frontend automation.
  Everything must be allowlisted, scoped, logged, and reversible.
- Do not show raw saved MCP secrets in the UI.
- Do not silently mount destructive MCP tools when an allowlist exists.
- Do not activate installed or generated skills directly from a model tool call.
- Do not present skills install as safe just because `--ignore-scripts` is used;
  it remains RISKY supply-chain input.
- Do not allow pending skills to run, inject prompt content, or override active
  skills before approval.

## Source Files Read

- `cmd/aura/mcp.go`
- `cmd/aura/main.go`
- `cmd/aura/serve.go`
- `cmd/aura/chat.go`
- `cmd/aura/task.go`
- `cmd/aura/skills.go`
- `cmd/aura/web.go`
- `cmd/aura/neo4j.go`
- `cmd/aura/identity.go`
- `cmd/aura/paused_states.go`
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
- `internal/toolinvocations/store.go`
- `README.md`
- `prd.md`
- `CLAUDE.md`
- `app/pages/ChatPage.tsx`
- `app/components/chat/RenderChat.tsx`
- `app/components/chat/RenderDisplay.tsx`
- `app/components/chat/RenderDisplayView.tsx`
- `app/components/chat/MergeDisplays.tsx`
- `app/components/chat/FlowDisplay.tsx`
- `app/components/chat/NodeDetailsSidebar.tsx`
- `app/components/chat/QueryInput.tsx`
- `app/components/chat/components/DisplayPagination.tsx`
- `app/components/chat/components/CitationBubble.tsx`
- `app/components/chat/components/FeedbackButtons.tsx`
- `app/components/explorer/DataExplorer.tsx`
- `app/components/explorer/DataMetadata.tsx`
- `app/components/navigation/SidebarComponent.tsx`
- `tailwind.config.ts`
- `app/globals.css`

## Odysseus Source Files Read

- `README.md`
- `static/index.html`
- `static/style.css`
- `static/app.js`
- `static/js/MODULE_SUMMARY.md`
- `static/js/init.js`
- `static/js/ui.js`
- `static/js/chatRenderer.js`
- `static/js/chatStream.js`
- `static/js/modalManager.js`
- `static/js/sidebar-layout.js`
- `static/js/theme.js`
- `static/js/keyboard-shortcuts.js`
- `static/js/compare/index.js`
- `static/js/research/panel.js`
