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

Current board frames:
- Frame 01 - Aura Chat + Display Workspace
- Frame 02 - Tree View + Reasoning Drawer
- Frame 03 - Source Explorer
- Frame 04 - System Events
- Frame 05 - Mobile
- Frame 06 - Neo4j Graph Explorer
- Frame 07 - Odysseus Operator OS Patterns
- Frame 08 - MCP Configuration + Skills Install

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
- MCP and skills are governed operator surfaces: config provenance, redacted
  env, doctor/tools checks, risk tiers, pending activation, approvals, and audit
  events are visible.
- Minimal industrial swarm UI: worker reports, not inter-agent chat theater.
