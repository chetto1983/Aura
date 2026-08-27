#!/usr/bin/env python3
from __future__ import annotations

import argparse
import datetime as dt
import json
import os
import pathlib
import re
import subprocess
import sys
import time
import urllib.error
import urllib.request
from typing import Any

from evidence_metadata import candidate_commit


DIGEST = re.compile(r"^sha256:[0-9a-f]{64}$")
MIGRATION_STATUS_ARGS = [
    "run",
    "--rm",
    "--no-deps",
    "--entrypoint",
    "sh",
    "aura-migrate",
    "-lc",
    "aura db status",
]
DEPLOY_ARGS = [
    "up",
    "-d",
    "--no-deps",
    "--force-recreate",
    "aura",
]


def compose_command(compose_file: pathlib.Path, args: list[str]) -> list[str]:
    return ["docker", "compose", "-f", str(compose_file.resolve()), *args]


def image_digest(image: str, repo: pathlib.Path) -> str:
    completed = subprocess.run(
        ["docker", "image", "inspect", "--format", "{{.Id}}", image],
        cwd=repo,
        text=True,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        check=False,
        timeout=60,
    )
    if completed.returncode != 0:
        raise RuntimeError(f"cannot inspect image {image}: {completed.stderr.strip()}")
    digest = completed.stdout.strip()
    if DIGEST.fullmatch(digest) is None:
        raise RuntimeError(f"image {image} has invalid digest {digest!r}")
    return digest


def run_for_image(image: str, command: list[str], repo: pathlib.Path) -> None:
    environment = os.environ.copy()
    environment["AURA_IMAGE"] = image
    completed = subprocess.run(
        command,
        cwd=repo,
        env=environment,
        text=True,
        stdout=subprocess.PIPE,
        stderr=subprocess.STDOUT,
        check=False,
        timeout=900,
    )
    if completed.returncode != 0:
        raise RuntimeError(
            f"{image}: {' '.join(command)} failed: {completed.stdout[-4000:]}"
        )


def probe_endpoint(url: str) -> tuple[bool, str]:
    try:
        with urllib.request.urlopen(url, timeout=3) as response:
            if response.status == 200:
                return True, "HTTP 200"
            return False, f"HTTP {response.status}"
    except (OSError, urllib.error.URLError) as exc:
        return False, str(exc)


def wait_endpoint(url: str, deadline: float) -> None:
    last_error = "not attempted"
    while time.monotonic() < deadline:
        healthy, last_error = probe_endpoint(url)
        if healthy:
            return
        time.sleep(1)
    raise RuntimeError(f"endpoint {url} did not become healthy: {last_error}")


def container_health(container: str, repo: pathlib.Path) -> str:
    completed = subprocess.run(
        [
            "docker",
            "inspect",
            "--format",
            "{{if .State.Health}}{{.State.Health.Status}}{{else}}{{.State.Status}}{{end}}",
            container,
        ],
        cwd=repo,
        text=True,
        stdout=subprocess.PIPE,
        stderr=subprocess.PIPE,
        check=False,
        timeout=15,
    )
    if completed.returncode != 0:
        return "missing"
    return completed.stdout.strip()


def container_logs(container: str, repo: pathlib.Path) -> str:
    completed = subprocess.run(
        ["docker", "logs", "--tail", "120", container],
        cwd=repo,
        text=True,
        stdout=subprocess.PIPE,
        stderr=subprocess.STDOUT,
        check=False,
        timeout=15,
    )
    detail = completed.stdout.strip()
    return detail[-4000:] if detail else "no container logs available"


def wait_deployment(
    health_url: str,
    container: str,
    timeout: float,
    repo: pathlib.Path,
) -> None:
    deadline = time.monotonic() + timeout
    terminal_states = {"dead", "exited", "missing", "removing", "unhealthy"}
    last_endpoint = "not attempted"
    last_status = "not attempted"
    while True:
        endpoint_healthy, last_endpoint = probe_endpoint(health_url)
        last_status = container_health(container, repo)
        if endpoint_healthy and last_status == "healthy":
            return
        if last_status in terminal_states:
            raise RuntimeError(
                f"container {container} stopped before readiness "
                f"(state={last_status}, endpoint={last_endpoint}):\n"
                f"{container_logs(container, repo)}"
            )
        if time.monotonic() >= deadline:
            raise RuntimeError(
                f"deployment did not become healthy "
                f"(container={container}, state={last_status}, "
                f"endpoint={last_endpoint}):\n"
                f"{container_logs(container, repo)}"
            )
        time.sleep(1)


