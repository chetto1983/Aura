# Craft apps upgrade: six web builds and six MCP CLIs to storytold's current releases

Date: 2026-10-24. The operator asked for the Craft apps to be upgraded ("aggiorna le app craft"),
and then whether they now speak Italian ("Guarda anche se adesso supportano anche l'italiano").
Run in a cloud dev container (4 CPUs, 15 GB, Docker 29.9.2 started by hand), without the aura
stack.

**Verdict: every app opens, edits and saves a real file through the bridge at its new version,
checked with a tool outside the app. The six CLIs serve the same MCP tools and render the same
native previews. One change in upstream behaviour needed a fix in Aura's patch: PhotoCraft's
unconfirmed writes.**

| App | From | To | Web build | MCP CLI |
|---|---|---|---|---|
| PdfCraft | v0.9.0 | v0.13.0 | patch rebased | `pdfcraft-cli-0.13.0` |
| WordCraft | v0.8.0 | v0.10.0 | patch rebased | `wordcraft-0.10.0` |
| DeckCraft | v0.8.0 | v0.10.0 | patch rebased, one hunk dropped | `deckcraft-0.10.0` |
| PhotoCraft | v0.10.0 | v0.15.1 | patch rebased, one line added | `photocraft-0.15.1` |
| FilmCraft | v0.9.0 | v0.12.0 | patch applies unchanged | `filmcraft-0.12.0` |
| GridCraft | v0.8.0 | v0.11.1 | official zip, unmodified | `gridcraft-0.11.1` |

The pinned versions date from 2026-10-17; the new tags from 2026-10-22 and 2026-10-23.

## The patches

- **The rebase.** `git apply --3way` against both tags left conflicts in four patches, resolved by
  hand:
  - PdfCraft: `i18n/mod.rs`;
  - WordCraft: its web `Cargo.toml`, `main.rs`, `web.rs` and `lib.rs`;
  - DeckCraft: `recovery.rs`;
  - PhotoCraft: its web `Cargo.toml`.
- **Two parts are now upstream:**
  - **DeckCraft's wasm panic.** 0.10.0's `instance_id` hashes the browser clock on wasm, so the
    patch drops its `recovery.rs` hunk. Opening `sample.pptx` logged no panic.
  - **PdfCraft's browser language.** It reads `navigator.languages` itself since #2954. The patch
    keeps only the face filter of decision 8, in front of upstream's choice.
- **WordCraft's merge.** 0.10.0 added stored preferences to the web shell. The bridge field and
  the ready, failed and sync hooks sit beside them.
- **The brand terms.** The `ArtCraft` mentions in the patched trees' Rust source, outside comments
  and tests, are the same lines as in the old patched trees, except one PhotoCraft doc comment. No
  file with `artcraft`, `brand`, `wordmark` or `logo` in its path was added between the tags.

## The builds

- **The pipeline.** `docker/craft-web/build.sh` ran outside Docker with the Dockerfile's tools,
  installed from the same SHA-256-pinned tarballs (trunk 0.21.14, wasm-bindgen 0.2.129,
  wasm-opt 123) and rust 1.97.0. Every new tag declares `rust-version` 1.95 or 1.90, and every
  lock pins the wasm-bindgen crate at 0.2.129.
- **The times.** filmcraft 469 s, deckcraft 593 s, wordcraft 598 s, pdfcraft 568 s,
  photocraft 756 s, sharing the CPUs with other work.
- **GridCraft.** `gridcraft-web-0.11.1.zip` matches the SHA-256 its release's `SHA256SUMS.txt`
  publishes, `b9491325…`. It unpacks to `gridcraft-web-0.11.1/` with `_headers` and no
  `.htaccess`, as `docker/aura/Dockerfile` expects.

## The bridge, app by app

The harness was `spikes/craft-web/` (the `serve` binary and `drive.mjs`), in headless Chromium
with SwiftShader.

