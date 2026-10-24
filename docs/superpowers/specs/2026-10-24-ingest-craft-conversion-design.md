# Ingest: the Craft CLIs convert the legacy formats, and LibreOffice leaves aura-ingest

**The operator asked** on 2026-10-24: can the ingest use the Craft apps and slim its container?
**The operator decided** the same day: write the spec. **Still open:** what happens to `.ppt`
and `.odp` (D3). Nothing here is implemented yet.

## What exists, read in the tree on 2026-10-24 at `846ee75`

- **LibreOffice does one job in `aura-ingest`.** `services/ingest/extract.py` `normalise()`
  runs `soffice --headless --convert-to <target>` with a 300 s timeout. `_LEGACY` maps seven
  formats to OOXML:
  - `.doc`, `.odt` and `.rtf` to docx;
  - `.xls` and `.ods` to xlsx;
  - `.ppt` and `.odp` to pptx.

  `prepared()` converts once. Tika reads the text from the result, `aura-filecard` builds the
  card, and for a workbook filecard also builds the row passages (`rows.py`). poppler-utils
  stays either way: it belongs to filecard's PDF reader, not to LibreOffice.
- **Its cost.** `docker/aura-ingest/Dockerfile` installs `libreoffice-writer`,
  `libreoffice-calc` and `libreoffice-impress`. In the `aura-ingest:edge` of 2026-10-24 that
  layer is 474 MB of 1.42 GB. It is also the largest parser of untrusted files in the
  container.
- **The CLIs are already pinned elsewhere.** `docker/aura-sandbox/Dockerfile` installs the
  official release tarballs, each pinned by the SHA-256 its release publishes, amd64 and arm64:
  gridcraft 0.11.1, wordcraft 0.10.0 and deckcraft 0.10.0. The Craft MCP servers run them.
- **What else reads LibreOffice:**
  - `scripts/ingest_image_contract_test.sh` asserts that `soffice` is in the image;
  - `scripts/extractor_matrix_test.sh` calls it;
  - `scripts/make_document_fixtures.sh` uses it to write the legacy fixtures from the OOXML
    masters, inside `aura-ingest:local`;
  - the comment in `internal/documents/extensions.go` says `document_open` hands the original
    to "a box that has soffice". It is stale: the sandbox dropped LibreOffice for the Craft
    servers (2026-10-18).

## Measured on 2026-10-24

Run inside the real `aura-ingest:edge`, on the fixtures in
`scripts/fixtures/document_pipeline_e2e/`. The card was built with `-name sample.<xlsx|docx>`,
the converted extension, as `_card_name` does.

| Format | Converter | filecard card against soffice's | Tika text |
|---|---|---|---|
| `.xls`, `.ods` | `gridcraft-cli convert <in> <out>` | the same, banner and numeric cells included; only the KB figure differs | identical |
| `.doc`, `.odt`, `.rtf` | `wordcraft-cli convert <in> <out>` | the same; only the KB figure differs | identical |
| `.ppt`, `.odp` | `deckcraft-cli` | refused: "not a presentation DeckCraft can read" | — |

- **The CLIs' warnings.** gridcraft prints them on stderr and still writes the file:
  - for `.xls`: fonts, fills, borders, charts, pictures and comments are not imported;
  - for `.ods`: formulas become their saved results.

  Values are all that the card and the text read.
- **Size.** `gridcraft-cli` is 12.9 MB and `wordcraft-cli` 77.4 MB, against the 474 MB layer.
- **Without conversion.** Tika reads `.ppt` and `.odp` directly: 236 and 232 characters,
  against 250 from the converted pptx. filecard routes only OOXML, so the card falls back to
  the bare `name — file, N KB` line, without the slide count or the title.

**What this does not show:**
- one file per format, all of them synthetic: no real corpus, no charts or pictures, no macros,
  no password-protected or damaged files;
- the row passages of a converted workbook (`ingest_reconcile_e2e.sh` checks those for
  `order.xls`);
- time, memory, arm64;
- how the CLIs fail on hostile input.

## Decisions

**D1. gridcraft-cli and wordcraft-cli replace soffice for five formats.**
- `.xls` and `.ods` go through `gridcraft-cli convert`.
- `.doc`, `.odt` and `.rtf` go through `wordcraft-cli convert`.

`normalise()` keeps its contract:
- it returns the converted path, with the stem kept;
- it raises when the CLI exits non-zero or writes nothing, and the error carries the CLI's
  stderr;
- the 300 s timeout stays.

A warning on stderr with exit 0 is not a failure.

**D2. One pin for both images.** The install function now in the sandbox Dockerfile moves into a
script, `docker/craft-cli/install.sh`, with the version and SHA-256 table. The sandbox runs it
for six apps and the ingest image for two. A second copy of the pins would drift, as the
postgres tag did (security-scan spec, D1).

**D3. `.ppt` and `.odp`: the operator's choice.**
- **A. Keep LibreOffice for these two only.** `libreoffice-impress` pulls `libreoffice-core`,
  so most of the 474 MB would stay. That size is an estimate; measure it before choosing A.
- **B. Stop converting them.** Tika keeps reading their text, and the name and text stay
  searchable. The card loses the slide count and the title. LibreOffice leaves the image
  entirely.
- **C. Keep LibreOffice until DeckCraft imports them.** That needs an issue upstream, at
  storytold/deckcraft.

**Recommendation: B, plus C's upstream issue.** The loss is two lines of a card for two formats
that rarely arrive. When DeckCraft learns to read them, they return to `_LEGACY` with one line.

**D4. The fixtures stop depending on the ingest image.** `make_document_fixtures.sh` writes
`.doc`, `.xls` and `.ppt`, and only LibreOffice writes those formats; the CLIs refuse to save
them. The script runs LibreOffice from a disposable container pinned by digest, outside
`aura-ingest`. The fixtures are committed, so this runs only when a master changes.

## Order of landing

1. **Measure first, on more than one file per format.** Gather a corpus of at least ten real
   files per legacy format from the lab VM's bucket, plus the existing fixtures. For each one,
   compare:
   - card, text and row passages against soffice;
   - time and peak memory;
   - the exit code on a truncated copy.

   Record the run in `docs/superpowers/verification/<date>-ingest-craft-conversion.md`. A
   format whose card or rows differ beyond the KB figure stays on LibreOffice, and this spec
   is amended.
2. **The shared installer (D2)**, with the sandbox moved onto it. The sandbox image contract
   test must pass unchanged.
3. **`normalise()` on the CLIs (D1)**, with `services/ingest/tests/test_extract.py` covering:
   - each format;
   - a CLI failure;
   - a missing output file.

   The contract test asserts `gridcraft-cli` and `wordcraft-cli` instead of `soffice`, and the
   extractor matrix calls them.
4. **D3 as decided, and D4.** The Dockerfile drops LibreOffice, `SAL_USE_VCLPLUGIN` and
   `fonts-dejavu-core`, unless D3 keeps impress. Check first that nothing else renders with that
   font.
5. **The E2E on the lab VM.** `make ingest-reconcile` and the document pipeline E2E pass on the
   new image, the image size is recorded, and the stale comment in `extensions.go` is fixed in
   the same change.

## What this does not cover

- **The aura image.** It carries no LibreOffice. Its poppler and fonts serve the Go code.
- **The Craft MCP servers in the sandbox.** They are unchanged, apart from D2's move.
- **Sandboxing the CLIs in ingest.** soffice ran with no fence there, and the CLIs will too. The
  sandbox runs them under landrun; giving ingest the same fence is a separate decision.
