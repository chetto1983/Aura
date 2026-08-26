from __future__ import annotations

import argparse
import datetime as dt
import hashlib
import json
import pathlib
import re
import sys
from collections.abc import Callable
from typing import Any


class GateError(RuntimeError):
    pass


def require(condition: bool, message: str) -> None:
    if not condition:
        raise GateError(message)


def passing_checks(report: dict[str, Any], required: set[str], label: str) -> None:
    checks = report.get("checks")
    require(isinstance(checks, list), f"{label}: checks must be a list")
    indexed = {
        item.get("id"): item for item in checks if isinstance(item, dict) and item.get("id")
    }
    missing = required - indexed.keys()
    require(not missing, f"{label}: missing checks {sorted(missing)}")
    for check_id in sorted(required):
        check = indexed[check_id]
        require(check.get("executed") is True, f"{label}: {check_id} did not execute")
        require(check.get("passed") is True, f"{label}: {check_id} failed")


def validate_security(report: dict[str, Any]) -> dict[str, Any]:
    require(report.get("passed") is True, "security: report did not pass")
    require(
        report.get("unresolved_release_blocking_alerts") == 0,
        "security: unresolved release-blocking alerts",
    )
    passing_checks(
        report,
        {"codeql", "govulncheck", "workflow_pin", "strict_profile"},
        "security",
    )
    return {}


def validate_coverage(report: dict[str, Any]) -> dict[str, Any]:
    percent = report.get("statements_percent")
    require(report.get("passed") is True, "coverage: report did not pass")
    require(isinstance(percent, (int, float)) and percent >= 85, "coverage: statements < 85%")
    require(report.get("scope") == "owned_internal", "coverage: unexpected scope")
    require(report.get("covered_statements", 0) > 0, "coverage: no covered statements")
    require(report.get("total_statements", 0) > 0, "coverage: no statements")
    tiers = set(report.get("tiers_executed", []))
    missing = {"db_integration"} - tiers
    require(not missing, f"coverage: missing tiers {sorted(missing)}")
    require(report.get("empty_tiers") == 0, "coverage: empty or filtered tier")

    package_coverage = report.get("package_coverage")
    require(isinstance(package_coverage, dict), "coverage: package evidence missing")
    require(package_coverage.get("passed") is True, "coverage: package policy did not pass")
    require(
        package_coverage.get("cross_package_attribution") is True,
        "coverage: package evidence lacks cross-package attribution",
    )
    require(
        package_coverage.get("policy_schema_version") == 1,
        "coverage: package policy schema_version must be 1",
    )
    target = package_coverage.get("target_percent")
    require(target == 85, "coverage: package target must be 85%")
    results = package_coverage.get("results")
    require(isinstance(results, list) and results, "coverage: package results missing")

    expected_delegated = {
        "github.com/chetto1983/aura/internal/sandbox/usersandbox": "docker_coverage",
    }
    packages: set[str] = set()
    below_target: list[str] = []
    delegated: list[dict[str, str]] = []
    at_target = 0
    for result in results:
        require(isinstance(result, dict), "coverage: package result must be an object")
        package = result.get("package")
        require(isinstance(package, str) and package, "coverage: package name missing")
        require(package not in packages, f"coverage: duplicate package result {package}")
        packages.add(package)
        require(result.get("passed") is True, f"coverage: {package} did not pass")
        require(result.get("target_percent") == target, f"coverage: {package} target mismatch")
        covered = result.get("covered_statements")
        total = result.get("total_statements")
        require(
            type(covered) is int and type(total) is int and 0 <= covered <= total and total > 0,
            f"coverage: {package} statement counts are invalid",
        )
        meets_target = covered * 100 >= target * total
        mode = result.get("mode")
        require(mode in {"target", "baseline", "delegated"}, f"coverage: {package} mode invalid")
        if mode == "target":
            require(meets_target, f"coverage: {package} is below its 85% target")
        elif mode == "baseline":
            baseline_covered = result.get("baseline_covered_statements")
            baseline_total = result.get("baseline_total_statements")
            require(
                type(baseline_covered) is int
                and type(baseline_total) is int
                and 0 < baseline_covered <= baseline_total,
                f"coverage: {package} baseline counts are invalid",
            )
            require(
                baseline_total == total,
                f"coverage: {package} baseline denominator changed",
            )
            require(
                baseline_covered * 100 < target * baseline_total,
                f"coverage: {package} baseline must be below target",
            )
            require(
                covered * baseline_total >= baseline_covered * total,
                f"coverage: {package} regressed below its exact baseline",
            )
        else:
            authority = result.get("authority")
            require(
                expected_delegated.get(package) == authority,
                f"coverage: {package} has unsupported delegated authority {authority!r}",
            )
            delegated.append({"package": package, "authority": authority})
            continue

        if meets_target:
            at_target += 1
        else:
            below_target.append(package)

    expected_delegated_rows = [
        {"package": package, "authority": authority}
        for package, authority in sorted(expected_delegated.items())
    ]
    require(
        sorted(delegated, key=lambda item: item["package"]) == expected_delegated_rows,
        "coverage: required usersandbox Docker delegation missing",
    )
    require(
        package_coverage.get("delegated_packages") == expected_delegated_rows,
        "coverage: delegated package summary mismatch",
    )
    require(
        package_coverage.get("packages_evaluated") == len(results),
        "coverage: evaluated package count mismatch",
    )
    require(
        package_coverage.get("packages_at_target") == at_target,
        "coverage: packages-at-target count mismatch",
    )
    require(
        package_coverage.get("packages_below_target") == sorted(below_target),
        "coverage: below-target package summary mismatch",
    )
    return {
        "statements_percent": percent,
        "packages_evaluated": len(results),
        "packages_below_target": len(below_target),
    }


