---
phase: 36-multi-user-identity-isolation-authula-cutover
plan: 10
subsystem: ui
tags: [agui, capability-grants, audit, react, react-query, rls, admin]

# Dependency graph
requires:
  - phase: 36-01
    provides: "migration 0026 local admin caps (governance.write/identity.create/agent.run seeded on `local`)"
  - phase: 36-02
    provides: "0031 identity-keyed audit indexes (mcp_audit.actor_identity_id, skill_audit.identity_id)"
  - phase: 36-04
    provides: "conversations owner RLS (0032) + agui.localIdentityID/scopedIdentityID seam"
provides:
  - "GET /api/me — self-scoped principal capability payload the SPA reads to gate admin surfaces (D-03)"
  - "GET /api/admin/audit — admin-gated per-user activity feed over the 3 identity-keyed ledgers (D-28)"
  - "POST/DELETE capability grant/revoke through the validated identity.Store seam (D-26)"
  - "SPA admin/user distinction: hidden Settings + Governance surfaces + not-authorized guard for non-admins"
  - "internal/webui/dist rebuilt (this plan is the sole dist owner in Phase 36)"
affects: [36-12]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "raw-pgx read store in agui (PgAuditStore) mirroring objectstore/identity_store.go — no sqlc regen"
    - "capability-gated SPA nav via an optional modes prop threaded from AppShell (backward-compatible default)"
    - "React Query useCapabilities() failing closed (isAdmin=false while loading/on error)"

key-files:
  created:
    - internal/agui/audit_store.go
    - internal/agui/audit_api.go
    - internal/agui/audit_api_test.go
    - internal/agui/audit_store_integration_test.go
    - cmd/aura/serve_webui_musr.go
    - web/src/admin/adminApi.ts
    - web/src/admin/useAdmin.ts
    - web/src/audit/AdminAuditView.tsx
    - web/src/settings/CapabilityAdminPanel.tsx
    - web/src/i18n/resources.admin.ts
  modified:
    - internal/agui/server.go
    - cmd/aura/serve_webui.go
    - cmd/aura/serve.go
    - web/src/AppShell.tsx
    - web/src/settings/SettingsWorkspace.tsx
    - web/src/shell/modes.ts
    - internal/webui/dist

key-decisions:
  - "Reuse governance.write for the settings write-gate (RESEARCH OQ3) — NO net-new settings.model.write"
  - "SPA gates BOTH settings + governance as ADMIN_MODES (same governance-capability admin class)"
  - "Capability mutations are audited via structured slog, not a new DB table (avoids out-of-scope migration / Rule 4)"

patterns-established:
  - "PgAuditStore: raw-pgx UNION over mcp_audit/skill_audit/tool_invocations→conversations, newest-first + pagination + local-alias key handling"
  - "Admin route mounts live in serve_webui_musr.go to keep serve_webui.go under the 600-LOC ceiling"

requirements-completed: []  # MUSR-01 is phase-spanning; the admin-audit-UI slice ships here but the requirement closes at 36-12 (flip + two-identity E2E). requirements mark-complete intentionally NOT run (matches 36-01..07 discipline).

coverage:
  - id: D1
    description: "Admin per-user audit read API + cross-identity isolation (audit_api.go handler + audit_store.go UNION over the 3 identity-keyed ledgers, D-28)"
    requirement: "MUSR-01"
    verification:
      - kind: unit
        ref: "internal/agui/audit_api_test.go#TestHandleAdminAuditProjectsAndSanitizes"
        status: pass
      - kind: integration
        ref: "internal/agui/audit_store_integration_test.go#TestPgAuditStoreListActivityForIdentity (db_integration)"
        status: unknown
    human_judgment: false
    rationale: "Handler + projection + sanitize unit-proven; the live UNION/cross-identity-isolation tier compiles + skips clean here (no CGO/live PG on this Windows host) and MUST run green in WSL/CI."
  - id: D2
    description: "Server-side fail-closed: a non-admin identity is refused (403) on the audit + grant/revoke routes (T-36-10-E)"
    requirement: "MUSR-01"
    verification:
      - kind: unit
        ref: "internal/agui/audit_api_test.go#TestAdminRoutesRefuseNonAdmin"
        status: pass
    human_judgment: false
  - id: D3
    description: "Admin-gated capability grant/revoke through GrantCapability/RevokeCapability (D-26): backend routes + SPA control"
    requirement: "MUSR-01"
    verification:
      - kind: unit
        ref: "internal/agui/audit_api_test.go#TestGrantCapabilityCallsStoreAndReturnsCaps"
        status: pass
      - kind: unit
        ref: "web/src/settings/__tests__/CapabilityAdminPanel.test.tsx"
        status: pass
    human_judgment: false
  - id: D4
    description: "SPA hides the Settings page + admin controls when governance.write is absent (D-03)"
    requirement: "MUSR-01"
    verification:
      - kind: automated_ui
        ref: "web/src/settings/__tests__/SettingsWorkspace.test.tsx#hides the admin controls behind a not-authorized fallback for a non-admin"
        status: pass
      - kind: automated_ui
        ref: "web/src/__tests__/AppShell.shell.test.tsx#hides the admin surfaces from the nav for a non-admin identity"
        status: pass
    human_judgment: false
  - id: D5
    description: "Admin audit UI — per-user activity view over the audit API (D-28)"
    requirement: "MUSR-01"
    verification:
      - kind: automated_ui
        ref: "web/src/audit/__tests__/AdminAuditView.test.tsx"
        status: pass
    human_judgment: true
    rationale: "The audit view's visual/UX fitness (design-system cohesion, feed legibility) is a human-judgment call per CLAUDE.md Frontend_aesthetics; automated tests prove the data wiring only."
  - id: D6
    description: "internal/webui/dist rebuilt from source (no stale bundle)"
    verification:
      - kind: automated_ui
        ref: "scripts/web_dist_freshness.sh (git diff --exit-code clean after npm run build)"
        status: pass
    human_judgment: false

