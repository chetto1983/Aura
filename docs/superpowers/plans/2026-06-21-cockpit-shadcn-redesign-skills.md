# Cockpit shadcn Redesign + Skills/Governance Fixes — Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Re-skin the Aura operator cockpit on shadcn/ui + lucide-react (mapped onto the locked Aura tokens, so it stays on-brand), convert governance write panels to real modal dialogs, and fix the live governance bugs (stale read-only banner, hanging row actions, archived/staged skills showing a UUID instead of a name).

**Architecture:** Add shadcn/ui primitives (Radix-based — the codebase already ships `@radix-ui/react-hover-card`) and `lucide-react`. shadcn's CSS variables are **overridden to the existing Aura `@theme` tokens** in a new `src/styles/shadcn.css` so primitives inherit the blue/graphite palette in both light + dark — no generic "AI slop". Governance + Skills are converted FIRST as the exemplar; the same primitives roll out to other surfaces in follow-up plans. Write panels (skill install, MCP install, env edit, remove-confirm) become `Dialog` modals instead of detail-pane slots.

**Tech Stack:** React 19, Vite 8 (`@tailwindcss/vite`), Tailwind v4 (`@theme` generated from `tokens/tokens.json`), TanStack Query v5, react-i18next, Radix UI, shadcn/ui (new-york, CSS variables), lucide-react. Go 1.26 backend for the skills-name fix.

## Global Constraints

- **Execution order:** This plan runs AFTER the background WhatsApp-connect agent has committed (it edits `McpServerDetail.tsx`, `governanceApi.ts`, `resources.governance.ts`, adds `WhatsAppConnect.tsx`, `connect_api.go`). Rebase every task on that committed state; the re-skin of `McpServerDetail` MUST also re-skin the `WhatsAppConnect` section.
- **Stay on-brand:** every shadcn primitive reads its colour from the Aura tokens via `src/styles/shadcn.css` — NEVER hard-code hex; NEVER introduce shadcn's default slate/zinc palette. Accent scarcity stays binding (03-SPEC §4.3): accent fill only on primary CTAs, focus rings, active tab/selected row.
- **Typography gate (29-UI-SPEC):** exactly three sizes 13px · 15.5px · 20px, two weights 400/600; mono data values carry `tabular-nums`. Do not reintroduce 12px/18px.
- **Spacing:** 4px-multiple scale (`gap-1/2`, `p-1/2/3/4`, `gap-6`, `p-8`); 44px touch floor on every control.
- **i18n:** add EVERY new key to BOTH `governanceEn` AND `governanceIt` in `web/src/i18n/resources.governance.ts`. Output copy in English in the source; the IT bundle is the translation.
- **Quality gates:** `npm run typecheck && npm run lint && npm run test` green; vitest coverage ≥85% (the threshold fails the build otherwise); Stryker ≥70% on touched modules. Go: `go build/vet/test (+ -race)` via WSL (native `.exe` is AV-blocked: ALWAYS `wsl bash -lc "cd /mnt/d/Aura && export PATH=$HOME/.local/bin:$HOME/go/bin:$PATH && <cmd>"`).
- **No file > 600 LOC.** Master-direct commits, atomic, no `git push`. Co-Authored-By trailer:
  `Co-Authored-By: Claude Opus 4.8 (1M context) <noreply@anthropic.com>`
- **Visual gate (operator directive "use playwright, stop assuming"):** after each frontend phase, rebuild dist (`cd web && npm run build`) + `docker compose build aura && docker compose up -d aura`, then screenshot the live surface with the proven harness:
  `docker run --rm --network aura_default --add-host aura.localhost:<AURA_IP> -e NODE_PATH=/work/web/node_modules -v D:/Aura:/work -v D:/tmp:/out -w /work/web mcr.microsoft.com/playwright:v1.61.0-noble node /out/shots.cjs`
  (`shots.cjs` logs in via `.env` passphrase, drives `aura.localhost:9080` with `--host-resolver-rules=MAP aura.localhost <AURA_IP>`; `AURA_IP` from `docker inspect aura --format '{{range .NetworkSettings.Networks}}{{.IPAddress}}{{end}}'`). VIEW the PNGs; do not declare done on a green test alone.

