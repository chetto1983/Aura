# ADR 0039 — Conversation sharing vs. identity isolation: the public tier as a bounded, mitigated hole in MUSR

- **Status:** Accepted
- **Date:** 2026-07-17
- **Requirement:** WEBSHARE-02 (records the D-01/D-02/D-03/D-04 tier + capability + token design and their fail-closed mitigations)
- **Phase:** 37F — conversation-artifact-sharing-export-inserted
- **Supersedes / relates to:** Phase 36 (multi-user-identity-isolation-authula-cutover) D-01/D-04 (capability
  gate), D-06 (404-on-read / 403-on-mutate), D-17 (random-unguessable-ID + owner-binding discipline);
  37F-CONTEXT.md D-01..D-15; 37F-RESEARCH.md §State of the Art, §Risks & Landmines (R-08/R-10/R-11/R-13),
  §UI/UX Research §0; PRD `prd.md` Amendment #84 (WEBSHARE-01..04 + the share surface).

---

## Context

Phase 36 established MUSR (multi-user identity isolation) as a **whole-origin invariant**: every
data read is `*ForIdentity`-scoped, a foreign read 404s to hide existence while a foreign mutate
403s (D-06), Postgres RLS (migration `0032_owner_rls`) backstops a forgotten `WHERE` clause as a
kernel-enforced defense-in-depth layer beneath the app-level scoping, and the entire AG-UI origin
sits behind `RequireAuth` with a small, explicit public-path allowlist (`healthz`, `readyz` for
loopback probes, password-reset, bootstrap). The capability model (D-01/D-04) makes every
privileged mutation go through `HasCapability(ctx, identityID, capability)`, and Phase 36's D-17
establishes that any resource an authenticated identity can address by ID gets a random,
unguessable identifier bound to its owner.

WEBSHARE-02 charters a **public** share tier: an opt-in, expiring, opaque-token link that anyone
holding the URL can open **without an Aura identity at all**. This is stated plainly, not
euphemized: `/s/{token}` is this phase's only unauthenticated surface, and it is a **deliberate,
bounded hole** in the MUSR invariant that Phase 36 built. The capability that opens it is a
**token**, not an identity — the first time in the codebase that an anonymous, unauthenticated
caller can read live-adjacent Aura data by design, rather than by omission or bug.

This ADR exists because "we punched a hole in MUSR" is not an acceptable amount of context for a
future reviewer or auditor; it must be *this specific, bounded hole, closed by these specific
mechanisms, with these specific accepted residual risks*.

---

## Decision

**Ship the public share tier, bounded by seven fail-closed mitigations — each named here with the
exact mechanism that enforces it, plus an eighth control (audit) that runs across every tier:**

1. **Capability gate.** Minting a public link requires the `share.public` capability
   (`RequireCapability`, `internal/agui/auth.go:281`) — net-new, per-user, off by default,
   admin-grantable. No identity holds it unless explicitly granted (except the bootstrap
   operator, see Consequences).
2. **Org kill-switch.** `AURA_SHARE_PUBLIC_ENABLED`, default `false`, re-checked **inside the
   share-create handler itself** — not only at the route mount — because `RequireCapability`
   returns `next` unchanged when `!SecretConfigured` (`auth.go:282`): on loopback the capability
   gate does not exist at all, so the org kill-switch is the control that still holds when the
   capability gate structurally cannot. Two independent gates; one survives loopback.
3. **Never the default.** The tier is an explicit, per-act opt-in; an absent tier selection always
   resolves to the internal (bearer-within-auth) tier, never public.
4. **Mandatory expiry.** Default 7 days, owner-selectable up to a hard cap
   (`AURA_SHARE_MAX_EXPIRY_DAYS`, default 90), enforced **lazily at every resolve**
   (`revoked_at IS NULL AND (expires_at IS NULL OR expires_at > now())`) so an unrun sweep can
   never resurrect an expired link — the lazy check is the security gate, not a convenience.
5. **Mandatory revoke.** Always available, independent of expiry; revoke drops the token's Garage
   bytes so a revoked link cannot be resurrected even from a stale blob.
6. **Redacted snapshot.** The token addresses an allowlist-projected `Snapshot` with no field
   capable of holding raw tool-call arguments/results or any host/container path — the recipient's
   browser is never treated as a trust boundary, so redaction happens server-side, once, before any
   byte reaches the token holder.
