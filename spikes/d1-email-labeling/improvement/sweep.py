"""Select a prompt on exposed development emails; evaluate it once on fresh held-out text."""

import argparse
import collections
import csv
import hashlib
import json
import random
import statistics
import time
from datetime import datetime, timezone
from pathlib import Path

import variants


benchmark = variants.english_prompt.benchmark
HERE = Path(__file__).resolve().parent


def read_cases(path, prefix):
    with path.open(encoding="utf-8", newline="") as stream:
        cases = list(csv.DictReader(stream, delimiter="\t"))
    for i, case in enumerate(cases, 1):
        case["id"] = f"{prefix}{i:03}"
    return cases


def sha(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def evaluate(name, config, cases, out, base_url):
    warmup = {"state": "Ciao nonna, ti telefono stasera per raccontarti come stanno i bambini.", "questions": {"label": config["question"]}}
    benchmark.save_json(out / f"{name}.warmup.json", benchmark.request_json(base_url + "/v1/systemone", warmup))
    rows = []
    order = list(cases)
    random.Random(20261102).shuffle(order)
    with (out / f"{name}.predictions.jsonl").open("w", encoding="utf-8") as stream:
        for case in order:
            start = time.perf_counter()
            payload = {"state": f"Oggetto: {case['subject']}\n\n{case['body']}", "questions": {"label": config["question"]}}
            raw = benchmark.request_json(base_url + "/v1/systemone", payload)
            elapsed = (time.perf_counter() - start) * 1000
            answer = raw["answers"]["label"]
            probabilities = {config["label_map"][key]: value for key, value in answer["probabilities"].items()}
            assert set(probabilities) == set(variants.LABELS)
            assert all(0 <= p <= 1 for p in probabilities.values()) and abs(sum(probabilities.values()) - 1) < 1e-5
            predicted = config["label_map"][answer["choice"]]
            assert predicted == max(probabilities, key=probabilities.get)
            assert raw["usage"]["output_tokens"] == 0
            row = {"id": case["id"], "gold": case["label"], "difficulty": case["difficulty"], "subject": case["subject"],
                   "predicted": predicted, "probabilities": probabilities, "top_probability": max(probabilities.values()),
                   "latency_ms": elapsed, "raw_response": raw}
            rows.append(row)
            stream.write(json.dumps(row, ensure_ascii=False) + "\n")
            stream.flush()
    summary = benchmark.summarize(rows)
    benchmark.save_json(out / f"{name}.summary.json", summary)
    print(f"{name}: {summary['correct']}/{len(rows)} = {summary['accuracy']:.2%}, macro F1 {summary['macro_f1']:.4f}, p50 {summary['latency_ms']['p50']:.1f} ms", flush=True)
    return summary


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--phase", choices=["development", "holdout"], required=True)
    parser.add_argument("--out", type=Path, required=True)
    parser.add_argument("--base-url", default="http://127.0.0.1:18086")
    args = parser.parse_args()
    if args.phase == "development":
        args.out.mkdir(parents=True, exist_ok=False)
        original = read_cases(HERE.parent / "emails.tsv", "IT")
        holdout = read_cases(HERE / "holdout.tsv", "H")
        assert len(holdout) == 80 and collections.Counter(c["label"] for c in holdout) == {label: 10 for label in variants.LABELS}
        assert len({(c["subject"], c["body"]) for c in original + holdout}) == len(original) + len(holdout)
        examples, development = [], []
        rng = random.Random(20261102)
        for label in variants.LABELS:
            group = [case for case in original if case["label"] == label]
            examples.append(group[0])
            development.extend(rng.sample([c for c in group[1:] if c["difficulty"] == "standard"], 6))
            development.extend(rng.sample([c for c in group[1:] if c["difficulty"] == "challenge"], 2))
        assert len(development) == 64 and not {c['id'] for c in development}.intersection(c['id'] for c in examples)
        configs = variants.candidates(examples)
        for name, config in configs.items():
            benchmark.save_json(args.out / f"{name}.config.json", config)
        manifest = {"created_utc": datetime.now(timezone.utc).isoformat(), "source_sha256": sha(HERE.parent / "emails.tsv"),
                    "heldout_sha256": sha(HERE / "holdout.tsv"), "driver_sha256": sha(Path(__file__)), "variants_sha256": sha(HERE / "variants.py"),
                    "selection_rule": "maximum development accuracy, then macro F1, then shorter question text",
                    "development_ids": [c["id"] for c in development], "example_ids": [c["id"] for c in examples],
                    "candidate_names": list(configs), "server_models": benchmark.request_json(args.base_url + "/v1/models")}
        benchmark.save_json(args.out / "manifest.json", manifest)
        scores = {name: evaluate(name, config, development, args.out, args.base_url) for name, config in configs.items()}
        winner = max(configs, key=lambda name: (scores[name]["accuracy"], scores[name]["macro_f1"], -len(json.dumps(configs[name]["question"]))))
        selection = {"chosen_on_development_only": winner, "config": configs[winner], "development_scores": {name: {k: value[k] for k in ("correct", "total", "accuracy", "macro_f1")} for name, value in scores.items()}, "frozen_utc": datetime.now(timezone.utc).isoformat()}
        benchmark.save_json(args.out / "selection.json", selection)
        print(f"FROZEN selection: {winner}; held-out inference has not run.", flush=True)
    else:
        manifest = json.loads((args.out / "manifest.json").read_text())
        assert manifest["heldout_sha256"] == sha(HERE / "holdout.tsv")
        assert manifest["source_sha256"] == sha(HERE.parent / "emails.tsv")
        assert manifest["driver_sha256"] == sha(Path(__file__)) and manifest["variants_sha256"] == sha(HERE / "variants.py")
        selection = json.loads((args.out / "selection.json").read_text())
        target = args.out / "heldout"
        target.mkdir(exist_ok=False)
        heldout = read_cases(HERE / "holdout.tsv", "H")
        baseline = json.loads((args.out / "baseline_english.config.json").read_text())
        benchmark.save_json(target / "evaluation_manifest.json", {"selection_sha256": sha(args.out / "selection.json"), "heldout_sha256": sha(HERE / "holdout.tsv"), "started_utc": datetime.now(timezone.utc).isoformat(), "chosen": selection["chosen_on_development_only"], "config": selection["config"]})
        original = evaluate("baseline_english", baseline, heldout, target, args.base_url)
        improved = evaluate("selected", selection["config"], heldout, target, args.base_url)
        benchmark.save_json(target / "comparison.json", {"baseline": {k: original[k] for k in ("correct", "total", "accuracy", "macro_f1", "latency_ms", "selective_top_probability")}, "selected": {k: improved[k] for k in ("correct", "total", "accuracy", "macro_f1", "latency_ms", "selective_top_probability")}, "accuracy_difference": improved["accuracy"] - original["accuracy"], "completed_utc": datetime.now(timezone.utc).isoformat()})


if __name__ == "__main__":
    main()
