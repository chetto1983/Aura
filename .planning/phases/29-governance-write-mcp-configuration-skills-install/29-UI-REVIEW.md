# Phase 29 — UI Review

**Audited:** 2026-06-21
**Baseline:** 29-UI-SPEC.md (approved design contract)
**Screenshots:** Not captured (no dev server at localhost:3000/5173/8080 — code-only audit)

---

## Pillar Scores

| Pillar | Score | Key Finding |
|--------|-------|-------------|
| 1. Copywriting | 3/4 | install.isError in SkillInstallPanel surfaces emptySource copy instead of governance.error; "Cancel approval" is the trust-cancel label (spec contract not consulted) |
| 2. Visuals | 3/4 | McpServerDetail header uses text-[18px] instead of the contracted 20px Display; content-hash chips in list rows use 12px (spec says strictly 13px mono); no install-button spinner — only opacity-60 on disabled |
| 3. Color | 4/4 | Zero hard-coded hex; accent used only on exactly the five contracted elements; semantic tokens throughout; contrast-check gate updated and 36/36 pairs pass WCAG AA |
| 4. Typography | 2/4 | Three distinct unauthorised sizes in use (12px in list rows, 18px in detail headers — both undeclared in the spec's strict "13/15.5/20px only" gate); required-state chip label renders text-text-muted instead of contracted text |
| 5. Spacing | 3/4 | Consistent use of contracted spacing multiples; one arbitrary-value exception: gap-1.5 (6px) used for dot-to-label gaps — not in the declared xs/sm/md/lg/xl/2xl scale; gap-0.5 (2px) on offending-keys list |
| 6. Experience Design | 3/4 | All five board states (empty/loading/populated/error/error-auth) covered via BoardStateView reuse; submitting states present (button disabled); gap: no visual spinner/busy text on Install/Save/Stage buttons during submit — spec says "button spinner + disabled"; denied-tool per-row marker (MCPW-03 prohibition #6) absent from McpServerDetail tool list |

**Overall: 18/24**

---

## Top 3 Priority Fixes

1. **install.isError in SkillInstallPanel shows emptySource copy on server errors** — An operator who types a valid source and gets a network/backend error (502, auth expiry) sees "Enter a valid source — owner/repo, a URL, or a path." instead of the generic sanitized error. This is actively misleading: the user concludes their source is malformed when the real problem is a service failure. Fix: change the `install.isError` block at `web/src/governance/SkillInstallPanel.tsx:250-253` from `t('governance.skills.install.emptySource')` to `t('governance.error')`.

2. **12px and 18px font sizes violate the spec's strict three-size gate (13/15.5/20px)** — `McpServerDetail.tsx:40` uses `text-[18px]` for the server-name heading (should be `text-[20px]`); `SkillsBoard.tsx:233` and `McpBoard.tsx:170,174` use `text-[12px]` for list-row content-hash chips (should be `text-[13px]` with tighter tracking per spec §Typography). This creates a fourth undeclared size and breaks the contract's "≤4 sizes gate is satisfied with margin" claim. Fix: change all `text-[18px]` occurrences to `text-[20px]` in detail headers (`McpServerDetail`, `SkillDetail`, `TaskRunHistory`); change `text-[12px]` to `text-[13px] tracking-tight` in list-row chips.

3. **No submit spinner on primary CTAs — only opacity-60 disabled** — The spec (`§Surface-by-surface`) explicitly requires "submitting → button spinner + disabled" for Install server, Save changes, Trust & approve, and Stage for approval. All four buttons only apply `disabled:opacity-60` — there is no visual spinner, no text change, and no `aria-busy` during the pending window. An operator clicking Install on a slow server sees no feedback. Fix: add a spinner glyph or animated indicator and change button label to e.g. `t('governance.mcp.install.submitting')` (adding the i18n key in both locales) when `mutation.isPending` is true, or use a CSS animation on an inner `<span>`.

---

## Detailed Findings

### Pillar 1: Copywriting (3/4)

**Confirmed correct:**
- All primary CTAs match spec verbatim: "Install server" / "Save changes" / "Trust & approve" / "Stage for approval" (never bare "Install"/"Activate") — confirmed in `resources.governance.ts` and component t() calls.
- All secondary CTAs match: "Discard install", "Discard changes", "Remove server", "Keep server", "Archive skill", "Restore skill", "Add MCP server", "Edit environment", "Disable server" / "Enable server" — all action-specific verb+noun, no bare Cancel/Submit/OK.
- Destructive confirmation dialog: "Remove «name»?" title + action-specific confirm "Remove server" + safe-action "Keep server" — matches spec precisely.
- Soft-warning heading/body copy matches spec (resources.governance.ts:89-92).
- RISKY banner copy matches spec (resources.governance.ts:137-138).
- Container-isolation note copy matches spec (resources.governance.ts:153).
- Empty states for MCP and Skills match spec (resources.governance.ts:29-30/117-119).
- en+it parity gate shipped as `resources.parity.test.ts` — green in CI.

**Defect 1 — WARNING (wrong error copy on install failure):**
- `web/src/governance/SkillInstallPanel.tsx:250-253`: When `install.isError` is true, the component displays `t('governance.skills.install.emptySource')` = "Enter a valid source — owner/repo, a URL, or a path." This copy is the field-validation error for an empty input, NOT a backend failure message. The spec's error state copy for write surfaces is `t('governance.error')` = "Couldn't complete that. The service may be unavailable. Retry, or check the runtime status." A backend 502, auth expiry, or Writer-gate failure will show the wrong message. Fix: change to `t('governance.error')`.

**Defect 2 — WARNING (trust-cancel label):**
- `resources.governance.ts:103`: `trustCancel` = "Cancel approval". The spec copywriting contract lists secondary CTAs including context-specific labels, but does not explicitly enumerate a trust-cancel verb. "Cancel approval" is a reasonable action-specific label and is NOT a bare "Cancel". This is a minor deviation from the "Cancel approval" label being interpreted as close to a bare-cancel pattern vs the richer "Discard changes" style — marking as WARNING rather than BLOCKER.

### Pillar 2: Visuals (3/4)

**Confirmed correct:**
- Accent primary CTAs clearly distinct from secondary surface-2 buttons — visual hierarchy is present.
- RISKY badge uses warning-toned bordered card (`bg-warning/15 border-warning`) — visually prominent before the submit action.
- Soft-warning card uses `bg-warning/10 border-warning` — visible but not blocking.
- Remove dialog uses `bg-surface-2 border-danger` — appropriate danger framing with safe-action default focus via `cancelRef.current?.focus()`.
- Status is never color-alone: every state has dot + label (enable/disable toggle: dot + text; env states: dot + chip label; terminal chips: dot + label).
- Four-state env rows are visually distinct for missing (danger dot) and placeholder (warning dot); required and optional share neutral dots but differ by context position.
- Mode toggle uses `aria-pressed` with `bg-accent text-on-accent` for active segment.
- Install panel preview block uses distinct `bg-surface-3` to separate "will write to" preview from form fields — clear pre-commit context.

**Defect 1 — WARNING (18px header instead of contracted 20px):**
- `web/src/governance/McpServerDetail.tsx:40`: server-name heading uses `text-[18px]` not `text-[20px]`. The spec §Typography explicitly states "one 20px Display size covers both the panel header and the empty-state heading." At 18px the display-role heading is visually undersized relative to nearby 20px headings in other panels (install panel at line 115 uses `text-[20px]` correctly). Same 18px deviation is in `SkillDetail.tsx:24` and `TaskRunHistory.tsx:39` — these are Phase-28 carry-overs that Phase-29 extends in place.

**Defect 2 — WARNING (no spinner on submitting state):**
- Install/Save/Stage buttons apply `disabled:opacity-60` during submit but show no spinner, animated indicator, or label change. Per spec: "submitting → button spinner + disabled." An operator on a slow backend connection sees a static, slightly-dimmed button with no in-progress feedback. This degrades perceived responsiveness for writes that can take 1-3s (MCP install involves a live probe repro after write).

**Defect 3 — MINOR (denied-tool markers not rendered):**
- The spec §Surface-by-surface §3 MCPW-03 requires: "a tool excluded by the mount-time allowlist renders explicitly with a danger marker + label (`Excluded — destructive/denied`)." The `McpProbeResult` DTO carries only `tool_count` + `detail` + `err` — there is no per-tool list in the DTO. The `McpServerDetail` probe section shows `tool_count` as an integer but never iterates individual tool names, so the denied-tool marker (the `deniedTool` i18n key = "Excluded — destructive/denied tool (not mounted).") never renders in the UI. The key IS defined in both en and it bundles but is unreachable from any component. This is a contract gap: prohibition #6 ("a denied/destructive tool NEVER silently omitted") is not visually enforced in the cockpit.

### Pillar 3: Color (4/4)

**Confirmed correct:**
- Zero hard-coded hex or rgb() values in any Phase-29 source file — all colors via semantic token classes.
- Accent (`bg-accent`, `text-on-accent`) used exclusively on: (1) primary CTA fills (Install server, Save changes, Trust & approve, Stage for approval); (2) `focus-visible:ring-ring` focus rings; (3) `aria-pressed`/`aria-selected` active tab/selected-row states; (4) `border-accent/40` on the approval card shell (existing Phase-25 pattern, within the accent-scarcity gate). No decorative or secondary elements use accent fill.
- Secondary buttons correctly use `bg-surface-2 border-border-strong text-text` — never accent-filled.
- Status tones correctly applied: success (enabled dot, secret-preserved), warning (probe checking, placeholder, soft-warning, RISKY badge, fail-soft probe warning), danger (missing, remove confirmation, denied/destructive marker key), muted (optional, archived, neutral metadata).
- The contrast-check gate was extended for Phase-29 pairs: `warning on surface-2`, `warning on surface-3`, `success on surface-2`, `danger on surface-3`, `on-accent on warning/15 bg` — 36/36 pairs confirmed passing WCAG AA per 29-05-SUMMARY.
- The `bg-warning/10` and `bg-warning/15` tinted backgrounds are used only for the soft-warning card and RISKY badge respectively — consistent with the spec's `bg-warning/10` soft-warning and `bg-warning/15` RISKY band prescriptions.

### Pillar 4: Typography (2/4)

**Spec contract:** exactly three distinct sizes (13px · 15.5px · 20px), exactly two weights (400 · 600).

**Confirmed correct:**
- Font weights: only `font-semibold` (600) and implicit regular (400) — no `font-medium`, `font-bold`, `font-light` found in Phase-29 files.
- Heading/eyebrow style: `text-[13px] font-semibold uppercase tracking-wide text-text-muted` — matches spec §Heading role.
- Body: `text-[15.5px] leading-relaxed` — matches spec §Body role.
- Display headings in install panels: `text-[20px] font-semibold font-display` — correct in McpInstallPanel (line 115), SkillInstallPanel (line 83), RemoveDialog (line 239).
- Mono data values: `font-mono text-[13px]` — env keys, CLI-equivalent, content hash, destination path, source field.
- `tabular-nums` is NOT applied to mono chips in the Phase-29 code — the spec lists `tabular-nums` for Mono role. This is a minor omission.

**Defect 1 — BLOCKER (12px unauthorised fourth size):**
- `web/src/governance/SkillsBoard.tsx:233`: content-hash chips in list rows use `text-[12px]`. Spec §Typography explicitly says "no 12px dense-chip exception (dense Mono chips share the 13px Mono size with tighter tracking)." This is also present in `McpBoard.tsx:170,174` (env-key count/source chip in list rows), `SchedulerBoard.tsx:84`, and `TaskRunHistory.tsx:74,82` — the pattern was established in Phase-28 and carried forward. The Phase-29 WRITE surfaces (SkillsBoard write controls) sit on the same component that uses 12px.

**Defect 2 — BLOCKER (18px unauthorised Display variant):**
- `McpServerDetail.tsx:40`, `SkillDetail.tsx:24`, `TaskRunHistory.tsx:39`: detail pane headers use `text-[18px]` not `text-[20px]`. The spec states "one 20px Display size covers both the panel/detail header and the empty-state heading." The install panels (new in Phase-29) use 20px correctly, creating a visual inconsistency between the install panel header and the read-detail header at 18px.

**Defect 3 — WARNING (required state chip label uses text-muted, should be text):**
- `McpEnvEditForm.tsx:241-245`: `stateTone('required')` returns `'text-text-muted'`. Spec §2 table says: "required (recipe var, present) — `text` label." The optional state should be muted; the required state should use the default text color to signal "this needs attention." Both states currently render identically at the chip label level, reducing the four-state visual distinction.

### Pillar 5: Spacing (3/4)

**Confirmed correct:**
- Standard spacing scale observed throughout: `gap-1` (4px), `gap-2` (8px), `px-3 py-2` (12px — the inherited Phase-28 md token), `gap-4 p-4` (16px), `gap-6` (24px), `p-8` (32px for empty state via BoardStateView).
- Touch target floor respected: every interactive control in Phase-29 components has `min-h-[44px]`; icon-only close buttons have `min-h-[44px] min-w-[44px]`.
- Install panel body padding: `p-4` (16px) — matches spec lg token.
- Env-edit form `flex flex-col gap-4` between sections — correct lg separation.

**Defect 1 — WARNING (gap-1.5 = 6px, not in declared scale):**
- Used for dot-to-label gaps in status chips (McpEnvEditForm:161, McpLifecycleCluster:82, SkillInstallPanel:101, InlineApprovalCard:260, TerminalChip:289). Tailwind `gap-1.5` = 6px is between the xs (4px) and sm (8px) steps. It is an arbitrary value relative to the spec's named scale, even though it is a multiple-of-2 rhythm. The spec declares `gap-1` / `gap-2` as the smallest steps; 6px falls outside this. The visual impact is minor (dot-label micro-spacing) but it is a spec deviation.

**Defect 2 — MINOR (gap-0.5 = 2px, below declared minimum):**
- `McpEnvEditForm.tsx:203`: the offending-keys list uses `gap-0.5` (2px) between keys. This is below the xs=4px floor. The keys are mono chip-sized text; 2px gap is barely perceptible and may cause the list to read as a single run. Fix: use `gap-1` (4px).

**Defect 3 — MINOR (px-1.5 py-0.5 in inline chips):**
- `McpServerDetail.tsx:111`, `McpBoard.tsx:170`: env-key chips and list-row source chips use `px-1.5 py-0.5` (6px / 2px). Same gap-1.5 pattern as above — 6px is not in the declared xs=4px / sm=8px scale. Minor.

### Pillar 6: Experience Design (3/4)

**Confirmed correct:**
- All five board states (loading / empty / populated / error / error-auth) are covered via `BoardStateView` + `boardStatus` reuse in both McpBoard and SkillsBoard.
- Write surfaces cover: submitting (button disabled + opacity-60), success (panel closes + invalidateQueries re-fetch), field-validation error (`aria-invalid` + `role="alert"` inline message, `ariaInvalid(cond)` helper from `a11y/aria`).
- Secrets never in DOM: `McpEnvChip` carries only key + redacted flag; no value field exists in the DTO; env-edit initial value for a secret is the `${KEY}` placeholder token (never the real value).
- Remove dialog is focus-trapped with Escape-dismissable behavior; safe action "Keep server" is default-focused (`cancelRef.current?.focus()`), not the destructive action — NN/g compliance.
- The approval resume bridge (skill install → `/api/approvals` queue → approve → activate) is correctly implemented; the pending tab carries NO run/activate affordance; `BoardStateView` covers the pending-list empty/error state.
- Restore collision is guarded 409-side and surfaces inline `role="alert"` per-row (SkillsBoard:267-270) — clear, row-scoped.
- The `aria-pressed` / `aria-selected` pattern is correct on mode toggles, lifecycle enable/disable, and tab buttons.
- `prefers-reduced-motion` is handled via the inherited `motion.css` guard (no new animation framework added).
- Auth sweep test covers all 13 mutating routes at 401-unauthenticated + 403-ungranteds per 29-05-SUMMARY.

**Defect 1 — WARNING (no visual spinner on submitting state):**
- As noted in Pillar 2: all primary CTAs become `disabled:opacity-60` during pending but show no spinner, `aria-busy`, or label text change. The spec states "submitting → button spinner + disabled." This affects all four primary write CTAs across the four new surfaces. A user on a slow connection cannot distinguish "I accidentally double-clicked and nothing registered" from "the write is in flight." Fix: add an animated spinner glyph (CSS `animate-spin` on an `<span>`) and `aria-busy="true"` when `mutation.isPending` is true.

**Defect 2 — WARNING (denied-tool marker absent in McpServerDetail):**
- MCPW-03 / prohibition #6: "in the detail's tool list, a tool excluded by the mount-time allowlist renders explicitly with a `danger` marker." The `McpProbeResult` DTO (`governanceApi.ts:44-51`) carries only `tool_count` (integer), `detail` (string), and `err` (optional string) — no per-tool list. The backend returns aggregate probe data only; the individual tool names + allowed/denied status are not projected through the thin DTO. The `deniedTool` i18n key ("Excluded — destructive/denied tool (not mounted).") exists in both bundles but is referenced in no TSX component — it is unreachable. Without a `tools: {name: string; denied: boolean}[]` projection from the backend probe endpoint, the cockpit cannot surface individual denied-tool markers. This is a spec gap requiring a backend DTO extension.

**Defect 3 — MINOR (install.isError in SkillInstallPanel shows wrong copy — see Pillar 1):**
- Repeated here: a backend install failure shows source-validation copy, which may cause the user to retry with a different source when the real fix is a service issue.

---

## Registry Safety

Registry audit: 0 third-party blocks checked, not applicable — no shadcn (`components.json` absent), no component registry. All components are hand-rolled over the locked token system. No registry vetting gate runs.

---

## Files Audited

**Phase-29 new components:**
- `web/src/governance/McpInstallPanel.tsx`
- `web/src/governance/McpEnvEditForm.tsx`
- `web/src/governance/McpLifecycleCluster.tsx`
- `web/src/governance/SkillInstallPanel.tsx`

**Phase-29 modified components:**
- `web/src/governance/McpBoard.tsx`
- `web/src/governance/McpServerDetail.tsx`
- `web/src/governance/SkillsBoard.tsx`
- `web/src/approvals/InlineApprovalCard.tsx`
- `web/src/governance/governanceApi.ts`
- `web/src/i18n/resources.governance.ts`
- `web/src/i18n/resources.ts` (approval.skill.* bundle)
- `web/scripts/contrast-check.mjs`

**Context files read:**
- `.planning/phases/29-governance-write-mcp-configuration-skills-install/29-UI-SPEC.md`
- `.planning/phases/29-governance-write-mcp-configuration-skills-install/29-CONTEXT.md`
- `.planning/phases/29-governance-write-mcp-configuration-skills-install/29-01-SUMMARY.md` through `29-05-SUMMARY.md`
- `.planning/phases/29-governance-write-mcp-configuration-skills-install/29-01-PLAN.md`
