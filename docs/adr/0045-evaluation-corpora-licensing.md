# ADR 0045 — Evaluation corpora: `open_ragbench` and LOCOMO declined

- **Status:** Accepted
- **Date:** 2026-08-22
- **Requirement:** Document routing recall@1 (`docs/aura-quality-snapshot.md`) / agent-memory LOCOMO suite
- **Relates to:** `prd.md` §Perimetro e licenze (`:6373`) and Amendment #119 (`:6292`)

## Context

Two third-party corpora were staged as the reference data for measurements this project owes but
has never produced:

- **`vectara/open_ragbench`** — 1,914 queries with real qrels, staged for the document-routing
  `recall@1` row and for the abstention working point Amendment #119 ratifies as a direction but
  never as a number. License: **CC-BY-NC-4.0**.
- **`snap-research/locomo`** — staged for `internal/arcadedb/locomo_test.go`. License:
  **NOASSERTION**.

Neither is downloaded by any pipeline today: `AURA_LOCOMO_DIR` defaults to a WSL scratch path
(`internal/arcadedb/locomo_test.go:86`) that no workflow sets, and `scripts/agent_memory_eval.py:52`
skips `^TestLocomo` outright. The two retrieval harnesses
(`internal/documents/retrieval_abstention_eval_test.go`,
`retrieval_fusion_bench_test.go`, build tag `retrieval_eval`) are compiled by
`scripts/tagged_tier_compile.sh` and executed by nothing. So nothing is in violation — but the
decision was **due before** a pipeline fetches either one, not after, and `prd.md:6373` records it
as outstanding. Wiring a download **is** taking the decision.

## Decision

**Both corpora are declined. Neither may be fetched by any pipeline, CI job, Makefile target, test
helper, or container build.**

- `open_ragbench` is declined because CC-BY-NC-4.0 restricts use to non-commercial purposes.
  Aura's commercial posture is not settled, and a corpus baked into a CI gate is not a decision
  that can be quietly reversed later: it would have to be un-measured, not just un-downloaded.
- LOCOMO is declined because **NOASSERTION means no permission was granted**. Absence of a
  license is not a permissive license.

The measurements these corpora were staged for are **redefined, not cancelled**. Their replacement
must be a corpus whose license permits redistribution and CI use, or one this project owns
outright. The two harnesses stay on disk and stay compiled: they are the apparatus, and the
apparatus is not what was declined.

## Consequences

- The document-routing `recall@1` row stays **UNKNOWN** until a permissible reference corpus with
  qrels exists. Its acceptance criterion (`>=75% recall@1 on the reference corpus`) is unmeasurable
  while "the reference corpus" is undefined, and the row must name the corpus it means before it
  can carry a number. Two of the three levers it cites are already dead
  (`AURA_DOCUMENT_OCR_ENABLED` has zero occurrences; Docling was removed), so the row is rewritten,
  not merely filled.
- The LOCOMO suite is **retired as specified**. `locomo_test.go` and the
  `scripts/agent_memory_eval.py:52` skip stay as they are until a replacement corpus is chosen;
  a skipped suite that nobody can provision is not a gap that closes by waiting.
- Amendment #119's abstention working point stays **unset**. It ratifies a direction ("low
  confidence must push toward `document_open`") and reports ROC AUC 0.880 with no working point.
  Writing `if score < …` without a permissible corpus to fit it on is inventing the number.
- A future ADR may reverse this for either corpus. Reversal requires a settled commercial posture
  (for `open_ragbench`) or an explicit grant from the publisher (for LOCOMO) — not a re-reading of
  the same license text.

## What this decision does NOT establish

- It does not say the replacement corpus is smaller, weaker, or harder to obtain — nobody has
  looked yet. It says only that these two are not it.
- It does not measure anything. No number in this ADR is a measurement; the licenses are legal
  facts and the file-level claims are reads of the tree at `9b053ee8e`.
- It does not settle whether Aura is a commercial product. It removes that question from the
  critical path of two measurements, which is the opposite of answering it.
