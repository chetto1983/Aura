---
phase: 37F-conversation-artifact-sharing-export-inserted
plan: 08
subsystem: security
tags: [share-service, owner-gate, bearer-within-auth, copy-not-reference, postgres, objectstore, go]

# Dependency graph
requires:
  - phase: 37F-03
    provides: "Snapshot/SnapshotTurn/SnapshotArtifact/ConvMeta/ArtifactMeta + BuildSnapshot (redaction core)"
  - phase: 37F-04
    provides: "share.Mint/Hash (token) + share.ResolveExpiry (expiry math) + objectstore.ShareSnapshotKey/ShareArtifactKey/ShareKeyPrefix"
  - phase: 37F-07
    provides: "share.Store (raw-pgx CRUD + dual lazy resolvers) + share.AuditWriter + share.ErrShareNotFound"
provides:
  - "share.Service — Create/Update/Revoke/ResolveByToken/ResolveInternal, the composed share lifecycle"
  - "share.BundleFilter + bundleArtifacts + dropBlobs + dropSnapshotBlobs — the agent-artifacts-only filter and copy-on-share blob lifecycle (bundle.go)"
  - "share.ConversationReader / share.ArtifactLister / share.ArtifactOpener — the three consumer-declared seams a later composition-root plan wires to *conversations.Store / *agui.AssetService"
  - "share.CreateRequest / share.CreateResult / share.Tier (TierInternal/TierPublic) / share.ErrSharePublicDisabled"
affects: [37F-10, 37F-11, 37F-12, share-api-handlers, public-share-page]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Owner-gate-first ordered lifecycle with numbered doc comment matching numbered body steps (stolen verbatim from internal/runner/runner_delete.go)"
    - "Consumer-declared seam over a real store's return type (ConversationReader.GetForIdentity returns the package-local ConvMeta, not conversations.Conversation) so the seam carries zero risk of leaking the owner identity id even transitively — the live store needs a thin composition-root adapter, not direct structural satisfaction"
    - "Pure-function extraction for a security-critical branch (resolveTier) specifically so its own fail-closed property is unit-testable with zero database, mirroring ResolveExpiry's/BundleFilter's existing I/O-free design"
    - "Snapshot-id-keyed immutable blobs + atomic row-pointer swap for update-without-overwrite (Update writes the NEW snapshot's keys before swapping the pointer, drops the OLD snapshot's keys only after the swap succeeds)"

key-files:
  created:
    - internal/share/bundle.go
    - internal/share/bundle_test.go
    - internal/share/service.go
    - internal/share/service_integration_test.go
    - internal/share/service_integration_edge_test.go
  modified: []

key-decisions:
  - "BundleFilter operates on []assets.Asset directly (not a bundler-local BundleArtifact type or an extended ArtifactMeta) — the plan explicitly offered 'extend ArtifactMeta OR define a bundler-local input type'; assets.Asset already carries every field the filter needs (SourceKind, Status) AND is literally what ArtifactLister.ListForThread already returns in production, so introducing a third shape would be a pure conversion tax with no safety benefit. ArtifactMeta (37F-03's four-field snapshot allowlist) stays untouched — bundleArtifacts constructs it fresh, per-candidate, from the SAME assets.Asset fields BundleFilter already inspected, so the filter's inputs and the snapshot's outputs never share a type without also sharing a field list."
  - "dropSnapshotBlobs (bundle.go) is a Task-2-scoped addition to a Task-1-scoped file: Update's D-06 'new snapshot_id, keep the token' semantics need a way to reclaim ONLY the OLD snapshot's blobs (not the whole share prefix, which would also delete the brand-new snapshot's keys — both snapshots' keys briefly coexist under the same share/<shareID>/ prefix during Update). Landed as a bundle.go sibling to dropBlobs rather than a new file, since bundle.go already owns every objectstore blob-lifecycle helper for share."
  - "resolveTier is extracted as its own pure function in service.go specifically so TestDefaultTierIsInternal can live UNTAGGED in bundle_test.go rather than in the db_integration-tagged integration file — Service.Create itself cannot run without a live Postgres pool (Insert requires a real *pgxpool.Pool-backed *Store), so without this extraction the tier-default property could only be proven end-to-end through a full Create call. The plan's own Task 3 text explicitly sanctioned this relocation ('prefer the untagged home'); documented here per that instruction."
  - "ConversationReader.GetForIdentity returns share.ConvMeta (Title/Model/CreatedAt only), never conversations.Conversation — this means the real *conversations.Store does NOT structurally satisfy the interface as-is; a composition-root adapter (convReaderAdapter in the test files, a production equivalent in a later wiring plan) is required. This is intentional: conversations.Conversation carries the owner's IdentityID, and accepting that type here would put the leak-by-transitivity risk back in reach for zero benefit — matching snapshot.go's own ConvMeta precedent from 37F-03."
  - "Anonymous public-tier 'open' audit events record the literal identity_id marker 'public' (anonymousAuditPrincipal), not an empty string or NULL — aura.share_audit.identity_id is NOT NULL text with no FK (matching skill_audit/mcp_audit), and a free-text marker communicates 'no principal, by design' more clearly than an empty string would in the admin audit UI."
  - "Audit-write failures (AuditWriter.Append erroring after a successful Insert/Revoke/UpdateSnapshot) are propagated as hard errors from Create/Update/Revoke/ResolveByToken/ResolveInternal, not swallowed as a best-effort warning — this codebase's own audit ledgers are documented as append-only integrity guarantees (D-14), and a silently-dropped audit row on a share action would read as complete when it is not. Accepted trade-off: a transient audit-write failure surfaces as a failed Create/Update/Revoke even though the underlying mutation already succeeded; no rollback is attempted (there is no cross-store transaction spanning Postgres + the audit table's own writer in this design)."
  - "Split service_integration_test.go into two files (service_integration_test.go for the plan's 9 named tests + shared helpers/adapters/fakes, service_integration_edge_test.go for the coverage-floor deviation tests) purely to stay under the repo-wide 600-LOC file cap — the combined single-file draft reached 624 LOC. service_integration_edge_test.go is not in the plan's files_modified list; see Deviations."

