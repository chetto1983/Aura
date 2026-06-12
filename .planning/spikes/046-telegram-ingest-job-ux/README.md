---
id: 046
title: Telegram Ingest Job UX
verdict: VALIDATED
date: 2026-06-12
related:
  - 043a-aura-large-doc-markitdown
  - 043b-privategpt-async-ingest-reference
---

# 046 - Telegram Ingest Job UX

## Question

What user-visible job lifecycle should Aura expose when a Telegram document enters a 5-50 MiB async conversion and memory-ingest lane?

## Harness

Run:

```powershell
go run ./.planning/spikes/046-telegram-ingest-job-ux
```

The harness models the expected job lifecycle and verifies status transitions for success, failure, refusal, cancel, and polling.

## Contract

For files above the sync tier and at or below 50 MiB, Aura should create an ingest job before doing conversion work. The job should expose:

- `accepted`
- `running`
- `success`
- `failure`
- `refused`
- `canceled`

Telegram can render this as a short accepted message, optional progress edits or follow-up messages, and a final success/failure message. PrivateGPT's async endpoint provides the useful reference shape: return a task ID early, then make task state queryable.

## Findings

- A job ID before work starts is the key UX difference from the current goroutine-only conversion path.
- Failure needs a durable status, not just an immediate callback, because memory ingest can outlive a single handler context.
- Oversize refusal remains immediate and should not allocate a job.
- Cancel/delete semantics should exist before memory writes begin, especially if a delete request races an ingest job.

## Recommendation

Add a small local ingest-job registry when document conversion is wired to memory ingest. It can start in-process, but the public contract should look durable enough to survive a future move to persisted jobs.

## Verdict

VALIDATED. The job-state contract is small, fits Aura, and borrows the right lifecycle ideas from PrivateGPT without importing its infrastructure.
