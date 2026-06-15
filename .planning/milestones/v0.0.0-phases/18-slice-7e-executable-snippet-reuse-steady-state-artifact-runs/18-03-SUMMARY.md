---
phase: 18-slice-7e-executable-snippet-reuse-steady-state-artifact-runs
plan: 03
subsystem: skills
tags: [skills, snippet, lifecycle, restore, archive, save_snippet, d-02, ungated, tool-seam]

# Dependency graph
requires:
  - phase: 18 (Wave 1 / 18-01)
    provides: tool_invocations ledger + PRD amendment #55 (host-primary D-01)
  - phase: 18 (Wave 2 / 18-02)
    provides: host-primary action=use frame + skillLoader.Snippet hostPath seam
  - phase: 11 (Slice 7c/7e-core)
    provides: Writer (SaveSnippet/Archive/Activate/Materialize), the 0010 skill_audit ledger + D-29 coherence CHECK, the skill action-enum tool + skillWriter seam
provides:
  - skills.Writer.Restore — the inverse of Archive (promote archived->active + Materialize + SetUsageStatus active + audit-as-activate/cli)
  - tools.SkillTool.actionRestore / actionArchive / actionSaveSnippet — the three new action handlers (save UNGATED, D-02; restore/archive normal results)
  - tools.skillWriter seam widened with SaveSnippet/Restore/ArchiveSnippet; skillWriteArgs gains Language/Code (+needs_network/needs_workspace)
  - "save_snippet" in the skillParamsSchema action enum + a property-level "language" enum (python|shell|js) + a "code" field (root stays required=[action] only)
  - cmd/aura skillWriterAdapter.SaveSnippet/Restore/ArchiveSnippet (actor="model", cli ApprovalSource for restore/archive)
affects: [18-04, slice-7e]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Action-constant reuse to avoid a migration: restore audits as the EXISTING AuditActivate constant with the cli ApprovalCLI tuple (restore IS a re-activation) — the 0010 action CHECK has no 'restore' value and this phase is forbidden from adding a snippet migration (D-19), so NO AuditRestore constant is introduced"
    - "Ungated-action inverse of the gated writeAction: actionSaveSnippet copies writeAction's decode+require-writer+call shape but DROPS the *ErrAwaitingUserInput pause tail — only ask_user may pause (TestAskUserOnlyPauseConstraint); the save returns a NORMAL ToolResult (D-02, Claude-Code parity)"
    - "Lifecycle-handler seam discipline: tools<->skills boundary held (0 internal/skills deps in internal/agent/tools); the live *skills.Writer is adapted onto the consumer-declared skillWriter seam at the cmd/aura composition root"

key-files:
  created:
    - internal/skills/snippet_restore_integration_test.go
  modified:
    - internal/skills/writer_activate.go
    - internal/agent/tools/skill.go
    - internal/agent/tools/skill_write.go
    - internal/agent/tools/skill_test.go
    - internal/agent/tools/skill_write_test.go
    - cmd/aura/serve_adapters.go
    - scripts/check-file-size.sh

key-decisions:
  - "Restore audits as AuditActivate/ApprovalCLI (NOT a new AuditRestore action) so the 0010 action CHECK + the D-29 coherence CHECK accept it with NO new migration — restore is a re-activation, documented in a code comment so a future reader does not mistake it for a missing 'restore' action"
  - "actionSaveSnippet is the architectural inverse of writeAction: same decode+require-writer+call shape, but it returns a normal NewResult and NEVER the pause sentinel (D-02). The save still routes through Writer.SaveSnippet's validate -> injection-blocklist-on-CODE -> RISKY tier -> pending (never self-activates), so the model cannot bypass the save-time gate"
  - "ArchiveSnippet adapter uses the cli ApprovalSource (operator-source manual archive), distinct from the TTL sweep's auto source which records AuditAutoArchive — both ride the same Writer.Archive, the src argument selects the audit action"
  - "notYetWired removed (now unused): both reserved router keys (restore/archive) are wired, plus save_snippet added — the schema enum grows by one (save_snippet) and a property-level language enum is added; the root stays required=[action] only (no oneOf/anyOf, DeepSeek-wire-safe)"

patterns-established:
  - "Migration-avoidance via constant reuse: when a new lifecycle verb maps semantically onto an EXISTING audit action+tuple that the live CHECK already accepts, reuse the constant rather than ALTER the CHECK — document the mapping in a code comment"

requirements-completed: [CAP-08.1]

# Metrics
duration: ~45min
completed: 2026-06-06
---

# Phase 18 Plan 03: Snippet lifecycle + in-loop save (restore/archive/save_snippet) Summary

