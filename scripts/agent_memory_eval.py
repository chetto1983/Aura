#!/usr/bin/env python3
from __future__ import annotations

import argparse
import datetime as dt
import json
import math
import pathlib
import subprocess
import sys
import tempfile
import time
from typing import Any

from agent_memory_eval_metadata import (
    RUNTIME_MARKER,
    candidate_state,
    manifest_sha256,
    parse_runtime_marker,
)


SCHEMA_ID = "aura.agent-memory-eval/v1"
REVISION = 1
TOTAL_POINTS = 100.0
PASS_THRESHOLD = 96.5
P95_MAX_MS = 1000.0
P95_MIN_SAMPLES = 25
LATENCY_MARKER = "AURA_AGENT_MEMORY_LATENCY_JSON="
LATENCY_PATH = "cli_identity_mcp_search"
DIMENSIONS = {
    "isolation_security": 25.0,
    "fact_provenance": 25.0,
    "retrieval_temporal": 25.0,
    "mcp_runtime_resilience": 15.0,
    "operability_coverage": 10.0,
}


def default_manifest() -> dict[str, Any]:
    aura_unit_filter = (
        "^(TestMemoryVerbMappingNegativeCases|"
        "TestMemoryNotConfigured|"
        "TestMemoryReadinessCheckRunsAnIsolatedFunctionalSearch|"
        "TestMemoryReadinessCheckRejectsSemanticAndTransportFailure)$"
    )
    # fmt: off
    suite_rows = [
        ("arcadedb_unit", "deterministic", False, ["go", "test", "-json", "-count=1", "./internal/arcadedb/"]),
        ("arcadedb_mcp_unit", "deterministic", False, ["go", "test", "-json", "-count=1", "./cmd/arcadedb-mcp/"]),
        ("aura_memory_unit", "deterministic", False, ["go", "test", "-json", "-count=1", "-run", aura_unit_filter, "./cmd/aura/"]),
        ("arcadedb_live", "live", True, ["go", "test", "-race", "-json", "-tags=arcadedb_integration", "-count=1", "-coverprofile={coverage_profile}", "-skip", "^TestLocomo|^TestMemoryVectorAnswersACrossLingualQuestion$", "./internal/arcadedb/"]),
        ("arcadedb_mcp_live", "live", False, ["go", "test", "-race", "-json", "-tags=arcadedb_integration", "-count=1", "-run", "^TestAgentMemoryMCPLive", "./cmd/arcadedb-mcp/"]),
        ("aura_memory_latency_live", "live", False, ["go", "test", "-race", "-json", "-tags=arcadedb_integration", "-count=1", "-run", "^TestAgentMemoryCLILiveSearchP95$", "./cmd/aura/"]),
        ("arcadedb_purge_live", "live", False, ["go", "test", "-race", "-json", "-tags=arcadedb_integration", "-count=1", "-run", "^TestArcadeMemoryPurgeLive", "./cmd/aura/"]),
    ]
    test_rows = [
        ("database_name_isolation", "deterministic", "isolation_security", 4, "negative", "arcadedb_unit", "TestDatabaseForIsolatesAndFailsClosed"),
        ("tenant_credentials_are_distinct", "deterministic", "isolation_security", 3, "positive", "arcadedb_unit", "TestTenantCredentialsAreDerivedAndDistinct"),
        ("tenant_credentials_fail_closed", "deterministic", "isolation_security", 3, "negative", "arcadedb_unit", "TestTenantCredentialsFailClosed"),
        ("mcp_rejects_unstamped_identity", "deterministic", "isolation_security", 3, "negative", "arcadedb_mcp_unit", "TestTenantsRefusesAnUnstampedCall"),
        ("zero_cross_tenant_leakage", "live", "isolation_security", 6, "negative", "arcadedb_mcp_live", "TestAgentMemoryMCPLiveInitializeListCallAndIsolation"),
        ("fact_upsert_preserves_graph_shape", "deterministic", "fact_provenance", 3, "positive", "arcadedb_unit", "TestUpsertFactCreatesEntitiesThenTheEdge"),
        ("incomplete_fact_is_rejected", "deterministic", "fact_provenance", 2, "negative", "arcadedb_unit", "TestValidateRejectsIncompleteFacts"),
        ("mcp_upsert_stores_provenance", "deterministic", "fact_provenance", 3, "positive", "arcadedb_mcp_unit", "TestUpsertFactStoresProvenance"),
        ("mcp_search_returns_provenance", "deterministic", "fact_provenance", 4, "positive", "arcadedb_mcp_unit", "TestSearchUsesTheFullTextIndexAndKeepsProvenance"),
        ("idempotent_multisource_fact_lifecycle", "live", "fact_provenance", 4, "positive", "arcadedb_live", "TestMemoryProvenanceReplayAttachDetachLifecycle"),
        ("hybrid_fusion_order", "deterministic", "retrieval_temporal", 3, "positive", "arcadedb_unit", "TestSearchFactsHybridRestoresFusionOrder"),
        ("hybrid_failure_falls_back", "deterministic", "retrieval_temporal", 2, "negative", "arcadedb_unit", "TestSearchFactsHybridFallsBackToLexical"),
        ("lucene_input_is_escaped", "deterministic", "retrieval_temporal", 2, "negative", "arcadedb_unit", "TestEscapeLuceneEscapesOperators"),
        ("default_search_is_temporal", "deterministic", "retrieval_temporal", 2, "negative", "arcadedb_unit", "TestSearchFactsAlwaysFiltersOnValidity"),
        # Was one_mcp_session_per_submission, whose evidence proved a whole onboarding
        # submission rode ONE MCP session. That property no longer exists to prove: the
        # operator profile moved to Postgres (migration 0097), so onboarding makes no MCP
        # call at all and the test went with the code path. The three points stay in this
        # dimension on the resilience property that IS still real -- the memory CLI degrades
        # cleanly with no memory server configured instead of panicking or reporting success.
        ("memory_cli_degrades_without_a_server", "deterministic", "mcp_runtime_resilience", 3, "negative", "aura_memory_unit", "TestMemoryNotConfigured"),
        ("memory_readiness_accepts_typed_empty_result", "deterministic", "mcp_runtime_resilience", 2, "positive", "aura_memory_unit", "TestMemoryReadinessCheckRunsAnIsolatedFunctionalSearch"),
        ("memory_readiness_rejects_failures", "deterministic", "mcp_runtime_resilience", 2, "negative", "aura_memory_unit", "TestMemoryReadinessCheckRejectsSemanticAndTransportFailure"),
        ("memory_cli_rejects_invalid_operations", "deterministic", "mcp_runtime_resilience", 1, "negative", "aura_memory_unit", "TestMemoryVerbMappingNegativeCases"),
        ("vector_schema_is_embeddinggemma_width", "deterministic", "operability_coverage", 1, "positive", "arcadedb_unit", "TestSchemaIsIdempotentAndCarriesTheVectorIndex"),
        ("graph_schema_exposes_entity_kinds", "deterministic", "operability_coverage", 1, "positive", "arcadedb_mcp_unit", "TestGraphSchemaSplitsTypesByKind"),
        ("purge_revokes_database_and_credential", "live", "isolation_security", 4, "negative", "arcadedb_purge_live", "TestArcadeMemoryPurgeLiveErasesDatabaseAndCredential"),
        ("purge_is_idempotent", "live", "isolation_security", 2, "positive", "arcadedb_purge_live", "TestArcadeMemoryPurgeLiveIsIdempotent"),
        ("live_fact_keeps_endpoints_and_provenance", "live", "fact_provenance", 5, "positive", "arcadedb_live", "TestRetrievedFactCarriesItsEndpointsAndProvenance"),
        ("live_supersession_keeps_history", "live", "fact_provenance", 4, "positive", "arcadedb_live", "TestSupersessionClosesTheWindowAndKeepsThePastQueryable"),
        ("expired_fact_is_not_current", "live", "retrieval_temporal", 3, "negative", "arcadedb_live", "TestExpiredFactIsExcludedFromAPresentTenseSearch"),
        ("historical_as_of_is_queryable", "live", "retrieval_temporal", 3, "positive", "arcadedb_live", "TestAsOfReturnsWhatWasTrueThen"),
        ("exact_traversal_respects_time", "live", "retrieval_temporal", 3, "positive", "arcadedb_live", "TestFactsAboutAnswersExactlyAndRespectsTime"),
        ("studio_graph_preserves_direction", "live", "retrieval_temporal", 2, "positive", "arcadedb_live", "TestStudioGraphSerializerReturnsFactEndpoints"),
        ("embeddinggemma_returns_768_dimensions", "live", "retrieval_temporal", 2, "positive", "arcadedb_live", "TestMemoryVectorSidecarReturnsTheIndexWidth"),
        ("nonexistent_fact_abstains_explicitly", "live", "retrieval_temporal", 3, "negative", "arcadedb_mcp_live", "TestAgentMemoryMCPLiveAbstainsOnNonexistentFact"),
        ("embedder_outage_degrades_to_lexical", "live", "mcp_runtime_resilience", 2, "negative", "arcadedb_live", "TestMemoryVectorDegradesWithoutAnEmbedder"),
        ("live_mcp_initialize_list_call", "live", "mcp_runtime_resilience", 5, "positive", "arcadedb_mcp_live", "TestAgentMemoryMCPLiveInitializeListCallAndIsolation"),
        ("live_schema_replay_is_idempotent", "live", "operability_coverage", 2, "positive", "arcadedb_live", "TestEnsureMemorySchemaIsIdempotentAgainstALiveDatabase"),
    ]
    # fmt: on
    suites = []
    for suite_id, tier, coverage, command in suite_rows:
        suite = {"id": suite_id, "tier": tier, "command": command}
        if coverage:
            suite["coverage"] = True
        suites.append(suite)
    scenarios = [
        {
            "id": scenario_id,
            "tier": tier,
            "dimension": dimension,
            "points": points,
            "polarity": polarity,
            "evidence": {"kind": "go_test", "suite": suite, "test": test_name},
        }
        for scenario_id, tier, dimension, points, polarity, suite, test_name in test_rows
    ]
    scenarios.append(
        {
            "id": "live_mcp_bounded_p95",
            "tier": "live",
            "dimension": "operability_coverage",
            "points": 2,
            "polarity": "positive",
            "evidence": {
                "kind": "p95",
                "suite": "aura_memory_latency_live",
                "test": "TestAgentMemoryCLILiveSearchP95",
                "minimum_samples": P95_MIN_SAMPLES,
                "maximum_ms": P95_MAX_MS,
                "sequential": True,
                "cold_samples_retained": True,
                "path": LATENCY_PATH,
            },
        }
    )
    scenarios.append(
        {
            "id": "arcadedb_package_coverage",
            "tier": "live",
            "dimension": "operability_coverage",
            "points": 4,
            "polarity": "positive",
            "evidence": {"kind": "coverage", "suite": "arcadedb_live", "minimum_percent": 85.0},
        }
    )
    return {
        "schema_id": SCHEMA_ID,
        "revision": REVISION,
        "total_points": TOTAL_POINTS,
        "pass_threshold": {"operator": ">", "value": PASS_THRESHOLD},
        "dimensions": DIMENSIONS,
        "suites": suites,
        "scenarios": scenarios,
        "hard_gates": {
            "zero_cross_tenant_leakage": ["zero_cross_tenant_leakage"],
            "live_mcp_initialize_list_call": ["live_mcp_initialize_list_call"],
            "embeddinggemma_768": ["embeddinggemma_returns_768_dimensions"],
            "explicit_abstention": ["nonexistent_fact_abstains_explicitly"],
            "package_coverage_85": ["arcadedb_package_coverage"],
            "bounded_p95": ["live_mcp_bounded_p95"],
        },
    }


