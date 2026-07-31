from __future__ import annotations

import json
import unittest

import security_evidence


class SecurityEvidenceTest(unittest.TestCase):
    def test_codeql_requires_both_languages_successful(self) -> None:
        payload = {
            "check_runs": [
                {
                    "name": "Analyze (go)",
                    "status": "completed",
                    "conclusion": "success",
                    "app": {"name": "GitHub Actions"},
                },
                {
                    "name": "Analyze (javascript-typescript)",
                    "status": "completed",
                    "conclusion": "success",
                    "app": {"name": "GitHub Actions"},
                },
            ]
        }
        evidence = security_evidence.validate_codeql(payload)
        self.assertEqual(evidence["languages"], ["go", "javascript-typescript"])

        payload["check_runs"][1]["conclusion"] = "failure"
        with self.assertRaisesRegex(ValueError, "did not succeed"):
            security_evidence.validate_codeql(payload)

    def test_strict_profile_json_requires_every_named_test(self) -> None:
        lines = [
            json.dumps({"Action": "pass", "Test": name})
            for name in sorted(security_evidence.STRICT_TESTS)
        ]
        evidence = security_evidence.validate_strict_test_output("\n".join(lines))
        self.assertEqual(evidence["tests_passed"], len(security_evidence.STRICT_TESTS))

        with self.assertRaisesRegex(ValueError, "missing"):
            security_evidence.validate_strict_test_output(lines[0])

    def test_report_is_fail_closed(self) -> None:
        checks = [
            {"id": check_id, "executed": True, "passed": True}
            for check_id in security_evidence.REQUIRED_CHECKS
        ]
        report = security_evidence.build_report("a" * 40, checks)
        self.assertTrue(report["passed"])
        self.assertEqual(report["unresolved_release_blocking_alerts"], 0)
        checks[0]["passed"] = False
        self.assertFalse(security_evidence.build_report("a" * 40, checks)["passed"])


if __name__ == "__main__":
    unittest.main()
