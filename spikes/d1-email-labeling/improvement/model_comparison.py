"""Run an untuned model alternative with the original English question on fixed datasets."""

import argparse
import json
from pathlib import Path

import sweep


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--out", type=Path, required=True)
    parser.add_argument("--base-url", default="http://127.0.0.1:18088")
    parser.add_argument("--name", default="d1-3b")
    parser.add_argument("--model", default="LiquidAI/d1-3B-GGUF Q4_K_M")
    parser.add_argument("--revision", default="bb1e436ea78eb96a3f1acb6da865f70c2fbeb563")
    parser.add_argument("--model-sha256", default="16aff27ea2eefdc32b9897f43854a5d3170c1dc8dccb9c756905af30a4e22402")
    args = parser.parse_args()
    parent = args.out
    root = parent / args.name
    root.mkdir(exist_ok=False)
    confirmation = json.loads((parent / "confirmation-manifest.json").read_text())
    assert confirmation["confirmation_sha256"] == sweep.sha(sweep.HERE / "confirmation.tsv")
    config = json.loads((parent / "baseline_english.config.json").read_text())
    sweep.benchmark.save_json(root / "manifest.json", {
        "model": args.model,
        "revision": args.revision,
        "model_sha256": args.model_sha256,
        "original_sha256": sweep.sha(sweep.HERE.parent / "emails.tsv"),
        "heldout_sha256": sweep.sha(sweep.HERE / "holdout.tsv"),
        "confirmation_sha256": sweep.sha(sweep.HERE / "confirmation.tsv"),
        "driver_sha256": sweep.sha(Path(__file__)),
        "config": config,
        "models": sweep.benchmark.request_json(args.base_url + "/v1/models"),
        "props": sweep.benchmark.request_json(args.base_url + "/props"),
    })
    for name, path, prefix in [
        ("confirmation40", sweep.HERE / "confirmation.tsv", "C"),
        ("holdout80", sweep.HERE / "holdout.tsv", "H"),
        ("original200", sweep.HERE.parent / "emails.tsv", "IT"),
    ]:
        print(f"Starting {name}", flush=True)
        sweep.evaluate(name, config, sweep.read_cases(path, prefix), root, args.base_url)


if __name__ == "__main__":
    main()
