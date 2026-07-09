from __future__ import annotations

import os
import socket
import subprocess
import time
from dataclasses import dataclass
from pathlib import Path
from typing import Any

from turingdb import TuringDB


@dataclass
class DockerRun:
    name: str
    image: str
    data_dir: Path
    port: int
    token: str | None = None


@dataclass
class LocalServer:
    data_dir: Path
    port: int
    token: str | None
    log_path: Path
    process: subprocess.Popen | None = None
    log_file: Any | None = None
    pid: int | None = None


def free_port() -> int:
    sock = socket.socket(socket.AF_INET, socket.SOCK_STREAM)
    sock.bind(("127.0.0.1", 0))
    port = int(sock.getsockname()[1])
    sock.close()
    return port


def docker(args: list[str], timeout: int = 30, check: bool = True) -> subprocess.CompletedProcess[str]:
    proc = subprocess.run(
        ["docker", *args],
        text=True,
        stdout=subprocess.PIPE,
        stderr=subprocess.STDOUT,
        timeout=timeout,
    )
    if check and proc.returncode != 0:
        raise RuntimeError(f"docker {' '.join(args)} failed ({proc.returncode}): {proc.stdout[-4000:]}")
    return proc


def remove_container(name: str) -> dict[str, Any]:
    proc = docker(["rm", "-f", name], timeout=20, check=False)
    return {"returncode": proc.returncode, "output_tail": proc.stdout[-2000:]}


def wait_rest(port: int, token: str | None = None, timeout_s: float = 30.0) -> None:
    deadline = time.time() + timeout_s
    client = TuringDB(type="json", host=f"http://127.0.0.1:{port}", token=token)
    last_error: Exception | None = None
    while time.time() < deadline:
        try:
            client.try_reach(timeout=2)
            return
        except Exception as exc:
            last_error = exc
            time.sleep(0.5)
    raise RuntimeError(f"TuringDB REST did not become ready on port {port}: {last_error}")


def start_container(run: DockerRun, reset_default: bool = False) -> dict[str, Any]:
    remove_container(run.name)
    run.data_dir.mkdir(parents=True, exist_ok=True)
    (run.data_dir / "data").mkdir(parents=True, exist_ok=True)
    cmd = [
        "run",
        "-d",
        "--name",
        run.name,
        "-p",
        f"127.0.0.1:{run.port}:6666",
        "-v",
        f"{run.data_dir}:/turing",
    ]
    if run.token:
        cmd.extend(["-e", f"TURINGDB_AUTH_TOKEN={run.token}"])
    cmd.extend(
        [
            run.image,
            "start",
            "-turing-dir",
            "/turing",
            "-i",
            "0.0.0.0",
            "-p",
            "6666",
        ]
    )
    if reset_default:
        cmd.append("-reset-default")
    if run.token:
        cmd.append("-auth-on")
    proc = docker(cmd, timeout=60)
    container_id = proc.stdout.strip()
    wait_rest(run.port, token=run.token)
    return {"container_id": container_id, "name": run.name, "port": run.port, "data_dir": str(run.data_dir)}


def logs(name: str) -> str:
    proc = docker(["logs", "--tail", "200", name], timeout=20, check=False)
    return proc.stdout


def image_from_env() -> str:
    return os.environ.get("TURINGDB_DOCKER_IMAGE", "aura-turingdb-spike:1.35")


def client_for(run: DockerRun) -> TuringDB:
    return TuringDB(type="json", host=f"http://127.0.0.1:{run.port}", token=run.token)


def client_for_local(server: LocalServer) -> TuringDB:
    return TuringDB(type="json", host=f"http://127.0.0.1:{server.port}", token=server.token)


def start_local_server(data_dir: Path, port: int, token: str | None = None) -> LocalServer:
    data_dir.mkdir(parents=True, exist_ok=True)
    (data_dir / "data").mkdir(parents=True, exist_ok=True)
    log_path = data_dir / "server.log"
    env = os.environ.copy()
    if token:
        env["TURINGDB_AUTH_TOKEN"] = token
    cmd = [
        "turingdb",
        "start",
        "-turing-dir",
        str(data_dir),
        "-i",
        "127.0.0.1",
        "-p",
        str(port),
        "-demon",
        "-start-timeout",
        "5000",
    ]
    if token:
        cmd.append("-auth-on")
    log_file = log_path.open("ab")
    process = subprocess.Popen(
        cmd,
        stdin=subprocess.DEVNULL,
        stdout=log_file,
        stderr=subprocess.STDOUT,
        env=env,
    )
    server = LocalServer(
        data_dir=data_dir,
        port=port,
        token=token,
        log_path=log_path,
        process=process,
        log_file=log_file,
        pid=process.pid,
    )
    try:
        wait_rest(port, token=token, timeout_s=30.0)
    except Exception:
        stop_local_server(server)
        raise
    return server


def stop_local_server(server: LocalServer | None) -> dict[str, Any]:
    if server is None:
        return {"stopped": False, "reason": "no server"}
    cmd = ["turingdb", "stop", "-turing-dir", str(server.data_dir), "-timeout", "5000"]
    with server.log_path.open("ab") as log_file:
        proc = subprocess.run(cmd, stdout=log_file, stderr=subprocess.STDOUT, timeout=20)
    if server.process is not None and server.process.poll() is None:
        try:
            server.process.wait(timeout=5)
        except subprocess.TimeoutExpired:
            server.process.terminate()
            try:
                server.process.wait(timeout=5)
            except subprocess.TimeoutExpired:
                server.process.kill()
                server.process.wait(timeout=5)
    if server.log_file is not None:
        try:
            server.log_file.close()
        except Exception:
            pass
    process_returncode = None if server.process is None else server.process.returncode
    return {
        "stopped": True,
        "stop_returncode": proc.returncode,
        "process_returncode": process_returncode,
        "log_path": str(server.log_path),
    }


def read_tail(path: Path, limit: int = 4000) -> str:
    if not path.exists():
        return ""
    data = path.read_bytes()
    return data[-limit:].decode("utf-8", errors="replace")