**Closed the snippet loop's two missing autonomous capabilities: the model can now SAVE a reusable executable snippet in-loop UNGATED (D-02 — a normal ToolResult, NEVER an ask_user pause), and the restore/archive lifecycle handlers replace the notYetWired stubs. `Writer.Restore` is the structural inverse of the shipped `Writer.Archive` (promote archived->active + re-Materialize + flip the sidecar to active + audit-as-activate/cli) — recorded with the EXISTING `AuditActivate` constant so NO snippet migration is needed; `archive` is a SAFE-tier manual de-materialize, recoverable via `restore`.**

## Performance

- **Duration:** ~45 min wall (incl. diagnosing + fixing a Windows-bash pre-commit hook bug)
- **Completed:** 2026-06-06
- **Tasks:** 2/2 (both TDD: RED -> GREEN)
- **Files:** 7 (1 created)

## Accomplishments

- **Writer.Restore** (`writer_activate.go`): the inverse of Archive — SanitizeName chokepoint, guard on `archiveDir`, best-effort hash of the archived tree, `promoteDir` archived->active, `Materialize` back into the export dir (so the loader + the D-01 host path see it again), `SetUsageStatus(name, "active")`, and `auditActivationLike(AuditActivate, ..., ApprovalCLI, ...)`. Errors clearly when `archiveDir` is unset or the archived snippet is absent.
- **db_integration round-trip** (`snippet_restore_integration_test.go`): Save+Activate -> Archive -> Restore round-trips the active dir + the export-dir materialization back, flips the sidecar status to "active", and writes a cli-tuple `activate` audit row. RAN green under the composed DSN (0.17s, not skipped).
- **actionSaveSnippet** (`skill_write.go`, D-02 UNGATED): decodes `{name,language,code,description}`, requires the writer, routes straight to `Writer.SaveSnippet` (which still validates + runs the injection blocklist on the CODE + lands pending — never self-activates), and returns a NORMAL `NewResult` confirming the pending save. It NEVER returns `*ErrAwaitingUserInput` — proven by `TestSnippetSaveAction` (`errors.As(err, &sentinel)` is false).
- **actionRestore / actionArchive** (`skill_write.go`): name-only handlers dispatching to `Writer.Restore` / `Writer.ArchiveSnippet` via the seam, returning normal results (restore re-materializes; archive is SAFE-tier, recoverable via restore).
- **Seam + schema** (`skill_write.go` + `skill.go`): the `skillWriter` interface gains `SaveSnippet/Restore/ArchiveSnippet`; `skillWriteArgs` gains `Language/Code` (+`needs_network/needs_workspace`). The router maps the three new keys (replacing `notYetWired` for restore/archive, which is removed). `save_snippet` joins the property-level action enum; a property-level `language` enum (python|shell|js) + a `code` field are added; the root stays `required=["action"]` only (no oneOf/anyOf/enum — DeepSeek-wire-safe).
- **Adapter** (`serve_adapters.go`): `skillWriterAdapter` gains `SaveSnippet/Restore/ArchiveSnippet` mirroring `WriteMutation`, labeling the actor `"model"` (attributable per T-18-08-S); Restore/Archive use the cli `ApprovalSource`.

## Task Commits

1. **fix: file-size hook here-string bug (blocking-issue, Rule 3)** - `3397c5ab`
2. **Task 1: Writer.Restore — the inverse of Archive (TDD)** - `041778bc` (feat)
3. **Task 2: wire restore/archive/save_snippet tool actions (TDD)** - `1b254c59` (feat)

## Decisions Made

- **Restore audits as `AuditActivate`/`ApprovalCLI`, not a new action.** The 0010 `action` CHECK has no `'restore'` value and this phase is forbidden from adding a snippet migration (D-19). A restore IS a re-activation, so the cli D-29 tuple (NULL token, gate_recommended=true, gate_taken=true) — already accepted by both the action CHECK and the coherence CHECK — is reused. A code comment documents this so a future reader does not mistake it for a missing `'restore'` action. No `AuditRestore` constant (it would fail the live CHECK).
- **`actionSaveSnippet` is the inverse of `writeAction`** (D-02): same decode+require-writer+call shape, but it returns a normal `NewResult` and NEVER the pause sentinel. The save still goes through `Writer.SaveSnippet`'s validate -> blocklist-on-CODE -> RISKY tier -> pending, so the model cannot bypass the save-time gate; a saved snippet still requires operator activation before `action=use` can run it.
- **`ArchiveSnippet` adapter uses the cli `ApprovalSource`** (operator-source manual archive, recorded as `AuditArchive`), distinct from the TTL sweep's `auto` source (recorded as `AuditAutoArchive`). Both ride the same `Writer.Archive`; the `src` argument selects the audit action.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking Issue] Fixed a Windows-bash pre-commit file-size hook bug**
- **Found during:** Task 1 commit (the very first commit of the run)
- **Issue:** `scripts/check-file-size.sh` iterated the tracked-Go-file list with a `<<< "$TARGETS"` here-string. On the Windows Git Bash (MSYS/busybox) shell that here-string mangled the LAST list entry into a truncated `"<tail>.go"` fragment (`internal/web/transport_test.go` -> a phantom `t.go` iteration), which `wc -l` could not open; under `set -e` that became a deterministic FALSE commit-blocking failure with ZERO real cap violations (verified: no tracked Go file exceeds 600 LOC, my files are 245 + ~124).
- **Fix:** Replaced the here-string with a process-substitution-fed loop (`done < <(printf '%s\n' "$TARGETS")`) so the loop still runs in the current shell (violations counter + exit code propagate) but the final-entry corruption is gone. Verified clean pass (exit 0) on the same shell, and the hook now passes for every subsequent commit.
- **Files modified:** `scripts/check-file-size.sh`
- **Commit:** `3397c5ab`
- **Scope note:** out of the plan's `files_modified` list, but a blocking-issue auto-fix (Rule 3) — without it no commit could land. Committed separately as a `fix(18-03)` so it is isolable.

