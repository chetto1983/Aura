from __future__ import annotations

import http.server
import json
import os
import pathlib
import signal
import socket
import subprocess
import threading
import time
import urllib.error
import urllib.parse
import urllib.request

VEGETA_VERSION = "12.13.0"
TOXIPROXY_VERSION = "2.12.0"
TOXIPROXY_IMAGE = (
    "ghcr.io/shopify/toxiproxy@"
    "sha256:9378ed52a28bc50edc1350f936f518f31fa95f0d15917d6eb40b8e376d1a214e"
)
# One contract for the producer-side check and the release gate. The three outage
# scenarios observe a readiness surface, so they must report zero false-ready answers;
# the kill test has no HTTP surface and asserts no partially promoted artifact instead.
# Before this table the gate demanded false_ready_responses of every scenario while the
# producer never emitted it for two of them, and the gate's own fixture fabricated the
# field: no real chaos report had ever passed the gate (PRD Amendment #195).
CHAOS_SCENARIO_ZERO_FIELDS = {
    "database-outage-recovery": ("false_ready_responses", "duplicate_side_effects"),
    "mcp-timeout-storm": ("false_ready_responses", "duplicate_side_effects"),
    "garage-outage-recovery": ("false_ready_responses", "duplicate_side_effects"),
    "process-kill-write-atomicity": (
        "partial_promoted_artifacts",
        "duplicate_side_effects",
    ),
}
REQUIRED_CHAOS_SCENARIOS = tuple(CHAOS_SCENARIO_ZERO_FIELDS)


def validate_load_report(report: dict) -> None:
    if not report.get("passed"):
        raise ValueError("load report is not passed")
    if int(report.get("supported_concurrency", 0)) < 1:
        raise ValueError("supported concurrency is missing")
    resource = report.get("resource", {})
    if int(resource.get("peak_rss_bytes", 0)) > int(
        resource.get("max_rss_bytes", 0)
    ):
        raise ValueError("Aura RSS exceeded the declared budget")
    for name in ("healthz", "readyz"):
        endpoint = report.get("endpoints", {}).get(name)
        if endpoint is None:
            raise ValueError(f"load endpoint {name} did not execute")
        if int(endpoint.get("requests", 0)) < 1:
            raise ValueError(f"load endpoint {name} issued no requests")
        if float(endpoint.get("success_ratio", 0)) < 0.999:
            raise ValueError(f"load endpoint {name} success ratio is below 99.9%")
        if float(endpoint.get("p95_ms", 1e30)) > float(
            endpoint.get("p95_budget_ms", 0)
        ):
            raise ValueError(f"load endpoint {name} exceeded its p95 budget")


def validate_chaos_report(report: dict) -> None:
    if not report.get("passed"):
        raise ValueError("chaos report is not passed")
    scenarios = {
        item.get("id"): item for item in report.get("scenarios", [])
    }
    missing = set(REQUIRED_CHAOS_SCENARIOS) - set(scenarios)
    if missing:
        raise ValueError(f"chaos scenarios missing: {sorted(missing)}")
    for scenario_id in REQUIRED_CHAOS_SCENARIOS:
        scenario = scenarios[scenario_id]
        if not scenario.get("executed") or not scenario.get("passed"):
            raise ValueError(f"chaos scenario failed: {scenario_id}")
        if not scenario.get("cleanup_ok"):
            raise ValueError(f"chaos scenario cleanup failed: {scenario_id}")
        for field in CHAOS_SCENARIO_ZERO_FIELDS[scenario_id]:
            if scenario.get(field) != 0:
                raise ValueError(
                    f"chaos scenario {scenario_id} must report {field} == 0"
                )


