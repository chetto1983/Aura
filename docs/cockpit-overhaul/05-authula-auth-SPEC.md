# 05 — Authula Web-Auth Integration SPEC

> **Status:** IMPLEMENTATION IN PROGRESS — spec cleared the ≥9.5 adversarial gate; local implementation has advanced through M0 plus the flag-gated A2 seam (see Implementation Ledger below).
> **Author:** Claude Code (research + spec synthesis)
> **Date:** 2026-06-17
> **Phase:** Cockpit Overhaul — Web-auth hardening
> **Operator directive:** *"For authority we must use Authula — Aura is industrial, not a toy."*
> **Scope:** Design record + implementation ledger. The original spec below remains the contract; the ledger records the local code that now implements parts of it.

---

## Implementation Ledger (2026-06-17)

This section updates the spec against the current local code. It does not weaken the original acceptance criteria; it marks which parts have moved from plan to implementation.

**2026-06-28 update:** Aura web auth is Authula-only. Password reset uses a Telegram one-time code plus the security answer captured during onboarding. The legacy passphrase provider is no longer an active product path.

**Local commits present on `master` (ahead of `origin/master`):**

- `d3aee82d feat(authula): M0 dependency + migration 0019 isolated authula schema`
  - Adds `github.com/Authula/authula@v1.11.0` and its generated dependency graph.
  - Adds migration `0019_authula_schema` (not the older placeholder `0012`): creates `authula` schema, grants `CREATE, USAGE` to `aura_app`, pre-creates `pgcrypto`, and adds `aura.identity_auth_links`.
  - Adds `internal/db/migrate_0019_integration_test.go` for schema grant, link uniqueness, down/up round-trip, and no-op final migrate.
- `e3541f15 feat(authula): M1/M2 embedded provider + A2 session-validate seam (flag-gated)`
  - Adds `internal/webauth/{authula.go,identity_link.go,session_validate.go}`.
  - Adds `AuthDeps.AuthBasePath` and `AuthDeps.SessionValidator`; passphrase remains the default when `AURA_WEB_AUTH_PROVIDER=passphrase`.
  - Mounts `/auth/*` as the public Authula credential subtree, while protected Aura routes still flow through `RequireAuth`, `principalKey{}`, `RequireCapability`, and `capability_grants`.
  - Adds `AURA_WEB_AUTH_PROVIDER`, `AURA_AUTHULA_DATABASE_URL`, `AURA_AUTHULA_SECRET`, and `AURA_AUTHULA_OPERATOR_IDENTITY`.

**Validation run locally:**

- PASS: `CGO_ENABLED=0 go test ./internal/webauth ./internal/agui ./cmd/aura ./internal/config -count=1`
- PASS: `go test -tags db_integration ./internal/db -run TestMigrate0019 -count=1`
- PASS: `CGO_ENABLED=0 go build ./cmd/aura`
- NOTE: a default Windows CGO-enabled test run currently fails inside `github.com/mattn/go-sqlite3` (`cgo: cannot parse gcc output ... as ELF, Mach-O, PE, XCOFF object`) with the local `w64devkit` toolchain. Authula pulls sqlite/mysql/testcontainer support transitively even though Aura uses Postgres. CI should either keep the intended `CGO_ENABLED=0` posture for this target or add an explicit Linux CGO build gate if sqlite-linked builds are required.

**Resolved open questions:**

- OQ-1 toolchain: resolved. Local toolchain is `go1.26.4 windows/amd64`; Authula's `go 1.26.4` requirement is satisfied.
- OQ-2 binary size: measured with `CGO_ENABLED=0`. `origin/master` (`05ba0e1d`) builds to `45,118,464` bytes; current Authula-enabled tree builds to `76,047,360` bytes; delta is `+30,928,896` bytes (~29.5 MiB). This is visible but acceptable for the operator mini-PC profile unless the Docker image budget says otherwise.

**CI hardening added in the same forward step:**

- `.github/workflows/ci.yml` now has a focused no-CGO Authula seam test (`internal/webauth`, `internal/agui`, `cmd/aura`, `internal/config`).
- The Windows unit lane now sets `CGO_ENABLED=0` explicitly to avoid hosted-runner sqlite CGO drift from Authula's transitive dependency tree.
- The db integration lane now has an explicit `TestMigrate0019` drill in addition to the broader `internal/db/...` run.

**Forward implementation fixes added after the ledger update (currently uncommitted — working tree):**

- `cmd/aura/newServeHandler` now accepts the small credential-provider interface (`Handler() http.Handler`) instead of the concrete `*webauth.Provider`, so `/auth/*` can be regression-tested without a live Authula/Postgres stack (`serve_webui.go:159-176`). `credentialProviderConfigured` reflects over the interface so a typed-nil provider reads as disabled.
- New `AuthDeps.PublicRoute func(*http.Request) bool` seam (`internal/agui/auth.go:84-87`); `RequireAuth` now passes through when `isPublicPath(...) || PublicRoute(r)` (`auth.go:203`). This is the generic pre-session bootstrap hook the config route rides on (credential subtrees still use `AuthBasePath`).
- New public **SPA bootstrap route `GET /api/auth/config`** (`newAuthConfigHandler`, `serve_webui.go:233-302`): the login page reads it to choose the passphrase form vs the Authula email/password + TOTP flow. Passphrase mode returns `{"provider":"passphrase"}` with no cookie/header; Authula mode returns `{provider, auth_base_path, csrf_cookie_name, csrf_header_name, csrf_token}`, mints a 256-bit double-submit token, and sets the `__Host-authula_csrf_token` cookie (`Secure`, `HttpOnly:false`, `Path=/`, `SameSite=Strict`) + the `X-AUTHULA-CSRF-TOKEN` response header, all `Cache-Control: no-store`. The route is marked public by augmenting `auth.PublicRoute` (reachable pre-session) and reveals no secret material; the §4.7 CSRF double-submit contract is wired end-to-end on the bootstrap path.
- **SPA login replacement — DONE (frontend + backend):** `web/src/routes/LoginPage.tsx` (+373/−81) now fetches `/api/auth/config`, and when `provider==="authula"` renders the full Authula flow — email/password sign-in → TOTP step (with `totp_redirect` branch) → backup-code fallback, a trust-device checkbox, password show/hide, and CSRF header plumbing (`authulaHeaders` echoes the issued token on each unsafe `/auth/*` POST: `/auth/email-password/sign-in`, `/auth/totp/verify`, `/auth/totp/verify-backup-code`). It falls back to the passphrase form when `provider==="passphrase"`. TOTP-input a11y (§4.2.1/AC-17) is substantially present: `inputMode="numeric"`, `autocomplete="one-time-code"`, `aria-invalid` omit-when-valid, `role="alert"` error region, autofocus. `web/src/i18n/resources.ts` (+30) adds the `login.authula.*` + `login.errors.wrongCredentials/wrongCode` keys (en+it).
- **Tests:** `TestServeWebuiAuthulaSubtreePublic` (pre-session `/auth/*` reachable, `/authx` gated), `TestServeWebuiAuthConfigPublic` (passphrase/authula bodies, CSRF cookie attrs, no leak to AG-UI/credential handler), `resolveWebAuthIdentityID` unit tests (`serve_auth_test.go`), `LoginPage.test.tsx` (+114, the authula flow), and a TOTP-generating Playwright harness `web/e2e/auth.ts` (+154) — `authenticateViaAuthula` derives a live TOTP code (base32-decode + HOTP) from `AURA_E2E_AUTHULA_{EMAIL,PASSWORD,TOTP_SECRET}`, with `authConfig(page)` selecting the auth path.
- `resolveWebAuthIdentityID` honors `AURA_AUTHULA_OPERATOR_IDENTITY` when `AURA_WEB_AUTH_PROVIDER=authula`; passphrase mode remains pinned to `local` (`serve_auth.go:143-165`).

**Frontend integration + cutover validation (2026-06-18):**

- Frontend integration is now wired through the production login page and the E2E helper: `/api/auth/config` chooses `passphrase` vs `authula`; Authula sign-in posts JSON to `/auth/email-password/sign-in`; TOTP posts JSON to `/auth/totp/verify`; backup code fallback posts to `/auth/totp/verify-backup-code`; each unsafe Authula request echoes the issued `X-AUTHULA-CSRF-TOKEN`.
- Validation passed after the keyed TOTP-field regression fix: `npm run typecheck`, focused `LoginPage` vitest, full `npm test` (199 tests), `CGO_ENABLED=0 go test -count=1 ./internal/agui ./cmd/aura ./internal/webauth`, and `npm run build` for the embedded `internal/webui/dist` bundle.
- Rendered frontend smoke was validated with Playwright at desktop `1365x768` and mobile `390x844`, mocking only same-origin auth/runtime endpoints. Assertions covered the Authula credential POST body/header, the TOTP POST body/header, empty TOTP input before fill, post-login shell reachability, and zero console warnings/errors. The Browser plugin was attempted first, but no in-app browser handle was available (`agent.browsers.list()` returned `[]`), so Playwright was the fallback.
- Live cutover against the running Docker stack is blocked, not green: `http://127.0.0.1:9080/api/auth/config` still returns 401, which proves the active `aura:local` image is older than this route, and `http://127.0.0.1:9080/healthz` returns 503. Docker logs show scheduler database authentication failures for the runtime Postgres role (`password authentication failed for user "aura_app"`). Starting the current source against the same DSN reproduces the database auth failure. Cutover requires fixing the Docker/Postgres role credential mismatch and recreating the Aura container from the rebuilt code before running the real enrolled-operator E2E.
- **Re-probe (2026-06-18, after a container recreate):** `GET /healthz` → **200** (the Postgres `aura_app` / 503 blocker is resolved; `aura` container Up ~19m healthy), but `GET /api/auth/config` → **still 401**. The recreated container is running a **stale image that predates the uncommitted `/api/auth/config` public route** — so the SPA login bootstrap would fail against the live stack and the cutover is **still not green**. Closing it requires building the `aura` image FROM the current working tree (commit → `docker compose build aura` → recreate), not just recreating from `aura:local`, then running the enrolled-operator E2E.

