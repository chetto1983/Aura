# Phase 49 API Coverage Declaration

No external API integration: Phase 49 changes Aura-owned in-tree memory, runner, MCP, PostgreSQL, ArcadeDB, and evaluator contracts without adding a third-party API, SDK, service, or package install.

Phase 49 adds no new external API integration, third-party SDK, service dependency, or package-manager install surface.

The phase extends Aura's existing in-tree PostgreSQL, ArcadeDB, MCP, runner, and evaluator contracts. `memory_recall` and the planned `memory_batch` operation are Aura-owned MCP surfaces served by the already integrated `cmd/arcadedb-mcp` process; ArcadeDB remains the existing configured backend. Therefore capability-row generation against the stale `.planning/intel/API-SURFACE.md` would fabricate external capabilities from an index that explicitly reports zero symbols and `stale: true`.

Coverage is instead carried by the Phase 49 plan contracts and live tests for authenticated identity isolation, unified retrieval, explicit-only reasoning, ordered capture durability, and atomic batch semantics.

## Revision-3 plan coverage

- **14 plans / 32 tasks / 12 waves.** Every task has `<files>`, `<action>`, `<verify><automated>`, `<fails_when>`, `<acceptance_criteria>`, and `<done>`.
- All 32 automated commands are fail-fast (`set -euo pipefail`), and focused Go/Python test gates use their native inventory/output mechanisms to fail executable zero-test runs.
- Every plan has the exact `## Artifacts this phase produces` section and names its symbols, schema/transport/environment fields, and repository analogs.
- Every plan modifies fewer than 10 files (range 3–9); same-wave modified-file overlap is zero.
- Schema aggregation is explicit: Plan 49-07 registers `conversationSchemaStatements` in `EnsureMemorySchema` after Plan 49-06; Plan 49-04 registers `reasoningSchemaStatements` in `EnsureMemorySchema` after Plan 49-08.
- Live/integration proof is split from threshold-sized production slices: 49-08→49-13 and 49-10→49-14; reasoning schema/retrieval 49-04 precedes production builder 49-12.

## Multi-source audit

| Source | Items | Coverage |
|---|---:|---|
| ROADMAP goal | 1 | Plans 49-02/07 projection; 03/08/13 recall; 04/12/09 reasoning; 05/10/14 capture; 06/11 atomic batch |
| Requirements | 8/8 | MEM-01: 02/07/11; MEM-02: 03/08/13/11; MEM-03: 04/12/09/11; MEM-06: 01/04/11; TOOL-05: 01/03/08/13/11; AUTO-03: 05/10/14/11; CTX-05: 04/05/09/10/12/14/11; HARN-05: 06/11 |
| Research contracts | all resolved | Native ArcadeDB `vector.fuse`; tier `effective_path` separate from actual graph/hybrid/lexical backend `path` with query/entity/fallback response/OTel equality; unsigned/untrusted bounded cursor; exact reasoning success 30d and failed/cancelled 7d; post-commit projection; direct AcceptedCapture producers; final-state batch transaction |
| Context decisions | 21/21 | D-01: 01/04/11; D-02–03: 02/07/09; D-04–05: 02/07; D-06–10: 03/08/13; D-11–14: 04/12/09; D-15–18: 05/10/14; D-19–21: 06/11 |

Deferred ideas remain excluded. No external package install, service setup, or new SDK is planned.

## Wave and ownership audit

`W1 01 → W2 02+06 → W3 07 → W4 03 → W5 08 → W6 04+13 → W7 12 → W8 09 → W9 05 → W10 10 → W11 14 → W12 11`

Plan 49-11 owns the final no-skip evaluator, exact Amendment #201 isolation/non-empty six-path ancestry, and three named authenticated scenarios against running Aura: `beyond_active_context_recall` has exactly 1 terminal answer, `provider_visible_reasoning_exclusion_explicit_recall` has exactly 3, and `durable_shell_file_capture_later_recall` has exactly 2. The report records every observed terminal answer in a `responses` array with a globally unique stable response/turn ID, its own `actual_response_score`, and evidence/correlation references. The observed terminal IDs and scored response IDs must form an exact per-scenario bijection, and every one of the six scores must be strictly >9.8; averages, aggregate-only values, missing, duplicate, extra, or unscored answers fail. Unit fixtures include a weak later response hidden by a high aggregate plus missing/duplicate/extra response failures. Correlated Tempo, RLS-scoped `aura.tool_invocations`, `aura.conversation_turns`, and ArcadeDB evidence must pass before Go vet/build/unit/race/goleak, coverage, and mutation can pass.
