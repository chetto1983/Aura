#!/usr/bin/env python3
from __future__ import annotations

import argparse
import datetime as dt
import hashlib
import json
import pathlib
import re
import sys
from collections import Counter
from typing import Any

from evidence_metadata import FULL_GIT_SHA, candidate_commit


class GateError(RuntimeError):
    pass


FINDING_ID = re.compile(
    r"\b(?:"
    r"F-\d{3}|QA-[A-D]-\d{2}|R-\d{2}|BUG-\d[A-Za-z]?|VW-\d{2}|EXT-\d{3}|"
    r"(?:ARC|CON|CTX|MCP|MEM|OBS|PERF|PRIV|REL|SEC|TST)-\d{3}"
    r")\b"
)
ALLOWED_STATES = {"closed", "superseded", "retired", "external_blocked", "open"}
DEFAULT_SOURCES = (
    "docs/audit/bug-report.md",
    "docs/audit/quality/slice-A-agent-core.md",
    "docs/audit/quality/slice-B-persistence.md",
    "docs/audit/quality/slice-C-transport-web.md",
    "docs/audit/quality/slice-D-frontend-ops.md",
    "docs/audit/agent-memory-context/08-findings-register.md",
    "docs/audit/retrieval-findings-register-2026-07-30.md",
    "docs/audit/operator-reported-runtime-bugs-2026-07-18.md",
    "docs/audit/consolidated-fix-plan-2026-07-20.md",
    "docs/audit/verify-work-runtime-findings-2026-07-18.md",
    "docs/audit/direct-production-tool-audit-2026-07-30.md",
)


def require(condition: bool, message: str) -> None:
    if not condition:
        raise GateError(message)


def source_findings(sources: list[pathlib.Path]) -> tuple[set[str], list[dict[str, str]]]:
    findings: set[str] = set()
    records = []
    for source in sources:
        require(source.is_file(), f"source register missing: {source}")
        raw = source.read_bytes()
        ids = set(FINDING_ID.findall(raw.decode("utf-8")))
        require(ids, f"source register has no finding IDs: {source}")
        findings.update(ids)
        records.append(
            {
                "path": source.as_posix(),
                "sha256": hashlib.sha256(raw).hexdigest(),
                "finding_count": str(len(ids)),
            }
        )
    return findings, records


def parse_ledger(path: pathlib.Path) -> list[dict[str, str]]:
    require(path.is_file(), f"ledger missing: {path}")
    rows = []
    for line_number, line in enumerate(path.read_text(encoding="utf-8").splitlines(), 1):
        cells = [cell.strip() for cell in line.strip().strip("|").split("|")]
        if len(cells) != 4 or FINDING_ID.fullmatch(cells[0]) is None:
            continue
        finding_id, state, evidence, verification = cells
        require(state in ALLOWED_STATES, f"{finding_id}: invalid state {state!r}")
        require(bool(evidence), f"{finding_id}: evidence is blank")
        require(bool(verification), f"{finding_id}: verification is blank")
        rows.append(
            {
                "id": finding_id,
                "state": state,
                "evidence": evidence,
                "verification": verification,
                "line": str(line_number),
            }
        )
    require(rows, "ledger has no finding rows")
    return rows


def run_gate(
    *,
    sources: list[pathlib.Path],
    ledger: pathlib.Path,
    output: pathlib.Path,
    candidate: str,
    minimum_score: float,
) -> dict[str, Any]:
    require(FULL_GIT_SHA.fullmatch(candidate) is not None, "candidate must be a full Git SHA")
    expected, source_records = source_findings(sources)
    rows = parse_ledger(ledger)
    counts_by_id = Counter(row["id"] for row in rows)
    duplicates = sorted(finding_id for finding_id, count in counts_by_id.items() if count > 1)
    require(not duplicates, f"duplicate ledger findings: {duplicates}")
    actual = set(counts_by_id)
    missing = sorted(expected - actual)
    unknown = sorted(actual - expected)
    require(not missing, f"missing ledger findings: {missing}")
    require(not unknown, f"unknown ledger findings: {unknown}")

    counts = Counter(row["state"] for row in rows)
    open_ids = sorted(row["id"] for row in rows if row["state"] == "open")
    require(not open_ids, f"open findings remain: {open_ids}")
    external = sorted(
        row["id"] for row in rows if row["state"] == "external_blocked"
    )
    scored = len(rows) - len(external)
    require(scored > 0, "no scored findings remain")
    completed = scored - counts["open"]
    score = completed * 10 / scored
    require(
        score > minimum_score,
        f"audit score {score:.3f} does not exceed {minimum_score:.3f}",
    )
    normalized_counts = {state: counts[state] for state in sorted(ALLOWED_STATES)}
    report = {
        "schema_version": 1,
        "generated_at": dt.datetime.now(dt.timezone.utc).isoformat(),
        "candidate_commit": candidate,
        "passed": True,
        "score": score,
        "total_findings": len(rows),
        "scored_findings": scored,
        "counts": normalized_counts,
        "undisclosed_findings": 0,
        "external_blockers": external,
        "ledger": {
            "path": ledger.as_posix(),
            "sha256": hashlib.sha256(ledger.read_bytes()).hexdigest(),
        },
        "sources": source_records,
    }
    output.parent.mkdir(parents=True, exist_ok=True)
    output.write_text(json.dumps(report, indent=2) + "\n", encoding="utf-8")
    return report


def parse_args() -> argparse.Namespace:
    repo = pathlib.Path(__file__).resolve().parents[1]
    parser = argparse.ArgumentParser(description="Verify Aura's definitive audit ledger")
    parser.add_argument(
        "--source",
        action="append",
        type=pathlib.Path,
        help="canonical finding register; repeatable",
    )
    parser.add_argument(
        "--ledger",
        type=pathlib.Path,
        default=repo / "docs/audit/definitive-closure-ledger-2026-07-31.md",
    )
    parser.add_argument(
        "--output",
        type=pathlib.Path,
        default=repo / "artifacts/production-readiness/audit-closure-report.json",
    )
    parser.add_argument("--candidate", default=candidate_commit(repo))
    parser.add_argument("--minimum-score", type=float, default=9.8)
    args = parser.parse_args()
    if args.source is None:
        args.source = [repo / path for path in DEFAULT_SOURCES]
    return args


def main() -> int:
    args = parse_args()
    try:
        report = run_gate(
            sources=[path.resolve() for path in args.source],
            ledger=args.ledger.resolve(),
            output=args.output.resolve(),
            candidate=args.candidate,
            minimum_score=args.minimum_score,
        )
    except (GateError, OSError, UnicodeError) as exc:
        print(f"audit-closure-gate: FAIL: {exc}", file=sys.stderr)
        return 1
    print(
        f"audit-closure-gate: PASS: {report['total_findings']} findings, "
        f"{report['counts']['external_blocked']} external blockers excluded, "
        f"score {report['score']:.1f}/10"
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
