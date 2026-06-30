# Phase 32 — UI Review

**Audited:** 2026-06-30
**Baseline:** Abstract 6-pillar standards + Cockpit brand guide (no UI-SPEC.md; this phase is a behaviour-preserving cleanup)
**Screenshots:** Captured (four Playwright screenshots provided via `uat-evidence/`)
**Scope:** a11y focusTrap dedup, skeleton system unification (3 consumers migrated), getJSON dedup (no visual change)

---

## Pillar Scores

| Pillar | Score | Key Finding |
|--------|-------|-------------|
| 1. Copywriting | 3/4 | All CTAs are action-specific; no generic labels in scope |
| 2. Visuals | 3/4 | Skeleton cards render correctly; governance max-w staggering is visually subtle at wide viewports |
| 3. Color | 4/4 | All tokens — no hardcoded values except idiomatic `bg-black/50` backdrop |
| 4. Typography | 3/4 | Existing scale preserved; standing dual-unit inconsistency (rem vs px) not introduced here |
| 5. Spacing | 3/4 | System-aligned Tailwind scale; SkeletonBlock h/w strings are intentional API usage |
| 6. Experience Design | 3/4 | Good state coverage; two a11y gaps: missing live regions on two skeleton wrappers, and alertdialog semantics on RemoveDialog |

**Overall: 19/24**

---

## Top 3 Priority Fixes

1. **ConversationListSkeleton and SearchPanel loading containers lack `role=status`/`aria-live`** — Screen readers cannot announce "Loading conversations" or "Searching" because neither wrapper carries a live region role. GovernanceView's `BoardStateView` does this correctly (`role="status"` + `aria-label` + `sr-only` span). Fix: add `role="status"` to the `<div>` at `ConversationSidebar.tsx:360` and the loading `<div>` at `SearchPanel.tsx:85`.

2. **RemoveDialog declares `role="dialog"` where `role="alertdialog"` is the correct semantic** — `alertdialog` signals to assistive technology that the dialog contains critical information requiring immediate attention (WAI-ARIA 1.1 §dialog_roles). An irreversible destructive confirmation is precisely the alertdialog use-case. Fix: change `role="dialog"` to `role="alertdialog"` in `McpLifecycleCluster.tsx:231`.

3. **Human keyboard verification of the canonical focusTrap's expanded selector remains open** — The `VERIFICATION.md` explicitly flags this as a pending human check. Code analysis confirms `FOCUSABLE_SELECTOR` covers inputs, links, and `[tabindex]` elements with the `isFocusable` disabled filter, but the static screenshots cannot verify that Tab cycling reaches non-button focusables in the live cockpit. Fix: manually tab through the RemoveDialog and the BoardLayout mobile bottom sheet in a live browser with `<input>` or `[tabindex]` elements present in the trap.

---

## Detailed Findings

### Pillar 1: Copywriting (3/4)

No new copy was introduced by getJSON or focusTrap dedup. The skeleton migration carries no text. The in-scope copy surfaces are pre-existing i18n strings in the changed components.

Positive findings:
- `removeCancel` resolves to "Keep server" / `removeConfirm` to "Remove server" — action-specific, not "Cancel/OK" (`resources.governance.ts:193`)
- `removeTitle` renders as "Remove 'github'?" with the server name interpolated (`resources.governance.ts:189`)
- `removeBody` is descriptive: "This deletes the server from your managed config and unmounts its tools. This can't be undone. An audit entry is recorded." — visible and confirmed in `phase32-focustrap-dialog.png`
- `trustConfirm` = "Trust & approve", `trustCancel` = "Cancel approval" — contextual
- Loading labels use i18n keys (`conversations.loading`, `conversations.search.searching`) — no hardcoded strings

Minor gap (not new to this phase):
- `governance.retry` resolves to "Retry" — minimally specific, does not tell the user what action is retried. Acceptable but could be "Retry loading servers".

Score rationale: All CTAs in scope are action-specific. No "Submit/OK/Cancel" patterns. -1 for the generic "Retry" label (pre-existing, not a regression from Phase 32).

### Pillar 2: Visuals (3/4)

From screenshot evidence:

**ConversationSidebar skeletons** (`phase32-skeleton-sidebar.png`): Four `Card` skeleton items visible. Each card has two `SkeletonBlock` lines (75% width at `1rem` height and 50% width at `0.75rem` height). The two-line layout mirrors the actual conversation item shape (title + subtitle). Visual weight and proportions are appropriate. The `bg-surface-2/40` card wrapper correctly provides context for each placeholder.

**SearchPanel skeletons** (`phase32-skeleton-search.png`): Two `SkeletonBlock` elements at `3.5rem` height with `radius="md"`. The tall proportions match the actual search result button height (`min-h-14` = `3.5rem`). The blocks fill the panel width correctly. Clear feedback that two results are being loaded.

