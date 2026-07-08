---
phase: 37A-web-artifact-delivery-lane
plan: 03
type: execute
wave: 2
depends_on: ["37A-01"]
files_modified:
  - internal/agui/assets_api.go
  - internal/agui/asset_download_test.go
autonomous: true
requirements: [WEBART-03]

must_haves:
  truths:
    - "GET /api/assets/{id}/download streams the owner's asset body from Garage with Content-Disposition: attachment and Content-Type: application/octet-stream"
    - "A non-owner (or non-existent id) request returns 404 — existence-hidden, never 403 and never 200"
    - "An unauthenticated request is rejected (401/302) — the route inherits RequireAuth whole-origin, no unauthenticated download surface is added"
    - "A client disconnect cancels the Garage read (request-ctx-scoped io.Copy) with no goroutine leak"
    - "The served Content-Type is the neutral octet-stream regardless of the sniffed mime_type (stored-XSS guard); the sniffed mime is NEVER trusted as the serve header"
  artifacts:
    - path: "internal/agui/assets_api.go"
      provides: "handleAssetDownload + the GET /api/assets/{id}/download route registration"
      contains: "handleAssetDownload"
  key_links:
    - from: "registerAssetRoutes"
      to: "GET /api/assets/{id}/download → handleAssetDownload"
      via: "mux.HandleFunc inside the existing registerAssetRoutes block"
      pattern: "/api/assets/\\{id\\}/download"
    - from: "handleAssetDownload"
      to: "assets.OpenForIdentity (ownership → stream)"
      via: "principalIdentityID → OpenForIdentity → 404 on any error → io.Copy"
      pattern: "OpenForIdentity"
    - from: "handleAssetDownload headers"
      to: "contentDisposition helper"
      via: "Content-Disposition set from contentDisposition(asset.FileName)"
      pattern: "contentDisposition\\(asset.FileName\\)"
  prohibitions:
    - "A non-owner request MUST NOT return 200 or 403 — only 404 (T-IDOR existence-hiding, D-12)"
    - "The serve Content-Type MUST NOT be the sniffed asset.MIMEType — it MUST be application/octet-stream (T-XSS, D-10)"
    - "The route MUST NOT presign / redirect to a store URL — stream-through only (D-09, store-URL-leak)"
    - "No unauthenticated download route/handler is registered outside RequireAuth's whole-origin gate"
    - "No new external package is installed (go.mod/go.sum byte-unchanged)"
---

<objective>
Add the WEBART-03 authenticated streaming download: `GET /api/assets/{id}/download` inside `registerAssetRoutes` (inheriting `RequireAuth` whole-origin), enforcing identity ownership via 37A-01's `OpenForIdentity` (404 on miss/non-owner), forcing the stored-XSS-safe headers, encoding the filename with 37A-01's `contentDisposition` helper, and streaming the Garage body with a request-ctx-scoped `io.Copy` so a disconnect cancels the read.

Purpose: this is the same-origin, auth-gated delivery surface the web download button (37A-04) targets — the security keystone of the phase (IDOR 404, XSS force-attachment, CRLF-safe filename, DoS-safe stream, no store-URL leak). It has NO file overlap with 37A-02, so it runs in parallel in Wave 2 on top of 37A-01's `OpenForIdentity` + `contentDisposition`.

Output: `handleAssetDownload` + the route + the security/behavior test suite (owner-200, non-owner-404, unauth-401, client-disconnect + goleak, header assertions).

## Research corrections honored (do not regress)
- **404 not 403 (D-12):** a not-found OR not-owned request returns 404 (existence-hiding, OWASP IDOR). The non-owner→404 regression test is the mitigation proof and is mandatory.
- **Force headers regardless of sniffed MIME (D-10):** `Content-Type: application/octet-stream` + `X-Content-Type-Options: nosniff` + `Content-Disposition: attachment` — the content-sniffed `mime_type` rides the SSE event only (for the card icon), NEVER the serve header.
- **`Content-Length` from `asset.SizeBytes` (Open Q2 resolved):** authoritative post-`MarkAccepted` stored size; simpler than chunked; the ingest Put→MarkUploaded→MarkAccepted sequence rules out a truncated stored object (Landmine 8).
- **Route specificity:** Go 1.22+ `ServeMux` matches `/{id}/download` over `/{id}` regardless of registration order — no precedence issue.
</objective>

