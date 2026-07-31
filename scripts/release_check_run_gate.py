#!/usr/bin/env python3
from __future__ import annotations

import argparse
import json
import pathlib
import sys
from typing import Any


REQUIRED_CHECK = "Production readiness bundle"


def validate(payload: dict[str, Any]) -> None:
    runs = payload.get("check_runs")
    if not isinstance(runs, list):
        raise ValueError("check_runs are missing")
    candidates = [
        run
        for run in runs
        if isinstance(run, dict) and run.get("name") == REQUIRED_CHECK
    ]
    if not candidates:
        raise ValueError(f"required check is missing: {REQUIRED_CHECK}")
    successful = [
        run
        for run in candidates
        if run.get("status") == "completed" and run.get("conclusion") == "success"
    ]
    if not successful:
        raise ValueError(f"required check is not successful: {REQUIRED_CHECK}")
    if not any(run.get("app", {}).get("name") == "GitHub Actions" for run in successful):
        raise ValueError(f"required check has an unexpected producer: {REQUIRED_CHECK}")


def main() -> int:
    parser = argparse.ArgumentParser(
        description="Require Aura's exact-candidate readiness check before release"
    )
    parser.add_argument("check_runs_json", type=pathlib.Path)
    args = parser.parse_args()
    try:
        payload = json.loads(args.check_runs_json.read_text(encoding="utf-8"))
        validate(payload)
    except (OSError, json.JSONDecodeError, ValueError) as exc:
        print(f"release-check-run-gate: FAIL: {exc}", file=sys.stderr)
        return 1
    print(f"release-check-run-gate: PASS: {REQUIRED_CHECK}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