def validate_manifest(manifest: dict[str, Any]) -> dict[str, Any]:
    if manifest.get("schema_id") != SCHEMA_ID or manifest.get("revision") != REVISION:
        raise ValueError(f"manifest must be {SCHEMA_ID} revision {REVISION}")
    threshold = manifest.get("pass_threshold")
    if threshold != {"operator": ">", "value": PASS_THRESHOLD}:
        raise ValueError("MRS threshold must be strictly > 96.5")
    if manifest.get("total_points") != TOTAL_POINTS:
        raise ValueError("manifest total_points must be 100")
    if manifest.get("dimensions") != DIMENSIONS:
        raise ValueError("manifest dimensions do not match the v1 contract")
    suites = manifest.get("suites")
    scenarios = manifest.get("scenarios")
    if not isinstance(suites, list) or not isinstance(scenarios, list):
        raise ValueError("manifest suites and scenarios must be arrays")
    suite_ids: set[str] = set()
    for suite in suites:
        if not isinstance(suite, dict) or not isinstance(suite.get("id"), str):
            raise ValueError("every suite needs a string id")
        if suite["id"] in suite_ids:
            raise ValueError(f"duplicate suite {suite['id']}")
        suite_ids.add(suite["id"])
        if suite.get("tier") not in {"deterministic", "live"}:
            raise ValueError(f"suite {suite['id']} has an invalid tier")
        command = suite.get("command")
        if not isinstance(command, list) or not command or not all(isinstance(token, str) and token for token in command):
            raise ValueError(f"suite {suite['id']} command must be non-empty argv")
    scenario_ids: set[str] = set()
    dimension_points = {name: 0.0 for name in DIMENSIONS}
    for scenario in scenarios:
        if not isinstance(scenario, dict) or not isinstance(scenario.get("id"), str):
            raise ValueError("every scenario needs a string id")
        scenario_id = scenario["id"]
        if scenario_id in scenario_ids:
            raise ValueError(f"duplicate scenario {scenario_id}")
        scenario_ids.add(scenario_id)
        tier = scenario.get("tier")
        dimension = scenario.get("dimension")
        points = scenario.get("points")
        if tier not in {"deterministic", "live"} or dimension not in DIMENSIONS:
            raise ValueError(f"scenario {scenario_id} has an invalid tier or dimension")
        if not isinstance(points, (int, float)) or isinstance(points, bool) or points <= 0:
            raise ValueError(f"scenario {scenario_id} points must be positive")
        if scenario.get("polarity") not in {"positive", "negative"}:
            raise ValueError(f"scenario {scenario_id} polarity must be positive or negative")
        dimension_points[dimension] += float(points)
        evidence = scenario.get("evidence")
        if evidence is None:
            if not scenario.get("missing_reason"):
                raise ValueError(f"missing scenario {scenario_id} needs a reason")
            continue
        if not isinstance(evidence, dict) or evidence.get("suite") not in suite_ids:
            raise ValueError(f"scenario {scenario_id} references an unknown suite")
        kind = evidence.get("kind")
        if kind == "go_test" and not isinstance(evidence.get("test"), str):
            raise ValueError(f"scenario {scenario_id} needs a Go test name")
        if kind == "coverage" and not isinstance(evidence.get("minimum_percent"), (int, float)):
            raise ValueError(f"scenario {scenario_id} needs a coverage floor")
        if kind == "p95":
            valid_p95 = isinstance(evidence.get("test"), str) and evidence.get("maximum_ms") == P95_MAX_MS and evidence.get("minimum_samples") == P95_MIN_SAMPLES and evidence.get("sequential") is True and evidence.get("cold_samples_retained") is True and evidence.get("path") == LATENCY_PATH
            if not valid_p95:
                raise ValueError(f"scenario {scenario_id} does not implement the v1 p95 contract")
        if kind not in {"go_test", "coverage", "p95"}:
            raise ValueError(f"scenario {scenario_id} has unsupported evidence {kind!r}")
    if dimension_points != DIMENSIONS:
        raise ValueError(f"scenario points by dimension are {dimension_points}, want {DIMENSIONS}")
    if not isinstance(manifest.get("hard_gates"), dict):
        raise ValueError("manifest hard_gates must be an object")
    for gate, required in manifest["hard_gates"].items():
        if not isinstance(required, list) or not required or any(item not in scenario_ids for item in required):
            raise ValueError(f"hard gate {gate} references an unknown scenario")
    return manifest