<execution_context>
@.claude/get-shit-done/workflows/execute-plan.md
@.claude/get-shit-done/templates/summary.md
</execution_context>

<context>
@.planning/phases/37A-web-artifact-delivery-lane/37A-CONTEXT.md
@.planning/phases/37A-web-artifact-delivery-lane/37A-RESEARCH.md
@.planning/phases/37A-web-artifact-delivery-lane/37A-PATTERNS.md
@.planning/phases/37A-web-artifact-delivery-lane/37A-01-SUMMARY.md
</context>

<tasks>

<task type="auto" tdd="true">
  <name>Task 1: handleAssetDownload + route registration + forced headers + ctx-scoped stream</name>
  <files>internal/agui/assets_api.go</files>
  <read_first>
    - internal/agui/assets_api.go (`registerAssetRoutes` :11-19 — add the one route line inside the block; `handleAssetGet` :103-119 — the exact principal-read → GetForIdentity → 404-on-miss template to diverge from; `principalIdentityID` :161-164; `sanitizeErr` for the error body; the `s.assets == nil` 503 guard)
    - internal/agui/asset_service.go (the AssetService interface with `OpenForIdentity` added by 37A-01 — `s.assets.OpenForIdentity` is the call)
    - internal/agui/content_disposition.go (the `contentDisposition` helper from 37A-01 — the Content-Disposition value)
    - internal/agui/auth.go (`RequireAuth` whole-origin :183 — inherited, no per-route wiring)
    - internal/assets/service.go (`OpenForIdentity` signature + `Asset.FileName`/`Asset.SizeBytes` fields from 37A-01)
    - 37A-PATTERNS.md §6 (handler + route + auth) + 37A-RESEARCH.md Gap D + §Security Domain
  </read_first>
  <action>
    Add `handleAssetDownload(w http.ResponseWriter, r *http.Request)` to `internal/agui/assets_api.go`, modeled on `handleAssetGet`: nil-`s.assets` → 503; `principalIdentityID(r)` → 401 if `!ok`; `rc, asset, err := s.assets.OpenForIdentity(r.Context(), r.PathValue("id"), identityID)` → `http.Error(w, sanitizeErr(err), http.StatusNotFound)` on ANY error (D-12 existence-hiding); `defer rc.Close()`; set headers BEFORE the first write — `Content-Type: application/octet-stream` (D-10), `X-Content-Type-Options: nosniff`, `Content-Disposition: contentDisposition(asset.FileName)` (D-11), `Content-Length: strconv.FormatInt(asset.SizeBytes, 10)` (Open Q2); then `io.Copy(w, rc)` (the `r.Context()` already scopes the OpenForIdentity read → a disconnect cancels it, D-09). Register `mux.HandleFunc("GET /api/assets/{id}/download", s.handleAssetDownload)` inside the existing `registerAssetRoutes` block. Do NOT add any auth wiring (RequireAuth is inherited whole-origin). Do NOT presign.
  </action>
  <behavior>
    - owner request → 200; body bytes == the stored object; `Content-Disposition` starts with `attachment;`; `Content-Type == application/octet-stream`; `X-Content-Type-Options == nosniff`; `Content-Length == asset.SizeBytes`
    - a stored asset whose sniffed `mime_type` is `text/html`/`image/svg+xml` STILL serves `application/octet-stream` (the sniffed mime is not trusted)
    - the route is registered inside registerAssetRoutes (inherits RequireAuth)
  </behavior>
  <acceptance_criteria>
    - `internal/agui/assets_api.go` contains `func (s *Server) handleAssetDownload` and `registerAssetRoutes` contains `GET /api/assets/{id}/download`
    - the handler calls `OpenForIdentity` and returns `http.StatusNotFound` on its error (grep: no `StatusForbidden` in this handler)
    - the handler sets `Content-Type` to `application/octet-stream` (grep: it does NOT set Content-Type from `asset.MIMEType`)
    - `go build ./... && go vet ./internal/agui/` clean
  </acceptance_criteria>
  <verify>
    <automated>go build ./... &amp;&amp; go vet ./internal/agui/</automated>
  </verify>
  <done>The download route streams the owner's Garage body with forced attachment + octet-stream + nosniff + RFC-6266 filename + Content-Length, 404s on OpenForIdentity error, and inherits RequireAuth — no presign, no unauthenticated surface.</done>
