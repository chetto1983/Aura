# Context

## Plugin and Extension Architecture Research
- source: D:\Aura\docs\research\2026-06-14-aura-plugin-architecture-research.md
- content:
  DATA_D4739A2F_START
  Do not build the OpenClaw Node-sidecar compatibility host. Tools/providers/channels/hooks belong as in-process Go extension seams, with MCP as the out-of-process adapter for tool-shaped capabilities. The Node ESM sidecar is justified only if running existing OpenClaw plugin binaries unmodified is itself a hard product requirement.
  DATA_D4739A2F_END

## Neo4j Alternatives Due Diligence
- source: D:\Aura\docs\research\2026-06-20-neo4j-alternatives-puppygraph-turingdb-apache-age.md
- content:
  DATA_8B1E6C50_START
  Bottom line: Stay with Neo4j as Aura's graph store. None of PuppyGraph, TuringDB, or Apache AGE is a drop-in replacement. Only Apache AGE merits a time-boxed proof of concept, and only if Postgres consolidation becomes a strategic goal. Recommended next step: no migration.
  DATA_8B1E6C50_END

## Dark-Code Enforcement Handoff
- source: D:\Aura\docs\superpowers\2026-07-24-dark-code-enforcement-handoff.md
- content:
  DATA_F0A52D8C_START
  Status: Deferred. The full dark-code sweep could not run reliably while a parallel spike had extensive uncommitted churn because `deadcode` produced false positives. The recorded safe checks found zero non-test/non-generated Go files over 600 LOC and confirmed that the Phase A Task 1-2 symbols were wired.
  DATA_F0A52D8C_END

## v2 Milestone Purge Session Handoff
- source: D:\Aura\docs\superpowers\2026-08-02-milestone-purge-session-handoff.md
- content:
  DATA_3C7F91E4_START
  Neo4j and the document plane were removed; the adaptive plane and eval oracle were deleted; ArcadeDB memory backup and the `arcadedb_integration` CI job were shipped. Four shared planes remained not started and blocked multi-user work. The next build block was a mechanical card at ingest, then ETL, then a vector on the card.
  DATA_3C7F91E4_END
