---
phase: 37F-conversation-artifact-sharing-export-inserted
plan: 09
subsystem: api
tags: [export, markdown, json, identity-scoped, share, agui, conversations, webshare]

# Dependency graph
requires:
  - phase: 37F-06
    provides: "share.BuildSnapshot + Snapshot.Markdown()/JSON() — the redaction-safe format adapters this endpoint calls verbatim"
provides:
  - "GET /api/conversations/{id}/export?format=md|json — the WEBSHARE-01 identity-scoped conversation export endpoint"
  - "internal/agui/share_export.go: handleConversationExport + exportFilenameStem"
  - "One route line in registerConversationRoutes with zero cmd/aura/serve_webui.go delta (F-1)"
affects: [37F-10, 37F-19]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Owner-gate-before-unscoped-read (GetForIdentity then LoadHistory), mirroring handleMessages/handleAssetDownload's existing shape"
    - "One BuildSnapshot call feeding two format adapters — never a second serializer"
    - "Neutral octet-stream + nosniff for both export formats (37A D-10), never a format-specific MIME"

key-files:
  created: [internal/agui/share_export.go, internal/agui/share_export_test.go]
  modified: [internal/agui/conversations_api.go]

key-decisions:
  - "Zero cmd/aura/serve_webui.go delta (F-1): the export route rides the already-mounted conversationsRoutePrefix subtree, inheriting RequireAuth whole-origin"
  - "Both formats serve as application/octet-stream + X-Content-Type-Options: nosniff, never text/markdown or application/json"
  - "Absent/unrecognized format defaults to Markdown rather than 400/500 — a missing optional query param is not a client error"
  - "ANY GetForIdentity error (foreign owner, absent, malformed id) collapses to the same 404 — never 403, so a read cannot confirm foreign existence"

patterns-established:
  - "exportFilenameStem: a unicode-aware slugify helper for download filenames; safety is delegated entirely to contentDisposition, this only improves readability"

requirements-completed: [WEBSHARE-01]

# Coverage metadata (#1602)
coverage:
  - id: D1
    description: "Owner-scoped GET /api/conversations/{id}/export downloads Markdown or JSON, both derived from one BuildSnapshot call, with the correct attachment headers"
    requirement: "WEBSHARE-01"
    verification:
      - kind: integration
        ref: "internal/agui/share_export_test.go#TestShareExportMarkdown"
        status: pass
      - kind: integration
        ref: "internal/agui/share_export_test.go#TestShareExportJSON"
        status: pass
    human_judgment: false
  - id: D2
    description: "Exporting a foreign conversation returns 404 without leaking the title — never 403, never 200 (SC4 row 1)"
    requirement: "WEBSHARE-01"
    verification:
      - kind: integration
        ref: "internal/agui/share_export_test.go#TestShareExportForeignConversation404"
        status: pass
    human_judgment: false
  - id: D3
    description: "An absent or unrecognized format falls back to the Markdown body rather than 400/500ing"
    requirement: "WEBSHARE-01"
    verification:
      - kind: integration
        ref: "internal/agui/share_export_test.go#TestShareExportFormatFallback"
        status: pass
    human_judgment: false
  - id: D4
    description: "Host filesystem paths (a send_file tool call's staged path, a tool-result's shell output) never reach either exported format's bytes — asserted end-to-end through the HTTP surface"
    requirement: "WEBSHARE-01"
    verification:
      - kind: integration
        ref: "internal/agui/share_export_test.go#TestShareExportRedactsHostPaths"
        status: pass
    human_judgment: false
  - id: D5
    description: "An unauthenticated request is rejected 401 by the handler's own principalIdentityID gate"
    requirement: "WEBSHARE-01"
    verification:
      - kind: integration
        ref: "internal/agui/share_export_test.go#TestShareExportUnauthenticated"
        status: pass
    human_judgment: false
  - id: D6
    description: "The route requires zero cmd/aura/serve_webui.go changes (F-1) and registers exactly one route line"
    requirement: "WEBSHARE-01"
    verification:
      - kind: other
        ref: "git diff --name-only 0df0f007~1..HEAD -- cmd/aura/serve_webui.go (empty output)"
        status: pass
      - kind: other
        ref: "grep -c 'conversations/{id}/export' internal/agui/conversations_api.go == 1"
        status: pass
    human_judgment: false

