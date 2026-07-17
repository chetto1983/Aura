---
phase: 37F-conversation-artifact-sharing-export-inserted
plan: 12
subsystem: api
tags: [go, net-http, servemux, capability-grants, share, routing, auth]

# Dependency graph
requires:
  - phase: 37F-10
    provides: share_api.go / share_api_internal.go / share_api_public.go (the eight share-lifecycle handlers + Server.registerShareRoutes)
provides:
  - cmd/aura/serve_webui_share.go — sharePublicCapability const, fail-closed isPublicShareRoute predicate, registerShareRoutes(mux, aguiHandler, auth) mount table
  - serve_webui.go's registerShareRoutes call + isPublicShareRoute entry in the PublicRoute chain (598/600 LOC)
  - the /s/ public-route allowlist, proven by test in both cmd/aura and internal/agui
affects: [37F-13, 37F-18, 37F-19]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Bare-mount + in-handler gate for dual-tier single routes: a mux entry that serves two authorization tiers via request BODY (not method/path) cannot carry a mux-level RequireCapability wrap — the differentiation has to live in-handler."
    - "Two-file predicate proof pattern: a package-main predicate (route allowlist logic) gets a direct unit test in cmd/aura (proves the function) PLUS a coverage-measured pair in internal/agui driving the same cases through the real RequireAuth chain with an equivalent closure (proves the property that matters, since cmd/aura contributes zero coverage at any tag)."

key-files:
  created:
    - cmd/aura/serve_webui_share.go
    - cmd/aura/share_public_route_test.go
    - internal/agui/share_public_route_test.go
  modified:
    - cmd/aura/serve_webui.go
    - .planning/phases/37F-conversation-artifact-sharing-export-inserted/deferred-items.md

key-decisions:
  - "Mounted all eight share routes bare (no RequireCapability wrap anywhere, including POST /api/shares) per the plan's Task 1/2 action text and machine-checked acceptance criteria — not per the plan's looser must_haves/threat-model prose, which says public minting is 'gated by RequireCapability(share.public) at the mount.' A single POST /api/shares mux entry serves both the internal and public tier via the JSON body; Go's ServeMux dispatches on method+path only, so a tier-specific gate structurally cannot live at the mount. Documented as a known gap (share.public is a declared, grammar-valid constant, but no code path calls HasCapability on it yet) rather than silently expanding scope into internal/agui/share_api.go, which is outside this plan's declared files_modified and the orchestrator's explicit 'commit only this plan's declared files' instruction."

patterns-established:
  - "registerXRoutes(mux, aguiHandler, auth) with auth accepted via blank identifier when every route in the file is bare — matches the established registerComposerRoutes precedent, signature parity for future gated routes."

requirements-completed: [WEBSHARE-02]

coverage:
  - id: D1
    description: "share.public capability const (D-02 semantics, identity.create's sibling) + fail-closed /s/ prefix predicate + eight-route mount table, in a new cmd/aura/serve_webui_share.go kept under the 600-LOC ceiling"
    requirement: "WEBSHARE-02"
    verification:
      - kind: unit
        ref: "cmd/aura/share_public_route_test.go#TestPublicShareRouteAllowlist"
        status: pass
      - kind: unit
        ref: "cmd/aura/share_public_route_test.go#TestSharePublicCapabilityNameValid"
        status: pass
    human_judgment: false
  - id: D2
    description: "Parent-mux wiring in serve_webui.go: one registerShareRoutes call + one isPublicShareRoute entry in the PublicRoute chain (593->598/600 LOC), fallbackExcludedPrefixes left deliberately untouched so /s/{token} falls through to the SPA shell"
    requirement: "WEBSHARE-02"
    verification:
      - kind: unit
        ref: "internal/agui/share_public_route_test.go#TestPublicShareRouteAllowlistThroughRequireAuth"
        status: pass
    human_judgment: false
  - id: D3
    description: "share.public capability enforcement (HasCapability check on public-tier mint) — a genuine gap, NOT closed by this plan"
    verification: []
    human_judgment: true
    rationale: "This plan's declared file scope (cmd/aura/serve_webui_share.go, serve_webui.go, and the two predicate test files) cannot wire a HasCapability check for POST /api/shares' public tier without editing internal/agui/share_api.go, which is out of scope per files_modified and the orchestrator's explicit run-time instruction to commit only this plan's declared files. A human (or 37F-13, whose SC4 row 8 exercises exactly this) must decide whether/where to close it."

