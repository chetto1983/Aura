#!/usr/bin/env python3
"""Runs go-mutesting on one file, or reuses the result measured on an identical input closure.

go-mutesting has no cache of its own. A scope's result is a function of the mutated file, the
test binary of its package and the toolchain, so it is reused when the sha256 over those inputs
matches an earlier measurement. Anything unreadable in the cache means "run", never an error.
"""
from __future__ import annotations

import argparse
import hashlib
import json
import os
import pathlib
import re
import shutil
import subprocess
import sys
import time
from collections.abc import Callable, Iterable
from typing import Any

from evidence_metadata import FULL_GIT_SHA, candidate_commit


SCHEMA_VERSION = 1
SUMMARY = re.compile(
    r"mutation score is ([0-9.]+) "
    r"\((\d+) passed, (\d+) failed, (\d+) duplicated, (\d+) skipped"
    r"(?:, total is \d+)?\)",
    re.IGNORECASE,
)
# Every file list `go help list` names as a build or embed input. Test lists are included for
# every package of the closure, not only the scope's own: measured over 263 pushes, narrowing
# them moved no scope's reuse rate by more than two points.
INPUT_FIELDS = (
    "GoFiles",
    "CgoFiles",
    "CFiles",
    "CXXFiles",
    "MFiles",
    "HFiles",
    "FFiles",
    "SFiles",
    "SwigFiles",
    "SwigCXXFiles",
    "SysoFiles",
    "EmbedFiles",
    "TestGoFiles",
    "XTestGoFiles",
    "TestEmbedFiles",
    "XTestEmbedFiles",
)
# go-mutesting's built-in exec runs a bare `go test`, so GOFLAGS is how build tags reach it.
TOOLCHAIN_ENV = ("GOFLAGS", "GOVERSION", "GOOS", "GOARCH", "GOEXPERIMENT", "CGO_ENABLED")
MODULE_FILES = ("go.mod", "go.sum")
GO_MUTESTING_TIMEOUT_SECONDS = 1800
HARD = "hard gate"
ADVISORY = "advisory"

Mutate = Callable[[str], tuple[int, str]]
MeasureScope = Callable[[str, str], dict[str, Any]]


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


def decode_packages(stream: str) -> list[dict[str, Any]]:
    # `go list -json` prints one object after another, not an array.
    decoder = json.JSONDecoder()
    packages: list[dict[str, Any]] = []
    index = 0
    while True:
        while index < len(stream) and stream[index].isspace():
            index += 1
        if index == len(stream):
            return packages
        package, index = decoder.raw_decode(stream, index)
        packages.append(package)


def closure_files(repo: pathlib.Path, packages: list[dict[str, Any]]) -> list[pathlib.Path]:
    files = {repo / name for name in MODULE_FILES}
    for package in packages:
        if not (package.get("Module") or {}).get("Main"):
            continue
        # The synthesized test main is generated into GOCACHE from test files hashed here.
        if package.get("ImportPath", "").endswith(".test"):
            continue
        directory = pathlib.Path(package["Dir"]).resolve()
        for field in INPUT_FIELDS:
            files.update(directory / name for name in package.get(field) or ())
        for root, _, names in os.walk(directory / "testdata"):
            files.update(pathlib.Path(root) / name for name in names)
    return sorted(files)


def fingerprint(
    repo: pathlib.Path,
    relative_path: str,
    toolchain: dict[str, str],
    packages: list[dict[str, Any]],
    read: Callable[[pathlib.Path], bytes] = pathlib.Path.read_bytes,
) -> str:
    inputs = [
        (path.relative_to(repo).as_posix(), hashlib.sha256(read(path)).hexdigest())
        for path in closure_files(repo, packages)
    ]
    identity = {
        "schema_version": SCHEMA_VERSION,
        "file": relative_path,
        "toolchain": toolchain,
        "inputs": sorted(inputs),
    }
    return hashlib.sha256(json.dumps(identity, sort_keys=True).encode("utf-8")).hexdigest()


