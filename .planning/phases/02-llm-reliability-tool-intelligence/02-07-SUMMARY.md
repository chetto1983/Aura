---
phase: 02-llm-reliability-tool-intelligence
plan: "07"
subsystem: api, web
tags: [git-tracking, wiki, dashboard, unversioned, badge]
dependency_graph:
  requires: ["02-03"]
  provides: ["GIT-01-observable"]
  affects: ["internal/api/wiki.go", "internal/api/wiki_test.go", "web/src/types/api.ts", "web/src/components/WikiPageView.tsx"]
tech_stack:
  added: []
  patterns: ["pageFrontmatter map passthrough", "conditional Tailwind badge", "i18n typed key"]
key_files:
  created: ["internal/api/wiki_test.go"]
  modified: ["internal/api/wiki.go", "web/src/types/api.ts", "web/src/components/WikiPageView.tsx", "web/src/i18n/locales/en.json", "web/src/i18n/locales/it.json"]
decisions:
  - "i18n keys used for badge text (wikiPage.unversionedBadge / wikiPage.unversionedTitle) — project enforces strict TranslationKey type checking; inline strings would bypass the type system"
  - "Badge reads from fm.unversioned first (backend source of truth per Task 1), with data.unversioned fallback for forward compat"
  - "List handler (WikiPageSummary) not modified — BLOCKER 7: WikiPageSummary has no Frontmatter field; widening is out of scope"
metrics:
  duration: "~20 minutes"
  completed: "2026-05-11"
  tasks: 2
  files: 6
---

# Phase 02 Plan 07: Unversioned Flag Dashboard Passthrough Summary

One-liner: Pass `Page.Unversioned` (GIT-01) from wiki store through the `GET /wiki/page?slug=…` API response and render a yellow "Git tracking pending" badge in the wiki page detail view.

## What Was Built

### Task 1: Backend (commit `80f53182`)

`internal/api/wiki.go` — `pageFrontmatter` now sets `fm["unversioned"] = page.Unversioned` unconditionally alongside all existing frontmatter fields. This means the JSON response for `GET /wiki/page?slug=X` always includes `frontmatter.unversioned` as a boolean (true or false), which is TS strict-typing friendly.

The list handler (`handleWikiPages` → `loadWikiSummaries` → `WikiPageSummary`) was NOT modified per BLOCKER 7 of the 2026-05-10 plan revision: `WikiPageSummary` has no `Frontmatter` field and widening the list-row shape would require additional frontend changes beyond this plan's scope.

`internal/api/wiki_test.go` (new file) — Two tests:

- `TestWikiPage_UnversionedJSON_False`: Writes a page on the happy path (git commit succeeds), hits `GET /wiki/page?slug=normal-page`, asserts `frontmatter["unversioned"] == false`.
- `TestWikiPage_UnversionedJSON_True`: Installs a failing git commit via `store.SetGitCommitFuncForTest` (the EXPORTED seam from Plan 03, callable from `package api`), writes a page (which triggers D-17 Unversioned=true re-write), hits `GET /wiki/page?slug=bad-commit-page`, asserts `frontmatter["unversioned"] == true`.

Both tests use `e.do("GET", "/wiki/page?slug="+slug)` — the correct route with slug as a query parameter (verified at `internal/api/router.go:156`). `go test -count=1 ./internal/api/` exits 0.

### Task 2: Frontend (commit `a4e9844d`)

`web/src/types/api.ts` — `WikiPage` interface gains `unversioned?: boolean` (optional for deploy-window backward compat). The backend always sends it in new responses; old responses simply omit it and the `undefined` case renders nothing.

`web/src/i18n/locales/en.json` and `it.json` — Added typed i18n keys:
- `wikiPage.unversionedBadge` = "Git tracking pending" (EN) / "Tracciamento Git in sospeso" (IT)
- `wikiPage.unversionedTitle` = accessible tooltip text explaining disk-saved / audit-degraded semantics