# Metrics
duration: ~95min
completed: 2026-07-05
status: complete
---

# Phase 36 Plan 10: Admin/User Distinction UI Surface Summary

**Admin-gated per-user audit read API (raw-pgx UNION over mcp_audit/skill_audit/tool_invocations) + capability grant/revoke control + SPA capability-gating that hides the Settings/Governance surfaces from non-admins, all behind the existing governance.write — plus the rebuilt embedded bundle.**

## Performance

- **Duration:** ~95 min
- **Completed:** 2026-07-05
- **Tasks:** 2/2
- **Files modified:** 29 source (+75 rebuilt dist assets)

## Accomplishments

- **Backend (Task 1):** `internal/agui/audit_store.go` (`PgAuditStore`) unions the three identity-keyed audit ledgers into one newest-first, paginated per-identity feed (mcp_audit `actor_identity_id`, skill_audit `identity_id`, tool_invocations joined to `conversations.identity_id`) with `local`-alias key handling; `audit_api.go` adds `GET /api/me` (self-scoped caps), `GET /api/admin/identities`, `GET /api/admin/audit`, and `POST/DELETE` capability grant/revoke, SanitizeString on every user-text field. Mounts live in the new `cmd/aura/serve_webui_musr.go` (the four `/api/admin/*` behind `RequireCapability(governance.write)`, `/api/me` RequireAuth-only) so `serve_webui.go` stays at 543 LOC.
- **Frontend (Task 2):** `useCapabilities()` (React Query over `/api/me`, fails closed) drives `visibleModes()` in `shell/modes.ts` to drop `settings`+`governance` from the nav for a non-admin; `SettingsWorkspace` hard-guards on `governance.write` (not-authorized fallback); `CapabilityAdminPanel` (grant/revoke) + `AdminAuditView` (per-user activity feed + pagination) ship inside the admin surface; the embedded `internal/webui/dist` was rebuilt and committed (freshness gate clean).
- **Security boundary:** proven server-side — a non-admin is 403'd on the admin routes (`TestAdminRoutesRefuseNonAdmin`); the SPA hide is cosmetic on top of the authoritative `RequireCapability` gate.

## Task Commits

1. **Task 1: Admin audit read API + per-user query methods + route mount** — `34a4f883` (feat)
2. **Task 2: SPA capability-gating + admin audit view + grant/revoke control + dist rebuild** — `a1eee958` (feat)

## Files Created/Modified

See frontmatter `key-files`. Highlights:
- `internal/agui/audit_store.go` / `audit_api.go` — the D-28 read store + admin API + `/api/me`.
- `cmd/aura/serve_webui_musr.go` — parent-mux admin mounts (kept out of the 600-LOC `serve_webui.go`).
- `web/src/admin/{adminApi,useAdmin}.ts` — the `/api/me` + `/api/admin/*` data layer + capability hooks.
- `web/src/audit/AdminAuditView.tsx`, `web/src/settings/CapabilityAdminPanel.tsx`, `web/src/settings/SettingsWorkspace.tsx` — the admin surfaces + guard.
- `web/src/shell/modes.ts` + the 3 nav components + `AppShell.tsx` — capability-gated nav.

## Decisions Made

