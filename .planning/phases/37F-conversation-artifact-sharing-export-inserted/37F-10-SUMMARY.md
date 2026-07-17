---
phase: 37F-conversation-artifact-sharing-export-inserted
plan: 10
subsystem: api
tags: [share, http, rest, security, oracle-free, capability, kill-switch, d-10, agui]

requires:
  - phase: 37F-08
    provides: "share.Service (Create/Update/Revoke/ResolveByToken/ResolveInternal) + share.Store's ResolveLiveByID/ListForIdentity/ListForConversation over migration 0040's aura.shared_links"
provides:
  - "agui.ShareService — the narrow consumer-declared interface AG-UI handlers call, mirroring share.Service + share.Store's list/artifact reads (share_service.go)"
  - "share_api.go — owner-scoped CRUD: POST/GET /api/shares, PATCH /api/shares/{id}/snapshot, DELETE /api/shares/{id}. 404-never-403 on foreign; in-handler ServerConfig.SharePublicEnabled re-check (R-08 closure surviving the auth.go:282 loopback capability bypass)"
  - "share_api_internal.go — the D-10 bearer-within-auth routes: GET /api/shares/{id}/data + /asset/{assetID}, RequireAuth-only, no capability, no owner predicate; tier/liveness resolved entirely by ResolveInternal's SQL predicate so unknown/revoked/expired/public-tier ids are one indistinguishable 404"
  - "share_api_public.go — the unauthenticated GET /s/{token}/data + /asset/{id} routes; oracle-free (one literal 404 body for every failure mode, no length/shape pre-check)"
  - "ServerConfig.SharePublicEnabled + Server.SetShareService (server.go wiring)"
  - "shareLinkResponse — the wire DTO matching the ALREADY-SHIPPED frontend contract (web/src/chat/share/shareTypes.ts's ShareLink, locked by plan 37F-15), since share.Link/share.CreateResult carry no JSON tags and additionally expose fields (OwnerIdentityID, SnapshotBucket, FormatOptions) that must never reach the wire"
affects: [37F-12, 37F-17, 37F-18, 37F-19]

tech-stack:
  added: []
  patterns:
    - "One trust boundary per file, enforced by grep-gated acceptance criteria (no owner predicate in share_api_internal.go, no assets.Service reference in either asset-serving file, no http.StatusForbidden outside the public-tier mint path)"
    - "registerXRoutes per file, orchestrated by one registerShareRoutes call site in server.go's Mux() — route strings live beside the handlers they drive, never centralized"
    - "Belt-and-suspenders R-08 closure: ServerConfig.SharePublicEnabled checked in-handler (this plan) AND share.Service.Create's own ErrSharePublicDisabled check (37F-08) — two independent reads of the same config value, since the mount-level capability gate structurally vanishes on loopback (auth.go:282)"

key-files:
  created:
    - internal/agui/share_service.go
    - internal/agui/share_api.go
    - internal/agui/share_api_internal.go
    - internal/agui/share_api_public.go
    - internal/agui/share_api_test.go
  modified:
    - internal/agui/server.go

key-decisions:
  - "shareLinkResponse is a NEW wire DTO, not share.Link marshaled directly — discovered mid-plan that share.Link/share.CreateResult carry zero JSON tags and expose owner/bucket/format-options fields with no business on the wire; cross-checked against the ALREADY-SHIPPED web/src/chat/share/{shareTypes.ts,shareApi.ts,SharePage.tsx} (plans 37F-15/16, wave 4, written against this plan's locked route table before it existed) to match the exact field names/url-derivation rules those files already assume live."
  - "registerShareRoutes shipped as an empty stub in Task 1 (share_service.go) and was replaced with the real 8-route registration in Task 2 (moved to share_api.go) — the only way to keep every intermediate commit independently `go build`-clean while satisfying Task 1's own acceptance criterion that server.go already calls it."
  - "ServerConfig.SharePublicEnabled (not a ShareService interface method) carries the R-08 kill-switch value into the handler — mirrors the existing CORSPermissive boot-time-config-value pattern rather than the Set*-style optional-service pattern, since it is a plain bool sourced from config.ShareConfig.PublicEnabled, not a nilable dependency."

