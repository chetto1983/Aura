---
phase: 37F-conversation-artifact-sharing-export-inserted
plan: 03
subsystem: security
tags: [redaction, allowlist, share, llm-message, mutation-testing, go]

# Dependency graph
requires:
  - phase: 37F-01
    provides: "PRD-amendment WEBSHARE-01..04 + ADR 0039 (sharing vs identity isolation) authorizing this schema"
provides:
  - "internal/share package (net-new): Snapshot / SnapshotTurn / SnapshotArtifact types + BuildSnapshot — the SC3 redaction boundary"
  - "redact.go: projectTurns (user/assistant allowlist, dense Seq, tool-role/system drop), toolNames (Function.Name only, order+dupes preserved, nil-normalized), projectArtifact (4-key allowlist)"
  - "ConvMeta / ArtifactMeta narrow local input structs (no conversations import, no path field)"
  - "Hostile-fixture test suite proving L-01..L-09 unrepresentable (13 test funcs, 100% coverage)"
  - "Mutation score 87.5% killed on redact.go+snapshot.go, recorded in 37F-VALIDATION.md"
affects: [37F-05, 37F-06, 37F-10, share-format-md-json, share-service, public-share-page]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Allowlist-construction projection (never denylist/delete): ported the sseAdapter.ts:353-361 technique to Go — every output field is assigned individually from a named source field, never produced by unmarshalling into map[string]any and deleting keys"
    - "Narrow local input structs (ConvMeta/ArtifactMeta) instead of accepting the domain type, so a structurally-absent field (owner identity, filesystem path) cannot be threaded through even by a future caller mistake"
    - "nolint:staticcheck used deliberately once, with a written reason, to keep an explicit allowlist construction instead of accepting the linter's suggested type-conversion shortcut — documented as a permanent decision, not suppressed noise"

key-files:
  created:
    - internal/share/snapshot.go
    - internal/share/redact.go
    - internal/share/snapshot_test.go
    - internal/share/redact_test.go
  modified: []

key-decisions:
  - "RED-phase compiling stub (Rule 3 auto-fix): the pre-commit go vet/lint hook blocks a genuinely non-compiling commit, so Task 1's RED commit ships snapshot.go with its FINAL types/doc comments but a stub BuildSnapshot returning a zero-value Snapshot — every test still fails on real assertions against the empty Snapshot, not a build error. The literal 'package does not exist, undefined: BuildSnapshot' RED state was verified standalone (via go test) before the stub was added, satisfying the plan's own <verify> requirement. This mirrors the identical constraint hit and resolved the same way in 37F-02."
  - "Dead-code removal during mutation autopsy (Rule 1): toolNames' first `if len(calls) == 0 { return nil }` guard was redundant with the trailing `if len(names) == 0 { return nil }` guard (both paths return nil for an empty result) — removed rather than kept as an equivalent-mutant magnet, and a new assertion (TestSnapshotToolNamesNilWhenAllBlank) was added to pin the remaining guard's nil-normalization behavior directly (`!= nil`, not just `len==0`), since JSON `omitempty` alone cannot distinguish nil from a non-nil empty slice."
  - "The one surviving mutant (branch mutator targeting projectTurns' empty `default:` arm) was verified via md5sum to be byte-identical to the unmutated source — a true no-op mutation, not a behavioral gap. Classified equivalent/advisory-accepted, confirmed not leak-class (it never touches projectArtifact, toolNames, or any argument/path-bearing field)."
  - "SchemaVersion is asserted via an unmarshalled marshalled-key-set comparison (not just SchemaVersion==1), per the plan's requirement that a json tag rename break this test rather than silently diverge from plan 37F-05's TypeScript mirror."

requirements-completed: [WEBSHARE-03]