# Metrics
duration: 36min
completed: 2026-07-17
status: complete
---

# Phase 37F Plan 09: Identity-Scoped Conversation Export (WEBSHARE-01) Summary

**`GET /api/conversations/{id}/export?format=md|json` — owner-gated Markdown/JSON download built on one `share.BuildSnapshot` call, requiring zero `cmd/aura/serve_webui.go` changes.**

## Performance

- **Duration:** 36 min
- **Started:** 2026-07-17T17:40:00Z
- **Completed:** 2026-07-17T18:16:00Z
- **Tasks:** 2
- **Files modified:** 3 (2 created, 1 modified) + 1 phase-tracking doc (`deferred-items.md`)

## Accomplishments

- `internal/agui/share_export.go` — `handleConversationExport`: gates ownership via `GetForIdentity` before any history read (ANY error → 404, never 403), then `LoadHistory` + `ListForThread` + `share.BundleFilter`, then exactly one `share.BuildSnapshot` call feeding either `Snapshot.Markdown()` or `Snapshot.JSON()`.
- Both formats serve as a neutral `application/octet-stream` attachment with `X-Content-Type-Options: nosniff` (37A D-10 stored-XSS guard) and a `contentDisposition`-escaped filename (D-11) — never a hand-concatenated header.
- An absent or unrecognized `?format=` value degrades to Markdown rather than 400/500ing.
- Exactly one new route line in `registerConversationRoutes`; **zero** lines changed in `cmd/aura/serve_webui.go` (F-1) — the route inherits `RequireAuth` whole-origin from the already-mounted `conversationsRoutePrefix` subtree.
- Six live `db_integration` tests proving the full behavior set: Markdown happy path, JSON round-trip, cross-identity 404 without a title leak, format fallback, end-to-end host-path redaction (both formats), and unauthenticated 401.

## Task Commits

Each task was committed atomically:

1. **Task 1: handleConversationExport — one snapshot, two formats, owner-gated** - `0df0f007` (feat)
2. **Task 2: export integration tests — MD, JSON, foreign 404, format fallback** - `d505ed7f` (test)

A follow-up docs commit records an environmental DB-state repair performed during verification (see Issues Encountered): `80c1fb6b` (docs).

**Plan metadata:** (this commit)

## Files Created/Modified

- `internal/agui/share_export.go` (148 LOC) - `handleConversationExport` + `exportFilenameStem`, the WEBSHARE-01 export endpoint
- `internal/agui/conversations_api.go` (411 LOC, +5) - one new route registration line + a 3-line comment
- `internal/agui/share_export_test.go` (287 LOC) - the 6 named `db_integration` tests + seed/request helpers
- `.planning/phases/37F-conversation-artifact-sharing-export-inserted/deferred-items.md` - logged + resolved a recurrence of the pre-existing wiped-`local`-identity issue 37F-07 first documented

## Decisions Made

