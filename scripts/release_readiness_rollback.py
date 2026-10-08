from __future__ import annotations

import re
from typing import Any

from release_readiness_errors import require


DIGEST = re.compile(r"^sha256:[0-9a-f]{64}$")


def _migration_head(value: Any) -> bool:
    return isinstance(value, int) and not isinstance(value, bool) and value > 0


def validate_rollback(report: dict[str, Any]) -> dict[str, Any]:
    require(report.get("passed") is True, "rollback: report did not pass")
    previous = report.get("previous_image_digest")
    candidate = report.get("candidate_image_digest")
    if report.get("bootstrap") is True:
        # First release: there is no approved image to roll back TO, so the rehearsal
        # is undefined rather than skipped. Accept it only in the exact shape
        # rollback_rehearsal.py --bootstrap writes -- no previous image, a real
        # candidate digest, a stated reason -- and hand the marker back so run_gate
        # records it. An exception nobody can read in the artifact is a hole.
        require(previous is None, "rollback: bootstrap must carry no previous digest")
        require(
            isinstance(candidate, str) and DIGEST.fullmatch(candidate),
            "rollback: candidate digest invalid",
        )
        reason = report.get("bootstrap_reason")
        require(isinstance(reason, str) and reason.strip(), "rollback: bootstrap reason missing")
        return {
            "bootstrap": True,
            "bootstrap_reason": reason,
            "candidate_image_digest": candidate,
        }
    require(isinstance(previous, str) and DIGEST.fullmatch(previous), "rollback: previous digest invalid")
    require(
        isinstance(candidate, str) and DIGEST.fullmatch(candidate),
        "rollback: candidate digest invalid",
    )
    require(previous != candidate, "rollback: previous and candidate digests are identical")
    require(bool(report.get("rollback_command")), "rollback: command missing")
    for field in ("config_started", "readiness_healthy"):
        require(report.get(field) is True, f"rollback: {field} is not true")
    digests = {"previous_image_digest": previous, "candidate_image_digest": candidate}
    # A report written before rollback_mode existed could only describe an image swap.
    mode = report.get("rollback_mode", "swap")
    if "rollback_mode" in report:
        require(report.get("upgrade_healthy") is True, "rollback: upgrade_healthy is not true")
    if mode == "restore":
        # CheckMigrationHead admits only a binary's own head, so across a schema change the
        # previous image can return only on its restored dump (prd.md §17, 2026-10-08).
        heads = (report.get("previous_migration_head"), report.get("candidate_migration_head"))
        require(all(_migration_head(head) for head in heads), "rollback: restore needs both migration heads")
        require(heads[0] < heads[1], "rollback: restore needs the candidate past the previous head")
        require(report.get("restore_verified") is True, "rollback: restore_verified is not true")
        return {**digests, "rollback_mode": "restore",
                "previous_migration_head": heads[0], "candidate_migration_head": heads[1]}
    require(mode == "swap", f"rollback: unknown rollback_mode {mode!r}")
    require(report.get("migrations_compatible") is True, "rollback: migrations_compatible is not true")
    return {**digests, "rollback_mode": "swap"}
