from __future__ import annotations

import copy
import os
import unittest
import unittest.mock

import agent_memory_eval as evaluator
import agent_memory_eval_running_aura as running_aura


EXPECTED_COUNTS = {
    "beyond_active_context_recall": 1,
    "provider_visible_reasoning_exclusion_explicit_recall": 3,
    "durable_shell_file_capture_later_recall": 2,
}


def running_aura_fixture() -> dict[str, object]:
    response_index = 0
    scenarios: list[dict[str, object]] = []
    for name, count in EXPECTED_COUNTS.items():
        responses = []
        terminal_ids = []
        for _ in range(count):
            response_index += 1
            response_id = f"response-{response_index}"
            terminal_ids.append(response_id)
            responses.append(
                {
                    "response_id": response_id,
                    "actual_response_score": 10.0,
                    "evidence_refs": [f"tempo:trace-{response_index}"],
                    "correlation_refs": [f"postgres:turn-{response_index}", f"arcadedb:source-{response_index}"],
                }
            )
        scenarios.append(
            {
                "name": name,
                "executed": True,
                "skipped": False,
                "mocked": False,
                "expected_response_count": count,
                "terminal_response_ids": terminal_ids,
                "responses": responses,
                "proof": {
                    "tempo_path_matches": True,
                    "historical_conversation_evidence": name == "beyond_active_context_recall",
                    "touched_edges": 1 if name == "provider_visible_reasoning_exclusion_explicit_recall" else 0,
                    "ordinary_reasoning_sentinel_absent": name == "provider_visible_reasoning_exclusion_explicit_recall",
                    "explicit_reasoning_recall": name == "provider_visible_reasoning_exclusion_explicit_recall",
                    "accepted_capture": name == "durable_shell_file_capture_later_recall",
                    "run_finished_before_recall": name == "durable_shell_file_capture_later_recall",
                },
            }
        )
    return {
        "executed": True,
        "fresh_image": True,
        "candidate_commit": "a" * 40,
        "running_commit": "a" * 40,
        "scenarios": scenarios,
        "evidence": {"tempo": 6, "tool_invocations": 4, "conversation_turns": 12, "arcadedb": 8},
    }


class RunningAuraConversationEvaluatorTest(unittest.TestCase):
    def evaluate(self, evidence: dict[str, object]) -> dict[str, object]:
        self.assertTrue(
            hasattr(evaluator.phase49, "evaluate_running_aura_conversation"),
            "running_aura_conversation evaluator entrypoint is missing",
        )
        return evaluator.phase49.evaluate_running_aura_conversation(evidence)

    def test_exact_six_response_bijection_passes(self) -> None:
        self.assertTrue(self.evaluate(running_aura_fixture())["passed"])

    def test_missing_skipped_mocked_or_stale_execution_fails(self) -> None:
        for field, value in (("executed", False), ("fresh_image", False)):
            with self.subTest(field=field):
                evidence = running_aura_fixture()
                evidence[field] = value
                self.assertFalse(self.evaluate(evidence)["passed"])
        for field in ("skipped", "mocked"):
            evidence = running_aura_fixture()
            evidence["scenarios"][0][field] = True
            self.assertFalse(self.evaluate(evidence)["passed"])

    def test_weak_later_response_fails_even_with_high_aggregate(self) -> None:
        evidence = running_aura_fixture()
        scenario = evidence["scenarios"][1]
        scenario["aggregate_score"] = 9.99
        scenario["responses"][-1]["actual_response_score"] = 9.8
        self.assertFalse(self.evaluate(evidence)["passed"])

    def test_missing_duplicate_extra_and_count_mismatch_fail(self) -> None:
        mutations = []
        missing = running_aura_fixture()
        missing["scenarios"][1]["responses"].pop()
        mutations.append(missing)
        duplicate = running_aura_fixture()
        duplicate["scenarios"][1]["responses"][1]["response_id"] = duplicate["scenarios"][1]["responses"][0]["response_id"]
        mutations.append(duplicate)
        extra = running_aura_fixture()
        extra["scenarios"][0]["responses"].append(copy.deepcopy(extra["scenarios"][0]["responses"][0]))
        extra["scenarios"][0]["responses"][-1]["response_id"] = "response-extra"
        mutations.append(extra)
        mismatch = running_aura_fixture()
        mismatch["scenarios"][2]["expected_response_count"] = 3
        mutations.append(mismatch)
        for index, evidence in enumerate(mutations):
            with self.subTest(index=index):
                self.assertFalse(self.evaluate(evidence)["passed"])

    def test_missing_ids_scores_or_correlations_fail(self) -> None:
        for field, value in (
            ("response_id", ""),
            ("actual_response_score", None),
            ("evidence_refs", []),
            ("correlation_refs", []),
        ):
            with self.subTest(field=field):
                evidence = running_aura_fixture()
                evidence["scenarios"][2]["responses"][1][field] = value
                self.assertFalse(self.evaluate(evidence)["passed"])
        for store in ("tempo", "tool_invocations", "conversation_turns", "arcadedb"):
            evidence = running_aura_fixture()
            evidence["evidence"][store] = 0
            self.assertFalse(self.evaluate(evidence)["passed"])

    def test_scenario_specific_proofs_fail_closed(self) -> None:
        cases = (
            (0, "historical_conversation_evidence"),
            (1, "touched_edges"),
            (1, "ordinary_reasoning_sentinel_absent"),
            (1, "explicit_reasoning_recall"),
            (2, "accepted_capture"),
            (2, "run_finished_before_recall"),
        )
        for index, field in cases:
            with self.subTest(field=field):
                evidence = running_aura_fixture()
                evidence["scenarios"][index]["proof"][field] = 0 if field == "touched_edges" else False
                self.assertFalse(self.evaluate(evidence)["passed"])


