---
phase: "02"
slug: "two-roles-and-a-budget"
status: approved
shadcn_initialized: true
preset: "new-york / radix base / neutral base color / lucide icons (components.json, unchanged)"
created: "2026-09-08"
---

# Phase 02 — UI Design Contract

> Visual and interaction contract for Phase 2: Two Roles and a Budget. This is an EXISTING
> cockpit — every rule below extends `web/src/admin/`, `web/src/settings/` and
> `web/src/onboarding/` as they stand today, cited by path:line. Phase 2's new aesthetic
> surface is two-tiered: a per-identity money gauge (reuses the fill-bar shape
> `ContextBudgetGauge.tsx` already established) plus an account-wide **Overview** dashboard
> (§Admin spend dashboard — Overview, added in this revision after the human pointed at
> OpenRouter's own activity dashboard). Both are zero-dependency: the dashboard's tiles,
> sparklines and ranked list follow the CSS/SVG-only convention `ChartDisplay.tsx` already
> established (`// no charting library — never recharts`), not a new charting dependency —
> `web/package.json` carries none today and this phase does not add one.

> **Revision note (2026-09-08, post-approval):** this UI-SPEC was checker-approved (5 PASS /
> 2 FLAG) before the human supplied a screenshot of OpenRouter's activity dashboard and asked
> for the credit surface to read like it, plus a directive to study LibreChat and assistant-ui.
> The Surface Map, Design System, Component Inventory provenance, Spacing, Typography's base
> scale, Registry Safety, and the Roster/Removal/Review-step sections below are **unchanged
> from the approved version** — this revision only extends §Credit panel into a new
> §Admin spend dashboard — Overview section, adds a Visual Focal Point subsection (Dimension 2
> flag), records the 5-size Typography trade-off explicitly (Dimension 4 flag), and reclassifies
> the over-allocation banner's element kind (state-coverage probe miss).

---

## Surface Map — what's new, what's reused, what's retired

Read before the rest of this document; it is the scope contract the rest builds on.

| Surface | State today | Phase 2 |
|---|---|---|
| Identity roster + capability grant/revoke | `web/src/settings/CapabilityAdminPanel.tsx` — a `<select>` roster picker + free-text capability grant/revoke, mounted from `IdentityAccessPanel.tsx:31` | **Retired.** RBAC-03 grants every non-admin capability at provisioning and RBAC-06 refuses the admin pair through the API — there is nothing left for this control to grant or revoke. Locked by CONTEXT D-01 + D-02, not a UI-researcher invention. Replaced by a read-only Admin/Member role badge (see §Roster). |
| Identity removal | No HTTP route, no cockpit control. Reverse saga exists at `internal/agui/deprovision.go`, reachable only from `aura identity deactivate\|purge` (CLI). | **New.** A destructive control on each roster row, per D-05/D-06. |
| Per-identity credit (cap / remaining / spend / reset interval) | Does not exist in the cockpit at all. `RuntimeFooter.tsx` shows the *signed-in user's own turn/session cost*, not a cap or a remaining balance, and it is not admin-scoped. | **New.** Admin-only credit control per identity, per CRED-03/CRED-06. Reuses the `role="progressbar"` fill-bar shape from `ContextBudgetGauge.tsx` — same shape, different denominator (USD cap, not context tokens). |
| Non-admin fallback for the whole Settings surface | Already built: `SettingsWorkspace.tsx:41-58` — a non-admin gets `ProfilePanel` plus an `admin.notAuthorized` explanation card; the rail itself hides admin-only entries via `visibleSections()` (`settingsSections.ts:70-72`). The `identities` section is already `adminOnly: true`. | **Unchanged, reused as-is.** This already answers "how does a non-admin experience the admin surface's absence": hidden from the rail, not a raw 403, not merely disabled-in-place. No new work. |
| Create-identity wizard | `OnboardingWizard.tsx` — 4-phase linear flow: `credentials → capabilities → review → complete` (`onboardingWizardModel.ts:6-8`). The `capabilities` phase renders `CapabilityPicker.tsx`, a checklist over creator-supplied grantable capabilities. | **Capabilities phase removed.** Nothing is left to pick (same reasoning as the roster control above) — `CapabilityPicker.tsx` becomes dead code and must be deleted, not hidden. Wizard becomes 3 phases: `credentials → review → complete`. `ReviewStep.tsx` gains two static (non-interactive) info rows in place of the capability badge list — see §Review step. |
| Turn refusal on zero credit | Generic `RUN_ERROR` renders through `MessagePrimitive.Error` as a plain `<p role="alert" className="text-sm text-danger">` in `ExternalStoreChat_messages.tsx:210-213` — currently an empty fallback with no copy routing. | **New copy, existing channel.** CRED-05's clean refusal rides the same channel; no new error-rendering mechanism. See §Copywriting Contract. |
| Self-service "what's my own cap/spend" view for a non-admin | Does not exist. | **Out of scope this phase.** CONTEXT.md and the ROADMAP closing run describe the credit panel exclusively under the already admin-gated `identities` settings section ("B's spend appears in the cockpit **against B**" is read by admin A, not self-served by B). Nothing in RBAC/CRED requires a member-facing balance view. If this is wrong, it is a planning-time question, not a UI-spec assumption — flagged here rather than silently added or silently dropped. |
| Account-wide spend overview | Does not exist. | **New (this revision).** An `Overview` panel above the roster: 5 reconciliation-tier KPI tiles + a Top-Identities-by-spend ranked list + the over-allocation advisory (moved up from the per-identity Credit panel, since it is account-wide, not per-identity). See §Admin spend dashboard — Overview. `Trends`/`Explore`/`Guardrails` (OpenRouter's other three tabs) are explicitly out of scope this phase — see that section's Scope fence. This is additive: it does not replace or relocate the Cap/Reset-interval/Save-cap control in §Credit panel, which remains the phase's binding CRED-03/CRED-06 surface. |

---

## Design System

| Property | Value |
|----------|-------|
| Tool | shadcn |
| Preset | `new-york` style, `radix` base, `neutral` base color, CSS variables on, no prefix (`web/components.json:3-9`) — unchanged by this phase |
| Component library | Radix UI primitives (6 `@radix-ui/*` packages) wrapped by shadcn's `new-york` style |
| Icon library | lucide-react `^1.40.0` |
| Font | Display: `Fraunces` (variable, 300–900) · Sans/body: `"Atkinson Hyperlegible Next"` (variable, 200–800) · Mono: `"Commit Mono"` — all self-hosted `.woff2`/`.otf`, declared in `web/src/styles/fonts.css`, tokenized in `web/src/styles/theme.css:26-28`. Deliberately not Inter/Roboto/Arial/system fonts. Unchanged by this phase — no new font is introduced. |

---

## Component Inventory

Enumerated by `npx shadcn info` (run from `web/`) — 16 components — `shadcn@4.21.0` CLI against this project's `components.json` — 2026-09-08.

| Component | Import path | Notes |
|-----------|-------------|-------|
| alert | `@/components/ui/alert` | `default` / `destructive` only (no `warning` variant — see §Color for the warning-tone convention used instead). Used for grant/revoke errors today; reused for credit-mutation errors. |
| badge | `@/components/ui/badge` | Variants `default`, `secondary`, `info`, `warning`, `danger`, `success` — the 15%-tint-on-token pattern (`badge.tsx:8-10`). `info` is the new Admin-role tag (§Roster); `secondary` is Member. |
| button | `@/components/ui/button` | Variants `default` (accent, constructive), `secondary`, `outline`, `ghost`, `destructive`; sizes all ≥44px (`button.tsx:8-27`). |
| card | `@/components/ui/card` | `bg-surface`, `border-border`, `rounded-lg` — the roster row container. |
| checkbox | `@/components/ui/checkbox` | Still installed; its only Phase-1-era consumer (`CapabilityPicker.tsx`) is deleted by this phase. No new Phase 2 use. |
| command | `@/components/ui/command` | Not used this phase. |
| dialog | `@/components/ui/dialog` | Underlies `ConfirmDialog` (below); the removal dialog extends it. |
| empty | `@/components/ui/empty` | `Empty`/`EmptyHeader`/`EmptyMedia`/`EmptyTitle` — used for the CRED-09 local-backend exemption state (§Credit panel), matching `SharedLinksSection.tsx:136-143`. |
| input | `@/components/ui/input` | The cap amount field (`inputMode="decimal"`, matching the `inputMode="numeric"` convention at `SettingField.tsx:129`). |
| label | `@/components/ui/label` | Every form field. |
| native-select | `@/components/ui/native-select` | The reset-interval dropdown (`daily`/`weekly`/`monthly`) — same component as the existing roster-identity `<select>` in `CapabilityAdminPanel.tsx:69-80`, just the shadcn-wrapped version. |
| popover | `@/components/ui/popover` | Not used this phase. |
| resizable | `@/components/ui/resizable` | Not used this phase. |
| tabs | `@/components/ui/tabs` | Not used this phase. |
| textarea | `@/components/ui/textarea` | Not used this phase. |
| tooltip | `@/components/ui/tooltip` | Installed but has **zero existing usages in the app** (`grep` found none). Do not introduce it here either — the disabled self-removal button uses an inline muted caption instead (§Roster), matching the codebase's actual convention over the installed-but-unproven one. |

This table is non-exhaustive per the standard rule — anything else the `new-york` registry exports is fair game if a real need appears.

### Reusable project components (not shadcn primitives, still "inventory before invention")

| Component | Import path | Reused for |
|---|---|---|
| `AdminSection` | `web/src/admin/AdminSection.tsx` | Shared kicker/heading/body chrome + roster loading/error/empty guard. Both the retired capability panel and the new roster/credit panels mount inside it — do not rebuild this chrome. |
| `ConfirmDialog` | `web/src/components/ui/confirm-dialog.tsx` | The base for the removal dialog. Takes `children`, so the typed-confirmation input (§Removal) is passed as a child, not a new dialog component. |
| `Spinner` | `web/src/components/Spinner.tsx` | Loading rows, in-flight mutations (`aria-busy`), the roster loading guard. |
| `useAdminIdentities` / `useCapabilities` | `web/src/admin/useAdmin.ts` | The roster query and the caller's own admin/capability check. Extend the roster query's return shape (cap/remaining/spend/reset — a backend/data-layer concern, not this document's) rather than adding a second query. |
| `ChartDisplay.tsx` (pattern, not a shared component) | `web/src/chat/displays/ChartDisplay.tsx` | **Cited precedent, not reused code** — this file already establishes "zero-dependency bar chart, CSS-width bars, no charting library" with the comment "never recharts." §Admin spend dashboard — Overview's KPI sparklines and any bar treatment follow this same convention (inline SVG / CSS width, no new dependency), confirming `web/package.json` needs no new charting library this phase. |

**shadcn re-check for this revision:** `npx shadcn info` (run from `web/`, 2026-09-08) still reports the same 16 components and `shadcn@4.21.0` — no new install. The Overview dashboard's tiles/sparklines/ranked list are custom markup over existing primitives (`card`, `badge`), not a new shadcn component; the provenance line above is unchanged by this revision.

---

## Spacing Scale

Declared values (multiples of 4), unchanged from the rest of the cockpit:

| Token | Value | Usage |
|-------|-------|-------|
| xs | 4px | Icon-to-label gaps (e.g. badge icon + text) |
| sm | 8px | Compact element spacing (badge list `gap-2`, form field `gap-2`) |
| md | 16px | Default element spacing (`gap-4` between roster rows and panel sections) |
| lg | 24px | Section padding (`gap-6` inside `IdentityAccessPanel`) |
| xl | 32px | Layout gaps |
| 2xl | 48px | Major section breaks |
| 3xl | 64px | Page-level spacing |

Exceptions: the 44px (`min-h-[44px]`) interactive-target floor on every `Button`/`Input`/`NativeSelect` size (`button.tsx:22-26`) is not a spacing token, it is a touch-target floor, and it is not a multiple of 4 — it is pre-existing sitewide policy, not introduced here. No other exception.

---

## Typography

Observed, not invented — these are the exact classes already in `AdminSection.tsx`, `IdentityAccessPanel.tsx`, `CapabilityAdminPanel.tsx` and `ReviewStep.tsx`, carried forward for every new element in this phase.

| Role | Size | Weight | Line Height |
|------|------|--------|-------------|
| Kicker/eyebrow | 12px (`text-xs`) | 600 (semibold) | 1.4, `uppercase tracking-wide` |
| Body | 15.5px (`text-[15.5px]`) | 400 (regular) | `leading-relaxed` (1.625) |
| Label / field caption | 13px (`text-[13px]`) | 600 (semibold) | 1.4 |
| Section heading | 20px (`text-[20px]`) | 600 (semibold) | 1.2, sans (`font-sans`, the default) |
| Display (dialog/step heading) | 22px (`text-[22px]`) | 600 (semibold) | 1.2, `font-display` (Fraunces) |
| Mono / identifier value (email, role, USD amount, reset interval) | 13–15.5px, `font-mono` | 400 | 1.5 |

Two weights only: 400 and 600 (semibold) — no 500, no 700, matching every file read. Money and identifiers are `font-mono` (mirrors `ReviewStep.tsx`'s "identifier-shaped value" rule, now extended to the cap/spend figures — a dollar amount is exactly the kind of scannable, tabular value mono type exists for).

**Dimension 4 trade-off, recorded explicitly (checker FLAG, non-blocking):** the inherited scale carries 5 sizes (12 / 13 / 15.5 / 20 / 22px) where the dataviz-informed ideal is 4. Phase 2 does not add a 6th — the Overview KPI tiles (§Admin spend dashboard) reuse the existing 22px "Display" size step for the stat-tile's large value rather than inventing a new one, per "prefer reuse." They do NOT reuse the Display *family* (`font-display`/Fraunces): the dataviz skill is explicit that a hero/stat-tile number is never set in a display or serif face ("reads as off-brand decoration"), so the 22px step is assigned `font-mono` for the KPI value instead — consistent with this table's own "money is mono" rule two rows up, and with `ContextBudgetGauge.tsx`'s existing money-in-mono convention. The 5-size scale itself is accepted as-is for this phase, not consolidated; if a future phase adds another dashboard-shaped surface, collapsing to 4 sizes is the next phase's job, not this one's.

---

## Color

| Role | Value | Usage |
|------|-------|-------|
| Dominant (60%) | `--color-bg` / `--color-surface` | Page background, panel background (`bg-bg`, `AdminSection`'s wrapping page) |
| Secondary (30%) | `--color-surface-2` / `--color-surface-3` / `--color-border` | Roster row cards, form field backgrounds, dividers (`border-t border-border`) |
| Accent (10%) | `--color-accent` / `--color-accent-text` | See reserved list below — nothing else |
| Destructive | `--color-danger` | Remove-identity button, removal-dialog confirm button, credit-exhausted refusal text, credit gauge's critical (≥90%-of-cap) fill tier |

**Accent reserved for** (extending the app's existing numbered reservation — comments in `Composer.tsx`, `ApprovalBadge.tsx`, `BranchPicker.tsx`, `ContextBudgetGauge.tsx` cite items 1/4/6/7 of a list this document does not have full sight of; the two items below are Phase 2's additions to it, not a renumbering):

- The credit-cap "Save" button (constructive mutation — `Button` default variant), matching the existing rule that identity creation's CTA is constructive-accent, never danger-styled (`ReviewStep.tsx:78-89` comment).
- The credit gauge's fill bar in its normal tier (< 70% of cap spent) — the exact same three-tier rule `ContextBudgetGauge.tsx:42` already applies (`bg-accent` normal → `bg-warning` ≥70% → `bg-danger` ≥90%), reapplied to a USD cap instead of a token window. Do not invent new thresholds; reuse `CONTEXT_NEAR_FULL_PERCENT` (70) / `CONTEXT_CRITICAL_PERCENT` (90) semantics by name for the credit gauge's own constants.

**Not accent** — everything else new in this phase uses an existing non-accent token role instead, because accent is scarce:

- Admin role badge: `Badge variant="info"`.
- Member role badge: `Badge variant="secondary"`.
- Over-allocation notice (M-12: sum of caps may exceed the account's total pool): warning-tone banner, `border-warning/40 bg-warning/10 text-warning`, the same 15%-tint-on-token formula `badge.tsx` documents — not a new visual language, just applied to a banner instead of a chip.
- Top-up-latency advisory ("takes about 25s to apply"): `text-warning`, inline caption, not a blocking `Alert`.
- Zero-cap / exhausted-credit turn refusal: `text-danger` inline in the chat error slot (existing `role="alert"` paragraph), consistent with every other hard-stop error in the app.
- **KPI sparkline's current-period trace** (§Admin spend dashboard): `--color-info`, not accent. The dataviz skill's stat-tile contract calls for "12-point sparkline in the de-emphasis hue, current period in the accent," but this document's accent list is already closed at exactly two items (above) and a third claim would dilute the scarcity the "10%" split exists to protect. `info` is already in use for the Admin role badge and is not itself a status/severity color, so it is the correct non-accent slot for a decorative trend line. The trace's de-emphasis half (prior-period ghost, if shown) uses `--color-border-strong`, matching the muted axis/grid convention.
- **Top-Identities-by-spend row marker** (§Admin spend dashboard): the existing role `Badge` (`info`/`secondary`), reused — not a new colored dot. OpenRouter's screenshot gives each ranked row a colored series dot that cross-references a legend in a chart lower on the page; Aura's Overview ships no such chart this phase (see Scope fence), so a dot with no legend to match would be decoration with no referent. Reusing the role badge instead ties the ranked list back to the same identity data the roster already shows, which is the actual cross-reference this surface has.

---

## Copywriting Contract

| Element | Copy |
|---------|------|
| Primary CTA (this phase's new constructive action) | **"Save cap"** — the credit-panel submit button. (Identity creation's own CTA, "Create identity", already exists at `onboarding.cta.provision` and is unchanged.) |
| Empty state heading | **"No spending cap to show"** — CRED-09's local-backend exemption. Rendered via `Empty`/`EmptyHeader`/`EmptyMedia`/`EmptyTitle`, matching `SharedLinksSection.tsx:136-143`'s composition exactly. |
| Empty state body | **"This deployment runs on a local model backend, which doesn't bill — there's no cap or spend to show."** Never a `$0.00` — a zero balance for a provider that charges nothing is a lie CRED-09 explicitly forbids. (The pre-existing roster-empty state, `admin.identity.empty` = "No identities yet.", is unchanged and out of this phase's scope.) |
| Error state | **Zero/exhausted credit, pre-flight refusal (CRED-05):** *"{{name}} has no remaining credit for this turn. Ask an administrator to add credit under Settings → Identities."* Rendered in the existing chat error slot (`MessagePrimitive.Error`, `text-danger`, `role="alert"`) — this is copy for an existing empty render target, not a new component. **Credit-mutation failure (save cap / change reset interval):** *"Couldn't update the spending cap. Check the amount and try again."* — same `Alert variant="destructive"` pattern as `admin.access.grantError`/`admin.access.revokeError` today. |
| Overview KPI/list fetch error (`POST /api/v1/analytics/query` or `GET /api/v1/keys` failing) | *"Couldn't load the spend overview. Try refreshing."* — same `Alert variant="destructive"` pattern as the roster-fetch guard; the Overview is reconciliation data, so its own fetch failing must never block or hide the roster/Credit panel below it, which read from a different source (D-08). |
| Overview empty (account has no completions yet) | *"No spend yet — this account hasn't made a billed request."* Never a bar chart or KPI row of all-zero tiles rendered as if real; a genuinely-empty reconciliation window reads as absence, not as five `$0.00` tiles (same CRED-09 discipline the local-backend empty state already applies, extended to "no data yet" rather than "backend doesn't bill"). |
| Top-Identities-by-spend spend label | **"Lifetime spend"** for the Overview ranked-list value (`GET /api/v1/keys`'s `usage`, cumulative since the key was minted) — deliberately distinct wording from the per-identity Credit panel's **"Spend"** label (§Credit panel item 3, current-reset-period only, ledger-tier). The two numbers will legitimately disagree for any identity mid-reset-interval; the wording is the disambiguator, not a tooltip or footnote. |
| Destructive confirmation | **Remove identity:** dialog title *"Remove {{name}}?"*; body: *"This permanently deletes {{name}}'s conversations, memory, files and OpenRouter key. This can't be undone."* Confirm button disabled until the admin types the identity's exact email into a required field labeled *"Type {{email}} to confirm"* — this is a NEW, heavier pattern than the app's existing single-click destructive dialogs (`DeleteConfirmDialog.tsx`, `SkillDeleteDialog.tsx`, neither of which requires typed input), justified because this is the one destructive action in the app that deletes an entire user across every plane rather than one artifact. Confirm label: *"Remove permanently"* (`variant="destructive"`). Cancel label: *"Cancel"* (`variant="outline"`, matching `ConfirmDialog`'s existing default). While the saga runs: *"Removing {{name}}…"* (`aria-busy`, `Spinner`) — deactivate is immediate but purge tears down every plane (Postgres, ArcadeDB, Garage, sandbox, skills root, Authula, OpenRouter key), so this is not instantaneous and the label must not imply it is. |

---

## Review step (onboarding wizard) — replacing the removed capabilities phase

`ReviewStep.tsx`'s "Granted capabilities" `<dt>/<dd>` pair (currently a `Badge` list, `ReviewStep.tsx:49-64`) is replaced by two static, non-interactive rows using the same `dl`/`dt`/`dd` structure and the same typography:

| Row | Copy |
|---|---|
| Access | *"Full access to Aura's tools — mounting MCP servers, authoring skills, running sandboxed commands and approving actions. Only user management stays admin-only."* — states RBAC-03's uniform grant plainly, so the admin isn't left wondering what happened to the picker they remember. |
| Starting credit | *"$0.00 — this identity can't run a turn until you add credit after creating it."* — states CRED-02/D-09's zero-cap start explicitly, in the one place an admin will look for it before the identity exists to have a credit panel of its own. |

No interaction, no checkbox, no server round-trip — both rows are copy the wizard already knows from RBAC-03/CRED-02, not new data.

---

## Roster (`web/src/settings` — replaces `CapabilityAdminPanel.tsx`)

Layout: `role="list"` of `Card` rows (not a `<table>` — no table component is installed, and a handful of self-hosted identities does not need one), each mounted inside the existing `AdminSection` chrome (loading/error/empty guard reused verbatim).

Each row:

- Identity name/email — `font-mono text-[15.5px] break-all` (identifier-shaped, matches `ReviewStep.tsx`'s email treatment and `CapabilityAdminPanel.tsx`'s `break-all font-mono` convention).
- Role badge — `info` variant "Admin" or `secondary` variant "Member" (§Color). Read-only: no grant/revoke control exists any more (§Surface Map).
- The signed-in admin's own row additionally carries the existing `" (you)"` suffix pattern (`CapabilityAdminPanel.tsx:75`).
- Remove action — icon `Button` (`variant="destructive"`, `size="icon"`, lucide `Trash2`, `aria-label="Remove {{name}}"`), opening the confirmation dialog above. **Disabled on the sole administrative identity's own row** (D-03 — admin is bootstrap-only, so this is unconditional, not a count check), with an inline muted caption directly beneath it: *"The administrator account can't remove itself — use `aura identity` on the host."* (no `Tooltip`, per the inventory note above: it has no precedent in this app and an always-visible caption is more discoverable than a hover-only one for a control that is disabled, not merely deferred).

---

## Credit panel (admin-only, per identity)

Mounted as a new panel inside the same roster row (expandable) or as the detail pane for the currently-selected identity — planner's choice of exact composition, but it MUST show, per identity, in this order:

1. **Cap** — `font-mono`, e.g. `"$5.00"`, editable via an `Input` (`inputMode="decimal"`, `min="0"`, `step="0.01"`, `$`-prefixed) + a **Save cap** button (accent, per §Color).
2. **Reset interval** — `NativeSelect` with options `daily` / `weekly` / `monthly` (CRED-03/D-10 — no "never" option is invented; OpenRouter's `limit_reset: null` exists in the API but D-10 frames this as the admin choosing among the three intervals, so default to `monthly` rather than adding a fourth choice not asked for).
3. **Remaining** and **Spend** — both `font-mono`, both sourced from Aura's own in-band ledger per D-08 (`aura.cache_metrics.cost_usd` / `turn_usage.go`), **never** from `GET /api/v1/key`'s `usage` field, which the measured M-07 lag (30–40s) makes an actively misleading live-balance source. Rendered as the `ContextBudgetGauge` fill-bar shape: a `role="progressbar"` bar, `aria-valuenow` = percent of cap spent, three-tier fill color (§Color), plus the numeric readout `"{{spend}} / {{cap}} · {{percent}}%"` mirroring `footer.gaugeValue`'s exact format string.
4. **Top-up latency advisory** — shown only immediately after a cap increase is saved, for the duration a real one would take: *"Takes about 25 seconds to apply."* (raising, M-06) vs. *"Takes about 5 seconds to apply."* (lowering, M-05) — two distinct strings, not one generic "a moment", because the two measured latencies differ by 5x and the CONTEXT decision (D-09) explicitly requires the UI to say so.
5. **Local-backend exemption** (CRED-09) — when the deployment's LLM backend is not OpenRouter, items 1–4 are replaced entirely by the `Empty` composition in §Copywriting Contract, for every identity uniformly (this is a deployment-wide backend choice, not a per-identity one, per D-13/`internal/llm/spend.go`'s `ErrSpendNotApplicable`).

**Over-allocation notice** (M-12) has moved to the top of §Admin spend dashboard — Overview, because it is account-wide (a property of the whole cap allocation, not of any one identity's row) and this revision gives account-wide state a proper home above the roster instead of floating it inside a per-identity panel. Its copy and trigger condition are unchanged; see that section for both, plus its element-kind reclassification (Dimension coverage fix).

---

## Admin spend dashboard — Overview

New in this revision. Mounted above the Roster (§Roster, unchanged), inside the same
`identities` admin-gated settings section. **Additive, not a replacement:** the Cap /
Reset-interval / Save-cap / Remaining / Spend control in §Credit panel above is unchanged and
remains the phase's binding CRED-03/CRED-06 surface — nothing here edits a cap.

### Why this exists

The human's directive was a screenshot of OpenRouter's own account activity dashboard, with the
instruction "as picture." That is read as a directive on **shape** (an account-wide reconciliation
view: KPI tiles + ranked lists + charts, sitting above a per-entity detail), not as a mandate to
reproduce all eleven of its panels regardless of whether Aura has the data or the phase needs it.
Every panel below is decided against 02-OPENROUTER-API.md and 02-RESEARCH.md, not against the
screenshot's literal content.

### Data-source ledger — what ships, what's cut, what's deferred

Per D-08's three-way split (enforcement = OpenRouter's `limit`; balance/refusal = Aura's own
in-band ledger; **reconciliation = `GET /api/v1/keys` + `POST /api/v1/analytics/query`**), this
whole Overview surface is reconciliation-tier. It is periodically refreshed, never used for the
pre-flight refusal (CRED-05, which stays on the in-band ledger per §Credit panel item 3), and it
disagrees with the ledger figures by design during OpenRouter's measured 30–40s lag (M-07) — the
Overview is explicitly not the number CRED-05 checks.

| Screenshot panel | Ships this phase? | Source | Reasoning |
|---|---|---|---|
| Tab bar (`Overview`/`Trends`/`Explore`/`Guardrails`) | **Overview only** | — | See Scope fence below. |
| KPI row — Total spend | ✅ | `POST /api/v1/analytics/query`, metric `total_usage`, no dimension | Direct 1:1 metric match. |
| KPI row — Requests | ✅ | same query, metric `request_count` | Direct 1:1 metric match. |
| KPI row — Token volume | ✅ | same query, metric `tokens_total` | Direct 1:1 metric match. |
| KPI row — Cache hit rate | ✅ | same query, metric `cache_hit_rate` | Direct 1:1 metric match. |
| KPI row — Blended $/1M | ✅ | same query, metric `blended_cost_per_million_tokens` | Direct 1:1 metric match. |
| KPI sparklines (all 5) | ✅ | same query, `granularity: "day"`, no dimension, trailing window | One call returns a day-bucketed row per metric; no second endpoint needed. |
| KPI deltas ("vs prev period") | ✅ | a second `analytics/query` call over the prior window of equal length, diffed client-side | Doubles the call count per KPI row load; noted as an implementation cost, not a blocker. |
| Top API Keys (ranked list) | ✅, reframed as **Top Identities by spend** | `GET /api/v1/keys` — `data.external_user` (set to the Aura identity id at mint, per D-07/02-OPENROUTER-API.md `external.user`) joins the row to an identity; `data.label` (masked, e.g. `sk-or-v1-caa...61c`) is the safe-to-display key string; `data.usage` is lifetime spend | This IS the per-identity fit — see below. Sorted by `usage` desc, top 5. |
| Top Apps | ❌ cut | would be `POST /api/v1/analytics/query` dimension `app` | Degenerate for Aura: every identity's traffic attributes to one application, so this panel would always render exactly one row. Buildable, not valuable — cut rather than shipped as a single-row curiosity. |
| Usage by model ($ bar, full width) | ⏸ deferred | would be `POST /api/v1/analytics/query`, dimensions `["model"]`, metric `total_usage`, `granularity: "day"` | Buildable, but per-model breakdown serves an analytics job RBAC/CRED never asked for. Folded into the same out-of-scope decision as the `Trends`/`Explore` tabs — see Scope fence. |
| Usage type (BYOK / OpenRouter Spend area) | ❌ cut | would be metrics `byok_usage` / `openrouter_usage` | Not merely deferred — inapplicable. D-07/D-13 mint every identity's key directly; Aura's design has no BYOK path, so this chart would render ~0% / ~100% every time it is asked. |
| Request volume by model (stacked bar) | ⏸ deferred | same as "Usage by model," metric `request_count` | Same reasoning as "Usage by model." |
| Token breakdown (Reasoning/Completion/Prompt) | ⏸ deferred | metrics `reasoning_tokens` / `tokens_completion` / `tokens_prompt` | Same reasoning as "Usage by model." |
| Prompt token caching (Uncached/Cached) | ⏸ deferred | metrics `cached_tokens` vs `tokens_total − cached_tokens` | Same reasoning as "Usage by model." |
| Cap / Save cap / reset interval / Remaining / Spend / top-up advisory | ✅, **unchanged** | Aura's in-band ledger (D-08) | Already specified in §Credit panel; not part of this surface, not displaced by it. |

Deferred rows are recorded here so the next phase that wants a `Trends`/`Explore` surface starts
from this table rather than a search — mirroring how 02-OPENROUTER-API.md's own "Adjacent
surface, deliberately not used" section already does this for workspaces/guardrails.

### Scope fence

Phase 2's requirement is RBAC-01..11 / CRED-01..09 / REL-06 — "an admin adds and removes users
and nobody else can; what bounds a user is money, enforced by OpenRouter" (02-CONTEXT.md
`<domain>`). None of RBAC or CRED asks for model-level spend trends, BYOK/OpenRouter split, or a
guardrails surface. `Trends`, `Explore` and `Guardrails` (OpenRouter's other three tabs) are
**out of scope this phase** — and per CLAUDE.md's "no asilo nido" rule, that means they are not
rendered at all, not built as inert/disabled tabs waiting for a future phase to enable them. There
is no tab bar in Aura's Overview this phase: the page under the `identities` section heading goes
directly from the over-allocation banner into the KPI row, with no chrome implying three more
views exist. The four deferred/cut chart rows above are the literal content those tabs would hold,
so this one fence covers both.

### How per-identity fits

OpenRouter's dashboard is account-wide; Phase 2's requirement is per-identity ("B's spend appears
in the cockpit **against B**," 02-CONTEXT.md). The two compose as a drill-down, not two unrelated
views:

1. **Over-allocation banner** (moved here from §Credit panel) — account-wide, sits above the KPI
   row. Unchanged copy/trigger: *"Assigned caps total more than this account's available
   OpenRouter credit. A lower-priority identity could be starved without warning — lower a cap or
   add credit to the account."* Shown only when `Σ(cap)` across all identities exceeds
   `GET /api/v1/credits`'s `total_credits − total_usage` (M-12). Advisory, not blocking — OpenRouter
   does not prevent the over-allocation, so the UI must not pretend it does by disabling anything.
2. **KPI row** — account-wide, five stat tiles (below).
3. **Top Identities by spend** — a `role="list"` of the top 5 identities ranked by lifetime spend.
   Each row: role `Badge` (§Color, reused as the row marker) · identity name/email (`font-mono`,
   `break-all`, matching the Roster convention) · the masked key label beneath it in a second,
   muted `font-mono` line · right-aligned lifetime spend (`font-mono`). This ranked list **is** the
   per-identity fit: it is the same identities the Roster lists, re-sorted by spend instead of by
   provisioning order. It carries no controls (no remove, no cap edit) — it is read-only
   reconciliation. Below the list, a plain caption *"See full roster below"* replaces OpenRouter's
   `Explore ›` link: Aura is a single-page cockpit, so the destination is an anchor to §Roster on
   the same page, not a second tab (which Scope fence rules out this phase).
4. **Roster** (§Roster, unchanged) — every identity, role, remove action.
5. **Credit panel** (§Credit panel, unchanged) — per-identity, ledger-tier cap/remaining/spend
   control, mounted per roster row exactly as already specified.

### KPI row

Per the dataviz skill's stat-tile contract (`label` · `value` · `delta` · `trend`), rendered as 5
tiles in a row (wraps on narrow viewports, no new breakpoint token — reuses the app's existing
responsive wrap behavior):

- **Value:** the existing 22px "Display" size step, `font-mono` (not `font-display` — see the
  Dimension 4 note under §Typography), proportional-not-tabular per the dataviz figure contract,
  auto-compact (`$5.52`, `3K`, `52.1M`, `51.5%`, `$0.11`).
- **Label:** sentence case, no trailing colon, the existing 12px kicker size/weight (`text-xs
  font-semibold uppercase tracking-wide text-text-faint`, matching `AdminSection`'s kicker
  treatment) — e.g. "Total spend," "Requests," "Token volume," "Cache hit rate," "Blended $/1M."
- **Delta:** signed (`↑`/`↓` + percent), captioned "vs prev period." Color is **not** the naive
  "up = green, down = red" the screenshot appears to use — that would color a rising spend figure
  green as if more spend were an achievement. Instead: Cache hit rate and Blended $/1M carry
  direction-aware color (cache hit rate ↑ = `text-success`, ↓ = `text-warning`; blended $/1M ↑ =
  `text-warning`, ↓ = `text-success` — cost-per-token rising is the one worth flagging, at
  warning severity, not danger, which stays reserved for the exhausted-credit/critical-cap meaning
  in §Color). Total spend, Requests and Token volume are pure volume facts with no inherent
  good/bad direction for a reconciliation view — their delta renders in `text-text-muted`
  regardless of sign.
- **Trend:** a 12-point inline sparkline, de-emphasis line in `--color-border-strong`, current
  period in `--color-info` (§Color). Built as inline SVG (a `<polyline>`), the same zero-dependency
  approach `ChartDisplay.tsx` already established — no charting library.
- Per the dataviz skill, a single-series sparkline needs no legend box (the tile's own label names
  what's plotted).

### Prior art considered — LibreChat and assistant-ui

Read per the human's directive ("look librechat and ui assistent") against the local checkouts
before writing the above.

**LibreChat** (`danny-avila/LibreChat`, read at `D:/tmp/LibreChat`):

- `TokenCreditsItem.tsx` (`client/src/components/Nav/SettingsTabs/Balance/`) shows a user their
  own balance as a bare label + `tokenCredits.toFixed(2)` value, defaulting to `'0.00'` when
  undefined. **Rejected** for Aura's zero-cap state: CRED-09/§Copywriting Contract already forbids
  a `$0.00` fallback for the local-backend exemption because it reads as "you have nothing" rather
  than "billing doesn't apply here," and the same objection holds against LibreChat's undefined→
  `0.00` default in general. Aura already resolves this correctly; LibreChat does not.
- `Error.tsx`'s `token_balance` handler renders *"Insufficient funds. Balance: {{0}}. Prompt
  tokens: {{1}}. Cost: {{2}}."* — the exact numeric balance inline in the chat error.
  **Rejected.** Under D-08, the number Aura could put in that slot is either the in-band ledger
  (correct but then duplicated in two places that can drift by a request or two) or `GET
  /api/v1/key`'s `usage` (wrong — M-07's 30–40s lag makes a number in a refusal message actively
  misleading at the exact moment it matters most). CRED-05's *"{{name}} has no remaining credit
  for this turn. Ask an administrator to add credit under Settings → Identities"* (already
  specified, unchanged) states the actionable fact — who to ask and where — instead of a number
  that cannot be trusted at refusal time. This is a considered improvement over LibreChat's shape,
  not an oversight.
- No standalone admin user-management table exists in the LibreChat OSS `client/` tree read here
  (`grep` found `PeoplePickerAdminSettings.tsx` — a sharing-permissions picker, not a roster) — the
  full "Admin Panel" the web search surfaced is a separate hosted product outside this checkout.
  Nothing further to transfer or reject on that front; Aura's card-list Roster (§Roster, unchanged)
  stands on its own precedent.

**assistant-ui** (`@assistant-ui/react` ^0.15.18, already in `web/package.json` and
`components.json`'s `@assistant-ui` registry): `ExternalStoreChat_messages.tsx` already renders
CRED-05's refusal target through `MessagePrimitive.Error` (a bare `<p role="alert">` today, filled
with the copy in §Copywriting Contract — no new component), and the existing
`BudgetLimitNotice.tsx` (a plain `<p role="status">` styled with the warning-tint formula, mounted
under `MessagePrimitive.Parts`, not an assistant-ui primitive itself) is the established precedent
for "a status line under an answer" if a future phase needs a low-but-nonzero-credit in-chat
warning. Phase 2's CRED-05 requirement is only the hard zero-credit refusal, which already has its
render target — no new primitive, no parallel refusal mechanism, is introduced by this revision.

---

## UI Considerations

Produced by the compiled state-coverage probe (`ui-consideration-probe.cjs`, the UI
element/state adapter of `probe-core`) over the 8 surfaces described in this document,
run 2026-09-08 after checker approval. The relevance filter classified each surface's
element kinds and raised only the categories that apply to those kinds.

**58 applicable considerations: 38 resolved (explicit) · 9 resolved (backstop) · 11 dismissed with reason · 0 unresolved.**

This section REPLACES the hand-written table of the pre-revision spec — it is the probe's
report, not a prose restatement of it. Empty-state and error-state COPY lives in
§Copywriting Contract; the rows below reference it rather than restating it.

### Resolved — explicit

The planner lifts each of these into a plan's `must_haves.truths` as a plain string.

| Surface | Category | Truth |
|---|---|---|
| Roster | `empty` | Pre-existing `admin.identity.empty` = "No identities yet." guard in AdminSection.tsx:57, reused unchanged. |
| Roster | `loading` | Pre-existing AdminSection Spinner + role="status" guard, reused verbatim. |
| Roster | `error` | Pre-existing Alert variant="destructive" roster-fetch guard at AdminSection.tsx:53-55, reused. |
| Roster | `populated` | role="list" of Card rows: mono name/email, read-only Admin/Member role badge, ' (you)' suffix on the signed-in admin's own row, destructive icon Button with aria-label. |
| Roster | `partial` | Removal in flight (deactivate done, purge running) renders "Removing {{name}}..." with aria-busy and Spinner. |
| Roster | `overflow` | Long identity name/email uses `break-all font-mono`, matching the ReviewStep.tsx / CapabilityAdminPanel.tsx convention. No truncation, no ellipsis. |
| Roster | `zero-one-many` | The admin's own disabled-remove is unconditional per D-03 regardless of roster size; every other row behaves identically at 1 or 20 identities. |
| Roster | `long-text` | Same `break-all font-mono` treatment as overflow; the row wraps rather than clipping. |
| Removal confirmation dialog | `empty` | Initial state: the "Type {{email}} to confirm" input is empty and the "Remove permanently" confirm button is disabled until it matches. |
| Removal confirmation dialog | `loading` | "Removing {{name}}..." with aria-busy and Spinner for the duration of the reverse saga across every plane. |
| Removal confirmation dialog | `overflow` | The email in the field label and in the dialog body wraps with `break-all`, consistent with the roster row treatment. |
| Removal confirmation dialog | `long-text` | Dialog body is fixed translated copy; only {{name}} and {{email}} vary and both are handled by break-all. No truncation. |
| Credit panel (per identity) | `empty` | CRED-09 local-backend exemption replaces items 1-4 with the Empty/EmptyHeader/EmptyMedia/EmptyTitle composition - "No spending cap to show" - never a $0.00. |
| Credit panel (per identity) | `loading` | AdminSection Spinner + role="status" guard; aria-busy on the in-flight cap mutation. |
| Credit panel (per identity) | `error` | "Couldn't update the spending cap. Check the amount and try again." via the existing Alert variant="destructive" pattern. |
| Credit panel (per identity) | `populated` | Cap Input + Save cap button, reset-interval NativeSelect (daily/weekly/monthly, default monthly), and the role="progressbar" three-tier fill bar with the "{{spend}} / {{cap}} - {{percent}}%" readout sourced from Aura's in-band ledger per D-08. |
| Credit panel (per identity) | `long-text` | The two top-up latency advisories are fixed translated sentences ("Takes about 25 seconds to apply." / "Takes about 5 seconds to apply."); the numeric fields are mono and bounded by the input's min/step. |
| Onboarding review step | `populated` | Two dl/dt/dd rows: "Access" (full tool access, only user management stays admin-only) and "Starting credit" ($0.00, cannot run a turn until credit is added), reusing ReviewStep.tsx's existing typography. |
| Onboarding review step | `overflow` | Reuses ReviewStep.tsx's existing dl wrapping; the copy is fixed-length translated prose that wraps rather than clipping. |
| Over-allocation advisory banner | `populated` | One fixed translated sentence in warning tone above the KPI row: "Assigned caps total more than this account's available OpenRouter credit. A lower-priority identity could be starved without warning - lower a cap or add credit to the account." Presentational text, not role="alert"; advisory per M-12, disables nothing. |
| Over-allocation advisory banner | `overflow` | Wraps within its container. The wording never varies with data, so its longest form is known at author time; no truncation, no line-clamp. |
| Over-allocation advisory banner | `long-text` | Non-user-generated, non-interpolated copy - the same wrapping as populated. Nothing about its content varies with data. |
| Zero-credit turn refusal | `error` | This element IS the error state: role="alert", text-danger, "{{name}} has no remaining credit for this turn. Ask an administrator to add credit under Settings > Identities." Rendered into the existing empty error target, not a new component. |
| Zero-credit turn refusal | `overflow` | A single sentence in the existing chat error slot; it wraps exactly like every other message in that column. |
| Zero-credit turn refusal | `long-text` | Fixed translated copy with only {{name}} interpolated; wraps, no truncation. |
| Overview KPI row | `empty` | "No spend yet - this account hasn't made a billed request." Never five $0.00 tiles rendered as if real - the same CRED-09 discipline extended from "backend doesn't bill" to "no data yet." |
| Overview KPI row | `loading` | The same AdminSection Spinner + role="status" guard as the roster; not a new loading pattern. |
| Overview KPI row | `error` | "Couldn't load the spend overview. Try refreshing." via the Alert variant="destructive" pattern, and it must not block or hide the Roster/Credit panel below, which read from a different source per D-08. |
| Overview KPI row | `populated` | Five tiles - Total spend, Requests, Token volume, Cache hit rate, Blended $/1M - each with a 12px uppercase kicker label, a 22px font-mono compact value, a signed delta with "vs prev period", and a 12-point inline SVG polyline sparkline (border-strong de-emphasis, --color-info current period). No legend box, single series per tile. |
| Overview KPI row | `zero-one-many` | The row is always exactly five tiles - it is a fixed metric set, not a variable collection. It wraps on narrow viewports using the app's existing responsive wrap behavior, adding no new breakpoint token. |
| Overview KPI row | `long-text` | Values use auto-compact notation ($5.52, 3K, 52.1M, 51.5%, $0.11) so a large figure cannot overrun the tile; labels are fixed translated strings at the 12px kicker size. |
| Top Identities by spend | `empty` | Shares the Overview empty state - "No spend yet - this account hasn't made a billed request." - rather than rendering five zero-spend rows. |
| Top Identities by spend | `loading` | The same AdminSection Spinner + role="status" guard as the roster and the KPI row. |
| Top Identities by spend | `error` | Shares the Overview fetch-error copy ("Couldn't load the spend overview. Try refreshing."); its failure must not hide the Roster or Credit panel below it. |
| Top Identities by spend | `populated` | role="list" of up to five rows, ranked by lifetime spend: role Badge as the row marker, mono break-all name/email, the masked key label on a muted second line, right-aligned "Lifetime spend" in mono. Read-only - no remove, no cap edit - followed by a "See full roster below" caption anchoring to the Roster on the same page. |
| Top Identities by spend | `partial` | An identity present in GET /api/v1/keys but with no usage figure yet renders its row with a zero lifetime spend rather than being omitted - the list is a ranking of the roster's identities, so omitting one would misrepresent the roster. |
| Top Identities by spend | `overflow` | Name/email uses break-all font-mono like the roster; the masked key label is already truncated by OpenRouter's own sk-or-v1-7ff...972 form. |
| Top Identities by spend | `long-text` | Same break-all treatment as the roster; the spend figure is mono and compact, so a long name cannot push it off the row. |

### Resolved — backstop

Each lifts as a structured item — `{ statement: <the check>, verification: backstop }` with a
flat scalar `verification:` continuation key, never a nested object. At verify time a backstop
truth the verifier cannot confirm with explicit evidence abstains to `human_needed`
(`insufficient_spec`) rather than passing silently. These are the genuinely-unsettled points:
this document states the question, and refuses to invent the answer.

- statement: **Removal confirmation dialog · `error`** — What the dialog shows when the reverse saga fails PART WAY (some planes torn down, others not) is not settled by CONTEXT.md or RESEARCH.md. internal/agui/deprovision.go is journaled and idempotent, so a retry affordance is plausible, but the copy and the affordance must be decided and tested explicitly rather than assumed.
  verification: backstop
- statement: **Removal confirmation dialog · `partial`** — Typed-confirmation email match semantics - exact-string vs trimmed vs case-insensitive - is a genuine UX judgment call not settled by any upstream artifact. Held out rather than silently assumed either way.
  verification: backstop
- statement: **Credit panel (per identity) · `partial`** — A cap set on a brand-new identity with no ledger rows yet: whether the gauge renders 0% or an em-dash placeholder is not pinned down. Must be decided and tested, not assumed to be 0%.
  verification: backstop
- statement: **Credit panel (per identity) · `overflow`** — Cap typed with excessive precision (e.g. 5.123456): Input carries step="0.01", but whether the server rejects or rounds a higher-precision value is unverified against the live PATCH /api/v1/keys behavior.
  verification: backstop
- statement: **Onboarding review step · `long-text`** — Both sentences are long for a dl row and the app is translated; a longer locale could reflow the review step. No upstream artifact pins the i18n breadth, so the reflow must be checked rather than assumed.
  verification: backstop
- statement: **Over-allocation advisory banner · `zero-one-many`** — The boundary case where the sum of caps exactly equals total_credits minus total_usage: whether equality triggers the warning (> vs >=) is not pinned down by CONTEXT.md or RESEARCH.md and must be decided and tested explicitly.
  verification: backstop
- statement: **Overview KPI row · `partial`** — When the analytics query returns some metrics but not others (a partial window), whether a tile renders a placeholder or the whole row degrades is not specified. Must be decided rather than assumed.
  verification: backstop
- statement: **Overview KPI row · `overflow`** — Two distinct unresolved points: (a) rate-metric aggregation across day buckets - cache hit rate and blended $/1M are rates, and naively summing a rate across days is wrong, so the headline value's derivation (sum, re-derived ratio, or last bucket) must be picked and documented; (b) the delta when the prior period's value is zero, where a percent change is undefined - no delta, "new", or an em-dash must be chosen before the tile ships.
  verification: backstop
- statement: **Top Identities by spend · `zero-one-many`** — Row count is handled explicitly - fewer than five identities renders that many rows with no placeholder padding, and the sole-admin case renders exactly one row (itself). What is NOT settled is the sort tiebreak when two identities tie on lifetime spend: a stable, deterministic tiebreak must exist so the ranking does not shuffle on refresh, but which key (creation order, identity id, provisioning order) is a planner decision.
  verification: backstop

### Dismissed — with reason

Not lifted. The relevance filter raised these because the prose classifier over-included an
element kind (a block of fixed prose reading as a form, for instance). Each carries the reason
it does not apply — silence is not a resolution, so the reason is the audit trail.

| Surface | Category | Why it does not apply |
|---|---|---|
| Credit panel (per identity) | `zero-one-many` | The credit panel is per-identity singular by construction - it mounts against one roster row. There is no zero/one/many axis on a single identity's own cap; the multi-identity axis lives on the Top Identities list (E8) and the Roster (E1). |
| Onboarding review step | `empty` | Both rows are fixed prose the wizard already knows from RBAC-03 and CRED-02 before the identity exists. There is no data that can be absent. |
| Onboarding review step | `loading` | The review step performs no server round-trip - the spec states no interaction, no checkbox, no fetch. Nothing can be in flight. |
| Onboarding review step | `error` | Nothing is fetched, so there is no load or submit that can fail. Creation errors belong to the wizard's own provision step, not this one. |
| Onboarding review step | `partial` | Both rows are always rendered and their copy is static; a partially-populated render is not reachable. |
| Onboarding review step | `zero-one-many` | Exactly two rows, always. It is not a variable-length collection. |
| Over-allocation advisory banner | `empty` | Only the banner's visibility is conditional. When there is nothing to warn about it is not rendered at all - absence is the absence, not an empty state within it. |
| Over-allocation advisory banner | `loading` | The trigger is derived from data already loaded for the KPI row and the roster (sum of caps vs GET /api/v1/credits). The banner issues no request of its own, so it has no in-flight state. |
| Over-allocation advisory banner | `error` | It carries no control and performs no request, so it has no failure mode of its own. A failure to load the credits figure suppresses the banner and surfaces through the Overview fetch-error copy instead. |
| Over-allocation advisory banner | `partial` | Fixed prose with no fields - there is nothing that can be partially present. |
| Zero-credit turn refusal | `loading` | The refusal is rendered synchronously into the existing MessagePrimitive.Error slot after a pre-flight refusal that already resolved. It has no in-flight state of its own. |

---

## Visual Focal Point

Checker Dimension 2 (Visuals) flagged the prior version for not declaring a primary anchor per
surface. Declared explicitly, one line per surface this phase touches:

| Surface | Primary anchor | Secondary |
|---|---|---|
| Admin spend dashboard — Overview | The **Total spend** KPI tile (first, leftmost, the figure the over-allocation banner directly above it is about) | The remaining four KPI tiles, then the Top Identities by spend list, in that reading order |
| Top Identities by spend list | The **identity name/email** per row (the same field that anchors the Roster) | The role badge, the masked key label, then the spend figure |
| Roster | The **identity name/email** — `font-mono`, first in reading order in every row | Role badge and remove action, both secondary to "whose row is this" |
| Credit panel (per identity) | The **fill-bar gauge** (`Remaining`/`Spend` readout) — the one figure that answers "can this identity still run a turn" | Cap input + Save cap button, then reset interval, then the top-up advisory |
| Removal confirmation dialog | The **typed-confirmation input** — the one control gating an irreversible action | The destructive/cancel button pair beneath it |
| Review step (onboarding) | The **"Starting credit: $0.00"** row — the fact most likely to surprise an admin who remembers the old capability picker | The "Access" row above it |
| Zero-credit turn refusal | The **refusal sentence itself** (the only content in the slot) | — (no secondary; the slot renders nothing else) |

---

## Registry Safety

| Registry | Blocks Used | Safety Gate |
|----------|-------------|-------------|
| shadcn official (`new-york`, radix base) | alert, badge, button, card, dialog, empty, input, label, native-select (all already installed; no new install needed this phase) | not required |

No third-party registry is used by this phase. `components.json` lists `@assistant-ui` for an unrelated existing chat surface; Phase 2 does not touch it and mints no new registry entry.

---

## Checker Sign-Off

Verified by `gsd-ui-checker` on 2026-09-08, re-run after the revision that added
§Admin spend dashboard — Overview, §Visual Focal Point and the §Typography Dimension 4
trade-off note. The prior run's two non-blocking FLAGs (Dimension 2 — no declared focal
point; Dimension 4 — the inherited 5-size scale) are both resolved; the checker verified
the dashboard's data-source ledger against `02-OPENROUTER-API.md` field by field, confirmed
`web/package.json` gained no charting dependency, and spot-checked the `path:line` citations.

- [x] Dimension 1 Copywriting: PASS
- [x] Dimension 2 Visuals: PASS
- [x] Dimension 3 Color: PASS
- [x] Dimension 4 Typography: PASS
- [x] Dimension 5 Spacing: PASS
- [x] Dimension 6 Registry Safety: PASS
- [x] Dimension 7 Inventory Provenance: PASS

**Approval:** approved 2026-09-08 — 7/7 dimensions, no BLOCK, no open FLAG.

State coverage (§UI Considerations) was probed after approval, so the section reflects the
final contract: 58 applicable considerations, 0 unresolved, 9 held out as `backstop` for the
planner to decide and test rather than guessed here.
