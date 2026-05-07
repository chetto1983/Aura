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

## V13-MEM: Memory Consolidation And Quality

Status: done

Aura must keep durable memory clean, connected, and searchable. This milestone audited the live wiki graph, checked-in docs, generated artifacts, search index, embedding cache, and `search_memory` answer quality.

### Acceptance Criteria

- Operational docs (`SCHEMA.md`, `index.md`, `log.md`) are not listed as ordinary wiki pages or indexed as user memory.
- Generated workflow docs that duplicate `.planning/` are removed or archived outside active docs.
- The LLM tool registry includes `clean_wiki_memory`, which dry-runs by default and can apply deterministic hub creation, alias repair, related-frontmatter repair, index rebuild, and audit logging; nightly wiki maintenance also runs that cleaner automatically.
- Orphan wiki pages are either linked into a hub, repaired, or explicitly archived/deleted.
- Broken wiki links are repaired to real slugs or converted into intentional new hub pages.
- The dashboard graph has meaningful clusters and no operational/test debris nodes.
- Embeddings use the dedicated embedding settings only; no embedding path falls back to `LLM_API_KEY`.
- Embedding cache hit/miss behavior has focused tests and remains visible through `/api/health`.
- `search_memory` returns evidence envelopes that combine wiki, source, and archive evidence without silently mutating durable wiki pages.
- Proposal tools keep requiring evidence when the origin is `search_memory`.
- Deterministic scorecards and live wiki hygiene pass before closure.
- Live LLM memory routing passes the 30s latency gate with the DB-configured model and keeps proposals review-gated.
