---
phase: 37F-conversation-artifact-sharing-export-inserted
plan: 06
subsystem: api
tags: [markdown, json, redaction, property-based-testing, rapid, share, go]

# Dependency graph
requires:
  - phase: 37F-03
    provides: "internal/share.Snapshot/SnapshotTurn/SnapshotArtifact + BuildSnapshot — the canonical redacted snapshot both adapters serialize, and nothing else"
provides:
  - "internal/share.Snapshot.JSON() — the D-07 lossless JSON export adapter, a pure method on Snapshot"
  - "internal/share.Snapshot.Markdown() — the D-07 human-readable export adapter (title + per-turn headings + tool provenance + fenced text + trailing artifacts section), fence-injection-safe"
  - "internal/share/share_property_test.go — 3 of the phase's property tests (redaction totality, redaction idempotence, JSON round-trip) over rapid-generated hostile histories, with an anti-vacuity guard"
affects: [37F-05, 37F-10, 37F-11, share-file-export, public-share-page]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Format adapter as a zero-extra-parameter method on the canonical redacted type (func (s Snapshot) X()) — the D-07 one-core guarantee holds by construction, grep-gated (neither adapter file references the raw message type)"
    - "RED-phase compiling stub for a TDD RED commit under a whole-module go-vet/build pre-commit gate: a deliberately-wrong non-panicking body (an explicit error / sentinel string), not a panic, so the full test run still completes and reports every genuine failure — same resolution as 37F-03, refined here to avoid the panic variant aborting the rest of the suite"
    - "rapid (pgregory.net/rapid, already vendored — used by internal/swarm, internal/gateway, internal/agent/workflow, etc.) for property-based tests, following the repo's existing rapid.Check(t, func(rt *rapid.T){...}) idiom rather than a hand-rolled seeded math/rand loop"
    - "Anti-vacuity guard in a generative property test: force the generator to guarantee BOTH a secret-bearing draw AND a draw that survives the production allowlist, so an empty result set can never make the property pass trivially"

key-files:
  created:
    - internal/share/jsonfmt.go
    - internal/share/markdown.go
    - internal/share/format_test.go
    - internal/share/share_property_test.go
  modified: []

key-decisions:
  - "RED-phase stubs used a deliberately-wrong non-panicking body (an explicit unimplemented error for JSON(), a fixed sentinel string for Markdown()/longestBacktickRun) rather than a panic: a panic in one subtest aborted the entire test binary before later subtests could run and report their own genuine failures, which is a materially worse RED signal than a clean, complete failing run. Verified via a live run (Task 2 RED) that the panic variant does in fact truncate the report, then switched to the sentinel-value form for both tasks' stubs."
  - "Task 1's TestSnapshotJSONRoundTrip and Task 3's TestPropertyJSONRoundTrip both build CreatedAt/SnapshotAt/now via time.Date(...) or time.Now().UTC().Truncate(time.Second), never a bare time.Now(): time.Now() carries a monotonic clock reading that json.Marshal/Unmarshal always strips, so comparing a round-tripped value against an un-truncated time.Now() value via reflect.DeepEqual would fail for a reason unrelated to the adapter under test. Verified independently with a small throwaway Go program before writing the tests, confirming Go's json.Unmarshal produces a non-nil-but-empty slice (not nil) for an empty JSON array `[]`, so a Snapshot with zero turns/artifacts also round-trips cleanly through reflect.DeepEqual without special-casing."
  - "Task 3 uses pgregory.net/rapid (already a go.mod dependency, used by 6+ existing _property_test.go files in this repo) rather than a hand-rolled seeded math/rand loop — the plan's own guidance said to prefer an already-vendored property library, and go.mod/go.sum diff is verified empty. rapid.Check(t, ...) runs 100 generated cases per property by default and prints a deterministic -rapid.seed on failure for exact reproduction, satisfying the plan's determinism requirement without any custom seed-plumbing code."
  - "The property generator (genHostileHistory) forces its fallback draw to kindAssistantTool specifically (not any other tool-bearing kind) because that single kind is simultaneously an assistant-role turn (survives projectTurns' user/assistant allowlist) and a tool-call turn (carries a secret in Arguments) — this was NOT the first design: an early version only guarded 'at least one secret exists' and rapid caught, on its first real run, a generated all-tool/system-role history that planted a secret but produced a zero-turn Snapshot, which would have let the totality property pass vacuously. Fixed by adding a second guard (at least one surviving turn) resolved by the same forced draw."
  - "markdown.go hardcodes the two role-heading strings (\"user\"/\"assistant\") rather than importing internal/llm's RoleUser/RoleAssistant constants, specifically so the file carries zero references to the llm package — the plan's acceptance criteria greps markdown.go/jsonfmt.go for the literal substring \"llm.\" and requires no match; a first draft imported internal/llm for the constants and had to be corrected before the GREEN commit."

