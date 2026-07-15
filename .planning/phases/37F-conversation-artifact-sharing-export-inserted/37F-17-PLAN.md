---
phase: 37F-conversation-artifact-sharing-export-inserted
plan: 17
type: execute
wave: 5
depends_on: ["37F-15"]
files_modified:
  - web/src/chat/share/SharedSection.tsx
  - web/src/chat/share/useThreadShares.ts
  - web/src/chat/artifacts/ArtifactsPanel.tsx
  - web/src/settings/SharedLinksSection.tsx
  - web/src/chat/share/SharedSection.test.tsx
  - web/src/settings/SharedLinksSection.test.tsx
autonomous: true
requirements: [WEBSHARE-02]

must_haves:
  truths:
    - "The ArtifactsPanel shows a Condiviso section listing this thread's active shares — the section 37B deferred to 37F"
    - "A global Shared links surface in Settings lists every share the owner holds, with revoke"
    - "Each row shows tier, created, and relative expiry — data open-webui's equivalent surface does not have"
    - "An expired-but-not-swept row renders as expired and is visually inert"
    - "Per-row revoke and revoke-all are both confirmed"
    - "ArtifactsPanel's props contract stays exactly { threadId, onClose }"
  artifacts:
    - path: "web/src/chat/share/SharedSection.tsx"
      provides: "the Condiviso section for the ArtifactsPanel"
      min_lines: 50
    - path: "web/src/chat/share/useThreadShares.ts"
      provides: "the thread-scoped share list hook + a pure exported select"
      min_lines: 20
    - path: "web/src/settings/SharedLinksSection.tsx"
      provides: "the global shared-links management surface with revoke + revoke-all"
      min_lines: 80
  key_links:
    - from: "web/src/chat/artifacts/ArtifactsPanel.tsx"
      to: "web/src/chat/share/SharedSection.tsx"
      via: "the section derives from threadId via its own hook — no new prop"
      pattern: "SharedSection"
  prohibitions:
    - "MUST NOT add a prop to ArtifactsPanel — its contract is stated at :9-12 as exactly { threadId, onClose } so AppShell mounts it without re-deriving any contract; the section derives from threadId via its own hook"
    - "MUST NOT grow ArtifactsPanel with the section body — extract it; the header's 'self-contained' claim is the argument"
    - "MUST NOT skip the confirm on per-row revoke — open-webui's no-confirm per-row is defensible for a free link, but Aura's links are capability-gated and audited; treat revoke as destructive consistently"
    - "MUST NOT read the sweep's stamp to decide whether a row is expired — read expires_at; an expired-but-not-swept row must render as expired"
    - "MUST NOT build a bespoke list table — reuse an existing list primitive"
    - "MUST NOT show a raw plaintext token in any list — it is returned once at creation and never re-fetchable"
    - "MUST NOT let any touched file exceed 600 LOC"
---

<objective>
Ship the two share-management surfaces — the per-thread "Condiviso" section that **37B explicitly
deferred to this phase**, and the global Settings list.

open-webui ships both surfaces (a per-thread modal line and a Settings → Data Controls → Shared Chats
modal), and both are needed. But Aura's rows can be **richer than the reference, because Aura has data
open-webui lacks**: it has no `expires_at` column at all, so its list cannot show an expiry. Aura's rows
show tier, created, and a relative expiry.

Two of open-webui's choices are worth inverting:
- **Per-row revoke with no confirm.** Defensible for a free, ungated link. Aura's public links are
  capability-gated and audited — treat revoke as destructive consistently, per-row and bulk alike.
- **"Unshare All" was a post-hoc addition** they shipped after user demand. Ship it up front; it is one
  extra endpoint and real operator value.

The `ArtifactsPanel` constraint is load-bearing: its props contract is *stated as a contract* at `:9-12` —
exactly `{ threadId, onClose }`, so `AppShell` mounts it without re-deriving anything. The section derives
from `threadId` via its own hook.

Purpose: the owner can see and revoke what they have shared.
Output: `SharedSection.tsx`, `useThreadShares.ts`, `SharedLinksSection.tsx`.
</objective>

<execution_context>
@/home/user/Aura/.claude/get-shit-done/workflows/execute-plan.md
@/home/user/Aura/.claude/get-shit-done/templates/summary.md
</execution_context>

<context>
@.planning/phases/37F-conversation-artifact-sharing-export-inserted/37F-RESEARCH.md
@.planning/phases/37F-conversation-artifact-sharing-export-inserted/37F-PATTERNS.md
@web/src/chat/share/shareTypes.ts
@CLAUDE.md
</context>