---

## File Structure

**New (foundation, Phase 0):**
- `web/components.json` — shadcn config (new-york, CSS variables, `@/` alias).
- `web/src/lib/utils.ts` — the `cn()` helper (clsx + tailwind-merge).
- `web/src/styles/shadcn.css` — maps shadcn CSS vars → Aura `@theme` tokens (light + dark); imported by `index.css` after `theme.css`.
- `web/src/components/ui/dialog.tsx`, `button.tsx`, `card.tsx`, `tabs.tsx`, `badge.tsx`, `input.tsx`, `label.tsx`, `tooltip.tsx` — shadcn primitives.
- `web/src/components/ui/__tests__/dialog.test.tsx` — a11y/behaviour smoke for the modal primitive.

**Modified:**
- `web/vite.config.ts` — add `resolve.alias` `@` → `/src`.
- `web/tsconfig.app.json` (or `tsconfig.json`) — add `paths` `@/*` → `src/*`.
- `web/src/styles/index.css` — `@import './shadcn.css';` after `theme.css`.
- `web/src/governance/GovernanceWorkspace.tsx` — remove the read-only banner; tabs → shadcn `Tabs` + lucide tab icons.
- `web/src/governance/SkillsBoard.tsx` — inline row actions (kebab/icon `Button`, not a hanging block); tabs → `Tabs`; install trigger opens a `Dialog`.
- `web/src/governance/SkillInstallPanel.tsx` — render inside a `Dialog` (modal); shadcn `Input`/`Button`/`Badge`.
- `web/src/governance/McpBoard.tsx`, `McpServerDetail.tsx`, `SchedulerBoard.tsx`, `TaskRunHistory.tsx`, `SkillDetail.tsx` — shadcn `Card`/`Badge`/`Button` + lucide icons (on-token).
- `web/src/governance/McpInstallPanel.tsx`, `McpEnvEditForm.tsx`, `McpLifecycleCluster.tsx` — install/env-edit → `Dialog`; remove-confirm → shadcn `AlertDialog`/`Dialog`.
- `web/src/i18n/resources.governance.ts` — drop `readOnlyBanner`; add any new aria/label keys (en+it).
- Backend: `internal/skills/*.go` (StageSkill name population) + `internal/agui/governance_api.go` (`stageSkillRows`) — the archived/staged-name fix.

---

## Parallel execution map (after Phase 0)

Phase 0 (foundation) is **sequential** — everything depends on it. Then three agents run in parallel on disjoint file sets:
- **Agent SKILLS** → `GovernanceWorkspace.tsx`, `SkillsBoard.tsx`, `SkillDetail.tsx`, `SkillInstallPanel.tsx`, skills i18n keys, skills tests. (Tasks 3, 4, 6)
- **Agent MCP** → `McpBoard.tsx`, `McpServerDetail.tsx` (+ `WhatsAppConnect.tsx`), `McpInstallPanel.tsx`, `McpEnvEditForm.tsx`, `McpLifecycleCluster.tsx`, `SchedulerBoard.tsx`, `TaskRunHistory.tsx`, mcp tests. (Tasks 5, 7)
- **Agent BACKEND** → `internal/skills/*`, `internal/agui/governance_api.go` + Go tests. (Task 2)

`resources.governance.ts` is shared — Agent SKILLS owns it; Agent MCP appends its keys via a clearly-separated block, or the orchestrator merges i18n last. To avoid the clobber, **the orchestrator applies the read-only-banner i18n removal + any shared-key additions in Phase 0** so the two frontend agents never both edit `resources.governance.ts`.

---

## Task 1 (Phase 0): shadcn + lucide foundation, on Aura tokens

**Files:**
- Create: `web/components.json`, `web/src/lib/utils.ts`, `web/src/styles/shadcn.css`, `web/src/components/ui/{dialog,button,card,tabs,badge,input,label,tooltip}.tsx`, `web/src/components/ui/__tests__/dialog.test.tsx`
- Modify: `web/vite.config.ts`, `web/tsconfig.app.json`, `web/src/styles/index.css`, `web/package.json`
- Test: `web/src/components/ui/__tests__/dialog.test.tsx`