**GovernanceView skeletons** (`phase32-skeleton-governance.png`): Three `SkeletonBlock` elements at `3rem` height with `max-w-xl` / `max-w-lg` / `max-w-2xl` constraints. At the captured viewport width (governance content area ~1040px), the `max-w-lg` (512px) / `max-w-xl` (576px) / `max-w-2xl` (672px) differences are visible but subtle — the first and third block look nearly identical in width. Staggering is present but low-contrast at wide viewports. The skeleton rows don't include a badge/icon placeholder column mirroring the actual MCP server list item shape (which has name + trusted badge + health status side-by-side).

**RemoveDialog** (`phase32-focustrap-dialog.png`): Inline card with danger-red border, clear heading, body text, and two action buttons. Visual hierarchy is correct: the title is the largest element, body text is secondary, buttons are clearly separated. The card position (inline below the action row) is appropriate.

Score rationale: All skeletons render correctly and match content proportions. -1 for the governance skeleton's low-differentiation max-width staggering and the lack of a badge/icon placeholder in the governance rows.

### Pillar 3: Color (4/4)

No hardcoded hex values or `rgb()` literals found in any of the five audited files (`focusTrap.ts`, `McpLifecycleCluster.tsx`, `BoardLayout.tsx`, `ConversationSidebar.tsx`, `SearchPanel.tsx`, `governanceView.tsx`).

Findings:
- `bg-black/50` in `BoardLayout.tsx:107` for the mobile backdrop — idiomatic and intentional; CSS custom properties do not provide a semantic "scrim" token in the cockpit system
- Skeleton CSS (`skeleton.css`) uses only `var(--color-surface-2)`, `var(--color-text)`, `var(--color-border)`, `var(--color-accent)`, `var(--color-surface)`, `var(--color-bg)` — full token compliance
- `--skeleton-refetch-bar::before` uses `color-mix(in srgb, var(--color-accent) 64%, ...)` — accent usage is confined to the refetch progress bar, not decorative elements
- `border-danger` / `text-danger` / `hover:bg-danger/10` in RemoveDialog use semantic danger tokens
- Captured screenshots confirm: neutral gray skeletons, blue active tab accent, green status dots, red danger border — all from tokens

The 60/30/10 split is maintained: neutral dark surface dominates, accent (blue) appears only on the active tab, danger (red) only on the destructive action.

### Pillar 4: Typography (3/4)

Phase 32 introduced zero new text rendering. The skeleton migration replaces `<Skeleton/>` (no text) with `<SkeletonBlock/>` (no text). Assessed against the pre-existing type scale in the changed files.

Sizes in use across audited files:
- `text-[0.75rem]` — smallest label (heading label, checkbox label)
- `text-[0.8125rem]` — secondary text (group headers, search result snippets)
- `text-[13px]` — MCP cluster labels (note: 13px = 0.8125rem, dual-unit inconsistency)
- `text-[15.5px]` — body text in dialogs and empty descriptions
- `text-[20px]` — RemoveDialog title
- `text-[22px]` — GovernanceView empty heading
- `text-sm` — standard body (4 occurrences)

Weights: `font-normal`, `font-medium`, `font-semibold`, `font-display`

Standing inconsistency (pre-existing, not introduced by Phase 32): `text-[13px]` and `text-[0.8125rem]` express the same value in different units across neighboring files. This creates maintainability debt but no visual discrepancy. The size distribution (6 discrete sizes) is at the top of acceptable range (≤6 for a complex operator cockpit).

Score rationale: Typography is preserved with no regression. -1 for the dual-unit inconsistency (`13px` vs `0.8125rem`) as a standing weakness surfaced by the refactored files.

### Pillar 5: Spacing (3/4)

Standard Tailwind spacing is used throughout. No arbitrary `[*px]` or `[*rem]` spacing utilities in layout elements.

Spacing classes found in audited files:
- `gap-2`, `gap-3`: consistent 8px / 12px rhythm
- `p-3`, `p-4`, `p-6`, `p-8`: standard Tailwind scale
- `px-2`, `px-3`, `px-4`: consistent horizontal padding
- `py-1.5`, `py-2`, `py-3`, `py-6`, `py-8`: standard vertical padding

`SkeletonBlock` props use CSS string values (`height="1rem"`, `width="75%"`, `height="3.5rem"`) — these are the designed prop API for the component, not spacing utilities, and should not be flagged as arbitrary values.

Minor observation: The sidebar skeleton uses `Card` wrappers (adds `p-3` internally) while search and governance use bare `SkeletonBlock` — this is intentional visual differentiation, not an inconsistency.