def load_entry(path: pathlib.Path, relative_path: str, value: str) -> dict[str, Any] | None:
    try:
        entry = json.loads(path.read_text(encoding="utf-8"))
        usable = (
            isinstance(entry, dict)
            and entry.get("schema_version") == SCHEMA_VERSION
            and entry.get("file") == relative_path
            and entry.get("fingerprint") == value
            and FULL_GIT_SHA.fullmatch(str(entry.get("commit"))) is not None
            and isinstance(entry.get("duration_seconds"), (int, float))
        )
        if not usable:
            return None
        entry["parsed"] = parse_go_mutation_output(entry["log"])
        return entry
    except (OSError, ValueError, TypeError, KeyError):
        return None


def measure(
    scope_id: str,
    relative_path: str,
    value: str,
    mutate: Mutate | None,
    cache_dir: pathlib.Path,
    log_dir: pathlib.Path,
    commit: str,
) -> dict[str, Any]:
    started = time.monotonic()
    entry_path = cache_dir / f"{scope_id}.json"
    log_dir.mkdir(parents=True, exist_ok=True)
    log_path = log_dir / f"{scope_id}.log"
    entry = load_entry(entry_path, relative_path, value)
    if entry is not None and entry["commit"] == commit:
        # Measured on this very commit, e.g. by a sibling job of the same CI run: that is
        # the candidate's own measurement, so it carries that job's duration and no reuse.
        log_path.write_text(entry["log"], encoding="utf-8")
        return {
            **entry["parsed"],
            "fingerprint": value,
            "duration_seconds": entry["duration_seconds"],
        }
    if entry is not None:
        log_path.write_text(
            f"# reused: measured on {entry['commit']}, fingerprint {value}\n{entry['log']}",
            encoding="utf-8",
        )
        return {
            **entry["parsed"],
            "fingerprint": value,
            "reused_from": entry["commit"],
            "duration_seconds": round(time.monotonic() - started, 3),
        }
    if mutate is None:
        state = "stale" if entry_path.exists() else "missing"
        raise RuntimeError(f"{scope_id}: no measurement of its current inputs ({state} {entry_path})")
    returncode, output = mutate(relative_path)
    log_path.write_text(output, encoding="utf-8")
    if returncode != 0:
        raise RuntimeError(f"{scope_id}: go-mutesting exited {returncode}")
    parsed = parse_go_mutation_output(output)
    duration = round(time.monotonic() - started, 3)
    cache_dir.mkdir(parents=True, exist_ok=True)
    entry_path.write_text(
        json.dumps(
            {
                "schema_version": SCHEMA_VERSION,
                "scope": scope_id,
                "file": relative_path,
                "fingerprint": value,
                "commit": commit,
                "duration_seconds": duration,
                "log": output,
            },
            indent=2,
        )
        + "\n",
        encoding="utf-8",
    )
    return {**parsed, "fingerprint": value, "duration_seconds": duration}


def go_output(repo: pathlib.Path, *args: str) -> str:
    completed = subprocess.run(
        ["go", *args], cwd=repo, text=True, capture_output=True, check=False
    )
    if completed.returncode != 0:
        raise RuntimeError(f"go {' '.join(args)} failed: {completed.stderr.strip()}")
    return completed.stdout


def go_toolchain(repo: pathlib.Path, executable: str) -> dict[str, str]:
    toolchain = json.loads(go_output(repo, "env", "-json", *TOOLCHAIN_ENV))
    modules = [
        line.strip()
        for line in go_output(repo, "version", "-m", executable).splitlines()
        if line.strip().startswith("mod\t")
    ]
    if len(modules) != 1:
        raise RuntimeError(f"{executable} carries no single module version")
    return {**toolchain, "runner": modules[0]}


def scope_measurer(
    relative_paths: Iterable[str],
    fingerprint_of: Callable[[str], str],
    mutate: Mutate | None,
    cache_dir: pathlib.Path,
    log_dir: pathlib.Path,
    commit: str,
) -> MeasureScope:
    # All before the first mutant: a run leaves files in the tree (rapid writes a timestamped
    # fail file into the mutated package's testdata/rapid/), and a later scope whose closure
    # holds that package would hash a state no fresh checkout reproduces.
    fingerprints = {path: fingerprint_of(path) for path in relative_paths}

    def measure_scope(scope_id: str, relative_path: str) -> dict[str, Any]:
        return measure(
            scope_id, relative_path, fingerprints[relative_path], mutate, cache_dir, log_dir, commit
        )

    return measure_scope


