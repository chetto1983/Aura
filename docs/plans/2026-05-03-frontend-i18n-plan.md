# Frontend i18n Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Add multilanguage support to the React web dashboard with Italian as the first additional language and browser auto-detection.

**Architecture:** react-i18next with i18next-browser-languagedetector for locale detection, JSON translation files namespaced by feature, and a `useLocale()` hook wrapping translation + Intl formatting. All formatting uses the zero-dependency browser `Intl` API.

**Tech Stack:** React 19, TypeScript 6, Vite 8, react-i18next, i18next-browser-languagedetector

---

### Task 1: Install dependencies and scaffold i18n infrastructure

**Files:**
- Modify: `web/package.json`
- Create: `web/src/i18n/index.ts`
- Create: `web/src/i18n/locales/en.json`
- Create: `web/src/i18n/locales/it.json`
- Create: `web/src/i18n/types.ts`
- Modify: `web/src/main.tsx`

**Step 1: Install react-i18next and i18next-browser-languagedetector**

```bash
cd web && npm install react-i18next i18next i18next-browser-languagedetector
```

**Step 2: Create `web/src/i18n/index.ts` — i18next initialization**

```typescript
import i18next from 'i18next';
import { initReactI18next } from 'react-i18next';
import LanguageDetector from 'i18next-browser-languagedetector';
import en from './locales/en.json';
import it from './locales/it.json';

export const SUPPORTED_LANGS = ['en', 'it'] as const;
export type SupportedLang = (typeof SUPPORTED_LANGS)[number];

i18next
  .use(LanguageDetector)
  .use(initReactI18next)
  .init({
    resources: { en: { translation: en }, it: { translation: it } },
    fallbackLng: 'en',
    interpolation: { escapeValue: false },
    detection: {
      order: ['navigator'],
      caches: [],
    },
  });

export default i18next;
```

**Step 3: Create `web/src/i18n/locales/en.json` with initial scaffold**

```json
{
  "common.loading": "Loading…",
  "common.close": "Close",
  "common.tryAgain": "Try again",
  "common.error": "Something went wrong",
  "common.checkConsole": "Check the console for the full stack."
}
```

**Step 4: Create `web/src/i18n/locales/it.json` — empty mirror (fill later)**

```json
{}
```

Italian keys are added in Task 8 after all English keys are finalized.

**Step 5: Create `web/src/i18n/types.ts`**

```typescript
import type en from './locales/en.json';

type EnKeys = keyof typeof en;
export type TranslationKey = EnKeys;
```

**Step 6: Import i18n in `web/src/main.tsx`**

Add at the top of `main.tsx`, before any component imports:

```typescript
import './i18n'; // must be imported before App so i18next is initialized
```

The import order in `main.tsx` should become:

```typescript
import './i18n';
import { StrictMode } from 'react';
import { createRoot } from 'react-dom/client';
// ... rest unchanged
```

**Step 7: Verify the app still builds**

```bash
cd web && npm run build
```

Expected: Build succeeds with no errors (the app still uses hardcoded strings at this point, no component has been touched).

**Step 8: Commit**

```bash
git add web/package.json web/package-lock.json web/src/i18n/ web/src/main.tsx
git commit -m "feat: scaffold i18n infrastructure with react-i18next"
```

---

### Task 2: Create useLocale hook

**Files:**
- Create: `web/src/hooks/useLocale.ts`

**Step 1: Write `web/src/hooks/useLocale.ts`**

```typescript
import { useTranslation } from 'react-i18next';
import type { TranslationKey } from '@/i18n/types';
import type { SupportedLang } from '@/i18n';

export function useLocale() {
  const { t, i18n } = useTranslation();

  const locale = i18n.language as SupportedLang;

  const formatDate = (
    date: Date | number | string,
    options?: Intl.DateTimeFormatOptions,
  ): string => {
    const d = typeof date === 'string' ? new Date(date) : date;
    return new Intl.DateTimeFormat(locale, options).format(d);
  };

  const formatRelative = (
    value: number,
    unit: Intl.RelativeTimeFormatUnit,
  ): string => {
    return new Intl.RelativeTimeFormat(locale, { numeric: 'auto' }).format(
      value,
      unit,
    );
  };

  const formatNumber = (
    value: number,
    options?: Intl.NumberFormatOptions,
  ): string => {
    return new Intl.NumberFormat(locale, options).format(value);
  };

  return { t, locale, formatDate, formatRelative, formatNumber };
}
```

