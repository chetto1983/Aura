from __future__ import annotations

import json
import pathlib
import subprocess
import time
from typing import Any

from agent_memory_eval_phase49_batch import BATCH_MARKER, attach_batch_evidence, evaluate_batch_atomicity, extract_batch_markers
from agent_memory_eval_running_aura import attach_running_aura_evidence, evaluate_running_aura_conversation, run_running_aura_conversation


SCHEMA_ID = "aura.phase49-memory-evidence/v1"
SCENARIOS = ("mixed_tier_recall", "reasoning_isolation")
EFFECTIVE_PATHS = {"facts", "conversations", "mixed", "reasoning"}
BACKEND_PATHS = {"graph", "hybrid", "lexical"}
OTEL_ATTRIBUTES = {
    "effective_path": "memory.recall.effective_path",
    "path": "memory.recall.path",
}
MIXED_TIER_MARKER = "AURA_AGENT_MEMORY_MIXED_TIER_JSON="
SURFACE_MARKER = "AURA_AGENT_MEMORY_SURFACE_JSON="
MIXED_TIER_CASES = {
    "mixed": ("mixed", "hybrid"),
    "query": ("facts", "hybrid"),
    "entity": ("facts", "graph"),
    "forced_fallback": ("facts", "lexical"),
}
MEMORY_RECALL_INPUT_PROPERTIES = (
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
)
MEMORY_RECALL_MODES = ("semantic", "recent", "open", "scroll", "reasoning")


def _empty_observation() -> dict[str, Any]:
    return {
        "executed": False,
        "status": "not_observed",
        "retrieval": {"effective_path": None, "path": None},
        "tier_counts": {"facts": 0, "conversations": 0, "reasoning": 0},
        "tier_ranks": {"facts": [], "conversations": [], "reasoning": []},
        "latency_ms": None,
        "abstention": {"observed": False, "abstained": None, "reason": None},
        "otel": {
            OTEL_ATTRIBUTES["effective_path"]: None,
            OTEL_ATTRIBUTES["path"]: None,
        },
    }


def default_contract() -> dict[str, Any]:
    final = {scenario: _empty_observation() for scenario in SCENARIOS}
    return {
        "schema_id": SCHEMA_ID,
        "scenario_definitions": [
            {
                "id": "mixed_tier_recall",
                "required_effective_path": "mixed",
                "required_backend_paths": ["hybrid", "graph", "lexical"],
                "requires_otel_match": True,
            },
            {
                "id": "reasoning_isolation",
                "required_effective_path": "reasoning",
                "required_backend_paths": ["graph", "hybrid", "lexical"],
                "requires_explicit_only": True,
                "requires_otel_match": True,
            },
        ],
        "otel_attributes": dict(OTEL_ATTRIBUTES),
        "observations": {
            "baseline": {
                "mixed_tier_recall": {
                    "executed": True,
                    "status": "observed_partial",
                    "retrieval": {"effective_path": "facts", "path": "hybrid"},
                    "tier_counts": {"facts": 1, "conversations": 0, "reasoning": 0},
                    "tier_ranks": {"facts": [1], "conversations": [], "reasoning": []},
                    "latency_ms": {
                        "samples": 25,
                        "p50": 39.358,
                        "p95": 85.395,
                        "max": 122.599,
                        "cold_retained": True,
                    },
                    "abstention": {
                        "observed": True,
                        "abstained": True,
                        "reason": "no_qualified_candidates",
                        "backend_path": "hybrid",
                    },
                    "otel": {
                        OTEL_ATTRIBUTES["effective_path"]: "facts",
                        OTEL_ATTRIBUTES["path"]: "hybrid",
                    },
                },
                "reasoning_isolation": _empty_observation(),
            },
            "final": final,
        },
    }


def validate(contract: Any) -> None:
    if not isinstance(contract, dict) or contract.get("schema_id") != SCHEMA_ID:
        raise ValueError(f"phase49_evidence must use {SCHEMA_ID}")
    definitions = contract.get("scenario_definitions")
    if not isinstance(definitions, list) or [item.get("id") for item in definitions if isinstance(item, dict)] != list(SCENARIOS):
        raise ValueError("phase49_evidence must define mixed_tier_recall and reasoning_isolation in order")
    if contract.get("otel_attributes") != OTEL_ATTRIBUTES:
        raise ValueError("phase49_evidence OTel attribute names drifted")
    observations = contract.get("observations")
    if not isinstance(observations, dict) or set(observations) != {"baseline", "final"}:
        raise ValueError("phase49_evidence needs baseline and final observations")
    for phase, phase_observations in observations.items():
        if not isinstance(phase_observations, dict) or set(phase_observations) != set(SCENARIOS):
            raise ValueError(f"phase49_evidence {phase} observations must cover both scenarios")
        for scenario, observation in phase_observations.items():
            _validate_observation(phase, scenario, observation)


