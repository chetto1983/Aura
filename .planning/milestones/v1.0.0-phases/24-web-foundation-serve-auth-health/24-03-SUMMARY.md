---
phase: 24-web-foundation-serve-auth-health
plan: 03
subsystem: auth
tags: [web-auth, session-cookie, hmac, capability-grants, RequireAuth, WEB-03]
requires:
  - phase: 24-01
    provides: "config.GuardWebBind + Config.WebAuthSecret/WebTrustProxy (the boot guard that confines an unconfigured secret to loopback)"
  - phase: 24-02
    provides: "internal/webui.spaFallback + fallbackExcludedPrefixes (the SPA host + exclusion list the auth gate wraps)"
  - phase: 04
    provides: "internal/identity.Store (GetIdentityByID + HasCapability + the seeded local `*` wildcard, migration 0004)"
provides:
  - "internal/agui auth_cookie.go: validateSecret (fail-closed) + signSession/verifySession (HMAC-SHA256, canonical-base64, absolute-TTL) + setSessionCookie/clearSessionCookie"
  - "internal/agui auth.go: AuthDeps + narrow consumer-side identityChecker interface + POST /login + POST /logout + RequireAuth(next, deps) whole-origin gate + RequireCapability(next, deps, cap)"
  - "cmd/aura: newServeHandler(aguiHandler, agui.AuthDeps) wraps the parent mux in RequireAuth, registers POST /login + /logout, interposes RequireCapability on POST /agent/run; bootServe threads buildAuthDeps"
  - "cmd/aura serve_auth.go: identityCheckerAdapter (*identity.Store -> agui.identityChecker) + buildAuthDeps (sha256(secret) signing key, local-identity binding)"
affects:
  - "25-chat (the chat lane mounts behind RequireAuth + may add mutating routes that take RequireCapability)"
  - "28-gov-read-onboarding (governance write routes land here and attach their own capability gates; CSRF re-evaluation trigger)"
  - "24-04 (the React login page POSTs to /login + reads the gated SPA shell)"
tech-stack:
  added: []
  patterns:
    - "stateless HMAC-SHA256 signed session cookie (stdlib crypto/hmac+sha256+subtle, NO gorilla); canonical-base64 verify rejects non-canonical-encoding tamper bypass"
    - "whole-origin RequireAuth middleware with public-path exceptions handled INSIDE the middleware (login + assets + GET /healthz); no-op pass-through when SecretConfigured==false"
    - "cookie-only credential read (never a client Authorization/X-* header); capability gate interposed on the parent mux via Go 1.22 method-pattern precedence (POST /agent/run > /agent/run)"
    - "return-type-projection adapter (*identity.Store -> agui.Identity) so the agui package declares its own narrow Identity + identityChecker and need not import internal/identity"
key-files:
  created:
    - internal/agui/auth_cookie.go
    - internal/agui/auth.go
    - internal/agui/auth_test.go
    - internal/agui/auth_capability_integration_test.go
    - cmd/aura/serve_auth.go
  modified:
    - cmd/aura/serve.go
    - cmd/aura/serve_webui.go
    - cmd/aura/serve_webui_test.go
key-decisions:
  - "verifySession rejects NON-CANONICAL base64 (re-encode + compare) — without it a single-byte flip of the final base64 char decodes to the same bytes and the re-computed MAC still matches, a tamper bypass the rapid property test caught (Rule 1 auto-fix)."
  - "RequireCapability is EXPORTED so the composition root (cmd/aura) interposes it on the parent mux; the plan's lowercase requireCapability could not be called cross-package."
  - "agui declares its OWN narrow Identity + identityChecker; *identity.Store does NOT satisfy it implicitly (return type differs), so a thin return-type-projection adapter bridges it in BOTH cmd/aura (identityCheckerAdapter) and the agui db_integration test (storeChecker)."
  - "Login-page static assets are scoped to a narrow /login-assets/ public prefix, NOT the broad SPA /assets/ tree (which is only served to an authenticated shell)."
  - "CSRF posture: SameSite=Strict only (no double-submit token) for the same-origin SPA — documented inline with a Phase 28/29 cross-origin-write re-evaluation trigger (T-24-19 accepted)."
patterns-established:
  - "stdlib-only signed-cookie auth boundary (~150 LOC) — the minimal industrial shape over a session library (no-atomic-bombs doctrine)"
  - "narrow consumer-side interface + return-type-projection adapter at the composition root to keep a leaf package import-free of the concrete store"
requirements-completed: [WEB-03]
duration: 13min
completed: 2026-06-16
---

