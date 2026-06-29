---
phase: 28-governance-boards-web-onboarding
plan: 04
subsystem: auth
tags: [prd-amendment, capability_grants, authula, identity_auth_links, multi-user, roadmap, governance]

# Dependency graph
requires:
  - phase: 24-web-foundation-serve-auth-health
    provides: GAP-2 web-auth boundary + RequireAuth/RequireCapability + capability_grants principal seam
  - phase: cockpit-overhaul (Authula)
    provides: embedded Authula provider, OperatorUserID single-user guard, identity_auth_links (migration 0019, 1:N-ready)
provides:
  - "PRD-amendment #64 (prd.md §Slice 1.7): single-operator boundary relaxed for web-loginable identities; capability_grants stays the only authz model; identity.create gate introduced (parity with agent.run)"
  - "PROJECT.md §Out of Scope + ONBOARD-* row relaxed to the 2nd-web-loginable-identity / capability_grants-only posture"
  - "Phase 30 absorbed into Phase 28 (D-09): ROADMAP targeted edits on the 3 Phase-30 touch points + 30-SPEC tombstone -> 28-SPEC §ONBD-01b"
  - "webauth.OperatorUserID >1-user case made non-fatal via ErrOperatorAmbiguous sentinel (enrollment-time skip; live path resolves 1:N via ResolveIdentityID over identity_auth_links)"
  - "Live-proven no-regression: single-operator unchanged; 2 enrolled users do not break boot and resolve correctly"
affects: [28-05 onboarding provisioning saga, 28-06 onboarding wizard, Plan 05 identity.create gate]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "PRD-first gate: BLOCKING amendment commit lands before any provisioning code (git-log ordering is the gate)"
    - "Typed SKIP sentinel (ErrOperatorAmbiguous) instead of a fatal error for an enrollment-time ambiguity the live path does not depend on"
    - "ROADMAP phase-absorption via TARGETED edits (gsd roadmap tooling cannot express an absorption marker/tombstone)"

key-files:
  created:
    - internal/webauth/authula_multiuser_test.go
  modified:
    - .planning/PROJECT.md
    - prd.md
    - .planning/ROADMAP.md
    - .planning/phases/30-telegram-onboarding-on-frontend-with-link-and-qr-code/30-SPEC.md
    - docs/cockpit-overhaul/05-authula-auth-SPEC.md
    - internal/webauth/authula.go
    - cmd/aura/serve_auth.go

key-decisions:
  - "Used prd.md amendment #64 (verified #61/#62/#63 already taken; #64 was the next free number)"
  - "OperatorUserID >1-user returns a typed ErrOperatorAmbiguous SKIP sentinel (recommendation a), NOT 'return the first/oldest user' (b) — GetAll(nil,2) ordering is not guaranteed, and the live path never depends on OperatorUserID, so guessing an operator would be unsafe and unnecessary"
  - "All 3 ROADMAP Phase-30 touch points edited via targeted Edit; gsd roadmap tooling subcommands (analyze/get-phase/update-plan-progress/annotate-dependencies/validate/upgrade) cannot express an 'absorbed' status, and `phase complete`/`phase remove` would lose the absorption pointer + traceability D-09 requires"
  - "30-SPEC converted to a tombstone banner that PRESERVES the original 7 requirements verbatim below it (traceability), rather than deleting the body"

patterns-established:
  - "Phase-absorption tombstone: top-of-file banner -> authoritative spec pointer, original requirements retained for traceability"

requirements-completed: [ONBD-01]

# Metrics
duration: 27min
completed: 2026-06-20
status: complete
---

# Phase 28 Plan 04: PRD-Amendment (Single-Operator Relaxation) + Phase-30 Absorption + OperatorUserID Guard Relaxation Summary

**BLOCKING PRD-amendment #64 relaxing the single-operator boundary for web-loginable identities (capability_grants stays the only authz model, identity.create gate introduced), Phase 30 absorbed into Phase 28 via targeted ROADMAP edits + 30-SPEC tombstone, and webauth.OperatorUserID's >1-user case made non-fatal via an ErrOperatorAmbiguous SKIP sentinel — live-proven no-regression.**

## Performance

- **Duration:** ~27 min
- **Started:** 2026-06-20T09:00:59Z
- **Completed:** 2026-06-20T09:27:49Z
- **Tasks:** 2
- **Files modified:** 8 (5 docs/PRD + 2 code + 1 new test)

