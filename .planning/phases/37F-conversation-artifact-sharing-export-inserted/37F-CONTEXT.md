# Phase 37F: Conversation & Artifact Sharing / Export (INSERTED) - Context

**Gathered:** 2026-07-13
**Status:** Ready for planning

<domain>
## Phase Boundary

The owner of a conversation can (1) **export** the thread as a downloadable file
(Markdown/JSON) via an identity-scoped endpoint, and (2) create a **share link** to that
conversation that respects Aura's MUSR identity isolation. Three delivery tiers ship,
**fail-closed**: file export (always), an **internal-identity revocable** link
(bearer-within-auth), and a **public opt-in expiring opaque token** (never default). The
share act is audited; **no host/container path and no other identity's data ever reaches a
recipient**. This also builds the **"Condiviso" section + share-arrow** that Phase 37B
explicitly deferred here.

**In scope:** whole-conversation export (MD + JSON) via an identity-scoped endpoint; the
three share tiers; a redacted **static snapshot** model (frozen at creation + "Update");
the `shared_links` table (migration 0036) + token-scoped Garage snapshot/artifact blobs; a
dedicated `share_audit` ledger; the rendered read-only public page at `/s/{token}`; the
web share affordance (thread-header share-arrow + "Condiviso" section in the ArtifactsPanel);
revoke + expiry + the revoke-on-conversation-delete cascade; the cross-identity deny E2E.

**Out of scope (→ deferred / other phases):** standalone **single-artifact** share links
and **single-message** share links (deferred — per-artifact *download* already exists from
37A/37B); explicit **per-recipient-identity** internal grants + recipient-picker UI
(deferred, D-10 ships bearer-within-auth); remix/re-import of a shared JSON; a real authz
engine (Casbin, Phase 36 deferred). Requirements are WEBSHARE-01..04 (LOCKED — this is HOW,
not WHETHER).
</domain>

<decisions>
## Implementation Decisions

Requirements WEBSHARE-01..04 are locked (see canonical refs). These are the HOW decisions
resolved in discussion, grounded in a user-directed industrial-pattern research pass
(open-webui, ChatGPT, Claude, LibreChat) and the shipped 37A/37B/36 seams.