def _validate_observation(phase: str, scenario: str, observation: Any) -> None:
    if not isinstance(observation, dict) or not isinstance(observation.get("executed"), bool):
        raise ValueError(f"phase49_evidence {phase}/{scenario} needs an executed boolean")
    status = observation.get("status")
    if not observation["executed"] and status == "pass":
        raise ValueError(f"phase49_evidence {phase}/{scenario} unobserved evidence cannot claim pass")
    if status not in {"not_observed", "observed_partial", "observed_complete"}:
        raise ValueError(f"phase49_evidence {phase}/{scenario} has invalid status {status!r}")
    retrieval = observation.get("retrieval")
    if not isinstance(retrieval, dict):
        raise ValueError(f"phase49_evidence {phase}/{scenario} needs retrieval evidence")
    effective_path = retrieval.get("effective_path")
    backend_path = retrieval.get("path")
    if not observation["executed"]:
        if status != "not_observed" or effective_path is not None or backend_path is not None:
            raise ValueError(f"phase49_evidence {phase}/{scenario} unobserved evidence cannot claim a path or pass")
    else:
        if status == "not_observed":
            raise ValueError(f"phase49_evidence {phase}/{scenario} executed evidence cannot be unobserved")
        if effective_path not in EFFECTIVE_PATHS:
            raise ValueError(f"phase49_evidence {phase}/{scenario} has invalid effective path")
        if backend_path not in BACKEND_PATHS:
            raise ValueError(f"phase49_evidence {phase}/{scenario} has invalid backend path")
    counts = observation.get("tier_counts")
    ranks = observation.get("tier_ranks")
    expected_tiers = {"facts", "conversations", "reasoning"}
    if not isinstance(counts, dict) or set(counts) != expected_tiers or any(not isinstance(value, int) or isinstance(value, bool) or value < 0 for value in counts.values()):
        raise ValueError(f"phase49_evidence {phase}/{scenario} has invalid tier counts")
    if not isinstance(ranks, dict) or set(ranks) != expected_tiers:
        raise ValueError(f"phase49_evidence {phase}/{scenario} has invalid tier ranks")
    for tier in expected_tiers:
        values = ranks[tier]
        if not isinstance(values, list) or len(values) != counts[tier] or any(not isinstance(rank, int) or isinstance(rank, bool) or rank <= 0 for rank in values):
            raise ValueError(f"phase49_evidence {phase}/{scenario} rank/count mismatch for {tier}")
    if observation["executed"] and effective_path != _effective_path_from_counts(counts):
        raise ValueError(f"phase49_evidence {phase}/{scenario} effective path does not match tier contribution")
    otel = observation.get("otel")
    if not isinstance(otel, dict) or set(otel) != set(OTEL_ATTRIBUTES.values()):
        raise ValueError(f"phase49_evidence {phase}/{scenario} has invalid OTel evidence")
    if otel[OTEL_ATTRIBUTES["effective_path"]] != effective_path or otel[OTEL_ATTRIBUTES["path"]] != backend_path:
        raise ValueError(f"phase49_evidence {phase}/{scenario} response and OTel paths disagree")


def _effective_path_from_counts(counts: dict[str, int]) -> str | None:
    if counts["reasoning"] > 0 and counts["facts"] == 0 and counts["conversations"] == 0:
        return "reasoning"
    if counts["facts"] > 0 and counts["conversations"] > 0:
        return "mixed"
    if counts["facts"] > 0:
        return "facts"
    if counts["conversations"] > 0:
        return "conversations"
    return None


