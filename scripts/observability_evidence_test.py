from __future__ import annotations

import re
import json
import pathlib
import tempfile
import unittest
from unittest import mock

import observability_evidence


class ObservabilityEvidenceTest(unittest.TestCase):
    def test_powershell_falls_back_to_windows_powershell(self) -> None:
        with mock.patch.object(
            observability_evidence.shutil,
            "which",
            side_effect=[None, r"C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe"],
        ):
            self.assertEqual(
                observability_evidence.resolve_powershell(),
                r"C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe",
            )

    def test_report_requires_every_release_check(self) -> None:
        candidate = "a" * 40
        checks = [
            {"id": check_id, "executed": True, "passed": True}
            for check_id in observability_evidence.REQUIRED_CHECKS
        ]
        report = observability_evidence.build_report(candidate, checks)
        self.assertTrue(report["passed"])
        self.assertRegex(report["candidate_commit"], re.compile(r"^[0-9a-f]{40}$"))

        checks[2]["passed"] = False
        self.assertFalse(
            observability_evidence.build_report(candidate, checks)["passed"]
        )

    def test_report_rejects_missing_or_duplicate_checks(self) -> None:
        candidate = "b" * 40
        checks = [
            {"id": check_id, "executed": True, "passed": True}
            for check_id in observability_evidence.REQUIRED_CHECKS[:-1]
        ]
        with self.assertRaisesRegex(ValueError, "missing"):
            observability_evidence.build_report(candidate, checks)

        checks.append(dict(checks[0]))
        with self.assertRaisesRegex(ValueError, "duplicate"):
            observability_evidence.build_report(candidate, checks)

    def test_load_report_supplies_exact_candidate_live_endpoint_evidence(self) -> None:
        candidate = "c" * 40
        report = {
            "schema_version": 1,
            "candidate_commit": candidate,
            "passed": True,
            "endpoints": {
                "healthz": {
                    "requests": 20,
                    "success_ratio": 1,
                    "p95_ms": 2,
                    "p95_budget_ms": 50,
                },
                "readyz": {
                    "requests": 20,
                    "success_ratio": 1,
                    "p95_ms": 4,
                    "p95_budget_ms": 100,
                },
            },
        }
        with tempfile.TemporaryDirectory() as raw:
            path = pathlib.Path(raw) / "load-report.json"
            path.write_text(json.dumps(report), encoding="utf-8")
            checks = observability_evidence.endpoint_checks_from_load(
                path, candidate
            )
        self.assertEqual([check["id"] for check in checks], ["live_healthz", "live_readyz"])
        self.assertTrue(all(check["passed"] for check in checks))

        report["candidate_commit"] = "d" * 40
        with tempfile.TemporaryDirectory() as raw:
            path = pathlib.Path(raw) / "load-report.json"
            path.write_text(json.dumps(report), encoding="utf-8")
            with self.assertRaisesRegex(ValueError, "candidate"):
                observability_evidence.endpoint_checks_from_load(path, candidate)


if __name__ == "__main__":
    unittest.main()
