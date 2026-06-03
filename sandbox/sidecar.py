#!/usr/bin/env python3
"""Aura sandbox 2a sidecar — stdlib-only HTTP execution server (D-16).

This server is the trust boundary the Go runner (05-03) talks to over HTTP.
It is intentionally stdlib-only: no pip, no third-party imports. The CURATED
batteries-included package set (numpy/pandas/...) baked by the Dockerfile is
for USER CODE run via /exec/python, NOT for this server (D-20/D-20a).

Routes:
  POST /exec/python              -> python3 -c <code> (stateless, 2a)
  POST /exec/shell               -> bash    -c <code> (stateless, 2a)
  POST /session/{id}/exec/python -> exec(code) into a per-session namespace (2b)
  POST /session/{id}/exec/shell  -> bash -c <code> with a per-session cwd (2b)
  GET  /healthz                  -> 200 (compose healthcheck + runner auto-start probe)

Session-bound exec (2b, D-01/D-02/D-03) is asymmetric: a Python session keeps a
long-lived namespace dict so `x=42` set in call 1 is readable as `x` in call 2;
a shell session stays subprocess.run per call but re-applies the API-managed cwd,
so `cd`/`export` inside the snippet do NOT persist (only the tracked cwd does).
The sidecar stays Python-stdlib-only: persistence is `exec()` into a dict, NOT a
notebook-kernel (no third-party deps, no message-queue transport — D-03).

Request  body: {"code": str, "timeout_sec": int}
Response body: {"stdout","stderr","exit_code","elapsed_ms","truncated","limit_hit"}
  where limit_hit in {"timeout","oom","pids", null}.

The hardening floor (cap_drop:ALL, read_only, network_mode:none, seccomp,
pids/mem/cpu/nofile limits, gVisor runsc on x86) is supplied by compose +
the daemon, NOT by this process. This file only spawns the subprocess,
bounds it by the caller's timeout, and reports limit signals best-effort.
"""

import contextlib
import io
import json
import os
import subprocess
import threading
import time
import traceback
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

# Default per-session working dir for shell exec when the host has not pinned one.
# 2b workspaces are bind-mounted under /workspace; /tmp is the safe stdlib default
# inside the read_only rootfs (it is the writable tmpfs the container always has).
SESSION_DEFAULT_CWD = "/workspace"

# Per-session state: session_id -> {"ns": {<python globals>}, "cwd": <shell cwd>}.
# The python "ns" dict is the long-lived namespace that makes x=42 survive across
# calls (D-01). SESSIONS_LOCK guards the MAP (create/lookup) against a 2nd
# concurrent Aura process — the Go per-session lock (D-07) is Aura-local, so it
# does not serialize two Aura processes hitting the same sidecar (RESEARCH A6).
SESSIONS: dict[str, dict] = {}
SESSIONS_LOCK = threading.Lock()


def _parse_session_path(path: str) -> tuple[str, str] | None:
    """Parse POST /session/{id}/exec/{lang} -> (session_id, lang) or None.

    Returns None for any non-session path (the caller then tries the static
    INTERPRETERS map) and for a malformed/unknown-lang session path (the caller
    then 404s). The id is taken verbatim — the host sends a server-generated
    conversation id, not user input.
    """
    if not path.startswith("/session/"):
        return None
    parts = path.split("/")  # ["", "session", "{id}", "exec", "{lang}"]
    if len(parts) != 5 or parts[3] != "exec":
        return None
    session_id, lang = parts[2], parts[4]
    if not session_id or lang not in ("python", "shell"):
        return None
    return session_id, lang


def _get_session(session_id: str) -> dict:
    """Get-or-create the per-session state under SESSIONS_LOCK."""
    with SESSIONS_LOCK:
        sess = SESSIONS.get(session_id)
        if sess is None:
            sess = {"ns": {}, "cwd": SESSION_DEFAULT_CWD}
            SESSIONS[session_id] = sess
        return sess


def run_session_python(session_id: str, code: str, timeout_sec: int) -> dict:
    """Exec <code> into the session's persistent namespace (D-01).

    Unlike run_code this does NOT spawn a subprocess — persistence requires the
    namespace to outlive the call, so we exec into the kept dict and capture
    stdout/stderr by redirecting them to io.StringIO. timeout_sec is accepted for
    wire-contract parity (the Go runner always sends it) but a cooperative
    in-process exec cannot be hard-killed from here; the container's per-call
    deadline / mem+pids cgroup is the real bound (same as 2a). An exception in the
    user code is captured into stderr (exit_code=1) — it must NEVER crash the
    server (one bad call must not kill the whole session, D-03).
    """
    t0 = time.monotonic()
    sess = _get_session(session_id)
    ns = sess["ns"]
    out_buf, err_buf = io.StringIO(), io.StringIO()
    rc = 0
    with contextlib.redirect_stdout(out_buf), contextlib.redirect_stderr(err_buf):
        try:
            exec(compile(code, f"<session-{session_id}>", "exec"), ns)  # noqa: S102 — untrusted exec is the point; isolation is the container's job
        except BaseException:  # noqa: BLE001 — capture EVERYTHING (incl. SystemExit) so the server survives
            traceback.print_exc(file=err_buf)
            rc = 1
    out, err = out_buf.getvalue(), err_buf.getvalue()
    out_trunc = len(out) > MAX_STREAM
    err_trunc = len(err) > MAX_STREAM
    return {
        "stdout": out[:MAX_STREAM],
        "stderr": err[:MAX_STREAM],
        "exit_code": rc,
        "elapsed_ms": int((time.monotonic() - t0) * 1000),
        "truncated": out_trunc or err_trunc,
        "limit_hit": None,
    }


