# Roadmap

## Completed Milestones

### v1.2 Source Intake Closure

Status: closed

Closed on 2026-05-06 with truthful upload support for PDF, TXT, Markdown, JSON, CSV, XLSX, and DOCX across Telegram, API, source storage, extraction, wiki ingestion handoff, and dashboard copy.

Key outcomes:

- Shared upload-format policy across source, API, Telegram, and React UI.
- Normalized extraction evidence through `extract.md` and `extract.json`.
- PDF OCR adapted into the same evidence contract.
- XLSX extraction wired through Pyodide with mounted input files.
- DOCX extraction wired through Pyodide with bounded ZIP/XML/text limits.
- Extracted source ingestion closes the non-PDF path into wiki pages.
- Dashboard polish and E2E closure for source inbox, graph, conversations, settings, and adjacent panels.

## Next Candidates

- Evaluate MarkItDown or a similar converter layer for broader document classes.
- Expand intake to images, audio, video, PPTX, email, URLs, and cloud connectors.
- Add richer retrieval quality evaluation for the compiled wiki and source evidence stack.
- Improve review-gated proposal workflows for durable memory updates.
