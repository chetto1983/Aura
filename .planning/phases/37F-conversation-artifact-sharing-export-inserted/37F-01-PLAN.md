---
phase: 37F-conversation-artifact-sharing-export-inserted
plan: 01
type: execute
wave: 1
depends_on: []
files_modified:
  - prd.md
  - docs/adr/0039-conversation-sharing-vs-identity-isolation.md
autonomous: true
requirements: [WEBSHARE-01, WEBSHARE-02, WEBSHARE-03, WEBSHARE-04]

must_haves:
  truths:
    - "The PRD documents WEBSHARE-01..04, the three share tiers, the snapshot model, and the shared_links/share_audit tables before any 37F code exists"
    - "The PRD records the D-08 amendment (reasoning traces DROPPED — never persisted) as a permanent decision"
    - "The PRD records the D-13 amendment (hash-indexed equality, NOT a constant-time table scan) explicitly enough that a later reviewer cannot 'fix' it into a scan"
    - "An ADR records the public tier as a deliberate, bounded hole in MUSR identity isolation with its fail-closed mitigations"
  artifacts:
    - path: "prd.md"
      provides: "WEBSHARE-01..04 amendment + tiers + snapshot model + tables + D-08/D-13 amendments"
      contains: "WEBSHARE-01"
    - path: "docs/adr/0039-conversation-sharing-vs-identity-isolation.md"
      provides: "ADR: sharing vs identity isolation"
      min_lines: 40
  key_links:
    - from: "prd.md"
      to: ".planning/REQUIREMENTS.md WEBSHARE-01..04"
      via: "requirement IDs quoted verbatim"
      pattern: "WEBSHARE-0[1-4]"
    - from: "docs/adr/0039-conversation-sharing-vs-identity-isolation.md"
      to: "prd.md"
      via: "ADR referenced from the PRD share section"
      pattern: "0039"
  prohibitions:
    - "MUST NOT write any Go/TS/SQL code in this plan — this plan is the PRD-first gate that authorizes it"
    - "MUST NOT document reasoning/thinking traces as part of the snapshot (D-08 amended: DROPPED)"
    - "MUST NOT document 'constant-time compare on lookup' as the token lookup (D-13 amended: hash-indexed equality)"
    - "MUST NOT document migration 0036 (taken by Phase 42) — the slot is 0040"
    - "MUST NOT document an S3-public Garage bucket — 'public-readable' means reachable without an identity principal THROUGH Aura; Aura streams the bytes"
---

<objective>
Land the PRD-amendment and the ADR that authorize every line of 37F code.

CLAUDE.md: *"Senza PRD completo non si scrive una riga di codice."* PRD-first is ABSOLUTE on this
project. 37F additionally carries **two operator-approved amendments that contradict the PRD's own
prior text** (D-08 reasoning traces, D-13 constant-time compare) — shipping code against a PRD that
still says the old thing guarantees a later reviewer "fixes" the code back to the wrong design.

Purpose: gate the phase. Every downstream 37F plan depends on this one.
Output: `prd.md` share section + `docs/adr/0039-conversation-sharing-vs-identity-isolation.md`.
</objective>

<execution_context>
@/home/user/Aura/.claude/get-shit-done/workflows/execute-plan.md
@/home/user/Aura/.claude/get-shit-done/templates/summary.md
</execution_context>

<context>
@.planning/ROADMAP.md
@.planning/REQUIREMENTS.md
@.planning/phases/37F-conversation-artifact-sharing-export-inserted/37F-CONTEXT.md
@.planning/phases/37F-conversation-artifact-sharing-export-inserted/37F-RESEARCH.md
@CLAUDE.md
@prd.md
</context>

## Artifacts this phase produces

37F creates the following symbols. This plan documents them; later plans build them.

