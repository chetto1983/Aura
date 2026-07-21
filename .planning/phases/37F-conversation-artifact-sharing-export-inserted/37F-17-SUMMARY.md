---
phase: 37F-conversation-artifact-sharing-export-inserted
plan: 17
subsystem: ui
tags: [share, react, tanstack-query, vitest, settings, artifacts-panel, i18n]

requires:
  - phase: 37F-15
    provides: "shareApi.ts client, shareTypes.ts wire types, shareViewModel.ts (daysUntil/isLinkExpired/formatShareDate/absoluteShareUrl), RevokeConfirmDialog"
  - phase: 37F-10
    provides: "the live GET/DELETE /api/shares HTTP surface (shareLinkResponse wire shape) this plan's queries and mutations call"
provides:
  - "SharedSection.tsx — the 'Condiviso' per-thread section 37B deferred, mounted inside ArtifactsPanel without adding a prop"
  - "useThreadShares.ts — thread-scoped useQuery + pure exported selectActiveShares (drops revoked, keeps expired, sorts newest-first)"
  - "ShareLinkRow.tsx — the row shared by both management surfaces: tier badge, created date, relative expiry with absolute-date title, internal-only copy-link, confirmed revoke"
  - "SharedLinksSection.tsx — the global Settings shared-links list with confirmed per-row and bulk revoke, aria-live announcement, role=alert load error"
affects: [37F-18, 37F-19, 37F-20]

tech-stack:
  added: []
  patterns:
    - "Shared row component (ShareLinkRow) consumed by two independent list surfaces instead of duplicated markup"
    - "Pure exported `select` projection (selectActiveShares) mirroring useThreadArtifacts.ts's selectAgentArtifacts, unit-tested without a render"
    - "Query-key prefix convention: ['shares'] (global) vs ['shares', threadId] (thread-scoped) — a single invalidateQueries({queryKey: ['shares']}) covers both via React Query's default prefix matching"
    - "Client-side fan-out (Promise.allSettled) over an existing single-item endpoint when no bulk endpoint exists, rather than inventing a new backend route"

key-files:
  created:
    - web/src/chat/share/useThreadShares.ts
    - web/src/chat/share/ShareLinkRow.tsx
    - web/src/chat/share/SharedSection.tsx
    - web/src/chat/share/SharedSection.test.tsx
    - web/src/settings/SharedLinksSection.tsx
    - web/src/settings/SharedLinksSection.test.tsx
  modified:
    - web/src/chat/artifacts/ArtifactsPanel.tsx
    - web/src/settings/SettingsWorkspace.tsx
    - web/src/i18n/resources.share.ts

key-decisions:
  - "ShareLinkRow extracted as a genuinely shared component (not copy-pasted) between SharedSection and SharedLinksSection, per Task 2's explicit instruction."
  - "Conversation title left unset on every SharedLinksSection row: the already-shipped GET /api/shares response (internal/agui/share_api.go, plan 37F-10) deliberately excludes a conversation identifier from the wire ('none of which belong on the wire' per its own doc comment). Extending that Go DTO is a backend change explicitly out of this frontend-only plan's scope. Verified this does not block the plan's actual goal: must_haves.truths requires only tier/created/relative-expiry/revoke, not a title — the row ships everything the authoritative spec asks for. ShareLinkRow's `title` prop stays optional so a future plan can wire it through with a one-line change."
  - "'Revoke all' fans the existing single-delete route out client-side via Promise.allSettled rather than calling a bulk-revoke endpoint, because the shipped 37F-10 route table has exactly 4 owner CRUD routes and no bulk route — adding one is a Go change out of scope for this plan."
  - "SharedSection mounts unconditionally inside ArtifactsPanel with no dedicated query-error UI (mirrors ArtifactsPanel's own existing artifacts-query error handling, which also has none). Verified empirically before wiring this in: under this project's real vitest+jsdom runtime, an unmocked relative-URL fetch('/api/shares...') rejects in ~1ms with a TypeError (no base URL to resolve against), which React Query swallows into an ordinary error state — never an unhandled rejection, never a hang. This keeps the existing, unedited ArtifactsPanel.test.tsx green with zero modifications."
  - "SharedLinksSection registered in the (admin-gated) SettingsWorkspace.tsx using the exact self-contained top-level <section> shape TelegramSettingsPanel.tsx already establishes (its own border-t divider, no wrapping div from the parent) — no new settings-registration mechanism invented."

requirements-completed: [WEBSHARE-02]