def validate_docker_coverage(report: dict[str, Any]) -> dict[str, Any]:
    percent = report.get("statements_percent")
    covered = report.get("covered_statements")
    total = report.get("total_statements")
    require(report.get("passed") is True, "docker coverage: report did not pass")
    require(report.get("scope") == "docker_owned_internal", "docker coverage: unexpected scope")
    require(isinstance(covered, int) and covered > 0, "docker coverage: no covered statements")
    require(isinstance(total, int) and total > 0, "docker coverage: no statements")
    require(covered <= total, "docker coverage: covered statements exceed total")
    require(covered * 100 >= 85 * total, "docker coverage: exact statements < 85%")
    require(isinstance(percent, (int, float)) and percent >= 85, "docker coverage: statements < 85%")
    require(
        report.get("tiers_executed") == ["docker_integration"],
        "docker coverage: docker_integration is not the sole tier",
    )
    require(report.get("empty_tiers") == 0, "docker coverage: empty tier")
    return {"statements_percent": percent}


def validate_agent_memory(report: dict[str, Any]) -> dict[str, Any]:
    require(report.get("passed") is True, "agent memory: report did not pass")
    require(report.get("tier") == "all", "agent memory: report is not the all tier")
    require(report.get("verdict") == "PASS", "agent memory: verdict is not PASS")
    require(report.get("mrs_eligible") is True, "agent memory: report is not MRS eligible")
    score = report.get("memory_reliability_score")
    require(isinstance(score, (int, float)) and score > 96.5, "agent memory: MRS <= 96.5")
    scenarios = report.get("scenarios")
    require(isinstance(scenarios, list), "agent memory: scenarios missing")
    indexed = {
        item.get("id"): item for item in scenarios if isinstance(item, dict) and item.get("id")
    }
    coverage = indexed.get("arcadedb_package_coverage")
    require(isinstance(coverage, dict), "agent memory: ArcadeDB coverage scenario missing")
    require(coverage.get("status") == "PASS", "agent memory: ArcadeDB coverage scenario failed")
    detail = coverage.get("detail")
    require(isinstance(detail, dict), "agent memory: ArcadeDB coverage detail missing")
    percent = detail.get("actual_percent")
    covered = detail.get("covered_statements")
    total = detail.get("total_statements")
    require(isinstance(covered, int) and covered > 0, "agent memory: no covered statements")
    require(isinstance(total, int) and total > 0, "agent memory: no statements")
    require(covered <= total, "agent memory: covered statements exceed total")
    require(covered * 100 >= 85 * total, "agent memory: exact ArcadeDB statements < 85%")
    require(
        isinstance(percent, (int, float)) and percent >= 85,
        "agent memory: ArcadeDB coverage < 85%",
    )
    return {"memory_reliability_score": score, "arcadedb_statements_percent": percent}


