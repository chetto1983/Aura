---
phase: 02-agent-cornerstone
plan: 01
subsystem: infra
tags: [go, dependencies, uuid, rapid, canonical-json, determinism, hashing]

# Dependency graph
requires:
  - phase: 02-00
    provides: Gate-0 converged artifacts (SPEC/CONTEXT/RESEARCH/PATTERNS), A1-A7 amendments
provides:
  - github.com/google/uuid v1.6.0 as a direct dependency (UUIDv7 TraceID source for D-16/SC#4)
  - pgregory.net/rapid v1.3.0 as a direct test-only dependency (property-based testing, D-21)
  - internal/canonicaljson.Marshal — deterministic serializer for sha256(name+canonical_json(args)) dedup fingerprint
affects: [02-03 budget dedup, 04 conversation hash, 11 skill content_hash, agent event trace IDs]

# Tech tracking
tech-stack:
  added:
    - github.com/google/uuid v1.6.0
    - pgregory.net/rapid v1.3.0 (test-only)
  patterns:
    - "Deterministic JSON via json.Number (UseNumber) literal preservation — 1 != 1.0"
    - "Strict-reject un-canonicalizable input through encoding/json validation round-trip"
    - "Go-byte-order map key sorting for hash-stable output (NOT RFC-8785)"

key-files:
  created:
    - internal/canonicaljson/canonicaljson.go
    - internal/canonicaljson/canonicaljson_test.go
  modified:
    - go.mod
    - go.sum

key-decisions:
  - "canonicaljson is NOT RFC-8785: no cross-system signature consumer, so float-canonicalization minefield is deliberately avoided (D-08/A3)"
  - "Numbers preserved as literal text (json.Number) so 1 and 1.0 stay distinct — prevents silent hash-collision merging of distinct tool calls (T-02-04 mitigation)"
  - "uuid promoted to direct via a genuine test consumer (serializing a uuid.UUID), not a synthetic blank import — keeps go mod tidy honest"
  - ".golangci.yml left untouched: existing _test.go gosec/errcheck exclusions already cover the new rapid import; no new path exclusion needed"

patterns-established:
  - "Deterministic serializer: normalize-via-json-roundtrip then recursive byte-order-sorted encode"
  - "TDD RED (failing test) -> GREEN (minimal impl) commit cadence with atomic per-step commits"

requirements-completed: [INFRA-03]

# Metrics
duration: ~25min
completed: 2026-05-29
---

# Phase 2 Plan 01: Two-Dep + Canonical JSON Foundation Summary

**Promoted google/uuid v1.6.0 to a direct dep, added pgregory.net/rapid v1.3.0 (test-only), and hand-rolled the internal/canonicaljson deterministic serializer (D-08/A3 — NOT RFC-8785) that distinguishes 1 from 1.0, sorts keys by Go byte order, and strict-rejects NaN/Inf/func/chan; fuzz-clean over 200k execs and race-clean.**

## Performance

- **Duration:** ~25 min
- **Started:** 2026-05-29
- **Completed:** 2026-05-29
- **Tasks:** 2 (Task 2 was TDD: RED + GREEN)
- **Files modified:** 4 (2 created, 2 dep manifests)

## Accomplishments
- `github.com/google/uuid v1.6.0` is now a **direct** require (was absent/indirect); `pgregory.net/rapid v1.3.0` added as a direct test-only dep. `go mod verify` clean; go.sum reconciled with full h1 hashes (the planning-time partial uuid line is now complete).
- `internal/canonicaljson.Marshal(any) ([]byte, error)` ships: key-order-independent, `1` != `1.0` (json.Number literal preservation), strict-rejects un-canonicalizable input with nil bytes + wrapped error.
- `FuzzCanonical_RoundTripAndDistinctNumbers` fuzz target plus a rapid property test and 6 unit tests; fuzz ran 200k execs / 190 interesting in 10s with no failing corpus entry.
- 149 LOC (≤600 cap), no `internal/agent` import (one-direction dependency), golangci-lint 0 issues module-wide.

## Task Commits

1. **Task 1: Promote uuid to direct dep + add rapid** - `ec6cd05d` (chore)
2. **Task 2 (RED): failing canonicaljson determinism + fuzz tests** - `736ff2e5` (test)
3. **Task 2 (GREEN): hand-roll canonicaljson serializer + go mod tidy promotion** - `2e90a9ad` (feat)

_Note: Task 2 is TDD — RED then GREEN. No REFACTOR commit needed (impl was clean at GREEN: each encode function single-responsibility, 149 LOC)._

## Files Created/Modified
- `internal/canonicaljson/canonicaljson.go` - `Marshal(any) ([]byte, error)`: normalize via json round-trip (UseNumber) then recursive Go-byte-order-sorted encode; rejects NaN/Inf/func/chan.
- `internal/canonicaljson/canonicaljson_test.go` - 6 unit tests + 1 rapid property test + `FuzzCanonical_RoundTripAndDistinctNumbers`; imports uuid (real consumer) and rapid.
- `go.mod` - uuid + rapid in the direct require block.
- `go.sum` - full h1 hashes for both new modules.

## Verify Command Outputs (evidence)

**Task 1:**
```
$ go build ./...            # exit 0
$ go list -m github.com/google/uuid pgregory.net/rapid
github.com/google/uuid v1.6.0
pgregory.net/rapid v1.3.0
$ grep -v '^//' go.mod | grep 'github.com/google/uuid v1.6.0'   # present, no // indirect after tidy
$ go mod verify             # all modules verified
```

**Task 2:**
```
$ go test ./internal/canonicaljson/                      # ok  0.653s
$ go test -race ./internal/canonicaljson/                # ok  2.316s   (as --version = Binutils 2.46)
$ go test -run x -fuzz FuzzCanonical_RoundTripAndDistinctNumbers -fuzztime 10s ./internal/canonicaljson/
  fuzz: elapsed: 11s, execs: 200090, new interesting: 182 (total: 190)
  PASS  ok  11.946s
$ go vet ./internal/canonicaljson/...                    # clean
$ ~/go/bin/golangci-lint run ./...                       # 0 issues
$ bash scripts/check-file-size.sh                        # all Go files within the 600-LOC cap
```

## Decisions Made
- **uuid promoted via a real test consumer.** `go mod tidy` prunes any require with no importer. Rather than a synthetic blank import or skipping tidy, the test serializes a `uuid.UUID` (a realistic downstream input — Phase-2 feeds trace IDs through this serializer), giving uuid a genuine consumer so it lands as a clean **direct** dep after tidy. rapid is consumed by the property test the same way.
- **`.golangci.yml` untouched.** The plan permitted touching it only if a NEW path exclusion were genuinely required. The existing `_test.go` rule (gosec/errcheck excluded) plus the enabled linter set lint the rapid/uuid test imports with 0 issues, so no change was made.
- **No REFACTOR step.** GREEN implementation was already clean (single-responsibility encode helpers, 149 LOC), so the TDD REFACTOR phase produced no diff.

## Deviations from Plan

None - plan executed exactly as written. The plan's `go get ... && go mod tidy` sequence behaves as intended once a code/test consumer exists: Task 1 committed the deps (uuid present in require block, go.sum reconciled), and the Task 2 GREEN commit's `go mod tidy` flipped uuid + rapid from `// indirect` to direct once the test imported them. This is the normal Go tooling order, not a deviation.

## Issues Encountered
- `go mod tidy` initially pruned uuid + rapid because nothing imported them yet (expected Go behavior). Resolved by ordering: commit deps in Task 1 (acceptance grep passes on the require-block line), then let the Task 2 test imports promote both to direct via tidy in the GREEN commit. `go mod verify` clean throughout.

## User Setup Required
None - no external service configuration required. No new env vars in this plan (the A7 `AURA_LOOP_*` vars land in later Phase-2 plans).

## Threat Model Notes
- **T-02-04 (Tampering, dedup fingerprint):** mitigated as planned — strict-reject NaN/Inf/func/chan + `json.Number` literal preservation (`1` != `1.0`) proven by `FuzzCanonical_RoundTripAndDistinctNumbers` and `TestMarshal_DistinctNumberLiterals`.
- **T-02-SC (Tampering, go get of uuid+rapid):** accepted per plan — both are registry-verified canonical packages pinned at exact versions; `go mod verify` clean.
- No new security surface introduced beyond the threat register.

## Next Phase Readiness
- `internal/canonicaljson.Marshal` is ready for the Plan 02-03 Budget dedup fingerprint (`sha256(name + canonicaljson.Marshal(args))`) and for Phase 4/11 hash reuse.
- `github.com/google/uuid` is wired direct, ready for the agent Event UUIDv7 TraceID work (D-16) in the sibling Phase-2 plan that creates `event.go`.
- `pgregory.net/rapid` available for the budget/event property tests (D-21) in later plans.
- No blockers.

## Self-Check: PASSED

- FOUND: internal/canonicaljson/canonicaljson.go
- FOUND: internal/canonicaljson/canonicaljson_test.go
- FOUND: .planning/phases/02-agent-cornerstone/02-01-SUMMARY.md
- FOUND commit ec6cd05d (Task 1 deps)
- FOUND commit 736ff2e5 (Task 2 RED)
- FOUND commit 2e90a9ad (Task 2 GREEN)

---
*Phase: 02-agent-cornerstone*
*Completed: 2026-05-29*
