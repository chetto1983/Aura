---
phase: 24
slug: web-foundation-serve-auth-health
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-06-16
---

# Phase 24 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.
> Seeded from `24-RESEARCH.md` §"Validation Architecture". The per-task map is
> populated by `gsd-planner` once PLAN.md task IDs exist; the requirement-level
> test plan below is authoritative until then.

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

- **After every task commit:** `go vet ./... && go build ./... && go test ./internal/agui/ ./internal/config/ ./internal/webui/ ./cmd/aura/` + `go test -race` on each touched package (CLAUDE.md post-edit gate).
- **After every plan wave:** full untagged suite + `go test -tags db_integration -race -p 1 ./internal/agui/ ./cmd/aura/` (the `capability_grants` binding tier) + the Playwright E2E.
- **Before `/gsd-verify-work`:** full `db_integration neo4j_integration` matrix green + `make coverage` ≥85% owned-surface + the live serve smoke (`serve_smoke` tag) + `scripts/agui_boundary_check.sh` (leaf invariant) green.
- **Max feedback latency:** < 60s (quick gate).

---

## Per-Task Verification Map

> Populated by `gsd-planner` per task (`24-NN-MM` IDs). Until plans exist, the
> requirement-level plan from `24-RESEARCH.md` §"Phase Requirements → Test Map"
> is authoritative:

| Req | Behavior | Test Type | Automated Command | Threat Ref | File (Wave 0?) |
|-----|----------|-----------|-------------------|------------|----------------|
| WEB-01 | Unknown client route → index.html (SPA shell) | unit (httptest) | `go test ./internal/webui/ -run TestSPAFallback` | T-SPA-404 | ❌ W0 `internal/webui/embed_test.go` |
| WEB-01 | Excluded API/`/agent`/health prefix → real 404, never HTML | unit (httptest) | `go test ./cmd/aura/ -run TestServeWebui` | T-SPA-404 | ❌ W0 (flip `serve_webui_test.go` no-fallback case) |
| WEB-01 | AG-UI + new `/api/` prefixes keep precedence over `/` catch-all | unit (httptest) | `go test ./cmd/aura/ -run TestServeWebui` | — | ✅ partial (add `/api/` case) |
| WEB-02 | Loopback bind boots with no credential | unit (pure fn) | `go test ./internal/config/ -run TestGuardWebBind` | T-EXPOSE | ❌ W0 `config_webauth_test.go` |
| WEB-02 | Non-loopback + neither credential → error naming the vars | unit (pure fn) | `go test ./internal/config/ -run TestGuardWebBind` | T-EXPOSE | ❌ W0 |
| WEB-02 | Non-loopback + secret OR trust-proxy → boots | unit (pure fn) | `go test ./internal/config/ -run TestGuardWebBind` | T-EXPOSE | ❌ W0 |
| WEB-02 | `0.0.0.0` / `::` / `[::]` treated as non-loopback (gated) | unit (pure fn) | `go test ./internal/config/ -run TestGuardWebBind` | T-EXPOSE | ❌ W0 |
| WEB-03 | Empty configured secret → login rejected (fail-closed) | unit | `go test ./internal/agui/ -run TestValidateSecret` | T-FAILOPEN | ❌ W0 `internal/agui/auth_test.go` |
| WEB-03 | Correct secret → login sets cookie HttpOnly+Secure+SameSite=Strict+Path=/ | unit (httptest) | `go test ./internal/agui/ -run TestLogin` | T-COOKIE | ❌ W0 |
| WEB-03 | Cookie sign→verify round-trip identity preserved | property (rapid) | `go test ./internal/agui/ -run TestSignVerifyRoundtrip` | T-FORGE | ❌ W0 |
| WEB-03 | Tampered cookie payload/sig → verify fails | unit + property | `go test ./internal/agui/ -run TestVerifyTamper` | T-FORGE | ❌ W0 |
| WEB-03 | Expired cookie → verify fails | unit | `go test ./internal/agui/ -run TestVerifyExpiry` | T-REPLAY | ❌ W0 |
| WEB-03 | `RequireAuth`: no cookie → redirect/401; valid → next; deleted identity → reject | unit (httptest) | `go test ./internal/agui/ -run TestRequireAuth` | T-ACCESS | ❌ W0 |
| WEB-03 | D-03 public paths: `/login`, login assets, `/healthz` open; rest gated | unit (httptest) | `go test ./internal/agui/ -run TestRequireAuthPublicPaths` | T-ACCESS | ❌ W0 |
| WEB-03 | `POST /agent/run` requires `HasCapability`; `local` (`*`) passes | integration (db_integration) | `go test -tags db_integration -race -p 1 ./internal/agui/ -run TestAgentRunCapability` | T-ACCESS | ❌ W0 (real `identity.Store`, migration 0004) |
| WEB-04 | `/healthz` + `/readyz` JSON shape stable (panel contract) | unit / integration | `go test ./internal/agui/ -run 'TestHealthz\|TestReadyz'` | — | ✅ (assert panel fields don't regress) |
| WEB-04 | Theme/density before paint (no flash) + health panel renders | smoke / E2E (Playwright) | `cd web && npx playwright test health-panel.spec.ts` | — | ❌ W0 (extend Phase-23 E2E) |
| WEB-01/03 | Live: non-loopback + no secret → non-zero exit w/ message; w/ secret → boots behind auth redirect; `/api/nope` → 404 | live serve smoke | `go test -tags serve_smoke ./cmd/aura/ -run TestServeSmoke` | T-EXPOSE / T-ACCESS | ❌ W0 `cmd/aura/serve_smoke_test.go` |

*Status: ⬜ pending · ✅ green · ❌ red · ⚠️ flaky*

---

## Wave 0 Requirements

- [ ] `internal/agui/auth.go` + `internal/agui/auth_test.go` — WEB-03 (sign/verify, validateSecret, RequireAuth, cookie flags); split `auth_cookie.go` if `auth.go` nears 600 LOC.
- [ ] `internal/agui/auth_capability_integration_test.go` (build tag `db_integration`) — WEB-03 `POST /agent/run` + `HasCapability` over the seeded `local` identity.
- [ ] `internal/config/config_webauth_test.go` — WEB-02 `GuardWebBind` matrix; add `WebAuthSecret`/`WebTrustProxy` to env coverage.
- [ ] `internal/webui` SPA-fallback handler + `embed_test.go` cases — WEB-01 client-route → index.html, excluded-prefix → 404.
- [ ] `cmd/aura/serve_webui_test.go` — flip the "no SPA-fallback (Phase 24)" case to assert the fallback; add the `/api/` 404 case + an auth-redirect case.
- [ ] `cmd/aura/serve_smoke_test.go` (build tag `serve_smoke`) — WEB-02 fail-fast exit + WEB-01 live 404 + WEB-03 live auth-redirect by booting the real binary.
- [ ] `web/e2e/health-panel.spec.ts` (extend the Phase-23 Playwright E2E) — WEB-04 panel render + theme-before-paint.
- [ ] Framework install: **none** — stdlib `testing` + already-vendored `rapid` + Phase-23 Playwright.

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Operator live sign-off: expose `aura serve` on a real non-loopback bind with `AURA_WEB_AUTH_SECRET` set, log in through a browser, confirm the cockpit + health panel load behind auth and the unguarded bind fails fast | WEB-02/03/04 | Requires a real network bind + browser session that the automated `serve_smoke`/Playwright tiers approximate but don't fully reproduce on a single host | 1) `AURA_AGUI_BIND=0.0.0.0:9080 AURA_WEB_AUTH_SECRET=<pass> aura serve` 2) open from another device 3) confirm login gate + panel 4) repeat without the secret → process exits non-zero with the actionable message |

*All other phase behaviors have automated verification (unit / property / db_integration / serve_smoke / Playwright).*

---

## Validation Sign-Off

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 60s
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