requirements-completed: [WEBSHARE-01, WEBSHARE-03]

coverage:
  - id: D1
    description: "Snapshot.JSON() — a pure method on Snapshot (no other parameter) wrapping encoding/json.Marshal, round-tripping losslessly, omitting empty tool_names, and deterministic across calls"
    requirement: "WEBSHARE-01"
    verification:
      - kind: unit
        ref: "internal/share/format_test.go#TestSnapshotJSONRoundTrip"
        status: pass
      - kind: unit
        ref: "internal/share/format_test.go#TestSnapshotJSONOmitsEmptyToolNames"
        status: pass
      - kind: unit
        ref: "internal/share/format_test.go#TestSnapshotJSONDeterministic"
        status: pass
    human_judgment: false
  - id: D2
    description: "Snapshot.Markdown() — a pure method on Snapshot rendering a title heading, per-turn role headings, tool-provenance lines, fenced turn text (fence-injection-safe against embedded backtick runs), and a trailing artifacts section; agrees with JSON() on turn count/roles/tool names"
    requirement: "WEBSHARE-01"
    verification:
      - kind: unit
        ref: "internal/share/format_test.go#TestSnapshotMarkdownStructure"
        status: pass
      - kind: unit
        ref: "internal/share/format_test.go#TestSnapshotMarkdownToolProvenance"
        status: pass
      - kind: unit
        ref: "internal/share/format_test.go#TestSnapshotMarkdownArtifactSection"
        status: pass
      - kind: unit
        ref: "internal/share/format_test.go#TestSnapshotMarkdownFenceInjection"
        status: pass
      - kind: unit
        ref: "internal/share/format_test.go#TestSnapshotMarkdownEmpty"
        status: pass
      - kind: unit
        ref: "internal/share/format_test.go#TestSnapshotFormatsAgree"
        status: pass
    human_judgment: false
  - id: D3
    description: "Property suite over rapid-generated hostile histories: redaction totality (SC3 as a universal, against both Markdown() and JSON()) with an anti-vacuity guard, redaction idempotence, and JSON round-trip"
    requirement: "WEBSHARE-03"
    verification:
      - kind: unit
        ref: "internal/share/share_property_test.go#TestPropertyRedactionTotality (100 generated cases)"
        status: pass
      - kind: unit
        ref: "internal/share/share_property_test.go#TestPropertyRedactionIdempotent (100 generated cases)"
        status: pass
      - kind: unit
        ref: "internal/share/share_property_test.go#TestPropertyJSONRoundTrip (100 generated cases)"
        status: pass
    human_judgment: false

duration: 22min
completed: 2026-07-17
status: complete
---

# Phase 37F Plan 06: Markdown + JSON Format Adapters Over the Canonical Snapshot Summary

**`Snapshot.Markdown()` and `Snapshot.JSON()` — both zero-extra-parameter methods on the D-07 canonical redacted snapshot, fence-injection-safe and lossless-round-trip respectively, proven against `[]llm.Message`-reachable secrets via a rapid property suite with an anti-vacuity guard**

## Performance

- **Duration:** ~22 min
- **Started:** 2026-07-17T13:23Z (first RED commit)
- **Completed:** 2026-07-17T13:45Z (property suite commit)
- **Tasks:** 3 planned, all completed as specified (2 TDD RED→GREEN pairs + 1 test-only property suite)
- **Files modified:** 4 (all new)

## Accomplishments

