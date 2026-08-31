# Phase 49 API Coverage Declaration

Phase 49 adds no new external API integration, third-party SDK, service dependency, or package-manager install surface.

The phase extends Aura's existing in-tree PostgreSQL, ArcadeDB, MCP, runner, and evaluator contracts. `memory_recall` and the planned `memory_batch` operation are Aura-owned MCP surfaces served by the already integrated `cmd/arcadedb-mcp` process; ArcadeDB remains the existing configured backend. Therefore capability-row generation against the stale `.planning/intel/API-SURFACE.md` would fabricate external capabilities from an index that explicitly reports zero symbols and `stale: true`.

Coverage is instead carried by the Phase 49 plan contracts and live tests for authenticated identity isolation, unified retrieval, explicit-only reasoning, ordered capture durability, and atomic batch semantics.