def load_manifest(path: pathlib.Path | None) -> dict[str, Any]:
    if path is None:
        manifest = default_manifest()
    else:
        try:
            manifest = json.loads(path.read_text(encoding="utf-8"))
        except (OSError, json.JSONDecodeError) as exc:
            raise ValueError(f"cannot read manifest {path}: {exc}") from exc
        if not isinstance(manifest, dict):
            raise ValueError("manifest root must be an object")
    return validate_manifest(manifest)


def parse_latency_marker(raw: str) -> dict[str, Any]:
    try:
        payload = json.loads(raw.strip())
    except json.JSONDecodeError as exc:
        raise ValueError(f"invalid latency marker JSON: {exc}") from exc
    if not isinstance(payload, dict):
        raise ValueError("latency marker root must be an object")
    samples = payload.get("samples")
    if not isinstance(samples, int) or isinstance(samples, bool) or samples < P95_MIN_SAMPLES:
        raise ValueError(f"latency marker needs at least {P95_MIN_SAMPLES} samples")
    values: dict[str, float] = {}
    for name in ("p50_ms", "p95_ms", "max_ms"):
        value = payload.get(name)
        if not isinstance(value, (int, float)) or isinstance(value, bool) or not math.isfinite(value) or value < 0:
            raise ValueError(f"latency marker {name} must be a finite non-negative number")
        values[name] = float(value)
    if not values["p50_ms"] <= values["p95_ms"] <= values["max_ms"]:
        raise ValueError("latency marker percentiles are not ordered")
    if payload.get("cold_retained") is not True or payload.get("path") != LATENCY_PATH:
        raise ValueError("latency marker must retain cold samples on the CLI identity/MCP/search path")
    return {"samples": samples, "cold_retained": True, "path": LATENCY_PATH, **values}