coverage:
  - id: D1
    description: "BuildSnapshot is the only function accepting []llm.Message; Snapshot/SnapshotTurn/SnapshotArtifact have no field capable of holding an argument, a result, a filesystem path, or the owner identity id"
    requirement: "WEBSHARE-03"
    verification:
      - kind: unit
        ref: "internal/share/snapshot_test.go#TestSnapshotRedactsHostPaths (hostile fixture: send_file path arg, shell_exec /etc/passwd arg, raw tool stdout with container id + sidecar path — none survive; assistant prose + tool names do)"
        status: pass
      - kind: unit
        ref: "internal/share/snapshot_test.go#TestSnapshotOmitsIdentity (structural reflect check + behavioral check with an identity id embedded in a tool argument)"
        status: pass
      - kind: structural
        ref: "grep -nE \"Arguments|ArgsRaw|ResultPreview|SidecarPath|IdentityID|ToolCallID\" internal/share/snapshot.go → no matches"
        status: pass
    human_judgment: false
  - id: D2
    description: "redact.go is construction-only (allowlist), never a denylist/regex/delete-based projection"
    requirement: "WEBSHARE-03"
    verification:
      - kind: structural
        ref: "grep -nE \"delete\\(|regexp\\.|ReplaceAll\" internal/share/redact.go → no matches; grep -c SanitizeString → 2 (anti-analog named in header)"
        status: pass
      - kind: unit
        ref: "internal/share/redact_test.go (TestSnapshotKeepsToolNamesDropsArgs, TestSnapshotToolNamesOrderAndDuplicates, TestSnapshotToolNamesOmittedWhenEmpty, TestSnapshotToolNamesNilWhenAllBlank, TestSnapshotArtifactAllowlist)"
        status: pass
    human_judgment: false
  - id: D3
    description: "internal/share imports neither internal/conversations nor internal/agui; package coverage ≥85%; mutation score ≥70% killed with zero leak-class survivors"
    requirement: "WEBSHARE-03"
    verification:
      - kind: unit
        ref: "go list -deps ./internal/share/ | grep -E 'internal/(conversations|agui)$' → no matches"
        status: pass
      - kind: unit
        ref: "go test ./internal/share/ -cover -count=1 → 100.0%"
        status: pass
      - kind: manual
        ref: "go-mutesting ./internal/share/redact.go ./internal/share/snapshot.go (WSL) → 87.5% killed (7/8), 1 equivalent survivor classified in 37F-VALIDATION.md"
        status: pass
    human_judgment: false

duration: 35min
completed: 2026-07-17
status: complete
---

# Phase 37F Plan 03: The Canonical Redacted Conversation Snapshot Summary

**`internal/share.BuildSnapshot` — the sole allowlist projection from `[]llm.Message` to share-bound data, structurally incapable of carrying tool arguments, tool results, filesystem paths, or the owner's identity, verified against a hostile fixture and an 87.5%-killed mutation spot-check**

## Performance

- **Duration:** ~35 min
- **Started:** 2026-07-17T11:00Z (session start, after 37F-02)
- **Completed:** 2026-07-17T11:29Z
- **Tasks:** 3 planned, all completed as specified (TDD RED→GREEN + mutation spot-check)
- **Files modified:** 4 (all new)

## Accomplishments

