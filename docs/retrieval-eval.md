# Retrieval eval harness (RET-05)

Aura's retrieval quality is gated by an offline eval harness that quantifies the rerank
lift and proves the non-monotonic guard prevents regressions. It answers the spike-070
question with numbers, not trust: **does the cross-encoder reranker measurably beat pure
vector retrieval on Aura's real corpus, and does it ever hurt?**

- Spike: `.planning/spikes/070-rerank-value-or-overengineered/README.md`
- Guard: `internal/documents/rerank_guard.go` (`applyRerankGuard`)
- Pure metrics: `internal/eval/retrieval_metrics.go` (default build, coverage-counted)
- Harness: `internal/eval/retrieval_eval.go` + `retrieval_eval_test.go` (`-tags retrieval_eval`)
- Judged set: `internal/eval/testdata/retrieval_judgments.json`

## Metrics

Three pure, deterministic information-retrieval metrics, scored over a ranked chunk-id
list against a relevant-id set:

| Metric | Definition |
|---|---|
| **nDCG@10** | DCG@10 / IDCG@10 with binary relevance (gain `1/log2(rank+1)`). The primary lift metric — it rewards ranking the right chunk high, which is exactly what rerank changes. |
| **Recall@5** | fraction of relevant chunks present in the top 5. |
| **MRR** | reciprocal rank of the first relevant chunk. |

The metrics carry **no build tag**, so they compile and are unit-tested under the default
build (`retrieval_metrics_test.go`) and are counted by the `make coverage` owned-surface
floor. The harness that drives the live pipeline carries the `retrieval_eval` tag and is
**never** part of `go test ./...` or `make quality-full`.

## Judgment format

`internal/eval/testdata/retrieval_judgments.json` is an array of >=30 labeled queries.
Chunk ids are content-hash-derived at ingest time and cannot be hard-coded, so each
judgment carries **stable content phrases**; the harness resolves them to the gold
chunk-id set against the freshly-ingested corpus (the spike-070 methodology — the gold
chunk is the one carrying the answer phrase). The relevance signal is independent of the
ranker, so it fairly discriminates vector-only from vector+rerank.

```json
{
  "query": "what tightening torque do the terminal cover screws need",
  "relevant_phrases": ["tightening torque", "terminal"],
  "note": "spike-070: vector #1 was a TOC false-positive; rerank found the real instruction"
}
```

A judgment whose phrases match no chunk in the supplied corpus is skipped; at least
`AURA_RERANK_EVAL_MIN_JUDGED` (default 8) queries must resolve relevant chunks, so a
corpus/phrase mismatch fails loud instead of passing vacuously. The set is seeded from
the six spike-070 proven cases (torque, fault code, ambient temperature, cable
cross-section, factory reset, and the **back-to-box** demotion the guard protects) plus
G220-class drive/converter queries.

## What the harness asserts

For each judged query the harness runs `documents.Service.Retrieve` twice over the same
two-stage pipeline:

- **vector-only** — an identity reranker keeps the vector-seed order;
- **vector+rerank** — the real GPU rerank sidecar (`AURA_RERANK_BASE_URL`).

It then asserts, over the judged set:

1. **Mean nDCG@10 lift** `>= AURA_RERANK_EVAL_MIN_LIFT` (default `0.0` — reranked is at
   least as good on average; spike-070 measured a clear positive lift on G220, and the
   floor is tunable upward on a known-hardware run).
2. **Zero non-monotonic regressions** — no query drops more than
   `AURA_RERANK_EVAL_NOISE` (default `0.10`) nDCG@10 under rerank. This is the guard
   verification: rerank must not bury a right answer (the spike-070 back-to-box case).
3. **No-skip-as-green** — under `$CI`, unset `AURA_DOC_TEST_PDF`/`AURA_RERANK_BASE_URL`
   `t.Fatal`s, and a reranker that reorders **zero** queries (sidecar unreachable)
   `t.Fatal`s rather than passing green. Locally these conditions `t.Skip`.

The scored report is written to `docs/aura-retrieval-eval.md` (never `/tmp`).

## Run command (GPU host)

The live "rerank beats vector" comparison needs the GPU reranker (spike-070: CPU is
~70-1000x too slow). On a GPU host with the stack up and `aura-rerank` running:

```bash
set -a; . ./.env; set +a
export PATH="$HOME/.local/bin:$HOME/go/bin:$PATH"
export AURA_DOC_TEST_PDF=/path/to/G220.pdf
export AURA_RERANK_BASE_URL=http://127.0.0.1:8085
go test -tags retrieval_eval -run TestRetrievalEval -timeout 900s -v ./internal/eval/
```

On a host without the GPU sidecar the tier `t.Skip`s (locally) — it is not wired into CI
(a paid/GPU-gated tier). The pure metrics still run and are coverage-counted under the
default build.

## Self-learning: OUT (deferred per spike-070)

This phase ships the **static** reranker plus the regression gate only. No
`internal/activelearn` learning loop is added. Spike-070 concluded the fixed
Qwen3-Reranker already works well and there is no production miss-data to learn from yet;
a learning loop now would be overengineering. Revisit only if production eval shows a
persistent, patterned rerank miss.