- **Reuse `governance.write`** for the settings write-gate (RESEARCH OQ3) — the settings write-routes were already gated in `serve_webui.go`, so no route change was needed; no net-new `settings.model.write` capability introduced (plan prohibition honored).
- **Gate both `settings` and `governance`** as `ADMIN_MODES`. D-03 names the Settings page; Governance is the same governance-capability admin class (a non-admin holds neither `governance.read` nor `governance.write` this phase), so hiding both is the correct posture and avoids a visible-but-403 tab.
- **Capability mutations audited via structured `slog`**, not a new DB ledger. A dedicated capability-audit table is a Rule-4 architectural change requiring a migration outside this plan's scope (36-01 SUMMARY flagged the grant/revoke audit gap for "a later Phase-36 plan"); the change flows through the single validated `identity.Store` seam and is slog-attributable (actor+target+capability). A durable capability-mutation ledger is a documented follow-up.
- **`SettingsPage.tsx` → `SettingsWorkspace.tsx`:** the plan's `files_modified` named `SettingsPage.tsx`, but the real settings page component is `SettingsWorkspace.tsx`; the admin guard + new sections landed there, with the grant/revoke control extracted to `CapabilityAdminPanel.tsx` and the audit UI to the plan's firm artifact `AdminAuditView.tsx`.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Removed a duplicate `localIdentityID` const that broke the db_integration compile**
- **Found during:** Task 1 (adding the db_integration audit-store test)
- **Issue:** `internal/agui/auth.go:325` (a Phase-36 addition) and `internal/agui/server_integration_test.go:40` both declared `const localIdentityID`, so the whole `db_integration` build of package `agui` failed to compile (`localIdentityID redeclared`). This blocked the new audit-store integration test from ever running green in CI (no-skip-as-green).
- **Fix:** Dropped the now-redundant test-file duplicate (identical value) so the tier shares the one production const.
- **Files modified:** `internal/agui/server_integration_test.go`
- **Verification:** `go vet -tags db_integration ./internal/agui/` clean; untagged `go test ./internal/agui/` still green.
- **Committed in:** `34a4f883`

**2. [Rule 2 - Missing Critical] Added `GET /api/me` + composition-root wiring beyond `files_modified`**
- **Found during:** Task 1/2 (the SPA needs the current principal's capabilities to gate admin surfaces; no such endpoint existed — Phase 24-29 was single-operator with `*`)
- **Issue:** Without a self-scoped capabilities payload the frontend cannot hide admin surfaces (must_have truth 1 unachievable).
- **Fix:** Added `GET /api/me` (self-scoped, RequireAuth-only) + `SetAuditStore`/`SetIdentityAdmin` wiring in `cmd/aura/serve.go`, plus the shell components (`modes.ts`, `ModeSwitcher`/`ModeTabBar`/`MobileAppSidebar`/`ShellHeader`/`BottomDock`, `AppShell`) and `web/src/admin/*` needed for the gate. All additive; the nav components take an optional `modes` prop defaulting to the full list so existing shell tests are untouched.
- **Verification:** unit + web tests; `TestHandleMeReturnsCallerCapabilities`, AppShell non-admin nav test.
- **Committed in:** `34a4f883` (backend), `a1eee958` (frontend)

---

**Total deviations:** 2 (1 blocking compile fix, 1 missing-critical additive). **Impact:** both essential; no scope creep beyond the admin/user-distinction the plan targets.

## Issues Encountered

- **`exactOptionalPropertyTypes` + `noUnusedParameters` + `no-non-null-assertion`:** the frontend toolchain is strict on three axes at once. Resolved by making the optional `modes` prop explicitly `| undefined`, and by rewriting mock-call inspection in tests to the accumulator pattern (capture request info inside the mock body) instead of indexing `mock.calls` tuples. Full `tsc --noEmit`, `eslint --max-warnings=0`, and the ≥85% coverage gate all pass.

## No-Skip-As-Green Status

- **Ran green here (Windows):** `go build ./...`, `go vet ./internal/agui/ ./cmd/aura/` (untagged + `-tags db_integration` compile), untagged `go test ./internal/agui/`; web `tsc --noEmit`, `eslint --max-warnings=0`, `vitest run --coverage` (85.53% branches / 91.31% statements / 91.57% functions / 93.18% lines — all ≥85%), `npm run build`, `web_dist_freshness` (git diff clean).
- **NOT run here (must run in WSL/CI before phase close):** `go test -race` (no CGO/gcc on this host) and the `db_integration` tier `TestPgAuditStoreListActivityForIdentity` (no live Postgres) — both compile-clean + skip-clean; honestly `unknown`.

## Next Phase Readiness

- The admin/user distinction UI + audit surface are complete. MUSR-01 stays open (phase-spanning): the provisioning saga (36-08), the `AURA_MUSR_ISOLATION` flip, and the two-identity live E2E close at **36-12**.
- 36-03's note to wire `ShellPoll.Caps`/`ShellKill.Caps` (D-18 admin cross-session recovery) to `governance.write` is still open — this plan established the `HasCapability(governance.write)` admin seam that path will consult.

## Self-Check: PASSED

- All 11 claimed created files exist on disk (incl. this SUMMARY).
- Both task commits present in git history: `34a4f883`, `a1eee958`.
- `internal/webui/dist` rebuilt (new `useAdmin-*.js` chunk present); `git diff` on dist is clean (freshness gate green).

---
*Phase: 36-multi-user-identity-isolation-authula-cutover*
*Completed: 2026-07-05*
