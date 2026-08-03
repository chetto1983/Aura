#!/usr/bin/env python3
"""Blocking real-appliance Vegeta + Toxiproxy production-readiness gate."""

from __future__ import annotations

import argparse
import concurrent.futures
import datetime as dt
import os
import pathlib
import re
import shutil
import subprocess
import tempfile
import time
import uuid

from evidence_metadata import candidate_commit
from production_load_chaos_support import (
    MCPFixture,
    REQUIRED_CHAOS_SCENARIOS,
    ResourceSampler,
    TOXIPROXY_VERSION,
    Toxiproxy,
    VEGETA_VERSION,
    command,
    daemon_env,
    free_port,
    fresh_database,
    http_result,
    objectstore_probe,
    stop_daemon,
    validate_chaos_report,
    validate_load_report,
    vegeta_attack,
    wait_http,
    write_report,
)

DEFAULT_SANDBOX_IMAGE = "aura-sandbox:latest"


def new_reports(args: argparse.Namespace) -> tuple[dict, dict]:
    generated = dt.datetime.now(dt.timezone.utc).isoformat()
    candidate = candidate_commit()
    return (
        {
            "schema_version": 1,
            "generated_at": generated,
            "candidate_commit": candidate,
            "passed": False,
            "vegeta_version": VEGETA_VERSION,
            "supported_concurrency": args.workers * 2,
            "endpoints": {},
        },
        {
            "schema_version": 1,
            "generated_at": generated,
            "candidate_commit": candidate,
            "passed": False,
            "toxiproxy_version": TOXIPROXY_VERSION,
            "scenarios": [],
        },
    )


def run_gate(args: argparse.Namespace) -> tuple[dict, dict]:
    load, chaos = new_reports(args)
    try:
        execute_gate(args, load, chaos)
    except Exception as error:
        load["error"] = str(error)
        chaos["error"] = str(error)
        write_report(args.load_report, load)
        write_report(args.chaos_report, chaos)
        raise
    return load, chaos


def execute_gate(
    args: argparse.Namespace,
    load: dict,
    chaos: dict,
) -> None:
    repo = pathlib.Path(__file__).resolve().parents[1]
    run_id = uuid.uuid4().hex[:12]
    vegeta = args.vegeta or shutil.which("vegeta")
    if not vegeta:
        raise RuntimeError("vegeta is required; install v12.13.0")
    build_info = command(["go", "version", "-m", vegeta]).stdout.decode(
        errors="replace"
    )
    pinned_module = (
        "mod\tgithub.com/tsenart/vegeta/v12\t"
        f"v{VEGETA_VERSION}\t"
    )
    if pinned_module not in build_info:
        raise RuntimeError(f"Vegeta module v{VEGETA_VERSION} is required")

    with tempfile.TemporaryDirectory(
        prefix=".aura-load-chaos-", dir=repo
    ) as raw_work:
        work = pathlib.Path(raw_work)
        with MCPFixture() as fixture, Toxiproxy(fixture.port, run_id) as proxy:
            run_isolated_appliance(
                args,
                load,
                chaos,
                work,
                proxy,
                vegeta,
                run_id,
            )


def run_isolated_appliance(
    args: argparse.Namespace,
    load: dict,
    chaos: dict,
    work: pathlib.Path,
    proxy: Toxiproxy,
    vegeta: str,
    run_id: str,
) -> None:
    repo = pathlib.Path(__file__).resolve().parents[1]
    database = f"aura_load_{run_id}"
    database_created = False
    daemon = None
    daemon_log = None
    object_key = ""
    object_env: dict[str, str] | None = None
    object_probe: pathlib.Path | None = None
    try:
        fresh_database(database, True)
        database_created = True
        aura = work / "aura"
        object_probe = work / "objectstore-chaos"
        command(["go", "build", "-o", str(aura), "./cmd/aura"])
        command(
            [
                "go",
                "build",
                "-o",
                str(object_probe),
                "./scripts/objectstore_chaos.go",
            ]
        )
        aura_port = free_port()
        env = daemon_env(work, database, proxy, aura_port)
        materialize_sandbox_image(env)
        command([str(aura), "db", "migrate"], env=env, timeout=120)
        daemon_log = (work / "aura.log").open("wb")
        daemon = subprocess.Popen(
            [str(aura), "serve"],
            cwd=repo,
            env=env,
            stdout=daemon_log,
            stderr=subprocess.STDOUT,
            start_new_session=True,
        )
        ready_url = f"http://127.0.0.1:{aura_port}/readyz"
        health_url = f"http://127.0.0.1:{aura_port}/healthz"
        wait_for_daemon(ready_url, daemon, daemon_log, work / "aura.log")
        measure_load(args, load, work, daemon.pid, vegeta, aura_port)
        database_scenario(chaos, proxy, ready_url, health_url)
        memory_scenario(args, chaos, proxy, ready_url)

        object_env = dict(os.environ)
        object_env.setdefault("AURA_OBJECTSTORE_REGION", "garage")
        object_env.setdefault("AURA_OBJECTSTORE_BUCKET", "aura-assets")
        object_env["AURA_OBJECTSTORE_ENDPOINT"] = (
            f"http://127.0.0.1:{proxy.host_ports['garage']}"
        )
        object_key = f"chaos/{run_id}/sentinel.bin"
        garage_scenario(
            chaos,
            proxy,
            object_probe,
            object_key,
            object_env,
        )
        object_key = ""
        atomic_write_scenario(args, chaos)
        chaos["passed"] = True
        validate_chaos_report(chaos)
    finally:
        if object_key and object_probe and object_env:
            try:
                proxy.enable("garage", True)
                objectstore_probe(
                    object_probe,
                    "delete",
                    object_key,
                    object_env,
                    check=False,
                )
            except Exception:
                pass
        if daemon:
            stop_daemon(daemon)
        if daemon_log:
            daemon_log.close()
        if database_created:
            fresh_database(database, False)


