#!/usr/bin/env python3
from __future__ import annotations

import argparse
import datetime as dt
import hashlib
import json
import pathlib
import re
import subprocess
import sys
from collections import Counter
from collections.abc import Iterable
from typing import Any

from evidence_metadata import FULL_GIT_SHA, candidate_commit


class GateError(RuntimeError):
    pass


ISSUE_ID = re.compile(r"(?:EXT|REL|TST|UX)-\d{3}")
ALLOWED_STATES = {"external_blocked", "open"}
REQUIRED_CURRENT_IDS = frozenset(
    {
        "EXT-001",
        "EXT-002",
        "EXT-003",
        "EXT-004",
        "EXT-005",
    }
)
EXPECTED_AUDIT_PATHS = frozenset({"docs/audit/README.md"})
REGISTER_HEADER = [
    "ID",
    "State",
    "Current evidence",
    "Closure condition",
    "Owner",
]


def require(condition: bool, message: str) -> None:
    if not condition:
        raise GateError(message)


def parse_register(path: pathlib.Path) -> list[dict[str, str]]:
    require(path.is_file(), f"current audit register missing: {path}")
    rows = []
    header_seen = False
    for line_number, line in enumerate(path.read_text(encoding="utf-8").splitlines(), 1):
        cells = [cell.strip() for cell in line.strip().strip("|").split("|")]
        if cells == REGISTER_HEADER:
            header_seen = True
            continue
        if not header_seen or not line.lstrip().startswith("|"):
            continue
        if len(cells) == 5 and all(set(cell) <= {"-", ":"} for cell in cells):
            continue
        require(len(cells) == 5, f"current issue row {line_number} has {len(cells)} cells")
        require(
            ISSUE_ID.fullmatch(cells[0]) is not None,
            f"current issue row {line_number} has invalid ID {cells[0]!r}",
        )
        issue_id, state, evidence, closure_condition, owner = cells
        require(state in ALLOWED_STATES, f"{issue_id}: invalid state {state!r}")
        require(bool(evidence), f"{issue_id}: evidence is blank")
        require(bool(closure_condition), f"{issue_id}: closure condition is blank")
        require(bool(owner), f"{issue_id}: owner is blank")
        rows.append(
            {
                "id": issue_id,
                "state": state,
                "evidence": evidence,
                "closure_condition": closure_condition,
                "owner": owner,
                "line": str(line_number),
            }
        )
    require(header_seen, "current issue table header missing")
    return rows


def validate_tracked_audit_paths(paths: Iterable[str]) -> None:
    actual = frozenset(path.strip().replace("\\", "/") for path in paths if path.strip())
    stale = sorted(actual - EXPECTED_AUDIT_PATHS)
    missing = sorted(EXPECTED_AUDIT_PATHS - actual)
    require(not stale, f"historical audit paths remain tracked: {stale}")
    require(not missing, f"current audit paths missing: {missing}")


def tracked_audit_paths(repo: pathlib.Path) -> list[str]:
    result = subprocess.run(
        ["git", "ls-files", "docs/audit"],
        cwd=repo,
        check=False,
        capture_output=True,
        text=True,
    )
    require(result.returncode == 0, f"git ls-files failed: {result.stderr.strip()}")
    return result.stdout.splitlines()


def run_gate(
    *,
    register: pathlib.Path,
    output: pathlib.Path,
    candidate: str,
    required_ids: frozenset[str] = REQUIRED_CURRENT_IDS,
) -> dict[str, Any]:
    require(FULL_GIT_SHA.fullmatch(candidate) is not None, "candidate must be a full Git SHA")
    rows = parse_register(register)
    counts_by_id = Counter(row["id"] for row in rows)
    duplicates = sorted(issue_id for issue_id, count in counts_by_id.items() if count > 1)
    require(not duplicates, f"duplicate current issues: {duplicates}")

    actual_ids = set(counts_by_id)
    missing = sorted(required_ids - actual_ids)
    unknown = sorted(actual_ids - required_ids)
    require(not missing, f"missing current issues: {missing}")
    require(not unknown, f"unknown current issues: {unknown}")

    counts = Counter(row["state"] for row in rows)
    normalized_counts = {state: counts[state] for state in sorted(ALLOWED_STATES)}
    report = {
        "schema_version": 1,
        "generated_at": dt.datetime.now(dt.timezone.utc).isoformat(),
        "candidate_commit": candidate,
        "passed": True,
        "release_ready": not rows,
        "open_total": len(rows),
        "counts": normalized_counts,
        "issues": rows,
        "register": {
            "path": register.as_posix(),
            "sha256": hashlib.sha256(register.read_bytes()).hexdigest(),
        },
    }
    output.parent.mkdir(parents=True, exist_ok=True)
    output.write_text(json.dumps(report, indent=2) + "\n", encoding="utf-8")
    return report


def parse_args() -> argparse.Namespace:
    repo = pathlib.Path(__file__).resolve().parents[1]
    parser = argparse.ArgumentParser(description="Verify Aura's current unresolved audit register")
    parser.add_argument(
        "--register",
        type=pathlib.Path,
        default=repo / "docs/audit/README.md",
    )
    parser.add_argument(
        "--output",
        type=pathlib.Path,
        default=repo / "artifacts/production-readiness/audit-closure-report.json",
    )
    parser.add_argument("--candidate", default=candidate_commit(repo))
    args = parser.parse_args()
    args.repo = repo
    return args


def main() -> int:
    args = parse_args()
    try:
        validate_tracked_audit_paths(tracked_audit_paths(args.repo))
        report = run_gate(
            register=args.register.resolve(),
            output=args.output.resolve(),
            candidate=args.candidate,
        )
    except (GateError, OSError, UnicodeError) as exc:
        print(f"audit-closure-gate: FAIL: {exc}", file=sys.stderr)
        return 1
    print(
        f"audit-closure-gate: PASS disclosure; {report['open_total']} current unresolved, "
        f"{report['counts']['external_blocked']} external, "
        f"release_ready={str(report['release_ready']).lower()}"
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