- `internal/share.Snapshot.JSON() ([]byte, error)` — wraps `encoding/json.Marshal` with a `%w`-wrapped error; round-trips losslessly (`reflect.DeepEqual` after unmarshal), omits `tool_names` when empty (asserted on the raw marshalled string, not the unmarshalled struct, so the test actually exercises `omitempty`), and is deterministic across calls
- `internal/share.Snapshot.Markdown() []byte` — renders a `# {Title}` heading, one `## N. {Role}` section per turn in input order, a `_Tools used: ..._` provenance line only on tool-calling turns, the turn text inside a backtick fence, and a trailing `## Artifacts` section listing filename + size (omitted when empty)
- **Fence-injection safety**: `fenceForText` scans the turn's own text for the longest backtick run and emits a fence strictly longer than that run (3-backtick floor) — a turn whose content itself contains a triple-backtick (or longer) run cannot terminate the emitted fence early; both cases are asserted, and the full original text is confirmed to survive verbatim
- **D-07 one-core agreement**: `TestSnapshotFormatsAgree` parses both `Markdown()` and `JSON()` back and asserts identical turn count, roles in the same order, and identical tool names per turn — the machine-checkable form of "both formats derive from the same Snapshot"
- Neither `markdown.go` nor `jsonfmt.go` references the `internal/llm` package at all (grep-gated, `llm\.` returns nothing in either file) — `markdown.go` hardcodes its own `"user"`/`"assistant"` role-heading strings rather than importing `llm.RoleUser`/`llm.RoleAssistant`, precisely so the adapter cannot even syntactically reach the raw message type
- A `pgregory.net/rapid` property suite (`share_property_test.go`, no new dependency — `rapid` was already vendored and used by 6+ existing `_property_test.go` files in this repo) proving three of the phase's four owned properties over generated hostile histories, each running rapid's default 100 cases:
  - `TestPropertyRedactionTotality` — the SC3 universal: for every generated history and every secret drawn from a realistic corpus (a `send_file`-shaped path argument, a `shell_exec`-shaped `/etc/passwd` result, an `$AURA_RUN_DIR` sidecar path template, a permission-denied error carrying a path) and planted into it, the secret is absent from BOTH `Markdown()` and `JSON()`
  - `TestPropertyRedactionIdempotent` — `BuildSnapshot` applied to an already-projected turn set is a fixpoint
  - `TestPropertyJSONRoundTrip` — every generated `Snapshot` round-trips losslessly through `JSON()`/`json.Unmarshal`
  - **Anti-vacuity guard, caught live by rapid on this generator's first real run**: an early version of the guard only checked "at least one secret was planted," which let a generated history built entirely from `role=tool`/`role=system` messages plant a secret (in the dropped tool-result content) while yielding a **zero-turn** `Snapshot` — a totality property over an empty snapshot passes vacuously. Fixed by requiring the generator to guarantee BOTH a secret-bearing draw AND a draw that survives `projectTurns`' allowlist, resolved by a single forced `kindAssistantTool` fallback (which is simultaneously both).

## Task Commits

Each task was committed atomically as its own RED/GREEN pair (task 3 is test-only, since the adapters it validates were already implemented in tasks 1-2):

1. **Task 1 RED: failing JSON round-trip tests for the share snapshot** - `a94e75fae` (test)
2. **Task 1 GREEN: implement the share snapshot JSON adapter** - `8e682afb8` (feat)
3. **Task 2 RED: failing Markdown adapter tests incl. fence injection** - `8a2c30e2f` (test)
4. **Task 2 GREEN: implement the share snapshot Markdown adapter** - `605ae67cf` (feat)
5. **Task 3: property suite for redaction totality + round-trip** - `258ee85ba` (test)

**Plan metadata:** (this commit, docs: complete plan)

