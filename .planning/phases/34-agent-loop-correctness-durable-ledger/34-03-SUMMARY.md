---
phase: 34-agent-loop-correctness-durable-ledger
plan: 03
subsystem: agent-tools
tags: [send_file, fs_write, fs_edit, atomic-write, file-mode, pgxpool, boot, resource-leak, composition-root]

# Dependency graph
requires:
  - phase: 33-runtime-profiles-config-validation
    provides: "cfg.Validate()/ValidateProfile profile-aware boot fail-fast (QUAL-04 deferred here as D-15b)"
provides:
  - "send_file outside-workspace deterministic reject (no dead ask_user/resume_context route)"
  - "fs_write/fs_edit mode-preserving atomic overwrite via existingFileMode helper"
  - "leak-free bootChatEnvWithConfig: resolveConfigAndPool + assembleChatEnv + releaseBootResources close-on-error guard"
  - "cmd/aura/chat.go boot restructure (injectable dbOpener, close-on-error) that 34-06 Wave 3 ResumeCommitter injection builds on"
affects: [34-06, agent-tools, cmd-aura-boot]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Deferred success-flag close-on-error guard for a multi-step composition root"
    - "Injectable dbOpener seam so pool lifecycle is unit-testable without a live Postgres (db.Open eagerly Pings)"
    - "existingFileMode(path) → preserve existing regular-file mode, else 0o644"

key-files:
  created:
    - "cmd/aura/chat_boot_test.go — PG-free boot error-path pool-close + closers-drain coverage"
  modified:
    - "internal/agent/tools/send_file.go — outsideWorkspaceResult rewritten to a terminal reject; dead approval route REMOVED"
    - "internal/agent/tools/send_file_test.go — assert reject excludes ask_user/resume_context, instructs workspace copy"
    - "internal/agent/tools/fs.go — new existingFileMode helper"
    - "internal/agent/tools/fs_write.go — mode-preserving atomic overwrite"
    - "internal/agent/tools/fs_edit.go — mode-preserving atomic overwrite"
    - "internal/agent/tools/fs_test.go — mode-preservation table + atomicity tests"
    - "cmd/aura/chat.go — bootChatEnvWithConfig split into resolveConfigAndPool/assembleChatEnv + releaseBootResources"

key-decisions:
  - "D-11: outside-workspace send_file is a deterministic outside_workspace_unsupported reject; the dead ask_user/resume_context route is deleted (no approval subsystem — Phase 35+)"
  - "D-12: preserve existing file mode on overwrite (new file 0o644); applied to fs_write (requirement) and fs_edit (deep-refactor-on-touch) via one shared helper"
  - "D-15b: real leak was the CommandHookManagerFromEnv failure path (returned after pool+MCP registry built, no cleanup); fixed with a deferred close-on-error guard covering every post-open error return"
  - "Kept BOTH cfg.Validate() calls (pre-open fail-fast before db.Open; post-overlay reloaded-config re-check) — the :163 overlay branch does NOT leak (A2), so no close added there"

patterns-established:
  - "close-on-error: success flag + deferred releaseBootResources(pool, closers) drains MCP closers then closes the pool"
  - "test seam: injectable dbOpener + non-pinging pgxpool.New pool makes boot error paths a fast PG-free unit tier"

requirements-completed: [LOOP-06, LOOP-07, QUAL-04]

# Metrics
duration: 45min
completed: 2026-07-01
---

# Phase 34 Plan 03: send_file reject + fs mode preservation + boot pool-leak Summary

**Killed the send_file infinite-ask footgun (deterministic reject), stopped fs_write/fs_edit from clobbering a 0o600/0o755 file's mode on overwrite, and closed a boot-time pgxpool + MCP-subprocess leak on the CommandHookManagerFromEnv failure path.**

## Performance

- **Duration:** ~45 min
- **Completed:** 2026-07-01
- **Tasks:** 3 (Task 2 is TDD → RED+GREEN)
- **Files modified:** 7 (1 created)

## Accomplishments
- **LOOP-06 / F-009:** `outsideWorkspaceResult` now returns a terminal `outside_workspace_unsupported` error instructing the model to copy the file into the workspace; the advertised `ask_user`/`resume_context={"type":"send_file_outside_workspace"}` route (which no resume hook consumed → infinite ask loop) is fully removed. `grep -rn 'send_file_outside_workspace' internal/` returns nothing.
- **LOOP-07 / F-010:** `fs_write` and `fs_edit` stat the target and pass its existing mode to `atomicWriteFile` (new file → 0o644) via a shared `existingFileMode` helper, so an overwrite no longer silently downgrades a restrictive mode. Atomicity (temp+rename, never truncate) was already shipped and is now regression-tested.
- **QUAL-04b / D-15b:** `bootChatEnvWithConfig` split into `resolveConfigAndPool` (config resolve + pool open) and `assembleChatEnv` (composition root on the open pool). `assembleChatEnv` arms a deferred `success`-flag close-on-error guard (`releaseBootResources`), so the CommandHookManagerFromEnv failure path — and every other post-open error return — now closes the pool and drains `mcpClosers` instead of leaking them.

## Task Commits

1. **Task 1: send_file outside-workspace deterministic reject** — `ccb302d5` (fix)
2. **Task 2 (RED): failing mode/atomicity tests** — `9f1ef521` (test)
3. **Task 2 (GREEN): fs_write/fs_edit mode preservation** — `b4365c39` (feat)
4. **Task 3: boot pool-leak close-on-error + tests** — `d5b0bda2` (fix)