def validate_mutation(report: dict[str, Any]) -> dict[str, Any]:
    require(report.get("passed") is True, "mutation: report did not pass")
    scopes = report.get("scopes")
    require(isinstance(scopes, list), "mutation: scopes must be a list")
    indexed = {
        item.get("id"): item for item in scopes if isinstance(item, dict) and item.get("id")
    }
    required = {
        "gateway",
        "identity_isolation",
        "profile_validation",
        "sandbox",
        "frontend",
    }
    missing = required - indexed.keys()
    require(not missing, f"mutation: missing scopes {sorted(missing)}")
    scores: dict[str, float] = {}
    for scope_id in sorted(required):
        scope = indexed[scope_id]
        score = scope.get("score_percent")
        require(scope.get("executed") is True, f"mutation: {scope_id} did not execute")
        require(
            isinstance(score, (int, float)) and score >= 70,
            f"mutation: {scope_id} score {score!r} < 70%",
        )
        scores[scope_id] = float(score)
    return {"scores": scores}


def validate_capability(report: dict[str, Any]) -> dict[str, Any]:
    totals = report.get("totals")
    require(isinstance(totals, dict), "capability: totals missing")
    declared = totals.get("declared")
    require(isinstance(declared, int) and declared > 0, "capability: no declared scenarios")
    require(totals.get("executed") == declared, "capability: not every scenario executed")
    require(totals.get("passed") == declared, "capability: not every scenario passed")
    for field in ("failed", "skipped", "missing", "blocked"):
        require(totals.get(field) == 0, f"capability: {field} is non-zero")

    scenarios = report.get("scenarios")
    require(isinstance(scenarios, list), "capability: scenarios missing")
    require(len(scenarios) == declared, "capability: scenario count differs from declared")
    required_classes = {
        "shell_containment",
        "filesystem_ownership",
        "mcp_contract",
        "memory_contract",
        "pause_resume_atomicity",
        "terminal_error_truth",
        "idempotent_mutation",
        "workflow_completion",
    }
    polarities: dict[str, set[str]] = {name: set() for name in required_classes}
    for scenario in scenarios:
        require(isinstance(scenario, dict), "capability: malformed scenario")
        require(scenario.get("status") == "pass", "capability: scenario did not pass")
        capability_class = scenario.get("capability_class")
        if capability_class in polarities:
            polarities[capability_class].add(str(scenario.get("polarity")))
    missing = [
        name for name, values in polarities.items() if not {"positive", "negative"} <= values
    ]
    require(not missing, f"capability: classes lack positive/negative evidence {sorted(missing)}")
    return {"scenarios": declared}


def validate_load(report: dict[str, Any]) -> dict[str, Any]:
    require(report.get("passed") is True, "load: report did not pass")
    concurrency = report.get("supported_concurrency")
    require(isinstance(concurrency, int) and concurrency > 0, "load: concurrency missing")
    endpoints = report.get("endpoints")
    require(isinstance(endpoints, dict), "load: endpoints missing")
    for endpoint in ("healthz", "readyz"):
        result = endpoints.get(endpoint)
        require(isinstance(result, dict), f"load: {endpoint} missing")
        require(result.get("requests", 0) > 0, f"load: {endpoint} executed no requests")
        require(result.get("success_ratio") == 1, f"load: {endpoint} success ratio < 1")
        require(
            result.get("p95_ms", float("inf")) <= result.get("p95_budget_ms", -1),
            f"load: {endpoint} p95 exceeded budget",
        )
    resource = report.get("resource", {})
    require(
        resource.get("peak_rss_bytes", float("inf")) <= resource.get("max_rss_bytes", -1),
        "load: RSS exceeded budget",
    )
    return {"supported_concurrency": concurrency}


