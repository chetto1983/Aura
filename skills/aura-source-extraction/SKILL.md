---
name: aura-source-extraction
description: Use when implementing Aura source ingestion, extraction, normalization, memory scorecards, uploaded files, Pyodide extractors, extract.md, extract.json, or OCR compatibility.
---

# Aura Source Extraction

## Overview

Aura v1.2 turns uploaded files into memory through one contract: every supported source produces `extract.md` and `extract.json`, then the ingest/wiki/retrieval path consumes that normalized evidence.

**REQUIRED SUB-SKILL:** Use test-driven-development before code changes.

## Non-Negotiables

- Preserve user edits and keep changes scoped to the plan task.
- Do not add another agent-facing file creation tool.
- Do not run arbitrary user Python for extraction.
- Do not replace the PDF OCR path; adapt it into normalized evidence.
- Do not confuse generated sandbox artifacts with uploaded source evidence.

## Source Contract

Supported uploaded source types for v1.2:

| Extension | Kind | Extraction path |
| --- | --- | --- |
| `.pdf` | `pdf` | Existing Mistral OCR, then adapter writes `extract.md` and `extract.json` |
| `.txt` | `text` | Go-native text extractor |
| `.md` | `markdown` | Go-native markdown/text extractor |
| `.json` | `json` | Go-native JSON parser and stable markdown rendering |
| `.csv` | `csv` | Go-native CSV parser first; Pyodide only for table-heavy fallback |
| `.docx` | `docx` | DOCX extractor that returns headings, paragraphs, and tables as markdown |
| `.xlsx` | `xlsx` | Fixed Pyodide extractor using bundled spreadsheet packages |

Normalized files live beside `source.json` under `wiki/raw/<source-id>/`:

- `original.<ext>`: immutable uploaded file
- `extract.md`: normalized markdown evidence
- `extract.json`: extractor metadata, warnings, counts, and version
- `ocr.md` / `ocr.json`: PDF compatibility files only

## Pyodide Rules

Use Pyodide only behind fixed Aura-owned extractor scripts or templates.

Required constraints:

- `allowNetwork=false`
- bounded runtime
- bounded stdout/stderr
- bounded normalized text size
- structured warnings for truncation or parse loss
- no durable writes outside the source directory

If a task asks for a script, create an extractor template or Go bridge, not a general-purpose file tool.

## Quality Bar

Sandbox extraction code must be boring, deterministic, and easy to test.

- Prefer small functions with explicit inputs and outputs.
- Return structured errors and warnings instead of parsing stderr text.
- Keep source bytes, normalized markdown, and metadata as separate values.
- Version every extractor contract so old `extract.json` files stay understandable.
- Treat truncation as a recorded warning, not a silent success.
- Use fixture files for malformed input, table input, and large input.
- Keep network disabled even when third-party parsing libraries are used.
- Avoid hidden global state; pass runners, limits, and stores as dependencies.

## Implementation Pattern

For each task:

1. Write the failing test for the exact source behavior.
2. Run it and confirm the expected failure.
3. Implement the smallest change.
4. Run the focused test.
5. Run the adjacent package test.
6. Commit only the files for that task.

## Common Mistakes

| Mistake | Fix |
| --- | --- |
| Keeping dashboard upload PDF-only | Use the shared source format policy in both dashboard API and Telegram |
| Reading only `ocr.md` | Read `extract.md` first, then fall back to `ocr.md` for legacy PDFs |
| Adding a separate spreadsheet ingestion tool | Route XLSX through the same source normalization contract |
| Returning raw JSON/CSV without markdown structure | Render stable markdown evidence suitable for wiki summaries and retrieval |
| Marking extraction complete without metadata | Always write both `extract.md` and `extract.json` |

## Release Gate

Before claiming v1.2 source extraction work is complete, run the task-specific checks plus:

```bash
go test ./internal/source ./internal/ingest ./internal/api ./internal/telegram ./internal/tools ./internal/memoryscore -count=1
go run ./cmd/debug_memory_scorecard
```
