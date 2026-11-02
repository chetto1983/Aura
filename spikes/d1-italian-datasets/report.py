"""Accuracy, ranking and calibration for bench.py runs.

Calibration is reported as Brier score and a 10-bin expected calibration error (ECE), the measures
rizzo-flow publishes beside accuracy: a decision model's probability is only usable as a threshold
or in a fusion if it means what it says.
"""

import json
import sys


def auroc(pos: list[float], neg: list[float]) -> float:
    return sum((p > n) + 0.5 * (p == n) for p in pos for n in neg) / (len(pos) * len(neg))


def ece(pairs: list[tuple[float, bool]], bins: int = 10) -> float:
    total = 0.0
    for b in range(bins):
        inside = [(p, y) for p, y in pairs if b / bins <= p < (b + 1) / bins or (b == bins - 1 and p == 1.0)]
        if inside:
            total += len(inside) * abs(sum(p for p, _ in inside) / len(inside) - sum(y for _, y in inside) / len(inside))
    return total / len(pairs)


def main(path: str) -> None:
    rows = [json.loads(line) for line in open(path, encoding="utf-8")]
    spam = [r for r in rows if r["set"] == "spam"]
    pairs = [(r["p"], r["gold"]) for r in spam]
    print(f"spam ({len(spam)}): accuracy at 0.5 {sum((p >= 0.5) == y for p, y in pairs)}/{len(pairs)}, "
          f"AUROC {auroc([p for p, y in pairs if y], [p for p, y in pairs if not y]):.3f}, "
          f"Brier {sum((p - y) ** 2 for p, y in pairs) / len(pairs):.3f}, ECE {ece(pairs):.3f}")
    tickets = [r for r in rows if r["set"] == "tickets"]
    for field in ("category", "subcategory"):
        hits = [(r[field + "_p"] or 0.0, r[field] == r["gold_" + field]) for r in tickets]
        line = f"tickets {field} ({len(hits)}): {sum(h for _, h in hits)}/{len(hits)}, ECE of confidence {ece(hits):.3f}"
        for floor in (0.5, 0.7, 0.9):
            kept = [h for p, h in hits if p >= floor]
            line += f" | conf>={floor}: {sum(kept)}/{len(kept)} decided"
        print(line)
    secs = sorted(r["seconds"] for r in rows)
    print(f"latency median {secs[len(secs) // 2]:.2f}s, max {secs[-1]:.2f}s")


if __name__ == "__main__":
    main(sys.argv[1])
