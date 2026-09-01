from __future__ import annotations

import json
import pathlib
import subprocess
import sys
import tempfile
import unittest
from unittest import mock

# Keep both documented invocations working: direct script execution and
# `python -m unittest scripts.agent_memory_eval_test` from the repository root.
sys.path.insert(0, str(pathlib.Path(__file__).resolve().parent))

import agent_memory_eval as evaluator
import agent_memory_eval_metadata as metadata


MEMORY_RECALL_INPUT_PROPERTIES = [
    "mode",
    "query",
    "entity",
    "predicate",
    "conversation_id",
    "anchor_seq",
    "cursor",
    "direction",
    "trace_id",
    "limit",
    "as_of",
]
MEMORY_RECALL_MODES = ["semantic", "recent", "open", "scroll", "reasoning"]


def event(action: str, test: str | None = None, elapsed: float | None = None) -> str:
    value: dict[str, object] = {"Action": action, "Package": "example.test"}
    if test is not None:
        value["Test"] = test
    if elapsed is not None:
        value["Elapsed"] = elapsed
    return json.dumps(value)


def passing_suites(manifest: dict[str, object]) -> dict[str, object]:
    suites: dict[str, object] = {}
    for suite in manifest["suites"]:  # type: ignore[index]
        suites[suite["id"]] = {
            "id": suite["id"],
            "tier": suite["tier"],
            "executed": True,
            "passed": True,
            "return_code": 0,
            "tests": {},
            "latency_metrics": {},
            "runtime_metadata": {},
            "skipped_tests": [],
            "package_failed": False,
            "protocol_errors": [],
        }
    for scenario in manifest["scenarios"]:  # type: ignore[index]
        evidence = scenario.get("evidence")
        if not evidence or evidence["kind"] not in {"go_test", "p95"}:
            continue
        tests = suites[evidence["suite"]]["tests"]
        tests.setdefault(evidence["test"], []).extend({"status": "PASS", "elapsed_ms": 10.0} for _ in range(evidence.get("minimum_runs", 1)))
        if evidence["kind"] == "p95":
            suites[evidence["suite"]]["latency_metrics"][evidence["test"]] = [
                {
                    "samples": 25,
                    "p50_ms": 300.0,
                    "p95_ms": 500.0,
                    "max_ms": 900.0,
                    "cold_retained": True,
                    "path": "cli_identity_mcp_search",
                }
            ]
    suites["arcadedb_live"]["coverage"] = {
        "covered_statements": 940,
        "total_statements": 1000,
        "percent": 94.0,
    }
    suites["arcadedb_mcp_live"]["runtime_metadata"] = {
        "TestAgentMemoryMCPLiveInitializeListCallAndIsolation": [
            {
                "arcadedb_version": "26.7.3",
                "mcp_server_version": "0.1.0",
                "embedding_model": "embeddinggemma-300M-Q8_0.gguf",
                "embedding_dimension": 768,
            }
        ]
    }
    return suites


def mixed_tier_case(
    name: str,
    effective_path: str,
    backend_path: str,
    facts: int,
    conversations: int,
) -> dict[str, object]:
    counts = {"facts": facts, "conversations": conversations, "reasoning": 0}
    return {
        "name": name,
        "response": {
            "effective_path": effective_path,
            "path": backend_path,
            "tier_counts": counts,
        },
        "otel": {
            "memory.recall.effective_path": effective_path,
            "memory.recall.path": backend_path,
            "memory.recall.fact_count": facts,
            "memory.recall.conversation_count": conversations,
            "memory.recall.reasoning_count": 0,
        },
        "returned_tier_counts": counts.copy(),
        "active_source_count": 0,
        "foreign_source_count": 0,
    }


