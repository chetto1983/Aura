---
phase: 37F-conversation-artifact-sharing-export-inserted
plan: 11
subsystem: share-lifecycle
tags: [postgres, objectstore, cron-sweep, garbage-collection, D-15, R-10, rapid]

# Dependency graph
requires:
  - phase: 37F-07
    provides: "the lazy fail-closed liveness predicate in ResolveByToken/ResolveLiveByID, DueForExpiry, RevokeForIdentity, RevokeForConversation, share_expiry_sweep in the scheduler kind CHECK (migration 0040)"
  - phase: 37F-08
    provides: "dropBlobs/dropSnapshotBlobs, Service.Revoke's drop-blobs-then-stamp ordering, bundle.go's copy-never-reference discipline"
provides:
  - "Service.ExpireDue — the batch expiry sweep target: drops Garage bytes for every due link, THEN stamps the row, THEN audits expire, idempotent + resumable"
  - "handlers.KindShareExpirySweep / handlers.ShareExpirer / handlers.NewShareExpiryHandler — the cron sweep handler, a copy-and-rename shell over the shared newCountingSweep, nil-safe, no reverse import"
  - "Service.RevokeConversationShares — the D-15 conversation-delete cascade target: revokes every live share for a conversation (each drop-blobs-then-stamped via the existing Revoke), best-effort per link"
  - "runner.ShareRevoker — the consumer-declared seam + Runner.DeleteConversationLifecycle step 4.5, WARN-and-continue on failure, nil-safe"
affects: [37F-12, 37F-13, 37F-17, 37F-18, 37F-19]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Counting-sweep copy-and-rename: a new scheduler kind is a ~20-40 LOC file over the shared newCountingSweep shell (sweep.go), never a bespoke handler struct"
    - "Consumer-declared seam as a nilable Deps-injected struct field (mirrors Gateway/HookManager/ResumeCommitter), not a registry type-assertion scan, when the concrete producer is a service rather than a tool"
    - "Fault-injection via a thin objectstore.Store-wrapping decorator (embed *FakeStore, override one method) to prove an ordering/crash-safety property under an ACTUAL failure, not just by code inspection"
    - "rapid-driven range properties for a monotonicity claim, reusing the package's existing pgregory.net/rapid dependency rather than a hand-picked fixed value"

key-files:
  created:
    - internal/share/expirer.go
    - internal/share/cascade.go
    - internal/cron/handlers/share_expiry.go
    - internal/cron/handlers/share_expiry_test.go
    - internal/share/expirer_integration_test.go
    - internal/runner/runner_delete_share_test.go
  modified:
    - internal/runner/runner_delete.go
    - internal/runner/runner.go

key-decisions:
  - "Service.RevokeConversationShares loops the conversation's live links through the already-tested single-link Revoke (drop-blobs-then-stamp) rather than the pre-existing bulk aura.shared_links.RevokeForConversation store primitive, because the bulk primitive stamps all rows in one UPDATE before the caller can drop any blobs — that ordering would violate the plan's explicit never-stamp-before-drop prohibition on a crash between the stamp and the drop."
  - "The runner's ShareRevoker seam is a nilable Deps/Runner struct field (mirroring Gateway/HookManager/ResumeCommitter), not a tools.Registry type-assertion scan like SessionJobTerminator — a share revoker is a service dependency, not a registered Tool, so there is no natural registry to scan."
  - "TestShareExpirySweep/TestShareExpirySweepIdempotent were placed in internal/share (driving Service.ExpireDue directly) rather than internal/cron/handlers, per the plan's own explicit discretion clause — internal/cron/handlers must never import internal/share, and the handler itself is already unit-proved as a copy-and-rename shell in share_expiry_test.go."
  - "TestPropertyExpiryMonotonicity uses pgregory.net/rapid (already a project dependency, already used elsewhere in this exact package) drawing a random past-expiry offset from 1 second to 2 years per check, rather than a hand-picked fixed value or a new property-testing dependency."

requirements-completed: [WEBSHARE-02, WEBSHARE-03]