**Interfaces — Produces (consumed by all later tasks):**
- `cn(...inputs: ClassValue[]): string` from `@/lib/utils`.
- `Dialog, DialogTrigger, DialogContent, DialogHeader, DialogTitle, DialogDescription, DialogFooter, DialogClose` from `@/components/ui/dialog`.
- `Button` (variants `default | secondary | outline | ghost | destructive`, sizes `default | sm | icon`, all `min-h-[44px]` except `icon` which is `44×44`) from `@/components/ui/button`.
- `Card, CardHeader, CardTitle, CardContent, CardFooter`; `Tabs, TabsList, TabsTrigger, TabsContent`; `Badge` (variants `default | secondary | warning | danger | success`); `Input`; `Label`; `Tooltip*`.

- [ ] **Step 1: Add deps** (Linux lockfile — run in the Playwright/node container to avoid host churn, OR `cd web && npm install`):
  `npm install lucide-react class-variance-authority clsx tailwind-merge @radix-ui/react-dialog @radix-ui/react-tabs @radix-ui/react-tooltip @radix-ui/react-label tw-animate-css`
  Commit the `package.json` + `package-lock.json`.

- [ ] **Step 2: Path alias.** In `web/vite.config.ts` add to the config object: `resolve: { alias: { '@': new URL('./src', import.meta.url).pathname } }`. In `web/tsconfig.app.json` `compilerOptions` add `"baseUrl": ".", "paths": { "@/*": ["src/*"] }`. Run `npm run typecheck` → expect PASS.

- [ ] **Step 3: `cn` helper.** Create `web/src/lib/utils.ts`:
```ts
import { clsx, type ClassValue } from 'clsx';
import { twMerge } from 'tailwind-merge';

export function cn(...inputs: ClassValue[]): string {
  return twMerge(clsx(inputs));
}
```

- [ ] **Step 4: Token bridge.** Create `web/src/styles/shadcn.css` mapping shadcn vars to Aura tokens (so primitives inherit the palette). Use the existing `--color-*` tokens from `theme.css`:
```css
/* shadcn variable bridge — every shadcn primitive reads the Aura @theme tokens. */
@theme inline {
  --color-background: var(--color-bg);
  --color-foreground: var(--color-text);
  --color-card: var(--color-surface);
  --color-card-foreground: var(--color-text);
  --color-popover: var(--color-surface);
  --color-popover-foreground: var(--color-text);
  --color-primary: var(--color-accent);
  --color-primary-foreground: var(--color-on-accent);
  --color-secondary: var(--color-surface-2);
  --color-secondary-foreground: var(--color-text);
  --color-muted: var(--color-surface-2);
  --color-muted-foreground: var(--color-text-muted);
  --color-destructive: var(--color-danger);
  --color-destructive-foreground: var(--color-on-accent);
  --color-input: var(--color-border);
  --color-ring: var(--color-ring);
  --radius: var(--radius-md);
}
```
  Then in `web/src/styles/index.css` add `@import './shadcn.css';` immediately AFTER `@import './theme.css';`.

- [ ] **Step 5: Add primitives.** Generate the shadcn primitives (new-york, CSS-vars) into `web/src/components/ui/`: `dialog button card tabs badge input label tooltip`. Either `npx shadcn@latest add <name>` (then verify each references the bridged vars above — no slate/zinc) OR hand-author from the shadcn new-york source. ENFORCE: `Button` default/secondary/outline/ghost sizes carry `min-h-[44px]`; add a `size="icon"` = `h-11 w-11` (44px touch floor); `Badge` gains `warning/danger/success` variants using `bg-warning/15 text-warning` etc. Each file < 600 LOC.