requirements-completed: [WEBSHARE-02, WEBSHARE-03]

coverage:
  - id: D1
    description: "share.BundleFilter reproduces web/src/chat/artifacts/useThreadArtifacts.ts's selectAgentArtifacts server-side as a fail-closed allowlist of one (assets.SourceAgent); bundleArtifacts copies bytes into token-scoped ShareArtifactKey locations; dropBlobs/dropSnapshotBlobs idempotently reclaim them"
    requirement: "WEBSHARE-02"
    verification:
      - kind: unit
        ref: "internal/share/bundle_test.go#TestBundleFiltersAgentArtifacts (7 behavior rows) + #TestBundleArtifactsCopiesUnderPrefix + #TestBundleArtifactsOpenFailure + #TestBundleArtifactsInvalidAssetID + #TestBundleDropBlobsIdempotent + #TestBundleDropBlobsListError + #TestDropSnapshotBlobsListError"
        status: pass
    human_judgment: false
  - id: D2
    description: "share.Service.Create runs the owner gate before any mint/build/write, defaults fail-closed to the internal tier, and refuses a public mint without a valid expiry or with the org kill-switch off — all proven against a live Postgres"
    requirement: "WEBSHARE-02"
    verification:
      - kind: unit
        ref: "internal/share/bundle_test.go#TestDefaultTierIsInternal (untagged, tests service.go's resolveTier with zero database)"
        status: pass
      - kind: integration
        ref: "internal/share/service_integration_test.go#TestSharePublicRequiresExpiryService + #TestSharePublicDeniedWhenKillSwitchOff (db_integration tag, live Postgres)"
        status: pass
      - kind: integration
        ref: "internal/share/service_integration_edge_test.go#TestShareCreateOwnerGate + #TestShareCreateArtifactBundleFailure (db_integration tag)"
        status: pass
    human_judgment: false
  - id: D3
    description: "Update mints a NEW snapshot_id and keeps the same token (D-06); Revoke drops blobs then stamps revoked_at (R-10); both run the owner gate first"
    requirement: "WEBSHARE-02"
    verification:
      - kind: integration
        ref: "internal/share/service_integration_test.go#TestShareUpdateResnapshot + #TestShareRevokeDropsBlobs (db_integration tag)"
        status: pass
      - kind: integration
        ref: "internal/share/service_integration_edge_test.go#TestShareUpdateOwnerGate + #TestShareRevokeOwnerGate + #TestShareUpdateArtifactBundleFailure + #TestShareRevokeAlreadyRevoked (db_integration tag)"
        status: pass
    human_judgment: false
  - id: D4
    description: "ResolveInternal serves the D-10 bearer-within-auth read (a NON-owner authenticated identity opens an internal link) with no owner gate, rejects a public-tier id, and 404s a revoked/expired internal link with the sweep never run; ResolveByToken/ResolveInternal both collapse a missing-blob failure to the same sentinel; the resolve path never re-calls the identity-scoped ArtifactOpener (copy, not reference)"
    requirement: "WEBSHARE-03"
    verification:
      - kind: integration
        ref: "internal/share/service_integration_test.go#TestShareResolveInternalBearer (resolves as the NON-owner B, not vacuously as A) + #TestShareResolveInternalRejectsPublicTier + #TestShareResolveInternalLazyLiveness + #TestShareBundledArtifactTokenScoped (asserts the opener's call count is unchanged across ResolveInternal) + #TestShareSnapshotFrozen (db_integration tag)"
        status: pass
      - kind: integration
        ref: "internal/share/service_integration_edge_test.go#TestShareResolveInternalInvalidID + #TestShareResolveByTokenUnknown + #TestServiceGetSnapshotMissingBlob + #TestServiceGetSnapshotCorruptJSON + #TestShareResolveByTokenBlobMissing + #TestShareResolveInternalBlobMissing (db_integration tag)"
        status: pass
    human_judgment: false

