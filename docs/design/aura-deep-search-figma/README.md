# Aura Deep Search Figma Package

This folder contains Figma-ready design artifacts for Aura deep search.

Current board:
- `aura-deep-search-elysia-informed.svg` - current Elysia-informed Aura design
  with dedicated Neo4j Graph Explorer.
- `aura-deep-search-elysia-informed-preview.png` - rendered preview.

Files:
- `aura-deep-search-board.svg` - first-pass board, kept for comparison.
- `tokens.json` - design tokens for implementation.
- `ux-spec.md` - screen behavior, component inventory, and copy contract.

The SVG files are intentionally vector/text-based so they can be opened or
imported into Figma as editable layers. They are also readable directly in a
browser.

Design stance:
- Chat-first, but not chat-only: Chat, Tree, Graph, Displays, Settings.
- Typed dynamic renderers inspired by `weaviate/elysia-frontend`.
- First-class Online, D:\tmp, Neo4j Graph, MCP, and sidecar artifact
  provenance.
- Neo4j graph visualization is evidence-bearing: labels, edges, paths, Cypher,
  schema filters, and selected-node properties are inspectable.
- Minimal industrial swarm UI: worker reports, not inter-agent chat theater.
