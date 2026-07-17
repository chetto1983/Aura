---
phase: 37F-conversation-artifact-sharing-export-inserted
plan: 01
subsystem: docs
tags: [prd-amendment, adr, sharing, conversation-export, identity-isolation, webshare, migration-numbering]

# Dependency graph
requires:
  - phase: 36-multi-user-identity-isolation-authula-cutover
    provides: MUSR whole-origin invariant (404-read/403-mutate, capability_grants + RequireCapability, RLS 0032, random-unguessable-ID discipline) that ADR 0039 carves a bounded exception into
  - phase: 37A-web-artifact-delivery-lane
    provides: identity-scoped objectstore.Store + AssetKey pattern + never-presigned streaming download the share storage design reuses under a disjoint share/ prefix
  - phase: 37B-web-artifact-sidebar
    provides: the "Condiviso" section + share-arrow placeholder ArtifactsShell.tsx explicitly reserved for this phase
  - phase: 42-llm-conversation-compaction
    provides: migrations 0036-0039 (shipped 2026-07-14), which is why this plan's migration slot is 0040, not 0036
provides:
  - PRD Amendment #84 documenting WEBSHARE-01..04, the three fail-closed share tiers (file export / internal bearer-within-auth link / public opt-in expiring token), the share.public capability + AURA_SHARE_PUBLIC_ENABLED kill-switch, the canonical redacted Snapshot model, the shared_links/share_audit schema reserved on migration 0040, and the audit + revoke-on-delete lifecycle design
  - The D-08 PRD-amendment (reasoning/thinking traces DROPPED from the snapshot, permanently) superseding prior PRD text
  - The D-13 PRD-amendment (hash-indexed equality token lookup, NOT a constant-time table scan) superseding prior PRD text
  - The RESEARCH OQ#4 resolution (internal tier = id-addressed authenticated route; public tier = hashed opaque token) recorded as a stated decision with its rationale, unblocking every downstream 37F code plan
  - ADR 0039 recording the public share tier as a deliberate, bounded, mitigated hole in Phase 36's MUSR identity isolation
  - The PRD's migration-numbering source-of-truth block updated to reserve slot 0040 for 37F
affects: [37F-02, 37F-09, 37F-10, 37F-12, 37F-16, all downstream 37F code plans building internal/share]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "PRD-first pre-execution gate amendment (blockquote '▶ Amendment #N' pattern, matching #78/#79/#81/#82/#83) landed as a standalone docs commit before any phase code"
    - "ADR house format (Context/Decision/Consequences/Accepted Residuals/Alternatives Considered/Forward path) applied to a security-tradeoff record, matching ADR 0037/0038"

key-files:
  created:
    - docs/adr/0039-conversation-sharing-vs-identity-isolation.md
  modified:
    - prd.md

key-decisions:
  - "share.public is a net-new, per-user-grantable, off-by-default capability (identity.create precedent), rejecting the governance.write fallback that would force ordinary users through an admin-only grant to share their own chat"
  - "AURA_SHARE_PUBLIC_ENABLED is an env knob (not an aura.settings row) re-checked inside the share-create handler, closing the auth.go:282 loopback fail-open where RequireCapability is a pass-through"
  - "D-08 AMENDED: reasoning/thinking traces are DROPPED from the snapshot permanently - verified structurally impossible to persist today (no conversation_turns column, no llm.Message field, Chunk.Reasoning is stream-only)"
  - "D-13 AMENDED: the public token lookup is hash-indexed equality (WHERE token_hash = $1), NOT a constant-time table scan - the literal 'constant-time compare' reading is slower and no more secure"
  - "D-09 narrowed to agent-produced artifacts only (source_kind='agent'), mirroring selectAgentArtifacts - a user's own upload must never enter a share"
  - "OQ#4 resolved: the internal tier is an id-addressed authenticated route (GET /api/shares/{id}/data, no capability, no owner predicate) while the public tier is a hashed opaque token (GET /s/{token}/data) - D-03's 'no enumerable IDs' is scoped to the unauthenticated surface only, so D-11's locked 'token_hash (public only)' wording stands unamended"
  - "Migration slot is 0040, not the stale 0036 CONTEXT.md originally recorded - Phase 42 shipped 0036-0039 on 2026-07-14, verified on disk at plan time"
  - "Bootstrap operator's '*' wildcard intentionally auto-holds share.public (same rationale as local's '*' in migration 0004); provisioned identities receive only explicit named capabilities, never '*'"
  - "shared_links + share_audit land together in ONE migration (0040) - a partial apply that created shared_links without share_audit would let a share be created unaudited, an SC3 violation"

patterns-established:
  - "PRD amendment blockquote gate pattern for the 37F sub-phase family: a numbered (1)-(N) itemized amendment ending in a 'Scope guard' paragraph, landed as a standalone commit before any implementation code"

requirements-completed: []  # WEBSHARE-01..04 are DOCUMENTED/AUTHORIZED by this gate plan, not implemented. Per project precedent (37E-05 SUMMARY: "requirements mark-complete intentionally NOT run" for foundation-only plans), marking them complete here would be factually wrong - no export/share code exists yet. The terminal 37F plan owns the mark.