def mixed_tier_fixture() -> dict[str, object]:
    return {
        "scenario": "mixed_tier_recall",
        "scenarios": [
            mixed_tier_case("mixed", "mixed", "hybrid", 1, 1),
            mixed_tier_case("query", "facts", "hybrid", 1, 0),
            mixed_tier_case("entity", "facts", "graph", 1, 0),
            mixed_tier_case("forced_fallback", "facts", "lexical", 1, 0),
        ],
        "surface": {
            "retrieval_operations": ["memory_recall"],
            "memory_recall_input_properties": MEMORY_RECALL_INPUT_PROPERTIES.copy(),
            "memory_recall_mode_enum": MEMORY_RECALL_MODES.copy(),
        },
    }


def batch_atomicity_fixture() -> dict[str, object]:
    return {
        "scenario": "batch_atomicity",
        "executed_count": 5,
        "before_hash": "before",
        "committed_hash": "committed",
        "rollback_hash": "committed",
        "replay_hash": "committed",
        "cross_identity_before_hash": "foreign",
        "cross_identity_after_hash": "foreign",
        "replayed": True,
        "logical_effects": 1,
        "first_error_code": "target_not_found",
        "idempotency_conflict_observed": True,
    }


class ManifestTest(unittest.TestCase):
    def test_default_manifest_is_exactly_one_hundred_points(self) -> None:
        manifest = evaluator.load_manifest(None)
        self.assertEqual(sum(item["points"] for item in manifest["scenarios"]), 100)
        self.assertEqual(
            {name: sum(item["points"] for item in manifest["scenarios"] if item["dimension"] == name) for name in evaluator.DIMENSIONS},
            evaluator.DIMENSIONS,
        )

    def test_default_manifest_names_only_tests_present_in_the_repository(self) -> None:
        repo = pathlib.Path(__file__).resolve().parents[1]
        manifest = evaluator.load_manifest(None)
        suite_sources: dict[str, str] = {}
        for suite in manifest["suites"]:
            package = suite["command"][-1].removeprefix("./").rstrip("/")
            suite_sources[suite["id"]] = "\n".join(path.read_text(encoding="utf-8") for path in (repo / package).glob("*_test.go"))
        for scenario in manifest["scenarios"]:
            evidence = scenario.get("evidence")
            if evidence is None or evidence["kind"] not in {"go_test", "p95"}:
                continue
            self.assertIn(
                f"func {evidence['test']}(t *testing.T)",
                suite_sources[evidence["suite"]],
                scenario["id"],
            )

    def test_default_manifest_has_no_missing_evidence(self) -> None:
        manifest = evaluator.load_manifest(None)
        missing = [item for item in manifest["scenarios"] if item["evidence"] is None]
        self.assertEqual(missing, [])

    def test_rejects_non_strict_threshold_and_unknown_suite(self) -> None:
        manifest = evaluator.default_manifest()
        manifest["pass_threshold"] = {"operator": ">=", "value": 96.5}
        with self.assertRaisesRegex(ValueError, "strictly"):
            evaluator.validate_manifest(manifest)
        manifest = evaluator.default_manifest()
        manifest["scenarios"][0]["evidence"]["suite"] = "does-not-exist"
        with self.assertRaisesRegex(ValueError, "unknown suite"):
            evaluator.validate_manifest(manifest)

    def test_rejects_dimension_weight_drift(self) -> None:
        manifest = evaluator.default_manifest()
        manifest["scenarios"][0]["points"] = 3
        with self.assertRaisesRegex(ValueError, "points by dimension"):
            evaluator.validate_manifest(manifest)


