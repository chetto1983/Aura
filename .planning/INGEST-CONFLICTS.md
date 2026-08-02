## Conflict Detection Report

### BLOCKERS (6)

[BLOCKER] LOCKED ADR contradiction — Agent Memory graph store
  Found: `D:\Aura\docs\adr\0038-graph-store-license-neo4j-gplv3-vs-arcadedb-apache.md` declares that Neo4j holds the knowledge/agent-memory graph and decides to stay on Neo4j Community for the current single-node posture; source: `D:\Aura\docs\adr\0038-graph-store-license-neo4j-gplv3-vs-arcadedb-apache.md`
  Expected: `D:\Aura\docs\adr\0042-memory-provenance-and-erasure.md` declares that the current Agent Memory store is ArcadeDB and that ADR 0038 owns the store choice; both classifications are locked on the same Agent Memory datastore scope; source: `D:\Aura\docs\adr\0042-memory-provenance-and-erasure.md`, `D:\Aura\.planning\intel\classifications\0038-graph-store-license-neo4j-gplv3-vs-arcadedb-apache-2e608790.json`, `D:\Aura\.planning\intel\classifications\0042-memory-provenance-and-erasure-afdb1c4d.json`
  → Mark one ADR Superseded or amend the locked decisions so they name the same current Agent Memory store, then re-run ingest.

[BLOCKER] Cross-reference cycle — PRD and skill-catalog search SPEC
  Found: `D:\Aura\prd.md` cross-references `D:\Aura\docs\superpowers\specs\2026-07-17-skill-catalog-search-performance-design.md`, which cross-references `prd.md`; source: `D:\Aura\.planning\intel\classifications\prd-09350e6b.json`, `D:\Aura\.planning\intel\classifications\2026-07-17-skill-catalog-search-performance-design-e00dae23.json`
  Expected: The ingest cross-reference graph must be acyclic before either document can be synthesized; source: `D:\Aura\.planning\intel\classifications\prd-09350e6b.json`, `D:\Aura\.planning\intel\classifications\2026-07-17-skill-catalog-search-performance-design-e00dae23.json`
  → Remove one side of the circular source cross-reference or exclude one document with `--manifest`, then re-run ingest.

[BLOCKER] Cross-reference cycle — PRD and workspace implementation plan
  Found: `D:\Aura\prd.md` cross-references `D:\Aura\docs\superpowers\plans\2026-07-21-aura-dedicated-workspace-garage.md`, which cross-references `prd.md`; source: `D:\Aura\.planning\intel\classifications\prd-09350e6b.json`, `D:\Aura\.planning\intel\classifications\2026-07-21-aura-dedicated-workspace-garage-c682f874.json`
  Expected: The ingest cross-reference graph must be acyclic before either document can be synthesized; source: `D:\Aura\.planning\intel\classifications\prd-09350e6b.json`, `D:\Aura\.planning\intel\classifications\2026-07-21-aura-dedicated-workspace-garage-c682f874.json`
  → Remove one side of the circular source cross-reference or exclude one document with `--manifest`, then re-run ingest.

[BLOCKER] Cross-reference cycle — PRD and scheduled-task approval SPEC
  Found: `D:\Aura\prd.md` cross-references `D:\Aura\docs\superpowers\specs\2026-07-23-scheduled-task-approval-on-channel-design.md`, which cross-references `prd.md`; source: `D:\Aura\.planning\intel\classifications\prd-09350e6b.json`, `D:\Aura\.planning\intel\classifications\2026-07-23-scheduled-task-approval-on-channel-design-b0a9fbb2.json`
  Expected: The ingest cross-reference graph must be acyclic before either document can be synthesized; source: `D:\Aura\.planning\intel\classifications\prd-09350e6b.json`, `D:\Aura\.planning\intel\classifications\2026-07-23-scheduled-task-approval-on-channel-design-b0a9fbb2.json`
  → Remove one side of the circular source cross-reference or exclude one document with `--manifest`, then re-run ingest.