# Metrics
duration: 30min
completed: 2026-07-18
status: complete
---

# Phase 37F Plan 12: Parent-Mux Share Route Mount Summary

**Mounted the eight WEBSHARE-02/03 share-lifecycle routes on the parent mux — all bare (no capability wrap, including the public-tier mint) — plus a fail-closed `/s/` allowlist predicate proven in both `cmd/aura` and the coverage-measured `internal/agui`.**

## Performance

- **Duration:** 30 min
- **Started:** 2026-07-17T23:07:15Z (approx, from STATE.md's pre-session `last_updated`)
- **Completed:** 2026-07-17T23:36:52Z
- **Tasks:** 2
- **Files modified:** 4 (+ 1 deviation-tracking doc)

## Accomplishments

- New `cmd/aura/serve_webui_share.go` (125 LOC): `sharePublicCapability = "share.public"` (documented as `identity.create`'s sibling, not `governance.write`'s, citing the bootstrap-`*`-wildcard vs provisioned-named-capability contrast verified at plan time), a fail-closed `isPublicShareRoute(r)` prefix predicate (GET-only, `/s/`-prefixed, default false), and `registerShareRoutes(mux, aguiHandler, auth)` mounting all eight share-lifecycle routes
- `serve_webui.go` gained exactly 5 lines (593 → 598/600 — the objective's own predicted "2-LOC margin"): one `registerShareRoutes(...)` call + a 2-line comment, and one `isPublicShareRoute(r)` entry in the `PublicRoute` chain
- The named non-action honored: `fallbackExcludedPrefixes()` left completely untouched — `/s/` and `/shared/` deliberately stay OUT of it so `/s/{token}` falls through `mux.Handle("/", static)` to the SPA shell instead of 404ing
- Two untagged predicate test files proving the allowlist by execution, not by eye: `cmd/aura/share_public_route_test.go` (13-case table over the direct `isPublicShareRoute` function, plus `TestSharePublicCapabilityNameValid` settling 37F-RESEARCH.md assumption A3) and `internal/agui/share_public_route_test.go` (the coverage-measured pair, driving the same cases through the real `agui.RequireAuth` chain with an equivalent `PublicRoute` closure)
- Both suites assert the tricky negatives explicitly: `/sabotage` (naive prefix match trap), bare `/s` (no trailing slash), `/shared/abc` (the confusable internal-tier neighbour), and the two D-10 `GET /api/shares/{id}/data` / `.../asset/{assetID}` routes (must stay authenticated, never public — this is SC4 row 4's unit-level counterpart)

## Task Commits

Each task was committed atomically:

1. **Task 1: serve_webui_share.go — the capability const, the public predicate, and the mount table** - `db00a2d5` (feat)
2. **Task 2: serve_webui.go — one call + one chain entry, and the /s/ NON-action** - `39ee1485` (feat)

**Plan metadata:** (this commit) — SUMMARY + STATE + ROADMAP

## Files Created/Modified

- `cmd/aura/serve_webui_share.go` - `share.public` capability const, `isPublicShareRoute` predicate, `registerShareRoutes` mount table (125 LOC)
- `cmd/aura/serve_webui.go` - one `registerShareRoutes` call + one `PublicRoute` chain entry (593 → 598 LOC)
- `cmd/aura/share_public_route_test.go` - direct predicate test (13 cases) + capability-grammar test
- `internal/agui/share_public_route_test.go` - the coverage-measured `RequireAuth`-chain pair (13 cases)
- `.planning/phases/37F-conversation-artifact-sharing-export-inserted/deferred-items.md` - logged the `share.public` enforcement gap + an unrelated pre-existing `caddy/Caddyfile` test failure

## Decisions Made

**Mounted all eight share routes bare — no `RequireCapability` wrap anywhere, including `POST /api/shares`'s public-tier mint — following the plan's Task 1/2 action text and machine-checked acceptance criteria over its looser `must_haves`/threat-model prose.** The plan's frontmatter says public minting is "gated by `RequireCapability(share.public)` at the mount," but Task 1's action explicitly instructs "`POST /api/shares` — bare `aguiHandler`, RequireAuth-only... a single route serves both tiers, so the tier-specific gate cannot live at the mount," and Task 1's acceptance criteria literally asserts "`POST /api/shares` is mounted with a bare `aguiHandler`, not wrapped in `RequireCapability`." This is mechanically forced: Go's `http.ServeMux` dispatches on method+path only, never on request body, and `POST /api/shares` handles BOTH the internal and public tier via the JSON `tier` field — there is no way to wrap only the public-tier sub-case at the mux level. Verified against the actual shipped `internal/agui/share_api.go` (37F-10): `handleShareCreate` checks only the org kill-switch (`s.cfg.SharePublicEnabled`), never a capability. Rather than silently expanding scope to add that check (which would require editing `internal/agui/share_api.go`, outside this plan's declared `files_modified` and the orchestrator's explicit "commit only this plan's declared files" instruction), the gap is documented here, in the new file's header comment, and in `deferred-items.md`.

## Deviations from Plan

### Auto-fixed Issues

None — no Rule 1/2/3 auto-fixes were needed; the plan's own Task instructions were followed literally.

### Documented (not auto-fixed) Discrepancy

**1. [Scope boundary — documented, not fixed] `share.public` capability is declared but not yet enforced anywhere**
- **Found during:** Task 1, while writing `sharePublicCapability`'s doc comment and cross-checking the plan's `must_haves` truth "POST /api/shares public minting is gated by RequireCapability(share.public) at the mount" against the actual shipped `internal/agui/share_api.go` (37F-10).
- **Issue:** `handleShareCreate`'s public-tier branch checks only `s.cfg.SharePublicEnabled` (the org kill-switch). No code path anywhere calls `Identities.HasCapability(identityID, "share.public")`. The plan's own Task 1/2 action text and acceptance criteria are unambiguous that `POST /api/shares` mounts bare (see Decisions above) — so this plan cannot close the gap at the mount either, by the same single-route/dual-tier mechanics.
- **Fix:** Not fixed — logged in `serve_webui_share.go`'s header comment ("KNOWN GAP") and in `.planning/phases/37F-conversation-artifact-sharing-export-inserted/deferred-items.md`, per SCOPE BOUNDARY (fix only what the current task's changes directly caused; `internal/agui/share_api.go` is untouched, unmodified, and outside `files_modified`).
- **Files touched:** none beyond the plan's own declared 4 files (deferred-items.md is documentation, not production code).
- **Verification:** `internal/agui/share_api.go` re-read in full; confirmed no `HasCapability` call exists in `handleShareCreate` or `share.Service.Create`.
- **Impact:** 37F-13's SC4 row 8 ("B holds `share.public`; A does NOT; A mints public ⇒ 403") will very likely fail against current shipped code until a capability check lands in `share_api.go` — flagged explicitly for whoever executes 37F-13 next.

**2. [Out of scope, pre-existing — logged to deferred-items.md] `TestProductionContainerArtifactsMatchFatImageContract` fails against `caddy/Caddyfile`**
- **Found during:** post-task regression check (`go test ./cmd/aura/... -count=1`, beyond this plan's own scoped verification).
- **Issue:** `caddy/Caddyfile` is missing an `@authed` matcher the test expects. The file is completely untouched by this plan (confirmed via `git status --short`) — this is a pre-existing failure, most likely connected to the `fix/ci-red-37f-drift` branch + leaked `749a2c54` commit the orchestrating instructions flagged as "the human owns ALL of that reconciliation."
- **Fix:** Not fixed — out of scope (SCOPE BOUNDARY), logged in `deferred-items.md`.
- **Verification:** `go build ./...`, `go vet ./cmd/aura/ ./internal/agui/`, and the full `internal/agui` untagged suite are all green; this is the ONLY failure in a whole-package `cmd/aura` run.

---

**Total deviations:** 0 auto-fixed; 2 documented-and-deferred (1 scope-boundary gap discovered in a dependency's prior-plan code, 1 pre-existing unrelated test failure). **Impact on plan:** Neither required or permitted a change inside this plan's declared file scope. Both are transparently logged for the next plan/human to pick up.

## Issues Encountered

**Plan-internal ambiguity between frontmatter (`must_haves`/`threat_model`) and the Task action/acceptance-criteria text on whether `POST /api/shares` should carry a `RequireCapability(share.public)` mux wrap.** Resolved in favor of the Task action + acceptance criteria (the literal, machine-checked instructions) — see Decisions above for the full reasoning chain. This is not a "problem during planned work" in the traditional sense so much as an internal plan-document inconsistency that required careful textual analysis to resolve correctly without silently expanding scope.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

- The share routes are fully mounted: internal tier ungated (RequireAuth only), D-10 bearer-within-auth tier authenticated-only, `/s/` public tier admitted by the `PublicRoute` chain and falling through to the SPA shell.
- `serve_webui.go` sits at exactly 598/600 LOC — any future addition to this file MUST split into a sibling file first (the established four-times precedent: `_musr.go`/`_voice.go`/`_composer.go`/`_share.go`).
- **Blocker for 37F-13:** SC4 row 8 (public mint without `share.public` ⇒ 403) will very likely fail against current code — the capability is declared but not enforced anywhere. 37F-13's executor should read this SUMMARY's Deviations section before writing that row, and either (a) treat it as an expected/documented current-behavior gap and adjust the row's expectation with a TODO back-reference, or (b) escalate a small gap-closure plan (mirroring 37F-20's precedent) to wire `HasCapability(identityID, "share.public")` into `internal/agui/share_api.go`'s `handleShareCreate` before writing that test.
- Wave 7 (37F-13, depends_on 37F-11 + 37F-12) is now unblocked on this plan's side (37F-11 was already `[x]`).

---
*Phase: 37F-conversation-artifact-sharing-export-inserted*
*Completed: 2026-07-18*

## Self-Check: PASSED

- FOUND: cmd/aura/serve_webui_share.go
- FOUND: cmd/aura/serve_webui.go
- FOUND: cmd/aura/share_public_route_test.go
- FOUND: internal/agui/share_public_route_test.go
- FOUND: .planning/phases/37F-conversation-artifact-sharing-export-inserted/deferred-items.md (updated)
- FOUND commit: db00a2d5 (Task 1)
- FOUND commit: 39ee1485 (Task 2)
- Plan-level `<verification>` re-run: `go build ./...` clean; `go vet ./cmd/aura/ ./internal/agui/` clean; `go test ./internal/agui/ -run 'TestPublicShareRoute|TestSharePublicCapabilityName' -count=1` PASS; `wc -l cmd/aura/serve_webui.go` = 598 (<=598 required); `fallbackExcludedPrefixes()` contains no `/s/` (grep-gated, confirmed); `golangci-lint run ./cmd/aura/ ./internal/agui/` = 0 issues; `bash scripts/check-file-size.sh` exit 0
- ROADMAP.md: 37F-12 checkbox flipped to `[x]`, plans-executed count 16/20 -> 17/20 (confirmed via git diff)
- REQUIREMENTS.md: WEBSHARE-02 already `[x]` from an earlier 37F plan; no change needed (confirmed no diff)