**Step 2: Verify the build still passes**

```bash
cd web && npx tsc --noEmit
```

**Step 3: Commit**

```bash
git add web/src/hooks/useLocale.ts
git commit -m "feat: add useLocale hook wrapping react-i18next + Intl"
```

---

### Task 3: Migrate Shell layer (Login, Shell, Sidebar, ErrorBoundary)

**Files:**
- Modify: `web/src/components/Login.tsx`
- Modify: `web/src/components/Shell.tsx`
- Modify: `web/src/components/Sidebar.tsx`
- Modify: `web/src/components/ErrorBoundary.tsx`
- Modify: `web/src/i18n/locales/en.json`

**Step 1: Add shell-layer keys to `web/src/i18n/locales/en.json`**

Add these keys to the existing `en.json`:

```json
{
  "common.loading": "Loading…",
  "common.close": "Close",
  "common.tryAgain": "Try again",
  "common.error": "Something went wrong",
  "common.checkConsole": "Check the console for the full stack.",

  "sidebar.brand": "Aura",
  "sidebar.tagline": "Second brain",
  "sidebar.home": "Home",
  "sidebar.wiki": "Wiki",
  "sidebar.graph": "Graph",
  "sidebar.sources": "Sources",
  "sidebar.tasks": "Tasks",
  "sidebar.swarm": "Swarm",
  "sidebar.skills": "Skills",
  "sidebar.mcp": "MCP",
  "sidebar.pending": "Pending",
  "sidebar.conversations": "Conversations",
  "sidebar.summaries": "Summaries",
  "sidebar.maintenance": "Maintenance",
  "sidebar.settings": "Settings",
  "sidebar.lightTheme": "Light theme",
  "sidebar.darkTheme": "Dark theme",
  "sidebar.highContrast": "High contrast",
  "sidebar.cycleTheme": "Cycle light → dark → contrast",
  "sidebar.signOut": "Sign out",
  "sidebar.signingOut": "Signing out…",
  "sidebar.revokeToken": "Revoke this token and return to login",
  "sidebar.signedOut": "Signed out.",

  "shell.navigation": "Navigation",
  "shell.openNav": "Open navigation",
  "shell.keyboardShortcuts": "Keyboard shortcuts",
  "shell.showHelp": "Show this help",

  "login.title": "Aura dashboard",
  "login.instructions": "Paste your dashboard token to sign in. In Telegram, send",
  "login.startHint": "/start for first setup or /login for a fresh token.",
  "login.expired": "Your session expired or was revoked. Paste a fresh token below.",
  "login.tokenLabel": "Token",
  "login.tokenPlaceholder": "paste here",
  "login.signIn": "Sign in",
  "login.verifying": "Verifying…",
  "login.emptyToken": "Paste the token from Telegram first.",
  "login.rejected": "That token was rejected. Ask the bot for a fresh one.",
  "login.tip": "Tip: tokens are delivered only through your private Telegram chat.",
  "login.findingBot": "Finding your Telegram bot...",
  "login.botUnavailable": "Start Aura, then refresh this page to show the Telegram QR and link.",
  "login.telegramBot": "Telegram bot",
  "login.openTelegram": "Open Telegram",
  "login.qrAlt": "QR code for Telegram bot @{username}",
  "login.openBotAlt": "Open Telegram bot @{username}",
  "login.sendStart": "Send /start once, then paste the token Aura sends back here.",

  "error.somethingWentWrong": "Something went wrong",
  "error.toastDescription": "Check the console for the full stack."
}
```

**Step 2: Migrate `Login.tsx`**

Import `useLocale`:

```typescript
import { useLocale } from '@/hooks/useLocale';
```

Inside the `Login` component, add at the top:

```typescript
const { t, locale } = useLocale();
```

Replace all hardcoded strings with `t()` calls. Here's the mapping:

| Line | Old | New |
|------|-----|-----|
| 146 | `"Aura dashboard"` | `t('login.title')` |
| 148-149 | The instructions paragraph | `<>{t('login.instructions')}<br />{t('login.startHint')}</>` |
| 155 | `"Your session expired..."` | `t('login.expired')` |
| 163 | `"Token"` | `t('login.tokenLabel')` |
| 170 | `"paste here"` | `t('login.tokenPlaceholder')` |
| 184 | `"Verifying…"` / `"Sign in"` | `submitting ? t('login.verifying') : t('login.signIn')` |
| 104 | `"Paste the token from..."` | `t('login.emptyToken')` |
| 119 | `"That token was rejected..."` | `t('login.rejected')` |
| 189 | `"Tip: tokens are..."` | `t('login.tip')` |
| 207 | `"Finding your Telegram bot..."` | `t('login.findingBot')` |
| 208 | `"Start Aura, then refresh..."` | `t('login.botUnavailable')` |
| 232 | `"Telegram bot"` | `t('login.telegramBot')` |
| 248 | `"Open Telegram"` | `t('login.openTelegram')` |
| 220 | `"Open Telegram bot @..."` | `t('login.openBotAlt', { username: telegram.username })` |
| 224 | `"QR code for Telegram..."` | `t('login.qrAlt', { username: telegram.username })` |
| 252 | `"Send /start once..."` | `t('login.sendStart')` |

**Step 3: Migrate `ErrorBoundary.tsx`**

Import `useLocale` via a wrapper — since `ErrorBoundary` is a class component, wrap it with a functional component that passes `t`:

```typescript
import { useLocale } from '@/hooks/useLocale';
```

Replace the class component approach: convert `ErrorBoundary` to use `useLocale` via a thin function wrapper:

```typescript
export function ErrorBoundary({ children }: { children: React.ReactNode }) {
  const { t } = useLocale();
  return <ErrorBoundaryInner t={t}>{children}</ErrorBoundaryInner>;
}
```

The inner class component receives `t` as a prop. Replace hardcoded strings:

| Line | Old | New |
|------|-----|-----|
| 19 | `error.message \|\| 'Something went wrong'` | `error.message \|\| t('common.error')` |
| 20 | `'Check the console...'` | `t('common.checkConsole')` |
| 30 | `"Something went wrong"` | `t('error.somethingWentWrong')` |
| 37 | `"Try again"` | `t('common.tryAgain')` |

**Step 4: Migrate `Sidebar.tsx`**

Add import:

```typescript
import { useLocale } from '@/hooks/useLocale';
```

In the `Sidebar` component, add:

```typescript
const { t } = useLocale();
```

Replace the `ITEMS` array to use translated labels:

```typescript
const items = [
  { to: '/', key: 'sidebar.home', icon: LayoutDashboard },
  { to: '/wiki', key: 'sidebar.wiki', icon: BookText },
  // ... etc. Use key for t() lookup instead of hardcoded label
];
```

And in the JSX: `{t(item.key)}` instead of `{label}`.

Replace theme labels and other strings:

| Line | Old | New |
|------|-----|-----|
| 31 | `"Light theme"` | `t('sidebar.lightTheme')` |
| 32 | `"Dark theme"` | `t('sidebar.darkTheme')` |
| 33 | `"High contrast"` | `t('sidebar.highContrast')` |
| 133 | `"Cycle light → dark → contrast"` | `t('sidebar.cycleTheme')` |
| 94 | `"Signed out."` | `t('sidebar.signedOut')` |
| 143 | `"Revoke this token..."` | `t('sidebar.revokeToken')` |
| 146 | `"Signing out…"` / `"Sign out"` | `loggingOut ? t('sidebar.signingOut') : t('sidebar.signOut')` |
| 105 | `"Second brain"` | `t('sidebar.tagline')` |

**Step 5: Migrate `Shell.tsx`**

Add import:

```typescript
import { useLocale } from '@/hooks/useLocale';
```

In the `Shell` component, add:

```typescript
const { t } = useLocale();
```

Replace:

| Line | Old | New |
|------|-----|-----|
| 27 | `"Navigation"` | `t('shell.navigation')` |
| 39 | `"Open navigation"` | `t('shell.openNav')` |
| 142 | `"Keyboard shortcuts"` | `t('shell.keyboardShortcuts')` |
| 146 | `"Show this help"` | `t('shell.showHelp')` |
| 149-157 | Shortcut desc strings | `t('sidebar.home')`, `t('sidebar.wiki')`, etc. — reuse sidebar keys |
| 166 | `"Close"` | `t('common.close')` |