- [ ] **Step 6: Dialog a11y test.** Create `web/src/components/ui/__tests__/dialog.test.tsx`:
```tsx
import { describe, expect, it } from 'vitest';
import { render, screen } from '@testing-library/react';
import { Dialog, DialogContent, DialogTitle, DialogTrigger } from '../dialog';

describe('Dialog', () => {
  it('renders a labelled modal dialog when open', () => {
    render(
      <Dialog open>
        <DialogContent>
          <DialogTitle>Install skill</DialogTitle>
        </DialogContent>
      </Dialog>,
    );
    const dlg = screen.getByRole('dialog');
    expect(dlg).toBeTruthy();
    expect(dlg.getAttribute('aria-labelledby')).toBeTruthy();
    expect(screen.getByText('Install skill')).toBeTruthy();
  });
});
```

- [ ] **Step 7: Validate + commit.** Run `npm run typecheck && npm run lint && npm run test`. Expected: PASS, coverage ≥85%. Commit: `feat(cockpit): shadcn/ui + lucide foundation mapped onto Aura tokens`.

---

## Task 2 (Agent BACKEND): staged/archived skills show a real name, not a UUID

**Files:**
- Modify: `internal/skills/*.go` (the StageSkills lister / staging record — grep `func .*StageSkills` and the `StageSkill` struct `Name` field), `internal/agui/governance_api.go:325-335` (`stageSkillRows`).
- Test: `internal/skills/<store>_test.go`, `internal/agui/governance_api_test.go`.

**Interfaces — Consumes:** `skills.StageSkill{ Name, Description, Type, ... }`. **Produces:** `StageSkill.Name` is the human skill name for ALL staged skills (pending + archived), never the staging id/UUID.

- [ ] **Step 1: Reproduce (ground truth, not assumption).** Bring the stack up; install a skill from a repo/skills.sh through the live cockpit (or call `POST /api/governance/skills/install` with a real source), let it stage to `pending`, then `GET /api/governance/skills?stage=pending`. Confirm the returned `name` is a UUID, not the skill name. Record the exact value.

- [ ] **Step 2: Write the failing Go test.** In the skills store test, stage a skill whose source yields name "demo-skill"; assert `StageSkills("pending")[0].Name == "demo-skill"` (currently fails — it is the UUID). Run via WSL; expect FAIL.

- [ ] **Step 3: Root-cause + fix.** Trace where the staging record sets `Name` (likely the install/stage writer keys the record by a generated id and leaves `Name` as that id, or reads the dir name). Populate `Name` from the parsed SKILL.md frontmatter `name` (the same source the active `skills.Skill.Name` uses). Keep the id as a separate field if needed for the restore key.

- [ ] **Step 4: Run tests** (WSL): `go test ./internal/skills/ ./internal/agui/`. Expected PASS.

- [ ] **Step 5: Commit.** `fix(skills): staged/archived skills carry their real name, not the staging UUID`.

---

## Task 3 (Agent SKILLS): remove the stale read-only banner + Governance tabs → shadcn

**Files:** Modify `web/src/governance/GovernanceWorkspace.tsx`, `web/src/i18n/resources.governance.ts` (drop `readOnlyBanner` en+it). Test: `web/src/governance/__tests__/GovernanceWorkspace.test.tsx`.

- [ ] **Step 1: Failing test.** Assert the workspace does NOT render the read-only copy: `expect(screen.queryByText(/Read-only — viewing only/)).toBeNull();`. Run → FAIL.
- [ ] **Step 2: Remove banner.** Delete the `<p role="note">…readOnlyBanner…</p>` block (`GovernanceWorkspace.tsx:59-65`). Delete the `readOnlyBanner` key from `governanceEn` and `governanceIt`.
- [ ] **Step 3: Tabs → shadcn.** Replace the hand-rolled MCP/Skills/Scheduler `role=tablist` with `Tabs/TabsList/TabsTrigger/TabsContent` from `@/components/ui/tabs`, each trigger prefixed with a lucide icon (`Server`, `Sparkles`, `CalendarClock`). Keep `min-h-[44px]`, accent on the active trigger via the bridged `--primary`.
- [ ] **Step 4: Run** `npm run test` for the workspace test → PASS; coverage holds.
- [ ] **Step 5: Commit.** `fix(cockpit): drop stale governance read-only banner; tabs on shadcn + lucide`.

---

## Task 4 (Agent SKILLS): SkillsBoard — inline row actions + shadcn re-skin