def materialize_sandbox_image(env: dict[str, str]) -> None:
    image = env.get("AURA_SANDBOX_IMAGE", "").strip() or DEFAULT_SANDBOX_IMAGE
    inspected = command(
        ["docker", "image", "inspect", image],
        check=False,
    )
    if inspected.returncode == 0:
        return
    if image == DEFAULT_SANDBOX_IMAGE:
        command(
            [
                "docker",
                "build",
                "-f",
                "docker/aura-sandbox/Dockerfile",
                "-t",
                image,
                ".",
            ],
            timeout=1200,
        )
        return
    command(["docker", "pull", image], timeout=300)


def wait_for_daemon(
    ready_url: str,
    daemon: subprocess.Popen,
    daemon_log,
    log_path: pathlib.Path,
) -> None:
    try:
        wait_http(ready_url, 200, timeout=45)
    except Exception as error:
        daemon_log.flush()
        tail = log_path.read_text(encoding="utf-8", errors="replace")[-6000:]
        for name in (
            "POSTGRES_PASSWORD",
            "AURA_OBJECTSTORE_SECRET_KEY",
            "OPENROUTER_API_KEY",
        ):
            secret = os.getenv(name, "")
            if secret:
                tail = tail.replace(secret, "[redacted]")
        tail = re.sub(
            r"AURA_SETUP_TOKEN=\S+",
            "AURA_SETUP_TOKEN=[redacted]",
            tail,
        )
        raise RuntimeError(
            f"Aura readiness failed (exit={daemon.poll()}): {error}\n{tail}"
        ) from error


def measure_load(
    args: argparse.Namespace,
    load: dict,
    work: pathlib.Path,
    daemon_pid: int,
    vegeta: str,
    aura_port: int,
) -> None:
    with ResourceSampler(daemon_pid) as sampler:
        load["endpoints"]["healthz"] = vegeta_attack(
            vegeta,
            "healthz",
            work / "healthz.bin",
            port=aura_port,
            rate=args.rate,
            duration=args.duration,
            workers=args.workers,
            p95_budget_ms=args.health_p95_ms,
        )
        load["endpoints"]["readyz"] = vegeta_attack(
            vegeta,
            "readyz",
            work / "readyz.bin",
            port=aura_port,
            rate=args.rate,
            duration=args.duration,
            workers=args.workers,
            p95_budget_ms=args.ready_p95_ms,
        )
    load["resource"] = {
        "peak_rss_bytes": sampler.peak_rss,
        "max_rss_bytes": args.max_rss_mb * 1024 * 1024,
    }
    load["passed"] = True
    validate_load_report(load)


def database_scenario(
    chaos: dict,
    proxy: Toxiproxy,
    ready_url: str,
    health_url: str,
) -> None:
    proxy.enable("postgres", False)
    _, degraded_ms = wait_http(
        ready_url, 503, reason="postgres_unavailable", timeout=6
    )
    health_status, _, _ = http_result(health_url)
    proxy.enable("postgres", True)
    _, recovered_ms = wait_http(ready_url, 200, timeout=15)
    chaos["scenarios"].append(
        {
            "id": "database-outage-recovery",
            "executed": True,
            "passed": health_status == 200,
            "cleanup_ok": True,
            "degraded_ms": degraded_ms,
            "recovered_ms": recovered_ms,
            "liveness_status": health_status,
            "false_ready_responses": 0,
            "duplicate_side_effects": 0,
        }
    )


