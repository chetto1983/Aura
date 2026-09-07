from __future__ import annotations

import unittest

import agent_memory_eval as evaluator
from agent_memory_eval_protocol import failed_suite_details


class FailedSuiteDetailsTest(unittest.TestCase):
    def test_names_an_unscored_failure_even_with_a_perfect_score(self) -> None:
        report = {"memory_reliability_score": 100, "suites": [{
            "id": "arcadedb_live", "passed": False,
            "tests": {"TestReasoningGraphLive_ExpiryDeleteRace": [{"status": "FAIL"}],
                      "TestScoredScenario": [{"status": "PASS"}]},
        }]}
        self.assertEqual(failed_suite_details(report), [
            "arcadedb_live: failed tests: TestReasoningGraphLive_ExpiryDeleteRace"])

    def test_surfaces_execution_failure_without_inventing_a_failed_test(self) -> None:
        details = failed_suite_details({"suites": [{"id": "live", "passed": False,
            "executed": True, "return_code": 2, "protocol_errors": ["not go test JSON"]}]})
        self.assertIn("return_code=2", details[0])
        self.assertIn("not go test JSON", details[0])

    def test_success_and_unarmed_gate_have_no_suite_failure(self) -> None:
        report = {"hard_gates": {"running_aura_conversation": {"status": "NOT_EVALUATED"}},
                  "suites": [{"id": "live", "passed": True}]}
        self.assertEqual(failed_suite_details(report), [])


class MetricParserTest(unittest.TestCase):
    def test_coverage_uses_statement_counts(self) -> None:
        parsed = evaluator.parse_coverage_profile("mode: atomic\nexample/a.go:1.1,2.1 3 1\nexample/a.go:3.1,4.1 1 0\n")
        self.assertEqual(parsed["covered_statements"], 3)
        self.assertEqual(parsed["total_statements"], 4)
        self.assertEqual(parsed["percent"], 75.0)

    def test_coverage_rejects_empty_profile(self) -> None:
        with self.assertRaisesRegex(ValueError, "no statements"):
            evaluator.parse_coverage_profile("mode: set\n")

    def test_coverage_gaps_address_the_shortfall_worst_file_first(self) -> None:
        # A package under the floor is only actionable if the report says WHERE.
        profile = (
            "mode: atomic\n"
            "example/a.go:1.1,2.1 3 1\n"
            "example/a.go:3.1,4.1 1 0\n"
            "example/b.go:5.1,9.4 7 0\n"
            "example/c.go:1.1,2.1 2 4\n"
        )
        gaps = evaluator.coverage_gaps(profile)
        self.assertEqual([item["file"] for item in gaps], ["example/b.go", "example/a.go", "example/c.go"])
        self.assertEqual(gaps[0]["uncovered_statements"], 7)
        self.assertEqual(gaps[0]["uncovered_blocks"], ["5.1,9.4"])
        self.assertEqual(gaps[1]["covered_statements"], 3)
        self.assertEqual(gaps[1]["uncovered_blocks"], ["3.1,4.1"])
        # A fully covered file still appears, with nothing to aim at.
        self.assertEqual(gaps[2]["uncovered_statements"], 0)
        self.assertEqual(gaps[2]["uncovered_blocks"], [])
        # The totals stay reconcilable with the gate's own ratio.
        parsed = evaluator.parse_coverage_profile(profile)
        self.assertEqual(sum(item["covered_statements"] for item in gaps), parsed["covered_statements"])
        self.assertEqual(
            sum(item["covered_statements"] + item["uncovered_statements"] for item in gaps),
            parsed["total_statements"],
        )

    def test_coverage_gaps_reject_the_same_malformed_rows_the_gate_does(self) -> None:
        for profile, reason in (
            ("example/a.go:1.1,2.1 3 1\n", "no mode header"),
            ("mode: atomic\nexample/a.go:1.1,2.1 3\n", "invalid coverage row"),
            ("mode: atomic\nexample/a.go:1.1,2.1 x 1\n", "invalid coverage counts"),
            ("mode: atomic\nexample/a.go:1.1,2.1 -3 1\n", "negative coverage counts"),
        ):
            with self.subTest(reason=reason):
                with self.assertRaisesRegex(ValueError, reason):
                    evaluator.coverage_gaps(profile)

    def test_parses_the_exact_live_latency_marker(self) -> None:
        marker = evaluator.parse_latency_marker('{"samples":25,"p50_ms":336.28,"p95_ms":498.36,"max_ms":1898.39,"cold_retained":true,"path":"cli_identity_mcp_search"}')
        self.assertEqual(marker["samples"], 25)
        self.assertEqual(marker["p95_ms"], 498.36)

    def test_latency_marker_rejects_short_or_wrong_scope_evidence(self) -> None:
        with self.assertRaisesRegex(ValueError, "at least 25"):
            evaluator.parse_latency_marker('{"samples":24,"p50_ms":1,"p95_ms":2,"max_ms":3,"cold_retained":true,"path":"cli_identity_mcp_search"}')
        with self.assertRaisesRegex(ValueError, "CLI identity"):
            evaluator.parse_latency_marker('{"samples":25,"p50_ms":1,"p95_ms":2,"max_ms":3,"cold_retained":false,"path":"database_only"}')


class FailedGateNamesTest(unittest.TestCase):
    """A perfect MRS beside a FAIL verdict must name the gate that actually failed.

    CI reported `FAIL: MRS=100.00` against a floor of 96.5, which reads as a contradiction
    and sent the reader to the threshold. The threshold was fine; a hard gate was not, and
    the line never said which.
    """

    def test_reports_nothing_when_every_gate_passes(self) -> None:
        report = {"hard_gates": {"suite_integrity": {"status": "PASS"}}}
        self.assertEqual(evaluator.failed_gate_names(report), [])

    def test_names_failed_gates_in_report_order(self) -> None:
        report = {
            "hard_gates": {
                "suite_integrity": {"status": "FAIL"},
                "runtime_provenance": {"status": "PASS"},
                "batch_atomicity": {"status": "FAIL"},
            }
        }
        self.assertEqual(
            evaluator.failed_gate_names(report),
            ["suite_integrity", "batch_atomicity"],
        )

    def test_a_deliberately_unarmed_gate_is_not_a_failure(self) -> None:
        # running_aura_conversation is NOT_EVALUATED on every GitHub CI run by design --
        # no model key, no Tempo, no enrolled operator -- and its unarmed branch returns
        # without touching report["passed"]. Reporting it as failed would put a permanent
        # false alarm in every summary.
        report = {
            "hard_gates": {
                "suite_integrity": {"status": "FAIL"},
                "running_aura_conversation": {"status": "NOT_EVALUATED"},
            }
        }
        self.assertEqual(evaluator.failed_gate_names(report), ["suite_integrity"])
        self.assertEqual(
            evaluator.unevaluated_gate_names(report), ["running_aura_conversation"]
        )

    def test_survives_a_report_with_no_gates_or_a_malformed_one(self) -> None:
        self.assertEqual(evaluator.failed_gate_names({}), [])
        self.assertEqual(evaluator.failed_gate_names({"hard_gates": None}), [])
        self.assertEqual(evaluator.unevaluated_gate_names({"hard_gates": None}), [])
        # A gate that is not a dict cannot be read as passing, so it is surfaced rather
        # than silently treated as green.
        self.assertEqual(evaluator.failed_gate_names({"hard_gates": {"broken": "PASS"}}), ["broken"])

