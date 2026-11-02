"""Reproduce the additional 40-email baseline before loading the alternative model."""

import argparse
import json
from pathlib import Path

import sweep


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--out", type=Path, required=True)
    parser.add_argument("--base-url", default="http://127.0.0.1:18086")
    args = parser.parse_args()
    config = json.loads((args.out / "baseline_english.config.json").read_text())
    cases = sweep.read_cases(sweep.HERE / "confirmation.tsv", "C")
    assert len(cases) == 40
    target = args.out / "confirmation"
    target.mkdir(exist_ok=False)
    sweep.benchmark.save_json(args.out / "confirmation-manifest.json", {
        "confirmation_sha256": sweep.sha(sweep.HERE / "confirmation.tsv"),
        "frozen_model": "LiquidAI/d1-3B-GGUF Q4_K_M bb1e436ea78eb96a3f1acb6da865f70c2fbeb563",
        "prompt": "original baseline English, not changed after earlier heldout results",
        "design": "40 additional emails authored before d1-3B inference; model comparison, no further tuning on these outcomes",
    })
    sweep.evaluate("600m_baseline", config, cases, target, args.base_url)


if __name__ == "__main__":
    main()