Score rationale: Spacing is system-aligned throughout. -1 for the sidebar skeleton using Card padding while the search/governance skeletons use none, creating slightly different spacing density between skeleton states (a pre-existing pattern choice).

### Pillar 6: Experience Design (3/4)

**State coverage (good):**
- Loading: all three consumers have loading states (skeletons)
- Error: `BoardStateView` handles both `error` (with Retry) and `error-auth` (401) separately; `ConversationSidebar` shows an `Alert variant="destructive"`
- Empty: `ConversationSidebar` has icon + heading + body empty state; `SearchPanel` has no-results state with query interpolated; `GovernanceView` has `BoardStateView empty` path
- Destructive confirmation: RemoveDialog gates the irreversible remove action behind a confirmation

**A11y improvements from Phase 32 (verified):**
- `trapTabKey` now uses `FOCUSABLE_SELECTOR` covering `button, [href], input, textarea, select, [tabindex]:not([tabindex="-1"])` with `isFocusable()` disabled filter — replaces both the button-only query in McpLifecycleCluster and the missing-disabled-filter in BoardLayout
- `focusFirstDescendant` correctly moves focus into the BoardLayout mobile sheet on open
- Focus is restored to `restoreFocusRef` when the sheet closes (`BoardLayout.tsx:72`)
- RemoveDialog focuses the safe "Keep server" action on mount (`cancelRef.current?.focus()` at `McpLifecycleCluster.tsx:211`) rather than the destructive action

**Gap 1 — Missing live regions on ConversationListSkeleton and SearchPanel (WARNING):**
- `ConversationListSkeleton` (`ConversationSidebar.tsx:360`): `<div aria-label={label}>` — no `role="status"`, no `aria-live`. Screen readers will not announce "Loading conversations" when this replaces the conversation list.
- SearchPanel searching container (`SearchPanel.tsx:85`): `<div aria-label={t('conversations.search.searching')}>` — same gap. No live region.
- `BoardStateView` (`governanceView.tsx:56-67`) does this correctly: `role="status"` + `aria-label` + `<span className="sr-only">` for the sr-only announcement text.
- Impact: Users of NVDA/JAWS/VoiceOver are not informed that content is loading in the sidebar or search panel.

**Gap 2 — RemoveDialog uses `role="dialog"` instead of `role="alertdialog"` (WARNING):**
- `McpLifecycleCluster.tsx:231`: `role="dialog"` for a destructive, irreversible confirmation that requires user decision before proceeding
- WAI-ARIA 1.1 defines `alertdialog` as "a type of dialog that contains an alert message" where the alert requires immediate user response — exactly the intent here
- `role="alertdialog"` also prevents some AT from reading other page content while the dialog is open (stronger interrupt semantics)
- The visual and interaction pattern is correct; only the ARIA role is suboptimal

**Gap 3 — Human keyboard tabbing verification is open (WARNING):**
- `VERIFICATION.md` section "Human Verification Required" item 1 confirms that keyboard navigation through the expanded focusable set (non-button elements) in the live cockpit has not been verified
- Code analysis is correct, but the a11y fix's real-world completeness requires live tabbing to be confirmed

---

## Registry Audit

`components.json` is present (shadcn initialized). No third-party registries are listed in the spec or the components.json — only the official shadcn registry at `ui.shadcn.com`. `web/src/components/ui/skeleton.tsx` was retired (deleted) in this phase, so there are no third-party blocks to audit.

Registry audit: 0 third-party blocks — no flags.

---

## Files Audited

- `.planning/phases/32-quality-cleanup-dead-code-shared-helpers/32-08-SUMMARY.md`
- `.planning/phases/32-quality-cleanup-dead-code-shared-helpers/32-08-PLAN.md`
- `.planning/phases/32-quality-cleanup-dead-code-shared-helpers/32-CONTEXT.md`
- `.planning/phases/32-quality-cleanup-dead-code-shared-helpers/32-VERIFICATION.md`
- `web/src/a11y/focusTrap.ts`
- `web/src/components/skeleton/Skeleton.tsx`
- `web/src/styles/skeleton.css`
- `web/src/conversations/ConversationSidebar.tsx`
- `web/src/conversations/SearchPanel.tsx`
- `web/src/governance/governanceView.tsx`
- `web/src/governance/McpLifecycleCluster.tsx`
- `web/src/governance/BoardLayout.tsx`
- `web/src/i18n/resources.governance.ts` (copy verification)
- `web/components.json` (registry audit)
- Screenshots: `uat-evidence/phase32-focustrap-dialog.png`, `phase32-skeleton-sidebar.png`, `phase32-skeleton-search.png`, `phase32-skeleton-governance.png`
