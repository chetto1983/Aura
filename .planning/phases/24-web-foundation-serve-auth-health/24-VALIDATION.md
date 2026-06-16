---
phase: 24
slug: web-foundation-serve-auth-health
status: planned
nyquist_compliant: true
wave_0_complete: false
created: 2026-06-16
planned_at: 2026-06-16
---

# Phase 24 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.
> Seeded from `24-RESEARCH.md` §"Validation Architecture"; the per-task map below
> is now populated with the `24-NN` plan/task IDs from the four PLAN.md files.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | Go stdlib `testing` + `net/http/httptest` (no testify on the agui/webui/cmd HTTP surface — verified in `serve_webui_test.go`, `embed_test.go`, `server_integration_test.go`); `pgregory.net/rapid` for property tests (vendored, `go.mod:31`); frontend = Vitest + Playwright (Phase-23 locked) |
| **Config file** | none for Go (stdlib); `web/playwright.config.ts` + Vitest config (Phase 23) |
| **Quick run command** | `go test ./internal/agui/... ./internal/config/... ./internal/webui/... ./cmd/aura/...` |
| **Full suite command** | `go test -tags 'db_integration neo4j_integration' -race -p 1 ./... -count=1` (derive `AURA_DB_URL`/`AURA_DB_MIGRATE_URL` from `POSTGRES_PASSWORD`; stack up) |
| **Estimated runtime** | ~20–40s quick; full matrix several minutes (live stack) |

---

## Sampling Rate

- **After every task commit:** `go vet ./... && go build ./... && go test ./internal/agui/ ./internal/config/ ./internal/webui/ ./cmd/aura/` + `go test -race` on each touched package (CLAUDE.md post-edit gate). Frontend tasks: `cd web && npm run lint && npm run typecheck && npm run test`.
- **After every plan wave:** full untagged suite + `go test -tags db_integration -race -p 1 ./internal/agui/ ./cmd/aura/` (the `capability_grants` binding tier) + the Playwright E2E.
- **Before `/gsd-verify-work`:** full `db_integration neo4j_integration` matrix green + `make coverage` ≥85% owned-surface + the live serve smoke (`serve_smoke` tag) + `scripts/agui_boundary_check.sh` (leaf invariant) green + the web dist-freshness gate.
- **Max feedback latency:** < 60s (quick gate).

---

## Per-Task Verification Map

