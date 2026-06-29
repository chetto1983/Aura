---
phase: 29-governance-write-mcp-configuration-skills-install
plan: 05
subsystem: testing
tags: [security-backstops, callgraph, property-testing, append-only, playwright, axe, stryker, coverage, go-embed, secrets-redaction]

# Dependency graph
requires:
  - phase: 29-governance-write-mcp-configuration-skills-install (plan 01)
    provides: the append-only mcp_audit ledger + MCPAuditStore + governance.write const
  - phase: 29-governance-write-mcp-configuration-skills-install (plan 02)
    provides: the six MCP write endpoints + the four-state SetServerEnv merge + the GovernanceWriteProviders seam
  - phase: 29-governance-write-mcp-configuration-skills-install (plan 03)
    provides: the seven skills write endpoints + the operator-origin approval bridge + skill_audit ledger
  - phase: 29-governance-write-mcp-configuration-skills-install (plan 04)
    provides: the cockpit write panels (install/env-edit/lifecycle/skill-install) + the approval card + i18n
provides:
  - "The held-out secret log-scan + response-grep over a FULL MCP install + env-edit + skill install run (zero secret values) — which caught + fixed a real source-echo leak (sanitizeSkillSource at the handler boundary)"
  - "The redacted-placeholder PROPERTY backstop (rapid, ~100 cases): for every secret key, ${KEY} preserves the stored value exactly, a real value rotates"
  - "The no-model-approve backstop: a type-resolved go/packages CallExpr scan over internal/agent/tools/... asserting COUNT==0 edges into Writer.Activate/ResumeHandler.Resume (negative-control verified) + the active-loader-excludes-pending held-out, each passing independently"
  - "Append-only on BOTH ledgers: a new TestSkillAuditAppendOnly (live skill lifecycle install→activate→archive→restore, one row each + UPDATE/DELETE/TRUNCATE → 42501) mirroring the existing MCP TestMCPAuditAppendOnly"
  - "The no-silent-destructive-mount backstop: a denied (blocked) server is absent from RunnableManagedServers/RuntimeServers yet surfaced as StartupBlocked in SnapshotStatus"
  - "The cross-surface auth sweep: every new mutating route 401 unauthenticated + 403 without governance.write (all 13 routes via the production RequireCapability mount)"
  - "The full cockpit-write Playwright e2e + axe (chromium + mobile-chrome), LIVE-verified 6/6 against a freshly-served binary"
  - "Vitest >=85% on web/src/governance (88.7%) + web/src/approvals (97.8%); Stryker 86.4% killed on the new modules; the rebuilt internal/webui/dist (//go:embed fresh)"
affects: [gsd-verify-work, gsd-secure-phase, gsd-complete-milestone]

# Tech tracking
tech-stack:
  added:
    - "golang.org/x/tools v0.44.0 (transitive→direct promotion; the go/packages type-resolved callgraph scan import — no new external module, already in the graph)"
  patterns:
    - "Type-resolved static backstop: load the model-tool packages with go/packages (full TypesInfo), walk every CallExpr, resolve the selector via types.Info, assert COUNT==0 edges into a forbidden method — caught by a negative control, never a string grep"
    - "Held-out secret scan: drive a FULL multi-endpoint run against an httptest server with the slog default handler captured, grep every response + log line for the seeded secret VALUES, assert zero"
    - "Redacted-placeholder PROPERTY (rapid): generate many secret keys, prove the preserve-on-${KEY} invariant for ALL keys, plus a rotate branch so the preserve is not a degenerate always-keep"
    - "Install-source credential redaction at the handler boundary (sanitizeSkillSource): mask userinfo + secret query-params before the source echo crosses the wire"
    - "Head-derived migration round-trip bound (db.Status) so a down-step test never goes stale as new migrations land"

