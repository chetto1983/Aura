---
phase: 24-web-foundation-serve-auth-health
verified: 2026-06-16T21:00:00Z
status: passed
resolution: "Both human items resolved 2026-06-16: (1) operator visual sign-off was performed interactively during the Task-3 human-verify checkpoint — the operator found + I fixed aria-invalid (1c494d24) and the D-03 static-asset-401 (0fa2d865), then directed the i18n + quality follow-ons; (2) the stale REQUIREMENTS.md WEB-04 checkbox + tracking row were flipped to complete."
score: 4/4 must-haves verified
overrides_applied: 0
human_verification:
  - test: "Visual sign-off of login page + health panel rendering in a browser"
    expected: "Dark-operator theme, no light-mode flash, login form matches 24-UI-SPEC (labelled passphrase, role=alert error), health panel shows dot+label rows for Liveness/Readiness/Postgres/Neo4j/Bind/Build"
    why_human: "Playwright E2E exercises the headless render; only a human can confirm the approved visual design contract (color, spacing, brand, no flash) matches the live binary"
  - test: "WEB-04 checkbox in REQUIREMENTS.md is unchecked ([ ]) despite implementation being complete and ROADMAP showing Phase 24 as Complete"
    expected: "REQUIREMENTS.md line 44 should be [x] WEB-04 and the tracking table row 166 should say Complete to match the ROADMAP and code reality"
    why_human: "Documentation inconsistency — the code fully implements WEB-04 (RuntimeHealthPanel, useRuntimeHealth, theme-before-paint, Playwright E2E, dist committed) but the checkbox was not flipped. This is a docs-only fix, not a code gap, but requires human confirmation before marking the requirement Complete."
---

# Phase 24: Web Foundation — Serve + Auth + Health Verification Report

**Phase Goal:** A single-binary SPA host on `aura serve` (SPA-fallback route exclusion) with the GAP-2 web-auth boundary + non-loopback boot guard + a runtime health shell (WEB-01..04).
**Verified:** 2026-06-16T21:00:00Z
**Status:** human_needed
**Re-verification:** No — initial verification

---

## Goal Achievement

### Observable Truths

| # | Truth | Status | Evidence |
|---|-------|--------|----------|
| 1 | WEB-01: SPA host serves index.html for unknown client routes; excluded API/agent/health prefixes return a real 404, never the shell | VERIFIED | `internal/webui/embed.go` — `spaFallback` handler iterates `apiPrefixes`, calls `http.NotFound` on match; `statMissing` falls back to `index.html` for unknown routes; `TestSPAFallback` in `embed_test.go` covers both paths; `cmd/aura/serve_webui.go` single-sources `fallbackExcludedPrefixes()` including `/api/` carve-out; serve_smoke asserts `/api/nope` → 404 (authenticated) + index.html not in body |
| 2 | WEB-02: `aura serve` refuses non-loopback bind without `AURA_WEB_AUTH_SECRET` or `AURA_WEB_TRUST_PROXY`; loopback boots unchanged | VERIFIED | `config.GuardWebBind` in `internal/config/config.go` (line 229) — pure function, `net.SplitHostPort` + `net.ParseIP.IsLoopback()`, wildcards (0.0.0.0, ::) correctly non-loopback; wired in `cmd/aura/serve.go` bootServe (line 280) before `httpSrv` build; `TestGuardWebBind` matrix in `config_webauth_test.go`; serve_smoke sub-test "WEB-02 non-loopback bind without web-auth fail-fasts" asserts non-zero exit + message containing `AURA_WEB_AUTH_SECRET` |
| 3 | WEB-03: whole-origin gated except login + static assets + GET /healthz; HMAC-signed HttpOnly+Secure+SameSite=Strict cookie; fail-closed on empty secret; capability gate on POST /agent/run | VERIFIED | `internal/agui/auth.go` — `RequireAuth` reads only `r.Cookie(sessionCookieName)` (no client header); `isPublicPath` covers login route + `webui.IsPublicAsset` predicate; GET /healthz hard-coded public (line 176); `auth_cookie.go` — `validateSecret` fail-closes on empty configured secret; `signSession`/`verifySession` HMAC-SHA256 with `hmac.Equal`; `setSessionCookie` writes HttpOnly+Secure+SameSite=Strict+Path=/; `__Host-` prefix; `RequireCapability` guards POST /agent/run; all wired via `buildAuthDeps` + `newServeHandler`; full test suite: `TestValidateSecret`, `TestSignVerifyRoundtrip` (rapid property), `TestVerifyTamper`, `TestVerifyExpiry`, `TestLogin`, `TestLogout`, `TestRequireAuth`, `TestRequireCapability` + `TestAgentRunCapability` (db_integration) |
| 4 | WEB-04: app shell renders with theme/density before paint (no flash); runtime health panel aggregates /healthz + /readyz with dot+text per row; no new backend endpoint added | VERIFIED | `web/src/health/RuntimeHealthPanel.tsx` — exports `RuntimeHealthPanel`, renders `StatusDot` (aria-hidden) + text label per `StatusRow` (Liveness/Readiness/Postgres/Neo4j/Bind/Build); `useRuntimeHealth.ts` polls `/healthz` + `/readyz` via `useQuery` (@tanstack/react-query); no new backend endpoint; `internal/webui/dist/index.html` committed with synchronous pre-paint script setting `data-theme` + `data-density`; `web/e2e/health-panel.spec.ts` asserts `data-theme=dark` before paint and visible `Runtime` heading + all row labels; frontend tests: `RuntimeHealthPanel.test.tsx` (48 passing, ≥85% coverage, Stryker 82.2%) |

