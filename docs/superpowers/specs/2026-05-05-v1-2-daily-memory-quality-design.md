# v1.2 Universal Source Ingestion + Daily Memory Quality Design

Date: 2026-05-05
Status: approved for planning
Source of truth: `.planning/PROJECT.md`, `.planning/codebase/CONCERNS.md`, `docs/llm-wiki.md`, Cognee ingestion model review

## Purpose

v1.2 makes Aura more useful as a standalone second brain by letting users ingest the files they actually have, not only PDFs, and then proving that the resulting memory is reliable in daily use.

The milestone has two connected lanes:

- Universal source ingestion: accept common knowledge-file formats, normalize them into durable markdown/text evidence, and send them through the existing source/wiki/retrieval path.
- Daily memory quality: add a repeatable scorecard that checks whether Aura retrieves the right evidence, handles stale facts, and proposes useful wiki updates.

Cognee is useful as a product pattern because it treats ingestion as a broad data pipeline before memory construction. Aura should borrow that principle, not replace itself with Cognee or add a separate Python service. Aura remains a local Go application with SQLite, the existing source inbox, and reviewed wiki updates.

## Milestone Boundary

v1.2 is in scope when the work improves either what Aura can ingest as a source or how reliably Aura answers from those sources.

In scope:

- dashboard and Telegram uploads for `.pdf`, `.txt`, `.md`, `.json`, `.csv`, `.docx`, and `.xlsx`;
- a normalized extraction contract shared by all uploaded file types;
- a deterministic Pyodide-assisted extraction path for richer files where Go-native parsing is weak or expensive;
- source status and metadata that distinguish uploaded, extracted, failed, ingested, and reviewable states;
- retrieval and wiki proposal improvements that use normalized source evidence;
- a local mixed-source memory scorecard with realistic daily-use prompts;
- focused tests and fixtures for ingestion, extraction, retrieval, and proposal behavior.

Out of scope:

- images, audio, video, PPTX, cloud connectors, email import, and website crawling;
- replacing Mistral OCR for the existing PDF path;
- replacing `chromem-go`, SQLite, or the embedding stack;
- adopting Cognee as a runtime dependency;
- arbitrary Python execution in the ingestion path;
- automatic durable wiki mutation without human review;
- dashboard redesign beyond the controls and states needed for multi-format upload visibility.

## Requirements

### INGEST-01: Multi-Format Upload Acceptance

Aura must accept the first universal-ingestion file set from both user-facing upload surfaces.

Acceptance:

- Telegram document handling accepts `.pdf`, `.txt`, `.md`, `.json`, `.csv`, `.docx`, and `.xlsx`;
- dashboard upload accepts the same file set and shows the allowed formats in the input affordance;
- unsupported formats fail with a clear user-facing reason and no partial source record;
- sha256 dedup still works across all uploaded files;
- tests cover accepted and rejected extensions/MIME types.

### EXTRACT-01: Normalized Source Extraction Contract

Every uploaded file type must produce a common normalized evidence shape before ingestion.

Acceptance:

- each successful extraction writes normalized markdown/text evidence for retrieval and wiki proposal use;
- extraction metadata records source kind, extractor name, extractor version, sha256, original filename, text length, warnings, and relevant structure counts such as pages, sheets, rows, or sections;
- extraction failures store a durable failure reason that can be shown in the source inbox;
- the existing PDF OCR output is adapted to the same evidence contract without regressing PDF behavior.

### SANDBOX-01: Deterministic Pyodide Extractors

Aura must use the existing Python sandbox as an extraction helper where it gives clear leverage.

Acceptance:

- Pyodide extractors run fixed, versioned scripts/templates owned by Aura, not arbitrary user-provided Python;
- sandbox extraction is used for `.xlsx` and may be used for `.docx`, `.csv`, and table-heavy formats when it produces better normalized output than simple Go parsing;
- extractor execution has bounded runtime, bounded output size, no network access, and structured warnings for truncation or parse loss;
- extractor tests include at least one table fixture and one malformed-file failure fixture;
- sandbox artifact support remains separate from source extraction artifacts to avoid confusing generated files with ingested user sources.

### SKILL-SANDBOX-E2E: Skill-Guided Sandbox Memory Flow

Aura must prove that local procedural skills can guide useful sandbox work without hardcoding every workflow or adding extra runtime agents.

Acceptance:

- Aura reads required runtime skills from `D:\Aura\skills`;
- a local skill guides deterministic Python script creation for a real mixed-source fixture;
- the script runs in the existing Pyodide sandbox with no network and bounded timeout;
- both the generated script and result are persisted as source evidence;
- the persisted script/result can be read back and used for recall;
- the flow runs as a local E2E command suitable for release gates.

