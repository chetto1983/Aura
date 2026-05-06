# Aura

Aura is a standalone second-brain Telegram assistant with an embedded React dashboard. It owns its source inbox, extracted evidence, compiled wiki, graph, review queue, and audit trail.

The current product direction is the LLM Wiki memory pattern: immutable sources become normalized extraction evidence, durable facts and synthesis live in markdown wiki pages, and review-gated tools promote important findings into the wiki.

## Active Product Truth

- Users can upload and manage evidence from Telegram and the dashboard.
- Source ingestion supports PDF, TXT, Markdown, JSON, CSV, XLSX, and DOCX.
- Extracted sources write `extract.md` and `extract.json`; PDF OCR also preserves `ocr.md` and `ocr.json`.
- `extract_complete` sources are eligible for wiki ingestion through the shared `AfterExtract` path.
- The dashboard is served from Go-embedded React assets under `internal/api/dist`.

## Guardrails

- Keep raw sources immutable.
- Use wiki `Body` content and `[[slug]]` links for durable pages.
- Keep upload-format claims truthful across API, Telegram, and frontend text.
- Prefer narrow, auditable extractors for shipped formats. General converters such as MarkItDown can be evaluated for future broader document coverage.