coverage:
  - id: D1
    description: "Share expiry sweep: Service.ExpireDue drops Garage bytes then stamps then audits, idempotent; handlers.NewShareExpiryHandler wires it to the share_expiry_sweep scheduler kind, nil-safe"
    requirement: WEBSHARE-03
    verification:
      - kind: unit
        ref: "internal/cron/handlers/share_expiry_test.go#TestShareExpiryMeta"
        status: pass
      - kind: unit
        ref: "internal/cron/handlers/share_expiry_test.go#TestShareExpiryRunExpires"
        status: pass
      - kind: unit
        ref: "internal/cron/handlers/share_expiry_test.go#TestShareExpiryDisabled"
        status: pass
      - kind: unit
        ref: "internal/cron/handlers/share_expiry_test.go#TestShareExpiryRunError"
        status: pass
      - kind: integration
        ref: "internal/share/expirer_integration_test.go#TestShareExpirySweep"
        status: pass
      - kind: integration
        ref: "internal/share/expirer_integration_test.go#TestShareExpirySweepIdempotent"
        status: pass
      - kind: integration
        ref: "internal/share/expirer_integration_test.go#TestShareExpiryBlobDropFailureLeavesRowUnstamped"
        status: pass
    human_judgment: false
  - id: D2
    description: "Revoke-on-conversation-delete cascade: runner_delete.go step 4.5 drives a consumer-declared ShareRevoker seam (satisfied by Service.RevokeConversationShares) before the persistence delete, best-effort WARN-and-continue, nil-safe"
    requirement: WEBSHARE-02
    verification:
      - kind: integration
        ref: "internal/runner/runner_delete_share_test.go#TestDeleteLifecycleRevokesShares"
        status: pass
      - kind: integration
        ref: "internal/runner/runner_delete_share_test.go#TestDeleteLifecycleShareRevokerNil"
        status: pass
      - kind: integration
        ref: "internal/runner/runner_delete_share_test.go#TestDeleteLifecycleShareRevokeFailureDoesNotBlock"
        status: pass
      - kind: integration
        ref: "internal/share/expirer_integration_test.go#TestRevokeConversationShares"
        status: pass
      - kind: integration
        ref: "internal/share/expirer_integration_test.go#TestRevokeConversationSharesNoShares"
        status: pass
      - kind: integration
        ref: "internal/share/expirer_integration_test.go#TestRevokeConversationSharesPerLinkFailureContinues"
        status: pass
    human_judgment: false
  - id: D3
    description: "Expiry monotonicity: once ResolveByToken 404s a link for expiry at time t, it 404s for every t' > t — no clock-skew resurrection"
    verification:
      - kind: integration
        ref: "internal/share/expirer_integration_test.go#TestPropertyExpiryMonotonicity"
        status: pass
    human_judgment: false

duration: ~70min
completed: 2026-07-17
status: complete
---

# Phase 37F Plan 11: Share Lifecycle Garbage Collection Summary

**Batch expiry sweep (`Service.ExpireDue` + `share_expiry_sweep` cron handler) and a revoke-on-conversation-delete cascade (`runner_delete.go` step 4.5) that both drop Garage bytes strictly before stamping/deleting the row, closing the R-10 orphaned-blob gap with a fault-injection-proven crash-safety ordering.**

## Performance

- **Duration:** ~70 min
- **Completed:** 2026-07-17
- **Tasks:** 3
- **Files modified:** 8 (6 created, 2 modified)

## Accomplishments