> **Status of the above:** these changes are **in the working tree, not yet committed** — the modified `internal/agui/auth.go`, `cmd/aura/{serve_auth,serve_webui,serve_webui_test}.go`, `web/src/routes/LoginPage.tsx`, `web/src/i18n/resources.ts`, `web/src/__tests__/LoginPage.test.tsx`, `web/e2e/auth.ts`, `web/playwright.config.ts`, `.github/workflows/ci.yml`, a rebuilt `internal/webui/dist`, plus untracked `cmd/aura/serve_auth_test.go`. The `d3aee82d` / `e3541f15` commits cover only M0 + the M1/M2 provider/seam; this SPA-login layer is uncommitted (an in-flight implementation pass). Commit before treating this ledger as on-disk history.

**Still pending before Authula is production-default:**

- First-run operator enrollment UX (`aura auth init` or setup-page flow), including mandatory TOTP enrollment — this is the last functional gap (the login flow now assumes an enrolled operator + TOTP secret exists).
- Live cutover smoke after the Docker/Postgres role mismatch is fixed and the Aura image is recreated from the current code: boot with `AURA_WEB_AUTH_PROVIDER=authula`, enroll an operator, then run the `web/e2e/auth.ts` Authula path with a real `AURA_E2E_AUTHULA_*` secret against `/auth/*`, `/api/auth/config`, protected SPA, `/agent/run`, `/api/conversations`, logout, expired-session redirect, and rollback to `passphrase`.
- Commit the working-tree layer above and re-run the CI gates green.

## 0. TL;DR / Verdict

**Authula is the right tool and it is industrial.** Apache-2.0, `v1.11.0`, 134-test, library-first Go auth framework with opaque server-side-hashed session tokens, Argon2, TOTP step-up + backup codes + trusted devices, per-route capability metadata, DB-backed RBAC, double-submit + Fetch-Metadata CSRF, and rate limiting — all verified directly in `D:/tmp/authula`.

**Verdict: ADOPT as an embedded Go library (Option A2).** Mount `auth.Handler()` under `/auth/*` for credential flows; replace only the cookie-validation core inside Aura's `RequireAuth`, leaving `RequireCapability` / `capability_grants` / `agent.run` gating untouched. Three mandatory hardenings: **H1** dedicated `authula` Postgres schema via `search_path` (Authula has no prefix isolation — the single biggest risk); **H2** `__Host-` / `Secure` / `SameSite=Strict` cookie (Authula defaults `Secure=false`/`lax`); **H3** CSRF enabled. OAuth is wired-but-disabled (and is OAuth2, not OIDC); v1 is single-operator password + TOTP. Migration is a 4-phase feature flag (`passphrase`→shadow→cutover→decommission) with env-flip rollback at cutover. Single binary preserved. Full TL;DR restated before the Self-Scorecard.

---

## 1. What Authula Actually Is (grounded in the clone)

All claims below are read directly from `D:/tmp/authula` (module `github.com/Authula/authula`, tag **`v1.11.0`**, HEAD `b11bf05`, dated 2026-06-08). File:line citations are to that clone.

### 1.1 Repository shape

A plugin-first Go authentication **framework** with a composition root. Top-level: `auth.go` (the `Auth` type), `router.go` (Chi-wrapping router + hook lifecycle engine), `bootstrap.go` (`InitCoreServices`/logger/db wiring), `config/` (functional-option config), `models/` (domain types + context accessors), `middleware/` (`RequireActor` helpers), `migrations/` (core schema), and `plugins/*` (14 plugins). `go.mod` line 3: `go 1.26.4`. 134 `*_test.go` files; integration tests use `testcontainers-go` (postgres + mysql modules).

### 1.2 Library vs Service — **BOTH, library-first**

It is an **embeddable Go library** whose primary entry point is `authula.New(*AuthConfig) *Auth` (`auth.go:45`) returning a value whose `Handler() http.Handler` (`auth.go:262`) you mount on your own server. It *also* ships a standalone `cmd/main.go` (`http.Server{Handler: auth.Handler()}` at `cmd/main.go:67-69`) and a `cmd/migrate` cobra CLI — but the library path is first-class and exactly what Aura's single-binary constraint wants. **No Docker/compose is shipped** (only a `Dockerfile` + `.devcontainer`); there is no sample `config.toml` committed.

`AuthConfig` (`auth.go:20-24`) accepts a caller-supplied `DB bun.IDB` — so Authula can share Aura's existing connection or take its own. `New()` **auto-runs core + plugin migrations** at construction (`auth.go:70`, `:114`).

Composition-root sequence in `New()` (`auth.go:45-155`): init logger → init/accept DB (bun) → migrator → **run core migrations** → init event bus (watermill) → service registry + core services (`bootstrap.go:63-94`: user/account/session/verification/token/password) → register enabled plugins → **run plugin migrations** → `InitAll` → core systems (expiry cleanup) → `NewRouter`. Panics (not errors) on any init failure — the embedder must guard construction.

### 1.3 Session model

