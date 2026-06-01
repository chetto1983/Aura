#!/usr/bin/env python3
"""Aura sandbox 2a sidecar — stdlib-only HTTP execution server (D-16).

This server is the trust boundary the Go runner (05-03) talks to over HTTP.
It is intentionally stdlib-only: no pip, no third-party imports. The CURATED
batteries-included package set (numpy/pandas/...) baked by the Dockerfile is
for USER CODE run via /exec/python, NOT for this server (D-20/D-20a).

Routes:
  POST /exec/python  -> python3 -c <code>
  POST /exec/shell   -> bash    -c <code>
  GET  /healthz      -> 200 (compose healthcheck + runner auto-start probe)

Request  body: {"code": str, "timeout_sec": int}
Response body: {"stdout","stderr","exit_code","elapsed_ms","truncated","limit_hit"}
  where limit_hit in {"timeout","oom","pids", null}.

The hardening floor (cap_drop:ALL, read_only, network_mode:none, seccomp,
pids/mem/cpu/nofile limits, gVisor runsc on x86) is supplied by compose +
the daemon, NOT by this process. This file only spawns the subprocess,
bounds it by the caller's timeout, and reports limit signals best-effort.
"""

import json
import os
import subprocess
import time
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

# Per-stream server-side truncation cap (D-16: 1 MiB each).
MAX_STREAM = 1 << 20

# Fallback timeout when the request omits/zeroes timeout_sec. The Go runner
# already clamps the value to <=600 and substitutes its config default for an
# omitted field, so a supplied positive integer is trusted as-is; this is only
# the floor for a malformed/missing value.
DEFAULT_TIMEOUT_SEC = 30

# Largest request body we will read (defends the server itself; user code size
# is bounded well below this in practice).
MAX_BODY_BYTES = 8 << 20

LISTEN_HOST = "0.0.0.0"  # noqa: S104 — bound inside a network_mode:none container; reached only via the loopback compose port mapping
LISTEN_PORT = 18901

# Per-language interpreter dispatch. Both run "<interp> -c <code>" so the wire
# contract is identical across languages (D-16).
INTERPRETERS = {
    "/exec/python": ["python3", "-c"],
    "/exec/shell": ["bash", "-c"],
}


def _coerce_timeout(raw: object) -> int:
    """Trust a positive int timeout_sec; fall back to the safe default otherwise."""
    try:
        val = int(raw)
    except (TypeError, ValueError):
        return DEFAULT_TIMEOUT_SEC
    if val <= 0:
        return DEFAULT_TIMEOUT_SEC
    return val


def _detect_pids_limit(stderr: str) -> bool:
    """Best-effort pids-cap heuristic (RESEARCH OQ2).

    Under pids_limit a fork attempt surfaces as a fork/Resource-temporarily-
    unavailable error in the child's stderr. We detect that text rather than
    introspecting cgroup v2 pids.events from inside a read_only, cap_drop:ALL
    container (which is unreliable). D-16 only requires the field be reported;
    the Go side never guesses it. TRACKED-REFINEMENT: a precise cgroup-v2
    pids.events read could replace this heuristic in 2b.
    """
    needles = (
        "blockingioerror",
        "resource temporarily unavailable",
        "cannot allocate memory",
        "fork: retry",
        "fork: resource",
        "can't fork",
        "would exceed",  # bash: "fork: retry: ..." variants
    )
    low = stderr.lower()
    return any(n in low for n in needles)


def run_code(argv_prefix: list[str], code: str, timeout_sec: int) -> dict:
    """Run <interp> -c <code> in an isolated subprocess, bounded by timeout_sec.

    Maps the outcome to the D-16 response shape. limit_hit precedence:
      timeout (TimeoutExpired) > oom (rc 137 = SIGKILL from the mem cgroup) >
      pids (best-effort fork-failure heuristic) > null.
    """
    t0 = time.monotonic()
    limit_hit: str | None = None
    try:
        proc = subprocess.run(  # noqa: S603 — untrusted code execution is the whole point; isolation is the container's job
            [*argv_prefix, code],
            capture_output=True,
            text=True,
            timeout=timeout_sec,
            # No shell=True: argv is fixed; user code rides as the -c argument.
            check=False,
        )
        out, err, rc = proc.stdout or "", proc.stderr or "", proc.returncode
    except subprocess.TimeoutExpired as exc:
        out = exc.stdout or ""
        err = exc.stderr or ""
        if isinstance(out, bytes):
            out = out.decode("utf-8", "replace")
        if isinstance(err, bytes):
            err = err.decode("utf-8", "replace")
        rc = 124
        limit_hit = "timeout"
    except FileNotFoundError:
        # Interpreter missing in the image — an infra/build fault, surfaced as a
        # non-zero exit + stderr so the runner maps it like any failed exec.
        out, err, rc = "", "interpreter not found in sandbox image", 127

    if limit_hit is None:
        if rc == 137:  # 128 + SIGKILL: the mem cgroup OOM-killed the child
            limit_hit = "oom"
        elif rc != 0 and _detect_pids_limit(err):
            limit_hit = "pids"

    out_trunc = len(out) > MAX_STREAM
    err_trunc = len(err) > MAX_STREAM
    return {
        "stdout": out[:MAX_STREAM],
        "stderr": err[:MAX_STREAM],
        "exit_code": rc,
        "elapsed_ms": int((time.monotonic() - t0) * 1000),
        "truncated": out_trunc or err_trunc,
        "limit_hit": limit_hit,
    }


class Handler(BaseHTTPRequestHandler):
    # Quiet by default; the container's stdout is the operator log surface.
    def log_message(self, fmt: str, *args: object) -> None:  # noqa: A002
        return

    def _send_json(self, status: int, payload: dict) -> None:
        body = json.dumps(payload).encode("utf-8")
        self.send_response(status)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def do_GET(self) -> None:  # noqa: N802 — BaseHTTPRequestHandler dispatch name
        if self.path == "/healthz":
            self._send_json(200, {"status": "ok"})
            return
        self._send_json(404, {"error": "not found"})

    def do_POST(self) -> None:  # noqa: N802 — BaseHTTPRequestHandler dispatch name
        argv_prefix = INTERPRETERS.get(self.path)
        if argv_prefix is None:
            self._send_json(404, {"error": "unknown path"})
            return

        try:
            length = int(self.headers.get("Content-Length", "0"))
        except ValueError:
            self._send_json(400, {"error": "invalid Content-Length"})
            return
        if length <= 0 or length > MAX_BODY_BYTES:
            self._send_json(400, {"error": "missing or oversized request body"})
            return

        raw = self.rfile.read(length)
        try:
            req = json.loads(raw)
        except (json.JSONDecodeError, UnicodeDecodeError):
            self._send_json(400, {"error": "malformed JSON request body"})
            return
        if not isinstance(req, dict) or not isinstance(req.get("code"), str):
            self._send_json(400, {"error": "request must be an object with a string 'code'"})
            return

        timeout_sec = _coerce_timeout(req.get("timeout_sec"))
        result = run_code(argv_prefix, req["code"], timeout_sec)
        self._send_json(200, result)


def main() -> None:
    port = int(os.environ.get("AURA_SANDBOX_SIDECAR_PORT", str(LISTEN_PORT)))
    server = ThreadingHTTPServer((LISTEN_HOST, port), Handler)
    try:
        server.serve_forever()
    except KeyboardInterrupt:
        pass
    finally:
        server.server_close()


if __name__ == "__main__":
    main()