## Artifacts this plan produces

`SharedSection`, `useThreadShares`, `selectActiveShares`, `SharedLinksSection`.

<tasks>

<task type="auto" tdd="true">
  <name>Task 1: useThreadShares + SharedSection — the Condiviso section 37B deferred</name>
  <read_first>
    - `web/src/chat/artifacts/useThreadArtifacts.ts` — **the hook analog, whole file.** Copy: `useQuery` + a `queryKey` + an `enabled` guard on the thread id + a **pure exported `select`**. Copy the rationale verbatim from its doc — the select is exported *specifically so it unit-tests without a React render*.
    - `web/src/chat/artifacts/ArtifactsPanel.tsx` — **the whole file (160 LOC).** `:9-12` states the props contract as a contract (`exactly { threadId, onClose }`) — read it before touching anything. `:82-95` is the section header with its `uppercase tracking-[0.14em] text-text-faint` type treatment; `:111-129` is the list with the staggered reveal (`animate-in fade-in-0 slide-in-from-bottom-1 fill-mode-backwards` + `animationDelay`); `:147-160` is `EmptyState` with its glyph-plate + two-line copy.
    - `web/src/chat/share/shareApi.ts` + `shareTypes.ts` — `listShares(threadId)` and `ShareLink` (plans 37F-05/15)
    - `web/src/i18n/resources.share.ts` — the section heading + empty-state keys
    - `.planning/phases/37F-conversation-artifact-sharing-export-inserted/37F-RESEARCH.md` §"UI/UX Research" §3 (the two surfaces) + §5 (expiry display: relative with the absolute date on the title/tooltip; expired-but-not-swept renders as "Scaduto" and inert)
  </read_first>
  <behavior>
    - `useThreadShares(threadId)` queries the thread's shares; disabled when `threadId` is empty
    - `selectActiveShares` is a pure exported projection: drops revoked links, sorts newest-first
    - An expired-but-not-swept link is **kept** in the list and marked expired — the list reads `expires_at`, never the sweep's stamp
    - The section renders a heading matching the panel's existing type treatment
    - Zero shares ⇒ the empty state, in the panel's established `EmptyState` shape
    - Each row shows the tier badge, the created date, and a relative expiry with the absolute date on the title attribute
    - An expired row is visually inert (no copy, no update)
    - Each row offers revoke, which opens the confirm dialog
    - Rows reveal with the panel's staggered animation
  </behavior>
  <action>
    Create `web/src/chat/share/useThreadShares.ts` mirroring `useThreadArtifacts.ts`: `useQuery` with a
    `['shares', threadId]` key, an `enabled` guard, and a **pure exported** `selectActiveShares`. Copy the
    exported-select rationale into its doc — it exists so the projection unit-tests without a render.

    Create `web/src/chat/share/SharedSection.tsx` — the "Condiviso" section. It takes `threadId` and
    derives everything else from `useThreadShares(threadId)`.

    **Do not add a prop to `ArtifactsPanel`.** Its header states the contract at `:9-12`: props are exactly
    `{ threadId, onClose }` so `AppShell` mounts it — as a desktop ResizablePanel or a mobile Drawer —
    without re-deriving any contract. Adding a prop breaks that claim. **And do not grow the panel with the
    section body**: extract it into this module and have the panel render `<SharedSection threadId={…} />`.
    The header's "self-contained" claim is the argument for extraction, not against it.

    Reuse the panel's own idioms rather than inventing: the `:82-95` section-header type treatment, the
    `:111-129` staggered list reveal, and the `:147-160` `EmptyState` shape.

    **Expiry display (RESEARCH §5):** relative ("expires in 6 days") with the absolute date on the
    `title`/tooltip. An **expired-but-not-swept** row — the sweep window is real — renders as expired and
    visually inert. Read `expires_at`, **never** the sweep's stamp: the sweep is byte reclamation, not the
    source of truth for whether a link is live.

    Per-row revoke opens `RevokeConfirmDialog` (plan 37F-15). **Confirm on per-row too** — open-webui's
    no-confirm per-row is a defensible speed choice for a free link, but Aura's links are capability-gated
    and audited, so revoke is treated as destructive consistently.

    Refactor-on-touch `ArtifactsPanel.tsx` (160 LOC): dead code, dupl, ≤600, comments updated — same
    commit. If 37B's file carries a comment deferring the section to 37F, retire it here.

    Write `SharedSection.test.tsx` covering every `<behavior>` row, plus a direct unit test of
    `selectActiveShares` with **no render** (the reason it is exported).
  </action>
  <verify>
    <automated>npx vitest run web/src/chat/share web/src/chat/artifacts && npx tsc --noEmit -p web/tsconfig.json && npx eslint web/src/chat/share/</automated>
  </verify>
  <acceptance_criteria>
    - `npx vitest run web/src/chat/share web/src/chat/artifacts` passes, covering every `<behavior>` row; the existing artifacts tests still pass unedited.
    - **The props contract holds:** `ArtifactsPanel`'s prop type is still exactly `{ threadId, onClose }` — `git diff web/src/chat/artifacts/ArtifactsPanel.tsx` shows no prop added.
    - `selectActiveShares` is exported and has a direct unit test that performs **no render**.
    - **The expired-but-not-swept row renders as expired** — tested with a link whose `expires_at` is past and whose `revoked_at` is null.
    - The list reads `expires_at`, not a sweep stamp: `grep -ciE "swept|sweep" web/src/chat/share/SharedSection.tsx` returns `0`.
    - Per-row revoke is confirmed: a test asserts revoke does not fire until the confirm is accepted.
    - No plaintext token is rendered: `grep -ciE "token" web/src/chat/share/SharedSection.tsx` returns `0`, or every match is a non-secret identifier.
    - No literal user-facing strings — all via `t('share.…')`.
    - `wc -l web/src/chat/artifacts/ArtifactsPanel.tsx` ≤ 600; all touched files ≤600.
    - `npx eslint web/src/chat/share/` reports 0 errors; `npx tsc --noEmit` clean.
  </acceptance_criteria>
  <done>The "Condiviso" section 37B deferred now lists the thread's active shares with tier, created, and relative expiry, marks expired-but-not-swept rows inert, confirms per-row revoke, and did it without adding a prop to `ArtifactsPanel` or growing it.</done>
