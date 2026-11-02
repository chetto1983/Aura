"""The machine-learning arm beside d1: a class centroid over EmbeddingGemma vectors, and a fusion.

The centroid is what ArcadeDB answers with `vector.avg` and `vector.cosinesimilarity` (the arm that
won docs/superpowers/verification/2026-11-02-mail-classification-spike.md). Here it is computed in
Python over the same vectors, which is the same arithmetic, so no database is needed to measure it.

For k labelled examples per class -- drawn from the rows bench.py did NOT sample, five draws each --
it reports the centroid alone and its fusion with a bench.py run of d1:

    p_fused(c) ∝ p_d1(c)^(1-w) · softmax(cos(c) / TAU)^w,   w = k / (k + K0)

so d1 decides on day one and the centroid takes over as corrections accumulate. TAU and K0 are set
by hand before looking at results, not fitted: the fused numbers are a probe, not a calibration.
Vectors carry Aura's stored-document prefix, the way ArcadeDB holds them.
"""

import json
import math
import random
import sys
import urllib.request

import bench

PREFIX = "title: none | text: "
TAU = 0.05
K0 = 5
DRAWS = 5
KS = (1, 3, 5, 10, 20)


def embed(url: str, texts: list[str]) -> list[list[float]]:
    out = []
    for start in range(0, len(texts), 32):
        body = json.dumps({"input": [PREFIX + t for t in texts[start:start + 32]]}).encode()
        request = urllib.request.Request(url + "/v1/embeddings", data=body, headers={"Content-Type": "application/json"})
        with urllib.request.urlopen(request, timeout=600) as response:
            for item in json.load(response)["data"]:
                v = item["embedding"]
                norm = math.sqrt(sum(x * x for x in v)) or 1.0
                out.append([x / norm for x in v])
    return out


def centroid(vectors: list[list[float]]) -> list[float]:
    mean = [sum(xs) / len(vectors) for xs in zip(*vectors)]
    norm = math.sqrt(sum(x * x for x in mean)) or 1.0
    return [x / norm for x in mean]


def softmax(scores: dict[str, float]) -> dict[str, float]:
    top = max(scores.values())
    exp = {c: math.exp((s - top) / TAU) for c, s in scores.items()}
    total = sum(exp.values())
    return {c: e / total for c, e in exp.items()}


def fuse(d1: dict[str, float], cos: dict[str, float], k: int) -> str:
    w = k / (k + K0)
    soft = softmax(cos)
    return max(cos, key=lambda c: (1 - w) * math.log(max(d1.get(c, 0.0), 1e-9)) + w * math.log(max(soft[c], 1e-9)))


def run(test: list[dict], pool: list[tuple[str, str]], test_vectors, pool_vectors, d1_probs, gold_of) -> None:
    classes = sorted({label for _, label in pool})
    print(f"  d1 alone: {sum(max(d1_probs[i], key=d1_probs[i].get) == gold_of(t) for i, t in enumerate(test))}/{len(test)}")
    for k in KS:
        alone = fused = 0
        for draw in range(DRAWS):
            pick = random.Random(bench.SEED + 1000 * k + draw)
            cents = {}
            for c in classes:
                members = [j for j, (_, label) in enumerate(pool) if label == c]
                cents[c] = centroid([pool_vectors[j] for j in pick.sample(members, min(k, len(members)))])
            for i, t in enumerate(test):
                cos = {c: sum(a * b for a, b in zip(test_vectors[i], v)) for c, v in cents.items()}
                alone += max(cos, key=cos.get) == gold_of(t)
                fused += fuse(d1_probs[i], cos, k) == gold_of(t)
        n = len(test) * DRAWS
        print(f"  k={k:>2}: centroid {alone / n:.1%}, fused with d1 {fused / n:.1%} (w={k / (k + K0):.2f}, {DRAWS} draws)")


def main(embed_url: str, spam_csv: str, ticket_xlsx: str, d1_run: str) -> None:
    d1 = [json.loads(line) for line in open(d1_run, encoding="utf-8")]

    tickets = bench.ticket_items(ticket_xlsx, 10)
    taken = {t["row"] for t in tickets}
    pool = [(r[2], r[1]) for i, r in bench.italian_tickets(bench.ticket_rows(ticket_xlsx)) if i not in taken]
    by_row = {r["row"]: r for r in d1 if r["set"] == "tickets"}
    print(f"tickets, subcategory ({len(tickets)} test, {len(pool)} in the pool):")
    run(tickets, pool, embed(embed_url, [t["text"] for t in tickets]), embed(embed_url, [p[0] for p in pool]),
        [by_row[t["row"]]["subcategory_probs"] for t in tickets], lambda t: t["subcategory"])

    spam = bench.spam_items(spam_csv, 100)
    taken = {s["row"] for s in spam}
    rows = list(bench.csv.DictReader(open(spam_csv, encoding="utf-8")))
    clean = [(r["text"], "spam" if r["labels"] == "1" else "ham") for i, r in bench.clean_spam(rows) if i not in taken]
    clean = random.Random(bench.SEED).sample(clean, 2000)
    by_row = {r["row"]: r for r in d1 if r["set"] == "spam"}
    print(f"spam ({len(spam)} test, {len(clean)} in the pool):")
    run(spam, clean, embed(embed_url, [s["text"] for s in spam]), embed(embed_url, [p[0] for p in clean]),
        [{"spam": by_row[s["row"]]["p"], "ham": 1 - by_row[s["row"]]["p"]} for s in spam],
        lambda s: "spam" if s["gold"] else "ham")


if __name__ == "__main__":
    main(*sys.argv[1:5])