patterns-established:
  - "Three-trust-boundary-file split for a single route table: owner CRUD / bearer-within-auth / unauthenticated token, each file's header naming its own boundary and the opposite file's inverse rule, so a reader who greps one file cannot safely generalise to the others."

requirements-completed: [WEBSHARE-02, WEBSHARE-03]

coverage:
  - id: D1
    description: "Owner-scoped share CRUD (mint/list/update-snapshot/revoke) — 404-never-403 on a foreign id, audited, body-capped"
    requirement: "WEBSHARE-02"
    verification:
      - kind: integration
        ref: "internal/agui/share_api_test.go#TestShareAuditLedger"
        status: pass
      - kind: integration
        ref: "internal/agui/share_api_test.go#TestShareList"
        status: pass
      - kind: integration
        ref: "internal/agui/share_api_test.go#TestShareForeignOwnerGets404"
        status: pass
      - kind: integration
        ref: "internal/agui/share_api_test.go#TestShareCreateBodyCap"
        status: pass
    human_judgment: false
  - id: D2
    description: "D-10 bearer-within-auth internal tier: any authenticated identity holding the id resolves the redacted snapshot (200); anonymous 401s through the real RequireAuth chain; unknown/revoked/expired/public-tier ids are one byte-identical 404; artifacts are snapshot-scoped"
    requirement: "WEBSHARE-02"
    verification:
      - kind: integration
        ref: "internal/agui/share_api_test.go#TestShareInternalBearerWithinAuth"
        status: pass
      - kind: integration
        ref: "internal/agui/share_api_test.go#TestShareInternalAnonymous401"
        status: pass
      - kind: integration
        ref: "internal/agui/share_api_test.go#TestShareInternalRejectsPublicTierID"
        status: pass
      - kind: integration
        ref: "internal/agui/share_api_test.go#TestShareInternalRevokedExpired404"
        status: pass
      - kind: integration
        ref: "internal/agui/share_api_test.go#TestShareInternalAssetSnapshotScoped"
        status: pass
      - kind: integration
        ref: "internal/agui/share_api_test.go#TestShareInternalAssetContentType"
        status: pass
    human_judgment: false
  - id: D3
    description: "Public unauthenticated token tier: capability+kill-switch-gated mint, oracle-free resolve (byte-identical 404s across unknown/revoked/expired, 1000-token enumeration), PII-free open audit, never-logged token, snapshot-scoped assets, content-type/header-injection defenses"
    requirement: "WEBSHARE-03"
    verification:
      - kind: integration
        ref: "internal/agui/share_api_test.go#TestSharePublicMintWithCapability"
        status: pass
      - kind: integration
        ref: "internal/agui/share_api_test.go#TestShareRevokeThen404"
        status: pass
      - kind: integration
        ref: "internal/agui/share_api_test.go#TestShareTokenNoOracle"
        status: pass
      - kind: integration
        ref: "internal/agui/share_api_test.go#TestShareTokenEnumeration"
        status: pass
      - kind: integration
        ref: "internal/agui/share_api_test.go#TestSharePublicOpenAuditNoPII"
        status: pass
      - kind: integration
        ref: "internal/agui/share_api_test.go#TestShareTokenNeverLogged"
        status: pass
      - kind: integration
        ref: "internal/agui/share_api_test.go#TestSharePublicAssetContentType"
        status: pass
      - kind: integration
        ref: "internal/agui/share_api_test.go#TestSharePublicAssetHeaderInjection"
        status: pass
    human_judgment: false
  - id: D4
    description: "R-08: the org kill-switch survives the loopback capability bypass (RequireCapability structurally vanishes when !SecretConfigured) — the in-handler ServerConfig.SharePublicEnabled check is the gate that holds"
    requirement: "WEBSHARE-03"
    verification:
      - kind: integration
        ref: "internal/agui/share_api_test.go#TestSharePublicOrgKillSwitch"
        status: pass
    human_judgment: false