key-files:
  created:
    - internal/agui/governance_write_secret_scan_test.go
    - internal/agui/no_model_approve_test.go
    - internal/agui/governance_write_skills_redact.go
    - internal/agui/governance_write_skills_redact_test.go
    - internal/agui/governance_write_auth_sweep_test.go
    - internal/mcp/manager/envedit_property_test.go
    - internal/mcp/manager/denied_mount_test.go
    - web/e2e/governance-write.spec.ts
    - web/src/governance/__tests__/governanceWrite.coverage.test.tsx
    - web/src/governance/__tests__/McpLifecycleCluster.test.tsx
  modified:
    - internal/agui/governance_write_skills_api.go
    - internal/skills/installer_integration_test.go
    - internal/cron/dispatch_integration_test.go
    - web/src/governance/__tests__/McpBoard.test.tsx
    - web/src/governance/__tests__/SkillsBoard.test.tsx
    - web/src/governance/__tests__/McpServerDetail.test.tsx
    - web/stryker.config.json
    - go.mod
    - internal/webui/dist

key-decisions:
  - "The no-model-approve scan uses golang.org/x/tools/go/packages (promoted transitive→direct) over the plan's stdlib-only fallback: go/importer source-mode re-downloads the toolchain and gc-export mode cannot resolve internal/* by GOPATH, while go/packages loads internal/agent/tools with full TypesInfo cleanly (errs=0, ~2s). The promotion is NOT a package install (x/tools was already in the module graph; no go.sum change)."
  - "The secret-scan caught a REAL leak (Rule 1/2): the skill-install handler echoed the raw `source`, which can carry `?token=` / `user:pass@` credentials. Fixed at the agui handler boundary with sanitizeSkillSource so the fix covers both the fake and the real provider."
  - "TestSkillAuditAppendOnly records restore as the EXISTING 'activate' action (no 'restore' constant — writer_activate.go Restore's load-bearing decision), so the lifecycle asserts install=1, archive=1, activate=2 (activate + restore)."
  - "The cron migration-bound fix is a Rule-3 blocking fix needed to unblock make coverage (the bound was stale at head=0022, an off-by-one), made head-derived so it never re-staleness; logged in deferred-items.md as out-of-scope-but-gate-blocking."
  - "The live Playwright e2e RAN (not faked, not skipped-as-green): a freshly-built WSL binary served on :9099 against the live stack, Playwright run natively via AURA_E2E_ORIGIN — 6/6 on chromium + mobile-chrome with axe clean."

patterns-established:
  - "Type-resolved callgraph/AST backstop with a negative control (inject a real forbidden edge → assert the scan fails) so the COUNT==0 is provably non-vacuous"
  - "Both-ledgers append-only parity: the SAME live-PG UPDATE/DELETE/TRUNCATE→42501 + one-row-per-action shape on mcp_audit AND skill_audit"
  - "Cross-surface auth sweep table: 401-at-handler for the mutating routes + 403-at-mount for ALL routes (incl. the privileged GET), plus a grantee-reaches guard"

requirements-completed: [MCPW-01, MCPW-02, MCPW-03, SKW-01, SKW-02, SKW-03]

# Metrics
duration: ~2h30m
completed: 2026-06-21
---

# Phase 29 Plan 05: Gate-3 close — security backstops + full quality matrix + dist rebuild Summary

**The non-inferable prohibition backstops (held-out secret scan, redacted-placeholder property, type-resolved no-model-approve callgraph, both-ledgers append-only, no-silent-destructive-mount), the cross-surface 401/403 auth sweep, the LIVE-verified full-cockpit Playwright e2e + axe, the frontend gates (Vitest 88.7%/97.8%, Stryker 86.4%), the Go owned-surface gate (make coverage 88.0% + make quality-full green), and the rebuilt single-binary dist — closing Phase-29 to Gate-3.**

## Performance

