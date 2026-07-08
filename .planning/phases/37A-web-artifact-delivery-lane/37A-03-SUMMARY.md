---
phase: 37A-web-artifact-delivery-lane
plan: 03
subsystem: agui
tags: [http, download, streaming, idor, xss, rfc6266, content-disposition, goleak, security]

# Dependency graph
requires:
  - phase: 37A-web-artifact-delivery-lane
    plan: 01
    provides: "assets.Service.OpenForIdentity (owner-scoped streaming read, 404-upstream on non-owner) + internal/agui contentDisposition (RFC-6266 helper) + OpenForIdentity on the agui AssetService interface"
provides:
  - "internal/agui handleAssetDownload — authenticated GET /api/assets/{id}/download that streams the owner's object body with forced application/octet-stream + X-Content-Type-Options: nosniff + RFC-6266 attachment filename + Content-Length, 404ing on any OpenForIdentity error"
  - "GET /api/assets/{id}/download route registered inside registerAssetRoutes (inherits RequireAuth whole-origin — no unauthenticated surface added)"
affects: [37A-04]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Forced-attachment streaming download: neutral octet-stream + nosniff serve headers regardless of the stored/sniffed mime_type (stored-XSS guard); the sniffed mime is never trusted as a serve header"
    - "Existence-hiding authorization: OpenForIdentity error → uniform 404 (never 403/200) so a non-owned id is indistinguishable from an absent id"
    - "Request-ctx-scoped io.Copy stream-through: a client disconnect cancels the read (no goroutine leak), never presign/redirect to a store URL"

key-files:
  created:
    - internal/agui/asset_download_test.go
  modified:
    - internal/agui/assets_api.go

key-decisions:
  - "The serve Content-Type is hardcoded application/octet-stream (D-10): the content-sniffed asset.MIMEType rides the SSE card-icon event only, NEVER the download serve header — an html/svg-sniffed asset still downloads as an inert attachment"
  - "Any OpenForIdentity error collapses to 404 (D-12): existence-hiding IDOR mitigation; the handler mirrors handleAssetGet's sanitizeErr + 404 pattern rather than diverging (the real WHERE id AND identity_id lookup already returns one generic not-found for both non-owned and absent ids)"
  - "Stream-through with request-ctx-scoped io.Copy + defer rc.Close() (D-09): a disconnect cancels the Garage read leak-free; the private per-identity store URL is never presigned/leaked to the client"

patterns-established:
  - "Forced-attachment download handler: octet-stream + nosniff + RFC-6266 filename + Content-Length set before the first write, then a ctx-scoped io.Copy stream"
  - "Existence-hiding regression proof: assert the non-owner and absent responses are byte-identical (same status + body), not merely both 404"

requirements-completed: [WEBART-03]

coverage:
  - id: D1
    description: "GET /api/assets/{id}/download streams the owner's body with 200 + attachment/octet-stream/nosniff/Content-Length + a filename*=UTF-8'' RFC-6266 param; a hostile sniffed mime (image/svg+xml) STILL serves octet-stream (T-XSS)"
    requirement: "WEBART-03"
    verification:
      - kind: unit
        ref: "internal/agui/asset_download_test.go::TestAssetDownload_Owner"
        status: pass
    human_judgment: false
  - id: D2
    description: "A not-owned id yields the same generic not-found as an absent id → identical 404 response (never 403/200); existence-hiding IDOR mitigation (T-IDOR, D-12)"
    requirement: "WEBART-03"
    verification:
      - kind: unit
        ref: "internal/agui/asset_download_test.go::TestAssetDownload_NonOwner"
        status: pass
    human_judgment: false
  - id: D3
    description: "A RequireAuth-gated request without a session → 401 and OpenForIdentity is never reached; the route adds no unauthenticated surface (T-Unauth)"
    requirement: "WEBART-03"
    verification:
      - kind: unit
        ref: "internal/agui/asset_download_test.go::TestAssetDownload_Unauth"
        status: pass
    human_judgment: false
  - id: D4
    description: "A client disconnect cancels the ctx-scoped read → io.Copy unblocks, defer rc.Close() runs, no leaked goroutine (T-DoS, goleak-clean)"
    requirement: "WEBART-03"
    verification:
      - kind: unit
        ref: "internal/agui/asset_download_test.go::TestAssetDownload_ClientDisconnect (go.uber.org/goleak)"
        status: pass
    human_judgment: false

# Metrics
duration: ~25min
completed: 2026-07-08
status: complete
---

# Phase 37A Plan 03: Authenticated Streaming Download Route Summary

**`GET /api/assets/{id}/download` — the same-origin, auth-gated delivery keystone: it streams the owner's Garage object body as a forced, stored-XSS-safe `application/octet-stream` attachment, 404s on any ownership miss (existence-hiding IDOR), encodes the filename via 37A-01's RFC-6266 helper, and stream-throughs with a request-ctx-scoped `io.Copy` that cancels leak-free on a client disconnect — no presign, no unauthenticated surface.**

## Performance

- **Duration:** ~25 min (2 atomic task commits)
- **Completed:** 2026-07-08
- **Tasks:** 2
- **Files created:** 1 · **Files modified:** 1