def parse_go_test_json(output: str) -> dict[str, Any]:
    tests: dict[str, list[dict[str, Any]]] = {}
    latency_metrics: dict[str, list[dict[str, Any]]] = {}
    runtime_metadata: dict[str, list[dict[str, Any]]] = {}
    skipped: set[str] = set()
    package_failed = False
    protocol_errors: list[str] = []
    for line_number, raw in enumerate(output.splitlines(), 1):
        if not raw.strip():
            continue
        try:
            event = json.loads(raw)
        except json.JSONDecodeError:
            protocol_errors.append(f"line {line_number} is not go test JSON")
            continue
        if not isinstance(event, dict) or not isinstance(event.get("Action"), str):
            protocol_errors.append(f"line {line_number} has no Action")
            continue
        action = event["Action"]
        name = event.get("Test")
        emitted = event.get("Output")
        if action == "output" and isinstance(name, str) and isinstance(emitted, str) and LATENCY_MARKER in emitted:
            try:
                latency_metrics.setdefault(name, []).append(parse_latency_marker(emitted.partition(LATENCY_MARKER)[2]))
            except ValueError as exc:
                protocol_errors.append(f"test {name}: {exc}")
        if action == "output" and isinstance(name, str) and isinstance(emitted, str) and RUNTIME_MARKER in emitted:
            try:
                runtime_metadata.setdefault(name, []).append(parse_runtime_marker(emitted.partition(RUNTIME_MARKER)[2]))
            except ValueError as exc:
                protocol_errors.append(f"test {name}: {exc}")
        if action == "fail" and name is None:
            package_failed = True
        if not isinstance(name, str) or action not in {"pass", "fail", "skip"}:
            continue
        elapsed = event.get("Elapsed", 0.0)
        if not isinstance(elapsed, (int, float)) or isinstance(elapsed, bool) or elapsed < 0:
            protocol_errors.append(f"test {name} has invalid elapsed time")
            continue
        tests.setdefault(name, []).append({"status": action.upper(), "elapsed_ms": float(elapsed) * 1000})
        if action == "skip":
            skipped.add(name)
    return {
        "tests": tests,
        "latency_metrics": latency_metrics,
        "runtime_metadata": runtime_metadata,
        "skipped_tests": sorted(skipped),
        "package_failed": package_failed,
        "protocol_errors": protocol_errors,
    }