**Files:** Modify `web/src/governance/SkillsBoard.tsx`. Test: `web/src/governance/__tests__/SkillsBoard.test.tsx`.

**Interfaces — Consumes:** `Card`, `Button`, `Badge`, `Tabs` from `@/components/ui/*`; lucide `Archive`, `ArchiveRestore`, `Download`.

- [ ] **Step 1: Failing test.** Assert the Archive action is an in-row control, not a separate block: render an active skill and assert the row container holds both the name AND a button with an accessible name "Archive skill" (e.g. `within(row).getByRole('button', { name: /Archive skill/ })`). Add an assertion that the archived row shows the skill NAME (depends on Task 2 backend for live data; in the unit test mock the row `name: 'demo-skill'`). Run → FAIL/adjust.
- [ ] **Step 2: Re-skin rows.** Replace the master-list `<button>`-card + hanging action (`SkillsBoard.tsx:211-275`) with a `Card`-based row: the selectable row is the card; the Archive/Restore action is a right-aligned `Button variant="ghost" size="icon"` (lucide `Archive`/`ArchiveRestore`) INSIDE the row header, with an `aria-label` (`t('governance.skills.archive')`). The 4 sub-tabs → `Tabs`. The "Install skill" CTA → `Button` (lucide `Download`) that opens the install Dialog (Task 6). Type chip → `Badge variant="secondary"`; pending note → `Badge variant="warning"`.
- [ ] **Step 3: Keep the contract.** Pending rows still carry NO activate control; the collision inline error stays `role="alert"`. Content hash keeps `font-mono tabular-nums`.
- [ ] **Step 4: Run** `npm run test` → PASS. Visual gate: screenshot Skills Active + Archived; VIEW that actions are inline and archived shows the name.
- [ ] **Step 5: Commit.** `fix(cockpit): inline skills row actions; SkillsBoard on shadcn Card/Tabs/Badge`.

---

## Task 5 (Agent MCP): MCP boards + detail re-skin (incl. WhatsAppConnect)

**Files:** Modify `web/src/governance/McpBoard.tsx`, `McpServerDetail.tsx`, `WhatsAppConnect.tsx`, `SchedulerBoard.tsx`, `TaskRunHistory.tsx`, `SkillDetail.tsx`. Test: the matching `__tests__`.

- [ ] **Step 1: Re-skin lists + detail** to `Card`/`Badge`/`Button` from `@/components/ui/*` with lucide icons (status dots → keep dot+label, optionally a lucide glyph; trust/runtime chips → `Badge`). Server-name header stays 20px `font-display`. The `WhatsAppConnect` "Link device" section uses `Card` + the QR `<img>` + a `Button` (lucide `Smartphone`/`Unplug`). Probe detail keeps `font-mono tabular-nums`.
- [ ] **Step 2: Preserve a11y + contracts** — `aria-pressed`/`aria-selected`, 44px targets, the redacted env-KEY chips (value never in DOM), the fail-soft probe note.
- [ ] **Step 3: Run** `npm run test` → PASS. Visual gate: screenshot MCP list + a server detail + WhatsApp connect.
- [ ] **Step 4: Commit.** `refactor(cockpit): MCP boards + detail on shadcn primitives + lucide`.

---

## Task 6 (Agent SKILLS): SkillInstallPanel → Dialog modal

**Files:** Modify `web/src/governance/SkillInstallPanel.tsx`, `SkillsBoard.tsx` (open via `Dialog`). Test: `web/src/governance/__tests__/SkillInstallPanel.test.tsx`.