def validate_chaos(report: dict[str, Any]) -> dict[str, Any]:
    require(report.get("passed") is True, "chaos: report did not pass")
    scenarios = report.get("scenarios")
    require(isinstance(scenarios, list), "chaos: scenarios missing")
    indexed = {
        item.get("id"): item for item in scenarios if isinstance(item, dict) and item.get("id")
    }
    required = {
        "database-outage-recovery",
        "mcp-timeout-storm",
        "garage-outage-recovery",
        "process-kill-write-atomicity",
    }
    missing = required - indexed.keys()
    require(not missing, f"chaos: missing scenarios {sorted(missing)}")
    for scenario_id in sorted(required):
        scenario = indexed[scenario_id]
        require(scenario.get("executed") is True, f"chaos: {scenario_id} did not execute")
        require(scenario.get("passed") is True, f"chaos: {scenario_id} failed")
        require(scenario.get("cleanup_ok") is True, f"chaos: {scenario_id} cleanup failed")
        require(
            scenario.get("false_ready_responses") == 0,
            f"chaos: {scenario_id} emitted false readiness",
        )
        require(
            scenario.get("duplicate_side_effects") == 0,
            f"chaos: {scenario_id} duplicated a side effect",
        )
    return {"scenarios": len(required)}


def validate_dr(report: dict[str, Any]) -> dict[str, Any]:
    require(report.get("passed") is True, "disaster recovery: report did not pass")
    planes = report.get("planes")
    require(isinstance(planes, list), "disaster recovery: planes missing")
    indexed = {
        item.get("plane"): item for item in planes if isinstance(item, dict) and item.get("plane")
    }
    required = {"postgres", "sidecars", "garage", "arcadedb"}
    missing = required - indexed.keys()
    require(not missing, f"disaster recovery: missing planes {sorted(missing)}")
    expected_count = len(required)
    require(
        report.get("planes_executed") == expected_count,
        f"disaster recovery: executed plane count != {expected_count}",
    )
    require(
        report.get("planes_required") == expected_count,
        f"disaster recovery: required plane count != {expected_count}",
    )
    for plane_id in sorted(required):
        plane = indexed[plane_id]
        require(plane.get("restored_items", 0) > 0, f"disaster recovery: {plane_id} empty")
        require(plane.get("checksum_ok") is True, f"disaster recovery: {plane_id} checksum failed")
        require(plane.get("cleanup_ok") is True, f"disaster recovery: {plane_id} cleanup failed")
    return {"planes": expected_count}


def validate_observability(report: dict[str, Any]) -> dict[str, Any]:
    require(report.get("passed") is True, "observability: report did not pass")
    passing_checks(
        report,
        {
            "static_contract",
            "live_healthz",
            "live_readyz",
            "dashboards",
            "alerts",
            "runbooks",
        },
        "observability",
    )
    return {}


DIGEST = re.compile(r"^sha256:[0-9a-f]{64}$")


def validate_rollback(report: dict[str, Any]) -> dict[str, Any]:
    require(report.get("passed") is True, "rollback: report did not pass")
    previous = report.get("previous_image_digest")
    candidate = report.get("candidate_image_digest")
    require(isinstance(previous, str) and DIGEST.fullmatch(previous), "rollback: previous digest invalid")
    require(
        isinstance(candidate, str) and DIGEST.fullmatch(candidate),
        "rollback: candidate digest invalid",
    )
    require(previous != candidate, "rollback: previous and candidate digests are identical")
    require(bool(report.get("rollback_command")), "rollback: command missing")
    for field in ("config_started", "migrations_compatible", "readiness_healthy"):
        require(report.get(field) is True, f"rollback: {field} is not true")
    return {"previous_image_digest": previous, "candidate_image_digest": candidate}


def validate_audit(report: dict[str, Any]) -> dict[str, Any]:
    require(report.get("passed") is True, "audit: report did not pass")
    require(report.get("release_ready") is True, "audit: release_ready is not true")
    counts = report.get("counts")
    require(isinstance(counts, dict), "audit: counts missing")
    allowed = {"external_blocked", "open"}
    invalid = set(counts) - allowed
    require(not invalid, f"audit: invalid states {sorted(invalid)}")
    open_total = report.get("open_total")
    require(open_total == 0, "audit: current unresolved issues remain")
    issues = report.get("issues")
    require(isinstance(issues, list) and not issues, "audit: issue list must be empty")
    require(sum(counts.values()) == open_total, "audit: state counts do not match open_total")
    return {"open_total": open_total, "external_blocked": counts.get("external_blocked", 0)}


