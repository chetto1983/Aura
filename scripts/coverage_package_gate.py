from __future__ import annotations

import argparse
import json
import pathlib
import re
import sys
from typing import Any


PROFILE_POSITION = re.compile(r"^\d+\.\d+,\d+\.\d+$")
ALLOWED_DELEGATED_AUTHORITIES = {"docker_coverage"}


class GateError(RuntimeError):
    pass


def require(condition: bool, message: str) -> None:
    if not condition:
        raise GateError(message)


def read_json(path: pathlib.Path, label: str) -> dict[str, Any]:
    require(path.is_file(), f"{label} is missing: {path}")
    try:
        value = json.loads(path.read_text(encoding="utf-8"))
    except json.JSONDecodeError as exc:
        raise GateError(f"{label} is invalid JSON: {exc}") from exc
    require(isinstance(value, dict), f"{label} root must be an object")
    return value


def read_profile(path: pathlib.Path) -> dict[str, tuple[int, int]]:
    require(path.is_file(), f"coverage profile is missing: {path}")
    lines = path.read_text(encoding="utf-8").splitlines()
    require(lines and lines[0] in {"mode: atomic", "mode: count", "mode: set"},
            "coverage profile has no valid mode header")
    totals: dict[str, list[int]] = {}
    seen_blocks: set[str] = set()
    for line_number, line in enumerate(lines[1:], start=2):
        fields = line.split()
        require(len(fields) == 3, f"coverage profile row {line_number} is malformed")
        location, statements_raw, count_raw = fields
        try:
            source, position = location.rsplit(":", 1)
        except ValueError as exc:
            raise GateError(f"coverage profile row {line_number} has no source position") from exc
        require(PROFILE_POSITION.fullmatch(position) is not None,
                f"coverage profile row {line_number} has an invalid source position")
        require("/" in source, f"coverage profile row {line_number} has no package path")
        try:
            statements = int(statements_raw)
            count = int(count_raw)
        except ValueError as exc:
            raise GateError(f"coverage profile row {line_number} has non-integer counters") from exc
        require(statements >= 0, f"coverage profile row {line_number} has a negative statement count")
        require(count >= 0, f"coverage profile row {line_number} has a negative count")
        require(location not in seen_blocks, f"coverage profile contains duplicate block {location}")
        seen_blocks.add(location)
        if statements == 0:
            continue
        package = source.rsplit("/", 1)[0]
        counters = totals.setdefault(package, [0, 0])
        counters[1] += statements
        if count > 0:
            counters[0] += statements
    require(bool(totals), "coverage profile has no package statements")
    return {package: (counts[0], counts[1]) for package, counts in totals.items()}


def validate_policy(policy: dict[str, Any]) -> tuple[float, dict[str, dict[str, Any]]]:
    require(policy.get("schema_version") == 1, "policy schema_version must be 1")
    target = policy.get("target_percent")
    require(isinstance(target, (int, float)) and 0 < target <= 100,
            "policy target_percent must be in (0, 100]")
    packages = policy.get("packages")
    require(isinstance(packages, dict) and packages, "policy packages must be a non-empty object")
    for package, rule in packages.items():
        require(isinstance(package, str) and package.strip() == package and package,
                "policy contains an invalid package name")
        require(isinstance(rule, dict), f"policy rule for {package} must be an object")
        mode = rule.get("mode")
        require(mode in {"target", "baseline", "delegated"},
                f"policy rule for {package} has invalid mode {mode!r}")
        if mode == "baseline":
            covered = rule.get("covered_statements")
            total = rule.get("total_statements")
            require(isinstance(covered, int) and covered > 0,
                    f"{package}: baseline must cover at least one statement")
            require(isinstance(total, int) and total > 0 and covered <= total,
                    f"{package}: baseline statement counts are invalid")
            require(covered * 100 < target * total,
                    f"{package}: baseline already meets the target and must use target mode")
        if mode == "delegated":
            authority = rule.get("authority")
            require(authority in ALLOWED_DELEGATED_AUTHORITIES,
                    f"{package}: unsupported delegated authority {authority!r}")
    return float(target), packages


