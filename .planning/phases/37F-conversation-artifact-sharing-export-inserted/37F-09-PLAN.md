---
phase: 37F-conversation-artifact-sharing-export-inserted
plan: 09
type: execute
wave: 4
depends_on: ["37F-06"]
files_modified:
  - internal/agui/share_export.go
  - internal/agui/conversations_api.go
  - internal/agui/share_export_test.go
autonomous: true
requirements: [WEBSHARE-01]

must_haves:
  truths:
    - "The owner can download a conversation as Markdown via GET /api/conversations/{id}/export?format=md with Content-Disposition: attachment"
    - "The owner can download the same conversation as JSON, and it round-trips as a valid Snapshot"
    - "Exporting a conversation owned by another identity returns 404 — never 403, never 200"
    - "The export route inherits RequireAuth from the already-mounted conversations subtree with ZERO cmd/aura/serve_webui.go changes"
    - "MD and JSON both derive from the same BuildSnapshot call — no second serializer"
    - "An unknown or absent format falls back to a documented default rather than 500-ing"
  artifacts:
    - path: "internal/agui/share_export.go"
      provides: "handleConversationExport — the WEBSHARE-01 identity-scoped export endpoint"
      min_lines: 50
    - path: "internal/agui/conversations_api.go"
      provides: "the export route registered in the existing conversations subtree"
      contains: "export"
  key_links:
    - from: "internal/agui/conversations_api.go"
      to: "internal/agui/share_export.go"
      via: "mux.HandleFunc(\"GET /api/conversations/{id}/export\", s.handleConversationExport)"
      pattern: "conversations/\\{id\\}/export"
    - from: "internal/agui/share_export.go"
      to: "internal/share.BuildSnapshot"
      via: "one snapshot, two format adapters"
      pattern: "BuildSnapshot|Markdown\\(\\)|JSON\\(\\)"
  prohibitions:
    - "MUST NOT add a route mount, a const, or any line to cmd/aura/serve_webui.go — F-1: conversationsRoutePrefix is ALREADY mounted at serve_webui.go:381 and the export path falls inside it, inheriting RequireAuth free. That file is at 593/600."
    - "MUST NOT add /api/conversations/{id}/export to any public-path allowlist — export is identity-scoped, always authenticated"
    - "MUST NOT call LoadHistory before GetForIdentity — LoadHistory is unscoped; the owner gate must precede it"
    - "MUST NOT return 403 on a foreign conversation — reads hide existence (36 D-06)"
    - "MUST NOT concat a filename into Content-Disposition — reuse the RFC-6266 helper"
    - "MUST NOT write a second serializer — both formats are adapters over one Snapshot"
    - "MUST NOT trust or echo any client-supplied filename or MIME"
---

<objective>
Ship WEBSHARE-01: `GET /api/conversations/{id}/export?format=md|json`.

This is the phase's simplest requirement and its cheapest win, because of **F-1** — a finding PATTERNS
surfaced that RESEARCH missed. `cmd/aura/serve_webui.go:381` already mounts the whole
`/api/conversations/` subtree (`mux.Handle(conversationsRoutePrefix, aguiHandler)`), so the export path
falls **inside** it and inherits `RequireAuth` automatically. It needs **no const, no mount, and no
`serve_webui.go` edit** — only one `mux.HandleFunc` line in `registerConversationRoutes`. RESEARCH's
R-01 over-estimated the delta on a file with 7 LOC of headroom; the real delta there is **zero**.

Export is deliberately not gated by a capability: open-webui's `USER_PERMISSIONS_CHAT_EXPORT` defaults
`True` while public sharing defaults `False`, which is exactly D-01's split — (a) export always
available, (c) public sharing opt-in.

Purpose: the owner can get their conversation out, redacted, in both formats.
Output: `internal/agui/share_export.go` + one route line.
</objective>

<execution_context>
@/home/user/Aura/.claude/get-shit-done/workflows/execute-plan.md
@/home/user/Aura/.claude/get-shit-done/templates/summary.md
</execution_context>

<context>
@.planning/phases/37F-conversation-artifact-sharing-export-inserted/37F-RESEARCH.md
@.planning/phases/37F-conversation-artifact-sharing-export-inserted/37F-PATTERNS.md
@.planning/phases/37F-conversation-artifact-sharing-export-inserted/37F-VALIDATION.md
@internal/share/snapshot.go
@CLAUDE.md
</context>

## Artifacts this plan produces

`agui.handleConversationExport`, the route `GET /api/conversations/{id}/export`.

<tasks>

