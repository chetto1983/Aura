# Craft apps upgrade: six releases, three patches left

Date: 2026-10-29. The operator asked to check the Craft apps for updates and new pins ("Guarda le
craft app se hanno aggiornamenti e nuovi i pin"), then to do both bumps and "guarda se le patch
servono ancora", then to run the E2E with Docker, with Claude as the agent ("LLM sei te"), and to
update the skills. Run in a cloud dev container (4 CPUs, 15 GB, Docker 29.9.2 started by hand).

**Verdict: every app opens, edits and saves a real file at its new version, through the bridge
and through the live cockpit, checked with a tool outside the app. The six agent tasks pass on
the new MCP CLIs. Two patches are gone (WordCraft, PdfCraft): what they did is upstream now.**

| App | From | To | Web build | MCP CLI |
|---|---|---|---|---|
| GridCraft | v0.11.1 | v0.13.0 | official zip | `gridcraft-0.13.0` |
| PdfCraft | v0.13.0 | v0.15.0 | **official zip, patch dropped** | `pdfcraft-cli-0.15.0` |
| WordCraft | v0.10.0 | v0.12.0 | **official zip, patch dropped** | `wordcraft-0.12.0` |
| DeckCraft | v0.10.0 | v0.12.0 | patch rebased by hand | `deckcraft-0.12.0` |
| PhotoCraft | v0.15.1 | v0.17.0 | patch rebased by hand, CJK filter dropped | `photocraft-0.17.0` |
| FilmCraft | v0.12.0 | v0.14.0 | patch rebased by hand | `filmcraft-0.14.0` |

All six tags are dated 2026-10-28. Every tarball and zip matches its release's `SHA256SUMS.txt`.

## Which patches are still needed

- **WordCraft: no.** 0.12.0 ships the `?host=parent` bridge (#3822): ready, open, save, dirty,
  failed, with a `saveAs` flag the cockpit ignores (it compares the name). It also refuses a
  cross-origin or opaque parent. Its translations moved to storytold/craft-i18n, which
  `docker/craft-web/build.sh` never checked out: a source build would have been English only.
  The official zip has them and the CJK, Arabic and Devanagari faces (wasm 50.4 MB against
  45.9 MB for Aura's 0.10.0 build).
- **PdfCraft: no.** The patch only dropped, from Auto, the languages its web build had no face
  for (FINDINGS 14), and with it the ArtCraft marks a modified build may not carry. All six apps
  now pin storytold/egui@6d518fc, whose web runner draws a glyph no font has through the
  browser's Canvas 2D (`crates/eframe/src/web/canvas_glyphs.rs`). Measured on Aura's own 0.15.0
  build, without craft-fonts: a file named in Korean, Hindi, Thai, Lao, Persian and Japanese drew
  in the tab. The official zip draws the whole interface in Korean, Hindi, Thai, Persian, Lao and
  Japanese, and in Italian for `it-IT`.
  - The filter was also incomplete: 0.13.0 already offered `ckb`, `th`, `lo`, `hi` and `ko`,
    which it let through.
- **DeckCraft, PhotoCraft, FilmCraft: yes.** None has a bridge upstream (no `postMessage` in
  their web crates). PhotoCraft 0.17.0 still draws its web interface in English whatever the
  browser says (`detect_system_tags` returns nothing on wasm).
  - PhotoCraft's filter (ja, ko, zh, and hi, which it never had) goes, for the PdfCraft reason:
    measured with `it-IT` Italian, `ja-JP` Japanese, `hi-IN` Hindi, all drawn.
- **The rebase.** Upstream moved code between files: DeckCraft's About into `dialogs/about.rs`
  and the backstage into `backstage/files.rs`; PhotoCraft's frame hooks into
  `logic_frame`/`ui_frame`; FilmCraft's `Id::new` into `Id::unique`.
  - The `ArtCraft` mentions in the patched trees' Rust source, outside comments and tests, are
    the same set as in the old patched trees.
  - PhotoCraft 0.17.0 adds its app icon to the title bar. `assets/app-icon/LICENSE.txt` puts it
    under MIT/Apache, not under the brand terms, so it stays.

## The builds

`docker/craft-web/build.sh` ran outside Docker with the Dockerfile's tools, from the same
SHA-256-pinned tarballs (trunk 0.21.14, wasm-bindgen 0.2.129, wasm-opt 123), rust 1.97.0. Every
new tag declares `rust-version` 1.95, and every lock pins wasm-bindgen 0.2.129.

- **Times:** deckcraft 484 s, photocraft 795 s, filmcraft 355 s (and pdfcraft 479 s, before its
  patch was dropped).
- **Warnings:** none comes from the patched files.
- **`craft-official` stage:** built with BuildKit from `docker/aura/Dockerfile`. It unpacks the
  three zips, removes `_headers` and `.htaccess`, and gzips the wasm and the glue.

## The bridge, app by app

Harness `spikes/craft-web/` (`serve` through `internal/staticapp`, `drive.mjs`), headless
Chromium 1243 with SwiftShader.

| App | Opened | Edited | Saved, checked outside the app |
|---|---|---|---|
| GridCraft 0.13.0 | `sample.xlsx` | A3 typed | `sample.xlsx`, openpyxl: A3 "Modificato da Aura", A1:B2 kept |
| PdfCraft 0.15.0 (official and patched) | `Appunti.pdf` | Organize pages › rotate › Save pages | `Appunti.pdf`, `pdfinfo` `Page rot: 90` (0 before) |
| WordCraft 0.12.0 | `sample.docx` | text typed after the last paragraph | `sample.docx` (`saveAs: false`), `document.xml` ends with it |
| DeckCraft 0.12.0 | `sample.pptx` | title edited | `sample.pptx`, 2 slides, title "Corpus di prova Aura modificato", 13,148 bytes as before |

Every app announced its new version in `<app>:ready`. Each sent `<app>:dirty` true on the edit
and false after the save. The one 404 in every run is the harness page's favicon.

## The cockpit, live

`aura serve` (this branch) ran over Postgres 18.6 and Garage 2.4.1 in Docker, on the web-e2e job's
recipe, with `AURA_APPS_DIR` holding the six new builds. A local Playwright spec, not committed,
ran each case:
1. upload the file with the file manager's upload route;
2. open it from Documents › Files / folders, through Edit for an image or a video;
3. edit inside the app;
4. Ctrl+S, through CraftFrame's save;
5. read the stored object back from `/api/filemanager/direct`.

| App | Edit | Upload | Stored object, checked outside the app |
|---|---|---|---|
| GridCraft | A3 typed | 200 | openpyxl: A3 "Modificato dal cockpit" |
| PdfCraft | page rotated, Save pages, then Ctrl+S | 200, 200 | `pdfinfo` `Page rot: 90` |
| WordCraft | text typed | 200 | `document.xml` ends with " Modificato dal cockpit." |
| DeckCraft | title edited | 200 | python-pptx: 2 slides, "Corpus di prova Aura modificato" |
| PhotoCraft | Image › Invert | 200 | all 65,536 pixels the exact inverse; `dirty` false after the save |
| FilmCraft | clip imported, sequence made; Ctrl+S, export | 200, 200 | `Untitled.fcproj` (FilmCraft 0.14.0 project) and `edited.mp4`, H.264 + AAC, 4.02 s |

In this cockpit PDF and DOCX open straight into their app; only images and video go through Edit.

## The MCP CLIs, as the agent

- **`tools/list`, offline:** each CLI answered `initialize` and `tools/list` with
  `--network none`, with the recipes' own arguments. Three tools are new:
  - `gridcraft list_windows`;
  - `wordcraft list_documents`;
  - `photocraft doc_replace_smart_object`.

  `bridge_risk_craft.go` now classifies them; without that they would have failed closed as
  destructive. `internal/agent/mcptools/testdata/craft/` holds the new versions' real lists
  (DeckCraft's export also gains `rtf`).
- **The sandbox install was wrong for PhotoCraft.** Its 0.17.0 tarball also ships a bash
  completion named `photocraft-cli`, and `find -name | head -1` took it first: a text file
  installed as the CLI (`exec format error`). The Dockerfile now takes the executable match.
  Extracted that way in a container, all six are ELF.
- **Previews:** `TestCraftPreviewNative` (`-tags craft_integration`, race) passed in a bookworm
  box with the six new CLIs, together with both cancellation tests. Nine formats rendered at the
  same dimensions as with the old CLIs; photo.png's preview is 10 bytes larger.
  - A first run in plain `debian:bookworm-slim` failed `ResistantCancellation`: that box has no
    `python3`. It has nothing to do with the CLIs.
- **The six tasks of `web/e2e/craft-live.spec.ts`, Claude as the agent.** Claude made the calls
  itself through each server, following each `*-aura` skill, in a box with no network.

| Server | Task | Checked outside the server |
|---|---|---|
| GridCraft | `fcffginzu.xls` (Damodaran, Eli Lilly, the operator's file): risk-free 4.5% → 5% | B39 0.05, B40 untouched; value per share 377.14 → 333.6193, cached and recalculated by LibreOffice with recalculation forced on load |
| PdfCraft | an ANAC PDF (2 pages): redact "ANAC" | `redact_mark` 2, `redact_apply` 2, audit `count: 0`; `pdftotext` 2 → 0 |
| WordCraft | a bando-like .docx: determina n. 125/2026 del 12 ottobre 2026 | python-docx: the sentence filled, 14 paragraphs and 1 table as before, bold kept |
| DeckCraft | add a closing slide "Domande?" / "Grazie per l'attenzione" | python-pptx: the last slide carries both |
| PhotoCraft | a 1920×1080 Sintel frame with EXIF: resize, brighten, keep EXIF | Pillow: 1280×720, 4 EXIF tags kept, mean luma 44.0 → 51.1 |
| FilmCraft | `sintel_trailer-1080p.mp4`: the first 10 s at 720p, CRF 23 | ffprobe: 10.005 s, H.264 1280×720 + AAC, 1.59 MB (the operator's run: 10.05 s, 1.59 MB) |

What the tasks found is now in the skills:
- **GridCraft.** The output's value sat four columns right of its label. The value before the
  change was recovered by undo and redo.
- **DeckCraft.** A deck whose only layout has no placeholders (`DEFAULT`) drops `add_slide`'s
  title and body with no error, so the slide gets text boxes instead. With standard layouts,
  `titleAndContent` fills both.
- **PhotoCraft.** `command_list` "resize" finds nothing; "size" finds `image.imageSize`.
- **FilmCraft.** A part of a clip is `file.exportMedia`'s `range: "custom"`.

## What this does not establish

- **The Craft image.** `publish-craft-web.yml` could not be dispatched from this session (the
  integration's token answers 403). Until it runs on this branch and `docker/aura/Dockerfile` pins
  the digest it prints, `TestCraftWebImagePinMatchesItsInputs` stays red. The image's bytes were
  not driven here; the local builds of the same inputs were.
- **The sandbox and aura images.** Neither was built here: container HTTPS needs the session
  proxy's CA, which their Dockerfiles do not take. The CLIs ran in a bookworm box made for the
  test, without landrun's fence. CI's sandbox job builds the real image and runs the contract
  test.
- **The real agent through Aura.** The tasks ran with Claude calling the servers directly, not
  through `/agent/run`, `tool_search`, the bridge's policy or `send_file`.
- **The operator's files.** Only `fcffginzu.xls` and the Sintel trailer are the operator's. The
  rest are stand-ins: a 2-page ANAC PDF, a generated bando-like docx, the repository's
  `sample.pptx` and a Sintel frame with EXIF added.
- **Fidelity and other languages.** Round-trip fidelity of complex documents is not claimed, as
  before. Only the languages named above were tried.
