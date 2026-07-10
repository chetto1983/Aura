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

from turingdb import TuringDB, __version__

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


def classify_error(exc: Exception) -> dict[str, str]:
    text = str(exc)
    first = text.splitlines()[0] if text else type(exc).__name__
    return {"type": type(exc).__name__, "message": first[:500]}


def run_read(client: TuringDB, row: dict[str, Any]) -> dict[str, Any]:
    started = time.perf_counter()
    try:
        df = client.query(row["query"])
        elapsed_ms = round((time.perf_counter() - started) * 1000, 3)
        out = {**row, "ok": True, "elapsed_ms": elapsed_ms, "row_count": int(len(df.index)), "sample": records(df.head(5))}
        event("PASS", f"{row['name']} ({elapsed_ms} ms)")
        return out
    except Exception as exc:
        elapsed_ms = round((time.perf_counter() - started) * 1000, 3)
        out = {**row, "ok": False, "elapsed_ms": elapsed_ms, "error": classify_error(exc)}
        event("FAIL", f"{row['name']}: {out['error']['message']}")
        return out


def run_write(client: TuringDB, row: dict[str, Any]) -> dict[str, Any]:
    started = time.perf_counter()
    try:
        change = client.new_change()
        df = client.query(row["query"])
        client.query("CHANGE SUBMIT")
        client.checkout()
        verify_rows = None
        if row.get("verify_query"):
            verify_rows = records(client.query(row["verify_query"]))
            if row.get("verify_column"):
                expected = row.get("verify_value")
                actual = verify_rows[0].get(row["verify_column"]) if verify_rows else None
                assert actual == expected, {"expected": expected, "actual": actual, "rows": verify_rows}
        elapsed_ms = round((time.perf_counter() - started) * 1000, 3)
        out = {**row, "ok": True, "elapsed_ms": elapsed_ms, "change": change, "row_count": int(len(df.index)), "sample": records(df.head(5))}
        if verify_rows is not None:
            out["verify_rows"] = verify_rows
        event("PASS", f"{row['name']} ({elapsed_ms} ms)")
        return out
    except Exception as exc:
        try:
            client.checkout()
        except Exception:
            pass
        elapsed_ms = round((time.perf_counter() - started) * 1000, 3)
        out = {**row, "ok": False, "elapsed_ms": elapsed_ms, "error": classify_error(exc)}
        event("FAIL", f"{row['name']}: {out['error']['message']}")
        return out


def seed_graph(client: TuringDB, data_dir: Path) -> None:
    client.create_graph("aura091")
    client.set_graph("aura091")
    client.new_change()
    client.query(
        'CREATE (alice:Entity:Person {id: 1, name: "Alice", kind: "person", created_at: 1000}), '
        '(bob:Entity:Person {id: 2, name: "Bob", kind: "person", created_at: 1001}), '
        '(rome:Entity:Place {id: 3, name: "Rome", kind: "place", created_at: 1002}), '
        '(alice)-[:KNOWS {weight: 1.0, since: 2024}]->(bob), '
        '(bob)-[:VISITED {weight: 2.0, since: 2025}]->(rome)'
    )
    client.query("CHANGE SUBMIT")
    client.checkout()
    vector_file = data_dir / "data" / "compat_vectors.csv"
    vector_file.parent.mkdir(parents=True, exist_ok=True)
    vector_file.write_text("1,1.0,0.0,0.0,0.0\n2,0.0,1.0,0.0,0.0\n3,0.0,0.0,1.0,0.0\n", encoding="utf-8")
    client.query("CREATE VECTOR INDEX compat_vectors WITH DIMENSION 4 METRIC EUCLID")
    client.query('LOAD VECTOR FROM "compat_vectors.csv" IN compat_vectors')