coverage:
  - id: D1
    description: "PRD documents WEBSHARE-01..04 (verbatim), the three share tiers, the snapshot model, the shared_links/share_audit tables, and both the D-08 and D-13 amendments, before any 37F code exists"
    requirement: "WEBSHARE-01"
    verification:
      - kind: other
        ref: "grep -o 'WEBSHARE-0[1-4]' prd.md | sort -u | wc -l  => 4"
        status: pass
      - kind: other
        ref: "grep -n '0040' prd.md matches in both the 37F share section and the Persistence migration-numbering block"
        status: pass
      - kind: other
        ref: "grep -q 'share.public' prd.md; grep -q 'AURA_SHARE_PUBLIC_ENABLED' prd.md"
        status: pass
    human_judgment: false
  - id: D2
    description: "PRD resolves RESEARCH OQ#4 explicitly (internal = id-addressed authenticated route, public = hashed opaque token) so a reviewer cannot mistake the internal UUID for a D-03 violation"
    verification:
      - kind: other
        ref: "grep -q 'api/shares/{id}/data' prd.md"
        status: pass
      - kind: other
        ref: "grep -n 'isPublicShareRoute' prd.md and grep -n 'shared_links_tier_shape' prd.md both present in the OQ#4 rejection rationale"
        status: pass
    human_judgment: false
  - id: D3
    description: "ADR 0039 records the public tier as a deliberate, bounded hole in MUSR identity isolation with all seven fail-closed mitigations named by mechanism"
    verification:
      - kind: other
        ref: "test -f docs/adr/0039-conversation-sharing-vs-identity-isolation.md"
        status: pass
      - kind: other
        ref: "grep -cE 'share\\.public|AURA_SHARE_PUBLIC_ENABLED|expires_at|revoke|SHA-256|snapshot' docs/adr/0039-*.md  => 28 (>= 6 required)"
        status: pass
      - kind: other
        ref: "grep -c 'Rejected' docs/adr/0039-*.md => 6 (>= 5 required)"
        status: pass
    human_judgment: false

duration: 25min
completed: 2026-07-17
status: complete
---

# Phase 37F Plan 01: PRD-Amendment + ADR Gate Summary

**PRD Amendment #84 (WEBSHARE-01..04, three fail-closed share tiers, redacted Snapshot model, shared_links/share_audit on migration 0040, D-08/D-13 supersessions, OQ#4 resolution) + ADR 0039 (public tier as a bounded, mitigated MUSR hole) — the PRD-first gate authorizing every line of 37F code**

## Performance

- **Duration:** 25 min
- **Started:** 2026-07-17T07:33:07Z
- **Completed:** 2026-07-17T07:57:53Z
- **Tasks:** 2 completed
- **Files modified:** 2 (`prd.md`, `docs/adr/0039-conversation-sharing-vs-identity-isolation.md`)

## Accomplishments