coverage:
  - id: D1
    description: "The 'Condiviso' per-thread section 37B deferred now exists in ArtifactsPanel, listing the thread's active shares with tier, created, and relative expiry, without adding a prop to ArtifactsPanel"
    requirement: "WEBSHARE-02"
    verification:
      - kind: unit
        ref: "web/src/chat/share/SharedSection.test.tsx#SharedSection renders rows newest-first with tier, created date, and relative expiry (absolute date on title)"
        status: pass
      - kind: other
        ref: "grep -A3 'interface ArtifactsPanelProps' web/src/chat/artifacts/ArtifactsPanel.tsx (still exactly threadId/onClose)"
        status: pass
    human_judgment: false
  - id: D2
    description: "useThreadShares + selectActiveShares: revoked links dropped, expired-but-not-swept links kept and rendered inert, newest-first"
    requirement: "WEBSHARE-02"
    verification:
      - kind: unit
        ref: "web/src/chat/share/SharedSection.test.tsx#selectActiveShares (pure projection) drops revoked links and sorts newest-first"
        status: pass
      - kind: unit
        ref: "web/src/chat/share/SharedSection.test.tsx#SharedSection renders an expired-but-not-swept link as expired and visually inert (no copy)"
        status: pass
    human_judgment: false
  - id: D3
    description: "The global Settings shared-links surface lists every non-revoked share the owner holds, richer than open-webui (tier + created + relative expiry)"
    requirement: "WEBSHARE-02"
    verification:
      - kind: unit
        ref: "web/src/settings/SharedLinksSection.test.tsx#SharedLinksSection lists non-revoked links newest-first with tier, created, and relative expiry"
        status: pass
      - kind: unit
        ref: "web/src/settings/SharedLinksSection.test.tsx#SharedLinksSection lists with no thread filter (the global surface)"
        status: pass
    human_judgment: false
  - id: D4
    description: "Per-row and revoke-all are both confirmation-gated (no one-click revoke anywhere); revoking announces via aria-live; a load error renders in role=alert with working retry"
    requirement: "WEBSHARE-02"
    verification:
      - kind: unit
        ref: "web/src/settings/SharedLinksSection.test.tsx#SharedLinksSection confirms per-row revoke, then removes the row from the list"
        status: pass
      - kind: unit
        ref: "web/src/settings/SharedLinksSection.test.tsx#SharedLinksSection confirms revoke-all before clearing the list, and announces via aria-live"
        status: pass
      - kind: unit
        ref: "web/src/settings/SharedLinksSection.test.tsx#SharedLinksSection cancelling per-row revoke never calls revokeShare"
        status: pass
      - kind: unit
        ref: "web/src/settings/SharedLinksSection.test.tsx#SharedLinksSection renders a load error in role=\"alert\" with a working retry"
        status: pass
    human_judgment: false
  - id: D5
    description: "No plaintext token ever rendered; internal rows resolve /shared/{id} from the id directly (never from a server field); public rows never offer a copy affordance"
    requirement: "WEBSHARE-02"
    verification:
      - kind: unit
        ref: "web/src/chat/share/SharedSection.test.tsx#SharedSection offers a copy-link on an internal row, resolving /shared/{id}"
        status: pass
      - kind: unit
        ref: "web/src/chat/share/SharedSection.test.tsx#SharedSection offers no copy-link on a public row (listShares never returns its token)"
        status: pass
      - kind: other
        ref: "grep -ciE token / grep -ciE swept|sweep / grep -c \"/s/\" web/src/chat/share/SharedSection.tsx (all 0)"
        status: pass
    human_judgment: false
  - id: D6
    description: "Both new surfaces read visually consistent with their host UI (ArtifactsPanel's staggered-reveal/glyph-plate-empty-state idiom; the Settings section's existing panel shape) — a visual/aesthetic judgment automated tests cannot make"
    human_judgment: true
    rationale: "Visual polish and layout correctness (spacing, staggered animation feel, badge color choices) are judgment calls; automated tests only prove structure/behavior, not that it looks right in the running app."

duration: ~35min
completed: 2026-07-17
status: complete
---

# Phase 37F Plan 17: Shared-Links Management Surfaces Summary

**Two share-management UI surfaces — ArtifactsPanel's per-thread "Condiviso" section (37B's deferred feature) and a global Settings list with confirmed per-row and bulk revoke — sharing one row component and one pure select, both showing tier/created/relative-expiry data open-webui's equivalent surfaces cannot.**

## Performance

- **Duration:** ~35 min
- **Started:** ~2026-07-17T22:25:00Z
- **Completed:** 2026-07-17T23:00:28Z
- **Tasks:** 2
- **Files modified:** 9 (6 created, 3 modified)

## Accomplishments

