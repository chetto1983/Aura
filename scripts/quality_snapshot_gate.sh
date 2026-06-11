#!/usr/bin/env bash
# Enforce docs/aura-quality-snapshot.md freshness for changed quality-owned paths.
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"

SNAPSHOT="${AURA_QUALITY_SNAPSHOT:-docs/aura-quality-snapshot.md}"
HEAD_REF="${AURA_QUALITY_HEAD:-HEAD}"
CHANGED_FILE="$(mktemp)"
trap 'rm -f "$CHANGED_FILE"' EXIT

commit_exists() {
  git cat-file -e "$1^{commit}" 2>/dev/null
}

event_before_sha() {
  if [[ -z "${GITHUB_EVENT_PATH:-}" || ! -f "${GITHUB_EVENT_PATH:-}" ]]; then
    return 0
  fi
  python - "$GITHUB_EVENT_PATH" <<'PY'
import json
import sys

with open(sys.argv[1], "r", encoding="utf-8") as f:
    event = json.load(f)

before = event.get("before") or ""
if before and set(before) != {"0"}:
    print(before)
    raise SystemExit(0)

pr = event.get("pull_request") or {}
base = pr.get("base") or {}
sha = base.get("sha") or ""
if sha:
    print(sha)
PY
}

find_base() {
  if [[ -n "${AURA_QUALITY_BASE_REF:-}" ]] && commit_exists "$AURA_QUALITY_BASE_REF"; then
    printf '%s\n' "$AURA_QUALITY_BASE_REF"
    return 0
  fi

  local event_sha
  event_sha="$(event_before_sha || true)"
  if [[ -n "$event_sha" ]] && commit_exists "$event_sha"; then
    printf '%s\n' "$event_sha"
    return 0
  fi

  if [[ -n "${GITHUB_BASE_REF:-}" ]] && commit_exists "origin/${GITHUB_BASE_REF}"; then
    git merge-base "origin/${GITHUB_BASE_REF}" "$HEAD_REF"
    return 0
  fi

  if [[ -n "${GITHUB_REF_NAME:-}" ]] && commit_exists "origin/${GITHUB_REF_NAME}"; then
    git merge-base "origin/${GITHUB_REF_NAME}" "$HEAD_REF"
    return 0
  fi

  if commit_exists "${HEAD_REF}~1"; then
    printf '%s\n' "${HEAD_REF}~1"
  fi
}

BASE_REF="$(find_base || true)"

if [[ -n "${AURA_QUALITY_CHANGED_FILES:-}" ]]; then
  printf '%s\n' "$AURA_QUALITY_CHANGED_FILES" | sed '/^[[:space:]]*$/d' > "$CHANGED_FILE"
elif [[ -z "${GITHUB_ACTIONS:-}" && -n "$(git status --porcelain)" ]]; then
  {
    git diff --name-only
    git diff --name-only --cached
    git ls-files --others --exclude-standard
  } | sed '/^[[:space:]]*$/d' | sort -u > "$CHANGED_FILE"
elif [[ -n "$BASE_REF" ]] && commit_exists "$BASE_REF"; then
  if ! git diff --name-only "$BASE_REF...$HEAD_REF" > "$CHANGED_FILE" 2>/dev/null; then
    git diff --name-only "$BASE_REF" "$HEAD_REF" > "$CHANGED_FILE"
  fi
else
  git diff-tree --no-commit-id --name-only -r "$HEAD_REF" > "$CHANGED_FILE"
fi

if [[ -n "${AURA_QUALITY_BASE_DATE:-}" ]]; then
  BASE_DATE="$AURA_QUALITY_BASE_DATE"
elif [[ -n "$BASE_REF" ]] && commit_exists "$BASE_REF"; then
  BASE_DATE="$(git log -1 --format=%cs "$BASE_REF")"
else
  BASE_DATE="$(git log -1 --format=%cs "$HEAD_REF")"
fi

python - "$SNAPSHOT" "$CHANGED_FILE" "$BASE_DATE" <<'PY'
from __future__ import annotations

import fnmatch
import re
import sys
from dataclasses import dataclass
from pathlib import Path


@dataclass
class Row:
    metric: str
    last_measured: str
    owner: str
    globs: list[str]
    date: str | None


def normalize_path(value: str) -> str:
    return value.strip().replace("\\", "/")


def parse_globs(cell: str) -> list[str]:
    tokens = re.findall(r"`([^`]+)`", cell)
    if not tokens:
        tokens = [part.strip() for part in cell.split(",")]
    out: list[str] = []
    for token in tokens:
        cleaned = normalize_path(token.strip().strip("`").strip())
        if cleaned:
            out.append(cleaned)
    return out


def split_row(line: str) -> list[str]:
    return [cell.strip() for cell in line.strip().strip("|").split("|")]


def parse_rows(snapshot: Path) -> list[Row]:
    lines = snapshot.read_text(encoding="utf-8").splitlines()
    header_idx = None
    for idx, line in enumerate(lines):
        cells = split_row(line) if line.lstrip().startswith("|") else []
        if cells == ["Metric", "Target", "Last measured", "Last value", "Owner phase", "CI gate path"]:
            header_idx = idx
            break
    if header_idx is None:
        raise ValueError("main quality table header not found")
    if header_idx + 1 >= len(lines) or not re.match(r"^\|\s*-+\s*\|", lines[header_idx + 1]):
        raise ValueError("main quality table separator not found")

    rows: list[Row] = []
    for line_no, line in enumerate(lines[header_idx + 2 :], start=header_idx + 3):
        if not line.strip():
            break
        if not line.lstrip().startswith("|"):
            break
        cells = split_row(line)
        if len(cells) != 6:
            raise ValueError(f"main quality table row {line_no} has {len(cells)} cells, want 6")
        metric, _target, last_measured, _last_value, owner, gate_path = cells
        match = re.search(r"\b\d{4}-\d{2}-\d{2}\b", last_measured)
        rows.append(
            Row(
                metric=metric,
                last_measured=last_measured,
                owner=owner,
                globs=parse_globs(gate_path),
                date=match.group(0) if match else None,
            )
        )
    if not rows:
        raise ValueError("main quality table has no data rows")
    return rows


def row_matches(row: Row, changed: list[str]) -> bool:
    for file_path in changed:
        for glob in row.globs:
            if fnmatch.fnmatchcase(file_path, glob):
                return True
    return False


snapshot = Path(sys.argv[1])
changed_path = Path(sys.argv[2])
base_date = sys.argv[3]
changed = [normalize_path(line) for line in changed_path.read_text(encoding="utf-8").splitlines() if line.strip()]

try:
    rows = parse_rows(snapshot)
except Exception as exc:  # noqa: BLE001 - command-line gate prints parse failures.
    print(f"quality snapshot parse error: {exc}", file=sys.stderr)
    raise SystemExit(2)

if not changed:
    print("ok: quality snapshot gate found no changed files")
    raise SystemExit(0)

matched = [row for row in rows if row_matches(row, changed)]
if not matched:
    print("ok: quality snapshot gate found no matching measured rows")
    raise SystemExit(0)

stale = [row for row in matched if row.date is None or row.date < base_date]
if stale:
    for row in stale:
        print(
            f"quality snapshot row '{row.metric}' stale - owner {row.owner} must re-measure and update before merge (amendment #20)",
            file=sys.stderr,
        )
    raise SystemExit(1)

print(f"ok: quality snapshot gate checked {len(matched)} row(s) against base date {base_date}")
PY
