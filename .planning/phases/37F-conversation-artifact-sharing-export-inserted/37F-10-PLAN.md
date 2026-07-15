---
phase: 37F-conversation-artifact-sharing-export-inserted
plan: 10
type: execute
wave: 5
depends_on: ["37F-08"]
files_modified:
  - internal/agui/share_api.go
  - internal/agui/share_api_internal.go
  - internal/agui/share_api_public.go
  - internal/agui/share_service.go
  - internal/agui/server.go
  - internal/agui/share_api_test.go
autonomous: true
requirements: [WEBSHARE-02, WEBSHARE-03]

must_haves:
  truths:
    - "POST /api/shares mints an internal link with NO capability required — any owner shares their own thread internally"
    - "POST /api/shares with tier=public is denied when the org kill-switch is off, EVEN on loopback where RequireCapability is a pass-through"
    - "The plaintext token appears in the create response body exactly once and in no log line and no audit row"
    - "GET /api/shares/{id}/data lets ANY authenticated identity holding an internal link resolve its already-redacted snapshot (D-10) — this route is ResolveInternal's only consumer, and without it the internal tier mints rows no recipient can open"
    - "GET /api/shares/{id}/data returns 401/302 for an anonymous caller — the internal tier is NOT on the public allowlist (SC4 row 4)"
    - "GET /api/shares/{id}/asset/{assetID} streams only artifacts belonging to THAT share's snapshot — a bearer cannot pivot to another snapshot's bytes"
    - "An unknown, revoked, expired, or PUBLIC-tier id on the internal routes all return an identical 404 — no tier oracle"
    - "GET /s/{token}/data resolves a live public link without any session"
    - "GET /s/{token}/asset/{id} streams only artifacts belonging to THAT token's snapshot"
    - "Unknown, expired, and revoked tokens all return an identical 404 status and body — no oracle"
    - "A public token grants zero access to /api/* — the identity lane stays closed to it"
  artifacts:
    - path: "internal/agui/share_api.go"
      provides: "the owner-scoped share CRUD handlers (RequireAuth + owner predicate, 404-on-foreign)"
      min_lines: 100
    - path: "internal/agui/share_api_internal.go"
      provides: "the D-10 bearer-within-auth handlers: GET /api/shares/{id}/data + /asset/{assetID} (RequireAuth, NO owner predicate)"
      min_lines: 50
    - path: "internal/agui/share_api_public.go"
      provides: "the unauthenticated /s/{token} token handlers"
      min_lines: 60
    - path: "internal/agui/share_service.go"
      provides: "the narrow ShareService interface consumed by AG-UI handlers"
      min_lines: 20
  key_links:
    - from: "internal/agui/server.go"
      to: "internal/agui/share_api.go"
      via: "s.registerShareRoutes(mux) alongside registerAssetRoutes"
      pattern: "registerShareRoutes"
    - from: "internal/agui/share_api.go"
      to: "the org kill-switch"
      via: "in-handler re-check, not only the mount gate"
      pattern: "PublicEnabled|publicEnabled"
    - from: "internal/share/service.go ResolveInternal (plan 37F-08)"
      to: "internal/agui/share_api_internal.go handleShareResolveInternal"
      via: "GET /api/shares/{id}/data — the ONLY HTTP consumer of ResolveInternal; D-10 is unreachable without it"
      pattern: "ResolveInternal"
    - from: "internal/agui/share_api_internal.go"
      to: "token-scoped share/ blobs"
      via: "handleShareAssetInternal serves the SAME bundled bytes as the public handler, scoped to that share's snapshot"
      pattern: "handleShareAssetInternal"
  prohibitions:
    - "MUST NOT rely on the route-mount RequireCapability alone for the public tier — auth.go:282 returns next unchanged when !SecretConfigured, so on loopback the mount gate DOES NOT EXIST. The kill-switch MUST be re-checked inside the handler."
    - "MUST NOT distinguish unknown vs expired vs revoked in the status or the body — that is an oracle confirming a token WAS valid"
    - "MUST NOT resolve a share's asset through assets.Service — the public asset handler reads only token-scoped share/ blobs"
    - "MUST NOT serve a bundled artifact with its real MIME — application/octet-stream + nosniff regardless (37A D-10)"
    - "MUST NOT require a capability for the internal tier"
    - "MUST NOT add an owner predicate to GET /api/shares/{id}/data or GET /api/shares/{id}/asset/{assetID} — D-10 is bearer-within-auth; an owner check makes the internal tier owner-only and breaks SC4 row 3. This is the opposite rule from the owner-scoped CRUD handlers in the SAME package, which is exactly why they live in a separate file."
    - "MUST NOT mount the internal routes under /s/ — isPublicShareRoute (plan 37F-12) admits EVERY GET /s/... as unauthenticated, so an internal share routed there would resolve anonymously and break SC4 row 4"
    - "MUST NOT return a distinct status for a public-tier id on the internal routes — 404, byte-identical to unknown/revoked/expired. A 403 or 409 here is a tier oracle."
    - "MUST NOT resolve an internal share's artifact through assets.Service — same copy-never-reference rule as the public handler; the bearer is not the owner, so the identity lane would 404 anyway"
    - "MUST NOT log the plaintext token or put it in a share_audit row"
    - "MUST NOT accept an unbounded request body — cap with http.MaxBytesReader"
    - "MUST NOT return 403 on a foreign share id — owner-scoped reads/mutates 404"
    - "MUST NOT put more than one build tag on the test file"
