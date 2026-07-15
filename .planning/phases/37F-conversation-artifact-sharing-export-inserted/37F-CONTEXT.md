# Phase 37F: Conversation & Artifact Sharing / Export (INSERTED) - Context

**Gathered:** 2026-07-13
**Amended:** 2026-07-15 (post-research, operator-approved — see Amendment Log)
**Status:** Ready for planning

## Amendment Log — 2026-07-15 (post-research)

`37F-RESEARCH.md` (2026-07-15, HEAD `1a3252e64`) surfaced **five** locked decisions that were
stale, structurally impossible, or dangerously ambiguous. Each was verified against real code and
ruled on by the operator before planning. Decisions NOT listed here are unchanged and still locked.

| # | Decision | Change | Basis |
|---|---|---|---|
| 1 | **D-11** | migration `0036` → **`0040`** | Phase 42 shipped 0036–0039 on 2026-07-14, one day after this CONTEXT was gathered. Verified on disk. A blind 0036 collides → dirty migrate tracker → every later migration blocked. **Factual correction.** |
| 2 | **D-08** | **reasoning/thinking traces DROPPED** from the snapshot | Never persisted — verified 3 ways (no column, no `llm.Message` field, `Chunk.Reasoning` stream-only). `LoadHistory` structurally cannot produce them. **Operator-approved 2026-07-15. Requires PRD-amendment.** |
| 3 | **D-09** | "delivered artifacts" **narrowed to agent-produced only** | Original wording did not forbid bundling the user's OWN uploads into a public link. Claude excludes user attachments; open-webui shipped a write-through-share bug. **Operator-approved 2026-07-15.** |
| 4 | **D-13** | "constant-time compare on lookup" → **hash-indexed equality** | Literal reading = full table scan + per-row compare: slower, no more secure. Intent (no plaintext at rest, backup-leak-safe, no enumerable IDs) fully preserved. **Operator-approved 2026-07-15. Requires PRD-amendment.** |
| 5 | **D-05** | share-arrow target: "thread header" → **`ArtifactsShell.tsx` floating cluster** | Aura has no thread header; `ShellHeader.tsx` is app-level. 37B reserved the exact spot in code (`ArtifactsShell.tsx:20-22`). Intent validated, DOM target corrected. **Factual correction.** |

**Also corrected (D-08 parenthetical):** the 37A D-13 path strip is **client-side**
(`sseAdapter.ts:346-360`); the backend still ships `path`. The Go serializer must implement its
own server-side allowlist — a recipient's browser is not a trust boundary.

**PRD-amendments required before code** (PRD-first is absolute — CLAUDE.md): items **2** and **4**
above, in addition to the WEBSHARE-01..04 amendment the phase already required.

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
  **thread level** (top-right-of-thread share-arrow + a "Condiviso" section listing active shares
  in `ArtifactsPanel.tsx`).
  **[AMENDED 2026-07-15 — placement target corrected, intent unchanged]** Aura has **no thread
  header**: `ShellHeader.tsx` is the *app-level* header. The real top-right-of-thread seam is the
  floating overlay cluster at `AppShell.tsx:514-517`, and 37B **reserved this exact spot in code**
  — `ArtifactsShell.tsx:20-22`: *"the adjacent share-arrow is 37F, not built."* Ship `ShareToggle`
  as a sibling module (`web/src/shell/ShareShell.tsx`) mirroring `ArtifactsToggle`; order
  `[VoiceModeToggle] [ShareToggle] [ArtifactsToggle]`. The locked *intent* (thread-level
  share-arrow) is validated; only the DOM target moves.
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
  artifacts (D-09) + **tool-call provenance (tool NAMES only, per turn)**. **HARD SC3 redaction
  (non-negotiable, NOT a toggle):** raw tool-call
  arguments/results and any host/container filesystem path are ALWAYS stripped. Concrete leak
  sources to scrub: the `send_file` artifact descriptor `{path}` (host/container path),
  `aura.tool_invocations` args/results, and any other-identity data. The snapshot carries
  `asset_id`/`filename` only, never the raw path chip.
  **[AMENDED 2026-07-15 — reasoning/thinking traces REMOVED; requires PRD-amendment]** The
  original D-08 listed "reasoning/thinking traces" in the snapshot. Reasoning is **never
  persisted** — verified three ways: `aura.conversation_turns` has no reasoning column
  (`0005_conversations.up.sql:23-36`), `llm.Message` has no reasoning field (`client.go:24-30`),
  and `llm.Chunk.Reasoning` is **stream-only** (`client.go:79`). The only "reasoning" at rest is
  `metadata.reasoning_effort` — the *setting*, not the trace. `LoadHistory` structurally cannot
  produce traces. Operator decision (2026-07-15): **drop reasoning from the snapshot
  permanently.** No reference product ships CoT in a share (Claude keeps tool-call data private;
  open-webui stores only the chat JSON). Adding persistence would put CoT at rest and then export
  it — a privacy regression contradicting this phase's own SC3 posture. `internal/reasoningtrace`
  is an operator debug JSONL on disk and MUST NOT be exported (it carries host paths + prompts).
  **[AMENDED 2026-07-15 — the 37A path strip is CLIENT-side]** The parenthetical "37A D-13
  already replaces it in the live UI — the exporter must do the same" is misleading: the strip
  runs **in the browser** (`sseAdapter.ts:346-360`) while the backend still ships `path` over the
  wire (`Actions.ArtifactDelta map[string]any`, `event.go:72`). A recipient's browser is **not a
  trust boundary**. The Go serializer MUST implement its own server-side allowlist and be tested
  with hostile fixtures. Do NOT assume "37A already strips paths."