def run_bootstrap(args: argparse.Namespace) -> dict[str, Any]:
    """Report for the FIRST release, where no previously-approved image exists.

    A rollback rehearsal needs an image to roll back TO. Before the first release
    there is none, so the rehearsal is not merely skipped, it is undefined. This
    records that explicitly -- bootstrap plus a reason plus the candidate digest --
    so the release gate can accept it as a declared exception instead of reading a
    rehearsal that never ran as one that passed. The workflow refuses --bootstrap
    once any release exists, which is what keeps this a first-release-only path.
    """
    repo = pathlib.Path(__file__).resolve().parents[1]
    if args.previous_image:
        raise ValueError("--bootstrap takes no --previous-image: there is nothing to roll back to")
    return {
        "schema_version": 1,
        "generated_at": dt.datetime.now(dt.timezone.utc).isoformat(),
        "candidate_commit": candidate_commit(repo),
        "passed": True,
        "bootstrap": True,
        "bootstrap_reason": (
            "no previously-approved image exists: this is the first release, so a "
            "candidate-to-previous-to-candidate rehearsal has no previous image"
        ),
        "previous_image": None,
        "previous_image_digest": None,
        "candidate_image": args.candidate_image,
        "candidate_image_digest": image_digest(args.candidate_image, repo),
    }


def run_rehearsal(args: argparse.Namespace) -> dict[str, Any]:
    repo = pathlib.Path(__file__).resolve().parents[1]
    if not args.compose_file.resolve().is_file():
        raise ValueError(f"compose file does not exist: {args.compose_file}")
    if not args.previous_image:
        raise ValueError("--previous-image is required unless --bootstrap is set")
    previous_digest = image_digest(args.previous_image, repo)
    candidate_digest = image_digest(args.candidate_image, repo)
    if previous_digest == candidate_digest:
        raise ValueError("rollback requires distinct previous and candidate images")
    report: dict[str, Any] = {
        "schema_version": 1,
        "generated_at": dt.datetime.now(dt.timezone.utc).isoformat(),
        "candidate_commit": candidate_commit(repo),
        "passed": False,
        "previous_image": args.previous_image,
        "previous_image_digest": previous_digest,
        "candidate_image": args.candidate_image,
        "candidate_image_digest": candidate_digest,
        "rollback_command": (
            f"AURA_IMAGE={args.previous_image} "
            "docker compose up -d --no-deps --force-recreate aura"
        ),
        "config_started": False,
        "migrations_compatible": False,
        "readiness_healthy": False,
        "readiness_source": f"docker:{args.container}/.State.Health",
        "candidate_restored": False,
    }
    migration_status = compose_command(args.compose_file, MIGRATION_STATUS_ARGS)
    deploy = compose_command(args.compose_file, DEPLOY_ARGS)
    candidate_is_final = False
    try:
        run_for_image(args.candidate_image, migration_status, repo)
        run_for_image(args.candidate_image, deploy, repo)
        wait_deployment(
            args.health_url, args.container, args.timeout_seconds, repo
        )

        run_for_image(args.previous_image, migration_status, repo)
        report["migrations_compatible"] = True
        run_for_image(args.previous_image, deploy, repo)
        report["config_started"] = True
        wait_deployment(
            args.health_url, args.container, args.timeout_seconds, repo
        )
        report["readiness_healthy"] = True

        run_for_image(args.candidate_image, migration_status, repo)
        run_for_image(args.candidate_image, deploy, repo)
        wait_deployment(
            args.health_url, args.container, args.timeout_seconds, repo
        )
        candidate_is_final = True
        report["candidate_restored"] = True
        report["passed"] = True
    finally:
        if not candidate_is_final:
            run_for_image(args.candidate_image, deploy, repo)
            wait_deployment(
                args.health_url, args.container, args.timeout_seconds, repo
            )
    return report


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Aura image rollback rehearsal")
    parser.add_argument("--previous-image", default="")
    parser.add_argument(
        "--bootstrap",
        action="store_true",
        help="first release only: declare that no previous image exists to roll back to",
    )
    parser.add_argument("--candidate-image", required=True)
    parser.add_argument(
        "--compose-file", type=pathlib.Path, default=pathlib.Path("compose.yaml")
    )
    parser.add_argument(
        "--health-url", default="http://127.0.0.1:9080/healthz"
    )
    parser.add_argument("--container", default="aura")
    parser.add_argument("--timeout-seconds", type=float, default=300.0)
    parser.add_argument(
        "--output",
        type=pathlib.Path,
        default=pathlib.Path("artifacts/production-readiness/rollback-report.json"),
    )
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    try:
        report = run_bootstrap(args) if args.bootstrap else run_rehearsal(args)
    except (RuntimeError, ValueError, OSError, subprocess.SubprocessError) as exc:
        print(f"rollback-rehearsal: FAIL: {exc}", file=sys.stderr)
        return 1
    args.output.parent.mkdir(parents=True, exist_ok=True)
    args.output.write_text(json.dumps(report, indent=2) + "\n", encoding="utf-8")
    if args.bootstrap:
        print("rollback-rehearsal: BOOTSTRAP: first release, no previous image to roll back to")
    else:
        print("rollback-rehearsal: PASS: previous image healthy and candidate restored")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
