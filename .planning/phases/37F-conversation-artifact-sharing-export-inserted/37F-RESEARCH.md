# Phase 37F: Conversation & Artifact Sharing / Export - Research

**Researched:** 2026-07-15
**Domain:** Authenticated + opt-in-public share links, redacted conversation snapshot serialization, token-scoped blob storage, unauthenticated HTML surface hardening
**Confidence:** HIGH (all Go/TS seams read on disk at HEAD `1a3252e64`; open-webui studied from source at tag `0.10.2` / commit `ecd48e2f`)

---

## Summary

37F is **entirely net-new** — `grep` for `shared_links|share_audit|ShareLink|/s/{token}` across `internal/`, `cmd/`, `web/src/` returns **zero** matches. Every seam it builds on (identity-scoped conversation reads, the asset delivery lane, the capability gate, the audit union, the runner delete lifecycle, the sandboxed-iframe renderer) is shipped and verified below with file:line evidence.

The research turned up **four findings that invalidate or materially refine locked CONTEXT.md decisions**, all of which the planner must absorb before writing tasks:

1. **Migration 0036 is TAKEN.** D-11 states "0035 `assets_source_kind_agent` is latest on disk; 37F adds 0036." That was true on 2026-07-13 when CONTEXT.md was gathered. Phase 42 (compaction) shipped **0036–0039** on 2026-07-14. **37F must use 0040.** This is the exact numbering trap CLAUDE.md warns about.
2. **Reasoning/thinking traces are never persisted.** D-08 puts "reasoning/thinking traces" in the snapshot. `aura.conversation_turns` has no reasoning column, `llm.Message` has no reasoning field, and `llm.Chunk.Reasoning` is stream-only. The export path (`LoadHistory`) *structurally cannot* produce them. D-08 must be amended (recommend: drop — no reference product ships CoT in a share).
3. **Aura has no thread header.** D-05 locks a "thread-header share-arrow." `ShellHeader.tsx` is the *app-level* header. The real seam is the floating toggle cluster in `AppShell.tsx:517` — and 37B's code **explicitly reserved the spot**: `ArtifactsShell.tsx:20-22` says *"the adjacent share-arrow is 37F, not built."* Placement intent is validated; the implementation target is corrected.
4. **The 37A D-13 path strip is client-side.** `sseAdapter.ts:346-360` allowlists the artifact descriptor **in the browser**; the backend still ships `path` over the wire (`Actions.ArtifactDelta map[string]any`). The Go exporter cannot reuse it and must implement its own server-side allowlist.

On the open questions: the evidence is decisive on all five. **`share.public` costs zero schema** (capability names are free-text validated by a regex; four names already minted; `identity.create` is the exact per-user/off-by-default precedent — and open-webui independently converged on a dedicated `sharing.public_chats` permission defaulting to `False`). **Expiry needs both** lazy-on-access (the fail-closed gate) and a sweep (byte reclamation — D-09 is unmet without it). **Token-scoped keys must derive from `share_id`, not `token_hash`** — the internal tier has no token, so a hash-derived key leaves internal shares unaddressable.

**Primary recommendation:** Build a new `internal/share` package whose `Snapshot` type is the *single* constructor that ever touches `llm.Message` — MD, JSON, and the rendered page are all pure functions of it, so redaction cannot diverge (D-07). Gate public minting on a net-new `share.public` capability. Store blobs under a `share/` prefix disjoint from `identity/`. Enforce expiry lazily at every resolve and reclaim bytes with a `share_expiry_sweep` scheduler kind. Land `shared_links` + `share_audit` together in **migration 0040**. Put the SC4 cross-identity deny test in `internal/agui` under `db_integration` — a `cmd/aura` `musr_e2e` test contributes **zero** coverage.

---

## User Constraints (from CONTEXT.md)

### Locked Decisions

- **D-01 — All three tiers ship, fail-closed.** (a) File export always available; (b) an **internal-identity revocable** link is the default "Condividi"; (c) a **public opt-in expiring opaque token**, never default, behind an explicit warning.
- **D-02 — Public-link minting is capability-gated: off by default per user, admin-grantable, org kill-switch.** Reached via the existing `RequireCapability`/`HasCapability` interface + `capability_grants` seam (36 D-01/D-04). **Locked semantics:** per-user grantable, off by default, an admin can disable public sharing org-wide. **OPEN (planner/PRD call):** the capability *name*. **Internal-identity links need NO capability.**
- **D-03 — Public recipient sees a rendered read-only page at `/s/{token}`.** New unauthenticated HTML surface → MUST inherit 37A/37B XSS discipline: render ONLY the redacted snapshot, never tool-call payloads; HTML artifacts in a sandboxed `<iframe srcdoc>` (`sandbox="allow-scripts"`, no `allow-same-origin` — 37B D-07); SVG download-only; no enumerable IDs.
- **D-04 — Public token mandatory expiry.** Default **7 days**; owner-selectable (1d/7d/30d/custom up to a max cap). Revoke always available and independent of expiry. Fail-closed if the owner forgets to revoke.
- **D-05 — Whole-conversation only.** Single-artifact and single-message share links are OUT (deferred). The 37B-deferred "Condiviso" section + share-arrow operate at **thread level**.
- **D-06 — A shared conversation is a STATIC SNAPSHOT frozen at creation + an owner "Update" button.** Turns added after sharing NEVER retroactively appear. Not a live view.
- **D-07 — Export in BOTH Markdown and JSON.** The rendered public page AND both file formats all derive from **ONE canonical redacted snapshot model** (single serializer core + format adapters).
- **D-08 — Snapshot/export content:** visible user+assistant text (baseline) + delivered artifacts (D-09) + **tool-call provenance (tool NAMES only, per turn)** + reasoning/thinking traces. **HARD SC3 redaction (non-negotiable, NOT a toggle):** raw tool-call arguments/results and any host/container filesystem path are ALWAYS stripped. The snapshot carries `asset_id`/`filename` only, never the raw path chip.
- **D-09 — Delivered artifacts travel with the share; PUBLIC = bundled token-scoped.** Artifact bytes copied into a **token-scoped, public-readable snapshot store**; recipient downloads via the token, NEVER via `/api/assets/{id}/download`. Served `Content-Disposition: attachment` + `application/octet-stream` (37A D-10); HTML previews sandboxed (D-03). Revoke/expiry drops the bundled copy.
- **D-10 — Internal link = bearer-within-auth.** ANY authenticated Aura identity holding the link can open the already-redacted snapshot. No recipient picker, no per-recipient grant rows. Revocable + optional expiry.
- **D-11 — New `shared_links` table, migration 0036** (next free slot; 0035 `assets_source_kind_agent` is latest on disk). Columns (planner refines): `id`, `owner_identity_id`, `conversation_id`, `tier`, `token_hash` (public only), snapshot pointer, format/options, `expires_at`, `revoked_at`, `created_at`, `updated_at`. Reuse-assets-only REJECTED.
- **D-12 — Snapshot content + bundled artifact bytes = TOKEN-SCOPED Garage blobs** (reuses 37A `internal/objectstore`), NOT the identity-scoped `AssetKey(identityID, assetID)`. Public blobs must be reachable **without an identity principal**, yet remain opaque + revocable.
- **D-13 — Public token = 256-bit opaque random, stored HASHED at rest** (SHA-256), constant-time compare on lookup, never logged. Opaque → no enumerable IDs. Plaintext shown to the owner once at creation.
- **D-14 — Share act audited via a dedicated identity-keyed `share_audit` ledger** capturing **create / update-snapshot / revoke / expire / public-access(open)**. Joins the existing audit family (unioned in `internal/agui/audit_store.go`) and surfaces in the existing admin audit UI. Public-link opens audited (timestamp + coarse info, **NO recipient PII**). Fold-into-`tool_invocations` REJECTED.
- **D-15 — Revoke-on-delete cascade + expiry sweep.** Deleting a conversation MUST revoke + drop all its shares. Route through `Runner.DeleteConversationLifecycle` (`internal/runner/runner_delete.go:38`): revoke `shared_links` + delete token-scoped Garage blobs BEFORE the persistence delete. Expired/revoked token → **404, never a stale render**. Expiry enforcement = scheduler sweep and/or lazy-on-access — planner's call, fail-closed either way.

### Claude's Discretion

- Exact `shared_links` / `share_audit` column set + indexes; whether `share_audit` is its own migration or shares 0036.
- The capability NAME for public-share gating (D-02) — net-new `share.public` vs reuse `governance.write`; the SEMANTICS are locked, only the name is open.
- Token-scoped Garage key/bucket derivation (D-12); expiry sweep-vs-lazy (D-15).
- One serializer core + format adapters vs two writers for MD/JSON (D-07 prefers one core).
- The share-modal UX + "Condiviso" section layout.

### Deferred Ideas (OUT OF SCOPE)