**Go — new package `internal/share`:** `Snapshot`, `SnapshotTurn`, `SnapshotArtifact`,
`BuildSnapshot`, `Snapshot.Markdown()`, `Snapshot.JSON()`, `Mint()`, `Hash()`, `Link`,
`Store`, `Service`, `Create`, `Update`, `Revoke`, `ResolveByToken`, `ResolveInternal`,
`ExpireDue`, `ErrShareNotFound`, `bundleFilter`.
**Go — `internal/objectstore`:** `ShareSnapshotKey`, `ShareArtifactKey`, `ShareKeyPrefix`.
**Go — `internal/agui`:** `ShareService` interface, `registerShareRoutes`, `handleShareCreate`,
`handleShareResolvePublic`, `handleShareAssetPublic`, `handleConversationExport`.
**Go — `internal/cron/handlers`:** `KindShareExpirySweep = "share_expiry_sweep"`, `ShareExpirer`,
`NewShareExpiryHandler`.
**Go — `cmd/aura`:** `sharePublicCapability = "share.public"`, `isPublicShareRoute`,
`registerShareRoutes` (parent mux).
**Go — `internal/config`:** `ShareConfig`, `AURA_SHARE_PUBLIC_ENABLED`, `AURA_SHARE_MAX_EXPIRY_DAYS`.
**DB:** `aura.shared_links`, `aura.share_audit`, migration `0040`, scheduler kind
`share_expiry_sweep`, audit union source value `"share"`.
**Routes:** `GET /api/conversations/{id}/export`, `POST /api/shares`, `GET /api/shares`,
`PATCH /api/shares/{id}/snapshot`, `DELETE /api/shares/{id}`, `GET /s/{token}`,
`GET /s/{token}/data`, `GET /s/{token}/asset/{id}`.
**Web:** `ShareToggle`, `useSharePanel`, `ShareModal`, `RevokeConfirmDialog`, `SharePage`,
`SharedSection`, `useThreadShares`, `AssetSourceContext`, `shareEn`/`shareIt`.

<tasks>

