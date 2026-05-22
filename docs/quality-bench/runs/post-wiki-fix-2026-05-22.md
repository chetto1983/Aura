# Substrate bench — post Phase-WIKI-FIX (2026-05-22)

**HEAD:** `aad25d7f` (after all 8 US-WIKI-FIX-* commits)
**Substrate state:** Qdrant 291 points @ 768d, FTS5 `wiki_documents` **159 rows** (was 17 pre-fix).

## Numbers

| Metric | Baseline 2026-05-22 | Post Phase-WIKI-FIX | Δ |
|---|---|---|---|
| Latency p50 | 17ms (256d) / 57ms (768d) | **55ms** | invariato (current at 768d) |
| Latency p95 | 55ms / 85ms | **75ms** | leggermente meglio |
| FTS hit ratio in top-5 | 0/20 (0%) | **20/20 (100%)** | +100pp ⭐ |
| Exact hit ratio in top-5 | 0/20 (0%) | **19/20 (95%)** | +95pp ⭐ |
| Top-1 score range | 0.0131 flat (vector only) | 0.0230 → 0.0393 | discriminating |

## Qualitative top-1 hits — baseline vs post-fix

| Query | Before | After | Verdict |
|---|---|---|---|
| tika-xlsx q2 ("quadrato 12") | simatic-s7-1200-g2-plc | source-tika-testexcel | ⭐ FIX |
| tika-docx q1 ("Apache Tika URL") | source-test-pms-gestione | **apache-tika** | ⭐ FIX (exact match catched rare token) |
| tika-pptx q2 | source-test-pms-gestione | source-tika-testppt | ⭐ FIX |
| wiki-dante-txt q2 | aura-operating-memory (system leak) | **dante-alighieri** | ⭐ FIX-05 (system filter) |
| autori-json q2 | davide ×2 (dup) | de-monarchia (no dup) | ⭐ FIX-06 (dedup) |
| tika-pptx q1 | source-tika-testppt | apache-tika | ✓ entity-over-source preference |
| wiki-galileo/dante/collodi/pirandello | all correct | all correct | maintained |

Plausible top-1 rate (visual): ~12/20 baseline → ~17-18/20 post-fix.

## Substrate bug status

| # | Bug | Story | Status |
|---|---|---|---|
| 1 | settings.updated_at NOT NULL no default | FIX-07 | ✅ |
| 2 | No dim mismatch auto-detection | FIX-04 | ✅ |
| 3 | Embed sidecar n_ctx hard-cap 1024 | FIX-08 | ✅ |
| 4 | Rebuild bails on first embed error | FIX-03 | ✅ |
| 5 | Embed cache key non include dim | FIX-02 | ✅ |
| 6 | No /api/admin/reindex endpoint | FIX-04 | ✅ |
| 7 | **FTS5 mirror non sync with disk** | **FIX-01** | **✅ (the KEY fix)** |
| 8 | System pages leak into user search | FIX-05 | ✅ |
| 9 | Slug duplicati nell'index | FIX-06 | ✅ |
| 10 | Score 0.013 flat (vector only) | FIX-01 (symptom) | ✅ |

## Closure97 gate posture

| Gate | Target | Measured | Status |
|---|---|---|---|
| Latency p95 chat | ≤10s | n/a (need e2e probe) | pending |
| Latency p95 search | ≤500ms | **75ms** | ✅ 6.6× under |
| Recall@5 | ≥97% | n/a (no expected_slug annotation) | pending |
| Pass rate | ≥97% | n/a | pending |

To compute Recall@5 + pass rate, annotate `docs/quality-bench/queries.json` with `expected_slug` per query. ~30 min manual work for 20 queries. Then run `cmd/quality_bench` end-to-end for gate verdict.

## Strategic implication

Phase-WIKI-B Wave A can now ship meaningfully. Hybrid fusion was a no-op before because FTS5 was stale; it actually fuses now. Wave A (`mergeHybridResults` polish + heading subnodes + `wiki_subgraph` PPR tool) should produce measurable Recall@5 + nDCG@10 deltas against this baseline.

