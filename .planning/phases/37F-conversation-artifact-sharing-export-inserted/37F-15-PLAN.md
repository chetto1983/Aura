---
phase: 37F-conversation-artifact-sharing-export-inserted
plan: 15
type: execute
wave: 4
depends_on: ["37F-14"]
files_modified:
  - web/src/chat/share/ShareModal.tsx
  - web/src/chat/share/RevokeConfirmDialog.tsx
  - web/src/chat/share/shareApi.ts
  - web/src/chat/share/ShareModal.test.tsx
  - web/src/chat/share/shareApi.test.ts
  - web/src/AppShell.tsx
autonomous: true
requirements: [WEBSHARE-02]

must_haves:
  truths:
    - "The tier is chosen BEFORE minting — the internal option is preselected and public is never preselected"
    - "The public warning renders only when the public tier is selected"
    - "The warning states that revoking prevents new access but does not delete copies already seen or cached by search engines"
    - "Expiry chips appear for the public tier with 7 days preselected"
    - "The URL is rendered after minting and copied by a separate user gesture — no clipboard write after an await"
    - "The internal tier's URL is /shared/{id} and the public tier's is /s/{token} — the modal renders the right shape per tier and never a /s/{token} form for an internal link"
    - "The stale-snapshot affordance tells the user how many new messages are missing from the link"
    - "Revoke requires a confirm dialog"
    - "The tier selector is a real fieldset + legend + radio group, and the warning is aria-describedby-linked to the public option"
  artifacts:
    - path: "web/src/chat/share/ShareModal.tsx"
      provides: "the share modal state machine + tier/expiry/warning/copy/update/revoke UI"
      min_lines: 120
    - path: "web/src/chat/share/RevokeConfirmDialog.tsx"
      provides: "the destructive-action confirm"
      min_lines: 25
    - path: "web/src/chat/share/shareApi.ts"
      provides: "the typed share API client"
      min_lines: 40
  key_links:
    - from: "web/src/chat/share/ShareModal.tsx"
      to: "web/src/chat/share/shareApi.ts"
      via: "create / update / revoke calls"
      pattern: "createShare|updateShare|revokeShare"
  prohibitions:
    - "MUST NOT preselect the public tier — D-01: public is never default"
    - "MUST NOT render an internal link as /s/{token} — an internal share has NO token (migration 0040's CHECK forces token_hash IS NULL for tier='internal'), so that URL cannot resolve. Internal is /shared/{id}; public is /s/{token}. See PRD item 17 / RESEARCH OQ#4."
    - "MUST NOT render the public warning unconditionally — a warning shown always is a warning nobody reads"
    - "MUST NOT mint first and configure access after — that is open-webui's IA and it inverts D-01's explicit opt-in"
    - "MUST NOT call navigator.clipboard.writeText after an await — Safari loses the user gesture. Mint, render the URL, then copy on a direct gesture. Do NOT port open-webui's ClipboardItem workaround."
    - "MUST NOT render revoke as an inline text link — it is destructive and irreversible; it gets its own row and a ConfirmDialog"
    - "MUST NOT build a bespoke modal — the Dialog primitive is portal + focus-trap + Esc already"
    - "MUST NOT use divs with click handlers for the tier selection — a real fieldset/legend/radio group is required"
    - "MUST NOT emit aria-invalid=\"false\" — omit when valid via cond || undefined"
    - "MUST NOT use async onClick — use onClick with a void-wrapped call"
    - "MUST NOT let AppShell.tsx exceed 600 LOC"
---

<objective>
Build the share modal — the phase's densest UI and the place where D-01's "public is never default"
either becomes real or quietly inverts.

**open-webui's IA is the anti-pattern here, and it is worth being explicit about why**, because the modal
is otherwise the obvious thing to copy:
- It **mints first, then offers access control** — `AccessControl` is rendered only once the chat is
  already shared. That inverts D-01: you cannot choose the tier *before* creating the link.
- **Revoke is a text link buried mid-sentence** — a destructive, irreversible action rendered as inline
  prose.
- Its Safari clipboard workaround (wrapping the URL promise in a `ClipboardItem`) exists because
  `writeText` **after an await** loses the user gesture. Restructuring — mint, render the URL, copy on a
  separate gesture — sidesteps the bug entirely *and* is better UX, because the user sees the URL.

