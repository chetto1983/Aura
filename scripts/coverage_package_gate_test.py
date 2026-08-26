from __future__ import annotations

import json
import pathlib
import subprocess
import sys
import tempfile
import unittest


ROOT = pathlib.Path(__file__).resolve().parents[1]
GATE = ROOT / "scripts" / "coverage_package_gate.py"


def profile_rows(*rows: tuple[str, int, int]) -> str:
    lines = ["mode: atomic"]
    for index, (package, statements, count) in enumerate(rows, start=1):
        lines.append(f"{package}/file.go:{index}.1,{index + 1}.1 {statements} {count}")
    return "\n".join(lines) + "\n"


def valid_policy() -> dict[str, object]:
    return {
        "schema_version": 1,
        "target_percent": 85,
        "packages": {
            "example/internal/target": {"mode": "target"},
            "example/internal/debt": {
                "mode": "baseline",
                "covered_statements": 7,
                "total_statements": 10,
            },
            "example/internal/sandbox": {
                "mode": "delegated",
                "authority": "docker_coverage",
            },
        },
    }


class CoveragePackageGateTest(unittest.TestCase):
    def run_gate(
        self,
        profile: str,
        policy: dict[str, object] | None = None,
    ) -> subprocess.CompletedProcess[str]:
        with tempfile.TemporaryDirectory() as raw:
            root = pathlib.Path(raw)
            profile_path = root / "coverage.out"
            policy_path = root / "policy.json"
            report_path = root / "report.json"
            profile_path.write_text(profile, encoding="utf-8")
            policy_path.write_text(json.dumps(policy or valid_policy()), encoding="utf-8")
            report_path.write_text(
                json.dumps({"schema_version": 1, "passed": True}), encoding="utf-8"
            )
            result = subprocess.run(
                [
                    sys.executable,
                    str(GATE),
                    "--profile",
                    str(profile_path),
                    "--policy",
                    str(policy_path),
                    "--report",
                    str(report_path),
                ],
                text=True,
                capture_output=True,
                check=False,
            )
            if report_path.exists():
                result.report = json.loads(report_path.read_text(encoding="utf-8"))  # type: ignore[attr-defined]
            return result

    def passing_profile(self) -> str:
        return profile_rows(
            ("example/internal/target", 9, 1),
            ("example/internal/target", 1, 0),
            ("example/internal/target", 0, 0),
            ("example/internal/debt", 8, 1),
            ("example/internal/debt", 2, 0),
            ("example/internal/sandbox", 1, 1),
            ("example/internal/sandbox", 9, 0),
        )

    def test_valid_policy_appends_exact_package_evidence(self) -> None:
        result = self.run_gate(self.passing_profile())
        self.assertEqual(result.returncode, 0, result.stderr)
        package_report = result.report["package_coverage"]  # type: ignore[attr-defined]
        self.assertTrue(package_report["passed"])
        self.assertEqual(package_report["packages_evaluated"], 3)
        self.assertEqual(package_report["packages_below_target"], ["example/internal/debt"])
        self.assertEqual(
            package_report["delegated_packages"],
            [{"package": "example/internal/sandbox", "authority": "docker_coverage"}],
        )
        debt = next(
            item for item in package_report["results"] if item["package"] == "example/internal/debt"
        )
        self.assertEqual(debt["covered_statements"], 8)
        self.assertEqual(debt["total_statements"], 10)
        self.assertEqual(debt["mode"], "baseline")

    def test_exact_ratio_regression_fails(self) -> None:
        profile = profile_rows(
            ("example/internal/target", 9, 1),
            ("example/internal/target", 1, 0),
            ("example/internal/debt", 6, 1),
            ("example/internal/debt", 4, 0),
            ("example/internal/sandbox", 1, 1),
        )
        result = self.run_gate(profile)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("regressed below baseline", result.stderr)

    def test_inventory_is_fail_closed(self) -> None:
        unexpected = self.passing_profile() + "example/internal/new/file.go:1.1,2.1 1 1\n"
        result = self.run_gate(unexpected)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("unclassified packages", result.stderr)

        missing = profile_rows(
            ("example/internal/target", 9, 1),
            ("example/internal/debt", 8, 1),
            ("example/internal/debt", 2, 0),
        )
        result = self.run_gate(missing)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("policy packages missing", result.stderr)

    def test_baseline_denominator_change_fails(self) -> None:
        profile = profile_rows(
            ("example/internal/target", 9, 1),
            ("example/internal/target", 1, 0),
            ("example/internal/debt", 8, 1),
            ("example/internal/debt", 3, 0),
            ("example/internal/sandbox", 1, 1),
        )
        result = self.run_gate(profile)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("statement denominator changed", result.stderr)

    def test_zero_baseline_and_unknown_delegate_fail(self) -> None:
        policy = valid_policy()
        policy["packages"]["example/internal/debt"]["covered_statements"] = 0  # type: ignore[index]
        result = self.run_gate(self.passing_profile(), policy)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("baseline must cover at least one statement", result.stderr)

        policy = valid_policy()
        policy["packages"]["example/internal/sandbox"]["authority"] = "unknown"  # type: ignore[index]
        result = self.run_gate(self.passing_profile(), policy)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("unsupported delegated authority", result.stderr)


if __name__ == "__main__":
    unittest.main()
