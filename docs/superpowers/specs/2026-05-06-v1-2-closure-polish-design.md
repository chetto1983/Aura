# v1.2 Closure and Polish Design

Date: 2026-05-06
Status: approved for spec writing

## Purpose

v1.2 Closure and Polish finishes the active universal-ingestion thread and the recent dashboard polish work. The goal is not to reopen the older broad v1.2 memory-quality plan. The goal is to make the code that already landed feel complete, verified, and accurately reflected in `.planning/`.

This milestone is for the current Aura codebase after v1.1 Trustworthy Daily Use. It closes pending handoffs around source upload, normalized extraction, source ingestion, and frontend audit fixes.

## Scope

In scope:

- Verify the active source loop: upload, store, normalize, ingest, and create a wiki source page.
- Cover the active upload formats already represented in code: PDF, TXT, Markdown, JSON, CSV, DOCX, and XLSX.
- Keep PDF on the existing Mistral OCR path and normalized `extract.md`/`extract.json` adapter.
- Use Go extractors for TXT, Markdown, JSON, and CSV.
- Verify the existing Pyodide XLSX extractor work.
- Confirm whether DOCX is truly implemented. If it is not implemented, document it as deferred despite being accepted by the format policy.
- Ensure `extract_complete` sources can be ingested through API/dashboard flows.
- Verify Telegram and dashboard uploads use the shared source format policy.
- Verify the recent frontend polish audit: i18n, all-page rendering, source inbox, graph controls, conversation URL state, keyboard access, and secondary page semantics.
- Update `.planning/STATE.md`, `.planning/ROADMAP.md`, `.planning/PROJECT.md`, `.planning/REQUIREMENTS.md`, and `docs/implementation-tracker.md` so they match shipped reality.

Out of scope:

- The older broad v1.2 memory-quality scorecard.
- Retrieval ranking changes.
- Wiki proposal deduplication or proposal text redesign.
- Skills plus sandbox memory E2E beyond the already landed sandbox smoke coverage.
- New source types such as images, audio, video, PPTX, email, websites, or cloud connectors.
- Broad `internal/tools` package split.
- Settings at-rest encryption.
- Broad maintainability refactors.

## Current Findings

The repository is clean on `master`, and recent commits show active v1.2 work has landed. The live handoff says Task 5 completed dashboard and Telegram universal uploads, but deferred Task 6: extracted non-PDF auto-ingest through an `AfterExtract` style hook. A later frontend audit fixed dashboard-side ingestion for `extract_complete` sources and added strong frontend audit commands.

The planning docs still describe v1.1 as the current milestone. This milestone must reconcile that mismatch instead of leaving Aura with code history that says v1.2 and planning state that says "choose next milestone."

## Requirements

### V12-CLOSE-01: Source Loop Closure

Aura must prove that an uploaded source can complete the loop from raw file to wiki page for the active formats.

Acceptance:

- PDF upload still produces OCR evidence and normalized extraction evidence.
- TXT, Markdown, JSON, and CSV uploads produce `extract.md` and `extract.json`.
- `extract_complete` sources can be ingested into wiki source pages.
- Ingested pages point users to the correct extracted markdown path.
- Duplicate uploads keep the existing sha256 dedup behavior.
- Unsupported uploads fail before creating partial source records.
- Tests cover upload, extraction, ingest, and source status transitions.

### V12-CLOSE-02: Format Policy Truth

The supported-format story must match real behavior.

Acceptance:

- Dashboard, Telegram, and API use the same format policy.
- Frontend accepted file types match backend accepted file types.
- DOCX and XLSX are either fully verified or explicitly marked as known gaps.
- User-facing copy does not promise a format that cannot complete the source loop.

### V12-CLOSE-03: Frontend Polish Verification

The recent dashboard polish must be preserved by a repeatable verification path.

Acceptance:

- `npm --prefix web run i18n:check` passes.
- `npm --prefix web run e2e:pages` passes.
- `npm --prefix web run e2e` passes, unless a failure is documented as environmental and independently checked.
- `npm --prefix web run audit:frontend` passes or has a documented local dependency blocker.
- Source inbox shows extracted sources and exposes usable Download and Ingest actions.
- Graph, conversations, skills, MCP, summaries, swarm, maintenance, and settings retain the audit fixes recorded in `docs/audits/2026-05-05-frontend-e2e-i18n-audit.md`.

### V12-CLOSE-04: Planning Reconciliation

The active project docs must tell one story.

Acceptance:

- `.planning/STATE.md` names v1.2 Closure and Polish as active or complete, depending on execution state.
- `.planning/ROADMAP.md` records the v1.2 phases and success criteria.
- `.planning/PROJECT.md` records v1.2 as shipped when the gate passes.
- `.planning/REQUIREMENTS.md` lists only the active closure requirements, not the old broad memory-quality scope.
- `docs/implementation-tracker.md` has a compact v1.2 closure entry with verification results and known gaps.

### V12-CLOSE-05: Release Gate Lite

Closure requires focused backend, frontend, and planning verification.

Acceptance:

- Focused Go tests for source, ingest, API, Telegram, and frontend-facing upload behavior pass.
- `go test ./...` passes.
- `go build ./...` passes.
- Frontend build and audit commands pass or have documented environmental blockers.
- If packaging files changed, GoReleaser snapshot and Windows GUI subsystem checks pass.
- A validation file under `.planning/phases/` records commands, results, supported formats, and known gaps.

## Architecture

The milestone keeps the existing architecture. It does not add a new ingestion system.

The important boundary is normalized source evidence:

1. User uploads a file through Telegram or the dashboard.
2. Aura stores the immutable original file in `internal/source`.
3. PDF follows Mistral OCR, then writes normalized `extract.md` and `extract.json`.
4. TXT, Markdown, JSON, and CSV use Go-native extractors.
5. XLSX uses the existing fixed Pyodide extractor if the release gate confirms it works.
6. DOCX remains accepted by policy only if a real extractor path is present and verified.
7. `internal/ingest` compiles either OCR or extracted markdown into a wiki source page.
8. The dashboard source inbox shows the status and exposes next actions.

The design prefers tightening behavior and documentation over adding new abstractions. If a gap appears, fix the smallest path that completes the active loop.

## Phases

### Phase 1: Pending Source Loop Closure

Audit current source behavior and fix any gap that prevents supported active formats from reaching a wiki source page.

Key checks:

- `source.DetectUploadFormat`
- `source.ExtractGo`
- `source.ExtractWithPyodide`
- `api.handleSourceUpload`
- Telegram document processing
- `ingest.Pipeline.Compile`
- dashboard source inbox actions

### Phase 2: Frontend Polish Gate

Run the frontend audit commands and repair regressions from the recent page audit.

Key checks:

- i18n key parity
- all route rendering
- source inbox upload and ingest affordances
- graph controls
- conversation filter URL state and keyboard access
- secondary page labels, roles, and localized status values

### Phase 3: Planning Reconciliation

Update planning docs and tracker entries so v1.2 is no longer hidden in commit history.

Key outputs:

- updated roadmap
- updated requirements
- updated project state
- validation file
- implementation tracker closure entry

### Phase 4: Release Gate Lite

Run the closure gate and record the result. Packaging checks are required only if release, embed, or packaging files changed during the closure work.

## Error Handling

Unsupported uploads should fail before source creation. Malformed supported files should become a durable failed source only when the original file was validly accepted and extraction failed. Dashboard and Telegram paths should show clear, format-neutral messages.

If a format is accepted but cannot complete extraction or ingest, the milestone must either fix it or document it as a known gap. Silent partial support is not acceptable for closure.

## Testing Strategy

Focused backend tests:

- source format detection and status validation
- Go extractor success and failure cases
- PDF OCR adapter output
- API upload for text-like sources
- API ingest for `extract_complete`
- Telegram document validation and non-PDF extraction path
- ingest pipeline for `extract_complete`

Frontend tests:

- i18n audit
- all-page E2E
- source universal upload E2E
- graph controls
- conversation filter and keyboard access
- secondary page controls from the frontend audit

Broad checks:

- `go test ./...`
- `go build ./...`
- `npm --prefix web run build`
- `npm --prefix web run audit:frontend`

## Known Deferrals

These belong beyond this closure milestone unless the active code already implements them:

- mixed-source memory scorecard
- retrieval ranking improvements
- wiki proposal deduplication
- source-backed proposal text redesign
- skill-guided sandbox memory E2E as a release gate
- full DOCX extraction if no verified extractor exists
- broader source formats and cloud connectors

## Success Criteria

v1.2 Closure and Polish is complete when:

- active supported uploads can complete or have explicit known-gap status;
- extracted non-PDF sources can be ingested into wiki pages;
- frontend polish audit commands pass or have documented environmental blockers;
- planning docs and implementation tracker match the codebase;
- release validation is recorded under `.planning/phases/`;
- the working tree is clean after the closure commit.