class Phase49EvidenceContractTest(unittest.TestCase):
    def test_manifest_declares_mixed_recall_and_reasoning_isolation(self) -> None:
        contract = evaluator.load_manifest(None)["phase49_evidence"]
        self.assertEqual(contract["schema_id"], "aura.phase49-memory-evidence/v1")
        self.assertEqual(
            [scenario["id"] for scenario in contract["scenario_definitions"]],
            ["mixed_tier_recall", "reasoning_isolation"],
        )
        self.assertEqual(
            contract["otel_attributes"],
            {
                "effective_path": "memory.recall.effective_path",
                "path": "memory.recall.path",
            },
        )

    def test_baseline_keeps_tier_contribution_separate_from_backend_path(self) -> None:
        manifest = evaluator.load_manifest(None)
        report = evaluator.score(manifest, passing_suites(manifest), "deterministic", "a" * 40)
        baseline = report["phase49_evidence"]["observations"]["baseline"]
        mixed = baseline["mixed_tier_recall"]
        self.assertEqual(mixed["status"], "observed_partial")
        self.assertEqual(mixed["retrieval"]["effective_path"], "facts")
        self.assertEqual(mixed["retrieval"]["path"], "hybrid")
        self.assertEqual(mixed["tier_counts"], {"facts": 1, "conversations": 0, "reasoning": 0})
        self.assertEqual(mixed["tier_ranks"], {"facts": [1], "conversations": [], "reasoning": []})
        self.assertEqual(mixed["latency_ms"]["p95"], 85.395)
        self.assertEqual(
            mixed["abstention"],
            {
                "observed": True,
                "abstained": True,
                "reason": "no_qualified_candidates",
                "backend_path": "hybrid",
            },
        )
        self.assertEqual(mixed["otel"]["memory.recall.effective_path"], "facts")
        self.assertEqual(mixed["otel"]["memory.recall.path"], "hybrid")

    def test_unobserved_reasoning_and_final_evidence_never_claim_pass(self) -> None:
        manifest = evaluator.load_manifest(None)
        evidence = evaluator.score(manifest, passing_suites(manifest), "deterministic", "a" * 40)["phase49_evidence"]
        self.assertEqual(evidence["observations"]["baseline"]["reasoning_isolation"]["status"], "not_observed")
        for scenario in evidence["observations"]["final"].values():
            self.assertFalse(scenario["executed"])
            self.assertEqual(scenario["status"], "not_observed")
            self.assertIsNone(scenario["retrieval"]["effective_path"])
            self.assertIsNone(scenario["retrieval"]["path"])

    def test_manifest_rejects_conflated_or_invented_phase49_paths(self) -> None:
        manifest = evaluator.default_manifest()
        manifest["phase49_evidence"]["observations"]["baseline"]["mixed_tier_recall"]["retrieval"]["path"] = "facts"
        with self.assertRaisesRegex(ValueError, "backend path"):
            evaluator.validate_manifest(manifest)
        manifest = evaluator.default_manifest()
        final = manifest["phase49_evidence"]["observations"]["final"]["mixed_tier_recall"]
        final["status"] = "pass"
        with self.assertRaisesRegex(ValueError, "unobserved"):
            evaluator.validate_manifest(manifest)


