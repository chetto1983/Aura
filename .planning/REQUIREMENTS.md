# Requirements

## V12-CLOSE: v1.2 Closure And Polish

Status: done

Aura must close v1.2 by making the shipped upload and dashboard surfaces truthful, tested, and polished enough for daily use.

### Acceptance Criteria

- Upload policy is shared across backend, Telegram, and frontend.
- Supported source formats are PDF, TXT, Markdown, JSON, CSV, XLSX, and DOCX.
- XLSX uploads extract through the sandbox/Pyodide path with mounted input files, bounded retries, and dashboard plus Telegram coverage.
- DOCX uploads extract through a fixed offline extractor with bounded ZIP/XML/text handling.
- Extracted non-PDF sources can flow into wiki ingestion after extraction.
- Dashboard E2E uses a token minted from the local database-backed environment, not a hand-created fixture token.
- Frontend audit covers key dashboard pages, including source inbox, wiki graph, conversations, settings, skills, and other core panels.
- Empty wiki graph responses serialize `edges` as `[]`, not `null`.
- Conversations table rows remain valid table rows while still offering keyboard-accessible turn opening.
- Active docs reflect the real v1.2 scope and known future gaps.

### Deferred

- Broad image, audio, video, PPTX, email, website, and cloud-connector ingestion.
- A generalized document converter layer. Microsoft MarkItDown is a candidate for a future spike, but v1.2 stays on narrow fixed extractors.
- Larger memory scorecard, retrieval quality, and autonomous proposal work beyond closure validation.