**Score:** 4/4 truths verified

---

### Deferred Items

None — all four success criteria are met in code.

---

### Required Artifacts

| Artifact | Expected | Status | Details |
|----------|----------|--------|---------|
| `internal/config/config.go` | `func GuardWebBind` + `WebAuthSecret`/`WebTrustProxy` fields + `loadBase` env loading | VERIFIED | `GuardWebBind` at line 229; fields at lines 119–120; `loadBase` at lines 352–353; stale loopback-only comment at lines 105–112 correctly updated |
| `internal/config/config_webauth_test.go` | `TestGuardWebBind` table matrix + env-load coverage | VERIFIED | File exists; `TestGuardWebBind` (line 12) + `TestWebAuthConfigLoad` (line 68); covers loopback v4/v6/named, wildcards, non-loopback × {secret, trust-proxy, neither} |
| `cmd/aura/serve.go` | `config.GuardWebBind(chat.cfg.AGUIBind, ...)` inside `bootServe` on fail-fast path | VERIFIED | Line 280; uses `chat.close(); return nil, err` cleanup idiom; stale amendment-#35 comment rewritten (lines 219–224) |
| `internal/webui/embed.go` | `spaFallback` handler + `Handler(apiPrefixes []string) (http.Handler, error)` + `IsPublicAsset` + leaf-only imports | VERIFIED | `spaFallback` at line 70; `Handler` at line 41; `IsPublicAsset` at line 103; imports: embed/io/fs/net/http/strings only |
| `cmd/aura/serve_webui.go` | Single-source exclusion list with `/api/` carve-out passed to `webui.Handler`; `agui.RequireAuth` wrap; `POST /login` + `POST /logout` | VERIFIED | `fallbackExcludedPrefixes()` includes `/api/` (line 62+); `webui.Handler(fallbackExcludedPrefixes())` (line 100); `agui.RequireAuth(mux, auth)` returned (line 132); `/login` + `/logout` registered (lines 116–117) |
| `internal/agui/auth.go` | `RequireAuth`, `LoginHandler`, `LogoutHandler`, `RequireCapability`, `AuthDeps`, `identityChecker` interface | VERIFIED | All present; no client auth-header reads; CSRF posture documented inline |
| `internal/agui/auth_cookie.go` | `validateSecret`, `signSession`, `verifySession`, `setSessionCookie`, `clearSessionCookie`, `sessionCookieName`; imports `crypto/hmac`/`crypto/sha256`/`crypto/subtle` only | VERIFIED | All present; `hmac.Equal` used (not `bytes.Equal`); `subtle.ConstantTimeCompare` used; no gorilla/* dependency |
| `internal/agui/auth_test.go` | Full unit + rapid property test suite | VERIFIED | `TestValidateSecret`, `TestSignVerifyRoundtrip` (rapid), `TestVerifyTamper`, `TestVerifyExpiry`, `TestLogin`, `TestLogout`, `TestRequireAuth`, `TestRequireCapability` all present |
| `internal/agui/auth_capability_integration_test.go` | `TestAgentRunCapability` (build tag db_integration) | VERIFIED | File exists; `//go:build db_integration`; `TestAgentRunCapability` at line 53; `t.Fatal` under `$CI` with env unset (no skip-as-green) |
| `web/src/routes/LoginPage.tsx` | Login form with `type="password"` + `autocomplete="current-password"` + `role="alert"` error region | VERIFIED | `autoComplete="current-password"` (line 93); `aria-invalid={error !== null || undefined}` (line 97); `role="alert"` error paragraph (line 151); POST to `/login` same-origin; 303 redirect to "/" on success |
| `web/src/routes/NotFoundView.tsx` | "Page not found" h1 + Router link to "/" | VERIFIED | `export function NotFoundView()` (line 8); `<h1>` with i18n `notFound.title`; `<Link to="/">` from react-router-dom |
| `web/src/health/RuntimeHealthPanel.tsx` | Rows for Liveness/Readiness/Postgres/Neo4j/Bind/Build; `StatusDot` aria-hidden + text label per row | VERIFIED | `StatusDot` with `aria-hidden="true"` (line 22); `StatusRow` renders both dot + text `{status}` (line 37); 6 rows in render (lines 139–175) |
| `web/src/health/useRuntimeHealth.ts` | `useQuery` polling `/healthz` + `/readyz` | VERIFIED | `useQuery` from `@tanstack/react-query` (line 1); `fetchHealthz` fetches `/healthz` (line 29); `fetchReadyz` fetches `/readyz` (line 38) |
| `cmd/aura/serve_smoke_test.go` | `//go:build serve_smoke`; `TestServeSmoke` with WEB-02/01/03 assertions; no skip-as-green | VERIFIED | Build tag present (line 1); `TestServeSmoke` (line 165); non-loopback fail-fast subtest (line 168); WEB-01/03 subtest (line 195); `t.Fatal` under `$CI` pattern present |
| `internal/webui/dist` | Committed built SPA assets (login page + health panel); pre-paint script in index.html | VERIFIED | `dist/assets/` has 7 files including `.js` + `.css`; `dist/index.html` contains synchronous pre-paint script setting `data-theme`/`data-density` (lines 11–20); `dist/manifest.webmanifest` present |
| `web/package.json` | `react-router-dom` + `@tanstack/react-query` in dependencies | VERIFIED | `"@tanstack/react-query": "^5.101.0"` (line 19); `"react-router-dom": "^7.18.0"` (line 24) |
| `.env.example` | `AURA_WEB_AUTH_SECRET` + `AURA_WEB_TRUST_PROXY` entries with comments | VERIFIED | Both present with comments; `AURA_AGUI_BIND` comment updated to note non-loopback is now possible |
| `prd.md` env catalog | Two new rows for WEB-02 knobs | VERIFIED | Lines 5138–5139 in prd.md; correct format with derivation note and D-05 reference |

---

### Key Link Verification

| From | To | Via | Status | Details |
|------|----|-----|--------|---------|
| `cmd/aura/serve.go bootServe` | `config.GuardWebBind` | Call at line 280 before `httpSrv` build, `chat.close(); return nil, err` idiom | WIRED | Pattern `config\.GuardWebBind` present; correct cleanup idiom used |
| `cmd/aura/serve_webui.go newServeHandler` | `webui.Handler(fallbackExcludedPrefixes())` | `spaFallback` handler returned at `/` catch-all | WIRED | `webui.Handler(fallbackExcludedPrefixes())` at line 100; `/api/` in excluded list |
| `cmd/aura/serve_webui.go newServeHandler` | `agui.RequireAuth(mux, auth)` | Wraps the entire parent mux | WIRED | `return agui.RequireAuth(mux, auth), nil` at line 132; `auth.PublicAsset = webui.IsPublicAsset` at line 128 |
| `internal/agui RequireAuth` | `r.Cookie(sessionCookieName)` | Cookie-only read; no client header | WIRED | `r.Cookie(sessionCookieName)` at line 184; no `r.Header.Get("Authorization")` or `X-Forwarded-*` in RequireAuth |
| `cmd/aura/serve_auth.go buildAuthDeps` | `sha256.Sum256([]byte(secret))` | Derives HMAC signing key from AURA_WEB_AUTH_SECRET | WIRED | `key := sha256.Sum256([]byte(secret))` in `buildAuthDeps`; `SigningKey: key[:]` in returned AuthDeps |
| `web/src/health/useRuntimeHealth.ts` | `GET /healthz` + `GET /readyz` | `useQuery` REST poll, 5s refetch interval | WIRED | `fetch('/healthz')` + `fetch('/readyz')` in hook; `useQuery` calls at lines 57 + 63 |
| `web/src/main.tsx` | `LoginPage` + `NotFoundView` + `QueryClientProvider` + `BrowserRouter` | Lazy imports + `<Routes>` with `/login` + `*` catch-all | WIRED | `BrowserRouter`, `Routes`, `QueryClientProvider` all in main.tsx; `applyTheme()` called before `createRoot` |

---

### Data-Flow Trace (Level 4)

| Artifact | Data Variable | Source | Produces Real Data | Status |
|----------|---------------|--------|--------------------|--------|
| `RuntimeHealthPanel.tsx` | `healthz`, `readyz` | `useRuntimeHealth()` → `useQuery` → `fetch('/healthz')` + `fetch('/readyz')` → daemon `handleHealthz`/`readyz` handlers backed by real DB+Neo4j checks | Yes — daemon reads real PG pool ping + Neo4j; `bind_address`/`build_version` injected at daemon boot | FLOWING |
| `RequireAuth` in `auth.go` | `identityID` from cookie | `verifySession` → HMAC verify → `GetIdentityByID` via `identityCheckerAdapter` → `identity.Store` → real DB query | Yes — `identity.Store.GetIdentityByID` is a real sqlc-generated query against `aura.identities` | FLOWING |
| `spaFallback` in `embed.go` | embedded FS | `fs.Sub(distFS, "dist")` over committed `internal/webui/dist` | Yes — build assets committed and non-empty (7 files in assets/) | FLOWING |

---

### Behavioral Spot-Checks

| Behavior | Command | Result | Status |
|----------|---------|--------|--------|
| WEB-02: `GuardWebBind("127.0.0.1:9080","",false)` returns nil | Verified via `TestGuardWebBind` unit test (not re-run; pre-verified) | Green per verification evidence | PASS |
| WEB-02: `GuardWebBind("0.0.0.0:9080","",false)` returns error with both env var names | Verified via `TestGuardWebBind` | Green per verification evidence | PASS |
| WEB-01: `spaFallback` serves index.html for unknown route; 404 for `/api/nope` | Verified via `TestSPAFallback` + serve_smoke | Green per verification evidence | PASS |
| WEB-03: `RequireAuth` no-cookie → redirect/401; valid cookie → next | Verified via `TestRequireAuth` | Green per verification evidence | PASS |
| WEB-04: `validateSecret("","")` → false (fail-closed) | Verified via `TestValidateSecret` | Green per verification evidence | PASS |
| Live binary: non-loopback + no secret exits non-zero | Verified by serve_smoke `TestServeSmoke` sub-test (pre-run; operator-confirmed) | Green per verification evidence | PASS |
| Live binary: authenticated `/api/nope` → 404 (real, not SPA) | Verified by serve_smoke `TestServeSmoke` sub-test | Green per verification evidence | PASS |

---

### Probe Execution

No conventional `probe-*.sh` scripts found for Phase 24. The serve_smoke test (`go test -tags serve_smoke ./cmd/aura/ -run TestServeSmoke`) serves as the live binary probe. It was executed and passed as reported in the `<verification_evidence_already_run>` section of the verification request.

---

### Requirements Coverage

| Requirement | Source Plan | Description | Status | Evidence |
|-------------|------------|-------------|--------|----------|
| WEB-01 | 24-02-PLAN.md, 24-04-PLAN.md | SPA host serves index.html for client routes; API/agent/health prefixes 404 as real errors | SATISFIED | `spaFallback` in embed.go; `fallbackExcludedPrefixes()` in serve_webui.go; `TestSPAFallback`; serve_smoke `/api/nope` → 404 |
| WEB-02 | 24-01-PLAN.md | `aura serve` fail-fasts on non-loopback without web auth configured | SATISFIED | `config.GuardWebBind` pure function; wired in `bootServe`; `TestGuardWebBind` matrix; serve_smoke WEB-02 sub-test |
| WEB-03 | 24-03-PLAN.md | HMAC-signed HttpOnly session cookie; whole-origin gate except login+assets+/healthz; capability gate on POST /agent/run; fail-closed on empty secret | SATISFIED | `auth.go` + `auth_cookie.go`; `RequireAuth`; full test suite incl. rapid property tests; `TestAgentRunCapability` (db_integration); serve_smoke WEB-03 sub-test |
| WEB-04 | 24-04-PLAN.md | Theme/density before paint (no flash); read-only health panel with /healthz + /readyz; no new backend endpoint | SATISFIED (code) / DOCS INCONSISTENCY | `RuntimeHealthPanel.tsx` + `useRuntimeHealth.ts` poll only existing endpoints; `dist/index.html` pre-paint script; Playwright `health-panel.spec.ts`; HOWEVER: REQUIREMENTS.md line 44 still shows `[ ]` (unchecked) and tracking table row 166 shows "Pending" — code is complete but documentation was not updated |

---

### Anti-Patterns Found

| File | Line | Pattern | Severity | Impact |
|------|------|---------|----------|--------|
| `internal/agui/auth.go` | 268 | `r.Header.Get("Accept")` — reading Accept header in `wantsHTML` helper | Info | This is NOT an auth-header read; it distinguishes browser GET (302 redirect) from API call (401). It reads `Accept`, not `Authorization`/`X-Identity`. No security concern — this is the standard content-negotiation pattern. |

No TBD/FIXME/XXX/TODO/HACK/PLACEHOLDER markers found in any Phase 24 files.
No stub returns (return nil / return [] / return {}) in auth or handler critical paths.
No third-party session library (gorilla/*) imported.

---

### Human Verification Required

#### 1. Visual Sign-Off: Login Page + Health Panel Against 24-UI-SPEC

**Test:** Boot the binary on loopback (`AURA_AGUI_BIND=127.0.0.1:9080 ./aura serve`) and open `http://127.0.0.1:9080/` to confirm: dark-operator theme with no light-mode flash (D-08); the Aura brand in the header; the runtime health panel with Liveness/Readiness/Postgres/Neo4j/Bind/Build — each with a colored dot AND a text label. Then boot with `AURA_AGUI_BIND=0.0.0.0:9080 AURA_WEB_AUTH_SECRET=test-pass ./aura serve` and open the login page: confirm the labelled passphrase field (`type="password"`, `autocomplete="current-password"`), "Sign in" CTA ≥44px, a role=alert error on wrong passphrase, and session-expired notice on `?expired=1`.

**Expected:** All visual surfaces match the approved `24-UI-SPEC.md`. No unstyled flash. The health panel uses both a dot (color) AND text (label) per row — never color-only. The login form is accessible (WCAG 3.3.1 focus management, contrast ≥4.5:1 on dark tokens).

**Why human:** Playwright E2E headless-proves the DOM structure and data-theme attribute, but only a human can confirm the approved design contract (color scheme, spacing, brand lock-up, touch target sizes, contrast) against the 24-UI-SPEC. The operator checkpoint was completed during execution (commits `1c494d24` + `0fa2d865` fix the two issues found), but the final visual sign-off is the human gate.

#### 2. Fix WEB-04 Documentation Inconsistency

**Test:** Check `REQUIREMENTS.md` line 44 and the tracking table row 166.

**Expected:** Both should show WEB-04 as complete: `[x] **WEB-04**: ...` and `| WEB-04 | Phase 24 | Complete |`.

**Why human:** The code fully implements WEB-04 (RuntimeHealthPanel, useRuntimeHealth, theme-before-paint, Playwright E2E, committed dist, all tests green). The ROADMAP correctly shows Phase 24 as Complete (4/4 plans, 2026-06-16). But REQUIREMENTS.md was not updated when WEB-04 was delivered. This is a one-line doc fix, but a human must confirm and apply it. This is the only gap between the codebase state and the REQUIREMENTS tracking document.

---

### Gaps Summary

No code gaps. The four success criteria (WEB-01/02/03/04) are fully implemented and wired. The `status: human_needed` is driven by two items:

1. **Visual sign-off** — the human-verify checkpoint at Plan 04 Task 3 was executed during implementation (operator reported and fixes were applied), but the formal verifier must surface it as a human item since visual correctness cannot be confirmed programmatically.

2. **WEB-04 documentation inconsistency** — REQUIREMENTS.md checkbox `[ ] WEB-04` and tracking table "Pending" do not reflect the completed implementation. The ROADMAP (authoritative) shows Phase 24 complete. This is a documentation fix, not a code fix.

Once both human items are confirmed, this phase status should be elevated to `passed`.

---

_Verified: 2026-06-16T21:00:00Z_
_Verifier: Claude (gsd-verifier)_
