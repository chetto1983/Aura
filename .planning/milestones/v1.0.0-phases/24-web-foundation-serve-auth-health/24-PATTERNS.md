# Phase 24: Web Foundation — Serve + Auth + Health - Pattern Map

**Mapped:** 2026-06-16
**Files analyzed:** 8 (4 modified, 3 new code, plus their tests)
**Analogs found:** 8 / 8 (every new/modified file has a concrete in-repo analog)

> All file access for this map was read-only. The single security primitive the
> RESEARCH.md treats as "net-new" — the constant-time secret compare — **already
> exists in-repo** at `internal/setup/token.go:45-52` (`crypto/subtle.ConstantTimeCompare`).
> The HMAC-signing of a cookie is the only genuinely net-new code, and it is
> stdlib-only (`crypto/hmac` + `crypto/sha256` + `encoding/base64`); no analog,
> follow RESEARCH.md §Pattern 2 verbatim.

> **Load-bearing reconciliation the planner MUST surface to the executor:** an
> `/api/` prefix is ALREADY mounted on the parent mux today —
> `integrationsRoutePrefix = "/api/integrations/"` (`cmd/aura/integrations_proxy.go:34`,
> registered at `serve_webui.go:58`). The "forward-compat `/api/` carve-out"
> (CONTEXT.md Discretion, RESEARCH.md §Pattern 1) is therefore NOT a greenfield
> add — it must be the SPA-fallback EXCLUSION prefix (so `/api/anything` 404s
> instead of serving the SPA), and it already has one real consumer
> (`/api/integrations/`). Do not re-register `/api/` on the mux (it would collide
> with the existing `/api/integrations/` subtree); add it only to the fallback's
> exclusion list.

## File Classification

| New/Modified File | Role | Data Flow | Closest Analog | Match Quality |
|-------------------|------|-----------|----------------|---------------|
| `internal/webui/embed.go` (MOD) | embed / static handler | request-response (file I/O) | itself (`Handler()` today) + `cmd/aura/integrations_proxy.go` (prefix-exclusion dispatch) | exact (same file, upgrade in place) |
| `cmd/aura/serve_webui.go` (MOD) | route wiring / parent mux | request-response | itself (`newServeHandler`, `aguiRoutePrefixes`) | exact |
| `cmd/aura/serve.go` (MOD) | boot / composition root | boot-time config validation | `config.Validate()` (`config.go:198`) + existing `bootServe` error path (`serve.go:101-105,250-254`) | exact (same fail-fast shape) |
| `internal/config/config.go` (MOD) | config | env load + pure validation | itself (`Validate`, `loadBase`, `envDefault`/`envBoolDefault`) | exact |
| `internal/agui/auth.go` (NEW) | middleware + HTTP handler | request-response (auth gate) | `internal/setup/server.go` `requireSetupToken` (gate) + `internal/setup/token.go` (constant-time compare) + `internal/agui/server.go` `withCORS` (middleware-wrap idiom) | role-match (strong) — cookie-HMAC body net-new |
| `internal/agui/auth_test.go` (NEW) | test | unit / property | `cmd/aura/serve_webui_test.go` + `internal/webui/embed_test.go` (httptest, no testify) | exact (HTTP-surface test convention) |
| `internal/config/config_webauth_test.go` (NEW) | test | unit (table) | `internal/config/config_validate_test.go` (pure-fn table) | role-match |
| `internal/webui/dist/*` (login page assets) | embed (static) | n/a | existing `dist/` tree (leaf invariant) | exact |

## Pattern Assignments

### `internal/webui/embed.go` (embed / static handler — WEB-01)

**Analog:** itself (`Handler()` today, lines 30-36) + the prefix-dispatch shape from
`cmd/aura/integrations_proxy.go:78-88`.