</task>

<task type="auto" tdd="true">
  <name>Task 2: Download security + behavior test suite (owner-200 / non-owner-404 / unauth / client-disconnect + goleak)</name>
  <files>internal/agui/asset_download_test.go</files>
  <read_first>
    - internal/agui/assets_api_test.go (`fakeAssetService` :125-197 with the `OpenForIdentity` method + `openResp`/`openAsset`/`openErr` fields added by 37A-01; `SetAssetService`; the httptest setup + `principalFrom`/session header seam the existing asset tests use)
    - internal/agui/auth_blank_principal_test.go or the agui auth test helpers (how existing tests drive RequireAuth / an authenticated vs unauthenticated request)
    - internal/objectstore/fake.go (in-memory Store to back the fake's returned ReadCloser)
    - .claude/skills/golang-testing/SKILL.md (httptest + goleak) + .claude/skills/golang-concurrency/SKILL.md (goleak on the disconnect test)
    - 37A-PATTERNS.md §"Test Analogs" (asset_download_test row) + 37A-VALIDATION.md WEBART-03 rows (T-IDOR/T-XSS/T-DoS)
  </read_first>
  <action>
    Create `internal/agui/asset_download_test.go` (httptest + the `fakeAssetService`, daemon-free). Cases: (1) `TestAssetDownload_Owner` — configure the fake to return a ReadCloser over known bytes + an asset with a unicode FileName + SizeBytes; assert 200, body == bytes, `Content-Disposition` starts `attachment;` and carries `filename*=UTF-8''`, `Content-Type == application/octet-stream`, `X-Content-Type-Options == nosniff`, `Content-Length` matches. (2) `TestAssetDownload_NonOwner` — fake `OpenForIdentity` returns an error (as it would for a non-owned id) → assert 404 (NOT 403/200). (3) `TestAssetDownload_Unauth` — no principal on the request → 401 (or the RequireAuth 302/401 the agui gate emits). (4) `TestAssetDownload_ClientDisconnect` — a ReadCloser that blocks; cancel the request ctx mid-stream; assert `io.Copy` returns and `rc.Close()` ran; wrap with `defer goleak.VerifyNone(t)` (no goroutine leak). Mark independent cases `t.Parallel()` where safe.
  </action>
  <behavior>
    - owner → 200 + attachment/octet-stream/nosniff/Content-Length + body==bytes + filename*=UTF-8''
    - non-owner (OpenForIdentity error) → 404, no body leak of existence
    - unauthenticated → 401/302, handler body never streamed
    - client disconnect → io.Copy unblocks, rc closed, no leaked goroutine (goleak clean)
  </behavior>
  <acceptance_criteria>
    - `go test -race ./internal/agui/ -run 'TestAssetDownload_Owner|TestAssetDownload_NonOwner|TestAssetDownload_Unauth|TestAssetDownload_ClientDisconnect'` exits 0
    - the non-owner test asserts `http.StatusNotFound` (grep the test for `StatusNotFound` and the absence of `StatusForbidden`)
    - the disconnect test uses `goleak` (grep for `goleak`)
    - the owner test asserts `application/octet-stream` and a `filename*=UTF-8''` substring
    - daemon-free (no `//go:build garage_integration` tag on this file — it counts toward the 85% floor)
  </acceptance_criteria>
  <verify>
    <automated>go test -race ./internal/agui/ -run 'TestAssetDownload'</automated>
  </verify>
  <done>The four security/behavior cases pass daemon-free: owner-200 with forced headers, non-owner-404 (IDOR proof), unauth-rejected, and a goleak-clean ctx-cancel disconnect — all counting toward the coverage floor.</done>
</task>

</tasks>

<threat_model>
## Trust Boundaries

| Boundary | Description |
|----------|-------------|
| browser → download route | untrusted `{id}` path param + session principal cross into an owner-scoped object read |
| stored asset bytes/filename → HTTP response | attacker-influenced content + filename become the response body + Content-Disposition header |

## STRIDE Threat Register

| Threat ID | Category | Component | Disposition | Mitigation Plan |
|-----------|----------|-----------|-------------|-----------------|
| T-IDOR | Information Disclosure | ownership on `{id}` | mitigate | `OpenForIdentity` (`GetForIdentity` WHERE id AND identity_id) → 404 on miss/non-owner (D-12, existence-hiding); v4/v7 UUID ids unguessable. Proof: `TestAssetDownload_NonOwner` asserts 404 (Task 2) |
| T-XSS | Tampering / Elevation | served Content-Type/Disposition | mitigate | Force `Content-Type: application/octet-stream` + `Content-Disposition: attachment` + `X-Content-Type-Options: nosniff` regardless of sniffed MIME (D-10). Proof: owner test asserts octet-stream even for an html/svg sniffed asset (Task 2) |
| T-HdrInj | Tampering | filename in Content-Disposition | mitigate | `contentDisposition(asset.FileName)` (37A-01 helper, property-tested) strips CR/LF/`"`/`\` + percent-encodes; the owner test asserts a well-formed `filename*=UTF-8''` |
| T-DoS | Denial of Service | unbounded stream / goroutine leak | mitigate | request-ctx-scoped `io.Copy` (D-09) + `defer rc.Close()`; `TestAssetDownload_ClientDisconnect` + goleak prove cancel-on-disconnect (Task 2) |
| T-StoreLeak | Information Disclosure | delivery strategy | mitigate | Stream-through, NOT presign (D-09) — the private per-identity bucket URL never reaches the client; `OpenForIdentity` returns a ReadCloser, not a URL |
| T-Unauth | Spoofing | route registration | mitigate | Registered inside `registerAssetRoutes` → inherits `RequireAuth` whole-origin (`auth.go:183`); `TestAssetDownload_Unauth` proves rejection. No public path added |
| T-37A-03-SC | Tampering | package installs | accept | Zero installs (Go stdlib `io`/`net/http`/`strconv` + vendored goleak). go.mod/go.sum byte-unchanged. No `[ASSUMED]`/`[SUS]`/`[SLOP]` package — no legitimacy checkpoint |
</threat_model>

<verification>
- `go build ./... && go vet ./internal/agui/` + `gofmt` clean.
- `go test -race ./internal/agui/ -run 'TestAssetDownload'` green (daemon-free — counts toward the coverage floor).
- Grep guards: the handler 404s (no `StatusForbidden`); serves `application/octet-stream` (never `asset.MIMEType`); no presign/redirect; the route lives inside `registerAssetRoutes`.
- go.mod/go.sum byte-unchanged.
</verification>

<success_criteria>
`GET /api/assets/{id}/download` streams the owner's Garage object with attachment + octet-stream + nosniff + RFC-6266 filename + Content-Length; a non-owner → 404 (proven); unauth → rejected; a disconnect cancels the read leak-free (proven). No presign, no unauthenticated surface. The web button (37A-04) has its target.
</success_criteria>

<output>
Create `.planning/phases/37A-web-artifact-delivery-lane/37A-03-SUMMARY.md` when done.
</output>
