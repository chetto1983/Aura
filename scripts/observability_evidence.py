#!/usr/bin/env python3
from __future__ import annotations

import argparse
import datetime as dt
import json
import pathlib
import shutil
import subprocess
import sys
import urllib.error
import urllib.request
from typing import Any

from evidence_metadata import candidate_commit


REQUIRED_CHECKS = (
    "static_contract",
    "runtime_smoke",
    "live_healthz",
    "live_readyz",
    "dashboards",
    "alerts",
    "runbooks",
)


def command_check(
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
        detail = completed.stdout[-4000:]
    except (OSError, subprocess.TimeoutExpired) as exc:
        passed = False
        detail = str(exc)
    return {
        "id": check_id,
        "executed": True,
        "passed": passed,
        "duration_ms": (
            dt.datetime.now(dt.timezone.utc) - started
        ).total_seconds()
        * 1000,
        "command": command,
        "detail": detail,
    }


def endpoint_check(check_id: str, url: str, timeout: float) -> dict[str, Any]:
    started = dt.datetime.now(dt.timezone.utc)
    status = 0
    detail = ""
    try:
        with urllib.request.urlopen(url, timeout=timeout) as response:
            status = response.status
            detail = response.read(4096).decode("utf-8", errors="replace")
        passed = status == 200
    except (OSError, urllib.error.URLError) as exc:
        passed = False
        detail = str(exc)
    return {
        "id": check_id,
        "executed": True,
        "passed": passed,
        "status": status,
        "url": url,
        "duration_ms": (
            dt.datetime.now(dt.timezone.utc) - started
        ).total_seconds()
        * 1000,
        "detail": detail,
    }


def endpoint_checks_from_load(
    path: pathlib.Path, candidate: str
) -> list[dict[str, Any]]:
    try:
        report = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as exc:
        raise ValueError(f"cannot read load evidence {path}: {exc}") from exc
    if report.get("schema_version") != 1 or report.get("passed") is not True:
        raise ValueError("load evidence did not pass")
    if report.get("candidate_commit") != candidate:
        raise ValueError("load evidence candidate does not match")
    endpoints = report.get("endpoints")
    if not isinstance(endpoints, dict):
        raise ValueError("load evidence endpoints are missing")
    checks = []
    for endpoint in ("healthz", "readyz"):
        evidence = endpoints.get(endpoint)
        if not isinstance(evidence, dict):
            raise ValueError(f"load evidence endpoint {endpoint} is missing")
        passed = (
            evidence.get("requests", 0) > 0
            and evidence.get("success_ratio") == 1
            and evidence.get("p95_ms", float("inf"))
            <= evidence.get("p95_budget_ms", -1)
        )
        checks.append(
            {
                "id": f"live_{endpoint}",
                "executed": True,
                "passed": passed,
                "source": path.as_posix(),
                "evidence": evidence,
            }
        )
    return checks


def build_report(candidate: str, checks: list[dict[str, Any]]) -> dict[str, Any]:
    indexed = [str(check.get("id", "")) for check in checks]
    duplicates = sorted({check_id for check_id in indexed if indexed.count(check_id) > 1})
    if duplicates:
        raise ValueError(f"duplicate observability checks: {duplicates}")
    missing = sorted(set(REQUIRED_CHECKS) - set(indexed))
    if missing:
        raise ValueError(f"missing observability checks: {missing}")
    required = [check for check in checks if check["id"] in REQUIRED_CHECKS]
    return {
        "schema_version": 1,
        "generated_at": dt.datetime.now(dt.timezone.utc).isoformat(),
        "candidate_commit": candidate,
        "passed": all(
            check.get("executed") is True and check.get("passed") is True
            for check in required
        ),
        "checks": checks,
    }


def resolve_powershell() -> str:
    for executable in ("pwsh", "powershell", "powershell.exe"):
        resolved = shutil.which(executable)
        if resolved:
            return resolved
    raise RuntimeError(
        "PowerShell (pwsh or powershell.exe) is required for observability verification"
    )


def collect(args: argparse.Namespace) -> dict[str, Any]:
    repo = pathlib.Path(__file__).resolve().parents[1]
    candidate = candidate_commit(repo)
    pwsh = resolve_powershell()
    timeout = args.command_timeout_seconds
    fixture = command_check(
        "negative_fixtures",
        [pwsh, "-NoProfile", "-File", "scripts/verify-observability.Tests.ps1"],
        repo,
        timeout,
    )
    static = command_check(
        "static_contract",
        [
            pwsh,
            "-NoProfile",
            "-File",
            "scripts/verify-observability.ps1",
            "-StaticOnly",
        ],
        repo,
        timeout,
    )
    static["passed"] = static["passed"] and fixture["passed"]
    static["negative_fixture"] = fixture
    runtime = command_check(
        "runtime_smoke",
        [pwsh, "-NoProfile", "-File", "scripts/verify-observability.ps1"],
        repo,
        timeout,
    )
    checks = [static, runtime]
    if args.load_report is not None:
        checks.extend(endpoint_checks_from_load(args.load_report.resolve(), candidate))
    else:
        checks.extend(
            [
                endpoint_check(
                    "live_healthz", args.health_url, args.endpoint_timeout_seconds
                ),
                endpoint_check(
                    "live_readyz", args.ready_url, args.endpoint_timeout_seconds
                ),
            ]
        )
    for check_id in ("dashboards", "alerts", "runbooks"):
        checks.append(
            {
                "id": check_id,
                "executed": static["executed"],
                "passed": static["passed"],
                "source": "static_contract",
            }
        )
    return build_report(candidate, checks)


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Aura observability release evidence")
    parser.add_argument(
        "--health-url", default="http://127.0.0.1:9080/healthz"
    )
    parser.add_argument(
        "--ready-url", default="http://127.0.0.1:9080/readyz"
    )
    parser.add_argument("--endpoint-timeout-seconds", type=float, default=5.0)
    parser.add_argument("--command-timeout-seconds", type=float, default=1200.0)
    parser.add_argument("--load-report", type=pathlib.Path)
    parser.add_argument(
        "--output",
        type=pathlib.Path,
        default=pathlib.Path(
            "artifacts/production-readiness/observability-report.json"
        ),
    )
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    try:
        report = collect(args)
    except (RuntimeError, ValueError, OSError) as exc:
        print(f"observability-evidence: FAIL: {exc}", file=sys.stderr)
        return 1
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(json.dumps(report, indent=2) + "\n", encoding="utf-8")
    if not report["passed"]:
        failed = [
            check["id"] for check in report["checks"] if not check.get("passed")
        ]
        print(
            f"observability-evidence: FAIL: checks failed: {failed}",
            file=sys.stderr,
        )
        return 1
    print(
        f"observability-evidence: PASS: {len(REQUIRED_CHECKS)}/{len(REQUIRED_CHECKS)}"
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