_Note: both RED commits ship a compiling, deliberately-wrong stub (an explicit "not implemented" error for `JSON()`, a fixed sentinel string for `Markdown()`/`longestBacktickRun`) rather than a literal non-compiling state, because the repo's pre-commit hook runs `go vet ./...` (whole-module) and rejects a non-compiling commit — the same resolution 37F-03 used. Task 2's stub deliberately avoids a `panic()`-based stub (Task 1 used a returned error, which is safe; an initial draft of Task 2's stub used `panic()` and was corrected after a live run showed the panic aborted the test binary before later subtests could report their own genuine failures)._

## Files Created/Modified

- `internal/share/jsonfmt.go` - `func (s Snapshot) JSON() ([]byte, error)`, ~20 LOC
- `internal/share/markdown.go` - `func (s Snapshot) Markdown() []byte` + `mdRoleHeading`/`longestBacktickRun`/`fenceForText` helpers, ~130 LOC
- `internal/share/format_test.go` - JSON adapter tests (task 1) + Markdown adapter tests + `TestSnapshotFormatsAgree` (task 2), ~330 LOC
- `internal/share/share_property_test.go` - the rapid property suite (task 3), ~255 LOC

## Decisions Made

See frontmatter `key-decisions` for full detail. In short:
- RED-phase stubs use a returned error / sentinel value, never `panic()`, so a RED run always completes and reports every genuine failure.
- Round-trip tests build times via `time.Date(...)`/`time.Now().UTC().Truncate(time.Second)`, never bare `time.Now()`, to avoid an unrelated monotonic-clock `reflect.DeepEqual` mismatch.
- `pgregory.net/rapid` (already vendored) is the property-testing tool, per the plan's own preference for an already-present dependency over a hand-rolled seeded loop.
- The property generator's anti-vacuity guard covers both "a secret was planted" and "a turn survives the allowlist" — the second half was added only after rapid caught the gap live.
- `markdown.go` hardcodes role strings instead of importing `internal/llm`, to satisfy the plan's grep-gated "no `llm.` reference in either adapter file" acceptance criterion literally (a doc-comment mention of `llm.Message` was also caught and reworded, since the grep is a literal substring match against the whole file, comments included).

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Panic-based RED stub replaced with a non-panicking sentinel**
- **Found during:** Task 2 (RED commit attempt)
- **Issue:** An initial `markdown.go` RED-phase stub used `panic(errors.New(...))` for both `Markdown()` and `longestBacktickRun`. A live `go test -v` run showed the panic in the first failing subtest (`TestSnapshotMarkdownStructure`) aborted the entire test binary (`FAIL` + goroutine trace + process exit) before any of the other 5 Markdown test cases could execute and report their own genuine failures — a materially weaker RED signal than a clean run showing all 6 failures.
- **Fix:** Replaced the stub bodies with a returned sentinel: `Markdown()` returns a fixed `"MARKDOWN_NOT_IMPLEMENTED"` byte slice, `longestBacktickRun` returns `-1`. Re-ran and confirmed all 6 Markdown cases (plus `TestSnapshotFormatsAgree`) now report individually, each on a real assertion failure against the sentinel output, with `go build ./...` and `go vet` both clean.
- **Files modified:** `internal/share/markdown.go` (the RED-commit version, before the GREEN rewrite)
- **Verification:** `go test ./internal/share/ -run 'TestSnapshotMarkdown|TestSnapshotFormatsAgree' -count=1 -v` showed 6 independent `--- FAIL` blocks, no panic trace, `go build ./...`/`go vet` clean.
- **Committed in:** `8a2c30e2f`

**2. [Rule 1 - Bug] Removed an accidental `internal/llm` reference from markdown.go**
- **Found during:** Task 2 (GREEN implementation, before commit)
- **Issue:** The first GREEN draft of `mdRoleHeading` imported `internal/llm` and switched on `llm.RoleUser`/`llm.RoleAssistant` rather than the raw strings — violating the plan's acceptance criterion that `grep -n "llm\." internal/share/markdown.go` return nothing (the criterion is a literal substring check across the whole file, including doc comments; a subsequent doc-comment mention of "`llm.Message`" was caught by the same grep and reworded too).
- **Fix:** Hardcoded `"user"`/`"assistant"` directly in the `switch`, removed the `internal/llm` import, and reworded the doc comment to describe "the raw conversation history type" without using the literal substring `llm.`.
- **Files modified:** `internal/share/markdown.go`
- **Verification:** `grep -n "llm\." internal/share/markdown.go` returns nothing; `go build ./...`, `go vet ./internal/share/`, `golangci-lint run ./internal/share/...` (0 issues), and the full `go test ./internal/share/ -count=1`/`-race` suite all pass.
- **Committed in:** `605ae67cf`

**3. [Rule 1 - Bug] Anti-vacuity guard extended after a live rapid failure**
- **Found during:** Task 3 (first `go test -run TestProperty` run, before commit)
- **Issue:** `genHostileHistory`'s original guard only forced a secret-bearing draw when NO `kindAssistantTool`/`kindToolResult` was present. `rapid` found, on an early run (seed reported: `-rapid.seed=15174906586202490824`), a 2-message history drawn as `[kindToolResult, kindSystem]`: a secret WAS planted (satisfying the old guard), but both messages are dropped by `projectTurns`' user/assistant allowlist, yielding a **zero-turn** `Snapshot` — `TestPropertyRedactionTotality`'s own anti-vacuity assertion (`len(snap.Turns) == 0`) correctly caught this and failed the test, but it revealed the generator itself was under-constrained, not a production bug.
- **Fix:** Added a second guard (`needsSurvivor`, true when no `kindUser`/`kindAssistantPlain`/`kindAssistantTool` is present) alongside the original (`needsSecretBearer`), both resolved by the same forced fallback (`kindAssistantTool`, which is simultaneously a secret-bearer and a turn that survives the allowlist).
- **Files modified:** `internal/share/share_property_test.go`
- **Verification:** Re-ran `go test ./internal/share/ -run 'TestProperty' -count=1 -v`: all 3 properties pass at 100 generated cases each; `go test -race`/`-cover` (96.7%) both clean.
- **Committed in:** `258ee85ba` (this fix was part of the initial authoring of the file, landed in its single commit — no separate follow-up commit was needed since the bug was caught before the file was ever staged)

---

**Total deviations:** 3 auto-fixed (2 Rule 1/bug during authoring before commit, 1 Rule 1/bug caught live by the property test itself before commit)
**Impact on plan:** All three were caught and fixed DURING task execution, before any commit — none required amending an already-committed task. No scope creep: no new files beyond the plan's four, no behavior added beyond `must_haves`/`<behavior>`.

## Known Stubs

None remaining in the committed tree. Both RED-phase stubs (`jsonfmt.go`'s `errors.New("share: JSON not implemented")`, `markdown.go`'s `"MARKDOWN_NOT_IMPLEMENTED"` sentinel + `longestBacktickRun` returning `-1`) were fully replaced by their respective GREEN commits (`8e682afb8`, `605ae67cf`); verified by reading the current `internal/share/jsonfmt.go` and `internal/share/markdown.go` on disk.

## Threat Flags

None. Every surface this plan touches (`Snapshot.JSON()`, `Snapshot.Markdown()`, the property suite) is exactly the mitigation surface named in the plan's own `<threat_model>` (T-37F-33, T-37F-01, T-37F-34, T-37F-35) — no new network endpoint, auth path, file access pattern, or schema change was introduced. Both adapters remain grep-verified free of any `internal/llm` reference.

## Issues Encountered

- **Pre-commit `go vet ./...` vs. literal TDD RED commits** — resolved identically to 37F-03's precedent (a compiling stub, not a literal non-compiling commit); see Deviations 1 above for the refinement (sentinel-value stubs instead of a panic-based one).
- **`rapid`'s `testdata/rapid/.../*.fail` artifact directory** — the failed run during Task 3's authoring (before the anti-vacuity fix) left a `testdata/rapid/TestPropertyRedactionTotality/*.fail` file on disk; removed before staging so it never reached a commit (confirmed via `git status --short internal/share/` showing only the intended file before `git add`).

## User Setup Required

None - no external service configuration required. `internal/share` remains dependency-free beyond stdlib + `internal/llm` (production code) + `pgregory.net/rapid` (test-only, already vendored) — `git diff go.mod go.sum` verified empty at every commit in this plan.

## Next Phase Readiness

- `Snapshot.JSON()`/`Snapshot.Markdown()` are ready for the file-export HTTP route (WEBSHARE-01) and for plan 37F-05's TypeScript mirror to diff its wire shape against.
- The property suite's redaction-totality proof (against generated histories, not just the 37F-03 fixture) strengthens the SC3 evidence available to `/gsd-verify-work` and any future audit.
- `internal/share` package coverage after this plan: 96.7% (`go test ./internal/share/ -cover -count=1`), well above the 85% floor; `go test -race ./internal/share/ -count=1` clean; `golangci-lint run ./internal/share/...` reports 0 issues.
- No blockers. Token opacity/uniqueness and key-namespace-disjointness properties (the remaining 2 of the phase's 6 named properties) belong to plans 37F-04 (already shipped, per git log) and 37F-07 respectively — not this plan's scope.

---
*Phase: 37F-conversation-artifact-sharing-export-inserted*
*Completed: 2026-07-17*

## Self-Check: PASSED

All 4 created files (`internal/share/jsonfmt.go`, `markdown.go`, `format_test.go`,
`share_property_test.go`) verified present on disk; all 5 task commit hashes
(`a94e75fae`, `8e682afb8`, `8a2c30e2f`, `605ae67cf`, `258ee85ba`) verified present
in `git log --oneline --all`.
