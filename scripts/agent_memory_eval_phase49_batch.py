from __future__ import annotations

import json
from typing import Any


BATCH_MARKER = "AURA_AGENT_MEMORY_BATCH_JSON="


def evaluate_batch_atomicity(evidence: Any) -> dict[str, Any]:
    failures: list[str] = []
    if not isinstance(evidence, dict) or evidence.get("scenario") != "batch_atomicity":
        evidence = {}
        failures.append("batch_atomicity scenario is absent")
    executed = evidence.get("executed_count")
    if not isinstance(executed, int) or isinstance(executed, bool) or executed <= 0:
        failures.append("zero batch_atomicity scenarios executed")
    hashes = {
        name: evidence.get(name)
        for name in (
            "before_hash",
            "committed_hash",
            "rollback_hash",
            "replay_hash",
            "cross_identity_before_hash",
            "cross_identity_after_hash",
        )
    }
    if any(not isinstance(value, str) or not value for value in hashes.values()):
        failures.append("batch atomicity state hashes are absent")
    else:
        if hashes["before_hash"] == hashes["committed_hash"]:
            failures.append("committed batch did not change canonical state")
        if hashes["rollback_hash"] != hashes["committed_hash"]:
            failures.append("late failure changed canonical state")
        if hashes["replay_hash"] != hashes["committed_hash"]:
            failures.append("same-key replay changed canonical state")
        if hashes["cross_identity_after_hash"] != hashes["cross_identity_before_hash"]:
            failures.append("cross-identity attempt changed foreign state")
    if evidence.get("replayed") is not True:
        failures.append("same-key replay was not observed")
    if evidence.get("logical_effects") != 1:
        failures.append("ambiguous replay produced a duplicate logical effect")
    if evidence.get("first_error_code") != "target_not_found":
        failures.append("late failure did not preserve the first error")
    if evidence.get("idempotency_conflict_observed") is not True:
        failures.append("same-key different-hash conflict was not observed")
    return {
        "scenario": "batch_atomicity",
        "executed_count": executed,
        "passed": not failures,
        "status": "PASS" if not failures else "FAIL",
        "state_hashes": hashes,
        "failures": failures,
    }


def extract_batch_markers(output: str) -> list[Any]:
    markers: list[Any] = []
    for raw in output.splitlines():
        try:
            event = json.loads(raw)
        except json.JSONDecodeError:
            continue
        emitted = event.get("Output") if isinstance(event, dict) else None
        if event.get("Action") != "output" or not isinstance(emitted, str) or BATCH_MARKER not in emitted:
            continue
        try:
            markers.append(json.loads(emitted.partition(BATCH_MARKER)[2].strip()))
        except json.JSONDecodeError:
            markers.append(None)
    return markers


def attach_batch_evidence(report: dict[str, Any], suites: dict[str, Any], tier: str) -> dict[str, Any]:
    markers = suites.get("arcadedb_mcp_live", {}).get("batch_atomicity_markers", [])
    evidence = markers[0] if len(markers) == 1 else {}
    evaluated = evaluate_batch_atomicity(evidence)
    if len(markers) != 1:
        evaluated["failures"].append(f"batch atomicity emitted {len(markers)} markers, want exactly one")
        evaluated["passed"] = False
        evaluated["status"] = "FAIL"
    report["batch_atomicity"] = evaluated
    if tier == "all":
        report["hard_gates"]["batch_atomicity"] = {"status": evaluated["status"]}
        if not evaluated["passed"]:
            report["passed"] = False
            report["verdict"] = "FAIL"
    return report
