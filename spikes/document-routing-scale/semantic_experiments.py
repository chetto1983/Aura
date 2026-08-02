"""Close the gap the lexical harness left open: does a DENSE (embedding) leg
rescue routing / passage retrieval on PARAPHRASED queries where BM25 fails?

Uses a small multilingual ONNX model (paraphrase-multilingual-MiniLM-L12-v2,
384-dim) via fastembed -- no torch. Compares, on queries deliberately worded
with LOW lexical overlap with the target:

  A. big-doc passage recall: BM25-flat vs DENSE-flat vs HYBRID(RRF) vs
     DENSE-hierarchical (section-route then in-section dense).
  B. routing (concept queries): BM25-card vs DENSE-card vs HYBRID(RRF).

Deterministic corpus; the model is fixed. Run generate_scale_corpus.py first.
"""

import json
import os
import re
import numpy as np
from fastembed import TextEmbedding

from experiments import (BM25, tok, parse_sections, chunk_words, CORPUS,
                         card_xlsx, card_txt, role_of, flatten_xlsx)

MODEL = "sentence-transformers/paraphrase-multilingual-MiniLM-L12-v2"
_model = None


def embed(texts):
    global _model
    if _model is None:
        _model = TextEmbedding(model_name=MODEL)
    v = np.array(list(_model.embed(list(texts))), dtype=np.float32)
    n = np.linalg.norm(v, axis=1, keepdims=True)
    return v / np.clip(n, 1e-9, None)


def dense_rank(qvec, mat):
    """Return doc indices sorted by cosine desc (mat rows are unit vectors)."""
    sims = mat @ qvec
    return list(np.argsort(-sims))


def rrf(*ranklists, k=60):
    """Fuse ranked index lists by Reciprocal Rank Fusion -> fused index order."""
    score = {}
    for rl in ranklists:
        for rank, idx in enumerate(rl, 1):
            score[idx] = score.get(idx, 0.0) + 1.0 / (k + rank)
    return [i for i, _ in sorted(score.items(), key=lambda x: x[1], reverse=True)]


# ---------------------------------------------------------------------------
# A. big-doc passage recall on PARAPHRASED queries
# ---------------------------------------------------------------------------
# paraphrases share few surface tokens with the planted needle (BM25-hostile)
PARAPHRASES = {
    12: "che sanzione economica pago se chiudo il contratto prima del termine",
    23: "per quanto tempo devo mantenere segrete le informazioni dopo la fine dell'accordo",
    41: "in quale citta si terrebbe un'eventuale causa legale tra le parti",
    58: "qual e' il tetto massimo di risarcimento a carico del fornitore",
    77: "posso affidare l'esecuzione dei lavori a un'altra impresa",
    94: "cosa succede alle scadenze in caso di eventi imprevedibili e straordinari",
    108: "a chi appartengono i brevetti e le opere dell'ingegno prodotte",
    129: "quanto mi costa in piu se pago la fattura in ritardo",
}