**Step 6: Verify build**

```bash
cd web && npm run build
```

Expected: Build succeeds with no type errors.

**Step 7: Run the dev server and visually verify the login page and shell render correctly in English**

```bash
cd web && npm run dev
```

Expected: Login page, sidebar, and shell all show English text exactly as before.

**Step 8: Commit**

```bash
git add web/src/components/Login.tsx web/src/components/Shell.tsx web/src/components/Sidebar.tsx web/src/components/ErrorBoundary.tsx web/src/i18n/locales/en.json
git commit -m "feat: migrate shell-layer components to react-i18next"
```

---

### Task 4: Migrate core panels (TasksPanel, SettingsPanel, WikiPanel, SourceInbox)

**Files:**
- Modify: `web/src/components/TasksPanel.tsx`
- Modify: `web/src/components/SettingsPanel.tsx`
- Modify: `web/src/components/WikiPanel.tsx`
- Modify: `web/src/components/SourceInbox.tsx`
- Modify: `web/src/i18n/locales/en.json`

**This task follows the same pattern for every panel:**

1. Add the panel's translation keys to `en.json` (prefixed by feature, e.g. `tasks.*`, `settings.*`, `wiki.*`, `sources.*`)
2. Add `const { t, formatDate, formatRelative, formatNumber } = useLocale();` at the top of each component
3. Replace every hardcoded English string with `t('prefix.key')`
4. Replace raw date/number formatting with `formatDate()`, `formatRelative()`, `formatNumber()`
5. Run `npm run build` to catch type errors

**Step 1: Migrate TasksPanel**

Add tasks keys to `en.json`. Key strings to extract include (examine the file for exact text):

- Form labels: "Name", "Command", "Schedule", etc.
- Button text: "Create", "Save", "Cancel", "Delete", "Run now"
- Status labels: "Active", "Paused", "Next run", "Last run"
- Table headers and empty states
- Confirmation dialog text
- Schedule descriptions ("every 5 minutes", "daily at 9am", etc.)
- Toast messages ("Task created", "Task deleted", etc.)

After importing `useLocale`, replace each string. Example transformations:

```tsx
// Before
<label>Name</label>
// After
<label>{t('tasks.nameLabel')}</label>

// Before
toast.success('Task created');
// After
toast.success(t('tasks.created'));

// Before
`Next run: ${new Date(r).toLocaleString()}`
// After
`${t('tasks.nextRun')}: ${formatDate(r, { dateStyle: 'long', timeStyle: 'short' })}`
```

**Step 2: Migrate SettingsPanel**

Add `settings.*` keys to `en.json`. Extract group labels ("General", "Telegram", etc.), field hints, button text, toast messages.

**Step 3: Migrate WikiPanel**

Add `wiki.*` keys — table headers, empty states, filter UI labels.

**Step 4: Migrate SourceInbox**

Add `sources.*` keys — drop zone prompts, button labels, status labels ("Queued", "Processing", "Done", "Error"), table headers.

**Step 5: Verify build**

```bash
cd web && npm run build
```

**Step 6: Commit (one commit per panel)**

```bash
git add web/src/components/TasksPanel.tsx web/src/i18n/locales/en.json
git commit -m "feat: migrate TasksPanel to react-i18next"

git add web/src/components/SettingsPanel.tsx web/src/i18n/locales/en.json
git commit -m "feat: migrate SettingsPanel to react-i18next"

git add web/src/components/WikiPanel.tsx web/src/i18n/locales/en.json
git commit -m "feat: migrate WikiPanel to react-i18next"

git add web/src/components/SourceInbox.tsx web/src/i18n/locales/en.json
git commit -m "feat: migrate SourceInbox to react-i18next"
```

---

### Task 5: Migrate secondary panels (ConversationsPanel, SkillsPanel, SummariesPanel, SwarmPanel)

**Files:**
- Modify: `web/src/components/ConversationsPanel.tsx`
- Modify: `web/src/components/SkillsPanel.tsx`
- Modify: `web/src/components/SummariesPanel.tsx`
- Modify: `web/src/components/SwarmPanel.tsx`
- Modify: `web/src/i18n/locales/en.json`

