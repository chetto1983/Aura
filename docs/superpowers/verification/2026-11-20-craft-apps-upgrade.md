# Craft apps upgrade: five releases, DeckCraft official, FilmCraft held

Date: 2026-11-20. The operator asked to check the Craft apps' upstream updates ("Controlla gli
aggiornamenti upstream delle craft App"), then to do the bump starting from the harness ("sì,
procedi con il bump partendo dall'harness"). Run in a cloud dev container (Docker 29.10.0 started
by hand, headless Chromium with SwiftShader).

**Verdict: five apps move to the releases of 2026-11-11 and every one opens, edits and saves a real
file through the bridge, checked with a tool outside the app. DeckCraft's patch is gone: v0.16.0
ships the bridge. FilmCraft stays at v0.17.1: its own bridge arrived in v0.18.0, but from v0.19.0
its web build refuses every H.264 export.**

| App | From | To | Web build | MCP CLI |
|---|---|---|---|---|
| GridCraft | v0.16.1 | v0.19.1 | official zip | `gridcraft-0.19.1` |
| PdfCraft | v0.18.2 | v0.21.1 | official zip | `pdfcraft-cli-0.21.1` |
| WordCraft | v0.15.2 | v0.18.1 | official zip | `wordcraft-0.18.1` |
| DeckCraft | v0.15.1 | v0.18.1 | **official zip, patch dropped** | `deckcraft-0.18.1` |
| PhotoCraft | v0.20.2 | v0.23.1 | patch rebased; translations from craft-i18n | `photocraft-0.23.1` |
| FilmCraft | v0.17.1 | **v0.17.1 (held)** | patched build, unchanged | `filmcraft-0.17.1`, unchanged |

All five new tags are dated 2026-11-11. Every zip and tarball matches its release's
`SHA256SUMS.txt`; the Dockerfiles carry those digests for x86_64 and aarch64.

## Which patches are still needed

- **DeckCraft: no.** v0.16.0 added `apps/deckcraft-web/src/host.rs` on `craft-web-host`
  (storytold/craft-libs), the library GridCraft's, PdfCraft's and WordCraft's bridges use:
  `ready`, `open {name, bytes}`, `save {name, saveAs, bytes}`, `dirty`, `failed`, the five
  messages `CraftFrame.tsx` speaks. Aura's patch no longer applies (it adds a `host.rs` upstream
  already has).
- **FilmCraft: yes, for now.** v0.18.0 added its bridge too, but two things differ from Aura's:
  - **H.264 export is refused from v0.19.0.** `e317437b` ("h264enc: propagate pool-build error")
    turned the encoder's failed rayon pool into an error. On wasm32 a pool cannot be built, so
    `file.exportMedia` answers `encode: invalid configuration: cannot build the 1-thread pool:
    operation not supported on this platform`, with `crf` and without. Measured on the official
    v0.20.1 zip; PNG, WAV, GIF and MJPEG exports still reached the host. Aura's patched build
    would compile the same encoder. On upstream main the encoder has moved to craft-libs
    (`34aa8243`, 2026-11-13, unreleased).
  - **v0.18.0 exports, but opens a clip into the bin only.** Its official zip exported
    `edited.mp4` (H.264 + AAC, 320×180, 4.017 s, ffprobe) through the bridge once a sequence was
    made from the clip, 96 frames in 29 s. Its bridge imports a clip like a dropped file, with no
    sequence; Aura's makes one, so the editor opens on the timeline.
  - The operator chose to hold FilmCraft, web and CLI, at v0.17.1 until a release fixes the
    encoder.