- `useThreadShares(threadId)` — a thread-scoped `useQuery` mirroring `useThreadArtifacts.ts` exactly, with a pure exported `selectActiveShares` (drops revoked, keeps expired, sorts newest-first) unit-tested with zero renders.
- `SharedSection.tsx` — the "Condiviso" section 37B explicitly deferred to this phase, mounted inside `ArtifactsPanel` below the artifact list, deriving everything from `threadId` via its own hook so the panel's stated props contract (`{ threadId, onClose }`) stays untouched.
- `ShareLinkRow.tsx` — one row component shared by both surfaces: tier badge, created date, a relative expiry with the absolute date on the `title` attribute, an internal-tier-only copy-link resolving `/shared/{id}` (derived directly from the id, never from a server field), and a confirmed revoke.
- `SharedLinksSection.tsx` — the global Settings list of every share the owner holds, with per-row revoke and a "Revoke all" bulk action (client-side fan-out over the existing single-delete route, since no bulk endpoint exists), both confirmation-gated, an `aria-live` revoke announcement, and a `role="alert"` load error with working retry. Registered in `SettingsWorkspace.tsx` using the existing self-contained panel pattern.
- Verified empirically (a disposable probe test against the real vitest+jsdom runtime) that mounting `SharedSection` unconditionally inside `ArtifactsPanel` is safe for the pre-existing, unedited `ArtifactsPanel.test.tsx`: an unmocked relative-URL `fetch()` rejects deterministically in ~1ms under this project's test runtime.

## Task Commits

Each task was committed atomically:

1. **Task 1: useThreadShares + SharedSection — the Condiviso section 37B deferred** - `b197f397` (feat)
2. **Task 2: SharedLinksSection — the global Settings management surface with revoke-all** - `bd5caa95` (feat)

**Plan metadata:** (this commit)

## Files Created/Modified

- `web/src/chat/share/useThreadShares.ts` - thread-scoped share query + pure `selectActiveShares`
- `web/src/chat/share/ShareLinkRow.tsx` - the row shared by both management surfaces
- `web/src/chat/share/SharedSection.tsx` - the "Condiviso" per-thread section
- `web/src/chat/share/SharedSection.test.tsx` - covers every Task 1 `<behavior>` row + the pure select
- `web/src/settings/SharedLinksSection.tsx` - the global Settings shared-links list with revoke-all
- `web/src/settings/SharedLinksSection.test.tsx` - covers every Task 2 `<behavior>` row
- `web/src/chat/artifacts/ArtifactsPanel.tsx` - mounts `<SharedSection threadId={threadId} />` below the artifact list; no prop added
- `web/src/settings/SettingsWorkspace.tsx` - registers `<SharedLinksSection />` after `<ModelSettingsPanel />`
- `web/src/i18n/resources.share.ts` - adds `share.settings.loadError` and `share.settings.revokeError` (en+it)

## Decisions Made

See `key-decisions` in the frontmatter for full rationale. Summary:
- `ShareLinkRow` is a genuinely shared component, not duplicated markup, imported by both surfaces.
- Conversation title is left unset on Settings rows — the shipped `GET /api/shares` wire contract has no `conversation_id`, and extending it is a Go change out of this frontend-only plan's scope. This does not block the plan's actual goal (`must_haves.truths` requires tier/created/expiry/revoke, not a title).
- "Revoke all" is a client-side fan-out over the existing single-delete route (`Promise.allSettled`), since the already-shipped share HTTP surface (37F-10) has no bulk-revoke endpoint.
- `SharedSection` mounts unconditionally with no dedicated query-error UI, verified empirically safe for the untouched `ArtifactsPanel.test.tsx`.
- `SharedLinksSection` is registered using the exact existing top-level Settings panel shape — no new registration mechanism.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 2 - Missing Critical] Added revoke-failure error surfacing (`share.settings.revokeError`)**
- **Found during:** Task 1 design — a destructive-action mutation with zero failure feedback is a silently-swallowed-failure gap (CLAUDE.md Rule 2 example verbatim).
- **Issue:** The plan's `<behavior>` lists specify confirm-gating and a load-error surface (Task 2 only) but no revoke-mutation-failure surface for either task.
- **Fix:** Added a `role="alert"` message (`share.settings.revokeError`, en+it) shown on `revoke.isError` in both `SharedSection.tsx` and `SharedLinksSection.tsx`.
- **Files modified:** web/src/i18n/resources.share.ts, web/src/chat/share/SharedSection.tsx, web/src/settings/SharedLinksSection.tsx
- **Verification:** Type-checked, lint-clean; the happy-path revoke tests continue to pass with the new mutation config in place.
- **Committed in:** b197f397 (Task 1), bd5caa95 (Task 2)