</task>

<task type="auto" tdd="true">
  <name>Task 2: SharedLinksSection — the global Settings management surface with revoke-all</name>
  <read_first>
    - `web/src/settings/` — read the existing settings page structure and pick the established section shape; follow it. Find how existing sections are registered/rendered and mirror that. **Do not invent a new settings-section pattern.**
    - `web/src/chat/share/shareApi.ts` — `listShares()` (no thread filter) and `revokeShare(id)` (plan 37F-15)
    - `web/src/chat/share/SharedSection.tsx` + `useThreadShares.ts` (Task 1) — **reuse the row rendering and the select**; do not duplicate them. If the row is worth sharing between the two surfaces, extract it to a common module in this commit ("REUSABLE CODE — never duplicate; extract a helper").
    - `web/src/conversations/DeleteConfirmDialog.tsx` — the ConfirmDialog wrapper shape for the bulk action
    - `web/src/i18n/resources.share.ts` — the management-surface keys incl. revoke-all + its confirm
    - `.planning/phases/37F-conversation-artifact-sharing-export-inserted/37F-RESEARCH.md` §"UI/UX Research" §3 — the surface comparison, the richer-row recommendation, and the "Unshare All" rationale
  </read_first>
  <behavior>
    - Lists every non-revoked share the owner holds, newest-first
    - Zero shares ⇒ an empty state
    - Each row shows the conversation title, the tier badge, the created date, the relative expiry, and revoke
    - An expired row renders as expired and inert
    - Per-row revoke opens a confirm; confirming removes the row from the list
    - A revoke-all action opens its own confirm; confirming clears the list
    - Revoking announces via an `aria-live` region
    - A load error renders in `role="alert"`
  </behavior>
  <action>
    Create `web/src/settings/SharedLinksSection.tsx` following the existing settings-section shape you read.

    Rows are **richer than open-webui's** because Aura has the data: title, tier badge, created, relative
    expiry, revoke. open-webui's equivalent list has no expiry column at all — it has no `expires_at`.

    **Reuse the row and the select from Task 1** rather than duplicating them; extract a shared row module
    in this commit if that is the cleaner shape (CLAUDE.md: never duplicate; extract a helper). Reuse an
    existing list primitive rather than minting a bespoke table — open-webui itself reuses one shell
    across its archived and shared chat lists.

    **Ship "Revoke all" up front**, with its own ConfirmDialog. open-webui added theirs only after user
    demand; it is one extra call and real operator value. Both the per-row and the bulk action are
    confirmed — consistently destructive treatment.

    Skip ChatGPT's *Settings → Data controls → Shared links → Manage → row → details → Revoke* flow: a
    details step is unnecessary ceremony at Aura's scale. One list, inline revoke.

    After a revoke, the row leaves the list and an `aria-live` region announces it.

    Write `SharedLinksSection.test.tsx` covering every `<behavior>` row, including revoke-all's confirm and
    the expired-row inertness.
  </action>
  <verify>
    <automated>npx vitest run web/src/settings web/src/chat/share && npx tsc --noEmit -p web/tsconfig.json && npx eslint web/src/settings/ && bash scripts/check-file-size.sh</automated>
  </verify>
  <acceptance_criteria>
    - `npx vitest run web/src/settings web/src/chat/share` passes, covering every `<behavior>` row.
    - **Both revoke paths are confirmed:** tests assert neither per-row nor revoke-all fires before its confirm is accepted.
    - **No duplication:** the row rendering and the active-share projection are shared with `SharedSection`, not copy-pasted. `npx jscpd web/src/chat/share web/src/settings` (or the project's configured dupl check) reports no new clone; `golangci-lint`'s `dupl` does not apply to web, so verify by reading that both surfaces import the same row/select module.
    - The section is registered in the settings page using the **existing** section pattern — `git diff` shows no new bespoke settings-registration mechanism.
    - The expired row is inert — tested.
    - No plaintext token is rendered anywhere in the list.
    - `aria-live` announces the revoke.
    - No literal user-facing strings — all via `t('share.…')`.
    - All touched files ≤600 LOC; `bash scripts/check-file-size.sh` exits 0.
    - `npx eslint web/src/settings/` reports 0 errors; `npx tsc --noEmit` clean.
    - vitest coverage for `web/src/chat/share/` and `web/src/settings/SharedLinksSection.tsx` ≥ 85%.
  </acceptance_criteria>
  <done>Settings carries a Shared-links surface listing every owned link with tier, created, and relative expiry, with confirmed per-row revoke and a confirmed revoke-all, reusing Task 1's row and select rather than duplicating them.</done>
</task>

</tasks>

<threat_model>
## Trust Boundaries

| Boundary | Description |
|----------|-------------|
| what the owner believes is shared → what actually is | These two surfaces are the only way an owner audits their own exposure. A list that hides an expired-but-live row, or that omits a share, is a security failure expressed as a UI bug. |
| a revoke click → an irreversible action | Confirmed at both the per-row and bulk level. |

## STRIDE Threat Register

| Threat ID | Category | Component | Disposition | Mitigation Plan |
|-----------|----------|-----------|-------------|-----------------|
| T-37F-78 | Information Disclosure | an owner unaware a live public link exists | mitigate | Two surfaces — per-thread ("Condiviso") and global (Settings) — both listing tier and expiry. The `ShareToggle`'s `data-shared` (plan 37F-14) is the third signal. |
| T-37F-79 | Information Disclosure | a list that reads the sweep's stamp and hides an expired-but-still-listed link | mitigate | Rows read `expires_at`, never the sweep stamp; an expired-but-not-swept row renders as expired and inert. Tested explicitly, and grep-gated against any sweep reference. |
| T-37F-71 | Repudiation | an accidental irreversible revoke | mitigate | ConfirmDialog on **both** per-row and revoke-all — inverting open-webui's no-confirm per-row, because Aura's links are capability-gated and audited. |
| T-37F-11 | Information Disclosure | a plaintext token rendered in a management list | mitigate | The token is returned once at creation and is not re-fetchable; `listShares` returns links without it. Grep-gated. |
| T-37F-80 | Tampering | `ArtifactsPanel`'s stated props contract silently broken | mitigate | The section derives from `threadId` via its own hook; no prop is added. Enforced by a `git diff` check on the prop type. |
| T-37F-SC | Tampering | npm/pip/cargo installs | accept | No new web dependency — existing list/dialog primitives and `@tanstack/react-query` are present. |
</threat_model>

<verification>
- `npx vitest run web/src/chat/share web/src/chat/artifacts web/src/settings` (Windows Git Bash)
- `npx tsc --noEmit -p web/tsconfig.json`
- `npx eslint web/src/chat/share/ web/src/settings/`
- vitest coverage ≥85% for the new modules
- `bash scripts/check-file-size.sh` → 0
- `git diff web/src/chat/artifacts/ArtifactsPanel.tsx` → no prop added
</verification>

<success_criteria>
The 37B-deferred "Condiviso" section exists and the global Settings list exists, both showing tier,
created, and relative expiry — data the reference product cannot show. Expired-but-unswept links are
visible and inert, revoke is confirmed everywhere, no token is ever re-rendered, and `ArtifactsPanel`'s
props contract is intact.
</success_criteria>

<output>
Create `.planning/phases/37F-conversation-artifact-sharing-export-inserted/37F-17-SUMMARY.md` when done.
Record whether a shared row module was extracted and the coverage numbers.
</output>