Aura's differentiator is cheap and real: open-webui and ChatGPT cannot tell you whether new turns exist,
so they always offer "Update". Aura **can** — compare `conversations.last_active_at` against
`shared_links.updated_at`. The data already exists; it costs no new storage.

Purpose: the owner chooses a tier deliberately, sees the consequence, and can revoke.
Output: `ShareModal.tsx`, `RevokeConfirmDialog.tsx`, `shareApi.ts`.
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

`ShareModal`, `RevokeConfirmDialog`, `createShare`, `listShares`, `updateShareSnapshot`, `revokeShare`.

<tasks>

<task type="auto" tdd="true">
  <name>Task 1: shareApi.ts — the typed client</name>
  <read_first>
    - `web/src/chat/share/shareTypes.ts` — `ShareLink` and `Snapshot` (plan 37F-05); the contract this client returns
    - `internal/agui/share_api.go` (plan 37F-10) — the real routes, request shapes, and status codes. Read them; do not infer.
    - `web/src/documents/` — find the existing library-document API client module (the one `DocumentUploadDialog` calls, e.g. `uploadLibraryDocument`) and follow its fetch/error/`credentials` conventions exactly. Do not invent a new HTTP idiom.
  </read_first>
  <behavior>
    - `createShare(threadId, tier, expiryOption)` posts to the share route and returns the created `ShareLink`; for the **public** tier the response carries the one-time plaintext URL (`/s/{token}`), and for the **internal** tier the URL (`/shared/{id}`) is derivable from the returned `id` and carries no secret
    - `listShares(threadId?)` returns the owner's links, optionally filtered to one thread
    - `updateShareSnapshot(id)` re-snapshots and returns the updated link
    - `revokeShare(id)` revokes and resolves void
    - A non-2xx response throws an `Error` carrying the server's message
    - Every call is same-origin and abortable via an `AbortSignal`
  </behavior>
  <action>
    Create `web/src/chat/share/shareApi.ts` with the four functions above, typed against `shareTypes.ts`.

    Follow the existing document-API client's conventions for `fetch`, `credentials`, error shaping, and
    `AbortSignal` threading — do not invent a second HTTP idiom in this codebase.

    Note in the header that the plaintext token is returned by `createShare` **once** and is never
    re-fetchable: `listShares` returns links without it. That is D-13's "shown to the owner once at
    creation; thereafter it lives only in the URL," and it is why the modal must render the URL
    immediately rather than expecting to load it later.

    **State the per-tier URL shapes in the header too, because they are not symmetric** (PRD item 17 /
    RESEARCH OQ#4): a **public** link is `/s/{token}` and is one-time-only; an **internal** link is
    `/shared/{id}`, derived from the `id` the caller already holds, carries **no secret**, and is
    therefore re-derivable at any time — including from `listShares`. An internal link is **never**
    `/s/{token}`: migration 0040's CHECK forces `token_hash IS NULL` for `tier='internal'`, so there is no
    token to build that URL from and it would resolve nowhere.

    Write `shareApi.test.ts` (vitest) with a mocked `fetch` covering every `<behavior>` row, including the
    non-2xx throw and the abort path.
  </action>
  <verify>
    <automated>npx vitest run web/src/chat/share && npx tsc --noEmit -p web/tsconfig.json</automated>
  </verify>
  <acceptance_criteria>
    - `npx vitest run web/src/chat/share` passes, covering all 6 `<behavior>` rows.
    - The client is typed against `shareTypes.ts` and declares no local duplicate of `ShareLink`/`Snapshot`.
    - Non-2xx throws with the server's message; asserted by test.
    - The header states the token is returned once and is not re-fetchable.
    - `npx tsc --noEmit -p web/tsconfig.json` clean; `npx eslint web/src/chat/share/shareApi.ts` reports 0 errors.
  </acceptance_criteria>
  <done>`shareApi.ts` wraps the four share routes with the codebase's existing fetch conventions, types from `shareTypes.ts`, abort support, and a documented once-only token.</done>
</task>

<task type="auto" tdd="true">
  <name>Task 2: ShareModal — tier before mint, conditional warning, gesture-safe copy, stale detection</name>
  <read_first>
    - `web/src/documents/DocumentUploadDialog.tsx` — **the whole file (101 LOC): the Dialog-with-state-and-async-action template at almost exactly this complexity.** Copy: the `@/components/ui/dialog` barrel import (**do not build a modal** — the primitive is portal + focus-trap + Esc already); `{ open, onOpenChange, on<Done> }` props all `readonly`; the `Dialog > DialogContent > DialogHeader(DialogTitle + DialogDescription) > body > DialogFooter` structure; the guard-clause + `try/catch/finally` async handler with `setError` in `catch` and the flag reset in `finally`; `role="status"` for progress and `role="alert"` for errors; `variant="outline"` Cancel then the primary action in `DialogFooter`; `onClick={() => void action()}` (**never** `async onClick`); `disabled` on the primary while in flight.
    - `web/src/chat/artifacts/ArtifactsPanel.tsx:121-122` — the motion idiom to reuse for the warning/expiry reveal: `animate-in fade-in-0 slide-in-from-bottom-1 fill-mode-backwards` with a staggered `animationDelay`
    - `web/src/i18n/resources.share.ts` — every string key (plan 37F-05). Use `t('share.…')`; never a literal.
    - `web/src/chat/share/shareTypes.ts` + `shareApi.ts` — the types and calls
    - `web/src/components/ui/` — check whether a radio-group primitive exists. **PATTERNS says none does.** If absent, use native `<input type="radio">` inside a `<fieldset>` — which RESEARCH §6 mandates for a11y anyway. Do not add a dependency for it.
    - `.planning/phases/37F-conversation-artifact-sharing-export-inserted/37F-RESEARCH.md` §"UI/UX Research" §2 (**the design source — the modal layout, the state machine, and every key design decision with its reason**) + §5 (revoke/expiry UX) + §6 (accessibility)
  </read_first>
  <behavior>
    - **States:** `idle → creating → shared → updating → revoking → revoked`
    - Opening on an unshared thread ⇒ `idle`, with the **internal** tier preselected
    - The **public** tier is never preselected
    - Selecting public ⇒ the warning block appears (`role="note"`, warning-toned) and the expiry chips
      appear with 7 days preselected
    - Selecting internal ⇒ the warning and the expiry chips disappear
    - The warning is `aria-describedby`-linked to the public radio, so a screen reader hears it **as part
      of** that option rather than as orphaned prose
    - The snapshot-frozen note is stated **before** minting
    - Create ⇒ `creating` (primary disabled, `role="status"`) ⇒ `shared`
    - In `shared`: the URL renders in a readonly input with a **separate** Copy button; clicking Copy
      swaps the label to "Copied" for ~2s and announces via `aria-live="polite"`
    - In `shared` on the **internal** tier, the rendered URL is `/shared/{id}` (absolute, same origin)
    - In `shared` on the **public** tier, the rendered URL is `/s/{token}` (absolute, same origin)
    - In `shared`: a metadata line shows tier, relative expiry ("expires in 6 days") with the absolute
      date on the title attribute, and the created date
    - When the thread has newer turns than the link's snapshot, a stale note shows the count and
      emphasises Update
    - When there are no newer turns, no stale note renders
    - Update ⇒ `updating` ⇒ back to `shared` with a refreshed snapshot and the **same** URL
    - Revoke opens the confirm dialog; confirming ⇒ `revoking` ⇒ `revoked`; cancelling returns to `shared`
    - An expired-but-not-swept link renders as expired and is visually inert
    - A custom expiry above the cap shows `aria-invalid`; a valid one omits the attribute entirely
    - Errors render in `role="alert"`
  </behavior>
  <action>
    Create `web/src/chat/share/ShareModal.tsx`, modelled on `DocumentUploadDialog.tsx`, implementing the
    state machine and the layout from RESEARCH §UI/UX 2.

    **The four design decisions that must not be softened, each with its reason:**
    1. **Tier is chosen BEFORE minting.** open-webui's mint-then-configure inverts D-01's explicit opt-in.
       The radio group is a real `<fieldset>` + `<legend>` + radios — not divs with click handlers.
    2. **The warning renders only when public is selected.** A warning shown always is a warning nobody
       reads. Include the honesty copy at **mint** time (not only at revoke): revoking prevents new access
       but does not delete copies already seen or cached by search engines. Mint is when the decision is
       made.
    3. **Mint, THEN copy as a separate gesture.** This sidesteps the Safari `ClipboardItem` bug entirely —
       `writeText` after an `await` loses the user gesture — **and** is better UX because the user sees the
       URL. Do **not** port open-webui's workaround; restructure instead.
    4. **Revoke gets its own row and a ConfirmDialog.** open-webui renders it as an inline text link
       mid-sentence. It is destructive and irreversible; treat it as such.

    **The stale-snapshot affordance is the highest-value UX win in this phase and costs no storage:**
    compare the thread's last-activity timestamp against the link's `updated_at` and show "N new messages
    are not in this link", emphasising Update. Derive the count from data already on hand; do not add a
    field or an endpoint for it.

    Motion (CLAUDE.md Frontend_aesthetics): **one orchestrated reveal** — the warning block and the expiry
    row slide in on tier change using `ArtifactsPanel.tsx:121-122`'s existing idiom. A high-impact moment,
    not scattered micro-interactions. Reuse the token palette; the cockpit is **BLUE** and approved — do
    not re-skin.

    Accessibility: `aria-invalid` on the custom-expiry input is **omitted when valid** (`cond || undefined`
    so React drops `aria-invalid="false"`). Copy feedback via `aria-live="polite"` + the label swap —
    inline beats a toast for a control the user is looking directly at, and it needs no toast
    infrastructure. The Dialog primitive already traps focus and returns it on close; do not rebuild it.

    Create `web/src/chat/share/RevokeConfirmDialog.tsx` from
    `web/src/conversations/DeleteConfirmDialog.tsx` — **copy the whole file (~41 LOC)** and swap the
    `conversations.delete.*` keys for `share.revoke.*`. Mirror its header, which states the invariant:
    the destructive action never fires without this confirm, and the shared `ConfirmDialog` is
    portal-backed, focus-trapped, Escape-dismissable, and centered.

    Wire the modal into `AppShell.tsx` via `useSharePanel`'s state — the conditional-mount idiom at
    `:522-524`. **Keep AppShell ≤600 LOC**; if the mount pushes it over, do the refactor-on-touch split in
    THIS commit.

    Write `ShareModal.test.tsx` covering every `<behavior>` row. Split into
    `ShareModal.tsx` + a state-machine module if the file approaches 600 LOC.
  </action>
  <verify>
    <automated>npx vitest run web/src/chat/share && npx tsc --noEmit -p web/tsconfig.json && npx eslint web/src/chat/share/ && bash scripts/check-file-size.sh</automated>
  </verify>
  <acceptance_criteria>
    - `npx vitest run web/src/chat/share` passes, covering every `<behavior>` row.
    - **`TestPublicNeverPreselected` equivalent:** a test asserts that on open, the internal radio is checked and the public radio is NOT — the single most important assertion in this plan.
    - **The warning is conditional:** a test asserts it is absent on open and present only after selecting public.
    - **The warning is `aria-describedby`-linked** to the public radio — asserted by test.
    - **The tier selector is a real radio group:** the DOM contains a `fieldset`, a `legend`, and `input[type=radio]` elements. `grep -c "onClick" web/src/chat/share/ShareModal.tsx` shows no click-handler-on-div tier selection.
    - **No clipboard-after-await:** `grep -n "clipboard" web/src/chat/share/ShareModal.tsx` shows `writeText` reached from a direct click handler with no preceding `await` in that handler; `grep -c "ClipboardItem" web/src/chat/share/ShareModal.tsx` returns `0`.
    - **The URL shape is right per tier:** a test asserts the readonly input holds a `/shared/{id}` URL after an internal mint and a `/s/{token}` URL after a public mint. A modal that renders `/s/…` for an internal link renders a URL that resolves nowhere and fails this criterion.
    - **Revoke is confirmed:** a test asserts revoke does not fire until the confirm dialog is accepted.
    - **The stale note is conditional:** present with newer turns, absent without — both tested.
    - `aria-invalid` is omitted when valid: `grep -q "|| undefined" web/src/chat/share/ShareModal.tsx` and a test asserts the attribute is absent for a valid custom expiry.
    - No `async onClick`: `grep -cE "onClick=\{async" web/src/chat/share/ShareModal.tsx` returns `0`.
    - No literal user-facing strings: every visible string comes from `t('share.…')`.
    - `wc -l web/src/AppShell.tsx` ≤ 600; `bash scripts/check-file-size.sh` exits 0.
    - `npx eslint web/src/chat/share/` reports 0 errors; `npx tsc --noEmit` clean.
    - vitest coverage for `web/src/chat/share/` is ≥85%.
  </acceptance_criteria>
  <done>The modal chooses tier before minting with public never preselected, shows the honesty-copy warning only for public and links it to the radio, mints-then-copies on a separate gesture, detects stale snapshots from existing data, and gates revoke behind a confirm — all at ≥85% vitest coverage.</done>
</task>

</tasks>

<threat_model>
## Trust Boundaries

| Boundary | Description |
|----------|-------------|
| user intent → a public link | The modal is where a private thread becomes world-readable. Every default, every preselection, and every piece of copy is a control on that transition. |
| UI gating → server gating | The modal's tier choice is a convenience, never a boundary. The server owns the capability check and the kill-switch (plans 37F-10/12); a client that hid the public option would still not be a gate. |

## STRIDE Threat Register

| Threat ID | Category | Component | Disposition | Mitigation Plan |
|-----------|----------|-----------|-------------|-----------------|
| T-37F-69 | Elevation of Privilege | a user creating a public link without meaning to | mitigate | Internal is preselected; public is never preselected; the tier is chosen before minting (not open-webui's mint-then-configure). Asserted by test. |
| T-37F-70 | Information Disclosure | a user not understanding that revoke cannot un-ring the bell | mitigate | The ChatGPT-derived honesty copy renders at **mint** time, conditionally on the public tier, `aria-describedby`-linked to the option so a screen reader hears it as part of the choice. |
| T-37F-71 | Repudiation | an accidental irreversible revoke | mitigate | Revoke gets its own row and a portal-backed, focus-trapped `ConfirmDialog` — not open-webui's inline text link. |
| T-37F-72 | Tampering | client-side tier selection mistaken for a security control | accept | Documented: the SPA hide is cosmetic; the server mount + in-handler kill-switch are the boundary (T-36-10-E precedent). No mitigation needed client-side. |
| T-37F-73 | Denial of Service | the Safari clipboard gesture bug silently failing the copy | mitigate | Mint → render URL → copy on a direct gesture, with no `await` in the click handler. The `ClipboardItem` workaround is deliberately not ported. |
| T-37F-SC | Tampering | npm/pip/cargo installs | accept | No new web dependency — the Dialog/ConfirmDialog primitives and native radios cover it; a radio-group library is explicitly not added. |
</threat_model>

<verification>
- `npx vitest run web/src/chat/share` (Windows Git Bash — WSL has no node)
- `npx tsc --noEmit -p web/tsconfig.json`
- `npx eslint web/src/chat/share/ web/src/AppShell.tsx`
- vitest coverage for `web/src/chat/share/` ≥ 85%
- `bash scripts/check-file-size.sh` → 0
- Visual inspection (Manual-Only, per VALIDATION.md): open the modal in all four states — not-shared / internal / public / revoked — and confirm public is never preselected and the warning renders only for public. "Inspect artifact visually, not just PASS status."
</verification>

<success_criteria>
The modal makes D-01 real in the UI: the tier is a deliberate pre-mint choice with internal preselected,
the public warning appears only when it matters and tells the truth about caching, the URL is copied by a
gesture that cannot hit the Safari bug, stale snapshots are detected from data that already exists, and
revoke is treated as the destructive action it is.
</success_criteria>

<output>
Create `.planning/phases/37F-conversation-artifact-sharing-export-inserted/37F-15-SUMMARY.md` when done.
Record the post-edit `wc -l web/src/AppShell.tsx` and the `web/src/chat/share/` coverage.
</output>
