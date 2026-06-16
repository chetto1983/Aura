# Phase 24: Web Foundation — Serve + Auth + Health - Research

**Researched:** 2026-06-16
**Domain:** Go single-binary web-serve boundary — SPA-fallback host, signed-session-cookie web-auth, non-loopback fail-fast boot guard, read-only runtime health shell (all on the ONE existing loopback `http.Server`)
**Confidence:** HIGH (every claim grounded in real Aura source read this session: `cmd/aura/serve*.go`, `internal/agui/{server,readiness}.go`, `internal/webui/{embed,doc}.go`, `internal/identity/store.go`, `internal/config/config.go`, `web/index.html`, `scripts/agui_boundary_check.sh`, plus golang-security skill refs and the Go stdlib `net/http.Cookie` surface)

---

<user_constraints>
## User Constraints (from CONTEXT.md)

These are LOCKED. Research is HOW to implement them, not WHETHER. The planner MUST honor every row verbatim.

### Locked Decisions

- **D-01 — In-binary login = env operator-secret + login form.** A passphrase in `AURA_WEB_AUTH_SECRET` is validated **constant-time** (`crypto/subtle`, fail-closed) by a `POST /login`-style endpoint that issues the signed session cookie. No new credential storage. Rejected: one-time bootstrap token; reusing the Phase-9a setup-wizard token.
- **D-02 — Session cookie = signed, `HttpOnly + Secure + SameSite=Strict`, bound to the operator `identity` row.** Cookie attributes locked by ROADMAP SC3. The bound identity is the `capability_grants` seam (CORE-03). Login is **really built and wired this phase**, not a skeleton.
- **D-03 — Whole origin private (in-binary cookie path).** When exposed non-loopback via the in-binary cookie, **every route requires the session** EXCEPT: the login page + its static assets, and `/healthz` (liveness must stay reachable for proxies/orchestrators). Intentionally goes *beyond* the research §4 read/write split — fully-private cockpit.
- **D-04 — `capability_grants` check is for the mutating routes that exist.** Only mutating route today is `POST /agent/run`; governance write routes land in Phase 28. The whole-origin gate (D-03) provides the principal; the `capability_grants` authorization layer attaches to mutating routes as they arrive. This phase wires the principal + the seam; it does NOT invent governance write routes early.
- **D-05 — Boot guard unlocks on EITHER credential.** Non-loopback bind boots **iff** `AURA_WEB_AUTH_SECRET` is set OR `AURA_WEB_TRUST_PROXY=true`. **Neither set + non-loopback = fail-fast exit** with a clear, actionable message. Loopback bind stays bootable with no config, exactly as today.
- **D-06 — Express the bind by WIDENING `AURA_AGUI_BIND`.** One server, one bind var: lift the hardcoded-loopback restriction; let D-05's guard govern non-loopback. **No new bind env, no alias.**
- **D-07 — Minimal panel = compose the existing endpoints.** The read-only health shell aggregates EXISTING `GET /healthz` + `GET /readyz` + bind address + build version. **No new backend endpoint this phase.** The richer `GET /api/health/runtime` aggregator stays in its later REST-read phase.
- **D-08 — Theme/density before boot reuses Phase 23.** The pre-hydration inline `<head>` script (Phase 23 D-08, already in `web/index.html`) sets `data-theme`/`data-density` before React mounts. WEB-04 consumes it; does NOT rebuild it.

### Claude's Discretion (resolved in this research — see §Discretion Resolutions)

- Session cookie TTL + logout behavior (idle vs absolute) and the cookie-signing mechanism.
- CSRF posture for a same-origin cookie-auth SPA under the whole-origin gate (D-03).
- SPA-fallback exclusion-list shape (`aguiRoutePrefixes` + the new `/api/` carve-out).
- Login-page asset placement inside `internal/webui` (keep the embed leaf-level).
- `crypto/subtle` constant-time compare + fail-closed details; trust-proxy header handling.

### Deferred Ideas (OUT OF SCOPE — do not pull forward)

- `GET /api/health/runtime` aggregator → later REST-read phase (Phase C / research §5).
- Governance write routes + their `capability_grants` enforcement → **Phase 28**.
- `showReasoning` web policy / CoT exposure → **Phase 25** (REQUIREMENTS line 136).
- assistant-ui chat lane + approval center → **Phase 25**.
- Typed-display protocol/router → Phase 26; graph explorer → Phase 27; governance boards + web onboarding → Phase 28.
- `ui_control` operator-OS shell + scheduler write surfaces → follow-up milestone.
- Real multi-user auth / RBAC / OAuth → out of scope for the entire milestone.
</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| WEB-01 | Operator opens the cockpit served by `aura serve` from the single binary (SPA via `//go:embed`); API / `/agent` / health routes excluded from the SPA catch-all so API 404s stay real | §"WEB-01: SPA-fallback host" — upgrade `internal/webui.Handler()` + `serve_webui.go` `aguiRoutePrefixes` into an SPA-fallback that serves `index.html` for unknown client routes and returns real 404s for API prefixes (incl. a forward-compat `/api/` carve-out). Pattern verified against ARCHITECTURE §3, Go 1.22 ServeMux precedence, and the existing `serve_webui_test.go`. |
| WEB-02 | `aura serve` refuses to bind a non-loopback address unless web auth is configured (fail-fast boot guard) | §"WEB-02: Fail-fast non-loopback boot guard" — widen `AURA_AGUI_BIND` (D-06), add a `cfg.Validate`-style guard run in `bootServe` (D-05). Loopback detection via `net.ParseIP(...).IsLoopback()` over the host split from the bind. Verified against `config.go` `Validate()` pattern + `serve.go` `bootServe`. |
| WEB-03 | Mutating routes require auth beyond loopback — reverse-proxy boundary (zero Go change) + in-binary signed session cookie (HttpOnly + Secure + SameSite=Strict) bound to an identity row (activates dormant `capability_grants`) | §"WEB-03: Web-auth boundary" — new `internal/agui/auth.go`: `POST /login` validating `AURA_WEB_AUTH_SECRET` constant-time (`crypto/subtle`), issuing an HMAC-SHA256-signed session cookie (stdlib only — NO new dependency); `RequireAuth` middleware wrapping the parent-mux subtree (whole-origin private per D-03 except login + assets + `/healthz`); principal bound to the seeded `local` identity row via `identity.GetIdentityByID`; the `capability_grants` check (`identity.HasCapability`) attaches to `POST /agent/run`. Verified against `identity/store.go`, golang-security cookies/network refs, stdlib `http.Cookie`. |
| WEB-04 | App shell renders with theme/density before boot (no flash) + read-only runtime health/readyz panel aggregating `/healthz` + `/readyz` + status | §"WEB-04: Runtime health shell" — a React panel using React Query (REST) over the EXISTING `/healthz` + `/readyz` + bind address + build version; theme/density via the already-shipped `web/index.html` pre-paint script (D-08). NO new backend endpoint. Verified against `web/index.html`, `agui/server.go` healthz body, `readiness.go` readyz body. |
</phase_requirements>

---

## Summary

Phase 24 turns the Phase-23 static embed placeholder into the **real single-binary SPA host** and wraps it in a **minimum GAP-2 web-auth boundary**, a **non-loopback fail-fast boot guard**, and a **read-only runtime health shell** — all on the ONE existing loopback `http.Server` that `bootServe` (`cmd/aura/serve.go:255`) already builds over `newServeHandler(aguiServer.Mux())`. There is no new listener, no new port, no new server. The four deliverables map 1:1 to WEB-01..04 and land on three existing files plus ONE new file (`internal/agui/auth.go`).

