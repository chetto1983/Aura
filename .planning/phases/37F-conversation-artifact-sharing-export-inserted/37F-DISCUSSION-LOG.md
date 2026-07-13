# Phase 37F: Conversation & Artifact Sharing / Export (INSERTED) - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-07-13
**Phase:** 37F-conversation-artifact-sharing-export-inserted
**Areas discussed:** Share tiers & posture, Share granularity, Export content & redaction, Link storage & data model, Internal-link semantics

**Research directive:** User asked to ground the discussion in industrial patterns
("search openwebui and all industrial patterns like claude and gpt"). A web-research pass on
open-webui / ChatGPT / Claude / LibreChat share+export mechanics informed every option below
(snapshot-not-live, tool-calls-stripped, public-opt-in-fail-closed, delete-cascades-revoke).

---

## Share tiers & posture

### Which tiers ship

| Option | Description | Selected |
|--------|-------------|----------|
| All 3, fail-closed | Export file + internal-identity revocable link + public opt-in expiring token; ADR + migration | ✓ |
| Export + internal-link only | File export + Aura-identity-only revocable link; no public surface | |
| Export file only | Downloadable MD/JSON, no link surface | |

**User's choice:** All 3 tiers, fail-closed (D-01).

### Public-link authority

| Option | Description | Selected |
|--------|-------------|----------|
| Capability-gated | `share.public` capability, admin-grantable, off by default, org kill-switch | ✓ |
| Any owner, self-scoped | Any authenticated user mints public links for own conversations | |

**User's choice:** Capability-gated (D-02). Codebase note: `governance.write` is the existing
admin capability (RESEARCH-OQ3 avoids net-new names) — capability name is a planner/PRD call,
but the per-user-grantable/off-by-default semantics are locked.

### Public recipient experience

| Option | Description | Selected |
|--------|-------------|----------|
| Rendered read-only page | Static HTML view at `/s/{token}` with XSS discipline | ✓ |
| Raw file download only | Token → exported MD/JSON blob download, no rendered page | |

**User's choice:** Rendered read-only page (D-03).

### Public-token expiry

| Option | Description | Selected |
|--------|-------------|----------|
| Mandatory, default 7d, owner-selectable | Forced expiry, 1d/7d/30d/custom + max cap, revoke always on | ✓ |
| Mandatory, default 30d | Same guarantee, longer default | |
| Revoke-only, no expiry | Lives until revoked (rejected by roadmap wording) | |

**User's choice:** Mandatory, default 7d, owner-selectable (D-04).

---

## Share granularity

### Which units get a share affordance

| Option | Description | Selected |
|--------|-------------|----------|
| Whole conversation | Thread-header share-arrow → full-thread redacted snapshot | ✓ |
| Single artifact | Per-row share in the 37B Artefatti panel | |
| Single message | Share-arrow on an individual assistant message | |

**User's choice:** Whole conversation ONLY (D-05). Single-artifact + single-message deferred.

### Snapshot vs live

| Option | Description | Selected |
|--------|-------------|----------|
| Static snapshot + Update button | Freeze at creation, owner refreshes explicitly | ✓ |
| Live view | Always reflects current thread state | |

**User's choice:** Static snapshot + "Update" (D-06).

---

## Export content & redaction

### Export format(s)

| Option | Description | Selected |
|--------|-------------|----------|
| Both Markdown + JSON | Human-readable MD + lossless structured JSON | ✓ |
| Markdown only | Human-readable only | |
| JSON only | Structured only | |

**User's choice:** Both MD + JSON (D-07).

### What goes into the snapshot (raw tool args/results/host-paths ALWAYS stripped)

| Option | Description | Selected |
|--------|-------------|----------|
| Delivered artifacts | The files the agent produced travel with the export | ✓ |
| Tool-call provenance (names only) | 'ran document_search / send_file' badge, no args | ✓ |
| Reasoning / thinking traces | Extended-reasoning text | ✓ |

**User's choice:** All three included (D-08) — max transparency, with the hard SC3 redaction of
raw tool-call args/results + host/container paths as a non-negotiable, not a toggle.

### Public artifacts

| Option | Description | Selected |
|--------|-------------|----------|
| Bundled, token-scoped | Bytes copied to a public-readable token-scoped store, attachment-served | ✓ |
| Referenced, owner-only | Filenames listed, download only for the owner | |
| Omitted with placeholder | '[artifact not included in public share]' | |

**User's choice:** Bundled, token-scoped (D-09).

---

## Link storage & data model

### Storage

| Option | Description | Selected |
|--------|-------------|----------|
| New `shared_links` table + Garage snapshot | Migration 0036 metadata table + token-scoped blobs | ✓ |
| `shared_links` table, snapshot inline jsonb | Same table, snapshot in a jsonb column | |
| Reuse assets/Garage only | Model a share as a special asset with a token | |

**User's choice:** New `shared_links` table + Garage snapshot (D-11/D-12).

### Audit

| Option | Description | Selected |
|--------|-------------|----------|
| Dedicated identity-keyed `share_audit` | create/update/revoke/expire/public-access, joins the 36 audit family | ✓ |
| Fold into `tool_invocations` | Reuse the existing tool-call audit sink | |

**User's choice:** Dedicated `share_audit` (D-14).

### Token shape

| Option | Description | Selected |
|--------|-------------|----------|
| 256-bit opaque, stored hashed | Hash at rest (SHA-256), constant-time compare, never logged | ✓ |
| Opaque random, stored plaintext | open-webui share_id style, plaintext at rest | |

**User's choice:** 256-bit opaque, stored hashed (D-13).

---

## Internal-link semantics

| Option | Description | Selected |
|--------|-------------|----------|
| Bearer-within-auth | Any authenticated Aura identity with the link can open the redacted snapshot | ✓ |
| Shared with specific identities | Owner picks recipient identity/identities; per-grant records | |
| Any authenticated, but admin-listable | Bearer-to-open + centrally revocable in the admin view | |

**User's choice:** Bearer-within-auth (D-10).

---

## Claude's Discretion

- Exact `shared_links` / `share_audit` column set + indexes; one migration vs two.
- Capability NAME for public-share gating (net-new `share.public` vs reuse `governance.write`) —
  semantics locked, name open.
- Token-scoped Garage key/bucket derivation; expiry sweep-vs-lazy enforcement.
- One serializer core + adapters vs two writers for MD/JSON.
- Share-modal UX + "Condiviso" section layout (candidate for `/gsd-ui-phase 37F`).

## Deferred Ideas

- Standalone single-artifact share link (Claude "Publish artifact").
- Single-message share link.
- Explicit per-recipient-identity internal grants + recipient-picker UI + admin-listable variant.
- Remix / re-import of a shared JSON export.
- Interactive AI-powered shared artifacts (Claude runtime-API artifacts requiring sign-in).
