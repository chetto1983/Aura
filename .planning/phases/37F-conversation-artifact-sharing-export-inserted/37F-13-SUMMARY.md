---
phase: 37F-conversation-artifact-sharing-export-inserted
plan: 13
subsystem: security
tags: [share, capability-grants, cross-identity, sc4, coverage-gate, webshare-04]

# Dependency graph
requires:
  - phase: 37F-conversation-artifact-sharing-export-inserted (plans 37F-11, 37F-12)
    provides: the share.Service lifecycle (expiry sweep, revoke-on-delete), and the parent-mux
      mount for the eight share routes + the /s/ public-route allowlist predicate
provides:
  - "TestShareCrossIdentityDeny — the WEBSHARE-04 SC4 ten-row cross-identity deny acceptance
    vehicle, internal/agui, single db_integration tag"
  - "handleShareCreate's public-tier mint now enforces the share.public capability (closes the
    gap plan 37F-12 discovered and logged)"
  - "identityAdmin interface extended with HasCapability — a reusable in-package capability-check
    seam for any future mint-time/action-time capability gate that cannot live at the mount"
  - "37F-VALIDATION.md closed out: full Per-Task Verification Map, Wave 0 sign-off, coverage
    verification"
affects: [37F-18, 37F-19, future-capability-gated-mint-routes]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "In-handler capability check over a shared identityAdmin.HasCapability seam, for routes
      where a single mux entry serves multiple trust tiers via the request body (Go's ServeMux
      cannot dispatch on body content)"
    - "Direct-service-layer fixture minting in tests (bypassing the HTTP handler and its
      capability gate) to construct a precondition without granting a capability the test
      deliberately withholds — precedent already established by TestShareInternalRevokedExpired404"

key-files:
  created:
    - internal/agui/share_cross_identity_test.go
    - internal/agui/share_api_internal_test.go
    - internal/agui/share_api_public_test.go
  modified:
    - internal/agui/share_api.go
    - internal/agui/audit_api.go
    - internal/agui/audit_api_test.go
    - internal/agui/server.go
    - cmd/aura/serve_webui_share.go
    - internal/agui/share_api_test.go
    - .planning/phases/37F-conversation-artifact-sharing-export-inserted/37F-VALIDATION.md
    - .planning/phases/37F-conversation-artifact-sharing-export-inserted/deferred-items.md

key-decisions:
  - "share.public capability check lives in-handler (handleShareCreate), not at the parent mux, because a single POST /api/shares route answers both the internal and public tier via the JSON body and Go's ServeMux dispatches on method+path only"
  - "Reused the existing identityAdmin interface (added HasCapability) instead of a new Server seam — *identity.Store already implements it and is already wired via SetIdentityAdmin at the composition root, so no new production wiring point was needed"
  - "A's public-link fixtures for SC4 rows 5/7/9 are minted directly via share.Service.Create (bypassing HTTP), not through the createShare test helper, so identity A genuinely never holds share.public anywhere in the test — required for row 8's negative assertion to mean anything"
  - "Split share_api_test.go into three files (shared fixtures + CRUD / D-10 internal / public-token) mirroring the existing share_api.go / share_api_internal.go / share_api_public.go production split, after the capability check pushed the single file to 613 LOC"
  - "Did not fix two pre-existing, unrelated failures discovered while verifying: internal/db's two migration round-trip tests (human-reserved, fix/ci-red-37f-drift) and internal/objectstore's sub-85% package coverage (structural, already deferred once by 37F-04) — both fully documented in deferred-items.md rather than silently worked around"

requirements-completed: [WEBSHARE-04]

