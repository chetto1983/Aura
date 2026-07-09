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


def run_step(results: list[dict[str, Any]], name: str, fn) -> Any:
    started = time.perf_counter()
    try:
        value = fn()
        elapsed_ms = round((time.perf_counter() - started) * 1000, 3)
        results.append({"name": name, "ok": True, "elapsed_ms": elapsed_ms, "detail": value})
        event("PASS", f"{name} ({elapsed_ms} ms)")
        return value
    except Exception as exc:
        elapsed_ms = round((time.perf_counter() - started) * 1000, 3)
        results.append({"name": name, "ok": False, "elapsed_ms": elapsed_ms, "error": type(exc).__name__, "detail": str(exc)})
        event("FAIL", f"{name}: {type(exc).__name__}: {exc}")
        return None


def main() -> int:
    parser = argparse.ArgumentParser()
    parser.add_argument("--scratch", required=True)
    parser.add_argument("--out", required=True)
    args = parser.parse_args()

    scratch = Path(args.scratch)
    out = Path(args.out)
    if scratch.exists():
        shutil.rmtree(scratch)
    data_dir = scratch / "db"
    db_data_dir = data_dir / "data"
    db_data_dir.mkdir(parents=True, exist_ok=True)

    marker = f"aura090_{int(time.time())}"
    graph = "aura090"
    image = os.environ.get("TURINGDB_CONTAINER_IMAGE", "aura-turingdb-spike:1.35")
    results: list[dict[str, Any]] = []
    server = None
    first_log_tail = ""
    final_log_tail = ""
    event("SETUP", f"client package {__version__}; container_image={image}; data_dir={data_dir}")

    try:
        holder: dict[str, Any] = {}

        def start_first() -> dict[str, Any]:
            holder["server"] = start_local_server(data_dir, free_port())
            return {"pid": holder["server"].pid, "port": holder["server"].port}

        run_step(results, "start_turingdb_daemon", start_first)
        server = holder["server"]
        client = client_for_local(server)
        run_step(results, "create_graph", lambda: records(client.create_graph(graph)))
        run_step(results, "set_graph", lambda: client.set_graph(graph) or graph)

        def write_runtime_node() -> dict[str, Any]:
            change = client.new_change()
            client.query(
                'CREATE (:RuntimeProbe {id: 9001, name: "aura-runtime", marker: "'
                + marker
                + '"})'
            )
            client.query("CHANGE SUBMIT")
            client.checkout()
            return {"change": change, "marker": marker}

        run_step(results, "write_submit_runtime_node", write_runtime_node)

        def read_runtime_node(c) -> list[dict[str, Any]]:
            df = c.query("MATCH (n:RuntimeProbe) WHERE n.id = 9001 RETURN n.id, n.name, n.marker")
            rows = records(df)
            assert rows and rows[0].get("n.marker") == marker, rows
            return rows

        run_step(results, "read_runtime_node_before_restart", lambda: read_runtime_node(client))

        def create_load_vector() -> list[dict[str, Any]]:
            csv = db_data_dir / "runtime_vectors.csv"
            csv.write_text(
                "100,1.0,0.0,0.0,0.0\n"
                "101,0.0,1.0,0.0,0.0\n"
                "102,0.0,0.0,1.0,0.0\n"
                "103,0.0,0.0,0.0,1.0\n",
                encoding="utf-8",
            )
            client.query("CREATE VECTOR INDEX runtime_vectors WITH DIMENSION 4 METRIC EUCLID")
            client.query('LOAD VECTOR FROM "runtime_vectors.csv" IN runtime_vectors')
            df = client.query("VECTOR SEARCH IN runtime_vectors FOR 2 (1.0, 0.0, 0.0, 0.0) YIELD ids, score RETURN ids, score")
            rows = records(df)
            assert rows and rows[0].get("ids") == 100, rows
            return rows

        run_step(results, "create_load_search_vector_index", create_load_vector)
        first_log_tail = read_tail(server.log_path)
        run_step(results, "stop_daemon_before_restart", lambda: stop_local_server(server))
        server = None

        def restart() -> dict[str, Any]:
            holder["server"] = start_local_server(data_dir, free_port())
            return {"pid": holder["server"].pid, "port": holder["server"].port}

        run_step(results, "restart_daemon_same_volume", restart)
        server = holder["server"]
        restarted = client_for_local(server)
        run_step(results, "load_graph_after_restart", lambda: restarted.load_graph(graph, raise_if_loaded=False))
        restarted.set_graph(graph)
        run_step(results, "read_runtime_node_after_restart", lambda: read_runtime_node(restarted))

        def search_vector_after_restart() -> list[dict[str, Any]]:
            df = restarted.query("VECTOR SEARCH IN runtime_vectors FOR 2 (1.0, 0.0, 0.0, 0.0) YIELD ids, score RETURN ids, score")
            rows = records(df)
            assert rows and rows[0].get("ids") == 100, rows
            return rows

        run_step(results, "search_vector_index_after_restart", search_vector_after_restart)
        final_log_tail = read_tail(server.log_path)
    finally:
        cleanup = stop_local_server(server)
        results.append({"name": "cleanup_daemon", "ok": True, "detail": cleanup})

    required = {
        "start_turingdb_daemon",
        "create_graph",
        "write_submit_runtime_node",
        "read_runtime_node_before_restart",
        "load_graph_after_restart",
        "read_runtime_node_after_restart",
    }
    passed = {r["name"] for r in results if r["ok"]}
    graph_pass = required.issubset(passed)
    vector_pass = "search_vector_index_after_restart" in passed
    verdict = "VALIDATED" if graph_pass and vector_pass else ("PARTIAL" if graph_pass else "INVALIDATED")

    payload = {
        "id": "090-turingdb-runtime-durability-fit",
        "timestamp": iso_now(),
        "client_package_version": __version__,
        "container_image": image,
        "scratch": str(scratch),
        "verdict": verdict,
        "first_daemon_logs_tail": first_log_tail,
        "final_daemon_logs_tail": final_log_tail,
        "results": results,
    }
    out.write_text(json.dumps(payload, indent=2, sort_keys=True), encoding="utf-8")
    event("SUMMARY", f"verdict={verdict}; results={out}")
    return 0 if verdict in {"VALIDATED", "PARTIAL"} else 1


if __name__ == "__main__":
    raise SystemExit(main())