def measurer(
    executable: str,
    repo: pathlib.Path,
    log_dir: pathlib.Path,
    cache_dir: pathlib.Path,
    relative_paths: Iterable[str],
    require_measured: bool = False,
) -> MeasureScope:
    toolchain = go_toolchain(repo, executable)

    def fingerprint_of(relative_path: str) -> str:
        package = "./" + pathlib.PurePosixPath(relative_path).parent.as_posix()
        packages = decode_packages(go_output(repo, "list", "-deps", "-test", "-json", package))
        return fingerprint(repo, relative_path, toolchain, packages)

    def mutate(relative_path: str) -> tuple[int, str]:
        completed = subprocess.run(
            [executable, relative_path],
            cwd=repo,
            text=True,
            stdout=subprocess.PIPE,
            stderr=subprocess.STDOUT,
            check=False,
            timeout=GO_MUTESTING_TIMEOUT_SECONDS,
        )
        return completed.returncode, completed.stdout

    return scope_measurer(
        relative_paths,
        fingerprint_of,
        None if require_measured else mutate,
        cache_dir,
        log_dir,
        candidate_commit(repo),
    )


def go_mutesting(explicit: str | None) -> str:
    executable = explicit or shutil.which("go-mutesting")
    if not executable:
        raise RuntimeError("go-mutesting is required")
    return executable


def provenance(measured: dict[str, Any]) -> str:
    if "reused_from" in measured:
        return f"reused from {measured['reused_from']}"
    return f"measured in {measured['duration_seconds']:.1f}s"


def shortfall_label(gate: str) -> str:
    return "FAIL" if gate == HARD else "advisory"


def verdict(
    relative_path: str, measured: dict[str, Any], minimum: float, gate: str
) -> tuple[bool, str]:
    score = measured["score_percent"]
    detail = f"for {relative_path} ({gate}; {provenance(measured)})"
    if score >= minimum:
        return True, f"ok: mutation score {score:.2f}% >= {minimum:g}% {detail}"
    return False, f"{shortfall_label(gate)}: mutation score {score:.2f}% < {minimum:g}% {detail}"


def parse_args(argv: list[str] | None) -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="go-mutesting with input-closure reuse")
    parser.add_argument("files", nargs="*", help="files whose score gates the exit status")
    parser.add_argument("--advisory", action="append", default=[], help="scored, never gating")
    parser.add_argument("--go-mutesting")
    parser.add_argument("--cache-dir", type=pathlib.Path, required=True)
    parser.add_argument("--log-dir", type=pathlib.Path, required=True)
    parser.add_argument("--minimum", type=float, default=70.0)
    args = parser.parse_args(argv)
    if not args.files and not args.advisory:
        parser.error("give at least one file, hard or --advisory")
    return args


def main(argv: list[str] | None = None, measure_scope: MeasureScope | None = None) -> int:
    args = parse_args(argv)
    if measure_scope is None:
        measure_scope = measurer(
            go_mutesting(args.go_mutesting),
            pathlib.Path(__file__).resolve().parents[1],
            args.log_dir.resolve(),
            args.cache_dir.resolve(),
            args.files + args.advisory,
        )
    failed = False
    gated = [(name, HARD) for name in args.files] + [(name, ADVISORY) for name in args.advisory]
    for relative_path, gate in gated:
        try:
            measured = measure_scope(relative_path.replace("/", "_"), relative_path)
            passed, line = verdict(relative_path, measured, args.minimum, gate)
        except (RuntimeError, ValueError, OSError, subprocess.SubprocessError) as exc:
            passed, line = False, f"{shortfall_label(gate)}: {relative_path}: {exc}"
        print(line, flush=True)
        failed = failed or (not passed and gate == HARD)
    return 1 if failed else 0


if __name__ == "__main__":
    sys.exit(main())