<task type="auto">
  <name>Task 1: handleConversationExport — one snapshot, two formats, owner-gated</name>
  <read_first>
    - **VERIFY F-1 YOURSELF FIRST:** `grep -n "conversationsRoutePrefix" cmd/aura/serve_webui.go`. At plan time this showed the const at `:124` (`= "/api/conversations/"`) and the mount at `:381` (`mux.Handle(conversationsRoutePrefix, aguiHandler)`). If that mount is gone, STOP and re-plan — the whole zero-delta premise depends on it.
    - `internal/agui/assets_api.go:24-59` — `handleAssetDownload`: **the stream-through template**. Copy the nil-service 503 guard, the four headers in that exact order (`Content-Type: application/octet-stream`, `X-Content-Type-Options: nosniff`, `Content-Disposition`, `Content-Length`), and `http.Error(w, sanitizeErr(err), 404)` on ANY error. Its doc block (`:24-34`) is the security-invariant template — reuse its shape with the tier substituted.
    - `internal/agui/conversations_api.go:48-62` — `registerConversationRoutes`: the Go 1.22 `mux.HandleFunc("METHOD /path", handler)` form and the existing `/api/conversations/{id}/…` siblings. Your route joins this list.
    - `internal/agui/content_disposition.go:23-40` — `contentDisposition(filename)`: RFC-6266 `filename` + `filename*=UTF-8''` with a `url.PathEscape` header-injection guard and a diacritic ASCII fallback. **Already tested. Never concat a filename.**
    - `internal/agui/assets_api.go:201-204` — `principalIdentityID(r)`: reuse, do not re-derive.
    - `internal/conversations/store_identity.go:28` + `internal/conversations/store.go:260` — `GetForIdentity` (the owner gate) and `LoadHistory` (**unscoped** — must be called only after the gate).
    - `internal/share/snapshot.go` + `markdown.go` + `jsonfmt.go` — `BuildSnapshot`, `Markdown()`, `JSON()` (plans 37F-03/06). Read the real signatures.
    - `internal/agui/server.go:104-124` — the `Server` struct; you need the conversation store seam it already holds.
  </read_first>
  <action>
    Create `internal/agui/share_export.go`, package `agui`.

    `handleConversationExport(w http.ResponseWriter, r *http.Request)`:
    1. Nil-service guard → 503, mirroring `handleAssetDownload`'s opening.
    2. `principalIdentityID(r)`; missing ⇒ 401.
    3. **Owner gate:** `GetForIdentity(r.Context(), r.PathValue("id"), identityID)`. **Any** error ⇒
       `http.Error(w, sanitizeErr(err), http.StatusNotFound)`. Never 403 — reads hide foreign existence
       (36 D-06). This is SC4 row 1.
    4. Only then `LoadHistory` + list the thread's artifacts, filter via `share.BundleFilter`.
    5. `share.BuildSnapshot(...)` **once**.
    6. Switch on `format`: `md` ⇒ `Snapshot.Markdown()`; `json` ⇒ `Snapshot.JSON()`. **Absent or
       unrecognized ⇒ default to `md`**, and document the choice (a human clicking "export" wants the
       readable one; a 400 on a missing optional query param is hostile). Do not 500.
    7. Headers, in `handleAssetDownload`'s order: `Content-Type: application/octet-stream`;
       `X-Content-Type-Options: nosniff`; `Content-Disposition: contentDisposition(<name>)`;
       `Content-Length`.
       **Serve `application/octet-stream` for BOTH formats** — not `text/markdown`, not
       `application/json`. Document why: it is 37A D-10's stored-XSS guard, and the exported bytes contain
       user-authored text. The neutral type + `nosniff` is what makes a downloaded file inert. The client
       asked for a download, not a rendered document.
    8. Build the filename from the conversation title, slugified, plus the extension. The title is
       user-authored, so it rides through `contentDisposition`'s escaping — never concatenated.

    Doc block, modelled on `assets_api.go:24-34` with the tier substituted. State the invariants:
    existence-hiding (the owner gate precedes any history read, and ANY error collapses to 404);
    the octet-stream + `nosniff` guard; the header-injection guard via `contentDisposition`; and — the one
    line to state explicitly — **it inherits `RequireAuth` whole-origin from the parent mux's
    `conversationsRoutePrefix` mount (`serve_webui.go:381`), so there is no per-route auth wiring and no
    unauthenticated surface here.** (Contrast this with the public share handler in plan 37F-10, whose doc
    must say the *opposite*, loudly.)

    Register it in `registerConversationRoutes` (`conversations_api.go:48`) with **one** line:
    `mux.HandleFunc("GET /api/conversations/{id}/export", s.handleConversationExport)`. Add a brief
    comment naming the requirement (WEBSHARE-01) and the fact that it inherits the subtree's auth.

    **Do not touch `cmd/aura/serve_webui.go`.** (F-1.)

    Refactor-on-touch on `conversations_api.go` (406 LOC): dead code, dupl, ≤600, comments updated — same
    commit.
  </action>
  <verify>
    <automated>go build ./... && go vet ./internal/agui/ && golangci-lint run ./internal/agui/ && git diff --name-only | grep -q "cmd/aura/serve_webui.go" && { echo "FAIL: F-1 violated — serve_webui.go must not change"; exit 1; } || echo "F-1 OK: serve_webui.go untouched"</automated>
  </verify>
  <acceptance_criteria>
    - **F-1 holds:** `git diff --name-only` does NOT list `cmd/aura/serve_webui.go`. Zero delta on the 593/600 file.
    - `grep -c "conversations/{id}/export" internal/agui/conversations_api.go` returns `1` — exactly one route line.
    - **Owner gate precedes the history read:** in `handleConversationExport`, the `GetForIdentity` call is at a lower line number than any `LoadHistory` call.
    - `grep -c "http.StatusForbidden\|403" internal/agui/share_export.go` returns `0` — reads never 403.
    - The four headers are set in `handleAssetDownload`'s order, and the Content-Type is `application/octet-stream` for both formats: `grep -c "text/markdown\|application/json" internal/agui/share_export.go` returns `0`.
    - `grep -q "contentDisposition(" internal/agui/share_export.go` and no manual `Content-Disposition` string concatenation exists.
    - `grep -c "BuildSnapshot" internal/agui/share_export.go` returns `1` — one snapshot, two adapters.
    - The doc block explicitly states the route inherits `RequireAuth` from the parent-mux conversations mount.
    - `golangci-lint run ./internal/agui/` reports 0 issues; `conversations_api.go` and `share_export.go` are each ≤600 LOC.
  </acceptance_criteria>
  <done>`GET /api/conversations/{id}/export` is registered on the existing conversations subtree with zero `serve_webui.go` delta, gates ownership before reading history, 404s on foreign, and serves both formats from one `BuildSnapshot` as a neutral-typed attachment.</done>
