"""Re-run the retrieval measurement with EmbeddingGemma-300M (GGUF, via llama.cpp).

The MiniLM-384d leg surfaced only 4/8 paraphrased needles in the top-12 (the
candidate-recall ceiling that capped the LLM reranker). Question: does a stronger
multilingual embedder (EmbeddingGemma-300M, 768d) raise that ceiling?

Measures, on the 8 PARAPHRASED big-doc queries + 4 concept routing queries:
  - DENSE-flat / HYBRID recall@1/@5 and the top-12 candidate-recall ceiling,
    vs the MiniLM baseline (dense 2/8, hybrid 4/8@5, ceiling 4/8).

Embeddings are cached to models/emb_cache.npz. EmbeddingGemma prompt formats:
  query    -> "task: search result | query: {text}"
  document -> "title: none | text: {text}"
"""

import json
import os
import numpy as np
from llama_cpp import Llama

from experiments import BM25, tok, parse_sections, chunk_words, CORPUS, card_xlsx, card_txt, role_of
from semantic_experiments import rrf, PARAPHRASES, CONCEPT_QUERIES

HERE = os.path.dirname(os.path.abspath(__file__))
MODEL = os.path.join(HERE, "models", "embeddinggemma-300M-Q8_0.gguf")

_llm = None


def _model():
    global _llm
    if _llm is None:
        _llm = Llama(model_path=MODEL, embedding=True, n_ctx=2048, n_batch=512,
                     pooling_type=getattr(__import__("llama_cpp"), "LLAMA_POOLING_TYPE_MEAN", 1),
                     verbose=False)
    return _llm


def _embed_raw(texts, batch=16):
    llm = _model()
    vecs = []
    for i in range(0, len(texts), batch):
        out = llm.create_embedding(texts[i:i + batch])
        for d in out["data"]:
            v = np.asarray(d["embedding"], dtype=np.float32)
            if v.ndim > 1:            # per-token (no pooling) -> mean pool
                v = v.mean(axis=0)
            vecs.append(v)
    m = np.vstack(vecs)
    return m / np.clip(np.linalg.norm(m, axis=1, keepdims=True), 1e-9, None)


def embed_docs(texts):
    return _embed_raw(["title: none | text: " + t for t in texts])


def embed_queries(texts):
    return _embed_raw(["task: search result | query: " + t for t in texts])


def dense_rank(qv, mat):
    return list(np.argsort(-(mat @ qv)))


def exp_a():
    print("\n### GEMMA-A - big-doc passage recall, PARAPHRASED queries ###")
    secs = parse_sections(os.path.join(CORPUS, "contratto_quadro.txt"))
    needles = json.load(open(os.path.join(CORPUS, "needles.json")))
    flat = [(si, t, ch) for si, (t, b) in enumerate(secs) for ch in chunk_words(b)]
    texts = [c[2] for c in flat]
    bm = BM25([(i, tok(t)) for i, t in enumerate(texts)])

    cache = os.path.join(HERE, "models", "emb_cache_chunks.npy")
    if os.path.exists(cache):
        mat = np.load(cache)
    else:
        print("  embedding %d chunks with EmbeddingGemma ..." % len(texts))
        mat = embed_docs(texts)
        np.save(cache, mat)

    def has(order, needle):
        return next((p for p, idx in enumerate(order, 1) if needle[:30] in flat[idx][2]), None)

    dn1 = dn5 = hy1 = hy5 = ceil_dn = ceil_hy = 0
    for nd in needles:
        qv = embed_queries([PARAPHRASES[nd["section"]]])[0]
        needle = nd["needle"]
        dn = dense_rank(qv, mat)
        hy = rrf([i for i, _ in bm.rank(PARAPHRASES[nd["section"]])], dn)
        pd, ph = has(dn, needle), has(hy, needle)
        dn1 += pd == 1; dn5 += bool(pd and pd <= 5)
        hy1 += ph == 1; hy5 += bool(ph and ph <= 5)
        ceil_dn += bool(pd and pd <= 12); ceil_hy += bool(ph and ph <= 12)
    n = len(needles)
    print("  DENSE-flat   recall@1 %d/%d  recall@5 %d/%d  (ceiling@12 %d/%d)" % (dn1, n, dn5, n, ceil_dn, n))
    print("  HYBRID-flat  recall@1 %d/%d  recall@5 %d/%d  (ceiling@12 %d/%d)" % (hy1, n, hy5, n, ceil_hy, n))
    print("  [MiniLM-384d baseline: dense @5 2/8, hybrid @5 4/8, ceiling@12 4/8]")


def exp_b():
    print("\n### GEMMA-B - routing on CONCEPT queries ###")
    files = [f for f in sorted(os.listdir(CORPUS)) if f.endswith((".xlsx", ".txt"))]
    cards = [(f + " tipo " + role_of(f) + " " +
              (card_xlsx(os.path.join(CORPUS, f)) if f.endswith(".xlsx") else card_txt(os.path.join(CORPUS, f))))
             for f in files]
    bm = BM25([(files[i], tok(cards[i])) for i in range(len(files))])
    cache = os.path.join(HERE, "models", "emb_cache_cards.npy")
    if os.path.exists(cache):
        mat = np.load(cache)
    else:
        print("  embedding %d cards ..." % len(files))
        mat = embed_docs(cards)
        np.save(cache, mat)

    def mrr(order, targets):
        return next((1.0 / i for i, f in enumerate(order, 1) if f in targets), 0.0)

    sb = sd = sh = 0.0
    for q, targets in CONCEPT_QUERIES:
        qv = embed_queries([q])[0]
        bmf = [f for f, _ in bm.rank(q)]
        dn = dense_rank(qv, mat)
        dnf = [files[i] for i in dn]
        hyf = [files[i] for i in rrf([files.index(f) for f in bmf], dn)]
        sb += mrr(bmf, targets); sd += mrr(dnf, targets); sh += mrr(hyf, targets)
    n = len(CONCEPT_QUERIES)
    print("  BM25 %.3f   GEMMA-dense %.3f   HYBRID %.3f" % (sb / n, sd / n, sh / n))
    print("  [MiniLM-384d baseline: dense 0.751, hybrid 0.762]")


if __name__ == "__main__":
    exp_a()
    exp_b()
