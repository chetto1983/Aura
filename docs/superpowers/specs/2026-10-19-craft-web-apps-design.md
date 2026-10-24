# The Craft apps edit files in the cockpit

Date: 2026-10-19. The operator's direction: *"La parte di photo e video craft ha la Wasm da mettere sulla
UI di Aura?"*, *"Guarda tutte le App che usiamo come MCP oggi"*, then *"procedi con lo spike su PdfCraft e
tutte le modifiche necessarie per le altre non supportate a oggi con la modifica ed il salvataggio"*; later
*"Esistono le tradizioni in italiano delle App?"* and, on the answer, *"Procedi pure"*.
Projects 2, 3 and 4 of `docs/superpowers/specs/2026-10-18-craft-mcp-design.md`: FilmCraft as the cockpit's
video editor, one pipeline for patched Craft web builds, PhotoCraft/WordCraft/DeckCraft in the cockpit.

Evidence: `spikes/craft-web/FINDINGS.md` (verdicts 1–15, every one measured on a real file through
aura's own static-app handler).

## What changes

1. **The six Craft apps the agent drives as MCP servers are also the cockpit's editors.** GridCraft
   (xlsx, already), PdfCraft, WordCraft, DeckCraft, PhotoCraft, FilmCraft: aura serves each web build at
   `/apps/<app>/` and frames it with `?host=parent`; the file goes in as bytes, and every save comes back
   to the file manager's source, over the file under its own name or beside it under another.
2. **Spreadsheets and decks open in their app; documents, images and video keep a light viewer.**
   - `xlsx` → GridCraft (as before), `pptx` → DeckCraft (no viewer existed: a deck was download-only).
   - `pdf`, `docx`, `image`, `video` keep the browser's PDF viewer, docx-preview, `<img>` and the
     streaming player; an **Edit** button swaps the app in (PdfCraft, WordCraft, PhotoCraft, FilmCraft).
3. **Five patched builds.** WordCraft, DeckCraft, PhotoCraft and FilmCraft have no bridge upstream
   (FINDINGS 2), and PdfCraft and PhotoCraft draw their web interface in English whatever the browser's
   language (FINDINGS 13): aura builds each from its release tag with `docker/craft-web/<app>.patch`.
   Only GridCraft ships as released.
4. **`AURA_GRIDCRAFT_DIR` becomes `AURA_APPS_DIR`**, a folder with one folder per app
   (`/usr/share/aura/apps` in the image).

## Decisions

1. **The bridge is GridCraft's protocol, per app prefix.** `<app>:ready {version}`, `<app>:open {name,
   bytes}` (to the app), `<app>:save {name, bytes}`, `<app>:dirty {dirty}`, `<app>:failed {error}`; only
   the parent window on the page's own origin is heard or answered. PdfCraft's official build already
   speaks it (FINDINGS 3); the patches add a `host.rs` to the other four web crates, hooked where each app
   turns a save into a download (FINDINGS 4):
   - WordCraft: `Services::download`; an untitled document takes the name it was saved under.
   - DeckCraft: `Services::download`; a deck sent as `.pptx` comes back as `.pptx` (DeckCraft saves its own
     `.deckcraft`), and the deck counts as saved when the parent took it.
   - PhotoCraft: `Services::write`; the image opens under its own name (`open_file`), so Save writes back
     to it instead of going through Save As to `.psd`.
     - **Amended 2026-10-24: the bridge marks its writes confirmed.** PhotoCraft 0.15.1 counts a
       `Services::write` as unconfirmed by default (`write_unconfirmed`, #4962), because a browser
       download reports nothing back. The save then leaves the changes unsaved. Measured with the
       patch rebased unchanged: the image came back inverted, and `photocraft:dirty` stayed true,
       so the cockpit would have held the close of a file it had stored. The parent does take the
       bytes, so `Bridge::connect` sets `write_unconfirmed = false`.
   - FilmCraft: `fs::set_on_write`; a clip sent is imported and a sequence made from it; the project
     (`.fcproj`) and every export come back. Host mode starts with no demo and nothing an earlier visit
     left in OPFS.
2. **One upstream bug is fixed in the patch.** DeckCraft 0.8.0's official web build panics at its first
   document (`std::process::id()` on wasm, FINDINGS 6); the patch gates that call off wasm, as the line
   beside it already gates the clock.
   - **Amended 2026-10-24: upstream fixed it.** DeckCraft 0.10.0's `instance_id` hashes the
     browser clock on wasm, and `process::id` and the system clock only off wasm, so the patch drops
     the hunk.
3. **A modified build carries no ArtCraft marks** (FINDINGS 9): the brand terms let only unmodified
   official builds use them. The patches remove FilmCraft's compiled wordmark and reword the About texts
   to the allowed "based on <App> by the ArtCraft team"; the build script drops the translated rows that
   name ArtCraft. Copyright notices and the MIT/Apache licenses stay and ship beside each build.
   GridCraft ships unmodified, as released; PdfCraft, patched for its language, loses Home's community
   card and About's mark and wordmark (FINDINGS 14).
4. **The patched builds are made in the image, from source** (`docker/aura/Dockerfile`, stage
   `craft-web`), on the landrun pattern of the box image: the tag is cloned and checked against its
   commit, the patch applied, and Trunk runs `--offline` with wasm-bindgen 0.2.129 and wasm-opt 123
   from release tarballs pinned by SHA-256 per architecture. WebAssembly is the same everywhere, so the
   stage runs once on `$BUILDPLATFORM`. Cost: about 50 minutes of cold build (FINDINGS 10), cached by
   the release workflow's buildx cache afterwards.
   - **Amended 2026-10-21: the builds leave the aura image.** Measured on GitHub's runner: every
     build without a warm cache paid them again, about 35 minutes (pdfcraft 498 s, wordcraft 388 s,
     deckcraft 419 s, photocraft 469 s, filmcraft 295 s on 49dcf01), which held the Agent Memory job
     at 42 minutes for a 3-minute evaluation and cut it off at a 30-minute cap. The same stage now
     lives in `docker/aura-craft-web/Dockerfile`, `publish-craft-web.yml` publishes it as
     `ghcr.io/chetto1983/aura-craft-web` tagged with the hash of its inputs
     (`scripts/craft_web_inputs.sh`), and the aura image copies it by tag and digest.
     `TestCraftWebImagePinMatchesItsInputs` fails when `docker/craft-web/` changes and the pin does
     not follow. Not shown by this: how long a cold build of the remaining aura image takes; the
     acceptance below still reads the apps from `/usr/share/aura/apps`, which this does not move.
   - No craft-fonts, as in the official web builds (FINDINGS 12): their `release.yml` names a
     craft-fonts commit for the desktop builds, but the published wasm carries none of its faces.
5. **Edit is offered only where a save is stored.** The file manager's source (and the Documenti
   detail) has `save`; the chat's artifacts, the Studio and the share page do not, and there the viewer
   stays alone: an edit that cannot be stored could only be lost. GridCraft and DeckCraft, which are the
   preview, keep the existing notice that changes made there are not stored.
6. **A file is cached for good only when its URL names its content** (FINDINGS 11): a content hash in
   the name, or a `?v=` version in the query; anything else revalidates.
7. **One component frames every app** (`web/src/chat/artifacts/renderers/CraftFrame.tsx`, from
   `XlsxPreview`), with `EditInCraft` around the viewers.
8. **The apps start in the browser's language.** PdfCraft's and PhotoCraft's patches read
   `navigator.languages`, as WordCraft and FilmCraft already follow the browser, and take the first
   language the app has and can draw: the web builds have no Japanese, Chinese, Korean, Arabic or Telugu
   face, so those are skipped for the next language, else English, rather than drawn as boxes (FINDINGS
   14), which is what FilmCraft already does (FINDINGS 15). The cockpit's own language is not passed:
   the bridge has no field for it.
   - **Amended 2026-10-24: PdfCraft reads the browser itself.** Since #2954, PdfCraft 0.13.0 takes
     the first of `navigator.languages`, then `navigator.language`, that it has a catalog for. It
     does not ask whether it can draw that language. The patch keeps only that check: tags with no
     face in the web build are dropped before upstream's choice.

## What this does not do

- **No editing in the chat's artifacts, the Studio or the share page**: they have no source to save to.
- **No `.ppt`**: DeckCraft reads `.pptx`; a `.ppt` stays download-only.
- **A FilmCraft project in the library is not reopened**: `.fcproj` has no preview, and its media are
  FilmCraft's own in-page files. Ctrl+S stores it beside the clip (as `Untitled.fcproj`); the export is
  what carries the edit.
- **No save over 100 MiB.** A save goes through the file manager's upload route, capped at
  `config.DefaultAssetMaxDocumentBytes`; a longer FilmCraft export is refused and the frame says so. The
  export is also held whole in the page's memory while it is handed over.
- **No upstream contribution here.** The bridge and the DeckCraft fix would serve upstream as they serve
  aura; proposing them is the operator's call.
- **Round-trip fidelity of complex documents is not claimed** (FINDINGS, "does not show").
- **No Italian in GridCraft and DeckCraft**: neither has a catalog (FINDINGS 13). In the four others the
  few rows the patches reword (About, Help's links) have no translation and show in English.
  *Amended 2026-10-24:* GridCraft 0.11.1 has an Italian catalog and follows an `it-IT` browser;
  DeckCraft 0.10.0 still has none.
- **WordCraft is left as released on languages it cannot draw**: with a Japanese browser it draws boxes,
  as its official web build does (FINDINGS 15).

## Acceptance

1. The aura image builds with the five patched builds and GridCraft's official one under
   `/usr/share/aura/apps`; the image size change is measured and recorded.
2. In the file manager, each kind opens: xlsx and pptx in their app; pdf, docx, an image and a clip in
   their viewer with Edit; an edit saved in each app lands in the library over the file (or beside it for
   another name), checked with a tool outside the app.
3. Outside the file manager no Edit appears, and the share page still previews every kind.
4. With the browser in Italian, PdfCraft, WordCraft, PhotoCraft and FilmCraft open in Italian.
5. Gates: `make quality`, the web gates (lint, typecheck, format, tests, knip), the coverage policy, and
   `internal/webui/dist` rebuilt.
