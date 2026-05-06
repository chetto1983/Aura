# Frontend E2E + i18n Audit - 2026-05-05

## Finding FE-AUDIT-01: Frontend Had No Single 100% Page Sweep Command

**Severity:** Medium
**Status:** Fixed

Aura had useful dashboard E2E coverage, but no dedicated command that proved every routed dashboard page renders without missing locale artifacts. This allowed page-level gaps to hide behind workflow-specific tests.

**Evidence found:**
- Existing Playwright specs covered selected flows, but not every route in `web/src/App.tsx`.
- The first all-pages sweep exposed `/graph` lacking a semantic page heading.
- The same sweep also exposed that naive missing-key detection can false-positive on file names such as `mcp.json`, so the durable missing-locale check belongs in a locale audit script rather than page text regexes.

**Fix:**
- Added `npm run i18n:check` for locale key parity, interpolation parity, single-brace placeholder detection, and direct `t()` key coverage.
- Added `npm run e2e:pages` to visit all dashboard routes and assert each route renders without placeholder leaks or load/not-found errors.
- Added `npm run audit:frontend` as the useful frontend audit command: i18n check, lint, build, starts Aura from the current tree, runs all-page E2E, then stops Aura.
- Localized the graph route loading/error/mobile labels and added an accessible hidden page heading.

**Verification:**
- `npm --prefix web run i18n:check`
- `npm --prefix web run e2e:pages`
- `npm --prefix web run audit:frontend`

## Finding FE-AUDIT-02: Source Inbox UI Was Still PDF-Only After Universal Uploads

**Severity:** High
**Status:** Fixed

The backend accepts and normalizes text-like uploads (`.txt`, `.md`, `.json`, `.csv`) into `extract_complete`, but the dashboard Source Inbox still filtered uploads to `.pdf` only. Successful extracted sources were also at risk of disappearing because the frontend status union and grouping did not include `extract_complete`.

**Evidence found:**
- `web/src/components/SourceInbox.tsx` accepted only `.pdf,application/pdf` and skipped every non-PDF file before calling `/api/sources/upload`.
- `web/src/types/api.ts` did not model `markdown`, `json`, `csv`, `extracting`, or `extract_complete`.
- The Source page did not expose downloads for text-like raw files even though `/api/sources/{id}/raw` supports them.

**Fix:**
- Updated Source Inbox to accept PDF, TXT, MD, JSON, and CSV uploads.
- Added `extracting` / `extract_complete` grouping and localized status labels.
- Added text-like raw downloads and an Ingest action for `extract_complete` rows.
- Added `web/e2e/source-universal-upload.spec.ts` to prove a text upload request is sent, the extracted row renders, and Download/Ingest actions are visible.

**Verification:**
- `npm --prefix web run e2e`
- `npm --prefix web run audit:frontend`

## Finding FE-AUDIT-03: Extracted Sources Could Not Be Compiled From The Dashboard

**Severity:** High
**Status:** Fixed

Non-PDF uploads stopped at `extract_complete`, but `POST /api/sources/{id}/ingest` and `internal/ingest.Pipeline` only accepted `ocr_complete`. That meant the dashboard could show a useful extracted source but could not complete the source-to-wiki loop.

**Evidence found:**
- `internal/api/sources_write.go` rejected every `extract_complete` source with a conflict.
- `internal/ingest/pipeline.go` always read `ocr.md`, while extracted sources write `extract.md`.

**Fix:**
- `internal/ingest.Pipeline` now accepts `extract_complete`, reads `extract.md` for non-PDF sources, and renders the wiki source page against the normalized extracted markdown path.
- `POST /api/sources/{id}/ingest` now accepts `extract_complete`.
- Added backend coverage for direct pipeline compile and upload-then-ingest flow.

**Verification:**
- `go test ./internal/ingest ./internal/api -run "TestCompile_ExtractCompleteSource|TestSourceUploadTextCanBeIngested|TestSourceUploadAcceptsText" -count=1`
- `go test ./...`
- `go build ./...`

## Finding FE-AUDIT-04: Remaining Page Strings Bypassed Locale Coverage

**Severity:** Medium
**Status:** Fixed

Several secondary surfaces rendered hardcoded English or Italian strings. These were not caught by page smoke tests because the pages still loaded successfully.

**Evidence found:**
- `WikiPageView` had hardcoded loading, back, not-found, and error text.
- `ConversationDrawer` had hardcoded drawer labels and close text.
- `StderrLogSheet` mixed hardcoded Italian strings into the dashboard.
- `SummariesPanel` had a hardcoded evidence-chip aria-label.

**Fix:**
- Localized the remaining strings in English and Italian.
- Extended the i18n audit key count and direct `t()` coverage.

**Verification:**
- `npm --prefix web run i18n:check`
- `npm --prefix web run lint`

## Finding FE-AUDIT-05: Graph Page Was A Canvas Without Normal-User Controls

**Severity:** Medium
**Status:** Fixed

The real-browser page-by-page audit showed `/graph` rendered a force-directed canvas, but it gave a normal dashboard user no visible way to search, fit/reset the graph, understand node/link volume, or inspect a selected node before opening its wiki page.

**Evidence found:**
- Headed Chromium inventory for `/graph` originally had no visible buttons or inputs, only the canvas.
- The page depended on hover/click affordances that were not discoverable from the UI.

**Fix:**
- Added graph search by title, slug, or category.
- Added a fit-to-view button, node/link counts, match count, and selected-node context with an Open page action.
- Localized the new controls in English and Italian.
- Added Playwright coverage that proves `/graph` exposes these controls.

**Verification:**
- `npm --prefix web run e2e`
- Headed inline Playwright browser audit against `http://127.0.0.1:8081` with the real dashboard token.

## Real Browser Audit Notes

The final normal-user pass used headed Chromium via Playwright directly against the running dev app, not a generated temporary audit page/file. It visited `/`, `/wiki`, `/graph`, `/sources`, `/tasks`, `/swarm`, `/skills`, `/mcp`, `/pending`, `/conversations`, `/summaries`, `/maintenance`, and `/settings`.

**Confirmed:**
- No route had horizontal overflow at desktop viewport.
- `/graph` exposes search, fit, and counts.
- `/sources` accepts a real TXT upload, exposes Download before ingest, successfully ingests it through `POST /api/sources/{id}/ingest`, and still exposes Download after ingest.
- `/tasks` exposes a localized submit action (`Pianifica`) in the new-task dialog.
- No browser console errors were emitted during the headed pass.
