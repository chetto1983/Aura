# Aura Deep Search Figma Package

This folder contains Figma-ready design artifacts for Aura deep search.

Current board:
- `aura-deep-search-elysia-informed.svg` - current Elysia/Odysseus-informed
  Aura design with dedicated Neo4j Graph Explorer, dockable operator tools,
  MCP configuration, skills install governance, and approval center.
- `aura-deep-search-elysia-informed-preview.png` - rendered preview.

Files:
- `aura-deep-search-board.svg` - first-pass board, kept for comparison.
- `tokens.json` - design tokens for implementation.
- `ux-spec.md` - screen behavior, component inventory, and copy contract.
- `odysseus-pattern-study.md` - source-study notes and Aura translation.
- `BACKEND_CAPABILITY_MAP.md` - backend functionality mapped to Figma surfaces.
- `backend-capability-map.json` - machine-readable backend capability map.
- `FIGMA_PROJECT.md` - professional Figma project setup and review contract.
- `figma-project-manifest.json` - page, token, component, screen, and backend map manifest.
- `figma-capture.html` - local HTML capture surface for importing the full project into Figma.
- `VISUAL_DEBUG.md` - local Playwright visual-debug setup and commands.
- `package.json` - repeatable Node/Playwright harness for visual validation.

Current board frames:
- Frame 01 - Aura Chat + Display Workspace
- Frame 02 - Tree View + Reasoning Drawer
- Frame 03 - Source Explorer
- Frame 04 - System Events
- Frame 05 - Mobile
- Frame 06 - Neo4j Graph Explorer
- Frame 07 - Odysseus Operator OS Patterns
- Frame 08 - MCP Configuration + Skills Install

Project infrastructure capture sections:
- 00 README / Senior design workspace
- 01 File architecture
- 02 Foundations
- 03 Components
- 04 Patterns and flows
- 05 Backend capability map
- 06 Screens / imported source board
- 07 Prototype QA

Visual debug:
- `npm install`
- `npm run visual:install-browsers`
- `npm run visual:debug`

The visual runner starts or reuses a local static server, captures the desktop
viewport plus the backend map, source screen board, and prototype QA sections,
and writes artifacts to `.visual-debug/<timestamp>/`.

The SVG files are intentionally vector/text-based so they can be opened or
imported into Figma as editable layers. They are also readable directly in a
browser.

Design stance:
- Chat-first, but not chat-only: Chat, Tree, Graph, Displays, Settings.
- Typed dynamic renderers inspired by `weaviate/elysia-frontend`.
- Operator-OS mechanics inspired by `pewdiepie-archdaemon/odysseus`: adaptive
  icon rail, dockable tools, command palette, background job feedback, and
  audited UI-control events.
- First-class Online, D:\tmp, Neo4j Graph, MCP, and sidecar artifact
  provenance.
- Neo4j graph visualization is evidence-bearing: labels, edges, paths, Cypher,
  schema filters, and selected-node properties are inspectable.
- Aura backend control planes are visible: runtime shell, agent loop, tool
  registry, conversations, HITL, web safety, execution tools, scheduler, skills,
  MCP, AG-UI, swarm, memory, channels, and packaging.
- MCP and skills are governed operator surfaces: config provenance, redacted
  env, doctor/tools checks, risk tiers, pending activation, approvals, and audit
  events are visible.
- Minimal industrial swarm UI: worker reports, not inter-agent chat theater.
