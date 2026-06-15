---
phase: 14-onboarding-agent-md
plan: 01
subsystem: profile
tags: [agent-md, profile-store, filesystem, cli, atomic-write]

requires:
  - phase: 13-telegram-channel
    provides: channel setup and identity seams consumed by later onboarding plans
provides:
  - DB-free filesystem profile Store for Agent.md, preferences.json, metadata.json, and changelog.md
  - Stable Agent.md render/parser helpers and idempotent fact insertion
  - AURA_PROFILE_DIR and AURA_PROFILE_CERTAINTY_N root config knobs
  - aura profile show/add-fact operator CLI
affects: [phase-14, phase-15-memory, telegram-onboarding, prompt-cache]

tech-stack:
  added: []
  patterns: [same-directory temp atomic write, typed profile sentinels, hand-rolled CLI dispatcher]

key-files:
  created:
    - internal/profile/store.go
    - internal/profile/render.go
    - internal/profile/parser.go
    - internal/profile/atomic_windows.go
    - internal/profile/atomic_posix.go
    - internal/profile/store_fact.go
    - internal/profile/store_test.go
    - internal/profile/render_test.go
    - internal/config/config_profile_test.go
    - cmd/aura/profile.go
    - cmd/aura/profile_test.go
  modified:
    - internal/config/config.go
    - internal/config/config_test.go
    - cmd/aura/main.go

key-decisions:
  - "Profile storage is filesystem-only under AURA_PROFILE_DIR/<identity>/ and imports no db or knowledge packages."
  - "Agent.md display uses an ASCII section tree so the CLI remains portable and test-stable."
  - "Malformed AURA_PROFILE_CERTAINTY_N follows local config style and falls back to the default instead of boot-failing."

patterns-established:
  - "Profile identity validation runs before filepath.Join and rejects traversal, separators, leading dots, and names outside the locked grammar."
  - "Profile file writes use same-directory temp files, file Sync, platform replace, cleanup on failure, and best-effort directory Sync."

requirements-completed: [UX-05]

duration: ~20 min
completed: 2026-06-11
---

# Phase 14 Plan 01: Profile Store and CLI Summary

**Disk-backed Agent.md profile store with atomic writes, render/parser helpers, config knobs, and the aura profile CLI**

## Performance

- **Duration:** ~20 min
- **Started:** 2026-06-11T10:35:46Z
- **Completed:** 2026-06-11T11:25:00Z
- **Tasks:** 4
- **Files modified:** 14

## Accomplishments

- Added `internal/profile.Store`, a DB-free filesystem store for `Agent.md`, `preferences.json`, `metadata.json`, and `changelog.md`.
- Added cross-platform atomic replacement: POSIX uses `os.Rename`; Windows uses `MoveFileEx` with replace and write-through.
- Added stable Agent.md rendering, section parsing, ASCII tree display, and idempotent fact insertion.
- Added `AURA_PROFILE_DIR` and `AURA_PROFILE_CERTAINTY_N` to root config with default/override tests.
- Added `aura profile show` and `aura profile add-fact`, both routed through `internal/profile.Store`.

## Task Commits

1. **Task 1: create internal/profile Store with atomic cross-platform writes** - `55887022` (included in concurrent Phase 20 commit; see deviation)
2. **Task 2: render and parse Agent.md sections** - `13677060`
3. **Task 3: add profile config knobs** - `c1eafc45`
4. **Task 4: implement aura profile show/add-fact CLI** - `3184fe61`

## Files Created/Modified

- `internal/profile/store.go` - Profile Store, typed errors, identity validation, atomic file writes, read/write reconstruction.
- `internal/profile/atomic_windows.go` - Windows `MoveFileEx` replacement with write-through.
- `internal/profile/atomic_posix.go` - POSIX atomic rename replacement.
- `internal/profile/render.go` - Stable Agent.md renderer and idempotent Facts insertion.
- `internal/profile/parser.go` - Agent.md section parser and ASCII tree formatter.
- `internal/profile/store_fact.go` - Store-level add-fact flow with changelog append.
- `internal/config/config.go` - Profile root and certainty threshold config fields.
- `cmd/aura/profile.go` - `aura profile show` and `aura profile add-fact`.
- `cmd/aura/main.go` - Top-level profile command dispatch.

## Decisions Made

- Kept the profile package pure filesystem/stdlib to preserve the Phase 14 boundary from DB and Neo4j memory work.
- Used ASCII tree output instead of Unicode drawing characters to keep CLI snapshots portable on Windows terminals.
- Allowed `add-fact` to create a missing profile with empty companion files plus a changelog entry, making the operator path useful before Telegram onboarding exists.

## Deviations from Plan

### Operational Deviations

**1. [GSD commit discipline] Task 1 files landed in a concurrent Phase 20 commit**
- **Found during:** Task 1 commit
- **Issue:** `HEAD` moved while the pre-commit hook was running; Git reported a ref lock race, and the Task 1 files were captured by concurrent commit `55887022`.
- **Fix:** Did not rewrite the other session's commit. Verified the profile files were present and clean, then continued with narrowly scoped commits for Tasks 2-4.
- **Files modified:** `internal/profile/store.go`, `internal/profile/atomic_windows.go`, `internal/profile/atomic_posix.go`, `internal/profile/store_test.go`
- **Verification:** `go test ./internal/profile/ -run 'Store|Atomic|Path' -count=1`
- **Committed in:** `55887022`

---

**Total deviations:** 1 operational deviation, 0 product-code deviations.
**Impact on plan:** Implementation scope and behavior remained correct; only the task-commit attribution for Task 1 is imperfect.

## Issues Encountered

- Two git races occurred while another workflow was committing Phase 20 artifacts. Both hook runs passed; the final profile CLI commit landed as `3184fe61`. Unrelated Phase 20 planning files were left staged and were not folded into this plan.

## Verification

- `go test ./internal/profile/ -run 'Store|Atomic|Path' -count=1`
- `go test ./internal/profile/ -run 'Render|Parse|Fact' -count=1`
- `go test ./internal/config/ -run 'Profile|Config' -count=1`
- `go test ./cmd/aura/ -run Profile -count=1`
- `go test ./internal/profile/ ./cmd/aura/ -count=1`
- `go test ./internal/profile/ ./internal/config/ ./cmd/aura/ -count=1`
- `go test -race ./internal/profile/ ./cmd/aura/ -run Profile`
- `go build ./...`

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

The filesystem profile substrate and CLI are ready for Plan 14-02 prompt injection. The next plan can load `Agent.md` from `AURA_PROFILE_DIR` and compose it into the existing protected `messages[1]` context block.

---
*Phase: 14-onboarding-agent-md*
*Completed: 2026-06-11*
