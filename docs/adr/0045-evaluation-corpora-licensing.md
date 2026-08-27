# ADR 0045 — Evaluation corpora: `open_ragbench` and LOCOMO declined

- **Status:** Accepted; execution contract updated 2026-08-27
- **Date:** 2026-08-22
- **Requirement:** document answer quality / agent-memory cross-lingual retrieval
- **Relates to:** PRD amendments #119, #159 and #161

## Context

Two third-party corpora had been staged for measurements without an executable owner:

- `vectara/open_ragbench` is CC-BY-NC-4.0 and cannot be made a commercial release gate while
  Aura's commercial posture is unsettled.
- `snap-research/locomo` carries no asserted license. Absence of a license is not permission to
  redistribute it or fetch it in CI.

The original implementation left LOCOMO tests permanently skipped and document retrieval tests
compiled but unexecuted. That state was not evidence: it was stale code that looked like a gate.

## Decision

Neither corpus may be fetched by a pipeline, CI job, Makefile target, test helper or container
build. The retired LOCOMO tests are deleted rather than kept behind an unprovisionable skip.
Cross-lingual memory retrieval is now a mandatory live scenario over self-authored facts in
`scripts/agent_memory_eval.py`.

Document evaluation uses the checksummed, repository-owned 21-file corpus exercised by
`scripts/ingest_reconcile_e2e.sh`:

- `retrieval_fusion_bench_test.go` publishes a 20-query retrieval diagnostic. Its filename qrels
  are not an answer-quality oracle and therefore do not block release at a synthetic R@1 target.
- `TestDocumentCorpusAgentEval` is the behavior oracle: nine exact-answer cases, mandatory
  `document_search`, forbidden web tools and at most nine durable assistant turns.
- the unused rerank/abstention harness and qrel appendix are deleted. Production had no caller,
  calibrated threshold or decision that consumed their vectors.

## Measured evidence

On 2026-08-27 the real production Runner answered all nine document cases exactly. Durable call
counts were 3, 3, 5, 3, 7, 3, 3, 2 and 6; every case used `document_search` and none used a web
tool. The harness initially reported 8/9 because `not-paid` had an arbitrary six-call budget even
though its seven-call answer was exact. The budget is now uniformly nine, two turns above the
measured maximum. No post-fix paid rerun is claimed or required to reinterpret the recorded run.

The same lifecycle measured the retrieval diagnostic at approximately R@1 0.55, R@3 0.80 and
MRR 0.699. These values diagnose candidate ordering; they do not supersede the nine-case agent
oracle.

## Consequences

- No skipped LOCOMO suite or dead abstention report can be mistaken for current evidence.
- Document retrieval changes still emit a pinned diagnostic, while releases are judged on answers
  through the same tools and Runner users exercise.
- A future statistical abstention mechanism requires a production consumer, a permissible corpus
  and a separately measured operating point before a scoring harness is restored.
- A future ADR may reverse either corpus decision only with an explicit compatible license or a
  settled posture that permits the existing license.

## What this decision does not establish

- Nine cases are a product regression gate, not a population-level claim about every workbook or
  every unanswerable question.
- The diagnostic R@1 target is not a production threshold.
- This ADR does not settle whether Aura is a commercial product.