</task>

<task type="auto">
  <name>Task 2: export integration tests — MD, JSON, foreign 404, format fallback</name>
  <read_first>
    - `internal/agui/auth_capability_integration_test.go` — the coverage-gate-safe shape: single `//go:build db_integration` tag, the env + run-command + no-skip-as-green header, `migratedPool(t)`, `withPrincipal(httptest.NewRequest(...), identityID)` to inject a principal with **no cookie and no Authula**, `t.Cleanup` row deletion, fresh non-wildcard identity seeded with `name = "..."+t.Name()`
    - `internal/agui/conversations_api_test.go` — the in-package conversation-route test precedent; reuse its seeding/harness helpers rather than duplicating them
    - `internal/agui/server_integration_test.go` — the shared `envOrSkip` (it `t.Fatal`s under `$CI` when the DSN is unset). Use it; do not write a new one.
    - `.planning/phases/37F-conversation-artifact-sharing-export-inserted/37F-VALIDATION.md` — the exact names: `TestShareExportMarkdown`, `TestShareExportJSON`, `TestShareExportForeignConversation404`
  </read_first>
  <action>
    Create `internal/agui/share_export_test.go` with the **single** build tag `db_integration`.

    Tests (names from VALIDATION.md):
    - `TestShareExportMarkdown` — seed identity A + a conversation with turns; `GET
      /api/conversations/{id}/export?format=md` with A's principal ⇒ 200; assert
      `Content-Disposition` starts with `attachment`, `Content-Type` is `application/octet-stream`,
      `X-Content-Type-Options` is `nosniff`, and the body contains the assistant prose.
    - `TestShareExportJSON` — `?format=json` ⇒ 200; unmarshal the body into `share.Snapshot`; assert
      `schema_version == 1` and the turns round-trip.
    - `TestShareExportForeignConversation404` — **SC4 row 1**: seed identities A and B; B requests A's
      conversation ⇒ **404**, and assert the body does NOT contain the conversation title (a 404 that
      leaks the title is still an existence oracle).
    - `TestShareExportFormatFallback` — absent `format` and a garbage `format` both ⇒ 200 with the
      Markdown body, never 400/500.
    - `TestShareExportRedactsHostPaths` — seed a conversation whose history carries a `send_file` tool call
      with `{"path":"/abs/secret.xlsx"}` and a `role="tool"` result containing `/etc/passwd`; assert the
      exported bytes contain neither. This is SC3 asserted **end-to-end through the HTTP surface**, not
      just at the unit level — the unit tests prove the projection, this proves the wiring.
    - `TestShareExportUnauthenticated` — a request with no principal ⇒ 401 (not 200, not 404).

    Seed **provisioned non-wildcard identities** (R-13). Use `withPrincipal` to inject the principal
    directly — no Authula needed. **Exactly one build tag**: any extra tag means the file compiles+skips
    in CI and contributes ZERO coverage (WR-01).
  </action>
  <verify>
    <automated>go test -tags db_integration -race -p 1 -count=1 -run 'TestShareExport' ./internal/agui/ && go test ./internal/agui/ -count=1</automated>
  </verify>
  <acceptance_criteria>
    - `go test -tags db_integration -race -p 1 -count=1 -run 'TestShareExport' ./internal/agui/` passes with a **non-sub-second** runtime (sub-second means it skipped).
    - The untagged agui suite still passes: `go test ./internal/agui/ -count=1`.
    - `head -1 internal/agui/share_export_test.go` is exactly `//go:build db_integration`.
    - `grep -rn "garage_integration\|authula_integration\|musr_e2e\|docker_integration" internal/agui/share_export_test.go` returns NOTHING.
    - `TestShareExportForeignConversation404` asserts BOTH the 404 status AND that the body omits the title.
    - `TestShareExportRedactsHostPaths` asserts on the real HTTP response bytes, and covers both `/abs/` and `/etc/`.
    - All six tests are present: `grep -c "^func TestShareExport" internal/agui/share_export_test.go` returns `6`.
    - No test grants `*` to a seeded identity.
    - `internal/agui` package coverage under the gate tags does not regress below 85%.
  </acceptance_criteria>
  <done>All six export behaviors — MD, JSON, foreign-404 without a title oracle, format fallback, end-to-end path redaction, and unauthenticated 401 — pass live under a single `db_integration` tag.</done>
