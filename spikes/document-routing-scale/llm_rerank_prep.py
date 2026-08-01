"""Prepare an auditable LLM-as-reranker test.

The 384d embedder recovered only 2/8 paraphrased needles. Question: is that a
"semantics is hard" ceiling, or a "small model" ceiling? A strong reranker
(here: the LLM in the loop) reading the hybrid top-K should recover the needle
whenever retrieval SURFACED it.

This script dumps, per paraphrased query, the hybrid (BM25+dense) top-K candidate
chunks with NO answer marker, to llm_rerank_candidates.json. The LLM then writes
its picks to llm_rerank_picks.json (query_section -> chosen candidate id). The
scorer (llm_rerank_score.py) compares picks against the hidden ground truth.

It also reports the CANDIDATE-RECALL CEILING: how many needles are even present
in the top-K -- the reranker cannot recover what retrieval never surfaced.
"""

import json
import os
import numpy as np

from experiments import BM25, tok, parse_sections, chunk_words, CORPUS
from semantic_experiments import embed, dense_rank, rrf, PARAPHRASES

K = 12


def main():
    secs = parse_sections(os.path.join(CORPUS, "contratto_quadro.txt"))
    needles = json.load(open(os.path.join(CORPUS, "needles.json")))

    flat = []
    for si, (title, body) in enumerate(secs):
        for ch in chunk_words(body):
            flat.append((si, title, ch))
    texts = [c[2] for c in flat]
    bm = BM25([(i, tok(t)) for i, t in enumerate(texts)])
    mat = embed(texts)

    out_candidates = []
    truth = []
    ceiling = 0
    for nd in needles:
        sec = nd["section"]
        q = PARAPHRASES[sec]
        needle = nd["needle"]
        qv = embed([q])[0]
        fused = rrf([i for i, _ in bm.rank(q)], dense_rank(qv, mat))[:K]
        cands = [{"id": j, "text": flat[idx][2].strip()} for j, idx in enumerate(fused)]
        gt_local = next((j for j, idx in enumerate(fused) if needle[:30] in flat[idx][2]), None)
        ceiling += 1 if gt_local is not None else 0
        out_candidates.append({"section": sec, "query": q, "candidates": cands})
        truth.append({"section": sec, "needle": needle, "gt_candidate_id": gt_local})

    json.dump(out_candidates, open(os.path.join(os.path.dirname(__file__), "llm_rerank_candidates.json"), "w"),
              ensure_ascii=False, indent=1)
    json.dump(truth, open(os.path.join(os.path.dirname(__file__), "llm_rerank_truth.json"), "w"),
              ensure_ascii=False, indent=1)
    print("wrote llm_rerank_candidates.json (%d queries, top-%d each)" % (len(out_candidates), K))
    print("candidate-recall ceiling: needle present in top-%d for %d/%d queries" % (K, ceiling, len(needles)))


if __name__ == "__main__":
    main()