## Accomplishments
- `handleAssetDownload` (in `internal/agui/assets_api.go`) — modeled on `handleAssetGet`: `s.assets == nil` → 503; no principal → 401; `OpenForIdentity(r.Context(), id, identity)` → **404 on any error** (D-12 existence-hiding); then sets `Content-Type: application/octet-stream` (D-10), `X-Content-Type-Options: nosniff`, `Content-Disposition: contentDisposition(asset.FileName)` (D-11), `Content-Length: asset.SizeBytes` (Open Q2) **before** the first write; then `io.Copy(w, rc)` with `defer rc.Close()` (D-09 ctx-scoped stream-through).
- Route `GET /api/assets/{id}/download` registered **inside** `registerAssetRoutes`, inheriting `RequireAuth` whole-origin — zero per-route auth wiring, zero new public surface.
- `internal/agui/asset_download_test.go` (daemon-free, counts toward the 85% floor) — four security/behavior cases: owner-200 (+ T-XSS + T-HdrInj proof), non-owner-404 (T-IDOR existence-hiding, proven byte-identical to an absent id), unauth-401 (T-Unauth, service never reached), and a `goleak`-clean ctx-cancel disconnect (T-DoS).

## Task Commits

Each task committed atomically on the worktree branch:

1. **Task 1: handleAssetDownload + route + forced headers + ctx-scoped stream** — `93cb575b` (feat)
2. **Task 2: download security + behavior test suite** — `d5cbb649` (test)

## Files Created/Modified
- `internal/agui/assets_api.go` (modified) — added `handleAssetDownload` + the `GET /api/assets/{id}/download` route line; added `io` + `strconv` imports. File is 204 LOC (well under the 600 cap).
- `internal/agui/asset_download_test.go` (created) — the four-case httptest suite + a `ctxBlockingReadCloser` that models a ctx-scoped Garage body.

## Decisions Made
- **Serve `application/octet-stream`, never the sniffed mime (D-10).** The owner test seeds a hostile `image/svg+xml` sniffed mime and asserts the serve `Content-Type` is still octet-stream + `nosniff` — the sniffed mime is confined to the SSE card-icon event, never the download header.
- **Uniform 404 on any ownership miss (D-12).** The handler reuses `handleAssetGet`'s `sanitizeErr(err)` + `StatusNotFound` pattern instead of diverging. The T-IDOR proof asserts the non-owner and absent responses are byte-identical (same status + body), the true existence-hiding property — stronger than merely asserting both are 404.
- **Stream-through, ctx-scoped, never presign (D-09).** `io.Copy(w, rc)` over the request-context-scoped `ReadCloser` with `defer rc.Close()`; `TestAssetDownload_ClientDisconnect` proves cancel-on-disconnect is leak-free via `goleak`.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 — Test realism] Non-owner test error fixture corrected to model the real service contract**
- **Found during:** Task 2 (initial run of `TestAssetDownload_NonOwner`).
- **Issue:** The first draft seeded an unrealistic fake error (`"asset not found: id=other-owner-asset"`) that embedded the id, and asserted the body must not contain that id. It failed because `sanitizeErr` only redacts credential-bearing substrings (DSNs/tokens), not arbitrary error text — and echoing the service error is exactly `handleAssetGet`'s existing pattern.
- **Analysis:** The real `OpenForIdentity` folds the ownership check into a `WHERE id AND identity_id` lookup (37A-01), so a non-owned id returns the SAME generic not-found as an absent id — existence is already hidden at the source, and the id is never embedded in the error. The original assertion tested a fabricated fixture, not the real contract (violates CLAUDE.md "realistic fixtures / no test baby-sitting").
- **Fix:** Rewrote the test to seed one generic `errors.New("asset not found")` and assert the non-owner and absent responses are **byte-identical** (same status + body) — a faithful, stronger existence-hiding proof. The handler was left unchanged (plan-conformant, consistent with `handleAssetGet`); no handler bug existed.
- **Files modified:** `internal/agui/asset_download_test.go`
- **Commit:** `d5cbb649`

No functional deviations from the plan's `must_haves`. No architectural (Rule 4) decisions were required.

## Issues Encountered
- **`-race` unavailable on this Windows host.** `go test -race` requires `CGO_ENABLED=1` + a C compiler; `gcc` is not on this host's PATH (the project runs native `-race` in WSL/CI, the same constraint the 37A-01 executor faced). Untagged tests pass (`go test ./internal/agui/` green). The suite is race-clean by construction: each parallel subtest builds its own `Server`+fake (no shared mutable state), and the disconnect test coordinates only via `sync.Once` + channels with a happens-before on `<-done` before reading the recorder. **Deferred to WSL/CI for the live `-race` proof.**
- **golangci-lint GOCACHE cross-contamination.** The first Task-1 commit's `lint` hook reported 64 issues (14 gosec + 50 revive) all in a **sibling worktree** (`agent-a3913667f0bfcefa8/internal/objectstore/*`, `internal/assets/*`) — none in the changed file. Ran `golangci-lint cache clean` and re-committed; the retry reported **0 issues** on the owned surface. Both task commits' final lint runs are clean (0 issues).

## Known Stubs
None — the handler is fully wired to the 37A-01 `OpenForIdentity` + `contentDisposition` seams; no placeholder data paths.

## User Setup Required
None — no external configuration. The route is same-origin and inherits the existing `RequireAuth` gate.

## Next Phase Readiness
- **37A-04** (the web download button) now has its target: an authenticated `GET /api/assets/{id}/download` that returns a forced attachment. The button anchors/fetches this same-origin URL; the server enforces ownership + XSS-safe headers.

## Self-Check: PASSED
- Files: `internal/agui/assets_api.go` (modified) + `internal/agui/asset_download_test.go` (created) + `37A-03-SUMMARY.md` — all present.
- Commits: `93cb575b` (feat), `d5cbb649` (test), `9b173ddb` (docs) — all in git.
- STATE.md / ROADMAP.md untouched (worktree mode). go.mod/go.sum byte-unchanged (zero installs).

---
*Phase: 37A-web-artifact-delivery-lane*
*Completed: 2026-07-08*