duration: 95min
completed: 2026-07-17
status: complete
---

# Phase 37F Plan 10: Share HTTP Surface Across Three Trust Boundaries Summary

**Eight share-lifecycle routes split across three trust-boundary files (owner CRUD, D-10 bearer-within-auth, unauthenticated token), with the R-08 loopback-surviving kill-switch and oracle-free 404s proven by 19 live integration tests.**

## Performance

- **Duration:** ~95 min
- **Started:** ~2026-07-17T20:07:00Z
- **Completed:** 2026-07-17T21:42:21Z
- **Tasks:** 3
- **Files modified:** 6 (5 created, 1 modified)

## Accomplishments

- `agui.ShareService` — the narrow interface AG-UI handlers consume, mirroring `share.Service`'s five lifecycle methods plus `share.Store`'s two list reads and one token/snapshot-scoped artifact opener `Service` itself doesn't expose.
- `share_api.go` — owner-scoped CRUD (`POST/GET /api/shares`, `PATCH /api/shares/{id}/snapshot`, `DELETE /api/shares/{id}`), every mutate/read 404-never-403 on a foreign id, with the R-08 in-handler kill-switch re-check inside `handleShareCreate`.
- `share_api_internal.go` — the D-10 route the entire internal tier depends on: `GET /api/shares/{id}/data` + `.../asset/{assetID}`, RequireAuth-only, deliberately no owner predicate, tier/liveness resolved entirely by `ResolveInternal`'s SQL predicate so a public-tier id is indistinguishable from an unknown one.
- `share_api_public.go` — the phase's only unauthenticated handlers, `GET /s/{token}/data` + `.../asset/{id}`, oracle-free by construction (one literal 404, no length/shape pre-check before the DB probe).
- `internal/agui/share_api_test.go` — 19 live `db_integration` tests (the plan's 17 named tests + 2 added to close gaps: `TestShareList` for a 0%-covered handler, `TestShareForeignOwnerGets404` for the owner-tier invariant called out in this run's `<security_invariants>`).

## Task Commits

Each task was committed atomically:

1. **Task 1: ShareService interface + server wiring** - `4a016d2c` (feat)
2. **Task 2: the eight routes across three files** - `342d875e` (feat)
3. **Task 3: share API integration tests** - `882e8997` (test)

**Plan metadata:** (this commit)

## Files Created/Modified

- `internal/agui/share_service.go` - the `ShareService` interface (56 LOC)
- `internal/agui/share_api.go` - owner-scoped CRUD handlers + `shareLinkResponse`/`toShareLinkResponse` (223 LOC)
- `internal/agui/share_api_internal.go` - D-10 bearer-within-auth handlers (109 LOC)
- `internal/agui/share_api_public.go` - unauthenticated token handlers (93 LOC)
- `internal/agui/share_api_test.go` - 19 live integration tests, single `db_integration` tag (596 LOC)
- `internal/agui/server.go` - `share ShareService` field, `ServerConfig.SharePublicEnabled`, `SetShareService`, `registerShareRoutes` call site (506→532 LOC)

## Decisions Made