- Landed PRD Amendment #84: a 17-item pre-execution gate documenting WEBSHARE-01..04 verbatim, the three fail-closed share tiers (file export / internal bearer-within-auth link / public opt-in expiring token), the `share.public` capability (rejecting `governance.write` reuse), the `AURA_SHARE_PUBLIC_ENABLED` kill-switch closing the `auth.go:282` loopback fail-open, the canonical redacted `Snapshot` model, the nine verified redaction leak sources (L-01..L-09), the `shared_links`/`share_audit` schema reserved on migration `0040`, the audit union 4th leg, and the revoke-on-delete + lazy-expiry + sweep lifecycle.
- Recorded two amendments that **supersede prior PRD text** so a later reviewer cannot silently revert them: D-08 (reasoning/thinking traces are structurally verified absent and are DROPPED from the snapshot permanently) and D-13 (the public token lookup is hash-indexed equality `WHERE token_hash = $1`, not a constant-time table scan).
- Resolved RESEARCH's Open Question #4 — the one item the entire 37F plan set depended on — as a stated decision: the internal tier is an id-addressed authenticated route (`GET /api/shares/{id}/data`, no capability, no owner predicate, audited) while the public tier is a hashed opaque token (`GET /s/{token}/data`); recorded all four reasons this is correct and explicitly rejected OQ#4's own alternative (a token for the internal tier too) on its security merits.
- Updated the PRD's own §Persistence "Migration numbering — fonte di verità" block to reserve slot `0040` for 37F, re-verifying on disk that Phase 42 shipped `0036`-`0039` on 2026-07-14.
- Wrote `docs/adr/0039-conversation-sharing-vs-identity-isolation.md` in the house ADR format (matching ADR 0037/0038's section shape), recording the public share tier as a deliberate, bounded hole in Phase 36's MUSR whole-origin invariant, naming all seven fail-closed mitigations by mechanism, stating the honest consequences (what a token holder gets, that revoke cannot reach already-cached copies, the bootstrap-`*` fact), listing six rejected alternatives, and including the open-webui posture-comparison table showing Aura is strictly stronger on every security axis.

## Task Commits

Each task was committed atomically:

1. **Task 1: PRD-amendment — the 37F share surface, tiers, snapshot model, schema, the two contradicting amendments, and the OQ#4 link-format resolution** - `9a68e36e0` (docs)
2. **Task 2: ADR 0039 — sharing vs. identity isolation (the bounded MUSR hole)** - `08252cd38` (docs)

_No TDD tasks in this plan — it is documentation-only per the plan's own prohibition ("MUST NOT write any Go/TS/SQL code in this plan")._

## Files Created/Modified

- `prd.md` - Added Amendment #84 (17-item WEBSHARE pre-execution gate) after Amendment #83, and updated the §Persistence migration-numbering source-of-truth block to reserve migration `0040` for 37F
- `docs/adr/0039-conversation-sharing-vs-identity-isolation.md` - New ADR (185 lines): the public share tier as a bounded, mitigated hole in MUSR identity isolation, seven named fail-closed mitigations, honest consequences, six rejected alternatives, open-webui posture comparison

## Decisions Made

All decisions were pre-locked in `37F-CONTEXT.md` (D-01..D-15) or resolved by `37F-RESEARCH.md` (OQ1-OQ5); this plan's job was to transcribe them into the PRD/ADR as the authorizing gate, not to make new architectural calls. The one net-new item verified and recorded at plan time (not previously documented anywhere): the bootstrap operator's `*` wildcard capability auto-passes `share.public` (`serve_bootstrap.go:176-180` + `capability_grants.sql:22`), while provisioned onboarding identities receive only explicit named capabilities (`serve_onboarding.go:152-165`) and `retireLegacyLocalIdentityForAuthulaUser` does not migrate `capability_grants` at all — recorded as item (14) in the amendment so downstream test suites know to use provisioned, non-wildcard identities for capability assertions.

## Deviations from Plan

None - plan executed exactly as written. Every one of the plan's 17 required amendment items and both tasks' full acceptance-criteria checklists were verified via direct code reads (not assumed from RESEARCH.md alone) before being written into the PRD/ADR: `serve_webui.go:271-278` (identity.create precedent), `0026_local_admin_caps.up.sql` (governance.write rejection rationale), `internal/identity/store.go:33` (capability grammar), `internal/settings/settings.go` (16-key AllowedKeys), `internal/agui/auth.go:281-298` (RequireCapability + the loopback pass-through), `0005_conversations.up.sql` (no reasoning column), `internal/llm/client.go` (no reasoning field, stream-only Chunk.Reasoning), `internal/reasoningtrace/reasoningtrace.go` (operator debug JSONL), `internal/agent/event.go:69-72` (ArtifactDelta), `web/src/chat/artifacts/useThreadArtifacts.ts:33-38` (selectAgentArtifacts), `web/src/chat/sseAdapter.ts:330-364` (client-side path strip), `internal/runner/runner_delete.go:38-84` (DeleteConversationLifecycle), `cmd/aura/serve_bootstrap.go:160-188` (bootstrap `*` grant), `internal/db/queries/capability_grants.sql` (HasCapability wildcard), `cmd/aura/serve_onboarding.go:140-174` (named-capability grant loop), `cmd/aura/serve_auth.go:189-265` (retireLegacyLocalIdentityForAuthulaUser, confirmed no capability_grants migration), `internal/agent/tools/send_file.go` (path leak descriptor), `internal/db/migrations/0011_tool_invocations.up.sql` (args_raw/result_preview/result_sidecar_path/error/meta columns), and `docs/adr/0037-*.md` / `0038-*.md` (house ADR format). The on-disk migration floor was independently re-verified (`ls internal/db/migrations/ | tail`) rather than trusted from RESEARCH.md's prior-day snapshot, confirming `0039` is the floor and `0040` is free.

## Issues Encountered

None. A brief run interruption (API error) occurred after both task commits had already landed but before this SUMMARY was written; the coordinator verified on disk that `9a68e36e0` and `08252cd38` were intact and no rework was needed.

## User Setup Required

None - no external service configuration required. This plan is documentation-only.

## Next Phase Readiness

- The PRD-first gate is satisfied: every 37F design decision, including the two that contradict the PRD's prior text (D-08, D-13) and the one discovered at plan time (bootstrap `*`), is now documented with rationale before any `internal/share` code exists.
- ADR 0039 records the public tier's seven fail-closed mitigations and its accepted residuals, giving downstream plans (37F-02 onward, building `internal/share`, the AG-UI routes, the web `ShareToggle`/`SharePage`) a settled, reviewable authorization to build against.
- The route table in `37F-01-PLAN.md` (internal vs. public addressing) is now backed by a PRD-recorded rationale, so plans 37F-10/12/16 can mount `registerShareRoutes` and `isPublicShareRoute` without re-litigating OQ#4.
- No blockers. `requirements-completed` is intentionally empty; WEBSHARE-01..04 remain `[ ]` in REQUIREMENTS.md until a later, implementation-bearing 37F plan proves them live (matching the 37E-05 precedent for foundation/gate-only plans).

---
*Phase: 37F-conversation-artifact-sharing-export-inserted*
*Completed: 2026-07-17*