duration: ~2h45m
completed: 2026-07-17
status: complete
---

# Phase 37F Plan 08: Share Service Composition Summary

**`share.Service` composes Create/Update/Revoke/ResolveByToken/ResolveInternal over the 37F-03/04/07 primitives with the owner gate first on every mutation, a fail-closed tier default, drop-then-stamp revoke, snapshot-id-keyed update-without-overwrite, and D-10's deliberately ungated bearer-within-auth internal resolver — plus `bundle.go`'s server-side, fail-closed `BundleFilter` that keeps a user's own uploads out of every share**

## Performance

- **Duration:** ~2h45m (includes an iterative coverage-floor pass after the initial 81.5% measurement, and a mid-session file split to respect the 600-LOC cap)
- **Started:** 2026-07-17T17:00 CEST (session start, reading `store.go`/`audit.go`/`token.go`/`expiry.go`/`snapshot.go` from prior plans)
- **Completed:** 2026-07-17T19:45 CEST
- **Tasks:** 3 planned, all complete (+1 file-split deviation)
- **Files:** 5 created (0 modified pre-existing files — this plan's file set is entirely new)

## Accomplishments

- `internal/share/bundle.go` — `BundleFilter([]assets.Asset) []assets.Asset`, a fail-closed allowlist-of-one (`assets.SourceAgent`, live status only) that reproduces `web/src/chat/artifacts/useThreadArtifacts.ts`'s `selectAgentArtifacts` server-side, at the trust boundary; `bundleArtifacts` copies each filtered artifact's bytes into `objectstore.ShareArtifactKey(shareID, snapshotID, assetID)` via the consumer-declared `ArtifactOpener` seam; `dropBlobs`/`dropSnapshotBlobs` idempotently reclaim a share's (or one snapshot's) blobs.
- `internal/share/service.go` — `Service.Create` runs the owner gate (`conv.GetForIdentity`) before minting, building, or writing a single byte; resolves the tier via the pure `resolveTier` (absent/unrecognized → internal, D-01); re-checks the org public-tier kill-switch inside the handler (`ErrSharePublicDisabled`) rather than trusting only the capability-gated mount; rejects a public mint with no valid expiry via `ResolveExpiry` before the DB CHECK ever sees it; mints the token only for the public tier and returns the plaintext exactly once in `CreateResult`.
- `Service.Update` re-snapshots to a NEW `snapshot_id` (never overwriting the live blob mid-write), keeps the SAME token (D-06), and drops the OLD snapshot's blobs only after the row's pointer swap succeeds.
- `Service.Revoke` drops blobs THEN stamps `revoked_at` — never the reverse (R-10) — so a crash mid-sequence re-runs the idempotent delete rather than orphaning bytes permanently.
- `Service.ResolveByToken`/`Service.ResolveInternal` both collapse every miss (unknown, revoked, expired, wrong tier, missing/corrupt blob) to the identical `ErrShareNotFound`. `ResolveInternal` deliberately runs NO owner gate — D-10's bearer-within-auth: the link is the capability, `RequireAuth` (a later plan's route mount) is the gate — and delegates tier + liveness entirely to `Store.ResolveLiveByID`'s SQL predicate with no Go-side re-check, documented at length (both in the doc comment and via a dedicated test that resolves as the NON-owner) against the owner-gate-first discipline the three functions above establish.
- 9 named integration tests (`service_integration_test.go`) plus 12 coverage-floor tests (`service_integration_edge_test.go`) proven live against Postgres under `db_integration`, package coverage raised from 81.5% (after adding `service.go`) to **85.2%** — clearing this project's 85% hard floor (CLAUDE.md, overriding the PRD's 75%/60% split).