</task>

</tasks>

<threat_model>
## Trust Boundaries

| Boundary | Description |
|----------|-------------|
| requester → a conversation | `GetForIdentity` is the boundary and it precedes the unscoped `LoadHistory`. Reversing that order would read a foreign history before deciding whether to allow it. |
| exported bytes → the user's filesystem | The file lands on a disk and may be opened by a browser. The neutral Content-Type + `nosniff` is what keeps user-authored text inert (37A D-10). |
| conversation title → an HTTP header | A user-authored title in `Content-Disposition` is a header-injection vector. |

## STRIDE Threat Register

| Threat ID | Category | Component | Disposition | Mitigation Plan |
|-----------|----------|-----------|-------------|-----------------|
| T-37F-45 | Information Disclosure | exporting another identity's conversation | mitigate | Owner gate precedes `LoadHistory`; ANY error ⇒ 404, never 403; the 404 body omits the title so it is not an existence oracle. `TestShareExportForeignConversation404`. |
| T-37F-46 | Information Disclosure | host paths in an exported file | mitigate | The export serializes a `share.Snapshot`, which cannot hold a path. Asserted end-to-end through HTTP by `TestShareExportRedactsHostPaths`, not only at unit level. |
| T-37F-47 | Information Disclosure | stored XSS from an exported artifact opened in a browser | mitigate | `application/octet-stream` + `X-Content-Type-Options: nosniff` for BOTH formats — never `text/markdown` or `application/json` (37A D-10). Grep-gated. |
| T-37F-14 | Spoofing | header injection via a user-authored conversation title | mitigate | `contentDisposition` (RFC-6266 + `url.PathEscape` + diacritic fallback), already tested. Never concatenated. |
| T-37F-48 | Elevation of Privilege | an unauthenticated export | mitigate | The route sits inside the already-mounted `conversationsRoutePrefix` subtree and inherits whole-origin `RequireAuth`; it is deliberately NOT added to any public allowlist. `TestShareExportUnauthenticated`. |
| T-37F-49 | Denial of Service | a `serve_webui.go` LOC breach blocking every commit | mitigate | F-1: the export needs zero lines in that 593/600 file. Enforced by a `git diff --name-only` gate in the task's verify. |
| T-37F-SC | Tampering | npm/pip/cargo installs | accept | Existing deps only. |
</threat_model>

<verification>
- `go build ./... && go vet ./internal/agui/`
- `go test ./internal/agui/ -count=1` (untagged)
- `go test -tags db_integration -race -p 1 -count=1 ./internal/agui/`
- `golangci-lint run ./internal/agui/` → 0 issues
- `git diff --name-only | grep cmd/aura/serve_webui.go` → **no match** (F-1)
- `bash scripts/check-file-size.sh` → 0
</verification>

<success_criteria>
WEBSHARE-01 ships: the owner downloads their conversation as Markdown or JSON through an identity-scoped
endpoint that 404s on foreign without leaking the title, redacts host paths end-to-end, serves a neutral
attachment, and required **zero** lines in `cmd/aura/serve_webui.go`.
</success_criteria>

<output>
Create `.planning/phases/37F-conversation-artifact-sharing-export-inserted/37F-09-SUMMARY.md` when done.
Confirm explicitly that `serve_webui.go` was not modified (F-1) and record the `db_integration` runtime.
</output>