- **Standalone single-artifact share link** ("Publish artifact" Claude-style).
- **Single-message share link.**
- **Explicit per-recipient-identity internal grants** + recipient-picker UI.
- **Remix / re-import of a shared JSON export.**
- **Interactive AI-powered shared artifacts** (Claude's runtime-API artifacts).

---

## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| **WEBSHARE-01** | Owner can export a conversation (Markdown/JSON) via an identity-scoped endpoint (`GetForIdentity`, `Content-Disposition: attachment`). | `GetForIdentity` verified `store_identity.go:28`; `LoadHistory` `store.go:260`; `contentDisposition` RFC-6266 helper verified `content_disposition.go:23`; download-route template `assets_api.go:35`. Serializer shape → OQ4. |
| **WEBSHARE-02** | Sharing is (a) revocable + capability-gated toward Aura identities, or (b) explicitly opt-in expiring opaque token with warning, never default; owner can revoke. | `RequireCapability` `auth.go:281`; `HasCapability` SQL wildcard `capability_grants.sql:17-23`; capability grammar `identity/store.go:33`; public-path allowlist `auth.go:213` + `serve_webui.go:523-534`. Capability name → OQ2. Expiry → OQ3. |
| **WEBSHARE-03** | No host/container path and no other identity's data reach a recipient; the share act is audited. | Redaction Inventory (below) — 9 verified leak sources. Audit union `audit_store.go:53-69` + `auditIdentityKeys` `:99`; `share_audit` slots in as a 4th UNION ALL leg → OQ5. |
| **WEBSHARE-04** | Unit + e2e + a cross-identity deny test on the shared link; coverage ≥85%. | Validation Architecture (below). **Landmine:** the existing cross-deny E2E (`cmd/aura/two_identity_e2e_test.go`) is 5-tag-gated AND in `cmd/aura` → contributes zero coverage. SC4 must live in `internal/agui` under `db_integration`. |

---

## Project Constraints (from CLAUDE.md)

| Directive | Bearing on 37F |
|---|---|
| **PRD-first (absolute)** | PRD-amendment for WEBSHARE-01..04 + likely ADR (sharing vs identity isolation) **before** code. |
| **NEVER SUPPOSE** | Every file:line in this doc was opened. Unverifiable items are marked OPEN, not guessed. |
| **NO GOD CLASS (≤600 LOC)** | Enforced by `scripts/check-file-size.sh` (cap 600, `make quality`, pre-push + CI). **Three target files will breach** — see Risks R-01/R-02/R-03. |
| **COVERAGE FLOOR 85%** | Overrides PRD ≥75%/≥60%. Owned surface = `./internal/...` minus `db/sqlc`, `agent/agenttest`, `llm/client.go` (`coverage_gate.sh:64-67`). |
| **COVERAGE GATE TAGS = `db_integration neo4j_integration` ONLY** | `coverage_gate.sh:25`. Any other tag ⇒ ZERO coverage. Drives the entire 37F test placement (see Validation Architecture). |
| **NO SKIP-AS-GREEN** | Skip-helpers must `t.Fatal` when required env unset AND `$CI` set. Precedent `musrEnvOrSkip` (`two_identity_e2e_harness_test.go:38`). |
| **Migration discipline** | Forward-only golang-migrate, `.up.sql` + `.down.sql`. **Verified on disk: 0039 is latest, 0040 is free.** |
| **i18n en+it** | react-i18next, `t('feature.key')`, per-domain modules ≤600 LOC. |
| **All prompt/LLM-facing overlays ENGLISH ONLY** | 37F adds no LLM-facing prompts (no agent-facing tool) — N/A, but the deferred-tool rule stays unarmed. |
| **Frontend_aesthetics** | No Inter/Roboto, no purple-on-white, distinctive type, motion on high-impact moments. Cockpit palette is **BLUE** (`tokens.json`, approved) — do NOT re-skin. |
| **Premium bar, not minimal** | Where open-webui is spartan, lean rich (see UI/UX §). |

---

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| Redaction (SC3) | **API / Backend** (`internal/share`) | — | The one non-negotiable security boundary. The 37A precedent put the path strip in the *browser* (`sseAdapter.ts:346`) — for a share that is unacceptable: the recipient's browser is not a trust boundary. Redaction MUST complete before bytes leave the server. |
| Snapshot serialization (MD/JSON/page-model) | **API / Backend** | — | D-07's one-core rule only holds if one Go constructor owns it. |
| Token mint / hash / verify | **API / Backend** | — | Secret material never reaches the client except once, in the response body. |
| Expiry + revocation enforcement | **API / Backend** (lazy) | **Scheduler** (sweep GC) | Lazy = the fail-closed gate. Sweep = byte reclamation only. Never the reverse. |
| Capability gate (`share.public`) | **API / Backend** (route mount) | Browser (cosmetic hide) | T-36-10-E precedent: SPA hide is cosmetic, the server mount is the boundary. |
| Blob storage + revoke-drops-bytes | **Database / Storage** (Garage) | API (orchestration) | `objectstore.Store` `List`+`Delete` already exist (`types.go:56-57`). |
| Public read-only page render | **Browser** (SPA at `/s/{token}`) | API (serves redacted JSON) | Reuses the 37B renderers verbatim (D-03). A second Go-template renderer would fork redaction — exactly what D-07 forbids. |
| HTML artifact isolation | **Browser** (null-origin iframe) | — | `sandbox="allow-scripts"` without `allow-same-origin` (`HtmlPreview.tsx:19`). |
| Share affordance + modal | **Browser** | — | Floating cluster (`AppShell.tsx:517`), spot reserved by 37B. |
| Delete-cascade revoke | **API / Backend** (`Runner`) | **Database** (FK CASCADE) | Lifecycle hook drops Garage bytes (FK cannot); FK is the backstop. |

---

## Code Seams

Every line below was opened and verified at HEAD `1a3252e64`.

### Backend — Go

| File:line | Symbol | 37F use |
|---|---|---|
| `internal/conversations/store_identity.go:28` | `GetForIdentity(ctx, convID, identityID) (Conversation, error)` | The identity-scoped read the export + share-create key on. Routes through `db.WithIdentityTx` → RLS 0032 backstop. Miss ⇒ `ErrConversationNotFound` ⇒ 404. |
| `internal/conversations/store.go:260` | `LoadHistory(ctx, convID) ([]llm.Message, error)` | The turn source for the serializer. **Unscoped** — the caller MUST gate with `GetForIdentity` first. |
| `internal/conversations/store.go:278` | `loadTurns` → `[]Turn` | Rehydrates sidecar-spilled content from disk. |
| `internal/conversations/store.go:118` | `Turn` struct | Carries `ContentSidecarPath` — **a filesystem path** (leak L-07). |
| `internal/llm/client.go:24` | `Message{Role, Content, ToolCalls, ToolCallID, Name}` | The projection input. `ToolCalls[].Function.Arguments` is the hard leak (L-01). |
| `internal/llm/client.go:34` | `ToolCall{ID, Type, Function{Name, Arguments}}` | `Function.Name` **survives** (D-08 tool names); `Arguments` **stripped**. |
| `internal/agui/assets_api.go:13` | `registerAssetRoutes(mux)` | Template for `registerShareRoutes`. Go 1.22 `mux.HandleFunc("METHOD /path", …)`. |
| `internal/agui/assets_api.go:35` | `handleAssetDownload` | The stream-through template: `octet-stream` + `nosniff` + `contentDisposition` + `Content-Length` + `io.Copy` scoped to `r.Context()`. Never presigned. |
| `internal/agui/assets_api.go:201` | `principalIdentityID(r) (string, bool)` | Principal extraction for every owner-scoped share route. |
| `internal/agui/content_disposition.go:23` | `contentDisposition(filename)` | RFC-6266 `filename` + `filename*=UTF-8''` with `url.PathEscape` header-injection guard. Reuse verbatim for the export + bundled-artifact download. |
| `internal/agui/auth.go:197` | `RequireAuth(next, deps)` | Whole-origin gate. `/s/{token}` must join the public allowlist. |
| `internal/agui/auth.go:213` | `deps.isPublicPath(p) \|\| deps.PublicRoute(r)` | The exact allowlist hook. |
| `internal/agui/auth.go:281` | `RequireCapability(next, deps, capability)` | The public-mint gate. Note `:282` — `!SecretConfigured` ⇒ **pass-through** (loopback dev). |
| `internal/agui/auth.go:307` | `withPrincipal` / `:316` `principalFrom` | Principal plumbing. |
| `internal/agui/audit_store.go:53-69` | `auditActivityQuery` | 3-leg UNION ALL, projection `source, action, target, detail, created_at`. `share_audit` = a 4th leg (OQ5). |
| `internal/agui/audit_store.go:99` | `auditIdentityKeys(identityID) []string` | `$1::text[]` keys. `share_audit.identity_id text` unions for free. |
| `internal/agui/audit_api.go:32` | `AuditEvent{Source, Action, Target, Detail, CreatedAt}` | `Source` gains `"share"`. |
| `internal/agui/audit_api.go:249-254` | `SanitizeString` on Target/Detail | Applies automatically to share rows. |
| `internal/agui/server.go:104-108` | `Server{… assets AssetService …}` | Add `share ShareService`. |
| `internal/agui/server.go:241/262` | `Mux()` / `registerAssetRoutes(mux)` | Registration site. |
| `internal/agui/asset_service.go:11-22` | `AssetService` interface | `ListForThread` + `OpenForIdentity` are the bundler's inputs. |
| `internal/objectstore/types.go:51-58` | `Store{PresignPut, Put, Head, Get, List, Delete}` | `List`+`Delete` = revoke-drops-bytes. `Get` = the stream source. |
| `internal/objectstore/types.go:60-62` | `AssetKey(identityID, assetID)` → `"identity/"+…` | The sibling to add: `Share*Key` under a disjoint `"share/"` prefix (OQ1). |
| `internal/objectstore/identity_store.go:81` | `IdentityStore.Resolve(ctx) (Credentials, error)` | **Reads identity from ctx.** `isShared("")` ⇒ true ⇒ empty principal returns the SHARED creds (`:151-153`). Decisive for OQ1. |
| `internal/objectstore/identity_store.go:34-38` | `Credentials{Bucket, AccessKey, SecretKey}` | Per-identity **bucket AND key** — not one bucket with prefixes. |
| `internal/assets/object_resolver.go:39` | `resolveObjects(...)` | `pgx.ErrNoRows` ⇒ shared; else per-identity store. |
| `internal/assets/types.go:65-71` | `SourceKind{web,telegram,cli,**agent**}` | `SourceAgent` = the artifact filter (matches Claude's artifact-vs-attachment rule). |
| `internal/runner/runner_delete.go:38` | `DeleteConversationLifecycle(ctx, identityID, convID) (int64, error)` | D-15 hook. Owner gate at `:44` runs FIRST; steps 1-4 teardown; step 5 (`:73`) persistence delete. **Share revoke inserts as step 4.5**, before `:73`. |
| `internal/identity/store.go:33` | `capNameRe = ^[a-z][a-z0-9._-]{0,63}$` | `share.public` matches. |
| `internal/identity/store.go:210` | `ValidateCapabilityName` | The only gate on a capability name. **No registry, no enum.** |
| `internal/db/queries/capability_grants.sql:17-23` | `HasCapability` → `WHERE identity_id=$1 AND (capability='*' OR capability=$2)` | `*` wildcard passes ANY name — incl. a net-new one. |
| `internal/cron/handlers/sweep.go:36` | `newCountingSweep(kind, maxDur, seam, …)` | `share_expiry_sweep` ≈ 20 LOC (OQ3). |
| `internal/cron/handlers/identity_purge.go:12` | `KindIdentityPurge TaskKind = "identity_purge"` | The kind precedent. |
| `internal/objectstore/fake.go:17` | `FakeStore` / `NewFake()` | In-memory `Store` ⇒ the share bundler is testable under `db_integration` with **no Garage**. Load-bearing for the coverage floor. |
| `cmd/aura/serve_webui.go:523-534` | `auth.PublicRoute = func(r) bool {…}` | Chaining pattern: `isPublicPasswordResetRoute`, `isPublicBootstrapRoute` → add `isPublicShareRoute`. |
| `cmd/aura/serve_webui.go:118` | `governanceWriteCapability = "governance.write"` | Where `sharePublicCapability` would live — **but the file is 593 LOC** (R-01). |
| `cmd/aura/serve_webui.go:517` | `mux.Handle("/", static)` | The SPA catch-all that serves `/s/{token}` once `PublicRoute` admits it. |

### Frontend — TypeScript / React

| File:line | Symbol | 37F use |
|---|---|---|
| `web/src/AppShell.tsx:514-517` | `<div className="pointer-events-none absolute right-3 top-2.5 z-20 flex items-center gap-1">` + `<VoiceModeToggle/>` + `<ArtifactsToggle/>` | **The real "thread header."** The share-arrow joins here. Children must set `pointer-events-auto`. |
| `web/src/shell/ArtifactsShell.tsx:20-22` | `// D-01: … the adjacent share-arrow is 37F, not built. It floats over the chat workspace so it reads as a header control without editing ShellHeader` | **37B reserved the spot.** Direct in-code evidence for the placement decision. |
| `web/src/shell/ArtifactsShell.tsx:23-42` | `ArtifactsToggle` | The exact pattern to mirror: `Button variant=ghost size=icon`, `aria-pressed`, `data-active`, `pointer-events-auto rounded-full bg-surface/70 backdrop-blur`. |
| `web/src/shell/ShellHeader.tsx:1-97` | `ShellHeader` | **App-level** (nav, modes, approvals, theme, language, logout). **NOT** a thread header — do not put share here. |
| `web/src/chat/artifacts/ArtifactsPanel.tsx:38` | `ArtifactsPanel({threadId, onClose})` | Gains the "Condiviso" section (D-05). 160 LOC — headroom OK, but see R-04. |
| `web/src/chat/artifacts/useThreadArtifacts.ts:33` | `selectAgentArtifacts` → `source_kind==='agent' && status!=='deleted' && status!=='canceled'` | **The artifact set to bundle.** Mirror this filter server-side. Matches Claude's artifact-vs-attachment rule exactly. |
| `web/src/chat/artifacts/renderers/HtmlPreview.tsx:17-22` | `<iframe srcDoc={data} sandbox="allow-scripts">` | D-03's mandated reuse. No `allow-same-origin` ⇒ null origin. |
| `web/src/chat/artifacts/renderers/useAssetContent.ts:32` | `fetch('/api/assets/${id}/download', {credentials:'same-origin'})` | **Hardcoded identity-scoped URL.** Blocks public-page reuse → needs an asset-URL resolver seam (R-05). |
| `web/src/chat/artifacts/PreviewModal.tsx:73,101` | `href={'/api/assets/'+id+'/download'}` | Same hardcode, two more sites. |
| `web/src/chat/artifacts/artifactMeta.ts:52` | `previewKind(mime, filename)` | SVG⇒`download` gate (T-37B-05). Reuse verbatim on the public page. |
| `web/src/chat/sseAdapter.ts:346-360` | `if (frame.name === 'aura.artifact' && isArtifactDescriptor(...))` — copies ONLY `filename`,`size_bytes`,`asset_id`,`mime_type` | **The 37A D-13 strip is CLIENT-SIDE.** Comment `:344-345`: *"`path` stays a backend/Telegram-only field."* The Go exporter must re-implement, not reuse. |
| `web/src/chat/displays/LocalArtifactDisplay.tsx:12-16` | `// A raw host/container path is NEVER rendered in EITHER branch (D-13) — the reducer omits it from the payload` | Confirms the strip location. |
| `web/src/i18n/resources.ts:1-17` | per-domain module imports (`resources.admin`, `resources.compaction`, …) | Add `resources.share.ts`. 576 LOC — R-03. |

---

## Redaction Inventory (SC3)

> **This is the machine-checkable core of SC3.** D-08 named 3 leak sources; the codebase has **9**.
> Every row below was traced to a struct field on disk. The planner turns each into an acceptance criterion.

| # | Field | Source (file:line) | Why it leaks | Snapshot carries instead |
|---|---|---|---|---|
| **L-01** | `ToolCall.Function.Arguments` (JSON string) | `internal/llm/client.go:39`, reached via `LoadHistory` → `turnToMessage` (`store_helpers.go:104`) | Verbatim tool args. `send_file` args are literally `{"path":"/abs/results.xlsx"}` (`send_file.go:57`). `shell_exec`/`read_file` args carry host paths. **The single worst leak.** | `tool_names: ["send_file"]` — `Function.Name` ONLY (D-08). |
| **L-02** | `Message.Content` on `role=="tool"` | `internal/llm/client.go:26`; tool turns persisted with `role='tool'` (`0005_conversations.up.sql:26`) | The raw tool **result**. `send_file` preview = `"queued results.xlsx for delivery"` (`send_file.go:201`); a `shell_exec` result is raw stdout (paths, hostnames, container IDs, env). D-08: results ALWAYS stripped. | **Turn omitted entirely.** Only `role IN ('user','assistant')` reach the snapshot. |
| **L-03** | artifact descriptor `{"path": …}` | `send_file.go:186` (`descriptor["path"] = path`) → `ToolResultMeta{"artifact":…}` (`:200`) → `Actions.ArtifactDelta map[string]any` (`agent/event.go:72`) → AG-UI `aura.artifact` CUSTOM event (`translator.go:19`) | Host **or container** path (`deliverFromBox` staging path when routed, `send_file.go:138`). Stripped only in the **browser** (`sseAdapter.ts:346-360`) — the backend ships it. | `{asset_id, filename, mime_type, size_bytes}` — the same 4-key allowlist, enforced **server-side** this time. |
| **L-04** | `tool_invocations.args_raw` | `0011_tool_invocations.up.sql:21` | Raw tool-call arguments at rest (D-08 explicit). | Not read by the serializer at all. |
| **L-05** | `tool_invocations.result_preview` | `0011_tool_invocations.up.sql:27` | Raw tool result at rest. | Not read. |
| **L-06** | `tool_invocations.result_sidecar_path` | `0011_tool_invocations.up.sql:30` | **A filesystem path column.** Beyond D-08's "args/results" wording — a distinct leak. | Not read. |
| **L-06b** | `tool_invocations.error` / `.meta` (jsonb) | `0011_tool_invocations.up.sql:26,31` | `error` embeds paths verbatim (`send_file.go:173`: `cannot read %q: %v`). `meta` jsonb carries the artifact descriptor incl. `path`. | Not read. |
| **L-07** | `Turn.ContentSidecarPath` | `internal/conversations/store.go:122` | `$AURA_RUN_DIR/conversations/<id>/<seq>.content` — a host path on the domain struct. A naive `json.Marshal(turn)` leaks it. | Never projected. The serializer consumes `[]llm.Message` (which has no such field), not `[]Turn`. |
| **L-08** | `Conversation.IdentityID` | `internal/conversations/store.go:102` | The owner's identity UUID. Not *another* identity's, but an internal identifier with no recipient value; open-webui leaks the owner's profile this way (`/s/[id]/+page.svelte:93` `getUserInfoById(chat.user_id)` → renders owner name/avatar). | Omitted. The snapshot names no identity. |
| **L-09** | `Message.ToolCallID` / `ToolCall.ID` | `internal/llm/client.go:28,35` | Internal correlation IDs; no recipient value; a needless enumerable surface (D-03: "no enumerable IDs"). | Omitted. |

### Redaction enforcement mechanism (planner: make this a task)

The inventory is only as good as its enforcement. Recommended structural guarantee:

1. **One constructor.** `share.BuildSnapshot(conv, msgs, artifacts) (Snapshot, error)` is the *only* function in the repo that takes an `llm.Message` and returns share-bound data. `Snapshot` has **no** field capable of holding args/results/paths — the leak is impossible by type, not by discipline.
2. **Allowlist, never denylist.** Build `SnapshotTurn` by *constructing* it field-by-field (like `sseAdapter.ts:353-360` does), never by copying and deleting.
3. **A negative test with realistic fixtures.** Feed a history containing `send_file` args with `/abs/secret/path.xlsx`, a `shell_exec` result with `/etc/passwd`, and a spilled turn; assert `!bytes.Contains(md, []byte("/abs/"))` and the same for JSON **and** for the page JSON. This is the machine-checkable form of SC3.
4. **Property test (see Validation Architecture):** for all generated histories, no output surface contains any string from the args/results corpus.

---

## Open Technical Questions — Resolved

### OQ1 — Token-scoped Garage key/bucket derivation (D-12)

**RECOMMENDATION**
```go
// internal/objectstore/types.go — sibling to AssetKey, DISJOINT prefix.
// uuid.UUID params (not string) make traversal structurally impossible: a
// hostile "../identity/<victim>/asset/x" cannot be expressed in the type.
func ShareSnapshotKey(shareID, snapshotID uuid.UUID) string {
    return "share/" + shareID.String() + "/snapshot/" + snapshotID.String() + "/canonical.json"
}
func ShareArtifactKey(shareID, snapshotID, assetID uuid.UUID) string {
    return "share/" + shareID.String() + "/snapshot/" + snapshotID.String() + "/asset/" + assetID.String()
}
func ShareKeyPrefix(shareID uuid.UUID) string { return "share/" + shareID.String() + "/" }
```
**Bucket:** the **shared bucket** (`cfg.ObjectStoreBucket`), reached through a `ShareObjects objectstore.Store` built from the **shared credentials at the composition root** — explicitly **NOT** via `IdentityStore.Resolve(ctx)`.

**EVIDENCE**
- `AssetKey` = `"identity/" + identityID + "/asset/" + assetID + "/original"` (`types.go:60-62`). A `"share/"` prefix is **lexically disjoint** from `"identity/"` → a share key can never address an identity object, and vice versa. Unit-testable as an invariant.
- `IdentityStore.Resolve(ctx)` (`identity_store.go:81`) reads `identityctx.IdentityID(ctx)`; `isShared(id)` returns true for `""` (`:151-153`). **A public recipient has no principal ⇒ `Resolve` would silently return the shared creds anyway.** Recommendation (a) is therefore *what already happens* — made explicit and intentional rather than accidental. That is strictly better than relying on an implicit fallback.
- **Key on `share_id`, NOT `token_hash` — four reasons, one decisive:**
  1. **DECISIVE:** D-11 makes `token_hash` **public-only** (NULL for internal). A hash-derived key leaves **internal-tier shares unaddressable** — but D-10 requires "Internal … shares resolve artifacts via the same snapshot."
  2. D-06's "Update" needs a *new* snapshot blob without destroying the old one mid-write. `snapshot_id` gives immutable blobs + an atomic pointer swap in the row.
  3. The token is an *authenticator*; the blob key is a *locator*. Deriving one from the other couples rotation to data movement.
  4. D-11 already says "The `shared_links` row points at the blob" — the row, not the URL, is the authority.
- **Revoke drops bytes:** `List(ListRequest{Bucket, Prefix: ShareKeyPrefix(shareID)})` → `Delete` each. Both exist on the `Store` interface (`types.go:56-57`).
- **`"public-readable"` in D-12 means *reachable without an identity principal through Aura* — NOT an S3 public ACL.** The bucket must stay private; the recipient never touches Garage. Aura streams the bytes (37A D-09 precedent: `handleAssetDownload` "is never presigned/redirected", `assets_api.go:33-34`). **This is a load-bearing clarification** — an S3-public bucket would make the blobs reachable *after* revoke, breaking D-09 and D-15.

**Why not a dedicated `aura-shares` bucket?** It needs Garage Admin API provisioning at boot (`garageadmin.Client`, 257 LOC) = a new boot failure mode, for isolation that buys little: the blobs are **already redacted** (D-08) and are **copies** (D-09) — the copy, not the bucket, is the isolation boundary. "No atomic bombs — find the minimal industrial form." The disjoint `share/` prefix makes a future bucket split a one-line change if a deployment ever wants it.

---

### OQ2 — The public-share capability NAME (D-02)

**RECOMMENDATION: net-new `share.public`.** Reject the `governance.write` fallback.

**EVIDENCE — a net-new capability name costs ZERO schema:**

| Claim | Evidence |
|---|---|
| `capability` is a **free-text column** — no enum, no CHECK, no registry | `0004_identity.up.sql:13-18`: `CREATE TABLE aura.capability_grants (identity_id uuid …, capability text NOT NULL, granted_at …, PRIMARY KEY (identity_id, capability))` |
| The **only** validation is a regex | `internal/identity/store.go:33` `capNameRe = ^[a-z][a-z0-9._-]{0,63}$`; `store.go:210` `ValidateCapabilityName` is the sole gate. **`share.public` matches** (`s` ∈ `[a-z]`; `hare.public` ⊂ `[a-z0-9._-]`; length 12 ≤ 64). |
| Four names are **already minted** | `agent.run` (`serve_webui.go:103`), `governance.read` (`:108`), `governance.write` (`:118`), `identity.create` (`:272`) |
| Seeding is a **data-only INSERT** | `0026_local_admin_caps.up.sql:29-36` — `INSERT … FROM (VALUES ('governance.write'),('identity.create'),('agent.run')) … ON CONFLICT DO NOTHING`, guarded by `WHERE EXISTS` |
| The `*` wildcard auto-passes any new name | `capability_grants.sql:22` `AND (capability = '*' OR capability = $2)` — the seeded operator is unaffected |

**Cost of `share.public`: one Go const + (optionally) one seed row.** No migration is even required for the capability itself.

**EVIDENCE — `identity.create` is the exact precedent for D-02's locked semantics.** `serve_webui.go:271-277`:
> *"the name becomes load-bearing for provisioned identities (**which never get '\*' nor identity.create unless the creator explicitly grants it AND holds it**)"*

That is verbatim D-02: per-user grantable, off by default, admin-grantable. `share.public` is `identity.create`'s sibling, not `governance.write`'s.

**EVIDENCE — the RESEARCH-OQ3 precedent does NOT apply here.** `0026_local_admin_caps.up.sql:1-3` records the actual reasoning:
> *"reuse the EXISTING `governance.write` for the D-02/D-03 **model-settings** capability rather than a net-new `settings.model.write`"*

`settings.model.write` was rejected because it was an **admin-scoped action already semantically covered** by `governance.write` — a genuine duplicate. Public sharing is a **per-user, non-admin action**. Reusing `governance.write` would mean *"to share your own chat publicly you must be a full org admin who can install MCP servers and RISKY supply-chain skills"* (`governance_write_seam.go:73`) — which **contradicts the locked per-user-grant semantics** and is a privilege-escalation smell (it would incentivize granting `governance.write` to ordinary users just so they can share).

**EVIDENCE — industrial convergence.** open-webui independently reached the same design:
- `backend/open_webui/config.py:1928` — `'public_chats': USER_PERMISSIONS_CHAT_ALLOW_PUBLIC_SHARING` — a **dedicated per-user permission**, not an admin role.
- `config.py:1839-1841` — defaults to **`False`** (off by default per user).
- `config.py:1843` — `USER_PERMISSIONS_CHAT_EXPORT` defaults **`True`** (export always on — validates D-01(a)).
- `ShareChatModal.svelte:152` — `sharePublic={$user?.permissions?.sharing?.public_chats || $user?.role === 'admin'}` — per-user permission **with** admin override.
- `CHANGELOG.md:461` — *"Public chat sharing permission control. Administrators can now control whether users are allowed to create publicly shareable chats through **a dedicated permission setting**."*
- `CHANGELOG.md:57` — the same pattern for folders: *"a new 'Folders Sharing' permission that is **off by default**."*

Claude converges too: *"Public sharing is off by default on Team and Enterprise plans — to enable it, an Owner must … turn on External sharing."*

**The org kill-switch (locked, name open):** open-webui uses a **separate global config flag** (`chats.py:482`: `if (user.role != 'admin') and (not await Config.get('ui.enable_community_sharing'))`) *in addition to* the per-user permission. Aura's equivalent seam is `aura.settings` (`0024_settings.up.sql:15`, `internal/settings`, allowlist-validated, `governance.write`-gated PUT — `settings_api.go:10-15`). **Recommend `AURA_SHARE_PUBLIC_ENABLED` in the settings allowlist**, checked *before* the capability. Two independent gates, both fail-closed — matching open-webui exactly.

> ⚠️ **`RequireCapability` returns `next` unchanged when `!SecretConfigured`** (`auth.go:282`) — loopback dev bypasses the capability gate entirely. The **org kill-switch must be checked inside the handler**, not only at the mount, or loopback dev can mint public links with no gate at all. This is a real fail-open the planner must close.

---

### OQ3 — Expiry enforcement: scheduler sweep vs lazy-on-access (D-15)

**RECOMMENDATION: BOTH — they are not alternatives. Lazy is the gate; the sweep is the GC.**

**Lazy-on-access is MANDATORY and is the security boundary.** Every token resolve is a single predicate:
```sql
SELECT … FROM aura.shared_links
 WHERE token_hash = $1 AND revoked_at IS NULL AND (expires_at IS NULL OR expires_at > now())
```
- Fail-closed **by construction**: if the sweep never runs (scheduler down, task unseeded, worker crash-looping), an expired link **still 404s**. A sweep-only design has a live window between `expires_at` and the next tick where an expired link resolves — a direct D-04/D-15 violation.
- It is the *same* statement as the lookup — **zero extra cost**, fully covered by the `token_hash` unique index (OQ5).
- `time.Now()` vs `now()`: use the **DB clock** in the predicate. A skewed app clock must not be able to resurrect a link.

**The sweep is REQUIRED for a different reason — D-09 is unmet without it.** D-09: *"Revoke/expiry drops the bundled copy."* Lazy enforcement makes an expired link *unreachable* but leaves its Garage bytes **forever**. Without a sweep: unbounded storage growth, and redacted-but-real user content persists in object storage indefinitely after its stated expiry — a data-retention problem, not just a cost one.

**Implementation — the precedent is exact and cheap:**
- Kind widen, mirroring `0033`/`0034` verbatim (`0034_scheduler_sandbox_reap_kind.up.sql:12-14`) — the `0009` inline CHECK is auto-named `scheduler_tasks_kind_check`; drop + re-add with `'share_expiry_sweep'` added. **Folds into migration 0040** (same slice, one commit).
- Handler ≈ **20 LOC** using `newCountingSweep` (`sweep.go:36`), copying `identity_purge.go:12-38` verbatim:
```go
const KindShareExpirySweep TaskKind = "share_expiry_sweep"
const shareExpiryMaxDuration = 5 * time.Minute
type ShareExpirer interface { ExpireDue(ctx context.Context, now time.Time) (int, error) }
func NewShareExpiryHandler(e ShareExpirer) Handler {
    var seam sweepFn
    if e != nil { seam = e.ExpireDue }
    return newCountingSweep(KindShareExpirySweep, shareExpiryMaxDuration, seam,
        "share expiry: disabled (no expirer)", "share expiry", "share expiry ok: expired %d link(s)")
}
```
- A nil expirer ⇒ disabled no-op, not a panic (`sweep.go:57-59`). `ReschedulesOnRecovery: false` (`:51`) is correct — the sweep is idempotent; the next tick re-evaluates the same due set.
- `ExpireDue` must be **idempotent + resumable**: drop blobs first, then stamp the row. A crash between the two re-runs the (idempotent) delete next tick. Never stamp-then-delete — that orphans bytes permanently.
- Emit a `share_audit` `expire` row per link (D-14 names `expire` as an audited action).

---

### OQ4 — One serializer core + format adapters vs two writers (D-07)

**RECOMMENDATION: one core. New package `internal/share`, 9 files, ~1030 LOC, every file well under 600.**

```
internal/share/
  snapshot.go   ~140  Snapshot/SnapshotTurn/SnapshotArtifact + BuildSnapshot()  ← THE redaction point
  redact.go     ~120  allowlist projections, path scrubbing, tool-name extraction
  markdown.go    ~90  func (Snapshot) Markdown() []byte
  jsonfmt.go     ~30  func (Snapshot) JSON() ([]byte, error)
  token.go       ~60  Mint() (plaintext, hash), Hash(plaintext)
  store.go      ~200  shared_links CRUD (raw pgx, PgAuditStore precedent)
  audit.go       ~80  share_audit writer
  service.go    ~250  Create / Update / Revoke / ResolveByToken / ResolveInternal / ExpireDue
  expiry.go      ~60  the ShareExpirer seam the cron handler drives
```

**The core type** — note what it *cannot* express (that is the whole point):
```go
type Snapshot struct {
    SchemaVersion int                `json:"schema_version"` // 1
    Title         string             `json:"title"`
    Model         string             `json:"model"`
    CreatedAt     time.Time          `json:"created_at"`
    SnapshotAt    time.Time          `json:"snapshot_at"`
    Turns         []SnapshotTurn     `json:"turns"`
    Artifacts     []SnapshotArtifact `json:"artifacts"`
}
type SnapshotTurn struct {
    Seq       int      `json:"seq"`
    Role      string   `json:"role"`                 // "user" | "assistant" ONLY
    Text      string   `json:"text"`
    ToolNames []string `json:"tool_names,omitempty"` // D-08: NAMES only
}
type SnapshotArtifact struct {
    AssetID   string `json:"asset_id"`
    FileName  string `json:"filename"`
    MIMEType  string `json:"mime_type"`
    SizeBytes int64  `json:"size_bytes"`
}
```

**Why one core actually holds (not just intent):**
- `BuildSnapshot` is the **only** function that accepts `[]llm.Message`. MD, JSON, and the page model are pure functions **of `Snapshot`**, which has no field able to hold args/results/paths. Divergence is a **type error**, not a review miss.
- `SnapshotTurn.Role` is `user|assistant` only ⇒ L-02 (tool-result content) is structurally excluded.
- No `Arguments` field anywhere ⇒ L-01 impossible.
- The public page fetches `Snapshot` as JSON from `GET /s/{token}/data` and renders it in React ⇒ the **third** surface is the **same** struct. This is why the rendered page must be the SPA, not a Go template: a Go template would be a second renderer with its own escaping and its own field access — a redaction fork.

**Enforcement test:** `BuildSnapshot` round-trip with a hostile fixture; assert no `/abs/`, no `/etc/`, no `container` id, no `AURA_RUN_DIR` substring survives into `Markdown()`, `JSON()`, or the page payload.

**Two writers is rejected**: MD and JSON would each independently re-derive from `[]llm.Message`, so a redaction fix in one silently misses the other — the exact failure D-07 exists to prevent.

---

### OQ5 — `shared_links` + `share_audit` schema (D-11/D-14)

#### ⚠️ Migration slot: **0040**, NOT 0036 — VERIFIED ON DISK

```
$ ls internal/db/migrations/ | tail
0035_assets_source_kind_agent.*      ← CONTEXT.md D-11's "latest"
0036_compaction_checkpoints.*        ← TAKEN (Phase 42)
0037_content_parts.*                 ← TAKEN
0038_compaction_memory.*             ← TAKEN
0039_compaction_rollout.*            ← TAKEN (git: 5bcca36b7 "feat(42-08)")
$ ls internal/db/migrations/0040*  → No such file
```
CONTEXT.md was gathered **2026-07-13**; Phase 42 shipped **2026-07-14** (MEMORY: *"Phase 42 shipped+pushed 2026-07-14"*). **D-11's slot claim is stale. 37F = 0040.** Re-verify at plan time — this project has ≥2 phases in flight.

#### `share_audit`: **own table, SAME migration 0040**

Rationale: one slice = one commit (CLAUDE.md); and a partial apply (`shared_links` without `share_audit`) would let a share be created **unaudited** — an SC3 violation. Atomic is the safer split. (0033/0034 were split because they were *different phases*.)

#### DDL

```sql
CREATE TABLE aura.shared_links (
    id                uuid        PRIMARY KEY,
    owner_identity_id uuid        NOT NULL REFERENCES aura.identities(id)    ON DELETE CASCADE,
    conversation_id   uuid        NOT NULL REFERENCES aura.conversations(id) ON DELETE CASCADE,
    tier              text        NOT NULL CHECK (tier IN ('internal','public')),
    token_hash        bytea,               -- SHA-256 raw; NULL for internal (D-11/D-13)
    snapshot_id       uuid        NOT NULL,-- blob pointer: share/{id}/snapshot/{snapshot_id}/…
    snapshot_bucket   text        NOT NULL,
    format_options    jsonb       NOT NULL DEFAULT '{}'::jsonb,
    expires_at        timestamptz,
    revoked_at        timestamptz,
    created_at        timestamptz NOT NULL DEFAULT now(),
    updated_at        timestamptz NOT NULL DEFAULT now(),   -- D-06 "Update"
    CONSTRAINT shared_links_tier_shape CHECK (
        (tier = 'public'   AND token_hash IS NOT NULL AND expires_at IS NOT NULL) OR
        (tier = 'internal' AND token_hash IS NULL)
    )
);
CREATE UNIQUE INDEX shared_links_token_hash_idx ON aura.shared_links (token_hash) WHERE token_hash IS NOT NULL;
CREATE INDEX shared_links_owner_idx        ON aura.shared_links (owner_identity_id, created_at DESC);
CREATE INDEX shared_links_conversation_idx ON aura.shared_links (conversation_id);
CREATE INDEX shared_links_expiry_idx       ON aura.shared_links (expires_at) WHERE revoked_at IS NULL;

CREATE TABLE aura.share_audit (
    id              uuid        PRIMARY KEY DEFAULT gen_random_uuid(),
    identity_id     text        NOT NULL,   -- text + NO FK: matches the audit family
    share_link_id   uuid,                   -- NO FK: audit outlives the link
    conversation_id uuid,                   -- NO FK: audit outlives the conversation
    action          text        NOT NULL CHECK (action IN ('create','update','revoke','expire','open')),
    tier            text,
    detail          text        NOT NULL DEFAULT '',
    created_at      timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX share_audit_identity_idx ON aura.share_audit (identity_id, created_at DESC);

GRANT SELECT, INSERT, UPDATE, DELETE ON aura.shared_links TO aura_app;
GRANT SELECT, INSERT                 ON aura.share_audit  TO aura_app;  -- append-only
```

**Column rationale (each tied to evidence):**
- **`token_hash bytea`, not text** — SHA-256 is 32 raw bytes; `bytea` avoids hex-vs-base64 ambiguity. Precedent: `secret_key_enc bytea` (`0030_identity_object_store`).
- **UNIQUE partial index on `token_hash`** — makes a duplicate mint impossible **and** is the lookup index (indexed equality, no scan).
- **`share_audit.identity_id text`, NO FK** — **matches the family verbatim**: `skill_audit.identity_id text NOT NULL DEFAULT 'local'` (`0010:29`), `mcp_audit.actor_identity_id text NOT NULL` (`0022:22`). An FK with CASCADE would **destroy the audit trail** when the identity is deleted — the opposite of an audit ledger's purpose. `text` also makes the union leg free (below).
- **`share_audit` append-only** (no UPDATE/DELETE grant to `aura_app`).
- **`shared_links … ON DELETE CASCADE` on `conversation_id`** — the DB backstop for D-15. open-webui does exactly this (`shared_chats.py:23`: `ForeignKey('chat.id', ondelete='CASCADE')`). **But the FK cannot drop Garage bytes** ⇒ the `Runner.DeleteConversationLifecycle` hook stays mandatory. FK = belt, hook = suspenders.
- **`shared_links_expiry_idx`** — the sweep's due-set index.

#### The audit union leg — 4 lines, admin UI free

`audit_store.go:53-69` projects `source, action, target, detail, created_at` and keys on `$1::text[]` (`auditIdentityKeys`, `:99`). `share_audit.identity_id text` slots in verbatim:

```sql
    UNION ALL
    SELECT 'share' AS source, action, COALESCE(conversation_id::text, ''), COALESCE(tier, ''), created_at
      FROM aura.share_audit
      WHERE identity_id = ANY($1::text[])
```
Then `AuditEvent.Source` gains `"share"` (doc comment `audit_api.go:33`). `SanitizeString` already runs on Target/Detail (`audit_api.go:251-254`). **D-14's "surfaces in the existing admin audit UI" is confirmed achievable with ~4 lines of SQL + 1 doc comment.**

#### ⚠️ D-13 refinement the planner must carry to the PRD

D-13 says *"stored HASHED at rest (SHA-256), **constant-time compare on lookup**"*. Implemented **literally**, that means scanning every row and `subtle.ConstantTimeCompare`-ing each — which is **slower and no more secure**.

The correct implementation is **hash-indexed equality lookup** (`WHERE token_hash = $1` on the unique index). This is safe: the lookup key is `SHA-256(token)`, and exploiting a timing signal on the index probe to recover the *token* would require inverting SHA-256. This is the standard "store a hash of the API key, index it, look it up" pattern.

D-13's **intent** — no plaintext token at rest, a DB/backup leak never exposes live links, no enumerable IDs — is **fully satisfied**. `crypto/subtle.ConstantTimeCompare` remains correct **only** where a secret is compared in Go memory (there is no such site in this design). Recommend the PRD-amendment record the hash-indexed lookup explicitly so a later reviewer does not "fix" it into a table scan.

---

## UI/UX Research — Share Surface

> Primary source: **open-webui `0.10.2`** read from source (commit `ecd48e2f`, 2026-07-01), cross-checked against ChatGPT + Claude help docs. Every open-webui claim below cites a file I opened.

### 0. The headline: Aura's design is already strictly stronger than open-webui's

| Dimension | open-webui (source) | Aura 37F (locked) |
|---|---|---|
| Token at rest | **Plaintext UUIDv4** as PK — `shared_chats.py:21` `id = Column(Text, primary_key=True)  # The share token` | SHA-256 hashed (D-13) |
| Expiry | **NONE** — no `expires_at` column exists (`shared_chats.py:19-30`) | Mandatory, default 7d (D-04) |
| Tiers | One | Three, fail-closed (D-01) |
| Public-share gate | Per-user permission (`config.py:1928`, default `False`) + global flag | Same **plus** capability + audit |
| Expired/missing | `goto('/')` — **redirect home** (`/s/[id]/+page.svelte:53`) | **404, never a stale render** (D-15) |
| Owner PII on the page | **Leaks it** — `getUserInfoById(chat.user_id)` renders owner name/avatar (`+page.svelte:93`) | Omitted (L-08) |
| Snapshot redaction | Whole `chat` JSON blob stored verbatim (`shared_chats.py:26`) | Allowlist projection (D-08) |

**Do not soften any locked decision to "match open-webui."** On every security axis, open-webui is the weaker reference. It is the right *UX* reference and the wrong *posture* reference.

### 1. Share affordance placement & discovery — **VALIDATE the locked placement, CORRECT the target**

**What the references do:** All three converge on **top-right of the thread**. ChatGPT: *"the share button at the top of the chat."* open-webui: chat navbar. Claude: top-right.

**What Aura actually has** (this is the correction):
- `ShellHeader.tsx` is the **app-level** header — nav trigger, mode switcher, approvals, runtime chip, theme, language, logout (`ShellHeader.tsx:1-97`). **There is no thread header.**
- The real top-right-of-thread is a **floating overlay cluster**, `AppShell.tsx:514-517`:
  ```tsx
  <div className="pointer-events-none absolute right-3 top-2.5 z-20 flex items-center gap-1">
    <VoiceModeToggle />
    <ArtifactsToggle active={artifactsActive} onToggle={toggleArtifacts} />
  </div>
  ```
- **37B explicitly reserved the spot** — `ArtifactsShell.tsx:20-22`:
  > *"D-01: the header-style doc toggle (the reference's top-right icon; **the adjacent share-arrow is 37F, not built**). It floats over the chat workspace so it reads as a header control **without editing ShellHeader** (out of this plan's scope) or shifting the chat layout."*

**RECOMMENDATION**
- Add `ShareToggle` to `ArtifactsShell.tsx`'s sibling module (a new `web/src/shell/ShareShell.tsx`), mirroring `ArtifactsToggle` (`ArtifactsShell.tsx:23-42`) **exactly**: `<Button variant="ghost" size="icon">`, `pointer-events-auto rounded-full bg-surface/70 backdrop-blur`, lucide icon.
- Order: `[VoiceModeToggle] [ShareToggle] [ArtifactsToggle]`. The artifacts toggle stays rightmost because it opens the right panel (spatial affinity); share is an action, not a panel toggle.
- Icon: lucide `Share2` (the arrow-node glyph — the "share arrow" D-05 names). `Link` is open-webui's choice but reads as "copy URL," not "share."
- **`aria-label`**, not a visible text label — the cluster is icon-only by established design.

**"Already shared" state signal — Aura should beat open-webui here (premium bar).**
- open-webui gives **no signal on the affordance**. You must open the modal to read *"You have shared this chat before"* (`ShareChatModal.svelte:130-132`). That is a genuine discoverability gap: a user cannot tell an active public link exists without clicking.
- Claude does better: a visibility dropdown showing `Public`/`Private` inline.
- **Aura recommendation:** `data-shared={sharedCount > 0}` on `ShareToggle`, styled with the **existing** `data-active` pattern (`ArtifactsShell.tsx:39-40` already does `data-[active=true]:text-accent-text`). An accent ring/dot. **~4 LOC, pure win.** Public tier deserves a distinct treatment (a `text-warning` dot — the token exists, `LocalArtifactDisplay.tsx:81`) since a live public link is the state a user most needs to notice.

### 2. The share modal

**open-webui's IA** (`ShareChatModal.svelte`, read in full):

| State | Copy / control |
|---|---|
| **Not shared** | *"Messages you send after creating your link won't be shared. Users with the URL will be able to view the shared chat."* Primary: **"Copy Link"** (`:186-215`) |
| **Shared** | *"You have shared this chat **before**. Click here to **delete this link** and create a new shared link."* (`:130-142`) Primary becomes **"Update and Copy Link"** (`:210-214`) |
| **Access** | `<AccessControl accessRoles={['read']} sharePublic={user.permissions.sharing.public_chats \|\| user.role==='admin'} …/>` — **only rendered when already shared** (`:147-157`) |
| **Community** | *"Share to Open WebUI Community"*, gated on `config.features.enable_community_sharing` (`:161-172`) |
| Copy feedback | `toast.success('Copied shared chat URL to clipboard!')` (`:216`) |
| Expiry | **Does not exist.** |
| Stale-snapshot | **Not handled** — the button is always "Update and Copy Link"; the user is never told whether anything is actually stale. |

**Weaknesses to fix, not copy:**
1. **Revoke is a text link buried mid-sentence** (`:134-142`) — a destructive, irreversible action rendered as inline prose. Unacceptable at Aura's bar.
2. **Access control appears only *after* sharing** — you cannot choose the tier *before* minting. That inverts D-01's "public is never default": in open-webui you mint first, then discover the access options.
3. **The Safari clipboard footgun** (`:190-208`): `navigator.clipboard.writeText()` **after an `await` loses the user gesture in Safari**, so they wrap the URL promise in a `ClipboardItem`. This is a real bug they hit and worked around.

**RECOMMENDED Aura modal — states: `idle → creating → shared → updating → revoking → revoked`**

```
┌─ Condividi conversazione ─────────────────────────── [X] ┐
│                                                          │
│  ◉ Link interno            Chiunque abbia il link E      │  ← tier radio, DEFAULT (D-01b)
│    (identità Aura)         un accesso ad Aura            │
│                                                          │
│  ○ Link pubblico  [!]      Chiunque abbia il link,       │  ← never preselected (D-01c)
│                            anche senza accesso ad Aura   │
│  ┌──────────────────────────────────────────────────┐   │
│  │ ⚠  Un link pubblico è visibile a chiunque lo      │   │  ← role="note", text-warning
│  │    riceva. Revocarlo impedisce nuovi accessi ma   │   │     ONLY when public selected
│  │    non cancella copie già viste o memorizzate     │   │     (the ChatGPT honesty note)
│  │    nella cache dei motori di ricerca.             │   │
│  └──────────────────────────────────────────────────┘   │
│  Scadenza  [ 1g ][ 7g •][ 30g ][ Personalizzata ]       │  ← D-04; 7g preselected
│                                                          │
│  Lo snapshot è congelato ora. I messaggi successivi      │  ← D-06, stated BEFORE minting
│  non compariranno nel link.                              │
│                              [ Annulla ] [ Crea link ]   │
└──────────────────────────────────────────────────────────┘
```
After mint (`shared` state):
```
│  https://…/s/hR3k…9fQ                        [ Copia ]   │  ← readonly input + separate button
│  Pubblico · scade tra 7 giorni · creato il 15/07         │
│  ⓘ 3 nuovi messaggi non sono in questo link.  [Aggiorna] │  ← STALE state (see below)
│                                        [ Revoca link ]   │  ← destructive, own row
```

**Key design decisions and why:**
- **Tier chosen BEFORE minting.** D-01/D-02 require public to be an explicit opt-in; open-webui's mint-then-configure inverts that. The warning renders **only when public is selected** — a warning shown always is a warning nobody reads.
- **Mint, THEN copy as a separate gesture.** This **sidesteps the Safari `ClipboardItem` bug entirely** (copy is a direct user gesture with no `await` before it) **and** is better UX — the user sees the URL. Derived from reading open-webui's workaround, not from copying it.
- **The "stale snapshot" affordance — Aura's differentiator, and it's cheap.** open-webui/ChatGPT can't tell you whether new turns exist; they just always offer "Update." Aura **can**: compare `conversations.last_active_at` (exists since `0005_conversations.up.sql:13`) against `shared_links.updated_at`. Show *"N nuovi messaggi non sono in questo link"* and emphasize **Aggiorna**. **The data already exists — zero new storage.** This is the single highest-value UX win available in this phase.
- **Revoke gets its own row + a `ConfirmDialog`** — destructive, irreversible, and (unlike open-webui's inline text link) treated as such.
- **Update is `PATCH /api/shares/{id}/snapshot`** — re-snapshot, keep the token (D-06 "Update"). Note Claude does this differently (*"unshare and share again"* → **new URL**); open-webui's keep-the-URL semantic is better and matches D-06's "Update" wording.
- Copy feedback: `aria-live="polite"` + label swap `Copia → Copiato ✓` (2s). Inline beats a toast for a control the user is looking directly at, and it needs no toast infrastructure.
- **Motion (CLAUDE.md Frontend_aesthetics):** one orchestrated reveal — the warning block and the expiry row slide in on tier change (`animate-in fade-in-0 slide-in-from-top-1`), reusing the staggered-reveal idiom already in `ArtifactsPanel.tsx:121-122`. High-impact moment; not scattered micro-interactions.

### 3. Shared-links management — **two surfaces, both needed**

open-webui ships **both**, and so should Aura:

| Surface | open-webui | Aura 37F |
|---|---|---|
| **Per-thread** | The modal's "shared before" line | **"Condiviso" section in `ArtifactsPanel.tsx`** (D-05, 37B-deferred) |
| **Global** | Settings → Data Controls → **Shared Chats** modal (`DataControls.svelte:28,153` → `SharedChatsModal.svelte`) | A Settings section (`resources.settings.ts` exists) |

**`SharedChatsModal.svelte` — the full read:**
- Shell reuse: `<ChatsModal title={$i18n.t('Shared Chats')} emptyPlaceholder={$i18n.t('You have no shared conversations.')} shareUrl={false} …>` — the **same shell** as ArchivedChatsModal. Aura should likewise reuse an existing list primitive rather than mint a bespoke table.
- Search with a **500ms debounce** (`:47-58`), `orderBy`/`direction` sort, cursor pagination (`:67-85`).
- **Per-row revoke: NO confirm dialog** (`:87-99`) → toast *"Chat unshared successfully."*
- **Bulk "Unshare All Shared Chats": HAS a confirm** (`:123-134`) — *"Are you sure you want to unshare all shared chats? This will remove all share links."* / confirmLabel *"Unshare All"* (added in `CHANGELOG.md:113`).
- Empty state: *"You have no shared conversations."*

**RECOMMENDATION for Aura's rows** — richer than open-webui's (premium bar), because Aura has data open-webui lacks:
`[title] · [tier badge internal|public] · [created] · [expires in Nd ⟵ open-webui has nothing here] · [Revoca]`
- **Confirm on per-row revoke too.** open-webui's no-confirm per-row is a defensible speed choice for a *free* link, but Aura's public links are capability-gated and audited — treat revoke as destructive consistently.
- **Keep "Revoca tutti"** with its ConfirmDialog — one extra endpoint, real operator value, and it's a proven addition (they shipped it *after* user demand).
- ChatGPT's flow is *Settings → Data controls → Shared links → Manage → row → details → Revoke*; a details step is unnecessary ceremony at Aura's scale — one list, inline revoke.

### 4. The public read-only page

**open-webui's `/s/[id]/+page.svelte`, read in full:**
- **Header:** `<h1 class="text-2xl font-medium line-clamp-1">{title}</h1>` + a `<time>` with `dayjs(...).format('LLL')` (`:167-181`). Minimal — no branding chrome beyond `<title>{title} • {WEBUI_NAME}` (`:150-155`).
- **Body:** reuses the **same `Messages` component** with `readOnly={true}` (`:185-201`). **This is the key architectural move** — one renderer, a read-only flag. It is also exactly why Aura's public page should be the SPA, not a Go template.
- **Footer:** a floating "Clone Chat" button with a gradient fade (`:203-216`) — shown **only when `$sessionUser` exists**, so an anonymous visitor sees no CTA.
- **Missing/expired:** `goto('/')` (`:53`) — silently redirects home. **Weaker than Aura's locked 404** (D-15).
- **PII leak:** `user = await getUserInfoById(localStorage.token, chat.user_id)` (`:93`) → the owner's name/avatar render on the page. **Aura must not** (L-08).
- Related bug they hit: *"Shared folder read-only chats no longer sign users out"* (`CHANGELOG.md:51`) — a resource-level access error was being read as session-expiry → the viewer got logged out. **A 403-vs-401 confusion.** Aura's 404-on-everything posture avoids this class entirely.

**RECOMMENDATION: serve the SPA at `/s/{token}`; it fetches `GET /s/{token}/data`.**
- **The static bundle is already public** — `auth.go:167-170`: *"The static bundle is the SAME code for everyone, so gating it would only break the login render without protecting anything."* `webui.IsPublicAsset` already admits it for the login page. **Zero extra asset-gate cost.**
- `/s/{token}` joins `PublicRoute` (`serve_webui.go:524-534` chaining), then `mux.Handle("/", static)` (`:517`) serves the SPA shell.
- **Reuses the 37B renderers verbatim** — D-03 mandates the sandboxed-iframe policy; a Go template would be a second renderer with its own escaping = a redaction/XSS fork (exactly what D-07 forbids).
- Bundle cost: the public page pulls the SPA — already the login page's accepted cost.
- **Blocker to plan for (R-05):** the 37B renderers **hardcode the identity-scoped URL** — `useAssetContent.ts:32` `fetch('/api/assets/${id}/download', {credentials:'same-origin'})`, plus `PreviewModal.tsx:73,101`. See R-05 for the recommended `AssetSourceContext` seam.
- **Page content:** title + snapshot date + a discreet "Condiviso da Aura" mark. **No owner name, no avatar, no identity.** Read-only: no composer, no regenerate, no continue. No "clone" affordance (that is the deferred remix idea).
- **Expired/revoked/unknown → 404 with an identical body for all three.** Distinguishing them is an oracle: "expired" confirms the token *was* valid. One page, one status.

### 5. Revoke / expiry UX

- **Confirmation:** ConfirmDialog for per-row and bulk (open-webui's `ConfirmDialog` for bulk-only is the floor, not the target).
- **After revoke:** the row leaves the list; the thread's `ShareToggle` drops its `data-shared` accent; an `aria-live` announcement. The modal returns to `idle`.
- **Honesty copy (from ChatGPT's FAQ, worth stealing):** *"revoking a link doesn't guarantee complete removal from the internet — the conversation may have already been cached by search engines or saved by viewers."* Aura should say this **at mint time** (in the public warning), not only at revoke — that is when the decision is actually made.
- **Expiry display:** relative (*"scade tra 6 giorni"*) with the absolute date on `title`/tooltip. Expired-but-not-swept rows (the sweep window) must render as **"Scaduto"** and be visually inert — the list reads `expires_at`, never the sweep's stamp.

### 6. Accessibility + i18n

- **Focus management:** modal traps focus, returns it to `ShareToggle` on close. Aura already has this — `Drawer` is described as *"Identical portal/focus-trap/Esc UX to the left nav drawer"* (`ArtifactsShell.tsx:83-85`). Reuse, don't rebuild.
- **Copy confirmation:** `aria-live="polite"` region (or the label swap, which announces naturally). Never a visual-only checkmark.
- **Tier selection:** a real `<fieldset>` + `<legend>` + radio group — not divs with click handlers. The warning block is `aria-describedby`-linked to the public radio so a screen reader hears the warning *as part of* the option, not as orphaned prose.
- **`aria-invalid` on the custom-expiry input: omit when valid** — `cond || undefined` so React drops `aria-invalid="false"` (project rule).
- **The toggle:** `aria-label` + `aria-pressed` if it toggles a panel; if it opens a modal use `aria-haspopup="dialog"` and **no** `aria-pressed` (`ArtifactsToggle` uses `aria-pressed` because it toggles a panel — the share control opens a dialog, so this differs deliberately).
- **i18n:** every string in **both** en and it. **New module `web/src/i18n/resources.share.ts`** exporting `shareEn`/`shareIt`, imported + spread in `resources.ts` (pattern: `resources.ts:1-17`). ~30 keys × 2 languages ≈ 70 LOC — this is **why it needs its own module** (R-03).
- **Public page i18n:** the recipient has no Aura language preference. Fall back to the browser `Accept-Language` → `i18next` detector default. Do **not** persist the owner's language into the snapshot (that is a fingerprinting-adjacent leak and a needless coupling).

---

## Validation Architecture

> **`workflow.nyquist_validation: true`** (`.planning/config.json:20`) — this section is mandatory.

### ⚠️ The finding that drives everything: the existing cross-deny E2E contributes ZERO coverage

`cmd/aura/two_identity_e2e_test.go:1`:
```go
//go:build db_integration && neo4j_integration && garage_integration && authula_integration && musr_e2e
```
Two independent reasons it cannot be 37F's SC4 coverage vehicle:
1. **Tags.** The gate runs **exactly** `db_integration neo4j_integration` (`coverage_gate.sh:25`). The other three tags mean the file **compiles + skips** in CI → **zero** coverage. This is the documented WR-01 failure mode (CLAUDE.md: *"those `//go:build docker_integration` tests compile+skip in CI and contribute ZERO coverage"*).
2. **Package.** The gate measures `./internal/...` only (`coverage_gate.sh:52-53`). **`cmd/aura` is not measured at all**, at any tag.

**⇒ The SC4 cross-identity deny test MUST live in `internal/agui` (or `internal/share`) under `db_integration`.** A `cmd/aura` `musr_e2e` variant is a *supplement* for the live-stack run, never the coverage vehicle.

**This is achievable with no Garage** — `objectstore.NewFake()` (`fake.go:17`) is an in-memory `Store`, and the share service takes the `objectstore.Store` interface. Precedent for the whole shape: `internal/agui/auth_capability_integration_test.go` (a `db_integration` capability test) + `internal/agui/conversations_api_test.go`.

### Test Framework

| Property | Value |
|----------|-------|
| Framework (Go) | stdlib `testing` + `net/http/httptest`; `go.uber.org/goleak`; raw pgx |
| Framework (Web) | vitest + @testing-library/react; Stryker (mutation) |
| Config | `.golangci.yml` (dupl 100, `_test.go` excluded); `web/vitest.config.ts` |
| Quick run (Go) | `go test ./internal/share/ ./internal/agui/` |
| Quick run (race) | `go test -race ./internal/share/ ./internal/agui/` |
| Full matrix | `bash scripts/coverage_docker.sh` (**run locally BEFORE push** — stack up) |
| Coverage gate | `scripts/coverage_gate.sh` — `-tags "db_integration neo4j_integration" -p 1 ./internal/...`, floor **85%** |
| Web gates | `npx vitest run --coverage` (≥85%); `npx stryker run` (≥70%) — **on Windows Git Bash, not WSL (no node)** |

### Coverage floor

**≥85% across the full tag matrix** (CLAUDE.md — overrides PRD ≥75%/≥60%). Owned surface = `./internal/...` minus `internal/db/sqlc/`, `internal/agent/agenttest/`, `internal/llm/client.go` (`coverage_gate.sh:64-67`). Current aggregate **90.3%** — 37F must not drag it under 85, and **every owned package must itself clear 85** (the 2026-06-13 campaign floor).

**Container/daemon-gated 37F code: NONE.** 37F adds no Docker-backed runtime. The only external dependency is Garage (S3), and `objectstore.FakeStore` covers it in-process. **Therefore 100% of 37F's Go surface is reachable under `db_integration` — there is no structural coverage hole.** This must stay true: if any 37F test reaches for a `garage_integration` tag, that code drops out of the floor.

### Daemon-free unit tests required (pure logic — no DB, no Garage)

| Target | Why it must be a plain unit test |
|---|---|
| `share.BuildSnapshot` / redaction | The SC3 core. Must run everywhere, fast, with hostile fixtures. |
| `Snapshot.Markdown()` / `.JSON()` | Pure functions of `Snapshot`. |
| `share.Mint()` / `Hash()` | No I/O. Entropy + hash-stability assertions. |
| `objectstore.ShareSnapshotKey` / `ShareArtifactKey` / `ShareKeyPrefix` | Pure string derivation. Mirrors `TestAssetKeyContainsNoFilename` (`objectstore_test.go:12`). |
| Expiry math (`1d/7d/30d/custom`, cap clamp) | Pure. Table-driven. |
| Tool-name extraction (`ToolCall.Function.Name` only) | Pure projection. |
| `isPublicShareRoute` predicate | Pure request matching. |
| Path-scrub assertions on every output surface | Pure. |

### Phase Requirements → Test Map

| Req | Behavior | Type | Command | Exists? |
|---|---|---|---|---|
| WEBSHARE-01 | Export MD via `GET /api/conversations/{id}/export?format=md` → 200, `Content-Disposition: attachment` | integration | `go test -tags db_integration ./internal/agui/ -run TestShareExportMarkdown` | ❌ Wave 0 |
| WEBSHARE-01 | Export JSON → 200, valid `Snapshot`, round-trips | integration | `… -run TestShareExportJSON` | ❌ Wave 0 |
| WEBSHARE-01 | Export of a **foreign** conversation → **404** (never 403) | integration | `… -run TestShareExportForeignConversation404` | ❌ Wave 0 |
| WEBSHARE-01 | MD/JSON derive from one `Snapshot` (no divergence) | unit | `go test ./internal/share/ -run TestSnapshotFormatsAgree` | ❌ Wave 0 |
| WEBSHARE-02 | Internal link: **no** capability needed; owner mints; any authed identity resolves | integration | `… -run TestShareInternalBearerWithinAuth` | ❌ Wave 0 |
| WEBSHARE-02 | Public mint **without** `share.public` → **403** | integration | `… -run TestSharePublicMintDeniedWithoutCapability` | ❌ Wave 0 |
| WEBSHARE-02 | Public mint **with** `share.public` → 201 + plaintext token once | integration | `… -run TestSharePublicMintWithCapability` | ❌ Wave 0 |
| WEBSHARE-02 | Org kill-switch off → 403 **even with** the capability **and** on loopback (`!SecretConfigured`) | integration | `… -run TestSharePublicOrgKillSwitch` | ❌ Wave 0 |
| WEBSHARE-02 | Public tier is **never** the default (absent tier ⇒ internal) | unit | `go test ./internal/share/ -run TestDefaultTierIsInternal` | ❌ Wave 0 |
| WEBSHARE-02 | Revoke → subsequent resolve **404** | integration | `… -run TestShareRevokeThen404` | ❌ Wave 0 |
| WEBSHARE-02 | Expired token → **404** with the sweep **never run** (lazy gate) | integration | `… -run TestShareExpiredLazy404` | ❌ Wave 0 |
| WEBSHARE-02 | Public mint **without** `expires_at` → rejected (CHECK + Go) | integration | `… -run TestSharePublicRequiresExpiry` | ❌ Wave 0 |
| **WEBSHARE-03** | **SC3: no host path in MD/JSON/page** (hostile fixture) | unit | `go test ./internal/share/ -run TestSnapshotRedactsHostPaths` | ❌ Wave 0 |
| WEBSHARE-03 | `send_file` `{path}` never in any surface | unit | `… -run TestSnapshotStripsSendFilePath` | ❌ Wave 0 |
| WEBSHARE-03 | Tool **names** survive; **args** never do | unit | `… -run TestSnapshotKeepsToolNamesDropsArgs` | ❌ Wave 0 |
| WEBSHARE-03 | `role=tool` turns never reach the snapshot | unit | `… -run TestSnapshotDropsToolRoleTurns` | ❌ Wave 0 |
| WEBSHARE-03 | Spilled turn (`ContentSidecarPath`) leaks no path | unit | `… -run TestSnapshotSpilledTurnNoSidecarPath` | ❌ Wave 0 |
| WEBSHARE-03 | Owner identity id absent from every surface | unit | `… -run TestSnapshotOmitsIdentity` | ❌ Wave 0 |
| WEBSHARE-03 | Every action (create/update/revoke/expire/open) writes `share_audit` | integration | `… -run TestShareAuditLedger` | ❌ Wave 0 |
| WEBSHARE-03 | `share_audit` surfaces in the admin union with `source="share"` | integration | `go test -tags db_integration ./internal/agui/ -run TestAuditUnionIncludesShare` | ❌ Wave 0 |
| WEBSHARE-03 | Public open audits **no recipient PII** (no IP/UA persisted) | integration | `… -run TestSharePublicOpenAuditNoPII` | ❌ Wave 0 |
| **WEBSHARE-04** | **SC4 cross-identity deny** (see below) | integration | `go test -tags db_integration ./internal/agui/ -run TestShareCrossIdentityDeny` | ❌ Wave 0 |
| D-06 | Turns appended after mint do NOT appear on the existing link | integration | `… -run TestShareSnapshotFrozen` | ❌ Wave 0 |
| D-06 | Update re-snapshots, keeps the token, bumps `updated_at` | integration | `… -run TestShareUpdateResnapshot` | ❌ Wave 0 |
| D-09 | Bundled artifact downloads via token; **not** via `/api/assets/{id}/download` | integration | `… -run TestShareBundledArtifactTokenScoped` | ❌ Wave 0 |
| D-09 | Only `source_kind='agent'`, non-deleted/canceled artifacts bundle | unit | `go test ./internal/share/ -run TestBundleFiltersAgentArtifacts` | ❌ Wave 0 |
| D-09 | Revoke drops the Garage bytes (FakeStore `List` → empty) | integration | `… -run TestShareRevokeDropsBlobs` | ❌ Wave 0 |
| D-12 | Share keys never collide with / escape the `identity/` namespace | unit+property | `go test ./internal/objectstore/ -run TestShareKeyNamespaceDisjoint` | ❌ Wave 0 |
| D-15 | `DeleteConversationLifecycle` revokes shares + drops blobs **before** the row delete | integration | `go test -tags db_integration ./internal/runner/ -run TestDeleteLifecycleRevokesShares` | ❌ Wave 0 |
| D-15 | FK `ON DELETE CASCADE` backstops a raw conversation delete | integration | `go test -tags db_integration ./internal/share/ -run TestSharedLinksCascade` | ❌ Wave 0 |
| OQ3 | Sweep expires due links + drops blobs; idempotent on re-run | integration | `go test -tags db_integration ./internal/cron/handlers/ -run TestShareExpirySweep` | ❌ Wave 0 |
| OQ3 | Nil expirer ⇒ disabled no-op, not a panic | unit | `go test ./internal/cron/handlers/ -run TestShareExpiryDisabled` | ❌ Wave 0 |
| D-03 | `/s/{token}` reachable **without** a session; every other route still gated | integration | `go test ./internal/agui/ -run TestPublicShareRouteAllowlist` | ❌ Wave 0 |
| D-03 | Public page HTML artifact ⇒ `sandbox="allow-scripts"`, no `allow-same-origin`, `srcDoc` | web unit | `npx vitest run web/src/share` | ❌ Wave 0 |
| D-05/UI | ShareToggle renders in the cluster; `data-shared` reflects state | web unit | `npx vitest run web/src/shell/ShareShell.test.tsx` | ❌ Wave 0 |
| UI | Modal states; public never preselected; warning only when public | web unit | `npx vitest run web/src/chat/share` | ❌ Wave 0 |
| i18n | Every share key present in **both** en and it | web unit | `npx vitest run web/src/i18n` | ❌ Wave 0 |

### The SC4 cross-identity deny E2E — exact wiring

**Location:** `internal/agui/share_cross_identity_test.go`, `//go:build db_integration`
**Identities:** two throwaway per-run UUIDs seeded into `aura.identities` (harness pattern: `two_identity_e2e_harness_test.go:95-102` — note it seeds `agent.run` via raw INSERT into `capability_grants`; 37F adds `share.public` the same way).
**Object store:** `objectstore.NewFake()` — **no Garage, no `garage_integration` tag** (this is what keeps it inside the coverage gate).

| # | Setup | Act | Assert |
|---|---|---|---|
| 1 | A owns conv-A | B `GET /api/conversations/{conv-A}/export` | **404** (not 403 — reads hide foreign existence, 36 D-06 / `store_identity.go:26-27`) |
| 2 | A owns conv-A | B `POST /api/shares` for conv-A | **404** (B cannot mint a link to A's thread) |
| 3 | A minted an **internal** link | B (authenticated) resolves it | **200** — D-10 bearer-within-auth is *intended*; the redacted snapshot is the protection |
| 4 | A minted an **internal** link | **Anonymous** (no session) resolves it | **401/302** — `RequireAuth` gates it; internal is NOT on the public allowlist |
| 5 | A minted a **public** link | Anonymous resolves | **200** + zero B data + zero paths |
| 6 | A minted a **public** link | B `POST /api/shares/{id}/revoke` | **404** (B cannot revoke A's link) |
| 7 | A minted a **public** link, then revoked | Anonymous resolves | **404** (never a stale render — D-15) |
| 8 | B holds `share.public`; A does **not** | A mints public | **403** |
| 9 | A's public snapshot | Anonymous `GET /s/{token}/asset/{B's assetID}` | **404** — a token scopes to **its** snapshot's artifacts only |
| 10 | A's public link | Anonymous `GET /api/assets/{A's assetID}/download` | **401/302** — the token grants **no** access to the identity-scoped lane (D-09) |

Rows 9 and 10 are the ones a naive implementation fails: 9 catches "token authenticates, then any asset id is fetched"; 10 catches "the public session leaks into the authenticated lane."

### No-skip-as-green

Every 37F integration test reads env through a skip-helper that **`t.Fatal`s when the var is unset AND `$CI` is set**, skipping only locally. Precedent: `musrEnvOrSkip` (`two_identity_e2e_harness_test.go:38-41`).

**Exact env the 37F tests read — the composed DSNs, NOT the `POSTGRES_*` primitives:**
- `AURA_DB_URL` (app role, `aura_app`)
- `AURA_DB_MIGRATE_URL` (DDL role, `aura_migrate`)

37F needs **no** Garage/Authula/Neo4j env (FakeStore + httptest + no graph). **This is a deliberate design property, not an accident** — it is what keeps the whole phase inside the two-tag coverage gate. A sub-second "integration" runtime is a skip tell: verify execution, not just PASS.

### Property-based testing (gopter/rapid — PRD-mandated where indicated)

| Property | Statement |
|---|---|
| **Token opacity/uniqueness** | ∀ n mints: all plaintexts distinct; each decodes to exactly 32 bytes; no two hashes collide; no plaintext is a prefix/substring of another. |
| **Redaction idempotence** | ∀ histories h: `BuildSnapshot(BuildSnapshot(h))` ≡ `BuildSnapshot(h)` (the projection is a fixpoint — a second pass finds nothing to strip). |
| **Redaction totality (the SC3 property)** | ∀ histories h, ∀ secrets s ∈ args ∪ results ∪ sidecar paths: `s ∉ Markdown(BuildSnapshot(h))` ∧ `s ∉ JSON(BuildSnapshot(h))`. This is SC3 stated as a machine-checkable universal. |
| **Serializer round-trip** | ∀ snapshots s: `JSON⁻¹(JSON(s)) ≡ s` (D-07's "lossless structured round-trip"). |
| **Key-namespace disjointness** | ∀ uuids a,b,c: `!strings.HasPrefix(ShareSnapshotKey(a,b), "identity/")` ∧ `!strings.HasPrefix(AssetKey(…), "share/")` ∧ `ShareKeyPrefix(a) ≠ ShareKeyPrefix(b)` for a≠b. |
| **Expiry monotonicity** | ∀ t, ∀ links l: once `resolve(l, t)` = 404 for expiry, `resolve(l, t')` = 404 ∀ t' > t (an expired link never resurrects — guards clock-skew bugs). |

### Mutation testing ≥70% killed

Run in **WSL** (`go-mutesting`, the only fork supporting go1.26). `PASS`=killed, `FAIL`=survived.

**Critical files (spot-check targets):**
1. **`internal/share/redact.go`** — the SC3 core. A surviving mutant here is a live leak. **Non-negotiable.**
2. **`internal/share/snapshot.go`** — the one constructor.
3. **`internal/share/token.go`** — mint/hash.

Command (per CLAUDE.md's container-gated recipe):
```bash
export PATH="$HOME/.local/bin:$HOME/go/bin:$PATH"
GOFLAGS=-tags=db_integration go-mutesting ./internal/share/redact.go ./internal/share/snapshot.go
```
Document the score in `37F-VALIDATION.md`'s Manual-Only table (precedent: db.go 82.8%, budget.go 89.4%). Apply the mutation-autopsy rule: `%w`-dense survivors are near-equivalent — classify, kill via seam, advisory-accept the rest.

### Web-side gates

- **vitest ≥85%** and **Stryker ≥70%** for all new `web/` code (frontend matches the Go floors on this project).
- **Run on Windows Git Bash** — WSL has no node.
- Stryker targets: `web/src/chat/share/*` (modal state machine, tier logic, stale detection), `web/src/shell/ShareShell.tsx`.
- **Rebuild `internal/webui/dist`** before the phase-close push — the CI freshness gate compares dist against source (precedent commit `0d58e6a9f`).
- Playwright E2E against the live container is optional here; the public page is better covered by vitest + the Go route tests.

### Security test angles (the unauthenticated surface)

| Angle | Test |
|---|---|
| **XSS via HTML artifact** | Snapshot with `<script>` in an HTML artifact → the public page renders it in `<iframe srcDoc sandbox="allow-scripts">` with **no** `allow-same-origin`. Assert the attribute string exactly (precedent: `renderers.test.tsx:145`). |
| **XSS via SVG** | `image/svg+xml` ⇒ `previewKind` returns `download`, never `image` (`artifactMeta.ts:54`, T-37B-05). Assert on the public path too. |
| **XSS via snapshot text** | A turn containing `<img src=x onerror=alert(1)>` renders escaped (React default). Assert the DOM has no `<img>`. |
| **Token enumeration** | 1000 random 32-byte tokens → all 404, **identical body and identical status** for unknown/expired/revoked. No oracle. |
| **Timing** | Unknown-token vs valid-token resolve latency: no *structural* early-return before the DB probe (e.g. never `if len(token) != 43 { return fast }` on a path that also does a DB read for valid tokens). The hash-indexed lookup itself is not a timing oracle (SHA-256 preimage resistance — see OQ5's D-13 refinement). |
| **404-never-stale-render** | Revoked and expired both → 404 with **no** snapshot bytes in the response (assert body length + absence of the title string). |
| **Content-type confusion** | Bundled artifact download ⇒ `application/octet-stream` + `X-Content-Type-Options: nosniff` regardless of the asset's real MIME (37A D-10, `assets_api.go:52-54`). |
| **Header injection** | Artifact filename `a"; rm -rf /\r\nX-Evil: 1` ⇒ `contentDisposition` percent-escapes (`content_disposition.go:25-29`). |
| **Cross-lane leak** | The public token grants **zero** access to `/api/*` (SC4 row 10). |
| **Body cap** | `POST /api/shares` bounded by `http.MaxBytesReader` (`assets_api.go:80` precedent). |
| **Token never logged** | Assert the plaintext token appears in **no** `slog` output and in **no** `share_audit` row (D-13). |

### Sampling rate

- **Per task commit:** `go vet ./... && go build ./... && go test -race ./internal/share/ ./internal/agui/`
- **Per wave merge:** `make quality` (vet + build + **file-size** + lint + dupl + race + vuln)
- **Phase gate:** `bash scripts/coverage_docker.sh` **locally, stack up** (a green local full-matrix run beats a push-and-wait CI cycle) → then `make quality-full` → then `/gsd-verify-work`.
- **Before the phase-close push:** update `docs/aura-quality-snapshot.md` for every row whose glob matches a changed file, and verify:
  ```bash
  AURA_QUALITY_CHANGED_FILES="$(git diff --name-only origin/master..HEAD)" \
  AURA_QUALITY_BASE_DATE="$(git log -1 --format=%cs origin/master)" \
  bash scripts/quality_snapshot_gate.sh   # must print: ok: … checked N row(s)
  ```

### Wave 0 gaps

- [ ] `internal/share/` — the whole package (net-new; nothing exists)
- [ ] `internal/share/snapshot_test.go`, `redact_test.go`, `token_test.go`, `markdown_test.go` — pure unit
- [ ] `internal/share/share_property_test.go` — the 6 properties above
- [ ] `internal/share/store_integration_test.go` — `//go:build db_integration`
- [ ] `internal/agui/share_api_test.go` — `//go:build db_integration`
- [ ] `internal/agui/share_cross_identity_test.go` — **SC4**, `//go:build db_integration`
- [ ] `internal/agui/share_public_route_test.go` — allowlist, plain unit
- [ ] `internal/objectstore/share_key_test.go` — namespace disjointness
- [ ] `internal/cron/handlers/share_expiry_test.go` — sweep + disabled no-op
- [ ] `internal/runner/runner_delete_share_test.go` — D-15 cascade, `//go:build db_integration`
- [ ] `web/src/chat/share/*.test.tsx`, `web/src/shell/ShareShell.test.tsx`
- [ ] `web/src/i18n/resources.share.ts` + its key-parity test
- [ ] Framework install: **none** — every framework is present.

---

## Don't Hand-Roll

| Problem | Don't build | Use instead | Why |
|---|---|---|---|
| Opaque token | A custom RNG/alphabet | `crypto/rand.Read(32)` + `base64.RawURLEncoding` | 256-bit (D-13); URL-safe with no padding; stdlib. |
| Token hashing | bcrypt/argon2 | `crypto/sha256.Sum256` | D-13 says SHA-256. A 256-bit random token has no brute-force surface — a slow KDF buys nothing and costs per-request latency. (This is why session/API-key discipline ≠ password discipline.) |
| Secret KDF | Bespoke derivation | `crypto/hkdf.Key` | Precedent `identity_store.go:203`, domain-separated info label. |
| `Content-Disposition` | String concat | `agui.contentDisposition` (`content_disposition.go:23`) | RFC-6266 + `url.PathEscape` header-injection guard + diacritic ASCII fallback. Already tested. |
| HTML artifact isolation | A sanitizer (DOMPurify etc.) | Null-origin `<iframe srcDoc sandbox="allow-scripts">` | 37B D-07 / `HtmlPreview.tsx:4-11`. Sanitizers are a bypass treadmill; a null origin is structural. |
| Expiry sweep | A bespoke goroutine | `newCountingSweep` + a scheduler kind | `sweep.go:36`; 0033/0034 precedent. A goroutine has no leader election, no recovery, no audit. |
| shared_links access control | Casbin / a policy engine | `RequireCapability` + `*ForIdentity` + RLS 0032 | 36 deferred a real authz engine. Three layers already exist. |
| Owner-scoped queries | Raw SQL with a hand-written WHERE | `db.WithIdentityTx` + `*ForIdentity` | RLS 0032 backstops a forgotten predicate (`store_identity.go:14-19`). |
| Audit ledger | A new admin UI | A 4th UNION ALL leg in `auditActivityQuery` | `audit_store.go:53`; the admin UI is free. |
| Object storage | A new S3 client | `objectstore.Store` (+ `FakeStore` in tests) | `types.go:51`; the Garage checksum footgun is already solved (`s3.go:100-105`). |
| Clipboard copy | `writeText` after an `await` | Mint → render URL → separate copy gesture | open-webui's Safari `ClipboardItem` workaround (`ShareChatModal.svelte:190-208`) exists because the await kills the gesture. Restructure instead of working around. |
| Modal focus trap | A new dialog | The existing `Drawer`/modal primitives | *"Identical portal/focus-trap/Esc UX"* (`ArtifactsShell.tsx:83-85`). |

**Key insight:** 37F's genuinely new code is *only* the redaction projection, the token lifecycle, and the share modal. Everything else is composition of shipped, tested seams. Any task that reimplements one of the rows above is a scope leak.

---

## Risks & Landmines

### R-01 — `cmd/aura/serve_webui.go` is at **593/600 LOC** — WILL BREACH
**Impact:** `make quality` (`scripts/check-file-size.sh`, cap 600) fails pre-push **and** blocks *every* commit (the hook scans the whole tree).
**37F needs there:** `sharePublicCapability` const, `isPublicShareRoute`, the share route mounts, the `PublicRoute` chain entry. ≈ 25-40 LOC. **7 LOC of headroom.**
**Mitigation:** a new `cmd/aura/serve_webui_share.go`. **The precedent is explicit** — `serve_webui.go:507-509` says *"lives in serve_webui_composer.go **to keep this file under the 600-LOC ceiling**"*. Existing splits: `serve_webui_composer.go`, `serve_webui_musr.go`, `serve_webui_voice.go`, `serve_webui_auth_config.go`. The `PublicRoute` chain (`:523-534`) is designed for it (`previousPublicRoute` composition).

### R-02 — `web/src/AppShell.tsx` is at **591/600 LOC** — WILL BREACH
**37F needs there:** mount `ShareToggle` in the cluster (`:514-517`) + share-modal state + handlers. ≈ 12-20 LOC. **9 LOC of headroom.**
**Mitigation:** put the state in a hook (`web/src/shell/useSharePanel.ts`, mirroring `useArtifactsPanel`) and the presentation in `web/src/shell/ShareShell.tsx` (mirroring `ArtifactsShell.tsx`). AppShell's delta then ≈ 4 LOC (one import, one hook call, one element) → 595. **Still only 5 LOC of headroom — flag AppShell for a refactor-on-touch split in this phase** (CLAUDE.md: "DEEP REFACTOR ON TOUCH ... LOC ≤600 in the SAME commit").

### R-03 — `web/src/i18n/resources.ts` is at **576/600 LOC**
**37F needs:** ~30 keys × 2 languages ≈ 70 LOC. **24 LOC of headroom.**
**Mitigation:** `web/src/i18n/resources.share.ts` exporting `shareEn`/`shareIt`; `resources.ts` gains 1 import + 2 spreads ≈ 4 LOC → 580. Pattern already established for 11 domains (`resources.ts:1-17`).

### R-04 — CONTEXT.md D-11's migration slot (0036) is **STALE** — 37F is **0040**
0036–0039 shipped with Phase 42 on 2026-07-14, one day after CONTEXT.md was gathered. A blind `0036_shared_links.up.sql` **collides with `0036_compaction_checkpoints`** → golang-migrate dirties the tracker → **every subsequent migration is blocked** (the exact failure `0026`'s guard comment describes). **Re-verify `ls internal/db/migrations/ | tail -1` at plan time and again at execute time** — this project runs multiple phases in flight.

### R-05 — The 37B renderers hardcode the identity-scoped asset URL
`useAssetContent.ts:32` `fetch('/api/assets/${id}/download', {credentials:'same-origin'})`; `PreviewModal.tsx:73,101`; `useBlobPreview.ts` (same family). D-03/D-09 require the public page to reuse these renderers **but** resolve bytes through `/s/{token}/asset/{id}`.
**Mitigation:** an `AssetSourceContext` providing `assetUrl(assetId) => string`, defaulting to `/api/assets/{id}/download` so **every existing call site and test stays byte-identical**; the public page wraps its tree in a provider returning the token-scoped URL. Threading a prop through 6 lazy renderer chunks is churn; context with a default is the "extract a helper, never duplicate" answer. **Also drop `credentials: 'same-origin'`** on the public path — the recipient has no session and sending cookies to an unauthenticated route is needless.

### R-06 — D-08's "reasoning/thinking traces" is **structurally impossible**
**Verified absent, three ways:** `aura.conversation_turns` has no reasoning column (`0005_conversations.up.sql:23-36`); `llm.Message` has no reasoning field (`client.go:24-30`); `llm.Chunk.Reasoning` is **stream-only** (`client.go:79`). The only "reasoning" at rest is `metadata.reasoning_effort` — the *setting* (`conversations.sql:100-112`), not the trace. `internal/reasoningtrace` is an **operator debug JSONL to disk** (`reasoningtrace.go:19-21`), not conversation data — and exporting *that* would leak host paths and prompts.
**Options:** (a) **RECOMMENDED — drop reasoning from the snapshot; PRD-amend D-08.** No reference product ships CoT in a share (Claude explicitly keeps tool-call data private; open-webui stores only the chat JSON). (b) Add reasoning persistence — a new migration + agent write-path change + a *privacy regression* (CoT at rest, then exported). This is a phase of its own and contradicts the phase's own redaction posture.
**This must be decided before planning** — it changes the `SnapshotTurn` shape and the acceptance criteria.

### R-07 — SC4 in `cmd/aura` with the 5-tag combo contributes **ZERO coverage** → CI fails ~20 min after push
See Validation Architecture. Two independent causes (tags **and** package). The fix is structural: SC4 lives in `internal/agui` under `db_integration` with `objectstore.NewFake()`.

### R-08 — `RequireCapability` is a **pass-through** when `!SecretConfigured`
`auth.go:282`: `if !deps.SecretConfigured { return next }`. On loopback dev the `share.public` gate **does not exist**. Safe for `governance.write` (loopback = the operator's own box) but 37F mints links intended to leave the box.
**Mitigation:** check the **org kill-switch inside the handler**, not only at the mount. Two gates, both fail-closed, one of which survives loopback.

### R-09 — Redaction on the *authenticated* side is client-side today
`sseAdapter.ts:346-360` strips `path` **in the browser**; the backend ships it (`Actions.ArtifactDelta map[string]any`, `event.go:72`). A share recipient's browser is not a trust boundary. **The Go serializer must implement its own allowlist and must be tested with hostile fixtures.** Do **not** assume "37A already strips paths."

### R-10 — Garage bytes are NOT dropped by the FK cascade
`shared_links.conversation_id ON DELETE CASCADE` removes the **row**; the Garage objects survive. If the D-15 lifecycle hook is skipped (or a raw `DELETE FROM aura.conversations` runs), the blobs are **orphaned forever** with no row pointing at them — unreclaimable even by the sweep (which scans rows). open-webui hit the mirror image: *"Unsharing cleans up orphaned rows"* (`CHANGELOG.md:238`).
**Mitigation:** the lifecycle hook is mandatory (D-15), **and** the sweep should also reconcile `List(prefix="share/")` against live rows periodically. At minimum, document the orphan path.

### R-11 — Bundled artifacts must be a **copy**, not a reference (open-webui shipped this bug)
`CHANGELOG.md:331`: *"**Shared-chat file write protection.** Access to a file through a shared chat now only grants **read** access, so users who can read a shared chat can no longer **modify or delete** files attached to it."* They granted **write** through a share link.
**Mitigation:** D-09's copy-on-share is the structural answer — the recipient's token addresses `share/{id}/…` blobs and has **no** path to `identity/{owner}/asset/…`. SC4 rows 9 and 10 test exactly this. Never "resolve the share's asset_id through `assets.Service`."

### R-12 — Claude excludes **user attachments** from shares; Aura must match
Claude: *"If you share a chat containing an attached file, the file itself is **not included** in the shared snapshot and remains private."* Only *artifacts* travel.
**Aura already has the right filter** — `selectAgentArtifacts` (`useThreadArtifacts.ts:35`) keeps `source_kind === 'agent'` and drops deleted/canceled. **Mirror it server-side.** Bundling `source_kind='web'` (the user's own uploads — possibly a passport scan) into a public link would be a serious privacy failure that D-09's wording ("delivered artifacts") does not obviously forbid. **Make it explicit in the plan.**

### R-13 — `local` holds the `*` wildcard → `share.public` auto-passes for the operator
`capability_grants.sql:22` `(capability = '*' OR capability = $2)`; `0004` seeds `local` with `*`. Correct (the operator is the admin) but the **cross-identity tests must use provisioned non-wildcard identities**, or every capability assertion passes vacuously. Note also `serve_auth.go:199-261` "retire legacy local identity" **migrates local's caps to the first real user** — verify whether that carries `*` into a real user; if so, that user auto-holds `share.public`. **OPEN — verify at plan time.**

### R-14 — `.env` leak breaks `make coverage`
`.env` exports `AURA_WEB_AUTH_SECRET`, which flips `SecretConfigured` and breaks config tests. **Unset before `make coverage`** (known project footgun).

### R-15 — Coverage gate against the live DB
`scripts/coverage_gate.sh:35` refuses `db_integration` against a DB named `aura` when run locally. Use `scripts/coverage_docker.sh` (provisions a disposable `aura_cov`). This closed a 2026-07-10 footgun that **wiped the live deployment's auth tables**. Do not bypass.

---

## State of the Art

| Old approach | Current approach | Evidence |
|---|---|---|
| Share link = plaintext token in a URL, stored plaintext | Hash the token at rest, index the hash | open-webui still stores plaintext (`shared_chats.py:21`) — Aura's D-13 is ahead of the reference |
| "Public link" = anonymous | **open-webui's "public" is `user:*` = any *authenticated* user** (`access_grants.py:509-521` + `get_verified_user` on `chats.py:1048`) | open-webui's share is **not anonymous** — Aura's tier (c) goes *beyond* it, which is exactly why the fail-closed stack (capability + expiry + revoke + hashed token + redaction) is load-bearing |
| Share = live view | **Static snapshot at creation + explicit Update** | Unanimous: open-webui (`shared_chats.py:26,90-110`), ChatGPT (*"a snapshot … not automatically added"*), Claude (*"All messages sent after sharing remain private"*) |
| Tool-call data travels with the share | **Tool-call payloads stay private** | Claude: *"the raw data retrieved from MCP tool calls remains hidden in the shared snapshot"* — validates D-08 |
| Public sharing = an admin role | **A dedicated per-user permission, default off** | open-webui `config.py:1928` + `:1839-1841` (`False`); `CHANGELOG.md:461`; Claude's "External sharing" toggle — validates OQ2 |
| Export gated like sharing | **Export on by default, sharing off by default** | `USER_PERMISSIONS_CHAT_EXPORT` = `True` vs `..._ALLOW_PUBLIC_SHARING` = `False` (`config.py:1843` / `:1839`) — validates D-01(a) vs D-01(c) |
| Delete leaves shares live | **Delete cascades revoke** | open-webui `ondelete='CASCADE'` (`shared_chats.py:23`); ChatGPT + Claude both — validates D-15 |
| Per-row revoke only | **+ bulk "Unshare All"** | `CHANGELOG.md:113` — a post-hoc addition worth shipping up front |

**Deprecated / outdated:**
- **CONTEXT.md D-11's "0036 is the next free slot"** — superseded by Phase 42 (0036-0039). → **0040**.
- **CONTEXT.md D-08's "reasoning/thinking traces"** — not persisted; not producible. → PRD-amend.
- **CONTEXT.md D-05's "thread-header"** — no thread header exists; the seam is the floating cluster, reserved by 37B.
- **D-13's literal "constant-time compare on lookup"** — the correct form is hash-indexed equality (see OQ5).

---

## Assumptions Log

| # | Claim | Section | Risk if wrong |
|---|---|---|---|
| A1 | An S3-public Garage bucket is the wrong reading of D-12's "public-readable"; Aura streams bytes instead | OQ1 | If the intent really was S3-public, revoke cannot drop access to already-issued URLs → D-09/D-15 unmet. **Recommend the PRD-amendment state this explicitly.** |
| A2 | The org kill-switch belongs in `aura.settings` (allowlist-validated) | OQ2 | Only the *mechanism* is assumed; D-02 locks the semantics. If the allowlist rejects a non-model key (`settings_api.go:13-15` says the allowlist "already excludes connection/security env"), an env-only switch is the fallback. **VERIFY the allowlist accepts a share key at plan time.** |
| A3 | `share.public` passes `ValidateCapabilityName` | OQ2 | Verified against the regex by inspection (`^[a-z][a-z0-9._-]{0,63}$`), not by execution. A one-line unit test settles it. |
| A4 | Reasoning is *nowhere* persisted | R-06 | Verified across migrations, `llm.Message`, sqlc queries, and `content_parts`. If a persistence path exists somewhere unsearched, D-08 becomes feasible. Confidence HIGH, not absolute. |
| A5 | The "retire legacy local identity" flow may carry `*` to the first real user | R-13 | If it does, that user auto-holds `share.public`. **OPEN — read `serve_auth.go:199-261` at plan time.** |
| A6 | LOC figures are HEAD `1a3252e64` | R-01..R-03 | Files may drift. **Re-measure at plan time**; the margins are 7/9/24 LOC. |

---

## Open Questions

1. **D-08 reasoning traces — the blocking one.**
   - Known: not persisted, in any form (R-06, verified 3 ways).
   - Unclear: whether the operator wants to (a) drop them or (b) fund persistence.
   - **Recommendation: (a) drop + PRD-amend.** No reference product ships CoT in a share; (b) is a privacy regression inside a privacy phase and a scope explosion.

2. **Org kill-switch mechanism (A2).**
   - Known: semantics locked; `aura.settings` exists and is `governance.write`-gated.
   - Unclear: whether the settings allowlist admits a non-model key.
   - **Recommendation:** verify the allowlist; if it refuses, use `AURA_SHARE_PUBLIC_ENABLED` (env) — the convention already carries third-party exceptions and `AURA_<DOMAIN>_<UNIT>` fits.

3. **Custom-expiry max cap (D-04) — the number is unspecified.**
   - Known: default 7d; 1d/7d/30d/custom "up to a max cap."
   - Unclear: the cap.
   - **Recommendation: 90 days**, as `AURA_SHARE_MAX_EXPIRY_DAYS`. Bounded, operator-tunable, well under "effectively permanent." Needs operator confirmation (it is a policy choice, not a technical one).

4. **Internal-tier link format.**
   - Known: no `token_hash` (D-11), bearer-within-auth (D-10).
   - Unclear: what the URL *is*. If it is `/api/shares/{id}` with a UUID, D-03's "no enumerable IDs" is arguably weakened even behind auth.
   - **Recommendation:** internal links also carry an opaque token (hashed), and D-11's CHECK becomes `token_hash IS NOT NULL` for **both** tiers, with `expires_at` mandatory only for public. This costs nothing, removes the enumerable-id question, and unifies `ResolveByToken`. **This deviates from D-11's "public only" wording — planner/PRD call.**

5. **Does `retireLocalIdentity` migrate the `*` wildcard? (A5/R-13.)**
   - **Recommendation:** read `cmd/aura/serve_auth.go:199-261` during planning. If `*` transfers, the first real user silently holds `share.public` — which may be intended (they are the operator) but must be a *decision*, not an accident.

---

## Environment Availability

| Dependency | Required by | Available | Version | Fallback |
|---|---|---|---|---|
| Postgres | `shared_links`, `share_audit`, all `db_integration` | ✓ | 5432, live | — |
| Garage (S3) | Snapshot + artifact blobs (D-12) | ✓ | live | **`objectstore.FakeStore`** for all tests — this is what keeps 37F inside the 2-tag coverage gate |
| Neo4j | — (37F touches no graph) | ✓ | 7687 | Tag needed only because the gate runs it globally |
| Go toolchain (WSL) | build, race, mutation | ✓ | go1.26, gcc 15, CGO_ENABLED=1 | — |
| `go-mutesting` | Mutation ≥70% | ✓ | WSL `~/go/bin` (only go1.26-capable fork) | — |
| `golangci-lint` | `make quality` | ✓ | v2.12.2 (CI-pinned) | — |
| node/npm | vitest, Stryker, tsc, prettier | ✓ | **Windows only — NOT in WSL** | — |
| Playwright | Optional public-page E2E | ✓ | `./node_modules/.bin/playwright` | vitest + Go route tests suffice |
| Authula | Not needed (37F tests use `withPrincipal` directly) | ✓ | — | — |

**Missing dependencies with no fallback:** none.
**Notable:** 37F requires **no new dependency, no new sidecar, no new package**. The one net-new Go package (`internal/share`) is stdlib + pgx + the existing `objectstore`/`conversations`/`identity` seams.

---

## Sources

### Primary — HIGH confidence (read on disk / from source)
- **Aura codebase @ HEAD `1a3252e64`** — every file:line in §Code Seams and §Redaction Inventory was opened directly.
- **open-webui `0.10.2`, commit `ecd48e2f718220a6400ecf49eafd4867a38feb10` (2026-07-01)** — sparse clone, read from source:
  - `backend/open_webui/models/shared_chats.py` (schema, create/update/get_by_chat_id/get_by_user_id/delete)
  - `backend/open_webui/routers/chats.py:481-486, 530-535, 1046-1079` (permission gate; `get_shared_chat_by_id` auth posture)
  - `backend/open_webui/models/access_grants.py:497-527` (`has_access`; `user:*` = public)
  - `backend/open_webui/config.py:1839-1843, 1922-1930` (`USER_PERMISSIONS_CHAT_ALLOW_PUBLIC_SHARING=False`, `..._CHAT_EXPORT=True`)
  - `src/lib/components/chat/ShareChatModal.svelte` (full modal IA, Safari clipboard workaround)
  - `src/routes/s/[id]/+page.svelte` (public page, `readOnly` Messages, owner-PII fetch, `goto('/')` on miss)
  - `src/lib/components/layout/SharedChatsModal.svelte` + `src/lib/components/chat/Settings/DataControls.svelte` (management view, Unshare All)
  - `CHANGELOG.md:51, 57, 113, 163, 238, 331, 429, 461` (shipped share behaviors + the bugs they fixed)
- `.planning/phases/37F-.../37F-CONTEXT.md`, `.planning/REQUIREMENTS.md:99-106`, `.planning/ROADMAP.md:587-604`, `CLAUDE.md`, `.planning/config.json`

### Secondary — MEDIUM confidence (official docs, WebSearch-verified)
- [ChatGPT Shared Links FAQ](https://help.openai.com/en/articles/7925741-chatgpt-shared-links-faq) — snapshot semantics, Data controls → Shared links → Revoke, the "cached by search engines" honesty note. *(Direct fetch 403'd; content via WebSearch summary of the same OpenAI Help page.)*
- [Where do I access my settings to see my shared links?](https://help.openai.com/en/articles/7943621-where-do-i-access-my-settings-to-see-my-shared-links) — Settings → Data Controls → Shared links → Manage.
- [Claude — Share and unshare chats](https://support.claude.com/en/articles/10593882-share-and-unshare-chats) — snapshot incl. artifacts; **attached files excluded**; MCP tool-call data private; Team/Enterprise org-only; unshare via the visibility dropdown.
- [Claude — Publish and share artifacts](https://support.claude.com/en/articles/9547008-publish-and-share-artifacts) — public sharing off by default; Owner enables "External sharing."

### Tertiary — LOW confidence (marked, not relied upon)
- None. Every claim in this document traces to source code or official documentation.

---

## Metadata

**Confidence breakdown:**
- **Code seams:** HIGH — every file:line opened at HEAD `1a3252e64`; zero inferred APIs.
- **Redaction inventory:** HIGH — all 9 leaks traced to a struct field or column on disk; L-03's client-side-strip finding verified through the full chain (`send_file.go:186` → `event.go:72` → `translator.go:19` → `sseAdapter.ts:346`).
- **OQ1 (key derivation):** HIGH on the constraint (`Resolve(ctx)` + `isShared("")` read directly); MEDIUM on the bucket choice (a judgement call — the disjoint prefix makes it reversible).
- **OQ2 (capability name):** HIGH — schema, regex, four precedent names, the 0026 rationale, **and** independent open-webui convergence all read from source.
- **OQ3 (expiry):** HIGH — sweep precedent (`sweep.go`, 0033/0034) and the lazy-gate argument are both structural.
- **OQ4 (serializer):** HIGH on the shape; MEDIUM on the LOC estimates (±20%).
- **OQ5 (schema):** HIGH — slot verified by `ls` + `git log`; audit-family column types verified in the migrations.
- **UI/UX:** HIGH — open-webui read from source, not docs; Aura components read directly; 37B's reserved-spot comment is dispositive on placement.
- **Validation architecture:** HIGH — `coverage_gate.sh` and the 5-tag E2E build constraint both read directly.
- **R-06 (reasoning absent):** HIGH — verified across migrations, `llm.Message`, sqlc queries, and `content_parts`. Not absolute (a negative claim), but four independent checks agree.

**Research date:** 2026-07-15
**Valid until:** 2026-08-14 (30 days) for the Aura seams — **EXCEPT the migration slot (R-04), which must be re-verified at plan time and again at execute time.** open-webui moves fast (0.10.2 was 2 weeks old at research time); its UX findings are stable, its API details less so.
