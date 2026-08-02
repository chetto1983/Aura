# Document Ingest Synthesis

## Document Counts

- classifications consumed: 37 (ADR: 9, SPEC: 23, PRD: 1, DOC: 4)
- documents synthesized: 30 (ADR: 9, SPEC: 17, PRD: 0, DOC: 4)
- documents excluded from synthesis due cross-reference cycles: 7

## Decisions

- decisions extracted: 9
- locked decisions: 8
- locked sources:
  - D:\Aura\docs\adr\0037-per-identity-docker-sandbox.md
  - D:\Aura\docs\adr\0038-graph-store-license-neo4j-gplv3-vs-arcadedb-apache.md
  - D:\Aura\docs\adr\0039-conversation-sharing-vs-identity-isolation.md
  - D:\Aura\docs\adr\0040-agent-loop-semantics.md
  - D:\Aura\docs\adr\0041-tool-consequence-policy.md
  - D:\Aura\docs\adr\0042-memory-provenance-and-erasure.md
  - D:\Aura\docs\adr\0043-mcp-trust-and-lifecycle.md
  - D:\Aura\docs\adr\0044-deployment-profiles.md

## Requirements

- requirements extracted: 0
- IDs: absent
- note: the sole PRD, `D:\Aura\prd.md`, is in a cyclic cross-reference component and was not synthesized

## Constraints

- constraints extracted: 17
- type breakdown: api-contract: 1, schema: 1, nfr: 5, protocol: 10

## Context

- context topics: 4
- topics: Plugin and Extension Architecture Research; Neo4j Alternatives Due Diligence; Dark-Code Enforcement Handoff; v2 Milestone Purge Session Handoff

## Conflicts

- blockers: 6
- competing variants: 2
- auto-resolved: 0
- detail: D:\Aura\.planning\INGEST-CONFLICTS.md

## Intel Files

- decisions: D:\Aura\.planning\intel\decisions.md
- requirements: D:\Aura\.planning\intel\requirements.md
- constraints: D:\Aura\.planning\intel\constraints.md
- context: D:\Aura\.planning\intel\context.md
