# Aura Deep Search UI/UX Spec

Date: 2026-06-04

This revision is altered after reading `weaviate/elysia-frontend` source. The
current source board is `aura-deep-search-elysia-informed.svg`.

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

## Component Inventory

- App shell
- Navigation rail
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
- System event card
- Mobile display drawer

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

Suggested display type mapping:

- `web_search` result -> `web_result` display
- `web_fetch` markdown -> `document` display
- `sandbox_exec` sidecar output -> `code` or `local_artifact` display
- `swarm_spawn` child report -> `swarm_report` display
- Neo4j MCP result -> `graph_chunk`, `graph_path`, `graph_schema`, or `table`
  display
- ask_user pause -> `system_event` with `needs_user_input`
- SSRF block -> `system_event` with safe block reason

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

## Important Non-Goals

- Do not copy Elysia's Weaviate collection model directly.
- Do not copy Elysia's abstract sphere as the main Aura dashboard decoration.
- Do not add swarm talk, join, mailbox, or sibling chat UI.
- Do not expose raw SearXNG backend parameters to the operator or model.
- Do not flatten every payload into generic cards; typed displays are the point.
- Do not render Neo4j as a decorative hairball. The graph view must answer an
  evidence/path question and keep source provenance visible.

## Source Files Read

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