coverage:
  - id: D1
    description: "TestShareCrossIdentityDeny — all ten WEBSHARE-04 SC4 cross-identity rows pass against real Postgres, non-vacuously (row 8 confirmed to fail against the pre-fix handler in a controlled sanity check, then confirmed green after restoring the fix)"
    requirement: "WEBSHARE-04"
    verification:
      - kind: integration
        ref: "internal/agui/share_cross_identity_test.go#TestShareCrossIdentityDeny (10 subtests)"
        status: pass
    human_judgment: false
  - id: D2
    description: "share.public capability gap closed: handleShareCreate now checks the caller's share.public capability for public-tier mints (previously only the org kill-switch was checked)"
    requirement: "WEBSHARE-04"
    verification:
      - kind: integration
        ref: "internal/agui/share_cross_identity_test.go#TestShareCrossIdentityDeny/row8_mint_public_without_capability_403"
        status: pass
      - kind: integration
        ref: "internal/agui/share_api_public_test.go#TestSharePublicMintWithCapability (regression: capability holder still mints 201)"
        status: pass
    human_judgment: false
  - id: D3
    description: "Phase-wide tag audit: no 37F-owned test file carries a build tag outside db_integration"
    requirement: "WEBSHARE-04"
    verification:
      - kind: other
        ref: "grep -rlE 'go:build .*(garage_integration|authula_integration|musr_e2e|docker_integration)' over every 37F-owned file/directory — zero matches (one incidental, pre-existing, out-of-scope match in internal/objectstore/garageadmin/, predates 37F by 6 weeks)"
        status: pass
    human_judgment: false
  - id: D4
    description: "Coverage floor: aggregate >=85% across the real db_integration+neo4j_integration matrix, run against a disposable DB via scripts/coverage_docker.sh"
    requirement: "WEBSHARE-04"
    verification:
      - kind: other
        ref: "bash scripts/coverage_docker.sh (worked around a pre-existing, unrelated internal/db failure that blocked the script's own final aggregation step; real aggregate 85.7% reconstructed by hand-applying the gate's own filter to the fresh, complete coverage profile the run left on disk)"
        status: pass
    human_judgment: true
    rationale: "The coverage_docker.sh script itself did not exit 0 (a pre-existing, human-reserved internal/db test failure aborted it before the aggregation step) — the 85.7% figure is a faithful manual reconstruction using the gate's own filter logic against the same fresh profile, not the script's own printed output. A human should confirm this reconstruction is acceptable, and separately be aware internal/objectstore individually measures 69.6% (pre-existing, structural, documented in deferred-items.md) — 5 of 6 owned packages clear the per-package floor, objectstore does not."
  - id: D5
    description: "37F-VALIDATION.md closed out: Per-Task Verification Map populated (37 rows across all 20 phase plans), Wave 0 Requirements checked off, Validation Sign-Off completed honestly (one item left deliberately unchecked with full documentation), nyquist_compliant: true, status: complete"
    requirement: "WEBSHARE-04"
    verification: []
    human_judgment: true
    rationale: "Documentation completeness and the judgment call to mark nyquist_compliant/status complete despite one honestly-unchecked sign-off item benefit from a human reviewer's confirmation — no automated test asserts documentation quality."

duration: 55min
completed: 2026-07-18
status: complete
---

# Phase 37F Plan 13: SC4 Cross-Identity Deny + Close the share.public Capability Gap Summary

**Ships the WEBSHARE-04 security centrepiece — a ten-row cross-identity deny E2E in `internal/agui` — and, along the way, closes a real authorization gap it exists to catch: `POST /api/shares` minted public shares for any authenticated identity regardless of whether they held `share.public`.**

## Performance