## Accomplishments
- Landed the PRD-first gate (CLAUDE.md, absolute) BEFORE any Phase-28 provisioning code: PROJECT.md + prd.md amendment #64 relaxing the single-operator boundary, authz explicitly STAYING capability_grants-only (NO RBAC / route-scoping / OAuth / per-identity session isolation), introducing the `identity.create` create-mutation capability (parity with `agent.run`).
- Absorbed Phase 30 into Phase 28 (D-09): the three ROADMAP Phase-30 touch points (phase-list bullet, `### Phase 30:` detail section, Progress-table row) marked `absorbed-into-28` with a pointer to `28-SPEC §ONBD-01b`; `30-SPEC.md` converted to a tombstone (original 7 requirements retained for traceability).
- Made `webauth.OperatorUserID`'s >1-user case non-fatal via a new `ErrOperatorAmbiguous` SKIP sentinel; the enrollment auto-pin is skipped (not aborted), the existing `local` link is never de-pinned, and the live session-validate path resolves identity 1:N via `ResolveIdentityID` over `aura.identity_auth_links` — verified that no live request handler calls `OperatorUserID`.
- Added `internal/webauth/authula_multiuser_test.go` (db_integration) proving live, against the running stack: (1) 2 enrolled Authula users → `ErrOperatorAmbiguous` (skip, not fatal) + `local` stays resolvable + a 2nd identity resolves via its own link row; (2) single-operator unchanged.

## Task Commits

1. **Task 1: PRD-amendment — relax single-operator (D-07) + absorb Phase 30 (D-09)** — `f73c47db` (docs)
2. **Task 2: Relax the OperatorUserID >1-user guard (enrollment-time only)** — landed in `aaefbad1` (see "Issues Encountered" — the parallel Codex session swept the uncommitted Task-2 files into its own commit via `git add -A` before this executor committed them; the code is verbatim what was authored + tested green)

_No separate plan-metadata commit yet at the time of writing — this SUMMARY + STATE/ROADMAP updates are the metadata commit that follows._

## Files Created/Modified

