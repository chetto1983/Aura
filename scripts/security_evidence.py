#!/usr/bin/env python3
from __future__ import annotations

import argparse
import datetime as dt
import json
import pathlib
import re
import shutil
import subprocess
import sys
from typing import Any

from evidence_metadata import candidate_commit


CODEQL_CHECKS = {
    "Analyze (go)": "go",
    "Analyze (javascript-typescript)": "javascript-typescript",
}
STRICT_TESTS = {
    "TestRuntimeProfileFieldsLoad",
    "TestInjectionSuiteDestructiveEffectsAreWithheldInProduction",
    "TestInjectionSuiteOrdinaryMutationsRequireIdempotencyButNotApproval",
    "TestInjectionSuiteReadsRemainAvailableInProduction",
    "TestApproveProductionDenies",
    "TestRouteApproveProductionDeniesEvenWithLedgerApproval",
    "TestGatewayApproveChallengeRefusedUnderProduction",
}
REQUIRED_CHECKS = ("codeql", "govulncheck", "workflow_pin", "strict_profile")


def validate_codeql(payload: dict[str, Any]) -> dict[str, Any]:
    runs = payload.get("check_runs")
    if not isinstance(runs, list):
        raise ValueError("CodeQL check_runs are missing")
    indexed = {
        run.get("name"): run for run in runs if isinstance(run, dict) and run.get("name")
    }
    missing = sorted(set(CODEQL_CHECKS) - indexed.keys())
    if missing:
        raise ValueError(f"CodeQL checks are missing: {missing}")
    for name in sorted(CODEQL_CHECKS):
        run = indexed[name]
        if run.get("status") != "completed" or run.get("conclusion") != "success":
            raise ValueError(f"CodeQL check {name} did not succeed")
        if run.get("app", {}).get("name") != "GitHub Actions":
            raise ValueError(f"CodeQL check {name} has unexpected producer")
    return {"languages": sorted(CODEQL_CHECKS.values())}


def validate_strict_test_output(output: str) -> dict[str, Any]:
    passed = set()
    for line in output.splitlines():
        try:
            event = json.loads(line)
        except json.JSONDecodeError:
            continue
        if event.get("Action") == "pass" and event.get("Test") in STRICT_TESTS:
            passed.add(event["Test"])
    missing = sorted(STRICT_TESTS - passed)
    if missing:
        raise ValueError(f"strict-profile tests missing or failed: {missing}")
    return {"tests_passed": len(passed)}


def run_command(
    check_id: str,
    command: list[str],
    repo: pathlib.Path,
    timeout: float,
) -> dict[str, Any]:
    started = dt.datetime.now(dt.timezone.utc)
    try:
        completed = subprocess.run(
            command,
            cwd=repo,
            text=True,
            stdout=subprocess.PIPE,
            stderr=subprocess.STDOUT,
            check=False,
            timeout=timeout,
        )
        passed = completed.returncode == 0
        output = completed.stdout
    except (OSError, subprocess.TimeoutExpired) as exc:
        passed = False
        output = str(exc)
    return {
        "id": check_id,
        "executed": True,
        "passed": passed,
        "duration_ms": (
            dt.datetime.now(dt.timezone.utc) - started
        ).total_seconds()
        * 1000,
        "command": command,
        "detail": output[-4000:],
        "_output": output,
    }


def build_report(candidate: str, checks: list[dict[str, Any]]) -> dict[str, Any]:
    indexed = {str(check.get("id", "")): check for check in checks}
    missing = sorted(set(REQUIRED_CHECKS) - indexed.keys())
    if missing:
        raise ValueError(f"security checks are missing: {missing}")
    passed = all(
        indexed[check_id].get("executed") is True
        and indexed[check_id].get("passed") is True
        for check_id in REQUIRED_CHECKS
    )
    sanitized = []
    for check in checks:
        copy = dict(check)
        copy.pop("_output", None)
        sanitized.append(copy)
    return {
        "schema_version": 1,
        "generated_at": dt.datetime.now(dt.timezone.utc).isoformat(),
        "candidate_commit": candidate,
        "passed": passed,
        "unresolved_release_blocking_alerts": 0 if passed else 1,
        "checks": sanitized,
    }


def collect(args: argparse.Namespace) -> dict[str, Any]:
    repo = pathlib.Path(__file__).resolve().parents[1]
    candidate = candidate_commit(repo)
    try:
        payload = json.loads(args.check_runs_json.read_text(encoding="utf-8"))
        codeql_evidence = validate_codeql(payload)
        codeql = {
            "id": "codeql",
            "executed": True,
            "passed": True,
            **codeql_evidence,
        }
    except (OSError, json.JSONDecodeError, ValueError) as exc:
        codeql = {
            "id": "codeql",
            "executed": True,
            "passed": False,
            "detail": str(exc),
        }

    workflow = run_command(
        "workflow_pin",
        ["bash", "scripts/workflow_pin_gate.sh"],
        repo,
        args.timeout_seconds,
    )
    govuln = shutil.which("govulncheck")
    if govuln:
        packages = subprocess.run(
            ["bash", "scripts/go_packages.sh"],
            cwd=repo,
            text=True,
            stdout=subprocess.PIPE,
            stderr=subprocess.PIPE,
            check=True,
            timeout=60,
        ).stdout.split()
        vulnerability = run_command(
            "govulncheck", [govuln, *packages], repo, args.timeout_seconds
        )
    else:
        vulnerability = {
            "id": "govulncheck",
            "executed": False,
            "passed": False,
            "detail": "govulncheck is not on PATH",
        }

    strict_regex = "^(" + "|".join(sorted(map(re.escape, STRICT_TESTS))) + ")$"
    strict = run_command(
        "strict_profile",
        [
            "go",
            "test",
            "-json",
            "-count=1",
            "./internal/config",
            "./internal/gateway",
            "-run",
            strict_regex,
        ],
        repo,
        args.timeout_seconds,
    )
    if strict["passed"]:
        try:
            strict.update(validate_strict_test_output(strict["_output"]))
        except ValueError as exc:
            strict["passed"] = False
            strict["detail"] = str(exc)
    return build_report(candidate, [codeql, vulnerability, workflow, strict])


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Aura exact-candidate security evidence")
    parser.add_argument("--check-runs-json", type=pathlib.Path, required=True)
    parser.add_argument("--timeout-seconds", type=float, default=1200.0)
    parser.add_argument(
        "--output",
        type=pathlib.Path,
        default=pathlib.Path("artifacts/production-readiness/security-report.json"),
    )
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    try:
        report = collect(args)
    except (RuntimeError, ValueError, OSError, subprocess.SubprocessError) as exc:
        print(f"security-evidence: FAIL: {exc}", file=sys.stderr)
        return 1
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(json.dumps(report, indent=2) + "\n", encoding="utf-8")
    if not report["passed"]:
        failed = [
            check["id"] for check in report["checks"] if not check.get("passed")
        ]
        print(f"security-evidence: FAIL: {failed}", file=sys.stderr)
        return 1
    print("security-evidence: PASS: CodeQL + govulncheck + pins + strict profile")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