# Phase 24 Plan 03: Web-Auth Boundary (WEB-03) Summary

**Stdlib-only HMAC-SHA256 signed-session-cookie auth boundary: a constant-time, fail-closed `POST /login` that mints an `HttpOnly+Secure+SameSite=Strict` `__Host-` cookie bound to the seeded `local` identity, a `RequireAuth` middleware gating the whole origin except login+assets+`/healthz`, and a `capability_grants` gate on `POST /agent/run` that finally exercises the dormant Phase-4 scaffolding — wired into `bootServe` with loopback dev left unauthenticated.**

## Performance

- **Duration:** 13 min
- **Started:** 2026-06-16T16:20:08Z
- **Completed:** 2026-06-16T16:33:03Z
- **Tasks:** 3
- **Files modified:** 8 (5 created, 3 modified)

## Accomplishments
- **Cookie crypto (D-01/D-02):** `validateSecret` (fail-closed on an empty configured secret, `subtle.ConstantTimeCompare`), `signSession`/`verifySession` (HMAC-SHA256, `hmac.Equal`, canonical-base64, absolute 12h TTL, no panic on any malformed value), `setSessionCookie`/`clearSessionCookie` with the locked `__Host-` attributes. Zero third-party packages.
- **RequireAuth (D-03):** whole-origin gate; public paths are the login route + `/login-assets/` + `GET /healthz` (`/readyz`, `/metrics`, `/debug/vars`, the SPA shell all gated). Reads ONLY the session cookie — a forged `Authorization`/`X-Forwarded-User`/`X-Identity-Id` header grants nothing (test-asserted). No-op pass-through when `SecretConfigured==false`.
- **Capability gate (D-04):** `RequireCapability("agent.run")` interposed on the only mutating route via Go 1.22 method-pattern precedence; the seeded `local` passes via its `*` wildcard. Verified LIVE against the real Postgres-seeded identity (wildcard grant → next; ungranted identity → 403).
- **Wiring:** `newServeHandler` takes `agui.AuthDeps`, registers public `POST /login` + `POST /logout`, and wraps the whole parent mux in `RequireAuth`; `bootServe` derives the signing key from `sha256(AURA_WEB_AUTH_SECRET)`, binds the `local` identity, and threads the deps. Loopback dev boots unauthenticated exactly as before.

## Task Commits

Each task was committed atomically:

1. **Task 1: Cookie crypto + login/logout** - `24abbc2a` (feat) — `auth_cookie.go` + `auth.go` (login/logout) + `auth_test.go`; TDD: implementation + property/unit tests in one feature commit.
2. **Task 2: RequireAuth + capability gate + identity interface** - `b21d94d3` (feat) — `RequireAuth` + `RequireCapability` + public-path table in `auth.go`/`auth_test.go`.
3. **Task 3: Wire into the mux + bootServe + db_integration test** - `cf5a8649` (feat) — `serve_webui.go` + `serve_auth.go` + `serve.go` + `serve_webui_test.go` + `auth_capability_integration_test.go`.

**Plan metadata:** _(this docs commit)_

_TDD: each task's tests and the implementation that satisfies them landed in one feature commit (the codebase's per-task convention, consistent with Plans 01–02)._

## Files Created/Modified
- `internal/agui/auth_cookie.go` (created) - stdlib cookie crypto: validateSecret, signSession/verifySession, set/clearSessionCookie, deriveSigningKey, `__Host-aura_session` name + 12h default TTL.
- `internal/agui/auth.go` (created) - AuthDeps, narrow `identityChecker` interface + `agui.Identity` projection, POST /login + POST /logout handlers, principal-on-context helpers, `RequireAuth`, `RequireCapability`, `isPublicPath`, `redirectToLogin`.
- `internal/agui/auth_test.go` (created) - validateSecret fail-closed table; rapid property tests (sign/verify roundtrip + single-byte-tamper rejection); expiry/wrong-key/malformed rejection; login cookie-flags + no-oracle + fail-closed; logout clear; RequireAuth (valid/no-cookie redirect+401/tamper/deleted-identity/public-path table/pass-through/no-forged-header); RequireCapability (wildcard/forbidden/no-principal/store-error).
- `internal/agui/auth_capability_integration_test.go` (created, `db_integration`) - RequireCapability over the REAL seeded local identity; storeChecker adapter; no-skip-as-green via the shared envOrSkip.
- `cmd/aura/serve_auth.go` (created) - identityCheckerAdapter (*identity.Store → agui.identityChecker) + buildAuthDeps (sha256 key, local-identity binding, SecretConfigured from the non-empty secret).
- `cmd/aura/serve_webui.go` (modified) - newServeHandler takes agui.AuthDeps; registers public POST /login + /logout; interposes RequireCapability on POST /agent/run; wraps the mux in RequireAuth; `agentRunCapability` const.
- `cmd/aura/serve.go` (modified) - bootServe builds buildAuthDeps and threads it into newServeHandler; GuardWebBind + cleanup idiom preserved.
- `cmd/aura/serve_webui_test.go` (modified) - existing test passes a zero AuthDeps (loopback no-op); new TestServeWebuiAuthWiring pins the auth-active wiring end-to-end (gate 401, /healthz public, valid cookie → AG-UI, POST /agent/run → capability gate).