**Task 1 (committed in `f73c47db`):**
- `.planning/PROJECT.md` — §Out of Scope "Multi-user con auth/RBAC reale" bullet relaxed to the 2nd-web-loginable-identity / capability_grants-only posture; §Active ONBOARD-* row notes full provisioning of a loginable identity gated by `identity.create`.
- `prd.md` — PRD-amendment **#64** blockquote at the top of §Slice 1.7 (names Phase 28, capability_grants-only, the `identity.create` gate, and the `OperatorUserID` >1-user supersession by 1:N resolution through `identity_auth_links`).
- `.planning/ROADMAP.md` — 3 targeted Phase-30 edits: line-56 bullet → `✅ absorbed-into-28` + pointer; `### Phase 30:` detail section → tombstone Goal pointing at `28-SPEC §ONBD-01b`; Progress-table row → `Absorbed into 28`. Rest of file byte-unchanged (anti-pattern #15 respected).
- `.planning/phases/30-telegram-onboarding-on-frontend-with-link-and-qr-code/30-SPEC.md` — tombstone banner → `28-SPEC §ONBD-01b`; original 7 requirements retained for traceability.
- `docs/cockpit-overhaul/05-authula-auth-SPEC.md` — OQ-8 (multi-user) marked ⚠️ PARTIALLY RESOLVED for the capability_grants-only path, pointing at amendment #64.

**Task 2 (committed in `aaefbad1`):**
- `internal/webauth/authula.go` — new `ErrOperatorAmbiguous` sentinel; `OperatorUserID` doc-comment rewritten to the enrollment-time / multi-user-via-identity_auth_links posture; >1-user case returns the SKIP sentinel instead of a fatal error.
- `cmd/aura/serve_auth.go` — enrollment block converted to a `switch` that explicitly recognizes `ErrOperatorAmbiguous` (skip auto-pin, accurate log line); doc-comment updated; `errors` import added.
- `internal/webauth/authula_multiuser_test.go` — NEW db_integration test (`TestAuthulaMultiUser_DoesNotBreakBoot` + `TestAuthulaMultiUser_SingleOperatorUnregressed`) with a self-contained `multiEnvOrSkip` helper (no-skip-as-green) and a throwaway-identity fixture for the 1:N link assertion.

## Verification (live)

All toolchain commands run inside WSL (native .exe is AV-killed on this host), against the already-up Docker stack, serialized with `-p 1` (shared Postgres + parallel Codex session).

- `go build ./...` + `go vet ./...` — **clean** (untagged + `-tags db_integration` + `-tags webauth_integration` + combined tags; no symbol clash across tiers).
- Plan verify command — `go test -tags db_integration ./internal/webauth/ -run 'TestOperatorUserID|TestAuthulaMultiUser|TestIdentityLink' -count=1 -p 1 -v`:
  - `TestAuthulaMultiUser_DoesNotBreakBoot` — **PASS** (Authula migrations applied live; the per-test plugin-init logs confirm real execution, not a skip).
  - `TestAuthulaMultiUser_SingleOperatorUnregressed` — **PASS**.
  - `TestIdentityLinkerNilPool` — **PASS**.
  - `ok github.com/chetto1983/aura/internal/webauth` (exit 0).
- No-regression — `go test -race ./internal/webauth/` (untagged): **all PASS** including `TestProviderCloseNilSafe` (nil-provider `OperatorUserID`) and `TestValidate_*` (live-path `ResolveIdentityID`).
- Task-1 grep gate — `grep -q "absorbed-into-28" .planning/ROADMAP.md && grep -q "Phase 28" prd.md && grep -riq "absorbed" .../30-SPEC.md`: **all PASS**.
- Task-2 grep gate — Grep tool confirms the ONLY non-test caller of `OperatorUserID` is `cmd/aura/serve_auth.go:142` (the enrollment block); no live request handler calls it.

_Note: `AURA_AUTHULA_SECRET` is empty in `.env`; the test runner injected a deterministic 32-char test secret (the Authula schema is isolated; the secret only governs HMAC/token derivation for the test session). `POSTGRES_PASSWORD` (single-quoted in `.env`) sourced correctly (len 12)._

## Decisions Made
- **Amendment number #64** — verified #61/#62/#63 are already used in prd.md (Phase 15 memory + Phase 17 packaging + Aura Plugins); #64 was the next free number (an earlier truncated grep had wrongly suggested #61).
- **ErrOperatorAmbiguous (skip) over "return first/oldest user"** — RESEARCH §Hard Problem 2 offered (a) keep OperatorUserID enrollment-only or (b) return the seeded-operator id. Chose (a) with a typed sentinel: `UserService.GetAll(nil, 2)` ordering is not guaranteed, and the live path resolves via `ResolveIdentityID` (never `OperatorUserID`), so guessing "the" operator from an unordered set would be both unsafe and unnecessary. The sentinel makes the enrollment-time intent explicit and testable.
- **ROADMAP via targeted edits (tooling fallback)** — see "Deviations".

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Phase-30 directory path in the plan was wrong**
- **Found during:** Task 1 (read_first / 30-SPEC tombstone).
- **Issue:** The plan's `files_modified` + RESEARCH referenced `.planning/phases/30-telegram-onboarding-link-qr/30-SPEC.md`, which does not exist. The real directory is `.planning/phases/30-telegram-onboarding-on-frontend-with-link-and-qr-code/`.
- **Fix:** Tombstoned the file at the real path; used the real path in all ROADMAP pointers and the SUMMARY.
- **Verification:** `grep -riq absorbed` on the real 30-SPEC passes; file exists.
- **Committed in:** `f73c47db`.

**2. [Rule 3 - Blocking] gsd roadmap tooling cannot express the Phase-30 absorption**
- **Found during:** Task 1 (ROADMAP edits).
- **Issue:** ROADMAP_EDIT_DISCIPLINE prefers gsd tooling for the structural Phase-30 status changes. The available subcommands are `roadmap {analyze,get-phase,update-plan-progress,annotate-dependencies,validate,upgrade}` and `phase {uat-passed,next-decimal,add,add-batch,insert,remove,complete}` — none can express an "absorbed-into-28" status, a one-line pointer, or a free-form section tombstone. `phase complete` mislabels (Phase 30 is absorbed, not done) and `phase remove` would destroy the absorption pointer + traceability D-09 requires. `roadmap update-plan-progress` only recomputes plan counts from disk and would actively overwrite the absorption status text.
- **Fix:** Used TARGETED `Edit` calls for all 3 Phase-30 touch points (never a full-file Write); confirmed via `git diff -U0` that only the Phase-30 line ranges (56, 276-283, 298) changed — the rest of ROADMAP is byte-unchanged.
- **Verification:** `git diff --stat` = 6 insertions / 6 deletions confined to the Phase-30 hunks; `grep "absorbed-into-28"` passes.
- **Committed in:** `f73c47db`.

**3. [Rule 1 - Bug] Test fixture FK violation (my own test)**
- **Found during:** Task 2 (first live test run).
- **Issue:** `TestAuthulaMultiUser_DoesNotBreakBoot` linked the "second" Authula user to a synthetic identity UUID that had no `aura.identities` row → `identity_auth_links_identity_id_fkey` violation (SQLSTATE 23503). Bug was in the test fixture, not in the code under test.
- **Fix:** Insert a throwaway `aura.identities` row (kind `user`, `ON CONFLICT DO NOTHING`) before the link, with cleanup (link rows cascade via the migration-0019 FK; identity deleted last). This plan does not create identities (that is Plan 05) — the test only proves the link-table mechanics generalize 1:N.
- **Verification:** Re-run → all 3 tests PASS (exit 0).
- **Committed in:** `aaefbad1` (the test file content).

---

**Total deviations:** 3 auto-fixed (2 blocking, 1 bug). **Impact:** No scope creep — path/tooling corrections + a test-fixture fix. The code-under-test behavior matched the plan on the first run; only the test fixture needed a correction.

## Issues Encountered

**A. Task-2 production code was swept into a parallel Codex commit (`aaefbad1`).**
A parallel Codex session shares this working tree (sequential mode, main tree, no worktree isolation). After Task 2's edits were on disk and verified green, the Codex session ran `git add -A` / `git commit -a` ("feat: scope graph memory by identity", `aaefbad1`) which swept in my three uncommitted Task-2 files (`authula.go`, `serve_auth.go`, `authula_multiuser_test.go`) **verbatim** (diff confirmed identical to what was authored + tested). Consequence: Task 2's code is committed and live-green, but under the wrong commit message/scope and without this executor's Co-Authored-By trailer. Per the destructive-git-prohibition + multi-active-session rules, I did NOT rewrite/extract `aaefbad1` (it also contains Codex's legitimate graph-memory work; rewriting a shared commit would destroy concurrent work). Outcome: code integrity is intact (verified green at HEAD); the attribution anomaly is recorded here. The atomic-close-out invariant holds (production code committed before SUMMARY).