def apply_policy(
    actual: dict[str, tuple[int, int]],
    target: float,
    policy_packages: dict[str, dict[str, Any]],
) -> dict[str, Any]:
    actual_names = set(actual)
    policy_names = set(policy_packages)
    unexpected = sorted(actual_names - policy_names)
    missing = sorted(policy_names - actual_names)
    require(not unexpected, f"unclassified packages: {unexpected}")
    require(not missing, f"policy packages missing from profile: {missing}")

    results: list[dict[str, Any]] = []
    below_target: list[str] = []
    delegated: list[dict[str, str]] = []
    # Every violating package is reported, not just the first. The gate is still
    # fail-closed and nothing it accepts has changed -- but a fail-fast raise made
    # each unmet package cost a full CI round to discover, so a merge that moved
    # three of them looked like three unrelated failures instead of one regression.
    violations: list[str] = []
    for package in sorted(policy_packages):
        rule = policy_packages[package]
        mode = str(rule["mode"])
        covered, total = actual[package]
        percent = covered * 100 / total
        result: dict[str, Any] = {
            "package": package,
            "mode": mode,
            "passed": True,
            "target_percent": target,
            "covered_statements": covered,
            "total_statements": total,
            "statements_percent": percent,
        }
        if mode == "target":
            if covered * 100 < target * total:
                violations.append(
                    f"{package}: coverage {covered}/{total} ({percent:.2f}%) "
                    f"is below target {target:g}%"
                )
                result["passed"] = False
            result["minimum_percent"] = target
        elif mode == "baseline":
            baseline_covered = int(rule["covered_statements"])
            baseline_total = int(rule["total_statements"])
            if total != baseline_total:
                violations.append(
                    f"{package}: statement denominator changed from {baseline_total} to {total}"
                )
                result["passed"] = False
            elif covered * baseline_total < baseline_covered * total:
                violations.append(
                    f"{package}: coverage {covered}/{total} regressed below baseline "
                    f"{baseline_covered}/{baseline_total}"
                )
                result["passed"] = False
            result["baseline_covered_statements"] = baseline_covered
            result["baseline_total_statements"] = baseline_total
            result["baseline_percent"] = baseline_covered * 100 / baseline_total
        else:
            authority = str(rule["authority"])
            result["authority"] = authority
            delegated.append({"package": package, "authority": authority})
        if mode != "delegated" and covered * 100 < target * total:
            below_target.append(package)
        results.append(result)

    require(
        not violations,
        f"{len(violations)} package(s) violate the policy:\n  - " + "\n  - ".join(violations),
    )

    return {
        "passed": True,
        "cross_package_attribution": True,
        "policy_schema_version": 1,
        "target_percent": target,
        "packages_evaluated": len(results),
        "packages_at_target": len(results) - len(below_target) - len(delegated),
        "packages_below_target": below_target,
        "delegated_packages": delegated,
        "results": results,
    }


def run(profile_path: pathlib.Path, policy_path: pathlib.Path, report_path: pathlib.Path) -> None:
    actual = read_profile(profile_path)
    policy = read_json(policy_path, "package policy")
    target, policy_packages = validate_policy(policy)
    package_report = apply_policy(actual, target, policy_packages)
    report = read_json(report_path, "coverage report")
    require(report.get("passed") is True, "coverage report did not pass before package policy")
    report["package_coverage"] = package_report
    report_path.write_text(json.dumps(report, indent=2) + "\n", encoding="utf-8")


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Enforce package-local Aura coverage policy")
    parser.add_argument("--profile", required=True, type=pathlib.Path)
    parser.add_argument("--policy", required=True, type=pathlib.Path)
    parser.add_argument("--report", required=True, type=pathlib.Path)
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    try:
        run(args.profile, args.policy, args.report)
    except (GateError, OSError) as exc:
        print(f"FAIL: package coverage: {exc}", file=sys.stderr)
        return 1
    print("ok: package-local coverage policy passed")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