---

<objective>
Expose the share lifecycle over HTTP across **three distinct trust boundaries**, one file each: the
owner-scoped CRUD surface (`share_api.go`), the D-10 bearer-within-auth internal routes
(`share_api_internal.go`), and the unauthenticated token routes (`share_api_public.go`).

**The internal routes are the ones a reader will not expect, so read this first.** `share.Service`
(plan 37F-08) exposes `ResolveInternal`, and `GET /api/shares/{id}/data` is its **only** consumer. It
cannot be served by anything else in this plan: the three owner-scoped routes 404 for a non-owner *by
design*, and `GET /s/{token}/data` is structurally impossible for an internal share because the
migration's own `shared_links_tier_shape` CHECK (plan 37F-02) forces `token_hash IS NULL` for
`tier='internal'` — there is no token to put in a `/s/` URL, and `ResolveByToken`'s `WHERE token_hash =
$1` can never match NULL. Omit these routes and the internal tier — which **D-01 designates the DEFAULT
share action** — mints a row no recipient can ever open.

The security centre of this plan is **R-08**. `RequireCapability` (`auth.go:281-283`) returns `next`
unchanged when `!SecretConfigured` — so on loopback dev the `share.public` gate **does not exist**. That
is fine for `governance.write` (loopback is the operator's own box) and **not** fine for a link designed
to leave the box. The closure is two independent gates: the capability at the mount (plan 37F-12) **and**
the org kill-switch re-checked *inside* the handler, where no bypass applies. One of them survives
loopback. This plan owns the second.

Purpose: the HTTP surface, with the loopback fail-open closed and no token oracle.
Output: `internal/agui/share_api.go` + `share_service.go` + 2 lines of `server.go`.
</objective>

<execution_context>
@/home/user/Aura/.claude/get-shit-done/workflows/execute-plan.md
@/home/user/Aura/.claude/get-shit-done/templates/summary.md
</execution_context>

<context>
@.planning/phases/37F-conversation-artifact-sharing-export-inserted/37F-RESEARCH.md
@.planning/phases/37F-conversation-artifact-sharing-export-inserted/37F-PATTERNS.md
@.planning/phases/37F-conversation-artifact-sharing-export-inserted/37F-VALIDATION.md
@internal/share/service.go
@CLAUDE.md
</context>

## Artifacts this plan produces

`agui.ShareService`, `agui.registerShareRoutes`, `agui.handleShareCreate`, `handleShareList`,
`handleShareUpdateSnapshot`, `handleShareRevoke`, `handleShareResolveInternal`, `handleShareAssetInternal`,
`handleShareResolvePublic`, `handleShareAssetPublic`, and **eight** routes across three trust boundaries:

| Route | Boundary | Gate |
|---|---|---|
| `POST /api/shares` | owner | RequireAuth; public tier additionally capability + in-handler kill-switch |
| `GET /api/shares` | owner | RequireAuth + owner predicate (404-on-foreign) |
| `PATCH /api/shares/{id}/snapshot` | owner | RequireAuth + owner predicate (404-on-foreign) |
| `DELETE /api/shares/{id}` | owner | RequireAuth + owner predicate (404-on-foreign) |
| `GET /api/shares/{id}/data` | **bearer-within-auth (D-10)** | RequireAuth ONLY — no capability, **no owner predicate** |
| `GET /api/shares/{id}/asset/{assetID}` | **bearer-within-auth (D-10)** | RequireAuth ONLY — no capability, **no owner predicate**; snapshot-scoped |
| `GET /s/{token}/data` | unauthenticated | the token predicate is the entire gate |
| `GET /s/{token}/asset/{id}` | unauthenticated | the token predicate is the entire gate; snapshot-scoped |

<tasks>

<task type="auto">
  <name>Task 1: ShareService interface + server wiring</name>
  <read_first>
    - `internal/agui/asset_service.go` — **the whole file (27 LOC), the exact template**: the "narrow asset API surface consumed by AG-UI handlers" framing, per-method docs ONLY where a contract is non-obvious (who closes, what the gate is), and domain types from the owning package in the signature (agui does not re-declare them).
    - `internal/agui/server.go:104-124` — the `Server` struct (`assets AssetService` at ~`:108`); add `share ShareService` beside it.
    - `internal/agui/server.go:262-268` — the registration site: `s.registerAssetRoutes(mux)`, and `:263-268`'s comment shape for a new registration (what the routes are, where the parent-mux mount + gate live). Copy that comment shape.
    - `internal/share/service.go` — the real `Service` method signatures (plan 37F-08). The interface mirrors them; do not invent.
  </read_first>
  <action>
    Create `internal/agui/share_service.go` declaring a narrow `ShareService` interface covering exactly
    what the handlers call: create, list-for-identity, list-for-conversation, update-snapshot, revoke,
    resolve-by-token, **resolve-internal**, and open-a-bundled-artifact. Use `share.*` domain types in the
    signatures — `agui` does not re-declare them.

    **`ResolveInternal` is not optional and not decorative.** The interface claim "exactly what the
    handlers call" is only true if a handler calls it — Task 2 mounts `GET /api/shares/{id}/data` for
    precisely this. Mirror `share.Service.ResolveInternal`'s real signature from plan 37F-08
    (`(ctx, shareID, identityID) (share.Snapshot, share.Link, error)`); do not invent one.

    Per-method docs only where the contract is non-obvious: who closes the `io.ReadCloser` on the artifact
    open; that resolve-by-token's error is deliberately undifferentiated (no oracle); and that
    resolve-internal is **bearer-within-auth by design (D-10)** — the `identityID` argument is the
    *caller's* identity for auditing, **not** an owner filter, which is the opposite of every other
    identity-taking method on this interface. Say that explicitly: the argument's shape invites exactly
    the wrong assumption.

    Wire `server.go` at the two sites: add `share ShareService` to the struct, and
    `s.registerShareRoutes(mux)` after `s.registerAssetRoutes(mux)`, with a comment in
    `:263-268`'s shape naming the routes, the requirement (WEBSHARE-02), and — importantly — **where the
    parent-mux mounts and gates live** (`cmd/aura/serve_webui_share.go`, plan 37F-12), since they are not
    in this file. `server.go` is 506 LOC; the delta is ~2 LOC + the comment.
  </action>
  <verify>
    <automated>go build ./... && go vet ./internal/agui/ && grep -q "registerShareRoutes" internal/agui/server.go && grep -q "share.*ShareService" internal/agui/server.go && bash scripts/check-file-size.sh</automated>
  </verify>
  <acceptance_criteria>
    - `go build ./...` and `go vet ./internal/agui/` clean.
    - `internal/agui/share_service.go` declares `ShareService` using `share.*` types and re-declares no domain type: `grep -cE "^type (Snapshot|Link|Tier) " internal/agui/share_service.go` returns `0`.
    - **The interface declares `ResolveInternal`** and Task 2 calls it: `grep -q "ResolveInternal" internal/agui/share_service.go` and `grep -q "ResolveInternal" internal/agui/share_api_internal.go` both succeed. An interface method with no handler consumer is the defect this criterion exists to catch.
    - The `ResolveInternal` doc states its `identityID` is the caller's identity for audit, **not** an owner filter.
    - `grep -c "registerShareRoutes" internal/agui/server.go` returns `1`, placed after `registerAssetRoutes`.
    - The registration comment names where the parent-mux mount lives: `grep -q "serve_webui_share" internal/agui/server.go`.
    - `wc -l internal/agui/server.go` ≤ 600 (was 506).
    - `golangci-lint run ./internal/agui/` reports 0 issues.
  </acceptance_criteria>
  <done>`ShareService` is a narrow consumer-declared interface over `share.*` types, and `server.go` holds it and registers the share routes in ~2 LOC with a comment pointing at the parent-mux mount.</done>
</task>

<task type="auto">
  <name>Task 2: the eight routes across three files — owner CRUD, the D-10 internal bearer routes, the public token routes</name>
  <read_first>
    - `internal/agui/assets_api.go:13-22` — `registerAssetRoutes`: the Go 1.22 method+path registration form to mirror.
    - `internal/agui/assets_api.go:24-59` — `handleAssetDownload`: the stream-through template for `GET /s/{token}/asset/{id}` — nil-service 503, the four headers in order, `defer rc.Close()`, `io.Copy` scoped to `r.Context()`, `http.Error(w, sanitizeErr(err), 404)` on ANY error. **Its doc line "no unauthenticated surface" is FALSE for the `/s/` handlers — that line must be INVERTED, loudly** (see the action).
    - `internal/agui/assets_api.go:80` — `r.Body = http.MaxBytesReader(w, r.Body, maxRunBodyBytes)` (`maxRunBodyBytes = 1<<20`, `server.go:27`): the body cap for `POST /api/shares`.
    - `internal/agui/assets_api.go:201-204` — `principalIdentityID(r)`: reuse.
    - `internal/agui/auth.go:274-290` — `RequireCapability`, and **`:282` — `if !deps.SecretConfigured { return next }`**. Read this line. It is the reason the kill-switch is re-checked in-handler.
    - `internal/agui/conversations_api.go:149-155` — `writeJSON` / `writeJSONStatus`; `internal/agui/server_redact.go:41` — `sanitizeErr`. Reuse; never re-implement.
    - `internal/config/config_share.go` — `ShareConfig.PublicEnabled` (plan 37F-02): the kill-switch value the handler re-checks.
    - `internal/share/service.go` (plan 37F-08) — `ResolveInternal`'s real signature and its documented
      asymmetry (no owner gate, tier+liveness delegated to `ResolveLiveByID`, audits `open` with the
      caller's identity). The handler mirrors that contract; do not re-implement any of it.
    - `internal/db/migrations/0040_shared_links.up.sql` (plan 37F-02) — the `shared_links_tier_shape`
      CHECK: `(tier='public' AND token_hash IS NOT NULL AND expires_at IS NOT NULL) OR (tier='internal'
      AND token_hash IS NULL)`. **Read it.** It is the structural reason an internal share cannot be
      served over `/s/{token}` and therefore the reason `GET /api/shares/{id}/data` exists.
    - `.planning/phases/37F-conversation-artifact-sharing-export-inserted/37F-RESEARCH.md` §R-08 + §"Security test angles" + §"Open Questions" #4 (**resolved in the PRD by plan 37F-01**: the internal tier is an id-addressed authenticated route; the public tier keeps the hashed opaque token. Read the PRD's recorded rationale before questioning the route shape.)
  </read_first>
  <action>
    Create **three** files, package `agui`, **split by trust boundary rather than by line count**:
    `share_api.go` (owner-scoped CRUD), `share_api_internal.go` (the D-10 bearer routes), and
    `share_api_public.go` (the unauthenticated token routes). This split is mandatory, not
    LOC-conditional. The reason is not file size: `share_api.go`'s handlers 404 a non-owner **by design**
    while `share_api_internal.go`'s handlers serve a non-owner **by design**. Those are opposite rules,
    and a reader who greps one file and generalises to the package will get one of them wrong. Each
    file's header states which boundary it owns and which rule applies.

    `registerShareRoutes(mux)` registers **eight** routes:
    - owner-scoped: `POST /api/shares`, `GET /api/shares`, `PATCH /api/shares/{id}/snapshot`,
      `DELETE /api/shares/{id}`
    - **D-10 bearer-within-auth: `GET /api/shares/{id}/data`, `GET /api/shares/{id}/asset/{assetID}`**
    - unauthenticated: `GET /s/{token}/data`, `GET /s/{token}/asset/{id}`

    Note the Go 1.22 longest-pattern precedence: `GET /api/shares/{id}/data` and
    `GET /api/shares` coexist without ambiguity, and `DELETE /api/shares/{id}` does not shadow
    `GET /api/shares/{id}/data` because method+path patterns are matched whole.

    **`handleShareCreate`** — nil-service 503; `principalIdentityID` (401 if absent);
    `http.MaxBytesReader(w, r.Body, maxRunBodyBytes)`; decode the request.
    **The R-08 closure:** if the requested tier is public, re-check `PublicEnabled` **here, inside the
    handler** and 403 when off. Document why in a block comment that names `auth.go:282`: the mount-level
    `RequireCapability(share.public)` gate returns `next` unchanged when `!SecretConfigured`, so on
    loopback it does not exist; this in-handler check is the gate that survives. Two gates, both
    fail-closed. Then delegate to `share.Service.Create`. On `ErrShareNotFound` (foreign conversation) ⇒
    404. Respond 201 with the link **and the plaintext token, which appears here and nowhere else**.
    The internal tier requires **no** capability (D-02) — do not add one.

    **`handleShareList`** — owner-scoped list; supports the per-conversation filter the "Condiviso"
    section needs.
    **`handleShareUpdateSnapshot`** (PATCH) and **`handleShareRevoke`** (DELETE) — owner-scoped; ANY error
    ⇒ 404 (never 403) so a foreign share id is indistinguishable from a missing one. These are SC4 row 6.

    **`handleShareResolvePublic`** (`GET /s/{token}/data`) — extract the token from the path, resolve, and
    write the `Snapshot` JSON. **Any** failure — unknown, expired, revoked, malformed — returns the
    **same** 404 status and the **same** body. Document that distinguishing them is an oracle: "expired"
    confirms the token *was* valid. Do **not** early-return on a length/shape check before the DB probe:
    a structural fast-path on a route that otherwise does a DB read is a timing oracle. Audit `open` with
    no recipient PII.

    **`handleShareAssetPublic`** (`GET /s/{token}/asset/{id}`) — resolve the token first, then serve the
    asset **only if it belongs to that token's snapshot**; otherwise 404. This is SC4 row 9 — the row a
    naive implementation fails by authenticating the token and then fetching any asset id. Read only
    token-scoped `share/` blobs; **never** route through `assets.Service`. Headers exactly as
    `handleAssetDownload`: `application/octet-stream` + `nosniff` + `contentDisposition` +
    `Content-Length`, regardless of the artifact's real MIME (37A D-10).

    **`handleShareResolveInternal`** (`GET /api/shares/{id}/data`) — **the D-10 route, in
    `share_api_internal.go`.** This is the handler the whole internal tier hangs on: `share.Service`
    exposes `ResolveInternal` and nothing else calls it. Nil-service 503; `principalIdentityID(r)` (401
    if absent — `RequireAuth` should have guaranteed one, but do not assume the mount); parse the id;
    call `ShareService.ResolveInternal(ctx, shareID, principalIdentityID)`; write the `Snapshot` JSON.

    **No capability** (D-02: internal links need none) and — the part that reads as a bug — **no owner
    predicate**. D-10 is bearer-within-auth: any authenticated identity holding the link opens the
    already-redacted snapshot. The `identityID` you pass is the **caller's** identity, for the audit
    trail; it is **not** a filter. Document this against its neighbours: the owner-scoped handlers in
    `share_api.go` 404 a non-owner deliberately, and this one serves them deliberately. State that the
    gate is `RequireAuth` at the mount (plan 37F-12) plus the unguessable id, and that the redaction
    (D-08) is what bounds what a bearer sees. This is **SC4 row 3** — the one row whose expected answer
    is 200.

    **Any** failure ⇒ **404**: unknown id, malformed id, revoked, expired, **and a public-tier id**. The
    service (plan 37F-08) already returns one `ErrShareNotFound` for all of them because
    `ResolveLiveByID`'s SQL folds in `tier='internal'` + the liveness predicate — so map the sentinel to a
    single 404 writer and add **no** Go-side tier check of your own. Document why a public-tier id must
    not get its own status (403/409/400): that is a tier oracle, confirming the id names a real public
    share. Never 403 here.

    **`handleShareAssetInternal`** (`GET /api/shares/{id}/asset/{assetID}`) — also in
    `share_api_internal.go`. D-09: *"Internal (bearer-within-auth) shares resolve artifacts via the same
    snapshot."* Without this route the internal page renders an artifact list where every download 404s —
    the bearer is not the owner, so `/api/assets/{id}/download` refuses them, and `/s/{token}/asset/{id}`
    needs a token an internal share structurally does not have.

    Resolve the share via `ResolveInternal` **first**, then serve the asset **only if it belongs to that
    share's snapshot**; otherwise 404. This is the SC4-row-9 rule applied to the internal lane: holding a
    link authenticates *one snapshot*, not *any asset id*. Read only token-scoped `share/` blobs; **never**
    route through `assets.Service`. Headers exactly as `handleAssetDownload`: `application/octet-stream` +
    `nosniff` + `contentDisposition` + `Content-Length`, regardless of the artifact's real MIME (37A D-10)
    — a bearer is still a recipient, and the inert-bytes rule is not a public-tier special case.

    **The doc block for the two `/s/` handlers must INVERT `assets_api.go:24-34`'s line.** That template
    says *"It inherits RequireAuth whole-origin from the parent mux (no per-route auth wiring, no
    unauthenticated surface)"*. For `/s/{token}/*` that is **false** — these are the phase's ONLY
    unauthenticated handlers. Say so loudly and name the token predicate as the gate, or a future reader
    will assume `RequireAuth` covers them.

    All three files stay ≤600 LOC. If one still approaches the cap, split it further by concern in the
    SAME commit — never ship at the cap.
  </action>
  <verify>
    <automated>go build ./... && go vet ./internal/agui/ && golangci-lint run ./internal/agui/ && bash scripts/check-file-size.sh</automated>
  </verify>
  <acceptance_criteria>
    - `go build ./... && go vet ./internal/agui/` clean; `golangci-lint run ./internal/agui/` reports 0 issues.
    - **All eight routes are registered:** `grep -cE '"(GET|POST|PATCH|DELETE) /(api/shares|s/)' internal/agui/share_api*.go` returns `8`.
    - **The D-10 route exists and calls the service:** `grep -q '"GET /api/shares/{id}/data"' internal/agui/share_api*.go` and `grep -q "ResolveInternal" internal/agui/share_api_internal.go` both succeed. This is the criterion that fails if the internal tier is left unwired.
    - **The internal artifact route exists:** `grep -q '"GET /api/shares/{id}/asset/{assetID}"' internal/agui/share_api*.go` succeeds.
    - **The internal handlers carry NO owner predicate:** `handleShareResolveInternal` and `handleShareAssetInternal` contain no `GetForIdentity`/`ForIdentity` call and no comparison of the principal against the share's owner. A doc comment within 5 lines of each states the omission is D-10-intended.
    - **The internal handlers carry no Go-side tier check:** `grep -nE "Tier ==|tier ==|TierPublic" internal/agui/share_api_internal.go` returns NOTHING — tier is the store's SQL predicate (plan 37F-07), never a handler branch that could leak a distinct status.
    - **No 403 on the internal lane:** `grep -c "http.StatusForbidden" internal/agui/share_api_internal.go` returns `0` — a public-tier id, a revoked link, an expired link, and an unknown id all 404.
    - **The R-08 closure exists in-handler:** `grep -n "PublicEnabled\|publicEnabled" internal/agui/share_api.go` matches inside `handleShareCreate`, and a comment within ~5 lines names `auth.go:282` or `SecretConfigured`.
    - The internal tier is ungated: no capability check appears on the internal path, in any of the three files.
    - `grep -c "http.StatusForbidden" internal/agui/share_api*.go` — every occurrence is on the public-tier **mint** deny path; **zero** occur on an owner-scoped read/mutate (those 404) and zero on the internal lane.
    - No oracle: the unknown/expired/revoked paths all reach one shared 404 writer. `grep -ciE "expired|revoked" internal/agui/share_api*.go` — no match appears inside a `http.Error`/response-body string.
    - No structural early-return before the DB probe on the token path: `grep -nE "len\(token\) *[!=<>]" internal/agui/share_api*.go` returns NOTHING.
    - `grep -q "MaxBytesReader" internal/agui/share_api.go`.
    - **Neither asset handler touches the identity lane:** `grep -n "assets\.\|s.assets" internal/agui/share_api_public.go internal/agui/share_api_internal.go` returns NOTHING.
    - The `/s/` doc block states it IS an unauthenticated surface: `grep -ciE "unauthenticated" internal/agui/share_api_public.go` returns ≥1.
    - **Each file's header names its trust boundary:** `share_api.go` says owner-scoped/404-on-foreign, `share_api_internal.go` says bearer-within-auth/no-owner-predicate, `share_api_public.go` says unauthenticated.
    - Every file ≤600 LOC; `bash scripts/check-file-size.sh` exits 0.
  </acceptance_criteria>
  <done>All eight share routes are registered across three trust-boundary-named files; the D-10 internal routes resolve a bearer's snapshot and its artifacts with no capability and no owner predicate, 404ing a public-tier id like any unknown one; public minting is re-gated in-handler against the loopback bypass; owner-scoped reads/mutates 404 on foreign; and both token/bearer asset routes serve only their own snapshot's artifacts as neutral-typed attachments.</done>
</task>

<task type="auto">
  <name>Task 3: share API integration tests — tiers, kill-switch, oracle-freedom, token scoping</name>
  <read_first>
    - `internal/agui/auth_capability_integration_test.go` — the shape; its 403 subtest (`:91-116`) seeds a fresh **non-wildcard** identity and is the direct template for the capability rows
    - `internal/agui/server_integration_test.go` — the shared `envOrSkip`
    - `.planning/phases/37F-conversation-artifact-sharing-export-inserted/37F-VALIDATION.md` — the exact names: `TestShareInternalBearerWithinAuth`, `TestSharePublicMintWithCapability`, `TestSharePublicOrgKillSwitch`, `TestShareRevokeThen404`, `TestSharePublicOpenAuditNoPII`, `TestShareAuditLedger`
    - `.planning/phases/37F-conversation-artifact-sharing-export-inserted/37F-RESEARCH.md` §"Security test angles" — token enumeration, timing, 404-never-stale-render, content-type confusion, header injection, body cap, token-never-logged
  </read_first>
  <action>
    Create `internal/agui/share_api_test.go` with the **single** build tag `db_integration`,
    `objectstore.NewFake()`, and provisioned **non-wildcard** identities (R-13).

    Tests:
    - `TestShareInternalBearerWithinAuth` — A mints internal (no capability); B, **authenticated**,
      `GET /api/shares/{id}/data` ⇒ **200**. This is SC4 row 3 — bearer-within-auth is *intended*; the
      redaction is the protection. Resolve as **B, the non-owner**; resolving as A passes vacuously and
      proves nothing.
    - `TestShareInternalAnonymous401` — the same internal link, **no principal**, `GET
      /api/shares/{id}/data` ⇒ **401/302**, exercised through the real `RequireAuth` chain (a bare handler
      call bypasses the gate under test). This is SC4 row 4, and it is the assertion that fails if anyone
      "simplifies" the internal tier onto the `/s/` lane, which `isPublicShareRoute` admits
      unauthenticated.
    - `TestShareInternalRejectsPublicTierID` — a **public** share's id on `GET /api/shares/{id}/data` ⇒
      **404**, with a body byte-identical to an unknown id's. Not 403, not 409: a distinct status would
      confirm the id names a real public share.
    - `TestShareInternalRevokedExpired404` — a revoked internal link and an expired internal link both ⇒
      404 with the same body as an unknown id. Assert byte-equality across all three, as
      `TestShareTokenNoOracle` does for the token lane.
    - `TestShareInternalAssetSnapshotScoped` — B holds A's internal link and requests **another
      snapshot's** assetID under it ⇒ **404**. The SC4-row-9 rule on the internal lane: the link
      authenticates one snapshot, not any asset id.
    - `TestShareInternalAssetContentType` — an internal bundled artifact whose real MIME is `text/html`
      is served `application/octet-stream` + `nosniff`. The inert-bytes rule is not a public-tier special
      case.
    - `TestSharePublicMintWithCapability` — with `share.public` granted **and** the kill-switch on ⇒ 201,
      and the plaintext token is present in the body exactly once.
    - `TestSharePublicOrgKillSwitch` — kill-switch off ⇒ 403 **even with** the capability **and** with
      `SecretConfigured=false` (the loopback shape). This is the R-08 regression test; without it the
      loopback fail-open is untested. Assert 403, not 201.
    - `TestShareRevokeThen404` — revoke, then `GET /s/{token}/data` ⇒ 404 with **no snapshot bytes**:
      assert the body does not contain the conversation title and is short. "404 never a stale render."
    - `TestShareTokenNoOracle` — unknown, expired, and revoked tokens produce **identical** status AND
      identical body bytes. Assert byte-equality across the three responses.
    - `TestShareTokenEnumeration` — 1000 random 32-byte tokens all 404 with that same body.
    - `TestSharePublicOpenAuditNoPII` — after a public open, the `share_audit` row has an `open` action and
      contains **no** IP and **no** user-agent. Set a distinctive `X-Forwarded-For` and `User-Agent` on
      the request and assert neither string appears in any audit column.
    - `TestShareAuditLedger` — create/update/revoke each write the expected `share_audit` row.
    - `TestShareTokenNeverLogged` — capture `slog` output across a full mint→open→revoke cycle and assert
      the plaintext token appears in none of it; also assert it appears in no `share_audit` column (D-13).
    - `TestSharePublicAssetContentType` — a bundled artifact with a real MIME of `text/html` is served
      `application/octet-stream` + `nosniff` (content-type confusion).
    - `TestSharePublicAssetHeaderInjection` — an artifact filename of the shape
      `a"; rm -rf /` + CRLF + `X-Evil: 1` is percent-escaped by `contentDisposition`; assert no `X-Evil`
      header is present on the response.
    - `TestShareCreateBodyCap` — an over-cap body is rejected, not buffered.

    **Exactly one build tag.** Use `withPrincipal` to inject principals — no Authula.
  </action>
  <verify>
    <automated>go test -tags db_integration -race -p 1 -count=1 -run 'TestShare' ./internal/agui/ && go test ./internal/agui/ -count=1 && go test -tags db_integration -cover -p 1 -count=1 ./internal/agui/</automated>
  </verify>
  <acceptance_criteria>
    - `go test -tags db_integration -race -p 1 -count=1 -run 'TestShare' ./internal/agui/` passes with a **non-sub-second** runtime.
    - The untagged suite still passes.
    - `head -1 internal/agui/share_api_test.go` is exactly `//go:build db_integration`; `grep -rn "garage_integration\|authula_integration\|musr_e2e" internal/agui/share_api_test.go` returns NOTHING.
    - **`TestSharePublicOrgKillSwitch` exercises the `!SecretConfigured` (loopback) shape** and asserts 403 — the R-08 regression is real, not nominal.
    - `TestShareTokenNoOracle` asserts **byte-equality** of the response bodies across unknown/expired/revoked, not merely equal status codes.
    - `TestSharePublicOpenAuditNoPII` sets a distinctive `X-Forwarded-For` and `User-Agent` and asserts neither appears in any `share_audit` column.
    - `TestShareTokenNeverLogged` captures real `slog` output (not a mock) across mint→open→revoke.
    - `TestShareInternalBearerWithinAuth` asserts **200** (the one row where the answer is not 404) **and resolves as B, the non-owner** — an owner-resolving variant does not satisfy this criterion.
    - `TestShareInternalAnonymous401` exercises the **real `RequireAuth` chain**, not a bare handler call, and asserts 401/302 — proving the internal tier is not on the public allowlist (SC4 row 4).
    - `TestShareInternalRejectsPublicTierID` and `TestShareInternalRevokedExpired404` assert **byte-equality** of the 404 bodies against the unknown-id case — no tier or state oracle on the internal lane.
    - `TestShareInternalAssetSnapshotScoped` requests a foreign snapshot's assetID under a valid internal link and asserts 404.
    - `internal/agui` coverage under `db_integration` is ≥ 85%.
    - No test grants `*` to a seeded identity.
  </acceptance_criteria>
  <done>Internal bearer-within-auth (200 for a non-owner, 401 anonymous, 404 for a public-tier id, snapshot-scoped artifacts), capability-gated public minting, the loopback-surviving kill-switch, oracle-free 404s proven by byte-equality on both lanes, token enumeration, PII-free open audit, never-logged tokens, content-type confusion, header injection, and the body cap all pass live under one tag.</done>
</task>

</tasks>

<threat_model>
## Trust Boundaries

| Boundary | Description |
|----------|-------------|
| unauthenticated internet → `/s/{token}/*` | The phase's ONLY unauthenticated handlers. The token predicate is the entire gate; `RequireAuth` does not apply here. |
| any authenticated identity → another identity's internal share (`/api/shares/{id}/*`) | **A deliberate, bounded crossing (D-10).** `RequireAuth` is the gate and the unguessable id is the capability; the redacted snapshot (D-08) bounds the blast radius. This boundary is crossed *on purpose* — it is the only one in the phase where a non-owner read succeeds, which is exactly why it lives in its own file. |
| an internal share id → the wrong tier or a dead link | `ResolveLiveByID`'s SQL predicate (`tier='internal'` + liveness). A public id, a revoked link, and an expired link are the same miss — never a distinguishable status. |
| loopback dev → a link that leaves the box | `RequireCapability` vanishes when `!SecretConfigured`. The in-handler kill-switch is the only gate that crosses this boundary intact. |
| a valid token → the asset namespace | Holding a token authenticates *one snapshot*, not *any asset id*. |

## STRIDE Threat Register

| Threat ID | Category | Component | Disposition | Mitigation Plan |
|-----------|----------|-----------|-------------|-----------------|
| T-37F-03 | Elevation of Privilege | `RequireCapability` loopback pass-through (`auth.go:282`) | mitigate | The org kill-switch is re-checked INSIDE `handleShareCreate`, where no bypass applies; default `false`. `TestSharePublicOrgKillSwitch` runs the `!SecretConfigured` shape. |
| T-37F-50 | Information Disclosure | token oracle (expired ⇒ "it was valid once") | mitigate | Unknown/expired/revoked share one 404 writer; `TestShareTokenNoOracle` asserts byte-equality of the bodies. |
| T-37F-51 | Information Disclosure | timing oracle from a structural fast-path | mitigate | No length/shape early-return before the DB probe; grep-gated. The hash-indexed lookup itself is not an oracle (SHA-256 preimage resistance). |
| T-37F-52 | Elevation of Privilege | token authenticates, then any asset id is fetched (SC4 row 9) | mitigate | `handleShareAssetPublic` resolves the token first and serves only assets belonging to that snapshot; it never touches `assets.Service` (grep-gated). |
| T-37F-05 | Information Disclosure | public session leaking into the authenticated lane (SC4 row 10) | mitigate | The `/s/` handlers set no session and read only `share/` blobs; `/api/*` remains behind `RequireAuth` (plan 37F-12 asserts the allowlist is `/s/`-only). |
| T-37F-54 | Elevation of Privilege | the internal tier mounted under `/s/` or otherwise admitted by `isPublicShareRoute`, making an internal share anonymously resolvable | mitigate | The internal routes live under `/api/shares/`, which the `/s/`-prefix predicate cannot match and which the `"/api/"` carve-out already covers. `TestShareInternalAnonymous401` asserts 401/302 through the real `RequireAuth` chain; plan 37F-12 records the routes as explicit NON-members of `isPublicShareRoute` and `fallbackExcludedPrefixes`. |
| T-37F-55 | Information Disclosure | a tier oracle — a public-tier id answered with a distinct status on the internal route | mitigate | Tier lives in `ResolveLiveByID`'s SQL, so the handler has no tier branch; every miss maps to one 404 writer. `TestShareInternalRejectsPublicTierID` asserts byte-equality with the unknown-id body; `grep` gates against `StatusForbidden` and any Go-side tier comparison in `share_api_internal.go`. |
| T-37F-56 | Elevation of Privilege | an owner predicate added to the internal handlers, silently reducing D-10 to owner-only | mitigate | The handlers carry no `ForIdentity` call; each is documented as D-10-intended against its owner-scoped neighbours in `share_api.go`. `TestShareInternalBearerWithinAuth` resolves as a NON-owner and fails the moment a predicate appears. |
| T-37F-52b | Elevation of Privilege | an internal link authenticating, then any asset id being fetched under it | mitigate | `handleShareAssetInternal` resolves the share first and serves only assets belonging to that snapshot; it never touches `assets.Service`. `TestShareInternalAssetSnapshotScoped` asserts 404 for a foreign snapshot's assetID. |
| T-37F-01 | Information Disclosure | stale render after revoke | mitigate | `TestShareRevokeThen404` asserts the 404 body carries no snapshot bytes and omits the title. |
| T-37F-11 | Information Disclosure | plaintext token in logs or audit | mitigate | `TestShareTokenNeverLogged` captures real `slog` output across mint→open→revoke and asserts absence, plus absence from every `share_audit` column. |
| T-37F-39 | Information Disclosure | recipient PII in the open audit | mitigate | `TestSharePublicOpenAuditNoPII` sets a distinctive `X-Forwarded-For`/`User-Agent` and asserts neither is persisted. |
| T-37F-47 | Information Disclosure | stored XSS via a bundled artifact's real MIME | mitigate | `application/octet-stream` + `nosniff` regardless of `asset.MIMEType` (37A D-10); `TestSharePublicAssetContentType` uses a `text/html` artifact. |
| T-37F-14 | Spoofing | header injection via an artifact filename | mitigate | `contentDisposition` RFC-6266 + `url.PathEscape`; `TestSharePublicAssetHeaderInjection` asserts no injected header lands. |
| T-37F-12 | Denial of Service | unbounded `POST /api/shares` body | mitigate | `http.MaxBytesReader(w, r.Body, maxRunBodyBytes)`; `TestShareCreateBodyCap`. |
| T-37F-SC | Tampering | npm/pip/cargo installs | accept | Existing deps only. |
</threat_model>

<verification>
- `go build ./... && go vet ./internal/agui/`
- `go test ./internal/agui/ -count=1` (untagged)
- `go test -tags db_integration -race -p 1 -count=1 ./internal/agui/`
- `go test -tags db_integration -cover -p 1 ./internal/agui/` → ≥ 85%
- `golangci-lint run ./internal/agui/` → 0 issues
- `bash scripts/check-file-size.sh` → 0
</verification>

<success_criteria>
The share HTTP surface is live across all three trust boundaries, one file each. The **internal tier
actually works end-to-end**: `GET /api/shares/{id}/data` resolves a bearer's redacted snapshot with no
capability and no owner predicate (SC4 row 3 = 200), 401s an anonymous caller through the real
`RequireAuth` chain (SC4 row 4), 404s a public-tier id byte-identically to an unknown one, and
`GET /api/shares/{id}/asset/{assetID}` serves only that snapshot's artifacts. Internal minting needs no
capability; public minting is refused when the org kill-switch is off **even on loopback where the mount
gate vanishes**; the token routes are oracle-free (byte-identical 404s); a token reaches only its own
snapshot's artifacts; bundled bytes are served inert on both lanes; and the plaintext token exists only
in the create response — never a log, never an audit row.
</success_criteria>

<output>
Create `.planning/phases/37F-conversation-artifact-sharing-export-inserted/37F-10-SUMMARY.md` when done.
Record the `db_integration` runtime and the agui coverage number.
</output>