def matrix_rows() -> tuple[list[dict[str, Any]], list[dict[str, Any]]]:
    reads = [
        {"name": "basic_match", "category": "native_basic", "importance": "required", "query": "MATCH (n:Person) RETURN n.id, n.name", "expect": "pass"},
        {"name": "multi_label_match", "category": "native_basic", "importance": "required", "query": "MATCH (n:Entity:Person) RETURN n.name, labels(n)", "expect": "pass"},
        {"name": "edge_type_function", "category": "native_basic", "importance": "required", "query": "MATCH ()-[e]->() RETURN edgeType(e)", "expect": "pass"},
        {"name": "db_labels", "category": "metadata", "importance": "useful", "query": "CALL db.labels() YIELD label RETURN label", "expect": "pass"},
        {"name": "db_edge_types", "category": "metadata", "importance": "useful", "query": "CALL db.edgeTypes() YIELD edgeType RETURN edgeType", "expect": "pass"},
        {"name": "db_property_types", "category": "metadata", "importance": "useful", "query": "CALL db.propertyTypes() YIELD propertyType, valueType RETURN propertyType, valueType", "expect": "pass"},
        {"name": "neo4j_type_function", "category": "neo4j_compat", "importance": "required_for_drop_in", "query": "MATCH ()-[r]->() RETURN type(r)", "expect": "pass"},
        {"name": "neo4j_relationship_types_proc", "category": "neo4j_compat", "importance": "required_for_drop_in", "query": "CALL db.relationshipTypes() YIELD relationshipType RETURN relationshipType", "expect": "pass"},
        {"name": "neo4j_element_id", "category": "neo4j_compat", "importance": "required_for_drop_in", "query": "MATCH (n:Person) RETURN elementId(n)", "expect": "pass"},
        {"name": "properties_function", "category": "neo4j_compat", "importance": "useful", "query": "MATCH (n:Person) RETURN properties(n)", "expect": "pass"},
        {"name": "datetime_function", "category": "agent_memory", "importance": "required_for_agent_memory", "query": "RETURN datetime()", "expect": "pass"},
        {"name": "duration_function", "category": "agent_memory", "importance": "required_for_agent_memory", "query": "RETURN duration({days: 7})", "expect": "pass"},
        {"name": "point_function", "category": "agent_memory", "importance": "required_for_agent_memory", "query": "RETURN point({latitude: 45.0, longitude: 7.0})", "expect": "pass"},
        {"name": "apoc_remove_keys", "category": "agent_memory", "importance": "required_for_agent_memory", "query": 'RETURN apoc.map.removeKeys({a: 1, b: 2}, ["b"])', "expect": "pass"},
        {"name": "call_subquery", "category": "agent_memory", "importance": "required_for_agent_memory", "query": "CALL { MATCH (n:Person) RETURN count(n) AS c } RETURN c", "expect": "pass"},
        {"name": "neo4j_vector_query_proc", "category": "graphrag", "importance": "required_for_drop_in", "query": 'CALL db.index.vector.queryNodes("compat_vectors", 2, [1.0, 0.0, 0.0, 0.0]) YIELD node, score RETURN node.id, score', "expect": "pass"},
        {"name": "turing_vector_search", "category": "graphrag", "importance": "required_if_porting", "query": "VECTOR SEARCH IN compat_vectors FOR 2 (1.0, 0.0, 0.0, 0.0) YIELD ids MATCH (n:Entity) WHERE n.id = ids RETURN n.id, n.name", "expect": "pass"},
        {"name": "shortest_path_native", "category": "graph_algorithms", "importance": "useful", "query": 'MATCH (a {name: "Alice"}), (b {name: "Rome"}) shortestPath(a,b,weight,dist,path) RETURN dist', "expect": "pass"},
        {"name": "gds_pagerank", "category": "graph_algorithms", "importance": "required_for_neo4j_gds", "query": 'CALL gds.pageRank.stream("aura091") YIELD nodeId, score RETURN nodeId, score', "expect": "pass"},
    ]
    writes = [
        {"name": "merge_on_create_set", "category": "agent_memory_mutation", "importance": "required_for_agent_memory", "query": 'MERGE (n:Person {id: 99}) ON CREATE SET n.name = "Merge Person" RETURN n.id', "expect": "pass"},
        {"name": "foreach_mutation", "category": "agent_memory_mutation", "importance": "required_for_agent_memory", "query": "FOREACH (x IN [10, 11] | CREATE (:LoopNode {id: x}))", "expect": "pass"},
        {
            "name": "match_set_existing",
            "category": "mutation",
            "importance": "useful",
            "query": 'MATCH (n:Person {id: 1}) SET n.kind = "human" RETURN n.kind',
            "verify_query": "MATCH (n:Person {id: 1}) RETURN n.kind",
            "verify_column": "n.kind",
            "verify_value": "human",
            "expect": "pass",
        },
    ]
    return reads, writes


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
    (data_dir / "data").mkdir(parents=True, exist_ok=True)
    image = os.environ.get("TURINGDB_CONTAINER_IMAGE", "aura-turingdb-spike:1.35")
    event("SETUP", f"client package {__version__}; container_image={image}; data_dir={data_dir}")

    results: list[dict[str, Any]] = []
    server = None
    daemon_logs = ""
    try:
        server = start_local_server(data_dir, free_port())
        results.append({"name": "start_turingdb_daemon", "ok": True, "detail": {"pid": server.pid, "port": server.port}})
        client = client_for_local(server)
        seed_graph(client, data_dir)
        reads, writes = matrix_rows()
        results.extend(run_read(client, row) for row in reads)
        results.extend(run_write(client, row) for row in writes)
        daemon_logs = read_tail(server.log_path)
    except Exception as exc:
        results.append({"name": "setup_or_seed", "ok": False, "error": type(exc).__name__, "detail": str(exc)})
        daemon_logs = read_tail(server.log_path) if server else ""
        event("FAIL", f"setup_or_seed: {type(exc).__name__}: {exc}")
    finally:
        cleanup = stop_local_server(server)
        results.append({"name": "cleanup_daemon", "ok": True, "detail": cleanup})

    by_importance: dict[str, dict[str, int]] = {}
    for result in results:
        if "importance" not in result:
            continue
        bucket = by_importance.setdefault(result["importance"], {"pass": 0, "fail": 0})
        bucket["pass" if result["ok"] else "fail"] += 1

    drop_in_required = [r for r in results if r.get("importance") in {"required_for_drop_in", "required_for_agent_memory", "required_for_neo4j_gds"}]
    drop_in_failures = [r for r in drop_in_required if not r["ok"]]
    porting_required = [r for r in results if r.get("importance") in {"required", "required_if_porting"}]
    porting_failures = [r for r in porting_required if not r["ok"]]
    if drop_in_failures:
        verdict = "INVALIDATED_AS_DROP_IN"
    elif porting_failures:
        verdict = "PARTIAL"
    else:
        verdict = "VALIDATED"

    payload = {
        "id": "091-turingdb-aura-cypher-compat",
        "timestamp": iso_now(),
        "client_package_version": __version__,
        "container_image": image,
        "scratch": str(scratch),
        "verdict": verdict,
        "summary": by_importance,
        "drop_in_failures": [r["name"] for r in drop_in_failures],
        "porting_failures": [r["name"] for r in porting_failures],
        "daemon_logs_tail": daemon_logs,
        "results": results,
    }
    out.write_text(json.dumps(payload, indent=2, sort_keys=True), encoding="utf-8")
    event("SUMMARY", f"verdict={verdict}; results={out}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