class MixedTierRecallEvaluatorTest(unittest.TestCase):
    def evaluate(self, evidence: dict[str, object]) -> dict[str, object]:
        self.assertTrue(
            hasattr(evaluator.phase49, "evaluate_mixed_tier_recall"),
            "mixed_tier_recall evaluator entrypoint is missing",
        )
        return evaluator.phase49.evaluate_mixed_tier_recall(evidence)

    def test_complete_route_evidence_passes_and_reports_each_axis(self) -> None:
        report = self.evaluate(mixed_tier_fixture())
        self.assertTrue(report["passed"])
        self.assertEqual(report["scenario"], "mixed_tier_recall")
        self.assertEqual(report["tier_counts"], {"facts": 1, "conversations": 1, "reasoning": 0})
        self.assertEqual(
            report["backend_paths"],
            {"query": "hybrid", "entity": "graph", "forced_fallback": "lexical"},
        )

    def test_zero_scenarios_and_missing_fact_or_conversation_fail(self) -> None:
        empty = mixed_tier_fixture()
        empty["scenarios"] = []
        self.assertFalse(self.evaluate(empty)["passed"])
        for tier in ("facts", "conversations"):
            with self.subTest(tier=tier):
                evidence = mixed_tier_fixture()
                mixed = evidence["scenarios"][0]
                mixed["response"]["tier_counts"][tier] = 0
                mixed["returned_tier_counts"][tier] = 0
                self.assertFalse(self.evaluate(evidence)["passed"])
    def test_active_or_foreign_source_leakage_fails(self) -> None:
        for field in ("active_source_count", "foreign_source_count"):
            with self.subTest(field=field):
                evidence = mixed_tier_fixture()
                evidence["scenarios"][0][field] = 1
                self.assertFalse(self.evaluate(evidence)["passed"])

    def test_missing_swapped_or_conflated_route_axes_fail(self) -> None:
        mutations = {
            "missing response effective path": lambda case: case["response"].pop("effective_path"),
            "missing OTel path": lambda case: case["otel"].pop("memory.recall.path"),
            "swapped axes": lambda case: case["response"].update({"effective_path": "hybrid", "path": "mixed"}),
            "conflated axes": lambda case: case["response"].update({"effective_path": case["response"]["path"]}),
        }
        for name, mutate in mutations.items():
            with self.subTest(name=name):
                evidence = mixed_tier_fixture()
                mutate(evidence["scenarios"][0])
                self.assertFalse(self.evaluate(evidence)["passed"])

    def test_wrong_backend_and_otel_disagreement_fail(self) -> None:
        for case_name, wrong_path in (("query", "graph"), ("entity", "hybrid"), ("forced_fallback", "hybrid")):
            with self.subTest(case=case_name):
                evidence = mixed_tier_fixture()
                case = next(item for item in evidence["scenarios"] if item["name"] == case_name)
                case["response"]["path"] = wrong_path
                case["otel"]["memory.recall.path"] = wrong_path
                self.assertFalse(self.evaluate(evidence)["passed"])
        evidence = mixed_tier_fixture()
        evidence["scenarios"][1]["otel"]["memory.recall.path"] = "graph"
        self.assertFalse(self.evaluate(evidence)["passed"])

    def test_tier_count_disagreement_and_surface_drift_fail(self) -> None:
        evidence = mixed_tier_fixture()
        evidence["scenarios"][0]["otel"]["memory.recall.conversation_count"] = 0
        self.assertFalse(self.evaluate(evidence)["passed"])
        evidence = mixed_tier_fixture()
        evidence["surface"]["retrieval_operations"].append("memory_search")
        self.assertFalse(self.evaluate(evidence)["passed"])

    def test_stale_memory_recall_schema_fails(self) -> None:
        stale_surfaces = {
            "missing mode property": {
                "memory_recall_input_properties": MEMORY_RECALL_INPUT_PROPERTIES[1:],
                "memory_recall_mode_enum": MEMORY_RECALL_MODES,
            },
            "missing recent mode": {
                "memory_recall_input_properties": MEMORY_RECALL_INPUT_PROPERTIES,
                "memory_recall_mode_enum": [mode for mode in MEMORY_RECALL_MODES if mode != "recent"],
            },
            "observed five-field legacy schema": {
                "memory_recall_input_properties": ["query", "entity", "predicate", "limit", "as_of"],
                "memory_recall_mode_enum": [],
            },
        }
        for name, surface in stale_surfaces.items():
            with self.subTest(name=name):
                evidence = mixed_tier_fixture()
                evidence["surface"].update(surface)
                self.assertFalse(self.evaluate(evidence)["passed"])