### Expected (per plan)

**2. TestSkillDispatchErrors updated for the wired contract**
- The existing test asserted `action=restore` returned the `"not yet available"` placeholder. With restore/archive now WIRED, a loader-only tool (no writer) returns a clear `"no writer"` error instead. Updated the assertion to the new contract in the same commit (NOT a test fudged to pass — the placeholder no longer exists by design).

## Verification Evidence

- `go test ./internal/skills/ ./internal/agent/tools/` — green (skills 0.45s, tools cached/4.27s)
- `go test -tags db_integration -run 'TestRestoreSnippetRoundTrip|TestRestoreErrorsWhenArchiveDirUnset|TestSkillTTLSweep' ./internal/skills/` — **RAN green** under the composed DSN (Restore round-trip 0.17s; the existing TTL sweep still green = no regression in the shared Archive path)
- `BASH_ENV=~/.aura-toolchain.sh go test -race ./internal/skills/ ./internal/agent/tools/` — race-clean
- `go test ./internal/agent/ -run TestAskUserOnlyPauseConstraint` — green (the new save action does not trip the ask_user-only pause constraint)
- `go list -deps ./internal/agent/tools | grep -c internal/skills` — **0** (tools<->skills boundary held)
- `go build ./cmd/aura/` — exit 0 (adapters wired; builds alongside the untouched parallel-session `cmd/aura/main.go` + `toolpipe.go`)
- `golangci-lint run ./internal/skills/ ./internal/agent/tools/ ./cmd/aura/` — **0 issues**
- `go vet ./internal/agent/tools/ ./internal/skills/ ./cmd/aura/` — clean
- Schema discipline (`TestSkillSpecSchemaDiscipline`): root `required=["action"]` only, no root oneOf/anyOf/enum; `save_snippet` in the action enum; `language` carries a property-level enum
- All touched files <=600 LOC (writer_activate.go 245, skill.go 182, skill_write.go 249, serve_adapters.go 421)

## Threat Surface

No new security-relevant surface beyond the plan's `<threat_model>`.
- **T-18-07-E (ungated save, accept):** the model can stage a snippet with no ask_user approval (D-02), but `SaveSnippet` still runs `ValidateForWrite` (NFKC + injection blocklist on the CODE, `allowBlocklisted=false` hard-reject for the model) -> RISKY tier -> pending, and NEVER self-activates. A saved snippet still requires operator activation before `action=use`.
- **T-18-08-S (save audit actor, mitigate):** the adapter labels the actor `"model"` on the D-29 pending audit tuple (immutable ledger, 0010 triggers) — a model-authored save is attributable.
- **T-18-09-T (restore path traversal, mitigate):** `Restore` calls `SanitizeName` before any `filepath.Join`; `promoteDir`/`Materialize` reuse the shipped Lstat-no-follow symlink strip.
- **T-18-10-D (over-eager archive, mitigate):** `action=restore` is the inverse — an over-eager archive is recoverable.

## Known Stubs

None — the `notYetWired` stubs for restore/archive are REPLACED with real handlers wired to the live Writer; save_snippet routes to a real `Writer.SaveSnippet`. No placeholder/empty-value flows.

## Next Phase Readiness

- **18-04** (steady-state E2E gate) unblocked: the model can now both SAVE a snippet in-loop (D-02) and RUN it by-path via the 18-02 host frame, so the steady-state reuse the gate measures from the `tool_invocations` ledger (end-event count <=6 + wall <40s) can come into existence in-loop. The eval registry must register the SAME production skill tool (the action enum now carries save_snippet/restore/archive) — no eval-only seams.

## Self-Check: PASSED

- Created file verified present: `internal/skills/snippet_restore_integration_test.go`.
- Task commits verified in git log: `3397c5ab` (hook fix), `041778bc` (Task 1), `1b254c59` (Task 2).
