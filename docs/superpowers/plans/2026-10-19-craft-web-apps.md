# Craft web apps in the cockpit: implementation plan

**Status (2026-10-20): COMPLETE.** Tasks 1–9 are done and on master. Task 8's live run closed on
the lab VM on build `5d12271d7`: the operator's pass in the cockpit, the unsaved-changes notice
included, and the edits checked outside the apps, all in
`docs/superpowers/verification/2026-10-19-craft-web-apps.md`. The apps moved to newer releases on
2026-10-24 (`2026-10-24-craft-apps-upgrade.md`). The operator asked for the spike and "tutte le
modifiche necessarie per le altre non supportate a oggi con la modifica ed il salvataggio", then, on
the Italian catalogs, "Procedi pure" (Task 7), and "Hai docker basta farlo partire" (Task 8's image
build).

> **For agentic workers:** REQUIRED SUB-SKILL: superpowers:executing-plans, task by task, with
> test-driven-development.

**Goal:** open and edit every file kind the Craft MCP servers handle in the cockpit with the same app's web
build, and store what the app saves in the library.

**Architecture:**
- aura serves `AURA_APPS_DIR/<app>` at `/apps/<app>/` for the six Craft apps (`internal/staticapp`), public
  like GridCraft was.
- One frame component (`CraftFrame`) speaks the `?host=parent` bridge with any app; `EditInCraft` wraps
  the PDF, docx, image and video viewers with an Edit button where the source can save.
- GridCraft ships as its official zip; PdfCraft, WordCraft, DeckCraft, PhotoCraft and FilmCraft are built
  in the image from their tags plus `docker/craft-web/<app>.patch` (`docker/craft-web/build.sh`).

**Tech Stack:** Go 1.27; React 19 + Vite cockpit; Rust 1.97 with Trunk 0.21.14, wasm-bindgen 0.2.129,
binaryen 123 for the patched builds.

**Spec:** `docs/superpowers/specs/2026-10-19-craft-web-apps-design.md`. **Spike:** `spikes/craft-web/FINDINGS.md`.

## Global Constraints

- Every Go change test-first; `go vet`, build, tests and `-race` per touched package.
- The web gates stay green: lint, typecheck, format, the suite with coverage, knip.
- `internal/webui/dist` is rebuilt after the web changes.
- A patch touches the upstream source only where the bridge, the DeckCraft wasm bug, the browser's
  language or the brand terms require it.

## Review Focus

1. The bridge hears only its own frame on its own origin, on both sides (`CraftFrame`, each `host.rs`).
2. Only the six named apps are public under `/apps/`; any other folder there is not served.
3. A file whose URL does not name its content is revalidated, not cached for good.
4. No Edit where nothing can be saved (chat artifacts, Studio, share page).
5. The patched builds carry no ArtCraft mark; the licenses ship beside each build.

## Decisions taken while writing this plan

- **Light viewers stay for PDF, docx, images and video.** Making every app the preview would download
  11–16 MB of WebAssembly to read a file the browser shows natively; Edit loads it on request. xlsx keeps
  GridCraft (its decision of 2026-10-18) and pptx gets DeckCraft, having had no viewer.
- **Build the patched apps in the image, not in a publishing workflow.** It follows landrun's pattern
  (source pinned by tag and commit, built in a stage) and needs no release asset whose checksum would have
  to be published before the Dockerfile could pin it. Cost: about 50 minutes of cold image build (40 for
  the four bridge patches, then PdfCraft for its language).
- **No craft-fonts**, as the official web builds have none (FINDINGS 12). A first version of the build
  script fetched the commit each app's `release.yml` names; the DeckCraft build it made weighed 36.0 MB
  against the official 30.5, the difference being BIZ UDPGothic and Inter, so the fetch was dropped.

## File structure

- `internal/staticapp/staticapp.go` — cache by URL (`namesItsContent`).
- `internal/config/config.go` — `AppsDir` / `AURA_APPS_DIR`.
- `cmd/aura/serve_webui_routes.go` — `craftApps`, `isCraftAppRoute`; `cmd/aura/serve_webui.go` — the mounts.
- `web/src/chat/artifacts/renderers/CraftFrame.tsx` (was `XlsxPreview.tsx`), `EditInCraft.tsx`,
  `previewDispatch.tsx`; `web/src/chat/artifacts/artifactMeta.ts` — the `pptx` kind.
- `docker/craft-web/{build.sh,pdfcraft.patch,wordcraft.patch,deckcraft.patch,photocraft.patch,filmcraft.patch}`;
  `docker/aura/Dockerfile` (stages `craft-official`, `craft-web`); `.dockerignore`; `.goreleaser.yaml`.

## Tasks

### Task 1: the spike

- [x] Serve each build through `internal/staticapp` beside a host page speaking the bridge
  (`spikes/craft-web/serve`, `host/`), drive it in headless Chromium (`drive.mjs`).
- [x] PdfCraft official: open, rotate, Save pages, Ctrl+S; check with `pdfinfo`.
- [x] Record verdicts in `spikes/craft-web/FINDINGS.md`, reports in `runs/`.

### Task 2: the patches

