#!/usr/bin/env python3
"""Run Aura's deterministic capability contract and emit machine-readable evidence."""

from __future__ import annotations

import argparse
import dataclasses
import datetime as dt
import json
import pathlib
import re
import subprocess
import sys
import time
from collections import defaultdict
from typing import Iterable

from evidence_metadata import candidate_commit


@dataclasses.dataclass(frozen=True)
class Scenario:
    scenario_id: str
    capability_class: str
    polarity: str
    package: str
    test: str


SCENARIOS = (
    Scenario("shell.exec.routed", "shell_containment", "positive", "./internal/agent/tools", "TestShellExec_RoutedRunsInBox"),
    Scenario("shell.exec.fail_closed", "shell_containment", "negative", "./internal/agent/tools", "TestShellExec_FailClosedNoHostFallback"),
    Scenario("fs.workspace.allowed", "filesystem_ownership", "positive", "./internal/agent/tools", "TestSendFileExecuteSetsArtifactMeta"),
    Scenario("fs.workspace.escape_denied", "filesystem_ownership", "negative", "./internal/agent/tools", "TestSendFileFencesToBoxWorkspace"),
    # These three named the bespoke client's tests, which phase 45.1 deleted along with the
    # client itself (internal/mcp/{http_client,client,client_timeout}_test.go). The
    # capabilities did not go away with them -- the SDK-backed client owes exactly the same
    # three -- so they are re-pointed rather than dropped: a scoreboard that loses a row
    # every time an implementation is swapped stops measuring the system and starts
    # measuring the file names.
    Scenario("mcp.initialize.valid", "mcp_contract", "positive", "./internal/mcp", "TestOpenSDKSessionStreamableHTTP"),
    Scenario("mcp.domain_error.typed", "mcp_contract", "negative", "./internal/mcp", "TestDecodeToolResultRejectsExplicitDomainFailure"),
    Scenario("mcp.timeout.bounded", "mcp_contract", "negative", "./internal/mcp", "TestOpenSDKSessionHTTPHonoursCallerDeadline"),
    # Re-pointed, not dropped, for the reason stated above. The witness used to be
    # TestMountForIdentityRemoteTrustOverrideIgnored, which asserted a per-identity
    # overlay could not elevate a remote server's trust. That overlay was deleted for
    # having no production caller, so the guarantee is now stronger than the old test
    # measured: there is no per-identity elevation PATH at all. What still needs a
    # witness is the half that remains reachable — an HTTP-shaped server must never be
    # auto-promoted to the runnable remote_http class and must resolve to blocked,
    # which only an explicit admin catalog entry can change. That is F-013.
    Scenario("mcp.remote_trust.blocked", "mcp_contract", "negative", "./internal/mcp", "TestClassify_RemoteEmptyTrust"),
    Scenario("memory.cli.mapping", "memory_contract", "positive", "./cmd/aura", "TestMemoryVerbMapping"),
    Scenario("memory.cli.invalid_rejected", "memory_contract", "negative", "./cmd/aura", "TestMemoryVerbMappingNegativeCases"),
    Scenario("memory.readiness.functional", "memory_contract", "positive", "./cmd/aura", "TestMemoryReadinessCheckRunsAnIsolatedFunctionalSearch"),
    Scenario("memory.readiness.semantic", "memory_contract", "negative", "./cmd/aura", "TestMemoryReadinessCheckRejectsSemanticAndTransportFailure"),
    Scenario("pause.resume.claim_first", "pause_resume_atomicity", "positive", "./internal/runner", "TestSplitResumeCommitter_CommitResume_ClaimsBeforeAppend"),
    Scenario("pause.resume.partial_batch_denied", "pause_resume_atomicity", "negative", "./internal/runner", "TestResumeBatch_PartiallyResolvedAppendsNothing"),
    Scenario("terminal.single.finalizes", "terminal_error_truth", "positive", "./internal/agent", "TestDispatch_SingleTerminalNoSiblingRunsTerminal"),
    Scenario("terminal.mutating_sibling_denied", "terminal_error_truth", "negative", "./internal/agent", "TestDispatch_TerminalRejectExclusivity"),
    Scenario("mutation.operation.stable", "idempotent_mutation", "positive", "./internal/agent", "TestExecToolDerivesStableChildFromHTTPMutation"),
    Scenario("mutation.domain_rejection.replayed", "idempotent_mutation", "negative", "./internal/agent", "TestExecToolReplaysDeterministicDomainRejectionAsError"),
    Scenario("workflow.completion.accepts", "workflow_completion", "positive", "./internal/agent", "TestCompletionGate_Done_AcceptsAfterCritic"),
    Scenario("workflow.retry_exhausted.truthful", "workflow_completion", "negative", "./internal/agent", "TestSynthesizeRetryExhaustedReturnsFinalError"),
)


