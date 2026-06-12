---
id: 043b
title: PrivateGPT Async Ingest Reference
verdict: VALIDATED
date: 2026-06-12
source:
  repo: https://github.com/zylon-ai/private-gpt
  checkout: D:/tmp/private-gpt
  commit: 8ac84e3c35ba48447d7b0eb136f5a1369bab7b2d
related:
  - 043a-aura-large-doc-markitdown
  - 046-telegram-ingest-job-ux
---

# 043b - PrivateGPT Async Ingest Reference

## Question

What concrete large-document ingestion lessons should Aura borrow from PrivateGPT, especially for async ingest, idempotency, and status visibility?

## Harness

Run:

```powershell
go run ./.planning/spikes/043b-privategpt-async-ingest-reference
```

The harness audits the local reference checkout at `D:/tmp/private-gpt` and pins facts to the current source, not just the published docs.

## Findings

- Current PrivateGPT source exposes sync ingest, async ingest, and async status under `/v1/artifacts`: `/ingest`, `/ingest/async`, and `/ingest/async/{task_id}`.
- Async ingest returns a task ID and queues `vector_index_task` through Celery. Non-URI async input is first uploaded to a temporary S3 bucket because Celery task args cannot safely carry large binary payloads.
- Ingest is idempotent by file hash inside the same artifact: re-ingesting the same file and same artifact returns zero new documents, while the same file under a different artifact still ingests.
- The vector index insert batch size is capped at 512, and the current async index path uses a bounded concurrency call with `concurrency_limit=1`.
- Temporary S3 cleanup is explicit and tested: success and non-autoretry failures remove temporary input; autoretry failures retain it for retry.
- Repo docs match the source route family, while the public published documentation also contains older `/v1/ingest/file` style pages. Treat source and OpenAPI as the reference of record.

## Recommendation

Borrow the pattern, not the stack. Aura does not need Celery/S3/Qdrant for the local desktop lane, but it should adopt:

- a durable ingest job ID,
- status polling or event replay,
- explicit progress/failure states,
- idempotency by `source_id`, `document_id`, and content hash,
- bounded insert/chunk batches,
- cleanup rules for temporary conversion artifacts.

## Verdict

VALIDATED. PrivateGPT is a useful reference architecture for large-file ingest UX and lifecycle semantics, but it is too heavy and too Python-stack-specific to become an Aura dependency.