def evaluate_mixed_tier_recall(evidence: Any) -> dict[str, Any]:
    failures: list[str] = []
    if not isinstance(evidence, dict) or evidence.get("scenario") != "mixed_tier_recall":
        evidence = {}
        failures.append("mixed_tier_recall scenario is absent")
    scenarios = evidence.get("scenarios")
    if not isinstance(scenarios, list) or not scenarios:
        scenarios = []
        failures.append("zero mixed_tier_recall scenarios executed")
    by_name: dict[str, dict[str, Any]] = {}
    for case in scenarios:
        if not isinstance(case, dict) or not isinstance(case.get("name"), str):
            failures.append("route evidence contains an unnamed scenario")
            continue
        name = case["name"]
        if name in by_name:
            failures.append(f"route scenario {name} executed more than once")
            continue
        by_name[name] = case
    missing = set(MIXED_TIER_CASES) - set(by_name)
    extra = set(by_name) - set(MIXED_TIER_CASES)
    if missing:
        failures.append(f"route scenarios missing: {sorted(missing)}")
    if extra:
        failures.append(f"unexpected route scenarios: {sorted(extra)}")
    for name, (effective_path, backend_path) in MIXED_TIER_CASES.items():
        case = by_name.get(name)
        if case is not None:
            failures.extend(_validate_mixed_tier_case(name, case, effective_path, backend_path))
    surface = evidence.get("surface")
    operations = surface.get("retrieval_operations") if isinstance(surface, dict) else None
    if operations != ["memory_recall"]:
        failures.append(f"model-facing retrieval surface is {operations!r}, want ['memory_recall']")
    properties = surface.get("memory_recall_input_properties") if isinstance(surface, dict) else None
    if properties != list(MEMORY_RECALL_INPUT_PROPERTIES):
        failures.append(
            f"memory_recall input properties are {properties!r}, want {list(MEMORY_RECALL_INPUT_PROPERTIES)!r}"
        )
    modes = surface.get("memory_recall_mode_enum") if isinstance(surface, dict) else None
    if modes != list(MEMORY_RECALL_MODES):
        failures.append(f"memory_recall mode enum is {modes!r}, want {list(MEMORY_RECALL_MODES)!r}")
    mixed_response = by_name.get("mixed", {}).get("response", {})
    tier_counts = mixed_response.get("tier_counts", {}) if isinstance(mixed_response, dict) else {}
    backend_paths = {
        name: case.get("response", {}).get("path")
        for name, case in by_name.items()
        if name in {"query", "entity", "forced_fallback"} and isinstance(case.get("response"), dict)
    }
    return {
        "scenario": "mixed_tier_recall",
        "executed_scenarios": len(scenarios),
        "passed": not failures,
        "status": "PASS" if not failures else "FAIL",
        "effective_path": mixed_response.get("effective_path") if isinstance(mixed_response, dict) else None,
        "tier_counts": tier_counts,
        "backend_paths": backend_paths,
        "surface": surface,
        "failures": failures,
        "route_evidence": scenarios,
    }


def _validate_mixed_tier_case(
    name: str,
    case: dict[str, Any],
    expected_effective_path: str,
    expected_backend_path: str,
) -> list[str]:
    failures: list[str] = []
    response = case.get("response")
    otel = case.get("otel")
    returned = case.get("returned_tier_counts")
    if not isinstance(response, dict):
        return [f"{name}: response retrieval metadata is absent"]
    effective_path = response.get("effective_path")
    backend_path = response.get("path")
    if effective_path != expected_effective_path:
        failures.append(f"{name}: response effective_path={effective_path!r}, want {expected_effective_path!r}")
    if backend_path != expected_backend_path:
        failures.append(f"{name}: response path={backend_path!r}, want {expected_backend_path!r}")
    counts = response.get("tier_counts")
    if not _valid_tier_counts(counts):
        failures.append(f"{name}: response tier counts are absent or invalid")
        counts = {"facts": 0, "conversations": 0, "reasoning": 0}
    if returned != counts:
        failures.append(f"{name}: response tier counts disagree with returned provenance")
    if _effective_path_from_counts(counts) != effective_path:
        failures.append(f"{name}: effective_path is conflated with or disagrees with tier contribution")
    if name == "mixed":
        if counts["facts"] < 1 or counts["conversations"] < 1 or counts["reasoning"] != 0:
            failures.append("mixed: fact and historical-conversation evidence are both required")
    elif counts["facts"] < 1 or counts["conversations"] != 0 or counts["reasoning"] != 0:
        failures.append(f"{name}: backend case must return fact-only evidence")
    if not isinstance(otel, dict):
        failures.append(f"{name}: OTel evidence is absent")
    else:
        expected_otel = {
            OTEL_ATTRIBUTES["effective_path"]: effective_path,
            OTEL_ATTRIBUTES["path"]: backend_path,
            "memory.recall.fact_count": counts["facts"],
            "memory.recall.conversation_count": counts["conversations"],
            "memory.recall.reasoning_count": counts["reasoning"],
        }
        for key, value in expected_otel.items():
            if otel.get(key) != value:
                failures.append(f"{name}: OTel {key}={otel.get(key)!r}, response={value!r}")
    for field in ("active_source_count", "foreign_source_count"):
        if case.get(field) != 0:
            failures.append(f"{name}: {field} must be zero")
    return failures