- **Duration:** ~55 min
- **Started:** 2026-07-17T23:37Z (immediately following 37F-12's completion)
- **Completed:** 2026-07-18T00:32Z
- **Tasks:** 2 (both `type="auto"`, no checkpoints)
- **Files modified:** 11 (3 created, 8 modified)

## Accomplishments

- `internal/agui/share_cross_identity_test.go` — `TestShareCrossIdentityDeny`, all ten SC4 rows as `t.Run` subtests under a single `//go:build db_integration` tag, 1.25s runtime (non-sub-second, real execution). Rows 9 and 10 (the two a naive implementation fails) both pass. Rows 3/4 exercise the concrete `GET /api/shares/{id}/data` route — 200 for a non-owner bearer (D-10 bearer-within-auth is intended), 401/302 anonymous through the real `RequireAuth` chain.
- **Closed a real authorization gap** (Rule 2 deviation, discovered and logged by plan 37F-12): `handleShareCreate` now checks the caller's `share.public` capability for public-tier mints, on top of the pre-existing org kill-switch. Verified non-vacuous by a controlled sanity check — the fix was temporarily disabled, row 8 was confirmed to fail (201 instead of 403), then the fix was restored and re-verified green.
- Extended the existing `identityAdmin` interface (`audit_api.go`) with `HasCapability` rather than adding new production wiring — `*identity.Store` already implements it and is already wired via `SetIdentityAdmin` at the composition root.
- Refactor-on-touch: split `share_api_test.go` (613 LOC after the capability-check addition, over the CLAUDE.md 600-LOC cap) into three files mirroring the existing `share_api.go`/`share_api_internal.go`/`share_api_public.go` production split.
- Phase-wide build-tag audit: confirmed no 37F-owned test carries a tag outside `db_integration`.
- Ran the real coverage matrix (`scripts/coverage_docker.sh`, disposable `aura_cov` DB): aggregate **85.7%**; 5 of 6 owned packages individually clear 85% (`internal/share` 85.3%, `internal/agui` 85.8%, `internal/runner` 92.4%, `internal/cron/handlers` 87.5%, `internal/config` 92.0%); `internal/objectstore` measures 69.6% (pre-existing, structural, documented below).
- Closed out `37F-VALIDATION.md`: populated the Per-Task Verification Map (37 rows spanning all 20 phase plans), checked off Wave 0 Requirements, completed the Validation Sign-Off, set `nyquist_compliant: true` and `status: complete`.

## Task Commits

Each task was committed atomically:

1. **Task 1: TestShareCrossIdentityDeny — the ten-row matrix** (+ the capability-gap Rule-2 deviation + the file-size refactor-on-touch) - `95092c3b` (feat)
2. **Task 2: Phase-wide tag audit + the real coverage gate + close out VALIDATION.md** - `42707add` (docs)

**Plan metadata:** (this commit)

## Files Created/Modified

- `internal/agui/share_cross_identity_test.go` — the WEBSHARE-04 SC4 acceptance vehicle, ten cross-identity rows
- `internal/agui/share_api.go` — `handleShareCreate` now gates the public tier on `share.public`; new `sharePublicCapabilityName` constant
- `internal/agui/audit_api.go` — `identityAdmin` interface gained `HasCapability`
- `internal/agui/audit_api_test.go` — `fakeIdentityAdmin` gained a matching `HasCapability`
- `internal/agui/server.go` — corrected a stale doc comment claiming the capability gate lives at the mount
- `cmd/aura/serve_webui_share.go` — corrected the "KNOWN GAP" comment to "CLOSED", pointing at the real in-handler fix
- `internal/agui/share_api_test.go` — trimmed to shared fixtures + owner-scoped CRUD tests; `newShareAPIEnv` wires `SetIdentityAdmin`; `createShare` auto-grants `share.public` for `tier=public` callers
- `internal/agui/share_api_internal_test.go` — new file, the six D-10 bearer-within-auth tests (split out)
- `internal/agui/share_api_public_test.go` — new file, the nine public-token-route tests (split out)
- `.planning/phases/37F-conversation-artifact-sharing-export-inserted/37F-VALIDATION.md` — Per-Task Verification Map populated, Wave 0 + Sign-Off completed, frontmatter closed
- `.planning/phases/37F-conversation-artifact-sharing-export-inserted/deferred-items.md` — two new entries (migration round-trip test drift; `internal/objectstore` coverage shortfall)

## Decisions Made

- **In-handler, not mount-level, capability check.** A single `POST /api/shares` route serves both the internal (D-01 default) and public tier via the JSON body; Go's `ServeMux` cannot dispatch on body content, so `RequireCapability(share.public)` structurally cannot wrap the mount without also (wrongly) forcing the internal-tier default to require the capability. The check lives inside `handleShareCreate`, gated on the parsed tier, over the same `identityAdmin.HasCapability` seam `RequireCapability` itself calls.
- **Reuse `identityAdmin`, don't add a new seam.** `*identity.Store` already satisfies `HasCapability`'s exact signature and is already wired at the composition root (`cmd/aura/serve.go`'s `SetIdentityAdmin(chat.identity)`) — extending the existing interface closed the gap with zero new production wiring.
- **A never holds `share.public`, anywhere, for the whole test.** Rows 5, 7, and 9 need A to already have a live/revoked public link, but row 8 needs A to still lack the capability when she attempts a NEW mint. Resolved by minting those fixture links directly over `share.Service.Create` (bypassing the HTTP handler and its capability gate entirely) — precedent already established in this file family by `TestShareInternalRevokedExpired404`'s direct-store fixture construction.
- **Refactor-on-touch over exception.** `share_api_test.go` breached the 600-LOC cap once the capability-check comment and helper changes landed; split three ways along the existing production-code seam rather than requesting a cap exception.
- **Two pre-existing findings documented, not silently worked around.** See Deviations below.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 2 - Missing Critical] Closed the share.public capability gap in `handleShareCreate`**
- **Found during:** Task 1 (writing SC4 row 8), and independently pre-flagged by plan 37F-12's own SUMMARY/deferred-items trail
- **Issue:** `handleShareCreate` checked only `s.cfg.SharePublicEnabled` (the org kill-switch) for a public-tier mint — never `HasCapability(identityID, "share.public")`. Any authenticated identity could mint a public share regardless of holding the capability.
- **Fix:** Added an in-handler capability check (`share_api.go`), gated on the parsed tier, over a newly-extended `identityAdmin.HasCapability` seam (`audit_api.go`). Internal-tier mints are unaffected (the check block is skipped entirely when `tier != public`).
- **Files modified:** `internal/agui/share_api.go`, `internal/agui/audit_api.go`, `internal/agui/audit_api_test.go`, `internal/agui/share_api_test.go` (fixture wiring so pre-existing public-tier tests keep minting), `internal/agui/server.go` + `cmd/aura/serve_webui_share.go` (stale doc-comment corrections)
- **Verification:** Sanity-checked non-vacuous — the fix was temporarily neutered, `go test -run TestShareCrossIdentityDeny/row8` failed (201 instead of 403), the fix was restored, and the full ten-row suite re-passed. Full `internal/agui` `db_integration` regression suite re-run afterward: 468 PASS, 1 pre-existing unrelated FAIL (`TestHandleCheckTelegramAvailabilityBranches`, a `TELEGRAM_BOT_TOKEN` env leak already documented in `deferred-items.md`'s 37F-07 entry).
- **Committed in:** `95092c3b`

**2. [Rule 1 - Bug/stale docs] Corrected two doc comments claiming the capability gate lives at the mount**
- **Found during:** Task 1, while implementing the in-handler fix
- **Issue:** `internal/agui/server.go` and `cmd/aura/serve_webui_share.go` both carried comments describing (or, in the second case, explicitly logging as a known gap) a mount-level `RequireCapability(share.public)` wrap that never existed and, per 37F-12's own finding, structurally cannot exist for this route.
- **Fix:** Rewrote both comments to describe the actual in-handler enforcement and cross-reference `share_api.go`'s doc.
- **Files modified:** `internal/agui/server.go`, `cmd/aura/serve_webui_share.go`
- **Committed in:** `95092c3b`

**3. [Rule 1 - Refactor-on-touch] Split `share_api_test.go` (600-LOC cap breach)**
- **Found during:** Task 1, post-implementation file-size check (`wc -l` → 613)
- **Issue:** The capability-check wiring comment + `createShare`'s auto-grant logic pushed `share_api_test.go` from ~597 to 613 lines, over the CLAUDE.md 600-LOC "no god class" cap.
- **Fix:** Split into three files along the existing production-code seam (owner-CRUD / D-10-internal / public-token), matching `share_api.go`/`share_api_internal.go`/`share_api_public.go`. All three new files verified under the cap via `bash scripts/check-file-size.sh`.
- **Files modified:** `internal/agui/share_api_test.go` (trimmed); created `internal/agui/share_api_internal_test.go`, `internal/agui/share_api_public_test.go`
- **Verification:** Full `internal/agui` `db_integration` suite re-run post-split with identical PASS/FAIL counts to pre-split.
- **Committed in:** `95092c3b`

---

**Total deviations:** 3 auto-fixed (1 missing-critical security gap, 1 stale-docs bug, 1 refactor-on-touch).
**Impact on plan:** All three were necessary for correctness/security/CLAUDE.md compliance. No scope creep beyond what the plan's own capability-gap directive explicitly authorized.

## Issues Encountered

**Two pre-existing, out-of-scope findings surfaced during Task 2's verification. Neither was fixed — both are fully documented in `deferred-items.md` with root cause and disposition, and neither blocks this plan's own deliverable (the SC4 test + capability fix are both fully verified green).**

1. **`internal/db`'s two migration round-trip tests fail** (`TestMigration0039RoundTrip`, `TestMigration0040RoundTrip`) — stale hardcoded version literals (39, 40) now that migration `0041_shared_links_rls` (37F-20, already an ancestor of this plan's starting commit) moved the on-disk floor to 41. This blocks `bash scripts/coverage_docker.sh` from ever reaching its own aggregation step. Both this plan's orchestrating instructions and 37F-20's own SUMMARY independently identify this exact area as belonging to the human-owned `fix/ci-red-37f-drift` branch — not touched. Worked around by hand-reconstructing the aggregate from the coverage profile the aborted run still wrote to disk (see `deferred-items.md`).

2. **`internal/objectstore` measures 69.6%** under the real two-tag gate — below the 85% per-package floor. Root cause: `s3.go`'s six live-Garage-endpoint methods are all at literal 0% (they need a real S3-compatible endpoint outside this gate's tag set; the coverage gate does not run a `garage_integration` tier). This is 100% pre-existing (37F never touched `s3.go`; this phase's only contribution to the package, `types.go`'s three key-derivation functions, is 100% covered each) and was already independently discovered and deferred once by plan 37F-04 under a plain untagged measurement. Closing it would mean introducing a mockable AWS-SDK-S3 client seam — a structural refactor of working, unrelated infrastructure, well outside WEBSHARE-04's scope.