7. **Hashed opaque token.** A 256-bit `crypto/rand` value, SHA-256-hashed at rest, looked up by
   hash-indexed equality (`WHERE token_hash = $1`), never logged; unknown, expired, and revoked
   tokens all return an identical 404 body and status, so there is no oracle to distinguish "never
   existed" from "existed once."

**Plus, across every tier (not just public): audited.** Every create / update-snapshot / revoke /
expire / public-open event lands in `aura.share_audit`, identity-keyed, with **no recipient PII**
on the open event (no IP, no user-agent, no identity — it proves the link was used, not who used
it).

---

## Consequences

**Positive**
- The public tier ships WEBSHARE-02(b) without becoming a second, unbounded unauthenticated
  surface: `/s/{token}` is the *only* one this phase adds, and every one of the seven controls
  above is independently fail-closed — losing any single one does not silently remove the others.
- The capability + kill-switch pairing specifically closes the one real fail-open in the existing
  auth stack (`auth.go:282`'s loopback pass-through), which no prior phase's write surface needed
  to worry about because none of them were reachable by a caller with **zero** Aura identity.
- Redaction happening server-side, once, in one canonical `Snapshot` constructor means the three
  surfaces that read it (Markdown export, JSON export, the rendered public page) cannot diverge —
  a fix to the redaction rule is a fix everywhere at once.

**Negative / costs (accepted)**
- **What a token holder gets, stated exactly so it is never assumed to be more:** the redacted
  snapshot of exactly ONE conversation, and exactly that snapshot's agent-produced artifacts.
  Never `/api/*`. Never the identity-scoped asset lane (`/api/assets/{id}/download`). Never another
  share's blobs (the token is scoped to its own `share_id`/`snapshot_id` pair). Never the owner's
  identity, name, or avatar — unlike open-webui, which renders the owner's profile on its public
  share page via `getUserInfoById(chat.user_id)`; Aura's snapshot has no field to hold it.
- **Revoke cannot un-ring the bell.** A recipient may already have cached, screenshotted, or
  copied the content, and a search engine may have indexed a public page before revoke. This is
  told to the owner **at mint time**, in the public-tier warning — not only after the fact at
  revoke — because mint is the moment the actual decision is made; telling the owner only at
  revoke is too late to change their behavior.
- **The bootstrap operator holds `*` and therefore auto-holds `share.public`.**
  `cmd/aura/serve_bootstrap.go:176-180` grants the literal `"*"` wildcard capability to the first
  real bootstrap identity, and `HasCapability` (`internal/db/queries/capability_grants.sql:22`)
  passes any capability name — including `share.public` — against it. This is **intended**, the
  same rationale as the seeded `local` identity's `*` in migration `0004`: the bootstrap identity
  is the operator/admin. It is not a leak because provisioned (onboarding) identities receive only
  explicit named capabilities (`cmd/aura/serve_onboarding.go:152-165`), never `*` — so an ordinary
  user does not hold `share.public` unless granted.

---

## Accepted Residuals

### Residual A — Revoke and expiry do not reach copies already made

The mitigations in the Decision make Aura's own systems stop serving a revoked or expired link
(mandatory revoke drops the Garage bytes; lazy-on-access enforcement 404s immediately). Neither
mechanism, nor any mechanism this ADR could add, can reach a copy a recipient already saved, a
screenshot they already took, or a page a search engine already crawled and cached before revoke.
**Compensating control:** the public-tier mint warning states this explicitly and in advance
(Consequences, above) so the owner makes an informed choice at the only point where the choice
still matters.

### Residual B — Orphaned Garage blobs if the lifecycle hook is skipped

The FK `shared_links.conversation_id … ON DELETE CASCADE` removes the `shared_links` row on a raw
conversation delete, but a foreign-key cascade cannot reach into Garage and delete the
corresponding blob — only application code can. If the `Runner.DeleteConversationLifecycle` hook
(`internal/runner/runner_delete.go:38`) is ever bypassed by a future code path that deletes a
conversation directly against the store, the share row disappears (so the link 404s — no security
regression) but its Garage bytes are orphaned indefinitely. **Compensating controls:** (1) the
lifecycle hook is the single, mandatory, already-existing conversation-delete entry point every
surface (AG-UI, Telegram, CLI) already routes through, so bypassing it requires a new code path,
not an oversight in an existing one; (2) the `share_expiry_sweep` scheduler kind reclaims bytes for
every share whose row still exists and has expired, which backstops the ordinary expiry path even
though it cannot reconcile a row that was already hard-deleted out from under it.

---

## Alternatives Considered

| Alternative | Verdict | Why |
|---|---|---|
| **Internal-only sharing (no public tier at all)** | Rejected | WEBSHARE-02(b) explicitly charters the public tier as in-scope; the roadmap names the public link as a design fork the operator resolved to ship, not an open question. |
| **Reuse `governance.write` as the public-mint gate instead of a net-new `share.public`** | Rejected | Collapses "may share my own chat publicly" into "is a full org admin who can install MCP servers and risky supply-chain skills," directly contradicting D-02's per-user, non-admin grant semantics — a privilege-escalation smell, not a simplification. |
| **An S3-public Garage bucket ACL for public-tier blobs** | Rejected | "Public-readable" in this design means reachable without an identity principal *through Aura*, not an S3-public ACL. A publicly-ACL'd bucket leaves blobs reachable by anyone who ever saw the URL even **after** revoke or expiry — directly breaking the mandatory-revoke and mandatory-expiry mitigations this ADR relies on. |
| **A live view of the conversation instead of a frozen snapshot** | Rejected | Turns added to the conversation after the share was minted would retroactively appear on an already-distributed link, which is a leak by definition for a public token nobody at Aura can retract from wherever it has already circulated. Unanimous industry convergence (open-webui, ChatGPT, Claude) on snapshot-not-live is corroborating, not decisive on its own. |
| **A dedicated Go-template renderer for the public `/s/{token}` page instead of the existing SPA** | Rejected | A second renderer means a second place redaction and HTML-escaping can drift from the authenticated app's renderers — exactly what the one-canonical-`Snapshot` design (Decision, mitigation 6) exists to prevent. The SPA reuses the 37B null-origin sandboxed-iframe policy verbatim instead of re-implementing it. |
| **A dedicated `aura-shares` Garage bucket, physically separate from the identity bucket** | Rejected | Requires Garage Admin API bucket provisioning at boot — a new boot failure mode — to buy isolation the design already has for free: share blobs are copies (never a reference into the identity-scoped store) under a `share/` prefix that is lexically disjoint from `identity/`. The disjoint prefix keeps a future bucket split a one-line change if a deployment ever wants the physical separation. |

**Aura's public-tier posture is already strictly stronger than the industrial reference it is most
often compared against.** Read directly from open-webui's `0.10.2` source:

| Dimension | open-webui (source-verified) | Aura 37F (this ADR) |
|---|---|---|
| Token at rest | Plaintext UUIDv4 as the primary key (`shared_chats.py:21`) | SHA-256 hashed, never logged |
| Expiry | None — no `expires_at` column exists | Mandatory, default 7d, capped by `AURA_SHARE_MAX_EXPIRY_DAYS` |
| Tiers | One (public only) | Three, fail-closed by default (file / internal / public) |
| Public-share gate | A per-user permission + a global flag | The same shape, **plus** a dedicated capability re-checked in-handler |
| Expired/unknown/revoked link | Silently redirects home (`goto('/')`) | Identical 404 for all three states — no oracle |
| Owner PII on the shared page | Leaked — renders the owner's name/avatar via `getUserInfoById` | Omitted; the snapshot has no identity field |
| Snapshot redaction | The whole chat JSON blob stored verbatim | Allowlist-projected `Snapshot`, no field for args/results/paths |

**Conclusion this table supports: open-webui is the right UX reference for this phase (affordance
placement, modal shape, management surfaces) and the wrong posture reference for it.** No locked
decision in this ADR or in PRD Amendment #84 is softened "to match open-webui" — every axis above
already favors Aura's design before this phase ships a single line of code.

---

## Forward path

1. **Now:** ship the public tier exactly as bounded above; no further mitigation is required before
   37F's implementation plans begin.
2. **If a future phase widens the public tier** (e.g., per-recipient-identity grants, a
   remix/re-import flow, or interactive live-compute shares — all explicitly out of 37F's scope),
   it must either satisfy all seven mitigations unchanged or amend this ADR with the same rigor:
   naming the new surface, the new mechanism, and the new accepted residual, rather than silently
   loosening one of the seven controls above.
3. **If Residual A or B is ever judged unacceptable** (e.g., a compliance requirement that no
   third-party cache/index ever retains shared content, or a requirement that orphaned bytes be
   provably zero), the fix is additive to this design — a takedown-request workflow for (A), a
   reconciling background job that diffs Garage's `share/` prefix against `shared_links` rows for
   (B) — not a re-architecture of the token/capability/expiry mechanism itself.
