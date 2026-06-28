---
status: testing
phase: 30-retrieval-memory-hardening
source: [30-VERIFICATION.md]
started: 2026-06-28T08:00:57Z
updated: 2026-06-28T08:00:57Z
---

## Current Test

number: 1
name: Live rerank quality + p95 (GPU host)
expected: |
  On a GPU host with the aura-rerank server-cuda sidecar running and
  AURA_RERANK_BASE_URL set, the injected-answer document ranks #1 and p95 over
  N>=5 short-doc reranks is < 400ms.
awaiting: user response

## Tests

These 4 items are GPU/environment-gated automated tiers, deferred-by-design from the
2026-06-28 execution because this host has no GPU capable of running the `server-cuda`
reranker. The code harness for each is in place and enforces NO-SKIP-AS-GREEN (each
t.Fatal under $CI when its env is set, t.Skip only off-CI). Run them on a GPU host /
in CI to close the manual-pending sign-off.

### 1. Live rerank quality + p95 (GPU host)
expected: `AURA_RERANK_BASE_URL=http://127.0.0.1:8085 go test -tags rerank_integration -run TestRerankLive ./internal/rerank/ -v` — injected-answer doc ranks #1, p95 < 400ms
result: [pending]

### 2. Rerank-dominant per-stage comparison (GPU host)
expected: `go test -tags graphrag_live -run TestGraphRAGLive ./internal/documents/ -v` with the reranker live — rerank p95 >= 50ms (GPU active) and vector + expand each well under the rerank stage
result: [pending]

### 3. Vector-vs-rerank precision lift (GPU host)
expected: `go test -tags retrieval_eval -run TestRetrievalEval ./internal/eval/ -v` with the reranker live — mean nDCG@10(vector+rerank) >= nDCG@10(vector-only), zero per-query regressions beyond noise (non-monotonic guard holds)
result: [pending]

### 4. Full Go Postgres+Neo4j E2E for widened ingest
expected: `go test -tags document_ingest_live ./internal/documents/ -v` with the composed DSNs + NEO4J_PASSWORD exported (per CLAUDE.md §"Quality tooling & gates") — a G220-class PDF AND at least one non-PDF format (PPTX/HTML) reach status `searchable`, and the live `:NEXT_CHUNK` edge count equals chunk_count-1 per document
result: [pending]

## Summary

total: 4
passed: 0
issues: 0
pending: 4
skipped: 0
blocked: 0

## Gaps
