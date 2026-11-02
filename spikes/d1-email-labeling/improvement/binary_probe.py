"""Development diagnostic using eight native noul questions instead of one choice."""

import argparse
import json
import random
import time
from pathlib import Path

import sweep


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--out", type=Path, required=True)
    args = parser.parse_args()
    out = args.out
    manifest = json.loads((out / "manifest.json").read_text())
    candidates = sweep.read_cases(sweep.HERE.parent / "emails.tsv", "IT")
    cases = [case for case in candidates if case["id"] in manifest["development_ids"]]
    questions = {
        label: {"type": "noul", "instructions": f"Is the main purpose of this email described by this category? {description}"}
        for label, description in zip(sweep.variants.LABELS, sweep.variants.SHORT_EN)
    }
    sweep.benchmark.save_json(out / "binary.config.json", {"questions": questions, "decision_rule": "argmax of the eight independent yes probabilities; not a normalized class distribution"})
    rows = []
    random.Random(20261102).shuffle(cases)
    with (out / "binary.predictions.jsonl").open("x", encoding="utf-8") as stream:
        for case in cases:
            start = time.perf_counter()
            raw = sweep.benchmark.request_json("http://127.0.0.1:18086/v1/systemone", {"state": f"Oggetto: {case['subject']}\n\n{case['body']}", "questions": questions})
            scores = {key: answer["noul"] for key, answer in raw["answers"].items()}
            assert len(scores) == 8 and all(0 <= score <= 1 for score in scores.values())
            assert raw["usage"]["output_tokens"] == 0
            row = {"id": case["id"], "gold": case["label"], "difficulty": case["difficulty"], "subject": case["subject"],
                   "predicted": max(scores, key=scores.get), "independent_yes_scores": scores,
                   "latency_ms": (time.perf_counter()-start)*1000, "raw_response": raw}
            rows.append(row)
            stream.write(json.dumps(row, ensure_ascii=False) + "\n")
            if len(rows) % 16 == 0:
                print(f"binary: {len(rows)}/64, {sum(r['gold']==r['predicted'] for r in rows)} correct", flush=True)
    summary = sweep.benchmark.summarize(rows)
    summary.pop("selective_top_probability")
    sweep.benchmark.save_json(out / "binary.summary.json", summary)
    print(f"binary development accuracy {summary['correct']}/64 = {summary['accuracy']:.2%}", flush=True)


if __name__ == "__main__":
    main()