## Task Commits

Each task was committed atomically:

1. **Task 1: bundle.go — the agent-artifacts-only filter and copy-on-share** - `cfed64f34` (feat)
2. **Task 2: service.go — Create/Update/Revoke/Resolve, owner gate first** - `2da85039f` (feat)
3. **Task 3: service integration tests — frozen snapshot, update, revoke-drops-blobs, default tier** - `22d17a963` (test)

**Plan metadata:** (this commit, docs: complete plan)

## Files Created/Modified

- `internal/share/bundle.go` (167 LOC) — `ArtifactLister`/`ArtifactOpener` seams, `BundleFilter`, `bundleArtifacts`, `dropBlobs`, `dropSnapshotBlobs`
- `internal/share/bundle_test.go` (254 LOC) — filter table test (7 rows), copy/prefix/idempotency/error-path tests, `TestDefaultTierIsInternal` (co-located per plan guidance)
- `internal/share/service.go` (493 LOC) — `Service`, `CreateRequest`/`CreateResult`/`Tier`, `ConversationReader`, `resolveTier`, `Create`/`Update`/`Revoke`/`ResolveByToken`/`ResolveInternal`, `ErrSharePublicDisabled`
- `internal/share/service_integration_test.go` (387 LOC) — shared adapters/fakes/helpers + the plan's 9 named security-property tests
- `internal/share/service_integration_edge_test.go` (236 LOC, deviation) — 12 coverage-floor tests

## Decisions Made

See frontmatter `key-decisions` for the full list. Summary: `BundleFilter` operates directly on `assets.Asset` (the type `ArtifactLister` already returns in production) rather than inventing a third shape; `dropSnapshotBlobs` lives in `bundle.go` as Update's narrower-scope sibling to `dropBlobs`; `resolveTier` is extracted as a pure function specifically so its fail-closed property is unit-testable with zero database; `ConversationReader.GetForIdentity` returns the package-local `ConvMeta` rather than `conversations.Conversation`, requiring a composition-root adapter (test-side `convReaderAdapter` here, a production equivalent in a later plan); anonymous public-tier audit "open" events record the literal marker `"public"`; audit-write failures propagate as hard errors from every lifecycle method; the integration test file was split in two to respect the 600-LOC cap.

## How each of ADR-0039's mitigations is enforced at THIS layer (per the executor's security_emphasis mandate)

