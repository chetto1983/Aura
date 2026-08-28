#!/usr/bin/env python3
"""Fail when the GSD state ledger disagrees with plans, summaries, or ROADMAP.md."""

from __future__ import annotations

import argparse
import re
import subprocess
import sys
from pathlib import Path


PHASE_ENTRY = re.compile(r"^- \[([ xX])\] \*\*Phase ([0-9.]+): ([^*]+?)\*\*", re.MULTILINE)
PROGRESS_ROW = re.compile(
    r"^\|\s*([0-9.]+)\.\s+([^|]+?)\s*\|\s*([^|]+?)\s*\|\s*([^|]+?)\s*\|",
    re.MULTILINE,
)


def field(text: str, name: str) -> str:
    match = re.search(rf"^\s*{re.escape(name)}:\s*(.+?)\s*$", text, re.MULTILINE)
    if match is None:
        raise ValueError(f"STATE.md is missing {name}")
    return match.group(1).strip('"')


def git(root: Path, *args: str) -> str:
    result = subprocess.run(
        ["git", *args], cwd=root, check=True, text=True, stdout=subprocess.PIPE
    )
    return result.stdout.strip()


def expected_state_heads(root: Path, state_path: Path) -> set[str]:
    relative = state_path.relative_to(root).as_posix()
    if git(root, "status", "--porcelain", "--", relative):
        return {git(root, "rev-parse", "HEAD")}
    state_commit = git(root, "log", "-1", "--format=%H", "--", relative)
    parents = git(root, "show", "-s", "--format=%P", state_commit).split()
    return set(parents or [state_commit])


def planned_and_completed(phases: Path) -> tuple[set[str], set[str]]:
    plans = {path.name.removesuffix("-PLAN.md") for path in phases.rglob("*-PLAN.md")}
    summaries = {
        path.name.removesuffix("-SUMMARY.md") for path in phases.rglob("*-SUMMARY.md")
    }
    return plans, plans & summaries


def audit(root: Path) -> list[str]:
    state_path = root / ".planning" / "STATE.md"
    roadmap_path = root / ".planning" / "ROADMAP.md"
    phases_path = root / ".planning" / "phases"
    state = state_path.read_text(encoding="utf-8")
    roadmap = roadmap_path.read_text(encoding="utf-8")
    plans, completed = planned_and_completed(phases_path)
    errors: list[str] = []

    state_total_plans = int(field(state, "total_plans"))
    state_completed_plans = int(field(state, "completed_plans"))
    if state_total_plans != len(plans):
        errors.append(f"STATE total_plans={state_total_plans}, filesystem={len(plans)}")
    if state_completed_plans != len(completed):
        errors.append(
            f"STATE completed_plans={state_completed_plans}, matching summaries={len(completed)}"
        )

    entries = {
        phase: (mark.strip().lower() == "x", name.strip())
        for mark, phase, name in PHASE_ENTRY.findall(roadmap)
    }
    state_total_phases = int(field(state, "total_phases"))
    state_completed_phases = int(field(state, "completed_phases"))
    roadmap_completed = sum(complete for complete, _ in entries.values())
    if state_total_phases != len(entries):
        errors.append(f"STATE total_phases={state_total_phases}, ROADMAP phases={len(entries)}")
    if state_completed_phases != roadmap_completed:
        errors.append(
            f"STATE completed_phases={state_completed_phases}, ROADMAP checked phases={roadmap_completed}"
        )

    rows = {
        phase: (name.strip(), count.strip(), status.strip())
        for phase, name, count, status in PROGRESS_ROW.findall(roadmap)
    }
    if entries.keys() != rows.keys():
        missing = sorted(entries.keys() - rows.keys())
        extra = sorted(rows.keys() - entries.keys())
        errors.append(f"ROADMAP phase/progress inventory differs: missing={missing}, extra={extra}")
    for phase, (_, count, _) in rows.items():
        phase_plans = {stem for stem in plans if stem.startswith(f"{phase}-")}
        phase_completed = {stem for stem in completed if stem.startswith(f"{phase}-")}
        expected_count = "0/TBD" if not phase_plans else f"{len(phase_completed)}/{len(phase_plans)}"
        if count != expected_count:
            errors.append(f"ROADMAP phase {phase} plans={count}, filesystem={expected_count}")

    current_phase = field(state, "current_phase")
    current_name = field(state, "current_phase_name")
    current_status = field(state, "status")
    entry = entries.get(current_phase)
    row = rows.get(current_phase)
    if entry is None or entry[1] != current_name:
        errors.append(f"current phase {current_phase} / {current_name!r} is absent or renamed in ROADMAP")
    if row is None or row[0] != current_name:
        errors.append(f"current phase {current_phase} / {current_name!r} has no matching progress row")
    elif current_status == "executing" and row[2] != "In Progress":
        errors.append(f"STATE status=executing but ROADMAP phase {current_phase} status={row[2]!r}")

    focus = re.search(r"\*\*Current focus:\*\* Phase ([0-9.]+) — ([^\n]+)", state)
    position = re.search(r"^Phase: ([0-9.]+) \(([^)]+)\) — ([A-Z]+)$", state, re.MULTILINE)
    if focus is None or focus.groups() != (current_phase, current_name):
        errors.append("STATE Current focus disagrees with its front matter")
    if position is None or position.groups()[:2] != (current_phase, current_name):
        errors.append("STATE Current Position disagrees with its front matter")

    state_head = field(state, "state_head")
    try:
        git(root, "cat-file", "-e", f"{state_head}^{{commit}}")
        expected_heads = expected_state_heads(root, state_path)
        if state_head not in expected_heads:
            errors.append(
                f"STATE state_head={state_head}, expected planning input {sorted(expected_heads)}"
            )
    except subprocess.CalledProcessError as exc:
        errors.append(f"STATE state_head is not a resolvable commit: {exc}")

    return errors


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--root", type=Path, default=Path(__file__).resolve().parents[1])
    args = parser.parse_args()
    problems = audit(args.root.resolve())
    if problems:
        for problem in problems:
            print(f"planning consistency: {problem}", file=sys.stderr)
        return 1
    print("planning consistency: ok")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