class RunningAuraArmingTest(unittest.TestCase):
    def attach(self, armed: str | None) -> dict[str, object]:
        module = running_aura
        report: dict[str, object] = {"hard_gates": {}, "passed": True, "verdict": "PASS"}
        with unittest.mock.patch.dict(
            os.environ, {} if armed is None else {module.ARM_ENV: armed}, clear=False
        ):
            if armed is None:
                os.environ.pop(module.ARM_ENV, None)
            return module.attach_running_aura_evidence(report, "all")

    def test_unarmed_records_not_evaluated_without_claiming_a_pass(self) -> None:
        for armed in (None, "", "0", "false", "no"):
            with self.subTest(armed=armed):
                report = self.attach(armed)
                self.assertEqual(report["running_aura_conversation"]["status"], "NOT_EVALUATED")
                self.assertIs(report["running_aura_conversation"]["executed"], False)
                self.assertNotIn("passed", report["running_aura_conversation"])
                self.assertEqual(
                    report["hard_gates"]["running_aura_conversation"]["status"], "NOT_EVALUATED"
                )

    def test_armed_runs_the_live_tier_and_never_degrades_on_failure(self) -> None:
        module = running_aura
        for armed in ("1", "true", "YES"):
            with self.subTest(armed=armed):
                with unittest.mock.patch.dict(os.environ, {module.ARM_ENV: armed}, clear=False):
                    with unittest.mock.patch.object(
                        module, "run_running_aura_conversation", side_effect=RuntimeError("stack is down")
                    ):
                        with self.assertRaises(RuntimeError):
                            module.attach_running_aura_evidence(
                                {"hard_gates": {}, "passed": True, "verdict": "PASS"}, "all"
                            )

    def test_armed_failure_fails_the_report(self) -> None:
        module = running_aura
        evidence = {"executed": True, "passed": False, "status": "FAIL", "failures": ["nope"]}
        with unittest.mock.patch.dict(os.environ, {module.ARM_ENV: "1"}, clear=False):
            with unittest.mock.patch.object(
                module, "run_running_aura_conversation", return_value=evidence
            ):
                report = module.attach_running_aura_evidence(
                    {"hard_gates": {}, "passed": True, "verdict": "PASS"}, "all"
                )
        self.assertIs(report["passed"], False)
        self.assertEqual(report["verdict"], "FAIL")
        self.assertEqual(report["hard_gates"]["running_aura_conversation"]["status"], "FAIL")
