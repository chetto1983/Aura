# Concurrent reasoning deletion — 2026-09-07

CI run `34118571515` reported MRS 100 with `suite_integrity=FAIL`. Its artifact
identified `TestReasoningGraphLive_ExpiryDeleteRace`: a test outside the scored
scenario list failed in the full live ArcadeDB package. The integrity gate was
correct; the CLI summary did not name the failing test.

The failure reproduced locally on the installed ArcadeDB engine. Two concurrent
deletions can produce HTTP 404 with the exact exception
`com.arcadedb.exception.VertexNotFoundException` after one transaction removes
a vertex selected by the other. A single successful run did not exclude this race.

The deletion retry loop now recognizes this specific failure, rolls back the
failed transaction and selects roots again in a new transaction. It does not
retry arbitrary 404s or report a failed deletion as success. Persistent missing
vertices exhaust the existing retry bound and remain errors.

Verification:

- Existing live expiry/delete race: **1,000/1,000 passed** after the correction.
- Focused tests verify fresh transaction selection, rollback order, persistent
  failure bounds, ordinary write conflicts and refusal of unrelated error classes.
- Six viable compiler-overlay mutations of the retry classifier: **6/6 killed**.
  The original production source was never modified during mutation execution.
- Full `make agent-memory-eval`: **PASS, MRS 100, suite integrity PASS**.
  Its Python entry suite now runs 70 tests, all passing.
- Package vet and Aura/MCP builds: PASS.

The CLI also reports failed suite/test names. Protocol parsing and focused tests
were split into their own modules to keep the touched source files below 600 lines;
scoring, coverage thresholds and gate behavior were preserved.

`running_aura_conversation` remained explicitly NOT_EVALUATED in this invocation.
The result is operational memory validation, not an independent answer-quality
benchmark. The existing mounted-MCP and guided answer evidence retains its own scope.