### MEMEVAL-01: Mixed-Source Memory Scorecard

Aura must have a repeatable local scorecard for daily-memory behavior over mixed source types.

Acceptance:

- scorecard cases live in versioned fixtures, not ad hoc manual notes;
- fixtures include at least one source each for text/markdown, JSON, CSV/XLSX table data, DOCX, and PDF;
- cases cover project-status recall, decision recall, next-action recall, stale-fact resistance, table fact recall, and evidence use;
- each case has an expected behavior and a small rubric that can be evaluated without live Telegram;
- the scorecard command produces a compact pass/fail summary suitable for release gates.

### RET-01: Evidence Retrieval Reliability

Aura must retrieve and pack the most relevant memory evidence before answering memory-heavy prompts.

Acceptance:

- retrieval includes wiki pages, conversation archive evidence, normalized sources, and pending proposal context where available;
- context packing prefers current, source-backed facts over older duplicate facts;
- normalized source evidence preserves enough metadata for answers or proposals to cite where facts came from;
- tests cover stale-vs-current facts, cross-source synthesis, and table-derived facts.

### PROP-01: Wiki Proposal Deduplication

Wiki proposals must avoid creating duplicate or near-duplicate review items.

Acceptance:

- new proposals are compared with existing pending proposals and relevant wiki pages;
- duplicate proposals are skipped or merged into an existing pending proposal with an explicit note;
- tests cover duplicate proposal suppression and non-duplicate proposal preservation.

### PROP-02: Source-Backed Proposal Text

Wiki proposals must be reviewable from their own text.

Acceptance:

- proposal text states what should change, why, and which evidence supports it;
- proposals can cite normalized source evidence from any supported file type;
- proposals avoid vague summaries such as "update the wiki" without concrete target content;
- stale or conflicting facts are flagged instead of silently overwritten;
- tests cover proposal text for an accepted new fact and a conflict case.

### REL-03: v1.2 Release Gate

v1.2 closes only when ingestion and memory quality pass together.

Acceptance:

- multi-format upload/extraction tests pass;
- focused retrieval/proposal tests pass;
- the mixed-source memory scorecard reaches the agreed threshold;
- broad Go verification passes;
- `.planning/` and `docs/implementation-tracker.md` record scorecard results, supported formats, and remaining known gaps.

## Architecture

The core architectural change is to introduce source normalization as the boundary between "a user uploaded a file" and "Aura can reason over this evidence."

### Source Normalization Layer

The existing source package should gain a small normalization layer that maps each source kind to an extractor:

- PDF: keep the current Mistral OCR path, then adapt its markdown output into the common normalized contract.
- TXT/MD: read directly with size limits and minimal cleanup.
- JSON: parse with Go, emit stable fenced or structured markdown summaries that preserve keys and values.
- CSV: parse with Go for simple cases; use Pyodide/pandas only when dialect or table handling needs it.
- XLSX: use Pyodide with the existing bundle packages (`pandas`, `python_calamine`, `xlrd`) to extract workbook sheets into compact markdown tables plus metadata.
- DOCX: prefer an extractor that returns headings, paragraphs, and tables as markdown. The implementation plan can choose Go-native parsing or Pyodide, but the output contract is the same.

The normalization layer should be callable from Telegram upload, dashboard upload, and future maintenance/debug commands.

### Pyodide Extractor Boundary

The Python sandbox is a helper, not a new agent-facing file tool. It should run locked extractor scripts that accept a mounted input file and return structured extraction output.

Extractor output:

- normalized markdown/text;
- structured metadata JSON;
- warnings;
- failure code and message when extraction cannot continue.

Extractor constraints:

- fixed script name and version per format;
- no network;
- bounded runtime;
- bounded stdout/stderr;
- bounded normalized text size with explicit truncation warnings;
- no durable writes outside the source extraction artifact directory.

This keeps the powerful Python environment useful for messy real-world files while preventing it from becoming a confusing parallel file-creation path for the agent.

### Source Inbox and Status Model

The source inbox should show all supported uploaded source types, not only PDFs.

Useful statuses:

- uploaded: file stored and deduped;
- extracting: normalization in progress;
- extract_complete: normalized evidence is available;
- extract_failed: normalization failed with a durable reason;
- ingested: source evidence has been indexed/used by the wiki ingestion path;
- needs_review: source created a wiki proposal or conflict that requires review.

The implementation can map these onto existing statuses initially, but the user-facing behavior should be format-neutral.

### Scorecard Package

Add or extend a test/debug surface that can run deterministic memory cases against local fixtures. The scorecard should avoid live LLM dependence at first when possible by testing extraction metadata, retrieval selection, proposal shaping, and expected evidence selection. If a live model path is needed later, it should be optional and clearly marked.

The scorecard should output:

