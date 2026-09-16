#!/usr/bin/env python3
from __future__ import annotations

import argparse
import datetime as dt
import json
import pathlib
import re
import shutil
import subprocess
import sys
import time
from typing import Any

from evidence_metadata import candidate_commit


GO_SCOPES = {
    "gateway": "internal/gateway/classify.go",
    "identity_isolation": "internal/identityctx/operator.go",
    "profile_validation": "internal/config/config_runtimeprofile.go",
    "sandbox": "internal/sandbox/usersandbox/spec.go",
    "media_clamp": "internal/mediagen/clamp.go",
    "media_watcher": "internal/mediagen/watcher_state.go",
}
# The generation cockpit is scored on its own denominator as well as inside the
# aggregate: eight media files among thirty-odd others cannot carry survivors that a
# strong unrelated suite would average away.
MEDIA_FRONTEND_FILES = (
    "src/chat/artifacts/renderers/GeneratedImagePreview.tsx",
    "src/chat/artifacts/renderers/VideoPreview.tsx",
    "src/chat/artifacts/renderers/previewDispatch.tsx",
    "src/chat/generation/GenerationFrame.tsx",
    "src/chat/generation/GenerationToolDisplay.tsx",
    "src/chat/generation/generationState.ts",
    "src/components/image-generation.tsx",
    "src/components/image.tsx",
)
FRONTEND_SCOPE_IDS = frozenset({"frontend", "media_frontend"})
# The scopes this plan added (2026-09-16). A mutation report written before them is stale
# evidence, not a passing one — release readiness says so by name.
MEDIA_SCOPE_IDS = frozenset({"media_clamp", "media_watcher", "media_frontend"})
# Spelled out rather than derived from GO_SCOPES: a requirement computed from the thing it
# checks cannot catch that thing being deleted. The contract test pins the two lists together.
REQUIRED_SCOPE_IDS = frozenset(
    {
        "gateway",
        "identity_isolation",
        "profile_validation",
        "sandbox",
        "media_clamp",
        "media_watcher",
        "frontend",
        "media_frontend",
    }
)
KILLED_STATUSES = ("Killed", "Timeout")
SURVIVED_STATUSES = ("Survived", "NoCoverage")
SUMMARY = re.compile(
    r"mutation score is ([0-9.]+) "
    r"\((\d+) passed, (\d+) failed, (\d+) duplicated, (\d+) skipped"
    r"(?:, total is \d+)?\)",
    re.IGNORECASE,
)


def parse_go_mutation_output(output: str) -> dict[str, Any]:
    matches = list(SUMMARY.finditer(output))
    if not matches:
        raise ValueError("go-mutesting output has no mutation summary")
    match = matches[-1]
    reported, killed, survived, duplicated, skipped = match.groups()
    killed_count = int(killed)
    survived_count = int(survived)
    scored = killed_count + survived_count
    if scored == 0:
        raise ValueError("go-mutesting summary has no scored mutants")
    calculated = killed_count / scored
    if abs(float(reported) - calculated) > 0.000001:
        raise ValueError(
            f"go-mutesting score {reported} differs from counts {killed_count}/{scored}"
        )
    return {
        "killed": killed_count,
        "survived": survived_count,
        "duplicated": int(duplicated),
        "skipped": int(skipped),
        "score_percent": calculated * 100,
    }


def normalized_report_path(path: str) -> str:
    cleaned = path.replace("\\", "/")
    for prefix in ("./", "web/"):
        if cleaned.startswith(prefix):
            cleaned = cleaned[len(prefix) :]
    return cleaned


def mutant_status_counts(file_report: Any) -> dict[str, int]:
    counts: dict[str, int] = {}
    if not isinstance(file_report, dict):
        return counts
    for mutant in file_report.get("mutants", []):
        if not isinstance(mutant, dict):
            continue
        status = str(mutant.get("status", "Unknown"))
        counts[status] = counts.get(status, 0) + 1
    return counts


def scored_mutants(counts: dict[str, int]) -> tuple[int, int]:
    killed = sum(counts.get(status, 0) for status in KILLED_STATUSES)
    survived = sum(counts.get(status, 0) for status in SURVIVED_STATUSES)
    return killed, survived


def selected_file_counts(
    files: dict[str, Any], only: tuple[str, ...] | None
) -> list[dict[str, int]]:
    if only is None:
        return [mutant_status_counts(body) for body in files.values()]
    indexed = {normalized_report_path(name): body for name, body in files.items()}
    missing = [name for name in only if name not in indexed]
    if missing:
        raise ValueError("Stryker report does not mutate: " + ", ".join(missing))
    selected = [(name, mutant_status_counts(indexed[name])) for name in only]
    # Per file, not just per scope: one `// Stryker disable all`, an empty mutants array or a
    # file that only produced CompileErrors contributes to neither side of the ratio, so its
    # seven neighbours would carry the score and the file would be silently unmeasured.
    unmeasured = [name for name, counts in selected if sum(scored_mutants(counts)) == 0]
    if unmeasured:
        raise ValueError("Stryker report scores no mutant for: " + ", ".join(unmeasured))
    return [counts for _, counts in selected]