### Share tiers & security posture (WEBSHARE-02 — likely ADR)
- **D-01 — All three tiers ship, fail-closed.** (a) File export always available; (b) an
  **internal-identity revocable** link is the default "Condividi"; (c) a **public opt-in
  expiring opaque token**, never default, behind an explicit warning. Max Claude/ChatGPT/
  open-webui parity while the public surface stays strictly fail-closed (the "premium bar"
  cockpit posture + the roadmap's fail-closed mandate).
- **D-02 — Public-link minting is capability-gated: off by default per user, admin-grantable,
  org kill-switch.** Reached via the existing `RequireCapability`/`HasCapability` interface +
  `capability_grants` seam (36 D-01/D-04). **Locked semantics:** per-user grantable, off by
  default, an admin can disable public sharing org-wide. **OPEN (planner/PRD call):** the
  capability *name* — the codebase's RESEARCH-OQ3 precedent reuses `governance.write` and
  avoids net-new capability names (`internal/agui/audit_api.go`, `shell_bg_owner.go`), but
  reusing `governance.write` collapses "may share publicly" into "is a full admin," which
  contradicts the per-user-grant intent. A distinct `share.public` best matches D-02's intent;
  if net-new names are rejected, `governance.write` is the fallback (with that caveat).
  **Internal-identity links need NO capability** — any owner shares their own thread internally.
- **D-03 — Public recipient sees a rendered read-only page at `/s/{token}`** (Claude/ChatGPT/
  open-webui convergence). This is a **new unauthenticated HTML surface** → it MUST inherit
  37A/37B XSS discipline: render ONLY the redacted snapshot, never tool-call payloads; HTML
  artifacts in a sandboxed `<iframe srcdoc>` (`sandbox="allow-scripts"`, no `allow-same-origin`
  — 37B D-07); SVG download-only; no enumerable IDs.
- **D-04 — Public token mandatory expiry.** Default **7 days**; owner-selectable (1d/7d/30d/
  custom up to a max cap). Revoke is always available and independent of expiry. Fail-closed if
  the owner forgets to revoke.

### Share granularity (fork b)
- **D-05 — Whole-conversation only.** Single-artifact and single-message share links are OUT
  (deferred). Per-artifact *download* already exists (37A/37B); a shared conversation still
  carries its artifacts (D-09). The 37B-deferred "Condiviso" section + share-arrow operate at
  **thread level** (thread-header share-arrow + a "Condiviso" section listing active shares in
  `ArtifactsPanel.tsx`).
- **D-06 — A shared conversation is a STATIC SNAPSHOT frozen at creation + an owner "Update"
  button** to refresh to a newer snapshot (Claude/ChatGPT/open-webui). Turns added after
  sharing NEVER retroactively appear on an existing link — the core SC3 leak-prevention. Not a
  live view.

### Export content & redaction (WEBSHARE-01, SC3)
- **D-07 — Export in BOTH Markdown and JSON.** MD = human-readable (per-turn headings, fenced
  code); JSON = lossless structured round-trip. Owner picks format at export time. The rendered
  public page AND both file formats all derive from **ONE canonical redacted snapshot model**
  (single serializer core + format adapters), so redaction can't diverge between surfaces.
- **D-08 — Snapshot/export content:** visible user+assistant text (baseline) + delivered
  artifacts (D-09) + **tool-call provenance (tool NAMES only, per turn)** + reasoning/thinking
  traces. **HARD SC3 redaction (non-negotiable, NOT a toggle):** raw tool-call
  arguments/results and any host/container filesystem path are ALWAYS stripped. Concrete leak
  sources to scrub: the `send_file` artifact descriptor `{path}` (host/container path),
  `aura.tool_invocations` args/results, and any other-identity data. The snapshot carries
  `asset_id`/`filename` only, never the raw path chip (37A D-13 already replaces it in the live
  UI — the exporter must do the same).
- **D-09 — Delivered artifacts travel with the share; PUBLIC = bundled token-scoped.** Artifact
  bytes are copied into a **token-scoped, public-readable snapshot store**; the recipient
  downloads via the token, NEVER via the identity-scoped `/api/assets/{id}/download`. Served
  `Content-Disposition: attachment` + `application/octet-stream` (37A D-10); HTML previews
  sandboxed (D-03). Revoke/expiry drops the bundled copy. Internal (bearer-within-auth) shares
  resolve artifacts via the same snapshot.

### Internal-identity link semantics (WEBSHARE-02 branch a)
- **D-10 — Internal link = bearer-within-auth.** ANY authenticated Aura identity holding the
  link can open the already-redacted snapshot — being logged into Aura is the gate, the link is
  the capability (open-webui "private share"). No recipient picker, no per-recipient grant rows
  (minimal industrial). Revocable + optional expiry. Because the snapshot is redacted (D-08), a
  bearer never sees more than the owner chose to share. Explicit per-identity grants +
  admin-listable-only variant = deferred.

### Storage & data model (fork c)
- **D-11 — New `shared_links` table, migration 0036** (next free slot; 0035
  `assets_source_kind_agent` is latest on disk). Columns (planner refines): `id`,
  `owner_identity_id`, `conversation_id`, `tier` (internal|public), `token_hash` (public only),
  snapshot pointer, format/options, `expires_at`, `revoked_at`, `created_at`, `updated_at` (for
  "Update"). Table = metadata + lifecycle. Reuse-assets-only REJECTED (assets can't cleanly
  express tier/expiry/revocation/capability-gating without bolting columns on anyway).
- **D-12 — Snapshot content + bundled artifact bytes = TOKEN-SCOPED Garage blobs** (reuses 37A
  `internal/objectstore`), NOT the identity-scoped `AssetKey(identityID, assetID)`. A new
  token-scoped key/bucket derivation is needed: public blobs must be reachable **without an
  identity principal**, yet remain opaque + revocable. The `shared_links` row points at the
  blob. Small serialized MD/JSON MAY be jsonb, but artifacts MUST be blobs — keep bytes in
  Garage for one consistent path.
- **D-13 — Public token = 256-bit opaque random, stored HASHED at rest** (SHA-256),
  constant-time compare on lookup, never logged. A DB/backup leak never exposes live links
  (session-token discipline). Opaque → no enumerable IDs. Plaintext shown to the owner once at
  creation; it lives only in the URL.
- **D-14 — Share act audited via a dedicated identity-keyed `share_audit` ledger** capturing
  **create / update-snapshot / revoke / expire / public-access(open)** events. Joins the
  existing audit family (`aura.mcp_audit` + `aura.skill_audit` + `aura.tool_invocations`,
  unioned in `internal/agui/audit_store.go`) and surfaces in the existing admin audit UI
  (`internal/agui/audit_api.go`, 36 D-28). Public-link opens are audited (timestamp + coarse
  info, **NO recipient PII**). Fold-into-`tool_invocations` REJECTED (share events aren't tool
  calls; semantically muddy).

### Lifecycle & cascade (fix-on-touch)
- **D-15 — Revoke-on-delete cascade + expiry sweep.** Deleting a conversation MUST revoke +
  drop all its shares (open-webui/ChatGPT parity). Route through the existing runner-lifecycle
  deletion `Runner.DeleteConversationLifecycle` (`internal/runner/runner_delete.go:38`, 36 D-22
  / MUSR-05): revoke `shared_links` + delete token-scoped Garage blobs BEFORE the persistence
  delete. Expired/revoked token → **404, never a stale render**. Expiry enforcement =
  scheduler sweep (mirrors the 0033/0034 scheduler-kind pattern) and/or lazy-on-access —
  planner's call, fail-closed either way.

### Claude's Discretion
- Exact `shared_links` / `share_audit` column set + indexes; whether `share_audit` is its own
  migration or shares 0036.
- The capability NAME for public-share gating (D-02) — net-new `share.public` vs reuse
  `governance.write`; the per-user-grantable / off-by-default / org-disableable SEMANTICS are
  locked, only the name is open.
- Token-scoped Garage key/bucket derivation (D-12); expiry sweep-vs-lazy (D-15).
- One serializer core + format adapters vs two writers for MD/JSON (D-07 prefers one core).
- The share-modal UX + "Condiviso" section layout — candidate for `/gsd-ui-phase 37F`.
</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Requirements & roadmap (locked)
- `.planning/ROADMAP.md` §"Phase 37F: Conversation & Artifact Sharing / Export (INSERTED)"
  (lines 587-604) — goal, success criteria SC1-4, the three design forks (scope/granularity/
  link storage), the security note, and the PRD-amendment + ADR requirement.
- `.planning/REQUIREMENTS.md` §"Conversation & Artifact Sharing / Export (WEBSHARE)"
  (lines 99-106) — WEBSHARE-01..04 acceptance text (locked).

### Dependency contexts (the seams this phase builds on)
- `.planning/phases/37A-web-artifact-delivery-lane/37A-CONTEXT.md` — the asset delivery lane:
  `assets.Service` ingest, `GET /api/assets/{id}/download`, `GetForIdentity` 404-on-miss
  ownership, `Content-Disposition: attachment` + `octet-stream` XSS guard (D-10), RFC-6266
  filenames (D-11), migration numbering.
- `.planning/phases/37B-web-artifact-sidebar/37B-CONTEXT.md` — the ArtifactsPanel; §Out-of-scope
  explicitly defers the **"Condiviso" section + share-arrow to Phase 37F**; D-07 sandboxed-
  iframe HTML render policy (reuse for the public page).
- `.planning/phases/36-multi-user-identity-isolation-authula-cutover/36-CONTEXT.md` — MUSR
  isolation: 404-read/403-mutate (D-06), `RequireCapability`/`HasCapability` interface
  (D-01/D-04), identity-keyed audit tables + admin audit UI (D-28), runner-lifecycle deletion
  (D-22), token/ID discipline (D-17).

### Code seams (owner-scoping, routes, export, audit)
- `internal/conversations/store.go` — `Turn` struct (`:118`), `LoadHistory` (`:260`, returns
  `[]llm.Message`); `store_identity.go` `GetForIdentity` (`:28`) — the identity-scoped read the
  export endpoint keys on. No existing exporter → net-new serializer.
- `internal/agui/assets_api.go` — `registerAssetRoutes` (`:11`) + `handleAssetDownload` (`:17`)
  are the templates for the new share routes + the public `/s/{token}` handler; the Go 1.22
  `mux.HandleFunc("METHOD /path", ...)` pattern.
- `internal/agui/auth.go` — `RequireAuth` (whole-origin gate except public paths, `:197`),
  `RequireCapability` (`:274`), `withPrincipal`. `/s/{token}` must be added to the public-path
  allowlist (like `/readyz`), NOT behind `RequireAuth`.
- `internal/agui/audit_store.go` + `audit_api.go` — the identity-keyed audit union + admin UI
  that `share_audit` extends (D-14).
- `internal/objectstore/types.go` — `AssetKey(identityID, assetID)` (`:60`); a token-scoped
  sibling key is needed for public snapshot/artifact blobs (D-12).
- `internal/runner/runner_delete.go` — `DeleteConversationLifecycle` (`:38`) — extend for the
  revoke-on-delete cascade (D-15).
- `internal/agent/tools/send_file.go` — the artifact descriptor carrying the host/container
  `{path}` that D-08 MUST strip from the snapshot.
- `internal/db/migrations/0035_assets_source_kind_agent.*` — latest on disk; 37F adds **0036**.

### Web
- `web/src/chat/artifacts/ArtifactsPanel.tsx`, `ArtifactRow.tsx`, `useThreadArtifacts.ts`,
  `artifactMeta.ts` — the 37B panel to extend with the "Condiviso" section.
- `web/src/AppShell.tsx` — chat shell + thread header (share-arrow toggle placement); lazy-import
  pattern for any share-page renderer.
- `web/src/i18n/resources.ts` (+ `resources.display.ts`) — en+it keys for all share UI copy
  (i18n = react-i18next, keys in BOTH languages).

### Industrial patterns (user-directed research, 2026-07-13)
- open-webui — `/api/v1/chats/{id}/share` → `share_id` on the chat; static snapshot; public
  gated by "Chats Public Sharing" permission (off by default); delete-cascades-revoke.
  Docs: https://docs.openwebui.com/features/chat-conversations/chat-features/chatshare/ ,
  https://docs.openwebui.com/features/chat-conversations/data-controls/shared-chats/
- ChatGPT — snapshot at creation, anonymized, read-only, revoke via Data controls → Shared
  links, delete-cascades. https://help.openai.com/en/articles/7925741-chatgpt-shared-links-faq
- Claude — separate "Share chat" (snapshot incl. artifacts, tool-call data stays private) vs
  "Publish artifact" (public link); Team/Enterprise = org-only authenticated share; Unshare
  revokes. https://support.claude.com/en/articles/10593882-share-and-unshare-chats ,
  https://support.claude.com/en/articles/9547008-publish-and-share-artifacts

### PRD / ADR (required before code)
- PRD-amendment for WEBSHARE-01..04 + the share surface + the three tiers + snapshot model +
  the new `shared_links`/`share_audit` tables (migration 0036).
- Likely **ADR: sharing vs. identity isolation** — the public tier is a deliberate, bounded
  hole in MUSR; the ADR records the fail-closed mitigations (capability gate, opt-in, mandatory
  expiry, mandatory revoke, redacted snapshot, hashed opaque token, audited).
</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- `assets.Service` ingest + `handleAssetDownload` streaming (37A) — the template for bundling
  artifact bytes into a token-scoped blob and streaming them out under `attachment`+`octet-stream`.
- `conversations.Store.GetForIdentity` + `LoadHistory` — identity-scoped read + turn
  reconstruction; the export serializer consumes these (never a raw unscoped query).
- The audit-union in `internal/agui/audit_store.go` + admin UI (`audit_api.go`) — `share_audit`
  slots into this family and appears in the existing admin view for free.
- 37B's sandboxed-iframe HTML renderer + MIME-gated preview set — reuse verbatim for the public
  `/s/{token}` page's artifact rendering.
- `capability_grants` + `RequireCapability`/`HasCapability` interface — the whole public-share
  gate mechanism already exists; add one grant + one gated route.

### Established Patterns
- Additive `*ForIdentity` reads + 404-on-miss ownership (37A D-12 / 36 D-06) — the export
  endpoint and internal-link resolution follow it.
- Whole-origin `RequireAuth` gate with an explicit public-path allowlist — `/s/{token}` joins
  the allowlist; everything else stays authenticated.
- Identity-keyed audit ledgers + random-unguessable IDs bound to owner (36 D-17) — token +
  audit follow it; token additionally hashed at rest (D-13, stronger than 36's plaintext IDs
  because this surface is unauthenticated).
- Forward-only golang-migrate migrations with `.up.sql` + `.down.sql`; scheduler-kind rows for
  the expiry sweep (0033/0034 precedent).

### Integration Points
- New share routes registered alongside `registerAssetRoutes`; the public `/s/{token}` page
  handler added to the `RequireAuth` public allowlist.
- `Runner.DeleteConversationLifecycle` extended to revoke shares + drop token blobs (D-15).
- `internal/objectstore` gains a token-scoped key derivation for public blobs (D-12).
- The web share-arrow (thread header) + "Condiviso" section (`ArtifactsPanel.tsx`) + share modal
  + the settings "Shared links" management/revoke log.
</code_context>

<specifics>
## Specific Ideas

- The user explicitly directed an **industrial-pattern research pass** ("search openwebui and
  all industrial patterns like claude and gpt"). Every tier/posture decision is anchored to the
  open-webui / ChatGPT / Claude convergence cited in `<canonical_refs>` — downstream agents
  should honor those patterns (snapshot-not-live, tool-calls-stripped, public-opt-in-fail-closed,
  delete-cascades-revoke) rather than re-derive.
- Strong "premium bar" parity intent (all three tiers, rendered public page, both export
  formats, full transparency incl. reasoning + tool-call names) **paired with** strict
  fail-closed security on the one unauthenticated surface (capability gate, mandatory expiry +
  revoke, hashed opaque token, hard SC3 redaction). Premium parity, not premium risk.
</specifics>

<deferred>
## Deferred Ideas

- **Standalone single-artifact share link** ("Publish artifact" Claude-style) — per-artifact
  *download* already exists (37A/37B); a *shareable link* to one artifact is its own future
  slice. A shared conversation still carries its artifacts, so this isn't blocking.
- **Single-message share link** — no reference product does message-level cleanly; low value.
- **Explicit per-recipient-identity internal grants** + recipient-picker UI (Claude-Enterprise
  "share with specific members") — D-10 ships bearer-within-auth; per-recipient grants + the
  admin-listable-only variant are a later granularity upgrade.
- **Remix / re-import of a shared JSON export** — the JSON format (D-07) enables it; the import
  flow is out of scope.
- **Interactive AI-powered shared artifacts** (Claude's runtime-API artifacts that require
  sign-in) — Aura's shares are static redacted snapshots; live-compute sharing is far future.

### Reviewed Todos (not folded)
None — `todo.match-phase 37F` returned 0 matches.
</deferred>

---

*Phase: 37F-conversation-artifact-sharing-export-inserted*
*Context gathered: 2026-07-13*
