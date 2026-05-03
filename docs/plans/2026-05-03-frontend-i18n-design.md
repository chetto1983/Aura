# Frontend i18n Design

**Date:** 2026-05-03
**Status:** approved

## Goal

Add multilanguage support to the React web frontend. Italian first, architecture must be extensible to more languages.

## Detection

Browser auto-detect via `navigator.language`. No user-facing language toggle. i18next-browser-languagedetector handles the detection chain: exact match → language-only match → fallback to `en`.

## Library

react-i18next (standard React i18n, ~30KB gzipped). Zero additional dependencies for formatting — `Intl.DateTimeFormat`, `Intl.RelativeTimeFormat`, and `Intl.NumberFormat` are built into all modern browsers.

## Structure

```
web/src/i18n/
  index.ts              # i18next init, detector config, Intl helpers
  locales/
    en.json             # source of truth — all keys defined here first
    it.json             # mirrors en.json structure with Italian values
  types.ts              # TypeScript key union for autocomplete
```

Supported languages declared as `["en", "it"]` in `index.ts`. Adding a language means adding a JSON file and appending to the array.

## Key strategy

- Namespaced by feature, flat: `"tasks.createButton"`, `"wiki.emptyState"`
- Descriptive keys, never index-based
- ICU MessageFormat for interpolation: `"tasks.lastRun": "Last run: {date}"`
- `useLocale()` hook wraps `useTranslation()` + Intl formatters so components import one thing

## Locale formatting

All via the `useLocale()` hook:

| Formatter | API |
|---|---|
| Dates | `formatDate(date, { dateStyle: 'long' })` |
| Relative time | `formatRelative(-2, 'minute')` → "2 minutes ago" / "2 minuti fa" |
| Numbers | `formatNumber(1234)` → `1,234` / `1.234` |

## Error handling

- **Missing key:** shows key itself in dev (debug-friendly), falls back to English value in production
- **Missing locale:** falls back to `en`
- **Missing translation file:** build-time error (static imports)
- **ICU variable error:** renders raw template, never throws

## Migration order

1. Setup: `i18n/index.ts`, `useLocale` hook, `en.json` scaffolding
2. Shell layer: Login, Shell, Sidebar, ErrorBoundary
3. Core panels: TasksPanel, SettingsPanel, WikiPanel, SourceInbox
4. Secondary panels: ConversationsPanel, SkillsPanel, SummariesPanel, SwarmPanel
5. Remaining: MCPPanel, MaintenancePanel, PendingUsersPanel, HealthDashboard
6. Common: AppSkeletons, ThemeToggle, ConfirmModal, ErrorCard

Each panel is migrated atomically. `en.json` is written first (keys = values), then `it.json` gets Italian values. English works fully during migration; Italian coverage grows incrementally.

## Existing Italian artifacts

The hardcoded Italian aria-labels in `AppSkeletons.tsx` and `ThemeToggle.tsx` get extracted into proper `it.json` keys during migration.