- **`shareLinkResponse` is a new wire DTO, not `share.Link` marshaled directly.** `share.Link`/`share.CreateResult` carry zero JSON tags and expose `OwnerIdentityID`/`SnapshotBucket`/`FormatOptions` — none of which belong on the wire. Read the ALREADY-SHIPPED `web/src/chat/share/{shareTypes.ts,shareApi.ts}` and `SharePage.tsx`/`main.tsx` (plans 37F-15/16, wave 4, explicitly written against this plan's locked route table before it existed — their own header comments say so) to match the exact contract those files assume: `id/tier/url?/expires_at?/revoked_at?/created_at/updated_at/snapshot_turn_count?`, with `url` derived as `/shared/{id}` (internal, always) or the one-time `/s/{token}` (public, create response only).
- **`registerShareRoutes` shipped as an empty stub in Task 1's `share_service.go`, then was deleted and re-defined for real in Task 2's `share_api.go`.** This is the only way every intermediate commit stays independently `go build`-clean while Task 1's own acceptance criteria require `server.go` to already call it.
- **`ServerConfig.SharePublicEnabled`, not a `ShareService` method, carries the R-08 kill-switch value.** It mirrors the existing `CORSPermissive` boot-config-value pattern (a plain bool from `config.ShareConfig.PublicEnabled`) rather than the `Set*`-optional-service pattern — it is not a nilable dependency, so it does not belong on the narrow consumer-declared interface.
- **Literal `"not found"` bodies (not `sanitizeErr(err)`) on the internal and public 404 paths.** `ResolveInternal`/`ResolveByToken` already collapse every failure to one sentinel, but a hardcoded literal is byte-equality-safe by construction and does not depend on the service layer's error message staying stable forever.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 2 - Missing Critical] Added `TestShareList` — `handleShareList` had 0% coverage**
- **Found during:** Task 3 final coverage verification (`go tool cover -func`)
- **Issue:** None of the plan's 17 named tests exercise `GET /api/shares` at all; the function measured a literal 0.0% of statements — an entire endpoint with zero live proof, including its `?conversation_id=` branch.
- **Fix:** Added `TestShareList`, asserting both the all-links form and the conversation-scoped form return exactly the minted link.
- **Files modified:** internal/agui/share_api_test.go
- **Verification:** Live pass; `handleShareList` moved to 69.6% covered; package coverage 85.0%→85.6%.
- **Committed in:** 882e8997 (Task 3 commit)

**2. [Rule 2 - Missing Critical] Added `TestShareForeignOwnerGets404` — the owner-tier 404-never-403 invariant had no direct assertion**
- **Found during:** Task 3, cross-checking this run's own `<security_invariants>` block ("OWNER tier: ... A non-owner gets 404 (never 403)") against the 17 named tests, none of which exercise a foreign identity against the owner-scoped `PATCH`/`DELETE` routes specifically (the store-level property is proven in `internal/share`'s own suite, but the AG-UI HTTP layer had no direct proof).
- **Fix:** Added `TestShareForeignOwnerGets404`: identity B's `PATCH .../snapshot` and `DELETE` against identity A's share both assert 404.
- **Files modified:** internal/agui/share_api_test.go
- **Verification:** Live pass; `handleShareUpdateSnapshot`/`handleShareRevoke` coverage rose (50.0%→66.7%, 45.5%→63.6%).
- **Committed in:** 882e8997 (Task 3 commit)

---

**Total deviations:** 2 auto-fixed (both Rule 2 — missing critical test coverage/verification, not missing production code). **Impact on plan:** Both strengthen confidence in code that was already correct (neither handler has any code path capable of returning 403); no scope creep, no architectural change, no new production behavior.

## Issues Encountered