- case name;
- requirement area;
- pass/fail;
- short failure reason;
- aggregate threshold result.

### Retrieval Improvements

Retrieval changes should stay near existing memory/search boundaries:

- `internal/tools/memory_search.go` for tool-facing memory search behavior;
- `internal/search` for ranking and evidence search helpers;
- `internal/conversation` for context packing and prompt envelopes;
- `internal/wiki` and proposal stores only where evidence metadata is needed;
- `internal/source` and `internal/ingest` where normalized source records enter the memory system.

The design should prefer explicit ranking signals over hidden prompt tricks. Useful signals include source type, recency, wiki link proximity, exact slug/title match, whether the fact already exists in a reviewed wiki page, and whether the evidence comes from a normalized source with extraction warnings.

### Proposal Improvements

Proposal logic should remain review-gated. v1.2 can make proposals smarter, but it should not silently mutate durable wiki content.

Proposal generation should produce structured internal data before final text where practical:

- target slug or suggested slug;
- proposed operation: create, update, merge, or conflict;
- evidence snippets or references;
- duplicate/conflict status;
- final reviewer-facing text.

This keeps dedupe and conflict handling testable without relying only on natural-language matching.

## Data Flow

1. User uploads a supported file through Telegram or the dashboard.
2. Aura stores the original file with sha256 dedup and source metadata.
3. The source normalization layer selects a Go-native or Pyodide extractor.
4. The extractor produces normalized markdown/text plus extraction metadata.
5. The source inbox shows extraction status and warnings.
6. Ingestion indexes normalized evidence and can create review-gated wiki proposals.
7. Memory-heavy prompts retrieve candidates from wiki, archive, normalized sources, and pending proposals.
8. Ranking chooses current and source-backed evidence first.
9. The scorecard runs the same extraction/retrieval/proposal paths against fixtures and records whether expected evidence and behavior appear.

## Error Handling

Ingestion and memory quality failures should be diagnosable but not user-hostile.

- Unsupported formats should fail before durable source creation with a clear reason.
- Malformed supported files should create an `extract_failed` state with a durable failure reason.
- Partial extraction should be allowed only when warnings clearly state what was skipped or truncated.
- Missing evidence should produce an "insufficient evidence" outcome rather than confident fabrication.
- Retrieval backend errors should be logged and surfaced in scorecard output.
- Proposal dedupe errors should fail closed by avoiding automatic durable mutation; creating a clearly marked review item is safer than silent wiki writes.
- Scorecard failures should name the case and missing expectation.

## Testing Strategy

Testing should start with deterministic ingestion and become the release gate for memory quality.

Focused tests:

- upload allowlist and rejection behavior for Telegram and dashboard handlers;
- extraction contract generation for TXT/MD, JSON, CSV, XLSX, DOCX, and existing PDF OCR output;
- Pyodide extractor success, truncation warning, and malformed-file failure behavior;
- scorecard fixture loading and threshold calculation;
- retrieval ranking for stale/current facts;
- retrieval packing for multi-source and table-derived evidence;
- duplicate proposal suppression;
- conflict proposal text.

Release verification:

- run the mixed-source memory scorecard;
- run focused source/ingest/sandbox/retrieval/proposal package tests;
- run the broad Go verifier.

Manual verification:

- upload one small TXT/MD, JSON, CSV/XLSX, DOCX, and PDF through the dashboard or Telegram;
- confirm each appears in the source inbox with a useful status;
- ask three daily-memory prompts, including one table fact question;
- review a sample generated proposal for clarity and evidence grounding.

## Success Criteria

v1.2 is complete when:

- Aura accepts `.pdf`, `.txt`, `.md`, `.json`, `.csv`, `.docx`, and `.xlsx` from Telegram and dashboard uploads;
- each supported file type produces normalized evidence and extraction metadata;
- Pyodide extractors are deterministic, bounded, and tested for richer table/office extraction;
- Aura has a repeatable mixed-source memory quality scorecard;
- the scorecard covers at least six realistic daily-use memory cases;
- retrieval improvements pass stale/current, cross-source, and table-evidence tests;
- wiki proposals are deduplicated and source-backed in tests;
- the scorecard reaches the agreed pass threshold;
- core Go verification remains green.

## Deferred

- images, audio, video, and PPTX ingestion;
- cloud connectors and external knowledge graph services;
- direct Cognee integration;
- automatic wiki mutation without human review;
- vector-store replacement;
- broad `internal/tools` package split;
- arbitrary package-wide coverage targets.

## Scorecard Threshold

The first v1.2 release threshold is fixed at planning start: all deterministic upload/extraction/retrieval/proposal tests must pass, and at least 5 of 6 mixed-source daily-memory cases must pass the scorecard before closing v1.2.