The single most important finding for the planner: **everything this phase needs already exists in the codebase or the Go standard library. No new third-party dependency is required, and none should be added.** The cookie-signing primitive is `crypto/hmac` + `crypto/subtle` (already in the stdlib, already used by the project's discipline). `gorilla/securecookie` / `gorilla/sessions` are NOT vendored (`go.mod` confirms) and adding them would violate the project's no-router/no-framework posture (`agui/server.go:88` "no chi/gorilla — matches the no-router codebase posture") and the minimal-industrial-shape doctrine that drove D-06/D-07. The identity/capability seam (`internal/identity.Store` — `GetIdentityByID`, `HasCapability`) is dormant Phase-4 scaffolding that this phase finally exercises; migration 0004 already seeds the `local` identity at `00000000-0000-0000-0000-000000000001` with the `*` wildcard, so the principal binding is a read, not a write.

The security-sensitive heart is `internal/agui/auth.go` (new): a `POST /login` that compares `AURA_WEB_AUTH_SECRET` with `crypto/subtle.ConstantTimeCompare` (fail-closed), mints an HMAC-SHA256-signed, `HttpOnly + Secure + SameSite=Strict` session cookie bound to the `local` identity, and a `RequireAuth` middleware that wraps the whole-origin subtree (D-03) reading the SPA cookie — **never** a client-supplied auth header (golang-security anti-pattern). The boot guard (WEB-02) is a pure-function check on `cfg.AGUIBind` run inside `bootServe` before the server is constructed: non-loopback bind + neither credential set → `fmt.Errorf` that `runServe` turns into a clean non-zero exit (the existing `bootServe` error path at `serve.go:101-105`).

**Primary recommendation:** Implement the auth boundary with stdlib `crypto/hmac` (HMAC-SHA256 signing) + `crypto/subtle` (constant-time compare) + `crypto/rand` (session id entropy) in a new `internal/agui/auth.go`; do NOT add any session/cookie library. Keep `internal/webui` leaf-level (the login page is just more embedded static assets). Wire the boot guard as a config-derived check in `bootServe`. Compose the health panel from the existing `/healthz` + `/readyz` — add NO new backend endpoint.

---

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| SPA shell delivery + client-side routing (WEB-01) | Frontend Server (the embed mount on the Go `http.Server`) | Browser (React Router resolves client routes) | The single binary serves `index.html`; the browser does client routing. The SPA-fallback decision (serve index.html vs real 404) is a **server-tier** decision — it must exclude API prefixes server-side, never in JS. |
| Non-loopback boot guard (WEB-02) | API / Backend (boot-time config validation in `bootServe`) | — | A bind-address policy is a backend boot concern; it cannot be enforced in the browser. Pure-function check on `cfg.AGUIBind`. |
| Authentication — login + session issue (WEB-03) | API / Backend (`internal/agui/auth.go`) | Browser (holds the HttpOnly cookie, cannot read it) | Auth is a server-tier trust boundary. The browser submits the form and carries the cookie; **all validation is server-side** (golang-security: "client-side authorization is bypassed by any HTTP client"). |
| Authorization — `capability_grants` check (WEB-03) | API / Backend (`identity.HasCapability` on `POST /agent/run`) | Database / Storage (`aura.capability_grants`) | Authorization decisions read the DB-backed grant rows; the principal is bound to an identity row. Never client-trusted. |
| Runtime health read (WEB-04) | API / Backend (existing `/healthz` + `/readyz`) | Browser (React Query polls + renders) | The health *data* is owned by the backend (already shipped). The browser is a pure renderer/poller — NO new aggregation endpoint this phase (D-07). |
| Theme/density before paint (WEB-04) | Browser (pre-hydration `<head>` script, already in `web/index.html`) | — | A flash-of-unstyled-content fix is inherently a browser-tier, pre-React concern. Reused verbatim from Phase 23 (D-08). |

---

## Standard Stack

### Core (all already present — NO install required)

| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| `crypto/hmac` (stdlib) | Go 1.26.4 | HMAC-SHA256 sign/verify the session cookie payload | The vetted, constant-time-verify (`hmac.Equal`) signing primitive; golang-security network.md §"Observable Timing" explicitly recommends `hmac.Equal` for MAC verification. No new dep. `[CITED: .claude/skills/golang-security/references/network.md:149-157]` |
| `crypto/subtle` (stdlib) | Go 1.26.4 | Constant-time compare of `AURA_WEB_AUTH_SECRET` at login (D-01) | golang-security: comparing secrets with `==` short-circuits on first differing byte → timing oracle. `subtle.ConstantTimeCompare` is the fix and "already handles unequal lengths without leaking timing". `[CITED: .claude/skills/golang-security/references/network.md:136-142]` |
| `crypto/rand` (stdlib) | Go 1.26.4 | Session id / nonce entropy (NEVER `math/rand`) | golang-security Common Mistakes: `math/rand` for tokens is predictable; use `crypto/rand`. `[CITED: golang-security/SKILL.md:145]` |
| `net/http` (stdlib) `http.Cookie` | Go 1.26.4 | Set/read the session cookie with `HttpOnly`, `Secure`, `SameSite=http.SameSiteStrictMode`, `MaxAge`, `Path:"/"` | All four locked cookie attributes (D-02) are native `http.Cookie` fields. `Cookie.Valid()` exists (go1.18+). The stdlib provides NO signing helper — sign with `crypto/hmac` separately. `[VERIFIED: pkg.go.dev/net/http#Cookie]` |
| `internal/identity.Store` | in-repo (Phase 4) | `GetIdentityByID` (bind principal), `HasCapability` (authorize `POST /agent/run`) | The dormant CORE-03 seam. `local` identity seeded by migration 0004 at `00000000-0000-0000-0000-000000000001` with the `*` wildcard. `[VERIFIED: internal/identity/store.go:100-138]` |
| `internal/webui` (`embed.FS`) | in-repo (Phase 23) | Serve the embedded SPA + login page assets; leaf-level (no internal/* imports) | The `//go:embed all:dist` host. Login assets are just more files in `dist/`. `[VERIFIED: internal/webui/embed.go:14, doc.go]` |
| `@tanstack/react-query` | (Phase-23 web stack) | WEB-04 health-panel polling of `/healthz` + `/readyz` (REST, not SSE) | ARCHITECTURE §5 "a panel that needs live numbers polls via React Query (REST), not SSE." Confirm presence in `web/package.json` at plan time; if absent it's a small frontend add inside the Phase-23-locked toolchain (NOT a backend dep). `[ASSUMED]` — see Assumptions Log A1. |
| `react-router-dom` | (locked Phase 23 D-14) | WEB-01 client-side SPA routes (the SPA-fallback serves index.html for these) | Locked as the SPA router in Phase 23 D-14, wired HERE. `[CITED: 23-CONTEXT.md D-14]` |

### Supporting (already present)

| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| `net` (stdlib) `ParseIP`/`SplitHostPort` | Go 1.26.4 | WEB-02 loopback detection on `cfg.AGUIBind` | The boot guard splits host:port and tests `IsLoopback()`. |
| `encoding/base64` (stdlib) | Go 1.26.4 | URL-safe encode the cookie payload + MAC | Cookie values must be ASCII; base64url the `{identity_id}\|{issued_at}\|{mac}` tuple. |
| `internal/config` | in-repo | Add `WebAuthSecret` + `WebTrustProxy` fields; widen `AGUIBind` semantics (D-05/D-06) | New knobs follow the existing `envDefault`/`os.Getenv` pattern. |

### Alternatives Considered (and rejected)

| Instead of | Could Use | Tradeoff — why rejected |
|------------|-----------|--------------------------|
| stdlib `crypto/hmac` cookie | `gorilla/securecookie` | NOT vendored (`go.mod`); adds a dependency for what is ~40 LOC of stdlib HMAC. Violates the no-framework posture (`agui/server.go:88`) and minimal-industrial-shape (D-06). The golang-security cookies.md gorilla example is illustrative, not a mandate. |
| stdlib HMAC session token | `gorilla/sessions` server-side store | Requires a session backing store (memory/DB) — new credential/state storage, explicitly rejected by D-01 ("No new credential storage"). A signed stateless cookie needs no store. |
| Bearer token in `Authorization` header | — | golang-security + ARCHITECTURE §4: bearer tokens are XSS-exposed in the browser, inferior to an HttpOnly cookie for a browser app. Reserve bearer for machine callers (out of scope). |
| New `AURA_WEB_BIND` env | widen `AURA_AGUI_BIND` | D-06 locks "one server, one bind var, no alias." |
| New `/api/health/runtime` aggregator | compose existing `/healthz` + `/readyz` | D-07 + ARCHITECTURE §5: the aggregator is a later phase. Adding it now pulls the future forward. |

**Installation:** None for the backend (stdlib only). Frontend additions stay inside the Phase-23-locked toolchain (`web/`).

**Version verification:** Go module is `go 1.26.4` (`[VERIFIED: go.mod:3]`). All crypto/http primitives are stdlib at that version. `http.Cookie` fields + `Cookie.Valid()` confirmed `[VERIFIED: pkg.go.dev/net/http#Cookie]`. No registry packages to slop-check on the backend.

## Package Legitimacy Audit

> This phase installs **NO new backend (Go) packages** — the entire auth/serve/health boundary is stdlib + in-repo. Therefore the backend audit is trivially clean.

| Package | Registry | Age | Downloads | Source Repo | slopcheck | Disposition |
|---------|----------|-----|-----------|-------------|-----------|-------------|
| `crypto/hmac`, `crypto/subtle`, `crypto/rand`, `net/http`, `net`, `encoding/base64` | Go stdlib | — | — | golang/go | N/A (stdlib) | Approved — no external install |
| `internal/identity`, `internal/webui`, `internal/config`, `internal/agui` | in-repo | — | — | this repo | N/A | Approved — existing code |
| `@tanstack/react-query` (frontend, if not already present) | npm | mature | very high | tanstack/query | not run (no new backend pkg; frontend stack was locked Phase 23) | Verify in `web/package.json` at plan time; if added, it is inside the Phase-23 toolchain, not a Phase-24 backend dep |

**Packages removed due to slopcheck [SLOP] verdict:** none — no external packages introduced.
**Packages flagged as suspicious [SUS]:** none.

*slopcheck was not run because this phase introduces zero new backend registry packages. The one possible frontend add (`@tanstack/react-query`) is a mature, ubiquitous TanStack package already implied by the Phase-23 stack; the planner should confirm it is in `web/package.json` rather than treat it as a new install.*

---

## Architecture Patterns

### System Architecture Diagram

```
                         ┌─────────────────────────────────────────────┐
   operator browser ───► │  ONE http.Server  (cfg.AGUIBind, serve.go)  │
   (cookie: HttpOnly)    │  Handler = newServeHandler(aguiServer.Mux()) │
                         └───────────────────────┬─────────────────────┘
                                                 │
                          WEB-02 boot guard ─────┤ (checked in bootServe BEFORE
                          non-loopback + neither  │  the server is built; fail-fast
                          credential ⇒ os.Exit    │  error → runServe non-zero exit)
                                                 │
                         ┌───────────────────────▼─────────────────────────────┐
                         │  parent mux (serve_webui.go newServeHandler)         │
                         │                                                      │
                         │  RequireAuth(  ── WEB-03 middleware, D-03 whole-origin│
                         │    wraps the WHOLE subtree EXCEPT:                    │
                         │      • POST /login            (issues cookie)         │
                         │      • login page + its assets (public)              │
                         │      • GET /healthz           (liveness, proxies)    │
                         │  )                                                    │
                         │     │                                                │
                         │     ├─ aguiRoutePrefixes ──► aguiServer.Mux()        │
                         │     │   /readyz /metrics /debug/vars                  │
                         │     │   POST /agent/run  ──► + HasCapability check    │
                         │     │   /threads/                  (D-04, CORE-03)    │
                         │     │   (NEW) /api/  ── reserved, real 404 (no SPA)   │
                         │     │                                                │
                         │     └─ "/" catch-all ──► spaFallback (WEB-01)        │
                         │          serve asset if exists, else index.html,     │
                         │          BUT real 404 for any excluded API prefix    │
                         └──────────────────────┬───────────────────────────────┘
                                                │
                  ┌─────────────────────────────┼──────────────────────────┐
                  ▼                             ▼                          ▼
          internal/webui (embed.FS)   internal/identity.Store      /healthz + /readyz
          dist/ + login assets        GetIdentityByID (bind)       (existing, agui)
          (LEAF — no internal/*)       HasCapability (authorize)    ◄── WEB-04 panel
                                       aura.capability_grants            React Query (REST)
```

A reader can trace the primary use case (operator hits the cockpit on a non-loopback bind): boot guard passes (secret set) → request arrives → `RequireAuth` finds no cookie → redirect to login page (public) → `POST /login` validates secret constant-time → issues signed cookie bound to `local` identity → subsequent requests carry the cookie → `RequireAuth` verifies the HMAC + binds the principal → SPA shell served via `spaFallback` → health panel polls `/healthz`/`/readyz` → a `POST /agent/run` additionally passes `HasCapability`.

### Recommended File Structure (changes only)

```
cmd/aura/
├── serve.go               # MODIFIED: bootServe runs the WEB-02 boot guard before building httpSrv
├── serve_webui.go         # MODIFIED: aguiRoutePrefixes += "/api/"; "/" → spaFallback; wrap subtree in RequireAuth
└── serve_webui_test.go    # MODIFIED: add SPA-fallback + /api/-404 + auth-redirect cases
internal/agui/
├── auth.go                # NEW: RequireAuth middleware, POST /login, POST /logout, cookie sign/verify
├── auth_test.go           # NEW: unit — sign/verify round-trip, constant-time compare, fail-closed, cookie flags
└── server.go              # (unchanged — Mux stays the AG-UI route owner)
internal/webui/
└── dist/                  # login page + assets land here (still LEAF — just static files)
internal/config/
├── config.go              # MODIFIED: + WebAuthSecret, WebTrustProxy; widen AGUIBind semantics
└── config_webauth_test.go # NEW: boot-guard pure-function test matrix (loopback/non-loopback × secret/proxy)
```

> File-size discipline (CLAUDE.md NO GOD CLASS ≤600 LOC): `auth.go` should split if it grows — e.g. `auth.go` (middleware + login/logout handlers) and `auth_cookie.go` (sign/verify/encode). Keep each well under 600.

### Pattern 1: SPA-fallback that excludes API prefixes (WEB-01)

**What:** Upgrade `internal/webui.Handler()` (and the `serve_webui.go` wiring) so unknown *client* routes get `index.html` (React Router resolves them) but excluded prefixes return a real 404.
**When to use:** Any embedded SPA served by a Go binary that also exposes API/health routes on the same mux.
**Why this is safe here:** Go 1.22 `http.ServeMux` gives a longer/more-specific registered pattern priority over `/` — the existing `serve_webui_test.go` already PROVES this (the `/healthz` and `/threads/` cases route to the AG-UI handler, not the catch-all). So the catch-all only fires for genuinely unmatched paths; the fallback's own exclusion list is belt-and-suspenders for *prefix* paths that aren't registered routes but must still 404 as API (e.g. a typo `/api/whatever`).

```go
// internal/webui — NEW fallback handler (replaces the bare http.FileServerFS in Handler()).
// Source pattern: .planning/research/ARCHITECTURE.md §3 (verified against existing serve_webui.go).
// Keep this in internal/webui so the embed stays self-contained AND leaf-level — it imports
// only embed/io/fs/net/http/strings (NO internal/*); scripts/agui_boundary_check.sh enforces.

// apiPrefixes are paths that must 404 as API errors, NEVER fall back to index.html. The
// forward-compat "api/" carve-out (D-discretion) makes the later REST reads 404 cleanly even
// though no /api/* routes exist yet. These mirror serve_webui.go's aguiRoutePrefixes; keep the
// two lists in sync (or share one exported slice — see the note below).
var apiPrefixes = []string{"agent/", "threads/", "api/", "healthz", "readyz", "metrics", "debug/"}

func spaFallback(sub fs.FS, fileSrv http.Handler) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        p := strings.TrimPrefix(r.URL.Path, "/")
        for _, api := range apiPrefixes {
            if strings.HasPrefix(p, api) {
                http.NotFound(w, r) // real 404 — never the SPA shell (WEB-01 / SC1)
                return
            }
        }
        if p == "" {
            p = "index.html"
        }
        if _, err := fs.Stat(sub, p); err != nil {
            p = "index.html" // deep client-route link → SPA shell (React Router resolves it)
        }
        r.URL.Path = "/" + p
        fileSrv.ServeHTTP(w, r)
    }
}
```

**Exclusion-list shape — definitive recommendation (resolves a Discretion item):**
The cleanest structure is to make `aguiRoutePrefixes` (currently in `serve_webui.go:33`) the **single source of truth** for what the AG-UI gateway owns, add `"/api/"` to it, and derive the fallback's exclusion set from the SAME list (strip the leading `/`). Two parallel hard-coded lists drift; one list, two consumers, does not. Concretely: keep `aguiRoutePrefixes` as the parent-mux registration list, and pass a trimmed copy into the fallback. The new `/api/` entry is registered nowhere on the AG-UI mux (no handler yet) but IS in the fallback exclusion set, so `/api/anything` → real 404 today and a real route tomorrow.

### Pattern 2: Constant-time secret check + HMAC-signed stateless session cookie (WEB-03)

**What:** `POST /login` reads the form passphrase, compares it to `AURA_WEB_AUTH_SECRET` with `subtle.ConstantTimeCompare` (fail-closed if the env secret is empty), and on match mints a signed cookie. `RequireAuth` verifies the HMAC on every protected request and binds the principal.
**When to use:** A single-operator in-binary auth boundary with no new credential storage (exactly D-01/D-02).
**Why stateless HMAC:** No session store needed (D-01 "no new credential storage"); the cookie carries `{identity_id}|{issued_at}` and a MAC over it keyed by a server secret derived from `AURA_WEB_AUTH_SECRET`. Tamper → MAC mismatch → reject. Expiry → `issued_at` + TTL check server-side.

```go
// internal/agui/auth.go (NEW) — stdlib only, NO new dependency.
// Verified primitives: golang-security network.md (subtle.ConstantTimeCompare, hmac.Equal),
// cookies.md (HttpOnly+Secure+SameSiteStrict+Path), pkg.go.dev/net/http#Cookie (field set).

const sessionCookieName = "__Host-aura_session" // __Host- prefix: forces Secure, no Domain, Path=/ (cookies.md §Cookie Prefix)

// validateSecret is the fail-closed login check (D-01). An empty configured secret means
// in-binary auth is NOT enabled — reject ALL logins (never accept an empty passphrase).
func validateSecret(provided, configured string) bool {
    if configured == "" {
        return false // fail-closed: no secret configured ⇒ no login possible via this path
    }
    return subtle.ConstantTimeCompare([]byte(provided), []byte(configured)) == 1
}

// signSession builds the cookie value: base64url(identityID "|" issuedUnix) "." base64url(HMAC).
func signSession(key []byte, identityID string, issued time.Time) string {
    payload := identityID + "|" + strconv.FormatInt(issued.Unix(), 10)
    mac := hmac.New(sha256.New, key)
    mac.Write([]byte(payload))
    sig := mac.Sum(nil)
    return base64.RawURLEncoding.EncodeToString([]byte(payload)) + "." +
        base64.RawURLEncoding.EncodeToString(sig)
}

// verifySession is constant-time (hmac.Equal) and enforces the absolute TTL.
func verifySession(key []byte, value string, ttl time.Duration, now time.Time) (identityID string, ok bool) {
    parts := strings.SplitN(value, ".", 2)
    if len(parts) != 2 {
        return "", false
    }
    rawPayload, err := base64.RawURLEncoding.DecodeString(parts[0])
    if err != nil {
        return "", false
    }
    gotSig, err := base64.RawURLEncoding.DecodeString(parts[1])
    if err != nil {
        return "", false
    }
    mac := hmac.New(sha256.New, key)
    mac.Write(rawPayload)
    if !hmac.Equal(gotSig, mac.Sum(nil)) { // constant-time MAC compare (network.md:155)
        return "", false
    }
    pp := strings.SplitN(string(rawPayload), "|", 2)
    if len(pp) != 2 {
        return "", false
    }
    issuedUnix, err := strconv.ParseInt(pp[1], 10, 64)
    if err != nil || now.After(time.Unix(issuedUnix, 0).Add(ttl)) {
        return "", false // expired (absolute TTL) — fail closed
    }
    return pp[0], true
}

// setSessionCookie writes the locked attributes (D-02 / SC3).
func setSessionCookie(w http.ResponseWriter, value string, ttl time.Duration) {
    http.SetCookie(w, &http.Cookie{
        Name:     sessionCookieName,
        Value:    value,
        Path:     "/",
        HttpOnly: true,                    // no JS access (CWE-1004)
        Secure:   true,                    // HTTPS only (CWE-614)
        SameSite: http.SameSiteStrictMode, // CSRF baseline (CWE-352)
        MaxAge:   int(ttl.Seconds()),
    })
}
```

**Principal binding (D-02, CORE-03):** On a successful MAC verify, `RequireAuth` calls `identity.GetIdentityByID(ctx, identityID)` to confirm the bound identity still exists (a deleted identity invalidates the session), stashes the identity id on the request context, and proceeds. The login binds to the seeded `local` identity (`00000000-0000-0000-0000-000000000001`, `[VERIFIED: server_integration_test.go:40]`).

**`capability_grants` on the only mutating route (D-04):** `POST /agent/run` additionally checks `store.HasCapability(ctx, identityID, "agent.run")` (or the chosen capability name — `local` holds `*` so it passes via the wildcard, `[VERIFIED: identity/store.go:125-138]`). This is the seam that finally exercises the dormant scaffolding without inventing governance routes.

### Pattern 3: Boot-guard as a pure function called in bootServe (WEB-02)

**What:** A pure function `guardBind(bind, secret string, trustProxy bool) error` that returns a non-nil actionable error when the bind is non-loopback and neither credential is set. Called inside `bootServe` (`serve.go`) before `httpSrv` is built; the existing `bootServe` error path (`serve.go:101-105`) turns it into `os.Exit(exitInfra)` with a human line.
**When to use:** Boot-time policy on an address that must not silently expose an unauthenticated surface.
**Why a pure function:** Trivially unit-testable across the matrix (loopback/non-loopback × secret-set/unset × proxy-true/false) without booting anything — mirrors `config.Validate()` (`config.go:198`).

```go
// In config.go (or a small webauth.go in config) — pure, table-test-friendly.
// "non-loopback + neither credential" is the ONLY fail case (D-05).
func GuardWebBind(bind, webAuthSecret string, trustProxy bool) error {
    host, _, err := net.SplitHostPort(bind)
    if err != nil {
        host = bind // tolerate a bare host
    }
    ip := net.ParseIP(host)
    isLoopback := host == "localhost" || (ip != nil && ip.IsLoopback())
    if isLoopback {
        return nil // loopback always bootable, exactly as today (D-05)
    }
    if strings.TrimSpace(webAuthSecret) != "" || trustProxy {
        return nil // unlocked by either credential (D-05)
    }
    return fmt.Errorf("config: AURA_AGUI_BIND=%q is non-loopback but web auth is not configured; "+
        "set AURA_WEB_AUTH_SECRET (in-binary login) or AURA_WEB_TRUST_PROXY=true (reverse proxy "+
        "terminates auth), or bind a loopback address", bind)
}
```

> **0.0.0.0 note:** `net.ParseIP("0.0.0.0").IsLoopback()` is false, so `0.0.0.0:9080` is correctly treated as non-loopback and gated. golang-security network.md:48 flags binding to 0.0.0.0 explicitly — the guard makes that exposure require a credential, which is the whole point of WEB-02.

### Anti-Patterns to Avoid

- **SPA index.html for a real API 404 (SC1 violation):** Serving `index.html` for `/api/typo` returns HTTP 200 + HTML to an API client that expected JSON — an information-leak / broken-contract bug. The fallback's `apiPrefixes` exclusion is the mitigation; verify it with a test that asserts `/api/nope` → 404, never HTML.
- **Trusting client-supplied auth headers (golang-security High):** `RequireAuth` MUST read the cookie, NEVER an `X-*` or `Authorization` header from the client (golang-security: "Trusting client headers — `X-Forwarded-For`, `X-Is-Admin` are trivially forged"). The trust-proxy path (D-05) means "the operator vouches a proxy terminates auth and Aura stays hands-off" — it does NOT mean Aura reads a proxy-injected identity header.
- **`==` on the secret (timing oracle, golang-security Medium):** Always `subtle.ConstantTimeCompare`. Same for the MAC: `hmac.Equal`, never `bytes.Equal` on the signature.
- **`math/rand` for any token/nonce (golang-security High):** `crypto/rand` only.
- **Fail-open on an empty/bad secret:** `validateSecret` returns false when `configured == ""`. A misconfigured deploy must reject logins, never accept a blank passphrase.
- **A second http.Server / port:** The single-binary invariant (CLAUDE.md, T-23-06) forbids a new listener. Everything mounts on the existing `httpSrv`.
- **`internal/webui` importing `internal/agui` or `internal/identity`:** breaks the leaf invariant; `scripts/agui_boundary_check.sh` fails the build. The login *page* (HTML/JS/CSS) lives in `webui/dist`; the login *logic* lives in `internal/agui/auth.go`.

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Constant-time secret compare | A byte loop / `==` | `crypto/subtle.ConstantTimeCompare` | Hand loops leak timing; subtle handles unequal lengths safely. |
| Cookie signature MAC | Custom hash concat | `crypto/hmac` + `hmac.Equal` | HMAC is the vetted construction; `hmac.Equal` is constant-time. |
| Session token entropy | `math/rand`, timestamps | `crypto/rand` | Predictable tokens are forgeable. |
| Cookie attribute serialization | Manual `Set-Cookie` string | `http.SetCookie` + `http.Cookie{}` | The stdlib serializes `SameSite`/`HttpOnly`/`Secure`/`MaxAge` correctly. |
| SPA route precedence | A regex router / chi/gorilla | Go 1.22 `http.ServeMux` method patterns | Already proven in `serve_webui_test.go`; the project posture is no-router. |
| Loopback detection | String-prefix `127.` checks | `net.ParseIP(...).IsLoopback()` | Covers `::1`, `127.0.0.0/8`, not just `127.0.0.1`. |
| Session store | An in-memory/DB session table | A stateless HMAC-signed cookie | D-01 forbids new credential storage; stateless needs no store. |
| Health aggregation | A new `/api/health/runtime` | Compose existing `/healthz` + `/readyz` client-side | D-07: the aggregator is a later phase. |

**Key insight:** This entire phase is achievable with the Go standard library and existing in-repo packages. The temptation to reach for `gorilla/sessions` or a session middleware is the "atomic bomb" the project's own memory ([[feedback_no_atomic_bombs_minimal_industrial_shape]]) warns against — the minimal industrial shape is ~150 LOC of stdlib in `internal/agui/auth.go`.

## Runtime State Inventory

> WEB-02/D-06 widen an env var (`AURA_AGUI_BIND`) and add two new ones (`AURA_WEB_AUTH_SECRET`, `AURA_WEB_TRUST_PROXY`). This is a config/code change, not a rename or data migration, but the env-var surface is worth an explicit inventory so the planner updates every reader.

| Category | Items Found | Action Required |
|----------|-------------|------------------|
| Stored data | **None.** The session cookie is stateless (HMAC-signed); no DB table, no Mem0/Neo4j key, no new migration. The `local` identity + `*` grant already exist (migration 0004). | None — verified: no new persistence. `HasCapability` reads existing `aura.capability_grants`. |
| Live service config | **None new at runtime.** A reverse-proxy (Caddy) path is the *operator's* config, not Aura's — D-05's `AURA_WEB_TRUST_PROXY=true` is the only Aura-side acknowledgement. No n8n/Datadog/Tailscale state. | None — the proxy story is documented, not wired into Aura state. |
| OS-registered state | **None.** No Task Scheduler / systemd / pm2 entry embeds the bind or secret. The daemon reads them from env at boot. | None. |
| Secrets/env vars | NEW: `AURA_WEB_AUTH_SECRET` (the login passphrase / cookie-key source), `AURA_WEB_TRUST_PROXY` (bool). WIDENED: `AURA_AGUI_BIND` (was hardcoded-loopback-only; now any bind, guarded). The secret should also be documented in `.env.example` / the PRD env catalog (~60 vars). | Add the two vars to `config.go` (`loadBase`), the env catalog/PRD, and `.env.example`. The cookie-signing key should derive from the secret (e.g. `sha256(AURA_WEB_AUTH_SECRET)`) so a single secret governs both login and signing — document this. |
| Build artifacts | The login page lands in committed `web/dist` → `internal/webui/dist` (D-05 commits dist + the CI freshness gate). A new login route/asset means a `dist` rebuild + re-commit, or the freshness gate fails. | Rebuild `web/dist`, re-commit, ensure the Node-24 Docker rebuild is byte-reproducible (Phase 23 D-05/D-06 freshness gate). |

**The canonical question — after every file is updated, what runtime systems still hold old state?** Answer: **nothing.** This phase adds no stored runtime state. The only "state" is the env-var surface (3 vars) and the committed `dist` (build artifact) — both covered above.

## Common Pitfalls

### Pitfall 1: SPA-fallback shadows a real API 404 (SC1)
**What goes wrong:** A request to a non-existent API path (`/api/foo`, `/agent/typo`) returns `index.html` with HTTP 200 instead of a 404, breaking the API contract and leaking the SPA to API clients.
**Why it happens:** A naive fallback (`if file missing → index.html`) doesn't distinguish client routes from API typos.
**How to avoid:** The `apiPrefixes` exclusion in `spaFallback` (Pattern 1). Add the forward-compat `/api/` carve-out now (D-discretion).
**Warning signs:** `curl -i http://.../api/nope` returns `Content-Type: text/html` and 200.

### Pitfall 2: Cookie not sent over loopback HTTP because `Secure: true`
**What goes wrong:** `Secure: true` cookies are only sent over HTTPS. On a plain-HTTP loopback dev run, the browser silently drops the cookie → infinite login redirect.
**Why it happens:** SC3 locks `Secure` (correctly, for the exposed case), but loopback dev is HTTP.
**How to avoid:** Two valid postures — (a) keep `Secure: true` always and document that the in-binary login path is exercised behind TLS / a proxy / `https://localhost` (loopback dev that doesn't need auth bypasses login entirely since the guard lets loopback boot with no secret), or (b) a narrowly-scoped dev allowance. **Recommendation:** keep `Secure: true` unconditionally (D-02 is locked and `__Host-` prefix *requires* it); the in-binary login is only relevant when exposed non-loopback, which is the TLS case. Loopback dev runs without `AURA_WEB_AUTH_SECRET` → no login → no cookie → no problem. Document this clearly so a tester doesn't try to log in over plain-HTTP loopback and report a "bug."
**Warning signs:** A login POST returns 200 + Set-Cookie but the next request still 401s on plain HTTP.

### Pitfall 3: Boot guard treats `0.0.0.0` or `::` as loopback
**What goes wrong:** A wildcard bind (`0.0.0.0`, `::`, `[::]`) exposes all interfaces but slips past a naive `strings.HasPrefix(host,"127.")` check.
**Why it happens:** String checks miss IPv6 loopback (`::1`) AND miss wildcard non-loopback.
**How to avoid:** `net.ParseIP(host).IsLoopback()` (Pattern 3). `0.0.0.0`/`::` return `IsLoopback()==false` → correctly gated.
**Warning signs:** `AURA_AGUI_BIND=0.0.0.0:9080` boots with no secret and no error.

### Pitfall 4: Auth middleware accidentally gates `/healthz`, breaking orchestrator probes (D-03)
**What goes wrong:** Wrapping the *whole* mux in `RequireAuth` 401s `/healthz`, so a proxy/orchestrator liveness probe fails and the container is killed.
**Why it happens:** D-03 is "whole origin private EXCEPT login page + assets + `/healthz`."
**How to avoid:** The exception list in `RequireAuth` must include `/healthz` (and the login page + its assets). `/readyz` is NOT excepted (D-03 lists only `/healthz`); confirm with the planner whether orchestrators need `/readyz` too — D-03 says only `/healthz`, so default to gating `/readyz`.
**Warning signs:** `curl http://.../healthz` returns 401/302 instead of the health JSON.

### Pitfall 5: Embed leaf-invariant regression
**What goes wrong:** Adding login logic to `internal/webui` (importing `internal/agui` or `internal/identity`) fails `scripts/agui_boundary_check.sh` and the CI build.
**Why it happens:** The login *page* feels like it belongs with the embed; the *logic* does not.
**How to avoid:** Login HTML/JS/CSS → `webui/dist` (static). Login handler/middleware → `internal/agui/auth.go`. `internal/webui` stays stdlib-only.
**Warning signs:** `go list -deps ./internal/webui/...` lists another `internal/*` package.

### Pitfall 6: Two drifting exclusion lists (serve_webui.go vs the fallback)
**What goes wrong:** `aguiRoutePrefixes` and the fallback's `apiPrefixes` diverge — a route registered on the AG-UI mux but absent from the fallback exclusion gets shadowed (or vice versa).
**Why it happens:** Copy-paste of two hard-coded lists.
**How to avoid:** One source of truth (Pattern 1 note) — derive the fallback exclusion from `aguiRoutePrefixes`.
**Warning signs:** A new AG-UI route works but its typo'd sibling returns the SPA.

## Code Examples

### Boot-guard wiring inside bootServe (WEB-02)
```go
// cmd/aura/serve.go — inside bootServe, BEFORE building httpSrv (around serve.go:250).
// The existing bootServe error path (serve.go:101-105) turns this into os.Exit(exitInfra)
// with a human-readable line — exactly the fail-fast posture D-05 wants.
// Source: config.Validate() pattern (config.go:198) + bootServe error path (serve.go:101).
if err := config.GuardWebBind(chat.cfg.AGUIBind, chat.cfg.WebAuthSecret, chat.cfg.WebTrustProxy); err != nil {
    chat.close()
    return nil, err // runServe prints "aura serve: <err>" and exits non-zero
}
```

### RequireAuth middleware skeleton (WEB-03, D-03 whole-origin)
```go
// internal/agui/auth.go (NEW). Reads the SPA cookie, NEVER a client header.
// Source: golang-security network.md (anti-pattern: trusting client headers) + identity/store.go.
func RequireAuth(next http.Handler, deps AuthDeps) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        // D-03 public exceptions: login + its assets + /healthz stay reachable.
        if isPublicPath(r.URL.Path) {
            next.ServeHTTP(w, r)
            return
        }
        c, err := r.Cookie(sessionCookieName)
        if err != nil {
            redirectToLogin(w, r) // browser GET → 302 to the login page; API → 401
            return
        }
        identityID, ok := verifySession(deps.SigningKey, c.Value, deps.TTL, time.Now())
        if !ok {
            redirectToLogin(w, r)
            return
        }
        if _, err := deps.Identities.GetIdentityByID(r.Context(), identityID); err != nil {
            redirectToLogin(w, r) // bound identity gone ⇒ session invalid
            return
        }
        next.ServeHTTP(w, withPrincipal(r, identityID))
    })
}
```

### capability_grants check on the only mutating route (WEB-03, D-04)
```go
// Wrap POST /agent/run with the authorization check. `local` holds "*" so HasCapability
// passes via the wildcard (identity/store.go:125). This exercises the dormant seam WITHOUT
// inventing governance routes. Source: identity/store.go HasCapability.
func requireCapability(next http.HandlerFunc, deps AuthDeps, capability string) http.HandlerFunc {
    return func(w http.ResponseWriter, r *http.Request) {
        identityID := principalFrom(r.Context()) // set by RequireAuth
        ok, err := deps.Identities.HasCapability(r.Context(), identityID, capability)
        if err != nil || !ok {
            http.Error(w, "forbidden", http.StatusForbidden)
            return
        }
        next(w, r)
    }
}
```

### WEB-04 health panel data sources (no new endpoint)
```
GET /healthz  → {"ok":true,"scheduler_last_tick":"..."}          (agui/server.go:101)
GET /readyz   → {"ready":true,"deps":{"postgres":"ok","neo4j":"ok"}}  (agui/readiness.go:33)
bind address  → cfg.AGUIBind (surface via the existing /healthz or a build-info read)
build version → buildInfo() (serve.go:109) — already available; expose to the SPA via index.html meta or a tiny existing route
```
The React panel polls `/healthz` + `/readyz` via React Query and renders status; theme/density are already applied pre-paint by `web/index.html:10-21` (D-08, no change).

## State of the Art

| Old Approach (Phase 23) | Current Approach (Phase 24) | When Changed | Impact |
|--------------------------|------------------------------|--------------|--------|
| `webui.Handler()` = bare `http.FileServerFS`, missing asset → plain 404 | SPA-fallback: missing *client* route → index.html; excluded API prefix → real 404 | WEB-01 | Deep client-route links work; API 404s stay real (SC1). |
| `AURA_AGUI_BIND` hardcoded loopback (amendment #35), no escape | `AURA_AGUI_BIND` any bind, guarded by D-05 | WEB-02/D-06 | Non-loopback exposure possible IFF auth configured. |
| Gateway deliberately unauthenticated (loopback = compensating control) | In-binary signed-cookie auth boundary (whole-origin private when exposed) | WEB-03 | The dormant `capability_grants` scaffolding activates. |
| No health UI | Read-only health panel composing `/healthz` + `/readyz` | WEB-04 | Operator-visible runtime status. |

**Deprecated/outdated:** The doc comments in `internal/webui/{embed.go,doc.go}` and `cmd/aura/serve_webui.go` that say "Phase-23 scope is the static placeholder ONLY … DO NOT add fallback logic here" describe the PRE-Phase-24 state. WEB-01 is the sanctioned change that supersedes those "do not" comments — update the comments in the same commit (CLAUDE.md: comments-updated on touch).

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | `@tanstack/react-query` is part of the Phase-23-locked frontend stack (used for the WEB-04 REST polling) | Standard Stack | LOW — if absent it's a small, ubiquitous frontend add inside the already-locked toolchain; the planner confirms via `web/package.json`. Does not affect the backend. |
| A2 | The cookie-signing key derives from `AURA_WEB_AUTH_SECRET` (e.g. `sha256(secret)`) rather than a separate `AURA_WEB_SIGNING_KEY` | Pattern 2 / Runtime State | LOW — D-01 says one secret, no new credential storage; deriving the HMAC key from it keeps the surface to ONE var. The planner should confirm whether a separate signing key is wanted (it adds a var but separates concerns). Recommend single-secret derivation. |
| A3 | The capability name checked on `POST /agent/run` is operator-chosen (e.g. `agent.run`); `local` passes via the `*` wildcard regardless of the exact name | Pattern 2 / Example | LOW — the wildcard match makes the exact name non-blocking for the seeded operator; the name only matters when real grants are introduced (Phase 28). Planner picks the name. |
| A4 | `/readyz` is gated by `RequireAuth` (D-03 lists only `/healthz` as the liveness exception) | Pitfall 4 | LOW-MEDIUM — if an orchestrator needs `/readyz` reachable unauthenticated, the exception list must add it. D-03 text excepts only `/healthz`; default to gating `/readyz` and flag for the planner/operator. |

## Open Questions

1. **Cookie-signing key: derive from `AURA_WEB_AUTH_SECRET` or a separate var?**
   - What we know: D-01 locks "no new credential storage" and one operator secret. The login passphrase and the cookie-signing key are conceptually distinct (one authenticates the human, one signs the session).
   - What's unclear: whether the operator wants ONE var (`sha256(AURA_WEB_AUTH_SECRET)` as the HMAC key) or a separate `AURA_WEB_SIGNING_KEY`.
   - Recommendation: **single secret, derive the HMAC key** (`sha256(secret)`). Minimal-industrial-shape; one var to set. Note in the plan as a confirm-if-cheap point.

2. **Session TTL: absolute, idle, or both? Logout behavior?**
   - What we know: Discretion item explicitly asks. golang-security cookies.md: "Use short MaxAge expiration" + "Clear cookies on logout."
   - What's unclear: the operator's session-length tolerance.
   - Recommendation: **absolute TTL** (e.g. 12h or 24h) encoded in `issued_at` + verified server-side; `MaxAge` mirrors it on the cookie. A `POST /logout` that sets the cookie with `MaxAge: -1` (delete). Idle-expiry adds sliding-window complexity for marginal value in a single-operator cockpit — recommend absolute-only. Planner picks the hour value.

3. **Does `/readyz` stay reachable unauthenticated (orchestrator dependency)?**
   - What we know: D-03 excepts only `/healthz`.
   - Recommendation: gate `/readyz` per D-03 literally; if an orchestrator needs it, surface to the operator (see A4).

## Environment Availability

> This phase is code + config only (Go backend + the already-scaffolded `web/`). It depends on no NEW external tool, service, or runtime — it reuses the existing daemon, Postgres (for `capability_grants` reads), and the Phase-23 web toolchain.

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| Go toolchain | all backend code | ✓ | go 1.26.4 (`go.mod:3`) | — |
| Postgres (`aura.capability_grants`, `aura.identities`) | WEB-03 `HasCapability`/`GetIdentityByID` (db_integration tier) | ✓ (compose stack) | per project | — |
| Node 24 + `web/` toolchain | WEB-01 login page / WEB-04 panel build (committed `dist`) | ✓ (Phase 23) | Node 24 | committed `dist` lets Go build with zero Node (D-05) |
| `@tanstack/react-query` (frontend) | WEB-04 REST polling | likely ✓ (Phase-23 stack) | — | trivial add inside the locked toolchain (A1) |

**Missing dependencies with no fallback:** none.
**Missing dependencies with fallback:** none blocking — the committed `dist` (D-05) means Go contributors never need Node.

## Validation Architecture

> `workflow.nyquist_validation: true` (`.planning/config.json`) — this section is REQUIRED and is consumed to generate VALIDATION.md. The Aura/agui/webui HTTP surface convention is **stdlib `testing` + `httptest`, no testify** (verified in `serve_webui_test.go`, `embed_test.go`, `server_integration_test.go`). Build-tag integration tiers `t.Fatal` under `$CI` when their env is unset (no-skip-as-green, `server_integration_test.go:42-53`). Coverage floor is **85% owned-surface** (CLAUDE.md, overrides PRD 75/60).

### Test Framework
| Property | Value |
|----------|-------|
| Framework | Go stdlib `testing` + `net/http/httptest` (no testify on the HTTP surface); `pgregory.net/rapid` for property tests (already vendored, `go.mod:31`); frontend = Vitest + Playwright (Phase-23 locked) |
| Config file | none for Go (stdlib); `web/playwright.config.ts` + Vitest config (Phase 23) |
| Quick run command | `go test ./internal/agui/... ./internal/config/... ./internal/webui/... ./cmd/aura/...` |
| Full suite command | `go test -tags 'db_integration neo4j_integration' -race -p 1 ./... -count=1` (derive `AURA_DB_URL`/`AURA_DB_MIGRATE_URL` from `POSTGRES_PASSWORD`; stack up) |

### Phase Requirements → Test Map
| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| WEB-01 | Unknown client route → index.html (SPA shell) | unit (httptest) | `go test ./internal/webui/ -run TestSPAFallback` | ❌ Wave 0 (`internal/webui/embed_test.go` exists; add SPA-fallback cases) |
| WEB-01 | Excluded API prefix (`/api/nope`, `/agent/typo`, `/healthz` typo) → real 404, never HTML | unit (httptest) | `go test ./cmd/aura/ -run TestServeWebui` | ❌ Wave 0 (extend `serve_webui_test.go` — it currently asserts the *no-fallback* Phase-23 behavior; that case flips) |
| WEB-01 | AG-UI route prefixes keep precedence over `/` catch-all (incl. new `/api/`) | unit (httptest) | `go test ./cmd/aura/ -run TestServeWebui` | ✅ partial (`serve_webui_test.go` proves precedence; add `/api/` case) |
| WEB-02 | Loopback bind boots with no credential | unit (pure fn) | `go test ./internal/config/ -run TestGuardWebBind` | ❌ Wave 0 (`config_webauth_test.go`) |
| WEB-02 | Non-loopback + neither credential → error naming the vars | unit (pure fn) | `go test ./internal/config/ -run TestGuardWebBind` | ❌ Wave 0 |
| WEB-02 | Non-loopback + secret OR trust-proxy → boots | unit (pure fn) | `go test ./internal/config/ -run TestGuardWebBind` | ❌ Wave 0 |
| WEB-02 | `0.0.0.0` / `::` / `[::]` treated as non-loopback (gated) | unit (pure fn) | `go test ./internal/config/ -run TestGuardWebBind` | ❌ Wave 0 |
| WEB-03 | Empty configured secret → login rejected (fail-closed) | unit | `go test ./internal/agui/ -run TestValidateSecret` | ❌ Wave 0 (`internal/agui/auth_test.go`) |
| WEB-03 | Correct secret → login succeeds, sets cookie with HttpOnly+Secure+SameSite=Strict+Path=/ | unit (httptest) | `go test ./internal/agui/ -run TestLogin` | ❌ Wave 0 |
| WEB-03 | Cookie sign→verify round-trip identity == issued identity | property (rapid) | `go test ./internal/agui/ -run TestSignVerifyRoundtrip` | ❌ Wave 0 |
| WEB-03 | Tampered cookie payload/sig → verify fails (forgery rejected) | unit + property | `go test ./internal/agui/ -run TestVerifyTamper` | ❌ Wave 0 |
| WEB-03 | Expired cookie (issued + TTL < now) → verify fails | unit | `go test ./internal/agui/ -run TestVerifyExpiry` | ❌ Wave 0 |
| WEB-03 | `RequireAuth`: no cookie → redirect/401; valid cookie → next; deleted identity → reject | unit (httptest, fake identity store) | `go test ./internal/agui/ -run TestRequireAuth` | ❌ Wave 0 |
| WEB-03 | D-03 exceptions: `/login`, login assets, `/healthz` reachable without a cookie; everything else gated | unit (httptest) | `go test ./internal/agui/ -run TestRequireAuthPublicPaths` | ❌ Wave 0 |
| WEB-03 | `POST /agent/run` requires `HasCapability`; `local` (`*`) passes | integration (db_integration) | `go test -tags db_integration -race -p 1 ./internal/agui/ -run TestAgentRunCapability` | ❌ Wave 0 (real `identity.Store` over seeded `local` + migration 0004) |
| WEB-04 | `/healthz` + `/readyz` JSON shape stable (panel contract) | unit / integration | `go test ./internal/agui/ -run 'TestHealthz|TestReadyz'` | ✅ (`readiness_test.go`, `server_test.go`) — assert the panel-consumed fields don't regress |
| WEB-04 | Theme/density applied before paint (no flash) + health panel renders | smoke / E2E (Playwright) | `cd web && npx playwright test health-panel.spec.ts` | ❌ Wave 0 (extend the Phase-23 E2E that boots `aura serve`) |
| WEB-01/03 (live) | Boot `aura serve` on a non-loopback bind without a secret → process exits non-zero with the actionable message; with a secret → boots and serves the shell behind the auth redirect; `/api/nope` → 404 | live serve smoke | new `cmd/aura/serve_smoke_test.go` (build-tagged `serve_smoke`), boots the binary, asserts exit code + HTTP behavior | ❌ Wave 0 |

### Sampling Rate
- **Per task commit:** `go vet ./... && go build ./... && go test ./internal/agui/ ./internal/config/ ./internal/webui/ ./cmd/aura/` + `go test -race` on each touched package (CLAUDE.md post-edit gate).
- **Per wave merge:** full untagged suite + `go test -tags db_integration -race -p 1 ./internal/agui/ ./cmd/aura/` (the `capability_grants` binding tier) + the Playwright E2E.
- **Phase gate:** full `db_integration neo4j_integration` matrix green + `make coverage` ≥85% owned-surface + the live serve smoke + `scripts/agui_boundary_check.sh` (leaf invariant) before `/gsd-verify-work`.

### Race / goleak posture
- `RequireAuth` + `spaFallback` are stateless per-request handlers — no shared mutable state, so the race surface is low, but run `-race` on `internal/agui` and `cmd/aura` per CLAUDE.md (the SSE pump goroutines already demand it).
- The existing `agui` integration tests use the goleak discipline (`main_test.go`); the new auth handlers add no goroutines, so no new leak surface — but keep the `-race -p 1` invocation for the db_integration tier (shared PG).

### Property-based candidates (rapid, already vendored)
- **Cookie sign→verify round-trip:** for any identity id + issued time, `verifySession(signSession(...))` returns the same identity and `ok==true` within TTL. (the canonical property — catches encoding/MAC bugs.)
- **Tamper rejection:** for any single-byte mutation of a valid cookie value, `verifySession` returns `ok==false`. (forgery resistance.)
- **Constant-time compare equivalence:** `validateSecret(a,b)` agrees with `a==b && a!=""` on equality outcome across random strings (correctness, not timing — timing is asserted by *using* `subtle`, not measured).

### Wave 0 Gaps
- [ ] `internal/agui/auth.go` + `internal/agui/auth_test.go` — covers WEB-03 (sign/verify, validateSecret, RequireAuth, cookie flags); add `auth_cookie.go` if `auth.go` nears 600 LOC.
- [ ] `internal/agui/auth_capability_integration_test.go` (build tag `db_integration`) — covers WEB-03 `POST /agent/run` + `HasCapability` over the real seeded `local` identity.
- [ ] `internal/config/config_webauth_test.go` — covers WEB-02 `GuardWebBind` matrix; add `WebAuthSecret`/`WebTrustProxy` to `config_serve_test.go` env coverage.
- [ ] `internal/webui` SPA-fallback cases in `embed_test.go` + a new fallback handler — covers WEB-01 client-route → index.html and excluded-prefix → 404.
- [ ] `cmd/aura/serve_webui_test.go` — flip the "no SPA-fallback (Phase 24)" case to assert the fallback + add the `/api/` 404 case + an auth-redirect case.
- [ ] `cmd/aura/serve_smoke_test.go` (build tag `serve_smoke`) — covers WEB-02 fail-fast exit + WEB-01 live 404 + WEB-03 live auth-redirect by booting the real binary.
- [ ] `web/e2e/health-panel.spec.ts` (extend the Phase-23 Playwright E2E) — covers WEB-04 panel render + theme-before-paint.
- [ ] Framework install: none — stdlib `testing` + already-vendored `rapid` + Phase-23 Playwright.

## Security Domain

> `security_enforcement` is not `false` in config (absent ⇒ enabled). This phase is the milestone's auth trust boundary — security depth is the point.

### Applicable ASVS Categories

| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V2 Authentication | yes | `POST /login` with constant-time secret compare (`crypto/subtle`); fail-closed on empty secret; the operator-secret model (single-operator, D-01). |
| V3 Session Management | yes | HMAC-SHA256-signed stateless cookie; `HttpOnly + Secure + SameSite=Strict`; `__Host-` prefix; absolute TTL; `POST /logout` clears it (`MaxAge: -1`). |
| V4 Access Control | yes | `RequireAuth` whole-origin gate (D-03) + `capability_grants` (`HasCapability`) on the mutating `POST /agent/run` (D-04); deny-by-default (fail-closed redirect/403). |
| V5 Input Validation | yes | Login form passphrase bounded; cookie value base64url-decoded with explicit error handling; existing `MaxBytesReader` on `/agent/run` (`server.go:152`); thread-id UUID parse (`server.go:167`). |
| V6 Cryptography | yes | stdlib `crypto/hmac`/`crypto/subtle`/`crypto/rand` only — NEVER hand-rolled. HMAC key derived from `AURA_WEB_AUTH_SECRET`. |
| V7 Error Handling / Logging | yes | Existing `SanitizeString` redaction (`server.go:486`) on error bodies; auth failures log server-side, return generic 401/403/302 to the client (no "wrong password vs no user" oracle). |
| V12 Files / Resources | partial | SPA-fallback serves only embedded `fs.FS` assets (no path traversal — `fs.Stat` on the embed, not the host FS). |

### Known Threat Patterns for {Go single-binary cookie-auth SPA on the AG-UI gateway}

| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| Cookie theft via XSS | Information Disclosure / Spoofing | `HttpOnly` (no JS access); `Secure` (no plaintext transit); CSP from the SPA (Phase-23 PWA). |
| Cookie replay after expiry | Spoofing | Absolute TTL encoded in `issued_at`, verified server-side; `MaxAge` mirrors. |
| Session forgery (forged MAC) | Tampering | HMAC-SHA256 + `hmac.Equal` constant-time verify; tamper → reject. |
| Timing oracle on the secret | Information Disclosure | `subtle.ConstantTimeCompare` at login; `hmac.Equal` on the MAC. |
| SPA-fallback masking a real API 404 | Information Disclosure / contract break | `apiPrefixes` exclusion in `spaFallback`; test asserts `/api/nope` → 404, not HTML. |
| Trust-proxy header spoofing | Spoofing / Elevation of Privilege | NEVER read a client-supplied auth/identity header; `AURA_WEB_TRUST_PROXY` only means "Aura stays hands-off, the proxy gates" — Aura reads cookies, not `X-*` headers. |
| Fail-open on misconfigured/empty secret | Elevation of Privilege | `validateSecret` returns false when `configured==""`; boot guard refuses non-loopback exposure without a credential. |
| Session fixation | Spoofing | The cookie is minted server-side at login (not accepted from the client pre-auth); a fresh `issued_at` per login. |
| CSRF on the cookie-auth POST routes | Tampering | `SameSite=Strict` (baseline). Same-origin SPA, no cross-origin write path → no extra token needed (see Discretion Resolution). |
| Unauthenticated non-loopback exposure | Information Disclosure / EoP | WEB-02 fail-fast boot guard. |
| Slowloris on the exposed bind | Denial of Service | Existing `ReadHeaderTimeout` (`serve.go:258`); consider `IdleTimeout`/`WriteTimeout` if exposing publicly (golang-security network.md). |
| pprof/`/debug/vars` exposed when non-loopback | Information Disclosure | D-03 whole-origin gate puts `/debug/vars` + `/metrics` behind `RequireAuth` (NOT in the public exception list — only `/healthz` is). |

### Discretion Resolution — CSRF posture (Discretion item)
**Recommendation: `SameSite=Strict` only, NO additional CSRF token — for now.**
Justification: (1) The cockpit is a same-origin SPA served by the same binary that owns the auth — there is no cross-origin write path in Phase 24 (the only mutating route is `POST /agent/run`, same-origin). (2) `SameSite=Strict` prevents the browser from sending the cookie on any cross-site request, which covers the classic CSRF vector for a cookie-auth SPA. (3) golang-security cookies.md lists the double-submit token as a *best-practice checklist item*, not a mandate, and the CONTEXT.md Discretion default is explicitly "SameSite-only unless a concrete cross-origin write path appears." (4) Adding a CSRF token now is complexity without a threat to counter (the no-atomic-bombs doctrine). **Re-evaluate** if Phase 28/29 introduces a cross-origin embed or a third-party-initiated write — at that point add a double-submit `__Host-CSRF` token. Document the decision inline (golang-security: "add a brief inline comment so the decision is documented and won't be re-flagged").

### Discretion Resolution — trust-proxy header handling (Discretion item)
**Recommendation:** `AURA_WEB_TRUST_PROXY=true` unlocks the non-loopback boot guard and means Aura does NOT enforce in-binary auth (the proxy terminates it). Aura still reads its OWN session cookie if present, but it MUST NOT read or trust any proxy-injected identity header (`X-Forwarded-User`, `X-Auth-Request-Email`, etc.) — golang-security High anti-pattern "trusting client headers." The proxy is responsible for not forwarding spoofed headers; Aura's posture is "hands-off, the origin is gated upstream." This keeps the two D-05 paths cleanly separated: cookie path = Aura enforces; proxy path = Aura defers.

## Sources

### Primary (HIGH confidence)
- In-repo source read this session: `cmd/aura/serve.go`, `cmd/aura/serve_webui.go`, `cmd/aura/serve_webui_test.go`, `internal/agui/server.go`, `internal/agui/readiness.go`, `internal/agui/server_integration_test.go`, `internal/webui/embed.go`, `internal/webui/doc.go`, `internal/webui/embed_test.go`, `internal/identity/store.go`, `internal/config/config.go`, `internal/config/config_validate_test.go`, `web/index.html`, `scripts/agui_boundary_check.sh`, `go.mod` — every code claim grounded here.
- `.planning/research/ARCHITECTURE.md` §3 (serve/embed SPA-fallback), §4 (Web Auth GAP-2 three options + four-layer write protection + identity seam), §5 (observability runtime_status), §7 Phase A.
- `.planning/phases/24-web-foundation-serve-auth-health/24-CONTEXT.md` (D-01..D-08 + Discretion + Deferred — the locked decisions).
- `.planning/ROADMAP.md` Phase 24 §(goal + SC1-SC4); `.planning/REQUIREMENTS.md` WEB-01..04 + traceability.
- `.claude/skills/golang-security/SKILL.md` + `references/cookies.md` + `references/network.md` — cookie flags, constant-time compare, `hmac.Equal`, client-header anti-pattern, fail-closed, bind-to-0.0.0.0.
- `pkg.go.dev/net/http#Cookie` — `http.Cookie` field set (`HttpOnly`/`Secure`/`SameSite`/`MaxAge`), `Cookie.Valid()`, no stdlib signing helper. `[VERIFIED]`

### Secondary (MEDIUM confidence)
- `.planning/phases/23-frontend-infrastructure-industrial-foundation/23-CONTEXT.md` (D-07/D-08 theme-before-paint, D-14 React Router, D-05/D-06 committed dist + freshness gate) — carried-forward decisions, not re-litigated.
- CLAUDE.md project rules (no god class ≤600 LOC, coverage floor 85%, no-skip-as-green, env naming `AURA_<DOMAIN>_<UNIT>`, deferred-tool pattern, master-direct workflow).

### Tertiary (LOW confidence)
- None — every claim is either in-repo-verified or cited to an authoritative source. The `@tanstack/react-query` presence (A1) is the only `[ASSUMED]` item and is flagged for plan-time confirmation.

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH — entirely stdlib + in-repo, every package read this session; `go.mod` confirms no session library is vendored (so the stdlib path is forced, which is also the recommended path).
- Architecture: HIGH — the SPA-fallback, mux precedence, boot-guard, and auth-middleware patterns are grounded in the existing `serve_webui.go` + `serve.go` + `server.go` and the ARCHITECTURE.md design; the existing `serve_webui_test.go` already proves the precedence the SPA-fallback relies on.
- Pitfalls: HIGH — derived from the actual code shape (Secure-over-loopback, leaf-invariant check, `0.0.0.0` detection) and golang-security refs.
- Security domain: HIGH — golang-security skill + the explicit CONTEXT.md threat-model-input requirements; CSRF/trust-proxy discretion resolved with justification.

**Research date:** 2026-06-16
**Valid until:** 2026-07-16 (30 days — stable stdlib surface; the only moving part is the Phase-23 frontend toolchain, which is locked).
