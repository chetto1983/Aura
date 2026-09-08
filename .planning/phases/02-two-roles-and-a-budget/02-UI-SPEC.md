---
phase: "02"
slug: "two-roles-and-a-budget"
status: draft
shadcn_initialized: true
preset: "new-york / radix base / neutral base color / lucide icons (components.json, unchanged)"
created: "2026-09-08"
---

# Phase 02 — UI Design Contract

> Visual and interaction contract for Phase 2: Two Roles and a Budget. This is an EXISTING
> cockpit — every rule below extends `web/src/admin/`, `web/src/settings/` and
> `web/src/onboarding/` as they stand today, cited by path:line. Nothing here proposes a new
> visual language; Phase 2's only new aesthetic surface is a money gauge, and it reuses the
> fill-bar shape `ContextBudgetGauge.tsx` already established.

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

---

## Copywriting Contract

| Element | Copy |
|---------|------|
| Primary CTA (this phase's new constructive action) | **"Save cap"** — the credit-panel submit button. (Identity creation's own CTA, "Create identity", already exists at `onboarding.cta.provision` and is unchanged.) |
| Empty state heading | **"No spending cap to show"** — CRED-09's local-backend exemption. Rendered via `Empty`/`EmptyHeader`/`EmptyMedia`/`EmptyTitle`, matching `SharedLinksSection.tsx:136-143`'s composition exactly. |
| Empty state body | **"This deployment runs on a local model backend, which doesn't bill — there's no cap or spend to show."** Never a `$0.00` — a zero balance for a provider that charges nothing is a lie CRED-09 explicitly forbids. (The pre-existing roster-empty state, `admin.identity.empty` = "No identities yet.", is unchanged and out of this phase's scope.) |
| Error state | **Zero/exhausted credit, pre-flight refusal (CRED-05):** *"{{name}} has no remaining credit for this turn. Ask an administrator to add credit under Settings → Identities."* Rendered in the existing chat error slot (`MessagePrimitive.Error`, `text-danger`, `role="alert"`) — this is copy for an existing empty render target, not a new component. **Credit-mutation failure (save cap / change reset interval):** *"Couldn't update the spending cap. Check the amount and try again."* — same `Alert variant="destructive"` pattern as `admin.access.grantError`/`admin.access.revokeError` today. |
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

**Over-allocation notice** (M-12): a warning-tone banner (§Color) above the roster, shown only when `Σ(cap)` across all identities exceeds `GET /api/v1/credits`'s `total_credits − total_usage`: *"Assigned caps total more than this account's available OpenRouter credit. A lower-priority identity could be starved without warning — lower a cap or add credit to the account."* This is advisory, not blocking — OpenRouter itself does not prevent the over-allocation (M-12), so the UI must not pretend it does by disabling anything.

---

## UI Considerations

Applicable state considerations resolved: 11 covered, 3 backstop, 0 unresolved.

| Category | Element(s) | Status | Resolution / Reason |
|----------|------------|--------|---------------------|
| empty | Credit panel | ✅ covered | CRED-09 local-backend state renders the `Empty` composition (§Copywriting Contract), never a `$0.00` |
| empty | Roster | ✅ covered | Pre-existing `admin.identity.empty` guard in `AdminSection.tsx:57`, unchanged, reused |
| loading | Roster, credit panel | ✅ covered | Pre-existing `AdminSection` `Spinner` + `role="status"` guard reused verbatim for both |
| error | Roster fetch | ✅ covered | Pre-existing `Alert variant="destructive"` guard in `AdminSection.tsx:53-55`, reused |
| error | Credit-cap save failure | ✅ covered | New copy in §Copywriting Contract, existing `Alert variant="destructive"` pattern |
| error | Zero-credit turn refusal | ✅ covered | New copy, existing `MessagePrimitive.Error` render slot (§Surface Map) |
| populated | Roster with mixed admin/member rows | ✅ covered | Role badge distinguishes every row; §Roster |
| partial | Removal in flight (deactivate done, purge running) | ✅ covered | "Removing {{name}}…" `aria-busy` state, §Copywriting Contract — the saga is not instantaneous and the copy says so |
| overflow | Long identity name/email in a roster row | ✅ covered | `break-all font-mono`, matching the pre-existing `ReviewStep.tsx`/`CapabilityAdminPanel.tsx` convention |
| zero-one-many | Roster size (1 admin only / 1 admin + 1 member / many members) | ✅ covered | Admin's own row disabled-remove is unconditional per D-03 regardless of roster size; every other row behaves identically whether there are 1 or 20 |
| long-text | Removal dialog body listing what is destroyed | ✅ covered | Fixed, translated copy — not user-generated, so no truncation concern |
| overflow | Cap amount typed with excessive precision (e.g. `5.123456`) | 🧪 backstop | Input `step="0.01"`; whether the server rejects or rounds a higher-precision value is unverified against the live Provisioning API — planner must confirm against `PATCH /api/v1/keys` behavior, not assume rounding |
| zero-one-many | Over-allocation banner at exactly `Σ(cap) == total_credits` (boundary) | 🧪 backstop | Whether the boundary itself (equal, not greater) triggers the warning is a `>` vs `≥` decision the CONTEXT/RESEARCH docs don't pin down — held out for the planner/executor to decide and test explicitly, not guessed here |
| partial | Typed-confirmation email match: case sensitivity / trailing whitespace | 🧪 backstop | Whether the match is exact-string or trimmed/case-insensitive is a genuine UX judgment call not settled by any upstream artifact — flagged rather than silently assumed either way |

---

## Registry Safety

| Registry | Blocks Used | Safety Gate |
|----------|-------------|-------------|
| shadcn official (`new-york`, radix base) | alert, badge, button, card, dialog, empty, input, label, native-select (all already installed; no new install needed this phase) | not required |

No third-party registry is used by this phase. `components.json` lists `@assistant-ui` for an unrelated existing chat surface; Phase 2 does not touch it and mints no new registry entry.

---

## Checker Sign-Off

- [ ] Dimension 1 Copywriting: PASS
- [ ] Dimension 2 Visuals: PASS
- [ ] Dimension 3 Color: PASS
- [ ] Dimension 4 Typography: PASS
- [ ] Dimension 5 Spacing: PASS
- [ ] Dimension 6 Registry Safety: PASS
- [ ] Dimension 7 Inventory Provenance: PASS

**Approval:** pending