class BatchAtomicityEvaluatorTest(unittest.TestCase):
    def evaluate(self, evidence: dict[str, object]) -> dict[str, object]:
        self.assertTrue(
            hasattr(evaluator.phase49, "evaluate_batch_atomicity"),
            "batch_atomicity evaluator entrypoint is missing",
        )
        return evaluator.phase49.evaluate_batch_atomicity(evidence)

    def test_complete_live_batch_evidence_passes(self) -> None:
        self.assertTrue(self.evaluate(batch_atomicity_fixture())["passed"])

    def test_missing_execution_or_state_hash_fails(self) -> None:
        evidence = batch_atomicity_fixture()
        evidence["executed_count"] = 0
        self.assertFalse(self.evaluate(evidence)["passed"])
        evidence = batch_atomicity_fixture()
        evidence["committed_hash"] = ""
        self.assertFalse(self.evaluate(evidence)["passed"])

    def test_rollback_replay_cross_identity_and_duplicate_effect_fail(self) -> None:
        for field in ("rollback_hash", "replay_hash", "cross_identity_after_hash"):
            with self.subTest(field=field):
                evidence = batch_atomicity_fixture()
                evidence[field] = "changed"
                self.assertFalse(self.evaluate(evidence)["passed"])
        evidence = batch_atomicity_fixture()
        evidence["logical_effects"] = 2
        self.assertFalse(self.evaluate(evidence)["passed"])


class GoTestParserTest(unittest.TestCase):
    def test_parses_repeated_passes_for_latency_samples(self) -> None:
        output = "\n".join(
            [
                event("run", "TestWire"),
                event("pass", "TestWire", 0.012),
                event("run", "TestWire"),
                event("pass", "TestWire", 0.009),
                event("pass", elapsed=0.025),
            ]
        )
        parsed = evaluator.parse_go_test_json(output)
        self.assertEqual(
            parsed["tests"]["TestWire"],
            [
                {"status": "PASS", "elapsed_ms": 12.0},
                {"status": "PASS", "elapsed_ms": 9.0},
            ],
        )
        self.assertFalse(parsed["package_failed"])

    def test_skip_and_package_failure_are_never_green(self) -> None:
        parsed = evaluator.parse_go_test_json("\n".join([event("skip", "TestLive", 0.0), event("fail", elapsed=0.1)]))
        self.assertEqual(parsed["skipped_tests"], ["TestLive"])
        self.assertTrue(parsed["package_failed"])

    def test_malformed_protocol_is_reported(self) -> None:
        parsed = evaluator.parse_go_test_json("not-json\n" + event("pass", "TestOK", 0.1))
        self.assertEqual(parsed["protocol_errors"], ["line 1 is not go test JSON"])

    def test_extracts_latency_marker_from_test_output(self) -> None:
        output = json.dumps(
            {
                "Action": "output",
                "Package": "example.test",
                "Test": "TestLiveWire",
                "Output": "    live_test.go:42: " + evaluator.LATENCY_MARKER + '{"samples":25,"p50_ms":300,"p95_ms":500,"max_ms":900,' + '"cold_retained":true,"path":"cli_identity_mcp_search"}\n',
            }
        )
        parsed = evaluator.parse_go_test_json(output)
        self.assertEqual(parsed["latency_metrics"]["TestLiveWire"][0]["p95_ms"], 500.0)

    def test_extracts_runtime_provenance_marker(self) -> None:
        output = json.dumps(
            {
                "Action": "output",
                "Package": "example.test",
                "Test": "TestLiveWire",
                "Output": "live_test.go:42: " + metadata.RUNTIME_MARKER + json.dumps(
                    {
                        "arcadedb_version": "26.7.3",
                        "mcp_server_version": "0.1.0",
                        "embedding_model": "embeddinggemma-300M-Q8_0.gguf",
                        "embedding_dimension": 768,
                    }
                ),
            }
        )
        parsed = evaluator.parse_go_test_json(output)
        self.assertEqual(parsed["runtime_metadata"]["TestLiveWire"][0]["embedding_dimension"], 768)


