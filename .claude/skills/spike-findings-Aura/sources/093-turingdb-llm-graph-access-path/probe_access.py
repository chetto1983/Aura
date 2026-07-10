from __future__ import annotations

import argparse
import json
import os
import shutil
import sys
import time
from datetime import datetime, timezone
from pathlib import Path
from typing import Any

import httpx
from turingdb import __version__

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from _turingdb_docker import client_for_local, free_port, read_tail, start_local_server, stop_local_server  # noqa: E402


def iso_now() -> str:
    return datetime.now(timezone.utc).isoformat()


def event(category: str, message: str) -> None:
    print(f"{iso_now()} [{category}] {message}", flush=True)


def clean(value: Any) -> Any:
    if hasattr(value, "item"):
        try:
            return value.item()
        except Exception:
            pass
    if value is None:
        return None
    if isinstance(value, float) and value != value:
        return None
    if isinstance(value, (str, int, float, bool)):
        return value
    return str(value)


def records(df: Any) -> list[dict[str, Any]]:
    return [{str(k): clean(v) for k, v in row.items()} for row in df.to_dict("records")]


def run_rest_roundtrip(server, graph: str) -> dict[str, Any]:
    client = client_for_local(server)
    client.create_graph(graph)
    client.set_graph(graph)
    change = client.new_change()
    client.query('CREATE (:AccessProbe {id: 9301, name: "rest-roundtrip"})')
    client.query("CHANGE SUBMIT")
    client.checkout()
    rows = records(client.query("MATCH (n:AccessProbe) RETURN n.id, n.name"))
    assert rows and rows[0].get("n.id") == 9301, rows
    return {"change": change, "rows": rows}


def mcp_shaped_query(server, graph: str) -> dict[str, Any]:
    client = client_for_local(server)
    client.set_graph(graph)
    tools = [
        {
            "name": "turingdb_query",
            "description": "Run a TuringDB query against a selected graph over the REST API.",
            "inputSchema": {"type": "object", "properties": {"graph": {"type": "string"}, "query": {"type": "string"}}, "required": ["graph", "query"]},
        },
        {
            "name": "turingdb_schema",
            "description": "List labels, edge types, and property types using TuringDB metadata procedures.",
            "inputSchema": {"type": "object", "properties": {"graph": {"type": "string"}}, "required": ["graph"]},
        },
    ]
    labels = records(client.query("CALL db.labels() YIELD label RETURN label"))
    rows = records(client.query("MATCH (n:AccessProbe) RETURN n.id, n.name"))
    return {"custom_bridge_required": True, "tools": tools, "labels": labels, "query_rows": rows}


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--scratch", required=True)
    parser.add_argument("--out", required=True)
    args = parser.parse_args()

    scratch = Path(args.scratch)
    out = Path(args.out)
    if scratch.exists():
        shutil.rmtree(scratch)
    scratch.mkdir(parents=True, exist_ok=True)
    image = os.environ.get("TURINGDB_CONTAINER_IMAGE", "aura-turingdb-spike:1.35")
    event("SETUP", f"client package {__version__}; container_image={image}; scratch={scratch}")
    results: list[dict[str, Any]] = []

    noauth = None
    auth = None
    noauth_logs = ""
    auth_logs = ""
    try:
        started = time.perf_counter()
        noauth = start_local_server(scratch / "server-noauth", free_port())
        results.append({"name": "start_rest_daemon", "ok": True, "detail": {"pid": noauth.pid, "port": noauth.port, "elapsed_ms": round((time.perf_counter() - started) * 1000, 3)}})
        event("PASS", f"REST daemon ready on {noauth.port}")
        results.append({"name": "rest_write_read_roundtrip", "ok": True, "detail": run_rest_roundtrip(noauth, "aura093")})
        event("PASS", "REST write/read roundtrip")
        results.append({"name": "mcp_shaped_rest_bridge", "ok": True, "detail": mcp_shaped_query(noauth, "aura093")})
        event("PASS", "MCP-shaped custom REST bridge")
        noauth_logs = read_tail(noauth.log_path)
    except Exception as exc:
        results.append({"name": "rest_path_error", "ok": False, "error": type(exc).__name__, "detail": str(exc)})
        noauth_logs = read_tail(noauth.log_path) if noauth else ""
        event("FAIL", f"REST path: {type(exc).__name__}: {exc}")
    finally:
        results.append({"name": "cleanup_rest_daemon", "ok": True, "detail": stop_local_server(noauth)})

    token = f"aura093-token-{int(time.time())}"
    try:
        started = time.perf_counter()
        auth = start_local_server(scratch / "server-auth", free_port(), token=token)
        results.append({"name": "start_auth_rest_daemon", "ok": True, "detail": {"pid": auth.pid, "port": auth.port, "elapsed_ms": round((time.perf_counter() - started) * 1000, 3)}})
        event("PASS", f"auth REST daemon ready on {auth.port}")
        unauth_status = None
        unauth_error = None
        try:
            response = httpx.post(f"http://127.0.0.1:{auth.port}/list_avail_graphs", timeout=5)
            unauth_status = response.status_code
        except Exception as exc:
            unauth_error = f"{type(exc).__name__}: {exc}"
        unauth_denied = unauth_status in {401, 403}
        results.append({"name": "auth_rejects_missing_token", "ok": unauth_denied, "detail": {"status": unauth_status, "error": unauth_error}})
        event("PASS" if unauth_denied else "FAIL", f"missing token status={unauth_status} error={unauth_error}")
        results.append({"name": "auth_accepts_bearer_token", "ok": True, "detail": run_rest_roundtrip(auth, "aura093auth")})
        event("PASS", "bearer token write/read roundtrip")
        auth_logs = read_tail(auth.log_path)
    except Exception as exc:
        results.append({"name": "auth_path_error", "ok": False, "error": type(exc).__name__, "detail": str(exc)})
        auth_logs = read_tail(auth.log_path) if auth else ""
        event("FAIL", f"auth path: {type(exc).__name__}: {exc}")
    finally:
        results.append({"name": "cleanup_auth_daemon", "ok": True, "detail": stop_local_server(auth)})

    passed = {r["name"] for r in results if r.get("ok")}
    required = {"start_rest_daemon", "rest_write_read_roundtrip", "mcp_shaped_rest_bridge"}
    auth_required = {"start_auth_rest_daemon", "auth_rejects_missing_token", "auth_accepts_bearer_token"}
    if required.issubset(passed) and auth_required.issubset(passed):
        verdict = "PARTIAL_CUSTOM_BRIDGE"
    elif required.issubset(passed):
        verdict = "PARTIAL_NO_AUTH_PROOF"
    else:
        verdict = "INVALIDATED"

    payload = {
        "id": "093-turingdb-llm-graph-access-path",
        "timestamp": iso_now(),
        "client_package_version": __version__,
        "container_image": image,
        "scratch": str(scratch),
        "verdict": verdict,
        "note": "TuringDB exposes REST/SDK access here; no native MCP or Bolt server was found in the pinned package, so Aura would need a custom MCP bridge.",
        "noauth_logs_tail": noauth_logs,
        "auth_logs_tail": auth_logs,
        "results": results,
    }
    out.write_text(json.dumps(payload, indent=2, sort_keys=True), encoding="utf-8")
    event("SUMMARY", f"verdict={verdict}; results={out}")
    return 0 if verdict != "INVALIDATED" else 1


if __name__ == "__main__":
    raise SystemExit(main())