def parse_coverage_profile(text: str) -> dict[str, Any]:
    lines = [line.strip() for line in text.splitlines() if line.strip()]
    if not lines or not lines[0].startswith("mode: "):
        raise ValueError("coverage profile has no mode header")
    covered = 0
    total = 0
    for line in lines[1:]:
        fields = line.rsplit(" ", 2)
        if len(fields) != 3:
            raise ValueError(f"invalid coverage row: {line}")
        try:
            statements = int(fields[1])
            count = int(fields[2])
        except ValueError as exc:
            raise ValueError(f"invalid coverage counts: {line}") from exc
        if statements < 0 or count < 0:
            raise ValueError(f"negative coverage counts: {line}")
        total += statements
        if count > 0:
            covered += statements
    if total == 0:
        raise ValueError("coverage profile has no statements")
    return {
        "covered_statements": covered,
        "total_statements": total,
        "percent": covered * 100 / total,
    }


def run_suites(manifest: dict[str, Any], tier: str, repo: pathlib.Path, timeout_seconds: int) -> dict[str, Any]:
    selected = [suite for suite in manifest["suites"] if tier == "all" or suite["tier"] == tier]
    results: dict[str, Any] = {}
    with tempfile.TemporaryDirectory(prefix="aura-agent-memory-eval-") as raw:
        temp_root = pathlib.Path(raw)
        for suite in selected:
            coverage_path = temp_root / f"{suite['id']}.cover"
            command = [token.replace("{coverage_profile}", str(coverage_path)) for token in suite["command"]]
            started = time.monotonic()
            try:
                completed = subprocess.run(
                    command,
                    cwd=repo,
                    text=True,
                    stdout=subprocess.PIPE,
                    stderr=subprocess.PIPE,
                    check=False,
                    timeout=timeout_seconds,
                )
                parsed = parse_go_test_json(completed.stdout)
                result: dict[str, Any] = {
                    "id": suite["id"],
                    "tier": suite["tier"],
                    "executed": True,
                    "command": command,
                    "return_code": completed.returncode,
                    "elapsed_ms": (time.monotonic() - started) * 1000,
                    "stderr_line_count": sum(1 for line in completed.stderr.splitlines() if line.strip()),
                    **parsed,
                }
                if suite.get("coverage"):
                    try:
                        result["coverage"] = parse_coverage_profile(coverage_path.read_text(encoding="utf-8"))
                    except (OSError, ValueError) as exc:
                        result["coverage_error"] = str(exc)
                result["passed"] = completed.returncode == 0 and not parsed["package_failed"] and not parsed["protocol_errors"] and not parsed["skipped_tests"]
            except (OSError, subprocess.SubprocessError) as exc:
                result = {
                    "id": suite["id"],
                    "tier": suite["tier"],
                    "executed": False,
                    "command": command,
                    "elapsed_ms": (time.monotonic() - started) * 1000,
                    "passed": False,
                    "error": str(exc),
                    "tests": {},
                    "skipped_tests": [],
                    "latency_metrics": {},
                    "runtime_metadata": {},
                    "package_failed": False,
                    "protocol_errors": [],
                }
            results[suite["id"]] = result
    return results


