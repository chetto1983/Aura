# Craft apps upgrade: six releases, three patches left

Date: 2026-10-29. The operator asked to check the Craft apps for updates and new pins ("Guarda le
craft app se hanno aggiornamenti e nuovi i pin"), then to do both bumps and "guarda se le patch
servono ancora", then to run the E2E with Docker, with Claude as the agent ("LLM sei te"), and to
update the skills. Run in a cloud dev container (4 CPUs, 15 GB, Docker 29.9.2 started by hand).

**Verdict: every app opens, edits and saves a real file at its new version, through the bridge
and through the live cockpit, checked with a tool outside the app. The six agent tasks pass on
the new MCP CLIs, and again through Aura itself (`craft-live.spec.ts`, Claude as the model). Two patches are gone (WordCraft, PdfCraft): what they did is upstream now.**

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
- **Published image:** `publish-craft-web.yml` run 40931924616, dispatched by the operator on this
  branch, took 36 min on a cold cache. It pushed
  `aura-craft-web:e4b0fdbdef68821b@sha256:0374d04f…95d1`, which `docker/aura/Dockerfile` now pins.
  Its JS glue is byte-identical to the local builds; its three wasm files are not. The published
  bytes were therefore driven on their own (below).

## The bridge, app by app

Harness `spikes/craft-web/` (`serve` through `internal/staticapp`, `drive.mjs`), headless
Chromium 1243 with SwiftShader.

| App | Opened | Edited | Saved, checked outside the app |
|---|---|---|---|
| GridCraft 0.13.0 | `sample.xlsx` | A3 typed | `sample.xlsx`, openpyxl: A3 "Modificato da Aura", A1:B2 kept |
| PdfCraft 0.15.0 (official and patched) | `Appunti.pdf` | Organize pages › rotate › Save pages | `Appunti.pdf`, `pdfinfo` `Page rot: 90` (0 before) |
| WordCraft 0.12.0 | `sample.docx` | text typed after the last paragraph | `sample.docx` (`saveAs: false`), `document.xml` ends with it |
| DeckCraft 0.12.0 | `sample.pptx` | title edited | `sample.pptx`, 2 slides, title "Corpus di prova Aura modificato", 13,148 bytes as before |
| PhotoCraft 0.17.0 | `photo.png` (256×256) | Image › Invert | `photo.png`, all 65,536 pixels the exact inverse; `dirty` false 2 ms after the save |
| FilmCraft 0.14.0 | `clip.mp4`, imported, sequence made | — | `Untitled.fcproj` (FilmCraft 0.14.0 project) on Ctrl+S; `edited.mp4` on export, H.264 + AAC, 4.02 s |

The published image's three apps, pulled by digest and served by the same harness with the same
steps, did the same: PhotoCraft 65,536 of 65,536 pixels inverted; DeckCraft 2 slides, the edited
title, 13,148 bytes; FilmCraft `Untitled.fcproj` (FilmCraft 0.14.0) and `edited.mp4`, H.264 + AAC,
4.017 s.

Every app announced its new version in `<app>:ready`. Each sent `<app>:dirty` true on the edit
and false after the save. The one 404 in every run is the harness page's favicon. PhotoCraft
also asks for `fonts/manifest.txt`, the optional served-font list its web build reads, which Aura
does not ship (404).

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

## Through Aura, with Claude as the model

The operator then asked for the six tasks to go through Aura itself, with Claude as the LLM
behind a synthetic llama.cpp endpoint ("LLM ci sei te come endpoint usi Llama.cpp sintetico").
- **The endpoint.** A local OpenAI-compatible server on `127.0.0.1:8084/v1` (`/models`, `/props`,
  streaming `/chat/completions`). It wrote each request to a file and streamed back the answer
  Claude wrote, as text or tool calls.
- **Aura.** `aura serve` (this branch) ran with `AURA_LLM_PROVIDER=llamacpp`, over Postgres,
  Garage and ArcadeDB 26.10.2 in Docker. The turn's wallclock and stream timeouts were raised for
  a model that answers by hand.
- **The images.** `aura-sandbox:latest` and `aura-egress:latest` were built from their Dockerfiles
  with the session proxy's CA added to a local copy; the files in the repository are unchanged.
  `scripts/sandbox_image_contract_test.sh` passed on that image: "the six Craft MCP servers
  initialize offline" and "landrun fences a Craft server to /workspace".
- **The run.** The six recipes were installed with `aura mcp install <app>`. `web/e2e/craft-live.spec.ts`
  then ran unchanged, one case at a time: the file attached through presign, PUT and finalize, one
  turn on `/agent/run`, and the result fetched from Garage. Each turn loaded the server's skill,
  called `document_open`, loaded the server's tools with `tool_search`, worked in the identity's
  box behind landrun, and finished with `send_file`.

| Case | Turn | Calls (Craft) | Checked outside the server, on the file `send_file` delivered |
|---|---|---|---|
| GridCraft | 65 s | 13 (9) | openpyxl: B39 0.05, B40 untouched, F71 333.6193 (377.14 before) |
| PdfCraft | 42 s | 10 (6) | `pdftotext`: 0 "ANAC" (2 before), 2 pages; audit `count: 0` |
| WordCraft | 46 s | 11 (7) | python-docx: "n. 125/2026 del 12 ottobre 2026", 14 paragraphs, 1 table, bold kept |
| DeckCraft | 45 s | 14 (10) | python-pptx: 3 slides, the last "Domande?" / "Grazie per l'attenzione" |
| PhotoCraft | 52 s | 8 (4) | Pillow: 2400×1350, 4 EXIF tags kept, mean luma 44.1 → 51.1 |
| FilmCraft | 37 s | 7 (3) | ffprobe: 10.005 s, H.264 1280×720 + AAC, 1,588,983 bytes |

Every case passed the spec's own checks:
- no RUN_ERROR;
- a `<server>__` call and a `send_file` in the turn;
- a new asset of the right kind;
- the conversation, with the delivered file, in the cockpit.

The times are a hand-written model's, not Aura's.

What the run through Aura found:
- **`document_open` needs ArcadeDB.** Opening an attachment by its asset id goes through the
  document index, which needs the identity's ArcadeDB credential: without ArcadeDB the call failed
  with "AURA_ARCADEDB_TENANT_SECRET is required". Production always runs it; the first stack here
  did not.
- **An MCP image reaches the model only through `read_file`.** The bridge writes an MCP image to
  `/workspace/mcp-files/…` and puts only its path in the result. `read_file` on that path attached
  the PNG to the next request as an image part. Measured on WordCraft's `render_page` and
  DeckCraft's `render_slide`, in a second DeckCraft turn (35 s, 12 calls). The skills now say so
  where they ask to look at a render.
- **DeckCraft's placeholder-less deck.** The case of the skill's new note came up inside Aura
  too: `add_slide` on the `DEFAULT` layout left 0 shapes, and the text went in with
  `add_text_box`.
- **A tool-less request.** Aura also asked the model for retrieval text for the attached photo,
  a request with no tools. It was answered by mistake with the agent's tool calls, so that photo's
  index text is wrong; the turn itself was not affected.

## What this does not establish

- **The Craft image in the cockpit.** The published image went through the bridge harness only;
  the live cockpit and the agent runs used the local builds. Its wasm files differ from the local
  builds of the same inputs, so the build is not bit-for-bit reproducible between those two
  environments (the runner's Docker build and this container's build outside Docker).
- **The aura image.** It was not built: `aura serve` ran as a binary of this branch, with the six
  apps from `AURA_APPS_DIR`. The sandbox image was built only with the proxy CA added, so CI's
  sandbox job is still the build of record.
- **A real model.** Claude answered every model call through Aura. A local or hosted model may
  choose other calls; the skills are what steer it.
- **The operator's files.** Only `fcffginzu.xls` and the Sintel trailer are the operator's. The
  rest are stand-ins: a 2-page ANAC PDF, a generated bando-like docx, the repository's
  `sample.pptx` and a Sintel frame with EXIF added.
- **Fidelity and other languages.** Round-trip fidelity of complex documents is not claimed, as
  before. Only the languages named above were tried.
