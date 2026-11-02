"""The numbers FINDINGS.md quotes for labels.py runs: one column per run, d1 against the baselines.

Type and counterparty are exact matches against the gold library. Tags are scored per (document,
tag) at the untuned 0.5 threshold, on the office corpus only: its library was written with tags,
complex_corpus's gold was not. The scanned PDF has no text, so its card is nearly empty: it is
kept, because an operator's library holds such files too.
"""

import json
import statistics
import sys

THRESHOLD = 0.5


def score(rows: list[dict], predicted: str, gold: str) -> str:
    hit = sum(r[predicted] == r[gold] for r in rows)
    return f"{hit}/{len(rows)}"


def tags(rows: list[dict], key: str) -> str:
    """Positives found and false positives: 4 of 68 (document, tag) pairs are positive, so a plain
    agreement count rewards answering no to everything."""
    found = positives = false = 0
    for r in rows:
        if r["corpus"] != "corpus":
            continue  # gold_complex.json assigns types only: its "no tag" is unknown, not a negative
        for tag in r["d1_tags"]:
            want = tag in r["gold_tags"]
            got = r[key][tag] >= THRESHOLD if key == "d1_tags" else tag in r[key]
            positives += want
            found += want and got
            false += got and not want
    return f"{found}/{positives} found, {false} false"


def main(paths: list[str]) -> None:
    for path in paths:
        rows = [json.loads(line) for line in open(path, encoding="utf-8")]
        print(f"== {path}")
        for condition in ("named", "blind"):
            part = [r for r in rows if r["condition"] == condition]
            print(f"{condition:6} type: d1 {score(part, 'd1_type', 'gold_type')}, rule {score(part, 'rule_type', 'gold_type')}, "
                  f"aura today 0/{len(part)} (no rule, no type) | "
                  f"counterparty: d1 {score(part, 'd1_counterparty', 'gold_counterparty')}, "
                  f"rule {score(part, 'rule_counterparty', 'gold_counterparty')}, "
                  f"extractor {score(part, 'extractor_counterparty', 'gold_counterparty')} | "
                  f"tags: d1 {tags(part, 'd1_tags')}, rule {tags(part, 'rule_tags')}")
            wrong = [f"{r['doc']}: {r['d1_type']} (gold {r['gold_type']})" for r in part if r["d1_type"] != r["gold_type"]]
            print(f"       d1 type errors: {wrong}")
        print(f"latency median {statistics.median(r['seconds'] for r in rows):.2f}s, max {max(r['seconds'] for r in rows):.2f}s, "
              f"input tokens median {statistics.median(r['input_tokens'] for r in rows)}")


if __name__ == "__main__":
    main(sys.argv[1:])
