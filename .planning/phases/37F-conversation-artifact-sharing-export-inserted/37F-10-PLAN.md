---
phase: 37F-conversation-artifact-sharing-export-inserted
plan: 10
type: execute
wave: 5
depends_on: ["37F-08"]
files_modified:
  - internal/agui/share_api.go
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
    - "GET /s/{token}/data resolves a live public link without any session"
    - "GET /s/{token}/asset/{id} streams only artifacts belonging to THAT token's snapshot"
    - "Unknown, expired, and revoked tokens all return an identical 404 status and body — no oracle"
    - "A public token grants zero access to /api/* — the identity lane stays closed to it"
  artifacts:
    - path: "internal/agui/share_api.go"
      provides: "share CRUD handlers + the public token handlers"
      min_lines: 150
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
  prohibitions:
    - "MUST NOT rely on the route-mount RequireCapability alone for the public tier — auth.go:282 returns next unchanged when !SecretConfigured, so on loopback the mount gate DOES NOT EXIST. The kill-switch MUST be re-checked inside the handler."
    - "MUST NOT distinguish unknown vs expired vs revoked in the status or the body — that is an oracle confirming a token WAS valid"
    - "MUST NOT resolve a share's asset through assets.Service — the public asset handler reads only token-scoped share/ blobs"
    - "MUST NOT serve a bundled artifact with its real MIME — application/octet-stream + nosniff regardless (37A D-10)"
    - "MUST NOT require a capability for the internal tier"
    - "MUST NOT log the plaintext token or put it in a share_audit row"
    - "MUST NOT accept an unbounded request body — cap with http.MaxBytesReader"
    - "MUST NOT return 403 on a foreign share id — owner-scoped reads/mutates 404"
    - "MUST NOT put more than one build tag on the test file"
---

<objective>
Expose the share lifecycle over HTTP: the owner CRUD surface and the two unauthenticated token routes.

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
`handleShareUpdateSnapshot`, `handleShareRevoke`, `handleShareResolvePublic`, `handleShareAssetPublic`,
and the routes `POST /api/shares`, `GET /api/shares`, `PATCH /api/shares/{id}/snapshot`,
`DELETE /api/shares/{id}`, `GET /s/{token}/data`, `GET /s/{token}/asset/{id}`.

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
    resolve-by-token, resolve-internal, and open-a-bundled-artifact. Use `share.*` domain types in the
    signatures — `agui` does not re-declare them.

    Per-method docs only where the contract is non-obvious: who closes the `io.ReadCloser` on the artifact
    open; that resolve-by-token's error is deliberately undifferentiated (no oracle); that resolve-internal
    is bearer-within-auth by design (D-10).

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
    - `grep -c "registerShareRoutes" internal/agui/server.go` returns `1`, placed after `registerAssetRoutes`.
    - The registration comment names where the parent-mux mount lives: `grep -q "serve_webui_share" internal/agui/server.go`.
    - `wc -l internal/agui/server.go` ≤ 600 (was 506).
    - `golangci-lint run ./internal/agui/` reports 0 issues.
  </acceptance_criteria>
  <done>`ShareService` is a narrow consumer-declared interface over `share.*` types, and `server.go` holds it and registers the share routes in ~2 LOC with a comment pointing at the parent-mux mount.</done>
</task>