`web/src/components/WikiPageView.tsx` — Badge conditional added:
```tsx
const unversioned = fm.unversioned === true || data.unversioned === true;
// ...
{unversioned && (
  <span
    className="inline-flex items-center px-2 py-0.5 rounded text-xs font-medium bg-yellow-100 text-yellow-800 border border-yellow-300 mt-1"
    title={t('wikiPage.unversionedTitle')}
    aria-label={t('wikiPage.unversionedBadge')}
  >
    {t('wikiPage.unversionedBadge')}
  </span>
)}
```

Badge is placed adjacent to the page `<h1>` title. Yellow Tailwind classes (`bg-yellow-100 text-yellow-800 border-yellow-300`) match the warning/caution color palette. `npm run build` exits 0; `npm run i18n:check` exits 0 (818 keys, 2 locales).

## API Shape

```json
{
  "slug": "my-page",
  "title": "My Page",
  "body_md": "...",
  "frontmatter": {
    "title": "My Page",
    "schema_version": 2,
    "prompt_version": "v1",
    "created_at": "2026-05-11T00:00:00Z",
    "updated_at": "2026-05-11T00:00:00Z",
    "unversioned": false
  }
}
```

When a git commit fails: `"unversioned": true`. Always present, never absent.

## BLOCKER 7 Status

All three BLOCKER 7 items are closed:
1. Route URL correct: tests use `/wiki/page?slug=` (query param), not `/wiki/{slug}` (path segment).
2. List-row scope explicitly declined: `WikiPageSummary` is unchanged.
3. Exported test seam used: `store.SetGitCommitFuncForTest` (not the unexported `gitCommitFunc` field).

## Deviations from Plan

### Auto-adapted — i18n pattern

**Found during:** Task 2

**Issue:** The acceptance criterion `grep -n "Git tracking pending" web/src/components/WikiPageView.tsx matches at least once` assumed inline string, but the project uses strict TypeScript-typed i18n (`TranslationKey` derived from `keyof typeof en`). Using an inline string would work visually but would diverge from the existing codebase pattern (every UI string in `WikiPageView` uses `t(...)`). The `i18n:check` script validates key consistency across locales.

**Fix:** Used `t('wikiPage.unversionedBadge')` instead of the literal string in the component. The literal "Git tracking pending" appears in `web/src/i18n/locales/en.json` line 429. The plan's Step 3 explicitly covers this case ("If the project uses i18n, add a translation key...").

**Files modified:** `web/src/i18n/locales/en.json`, `web/src/i18n/locales/it.json`

## Known Stubs

None. The flag is fully wired from `wiki.Page.Unversioned` (set by `store_writes.go` D-17/D-18) through `pageFrontmatter` to the JSON response and rendered in the UI.

## Threat Surface Scan

No new network endpoints, auth paths, file access patterns, or schema changes at trust boundaries were introduced. The `unversioned` boolean is a passthrough of an existing field — no new disclosure surface. The `/api/wiki` endpoint is already auth-gated.

## Self-Check: PASSED

- `internal/api/wiki_test.go` exists: FOUND
- `internal/api/wiki.go` contains `unversioned`: FOUND (line 123)
- `web/src/types/api.ts` contains `unversioned?: boolean`: FOUND (line 49)
- `web/src/components/WikiPageView.tsx` contains badge: FOUND (lines 42-50)
- Commit `80f53182` exists: FOUND
- Commit `a4e9844d` exists: FOUND
- `go test -count=1 ./internal/api/` exits 0: PASSED
- `npm run build` exits 0: PASSED
- `npm run i18n:check` exits 0: PASSED

## Notes for Plan 08

An optional integration test walking the full path (LLM write → gitCommit fails → `GET /wiki/page?slug=X` returns `frontmatter.unversioned: true`) could be added during the heuristic-removal sweep verification to close the observability loop end-to-end.