<task type="auto">
  <name>Task 1: PRD-amendment — the 37F share surface, tiers, snapshot model, schema, and the two contradicting amendments</name>
  <read_first>
    - `prd.md` — locate the section that catalogs web/cockpit requirements and the §Persistence "Migration numbering — fonte di verità" block. READ BOTH before editing; the migration-numbering block is the one that must gain 0040.
    - `.planning/REQUIREMENTS.md:99-106` — WEBSHARE-01..04 acceptance text (LOCKED, quote verbatim)
    - `.planning/phases/37F-conversation-artifact-sharing-export-inserted/37F-CONTEXT.md` — the Amendment Log (§ top) + all of `<decisions>` D-01..D-15
    - `.planning/phases/37F-conversation-artifact-sharing-export-inserted/37F-RESEARCH.md` — OQ1-OQ5 resolutions, the Redaction Inventory (9 leak sources L-01..L-09), R-06, R-08, R-11, R-12, R-13
  </read_first>
  <action>
    Amend `prd.md` with a 37F share section. Follow the PRD's existing slice-section shape and language
    (Italian prose is the PRD's register; keep it). The amendment MUST record, each as a stated decision
    with its rationale:

    (1) **WEBSHARE-01..04** quoted verbatim from REQUIREMENTS.md:99-106.

    (2) **The three tiers, fail-closed (D-01):** (a) file export always available; (b) internal-identity
    revocable link = the default "Condividi", NO capability required; (c) public opt-in expiring opaque
    token, NEVER default, behind an explicit warning.

    (3) **The capability name `share.public` (D-02, Claude's-discretion call → RESOLVED).** Record: net-new,
    per-user grantable, off by default, admin-grantable — the `identity.create` precedent
    (`serve_webui.go:271-277`), NOT the `governance.write` fallback. Record WHY the RESEARCH-OQ3
    reuse-precedent (`0026_local_admin_caps.up.sql:1-3`) does not apply: `settings.model.write` was
    rejected as an admin-scoped duplicate of `governance.write`, whereas public sharing is a per-user
    non-admin action, and collapsing it into `governance.write` would mean "to share your own chat you
    must be a full org admin who can install RISKY supply-chain skills" — a privilege-escalation smell.
    Record that a net-new name costs ZERO schema (`capability` is free-text; the only gate is
    `capNameRe = ^[a-z][a-z0-9._-]{0,63}$` at `internal/identity/store.go:33`).

    (4) **The org kill-switch = `AURA_SHARE_PUBLIC_ENABLED`, default `false` (D-02, A2 → RESOLVED).**
    Record the verified reason it is an env knob and NOT an `aura.settings` row: `settings.AllowedKeys`
    (`internal/settings/settings.go:45-62`) is a static 16-key **model-backend-only** allowlist whose
    package doc states it exists precisely so "a settings row can never clobber connection/security env",
    and it is guarded by a test. A share kill-switch IS security env. Record that it is re-checked
    **inside the handler**, not only at the route mount, because `RequireCapability` returns `next`
    unchanged when `!SecretConfigured` (`internal/agui/auth.go:282`) — on loopback the capability gate
    does not exist, and a default of `false` is what closes that fail-open.

    (5) **The snapshot model (D-06/D-07):** static snapshot frozen at creation + an owner "Update";
    ONE canonical redacted `Snapshot` + format adapters (MD/JSON/page all pure functions of it), so
    redaction cannot diverge across surfaces.

    (6) **[AMENDMENT — supersedes prior PRD text] D-08: reasoning/thinking traces are DROPPED from the
    snapshot, permanently.** Record the three-way verification: `aura.conversation_turns` has no reasoning
    column (`0005_conversations.up.sql:23-36`), `llm.Message` has no reasoning field (`client.go:24-30`),
    `llm.Chunk.Reasoning` is stream-only (`client.go:79`). `LoadHistory` structurally cannot produce them.
    The only "reasoning" at rest is `metadata.reasoning_effort` — the setting, not the trace. Record that
    `internal/reasoningtrace` is an operator debug JSONL carrying host paths + prompts and MUST NOT be
    exported. Record the rationale: no reference product ships CoT in a share, and adding persistence
    would put CoT at rest then export it — a privacy regression inside a privacy phase.

    (7) **What the snapshot DOES carry (D-08):** visible `user`+`assistant` text, agent-produced artifacts
    (D-09), and tool-call provenance as **tool NAMES only**. Record the HARD SC3 redaction as
    non-negotiable and NOT a toggle: raw tool args/results and any host/container path are ALWAYS
    stripped. Enumerate the 9 verified leak sources from RESEARCH's Redaction Inventory (L-01..L-09) and
    state the structural rule: **allowlist projection, never denylist** — `Snapshot` has no field able to
    hold args/results/paths, so the leak is a compile error, not a review miss. Record that the 37A path
    strip is **client-side** (`sseAdapter.ts:346-360`) while the backend still ships `path`
    (`agent/event.go:72`) — a recipient's browser is NOT a trust boundary, so the Go serializer implements
    its own server-side allowlist.

    (8) **[AMENDMENT] D-09 narrowed: agent-produced artifacts ONLY.** Record that the server mirrors
    `selectAgentArtifacts` (`web/src/chat/artifacts/useThreadArtifacts.ts:33-37`:
    `source_kind === 'agent' && status !== 'deleted' && status !== 'canceled'`) at the trust boundary.
    A user's own upload (`source_kind='web'` — possibly a passport scan) MUST NEVER enter a share.
    Record **copy, never reference**: the recipient's token addresses `share/{id}/…` blobs and has NO
    path to `identity/{owner}/asset/…`; never resolve a share's `asset_id` through `assets.Service`
    (open-webui shipped exactly this bug — granted **write** through a share link, `CHANGELOG.md:331`).

    (9) **[AMENDMENT — supersedes prior PRD text] D-13: hash-indexed equality lookup, NOT a constant-time
    table scan.** Record explicitly, in words a reviewer cannot misread: the lookup is
    `WHERE token_hash = $1` on the unique partial index. Record WHY this is not a downgrade — the lookup
    key is already `SHA-256(token)`, so exploiting a timing signal on the index probe to recover the token
    would require inverting SHA-256; the literal reading (scan every row + `subtle.ConstantTimeCompare`
    each) is slower and no more secure. Record that D-13's intent is fully preserved: no plaintext token
    at rest, a DB/backup leak never exposes live links, no enumerable IDs. Record that
    `crypto/subtle.ConstantTimeCompare` remains correct only where a secret is compared in Go memory and
    that this design has no such site. **State this as a decision so a later reviewer does not "fix" it
    into a scan.**

    (10) **Storage (D-12 / A1 → RESOLVED):** token-scoped Garage blobs under a `share/` prefix lexically
    disjoint from `identity/`, keyed on `share_id` + `snapshot_id` (NOT `token_hash` — the internal tier
    has no token, and D-10 requires internal shares to resolve artifacts via the same snapshot). Record
    the load-bearing clarification: **"public-readable" means reachable without an identity principal
    THROUGH Aura — NOT an S3-public ACL.** The bucket stays private; Aura streams the bytes (37A D-09
    precedent: never presigned/redirected). An S3-public bucket would leave blobs reachable AFTER revoke,
    breaking D-09 and D-15.

    (11) **Schema:** `aura.shared_links` + `aura.share_audit`, **migration 0040** — state that 0036-0039
    were taken by Phase 42 on 2026-07-14 and that a blind 0036 dirties the golang-migrate tracker.
    Update the PRD §Persistence "Migration numbering — fonte di verità" block so 0040 is reserved for
    37F. Record that both tables land in ONE migration because a partial apply (`shared_links` without
    `share_audit`) would let a share be created **unaudited** — an SC3 violation.

    (12) **Audit (D-14):** dedicated identity-keyed `share_audit` capturing create/update/revoke/expire/
    open; joins the existing union (`internal/agui/audit_store.go`) as a 4th leg with `source='share'`
    and surfaces in the admin audit UI. Public opens audited with NO recipient PII.

    (13) **Lifecycle (D-15):** revoke-on-delete cascade through `Runner.DeleteConversationLifecycle`
    (`internal/runner/runner_delete.go:38`) BEFORE the persistence delete; expiry enforced **lazily at
    every resolve** (the fail-closed security gate) **and** swept by a `share_expiry_sweep` scheduler kind
    (byte reclamation — D-09's "revoke/expiry drops the bundled copy" is unmet without it). Record that
    these are not alternatives: lazy is the gate, the sweep is the GC. Record that the FK
    `ON DELETE CASCADE` removes the row but **NOT** the Garage bytes, so the lifecycle hook is mandatory.

    (14) **[NEW — verified at plan time, record as a DECISION not an accident] The bootstrap operator
    holds `*` and therefore auto-holds `share.public`.** `cmd/aura/serve_bootstrap.go:176-180` grants the
    literal `"*"` wildcard to the first real bootstrap identity, and `HasCapability`
    (`internal/db/queries/capability_grants.sql:22`) passes any name against `*`. Record this as
    INTENDED — the bootstrap identity is the operator/admin, the same rationale as `local`'s `*` in 0004.
    Record the contrast that makes D-02's semantics real: **provisioned (onboarding) identities receive
    explicit named capabilities only** (`cmd/aura/serve_onboarding.go:152-165`, escalation-validated),
    never `*`, so an ordinary user does NOT hold `share.public` unless granted. Record that
    `retireLocalIdentity` (`cmd/aura/serve_auth.go`) does **not** migrate `capability_grants` at all.
    Record the test consequence: capability assertions MUST use provisioned non-wildcard identities or
    they pass vacuously.

    (15) **Expiry cap (D-04, open number → RESOLVED under Claude's discretion):** default 7 days;
    owner-selectable 1d/7d/30d/custom; max cap `AURA_SHARE_MAX_EXPIRY_DAYS`, default **90**. Record that
    the cap is operator-tunable so the policy choice needs no code change.

    (16) **Out of scope**, quoted from CONTEXT `<deferred>`: standalone single-artifact share links,
    single-message share links, per-recipient-identity internal grants + recipient picker, remix/re-import
    of a shared JSON, interactive AI-powered shared artifacts.

    Write the amendment where the PRD keeps its per-slice requirement sections. Do NOT restructure the
    PRD. Do NOT touch unrelated sections.
  </action>
  <verify>
    <automated>grep -c "WEBSHARE-0[1-4]" prd.md | grep -qv '^0$' && grep -q "0040" prd.md && grep -q "share.public" prd.md && grep -q "AURA_SHARE_PUBLIC_ENABLED" prd.md && echo PRD-OK</automated>
  </verify>
  <acceptance_criteria>
    - `grep -o "WEBSHARE-0[1-4]" prd.md | sort -u | wc -l` returns `4` — all four requirement IDs documented.
    - `grep -n "0040" prd.md` matches in BOTH the 37F share section AND the §Persistence migration-numbering block.
    - `grep -n "share.public" prd.md` matches, and the surrounding text names `identity.create` as the precedent and `governance.write` as the rejected fallback.
    - `grep -n "AURA_SHARE_PUBLIC_ENABLED" prd.md` matches, and the surrounding text states the default is `false` and that it is re-checked inside the handler because of `auth.go:282`.
    - `grep -niE "reasoning|thinking" prd.md` — every match inside the 37F section states traces are DROPPED/not persisted; NO match claims the snapshot carries them.
    - `grep -n "constant-time" prd.md` — if it matches inside the 37F section, the surrounding text states it is REJECTED for the token lookup in favor of hash-indexed equality. The 37F section contains the phrase `token_hash = $1` or an unambiguous "hash-indexed equality" statement.
    - `grep -niE "source_kind|agent-produced|agent artifacts" prd.md` matches inside the 37F section, stating user uploads never enter a share.
    - `grep -n "0036" prd.md` — any match inside the 37F section states 0036 is TAKEN by Phase 42, never that 37F uses it.
    - The 37F section states "public-readable" is NOT an S3-public ACL.
    - The 37F section records the bootstrap-`*` decision AND that provisioned identities get named caps only.
    - No Go, TypeScript, or SQL file is modified by this task: `git diff --name-only` lists only `prd.md`.
  </acceptance_criteria>
  <done>`prd.md` documents WEBSHARE-01..04, the three tiers, `share.public`, the kill-switch, the snapshot model, the 9 leak sources, the agent-artifacts-only rule, migration 0040, `share_audit`, the lifecycle, the bootstrap-`*` decision, and BOTH the D-08 and D-13 amendments as stated decisions with rationale.</done>
</task>

<task type="auto">
  <name>Task 2: ADR 0039 — sharing vs. identity isolation (the bounded MUSR hole)</name>
  <read_first>
    - `docs/adr/0037-per-identity-docker-sandbox.md` — the house ADR format (front-matter/status/context/decision/consequences shape). Follow it; do not invent a new template.
    - `docs/adr/0038-graph-store-license-neo4j-gplv3-vs-arcadedb-apache.md` — the second format reference; confirm section naming before writing.
    - `.planning/phases/36-multi-user-identity-isolation-authula-cutover/36-CONTEXT.md` — the MUSR isolation posture this ADR carves an exception in (D-06 404-read/403-mutate, D-01/D-04 capability gate, D-17 token/ID discipline)
    - `.planning/phases/37F-conversation-artifact-sharing-export-inserted/37F-RESEARCH.md` §"State of the Art" + §"Risks & Landmines" (R-08, R-10, R-11, R-13) + §"UI/UX Research" §0 (the open-webui posture comparison table)
  </read_first>
  <action>
    Write `docs/adr/0039-conversation-sharing-vs-identity-isolation.md` using the section shape of
    ADR 0037. Status: accepted. Date: the execute date.

    **Context:** Phase 36 established MUSR identity isolation as a whole-origin invariant — every read is
    `*ForIdentity`-scoped, a foreign read 404s to hide existence, RLS 0032 backstops a forgotten WHERE,
    and the entire origin sits behind `RequireAuth` with a small public-path allowlist. WEBSHARE-02
    charters a **public** share tier. State plainly that this is a **deliberate, bounded hole** in that
    invariant: `/s/{token}` is the phase's only unauthenticated surface, and a token — not an identity —
    is the capability that opens it.

    **Decision:** ship the public tier, bounded by seven fail-closed mitigations, each of which must be
    named with the mechanism that enforces it:
    1. **Capability gate** — `share.public`, per-user, off by default (`RequireCapability`, `auth.go:281`).
    2. **Org kill-switch** — `AURA_SHARE_PUBLIC_ENABLED`, default `false`, re-checked **inside the handler**
       because `RequireCapability` is a pass-through when `!SecretConfigured` (`auth.go:282`). Two
       independent gates; one survives loopback.
    3. **Never default** — the tier is an explicit opt-in per share act; absent tier ⇒ internal.
    4. **Mandatory expiry** — default 7d, capped by `AURA_SHARE_MAX_EXPIRY_DAYS` (90), enforced **lazily
       at every resolve** so an unrun sweep cannot resurrect a link.
    5. **Mandatory revoke** — always available, independent of expiry; revoke drops the Garage bytes.
    6. **Redacted snapshot** — an allowlist projection with no field able to hold args/results/paths;
       the recipient's browser is not a trust boundary.
    7. **Hashed opaque token** — 256-bit `crypto/rand`, SHA-256 at rest, never logged; unknown/expired/
       revoked all return an identical 404 body and status, so there is no oracle.
    Plus: **audited** — every create/update/revoke/expire/open lands in `share_audit`, with no recipient PII.

    **Consequences.** State honestly:
    - What a token holder gets: exactly the redacted snapshot of ONE conversation and ONE snapshot's
      agent-produced artifacts. NOT `/api/*`, NOT the identity-scoped asset lane, NOT another snapshot's
      blobs, NOT the owner's identity (L-08 — open-webui leaks the owner's name/avatar via
      `getUserInfoById`; Aura omits it).
    - Revoke cannot un-ring the bell: a recipient may have cached or copied the content, and search
      engines may have indexed it. This is stated to the owner **at mint time**, in the public warning —
      not only at revoke, because mint is when the decision is made.
    - Orphaned blobs (R-10): the FK `ON DELETE CASCADE` drops the row but not the Garage bytes, so a
      skipped lifecycle hook orphans blobs unreclaimable even by the row-scanning sweep. The lifecycle
      hook is mandatory and the sweep's prefix reconcile is the backstop.
    - The bootstrap operator holds `*` and therefore `share.public` (intended — they are the admin);
      provisioned identities do not.

    **Alternatives considered and rejected**, each with its reason:
    - **Internal-only (no public tier)** — rejected: WEBSHARE-02 charters (b) explicitly; the roadmap
      names the public link as a design fork the operator resolved to ship.
    - **Reuse `governance.write` as the gate** — rejected: collapses "may share my own chat" into "is a
      full org admin," contradicting D-02's per-user-grant semantics.
    - **S3-public Garage bucket** — rejected: revoke could not drop access to already-issued URLs.
    - **Live view instead of a snapshot** — rejected: turns added after sharing would retroactively leak;
      unanimous industry convergence on snapshot (open-webui/ChatGPT/Claude).
    - **A Go-template renderer for the public page** — rejected: a second renderer forks redaction and
      escaping; the SPA reuses the 37B null-origin iframe policy verbatim.
    - **A dedicated `aura-shares` bucket** — rejected: needs Garage Admin API provisioning at boot (a new
      boot failure mode) for isolation the copy already provides; the disjoint `share/` prefix keeps a
      future bucket split a one-line change.

    Include the posture table from RESEARCH §UI/UX 0 showing Aura is strictly stronger than open-webui on
    every security axis (token at rest, expiry, tiers, gate, expired-handling, owner PII, redaction), and
    state the conclusion it supports: **open-webui is the right UX reference and the wrong posture
    reference — do not soften a locked decision to "match open-webui."**

    Reference the ADR from the PRD's 37F section (one line).
  </action>
  <verify>
    <automated>test -f docs/adr/0039-conversation-sharing-vs-identity-isolation.md && grep -q "share.public" docs/adr/0039-conversation-sharing-vs-identity-isolation.md && grep -q "AURA_SHARE_PUBLIC_ENABLED" docs/adr/0039-conversation-sharing-vs-identity-isolation.md && grep -q "0039" prd.md && echo ADR-OK</automated>
  </verify>
  <acceptance_criteria>
    - `docs/adr/0039-conversation-sharing-vs-identity-isolation.md` exists and its section headings match the set used by `docs/adr/0037-per-identity-docker-sandbox.md`.
    - The ADR names all seven mitigations; `grep -c "share.public\|AURA_SHARE_PUBLIC_ENABLED\|expires_at\|revoke\|SHA-256\|snapshot" docs/adr/0039-*.md` is ≥ 6.
    - The ADR explicitly states `/s/{token}` is the only unauthenticated surface in the phase.
    - The ADR states the loopback fail-open (`auth.go:282`) and names the in-handler kill-switch as its closure.
    - The ADR lists ≥ 5 rejected alternatives, each with a reason.
    - `grep -n "0039" prd.md` matches — the PRD references the ADR.
    - `git diff --name-only` lists only `docs/adr/0039-conversation-sharing-vs-identity-isolation.md` and `prd.md`.
  </acceptance_criteria>
  <done>ADR 0039 exists in house format, records the public tier as a bounded MUSR hole with seven named fail-closed mitigations, states the honest consequences (cache/index, orphan blobs, bootstrap `*`), lists the rejected alternatives, and is referenced from the PRD.</done>
</task>

</tasks>

<threat_model>
## Trust Boundaries

| Boundary | Description |
|----------|-------------|
| documentation → implementation | A PRD that still states the superseded D-08/D-13 text will cause a reviewer to "correct" the code back to a privacy regression (D-08) or a pointless table scan (D-13). The PRD is the boundary that prevents this. |

## STRIDE Threat Register

| Threat ID | Category | Component | Disposition | Mitigation Plan |
|-----------|----------|-----------|-------------|-----------------|
| T-37F-D1 | Repudiation | prd.md | mitigate | Record D-08 (reasoning DROPPED) + D-13 (hash-indexed equality) as explicit dated amendments with rationale, so the decision is attributable and cannot be silently reverted. |
| T-37F-D2 | Elevation of Privilege | ADR 0039 | mitigate | Record the public tier as a bounded, mitigated exception with all seven fail-closed controls named, so a later phase cannot widen the hole without amending the ADR. |
| T-37F-D3 | Tampering | prd.md §Persistence | mitigate | Reserve slot 0040 in the migration-numbering source-of-truth so a concurrent in-flight phase does not also claim it. |
| T-37F-SC | Tampering | npm/pip/cargo installs | accept | 37F adds ZERO new dependencies, sidecars, or packages (RESEARCH §Environment Availability: "37F requires no new dependency, no new sidecar, no new package"). No install task exists in this phase, so no legitimacy gate applies. |
</threat_model>

<verification>
- `grep -o "WEBSHARE-0[1-4]" prd.md | sort -u | wc -l` → `4`
- `test -f docs/adr/0039-conversation-sharing-vs-identity-isolation.md`
- No source file touched: `git diff --name-only` ⊆ {`prd.md`, `docs/adr/0039-*.md`}
- `bash scripts/check-file-size.sh` passes (docs are not capped, but the hook scans the whole tree — confirm nothing else regressed)
</verification>

<success_criteria>
The PRD-first gate is satisfied: every 37F design decision — including the two that contradict the PRD's
prior text and the one (bootstrap `*`) discovered at plan time — is documented with rationale before any
code exists. ADR 0039 records the public tier as a bounded, mitigated hole in MUSR isolation.
</success_criteria>

<output>
Create `.planning/phases/37F-conversation-artifact-sharing-export-inserted/37F-01-SUMMARY.md` when done.
</output>
