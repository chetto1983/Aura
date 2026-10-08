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
MIGRATE_ARGS = ["run", "--rm", "--no-deps", "aura-migrate"]
DEPLOY_ARGS = [
    "up",
    "-d",
    "--no-deps",
    "--force-recreate",
    "aura",
]
# Every service reads its database from ${POSTGRES_DB:-aura}, so a rehearsal database is
# selected per command. Both names are constants: they reach SQL as identifiers.
PREVIOUS_DATABASE = "aura_rollback_previous"
RESTORE_DATABASE = "aura_rollback_restore"
DUMP_PATH = "/tmp/aura-rollback-previous.dump"


def compose_command(compose_file: pathlib.Path, args: list[str]) -> list[str]:
    return ["docker", "compose", "-f", str(compose_file.resolve()), *args]


def migration_head(status_output: str) -> int:
    """The single tracker row `aura db status` prints, refused when dirty or ambiguous."""
    rows = [line.split() for line in status_output.splitlines()]
    versions = [row for row in rows if len(row) == 2 and row[0].isdigit()]
    if len(versions) != 1:
        raise RuntimeError(f"aura db status: want one tracker row, got {status_output[-400:]!r}")
    version, dirty = versions[0]
    if dirty != "false":
        raise RuntimeError(f"aura db status: version {version} is dirty")
    return int(version)


def postgres(compose_file: pathlib.Path, repo: pathlib.Path, args: list[str]) -> None:
    # The bare -e takes PGPASSWORD from this process, so the password never sits in argv.
    environment = os.environ.copy()
    environment.setdefault("PGPASSWORD", os.environ.get("POSTGRES_PASSWORD", ""))
    command = compose_command(compose_file, ["exec", "-T", "-e", "PGPASSWORD", "postgres", *args])
    completed = subprocess.run(
        command, cwd=repo, env=environment, text=True, stdout=subprocess.PIPE,
        stderr=subprocess.STDOUT, check=False, timeout=600,
    )
    if completed.returncode != 0:
        raise RuntimeError(f"postgres {args[0]} failed: {completed.stdout[-2000:]}")


def psql(compose_file: pathlib.Path, repo: pathlib.Path, sql: str) -> None:
    user = os.environ.get("POSTGRES_USER", "aura")
    postgres(compose_file, repo, ["psql", "-v", "ON_ERROR_STOP=1", "-U", user, "-d", "postgres", "-c", sql])


def recreate_database(compose_file: pathlib.Path, repo: pathlib.Path, name: str) -> None:
    # CREATE DATABASE refuses a transaction block, so the two statements are two calls.
    psql(compose_file, repo, f'DROP DATABASE IF EXISTS "{name}" WITH (FORCE)')
    psql(compose_file, repo, f'CREATE DATABASE "{name}" OWNER "{os.environ.get("POSTGRES_USER", "aura")}"')


def drop_rehearsal_state(compose_file: pathlib.Path, repo: pathlib.Path) -> None:
    for step in (
        lambda: psql(compose_file, repo, f'DROP DATABASE IF EXISTS "{PREVIOUS_DATABASE}" WITH (FORCE)'),
        lambda: psql(compose_file, repo, f'DROP DATABASE IF EXISTS "{RESTORE_DATABASE}" WITH (FORCE)'),
        lambda: postgres(compose_file, repo, ["rm", "-f", DUMP_PATH]),
    ):
        try:
            step()
        except (RuntimeError, OSError, subprocess.SubprocessError) as exc:
            print(f"rollback-rehearsal: cleanup: {exc}", file=sys.stderr)


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