| Plan/Task | Req | Behavior | Test Type | Automated Command | Threat Ref |
|-----------|-----|----------|-----------|-------------------|------------|
| 24-01 T1/T2 | WEB-02 | Loopback bind (v4/v6/named) boots with no credential | unit (pure fn) | `go test ./internal/config/ -run TestGuardWebBind` | T-24-01 |
| 24-01 T2 | WEB-02 | Non-loopback + neither credential → error naming both env vars | unit (pure fn) | `go test ./internal/config/ -run TestGuardWebBind` | T-24-01 |
| 24-01 T2 | WEB-02 | Non-loopback + secret OR trust-proxy → boots | unit (pure fn) | `go test ./internal/config/ -run TestGuardWebBind` | T-24-01 |
| 24-01 T2 | WEB-02 | `0.0.0.0` / `::` / `[::]` treated as non-loopback (gated) | unit (pure fn) | `go test ./internal/config/ -run TestGuardWebBind` | T-24-02 |
| 24-01 T2 | WEB-02 | `bootServe` calls `GuardWebBind` on the fail-fast path | source/build | `go build ./cmd/aura/` + grep `config.GuardWebBind` | T-24-01 |
| 24-02 T1 | WEB-01 | Unknown client route → index.html (SPA shell) | unit (httptest) | `go test ./internal/webui/ -run TestSPAFallback` | T-24-05 |
| 24-02 T1 | WEB-01 | Excluded prefix → real 404, never HTML | unit (httptest) | `go test ./internal/webui/ -run TestSPAFallback` | T-24-05 |
| 24-02 T1 | WEB-01 | Leaf invariant intact (no internal/* import) | CLI | `bash scripts/agui_boundary_check.sh` | T-24-06 |
| 24-02 T2 | WEB-01 | Excluded API/`/agent`/`/api/` prefix → real 404 (flipped no-fallback case) | unit (httptest) | `go test ./cmd/aura/ -run TestServeWebui` | T-24-05 |
| 24-02 T2 | WEB-01 | AG-UI + `/api/integrations/` keep precedence; no `/api/` mux collision | unit (httptest) | `go test ./cmd/aura/ -run TestServeWebui` | T-24-07/T-24-08 |
| 24-03 T1 | WEB-03 | Empty configured secret → login rejected (fail-closed) | unit | `go test ./internal/agui/ -run TestValidateSecret` | T-24-09/T-24-14 |
| 24-03 T1 | WEB-03 | Correct secret → login sets cookie HttpOnly+Secure+SameSite=Strict+Path=/ | unit (httptest) | `go test ./internal/agui/ -run TestLogin` | T-24-12 |
| 24-03 T1 | WEB-03 | Cookie sign→verify round-trip identity preserved | property (rapid) | `go test ./internal/agui/ -run TestSignVerifyRoundtrip` | T-24-10 |
| 24-03 T1 | WEB-03 | Tampered cookie payload/sig → verify fails | unit + property | `go test ./internal/agui/ -run TestVerifyTamper` | T-24-10 |
| 24-03 T1 | WEB-03 | Expired cookie (issued + TTL < now) → verify fails | unit | `go test ./internal/agui/ -run TestVerifyExpiry` | T-24-11 |
| 24-03 T1 | WEB-03 | Wrong-passphrase login → generic 401, no oracle, no cookie | unit (httptest) | `go test ./internal/agui/ -run TestLogin` | T-24-15 |
| 24-03 T2 | WEB-03 | `RequireAuth`: no cookie → redirect/401; valid → next; deleted identity → reject | unit (httptest) | `go test ./internal/agui/ -run TestRequireAuth` | T-24-13 |
| 24-03 T2 | WEB-03 | D-03 public paths: `/login`, login assets, `/healthz` open; `/`,`/readyz`,`/metrics` gated | unit (httptest) | `go test ./internal/agui/ -run TestRequireAuth` | T-24-17 |
| 24-03 T2 | WEB-03 | `SecretConfigured==false` → RequireAuth pass-through no-op (loopback dev) | unit (httptest) | `go test ./internal/agui/ -run TestRequireAuth` | T-24-14 |
| 24-03 T2/T3 | WEB-03 | `POST /agent/run` requires `HasCapability`; `local` (`*`) passes | integration (db_integration) | `go test -tags db_integration -race -p 1 ./internal/agui/ -run TestAgentRunCapability` | T-24-16 |
| 24-04 T2 | WEB-04 | `/healthz` + `/readyz` panel render: dot+label per row (not color-only) | component (Vitest) | `cd web && npm run test` | T-24-20 |
| 24-04 T2 | WEB-03 | Login page: labelled passphrase field, role=alert error | component (Vitest) | `cd web && npm run test` | — |
| 24-04 T4 | WEB-04 | Theme/density before paint (no flash) + health panel renders | E2E (Playwright) | `cd web && npx playwright test health-panel.spec.ts` | T-24-22 |
| 24-04 T4 | WEB-01/02/03 | Live: non-loopback+no-secret → non-zero exit w/ message; w/ secret → boots behind auth redirect; `/api/nope` → 404; `/healthz` → 200 | live serve smoke | `go test -tags serve_smoke ./cmd/aura/ -run TestServeSmoke` | T-24-01/T-24-05/T-24-13/T-24-24 |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

These test files do not exist yet; the owning task (TDD-tagged where applicable) creates each test before/with its implementation, so no run of 3+ consecutive tasks lacks an automated verify.

- [ ] `internal/config/config_webauth_test.go` — 24-01 T2 (`GuardWebBind` matrix + env-load coverage).
- [ ] `internal/webui` SPA-fallback handler + `embed_test.go` `TestSPAFallback` — 24-02 T1.
- [ ] `cmd/aura/serve_webui_test.go` flip + `/api/`-404 + integrations-precedence cases — 24-02 T2.
- [ ] `internal/agui/auth_cookie.go` + `internal/agui/auth.go` + `internal/agui/auth_test.go` — 24-03 T1/T2 (sign/verify, validateSecret, RequireAuth, cookie flags, public-path table); split `auth_cookie.go` keeps each file < 600 LOC.
- [ ] `internal/agui/auth_capability_integration_test.go` (build tag `db_integration`) — 24-03 T3 (`POST /agent/run` + `HasCapability` over the seeded `local`).
- [ ] `web/src/__tests__/LoginPage.test.tsx` + `web/src/__tests__/RuntimeHealthPanel.test.tsx` — 24-04 T2.
- [ ] `web/e2e/health-panel.spec.ts` (extend the Phase-23 Playwright E2E) — 24-04 T4.
- [ ] `cmd/aura/serve_smoke_test.go` (build tag `serve_smoke`) — 24-04 T4 (live fail-fast exit + `/api/nope` 404 + auth redirect).
- [ ] Framework install: **none** for Go — stdlib `testing` + already-vendored `rapid` + Phase-23 Playwright. Frontend: `react-router-dom` + `@tanstack/react-query` added inside the Phase-23-locked toolchain (24-04 T1).

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Operator live sign-off: expose `aura serve` on a real non-loopback bind with `AURA_WEB_AUTH_SECRET` set, log in through a browser, confirm the cockpit + health panel load behind auth and the unguarded bind fails fast | WEB-02/03/04 | Requires a real network bind + browser session that the automated `serve_smoke`/Playwright tiers approximate but don't fully reproduce on a single host | 1) `AURA_AGUI_BIND=0.0.0.0:9080 AURA_WEB_AUTH_SECRET=<pass> aura serve` 2) open from another device 3) confirm login gate + panel 4) repeat without the secret → process exits non-zero with the actionable message |
| Visual UI-SPEC conformance of the login page + runtime health panel (theme-before-paint, accent reserved-list, status dot+label, focus ring) | WEB-03/04 | Pixel/interaction conformance to the approved 24-UI-SPEC needs a human eye; the Playwright E2E asserts structure, not visual fidelity | 24-04 Task 3 checkpoint: build the binary, open the loopback shell + the exposed login page, confirm against 24-UI-SPEC |

*All other phase behaviors have automated verification (unit / property / db_integration / serve_smoke / Playwright).*

---

## Validation Sign-Off

- [x] All tasks have `<automated>` verify or Wave 0 dependencies
- [x] Sampling continuity: no 3 consecutive tasks without automated verify
- [x] Wave 0 covers all MISSING references
- [x] No watch-mode flags
- [x] Feedback latency < 60s
- [x] `nyquist_compliant: true` set in frontmatter

**Approval:** planner-populated (per-task map seeded from the four PLAN.md files)