## User Setup Required

None — no external service configuration required.

## Known Stubs

None — this plan is backend Go test + security-fix code; no UI surface, no stubbed data paths.

## Threat Flags

None found — the new surface (the `share.public` in-handler capability check) is exactly the mitigation the plan's own `<threat_model>` (T-37F-06) already anticipated, not an undocumented addition.

## Next Phase Readiness

- **WEBSHARE-04 is satisfied.** All ten SC4 rows pass, non-vacuously, in `internal/agui` under the real coverage gate's tag set. The `share.public` authorization gap plan 37F-12 discovered is closed.
- **37F-18** (wave 8, depends on 37F-13 among others) owns the phase's deeper quality pass — mutation spot-check on the SC3 core, web gates (Vitest/Stryker), `internal/webui/dist` freshness, and the `docs/aura-quality-snapshot.md` re-attestation. Nothing in this plan blocks it.
- **37F-19** (wave 9, depends on 37F-18) owns the phase's final human UAT + git push to master. This plan's `37F-VALIDATION.md` close-out covers the automated/Nyquist layer only, as noted in its own Validation Sign-Off — 37F-19's human-verification truths (public-page rendering, iframe sandboxing, share-modal visual inspection) are unaffected and still pending.
- **Two blockers for whoever reconciles `fix/ci-red-37f-drift` or owns the next `internal/objectstore` coverage push:** both fully detailed in `deferred-items.md`'s two new 37F-13 entries, with root cause and a recommended closure path for each.

---
*Phase: 37F-conversation-artifact-sharing-export-inserted*
*Completed: 2026-07-18*

## Self-Check: PASSED

- FOUND: internal/agui/share_cross_identity_test.go
- FOUND: internal/agui/share_api_internal_test.go
- FOUND: internal/agui/share_api_public_test.go
- FOUND: .planning/phases/37F-conversation-artifact-sharing-export-inserted/37F-VALIDATION.md
- FOUND commit: 95092c3b (Task 1)
- FOUND commit: 42707add (Task 2)