class SuiteRunnerTest(unittest.TestCase):
    def test_toolchain_stderr_is_not_merged_into_go_test_json(self) -> None:
        manifest = {
            "suites": [
                {
                    "id": "unit",
                    "tier": "deterministic",
                    "command": ["go", "test", "-json", "./example"],
                }
            ]
        }
        completed = subprocess.CompletedProcess(
            args=manifest["suites"][0]["command"],
            returncode=0,
            stdout="\n".join([event("pass", "TestWire", 0.01), event("pass", elapsed=0.02)]),
            stderr="go: downloading example.test/module v1.0.0\n",
        )
        with tempfile.TemporaryDirectory() as raw, mock.patch.object(
            evaluator.subprocess, "run", return_value=completed
        ) as run:
            suites = evaluator.run_suites(manifest, "all", pathlib.Path(raw), 30)

        self.assertTrue(suites["unit"]["passed"])
        self.assertEqual(suites["unit"]["protocol_errors"], [])
        self.assertEqual(suites["unit"]["stderr_line_count"], 1)
        self.assertIs(run.call_args.kwargs["stderr"], subprocess.PIPE)


class MetricParserTest(unittest.TestCase):
    def test_coverage_uses_statement_counts(self) -> None:
        parsed = evaluator.parse_coverage_profile("mode: atomic\nexample/a.go:1.1,2.1 3 1\nexample/a.go:3.1,4.1 1 0\n")
        self.assertEqual(parsed["covered_statements"], 3)
        self.assertEqual(parsed["total_statements"], 4)
        self.assertEqual(parsed["percent"], 75.0)

    def test_coverage_rejects_empty_profile(self) -> None:
        with self.assertRaisesRegex(ValueError, "no statements"):
            evaluator.parse_coverage_profile("mode: set\n")

    def test_parses_the_exact_live_latency_marker(self) -> None:
        marker = evaluator.parse_latency_marker('{"samples":25,"p50_ms":336.28,"p95_ms":498.36,"max_ms":1898.39,"cold_retained":true,"path":"cli_identity_mcp_search"}')
        self.assertEqual(marker["samples"], 25)
        self.assertEqual(marker["p95_ms"], 498.36)

    def test_latency_marker_rejects_short_or_wrong_scope_evidence(self) -> None:
        with self.assertRaisesRegex(ValueError, "at least 25"):
            evaluator.parse_latency_marker('{"samples":24,"p50_ms":1,"p95_ms":2,"max_ms":3,"cold_retained":true,"path":"cli_identity_mcp_search"}')
        with self.assertRaisesRegex(ValueError, "CLI identity"):
            evaluator.parse_latency_marker('{"samples":25,"p50_ms":1,"p95_ms":2,"max_ms":3,"cold_retained":false,"path":"database_only"}')


