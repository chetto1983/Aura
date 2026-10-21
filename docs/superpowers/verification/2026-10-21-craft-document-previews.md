# Native Craft preview measurements

Date: 2026-10-21. Trigger: the user required reuse of existing Craft apps, including Office files,
and rejected a separate renderer. The proposed renderer was stopped before production code
existed; its sole untracked API test was preserved in the task's ignored scratch directory.

## Inventory

Read the pinned Craft specs, local public tool/command inventories and the deployed CLI help.
Existing Aura already owns `CraftFrame`, `EditInCraft`, `PreviewByKind`, identity-scoped sandbox
routing, streamed file staging and artifact copy-out. The native apps provide actual rendering.

- PdfCraft: native `run --script` can open a PDF then call `render_preview` with `max_side:480`.
  Its native batch script writes the PNG using `out`; no MCP protocol implementation is needed.
- WordCraft: `convert <source> <stage.pdf>`, then PdfCraft for bounded first-page rendering.
- GridCraft: native JSONL `sheet.activate` sheet0, then `file.exportPdf` of the active sheet with
  `fitToPage:true`, then PdfCraft. The tested workbook produced one page without an arbitrary range crop.
- DeckCraft: native `run --in <source> --export <stage.pdf>` with export parameters selecting slide0
  and including hidden slides, then PdfCraft.
- PhotoCraft: native isolated `doc_open` and bounded `render_preview`; its `serve` protocol also
  exposes `doc.open` and `doc.render` with a PNG output path for the production orchestration.

Every probe used an independent native process and a unique directory below the existing
identity box's `/workspace/scratch`. It did not use or change the agent's mounted editor session.
Input/output scratch files were removed after copying the PNGs out. No runtime image was rebuilt.

## Live outputs

| Source | Native path | Export / render observed | Output |
|---|---|---|---|
| Synthetic contract PDF | PdfCraft `doc_open` → `render_preview(max_side480)` via existing spike MCP helper | render15ms | 13,802byte PNG |
| Public ANAC DOCX | WordCraft convert → PdfCraft native batch |435ms /119ms |24,903byte PNG |
| Public EU-OSHA PPTX | DeckCraft first-slide PDF export → PdfCraft |399ms /130ms |47,905byte PNG |
| Public FCFF XLS | GridCraft first sheet fit-to-page PDF → PdfCraft |111ms /109ms |34,839byte PNG; PDF info reports one page |
| Public19MP Ponte Vecchio JPEG | PhotoCraft open → bounded render_preview | render92ms |310,349byte PNG |

The production-friendly PhotoCraft `serve` path was also executed separately: native JSON lines
`doc.open {path}` then `doc.render {maxSide:480,path}` produced the same310,349byte PNG in860ms
including Docker command/copy overhead. Its open response reported4948×3786 pixels and one layer;
both responses had `ok:true`. This proves that no newly written image decoder/resizer is needed.

All PNGs were opened and visually inspected: recognizable source content, no blank render,
long edge480px. Timings are single local observations through different native/CLI paths,
not a comparative benchmark or latency guarantee. Source files are the existing public
`D:/tmp/craft-real` fixtures except the generated synthetic PDF.

Artifacts: `output/playwright/paperless-comparison/craft-preview-probe.json` and
`craft-native-preview.png`, `craft-word-preview.png`, `craft-grid-preview.png`,
`craft-deck-preview.png`, `craft-photo-preview.png`. Probe scripts are in this task's ignored
`.superpowers/sdd/2026-10-21-paperless-documents/` directory.

## Ingestion inventory

The user explicitly chose **keep extraction; replace UI/rendering paths**.
`services/ingest` remains the existing extraction/reconciliation pipeline:

- `source.py` preserves original filename metadata; it reads all non-reserved source objects.
- `extract.py` normalizes Office formats and extracts text with existing tools.
- `media.py` routes image, audio, video and spreadsheet cases deliberately; video is metadata-only,
  audio transcribed, image text depends on the configured vision route, spreadsheets use row passages.
- `source_extensions.py` reads the canonical Go source-language inventory from
  `aura-filecard -source-extensions`; no duplicate code-language registry is needed.
- `app.py` publishes per-identity CocoIndex status through `arcade.record_status`.
- `internal/arcadedb/document_ingest_state.go` already reads status/observed_at/in_progress/finished/errors.
  These counts refer to work components, not files. Aggregate errors cannot identify which pending
  upload failed, and `ready` can coexist with errors.

This inventory justifies reusing the status reader in the new Processing UI and grouping file
families at the library boundary. It does not justify removing media/code from ingestion.

## Limits

Only the listed fixtures were rendered. Other documented Office imports still need regression
fixtures. Word conversion exports all pages before selecting the first preview; spreadsheet fit
to page is an overview of the first sheet. Font substitution and import fidelity remain native
Craft constraints. Unsupported formats must have an honest fallback, not a hidden old renderer.
The production HTTP/sandbox orchestration is still to be implemented and tested.
