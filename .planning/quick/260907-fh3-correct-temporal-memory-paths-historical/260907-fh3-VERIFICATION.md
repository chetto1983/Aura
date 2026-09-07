# Verification — 2026-09-07

Local acceptance: PASS. Publication and CI status are reported against the final
pushed commit, separately from these reproducible measurements.

- Native temporal path cases: expired shortcut/live alternative, inclusive start,
  exclusive end, future/unknown/missing support, directed/reverse/mixed paths,
  empty results, bounds and repeatable records during a concurrent close.
- Historical support: actual MCP supersession clears the active key, retains RID
  evidence, and survives mention rebuilding. Native uniqueness distinguishes
  multiple facts supporting identical endpoints; obsolete-index migration passes.
- Expansion: temporal support checked at every hop, direct evidence first,
  malformed evidence refused, oversized/partial inventories cause no sweep writes.
- Full live ArcadeDB race suite: 86.7% statement coverage, PASS, 17.418 seconds,
  no skipped tests. Full MCP SDK live race suite: PASS, 6.5 seconds.
- Final global `go vet ./...` and `go build ./...`: PASS. Fresh unit/race tests
  for ArcadeDB, MCP, the managed bridge and skills: PASS. Commit hooks checked
  formatting, the 600-line cap, package vet and lint (zero issues).
- Mounted MCP: 11/11 cases on the rebuilt containers, including schema migration
  and authenticated historical retrieval. See `mounted-e2e-results.json`.
- Fixed-corpus budget comparison and final answers: see `mounted-budget-results.json`,
  `ANSWER-CRITERIA.md`, and `ANSWER-RESULTS.md`.
- Mutation: `go-mutesting --match validateTemporalFact` produced 28 candidates:
  26 killed, one survivor and one duplicate (27 unique, 96.3%). The survivor removes
  `err != nil` for valid_from; parse failure returns zero, still rejected by the
  adjacent zero-time guard. No mutant build failures were counted as kills.

Mutation runs modified the target transiently; the accepted live suites ran
after restoration. Earlier contaminated or invalid runs were discarded.
Temporary databases were explicitly created and dropped for these tests; the
operator's memory received no test fixtures. Schema migration and scheduled
mention reconciliation were exercised on the actual deployed memory.

Review confirmed that query interpolation is limited to validated relation types,
direction punctuation and bounded depth; entity names and timestamps are parameters.
Identity remains OAuth-derived. No caller-supplied database or arbitrary query was
added. Native LINKs avoid treating a closed active key as historical identity.

Residual limits: valid-time over retained records, possible phantoms, guided
self-evaluation rather than a broad independent benchmark. Phase 49's separate
agent-quality acceptance is not declared closed by this task.