class ScoringTest(unittest.TestCase):
    candidate = "a" * 40

    def test_complete_default_evidence_can_reach_one_hundred(self) -> None:
        manifest = evaluator.load_manifest(None)
        report = evaluator.score(manifest, passing_suites(manifest), "all", self.candidate)
        self.assertEqual(report["memory_reliability_score"], 100.0)
        self.assertEqual(report["verdict"], "PASS")
        self.assertEqual(report["runtime"]["arcadedb_version"], "26.7.3")
        self.assertEqual(report["candidate"]["dirty_files"], [])
        self.assertEqual(len(report["evaluation_contract"]["dataset_hashes"]["scenario_manifest"]), 64)
        self.assertTrue(all(gate["status"] == "PASS" for gate in report["hard_gates"].values()))

    def test_missing_runtime_provenance_blocks_an_all_tier_score(self) -> None:
        manifest = evaluator.load_manifest(None)
        suites = passing_suites(manifest)
        suites["arcadedb_mcp_live"]["runtime_metadata"] = {}
        report = evaluator.score(manifest, suites, "all", self.candidate)
        self.assertEqual(report["memory_reliability_score"], 100.0)
        self.assertEqual(report["hard_gates"]["runtime_provenance"]["status"], "FAIL")
        self.assertEqual(report["verdict"], "FAIL")

    def test_threshold_is_strictly_greater_than_96_5(self) -> None:
        manifest = evaluator.default_manifest()
        first = manifest["scenarios"][0]
        second = manifest["scenarios"][1]
        first["points"] = 3.5
        second["points"] = 3.5
        suites = passing_suites(manifest)
        suites[first["evidence"]["suite"]]["tests"][first["evidence"]["test"]] = [{"status": "FAIL", "elapsed_ms": 10.0}]
        report = evaluator.score(manifest, suites, "all", self.candidate)
        self.assertEqual(report["memory_reliability_score"], 96.5)
        self.assertEqual(report["hard_gates"]["strict_score_above_96_5"]["status"], "FAIL")

    def test_hard_gate_cannot_be_compensated_by_high_score(self) -> None:
        manifest = evaluator.default_manifest()
        by_id = {item["id"]: item for item in manifest["scenarios"]}
        cross = by_id["zero_cross_tenant_leakage"]
        cross["points"] = 1
        by_id["database_name_isolation"]["points"] += 5
        cross["evidence"]["test"] = "TestOnlyHardGate"
        suites = passing_suites(manifest)
        evidence = cross["evidence"]
        suites[evidence["suite"]]["tests"][evidence["test"]] = [{"status": "FAIL", "elapsed_ms": 10.0}]
        report = evaluator.score(manifest, suites, "all", self.candidate)
        self.assertEqual(report["memory_reliability_score"], 99.0)
        self.assertEqual(report["hard_gates"]["zero_cross_tenant_leakage"]["status"], "FAIL")
        self.assertEqual(report["verdict"], "FAIL")

    def test_deterministic_report_never_claims_an_mrs(self) -> None:
        manifest = evaluator.load_manifest(None)
        report = evaluator.score(manifest, passing_suites(manifest), "deterministic", self.candidate)
        self.assertEqual(report["verdict"], "DETERMINISTIC_PASS")
        self.assertIsNone(report["memory_reliability_score"])
        self.assertFalse(report["mrs_eligible"])
        self.assertEqual(report["hard_gates"]["bounded_p95"]["status"], "NOT_EVALUATED")

    def test_report_round_trips_as_json(self) -> None:
        manifest = evaluator.load_manifest(None)
        report = evaluator.score(manifest, passing_suites(manifest), "deterministic", self.candidate)
        self.assertEqual(report["schema_version"], 1)
        with tempfile.TemporaryDirectory() as raw:
            path = pathlib.Path(raw) / "nested" / "report.json"
            evaluator.write_report(path, report)
            self.assertEqual(
                json.loads(path.read_text(encoding="utf-8"))["schema_id"],
                evaluator.SCHEMA_ID,
            )


class CandidateMetadataTest(unittest.TestCase):
    def test_candidate_state_hashes_tracked_and_untracked_dirty_files(self) -> None:
        with tempfile.TemporaryDirectory() as raw:
            repo = pathlib.Path(raw)
            subprocess.run(["git", "init", "-q"], cwd=repo, check=True)
            subprocess.run(["git", "config", "user.email", "test@example.test"], cwd=repo, check=True)
            subprocess.run(["git", "config", "user.name", "Test"], cwd=repo, check=True)
            tracked = repo / "tracked.txt"
            tracked.write_text("before\n", encoding="utf-8")
            subprocess.run(["git", "add", "tracked.txt"], cwd=repo, check=True)
            subprocess.run(["git", "commit", "-qm", "fixture"], cwd=repo, check=True)
            tracked.write_text("after\n", encoding="utf-8")
            (repo / "new.txt").write_text("new\n", encoding="utf-8")

            state = metadata.candidate_state(repo)
            self.assertEqual(len(state["commit"]), 40)
            self.assertEqual([item["path"] for item in state["dirty_files"]], ["new.txt", "tracked.txt"])
            self.assertTrue(all(len(item["sha256"]) == 64 for item in state["dirty_files"]))


if __name__ == "__main__":
    unittest.main()