- **Pre-existing, already-documented environmental test failure (not this plan's).** `TestHandleCheckTelegramAvailabilityBranches/no_token_configured_reports_not-configured` (`internal/agui/settings_api_branches_test.go`) fails whenever this dev environment's `.env` (which carries a real `TELEGRAM_BOT_TOKEN` for the live deployment) is sourced to obtain `POSTGRES_PASSWORD` for `db_integration` tests. This is the SAME issue plans 37F-07 and 37F-09 already logged in `deferred-items.md` (root cause: `TELEGRAM_BOT_TOKEN` env-pollution, zero code overlap with any file this plan touches). Reproduced identically with `internal/agui/share_api_test.go` temporarily moved out of the package, confirming it is unrelated to this plan's changes. Isolated (env var unset, no file touched) for this plan's own verification runs, which all report the true, unpolluted result.
- **`go test -race` requires WSL on this machine.** Windows Git Bash has no working `gcc`/cgo toolchain (`CGO_ENABLED=0`, `gcc` not on PATH); per CLAUDE.md's own documented posture ("WSL is the full primary dev environment"), all `-race` and live-DB test runs in this plan were executed via `wsl.exe -e bash -lc '...'` against the same Docker-Desktop-shared Postgres (`127.0.0.1:5432`, reachable from both sides). `go build`/`go vet`/`golangci-lint`/`check-file-size.sh` ran natively on Windows throughout with identical clean results.
- **The 594-600 LOC ceiling on `share_api_test.go` required several rounds of comment-trimming** (19 tests + 2 adapters + a handful of shared helpers is a lot of surface for one file under a hard 600-LOC cap with no second test file permitted by the plan's own file-scope lock). No test logic, assertion, or security-property proof was cut — only prose.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

- The share HTTP surface is fully live and tested in isolation (`Server.Mux()` serves all eight routes correctly today), but **nothing mounts it into the running daemon yet** — `cmd/aura/serve_webui_share.go` (plan 37F-12) still needs to: wire the composition-root `ShareService` adapter (a `*share.Service` + `*share.Store` + `objectstore.Store` triple, exactly the shape `internal/agui/share_api_test.go`'s `shareTestAdapter` already proves out), call `Server.SetShareService`, set `ServerConfig.SharePublicEnabled` from `config.ShareConfig.PublicEnabled`, mount `RequireAuth` whole-origin, mount `RequireCapability(share.public)` in front of the mint route, and add `/s/...` to the `isPublicShareRoute` unauthenticated allowlist (and confirm `/api/shares/...` is NOT admitted by it — `TestShareInternalAnonymous401` already proves the handler-level half of that; 37F-12 proves the mount-level half).
- The frontend (plans 37F-15/16/17 `shareApi.ts`/`shareTypes.ts`/`SharePage.tsx`/`main.tsx`) was written against this plan's locked route table sight-unseen and should now work end-to-end against a live server once 37F-12 lands — this plan's `shareLinkResponse` wire shape was built by cross-checking that exact frontend code, not the other way around.
- Remaining phase 37F plans per the last STATE.md position: 11, 12, 13, 17, 18, 19.

---
*Phase: 37F-conversation-artifact-sharing-export-inserted*
*Completed: 2026-07-17*

## Self-Check: PASSED

- FOUND: internal/agui/share_service.go
- FOUND: internal/agui/share_api.go
- FOUND: internal/agui/share_api_internal.go
- FOUND: internal/agui/share_api_public.go
- FOUND: internal/agui/share_api_test.go
- FOUND: internal/agui/server.go
- FOUND: commit 4a016d2c (Task 1)
- FOUND: commit 342d875e (Task 2)
- FOUND: commit 882e8997 (Task 3)
- Re-ran plan-level `<verification>`: `go build ./...` clean, `go vet -tags db_integration ./internal/agui/` clean, `golangci-lint run --build-tags db_integration ./internal/agui/...` 0 issues, `bash scripts/check-file-size.sh` clean (all tracked files ≤600 LOC), untagged `go test ./internal/agui/ -count=1` ok, `go test -tags db_integration -race -p 1 -count=1 ./internal/agui/` ok (27.2s, full package, WSL), `go test -tags db_integration -cover -p 1 -count=1 ./internal/agui/` ok, coverage 85.6-85.7% (≥85% floor, measured twice).
- Re-ran all 19 named-plus-added `TestShare*` acceptance-criteria assertions live: all PASS (6.1s, non-sub-second).