- `internal/share.Snapshot` / `SnapshotTurn` / `SnapshotArtifact` — the OQ4 wire-contract types, with json tags verified byte-exact by a marshalled-key-set test (`TestSnapshotSchemaVersion`) rather than just a `SchemaVersion==1` check, so a future tag rename breaks this test instead of silently diverging from plan 37F-05's TypeScript mirror
- `BuildSnapshot(ConvMeta, []llm.Message, []ArtifactMeta, time.Time) (Snapshot, error)` — the ONLY function in the repo that accepts an `llm.Message` and returns share-bound data; takes `snapshotAt` as a parameter (no internal `time.Now()`) for deterministic testability
- `redact.go`'s three allowlist projections (`projectTurns`, `toolNames`, `projectArtifact`), each constructing its output field-by-field — ported the technique from `web/src/chat/sseAdapter.ts:353-361` (the repo's only prior allowlist projection) rather than modeling on `internal/agui/server_redact.go`'s regex denylist (named explicitly as the anti-analog in the header)
- All 9 verified leak sources (L-01..L-09 from 37F-RESEARCH.md) made unrepresentable: tool call `Arguments` never read, `role=="tool"`/`"system"` turns dropped entirely (dense `Seq`, never leaking how many were dropped), the `send_file` `{path}` descriptor collapsed to a 4-key allowlist, `Conversation.IdentityID` never accepted (via the narrow `ConvMeta` input, not `conversations.Conversation`), `ContentSidecarPath` structurally unreachable (input type is `[]llm.Message`, which has no such field), and every `ToolCallID`/`ToolCall.ID` correlation id omitted
- A hostile-fixture test (`TestSnapshotRedactsHostPaths`) traceable to real code shapes (`send_file.go`'s `{"path":...}` argument and `"cannot read %q"` error, a shell_exec-style `/etc/passwd` argument, a `$AURA_RUN_DIR` sidecar path template) with both negative assertions (no forbidden substring survives) and positive assertions (assistant prose + tool names DO survive — closing the "empty Snapshot passes a pure negative test" gap)
- 13 test functions total, 100.0% package coverage, all green under `-race`
- Mutation spot-check (WSL `go-mutesting`) on the SC3 core: 87.5% killed (7/8), the single survivor confirmed via `md5sum` to be a true no-op mutation (byte-identical to the original source) targeting an intentionally-empty `default:` drop arm — not a leak, recorded and classified in `37F-VALIDATION.md`

## Task Commits

Each task was committed atomically:

1. **Task 1 RED: failing hostile-fixture tests for the share snapshot redaction core** - `cd174fdc9` (test)
2. **Task 2 GREEN: implement the allowlist share snapshot projection** - `a36d803d3` (feat)
3. **Task 3: mutation spot-check (87.5% killed) + kill a real gap** - `193ba508b` (test)

**Plan metadata:** (this commit, docs: complete plan)

_Note: Task 1 is TDD RED (compiling stub, per Deviations below); Task 2 is TDD GREEN. Task 3's auto-fix (dead-code removal + a new pinning assertion) landed in its own commit since it was discovered during the mutation exercise, not during Task 2._

## Files Created/Modified

- `internal/share/snapshot.go` - `Snapshot`/`SnapshotTurn`/`SnapshotArtifact`/`ConvMeta`/`ArtifactMeta` types + `BuildSnapshot`
- `internal/share/redact.go` - `projectTurns`/`toolNames`/`projectArtifact` allowlist projections
- `internal/share/snapshot_test.go` - hostile fixture + 8 `BuildSnapshot`-level tests
- `internal/share/redact_test.go` - 5 projection-level tests (tool names, artifact allowlist)

## Decisions Made

- **RED-phase compiling stub** (documented above in frontmatter `key-decisions`): the pre-commit `go vet`/lint gate cannot be bypassed for a literal non-compiling TDD RED commit, so `snapshot.go` shipped in Task 1 with its final doc comments and types but a stub `BuildSnapshot` returning a zero-value `Snapshot`. The true RED state (`undefined: BuildSnapshot` etc.) was verified standalone via `go test ./internal/share/` immediately after writing the test files and before adding the stub — satisfying the plan's own `<verify>` requirement for Task 1. This is the same resolution 37F-02 used for its config RED commit.
- **`nolint:staticcheck` on `projectArtifact`**: staticcheck's S1016 suggested collapsing the field-by-field construction to a type conversion (`SnapshotArtifact(a)`), since `ArtifactMeta` and `SnapshotArtifact` happen to share an identical field layout today. Declined via a documented `nolint`, because the explicit field list IS the SC3 allowlist contract — it must keep failing loudly (a compile error) the day someone adds a field to either struct that the other doesn't share, which a type conversion would silently paper over.
- **Dead-code removal + new pinning test** during the Task 3 mutation autopsy: see frontmatter `key-decisions` — `toolNames`' first early-return was truly redundant (not just untested), removed as a Rule-1 cleanup, and `TestSnapshotToolNamesNilWhenAllBlank` was added to pin the remaining guard's value-level guarantee (`!= nil`) since the wire-level `omitempty` check couldn't distinguish nil from a non-nil empty slice.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] RED-phase compiling stub for `snapshot.go`**
- **Found during:** Task 1 (RED commit attempt)
- **Issue:** The pre-commit `go vet`/lint hook rejects any commit where the package fails to compile — an unavoidable conflict with a literal "TDD RED = the package does not exist" commit, per the plan's own action text.
- **Fix:** Verified the literal RED state standalone first (`go test ./internal/share/` failed with `undefined: BuildSnapshot`/`ConvMeta`/etc., matching the plan's required grep pattern). Then added `snapshot.go` with its FINAL types and doc comments but a stub `BuildSnapshot` body (`return Snapshot{}, nil`), so every test still genuinely fails (real assertion/panic failures against an empty Snapshot) while the package compiles for the hook.
- **Files modified:** `internal/share/snapshot.go` (added, included in the Task 1 commit)
- **Verification:** `go build ./...`, `go vet ./internal/share/`, `golangci-lint run ./internal/share/...` all clean; `go test ./internal/share/` showed real test failures (assertion mismatches / an index-out-of-range panic), not a compile error.
- **Committed in:** `cd174fdc9`

**2. [Rule 1 - Bug/dead code] Removed a redundant early-return in `toolNames`, added a pinning assertion**
- **Found during:** Task 3 (mutation spot-check autopsy)
- **Issue:** `toolNames`' first `if len(calls) == 0 { return nil }` guard was unreachable-in-effect dead code: the trailing `if len(names) == 0 { return nil }` guard already covers the same case (an empty `calls` slice falls through to `make([]string, 0, 0)`, an empty loop, then the trailing guard). Mutating the first guard away produced a genuinely equivalent mutant (identical output for identical input) — a true positive finding that the code had redundant logic, not a leak.
- **Fix:** Removed the first guard (dead-code cleanup per CLAUDE.md refactor-on-touch). Added `TestSnapshotToolNamesNilWhenAllBlank`, asserting `ToolNames != nil` (not just `len==0`) for an all-blank-names input, so the remaining guard's nil-normalization behavior has a direct pinning assertion instead of relying solely on JSON `omitempty` (which cannot distinguish nil from a non-nil empty slice).
- **Files modified:** `internal/share/redact.go`, `internal/share/redact_test.go`
- **Verification:** Re-ran `go-mutesting` after the fix: score rose from 66.7% (6/9) to 87.5% (7/8, one fewer total mutant since the dead branch no longer exists to mutate); `go test ./internal/share/ -cover -count=1` → 100.0%.
- **Committed in:** `193ba508b`

---

**Total deviations:** 2 auto-fixed (1 blocking/Rule 3, 1 bug-dead-code/Rule 1)
**Impact on plan:** Both were necessary to reconcile the plan's literal TDD gate sequence with the repo's compile-clean pre-commit hook, and to close a genuine mutation-testing gap discovered during Task 3's own mandated autopsy. No scope creep — no new files beyond the plan's four, no behavior added beyond what `must_haves`/`<behavior>` specified.

## Known Stubs

None. `BuildSnapshot`'s temporary RED-phase stub body was fully replaced by Task 2's GREEN commit; no stub code remains in the current tree (verified: `internal/share/snapshot.go`'s `BuildSnapshot` composes `projectTurns`/`projectArtifact` as of commit `a36d803d3`).

## Threat Flags

None. Every surface this plan touches (`BuildSnapshot`, `projectTurns`, `toolNames`, `projectArtifact`) is exactly the mitigation surface named in the plan's own `<threat_model>` (T-37F-01, T-37F-18, T-37F-19, T-37F-20, T-37F-13, T-37F-21, T-37F-22, T-37F-23) — no new network endpoint, auth path, file access pattern, or schema change was introduced.

## Issues Encountered

- **Pre-commit hook vs. literal TDD RED commit** — see Deviation 1 above; resolved identically to the precedent set in 37F-02.
- **go-mutesting's tmp-folder diff output is misleading**: the tool's per-mutant diff sometimes prints against the NEXT mutant's temp file (an off-by-one in when it writes vs. reports), and `--do-not-remove-tmp-folder` retains files that, for at least one mutator, are byte-identical to the original (confirmed via `md5sum`, not by trusting the tool's diff view) — a true no-op mutation, not a diff-rendering bug. Cross-checked with `--verbose`/`--no-exec` runs before concluding the survivor was equivalent, to avoid mis-classifying a real gap as equivalent.

## User Setup Required

None - no external service configuration required. `internal/share` has zero external dependencies (stdlib + `internal/llm` only), no DB, no Garage, no network.

## Next Phase Readiness

- `internal/share.BuildSnapshot`/`Snapshot`/`SnapshotTurn`/`SnapshotArtifact` are ready for plan 37F-05 (the Markdown/JSON format adapters — both pure functions of `Snapshot`, per D-07) and plan 37F-06 (the service layer + public share page, which fetches `Snapshot` as JSON and renders the SAME struct client-side).
- The json tags are locked as the OQ4 wire contract; `TestSnapshotSchemaVersion` will catch any accidental tag rename before it silently breaks 37F-05's TypeScript mirror.
- No blockers. `internal/share` still imports neither `internal/conversations` nor `internal/agui` — the package stays free of both dependencies as designed.

---
*Phase: 37F-conversation-artifact-sharing-export-inserted*
*Completed: 2026-07-17*

## Self-Check: PASSED

All 4 created files (`internal/share/snapshot.go`, `redact.go`, `snapshot_test.go`,
`redact_test.go`) verified present on disk; all 3 task commit hashes
(`cd174fdc9`, `a36d803d3`, `193ba508b`) verified present in `git log --oneline --all`.