def evaluate_evidence(scenario: dict[str, Any], suites: dict[str, Any]) -> tuple[str, dict[str, Any]]:
    evidence = scenario.get("evidence")
    if evidence is None:
        return "MISSING", {"reason": scenario["missing_reason"]}
    suite = suites.get(evidence["suite"])
    if suite is None or not suite.get("executed"):
        return "MISSING", {"reason": f"suite {evidence['suite']} did not execute"}
    if evidence["kind"] == "coverage":
        coverage = suite.get("coverage")
        if not isinstance(coverage, dict):
            return "MISSING", {"reason": suite.get("coverage_error", "coverage evidence absent")}
        actual = float(coverage["percent"])
        minimum = float(evidence["minimum_percent"])
        return ("PASS" if actual >= minimum else "FAIL"), {
            "actual_percent": actual,
            "minimum_percent": minimum,
            **coverage,
        }
    runs = suite.get("tests", {}).get(evidence["test"], [])
    if not runs:
        return "MISSING", {"reason": f"test {evidence['test']} did not run"}
    statuses = [run["status"] for run in runs]
    if "SKIP" in statuses:
        return "SKIP", {"runs": runs}
    if "FAIL" in statuses or any(status != "PASS" for status in statuses):
        return "FAIL", {"runs": runs}
    minimum_runs = int(evidence.get("minimum_runs", 1))
    if len(runs) < minimum_runs:
        return "MISSING", {
            "reason": f"only {len(runs)} of {minimum_runs} required runs executed",
            "runs": runs,
        }
    if evidence["kind"] == "p95":
        metrics = suite.get("latency_metrics", {}).get(evidence["test"], [])
        if not metrics:
            return "MISSING", {"reason": f"test {evidence['test']} emitted no {LATENCY_MARKER} marker"}
        if len(metrics) != 1:
            return "FAIL", {"reason": f"test {evidence['test']} emitted {len(metrics)} latency markers, want exactly one"}
        metric = metrics[0]
        if metric["samples"] < int(evidence["minimum_samples"]):
            return "FAIL", {
                "reason": "latency sample count is below the manifest floor",
                **metric,
            }
        if metric["path"] != evidence["path"] or metric["cold_retained"] is not evidence["cold_samples_retained"]:
            return "FAIL", {
                "reason": "latency marker scope does not match the manifest",
                **metric,
            }
        actual = float(metric["p95_ms"])
        maximum = float(evidence["maximum_ms"])
        return ("PASS" if actual <= maximum else "FAIL"), {
            **metric,
            "maximum_ms": maximum,
        }
    return "PASS", {"runs": runs}


