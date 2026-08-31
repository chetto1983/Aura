from __future__ import annotations

from typing import Any


SCHEMA_ID = "aura.phase49-memory-evidence/v1"
SCENARIOS = ("mixed_tier_recall", "reasoning_isolation")
EFFECTIVE_PATHS = {"facts", "conversations", "mixed", "reasoning"}
BACKEND_PATHS = {"graph", "hybrid", "lexical"}
OTEL_ATTRIBUTES = {
    "effective_path": "memory.recall.effective_path",
    "path": "memory.recall.path",
}


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