- [x] WordCraft, DeckCraft, PhotoCraft, FilmCraft: `host.rs` + web crate wiring + web-sys features; the
  brand rewording; DeckCraft's `process::id` gate.
- [x] Build each, and open, edit and save a real fixture through the bridge; check each save outside the
  app (zipfile, Pillow, MP4 boxes).

### Task 3: static apps cached by URL

- [x] Failing test `TestOnlyAURLThatNamesItsContentIsCachedForGood`, then `namesItsContent`.
- [x] `go test ./internal/staticapp/`

### Task 4: the six apps under AURA_APPS_DIR

- [x] `TestServeWebuiServesTheCraftAppsWithoutASession` (every app public, a seventh folder not), then
  `craftApps`, `isCraftAppRoute`, the mounts, `AppsDir`; `.env.example`.
- [x] `go vet ./cmd/aura/ ./internal/... && go test ./cmd/aura/ ./internal/staticapp/ ./internal/config/`

### Task 5: the cockpit

- [x] `CraftFrame` from `XlsxPreview` (per-app prefix test), `EditInCraft` (no Edit without `save`; Edit
  swaps the app in), the `pptx` kind, the dispatch; i18n `edit`, a generic `notStarted`.
- [x] `npm run lint && npm run typecheck && npm run format:check && npm run test && npm run deadcode`
- [x] `npm run build` (refreshes `internal/webui/dist`).

### Task 6: the image

- [x] `docker/craft-web/build.sh`, run end to end outside Docker on DeckCraft.
- [x] Stages `craft-official` (GridCraft, PdfCraft zips by SHA-256) and `craft-web` (tools by SHA-256 per
  arch); `.dockerignore`, `.goreleaser.yaml` extra_files; `go test ./cmd/aura/ -run Release`.

### Task 7: the browser's language

- [x] Measure: with the browser in `it-IT`, PdfCraft and PhotoCraft start in English; the cause is their
  wasm branch, hard-wired to English (FINDINGS 13).
- [x] PdfCraft (`navigator.languages` through web-sys) and PhotoCraft (sys-locale's `js` feature) take the
  first browser language they have; a first build drew Japanese, Chinese, Arabic and Telugu as boxes, so
  a language without a face in the build is skipped (FINDINGS 14). PdfCraft, now modified, drops its
  ArtCraft marks; the Dockerfile builds it in `craft-web`.
- [x] `drive.mjs --locale <list>`: Italian, the fallthrough past Japanese, English for the skipped
  languages; the bridge again on both builds; `cargo clippy --target wasm32-unknown-unknown`; both
  patches applied to a fresh clone of their tag reproduce the tested source; `build.sh pdfcraft` end to
  end. WordCraft and FilmCraft under `ja-JP`, for comparison (FINDINGS 15).

### Task 8: acceptance

- [x] `docker build -f docker/aura/Dockerfile .`; record the build time and the image size change
  (51 min cold, +235 MB, +130 MB compressed; the image's apps saved through the bridge).
- [x] On the lab VM, the spec's Acceptance 2, 3 and 4 in the cockpit;
  `docs/superpowers/verification/2026-10-19-craft-web-apps.md` updated with the live run.
  - 2026-10-20, closed on build `5d12271d7`, confirmed by the operator in the cockpit, the notice
    included.
  - Checked outside the apps: the docx and the jpg carry the operator's edit. The xlsx, pdf, pptx
    and mp4 carry none from this pass.
  - The same day, a driven browser with the operator's session edited the xlsx (Q2) and the pptx
    (slide 1's title), both checked outside the apps. The pdf and mp4 edits are not run.
  - 2026-10-19: in progress. The builds below are live on the VM:
    - the preview opens from the file manager;
    - an image previews by its extension, and a video streams from the file manager's route;
    - Edit and Full screen sit in the header.

    A docx and an xlsx saved by the operator are valid but equal to the originals. The operator's
    pass is open.
  - The pass covers Task 9's notice too.

### Task 9: unsaved changes (2026-10-20)

Where the file manager stores a save, `CraftFrame` ignored `<app>:dirty`. An edit not yet saved lived
only in the app, and Esc, a click outside or ✕ dropped it without a word. The operator's docx and xlsx
came back equal to the originals, and nothing had said whether the edit had reached the file.

- [x] Failing tests:
  - `CraftFrame` shows "Unsaved changes: save from the app (Ctrl+S)" while the app reports dirty, and
    "Saved" once it saves and reports clean.
  - It tells its host, and holds the page's unload while dirty.
  - Once the host has held a close, it says the next close discards the edit.
  - `PreviewModal` holds the first close (Esc or ✕) while its preview reports unsaved changes, and
    closes on the second. Once the changes are saved, it closes at once.
- [x] `previewCloseGuardContext.ts` (the host's flag), `CraftFrame`, `PreviewModal`; i18n
  `unsavedChanges` and `closeDiscards`, in English and Italian.
- [x] `npm run lint && npm run typecheck && npm run format:check && npm run test && npm run deadcode`.
  Measured 2026-10-20: 369 files and 3,201 tests. One approvals test timed out at 5 s while the WSL
  gates ran beside the suite, and passed alone (7/7). Statements 92.55%, branches 86.95%.
- [x] `npm run build` (refreshes `internal/webui/dist`).