1. **Public is never the default (D-01).** `resolveTier` has exactly one explicit case (`TierPublic`) and a `default:` arm that yields `TierInternal` — no code path assigns `TierPublic` without an explicit request value. Proven by `TestDefaultTierIsInternal` (absent + garbage tier → internal; only an explicit `TierPublic` request reaches public).
2. **Capability gate.** NOT enforced in this layer by design — `Service.Create` deliberately has no capability check; the plan's own action text states the split explicitly: "The capability check lives at the handler (plan 37F-10) because it needs the request principal." Documented in `Create`'s doc comment so the split reads as intentional, not missing.
3. **Org kill-switch, re-checked inside the mint path.** `Service.Create` checks `svc.publicEnabled` (the composition root's `AURA_SHARE_PUBLIC_ENABLED` value, injected via `NewService`) BEFORE minting anything, independent of any capability/edge check — `ErrSharePublicDisabled`. Proven by `TestSharePublicDeniedWhenKillSwitchOff` (publicEnabled=false denies Create even with a valid expiry and tier).
4. **Mandatory expiry.** `Create`'s public branch calls `ResolveExpiry` (37F-04) and propagates its error (including `ErrNonPositiveCustomExpiry`) BEFORE any row is written — proven end-to-end by `TestSharePublicRequiresExpiryService`, which also asserts zero rows were written after the rejection (`ListForConversation` returns empty).
5. **Revoke always works, independent of expiry, and drops reachability.** `Revoke` runs the owner gate, then `dropBlobs` (every byte under the share's prefix), THEN `RevokeForIdentity` (the stamp) — proven by `TestShareRevokeDropsBlobs` (empty prefix listing + a subsequent `ResolveByToken` 404) and `TestShareRevokeAlreadyRevoked` (a second revoke surfaces the store's not-found sentinel rather than succeeding silently).
6. **No identity oracle.** `ResolveByToken` and `ResolveInternal` each collapse EVERY failure mode — unknown, revoked, expired, wrong tier, and (this plan's addition) a missing/corrupt snapshot blob — to the single `ErrShareNotFound` sentinel, with no code path returning a distinguishable error. Proven by `TestShareResolveInternalRejectsPublicTier`, `TestShareResolveInternalLazyLiveness`, `TestShareResolveByTokenUnknown`, `TestShareResolveByTokenBlobMissing`, `TestShareResolveInternalBlobMissing`.
7. **`ResolveInternal` has NO owner gate (D-10), by design.** Its doc comment states the omission explicitly and contrasts it against `Create`/`Update`/`Revoke`'s owner-gate-first discipline three functions above; the body contains no `GetForIdentity` call and delegates ALL tier/liveness logic to `Store.ResolveLiveByID`'s SQL predicate (no Go-side `tier ==`/`revoked`/`expires` comparison anywhere in the function). Proven by `TestShareResolveInternalBearer`, which resolves as bearer B — a DIFFERENT, non-owner identity — and asserts success (resolving as the owner would pass vacuously and not prove the property).
8. **Audit runs across every tier, on the failure paths too.** Every lifecycle method (`Create`/`Update`/`Revoke`/`ResolveByToken`/`ResolveInternal`) calls `AuditWriter.Append` and propagates its error rather than swallowing it — an audit-write failure surfaces as a failed operation rather than a silently-incomplete ledger. `ResolveByToken` records the literal `"public"` marker (no recipient PII); `ResolveInternal` records the resolving `identityID` (an authenticated bearer has a first-class identity to record — no PII concern).

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking issue] `dropSnapshotBlobs` added to `bundle.go` (Task 1's file) during Task 2**
- **Found during:** Task 2, writing `Service.Update`
- **Issue:** D-06's Update semantics ("new snapshot_id, keep the token") require reclaiming ONLY the OLD snapshot's blobs after the pointer swap. `dropBlobs(shareID)` (Task 1) reclaims the WHOLE share prefix — calling it during Update would also delete the brand-new snapshot's just-written blobs, since both snapshots' keys briefly coexist under the same `share/<shareID>/` prefix.
- **Fix:** Added `dropSnapshotBlobs(ctx, store, bucket, shareID, snapshotID)` to `bundle.go`, scoped to `share/<shareID>/snapshot/<snapshotID>/` only.
- **Files modified:** `internal/share/bundle.go`, `internal/share/bundle_test.go` (added `TestDropSnapshotBlobsListError`)
- **Verification:** `go build ./...`, `go vet`, `golangci-lint run` all clean; `TestShareUpdateResnapshot` (integration) proves the old snapshot's blobs are gone and the new one's are readable.
- **Committed in:** `2da85039f` (folded into the Task 2 commit, since the function serves `Update`)

**2. [Rule 2 - Missing critical] Coverage-floor test additions to clear the 85% package floor**
- **Found during:** Post-Task-3 verification, measuring `internal/share` coverage under `db_integration`
- **Issue:** With only the plan's 9 named tests plus Task 1's original 3 bundle tests, package coverage measured 81.5% — below this project's CLAUDE.md-mandated 85% hard floor (which overrides the PRD's 75%/60% split). `service.go`'s error branches (owner-gate misses on Update/Revoke, a malformed `ResolveInternal` id, an unknown `ResolveByToken` token, missing/corrupt blobs, artifact-bundle failures inside Create/Update, an already-revoked second Revoke) were never exercised.
- **Fix:** Added 6 tests to `bundle_test.go` (`TestBundleArtifactsOpenFailure`, `TestBundleArtifactsInvalidAssetID`, `TestBundleDropBlobsListError`, `TestDropSnapshotBlobsListError`, plus `TestDefaultTierIsInternal` already counted above) and 12 tests in a new file `service_integration_edge_test.go` (owner-gate misses, invalid-id, unknown-token, missing/corrupt-blob via both the shared `getSnapshot` helper directly and through each public resolver, artifact-bundle failures, already-revoked). Coverage after: **85.2%**.
- **Files modified:** `internal/share/bundle_test.go`, `internal/share/service_integration_test.go` (initially, before the split), `internal/share/service_integration_edge_test.go` (new)
- **Verification:** `go test -tags db_integration -p 1 -count=1 -coverprofile=... ./internal/share/` → `85.2%` (up from 81.5%); every new test asserts a specific, named failure mode via `errors.Is(err, ErrShareNotFound)` or a non-nil-error check, not a blanket "don't panic."
- **Committed in:** `22d17a963` (Task 3 commit)

**3. [Rule 3 - Blocking issue] Split the integration test file to respect the 600-LOC cap**
- **Found during:** Post-coverage-pass verification, after adding the 12 coverage-floor tests to what was then a single `service_integration_test.go`
- **Issue:** The combined file reached 624 lines — over CLAUDE.md's repo-wide 600-LOC-per-file cap (`scripts/check-file-size.sh`, a pre-commit hook gate).
- **Fix:** Split into `service_integration_test.go` (387 LOC — shared helpers/adapters/fakes + the plan's 9 named tests, in their original relative order) and `service_integration_edge_test.go` (236 LOC — the 12 coverage-floor deviation tests from #2 above). Both carry the identical `//go:build db_integration` tag and compile together; `TestSharePublicDeniedWhenKillSwitchOff` (one of the plan's 9 named tests, which had gotten interleaved with the deviation tests during incremental editing) was moved back into the core file so the plan's named-test set stays intact in one place.
- **Files modified:** `internal/share/service_integration_test.go`, `internal/share/service_integration_edge_test.go` (new — not in the plan's `files_modified` frontmatter list)
- **Verification:** `bash scripts/check-file-size.sh` exits 0 for both files; full `db_integration` suite re-run green (`ok ... 12.046s`, non-sub-second, genuinely executed); coverage unchanged at 85.2% after the split (pure file reorganization, no logic change).
- **Committed in:** `22d17a963` (Task 3 commit)

**4. [Rule 1 - Bug, self-caught via the plan's own acceptance-criteria greps] Two doc comments tripped their own grep-gated ban**
- **Found during:** Final verification pass, running the plan's own acceptance-criteria greps as a self-check (mirroring the 37F-04 precedent for this exact class of self-inflicted trip)
- **Issue:** (a) `service.go`'s `Service` struct doc explained "why not `objectstore.IdentityStore.Resolve(ctx)`" by naming that exact banned pattern in prose, tripping the acceptance criterion `grep -n "IdentityStore\|\.Resolve(ctx)" internal/share/service.go` → nothing. (b) `service_integration_test.go`'s header comment explained "no Garage, no `garage_integration` tag" by naming the literal banned substring, tripping `grep -rn "garage_integration" internal/share/` → nothing.
- **Fix:** Reworded both passages to describe the same rationale without the literal banned tokens — no behavior change. (a) now says "NEVER pulled from the ctx-keyed per-identity credential resolver objectstore already exposes elsewhere... its own 'is this the shared principal' helper." (b) now says "no additional build tag beyond the one on this file's own first line."
- **Files modified:** `internal/share/service.go`, `internal/share/service_integration_test.go`
- **Verification:** Both greps return nothing after the reword; `go build`, `go vet`, `golangci-lint run`, and the full test suite all re-verified green afterward.
- **Committed in:** `2da85039f` (service.go's fix) and `22d17a963` (service_integration_test.go's fix) — folded into each file's respective task commit since both were caught before that commit landed.

---

**Total deviations:** 4 auto-fixed (2 Rule 3 blocking-issue additions, 1 Rule 2 missing-coverage addition, 1 Rule 1 self-caught grep-trip fix), all documented above. No architectural-change (Rule 4) escalation was needed.

**Impact on plan:** All four are necessary for correctness (dropSnapshotBlobs), the project's coverage floor (test additions + the resulting file split), or the plan's own literal acceptance criteria (the reworded comments). No scope creep — no new production behavior beyond what the plan's task text specifies.

## Verification Evidence (db_integration tier genuinely executed, not skipped)

```
go build ./...                                             clean
go vet ./internal/share/...                                clean
golangci-lint run ./internal/share/...                     0 issues
bash scripts/check-file-size.sh                            all files within 600-LOC cap
go test ./internal/share/ -count=1                         ok, 0.1s (untagged suite, 62 tests incl. this plan's untagged additions)
go test -race ./internal/share/ -count=1                   ok, 1.2s
go test -tags db_integration -race -p 1 -count=1 ./internal/share/     ok, ~10-16s (NOT sub-second — genuinely executed against live Postgres)
go test -tags db_integration -cover -p 1 -count=1 ./internal/share/    ok, coverage: 85.2% of statements
```

Structural/grep-gated acceptance criteria, all re-verified clean after the final reword:
```
go list -deps ./internal/share/ | grep -E "internal/(agui|conversations)$"   -> nothing (no forbidden import)
grep -rn "garage_integration" internal/share/                                -> nothing
grep -n "IdentityStore\|\.Resolve(ctx)" internal/share/service.go            -> nothing (object store injected, not resolved)
grep -nE "%w.*[Tt]oken|Errorf.*plaintext" internal/share/service.go          -> nothing (token never logged/wrapped)
grep -q "assets.SourceAgent" internal/share/bundle.go                        -> match (real constant, not a literal)
grep -q "useThreadArtifacts" internal/share/bundle.go                        -> match (cross-referenced)
head -1 internal/share/service_integration_test.go                          -> exactly "//go:build db_integration"
```

Line-order acceptance criteria (owner-gate-first, drop-then-stamp), verified via `grep -n`:
```
Create: svc.conv.GetForIdentity at line 195; BuildSnapshot at 229; Put at 241; Mint() at 255; Insert at 281
        -> the owner gate is the lowest line number, before every side effect.
Revoke: dropBlobs at line 391; RevokeForIdentity (the stamp) at line 394 -> drop-then-stamp order holds.
ResolveInternal: no GetForIdentity call in its body; no tier==/Tier==/revoked/expires comparison in its body;
                 calls ResolveLiveByID; its doc comment (last ~26 lines before the signature) names "D-10" 3 times.
Update: no Mint() call anywhere in its body -> the token column is genuinely untouched.
```

## Known Stubs

None. `Service`'s five methods are fully wired against the primitives from 37F-03/04/07; the only unimplemented surface is the composition-root adapter wiring the consumer-declared seams to the live `*conversations.Store`/`*agui.AssetService` — explicitly out of this plan's scope (a later plan, per the plan's own `<read_first>` framing of `ConversationReader`/`ArtifactLister`/`ArtifactOpener` as seams "a later plan wires").

## Threat Flags

None beyond what ADR-0039 and the plan's own `<threat_model>` already register — every threat this plan's task text names (T-37F-07/08/09/11/41/42/43/44/45/46/53) is mitigated as designed and cited against a named test in the section above. No new unregistered surface was introduced.

## User Setup Required

None — no new environment variables, no new external service configuration. `Service` is a pure composition over already-existing primitives and an injected `objectstore.Store` + bucket string; wiring it into the running server (the composition root, `config.ShareConfig` → `NewService`, plus the `ConversationReader`/`ArtifactLister`/`ArtifactOpener` production adapters) is plan 37F-12's job.

## Next Phase Readiness

- `share.Service`, `share.CreateRequest`/`CreateResult`/`Tier`, `share.ErrSharePublicDisabled`, and the three consumer-declared seams (`ConversationReader`/`ArtifactLister`/`ArtifactOpener`) are all available for plan 37F-10 (`internal/agui/share_service.go` interface + `share_api.go` HTTP handlers) to build on directly — that plan owns the capability-gate check (`share.public`) this plan's `Create` explicitly defers to it, plus the composition-root adapters wiring the real `*conversations.Store`/`*agui.AssetService`.
- The internal tier's route (`GET /api/shares/{id}/data`, plan 37F-10) is `ResolveInternal`'s ONLY consumer and is now unblocked: without that route the internal tier — D-01's default share action — would mint rows no recipient could ever open.
- No blockers. `internal/share` still imports neither `internal/agui` nor `internal/conversations`; package coverage clears the 85% floor at 85.2%.

---
*Phase: 37F-conversation-artifact-sharing-export-inserted*
*Completed: 2026-07-17*

## Self-Check: PASSED

All 5 created files verified present on disk (`internal/share/bundle.go`,
`internal/share/bundle_test.go`, `internal/share/service.go`,
`internal/share/service_integration_test.go`,
`internal/share/service_integration_edge_test.go`); all 3 task commit hashes
(`cfed64f34`, `2da85039f`, `22d17a963`) verified present in `git log --oneline --all`.