- **D-09 — Delivered artifacts travel with the share; PUBLIC = bundled token-scoped.** Artifact
  bytes are copied into a **token-scoped, public-readable snapshot store**; the recipient
  downloads via the token, NEVER via the identity-scoped `/api/assets/{id}/download`. Served
  `Content-Disposition: attachment` + `application/octet-stream` (37A D-10); HTML previews
  sandboxed (D-03). Revoke/expiry drops the bundled copy. Internal (bearer-within-auth) shares
  resolve artifacts via the same snapshot.
  **[AMENDED 2026-07-15 — "delivered artifacts" NARROWED to agent-produced only]** The original
  wording did not obviously forbid bundling the **user's own uploads**. Operator decision
  (2026-07-15): **agent artifacts ONLY** — mirror the existing `selectAgentArtifacts` filter
  (`useThreadArtifacts.ts:35`: `source_kind === 'agent' && status !== 'deleted' && status !==
  'canceled'`) **server-side**. A user's own upload (`source_kind='web'` — possibly a passport
  scan) MUST NEVER enter a share, above all a public one. Claude does exactly this: *"If you share
  a chat containing an attached file, the file itself is not included in the shared snapshot and
  remains private."* Aura already encodes the rule client-side; 37F enforces it at the trust
  boundary. **Copy, never reference:** the recipient's token addresses `share/{id}/…` blobs and
  has NO path to `identity/{owner}/asset/…` — never "resolve the share's asset_id through
  `assets.Service`." open-webui shipped precisely this bug (granted **write** on files through a
  share link; fixed in their `CHANGELOG.md:331`).

### Internal-identity link semantics (WEBSHARE-02 branch a)
- **D-10 — Internal link = bearer-within-auth.** ANY authenticated Aura identity holding the
  link can open the already-redacted snapshot — being logged into Aura is the gate, the link is
  the capability (open-webui "private share"). No recipient picker, no per-recipient grant rows
  (minimal industrial). Revocable + optional expiry. Because the snapshot is redacted (D-08), a
  bearer never sees more than the owner chose to share. Explicit per-identity grants +
  admin-listable-only variant = deferred.

### Storage & data model (fork c)
- **D-11 — New `shared_links` table, migration 0040.**
  **[AMENDED 2026-07-15 — slot corrected 0036 → 0040; VERIFIED ON DISK]** The original text read
  "migration 0036 (next free slot; 0035 `assets_source_kind_agent` is latest on disk)". That was
  true on 2026-07-13 when this CONTEXT was gathered. **Phase 42 (llm-conversation-compaction)
  shipped 0036–0039 on 2026-07-14** (`0036_compaction_checkpoints`, `0037_content_parts`,
  `0038_compaction_memory`, `0039_compaction_rollout`). A blind `0036_shared_links.up.sql`
  **collides**, dirties the golang-migrate tracker, and blocks every subsequent migration. **37F
  uses 0040.** Re-verify `ls internal/db/migrations/ | tail -1` at execute time — this project
  runs multiple phases in flight. Columns (planner refines): `id`,
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
  **hash-indexed equality lookup**, never logged. A DB/backup leak never exposes live links
  (session-token discipline). Opaque → no enumerable IDs. Plaintext shown to the owner once at
  creation; it lives only in the URL.
  **[AMENDED 2026-07-15 — "constant-time compare on lookup" → hash-indexed equality; requires
  PRD-amendment]** Implemented **literally**, "constant-time compare on lookup" means scanning
  every row and `subtle.ConstantTimeCompare`-ing each — **slower and no more secure**. The correct
  implementation is `WHERE token_hash = $1` on the unique index (the standard "store a hash of the
  API key, index it, look it up" pattern). This is safe: the lookup key is already `SHA-256(token)`,
  so exploiting a timing signal on the index probe to recover the *token* would require inverting
  SHA-256. D-13's **intent** — no plaintext token at rest, a DB/backup leak never exposes live
  links, no enumerable IDs — is **fully satisfied**. `crypto/subtle.ConstantTimeCompare` remains
  correct **only** where a secret is compared in Go memory; this design has no such site. The
  PRD-amendment MUST record the hash-indexed lookup explicitly so a later reviewer does not
  "fix" it into a table scan.
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
  migration or shares 0040 (see amended D-11 — the slot is 0040, NOT 0036).
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