- **PhotoCraft: yes.** v0.23.1 still has no bridge (no `host.rs`), and `detect_system_tags` still
  returns nothing on wasm32.
  - **The rebase.** Three-way from v0.20.2: `Cargo.toml` (upstream's new `ErrorEvent` and
    `PromiseRejectionEvent` web-sys features kept beside the patch's `MessageEvent` and
    `UrlSearchParams`), `main.rs` (the doc line about the new `?free-mb=N` flag kept), and
    `web.rs` (the bridge set up before upstream's new panic and trap hooks). The other six files
    applied as they were.
  - **The translations moved.** From v0.23.0 PhotoCraft's catalogs live in storytold/craft-i18n,
    at the commit `craft-i18n.rev` names, and `crates/ui-egui/build.rs` embeds them from
    `CRAFT_I18N_DIR`. A build without it is English only: measured, the first v0.23.1 build drew
    its interface in English for `it-IT`. `docker/craft-web/build.sh` now fetches craft-i18n at
    that commit when the tag carries the file, sets `CRAFT_I18N_REQUIRED=1` so a missing catalog
    fails the build, and drops the ArtCraft rows from those catalogs as it did from the in-tree
    ones. The commit comes from the checked tag, so the chain of checks is unchanged.
  - The `ArtCraft` mentions in the patched tree's Rust source, outside comments and tests, are
    the same four as in the v0.20.2 patched tree.

## The builds

- **Toolchain.** Every new tag declares `rust-version` 1.95 and locks wasm-bindgen 0.2.129, the
  Craft image's. `cargo check --locked -p photocraft-web --target wasm32-unknown-unknown` on the
  patched tree passed with no warning from a patched file.
- **PhotoCraft.** `docker/craft-web/build.sh` ran outside Docker with the Dockerfile's
  SHA-256-pinned trunk 0.21.14, wasm-bindgen 0.2.129 and wasm-opt 123, rust 1.97.0.
- **The Craft image** now builds PhotoCraft and FilmCraft only. Its inputs changed, so its tag is
  now `7fd07aa96dd103c5`; `publish-craft-web.yml` must publish it before
  `TestCraftWebImagePinMatchesItsInputs` turns green and `docker/aura/Dockerfile` can pin it.

## The bridge, app by app

Harness `spikes/craft-web/` (`serve` through `internal/staticapp`, `drive.mjs`). The official zips
were unpacked as `docker/aura/Dockerfile` does: `_headers` and `.htaccess` removed, wasm and glue
gzipped. No run logged a Content-Security-Policy violation.

| App | Opened | Edited | Saved, checked outside the app |
|---|---|---|---|
| GridCraft 0.19.1 | `sample.xlsx` | a cell typed under the table | `sample.xlsx` (`saveAs: false`), openpyxl: A11 "Modificato da Aura", A1 and B6 kept |
| PdfCraft 0.21.1 | `Appunti.pdf` | Organize pages › rotate › Save pages, then Ctrl+S | `Appunti.pdf` twice, `pdfinfo` `Page 1 rot: 90` (0 before) |
| WordCraft 0.18.1 | `sample.docx` | text typed after the last paragraph | `sample.docx`, python-docx: last paragraph ends "Modificato da Aura.", 3 paragraphs as before |
| DeckCraft 0.18.1, official | `sample.pptx` | title edited | `sample.pptx` (`saveAs: false`), python-pptx: 2 slides, title "Corpus di prova Aura modificato", body kept |
| PhotoCraft 0.23.1, patched | `photo.png` (256×256) | Background layer selected, Ctrl+I | `photo.png`, all 65,536 pixels the exact inverse; `dirty` true, then false after the save |

Every app announced its new version in `<app>:ready`.
- **DeckCraft's** `ready` also carries `version: "1"` and `file: false`, as the other official
  bridges do.
- **PhotoCraft 0.23.1, a harness step, not a regression of the app.** The old step clicked the
  canvas before Ctrl+I. With Auto-Select on, a click on empty canvas leaves no layer active
  ("Invert is not available: no active layer"); after the open the Background layer is active.

**PhotoCraft's language.** The build with the catalogs drew its interface in Italian for `it-IT`
(File, Modifica, Immagine, Livello; "Aperto: photo.png" in the status bar) and in Japanese for
`ja-JP` (ファイル(F), 編集(E); "photo.png を開きました"). The same steps on it inverted all 65,536
pixels again. Its wasm is 39.2 MB against 31.9 MB for the English-only build of the same tag; the
build took 1,023 s here.

## The MCP CLIs

- **`tools/list`, offline.** Each new CLI answered `initialize` and `tools/list` under
  `unshare -rn`, with the recipes' arguments. One tool is new: **WordCraft `insert_table`**, a
  filled table at the caret. `bridge_risk_craft.go` now classifies it as a write; without that it
  failed closed (`TestCraftWritesAreReversibleWhateverTheServerDeclares`: "wordcraft:
  insert_table is destructive"). `internal/agent/mcptools/testdata/craft/` holds the new lists:
  - GridCraft and DeckCraft: unchanged;
  - PdfCraft: `text_extract` gains `format`, `text_find` gains `stop_at_limit`;
  - PhotoCraft: `doc_export` and `doc_save` gain `pdfSpotPlates`, `doc_export_layers` gains
    `maximizeCompatibility`; four tools changed their annotations, which the Craft table
    ignores.
- **Previews.** `TestCraftPreviewNative` (`-tags craft_integration`, race) passed in a
  `python:3.12-slim-bookworm` box with no network, the five new CLIs and FilmCraft 0.17.1's:
  nine formats rendered (notes.md, sample.xlsx, sample.pptx, photo.png, letter.rtf, table.csv,
  sample.pdf, sample.docx, synthetic.mp4), with both cancellation tests.
- **The weekly gate.** `scripts/security_scan/craft_osv.sh scan` (OSV-Scanner 2.7.0, rust 1.97.0)
  over the new pins: every app `ok`, no fixable finding at CVSS 7.0 or more. zip 3.0.0 is still in
  the wasm32 graphs of GridCraft, WordCraft and DeckCraft; none of the three calls `extract()` at
  the new tags (`ZipArchive::new` 54, 130 and 77 sites). Reported, below the gate: zip's
  RUSTSEC-2026-0545 and GHSA-8hxf-qr8p-62x4 (CVSS 6.5, fixed in 9.0.0), RUSTSEC-2026-0368;
  chrono's RUSTSEC-2026-0543 in all six; PdfCraft's rsa 0.10.0-rc.18 (RUSTSEC-2023-0071, 5.9).

## What this does not establish

- **The cockpit and the agent.** Neither `aura serve` with the new builds nor
  `web/e2e/craft-live.spec.ts` was run; the bridge harness speaks `CraftFrame`'s protocol, and
  the CLIs were checked by `tools/list` and the native previews only. The skills were not
  re-measured against the new servers.
- **The images.** Neither the aura image, the sandbox image nor the Craft image was built here.
  The Craft image's published bytes have not been driven; the last bump found the runner's wasm
  differs from a local build of the same inputs.
- **FilmCraft beyond the export.** Only v0.18.0's and v0.20.1's official zips were driven, for
  open, save and export; the hold rests on the H.264 refusal.
- **Fidelity and other languages.** Round-trip fidelity of complex documents is not claimed. Only
  the languages named above were tried.