def run_session_shell(session_id: str, code: str, timeout_sec: int) -> dict:
    """Run bash -c <code> per call with the session's API-managed cwd (D-02).

    Asymmetric persistence: the shell is still a fresh subprocess every call, so
    `cd`/`export` inside the snippet do NOT persist; only the tracked session cwd is
    re-applied as subprocess.run(cwd=...). Reuses run_code for the timeout/oom/pids
    + 1 MiB truncation contract.
    """
    sess = _get_session(session_id)
    return run_code(INTERPRETERS["/exec/shell"], code, timeout_sec, cwd=sess["cwd"])


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


def run_code(argv_prefix: list[str], code: str, timeout_sec: int, cwd: str | None = None) -> dict:
    """Run <interp> -c <code> in an isolated subprocess, bounded by timeout_sec.

    Maps the outcome to the D-16 response shape. limit_hit precedence:
      timeout (TimeoutExpired) > oom (rc 137 = SIGKILL from the mem cgroup) >
      pids (best-effort fork-failure heuristic) > null.

    cwd re-applies a per-session working dir for the 2b shell session path (D-02);
    it is None for the stateless 2a path, leaving the inherited cwd untouched.

    A set-but-missing cwd (a misconfigured/absent bind-mount) degrades to the tmpfs
    default /tmp with a stderr note rather than an opaque exit 127 — subprocess.run
    raises FileNotFoundError for BOTH a missing interpreter AND a missing cwd, so we
    disambiguate here: a non-existent cwd is recovered, a genuinely missing
    interpreter still maps to 127 below.
    """
    t0 = time.monotonic()
    limit_hit: str | None = None
    cwd_note = ""
    if cwd is not None and not os.path.isdir(cwd):
        cwd_note = f"[sandbox] cwd {cwd!r} does not exist; falling back to /tmp\n"
        cwd = "/tmp"
    try:
        proc = subprocess.run(  # noqa: S603 — untrusted code execution is the whole point; isolation is the container's job
            [*argv_prefix, code],
            capture_output=True,
            text=True,
            timeout=timeout_sec,
            cwd=cwd,
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

    if cwd_note:
        err = cwd_note + err

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

    def _read_exec_request(self) -> dict | None:
        """Read+validate the {"code","timeout_sec"} body shared by all exec routes.

        Returns the parsed dict, or None after having already sent the 4xx error
        (the caller just returns on None).
        """
        try:
            length = int(self.headers.get("Content-Length", "0"))
        except ValueError:
            self._send_json(400, {"error": "invalid Content-Length"})
            return None
        if length <= 0 or length > MAX_BODY_BYTES:
            self._send_json(400, {"error": "missing or oversized request body"})
            return None

        raw = self.rfile.read(length)
        try:
            req = json.loads(raw)
        except (json.JSONDecodeError, UnicodeDecodeError):
            self._send_json(400, {"error": "malformed JSON request body"})
            return None
        if not isinstance(req, dict) or not isinstance(req.get("code"), str):
            self._send_json(400, {"error": "request must be an object with a string 'code'"})
            return None
        return req

    def _drain_body(self) -> None:
        """Discard the request body so a 4xx reply does not leave the keep-alive
        connection half-read (an undrained body triggers a client-side RST)."""
        try:
            length = int(self.headers.get("Content-Length", "0"))
        except ValueError:
            return
        if 0 < length <= MAX_BODY_BYTES:
            self.rfile.read(length)

    def do_POST(self) -> None:  # noqa: N802 — BaseHTTPRequestHandler dispatch name
        session = _parse_session_path(self.path)
        argv_prefix = INTERPRETERS.get(self.path)
        if session is None and argv_prefix is None:
            self._drain_body()
            self._send_json(404, {"error": "unknown path"})
            return

        req = self._read_exec_request()
        if req is None:
            return
        timeout_sec = _coerce_timeout(req.get("timeout_sec"))

        if session is not None:
            session_id, lang = session
            if lang == "python":
                result = run_session_python(session_id, req["code"], timeout_sec)
            else:
                result = run_session_shell(session_id, req["code"], timeout_sec)
        else:
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