| App | Opened | Edited | Saved, checked outside the app |
|---|---|---|---|
| PdfCraft 0.13.0 | `sample.pdf` | Organize pages › rotate › Save pages | `sample.pdf`, `pdfinfo` `Page rot: 90` (0 before); after Ctrl+S alone, byte-identical to the original |
| WordCraft 0.10.0 | `sample.docx` | text typed at the end of the last paragraph | `sample.docx`, valid zip, `document.xml` ends with "Modificato da Aura." |
| DeckCraft 0.10.0 | `sample.pptx` | title edited | `sample.pptx`, valid zip, 2 slides, title "Corpus di prova Aura modificato", 13,148 bytes as 0.8.0's save |
| PhotoCraft 0.15.1 | `photo.png` (960×480) | Image › Invert | `photo.png`, all 460,800 pixels the exact inverse |
| FilmCraft 0.12.0 | `clip.mp4`, imported, sequence made | — | `Untitled.fcproj` (JSON) on Ctrl+S; `edited.mp4` on export, H.264 `avc1` + AAC `mp4a` |
| GridCraft 0.11.1 | `sample.xlsx` | a cell typed | `sample.xlsx`, valid zip, the new text and the original title |

Every app announced its new version in `<app>:ready`, and saved under the file's own name.
`<app>:dirty` went true on the edit and false after the save. The one 404 in every run is the
harness's host page favicon; every run the 2026-10-19 spike recorded has it too.

**PhotoCraft's unconfirmed writes.** With the patch rebased unchanged, the inverted image came
back but `photocraft:dirty` stayed true after the save. 0.15.1 marks a `Services::write`
unconfirmed by default (`write_unconfirmed`, #4962), because a browser download reports nothing
back. A save through it leaves the changes unsaved, so the cockpit's frame would have held the
close of a file it had stored. `Bridge::connect` now sets `write_unconfirmed = false`, since the
parent does take the bytes. Rebuilt with that line (777 s), the same run sent `photocraft:dirty`
false 1 ms after the save, and all 460,800 pixels were the exact inverse.

**The test step that painted.** The first PhotoCraft run left 423 pixels (a 23×24 patch) off the
inverse. 0.15.1 starts on the Brush tool (size 20), and the step that clicked the canvas to focus
it painted a dab. Clicking the empty area beside the image instead gave the exact inverse.

## Italian

| App | Italian catalog, old → new | `it-IT` | `ja-JP,it-IT` | `ja-JP` |
|---|---|---|---|---|
| PdfCraft | `it.tsv`, 2,647 → 2,749 rows | Italian | Italian | English |
| PhotoCraft | `it.tsv`, 4,333 → 4,501 rows | Italian | Italian | English |
| WordCraft | `it.tsv`, 2,643 → 2,801 rows | Italian | not run | not run |
| FilmCraft | `it.tsv`, 3,603 → 3,610 rows | Italian | not run | not run |
| GridCraft | **none → `it.rs`, 961 entries** | Italian | English | not run |
| DeckCraft | none → none | English | not run | not run |

- **GridCraft is new in Italian.** 0.11.1 adds the catalog and follows the browser as released:
  "Salvataggio automatico", "Formule", "Pronto". With `ja-JP` first it stays in English rather
  than falling through to Italian.
- **DeckCraft has no interface catalog at all**, in 0.10.0 as in 0.8.0.

## The MCP CLIs and the native previews

- **The tarballs.** The twelve tarballs (x86_64 and aarch64) match their releases'
  `SHA256SUMS.txt`. Each CLI links only glibc.
- **MCP.** Each answers `initialize` with no network (`unshare --net`), the way the box contract
  runs it. `tools/list`, run against the old and new CLIs with the recipes' own arguments, returns
  the same tool names for every app: gridcraft 21, pdfcraft 17 (`--compact`), wordcraft 16,
  deckcraft 25, photocraft 25, filmcraft 17. So `bridge_risk_craft.go` grades every tool as
  before.
- **Previews.** `TestCraftPreviewNative` (`-tags craft_integration`) ran against a box made of
  `debian:bookworm-slim` (the sandbox image's pinned base) with the six CLIs. It passed with the
  old CLIs and with the new:
  - nine formats rendered, at the same dimensions;
  - six corrupt files refused as invalid;
  - 4.33 s old, 4.74 s new.

## What this does not establish

- **The aura image and the cockpit.** The images were not built here and nothing ran in the cockpit.
  `publish-craft-web.yml` builds the Craft image on GitHub, and its digest is pinned after that
  run. The lab VM pass is the operator's.
- **The sandbox image.** The CLIs ran in a box made for this test, not in `aura-sandbox`. The box
  contract and the native previews run in CI's sandbox job against the real image.
- **Fidelity.** Round-trip fidelity of complex documents is not claimed, as before.
- **Other languages.** Only the languages in the table were tried.