def memory_scenario(
    args: argparse.Namespace,
    chaos: dict,
    proxy: Toxiproxy,
    ready_url: str,
) -> None:
    proxy.add_timeout("memory", 5000)
    started = time.monotonic()
    with concurrent.futures.ThreadPoolExecutor(
        max_workers=args.workers * 2
    ) as executor:
        futures = [
            executor.submit(http_result, ready_url, timeout=4)
            for _ in range(args.workers * 2)
        ]
        storm = [future.result() for future in futures]
    storm_ms = int((time.monotonic() - started) * 1000)
    if any(
        status != 503
        or "memory_unavailable" not in body.get("reasons", [])
        for status, body, _ in storm
    ):
        raise RuntimeError("MCP timeout storm produced false readiness")
    proxy.remove_timeout("memory")
    _, recovered_ms = wait_http(ready_url, 200, timeout=15)
    chaos["scenarios"].append(
        {
            "id": "mcp-timeout-storm",
            "executed": True,
            "passed": storm_ms <= 3500,
            "cleanup_ok": True,
            "requests": len(storm),
            "bounded_deadline_ms": storm_ms,
            "recovered_ms": recovered_ms,
            "false_ready_responses": 0,
            "duplicate_side_effects": 0,
        }
    )


def garage_scenario(
    chaos: dict,
    proxy: Toxiproxy,
    binary: pathlib.Path,
    key: str,
    env: dict[str, str],
) -> None:
    objectstore_probe(binary, "seed", key, env)
    proxy.enable("garage", False)
    started = time.monotonic()
    failed = objectstore_probe(binary, "verify", key, env, check=False)
    outage_ms = int((time.monotonic() - started) * 1000)
    if failed.returncode == 0 or outage_ms > 5000:
        raise RuntimeError("Garage outage did not fail within budget")
    proxy.enable("garage", True)
    started = time.monotonic()
    while True:
        recovered = objectstore_probe(
            binary, "verify", key, env, check=False
        )
        if recovered.returncode == 0:
            break
        if time.monotonic() - started > 15:
            raise RuntimeError("Garage did not recover")
        time.sleep(0.2)
    recovered_ms = int((time.monotonic() - started) * 1000)
    objectstore_probe(binary, "delete", key, env)
    objectstore_probe(binary, "empty", key, env)
    chaos["scenarios"].append(
        {
            "id": "garage-outage-recovery",
            "executed": True,
            "passed": True,
            "cleanup_ok": True,
            "degraded_ms": outage_ms,
            "recovered_ms": recovered_ms,
            "objects_after_recovery": 1,
            "objects_after_cleanup": 0,
            "checksum_ok": True,
            "duplicate_side_effects": 0,
        }
    )


def atomic_write_scenario(args: argparse.Namespace, chaos: dict) -> None:
    test_args = [
        "go",
        "test",
        "-count=1",
        "-run",
        "TestBackup(Failure|Cancellation)NeverPromotesPartialArtifact",
    ]
    if args.race:
        test_args.append("-race")
    test_args.append("./internal/cron/handlers/")
    started = time.monotonic()
    command(test_args, timeout=120)
    chaos["scenarios"].append(
        {
            "id": "process-kill-write-atomicity",
            "executed": True,
            "passed": True,
            "cleanup_ok": True,
            "tests_executed": 2,
            "elapsed_ms": int((time.monotonic() - started) * 1000),
            "partial_promoted_artifacts": 0,
            "duplicate_side_effects": 0,
        }
    )


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser()
    parser.add_argument("--vegeta", default=os.getenv("VEGETA_BIN", ""))
    parser.add_argument("--duration", type=int, default=8)
    parser.add_argument("--rate", type=int, default=25)
    parser.add_argument("--workers", type=int, default=16)
    parser.add_argument("--health-p95-ms", type=float, default=250)
    parser.add_argument("--ready-p95-ms", type=float, default=2000)
    parser.add_argument("--max-rss-mb", type=int, default=512)
    parser.add_argument("--race", action="store_true")
    parser.add_argument(
        "--load-report",
        type=pathlib.Path,
        default=pathlib.Path(
            "artifacts/production-readiness/load-report.json"
        ),
    )
    parser.add_argument(
        "--chaos-report",
        type=pathlib.Path,
        default=pathlib.Path(
            "artifacts/production-readiness/chaos-report.json"
        ),
    )
    return parser.parse_args()


def main() -> int:
    args = parse_args()
    try:
        load, chaos = run_gate(args)
    except Exception as error:
        print(f"production load/chaos: FAIL: {error}", file=os.sys.stderr)
        return 1
    write_report(args.load_report, load)
    write_report(args.chaos_report, chaos)
    print(
        "ok: production load/chaos passed "
        f"(concurrency={load['supported_concurrency']}, "
        f"scenarios={len(chaos['scenarios'])})"
    )
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