class MCPFixtureHandler(http.server.BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def do_DELETE(self) -> None:
        self.send_response(200)
        self.send_header("Content-Length", "0")
        self.end_headers()

    def do_POST(self) -> None:
        length = int(self.headers.get("Content-Length", "0"))
        request = json.loads(self.rfile.read(length) or b"{}")
        method = request.get("method")
        if method == "notifications/initialized":
            self.send_response(202)
            self.send_header("Content-Length", "0")
            self.end_headers()
            return
        if method == "initialize":
            result = {
                "protocolVersion": "2025-06-18",
                "capabilities": {"tools": {}},
                "serverInfo": {"name": "aura-chaos-fixture", "version": "1"},
            }
        elif method == "tools/list":
            result = {
                "tools": [
                    {
                        "name": "memory_search",
                        "description": "Deterministic readiness fixture.",
                        "inputSchema": {"type": "object"},
                        "annotations": {
                            "readOnlyHint": True,
                            "destructiveHint": False,
                        },
                    }
                ]
            }
        elif method == "tools/call":
            payload = {"facts": []}
            result = {
                "content": [
                    {
                        "type": "text",
                        "text": json.dumps(
                            payload,
                            separators=(",", ":"),
                        ),
                    }
                ],
                "structuredContent": payload,
            }
        elif method == "ping":
            result = {}
        else:
            self.send_error(400, "unexpected MCP method")
            return
        body = json.dumps(
            {"jsonrpc": "2.0", "id": request.get("id"), "result": result}
        ).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Mcp-Session-Id", "aura-chaos-session")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        try:
            self.wfile.write(body)
        except (BrokenPipeError, ConnectionResetError):
            pass

    def log_message(self, _format: str, *_args: object) -> None:
        return


class MCPFixture:
    def __init__(self) -> None:
        self.server: http.server.ThreadingHTTPServer | None = None
        self.thread: threading.Thread | None = None

    def __enter__(self) -> "MCPFixture":
        self.server = http.server.ThreadingHTTPServer(
            ("0.0.0.0", 0), MCPFixtureHandler
        )
        self.server.daemon_threads = True
        self.thread = threading.Thread(
            target=self.server.serve_forever, daemon=True
        )
        self.thread.start()
        return self

    def __exit__(self, *_args: object) -> None:
        if self.server:
            self.server.shutdown()
            self.server.server_close()
        if self.thread:
            self.thread.join(timeout=3)

    @property
    def port(self) -> int:
        if not self.server:
            raise RuntimeError("MCP fixture is not running")
        return int(self.server.server_address[1])

    @property
    def url(self) -> str:
        return f"http://127.0.0.1:{self.port}/mcp/"


def free_port(exclude: set[int] | None = None) -> int:
    excluded = exclude or set()
    for port in range(12000, 20000):
        if port in excluded:
            continue
        with socket.socket() as listener:
            try:
                listener.bind(("127.0.0.1", port))
            except OSError:
                continue
            return port
    raise RuntimeError("no free low TCP port available")


def command(
    args: list[str],
    *,
    env: dict[str, str] | None = None,
    timeout: float = 180,
    check: bool = True,
    stdin=None,
) -> subprocess.CompletedProcess:
    completed = subprocess.run(
        args,
        cwd=pathlib.Path(__file__).resolve().parents[1],
        env=env,
        stdin=stdin,
        capture_output=True,
        timeout=timeout,
        check=False,
    )
    if check and completed.returncode:
        stderr = completed.stderr.decode(errors="replace").strip()
        raise RuntimeError(f"{args[0]} failed ({completed.returncode}): {stderr}")
    return completed


def http_result(
    url: str,
    *,
    method: str = "GET",
    body: dict | None = None,
    timeout: float = 4,
) -> tuple[int, dict, float]:
    data = None if body is None else json.dumps(body).encode()
    request = urllib.request.Request(
        url,
        data=data,
        method=method,
        headers={"Content-Type": "application/json"},
    )
    started = time.monotonic()
    try:
        response = urllib.request.urlopen(request, timeout=timeout)
    except urllib.error.HTTPError as error:
        response = error
    with response:
        raw = response.read()
        status = response.status
    parsed = json.loads(raw) if raw else {}
    return status, parsed, (time.monotonic() - started) * 1000


def wait_http(
    url: str,
    status: int,
    *,
    reason: str | None = None,
    timeout: float = 30,
) -> tuple[dict, int]:
    started = time.monotonic()
    last = ""
    while time.monotonic() - started < timeout:
        try:
            observed, body, _ = http_result(url)
            last = f"{observed} {body}"
            reasons = body.get("reasons", [])
            if observed == status and (reason is None or reason in reasons):
                return body, int((time.monotonic() - started) * 1000)
        except (OSError, ValueError) as error:
            last = str(error)
        time.sleep(0.2)
    raise RuntimeError(f"{url} did not reach {status}/{reason}: {last}")


class Toxiproxy:
    inner_ports = {"postgres": 15432, "garage": 13900, "memory": 18090}

    def __init__(self, fixture_port: int, run_id: str) -> None:
        self.fixture_port = fixture_port
        self.name = f"aura-load-chaos-{run_id}"
        ports: list[int] = []
        while len(ports) < len(self.inner_ports) + 1:
            candidate = free_port(set(ports))
            if candidate not in ports:
                ports.append(candidate)
        self.control_port = ports[0]
        self.host_ports = dict(zip(self.inner_ports, ports[1:]))
        self.base = f"http://127.0.0.1:{self.control_port}"

    def __enter__(self) -> "Toxiproxy":
        network = self._compose_network()
        args = [
            "docker",
            "run",
            "-d",
            "--name",
            self.name,
            "--network",
            network,
            "--add-host",
            "host.docker.internal:host-gateway",
            "-p",
            f"127.0.0.1:{self.control_port}:8474",
        ]
        for name, inner in self.inner_ports.items():
            args.extend(
                ["-p", f"127.0.0.1:{self.host_ports[name]}:{inner}"]
            )
        args.append(TOXIPROXY_IMAGE)
        try:
            command(args, timeout=120)
        except Exception:
            command(
                ["docker", "rm", "-f", self.name],
                timeout=30,
                check=False,
            )
            raise
        self._wait_version()
        self.create("postgres", "aura-postgres:5432")
        self.create("garage", "aura-garage:3900")
        self.create(
            "memory", f"host.docker.internal:{self.fixture_port}"
        )
        return self

    def __exit__(self, *_args: object) -> None:
        command(
            ["docker", "rm", "-f", self.name],
            timeout=30,
            check=False,
        )

    @staticmethod
    def _compose_network() -> str:
        inspected = command(
            [
                "docker",
                "inspect",
                "-f",
                "{{json .NetworkSettings.Networks}}",
                os.getenv("PG_CONTAINER", "aura-postgres"),
            ]
        )
        networks = json.loads(inspected.stdout)
        if not networks:
            raise RuntimeError("Postgres container has no Docker network")
        return next(
            (name for name in networks if name.endswith("_default")),
            next(iter(networks)),
        )

    def _wait_version(self) -> None:
        deadline = time.monotonic() + 30
        while time.monotonic() < deadline:
            try:
                with urllib.request.urlopen(
                    self.base + "/version", timeout=2
                ) as response:
                    version = response.read().decode().strip()
                if TOXIPROXY_VERSION not in version:
                    raise RuntimeError(f"unexpected Toxiproxy version {version}")
                return
            except OSError:
                time.sleep(0.2)
        raise RuntimeError("Toxiproxy control API did not start")

    def api(
        self, path: str, *, method: str = "GET", body: dict | None = None
    ) -> dict:
        status, response, _ = http_result(
            self.base + path, method=method, body=body
        )
        if status < 200 or status >= 300:
            raise RuntimeError(f"Toxiproxy {method} {path} returned {status}")
        return response

    def create(self, name: str, upstream: str) -> None:
        self.api(
            "/proxies",
            method="POST",
            body={
                "name": name,
                "listen": f"0.0.0.0:{self.inner_ports[name]}",
                "upstream": upstream,
                "enabled": True,
            },
        )

    def enable(self, name: str, enabled: bool) -> None:
        proxy = self.api(f"/proxies/{name}")
        proxy["enabled"] = enabled
        self.api(f"/proxies/{name}", method="POST", body=proxy)

    def add_timeout(self, name: str, timeout_ms: int) -> None:
        self.api(
            f"/proxies/{name}/toxics",
            method="POST",
            body={
                "name": "aura_timeout",
                "type": "timeout",
                "stream": "downstream",
                "toxicity": 1.0,
                "attributes": {"timeout": timeout_ms},
            },
        )

    def remove_timeout(self, name: str) -> None:
        self.api(
            f"/proxies/{name}/toxics/aura_timeout",
            method="DELETE",
        )


class ResourceSampler:
    def __init__(self, pid: int) -> None:
        self.pid = pid
        self.peak_rss = 0
        self.stop_event = threading.Event()
        self.thread = threading.Thread(target=self._sample, daemon=True)

    def __enter__(self) -> "ResourceSampler":
        self.thread.start()
        return self

    def __exit__(self, *_args: object) -> None:
        self.stop_event.set()
        self.thread.join(timeout=2)

    def _sample(self) -> None:
        status = pathlib.Path(f"/proc/{self.pid}/status")
        while not self.stop_event.wait(0.1):
            try:
                for line in status.read_text().splitlines():
                    if line.startswith("VmRSS:"):
                        self.peak_rss = max(
                            self.peak_rss, int(line.split()[1]) * 1024
                        )
                        break
            except OSError:
                return


def vegeta_attack(
    vegeta: str,
    endpoint: str,
    result_path: pathlib.Path,
    *,
    port: int,
    rate: int,
    duration: int,
    workers: int,
    p95_budget_ms: float,
) -> dict:
    with result_path.open("wb") as output:
        completed = subprocess.run(
            [
                vegeta,
                "attack",
                f"-name={endpoint}",
                f"-rate={rate}/s",
                f"-duration={duration}s",
                f"-workers={workers}",
                f"-max-workers={workers}",
                "-timeout=3s",
            ],
            input=f"GET http://127.0.0.1:{port}/{endpoint}\n".encode(),
            stdout=output,
            stderr=subprocess.PIPE,
            check=False,
        )
    if completed.returncode:
        raise RuntimeError(
            f"vegeta attack failed: {completed.stderr.decode(errors='replace')}"
        )
    with result_path.open("rb") as attack:
        raw = command(
            [vegeta, "report", "-type=json"],
            stdin=attack,
        ).stdout
    report = json.loads(raw)
    latencies = report["latencies"]
    return {
        "requests": report["requests"],
        "target_rate_rps": report["rate"],
        "throughput_rps": report["throughput"],
        "success_ratio": report["success"],
        "p50_ms": latencies["50th"] / 1_000_000,
        "p95_ms": latencies["95th"] / 1_000_000,
        "p99_ms": latencies["99th"] / 1_000_000,
        "max_ms": latencies["max"] / 1_000_000,
        "p95_budget_ms": p95_budget_ms,
        "status_codes": report["status_codes"],
        "errors": report["errors"],
    }


def objectstore_probe(
    binary: pathlib.Path,
    mode: str,
    key: str,
    env: dict[str, str],
    *,
    check: bool = True,
) -> subprocess.CompletedProcess:
    return command(
        [str(binary), f"--mode={mode}", f"--key={key}"],
        env=env,
        timeout=8,
        check=check,
    )


def write_report(path: pathlib.Path, report: dict) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    path.write_text(json.dumps(report, indent=2) + "\n", encoding="utf-8")


def required_env(name: str) -> str:
    value = os.getenv(name, "").strip()
    if not value:
        raise RuntimeError(f"{name} is required")
    return value


def fresh_database(name: str, create: bool) -> None:
    password = required_env("POSTGRES_PASSWORD")
    user = os.getenv("POSTGRES_USER", "aura")
    container = os.getenv("PG_CONTAINER", "aura-postgres")
    if create:
        statements = [f"CREATE DATABASE {name} OWNER {user};"]
    else:
        statements = [
            "SELECT pg_terminate_backend(pid) FROM pg_stat_activity "
            f"WHERE datname='{name}' AND pid <> pg_backend_pid();",
            f"DROP DATABASE IF EXISTS {name};",
        ]
    base = [
            "docker",
            "exec",
            "-e",
            f"PGPASSWORD={password}",
            container,
            "psql",
            "-v",
            "ON_ERROR_STOP=1",
            "-U",
            user,
            "-d",
            "postgres",
            "-c",
        ]
    for statement in statements:
        command(base + [statement])


def daemon_env(
    work: pathlib.Path,
    database: str,
    proxy: Toxiproxy,
    aura_port: int,
) -> dict[str, str]:
    env = dict(os.environ)
    password = urllib.parse.quote(required_env("POSTGRES_PASSWORD"), safe="")
    db_port = proxy.host_ports["postgres"]
    base = f"postgres://{{role}}:{password}@127.0.0.1:{db_port}/{database}?sslmode=disable"
    env.update(
        {
            "POSTGRES_DB": database,
            "AURA_DB_URL": base.format(role="aura_app"),
            "AURA_DB_MIGRATE_URL": base.format(role="aura_migrate"),
            "AURA_DB_BOOTSTRAP_URL": base.format(
                role=os.getenv("POSTGRES_USER", "aura")
            ),
            "AURA_AGUI_BIND": f"127.0.0.1:{aura_port}",
            "AURA_SETUP_BIND": f"127.0.0.1:{free_port()}",
            "AURA_ARCADEDB_MCP_PORT": str(proxy.host_ports["memory"]),
            "AURA_MCP_CONFIG": str(work / "managed-mcp.json"),
            "AURA_MCP_SERVERS_JSON": "",
            "AURA_IN_CONTAINER": "",
            "AURA_OBJECTSTORE_BACKEND": "fake",
            "AURA_RUN_DIR": str(work / "runs"),
            "AURA_SKILLS_DIR": str(work / "skills"),
            "AURA_RUN_DIR_SWEEP_INTERVAL_SEC": "0",
            "AURA_SCHEDULER_TICK_SECONDS": "60",
            "AURA_SCHEDULER_APPROVAL_REMINDER_SEC": "0",
            "AURA_WEB_AUTH_PROVIDER": "authula",
            "AURA_WEB_AUTH_SECRET": "",
            "AURA_WEB_TRUST_PROXY": "",
            "AURA_AUTHULA_SECRET": "2" * 64,
            "AURA_AUTHULA_DATABASE_URL": "",
            "AURA_AUTHULA_OPERATOR_IDENTITY": "",
            "OPENROUTER_API_KEY": "ci-load-chaos-no-network",
        }
    )
    return env


def stop_daemon(process: subprocess.Popen) -> None:
    if process.poll() is not None:
        return
    os.killpg(process.pid, signal.SIGTERM)
    try:
        process.wait(timeout=20)
    except subprocess.TimeoutExpired:
        os.killpg(process.pid, signal.SIGKILL)
        process.wait(timeout=5)