**What changes:** `Handler()` currently returns a bare `http.FileServerFS(sub)`. WEB-01
wraps it in an SPA-fallback `http.HandlerFunc` that (a) returns a real 404 for excluded
API prefixes, (b) serves `index.html` for unmatched client routes, (c) serves the asset
otherwise. **Stays leaf-level** — import only `embed`/`io/fs`/`net/http`/`strings`
(NO `internal/*`; `scripts/agui_boundary_check.sh` enforces).

**Current handler to replace** (embed.go:30-36):
```go
func Handler() (http.Handler, error) {
	sub, err := Sub()
	if err != nil {
		return nil, err
	}
	return http.FileServerFS(sub), nil
}
```

**Prefix-exclusion idiom to copy** (the trim-prefix + `strings.HasPrefix` loop is
exactly the dispatch shape already used in `integrations_proxy.go:79-85`):
```go
rest := strings.TrimPrefix(r.URL.Path, "/")
for _, api := range apiPrefixes {
    if strings.HasPrefix(rest, api) { http.NotFound(w, r); return }
}
```
Then `fs.Stat(sub, p)` to decide asset-vs-index.html (RESEARCH.md §Pattern 1, lines 219-237).

**Comment to update in the same commit** (DEEP REFACTOR ON TOUCH): embed.go:27-29 says
"Phase-23 scope is the static placeholder only … DO NOT add fallback logic here." WEB-01
is the sanctioned supersession — rewrite that comment (RESEARCH.md §State of the Art, line 510).

---

### `cmd/aura/serve_webui.go` (parent mux / route wiring — WEB-01 + WEB-03)

**Analog:** itself — `aguiRoutePrefixes` (lines 33-40) + `newServeHandler` (lines 47-61).

**Existing exclusion set to extend** (serve_webui.go:33-40):
```go
var aguiRoutePrefixes = []string{
	"/healthz",
	"/readyz",
	"/debug/vars",
	"/metrics",
	"/agent/run",
	"/threads/",
}
```
The fallback's `apiPrefixes` (in `embed.go`) must be derived from / kept in sync with this
list + `integrationsRoutePrefix` ("/api/integrations/") + the new forward-compat `/api/`
carve-out (RESEARCH.md §Pattern 1 note "single source of truth", Pitfall 6). The
**single-source recommendation**: export the prefix list (or pass a trimmed copy into
`webui.Handler`) so the two consumers cannot drift.

**Existing mux assembly to wrap** (serve_webui.go:47-61):
```go
func newServeHandler(aguiHandler http.Handler) (http.Handler, error) {
	static, err := webui.Handler()
	if err != nil {
		return nil, fmt.Errorf("webui handler: %w", err)
	}
	mux := http.NewServeMux()
	for _, prefix := range aguiRoutePrefixes {
		mux.Handle(prefix, aguiHandler)
	}
	mux.Handle(integrationsRoutePrefix, newIntegrationsProxy())
	mux.Handle("/", static)
	return mux, nil
}
```
WEB-03 wraps the *returned* handler (or the subtree) in `agui.RequireAuth(...)` — the
whole-origin gate (D-03). The `RequireAuth` wrap goes around `mux` here (the parent), with
the public-path exceptions (`/login`, login assets, `/healthz`) handled inside the
middleware, NOT by leaving routes unwrapped. `newServeHandler`'s signature will need the
auth deps (signing key, TTL, identity store) threaded from `bootServe`.

**Go 1.22 ServeMux precedence** the whole design relies on is already PROVEN by
`serve_webui_test.go` (the `/healthz` and `/threads/{id}/messages` cases route to the AG-UI
handler, not `/`). The SPA-fallback adds no new mux registration for client routes — they
fall through `/` to the fallback.

---

### `cmd/aura/serve.go` (boot guard — WEB-02)

**Analog:** `config.Validate()` (config.go:198-210) for the pure-function fail-fast shape;
the existing `bootServe` error path (serve.go:101-105 + 250-254) for the wiring.

