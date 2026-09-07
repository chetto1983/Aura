from __future__ import annotations

import json
import math
from typing import Any

import agent_memory_eval_phase49 as phase49
from agent_memory_eval_metadata import RUNTIME_MARKER, parse_runtime_marker

P95_MIN_SAMPLES = 25
LATENCY_MARKER = "AURA_AGENT_MEMORY_LATENCY_JSON="
LATENCY_PATH = "cli_identity_mcp_search"


def failed_suite_details(report: dict[str, Any]) -> list[str]:
    details = []
    for suite in report.get("suites", []):
        if suite.get("passed") is True:
            continue
        failed = [name for name, events in suite.get("tests", {}).items()
                  if any(event.get("status") == "FAIL" for event in events)]
        label = suite.get("id", "unknown")
        if failed:
            details.append(f"{label}: failed tests: {', '.join(failed)}")
        else:
            details.append(
                f"{label}: return_code={suite.get('return_code')}; "
                f"executed={suite.get('executed')}; "
                f"skipped_tests={suite.get('skipped_tests', [])}; "
                f"protocol_errors={suite.get('protocol_errors', [])}"
            )
    return details


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
        "batch_atomicity_markers": phase49.extract_batch_markers(output),
        "skipped_tests": sorted(skipped),
        "package_failed": package_failed,
        "protocol_errors": protocol_errors,
    }