**Same pattern as Task 4.** Add keys to `en.json` (prefixed `conversations.*`, `skills.*`, `summaries.*`, `swarm.*`), import `useLocale`, replace strings, verify build, commit per panel.

**Step 1: Migrate ConversationsPanel**

**Step 2: Migrate SkillsPanel**

**Step 3: Migrate SummariesPanel**

**Step 4: Migrate SwarmPanel**

**Step 5: Commit (one per panel)**

---

### Task 6: Migrate remaining panels (MCPPanel, MaintenancePanel, PendingUsersPanel, HealthDashboard)

**Files:**
- Modify: `web/src/components/MCPPanel.tsx`
- Modify: `web/src/components/MaintenancePanel.tsx`
- Modify: `web/src/components/PendingUsersPanel.tsx`
- Modify: `web/src/components/HealthDashboard.tsx`
- Modify: `web/src/i18n/locales/en.json`

**Same pattern as Task 4.** Prefixed keys: `mcp.*`, `maintenance.*`, `pending.*`, `health.*`.

**Step 1: Migrate MCPPanel**

**Step 2: Migrate MaintenancePanel**

**Step 3: Migrate PendingUsersPanel**

**Step 4: Migrate HealthDashboard**

**Step 5: Commit (one per panel)**

---

### Task 7: Migrate common components (AppSkeletons, ThemeToggle, ConfirmModal, ErrorCard, MarkdownReader)

**Files:**
- Modify: `web/src/components/common/AppSkeletons.tsx`
- Modify: `web/src/components/common/ThemeToggle.tsx`
- Modify: `web/src/components/common/ConfirmModal.tsx`
- Modify: `web/src/components/common/ErrorCard.tsx`
- Modify: `web/src/components/common/MarkdownReader.tsx`
- Modify: `web/src/components/App.tsx` (the `PanelLoading` "Loading…" text)
- Modify: `web/src/i18n/locales/en.json`

**Step 1: Add common keys to `en.json`**

```json
{
  "common.skillsLoading": "Loading skills",
  "common.skillsLoadingSr": "Loading skills...",
  "common.searching": "Searching",
  "common.searchingSr": "Searching...",
  "common.graphLoading": "Loading graph",
  "common.graphLoadingSr": "Loading graph...",
  "common.themeSelector": "Theme selector",
  "common.themeOption": "Theme {name}",
  "common.pageMetadata": "Page metadata",
  "common.closeWindow": "Close window"
}
```

**Step 2: Migrate `AppSkeletons.tsx`**

Replace the hardcoded Italian aria-labels with `t()` calls:

| Before | After |
|--------|-------|
| `aria-label="Caricamento skills"` | `aria-label={t('common.skillsLoading')}` |
| `Caricamento skills...` | `{t('common.skillsLoadingSr')}` |
| `aria-label="Ricerca in corso"` | `aria-label={t('common.searching')}` |
| `Ricerca in corso...` | `{t('common.searchingSr')}` |
| `aria-label="Caricamento grafo"` | `aria-label={t('common.graphLoading')}` |
| `Caricamento grafo...` | `{t('common.graphLoadingSr')}` |

These skeleton components are plain functions (not hooks), so pass `t` as props or use `useLocale()` directly (they meet hook rules).

**Step 3: Migrate `ThemeToggle.tsx`**

Replace hardcoded Italian labels:

| Before | After |
|--------|-------|
| `label: "Contrasto"` | `label: t('sidebar.highContrast')` (reuse sidebar key) |
| `short: "Alto contrasto"` | `short: t('sidebar.highContrast')` |
| `aria-label="Selettore tema"` | `aria-label={t('common.themeSelector')}` |
| `` `Tema ${option.short}` `` | `` t('common.themeOption', { name: option.short }) `` |