**Existing fail-fast wiring to copy** — `bootServe` already returns an error that `runServe`
turns into a clean non-zero exit (serve.go:101-105):
```go
env, err := bootServe(workCtx, override)
if err != nil {
	fmt.Fprintln(os.Stderr, "aura serve:", err)
	os.Exit(exitInfra)
}
```
And inside `bootServe` the same return-on-error idiom already exists (serve.go:250-254):
```go
serveHandler, err := newServeHandler(aguiServer.Mux())
if err != nil {
	chat.close()
	return nil, fmt.Errorf("build serve handler: %w", err)
}
```

**Where the guard lands:** inside `bootServe`, BEFORE `httpSrv` is built (around serve.go:250).
RESEARCH.md §Code Examples (lines 438-441):
```go
if err := config.GuardWebBind(chat.cfg.AGUIBind, chat.cfg.WebAuthSecret, chat.cfg.WebTrustProxy); err != nil {
	chat.close()
	return nil, err
}
```

**Comment to update (DEEP REFACTOR ON TOUCH):** serve.go:219-221 ("The bind is hardcoded
loopback via the config default — the compensating control … amendment #35") is now stale —
WEB-02/D-06 lifts the hardcoded restriction. Rewrite in the same commit.

---

### `internal/config/config.go` (config knobs + boot-guard fn — WEB-02 / D-05 / D-06)

**Analog:** itself — `Validate()` (the pure fail-fast), `loadBase()` (the env load), and the
`envDefault`/`envBoolDefault` helpers.

**`Validate()` is the EXACT shape to mirror for `GuardWebBind`** (config.go:198-210):
```go
func (c *Config) Validate() error {
	var missing []string
	if strings.TrimSpace(c.DB.URL) == "" {
		missing = append(missing, "POSTGRES_PASSWORD (or AURA_DB_URL)")
	}
	if strings.TrimSpace(c.Neo4j.Password) == "" {
		missing = append(missing, "NEO4J_PASSWORD")
	}
	if len(missing) > 0 {
		return fmt.Errorf("config: required secret(s) unset: %s", strings.Join(missing, ", "))
	}
	return nil
}
```
`GuardWebBind(bind, secret string, trustProxy bool) error` follows the same pure-function,
`fmt.Errorf("config: …")`-named-cause posture (RESEARCH.md §Pattern 3, lines 332-348). Use
`net.SplitHostPort` + `net.ParseIP(...).IsLoopback()` — `net` is ALREADY imported (config.go:14,
used by `composeDSN`'s `net.JoinHostPort` at line 347).

**New env knobs follow the existing AG-UI block exactly** (config.go:309-311):
```go
AGUIBind:           envDefault("AURA_AGUI_BIND", "127.0.0.1:9080"),
AGUICORSPermissive: envBoolDefault("AURA_AGUI_CORS_PERMISSIVE", false),
AGUIBufferCap:      envIntDefault("AURA_AGUI_BUFFER_CAP", 64),
```
Add: `WebAuthSecret: os.Getenv("AURA_WEB_AUTH_SECRET")` (empty default, NOT boot-fatal — the
guard decides), `WebTrustProxy: envBoolDefault("AURA_WEB_TRUST_PROXY", false)`. Naming obeys
`AURA_<DOMAIN>_<UNIT>` (CLAUDE.md §Env vars). Add the struct fields beside `AGUIBind`
(config.go:111-113) with the same one-line-comment convention; document them in the PRD env
catalog + `.env.example` (RESEARCH.md Runtime State Inventory).

---

### `internal/agui/auth.go` (RequireAuth middleware + POST /login + cookie sign/verify — WEB-03) [NEW]

**Closest middleware analog:** `internal/setup/server.go` `requireSetupToken` (server.go:169-181)
— the exact "wrap next, validate a credential, 401-or-pass" shape Aura uses for an HTTP gate:
```go
func (s *Server) requireSetupToken(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		presented := r.URL.Query().Get("token")
		if presented == "" {
			presented = r.Header.Get(setupHeaderName)
		}
		if !s.token.Valid(presented) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}
```
`RequireAuth` copies this skeleton but reads the **cookie** (`r.Cookie(name)`), never a
client header — and adds the D-03 public-path exception check first (RESEARCH.md §Code
Examples lines 448-471). Note the difference: `requireSetupToken` accepts a `?token=` query
param + header; `RequireAuth` must NOT (cookie only; golang-security client-header
anti-pattern, RESEARCH.md Anti-Patterns line 356).

**Closest constant-time-compare analog (CRITICAL — this is in-repo, NOT net-new):**
`internal/setup/token.go:45-52` already does exactly the fail-closed `crypto/subtle` compare
D-01 wants:
```go
func (t *Token) Valid(presented string) bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	if !t.valid || presented == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(presented), []byte(t.value)) == 1
}
```
`validateSecret(provided, configured string) bool` is the same idea: fail-closed when
`configured == ""` (RESEARCH.md §Pattern 2, lines 258-263). Reuse the `crypto/subtle` import
pattern verbatim from token.go.

**Middleware-wrap idiom (returns `next` unchanged when disabled):** `agui/server.go` `withCORS`
(server.go:131-146) is the in-package precedent for "conditionally wrap the mux":
```go
func (s *Server) withCORS(next http.Handler) http.Handler {
	if !s.cfg.CORSPermissive {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { ... })
}
```
`RequireAuth` mirrors this: when no `AURA_WEB_AUTH_SECRET` is configured (loopback dev), the
wrap can be a no-op pass-through (the boot guard guarantees this only happens on loopback).

**Cookie attribute set (locked by D-02 / SC3)** — `http.SetCookie` + `http.Cookie{}`, the
stdlib fields, NOT a library. There is NO in-repo SetCookie analog (grep across the repo:
only planning docs + token.go); follow RESEARCH.md §Pattern 2 (lines 305-316) and
golang-security cookies.md "Cookie Prefix Examples" (`__Host-` requires `Secure:true`,
`Domain:""`, `Path:"/"`):
```go
http.SetCookie(w, &http.Cookie{
	Name:     sessionCookieName, // "__Host-aura_session"
	Value:    value,
	Path:     "/",
	HttpOnly: true,                    // CWE-1004
	Secure:   true,                    // CWE-614
	SameSite: http.SameSiteStrictMode, // CWE-352
	MaxAge:   int(ttl.Seconds()),
})
```

**HMAC sign/verify (NET-NEW — no in-repo analog, stdlib only):** `crypto/hmac` +
`crypto/sha256` + `crypto/subtle`/`hmac.Equal` + `encoding/base64`. Follow RESEARCH.md
§Pattern 2 (`signSession`/`verifySession`, lines 266-303) verbatim. Use `hmac.Equal` for the
MAC compare (constant-time), NOT `bytes.Equal`.

**Principal binding + capability check seam:** `internal/identity/store.go` —
`GetIdentityByID` (store.go:100-113) confirms the bound identity still exists on every
protected request; `HasCapability` (store.go:128-138) gates the only mutating route
`POST /agent/run` (D-04). The `local` identity (`00000000-0000-0000-0000-000000000001`) holds
the `*` Wildcard (store.go:29, seeded by migration 0004) so `HasCapability` passes via the
wildcard branch regardless of the capability name chosen:
```go
func (s *Store) HasCapability(ctx context.Context, identityID, capability string) (bool, error) {
	id, err := parseUUID(identityID)
	if err != nil {
		return false, fmt.Errorf("has capability: %w", err)
	}
	ok, err := s.q.HasCapability(ctx, sqlc.HasCapabilityParams{IdentityID: id, Capability: capability})
	...
}
```
Declare a narrow consumer-side interface (D-A2-02 "accept interfaces" — `identity` declares no
interface; the consumer does) so `auth_test.go` can inject a fake store. See the precedent:
`agui/server.go` declares its own narrow `Runner` interface (server.go:57-60) rather than
importing the whole Runner.

**File-size discipline (CLAUDE.md NO GOD CLASS ≤600 LOC):** if `auth.go` nears 600 LOC, split
the cookie sign/verify/encode into `auth_cookie.go` (RESEARCH.md File Structure note, line 199;
the `<name>_<concern>.go` split convention).

---

### `internal/agui/auth_test.go` (NEW) + `internal/config/config_webauth_test.go` (NEW)

**Analog (HTTP surface):** `cmd/aura/serve_webui_test.go` + `internal/webui/embed_test.go` —
stdlib `testing` + `net/http/httptest`, **no testify** (the agui/webui surface convention,
asserted in RESEARCH.md Validation Architecture line 553). The fake-handler-records-hits
precedence pattern (`serve_webui_test.go:21-27`) is the template for the SPA-fallback +
auth-redirect cases.

**Analog (pure-fn table test):** `internal/config/config_validate_test.go` is the template for
the `GuardWebBind` matrix (loopback/non-loopback × secret-set/unset × proxy-true/false +
`0.0.0.0`/`::`/`[::]`). RESEARCH.md Test Map rows WEB-02.

**Property tests (rapid, already vendored `go.mod:31`):** cookie sign→verify round-trip +
single-byte-tamper rejection (RESEARCH.md Property-based candidates, lines 594-597). No new
framework install.

**Existing test to FLIP (do not add a parallel case):** `serve_webui_test.go:95-108`
("GET bogus asset -> 404 (no SPA-fallback, Phase 24)") currently asserts the Phase-23
no-fallback behavior. WEB-01 inverts it: an unknown *client* route now returns index.html;
the 404 assertion moves to an *excluded API prefix* (`/api/nope`, `/agent/typo`). This is a
legitimate test rewrite (the behavior changed) — justify in the commit message
(CLAUDE.md NEVER MODIFY TESTS TO MAKE THEM PASS, but the contract changed).

## Shared Patterns

### Constant-time secret compare (fail-closed)
**Source:** `internal/setup/token.go:45-52` (`crypto/subtle.ConstantTimeCompare`, in-repo, proven).
**Apply to:** `internal/agui/auth.go` `validateSecret`.
**Note:** RESEARCH.md flags this as needing care ("note whether any analog exists in-repo or
it is net-new") — **it is in-repo.** Copy the token.go idiom: guard `configured == ""` →
`return false` first, then `subtle.ConstantTimeCompare([]byte(provided),[]byte(configured)) == 1`.

### HTTP middleware that conditionally wraps the mux
**Source:** `internal/agui/server.go:131-146` (`withCORS` — returns `next` unchanged when the
knob is off) + `internal/setup/server.go:169-181` (`requireSetupToken` — wrap/validate/401).
**Apply to:** `internal/agui/auth.go` `RequireAuth` (D-03 whole-origin gate). Read the cookie,
never a client header.

### Go 1.22 ServeMux longest-pattern precedence (no router)
**Source:** `cmd/aura/serve_webui.go:47-61` + `internal/agui/server.go:90-99` (Mux) +
`internal/setup/server.go:129-138` (Mux) — three in-repo precedents, all `http.NewServeMux`
with method-pattern routing, **no chi/gorilla** (server.go:86 "no chi/gorilla — matches the
no-router codebase posture").
**Apply to:** the SPA-fallback + `/api/` exclusion (WEB-01). Proven by `serve_webui_test.go`.

### Pure-function fail-fast config validation
**Source:** `internal/config/config.go:198-210` (`Validate()`).
**Apply to:** `config.GuardWebBind` (WEB-02). Same `fmt.Errorf("config: …")` named-cause shape;
table-testable without booting.

### Boot-time error → clean non-zero exit (no panic)
**Source:** `cmd/aura/serve.go:101-105` (runServe prints `aura serve: <err>` + `os.Exit(exitInfra)`)
and the `bootServe` return-on-error idiom (serve.go:250-254).
**Apply to:** the WEB-02 guard call inside `bootServe` — return the error, let `runServe` exit.

### Narrow consumer-side interface for testability (accept interfaces)
**Source:** `internal/agui/server.go:57-60` (`Runner` interface declared at the consumer) +
`internal/setup/server.go:33-37` (`Store` interface declared at the consumer).
**Apply to:** the identity-store seam in `auth.go` — declare the 1-2 methods (`GetIdentityByID`,
`HasCapability`) the middleware needs so tests inject a fake; `*identity.Store` satisfies it
implicitly. (`internal/identity` itself declares no interface — store.go:7-9.)

### Error/secret redaction on the wire
**Source:** `internal/agui/server.go:486-502` (`SanitizeString`) + the `/readyz`/`/healthz`
bodies (`readiness.go:45`, `server.go:113`).
**Apply to:** auth-failure responses must be generic ("unauthorized"/"forbidden", no
"wrong-password vs no-user" oracle — RESEARCH.md Security Domain V7). Reuse `SanitizeString`
for any error surfaced from the identity store.

### Leaf-level embed invariant
**Source:** `internal/webui/embed.go` (imports only `embed`/`io/fs`/`net/http`) +
`internal/webui/doc.go`; enforced by `scripts/agui_boundary_check.sh`.
**Apply to:** the login page assets land in `internal/webui/dist/` (static); the login LOGIC
lives in `internal/agui/auth.go`. `internal/webui` must NOT import `internal/agui` or
`internal/identity` (RESEARCH.md Pitfall 5).

## No Analog Found

| File / Concern | Role | Data Flow | Reason |
|----------------|------|-----------|--------|
| `internal/agui/auth.go` cookie HMAC sign/verify (`signSession`/`verifySession`) | crypto helper | transform | No HMAC-signed-cookie code exists anywhere in the repo (grep confirms only `internal/setup/token.go` touches `crypto/subtle`, and it does a plain constant-time *compare*, not HMAC signing). This body is net-new, stdlib-only (`crypto/hmac`+`crypto/sha256`+`encoding/base64`). Follow RESEARCH.md §Pattern 2 (lines 266-303) — do NOT add `gorilla/securecookie`/`sessions` (not vendored; RESEARCH.md rejects it). |
| WEB-04 React health panel + theme-before-paint reuse | frontend component | request-response (REST poll) | Frontend (Phase-23 React/Vite toolchain), out of the Go-analog scope of this map. Data sources are the EXISTING `/healthz` (`agui/server.go:101-121`) + `/readyz` (`agui/readiness.go:33-61`) bodies; no new backend endpoint (D-07). The pre-paint script is reused verbatim from `web/index.html` (Phase 23 D-08). |
| `cmd/aura/serve_smoke_test.go` (build tag `serve_smoke`, boots the real binary) | live smoke test | process | No in-repo binary-boot smoke harness for the serve daemon was located in this pass; the planner should check for an existing `*_smoke_test.go` / live-tier pattern (e.g. the agui live smoke referenced in `server.go:166`) before writing fresh. |

## Metadata

**Analog search scope:** `cmd/aura/` (serve*.go, integrations_proxy*.go), `internal/agui/`
(server.go, readiness.go), `internal/webui/` (embed.go, embed_test.go), `internal/config/`
(config.go), `internal/identity/` (store.go), `internal/setup/` (server.go, token.go),
`.claude/skills/golang-security/references/` (cookies.md).
**Files read this pass:** 13 source/test/skill files (all read-only).
**Cross-cutting verification:** repo-wide grep for `crypto/hmac|crypto/subtle|hmac.Equal|
ConstantTimeCompare|SameSiteStrictMode|http.SetCookie|HttpOnly` — only `internal/setup/token.go`
matches among non-doc files (confirms the constant-time analog is in-repo; the cookie/HMAC
surface is net-new).
**Pattern extraction date:** 2026-06-16
