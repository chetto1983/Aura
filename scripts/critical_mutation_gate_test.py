from __future__ import annotations

import json
import os
import pathlib
import tempfile
import time
import unittest

import critical_mutation_gate


class GoMutationParserTest(unittest.TestCase):
    def test_sandbox_scope_is_a_compilable_policy_boundary(self) -> None:
        self.assertEqual(
            critical_mutation_gate.GO_SCOPES["sandbox"],
            "internal/sandbox/usersandbox/spec.go",
        )

    def test_parses_killed_total_and_score(self) -> None:
        output = (
            "PASS \"/tmp/example.go.0\" with checksum abc\n"
            "FAIL \"/tmp/example.go.1\" with checksum def\n"
            "The mutation score is 0.750000 "
            "(3 passed, 1 failed, 2 duplicated, 1 skipped, total is 7)\n"
        )
        parsed = critical_mutation_gate.parse_go_mutation_output(output)
        self.assertEqual(parsed["killed"], 3)
        self.assertEqual(parsed["survived"], 1)
        self.assertEqual(parsed["score_percent"], 75.0)

    def test_rejects_missing_or_inconsistent_summary(self) -> None:
        with self.assertRaisesRegex(ValueError, "summary"):
            critical_mutation_gate.parse_go_mutation_output("PASS only")
        with self.assertRaisesRegex(ValueError, "score"):
            critical_mutation_gate.parse_go_mutation_output(
                "mutation score is 0.900000 (3 passed, 1 failed, 0 duplicated, 0 skipped)"
            )


class FrontendMutationParserTest(unittest.TestCase):
    def test_stryker_config_emits_the_machine_readable_report(self) -> None:
        repo = pathlib.Path(__file__).resolve().parents[1]
        config = json.loads(
            (repo / "web/stryker.config.json").read_text(encoding="utf-8")
        )
        self.assertIn("json", config["reporters"])
        self.assertEqual(
            config["jsonReporter"]["fileName"],
            "reports/mutation/mutation.json",
        )

    def test_scores_detected_and_undetected_mutants(self) -> None:
        report = {
            "schemaVersion": "1.0",
            "files": {
                "src/a.ts": {
                    "mutants": [
                        {"id": "1", "status": "Killed"},
                        {"id": "2", "status": "Timeout"},
                        {"id": "3", "status": "Survived"},
                        {"id": "4", "status": "NoCoverage"},
                        {"id": "5", "status": "Ignored"},
                    ]
                }
            },
        }
        with tempfile.TemporaryDirectory() as raw:
            path = pathlib.Path(raw) / "mutation.json"
            path.write_text(json.dumps(report), encoding="utf-8")
            parsed = critical_mutation_gate.parse_frontend_report(path)
        self.assertEqual(parsed["killed"], 2)
        self.assertEqual(parsed["survived"], 2)
        self.assertEqual(parsed["score_percent"], 50.0)
        self.assertEqual(parsed["excluded"], 1)

    def test_rejects_empty_scored_report(self) -> None:
        with tempfile.TemporaryDirectory() as raw:
            path = pathlib.Path(raw) / "mutation.json"
            path.write_text(
                json.dumps(
                    {
                        "schemaVersion": "1.0",
                        "files": {"src/a.ts": {"mutants": [{"status": "Ignored"}]}},
                    }
                ),
                encoding="utf-8",
            )
            with self.assertRaisesRegex(ValueError, "no scored mutants"):
                critical_mutation_gate.parse_frontend_report(path)

    def test_rejects_stale_machine_report(self) -> None:
        report = {
            "schemaVersion": "1.0",
            "files": {
                "src/a.ts": {"mutants": [{"id": "1", "status": "Killed"}]}
            },
        }
        with tempfile.TemporaryDirectory() as raw:
            path = pathlib.Path(raw) / "mutation.json"
            path.write_text(json.dumps(report), encoding="utf-8")
            stale = time.time() - 48 * 60 * 60
            os.utime(path, (stale, stale))
            with self.assertRaisesRegex(ValueError, "stale"):
                critical_mutation_gate.parse_frontend_report(path)


if __name__ == "__main__":
    unittest.main()