- **Duration:** ~2h30m
- **Tasks:** 3 (+ 1 blocking Rule-3 fix + 1 e2e assertion fix)
- **Files modified:** 19 (10 created, 9 modified)
- **Gates:** make coverage 88.0% ≥85% (live `db_integration neo4j_integration` matrix); make quality-full green (vet+build+file-size≤600+lint+deadcode+test-race+vuln+coverage); Vitest 799 tests pass, governance 88.7% / approvals 97.8%; Stryker 86.4% killed; contrast 36/36 AA; Playwright 6/6 chromium + mobile-chrome live.

## Accomplishments
- **Task 1 — the backstops (commit `54cf4310`):** the held-out secret scan over a full install+edit+skill-install run (zero secret values) — which CAUGHT a real source-echo leak and fixed it (`sanitizeSkillSource`); the redacted-placeholder rapid property (~100 cases); the type-resolved `go/packages` no-model-approve scan (COUNT==0, negative-control-verified) + the active-loader-excludes-pending held-out (each independent); `TestSkillAuditAppendOnly` (both-ledgers append-only on the live skill lifecycle); and the no-silent-destructive-mount test.
- **Task 2 — frontend gates (commit `c00d4c9e`, e2e fix `6b02734e`):** the full cockpit-write Playwright e2e + axe (chromium + mobile-chrome) — LIVE-RUN 6/6 against a freshly-served binary; Vitest lifted to 88.7% on governance (governanceApi 60→100%, McpLifecycleCluster 17→74%) keeping approvals at 97.8%; Stryker extended to governanceView + approvalState → 86.4% killed; contrast 36/36 AA.
- **Task 3 — the matrix + dist (commits `10c5bcfb`, `9825a28f`, `ab507cf1`):** the cross-surface auth sweep (401 on the 12 mutating routes + 403 on all 13 via the production mount); i18n en↔it parity (green); `make coverage` 88.0% owned-surface on the live stack; `go test -tags db_integration -race` clean per touched package; `make quality-full` green; the rebuilt `internal/webui/dist` committed atomically (single-binary //go:embed fresh).

## Task Commits

1. **Task 1: non-inferable prohibition backstops + source-secret redaction fix** — `54cf4310` (test) — includes the Rule-1/2 `sanitizeSkillSource` fix + the go.mod x/tools promotion
2. **Task 2: frontend gates — governance-write e2e + Vitest ≥85% + Stryker ≥70%** — `c00d4c9e` (test)
3. **Task 3a: cross-surface governance-write auth sweep (401/403)** — `10c5bcfb` (test)
4. **Task 3b: rebuild embedded internal/webui/dist** — `9825a28f` (chore) — committed atomically as its own commit
5. **Rule-3 blocking fix: head-derive the cron migration round-trip bound** — `ab507cf1` (fix) — unblocks make coverage
6. **e2e assertion fix (live-verified): scope colliding-restore to the row** — `6b02734e` (fix)

**Plan metadata:** this SUMMARY + STATE/ROADMAP (docs).

## Files Created/Modified
- `internal/agui/governance_write_secret_scan_test.go` — the held-out secret scan + the wire-side preserve table
- `internal/agui/no_model_approve_test.go` — the go/packages type-resolved COUNT==0 scan + the active-loader-excludes-pending held-out
- `internal/agui/governance_write_skills_redact.go` (+`_test.go`) — `sanitizeSkillSource` (userinfo + secret-query-param redaction at the handler boundary)
- `internal/agui/governance_write_skills_api.go` — applies `sanitizeSkillSource` to the install source echo
- `internal/agui/governance_write_auth_sweep_test.go` — the 401/403 cross-surface sweep over all 13 routes
- `internal/mcp/manager/envedit_property_test.go` — the redacted-placeholder rapid property
- `internal/mcp/manager/denied_mount_test.go` — the no-silent-destructive-mount test
- `internal/skills/installer_integration_test.go` — `TestSkillAuditAppendOnly` (both-ledgers append-only on the live lifecycle)
- `internal/cron/dispatch_integration_test.go` — head-derived migration round-trip bound (Rule-3 blocking fix)
- `web/e2e/governance-write.spec.ts` — the full cockpit-write flow + axe (chromium + mobile-chrome)
- `web/src/governance/__tests__/governanceWrite.coverage.test.tsx` — the write fetch-layer coverage (60→100%)
- `web/src/governance/__tests__/McpLifecycleCluster.test.tsx` — the trust/enable/disable/remove cluster (17→74%)
- `web/src/governance/__tests__/{McpBoard,SkillsBoard,McpServerDetail}.test.tsx` — install-panel + env-edit + archive/restore toggles
- `web/stryker.config.json` — extended mutate scope (governanceView + approvalState)
- `internal/webui/dist` — rebuilt embedded SPA (//go:embed)
- `go.mod` — golang.org/x/tools v0.44.0 transitive→direct

## Decisions Made
See `key-decisions` frontmatter. The load-bearing ones: (1) `go/packages` for the type-resolved no-model-approve scan (the stdlib fallback could not resolve internal/* imports); (2) the secret-scan caught a genuine leak fixed at the handler boundary; (3) the live e2e was actually run, not faked.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1/2 - Bug/Missing Critical] The skill-install handler echoed a credential-bearing source on the wire**
- **Found during:** Task 1 (the held-out secret scan)
- **Issue:** `handleSkillInstall` returned `SkillsInstallInfo.Source = info.Source` verbatim; an install source can carry an embedded credential (`owner/repo?token=SECRET`, `https://user:pass@host/repo`, `https://ghp_TOKEN@host/repo`). The held-out secret scan caught the seeded `?token=` value crossing the `/api/governance/skills/install` response body — a real information-disclosure (T-29-05-01) the SPEC Prohibition #1 forbids.
- **Fix:** Added `sanitizeSkillSource` (internal/agui/governance_write_skills_redact.go) — masks URL userinfo (`user:<redacted>@`, or `<redacted>@` for an opaque token) and any secret-marked query-parameter value (`token`/`secret`/`pass`/`key`/...), preserving the non-secret shape. Applied at the `handleSkillInstall` boundary so it covers both the fake and the real `skillsWriteAdapter`.
- **Files modified:** internal/agui/governance_write_skills_redact.go (+test), internal/agui/governance_write_skills_api.go
- **Verification:** the secret scan now finds zero values over the full run; `TestSanitizeSkillSource` covers the URL/query shapes + a leak-belt.
- **Committed in:** `54cf4310` (Task 1)

**2. [Rule 3 - Blocking] The cron migration round-trip bound was stale at head=0022 (off-by-one)**
- **Found during:** Task 3 (`make coverage` `-p 1` run)
- **Issue:** `TestDispatchPendingNotificationIdentityRoundTrip` (internal/cron — OUTSIDE this plan's touched surface) walks the schema DOWN to revert 0014 with a hardcoded bound of 8 down-steps; with head now at 0022 (Phase-29 added 0022_mcp_audit), reverting 0014 needs 9 steps, so the bound stopped one short and the test failed — blocking `make coverage`.
- **Fix:** Derive the bound from the LIVE head via `db.Status` (`headDownBound`) so it never goes stale; the loop was already head-aware in shape, only the cap was a stale constant.
- **Files modified:** internal/cron/dispatch_integration_test.go
- **Verification:** the cron test passes; `make coverage` 88.0% ≥85%; `make quality-full` green.
- **Committed in:** `ab507cf1`; discovery logged in `deferred-items.md` (D-29-05-1).

**3. [Rule 1 - Bug] The colliding-restore e2e assertion hit a Playwright strict-mode violation**
- **Found during:** the live e2e run (Task 2 spec)
- **Issue:** `getByText('retired-skill')` matched both the archived row AND the inline 409 alert text — a strict-mode violation. The flow was correct (the inline error DID appear); only the post-assertion was ambiguous.
- **Fix:** Scope the row-untouched check to `getByRole('button', { name: /retired-skill/ })`.
- **Files modified:** web/e2e/governance-write.spec.ts
- **Verification:** 6/6 pass on chromium + mobile-chrome against the live serve.
- **Committed in:** `6b02734e`

---

**Total deviations:** 3 (1 bug/missing-critical security leak fix, 1 blocking gate fix, 1 e2e assertion fix). All necessary for correctness or to unblock a required gate; no scope creep — the only out-of-scope touch (cron) was a one-line head-aware bound bump needed for the gate, logged in deferred-items.md.

## Issues Encountered
- **Shared-PG concurrent-session contention:** a parallel Codex session writing to the shared Postgres caused transient flakes (`EnsureRoles: tuple concurrently updated XX000` when 3 packages call EnsureRoles concurrently; `TestApprovalsAPI_ListCrossThread` seeing extra rows). Each affected test PASSES in isolation; the coverage gate's `-p 1` serial execution is the canonical run and it passed green (88.0%). Not a regression in this plan.
- **Port 9080 occupied by the stale-dist container:** the live e2e could not reuse :9080 (the running container serves the pre-29-04 dist). Resolved by serving the freshly-built WSL binary on :9099 and pointing Playwright at it via `AURA_E2E_ORIGIN` — the new write UI was exercised end to end.

## Live Playwright e2e
**RAN — not faked, not skipped-as-green.** Built `aura` in WSL (`go build -o /tmp/aura-e2e ./cmd/aura`, fresh dist embedded), served it on :9099 against the live stack, and ran Playwright natively against `AURA_E2E_ORIGIN=http://127.0.0.1:9099`: **6/6 pass on chromium AND mobile-chrome**, axe clean on every new surface (install panel, env-edit, skill-install, remove dialog). To re-run:
```
# WSL: serve the fresh binary on a free port
cd /mnt/d/Aura && set -a && source <(tr -d '\r' < .env) && set +a \
  && export AURA_DB_URL="postgres://aura_app:${POSTGRES_PASSWORD}@127.0.0.1:5432/aura?sslmode=disable" \
  && export AURA_DB_MIGRATE_URL="postgres://aura_migrate:${POSTGRES_PASSWORD}@127.0.0.1:5432/aura?sslmode=disable" \
  && export AURA_AGUI_BIND="127.0.0.1:9099" && go build -o /tmp/aura-e2e ./cmd/aura && /tmp/aura-e2e serve --only=cli
# Windows (web/): run Playwright against it
cd /d/Aura/web && export AURA_E2E_ORIGIN=http://127.0.0.1:9099 \
  && export AURA_WEB_AUTH_SECRET=<from .env> \
  && npx playwright test governance-write.spec.ts --project=chromium --project=mobile-chrome
```

## User Setup Required
None - no external service configuration required.

## Known Stubs
None - every new file is a complete implementation (the backstop tests assert real behavior; `sanitizeSkillSource` is a real redaction; the e2e mocks model the Go-proven backend contract).

## Next Phase Readiness
- Phase 29 is closed to Gate-3: all 8 SPEC prohibitions have an automated backstop, the 14 acceptance criteria + edges have a green signal (Go + Vitest + live Playwright), the Go owned-surface is 88.0% ≥85% on the live stack, `make quality-full` is green, the frontend floors (Vitest ≥85% + Stryker ≥70%) hold, and the single-binary dist is fresh. Ready for `/gsd-verify-work` → `/gsd-secure-phase` (the canon-referred SSRF + name-traversal retro-verification) → `/gsd-complete-milestone`.
- The one out-of-scope cron migration-bound fix (deferred-items D-29-05-1) is resolved in-line; no open blockers.

## Self-Check: PASSED

---
*Phase: 29-governance-write-mcp-configuration-skills-install*
*Completed: 2026-06-21*