## Decisions Made
- **Canonical-base64 verify** (see frontmatter key-decisions) — the rapid tamper property exposed that `base64.RawURLEncoding` tolerates non-zero unused trailing bits, so two distinct cookie strings can decode to identical bytes. The fix re-encodes the decoded payload+sig and compares to the presented parts, forcing canonical form before the MAC check.
- **Exported `RequireCapability`** so the cmd/aura composition root can interpose it on the parent mux (cross-package call).
- **Return-type-projection adapter** in both cmd/aura and the agui db_integration test, because `*identity.Store` returns `identity.Identity` while agui declares its own `agui.Identity` — they are not structurally identical, so `*identity.Store` does NOT satisfy `identityChecker` directly.
- **Narrow `/login-assets/` public prefix** for the login page's own bundle (not the broad SPA `/assets/` tree).
- **SameSite=Strict-only CSRF** posture (T-24-19 accepted) documented inline with a Phase 28/29 re-evaluation trigger.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Canonical-base64 tamper bypass in verifySession**
- **Found during:** Task 1 (sign/verify property test, RED of the tamper property)
- **Issue:** `base64.RawURLEncoding` decodes non-canonical encodings (non-zero unused trailing bits) to the same bytes. The original `verifySession` MACed the *decoded* payload, so flipping the final base64 char of the payload/sig produced a different cookie string that decoded identically — the re-computed MAC still matched and the forged cookie was accepted (T-24-10 forgery vector). The `TestVerifyTamper` rapid property failed at pos=17 (the payload's last base64 char).
- **Fix:** `verifySession` now re-encodes the decoded payload+sig with `RawURLEncoding` and rejects unless they equal the presented parts, forcing canonical form before the MAC compare.
- **Files modified:** `internal/agui/auth_cookie.go`
- **Verification:** `TestVerifyTamper` (rapid, 100+ cases) green; full agui race suite clean.
- **Committed in:** `24abbc2a` (Task 1 commit)

**2. [Rule 3 - Blocking] *identity.Store does not satisfy agui.identityChecker; export RequireCapability**
- **Found during:** Task 3 (db_integration compile-check + cmd/aura wiring)
- **Issue:** The plan stated `*identity.Store` "satisfies identityChecker implicitly", but agui declares its own `agui.Identity` return type, so the method signatures differ and assignment fails to compile. Separately, the plan's lowercase `requireCapability` cannot be called from cmd/aura (cross-package).
- **Fix:** Added a return-type-projection adapter (`identityCheckerAdapter` in cmd/aura, `storeChecker` in the agui integration test) and exported `RequireCapability`. The narrow-consumer-side-interface intent is preserved — agui stays import-free of internal/identity.
- **Files modified:** `cmd/aura/serve_auth.go`, `internal/agui/auth.go`, `internal/agui/auth_capability_integration_test.go`
- **Verification:** `go build ./...` clean; db_integration test compiles and runs LIVE (2/2 PASS).
- **Committed in:** `cf5a8649` (Task 3 commit)

---

**Total deviations:** 2 auto-fixed (1 security bug, 1 blocking compile/wiring).
**Impact on plan:** Deviation 1 closes a real cookie-forgery vector (essential security correctness). Deviation 2 is a mechanical type-bridge the plan's implicit-satisfaction assumption missed. No scope creep — the boundary surface matches the plan's artifacts exactly.

## Issues Encountered
- The earlier RED of the tamper property left a gitignored rapid failfile under `internal/agui/testdata/rapid/` (confirmed `git check-ignore`); it is not committed and does not affect the suite.

## Verification

- `go vet ./internal/agui/ ./cmd/aura/` — clean.
- `go build ./...` — exits 0.
- `go test ./internal/agui/ ./cmd/aura/` (untagged) — green.
- `go test -race ./internal/agui/` — clean; goleak TestMain reports no leak.
- `go test -tags db_integration -race -p 1 ./internal/agui/ -run TestAgentRunCapability` — **PASS LIVE** on the running stack (wildcard grant → 200; ungranted identity → 403); SKIPs locally with the env unset, `t.Fatal`s under `$CI` (no-skip-as-green).
- `bash scripts/agui_boundary_check.sh` — green (auth.go is in internal/agui; internal/webui stays leaf).
- Acceptance greps: no `gorilla` import in `auth.go`/`auth_cookie.go`; RequireAuth reads no client `Authorization`/`X-*` auth header; stdlib `crypto/hmac`+`sha256`+`subtle` only.
- `go test ./internal/agui/ -cover` = **89.5%** (untagged AND with `db_integration`), above the 85% owned-surface floor.

## Threat Surface Coverage

All of this plan's `<threat_model>` mitigations are in place and test-asserted:
- **T-24-09** (login timing oracle): `subtle.ConstantTimeCompare` in `validateSecret`, fail-closed on empty secret.
- **T-24-10** (session forgery): HMAC-SHA256 + `hmac.Equal` + canonical-base64; single-byte-tamper rejection is a rapid property.
- **T-24-11** (expired-cookie replay): absolute TTL in `issued_at`, server-side `now.After(...)` check; expiry test.
- **T-24-12** (cookie theft): `HttpOnly`+`Secure`+`__Host-` prefix; flag test.
- **T-24-13** (client-supplied auth header): RequireAuth reads only the cookie; the forged-header test asserts no access.
- **T-24-14** (fail-open on empty secret): `validateSecret("p","")==false`; the no-op only fires when `SecretConfigured==false` (loopback-confined by Plan-01).
- **T-24-15** (login oracle): generic 401, no passphrase echo, no "wrong-pass vs no-secret" distinction.
- **T-24-16** (unauthorized mutating route): `RequireCapability("agent.run")` over `HasCapability`; live db_integration over the seeded `local`/`*`.
- **T-24-17** (`/debug/vars`+`/metrics` world-readable): gated behind RequireAuth; public-path table test confirms only `/healthz` is open.
- **T-24-18** (session fixation): the cookie is minted server-side at login with a fresh `issued_at`; a pre-auth cookie has no valid MAC.
- **T-24-19** (CSRF): SameSite=Strict accepted posture, documented inline with the re-evaluation trigger.
- **T-24-SC**: zero packages added — Go stdlib + the already-vendored `pgregory.net/rapid` only.

No new security surface beyond the plan's register.

## Known Stubs
None. The boundary is fully implemented and wired end-to-end; the React login PAGE (the form that POSTs to the live `/login`) is Plan 24-04's frontend deliverable, not a stub in this code.

## User Setup Required
None for this plan's code. To ACTIVATE the boundary at runtime an operator sets `AURA_WEB_AUTH_SECRET` (documented in `.env.example` by Plan 24-01); a non-loopback bind without it fail-fasts via `GuardWebBind` (Plan 24-01). Loopback dev needs no secret.

## Next Phase Readiness
- WEB-03 satisfied; the dormant `capability_grants` scaffolding is now exercised over the real seeded `local` identity (live-verified).
- Plan 24-04 (the React login page + runtime health panel) can POST to the live `/login`/`/logout` and consume the gated SPA shell.
- Phase 25 chat-lane mutating routes can attach `agui.RequireCapability` with their own capability names; Phase 28 governance write routes inherit the same seam (and trigger the CSRF re-evaluation).

## Self-Check: PASSED

- FOUND: internal/agui/auth.go (RequireAuth + RequireCapability + AuthDeps + identityChecker)
- FOUND: internal/agui/auth_cookie.go (validateSecret + signSession/verifySession + set/clearSessionCookie)
- FOUND: internal/agui/auth_test.go (validateSecret/sign-verify/tamper/expiry/login/logout/RequireAuth/RequireCapability)
- FOUND: internal/agui/auth_capability_integration_test.go (db_integration TestAgentRunCapability, ran LIVE 2/2 PASS)
- FOUND: cmd/aura/serve_auth.go (buildAuthDeps + identityCheckerAdapter)
- FOUND: .planning/phases/24-web-foundation-serve-auth-health/24-03-SUMMARY.md
- FOUND commit: 24abbc2a (Task 1 — cookie crypto + login/logout)
- FOUND commit: b21d94d3 (Task 2 — RequireAuth + capability gate)
- FOUND commit: cf5a8649 (Task 3 — mux + bootServe wiring + db_integration test)

---
*Phase: 24-web-foundation-serve-auth-health*
*Completed: 2026-06-16*