<task type="auto">
  <name>Task 2: share_api.go — owner CRUD + the two public token handlers</name>
  <read_first>
    - `internal/agui/assets_api.go:13-22` — `registerAssetRoutes`: the Go 1.22 method+path registration form to mirror.
    - `internal/agui/assets_api.go:24-59` — `handleAssetDownload`: the stream-through template for `GET /s/{token}/asset/{id}` — nil-service 503, the four headers in order, `defer rc.Close()`, `io.Copy` scoped to `r.Context()`, `http.Error(w, sanitizeErr(err), 404)` on ANY error. **Its doc line "no unauthenticated surface" is FALSE for the `/s/` handlers — that line must be INVERTED, loudly** (see the action).
    - `internal/agui/assets_api.go:80` — `r.Body = http.MaxBytesReader(w, r.Body, maxRunBodyBytes)` (`maxRunBodyBytes = 1<<20`, `server.go:27`): the body cap for `POST /api/shares`.
    - `internal/agui/assets_api.go:201-204` — `principalIdentityID(r)`: reuse.
    - `internal/agui/auth.go:274-290` — `RequireCapability`, and **`:282` — `if !deps.SecretConfigured { return next }`**. Read this line. It is the reason the kill-switch is re-checked in-handler.
    - `internal/agui/conversations_api.go:149-155` — `writeJSON` / `writeJSONStatus`; `internal/agui/server_redact.go:41` — `sanitizeErr`. Reuse; never re-implement.
    - `internal/config/config_share.go` — `ShareConfig.PublicEnabled` (plan 37F-02): the kill-switch value the handler re-checks.
    - `.planning/phases/37F-conversation-artifact-sharing-export-inserted/37F-RESEARCH.md` §R-08 + §"Security test angles"
  </read_first>
  <action>
    Create `internal/agui/share_api.go`, package `agui`.

    `registerShareRoutes(mux)` registers: `POST /api/shares`, `GET /api/shares`,
    `PATCH /api/shares/{id}/snapshot`, `DELETE /api/shares/{id}`, `GET /s/{token}/data`,
    `GET /s/{token}/asset/{id}`.

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

    **The doc block for the two `/s/` handlers must INVERT `assets_api.go:24-34`'s line.** That template
    says *"It inherits RequireAuth whole-origin from the parent mux (no per-route auth wiring, no
    unauthenticated surface)"*. For `/s/{token}/*` that is **false** — these are the phase's ONLY
    unauthenticated handlers. Say so loudly and name the token predicate as the gate, or a future reader
    will assume `RequireAuth` covers them.

    Split into `share_api.go` + `share_api_public.go` if the file approaches 600 LOC — same commit.
  </action>
  <verify>
    <automated>go build ./... && go vet ./internal/agui/ && golangci-lint run ./internal/agui/ && bash scripts/check-file-size.sh</automated>
  </verify>
  <acceptance_criteria>
    - `go build ./... && go vet ./internal/agui/` clean; `golangci-lint run ./internal/agui/` reports 0 issues.
    - **The R-08 closure exists in-handler:** `grep -n "PublicEnabled\|publicEnabled" internal/agui/share_api.go` matches inside `handleShareCreate`, and a comment within ~5 lines names `auth.go:282` or `SecretConfigured`.
    - The internal tier is ungated: no capability check appears on the internal path.
    - `grep -c "http.StatusForbidden" internal/agui/share_api.go` — every occurrence is on the public-tier deny path; **zero** occur on an owner-scoped read/mutate (those 404).
    - No oracle: the unknown/expired/revoked paths all reach one shared 404 writer. `grep -ciE "expired|revoked" internal/agui/share_api.go` — no match appears inside a `http.Error`/response-body string.
    - No structural early-return before the DB probe on the token path: `grep -nE "len\(token\) *[!=<>]" internal/agui/share_api.go` returns NOTHING.
    - `grep -q "MaxBytesReader" internal/agui/share_api.go`.
    - The public asset handler never touches the identity lane: `grep -n "assets\.\|s.assets" internal/agui/share_api.go` returns NOTHING.
    - The `/s/` doc block states it IS an unauthenticated surface: `grep -ciE "unauthenticated" internal/agui/share_api.go` returns ≥1.
    - Every file ≤600 LOC; `bash scripts/check-file-size.sh` exits 0.
  </acceptance_criteria>
  <done>The six share routes are registered; public minting is re-gated in-handler against the loopback bypass; owner-scoped reads/mutates 404 on foreign; the token routes return one indistinguishable 404 for unknown/expired/revoked and serve artifacts only from their own snapshot as neutral-typed attachments.</done>
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
      resolves it ⇒ 200. This is SC4 row 3 — bearer-within-auth is *intended*; the redaction is the
      protection.
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
    - `TestShareInternalBearerWithinAuth` asserts **200** (this is the one row where the answer is not 404).
    - `internal/agui` coverage under `db_integration` is ≥ 85%.
    - No test grants `*` to a seeded identity.
  </acceptance_criteria>
  <done>Internal bearer-within-auth, capability-gated public minting, the loopback-surviving kill-switch, oracle-free 404s proven by byte-equality, token enumeration, PII-free open audit, never-logged tokens, content-type confusion, header injection, and the body cap all pass live under one tag.</done>
</task>

</tasks>

<threat_model>
## Trust Boundaries

| Boundary | Description |
|----------|-------------|
| unauthenticated internet → `/s/{token}/*` | The phase's ONLY unauthenticated handlers. The token predicate is the entire gate; `RequireAuth` does not apply here. |
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
The share HTTP surface is live: internal minting needs no capability, public minting is refused when the
org kill-switch is off **even on loopback where the mount gate vanishes**, the token routes are
oracle-free (byte-identical 404s), a token reaches only its own snapshot's artifacts, bundled bytes are
served inert, and the plaintext token exists only in the create response — never a log, never an audit row.
</success_criteria>

<output>
Create `.planning/phases/37F-conversation-artifact-sharing-export-inserted/37F-10-SUMMARY.md` when done.
Record the `db_integration` runtime and the agui coverage number.
</output>