def score(manifest: dict[str, Any], suites: dict[str, Any], tier: str, candidate: str | dict[str, Any]) -> dict[str, Any]:
    if isinstance(candidate, str):
        candidate = {"commit": candidate, "dirty_files": []}
    selected = [scenario for scenario in manifest["scenarios"] if tier == "all" or scenario["tier"] == tier]
    scenario_results: list[dict[str, Any]] = []
    by_id: dict[str, dict[str, Any]] = {}
    earned = 0.0
    selected_points = 0.0
    dimensions = {name: {"earned": 0.0, "possible": 0.0} for name in DIMENSIONS}
    for scenario in selected:
        status, detail = evaluate_evidence(scenario, suites)
        points = float(scenario["points"])
        awarded = points if status == "PASS" else 0.0
        result = {
            **scenario,
            "status": status,
            "awarded_points": awarded,
            "detail": detail,
        }
        scenario_results.append(result)
        by_id[scenario["id"]] = result
        earned += awarded
        selected_points += points
        dimensions[scenario["dimension"]]["earned"] += awarded
        dimensions[scenario["dimension"]]["possible"] += points
    selected_suite_results = [result for result in suites.values() if tier == "all" or result["tier"] == tier]
    suite_integrity = bool(selected_suite_results) and all(result.get("passed") is True for result in selected_suite_results)
    no_missing_or_skipped = all(result["status"] not in {"MISSING", "SKIP"} for result in scenario_results)
    hard_gates: dict[str, dict[str, Any]] = {
        "suite_integrity": {"status": "PASS" if suite_integrity else "FAIL"},
        "no_missing_or_skipped_scenarios": {"status": "PASS" if no_missing_or_skipped else "FAIL"},
    }
    runtime_runs = suites.get("arcadedb_mcp_live", {}).get("runtime_metadata", {}).get(
        "TestAgentMemoryMCPLiveInitializeListCallAndIsolation", []
    )
    runtime = runtime_runs[0] if len(runtime_runs) == 1 else {}
    hard_gates["runtime_provenance"] = {
        "status": "NOT_EVALUATED" if tier == "deterministic" else ("PASS" if len(runtime_runs) == 1 else "FAIL")
    }
    for gate, required in manifest["hard_gates"].items():
        selected_required = [by_id[item] for item in required if item in by_id]
        if not selected_required:
            status = "NOT_EVALUATED"
        else:
            status = "PASS" if all(item["status"] == "PASS" for item in selected_required) else "FAIL"
        hard_gates[gate] = {"status": status, "scenarios": required}
    selected_score = earned * 100 / selected_points if selected_points else 0.0
    mrs = earned if tier == "all" else None
    if tier == "all":
        threshold_passed = bool(mrs is not None and mrs > PASS_THRESHOLD)
        hard_gates["strict_score_above_96_5"] = {"status": "PASS" if threshold_passed else "FAIL"}
        passed = threshold_passed and all(gate["status"] == "PASS" for gate in hard_gates.values())
        verdict = "PASS" if passed else "FAIL"
    else:
        passed = suite_integrity and all(result["status"] == "PASS" for result in scenario_results)
        verdict = f"{tier.upper()}_{'PASS' if passed else 'FAIL'}"
    return {
        "schema_id": SCHEMA_ID,
        "revision": REVISION,
        "generated_at": dt.datetime.now(dt.timezone.utc).isoformat(),
        "candidate_commit": candidate["commit"],
        "candidate": candidate,
        "runtime": runtime,
        "evaluation_contract": {
            "dataset_hashes": {"scenario_manifest": manifest_sha256(manifest)},
            "seed": "not_applicable_deterministic_fixtures",
        },
        "tier": tier,
        "verdict": verdict,
        "passed": passed,
        "mrs_eligible": tier == "all",
        "memory_reliability_score": mrs,
        "selected_score_percent": selected_score,
        "earned_points": earned,
        "selected_points": selected_points,
        "total_points": TOTAL_POINTS,
        "pass_threshold": manifest["pass_threshold"],
        "hard_gates": hard_gates,
        "dimensions": dimensions,
        "scenarios": scenario_results,
        "suites": selected_suite_results,
    }