def parse_frontend_report(
    path: pathlib.Path,
    max_age_hours: float = 24.0,
    only: tuple[str, ...] | None = None,
) -> dict[str, Any]:
    if max_age_hours <= 0:
        raise ValueError("frontend report max age must be positive")
    try:
        age_seconds = time.time() - path.stat().st_mtime
    except OSError as exc:
        raise ValueError(f"cannot stat Stryker report {path}: {exc}") from exc
    if age_seconds < -300 or age_seconds > max_age_hours * 60 * 60:
        raise ValueError(
            f"Stryker report is stale or future-dated ({age_seconds / 3600:.2f}h)"
        )
    try:
        report = json.loads(path.read_text(encoding="utf-8"))
    except (OSError, json.JSONDecodeError) as exc:
        raise ValueError(f"cannot read Stryker report {path}: {exc}") from exc
    if report.get("schemaVersion") != "1.0":
        raise ValueError("Stryker report schemaVersion must be 1.0")
    files = report.get("files")
    if not isinstance(files, dict):
        raise ValueError("Stryker report files are missing")
    counts: dict[str, int] = {}
    for file_counts in selected_file_counts(files, only):
        for status, count in file_counts.items():
            counts[status] = counts.get(status, 0) + count
    killed, survived = scored_mutants(counts)
    scored = killed + survived
    if scored == 0:
        raise ValueError("Stryker report has no scored mutants")
    excluded = sum(counts.values()) - scored
    return {
        "killed": killed,
        "survived": survived,
        "excluded": excluded,
        "score_percent": killed * 100 / scored,
        "status_counts": counts,
    }


def run_go_scope(
    executable: str,
    repo: pathlib.Path,
    scope_id: str,
    relative_path: str,
    log_dir: pathlib.Path,
) -> dict[str, Any]:
    completed = subprocess.run(
        [executable, relative_path],
        cwd=repo,
        text=True,
        stdout=subprocess.PIPE,
        stderr=subprocess.STDOUT,
        check=False,
        timeout=1800,
    )
    log_dir.mkdir(parents=True, exist_ok=True)
    (log_dir / f"{scope_id}.log").write_text(completed.stdout, encoding="utf-8")
    if completed.returncode != 0:
        raise RuntimeError(f"{scope_id}: go-mutesting exited {completed.returncode}")
    return scope(scope_id, [relative_path], parse_go_mutation_output(completed.stdout))


def scope(scope_id: str, files: list[str], parsed: dict[str, Any]) -> dict[str, Any]:
    # `executed` is READ OFF the counts, never asserted: a scope that scored nothing says so
    # in the field release readiness reads back, instead of claiming it ran.
    return {
        "id": scope_id,
        "executed": parsed["killed"] + parsed["survived"] > 0,
        "files": files,
        **parsed,
    }


def scope_failures(scopes: list[dict[str, Any]], minimum: float) -> list[str]:
    present = {item.get("id") for item in scopes}
    failures = [
        f"missing scope {name}" for name in sorted(REQUIRED_SCOPE_IDS - present)
    ]
    for item in scopes:
        scope_id = item.get("id")
        if item.get("executed") is not True or item.get("killed", 0) + item.get("survived", 0) == 0:
            failures.append(f"{scope_id} executed no mutants")
        elif item.get("score_percent", 0.0) < minimum:
            failures.append(f"{scope_id}={item['score_percent']:.2f}%")
    return failures


def write_report(path: pathlib.Path, report: dict[str, Any]) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(report, indent=2) + "\n", encoding="utf-8")


def run(args: argparse.Namespace) -> dict[str, Any]:
    repo = pathlib.Path(__file__).resolve().parents[1]
    executable = args.go_mutesting or shutil.which("go-mutesting")
    if not executable:
        raise RuntimeError("go-mutesting is required")
    output = args.output.resolve()
    log_dir = args.log_dir.resolve()
    report: dict[str, Any] = {
        "schema_version": 1,
        "generated_at": dt.datetime.now(dt.timezone.utc).isoformat(),
        "candidate_commit": candidate_commit(repo),
        "passed": False,
        "minimum_score_percent": args.minimum,
        "scopes": [],
    }
    try:
        for scope_id, relative_path in GO_SCOPES.items():
            report["scopes"].append(
                run_go_scope(executable, repo, scope_id, relative_path, log_dir)
            )
        frontend_report = args.frontend_report.resolve()
        report["scopes"].append(
            scope(
                "frontend",
                ["web/stryker.config.json"],
                parse_frontend_report(frontend_report, args.frontend_max_age_hours),
            )
        )
        report["scopes"].append(
            scope(
                "media_frontend",
                [f"web/{name}" for name in MEDIA_FRONTEND_FILES],
                parse_frontend_report(
                    frontend_report, args.frontend_max_age_hours, MEDIA_FRONTEND_FILES
                ),
            )
        )
        failures = scope_failures(report["scopes"], args.minimum)
        report["passed"] = not failures
        if failures:
            report["error"] = "mutation gate failed: " + ", ".join(failures)
    except Exception as exc:
        report["error"] = str(exc)
        write_report(output, report)
        raise
    write_report(output, report)
    if not report["passed"]:
        raise RuntimeError(str(report["error"]))
    return report


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Aura critical-boundary mutation gate")
    parser.add_argument("--go-mutesting")
    parser.add_argument(
        "--frontend-report",
        type=pathlib.Path,
        default=pathlib.Path("web/reports/mutation/mutation.json"),
    )
    parser.add_argument(
        "--output",
        type=pathlib.Path,
        default=pathlib.Path("artifacts/production-readiness/mutation-report.json"),
    )
    parser.add_argument(
        "--log-dir",
        type=pathlib.Path,
        default=pathlib.Path("artifacts/production-readiness/mutation-logs"),
    )
    parser.add_argument("--frontend-max-age-hours", type=float, default=24.0)
    parser.add_argument("--minimum", type=float, default=70.0)
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    try:
        report = run(args)
    except (RuntimeError, ValueError, OSError, subprocess.SubprocessError) as exc:
        print(f"critical-mutation-gate: FAIL: {exc}", file=sys.stderr)
        return 1
    scores = ", ".join(
        f"{scope['id']}={scope['score_percent']:.2f}%" for scope in report["scopes"]
    )
    print(f"critical-mutation-gate: PASS: {scores}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