def run_for_image(
    image: str, command: list[str], repo: pathlib.Path, database: str = ""
) -> str:
    environment = os.environ.copy()
    environment["AURA_IMAGE"] = image
    if database:
        environment["POSTGRES_DB"] = database
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
    return completed.stdout


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
    swap_command = (
        f"AURA_IMAGE={args.previous_image} "
        "docker compose up -d --no-deps --force-recreate aura"
    )
    report: dict[str, Any] = {
        "schema_version": 1,
        "generated_at": dt.datetime.now(dt.timezone.utc).isoformat(),
        "candidate_commit": candidate_commit(repo),
        "passed": False,
        "previous_image": args.previous_image,
        "previous_image_digest": previous_digest,
        "candidate_image": args.candidate_image,
        "candidate_image_digest": candidate_digest,
        "rollback_mode": None,
        "rollback_command": swap_command,
        "previous_migration_head": None,
        "candidate_migration_head": None,
        "config_started": False,
        "upgrade_healthy": False,
        "migrations_compatible": False,
        "restore_verified": False,
        "readiness_healthy": False,
        "readiness_source": f"docker:{args.container}/.State.Health",
        "candidate_restored": False,
    }
    status = compose_command(args.compose_file, MIGRATION_STATUS_ARGS)
    migrate = compose_command(args.compose_file, MIGRATE_ARGS)
    deploy = compose_command(args.compose_file, DEPLOY_ARGS)

    def serve(image: str, database: str = "") -> None:
        run_for_image(image, deploy, repo, database)
        wait_deployment(args.health_url, args.container, args.timeout_seconds, repo)

    candidate_is_final = False
    try:
        candidate_head = migration_head(run_for_image(args.candidate_image, status, repo))
        report["candidate_migration_head"] = candidate_head

        # The previous release first, on a database of its own making: the state an
        # operator upgrades from.
        recreate_database(args.compose_file, repo, PREVIOUS_DATABASE)
        run_for_image(args.previous_image, migrate, repo, PREVIOUS_DATABASE)
        previous_head = migration_head(
            run_for_image(args.previous_image, status, repo, PREVIOUS_DATABASE)
        )
        report["previous_migration_head"] = previous_head
        if previous_head > candidate_head:
            raise ValueError(
                f"previous image migrates to {previous_head}, past the candidate's {candidate_head}"
            )
        serve(args.previous_image, PREVIOUS_DATABASE)
        report["config_started"] = True
        user = os.environ.get("POSTGRES_USER", "aura")
        postgres(args.compose_file, repo,
                 ["pg_dump", "-U", user, "-Fc", "-d", PREVIOUS_DATABASE, "-f", DUMP_PATH])

        run_for_image(args.candidate_image, migrate, repo, PREVIOUS_DATABASE)
        serve(args.candidate_image, PREVIOUS_DATABASE)
        report["upgrade_healthy"] = True

        # CheckMigrationHead admits only a binary's own head, so a previous image can
        # return on the upgraded database only when the heads match.
        if previous_head == candidate_head:
            report["rollback_mode"] = "swap"
            serve(args.previous_image, PREVIOUS_DATABASE)
            report["migrations_compatible"] = True
        else:
            report["rollback_mode"] = "restore"
            report["rollback_command"] = (
                "restore the pre-upgrade pg_dump into a new database, then POSTGRES_DB=<it> "
                + swap_command
            )
            recreate_database(args.compose_file, repo, RESTORE_DATABASE)
            postgres(args.compose_file, repo,
                     ["pg_restore", "-U", user, "-d", RESTORE_DATABASE, "--exit-on-error", DUMP_PATH])
            serve(args.previous_image, RESTORE_DATABASE)
            report["restore_verified"] = True
        report["readiness_healthy"] = True

        serve(args.candidate_image)
        candidate_is_final = True
        report["candidate_restored"] = True
        report["passed"] = True
    finally:
        try:
            if not candidate_is_final:
                serve(args.candidate_image)
        finally:
            drop_rehearsal_state(args.compose_file, repo)
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
        print(
            f"rollback-rehearsal: PASS ({report['rollback_mode']}): heads "
            f"{report['previous_migration_head']} -> {report['candidate_migration_head']}, "
            "upgrade and rollback healthy, candidate restored"
        )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