- **F-1 held exactly as RESEARCH/PATTERNS predicted:** `conversationsRoutePrefix` was still mounted at `serve_webui.go:381` at execution time (re-verified per the task's own `read_first` instruction before writing any code); the export route needed only one `mux.HandleFunc` line.
- **Error-status shape for post-gate failures:** `LoadHistory`/`ListForThread`/`BuildSnapshot`/`JSON()` failures after the owner gate map to 500 (mirroring `handleMessages`'s existing `LoadHistory` error handling), not 404 — only the owner gate itself collapses every error to 404. The plan's acceptance criteria only constrain the owner-gate behavior and forbid 403; this choice is consistent with the codebase's existing `handleAssetList`/`handleMessages` precedent.
- **`exportFilenameStem` is a readability aid, not a security control:** it keeps unicode letters/digits (lowercased) and collapses everything else to a single hyphen; `contentDisposition` (already tested, unchanged) remains the sole header-injection/traversal safety boundary.
- **`WEBSHARE-01` was already checked `[x]` in REQUIREMENTS.md** (set prematurely by `37F-06`'s commit `1f1be990`, before the identity-scoped endpoint the requirement's own text names actually existed). This plan is what makes that checkbox factually true; `requirements mark-complete WEBSHARE-01` was still run as instructed by the plan frontmatter — idempotent, and now accurate.

## Deviations from Plan

None - plan executed exactly as written for the code deliverable (`share_export.go`, `conversations_api.go`, `share_export_test.go` — all three `files_modified` entries, no others touched).

## Issues Encountered

**Pre-existing, unrelated DB-state issue surfaced during full-package verification (not caused by this plan):** running the plan's own full plan-level verification command (`go test -tags db_integration -race -p 1 -count=1 ./internal/agui/`, i.e. the whole package, not just the 6 `TestShareExport*` tests) initially showed ~15 unrelated failures, all `FK 23503` on `conversations_identity_id_fkey` — the seeded `local` identity (`00000000-0000-0000-0000-000000000001`) was absent from the shared live `aura` database. This is the exact same class of issue `37F-07`'s `deferred-items.md` entry already documented and had once repaired; it recurred (evidently re-wiped by other work against the same shared dev Postgres since then).

This plan's own scoped tests were unaffected throughout: `go test -tags db_integration -race -p 1 -count=1 -run 'TestShareExport' ./internal/agui/` passed 6/6 cleanly (0.11s–0.33s each) even in the broken DB state, precisely because `share_export_test.go` follows the R-13 discipline (`seedShareExportIdentity` mints a fresh, non-wildcard identity per test, never depending on `local`).

Mirroring `37F-07`'s own precedent (treated there as "environmental data-repair, not a code change"), re-applied migration `0004`'s exact idempotent seed directly against the live DB via `docker exec aura-postgres psql` (local-socket trust auth, no `.env` password read or displayed): the `local` identity row and its `*` capability grant, both `ON CONFLICT DO NOTHING`. Verified absent before (0 rows) and present after (1 row each). Re-ran the full verification command: all ~15 FK failures cleared. The one remaining failure, `TestHandleCheckTelegramAvailabilityBranches/no_token_configured_reports_not-configured`, is the SAME pre-existing flake `37F-07` already documented (a `TELEGRAM_BOT_TOKEN`-shaped env leak, zero file overlap with this plan) — confirmed here to be genuinely independent of the identity-seed issue since it persists with `local` correctly seeded. See `deferred-items.md`'s `37F-09` entry for full detail; the identity-wipe recurrence itself is logged there for whoever picks up the shared dev-Postgres isolation problem.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

- `GET /api/conversations/{id}/export` is live and identity-scoped; `37F-10` (the public share handler) can now point to this plan's doc-block contrast (its own doc must say the *opposite* about auth — this route inherits `RequireAuth`, the public one deliberately does not).
- `WEBSHARE-01` is genuinely complete (endpoint + both formats + redaction proven end-to-end), not just checked off.
- No blockers for the remaining 37F plans (`37F-10` through `37F-20`).
- Known, tracked, out-of-scope environmental item: the shared dev Postgres's `local` identity seed keeps getting wiped by unrelated work in the same environment (now re-seeded); see `deferred-items.md`.

---
*Phase: 37F-conversation-artifact-sharing-export-inserted*
*Completed: 2026-07-17*

## Self-Check: PASSED

- FOUND: internal/agui/share_export.go
- FOUND: internal/agui/share_export_test.go
- FOUND: .planning/phases/37F-conversation-artifact-sharing-export-inserted/37F-09-SUMMARY.md
- FOUND: 0df0f007 (Task 1 commit)
- FOUND: d505ed7f (Task 2 commit)
- FOUND: 80c1fb6b (deferred-items.md accuracy follow-up commit)
- Re-ran all task-level acceptance criteria (F-1 diff guard, route-count grep, header/BuildSnapshot/403 greps, `go build`/`go vet`/`golangci-lint` on both untagged and `db_integration`-tagged builds) — all pass.
- Re-ran the plan-level `<verification>` block in full, live: `go build ./...`, `go vet ./internal/agui/`, `go test ./internal/agui/ -count=1`, `go test -tags db_integration -race -p 1 -count=1 ./internal/agui/` (all 6 `TestShareExport*` plus the rest of the package, post local-identity re-seed), `golangci-lint run ./internal/agui/` (0 issues), the F-1 `git diff` guard, and `bash scripts/check-file-size.sh` — all pass except the pre-existing, independently-documented `TestHandleCheckTelegramAvailabilityBranches` flake (37F-07, zero overlap with this plan's files).