- [ ] **Step 1: Failing test.** Assert the install surface is a modal: when opened, `screen.getByRole('dialog')` exists and is labelled "Install skill". Run → FAIL.
- [ ] **Step 2: Wrap in Dialog.** Render `SkillInstallPanel` inside `DialogContent` (open state owned by `SkillsBoard`'s `installing`), with `DialogTitle`=Install skill, `DialogClose` = the ✕. The RISKY band → `Badge variant="warning"` + body; Source field → `Input`; External-discovery toggle → `Button variant={on?'secondary':'outline'}` (lucide `Globe`); submit "Stage for approval" → `Button` with the existing `Spinner` on `isPending`; "Discard install" → `Button variant="outline"`. Keep the 5-item checklist + container note + the `governance.error` failure copy + the in-flight `aria-busy`.
- [ ] **Step 3: Run** `npm run test` (the existing SkillInstallPanel tests must still pass — update selectors to the dialog where needed). PASS; coverage ≥85%.
- [ ] **Step 4: Commit.** `feat(cockpit): skill install as a modal dialog (shadcn)`.

---

## Task 7 (Agent MCP): MCP install / env-edit / remove → Dialog modals

**Files:** Modify `web/src/governance/McpInstallPanel.tsx`, `McpEnvEditForm.tsx`, `McpLifecycleCluster.tsx`, `McpBoard.tsx`/`McpServerDetail.tsx` (open via Dialog). Test: the matching `__tests__`.

- [ ] **Step 1: Failing tests.** For each of install + env-edit, assert the opened surface is `role="dialog"` and titled. For remove, assert an `AlertDialog`-style confirm with action-specific "Remove server" / "Keep server" buttons (the safe action default-focused). Run → FAIL.
- [ ] **Step 2: Wrap install + env-edit in `Dialog`**; convert the existing remove-confirm to a shadcn `AlertDialog` (or `Dialog` with focus-trapped safe default). Preserve every contract: redacted env (no value in DOM), the soft-warning card, the `Spinner`+`aria-busy` on submit, `governance.error` failure copy.
- [ ] **Step 3: Run** `npm run test` → PASS. Visual gate: screenshot install modal, env-edit modal, remove dialog.
- [ ] **Step 4: Commit.** `feat(cockpit): MCP install/env-edit/remove as modal dialogs (shadcn)`.

---

## Task 8 (Orchestrator): integration, live E2E, full visual sweep

**Files:** none new (verification). 

- [ ] **Step 1: Merge + build.** Ensure all parallel branches are on master; `cd web && npm run typecheck && npm run lint && npm run test` (coverage ≥85%); Go `go build/vet/test` (WSL).
- [ ] **Step 2: Rebuild container.** `cd web && npm run build` (writes `internal/webui/dist`) → confirm freshness gate (`git diff --exit-code internal/webui/dist`) → `docker compose build aura && docker compose up -d aura` (wait healthy).
- [ ] **Step 3: Live E2E.** Run `web/e2e/governance-write.spec.ts` via the Playwright container against the live cockpit (`AURA_E2E_ORIGIN` external mode + `--host-resolver-rules` for the secure cookie). Expect green (axe 0 serious/critical).
- [ ] **Step 4: Full visual sweep.** Screenshot every governance surface (MCP list/detail/install-modal/env-modal/remove, Skills active/pending/archived/audit/install-modal, Scheduler/runs, WhatsApp connect). VIEW each PNG; confirm: no read-only banner, inline row actions, modals, archived shows names, lucide icons, on-brand palette in BOTH light + dark.
- [ ] **Step 5: Commit** any final polish; update `docs/research/2026-06-21-cockpit-connect-integration-contracts.md` status if needed.

---

## Self-Review

**Spec coverage:** terrible design → Tasks 1,3,4,5,6,7 (shadcn + lucide + modals); missing modals → Tasks 6,7; archived UUID → Task 2; skills.sh install path → exercised in Task 2 Step 1; read-only banner (found via Playwright) → Task 3; hanging row actions (found via Playwright) → Task 4; "use playwright" → the visual gate in Global Constraints + Tasks 4/5/7/8. ✓

**Placeholder scan:** foundation, token bridge, cn, dialog test are full code; re-skin tasks specify exact files + exact shadcn primitives + the concrete conversion (the shadcn primitives themselves are canonical, generated in Task 1). Backend fix is repro-driven (Task 2 Step 1 pins the exact value before the fix) — acceptable because the precise line is unknown until reproduced.

**Type consistency:** `cn`, `Dialog*`, `Button`, `Card*`, `Tabs*`, `Badge` names are defined in Task 1 Interfaces and reused verbatim in Tasks 3-7. ✓