def exp_a_passage():
    print("\n### SEM-A - big-doc passage recall, PARAPHRASED queries ###")
    secs = parse_sections(os.path.join(CORPUS, "contratto_quadro.txt"))
    needles = json.load(open(os.path.join(CORPUS, "needles.json")))

    flat = []  # (sec_idx, title, chunk_text)
    for si, (title, body) in enumerate(secs):
        for ch in chunk_words(body):
            flat.append((si, title, ch))
    texts = [c[2] for c in flat]
    bm = BM25([(i, tok(t)) for i, t in enumerate(texts)])
    print("  embedding %d chunks + %d sections ..." % (len(texts), len(secs)))
    mat = embed(texts)
    sec_mat = embed([t + " " + " ".join(b.split()[:30]) for t, b in secs])

    def sec_index(nsec):
        return next(i for i, (t, _) in enumerate(secs) if t.startswith("Articolo %d " % nsec))

    def hit(order, needle):
        for pos, idx in enumerate(order, 1):
            if needle[:30] in flat[idx][2]:
                return pos
        return None

    tallies = {n: [0, 0] for n in ["BM25-flat", "DENSE-flat", "HYBRID-flat", "DENSE-hier"]}
    for nd in needles:
        q = PARAPHRASES[nd["section"]]
        needle = nd["needle"]
        true_sec = sec_index(nd["section"])
        qv = embed([q])[0]

        bm_order = [i for i, _ in bm.rank(q)]
        dn_order = dense_rank(qv, mat)
        hy_order = rrf(bm_order, dn_order)

        # dense hierarchical: route section by dense, then dense within section
        sec_order = dense_rank(qv, sec_mat)
        sec_pos = next((i for i, s in enumerate(sec_order, 1) if s == true_sec), 999)
        hier_pos = None
        if sec_pos <= 3:
            in_chunks = chunk_words(secs[true_sec][1])
            cm = embed(in_chunks)
            for pos, idx in enumerate(dense_rank(qv, cm), 1):
                if needle[:30] in in_chunks[idx]:
                    hier_pos = pos
                    break

        for name, pos in [("BM25-flat", hit(bm_order, needle)),
                          ("DENSE-flat", hit(dn_order, needle)),
                          ("HYBRID-flat", hit(hy_order, needle)),
                          ("DENSE-hier", hier_pos)]:
            tallies[name][0] += 1 if pos == 1 else 0
            tallies[name][1] += 1 if (pos and pos <= 5) else 0

    n = len(needles)
    print("  (%d paraphrased queries, low lexical overlap with the needle)" % n)
    for name in ["BM25-flat", "DENSE-flat", "HYBRID-flat", "DENSE-hier"]:
        r1, r5 = tallies[name]
        print("    %-12s recall@1 %d/%d   recall@5 %d/%d" % (name, r1, n, r5, n))


# ---------------------------------------------------------------------------
# B. routing on concept queries
# ---------------------------------------------------------------------------
CONCEPT_QUERIES = [
    ("dove trovo l'elenco riepilogativo di tutte le fatture emesse", {"contabilita_2026.xlsx"}),
    ("quadro complessivo degli incassi mancati", {"report_insoluti_2026.xlsx", "contabilita_2026.xlsx"}),
    ("documento con le condizioni generali di fornitura", {"contratto_quadro.txt"}),
    ("prospetto dell'imposta sul valore aggiunto per periodo", {"report_iva_2026.xlsx", "contabilita_2026.xlsx"}),
]


def exp_b_routing():
    print("\n### SEM-B - routing on CONCEPT queries (card catalog) ###")
    files = [f for f in sorted(os.listdir(CORPUS)) if f.endswith((".xlsx", ".txt"))]
    cards = []
    for f in files:
        p = os.path.join(CORPUS, f)
        card = card_xlsx(p) if f.endswith(".xlsx") else card_txt(p)
        cards.append(f + " tipo " + role_of(f) + " " + card)
    bm = BM25([(files[i], tok(cards[i])) for i in range(len(files))])
    print("  embedding %d cards ..." % len(files))
    mat = embed(cards)

    def mrr(order_files, targets):
        return next((1.0 / i for i, f in enumerate(order_files, 1) if f in targets), 0.0)

    sums = {"BM25-card": 0.0, "DENSE-card": 0.0, "HYBRID-card": 0.0}
    for q, targets in CONCEPT_QUERIES:
        qv = embed([q])[0]
        bm_files = [f for f, _ in bm.rank(q)]
        dn_idx = dense_rank(qv, mat)
        dn_files = [files[i] for i in dn_idx]
        hy_files = [files[i] for i in rrf([files.index(f) for f in bm_files], dn_idx)]
        sums["BM25-card"] += mrr(bm_files, targets)
        sums["DENSE-card"] += mrr(dn_files, targets)
        sums["HYBRID-card"] += mrr(hy_files, targets)
    n = len(CONCEPT_QUERIES)
    for name in ["BM25-card", "DENSE-card", "HYBRID-card"]:
        print("    %-12s MRR %.3f" % (name, sums[name] / n))


def main():
    exp_a_passage()
    exp_b_routing()


if __name__ == "__main__":
    main()