def _valid_tier_counts(value: Any) -> bool:
    return (
        isinstance(value, dict)
        and set(value) == {"facts", "conversations", "reasoning"}
        and all(isinstance(count, int) and not isinstance(count, bool) and count >= 0 for count in value.values())
    )


def run_mixed_tier_recall(repo: pathlib.Path, timeout_seconds: int) -> dict[str, Any]:
    route = _run_go_json_suite(
        repo,
        [
            "go", "test", "-race", "-json", "-tags=arcadedb_integration", "-count=1",
            "-run", r"^TestAgentMemoryMCPLive_(MixedTierRecall|BackendPath)$", "./cmd/arcadedb-mcp/",
        ],
        MIXED_TIER_MARKER,
        timeout_seconds,
    )
    surface = _run_go_json_suite(
        repo,
        [
            "go", "test", "-json", "-count=1", "-run",
            r"^TestMemorySurfacePolicy_AliasKeepsIsolationAndHiddenSurface$", "./internal/agent/mcptools/",
        ],
        SURFACE_MARKER,
        timeout_seconds,
    )
    mounted_surface = _observe_mounted_memory_surface(repo, timeout_seconds)
    cases: list[Any] = []
    for payload in route["markers"]:
        if isinstance(payload, dict) and payload.get("scenario") == "mixed_tier_recall" and isinstance(payload.get("cases"), list):
            cases.extend(payload["cases"])
    surface_payload = surface["markers"][0] if len(surface["markers"]) == 1 else {}
    if mounted_surface["passed"]:
        surface_payload = dict(surface_payload)
        surface_payload.update(mounted_surface["surface"])
    report = evaluate_mixed_tier_recall({
        "scenario": "mixed_tier_recall",
        "scenarios": cases,
        "surface": surface_payload,
    })
    suite_failures = []
    if not route["passed"] or len(route["markers"]) != 4:
        suite_failures.append("live route suite failed, skipped, or did not emit exactly four evidence markers")
    if not surface["passed"] or len(surface["markers"]) != 1:
        suite_failures.append("unified-surface suite failed, skipped, or did not emit exactly one evidence marker")
    if not mounted_surface["passed"]:
        suite_failures.extend(mounted_surface["failures"])
    report["failures"].extend(route["marker_errors"] + surface["marker_errors"] + suite_failures)
    report["passed"] = not report["failures"]
    report["status"] = "PASS" if report["passed"] else "FAIL"
    report.update({
        "schema_version": 1,
        "schema_id": SCHEMA_ID,
        "tier": "mixed_tier_recall",
        "verdict": "MIXED_TIER_RECALL_PASS" if report["passed"] else "MIXED_TIER_RECALL_FAIL",
        "scenarios": [{"id": "mixed_tier_recall", "status": report["status"], "detail": report["route_evidence"]}],
        "suites": [route["summary"], surface["summary"], mounted_surface["summary"]],
    })
    return report