def validate_manifest(scenarios: Iterable[Scenario]) -> None:
    by_class: dict[str, set[str]] = defaultdict(set)
    ids: set[str] = set()
    tests: set[tuple[str, str]] = set()
    for scenario in scenarios:
        if scenario.scenario_id in ids:
            raise ValueError(f"duplicate scenario id: {scenario.scenario_id}")
        if (scenario.package, scenario.test) in tests:
            raise ValueError(f"duplicate package/test: {scenario.package} {scenario.test}")
        if scenario.polarity not in {"positive", "negative"}:
            raise ValueError(f"invalid polarity for {scenario.scenario_id}: {scenario.polarity}")
        ids.add(scenario.scenario_id)
        tests.add((scenario.package, scenario.test))
        by_class[scenario.capability_class].add(scenario.polarity)
    incomplete = sorted(name for name, polarities in by_class.items() if polarities != {"positive", "negative"})
    if incomplete:
        raise ValueError(f"classes missing positive/negative coverage: {', '.join(incomplete)}")


def parse_test_results(lines: Iterable[str]) -> dict[str, dict[str, object]]:
    results: dict[str, dict[str, object]] = {}
    for line in lines:
        try:
            event = json.loads(line)
        except json.JSONDecodeError:
            continue
        test = event.get("Test")
        action = event.get("Action")
        if not isinstance(test, str) or action not in {"pass", "fail", "skip"}:
            continue
        results[test] = {
            "status": action,
            "elapsed_seconds": float(event.get("Elapsed", 0.0)),
        }
    return results


def run_package(package: str, tests: list[str], race: bool) -> tuple[int, dict[str, dict[str, object]], int]:
    pattern = "^(" + "|".join(re.escape(test) for test in tests) + ")$"
    command = ["go", "test", "-json", "-count=1"]
    if race:
        command.append("-race")
    command.extend(["-run", pattern, package])
    started = time.perf_counter()
    completed = subprocess.run(command, capture_output=True, text=True, check=False)
    elapsed_ms = round((time.perf_counter() - started) * 1000)
    if completed.stderr:
        sys.stderr.write(completed.stderr)
    if completed.returncode != 0:
        sys.stderr.write(completed.stdout)
    return completed.returncode, parse_test_results(completed.stdout.splitlines()), elapsed_ms


def build_report(race: bool) -> tuple[dict[str, object], bool]:
    validate_manifest(SCENARIOS)
    grouped: dict[str, list[Scenario]] = defaultdict(list)
    for scenario in SCENARIOS:
        grouped[scenario.package].append(scenario)

    package_runs: dict[str, dict[str, object]] = {}
    observed: dict[tuple[str, str], dict[str, object]] = {}
    for package, scenarios in sorted(grouped.items()):
        returncode, results, elapsed_ms = run_package(package, [item.test for item in scenarios], race)
        package_runs[package] = {"returncode": returncode, "elapsed_ms": elapsed_ms}
        for test, result in results.items():
            observed[(package, test)] = result

    rows: list[dict[str, object]] = []
    for scenario in SCENARIOS:
        result = observed.get((scenario.package, scenario.test))
        status = str(result["status"]) if result else "missing"
        rows.append(
            {
                **dataclasses.asdict(scenario),
                "status": status,
                "elapsed_seconds": result["elapsed_seconds"] if result else 0.0,
            }
        )

    executed = sum(row["status"] in {"pass", "fail"} for row in rows)
    failed = sum(row["status"] != "pass" for row in rows)
    skipped = sum(row["status"] == "skip" for row in rows)
    missing = sum(row["status"] == "missing" for row in rows)
    go_version = subprocess.run(
        ["go", "version"], capture_output=True, text=True, check=False
    ).stdout.strip()
    report: dict[str, object] = {
        "schema_version": 1,
        "generated_at": dt.datetime.now(dt.timezone.utc).isoformat(),
        "candidate_commit": candidate_commit(),
        "race": race,
        "go_version": go_version,
        "totals": {
            "declared": len(rows),
            "executed": executed,
            "passed": len(rows) - failed,
            "failed": failed,
            "skipped": skipped,
            "missing": missing,
            "blocked": 0,
        },
        "package_runs": package_runs,
        "scenarios": rows,
    }
    return report, failed == 0 and all(run["returncode"] == 0 for run in package_runs.values())


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--race", action="store_true", help="run the selected Go tests with -race")
    parser.add_argument(
        "--output",
        default="artifacts/production-readiness/capability-eval.json",
        help="JSON report path",
    )
    args = parser.parse_args()

    report, passed = build_report(args.race)
    output = pathlib.Path(args.output)
    output.parent.mkdir(parents=True, exist_ok=True)
    output.write_text(json.dumps(report, indent=2) + "\n", encoding="utf-8")
    totals = report["totals"]
    print(
        "capability-eval: "
        f"{totals['passed']}/{totals['declared']} passed; "
        f"failed={totals['failed']} skipped={totals['skipped']} missing={totals['missing']}; "
        f"report={output}"
    )
    return 0 if passed else 1


if __name__ == "__main__":
    raise SystemExit(main())