**B. Pre-commit `file-size` hook tripped on external Codex churn (Task 1).**
The lefthook `file-size` check (`scripts/check-file-size.sh`) scans ALL tracked Go/TS files via `git ls-files`, and tripped on the parallel session's then-uncommitted `internal/agent/mcptools/bridge_test.go` (634 LOC > 600) — a file outside this plan's scope that the guardrail forbids touching. Task 1 is docs-only (5 `.md` files, none in the Go/TS file-size scope), so it cannot introduce a LOC-cap violation. Committed Task 1 with `--no-verify` (the documented external-churn exception), recorded in the commit body. `vet` passed. Task 2's code later landed via the Codex commit (Issue A), so this executor did not re-hit the hook for Task 2.

**C. WSL env-sourcing footgun (host shell pre-expands `$VAR`).**
`wsl bash -lc '... $VAR ...'` had the Windows host (Git Bash) pre-expand `$VAR` to empty before WSL ran. Fixed by writing the run/test logic to a `.sh` script (`D:/tmp/run_webauth_test.sh`) and executing it with `MSYS_NO_PATHCONV=1 wsl bash /mnt/d/tmp/...` (the `/mnt/...` path also needed `MSYS_NO_PATHCONV=1` to avoid MSYS path mangling).

## User Setup Required
None - no external service configuration required.

## Next Phase Readiness
- **The PRD-first gate is satisfied** — the BLOCKING single-operator relaxation (capability_grants-only) + Phase-30 absorption are committed on master BEFORE the provisioning saga. Plan 05 (onboarding provisioning saga) is unblocked.
- `identity.create` is now the documented create-mutation capability name Plan 05's `RequireCapability` mount must check.
- `OperatorUserID` tolerates >1 user (live-proven); the 1:N `identity_auth_links` resolution path is ready for the 2nd provisioned identity.
- **Carry-forward for the verifier:** Task-2 code is committed inside `aaefbad1` (Codex), not a dedicated `feat(28-04)` commit (Issue A) — verify Task-2 acceptance against HEAD, not against a commit-message scope filter.

## Self-Check: PASSED

- All 8 declared files + the SUMMARY exist on disk (verified with `[ -f ]`).
- Task-1 commit `f73c47db` exists; Task-2 code exists in commit `aaefbad1` (see Issue A).
- Plan automated verifications re-run green (Task-1 grep gate; Task-2 `db_integration` tests + untagged `-race`, exit 0).

---
*Phase: 28-governance-boards-web-onboarding*
*Completed: 2026-06-20*
