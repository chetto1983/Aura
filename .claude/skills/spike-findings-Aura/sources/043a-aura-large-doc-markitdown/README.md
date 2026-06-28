---
id: 043a
title: Aura Large-Document Markitdown Path
verdict: PARTIAL
date: 2026-06-12
related:
  - internal/channels/telegram/documents.go
  - docker/markitdown/app.py
  - internal/channels/telegram/documents_test.go
  - 043b-privategpt-async-ingest-reference
---

# 043a - Aura Large-Document Markitdown Path

## Question

Given a 25-49 MiB Telegram document, can Aura's current document conversion path ingest it through markitdown with visible success/failure and bounded memory?

## Harness

Run:

```powershell
go run ./.planning/spikes/043a-aura-large-doc-markitdown
```

The harness source-audits Aura's conversion code, exercises the same tier rules as `documentConverter.Convert`, and posts synthetic multipart payloads through an `httptest` sidecar stand-in to measure the in-memory request shape.

## Findings

- Conversion failures are visible to the user after the prior H3 fix: async callbacks carry both success and failure notifications through the Telegram dispatch tests.
- The implementation comment says `<=5 MiB` should stay sync and `>50 MiB` should refuse, but the switch currently uses `>=` for both thresholds. Exactly 5 MiB routes async, and exactly 50 MiB is refused.
- `postConvert` builds a full `bytes.Buffer` multipart request before sending the file to the sidecar. The sidecar then uses `await file.read()`, materializing the upload again before writing a temp file.
- The existing tests cover `threshold + 1` cases but not exact 5 MiB or exact 50 MiB boundary behavior.

## Recommendation

For a production large-file ingestion lane, keep the current refusal guard but replace the conversion transport with a streaming or temp-file based path. Fix the exact boundary checks to match the comments and compose contract, then add exact-boundary tests. If memory ingestion is added after conversion, it should be a job with status, not only a goroutine callback.

## Verdict

PARTIAL. Aura already has an async document-conversion path and user-visible failure notifications, but the current upload path is not memory bounded for 5-50 MiB files and has exact-boundary drift.