VALIDATORS: list[tuple[str, str, Callable[[dict[str, Any]], dict[str, Any]]]] = [
    ("security", "security-report.json", validate_security),
    ("coverage", "coverage-report.json", validate_coverage),
    ("docker_coverage", "docker-coverage-report.json", validate_docker_coverage),
    ("agent_memory", "agent-memory-eval-report.json", validate_agent_memory),
    ("mutation", "mutation-report.json", validate_mutation),
    ("capability", "capability-eval.json", validate_capability),
    ("load", "load-report.json", validate_load),
    ("chaos", "chaos-report.json", validate_chaos),
    ("disaster_recovery", "dr-report.json", validate_dr),
    ("observability", "observability-report.json", validate_observability),
    ("rollback", "rollback-report.json", validate_rollback),
    ("audit", "audit-closure-report.json", validate_audit),
]


def read_and_validate_common(
    path: pathlib.Path,
    candidate: str,
    now: dt.datetime,
    max_age: dt.timedelta,
) -> tuple[dict[str, Any], str]:
    require(path.is_file(), f"missing required artifact: {path.name}")
    raw = path.read_bytes()
    try:
        report = json.loads(raw)
    except json.JSONDecodeError as exc:
        raise GateError(f"{path.name}: invalid JSON: {exc}") from exc
    require(isinstance(report, dict), f"{path.name}: report root must be an object")
    require(report.get("schema_version") == 1, f"{path.name}: schema_version must be 1")
    require(
        report.get("candidate_commit") == candidate,
        f"{path.name}: candidate_commit does not match {candidate}",
    )
    generated_raw = report.get("generated_at")
    require(isinstance(generated_raw, str), f"{path.name}: generated_at missing")
    try:
        generated = dt.datetime.fromisoformat(generated_raw.replace("Z", "+00:00"))
    except ValueError as exc:
        raise GateError(f"{path.name}: invalid generated_at") from exc
    require(generated.tzinfo is not None, f"{path.name}: generated_at lacks timezone")
    age = now - generated.astimezone(dt.timezone.utc)
    require(age >= dt.timedelta(minutes=-5), f"{path.name}: generated_at is in the future")
    require(age <= max_age, f"{path.name}: stale evidence ({age})")
    return report, hashlib.sha256(raw).hexdigest()


def run_gate(
    evidence_dir: pathlib.Path,
    candidate: str,
    output: pathlib.Path,
    max_age_hours: float,
) -> dict[str, Any]:
    require(re.fullmatch(r"[0-9a-f]{40}", candidate) is not None, "candidate must be a full Git SHA")
    now = dt.datetime.now(dt.timezone.utc)
    max_age = dt.timedelta(hours=max_age_hours)
    require(max_age > dt.timedelta(0), "max evidence age must be positive")
    evidence = []
    for gate, filename, validator in VALIDATORS:
        report, digest = read_and_validate_common(evidence_dir / filename, candidate, now, max_age)
        metrics = validator(report)
        evidence.append(
            {
                "gate": gate,
                "filename": filename,
                "sha256": digest,
                "generated_at": report["generated_at"],
                **metrics,
            }
        )
    result = {
        "schema_version": 1,
        "generated_at": now.isoformat(),
        "candidate_commit": candidate,
        "passed": True,
        "score": 10.0,
        "required_gates": len(VALIDATORS),
        "passed_gates": len(VALIDATORS),
        "evidence": evidence,
    }
    output.parent.mkdir(parents=True, exist_ok=True)
    output.write_text(json.dumps(result, indent=2) + "\n", encoding="utf-8")
    return result


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Fail-closed Aura candidate release gate")
    parser.add_argument("--evidence-dir", type=pathlib.Path, required=True)
    parser.add_argument("--candidate", required=True)
    parser.add_argument("--output", type=pathlib.Path, required=True)
    parser.add_argument("--max-age-hours", type=float, default=24.0)
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    try:
        result = run_gate(
            args.evidence_dir.resolve(),
            args.candidate,
            args.output.resolve(),
            args.max_age_hours,
        )
    except (GateError, OSError) as exc:
        print(f"release-readiness-gate: FAIL: {exc}", file=sys.stderr)
        return 1
    print(
        f"release-readiness-gate: PASS: {result['passed_gates']}/{result['required_gates']} "
        f"gates, score {result['score']:.1f}/10"
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