[BLOCKER] Cross-reference cycle — PRD and adaptive-intelligence implementation plan
  Found: `D:\Aura\prd.md` cross-references `D:\Aura\docs\superpowers\plans\2026-07-24-aura-adaptive-intelligence-implementation.md`, which cross-references `prd.md`; source: `D:\Aura\.planning\intel\classifications\prd-09350e6b.json`, `D:\Aura\.planning\intel\classifications\2026-07-24-aura-adaptive-intelligence-implementation-4f3099ed.json`
  Expected: The ingest cross-reference graph must be acyclic before either document can be synthesized; source: `D:\Aura\.planning\intel\classifications\prd-09350e6b.json`, `D:\Aura\.planning\intel\classifications\2026-07-24-aura-adaptive-intelligence-implementation-4f3099ed.json`
  → Remove one side of the circular source cross-reference or exclude one document with `--manifest`, then re-run ingest.

[BLOCKER] Cross-reference cycle — adaptive-intelligence design and delivery appendix
  Found: `D:\Aura\docs\superpowers\specs\2026-07-24-aura-adaptive-intelligence-design.md` cross-references `D:\Aura\docs\superpowers\specs\2026-07-24-aura-adaptive-intelligence-delivery-and-evidence.md`, which cross-references the design; source: `D:\Aura\.planning\intel\classifications\2026-07-24-aura-adaptive-intelligence-design-63533f33.json`, `D:\Aura\.planning\intel\classifications\2026-07-24-aura-adaptive-intelligence-delivery-and-evidence-bf8086cc.json`
  Expected: The ingest cross-reference graph must be acyclic before either document can be synthesized; source: `D:\Aura\.planning\intel\classifications\2026-07-24-aura-adaptive-intelligence-design-63533f33.json`, `D:\Aura\.planning\intel\classifications\2026-07-24-aura-adaptive-intelligence-delivery-and-evidence-bf8086cc.json`
  → Remove one side of the circular source cross-reference or exclude one document with `--manifest`, then re-run ingest.

### WARNINGS (2)

[WARNING] Competing plugin-host variants
  Found: `D:\Aura\docs\superpowers\specs\2026-06-02-openclaw-plugin-compatibility-design.md` requires OpenClaw plugins, including providers, channels, hooks, and services, to be installable and usable, while `D:\Aura\docs\superpowers\specs\2026-06-14-aura-plugins-unified-extension-design.md` explicitly rejects a Node ESM host and OpenClaw binary/manifest compatibility; source: `D:\Aura\docs\superpowers\specs\2026-06-02-openclaw-plugin-compatibility-design.md`, `D:\Aura\docs\superpowers\specs\2026-06-14-aura-plugins-unified-extension-design.md`
  Impact: Both sources are SPECs with equal default precedence, so synthesis cannot choose a plugin-host contract without losing one stated variant; source: `D:\Aura\.planning\intel\classifications\2026-06-02-openclaw-plugin-compatibility-design-c91e2f8b.json`, `D:\Aura\.planning\intel\classifications\2026-06-14-aura-plugins-unified-extension-design-b37118b9.json`
  → Mark one SPEC Superseded or assign explicit precedence in `--manifest` before routing.

[WARNING] Competing reasoning-disclosure preference variants
  Found: `D:\Aura\docs\superpowers\specs\2026-07-15-aura-calm-prism-chat-refinement-design.md` freezes `aura.chat.reasoning.shown` as a persisted browser preference, while `D:\Aura\docs\superpowers\specs\2026-07-23-cockpit-compact-chat-ui-spec.md` deletes `reasoningPref.ts`, forbids localStorage use, and requires per-part ephemeral expansion; source: `D:\Aura\docs\superpowers\specs\2026-07-15-aura-calm-prism-chat-refinement-design.md`, `D:\Aura\docs\superpowers\specs\2026-07-23-cockpit-compact-chat-ui-spec.md`
  Impact: Both sources are SPECs with equal default precedence, so synthesis cannot choose the persistence contract from filename date or document order; source: `D:\Aura\.planning\intel\classifications\2026-07-15-aura-calm-prism-chat-refinement-design-afe9f720.json`, `D:\Aura\.planning\intel\classifications\2026-07-23-cockpit-compact-chat-ui-spec-665762d7.json`
  → Mark one SPEC Superseded or assign explicit precedence in `--manifest` before routing.

### INFO (0)
