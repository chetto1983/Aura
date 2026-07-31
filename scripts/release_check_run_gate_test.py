from __future__ import annotations

import unittest

import release_check_run_gate


class ReleaseCheckRunGateTest(unittest.TestCase):
    def test_requires_successful_exact_readiness_job(self) -> None:
        payload = {
            "check_runs": [
                {
                    "name": release_check_run_gate.REQUIRED_CHECK,
                    "status": "completed",
                    "conclusion": "success",
                    "app": {"name": "GitHub Actions"},
                }
            ]
        }
        release_check_run_gate.validate(payload)

        payload["check_runs"][0]["conclusion"] = "failure"
        with self.assertRaisesRegex(ValueError, "not successful"):
            release_check_run_gate.validate(payload)

    def test_rejects_missing_or_untrusted_check(self) -> None:
        with self.assertRaisesRegex(ValueError, "missing"):
            release_check_run_gate.validate({"check_runs": []})
        with self.assertRaisesRegex(ValueError, "producer"):
            release_check_run_gate.validate(
                {
                    "check_runs": [
                        {
                            "name": release_check_run_gate.REQUIRED_CHECK,
                            "status": "completed",
                            "conclusion": "success",
                            "app": {"name": "Other"},
                        }
                    ]
                }
            )


if __name__ == "__main__":
    unittest.main()