## Files Created/Modified
- `internal/agent/tools/send_file.go` — `outsideWorkspaceResult` → terminal `outside_workspace_unsupported` reject; dead route removed
- `internal/agent/tools/send_file_test.go` — reject asserts no `ask_user`/`resume_context`, instructs workspace copy
- `internal/agent/tools/fs.go` — `existingFileMode` helper (preserve existing regular-file mode, else 0o644)
- `internal/agent/tools/fs_write.go` / `fs_edit.go` — pass `existingFileMode(path)` to `atomicWriteFile`
- `internal/agent/tools/fs_test.go` — mode-preservation table (0o600/0o755/new→0o644) for both tools + atomicity-on-failure test (POSIX/non-root guarded)
- `cmd/aura/chat.go` — `resolveConfigAndPool` + `assembleChatEnv` + `releaseBootResources`; both `cfg.Validate()` kept with rationale
- `cmd/aura/chat_boot_test.go` (new) — PG-free unit coverage of the release primitive + pool-close on command-hook/reload/final-Validate failures + fail-fast-before-db.Open

## Decisions Made
- Followed D-11/D-12/D-15b exactly. The `send_file` dead route was deleted rather than wired to an approval subsystem (option A / Phase 35+). Mode preservation applied to both `fs_write` and `fs_edit` (deep-refactor-on-touch), since — per RESEARCH — `fs_edit` also hardcoded 0o644 and was NOT a pre-existing mode-preservation template.
- Confirmed the dead `send_file_outside_workspace` route had ZERO consumers across the tree (grep of `resume_context` hooks / `serve_adapters.go` chain), so the removal breaks no caller.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Fixed a latent nil-pool shadow in the settings-overlay boot branch**
- **Found during:** Task 3 (boot refactor)
- **Issue:** The inline `bootChatEnvWithConfig` declared the overlay pool with `pool, ok, overlayErr := openSettingsOverlayPool(ctx)` INSIDE the keyless `if err != nil` block. In a nested block `:=` creates a NEW `pool`, shadowing the outer `var pool`, so an overlay-SUCCESS boot fell through to the rest of the function with a nil outer pool (nil-panic on the first store use). Latent because it only triggers when the LLM key is absent from env but present in the DB settings overlay with a reachable DB.
- **Fix:** `resolveConfigAndPool` returns the overlay pool directly (`return cfg, pool, nil`), so `assembleChatEnv` receives the live pool. The shadow is gone.
- **Files modified:** cmd/aura/chat.go
- **Verification:** `go build ./...` + `go vet ./cmd/aura/` clean; `go test -race ./cmd/aura/` green (incl. the pre-existing keyless boot tests `TestChatBootStillRequiresAPIKey` / `TestServeKeylessBootReachesInfraValidation`, which prove the refactor preserves the boot contract).
- **Committed in:** d5b0bda2 (Task 3 commit)

**Structural note (not a deviation):** The plan's D-15b explicitly permits restructuring "while preserving the fail-fast-before-db.Open guard." The split into `resolveConfigAndPool` / `assembleChatEnv` + an injectable `dbOpener` is that permitted restructure — it makes the close-on-error paths a fast, PG-free unit tier (`db.Open` eagerly Pings, so the concrete boot cannot be driven past pool-open without a live Postgres). Both `cfg.Validate()` calls are kept.

---

**Total deviations:** 1 auto-fixed (1 bug).
**Impact on plan:** The bug fix is a free correctness win from the D-15b-sanctioned refactor. No scope creep; no new subsystem; no migration.

## Known Stubs
None — all three fixes are terminal behavior changes with no placeholder/empty-data paths.

## Issues Encountered
- The acceptance grep `grep -rn 'send_file_outside_workspace' internal/` must return 0. My first draft kept the literal string in an explanatory comment and in the test's banned-list. Reworded the comment and dropped the literal from the banned list (asserting `ask_user`/`resume_context` absence already proves the dead route is gone) so the grep is clean.
- mcpClosers-drain on the real CommandHookManagerFromEnv path is not directly observable in a unit test (no MCP servers → empty closers). The drain contract is proven by `TestBootReleaseResourcesDrainsClosersAndClosesPool` (spy closers); the full-path tests observe pool-close, which routes through the same `releaseBootResources` call.

## User Setup Required
None - no external service configuration required.

## Next Phase Readiness
- Wave 1 tools/boot fixes complete and race-clean. `cmd/aura/chat.go` now exposes `resolveConfigAndPool`/`assembleChatEnv` as the composition-root seam that **34-06 (Wave 3)** builds on to inject the `ResumeCommitter` (pool-owning cross-store tx) at boot.
- No blockers.

## Self-Check: PASSED

- Files exist: send_file.go, fs.go, fs_write.go, fs_edit.go, fs_test.go, send_file_test.go, cmd/aura/chat.go, cmd/aura/chat_boot_test.go — all present.
- Commits exist: ccb302d5, 9f1ef521, b4365c39, d5b0bda2 — all in git history.
- Gates: `grep -rn 'send_file_outside_workspace' internal/` → none; `go vet ./internal/agent/tools/ ./cmd/aura/` clean; `go test -race ./internal/agent/tools/ ./cmd/aura/` green; every touched file < 600 LOC (max chat.go 594).

---
*Phase: 34-agent-loop-correctness-durable-ledger*
*Completed: 2026-07-01*
