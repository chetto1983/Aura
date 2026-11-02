"""Step 4: the numbers FINDINGS.md quotes, from gate.jsonl.

Tokens are EmbeddingGemma's (the counter Aura's passages are cut by), recorded per passage by
dump_passages.py -- not what a given chat model would bill. The gate's threshold is the
2026-10-25 spike's untuned 0.5; a sweep is printed beside it, and reading a better threshold off
it would be in-sample.
"""

import collections
import json
import statistics
import sys

THRESHOLD = 0.5


def auroc(positives: list[float], negatives: list[float]) -> float:
    pairs = [(p > n) + 0.5 * (p == n) for p in positives for n in negatives]
    return sum(pairs) / len(pairs)


def at(rows_by_q: dict, threshold: float) -> dict:
    total = kept = 0
    lost, kept_answer, abstained_right = [], 0, 0
    reachable = [q for q, rows in rows_by_q.items() if any(r["answers"] for r in rows)]
    for q, rows in rows_by_q.items():
        total += sum(r["tokens"] for r in rows)
        kept += sum(r["tokens"] for r in rows if r["d1"] >= threshold)
        if q in reachable:
            if any(r["answers"] and r["d1"] >= threshold for r in rows):
                kept_answer += 1
            else:
                lost.append(q)
        elif not any(r["d1"] >= threshold for r in rows):
            abstained_right += 1
    return {"token_delta": kept / total - 1, "reachable": len(reachable), "kept_answer": kept_answer,
            "lost": lost, "unreachable": len(rows_by_q) - len(reachable), "abstained_right": abstained_right,
            "false_accepts": sum(r["d1"] >= threshold and not r["answers"] for rows in rows_by_q.values() for r in rows)}


def main(gate_path: str) -> None:
    rows = [json.loads(line) for line in open(gate_path, encoding="utf-8")]
    by_q = collections.defaultdict(list)
    for r in rows:
        by_q[r["qid"]].append(r)
    pos = [r for r in rows if r["answers"]]
    neg = [r for r in rows if not r["answers"]]
    print(f"{len(by_q)} questions, {len(rows)} (question, passage) pairs, {len(pos)} answering, {len(neg)} not")
    print(f"AUROC answering vs not: d1 {auroc([r['d1'] for r in pos], [r['d1'] for r in neg]):.3f}, "
          f"BM25 {auroc([r['bm25'] for r in pos], [r['bm25'] for r in neg]):.3f}")
    print(f"d1 on answering passages: min {min(r['d1'] for r in pos):.2f}, median {statistics.median(r['d1'] for r in pos):.2f}; "
          f"on the rest: median {statistics.median(r['d1'] for r in neg):.2f}, max {max(r['d1'] for r in neg):.2f}")
    print(f"d1 latency per decision: median {statistics.median(r['d1_seconds'] for r in rows):.1f}s, "
          f"max {max(r['d1_seconds'] for r in rows):.1f}s; input tokens median {statistics.median(r['d1_input_tokens'] for r in rows)}")

    print("\nthreshold  token_delta  answer_kept  lost  false_accepts  unreachable_abstained")
    for threshold in (0.1, 0.2, 0.3, 0.5, 0.7, 0.9):
        m = at(by_q, threshold)
        print(f"{threshold:9.1f}  {m['token_delta']:+11.0%}  {m['kept_answer']:>5}/{m['reachable']:<5}  "
              f"{len(m['lost']):>4}  {m['false_accepts']:>13}  {m['abstained_right']:>5}/{m['unreachable']}")
    m = at(by_q, THRESHOLD)
    print(f"\nat {THRESHOLD}: questions whose answering passage the gate dropped: {m['lost']}")
    by_cat = collections.defaultdict(lambda: [0, 0])
    for q, qrows in by_q.items():
        if any(r["answers"] for r in qrows):
            cat = qrows[0]["category"]
            by_cat[cat][1] += 1
            by_cat[cat][0] += any(r["answers"] and r["d1"] >= THRESHOLD for r in qrows)
    print("answer kept per category: " + ", ".join(f"{c} {k}/{n}" for c, (k, n) in sorted(by_cat.items())))
    print("\nper question (rank:doc#ordinal tokens d1, * = answering):")
    for q, qrows in by_q.items():
        cells = " ".join(f"{r['rank']}:{r['doc'][:18]}#{r['ordinal']} {r['tokens']}t {r['d1']:.2f}{'*' if r['answers'] else ''}"
                         for r in qrows)
        print(f"{q:28} {cells}")


if __name__ == "__main__":
    main(sys.argv[1])