**2. [Rule 3 - Blocking] Fixed an eslint `no-unnecessary-condition` violation in `useThreadShares.ts`**
- **Found during:** Task 1 lint verification.
- **Issue:** `(b.created_at ?? '').localeCompare(a.created_at ?? '')` copied `useThreadArtifacts.ts`'s pattern verbatim, but `ShareLink.created_at` (unlike `Asset.created_at`) is a required `string`, not optional — eslint's `strictTypeChecked` config flagged the `?? ''` fallback as unreachable.
- **Fix:** Simplified to `b.created_at.localeCompare(a.created_at)`.
- **Files modified:** web/src/chat/share/useThreadShares.ts
- **Verification:** `npx eslint` clean; `selectActiveShares` unit tests still pass.
- **Committed in:** b197f397 (Task 1)

---

**Total deviations:** 2 auto-fixed (1 missing critical error-handling, 1 blocking lint fix). **Impact on plan:** Both are small, necessary corrections — no scope creep, no architectural change.

## Known Stubs

**`ShareLinkRow`'s `title` prop is never populated from `SharedLinksSection`.** The already-shipped `GET /api/shares` response (`internal/agui/share_api.go`, plan 37F-10) deliberately does not carry a conversation identifier on the wire (confirmed by reading `shareLinkResponse`'s own doc comment: "Link additionally carries OwnerIdentityID/ConversationID/SnapshotBucket/FormatOptions, none of which belong on the wire"). There is therefore no id available client-side to resolve a title from without extending that Go DTO — a backend change explicitly out of this frontend-only plan's scope (confirmed via my `<web_context>` instructions: "There are NO Go changes").

This is NOT a blocker to this plan's goal: the authoritative `must_haves.truths` in `37F-17-PLAN.md`'s own frontmatter requires only "tier, created, and relative expiry" per row — not a title — and every row ships all three plus revoke. The `title` prop stays on `ShareLinkRow` (optional, currently always `undefined` from `SharedLinksSection`) specifically so a **future plan** that extends `shareLinkResponse` with `conversation_id` (or resolves title another way) can wire it through with a one-line change at the `SharedLinksSection.tsx` call site. No further action is needed from this plan.

## Issues Encountered

None — the conversation-title gap above was investigated, verified against the actual shipped Go source (not assumed), and resolved as a documented scope boundary rather than a blocking problem.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

- Both share-management surfaces are live, tested (196 tests passing across `web/src/chat/share`, `web/src/chat/artifacts`, `web/src/settings`), and wired into their host UIs.
- Coverage: `web/src/chat/share/` 95.32% statements / 90.53% branches; `web/src/settings/SharedLinksSection.tsx` 95.23% statements / 88.88% branches — both clear the ≥85% floor.
- A future plan wanting to show conversation titles in the Settings list needs one small addition: `ConversationID` on `internal/agui/share_api.go`'s `shareLinkResponse` (a Go change), then pass it straight through to `ShareLinkRow`'s existing `title` prop.
- `ChatWorkspaceControls.tsx`'s `ShareToggle` still mounts neutral (`hasInternalShare={false} hasPublicShare={false}`, per its own test's explicit "no share-list data source wired yet" comment) — wiring it to real share state is out of this plan's scope and was not touched.

---
*Phase: 37F-conversation-artifact-sharing-export-inserted*
*Completed: 2026-07-17*

## Self-Check: PASSED

- FOUND: web/src/chat/share/useThreadShares.ts
- FOUND: web/src/chat/share/ShareLinkRow.tsx
- FOUND: web/src/chat/share/SharedSection.tsx
- FOUND: web/src/chat/share/SharedSection.test.tsx
- FOUND: web/src/settings/SharedLinksSection.tsx
- FOUND: web/src/settings/SharedLinksSection.test.tsx
- FOUND: commit b197f397 (Task 1)
- FOUND: commit bd5caa95 (Task 2)
- Re-ran plan-level `<verification>`: `npx vitest run web/src/chat/share web/src/chat/artifacts web/src/settings` → 20 test files, 196 tests passed; `npx tsc --noEmit -p web/tsconfig.json` clean; `npx eslint web/src/chat/share/ web/src/settings/ --max-warnings=0` clean; vitest coverage `web/src/chat/share/` 95.32% stmts / 90.53% branch, `web/src/settings/SharedLinksSection.tsx` 95.23% stmts / 88.88% branch (both ≥85%); `bash scripts/check-file-size.sh` → all 1995 tracked files within the 600-LOC cap; `git diff`/direct read of `ArtifactsPanelProps` confirms no prop added (still exactly `{ threadId, onClose }`).
- Re-ran all `<acceptance_criteria>` grep gates on `web/src/chat/share/SharedSection.tsx`: `token` (0), `swept|sweep` (0), `/s/` (0).
- `npx prettier --check` clean on all 9 touched files; `npx --yes jscpd@4` (project-wide, threshold 0) reports no duplication.