- `internal/share/expirer.go`: `Service.ExpireDue` — drops every due link's Garage bytes, then stamps `revoked_at` via the existing `RevokeForIdentity`, then audits `expire`; idempotent (a re-run over an already-swept link is a no-op) and batch-limited (200/tick).
- `internal/cron/handlers/share_expiry.go` + `share_expiry_test.go`: `KindShareExpirySweep` (`"share_expiry_sweep"`, matches migration 0040's kind CHECK), the `ShareExpirer` consumer-declared seam, and `NewShareExpiryHandler` — a ~20-LOC copy-and-rename of `identity_purge.go` over the shared `newCountingSweep` shell. A nil expirer is a disabled no-op, never a panic; `ReschedulesOnRecovery` is `false` (the sweep is idempotent).
- `internal/share/cascade.go` (new — see Deviations): `Service.RevokeConversationShares` revokes every still-live share for a conversation by looping the already-tested single-link `Revoke` (drop-blobs-then-stamp), joining any per-link failures rather than abandoning the batch on the first one.
- `internal/runner/runner_delete.go`: the `ShareRevoker` consumer-declared seam + step 4.5, inserted between "terminate bg jobs" and "delete persistence." Best-effort like step 2 (WARN, never a blocked delete), with the R-10 inversion stated explicitly in the comment (the row cascades, the bytes do not). A nil seam is a silent skip.
- `internal/runner/runner.go` (new — see Deviations): wires `Deps.ShareRevoker` / `Runner.shareRevoker`, mirroring the existing `Gateway`/`HookManager` nilable-field injection pattern.
- Integration tests proving the D-15/OQ3 lifecycle live, including a fault-injection test (`TestShareExpiryBlobDropFailureLeavesRowUnstamped`) that forces the object-store `List` call to fail and asserts the row is still NOT stamped — the drop-before-stamp crash-safety argument proven under an actual failure, not only by code inspection — and an ordering test (`TestDeleteLifecycleRevokesShares`) that records firing order rather than checking only end state, so a stamp-then-delete implementation could not pass it.

## Task Commits

Each task was committed atomically:

1. **Task 1: share.ExpireDue + the cron sweep handler** - `d5eae07c` (feat)
2. **Task 2: runner_delete.go step 4.5 — revoke shares + drop blobs before the persistence delete** - `39184e60` (feat)
3. **Task 3: lifecycle integration tests — sweep, cascade, monotonicity** - `c806386b` (test)

**Plan metadata:** (this commit, immediately following)

## Files Created/Modified

- `internal/share/expirer.go` - `Service.ExpireDue`, the batch expiry sweep target
- `internal/cron/handlers/share_expiry.go` - `share_expiry_sweep` scheduler kind + handler
- `internal/cron/handlers/share_expiry_test.go` - handler unit tests (Meta/Run/Disabled/Error)
- `internal/share/cascade.go` - `Service.RevokeConversationShares`, the D-15 cascade target
- `internal/runner/runner_delete.go` - `ShareRevoker` seam + step 4.5
- `internal/runner/runner.go` - `Deps.ShareRevoker` / `Runner.shareRevoker` wiring
- `internal/share/expirer_integration_test.go` - sweep + cascade + monotonicity integration tests
- `internal/runner/runner_delete_share_test.go` - D-15 cascade integration tests

## Decisions Made

See `key-decisions` in the frontmatter: (1) the cascade loops the already-tested single-link `Revoke` rather than the bulk `RevokeForConversation` store primitive, to honor the never-stamp-before-drop ordering; (2) the runner seam is a nilable Deps field, not a registry type-assertion scan, since a share revoker is a service dependency, not a Tool; (3) the sweep integration tests live in `internal/share` (driving `Service.ExpireDue` directly), per the plan's own discretion clause; (4) the monotonicity property reuses the package's existing `pgregory.net/rapid` dependency.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 2/3 - Missing critical / blocking] Added `internal/share/cascade.go` (not in the plan's `files_modified`)**
- **Found during:** Task 2 (runner_delete.go step 4.5)
- **Issue:** The plan's own design requires a "live `*share.Service`" satisfying the runner's `ShareRevoker` seam via a method that revokes every live share for a conversation and drops their bytes. No such method existed on `Service` before this plan — the pre-existing `store.RevokeForConversation` (added in 37F-07) only stamps rows in one bulk `UPDATE...RETURNING`, which would orphan bytes on a crash between the stamp and a caller-side blob drop, directly violating the plan's explicit "MUST NOT stamp the row before dropping the blobs" prohibition. Without a correctly-ordered Service-level method, step 4.5 could not be genuinely implemented — only stubbed.
- **Fix:** Added `Service.RevokeConversationShares`, which lists a conversation's shares and revokes each live one through the existing, already-tested `Service.Revoke` (which is already correctly drop-blobs-then-stamp ordered and audited). Per-link failures are joined (`errors.Join`) rather than aborting the batch.
- **Files modified:** `internal/share/cascade.go` (new)
- **Verification:** `TestRevokeConversationShares`, `TestRevokeConversationSharesNoShares`, `TestRevokeConversationSharesPerLinkFailureContinues` (all pass); `internal/share` package coverage 85.3% (≥85% floor) under `db_integration`.
- **Committed in:** `39184e60` (Task 2 commit)

**2. [Rule 2/3 - Missing critical / blocking] Modified `internal/runner/runner.go` (not in the plan's `files_modified`)**
- **Found during:** Task 2 (runner_delete.go step 4.5)
- **Issue:** The `ShareRevoker` seam needs somewhere to live on the `Runner` so `DeleteConversationLifecycle` can read it — the plan's own read_first explicitly models the seam after `Gateway`/`HookManager`/`ResumeCommitter`, all of which are `Deps`-injected `Runner` struct fields declared in `runner.go`, not `runner_delete.go`. The struct type itself cannot be extended from a different file's method set alone.
- **Fix:** Added `Deps.ShareRevoker ShareRevoker` and `Runner.shareRevoker ShareRevoker`, wired through in `New()`, exactly mirroring the existing `Gateway` field's nilable-injection shape.
- **Files modified:** `internal/runner/runner.go`
- **Verification:** `go build ./...` clean; full pre-existing `internal/runner` suite (untagged + `-race`) unregressed; `runner.go` stays at 589/600 LOC (file-size gate passes).
- **Committed in:** `39184e60` (Task 2 commit)

---

**Total deviations:** 2 auto-fixed (both Rule 2/3 — missing critical functionality required to make the plan's stated design actually work, not stubs). **Impact on plan:** Both additions are the concrete infrastructure the plan's own text describes as already existing ("satisfied by the live `*share.Service`") but that had not yet been built by a prior plan. No scope creep — both are strictly the minimum needed to make Tasks 2 and 3 real rather than mocked.

## Issues Encountered

- **`internal/share` package coverage was initially 82.9%** (below the 85% floor) because `cascade.go`'s `RevokeConversationShares` had 0% coverage from within the `internal/share` test suite itself (it was only exercised indirectly through `internal/runner`'s cross-package integration test, which does not count toward `internal/share`'s own coverage number). Resolved by adding three direct tests (`TestRevokeConversationShares`, `TestRevokeConversationSharesNoShares`, `TestRevokeConversationSharesPerLinkFailureContinues`) plus a fault-injection test for `ExpireDue`'s drop-before-stamp ordering (`TestShareExpiryBlobDropFailureLeavesRowUnstamped`), raising the package to 85.3%.
- **`go test -race` requires CGO, which is not enabled in the default Windows Git Bash shell used for this session.** Resolved by running the `-race` suites via WSL Ubuntu (which has `go1.26.5` + `gcc 15.2.0` + `CGO_ENABLED=1` already provisioned) against the same Windows-hosted Postgres container (`127.0.0.1:5432`, reachable from WSL2). All three touched packages passed `-race` both untagged and under `-tags db_integration -p 1`.
- **`gofmt` reformatted the step-4.5 doc-list entry** (`// 4.5. revoke shares ...`) because Go's doc-comment list formatter does not recognize an `N.M` numbered-list marker and re-indents it as a continuation of item 4. Accepted gofmt's own formatting rather than fighting it (CLAUDE.md treats gofmt as a hard gate); the "4.5" substring and its meaning are unaffected, only the indentation.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

- Bytes now actually go away on both lifecycle paths this phase's D-15/R-10 threat register named: the expiry sweep (garbage collection, bounded 5-minute tick) and the conversation-delete cascade (best-effort, order-proven before the persistence delete).
- `handlers.KindShareExpirySweep` is defined and unit-proved but **not yet seeded into the live scheduler** — a composition-root task (seeding the `share_expiry_sweep` task row + wiring `NewShareExpiryHandler(realShareService)` into the dispatcher, and wiring `Deps.ShareRevoker` at the runner's construction site) remains for a later plan, consistent with 37F-12/13's own scope (HTTP route mounting) rather than this plan's `files_modified`.
- No blockers for 37F-12 (HTTP route mounting) or the remaining 37F plans (17/18/19) — none of this plan's changes touch the HTTP surface, capability gating, or the migration numbering floor (still `0041_shared_links_rls`).

---
*Phase: 37F-conversation-artifact-sharing-export-inserted*
*Completed: 2026-07-17*

## Self-Check: PASSED

- FOUND: internal/share/expirer.go
- FOUND: internal/cron/handlers/share_expiry.go
- FOUND: internal/cron/handlers/share_expiry_test.go
- FOUND: internal/share/cascade.go
- FOUND: internal/share/expirer_integration_test.go
- FOUND: internal/runner/runner_delete_share_test.go
- FOUND commit: d5eae07c (Task 1)
- FOUND commit: 39184e60 (Task 2)
- FOUND commit: c806386b (Task 3)
- Re-ran plan-level `<verification>` block: `go build ./...` clean; `go vet` clean (both build tags); untagged suite green (`internal/share` 0.71s / `internal/runner` 1.65s / `internal/cron/handlers` 1.14s); `-tags db_integration -race -p 1 -count=1` green via WSL (`internal/share` 6.68s / `internal/runner` 2.84s / `internal/cron/handlers` 1.15s — all non-sub-second, no skip-as-green); `go list -deps ./internal/cron/handlers/ ./internal/runner/ | grep aura/internal/share$` → no match; `golangci-lint run` → 0 issues; `bash scripts/check-file-size.sh` → all files within the 600-LOC cap.
- Coverage under `db_integration`: `internal/share` 85.3%, `internal/runner` 92.4%, `internal/cron/handlers` 87.5% — all ≥85% floor.