def write_report(path: pathlib.Path, report: dict[str, Any]) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(report, indent=2) + "\n", encoding="utf-8")


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Aura Agent Memory reliability evaluator")
    parser.add_argument("--manifest", type=pathlib.Path)
    parser.add_argument("--tier", choices=("deterministic", "live", "all"), default="all")
    parser.add_argument("--timeout-seconds", type=int, default=900)
    parser.add_argument(
        "--output",
        type=pathlib.Path,
        default=pathlib.Path("artifacts/production-readiness/agent-memory-eval-report.json"),
    )
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    if args.timeout_seconds <= 0:
        print("agent-memory-eval: FAIL: timeout must be positive", file=sys.stderr)
        return 1
    repo = pathlib.Path(__file__).resolve().parents[1]
    try:
        manifest = load_manifest(args.manifest)
        candidate = candidate_state(repo)
        suites = run_suites(manifest, args.tier, repo, args.timeout_seconds)
        report = score(manifest, suites, args.tier, candidate)
        write_report(args.output, report)
    except (OSError, RuntimeError, ValueError) as exc:
        print(f"agent-memory-eval: FAIL: {exc}", file=sys.stderr)
        return 1
    score_text = "not MRS-eligible" if report["memory_reliability_score"] is None else f"MRS={report['memory_reliability_score']:.2f}"
    print(f"agent-memory-eval: {report['verdict']}: {score_text}; report={args.output}")
    return 0 if report["passed"] else 1


if __name__ == "__main__":
    raise SystemExit(main())