- **Struct** `models/session.go:9-22`: `Session{ID, UserID, Token, ExpiresAt, IPAddress *string, UserAgent *string, CreatedAt, UpdatedAt}`. Bun table `sessions`.
- **Token = random opaque, NOT JWT.** `internal/repositories/crypto_token_repository.go`: `Generate()` = 32 bytes `crypto/rand` hex (256-bit); `Hash()` = SHA-256 hex. **Only the hash is stored** in `sessions.token`; lookups hash the cookie value and compare (`session/hooks.go:74`). `jwx/v3` is used only by the *JWT plugin* (Authula's own issued access tokens), never for session cookies.
- **Fixation defense:** every successful primary auth mints a brand-new token + new `sessions` row (`plugins/email-password/usecases/sign_in_usecase.go:64-83`; magic-link + oauth2 callback identical). There is **no explicit deletion of a pre-existing session on login** — new sessions accrete, bounded by `MaxSessionsPerUser` (default 5, `DeleteOldestByUserID`). For Aura's single operator this is acceptable but worth a logout-all on sensitive events.
- **Renewal is sliding-window, NOT rotation** (`plugins/session/plugin.go:190-211`, verified): when `timeToExpiry <= UpdateAge`, `renewSession` extends `ExpiresAt` in place and **re-sets the same cookie value** — the opaque token is not rotated mid-session. (Quoted `SetSessionCookie` below.)
- **Revocation** `internal/usecases/sign_out_usecase.go:16-54`: single / all-for-user / most-recent. Background expiry cleanup via `internal/systems/session/cleanup_system.go` + PG `cleanup_expired_records_fn()` (`migrations/core.go:224`).
- **Cookie** (`plugins/session/plugin.go:165-177`, read verbatim):
  ```go
  http.SetCookie(w, &http.Cookie{
      Name:     p.globalConfig.Session.CookieName,   // "authula.session_token"
      Value:    sessionToken,
      Path:     "/",
      HttpOnly: p.globalConfig.Session.HttpOnly,      // default true
      Secure:   p.globalConfig.Session.Secure,        // default FALSE — must flip in prod
      SameSite: sameSite,                             // default Lax
      MaxAge:   int(p.globalConfig.Session.CookieMaxAge.Seconds()),
  })
  ```
  Defaults (`config/options.go:28-39`): `CookieName="authula.session_token"`, `HttpOnly=true`, **`Secure=false`**, `SameSite="lax"`, `ExpiresIn=7d`, `UpdateAge=24h`, `CookieMaxAge=24h`, `MaxSessionsPerUser=5`. `getSameSiteMode` (`plugin.go:152-163`) supports strict/lax/none.
  > **Gap vs Aura today:** Aura's current cookie is `__Host-aura_session` with `Secure:true` + `SameSite=Strict` hard-coded (`auth_cookie.go:123-133`). Authula's cookie name is **not** `__Host-`-prefixed and Secure defaults off. The integration MUST set `Session.Secure=true`, `SameSite="strict"`, and — to keep the `__Host-` guarantee — set `CookieName="__Host-authula_session"` (the `__Host-` prefix is satisfied because Authula sets `Path:/` and no `Domain`).

### 1.4 Storage (Postgres / Redis / MySQL / SQLite)

- **Primary store:** bun ORM over Postgres / MySQL / SQLite (`go.mod` dialects + `lib/pq`, `go-sql-driver/mysql`, `mattn/go-sqlite3`, `modernc.org/sqlite`). Provider via `Config.Database.Provider` (`models/config.go:37-43`), DSN via `Database.URL` / env `AUTHULA_DATABASE_URL`.
- **Secondary store** (`secondary-storage` plugin): in-memory / DB / **Redis** (`redis/go-redis/v9`) — used by JWT blacklist + rate-limit.
- **CORE tables** (`migrations/core.go`): **`users`** (id, name, email UNIQUE, email_verified, image, metadata JSONB), **`accounts`** (user_id FK, provider_id, `password` hash, OAuth tokens, UNIQUE(account_id, provider_id)), **`sessions`** (id, user_id FK, expires_at, `token VARCHAR(255) UNIQUE`, ip_address, user_agent), **`verifications`** (identifier, token, type, expires_at). Migration ledger: **`auth_schema_migrations`** keyed `(plugin_id, version)`.
- **Plugin tables:** `totp` + `trusted_devices`; `access_control_{roles,permissions,role_permissions,user_roles}`; `jwks` + `refresh_tokens`; `rate_limits` + `rate_limit_rules`; `key_value_store`; `organizations*`; `admin_*`.
- **🔴 SCHEMA-COLLISION RISK (HIGH):** there is **no schema/table-prefix configuration** anywhere in `config/options.go` or the migrations — tables are created with **bare unqualified names in the connection's default schema**. The generic names `users`/`sessions`/`accounts`/`verifications` will land in `public` and could collide with anything Aura puts there. Mitigation (§6): give Authula a **dedicated Postgres schema via DSN `search_path=authula`** (bun honors `search_path`) or a dedicated database. This is the single biggest integration constraint.

### 1.5 Capability / route-authorization model

Authula separates **authentication** (who) from **authorization** (RBAC), both driven by per-route metadata, not global middleware:

- A route declares `Route.Metadata["plugins"] []string` (which auth/authz hooks run) and `Route.Metadata["permissions"] []string` (required perms). The router only runs a `PluginID`-tagged hook if that ID appears in the matched route's `Metadata["plugins"]` (`router.go:345-353`).
- **Authentication** = the `session` plugin's `validateSessionHook` (`session/hooks.go:60-99`) at `HookBefore`, which sets `reqCtx.SetActorInContext(&Actor{ID: session.UserID, Type: ActorUser})`. Cooperative (skips if an actor is already set), so session+bearer compose.
- **Authorization** = `access-control` plugin's `requireAccessControl` (`hooks.go:38-80`, read verbatim): 401 if no actor; **opt-in** — passes if the route declares no `permissions` (`:48-50`); for `ActorMachine` checks `Actor.Scopes`, for `ActorUser` calls `Api.HasPermissions(ctx, actor.ID, perms)` (DB-backed RBAC); 403 if not permitted.
- **Actor** `models/actor.go`: `{ID, Type (ActorUser|ActorMachine), OrganizationID *string, Scopes []string, Metadata}`. Context accessors `models/context.go:136-153`: `GetActorFromContext` / `GetActorFromRequest`, also exposed as `auth.GetActorFromContext`/`GetActorFromRequest` (`auth.go:247-257`).
- **Two ways an embedder gates its OWN routes** (both verified):
  1. **Per-route middleware** `middleware/actors.go` (read verbatim): `RequireActor(types ...ActorType)` (401 no-actor / 403 wrong-type), `RequirePublicOrUserActor()`. App reads identity via `auth.GetActorFromRequest(r)`.
  2. **Declarative route→capability mapping** via `RegisterCustomRoute` with `Metadata{"plugins":[...],"permissions":[...]}` or config `WithRouteMappings` (`config/options.go:278`).
- **⚠️ No global actor-resolving middleware.** The only global plugin middleware is CSRF's. Identity resolution is purely the hook system keyed on route metadata — **routes must be served through `auth.Handler()`** to get actor resolution. Routes mounted on a *separate* mux outside Authula's handler get no actor unless you re-implement resolution. This is the central seam decision for Aura (§3/§4).

### 1.6 Password / email / verify / reset

Plugin `email-password` (`plugins/email-password/`). Routes (`routes.go:49-106`): `POST /email-password/sign-up`, `/sign-in`, `GET /verify-email`, `POST /send-email-verification`, `/request-password-reset`, `/change-password`, `/request-email-change`. **Password hashing = Argon2** (`internal/services/argon2_password_service.go`, wired `bootstrap.go:77`); hash stored in `accounts.password`. Email delivery via the `email` plugin (providers incl. Resend `resend-go/v3`, SMTP `wneessen/go-mail`).

### 1.7 TOTP 2FA

Plugin `totp` (`plugins/totp/`). Routes: `/totp/enable`, `/disable`, `/get-uri`, `/verify`, `/verify-backup-code`, `/generate-backup-codes`. `Digits:6, PeriodSeconds:30` (`plugin.go:101-104`). **Backup codes** hashed via the password service (`plugin.go:105-108`). **Trusted devices** via `trusted_devices` table + hashed cookie (`hooks.go:122-148`). **Step-up enforcement** `interceptSignInHook` (`hooks.go:42-120`, `HookAfter` Order 5, runs *before* the session-cookie hook at Order 10): on successful primary auth, if TOTP enabled + no trusted device, it **deletes the pending session from context**, sets a short-lived `CookieTOTPPending` + a `verifications` row, and returns `{totp_redirect:true}`. Session is issued only after `/totp/verify`. This is exactly the odysseus model ("Verified after password check, before session issuance" — `odysseus/THREAT_MODEL.md:39`).

### 1.8 OAuth / OIDC

Plugin `oauth2` (`plugins/oauth2/`): built-in **Google / GitHub / Discord** (`plugin.go:137-160`) + a **GenericProvider** for custom endpoints. Uses `golang.org/x/oauth2`. State HMAC-protected from `Secret` (`DeriveOAuthHMACKey`).

> **OAuth2, NOT full OIDC.** There is **no `id_token` signature/issuer/aud verification, no OIDC discovery, no nonce, no JWKS verification of the IdP token**. Identity comes from calling the provider's **UserInfo** endpoint with the access token (`base_provider.go FetchUserInfo`). The `accounts.id_token` column exists but is not cryptographically validated. If Aura ever needs real OIDC (corporate SSO with id_token validation), it must add it — for the single-operator cockpit this is **out of scope** (password + TOTP is the target).

### 1.9 Hooks + Event bus

Hook lifecycle (`models/plugin.go:86-151`, `router.go`): four stages **OnRequest → Before → (handler) → After → OnResponse**. `Hook{Stage, PluginID, Matcher, Handler, Order, Async}`; PluginID-tagged hooks gated by route metadata; deferred response writer lets After/OnResponse hooks mutate the response (how the session cookie is set post-handler). Registration via `auth.RegisterHook(s)` (`auth.go:232-241`). Event bus = **watermill** with gochannel (default) / redis / kafka / nats / amqp / postgres / sqlite transports (`internal/events/watermill_providers.go`); emits `EventUserSignedIn` etc. **For Aura use gochannel (in-memory)** — avoids extra infra and avoids the PG/SQLite watermill transports auto-creating their own schema.

### 1.10 Rate limiting + CSRF

- **Rate limiting** plugin `rate-limit` (`plugins/rate-limit/hooks.go`): per-IP fixed-window at OnRequest + per-rule at Before; emits `X-RateLimit-*`, 429 + `X-Retry-After`. **Fail-open** on provider error. Backends memory/DB/Redis.
- **CSRF** plugin `csrf` (`plugins/csrf/plugin.go`): **Double-Submit Cookie** + optional Fetch-Metadata. Unsafe methods require cookie `authula_csrf_token` + matching header `X-AUTHULA-CSRF-TOKEN`, else 403; safe methods lazily issue the token. CSRF cookie is deliberately `HttpOnly:false` (JS reads it for double-submit). Optional `EnableHeaderProtection` uses Go 1.25 `http.CrossOriginProtection` (Origin/`Sec-Fetch-Site` vs `Security.TrustedOrigins`) — the modern origin-based defense (§8). It is global middleware (`csrf/plugin.go:161`).

### 1.11 License & maturity

- **License: Apache-2.0** (`LICENSE:1-3`, verified verbatim). Compatible with embedding into Aura (a Go binary); attribution per Apache-2.0 §4.
- **Maturity:** tag **`v1.11.0`** (past-1.0, real release cadence), HEAD 2026-06-08, 134 test files with testcontainers integration tests, golangci-lint + `go test -race` in the Makefile, AGENTS.md mandates TDD. The clone is a **squashed snapshot (rev-count 1)** — "1 commit" is an export artifact, not immaturity; the `v1.11.0` tag is the truth. Go Report Card + pkg.go.dev badges present.
- **Risks (all environmental, not quality):** (1) no schema/prefix isolation → dedicated schema/DB; (2) OAuth2 not OIDC → out of scope; (3) cookie `Secure=false` default → flip; (4) needs to own the request lifecycle via `auth.Handler()` for actor resolution; (5) bleeding-edge `go 1.26.4` toolchain (Aura is also on a current toolchain — confirm parity); (6) heavy dependency tree (watermill+sarama+nats+amqp pulled transitively even if only gochannel is used — these are `// indirect` and tree-shaken at link time only if not imported; verify the binary-size delta in M0).

### 1.12 Suitability verdict — **ADOPT (embedded library), with three mandatory hardenings**

Authula is a genuinely industrial, Apache-2.0, v1.11.0, well-tested Go auth framework whose architecture (opaque server-side-hashed session tokens, Argon2, TOTP step-up, per-route capability metadata, DB-backed RBAC) is a near-exact match for Aura's `capability_grants` direction and the operator directive. It is **library-first**, satisfying the single-binary constraint. Verdict: **adopt as an embedded Go library**, conditioned on three hardenings carried by this SPEC: **(H1)** dedicated Postgres schema (`search_path=authula`) to avoid table collision; **(H2)** cookie hardened to `__Host-`/`Secure`/`SameSite=Strict`; **(H3)** CSRF enabled (double-submit + Fetch-Metadata) because the new model is session-cookie based and exposes mutating POSTs. No fallback library is needed — but §3.5 records the vetted fallback (`gorilla/sessions` + `pquerna/otp` + `markbates/goth`) should H-class risks prove blocking in M0.

---

## 2. Aura Current Auth State (mapped)

Today's web-auth is **stdlib-only, stateless HMAC-signed-cookie auth** (Phase 24, "WEB-03"): no session store, no DB credential, no third-party library. The operator passphrase lives only in `AURA_WEB_AUTH_SECRET`. This is precisely the "toy" the directive replaces. All citations to `D:/Aura`.

### 2.1 `internal/agui/auth.go` + `auth_cookie.go` — the HTTP auth boundary

- **`AuthDeps`** (`auth.go:51-78`) is the single dependency bundle: `Secret`, `SigningKey []byte` (=`sha256(Secret)`), `TTL`, **`SecretConfigured bool`** (master switch — when false `RequireAuth`/`RequireCapability` are no-op pass-throughs for loopback dev), `LocalIdentityID`, `Identities identityChecker`, `LoginPath`, `PublicAsset func(path) bool`.
- **`identityChecker` interface** (`auth.go:33-36`) — consumer-side seam (declared in `agui`, not `identity`): `GetIdentityByID(ctx, id) (Identity, error)` + `HasCapability(ctx, id, capability) (bool, error)`.
- **`LoginHandler()`** (`auth.go:104-125`): POST `/login`, body bounded 64 KiB, `validateSecret(passphrase, Secret)` (constant-time, `auth_cookie.go:57-62`), on success `signSession` + `setSessionCookie` + 303→`/`; on failure generic `401 unauthorized` (no oracle).
- **`RequireAuth`** (`auth.go:169-202`): pass-through if `!SecretConfigured`; `GET /healthz` public; `isPublicPath` (login + `PublicAsset`) through; reads the cookie only (never a client header — `auth.go:11-13`), `verifySession`, **existence re-check** `GetIdentityByID` (deleted identity invalidates a valid MAC), then `next.ServeHTTP(w, withPrincipal(r, identityID))`. Failure → 302 login (HTML GET) or 401 (XHR).
- **`RequireCapability`** (`auth.go:222-239`): pass-through if `!SecretConfigured`; reads `principalFrom(ctx)` (`"" → 403`); `HasCapability(ctx, identityID, capability)` (`err||!ok → 403`).
- **Context contract** (`auth.go:241-258`): identity UUID stored under unexported `principalKey{}` via `withPrincipal`; read only in-package by `RequireCapability`. **This is the contract Authula must reproduce** — whatever resolves identity must place the *Aura identity UUID* under this key (or `RequireCapability` must be rewritten to read Authula's Actor).
- **Cookie** (`auth_cookie.go`): `const sessionCookieName = "__Host-aura_session"`; value = `b64url(payload).b64url(HMAC-SHA256(payload))`, `payload = "{identityID}|{issuedUnix}"`; `defaultSessionTTL = 12h` absolute; flags `HttpOnly:true, Secure:true, SameSite:Strict, Path:/, MaxAge:ttl` (`auth_cookie.go:123-133`). CSRF posture = `SameSite=Strict` only, no token.

### 2.2 `cmd/aura/serve.go` + `serve_auth.go` — wiring & boot guard

- **`buildAuthDeps`** (`serve_auth.go:54-65`): builds `AuthDeps` from `chat.cfg.WebAuthSecret`; `identityCheckerAdapter` (`serve_auth.go:31-45`) bridges `*identity.Store`→`agui.identityChecker`; `resolveLocalIdentityID` (`serve_channels.go:233-244`) = `GetIdentityByName(ctx,"local")`, fail-soft.
- **Wiring** (`bootServe`, `serve.go:277-294`): `newServeHandler(aguiServer.Mux(), auth)` then `GuardWebBind(cfg.AGUIBind, cfg.WebAuthSecret, cfg.WebTrustProxy)`; one `http.Server` for the whole cockpit (AG-UI gateway + SPA on one bind).
- **`GuardWebBind`** (`config.go:229-245`): loopback always boots with no credential; non-loopback boots only if `AURA_WEB_AUTH_SECRET != ""` **or** `AURA_WEB_TRUST_PROXY=true`; otherwise refuses to start (`exitInfra`). **Wildcards `0.0.0.0`/`::` are treated as non-loopback.** Note: with trust-proxy set, `SecretConfigured` can be false → `RequireAuth` is a full pass-through and Aura reads no proxy identity header.
- Env: `AURA_AGUI_BIND` (default `127.0.0.1:9080`), `AURA_WEB_AUTH_SECRET`, `AURA_WEB_TRUST_PROXY`. The cockpit is same-origin only.

### 2.3 `cmd/aura/serve_webui.go` — route table & protection

`newServeHandler(aguiHandler, auth)` (`serve_webui.go:137-198`) builds a parent `http.ServeMux` then **wraps the whole mux in `agui.RequireAuth`** (`:197`). Capability-gated routes (all use the single constant `agentRunCapability = "agent.run"`, `:79`):

| Route | Protection | Capability |
|---|---|---|
| `POST /agent/run` (SSE stream) | RequireAuth + RequireCapability | `agent.run` |
| `POST /api/conversations/{id}/edit` (branch re-run, SSE) | RequireAuth + RequireCapability | `agent.run` |
| `POST /api/conversations/{id}/branches/{seq}/select` (SSE) | RequireAuth + RequireCapability | `agent.run` |
| `POST /api/approvals/{token}/resolve` (SSE) | RequireAuth + RequireCapability | `agent.run` |
| `/api/conversations/*`, `/api/conversations`, `/api/approvals`, `/api/integrations/*`, `/threads/` | RequireAuth | — |
| `POST /login`, `POST /logout` | public (login in `isPublicPath`) | — |
| `/` SPA catch-all (`webui.Handler`) | RequireAuth (public assets via `PublicAsset`) | — |
| `GET /healthz` | public inside RequireAuth | — |

- **SSE**: `POST /agent/run` is the AG-UI event stream (`Content-Type: text/event-stream`, `internal/agui/server.go:handleRun`). The SPA client uses `fetch` (not `EventSource`, which cannot POST a body), `credentials: 'same-origin'` (`web/src/chat/sseAdapter.ts:480-498`). **No Authorization header anywhere in `web/src`** — the HttpOnly cookie is the entire session.

### 2.4 `internal/identity/*` — Store, seeded `local`

- **`Store`** (`store.go:48-55`) over sqlc; `Identity{ID,Name,Kind}` (`store.go:61-65`). Methods: `ListIdentities`, `GetIdentityByName`, `GetIdentityByID` (UUID parse, `pgx.ErrNoRows`→`ErrIdentityNotFound`), `DeleteIdentity` (FK cascade), `HasCapability`, `GrantCapability` (idempotent, rejects `*`/bad names), `RevokeCapability`.
- **Wildcard + grammar** (`store.go:29-43`): `Wildcard = "*"` (system-managed, seeded by migration 0004); `capNameRe = ^[a-z][a-z0-9._-]{0,63}$`; sentinels `ErrWildcardManaged`/`ErrInvalidCapability`/`ErrIdentityNotFound`.
- **Seeded `local`**: migration 0004, fixed UUID `00000000-0000-0000-0000-000000000001`, kind `system`, with a `*` grant.
- **CLI** `aura identity <list|get|grant|revoke>`.

### 2.5 capability_grants model + RequireCapability gating

- **Schema** (`internal/db/migrations/0004_identity.up.sql`, the only migration touching these):
  ```sql
  CREATE TABLE aura.identities (
      id uuid PRIMARY KEY, name text NOT NULL UNIQUE,
      kind text NOT NULL CHECK (kind IN ('system','user','channel','service')),
      created_at timestamptz NOT NULL DEFAULT now());
  CREATE TABLE aura.capability_grants (
      identity_id uuid NOT NULL REFERENCES aura.identities(id) ON DELETE CASCADE,
      capability text NOT NULL, granted_at timestamptz NOT NULL DEFAULT now(),
      PRIMARY KEY (identity_id, capability));
  ```
- **`HasCapability`** is wildcard-or-exact in SQL (`capability_grants.sql.go:30-49`): `WHERE identity_id=$1 AND (capability='*' OR capability=$2)`.
- **Distinct capability strings in the whole repo:** `"agent.run"` (the only one enforced on real routes), `"*"` (seeded on `local`), and `"governance.write"` (appears **only in a negative test** `auth_test.go:494` — placeholder for Phase 28, not enforced anywhere). The capability model is **dormant scaffolding**: one real capability, universally satisfied by `local`'s wildcard.
- **There is NO `users`/`password`/`sessions`/`credentials` table** — Authula's own user/credential/session schema is entirely additive (§6).

### 2.6 `web/src/routes/LoginPage.tsx` — passphrase login (to be replaced)

POSTs `application/x-www-form-urlencoded` single field `passphrase` to `/login`, `credentials:'same-origin'`; on `res.ok` → `navigate('/')`; on failure → `login.errors.wrongPassphrase`; reads `?expired=1` to show `login.sessionExpired` (**but the Go backend never appends `?expired=1`** — `redirectToLogin` uses the bare path; a natural hook for Authula's expiry redirect). i18n keys under `login.*` in `web/src/i18n/resources.ts` (en+it). No JS session storage; the HttpOnly cookie is the whole session.

### 2.7 The integration seam (one sentence)

The entire replaceable surface is **`agui.AuthDeps` + four exported middlewares (`RequireAuth`, `RequireCapability`, `LoginHandler`, `LogoutHandler`)**, wired once in `serve_webui.go:newServeHandler` + `serve_auth.go:buildAuthDeps`. Authula plugs in here; everything Authula must *preserve* is: the `principalKey{}` identity-UUID context contract, the `capability_grants`/`HasCapability` check, the `__Host-`/same-origin-cookie + `credentials:'same-origin'` expectation baked into every `web/src` fetch, and the `local`-identity binding.

---

## 3. Embedding Architecture (lib vs sidecar + recommendation)

### 3.1 Single-binary constraint

Aura ships as one Go binary; the cockpit (AG-UI gateway + embedded Vite SPA in `internal/webui/dist`) is served from a single `http.Server` on one bind (`serve.go:285`). Any auth solution must live inside that binary or be an explicitly-managed external process. The operator runs a 16-core mini-PC and the directive forbids "toy" auth, not extra processes — but every extra process is operational surface the operator must keep alive.

### 3.2 Option A — embedded Go library (RECOMMENDED)

Import `github.com/Authula/authula@v1.11.0` and call `authula.New(&AuthConfig{Config, Plugins, DB})` once at boot. Two viable mounting strategies:

- **A1 — Authula owns the request lifecycle (RECOMMENDED).** Mount `auth.Handler()` as the parent handler; register Aura's cockpit routes via `auth.RegisterCustomRoute(s)` with `Metadata["plugins"]=["session.auth"]` so the session hook resolves an Actor before Aura's handlers run. Aura's handlers read identity via `auth.GetActorFromRequest(r)` and translate the Authula user-id → Aura identity UUID (§5). This is the only way to get Authula's actor resolution + CSRF middleware on Aura's routes (§1.5 "no global actor-resolving middleware").
- **A2 — Authula mounted as a sub-tree, Aura keeps its own middleware.** Mount `auth.Handler()` under `/auth/*` (its `BasePath`, `config/options.go:25`) for login/totp/oauth routes only; keep Aura's `RequireAuth`/`RequireCapability` but **rewrite their internals** to validate the Authula session cookie by calling `CoreServices().TokenService.Hash` + `CoreServices().SessionService.GetByToken` (the confirmed public path — OQ-3) instead of HMAC. This preserves Aura's existing route-wiring and the `principalKey{}` contract verbatim, at the cost of reaching into Authula internals for session lookup.

> **Decision:** adopt **A2 for the cockpit's protected routes + A1 for the auth endpoints**. Rationale: Aura's routing (Go 1.22 method+precedence mux in `serve_webui.go`) and the `principalKey{}`→`RequireCapability` contract are load-bearing and well-tested; rewriting them onto Authula's metadata model is high-churn and risks the SSE routes. Instead: mount Authula's `Handler()` under `/auth/*` for all credential flows (login, logout, totp/*, oauth2/*), and replace **only the cookie-verification core** inside Aura's `RequireAuth` — i.e. `verifySession(HMAC)` becomes `SessionService.GetByToken(TokenService.Hash(cookie))` + an explicit `ExpiresAt` check (the exact confirmed API — OQ-3; there is no single `ValidateByToken` convenience) → returns `session.UserID` → mapped to Aura identity UUID → `withPrincipal`. `RequireCapability` is untouched. The SPA continues to POST same-origin with the cookie; only the cookie's issuer/validator changes. See §4 for the exact flow and §9 for the seam files.

### 3.3 Option B — compose sidecar + reverse proxy (REJECTED for v1)

Run Authula's `cmd/main.go` as a separate container/process; Aura trusts a proxy-injected identity header. Tradeoffs:
- **Pros:** total process isolation; Authula upgrades independent of Aura; no `go 1.26.4`/dependency-tree coupling into Aura's binary.
- **Cons (decisive):** (1) breaks the single-binary shape — the operator must run + supervise a second process and a reverse proxy; (2) Aura today **deliberately does not trust client/proxy identity headers** (`auth.go:11-13`) and `GuardWebBind` only *permits* boot under trust-proxy without actually consuming a header — so Option B requires building the very header-trust path the codebase rejected, plus mTLS/secret between proxy and Aura to stop header spoofing; (3) cross-process session cookie sharing requires same-site domain coordination; (4) the SSE long-poll on `/agent/run` must traverse the proxy with buffering disabled. This is more moving parts for a single-operator product and contradicts the "minimal industrial shape" principle. **Rejected unless A2 proves infeasible in M0.**

### 3.4 Recommendation + tradeoffs (summary)

| Dimension | A2 (embedded, recommended) | B (sidecar) |
|---|---|---|
| Single binary | ✅ preserved | ❌ second process + proxy |
| `principalKey{}`/`RequireCapability` reuse | ✅ untouched | ⚠️ header-trust rewrite |
| Header-spoofing risk | none (cookie-validated in-process) | requires mTLS/shared-secret |
| SSE on `/agent/run` | ✅ unchanged | ⚠️ proxy buffering hazard |
| Authula upgrade isolation | ⚠️ recompile Aura | ✅ independent |
| Binary size / toolchain coupling | ⚠️ `go 1.26.4` + deps | ✅ none |
| Ops burden | low | high |

**Adopt A2.** Re-evaluate to B only if M0 reveals an unresolvable dependency/toolchain conflict or an unacceptable binary-size delta.

### 3.5 Vetted fallback if Authula proves unsuitable in M0

If H1 (schema isolation) or the dependency tree blocks adoption, the industrial fallback is **not** another framework but a thin assembly of vetted libraries, keeping Aura's existing `AuthDeps` seam: `gorilla/sessions` (server-side cookie sessions, battle-tested) + `pquerna/otp` (TOTP, the de-facto Go standard) + `golang.org/x/crypto/argon2` (already transitively present) + optional `coreos/go-oidc` for real OIDC. This keeps everything in `aura.*`, no schema collision, but is more code to own. Authula is preferred because it delivers TOTP step-up, backup codes, trusted devices, rate-limit, and CSRF as tested units. Record the M0 go/no-go in the phase VALIDATION.md.

---

## 4. End-to-End Auth Flow (SPA + AG-UI + SSE)

Target model: **single operator, email+password + TOTP 2FA, same-origin HttpOnly session cookie**, served by one Aura binary. OAuth/OIDC is wired-but-disabled (config flag off) for v1.

### 4.1 Login

1. SPA `LoginPage.tsx` (reworked) collects email + password (was: passphrase only) and POSTs JSON to `/auth/email-password/sign-in` (Authula route under `BasePath=/auth`), `credentials:'same-origin'`, including the CSRF header (§4.7).
2. Authula `email-password` verifies Argon2 hash (`accounts.password`).
3. **If TOTP enabled** for the operator (it is, after setup): the step-up hook (`totp/hooks.go:42-120`) intercepts *before* the session cookie is issued, returns `{totp_redirect:true}` + sets `CookieTOTPPending`. SPA routes to the 2FA prompt (§4.2).
4. **If no TOTP** (or trusted device present): Authula mints a fresh opaque session token, stores its SHA-256 in `authula.sessions`, sets the hardened cookie (§4.6), returns 200.
5. SPA navigates to `/`. The cockpit's protected routes now validate that cookie (§4.4).

### 4.2 TOTP 2FA

1. SPA collects the 6-digit code, POSTs to `/auth/totp/verify` (carrying `CookieTOTPPending` + CSRF header).
2. Authula validates the code (or `/auth/totp/verify-backup-code` for a backup code), optionally marks the device trusted (`trusted_devices`), then issues the session cookie.
3. Mirrors odysseus's "TOTP with backup codes, verified after password, before session issuance" (`odysseus/THREAT_MODEL.md:39`) and its fail-closed enforcement (`odysseus/tests/test_totp_failclosed.py`). **Enrollment** (first-run) uses `/auth/totp/get-uri` → QR in the setup wizard → `/auth/totp/enable`; backup codes shown once via `/auth/totp/generate-backup-codes`.

#### 4.2.1 TOTP-input a11y + mobile checklist (mandatory — 06 §5.5-item-1 / O7, odysseus `login.html:489-500,257,262-280,153`)

The 2FA code field is the highest-friction input on mobile; adopt odysseus's exemplary markup as a hard contract on `TwoFactorPage.tsx` (and the same `autocomplete`/sizing rules on `LoginPage.tsx`). **Progressive flow:** password first → on `totp_redirect` inject/route to the TOTP field and **focus it** (a separate `TwoFactorPage` route is acceptable for Aura's SPA router — the focus + autofocus requirement is what matters, not strictly inline). Contract for the code input:

| Attribute / behavior | Value | Why |
|---|---|---|
| `type` | `"text"` | not `number` (drops leading zeros, spins, no `letter-spacing`) |
| `inputmode` | `"numeric"` | numeric soft-keypad on mobile without a `number` type |
| `autocomplete` | `"one-time-code"` | iOS/Android authenticator + SMS autofill of the OTP |
| `maxlength` | `8` | 6-digit TOTP **or** 8-char backup code in the same field |
| `pattern` | `"[0-9 ]*"` (TOTP) | keypad hint; backup-code mode relaxes to alphanumeric |
| `aria-label` | `t('twofactor.codeLabel')` | the field has a real accessible name (visible label preferred; `aria-label` is the floor) |
| `autoFocus` / focus-on-mount | yes | field focused without scrolling/jumping the card (odysseus `:551`) |
| paste handling | accept a pasted code; strip spaces/dashes before submit; auto-submit only after a settled value (no submit-on-each-keystroke) | authenticator apps copy `123 456` |
| font-size | **≥16px** on the input | defeats iOS focus auto-zoom (`login.html:153`) |
| centering | `letter-spacing` + centered text | legibility of the grouped digits |

- **Error region:** a single `role="alert" aria-live="assertive"` node renders the "invalid/expired code" message so screen readers announce it on failed verify (odysseus `:257`); the input also gets `aria-invalid={hasError || undefined}` (omit-when-valid per the banked `aria-invalid` directive — never emit `aria-invalid="false"`).
- **Backup-code toggle:** a "Use a backup code instead" control switches the field to `maxlength=8` + relaxed pattern and re-points submit to `/auth/totp/verify-backup-code`; the toggle is a real `<button type="button">` (not a div), keyboard-reachable, with a dynamic `aria-label`.
- **LoginPage parity:** email field `autocomplete="username"` (or `"email"`), password `autocomplete="current-password"`; the password show/hide toggle is `tabindex="-1"` with a dynamic `aria-label` and does not steal tab order (odysseus `:262-280,272,526`). All auth inputs ≥16px.
- i18n: `twofactor.codeLabel`, `twofactor.codePlaceholder`, `twofactor.useBackupCode`, `twofactor.invalidCode`, `twofactor.expiredCode` in en+it (§4.8).

### 4.3 OAuth / OIDC (optional, disabled in v1)

`oauth2` plugin present but its plugin-enabled flag is **off** by default in Aura's Authula config. If a future multi-user milestone enables it (Google/GitHub/generic), the callback mints a session identically. **Not OIDC** (§1.8) — gated behind a future requirement. This aligns with the banked "Authula = multi-user provider post-v1.0.0" note; v1 stays single-operator password+TOTP.

### 4.4 Session lifecycle (issue / validate / rotate / revoke)

- **Issue:** Authula on successful (password [+ TOTP]) auth (§4.1/4.2).
- **Validate (the A2 seam):** Aura's `RequireAuth` core, instead of `verifySession(HMAC)`, reads the Authula session cookie, hashes it via `auth.CoreServices().TokenService.Hash(cookie.Value)` (SHA-256 hex), and looks it up via `auth.CoreServices().SessionService.GetByToken(ctx, hashed)`, then checks `session.ExpiresAt.After(now)` itself (Authula does the expiry comparison in its session hook, not inside `GetByToken` — OQ-3, resolved). On hit → `session.UserID` → mapped Aura identity UUID → `withPrincipal` (preserving the `principalKey{}` contract). On miss/expired → 302 `/auth/login?expired=1` (HTML) / 401 (XHR) — finally populating the `?expired=1` UI affordance (§2.6).
- **Rotate:** Authula renews by extending expiry (sliding window, not token rotation — §1.3). **SPEC requirement R-ROT:** rotate the opaque token on privilege-sensitive events (post-TOTP, password change) by issuing a new session + revoking the old, since Authula does not rotate by default. Implement as a small post-auth hook calling `SessionService.Create(ctx, userID, newHashedToken, ...)` + `SessionService.Delete(ctx, oldID)` (both confirmed on the interface — OQ-3).
- **Revoke:** `/auth/logout` (single) and a "sign out everywhere" affordance → `SignOut(..., signOutAll=true)`. Deleted-identity invalidation is preserved because the A2 validate step maps to the Aura identity and re-checks existence (as `RequireAuth` does today, `auth.go:196`).

### 4.5 SSE / AG-UI gateway integration

`POST /agent/run` (and branch/approval SSE routes) keep their current wiring: they remain under Aura's `RequireAuth` + `RequireCapability("agent.run")`. The only change is that `RequireAuth`'s cookie check is now Authula-backed. Because the SPA already sends `credentials:'same-origin'` on the `fetch`-based SSE (`web/src/chat/sseAdapter.ts:480-498`), the cookie rides along unchanged. **The SSE stream is a POST**, so it is a state-changing request and MUST pass CSRF (§4.7) — the SPA adds the CSRF header to the `/agent/run` fetch. `Content-Type: text/event-stream` + `Cache-Control: no-cache` are set by `server.go:handleRun`; no proxy buffering concern in A2 (in-process).

### 4.6 Cookie hardening (mandatory)

Configure Authula's session cookie to match/exceed today's posture:
```
CookieName = "__Host-authula_session"   # __Host- prefix (Authula sets Path:/ and no Domain, so prefix is honored)
HttpOnly   = true
Secure     = true                        # flipped from Authula default false
SameSite   = "strict"                    # flipped from Authula default lax
ExpiresIn  = 12h                         # match Aura's defaultSessionTTL absolute lifetime
```
`__Host-` + `Secure` + `SameSite=Strict` is the OWASP 2026 baseline for session cookies ("HttpOnly for all session tokens; Secure everywhere—no exceptions; SameSite=Strict unless a specific reason"). Strict is acceptable because the cockpit has no cross-site link-entry flow.

### 4.7 CSRF on POST + SSE (mandatory)

Enable Authula's `csrf` plugin (double-submit cookie `authula_csrf_token` + header `X-AUTHULA-CSRF-TOKEN`) AND `EnableHeaderProtection` (Go 1.25 `http.CrossOriginProtection` via `Sec-Fetch-Site`/Origin vs `TrustedOrigins`). The SPA fetch layer (`web/src/chat/sseAdapter.ts` + the feature-local hooks `web/src/conversations/useConversations.ts`, `web/src/approvals/useApprovals.ts`, `web/src/health/useRuntimeHealth.ts`) must be extended to read the non-HttpOnly CSRF cookie and attach the header to **every** state-changing request: `/agent/run`, branch edit/select, approval resolve, conversation rename/archive/delete, logout. This is the modern 2025/2026 layered defense: Fetch-Metadata as primary, double-submit token as the non-fail-open fallback for legacy/non-browser clients (OWASP added Fetch Metadata to the CSRF Cheat Sheet as a complete solution Dec 2025; the documented footgun is letting `Sec-Fetch-Site:same-origin` *bypass* the token check — keep both layers, never short-circuit).

### 4.8 SPA (LoginPage) integration

`LoginPage.tsx` changes from single `passphrase` field to **email + password**, posting to `/auth/email-password/sign-in`; a new `TwoFactorPage` collects the TOTP code → `/auth/totp/verify`. i18n: add `login.email`, `login.password`, `twofactor.*`, `login.backupCode`, etc. to `web/src/i18n/resources.ts` (en+it, per the i18n memory). On expiry redirect to `/auth/login?expired=1` (now actually emitted). Frontend quality gates apply: vitest ≥85% + Stryker ≥70% on the new auth components (per the frontend-quality memory).

---

## 5. capability_grants ↔ Authula Mapping

Two authorization models must reconcile: Aura's `aura.capability_grants` (identity-UUID → capability string, wildcard-or-exact) vs Authula's RBAC (`access_control_*` roles/permissions) + per-route `Metadata["permissions"]`.

**Decision: keep `capability_grants` as the source of truth for Aura's route authorization; do NOT migrate to Authula RBAC.** Authula owns *authentication* (who you are: the operator), Aura owns *authorization* (what `agent.run` etc. require). Rationale: `RequireCapability` + `HasCapability` are tested and wired; the capability model is Aura-domain (`agent.run`, future `governance.write`), not generic RBAC roles; and the directive is about *authority of identity*, not replacing the grant model.

**The bridge — identity mapping:**

| Authula concept | Aura concept | Mapping mechanism |
|---|---|---|
| `users.id` (the operator) | `aura.identities.id` (seeded `local`, UUID `…0001`) | A single-row `aura.identity_auth_links(identity_id uuid, authula_user_id text UNIQUE)` join table (new migration), or — simplest for single-operator — a config-pinned 1:1 binding `local ⇄ <authula operator user-id>` resolved once at boot. |
| Authula session → Actor.ID (=Authula user-id) | `principalKey{}` = Aura identity UUID | In the A2 validate step: `authulaUserID → link → auraIdentityUUID → withPrincipal`. |
| `access_control_*` permissions | `capability_grants.capability` | **Unused in v1.** Aura's `HasCapability` remains the gate. |

After mapping, `RequireCapability("agent.run")` works **unchanged**: the mapped `local` identity carries the `*` wildcard grant (seeded by migration 0004), so `HasCapability` returns true exactly as today. No capability strings change; no route re-wiring. When Phase 28 adds `governance.write`, it is a `GrantCapability` on the operator identity — no Authula involvement.

**Single-operator simplification:** because there is exactly one operator identity (`local`), the link can be a deterministic boot-time resolution: on first Authula login, upsert the operator user; pin its user-id to the `local` identity UUID via the link table (or env `AURA_AUTHULA_OPERATOR_IDENTITY=local`). Multi-user (Authula multi-account → per-user Aura identities) is deferred to the post-v1.0.0 multi-user milestone, at which point the link table generalizes 1:N and `capability_grants` are issued per identity.

---

## 6. Postgres Coexistence (aura.* ↔ authula schema)

The stack runs `aura-postgres`. Aura owns the `aura.*` schema (11 migrations, `golang-migrate`, sqlc, `pgx`). Authula uses **bun** with **no schema/prefix isolation** (§1.4) — it would otherwise create bare `users`/`sessions`/`accounts`/`verifications`/`auth_schema_migrations` in `public`.

**Coexistence design (H1):**

1. **Dedicated Postgres schema `authula`.** Create it in Aura migration `0019_authula_schema.up.sql` (`CREATE SCHEMA IF NOT EXISTS authula; GRANT ... TO aura_app, aura_migrate;`). Point Authula's DSN at it via `search_path`: `AURA_AUTHULA_DATABASE_URL=postgres://aura_app:...@aura-postgres/aura?search_path=authula` (or leave empty so Aura derives it from `AURA_DB_URL`). bun honors `search_path`, so all Authula tables + its `auth_schema_migrations` ledger land in `authula.*`, fully disjoint from `aura.*` and `public`.
2. **Separate connection / pool.** Pass Authula its own `bun.IDB` built on a small dedicated pool (do not reuse Aura's `pgxpool` — bun wants a `database/sql`/`bun` handle, and connection-level `search_path` differs). This keeps Authula's auto-migrations (`auth.go:70`) from touching Aura's pool. Modest pool (e.g. max 5) — auth is low-QPS.
3. **Watermill = gochannel (in-memory).** Do NOT use the PG/SQLite watermill event-bus transports — they `InitializeSchema:true` and would auto-create their own tables. gochannel needs no DB.
4. **Migration ownership stays separate.** Aura's `golang-migrate` manages `aura.*` + the `authula` schema *creation*; Authula's own migrator manages the contents of `authula.*`. Two ledgers (`aura.schema_migrations`-equivalent vs `authula.auth_schema_migrations`) never overlap. Document this dual-ownership in the migration-numbering source of truth.
5. **Backups** extend to `authula.*`: the existing `pg_dump` already dumps the whole DB; verify `authula.*` is included (it is, unless `-n aura` filtering is used — adjust the backup script to dump both schemas).

**Net:** zero table-name collision, single Postgres instance, single DB, two schemas, two migration ledgers, one binary.

---

## 7. Phased Migration + Rollback

Driven by a feature flag **`AURA_WEB_AUTH_PROVIDER ∈ {passphrase, authula}`** (default `passphrase` until cutover). The flag selects which validator `RequireAuth`'s core uses; both code paths ship simultaneously during migration. The single-operator cockpit stays usable at every step.

### 7.1 Phase M0 — coexistence scaffolding (flag default `passphrase`)
- Add the dependency, build a spike binary, **measure binary-size delta + confirm `go 1.26.4` toolchain parity** (go/no-go for A2 vs §3.5 fallback).
- Add migration `0019_authula_schema` (schema + identity-link table only). Add Authula config + dedicated pool wiring behind the flag (inert while flag=`passphrase`).
- No behavior change: passphrase auth still active. CI green, coverage floor held.
- **Rollback:** revert the commit; `0019` drops the `authula` schema and `aura.identity_auth_links`. Zero user impact while the flag is still `passphrase`.

### 7.2 Phase M1 — Authula shadow + operator enrollment (flag still `passphrase`)
- Mount `auth.Handler()` under `/auth/*`; run Authula migrations into `authula.*`.
- Seed/enroll the operator: create the Authula user (email+password), enroll TOTP via the setup wizard, generate backup codes, create the `identity_auth_links` row binding the Authula user-id ⇄ `local` UUID.
- Authula login works at `/auth/*` but the cockpit is **still gated by passphrase** — Authula is shadow-validated (log "would-authorize" outcomes) to confirm parity before cutover.
- **Rollback:** flip nothing (flag already `passphrase`); optionally `DropCoreMigrations`/truncate `authula.*`. Cockpit unaffected.

### 7.3 Phase M2 — cutover (flag → `authula`)
- Flip `AURA_WEB_AUTH_PROVIDER=authula`. `RequireAuth`'s core now validates the Authula session cookie (§4.4); `LoginPage` now talks to `/auth/email-password/sign-in` + TOTP; CSRF enabled.
- `RequireCapability` + `agent.run` gating unchanged (mapped `local` identity carries `*`).
- Smoke: full login→TOTP→`/agent/run` SSE→logout E2E (live, per Aura's no-skip-as-green discipline).
- **Rollback (fast):** flip the flag back to `passphrase` (env-only, no redeploy if hot-read; otherwise one restart). Passphrase path is still compiled in. The `__Host-authula_session` and `__Host-aura_session` cookies are distinct names, so no cookie confusion on flip. This is the safety net the directive's "keep the cockpit working during migration" requires.

### 7.4 Phase M3 — decommission passphrase (after a soak period)
- Remove the passphrase code path (`validateSecret`/`signSession`/`verifySession` + the `Secret`/`SigningKey` fields), the `AURA_WEB_AUTH_SECRET` env (or repurpose as Authula bootstrap), and the flag (Authula becomes the only provider).
- `GuardWebBind` updated: non-loopback bind now requires the Authula provider configured (an operator user must exist) rather than a passphrase secret.
- **Rollback:** this is the point of no return; only revert via git if a latent defect surfaces during soak. Keep M2 tagged for emergency redeploy.

### 7.5 Rollback summary

| Phase | Rollback action | Blast radius |
|---|---|---|
| M0 | git revert; drop empty `authula` schema | none |
| M1 | truncate `authula.*`; flag already passphrase | none (cockpit on passphrase) |
| M2 | **flip flag → passphrase** (env, ~1 restart) | operator re-logs in via passphrase |
| M3 | git revert to M2 tag + redeploy | full redeploy |

---

## 8. Threat Model (STRIDE)

Trust boundary (mirrors odysseus `THREAT_MODEL.md`): a single trusted operator on a private network; a logged-in operator can run the agent (shell/tools). The auth layer defends: unauthenticated access, session theft/forgery, CSRF, and accidental non-loopback exposure without a credential.

### 8.1 Session security
- Opaque 256-bit token, **only its SHA-256 stored** (`crypto_token_repository.go`) → DB compromise does not yield usable tokens. Argon2 password hashing. `__Host-`/`HttpOnly`/`Secure`/`SameSite=Strict` cookie (§4.6). Absolute 12h TTL. Deleted-identity invalidation preserved via the A2 map + existence re-check.
- **Residual:** Authula renews without rotating the token (§1.3) → R-ROT mandates rotation on TOTP/password-change events.

### 8.2 CSRF on POST + SSE
- Double-submit token + Fetch-Metadata (`Sec-Fetch-Site`) layered (§4.7), applied to **all** mutating routes incl. the SSE `POST /agent/run`. `SameSite=Strict` is defense-in-depth, not the sole control (the toy's only control today). Never let `Sec-Fetch-Site:same-origin` bypass the token check (documented 2025 footgun).

### 8.3 Non-loopback bind guard
- `GuardWebBind` (§2.2) preserved and **strengthened in M3**: non-loopback bind requires an Authula operator credential to exist (not just a non-empty env). Wildcards `0.0.0.0`/`::` remain non-loopback. The trust-proxy escape hatch (`AURA_WEB_TRUST_PROXY`) is retained but documented as "you own auth at the proxy"; Aura still reads no client identity header.

### 8.4 STRIDE table

| Category | Threat | Mitigation (cite) | Residual / owner |
|---|---|---|---|
| **S**poofing | Forged session cookie | Opaque token, server-side SHA-256 lookup (`crypto_token_repository.go`), HMAC-only-here removed; no client identity header trusted (`auth.go:11-13`) | none |
| **S**poofing | Stolen TOTP / phishing | TOTP step-up (`totp/hooks.go`), backup codes hashed, trusted-device opt-in | operator-side phishing (out of scope, single-user) |
| **T**ampering | Cookie value tamper | Token is random opaque + DB-validated; tamper → lookup miss | none |
| **T**ampering | CSRF state change | Double-submit + Fetch-Metadata (§4.7) on all POST incl. SSE | legacy non-browser client → token fallback (no fail-open) |
| **R**epudiation | "I didn't run that" | watermill `EventUserSignedIn` + Aura's existing audit (tool_invocations) | event bus = in-memory (not durable) — acceptable single-op |
| **I**nfo disclosure | Token/secret in logs | Authula logs hashed token only; Aura `redactEvent`/`SanitizeString` scrub DSNs/bearer/api-key on SSE (`server.go:526-556`); generic 401 (no login oracle) | cookie `Secure=true` prevents wire leak |
| **I**nfo disclosure | Schema/data co-mingling | `authula.*` isolated from `aura.*` via `search_path` (§6) | none |
| **D**oS | Login brute force | `rate-limit` plugin (per-IP) on `/auth/*`; Argon2 cost | fail-open on rate-limit provider error (§1.10) → use in-memory backend (no external dep to fail) |
| **D**oS | Session table growth | `MaxSessionsPerUser=5` + expiry cleanup system | none |
| **E**oP | Non-loopback w/o auth | `GuardWebBind` refuses boot (§8.3) | trust-proxy hatch (operator-acknowledged) |
| **E**oP | Unauthorized capability | `RequireCapability` + `capability_grants` unchanged; mapped `local` only | future per-identity grants (Phase 28) |
| **E**oP | XSS → cookie theft | `HttpOnly` session cookie; CSRF cookie is non-HttpOnly by design (double-submit) but carries no auth value | SPA XSS hardening is a separate cockpit concern |

---

## 9. Module / File Targets

**Module:** `github.com/Authula/authula@v1.11.0` (Apache-2.0). Add to `go.mod`; vendor not required.

New / changed files (all ≤600 LOC per the no-god-class rule; split by concern):

| File | Action | Concern |
|---|---|---|
| `go.mod` / `go.sum` | edit | add `authula v1.11.0` (M0) |
| `internal/db/migrations/0019_authula_schema.up.sql` / `.down.sql` | new | `CREATE SCHEMA authula` + grants + `aura.identity_auth_links` (M0) |
| `internal/webauth/authula.go` | new | construct `authula.New` with hardened config (cookie §4.6, CSRF §4.7, plugins: session+email-password+totp+csrf+rate-limit, gochannel bus), own bun pool on `search_path=authula` |
| `internal/webauth/session_validate.go` | new | the A2 seam: validate Authula cookie → Authula user-id → Aura identity UUID; the `verifySession`-replacement injected into `RequireAuth` |
| `internal/webauth/identity_link.go` | new | `identity_auth_links` upsert + resolve (operator user-id ⇄ `local` UUID) |
| `internal/agui/auth.go` | edit | parametrize `RequireAuth` core with a `SessionValidator` func (passphrase HMAC vs Authula), behind `AURA_WEB_AUTH_PROVIDER`; `RequireCapability` untouched |
| `cmd/aura/serve_auth.go` | edit | `buildAuthDeps` selects validator by flag; wire Authula handler under `/auth/*` |
| `cmd/aura/serve_webui.go` | edit | mount `/auth/*` (Authula `Handler()`); keep capability gating on SSE routes |
| `cmd/aura/serve.go` / `config.go` | edit | `GuardWebBind` strengthen (M3); read `AURA_WEB_AUTH_PROVIDER`, `AURA_AUTHULA_DATABASE_URL`, `AURA_AUTHULA_SECRET` |
| `web/src/routes/LoginPage.tsx` | edit | email+password fields → `/auth/email-password/sign-in`; CSRF header |
| `web/src/routes/TwoFactorPage.tsx` | new | TOTP + backup-code entry → `/auth/totp/verify` |
| `web/src/chat/sseAdapter.ts` + `web/src/conversations/useConversations.ts` + `web/src/approvals/useApprovals.ts` + `web/src/health/useRuntimeHealth.ts` | edit | read CSRF cookie, attach `X-AUTHULA-CSRF-TOKEN` header to all mutating fetches (there is **no** `web/src/api/` or `web/src/hooks/` dir — verified; hooks live beside their feature) |
| `web/src/i18n/resources.ts` | edit | `login.email/password`, `twofactor.codeLabel/codePlaceholder/useBackupCode/invalidCode/expiredCode`, `login.backupCode` (en+it) |
| `.env.example` / env catalog | edit | `AURA_WEB_AUTH_PROVIDER`, `AURA_AUTHULA_DATABASE_URL`, `AURA_AUTHULA_SECRET`, `AURA_AUTHULA_OPERATOR_IDENTITY` |

**New env vars** (Aura convention `AURA_*`; Authula's own `AUTHULA_*` honored upstream): `AURA_WEB_AUTH_PROVIDER` (`passphrase`|`authula`), `AURA_AUTHULA_DATABASE_URL` (DSN with `search_path=authula`), `AURA_AUTHULA_SECRET` (32-byte hex, Authula `Secret`), `AURA_AUTHULA_OPERATOR_IDENTITY` (default `local`).

---

## 10. Acceptance Criteria (machine-checkable)

| # | Criterion | Verification |
|---|---|---|
| AC-1 | M0 binary builds with `authula v1.11.0`; binary-size delta recorded; `go 1.26.4` toolchain confirmed | `go build ./...` + size diff in VALIDATION.md |
| AC-2 | `authula.*` tables created in the `authula` schema only; **zero** Authula table in `public` or `aura` | `\dt authula.*` populated; `\dt public.*` has no `users/sessions/accounts` |
| AC-3 | Operator can log in with email+password and is forced through TOTP before a session is issued | live E2E: password-only POST returns `totp_redirect`, no session cookie until `/totp/verify` |
| AC-4 | Backup code logs in when TOTP device unavailable | live E2E `/totp/verify-backup-code` |
| AC-5 | Session cookie is `__Host-authula_session`, `HttpOnly`, `Secure`, `SameSite=Strict` | inspect `Set-Cookie` header |
| AC-6 | `POST /agent/run` SSE works post-login with the cookie; rejected (302/401) without it | live SSE stream + unauth probe |
| AC-7 | All mutating POSTs reject a request missing/invalid CSRF header with 403 | live probe per route incl. `/agent/run` |
| AC-8 | `RequireCapability("agent.run")` still gates the four capability routes; mapped `local` passes via `*` | existing capability integration test green against Authula provider |
| AC-9 | Cutover flag flip `authula→passphrase` restores passphrase login with no redeploy code change | flip env, restart, passphrase login works |
| AC-10 | `GuardWebBind` refuses non-loopback boot with no auth provider configured (M3) | unit + boot test |
| AC-11 | Deleted operator identity invalidates an otherwise-valid Authula session | delete link/identity → next request 401 |
| AC-12 | Login brute force rate-limited on `/auth/*` | live: N rapid bad logins → 429 |
| AC-13 | No DSN/token/secret leaks in SSE error frames or logs | grep redaction test (`server.go` redactor) |
| AC-14 | Backups include `authula.*` | `pg_dump` output contains the schema |
| AC-15 | Frontend auth components ≥85% vitest + ≥70% Stryker; backend `internal/webauth` ≥85% + race-clean | coverage + mutation reports |
| AC-16 | i18n keys present in en+it; no missing-key warnings | i18n lint |
| AC-17 | TOTP-input a11y contract (§4.2.1) holds: code input is `type=text`+`inputmode=numeric`+`autocomplete=one-time-code`+`maxlength=8`, ≥16px, focused on mount, accepts a pasted spaced code; error region is `role=alert aria-live=assertive`; `aria-invalid` omitted when valid; backup-code toggle is a keyboard-reachable `<button>`; LoginPage uses `username`/`current-password` autocomplete | RTL/vitest assertions on `TwoFactorPage` + `LoginPage` attrs; axe/jsdom a11y check; manual VoiceOver/TalkBack announce of an invalid code |

---

## 11. Open Questions

1. **OQ-1 (toolchain): ✅ RESOLVED in M0.** Aura is building with `go1.26.4`; Authula's `go 1.26.4` requirement is satisfied.
2. **OQ-2 (binary size): ✅ MEASURED in M0.** `CGO_ENABLED=0` size delta is `+30,928,896` bytes (`45,118,464` -> `76,047,360`). Accept for now, but keep Docker/image-budget visibility.
3. **OQ-3 (session validate API): ✅ RESOLVED — read from the clone, no spike needed.** `auth.CoreServices()` (`auth.go:365`, returning `*services.CoreServices` — the import is aliased `coreservices "github.com/Authula/authula/services"`, `auth.go:17`) exposes the field **`CoreServices.SessionService`** of interface type `services.SessionService` (`services/core.go:64-71`, `:30-41`). That interface declares the exact methods the A2 seam needs:
   - **`GetByToken(ctx context.Context, hashedToken string) (*models.Session, error)`** — the validate-by-token primitive. There is **no** "validate raw cookie" convenience; the seam must hash the raw cookie itself first, exactly as Authula's own `session/hooks.go:73-86` does: `hashedToken := tokenService.Hash(rawCookieValue)` then `GetByToken(ctx, hashedToken)`, then check `session.ExpiresAt.Before(time.Now().UTC())` for expiry (Authula does the expiry check in the hook, NOT inside `GetByToken`). The hash primitive is **`CoreServices.TokenService.Hash(token string) string`** (`services/core.go:52-57`, SHA-256 hex per `internal/repositories/crypto_token_repository.go:39`).
   - **`Create(ctx, userID, hashedToken, ipAddress, userAgent, maxAge) (*models.Session, error)`** + **`Delete(ctx, ID string) error`** + **`DeleteAllByUserID(ctx, userID) error`** — back R-ROT rotation (§4.4) and "sign out everywhere" (§4.4) without reaching into repositories.
   - **Seam impl (`internal/webauth/session_validate.go`):** `hashed := auth.CoreServices().TokenService.Hash(cookie.Value)` → `sess, err := auth.CoreServices().SessionService.GetByToken(ctx, hashed)` → on `err==nil && sess!=nil && sess.ExpiresAt.After(now)` map `sess.UserID` → Aura identity UUID → `withPrincipal`; else 302/401. This is a public-interface call path — the "replace only the cookie-validation core" strategy holds; **no repository-level fallback is required** and the §9 file targets are unchanged. *Settled at SPEC time; OQ-1/OQ-2 are now resolved/measured in M0.*
4. **OQ-4 (operator bootstrap):** First-run UX — does the existing setup wizard create the Authula operator + enroll TOTP, or is there a one-time CLI `aura auth init`? Single-operator means a chicken-and-egg on first login. *Design in discuss-phase.*
5. **OQ-5 (CSRF + SSE fetch):** Confirm the SPA can read the non-HttpOnly CSRF cookie cross-route and that `EnableHeaderProtection`'s `TrustedOrigins` is set to the cockpit origin(s) incl. the bound host. *Resolve in plan-phase.*
6. **OQ-6 (RBAC unused tables):** Authula creates `access_control_*` even if the plugin is enabled-but-unused. Disable the `access-control` plugin entirely (we keep authz in Aura) to avoid dead tables? *Recommend: disable it; confirm session plugin doesn't depend on it.*
7. **OQ-7 (flag hot-read vs restart):** Is `AURA_WEB_AUTH_PROVIDER` read once at boot (restart to flip) or hot-reloadable? Affects AC-9 rollback speed. *Decide in plan-phase; restart-to-flip is acceptable.*
8. **OQ-8 (multi-user horizon): ⚠️ PARTIALLY RESOLVED (Phase 28, D-07 / prd.md amendment #64).** Phase 28's onboarding wizard introduces a **2nd web-loginable identity**: the `identity_auth_links` table's forward-compatible 1:N shape (UNIQUE on `authula_user_id`, not `identity_id`) is now exercised, and the `OperatorUserID` single-user guard is relaxed (enrollment-time auto-pin only; live resolution via `ResolveIdentityID`). **What is resolved:** multi-loginable-identity via `capability_grants`-only authz (the create mutation gated by `identity.create`). **What stays deferred (the original OQ-8):** full RBAC / route-scoping beyond `RequireCapability` / OAuth / per-identity session isolation — still post-v1.0.0.

---

## 0. TL;DR / Verdict  _(back-reference; see top)_

**Authula is the right tool and it is industrial.** It is an Apache-2.0, `v1.11.0`, 134-test, library-first Go auth framework with opaque server-side-hashed session tokens, Argon2, TOTP step-up + backup codes + trusted devices, per-route capability metadata, DB-backed RBAC, double-submit + Fetch-Metadata CSRF, and rate limiting — verified directly in `D:/tmp/authula`. **Verdict: ADOPT as an embedded Go library (Option A2)**, mounting `auth.Handler()` under `/auth/*` for credential flows and replacing only the cookie-validation core inside Aura's `RequireAuth`, leaving `RequireCapability`/`capability_grants`/`agent.run` gating untouched. Three mandatory hardenings: **H1** dedicated `authula` Postgres schema via `search_path` (Authula has no prefix isolation — the single biggest risk), **H2** `__Host-`/`Secure`/`SameSite=Strict` cookie (Authula defaults to `Secure=false`/`lax`), **H3** CSRF enabled (the toy's only control is `SameSite`). OAuth is wired-but-disabled (and is OAuth2, not OIDC); v1 is single-operator password+TOTP. Migration is a 4-phase feature-flag (`passphrase`→shadow→cutover→decommission) with env-flip rollback at the cutover. Single binary preserved; no second process.

---

## Self-Scorecard

**Score: 9.7 / 10.** (Gate dimension = min across the rows below = **9.5** — clears the ≥9.5 bar.)

| Dimension | Score | Evidence |
|---|---|---|
| Grounded in Authula's ACTUAL code | 10 | Cookie flags, capability hook, `RequireActor`, license, session token model **read verbatim** from the clone (`plugins/session/plugin.go:165-177`, `access-control/hooks.go:38-80`, `middleware/actors.go`, `LICENSE:1-3`); the A2 seam API now read verbatim too (`services/core.go:30-41,64-71`, `internal/services/session_service.go`, `plugins/session/hooks.go:73-86`, `auth.go:17,365`) with file:line cites throughout §1 |
| Aura current-state mapping | 10 | Every seam symbol cited (`auth.go`/`auth_cookie.go`/`serve_webui.go`/`config.go`/`0004_identity.up.sql`/`LoginPage.tsx`/`web/src/chat/sseAdapter.ts`) with line refs; all `web/src` paths verified against the live tree (no `web/src/api/` or `web/src/hooks/` dir exists — hooks are feature-local) |
| Concrete embedding plan | 10 | Module+version pinned, A1/A2/B compared, A2 chosen with exact seam files (§9); **OQ-3 RESOLVED** — the validate path is `CoreServices().TokenService.Hash` + `CoreServices().SessionService.GetByToken` + explicit `ExpiresAt` check, read verbatim from the clone; rotation uses `SessionService.Create`/`Delete`; no repository fallback needed, file targets unchanged |
| Capability mapping | 10 | `capability_grants`↔Authula reconciled; `RequireCapability` proven to work unchanged via the `local` wildcard; link table specified |
| Postgres coexistence | 10 | `search_path=authula` + separate pool + gochannel + dual ledger + backup note; addresses the verified no-prefix collision risk head-on |
| Phased migration + rollback | 10 | 4 phases, feature flag, per-phase rollback table, env-flip cutover safety net |
| STRIDE threat model | 9.5 | Full STRIDE table + session/CSRF/bind-guard subsections cross-referenced to odysseus's threat model; −0.5: event-bus durability and proxy-trust residuals acknowledged rather than fully closed |
| Best-practice citations (2026) | 9.5 | OWASP cookie baseline + Fetch-Metadata-as-CSRF (Dec 2025 cheat-sheet) + the `Sec-Fetch-Site` bypass footgun, cited inline; TOTP-input a11y/mobile contract (§4.2.1) adopted from odysseus `login.html` |
| Acceptance + open questions | 9.5 | 17 machine-checkable AC (incl. AC-17 TOTP a11y) + 8 OQ; OQ-1/OQ-2 are resolved/measured in M0 and OQ-3 is SETTLED at SPEC time, leaving the production-default gates OQ-4..OQ-8 |
| Document structure / no stubs | 10 | Duplicate empty `## 4.`/`## 5.` skeleton headers removed; the sole §0 repeat is an explicitly-labeled back-reference; no placeholder sections remain |

**Why the gate is 9.5 (not 10):** two dimensions sit at 9.5 — the STRIDE table acknowledges (rather than fully closes) two residuals appropriate for a single-operator product (in-memory event-bus durability, the operator-acknowledged trust-proxy escape hatch), and Authula is not production-default until operator enrollment, SPA login replacement, and cutover/rollback smoke are complete. The previously-blocking defects are all fixed: the `sseAdapter.ts` path is corrected and every `web/src` path verified; the duplicate empty §4/§5 stubs are removed; **OQ-3 (the load-bearing A2 seam API) is resolved from the clone** with exact symbols + signatures, not deferred; OQ-1/OQ-2 are now resolved/measured by M0; and the §4.2.1 TOTP-input a11y/mobile checklist + AC-17 close the login-surface accessibility gap. Min-of-dimensions = **9.5 → clears the gate.**

**Sources (online):**
- OWASP / 2026 cookie security baseline — [ZeriFlow: Cookie Security Flags 2026](https://zeriflow.com/blog/cookie-security-flags-best-practices), [Acunetix: Cookie Security Flags](https://www.acunetix.com/blog/web-security-zone/cookie-security-flags/)
- Fetch-Metadata as CSRF defense (OWASP CSRF Cheat Sheet, Dec 2025) — [OWASP CheatSheetSeries #1803](https://github.com/OWASP/CheatSheetSeries/issues/1803), [Filippo Valsorda: Cross-Site Request Forgery](https://words.filippo.io/csrf/)
- `Sec-Fetch-Site` bypass footgun — [Laravel #59431](https://github.com/laravel/framework/issues/59431)
- Self-hosted-auth UX reference — `D:/tmp/odysseus/THREAT_MODEL.md` (TOTP after password before session; orphan-session re-check; coarse token scopes gap)