Convert to use `useLocale()` directly (it's already a function component).

**Step 4: Migrate `MarkdownReader.tsx`**

Replace `aria-label="Metadati pagina"` with `aria-label={t('common.pageMetadata')}`.

**Step 5: Migrate `ConfirmModal.tsx` and `ErrorCard.tsx`**

Extract any remaining hardcoded strings (button labels, aria attributes).

**Step 6: Migrate `PanelLoading` in `App.tsx`**

Replace `"Loading…"` with `t('common.loading')`.

**Step 7: Verify build**

```bash
cd web && npm run build
```

**Step 8: Commit**

```bash
git add web/src/components/common/ web/src/components/App.tsx web/src/i18n/locales/en.json
git commit -m "feat: migrate common components to react-i18next"
```

---

### Task 8: Add Italian translations

**Files:**
- Modify: `web/src/i18n/locales/it.json`

**Step 1: Populate `it.json` with Italian translations for all keys in `en.json`**

Translate every key. Example entries:

```json
{
  "common.loading": "Caricamento…",
  "common.close": "Chiudi",
  "common.tryAgain": "Riprova",
  "common.error": "Qualcosa è andato storto",
  "common.checkConsole": "Controlla la console per lo stack completo.",
  "common.skillsLoading": "Caricamento skills",
  "common.skillsLoadingSr": "Caricamento skills...",
  "common.searching": "Ricerca in corso",
  "common.searchingSr": "Ricerca in corso...",
  "common.graphLoading": "Caricamento grafo",
  "common.graphLoadingSr": "Caricamento grafo...",
  "common.themeSelector": "Selettore tema",
  "common.themeOption": "Tema {name}",
  "common.pageMetadata": "Metadati pagina",
  "common.closeWindow": "Chiudi finestra",

  "sidebar.brand": "Aura",
  "sidebar.tagline": "Second brain",
  "sidebar.home": "Home",
  "sidebar.wiki": "Wiki",
  "sidebar.graph": "Grafo",
  "sidebar.sources": "Fonti",
  "sidebar.tasks": "Task",
  "sidebar.swarm": "Swarm",
  "sidebar.skills": "Skills",
  "sidebar.mcp": "MCP",
  "sidebar.pending": "In attesa",
  "sidebar.conversations": "Conversazioni",
  "sidebar.summaries": "Sommari",
  "sidebar.maintenance": "Manutenzione",
  "sidebar.settings": "Impostazioni",
  "sidebar.lightTheme": "Tema chiaro",
  "sidebar.darkTheme": "Tema scuro",
  "sidebar.highContrast": "Alto contrasto",
  "sidebar.cycleTheme": "Cambia tema: chiaro → scuro → contrasto",
  "sidebar.signOut": "Esci",
  "sidebar.signingOut": "Uscita…",
  "sidebar.revokeToken": "Revoca questo token e torna al login",
  "sidebar.signedOut": "Disconnesso."
}
```

Continue for all remaining keys (`login.*`, `shell.*`, `error.*`, `tasks.*`, `settings.*`, `wiki.*`, `sources.*`, `conversations.*`, `skills.*`, `summaries.*`, `swarm.*`, `mcp.*`, `maintenance.*`, `pending.*`, `health.*`).

**Step 2: Verify build**

```bash
cd web && npm run build
```

**Step 3: Test Italian rendering**

Set `navigator.language` override in browser DevTools or test by temporarily modifying the detection in `i18n/index.ts`:

```typescript
lng: 'it', // force Italian for testing
```

Visually spot-check each panel. Verify dates render in Italian format (e.g., "3 maggio 2026").

**Step 4: Remove test override and commit**

```bash
git add web/src/i18n/locales/it.json
git commit -m "feat: add Italian translations for all UI strings"
```

---

### Task 9: Verify e2e tests pass

**Files:**
- Potentially modify: `web/e2e/*.spec.ts` (only if tests assert on English strings)

**Step 1: Run e2e tests**

```bash
cd web && npm run e2e
```

**Step 2: Fix any broken selectors**

The Playwright tests in `web/e2e/` may reference English text labels that now come from `t()`. Since the tests run in Chromium with `navigator.language = "en-US"`, English translations should still render. If any test was relying on exact string matches that changed subtly, fix the selectors.

Common fixes:
- Replace `page.getByText('Sign in')` with `page.getByText(t('login.signIn'))` — but since tests can't import `t()`, use role-based selectors instead: `page.getByRole('button', { name: 'Sign in' })`
- Replace text-based assertions with test-id or role-based ones

**Step 3: Run e2e again to confirm green**

```bash
cd web && npm run e2e
```

Expected: All tests pass.

**Step 4: Final commit**

```bash
git add web/e2e/
git commit -m "test: fix e2e selectors for i18n migration"
```