def _observe_mounted_memory_surface(repo: pathlib.Path, timeout_seconds: int) -> dict[str, Any]:
    started = time.monotonic()
    failures: list[str] = []
    try:
        head = subprocess.run(
            ["git", "rev-parse", "HEAD"], cwd=repo, text=True, stdout=subprocess.PIPE,
            stderr=subprocess.PIPE, check=False, timeout=timeout_seconds,
        )
        version = subprocess.run(
            ["docker", "exec", "aura", "aura", "version"], cwd=repo, text=True,
            stdout=subprocess.PIPE, stderr=subprocess.PIPE, check=False, timeout=timeout_seconds,
        )
        tools = subprocess.run(
            ["docker", "exec", "aura", "aura", "mcp", "tools", "memory", "--json"],
            cwd=repo, text=True, stdout=subprocess.PIPE, stderr=subprocess.PIPE,
            check=False, timeout=timeout_seconds,
        )
    except (OSError, subprocess.SubprocessError) as exc:
        return {
            "passed": False,
            "surface": {},
            "failures": [f"mounted memory schema observation did not execute: {exc}"],
            "summary": {"executed": False, "error": str(exc)},
        }

    expected_commit = head.stdout.strip() if head.returncode == 0 else ""
    observed_commit = ""
    for line in version.stdout.splitlines():
        if line.startswith("commit:"):
            observed_commit = line.partition(":")[2].strip()
            break
    if not expected_commit or observed_commit != expected_commit:
        failures.append(
            f"running Aura commit is {observed_commit!r}, want current HEAD {expected_commit!r}"
        )

    advertised: Any = None
    if tools.returncode == 0:
        try:
            advertised = json.loads(tools.stdout)
        except json.JSONDecodeError as exc:
            failures.append(f"mounted tools JSON is invalid: {exc}")
    else:
        failures.append(f"mounted tools command exited {tools.returncode}: {tools.stderr.strip()}")
    recall = None
    if isinstance(advertised, list):
        recall = next(
            (tool for tool in advertised if isinstance(tool, dict) and tool.get("name") == "memory_recall"),
            None,
        )
    if not isinstance(recall, dict):
        failures.append("mounted memory server did not advertise memory_recall")
        schema: Any = {}
    else:
        schema = recall.get("inputSchema")
    properties = schema.get("properties") if isinstance(schema, dict) else None
    if not isinstance(properties, dict):
        failures.append("mounted memory_recall did not advertise an object input schema")
        properties = {}
    mode = properties.get("mode")
    modes = mode.get("enum") if isinstance(mode, dict) else None
    surface = {
        "memory_recall_input_properties": list(properties),
        "memory_recall_mode_enum": modes if isinstance(modes, list) else [],
        "observed_aura_commit": observed_commit,
    }
    return {
        "passed": not failures,
        "surface": surface,
        "failures": failures,
        "summary": {
            "command": ["docker", "exec", "aura", "aura", "mcp", "tools", "memory", "--json"],
            "return_code": tools.returncode,
            "elapsed_ms": (time.monotonic() - started) * 1000,
            "observed_aura_commit": observed_commit,
            "expected_commit": expected_commit,
        },
    }


def _run_go_json_suite(
    repo: pathlib.Path,
    command: list[str],
    marker: str,
    timeout_seconds: int,
) -> dict[str, Any]:
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
        markers, marker_errors, skipped = _parse_go_json_markers(completed.stdout, marker)
        summary = {
            "command": command,
            "return_code": completed.returncode,
            "elapsed_ms": (time.monotonic() - started) * 1000,
            "marker_count": len(markers),
            "skipped_tests": skipped,
            "stderr_line_count": sum(1 for line in completed.stderr.splitlines() if line.strip()),
        }
        return {
            "passed": completed.returncode == 0 and not skipped and not marker_errors,
            "markers": markers,
            "marker_errors": marker_errors,
            "summary": summary,
        }
    except (OSError, subprocess.SubprocessError) as exc:
        return {
            "passed": False,
            "markers": [],
            "marker_errors": [str(exc)],
            "summary": {"command": command, "executed": False, "error": str(exc)},
        }


def _parse_go_json_markers(output: str, marker: str) -> tuple[list[Any], list[str], list[str]]:
    markers: list[Any] = []
    errors: list[str] = []
    skipped: list[str] = []
    for line_number, raw in enumerate(output.splitlines(), 1):
        try:
            event = json.loads(raw)
        except json.JSONDecodeError:
            errors.append(f"line {line_number} is not go test JSON")
            continue
        if event.get("Action") == "skip" and isinstance(event.get("Test"), str):
            skipped.append(event["Test"])
        emitted = event.get("Output")
        if event.get("Action") != "output" or not isinstance(emitted, str) or marker not in emitted:
            continue
        try:
            markers.append(json.loads(emitted.partition(marker)[2].strip()))
        except json.JSONDecodeError as exc:
            errors.append(f"line {line_number} has invalid {marker} JSON: {exc}")
    return markers, errors, skipped


def format_mixed_tier_recall(report: dict[str, Any]) -> str:
    counts = report.get("tier_counts", {})
    backends = report.get("backend_paths", {})
    return (
        f"mixed_tier_recall: {report['status']}; effective_path={report.get('effective_path')}; "
        f"facts={counts.get('facts', 0)} conversations={counts.get('conversations', 0)} "
        f"reasoning={counts.get('reasoning', 0)}; "
        f"query={backends.get('query')} entity={backends.get('entity')} "
        f"forced_fallback={backends.get('forced_fallback')}"
    )
