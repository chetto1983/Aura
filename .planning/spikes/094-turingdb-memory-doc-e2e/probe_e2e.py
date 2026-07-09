from __future__ import annotations

import argparse
import base64
import json
import math
import os
import shutil
import statistics
import sys
import time
from datetime import datetime, timezone
from pathlib import Path
from typing import Any, Callable

from turingdb import __version__

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from _turingdb_docker import (  # noqa: E402
    client_for_local,
    free_port,
    read_tail,
    start_local_server,
    stop_local_server,
)


GRAPH = "aura094"
DIM = 12
MEMORY_INDEX = "aura094_memory_vectors"
DOCUMENT_INDEX = "aura094_document_vectors"


VOCAB = {
    "espresso": 0,
    "coffee": 0,
    "turingdb": 1,
    "turing": 1,
    "docker": 2,
    "memory": 3,
    "agent": 3,
    "reset": 4,
    "interlock": 5,
    "guard": 5,
    "document": 6,
    "retrieval": 6,
    "citation": 7,
    "chunk": 7,
    "latency": 8,
    "graph": 9,
    "safety": 10,
    "maintenance": 11,
}


MEMORIES = [
    {
        "vector_id": 1001,
        "id": "mem-alice-espresso",
        "user_id": "alice",
        "session_id": "goal-turingdb",
        "kind": "message",
        "content": "Davide prefers espresso after lunch while reviewing agent memory.",
    },
    {
        "vector_id": 1002,
        "id": "mem-alice-turingdb",
        "user_id": "alice",
        "session_id": "goal-turingdb",
        "kind": "fact",
        "content": "Aura migration target is TuringDB with Docker for graph memory.",
    },
    {
        "vector_id": 1003,
        "id": "mem-alice-doc-citations",
        "user_id": "alice",
        "session_id": "goal-turingdb",
        "kind": "preference",
        "content": "Keep document retrieval answers cited with chunk locators.",
    },
    {
        "vector_id": 1004,
        "id": "mem-bob-espresso",
        "user_id": "bob",
        "session_id": "decoy",
        "kind": "message",
        "content": "Bob wants espresso machine prices but has no Aura memory access.",
    },
]


DOCUMENTS = [
    {
        "id": "doc-machine-safety",
        "owner": "alice",
        "title": "Machine Safety Manual",
        "chunks": [
            {
                "vector_id": 2001,
                "chunk_id": "doc-machine-safety#1",
                "ordinal": 1,
                "locator": "page=4",
                "text": "Emergency stop reset requires the blue key and a safety guard interlock check.",
            },
            {
                "vector_id": 2002,
                "chunk_id": "doc-machine-safety#2",
                "ordinal": 2,
                "locator": "page=5",
                "text": "After reset, verify guard interlock lights before restarting the conveyor.",
            },
            {
                "vector_id": 2003,
                "chunk_id": "doc-machine-safety#3",
                "ordinal": 3,
                "locator": "page=12",
                "text": "Monthly maintenance records include oil inspection and latency-free checklist logging.",
            },
        ],
    },
    {
        "id": "doc-coffee-notes",
        "owner": "bob",
        "title": "Coffee Notes",
        "chunks": [
            {
                "vector_id": 2101,
                "chunk_id": "doc-coffee-notes#1",
                "ordinal": 1,
                "locator": "note=1",
                "text": "Espresso grinder notes for Bob's coffee workflow.",
            },
        ],
    },
]


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


def normalize(vec: list[float]) -> list[float]:
    norm = math.sqrt(sum(x * x for x in vec))
    if norm == 0:
        return [1.0] + [0.0] * (len(vec) - 1)
    return [x / norm for x in vec]


def embed(text: str) -> list[float]:
    vec = [0.0] * DIM
    lower = text.lower()
    for token, idx in VOCAB.items():
        if token in lower:
            vec[idx] += 1.0
    return normalize(vec)


def vector_literal(vec: list[float]) -> str:
    return "(" + ", ".join(f"{value:.8f}" for value in vec) + ")"


def esc(value: str) -> str:
    return value.replace("\\", "\\\\").replace('"', '\\"')


def write_vector_csv(path: Path, rows: list[tuple[int, list[float]]]) -> None:
    path.parent.mkdir(parents=True, exist_ok=True)
    with path.open("w", encoding="utf-8", newline="\n") as handle:
        for vector_id, vec in rows:
            handle.write(str(vector_id))
            handle.write(",")
            handle.write(",".join(f"{x:.8f}" for x in vec))
            handle.write("\n")


def percentile(values: list[float], pct: float) -> float:
    ordered = sorted(values)
    index = min(len(ordered) - 1, max(0, math.ceil((pct / 100.0) * len(ordered)) - 1))
    return ordered[index]


def add_check(checks: list[dict[str, Any]], name: str, ok: bool, detail: Any = None) -> None:
    checks.append({"name": name, "points": 1.0, "ok": bool(ok), "detail": detail})
    event("PASS" if ok else "FAIL", name)


def parse_printing_press_version(raw: str) -> dict[str, Any]:
    raw_b64 = os.environ.get("PRINTING_PRESS_VERSION_B64", "")
    if raw_b64:
        try:
            raw = base64.b64decode(raw_b64).decode("utf-8")
        except Exception as exc:
            return {"ok": False, "reason": f"invalid base64: {exc}", "raw_b64": raw_b64[:500]}
    if not raw:
        return {"ok": False, "reason": "PRINTING_PRESS_VERSION_JSON was not provided"}
    try:
        parsed = json.loads(raw)
    except json.JSONDecodeError as exc:
        return {"ok": False, "reason": f"invalid JSON: {exc}", "raw": raw[:500]}
    version = str(parsed.get("version", ""))
    parts = tuple(int(p) for p in version.split(".")[:3] if p.isdigit())
    ok = parts >= (4, 28, 0)
    return {"ok": ok, "version": version, "go": parsed.get("go"), "raw": parsed}


def create_graph(client, data_dir: Path) -> dict[str, Any]:
    client.create_graph(GRAPH)
    client.set_graph(GRAPH)
    client.new_change()

    nodes: list[str] = [
        '(alice:User {identifier: "alice", display: "Alice"})',
        '(bob:User {identifier: "bob", display: "Bob"})',
    ]
    for memory in MEMORIES:
        nodes.append(
            f'({memory["id"].replace("-", "_")}:Memory '
            f'{{id: "{esc(memory["id"])}", vector_id: {memory["vector_id"]}, '
            f'user_id: "{memory["user_id"]}", session_id: "{memory["session_id"]}", '
            f'kind: "{memory["kind"]}", content: "{esc(memory["content"])}"}})'
        )
    for doc in DOCUMENTS:
        nodes.append(
            f'({doc["id"].replace("-", "_")}:Document '
            f'{{id: "{doc["id"]}", owner: "{doc["owner"]}", title: "{esc(doc["title"])}", '
            f'chunk_count: {len(doc["chunks"])}, status: "searchable"}})'
        )
        for chunk in doc["chunks"]:
            nodes.append(
                f'({chunk["chunk_id"].replace("-", "_").replace("#", "_")}:Chunk '
                f'{{chunk_id: "{chunk["chunk_id"]}", vector_id: {chunk["vector_id"]}, '
                f'document_id: "{doc["id"]}", owner: "{doc["owner"]}", ordinal: {chunk["ordinal"]}, '
                f'locator: "{chunk["locator"]}", status: "active", text: "{esc(chunk["text"])}"}})'
            )

    edges: list[str] = []
    for memory in MEMORIES:
        user_var = memory["user_id"]
        memory_var = memory["id"].replace("-", "_")
        edges.append(f"({user_var})-[:HAS_MEMORY]->({memory_var})")
    for doc in DOCUMENTS:
        user_var = doc["owner"]
        doc_var = doc["id"].replace("-", "_")
        edges.append(f"({user_var})-[:HAS_DOCUMENT]->({doc_var})")
        previous_var = ""
        for chunk in doc["chunks"]:
            chunk_var = chunk["chunk_id"].replace("-", "_").replace("#", "_")
            edges.append(f"({doc_var})-[:HAS_CHUNK {{ordinal: {chunk['ordinal']}}}]->({chunk_var})")
            if previous_var:
                edges.append(f"({previous_var})-[:NEXT_CHUNK]->({chunk_var})")
            previous_var = chunk_var

    client.query("CREATE " + ", ".join(nodes + edges))
    client.query("CHANGE SUBMIT")
    client.checkout()

    memory_vectors = [(m["vector_id"], embed(m["content"])) for m in MEMORIES]
    document_vectors: list[tuple[int, list[float]]] = []
    for doc in DOCUMENTS:
        for chunk in doc["chunks"]:
            document_vectors.append((chunk["vector_id"], embed(chunk["text"])))

    memory_csv = data_dir / "data" / "memory_vectors.csv"
    document_csv = data_dir / "data" / "document_vectors.csv"
    write_vector_csv(memory_csv, memory_vectors)
    write_vector_csv(document_csv, document_vectors)
    client.query(f"CREATE VECTOR INDEX {MEMORY_INDEX} WITH DIMENSION {DIM} METRIC COSINE")
    client.query(f'LOAD VECTOR FROM "{memory_csv.name}" IN {MEMORY_INDEX}')
    client.query(f"CREATE VECTOR INDEX {DOCUMENT_INDEX} WITH DIMENSION {DIM} METRIC COSINE")
    client.query(f'LOAD VECTOR FROM "{document_csv.name}" IN {DOCUMENT_INDEX}')

    return {
        "graph": GRAPH,
        "memory_rows": len(memory_vectors),
        "document_vector_rows": len(document_vectors),
        "memory_index": MEMORY_INDEX,
        "document_index": DOCUMENT_INDEX,
    }


def sorted_by_score(rows: list[dict[str, Any]]) -> list[dict[str, Any]]:
    return sorted(rows, key=lambda row: float(row.get("score") or 0.0), reverse=True)


def search_memory(client, user_id: str, query: str, limit: int = 5) -> list[dict[str, Any]]:
    literal = vector_literal(embed(query))
    df = client.query(
        f"VECTOR SEARCH IN {MEMORY_INDEX} FOR 8 {literal} YIELD ids, score "
        f'MATCH (m:Memory) WHERE m.vector_id = ids AND m.user_id = "{esc(user_id)}" '
        "RETURN m.id, m.user_id, m.kind, m.content, score"
    )
    return sorted_by_score(records(df))[:limit]


def search_documents(client, owner: str, query: str, limit: int = 5) -> list[dict[str, Any]]:
    literal = vector_literal(embed(query))
    df = client.query(
        f"VECTOR SEARCH IN {DOCUMENT_INDEX} FOR 8 {literal} YIELD ids, score "
        f'MATCH (c:Chunk) WHERE c.vector_id = ids AND c.owner = "{esc(owner)}" '
        "RETURN c.chunk_id, c.document_id, c.locator, c.text, score"
    )
    return sorted_by_score(records(df))[:limit]


def next_chunk_context(client, chunk_vector_id: int) -> list[dict[str, Any]]:
    df = client.query(
        "MATCH (c:Chunk)-[:NEXT_CHUNK]->(n:Chunk) "
        f"WHERE c.vector_id = {chunk_vector_id} "
        "RETURN n.chunk_id, n.locator, n.text"
    )
    return records(df)


def run_latency_sample(client) -> dict[str, Any]:
    memory_latencies: list[float] = []
    document_latencies: list[float] = []
    for _ in range(15):
        started = time.perf_counter()
        search_memory(client, "alice", "espresso agent memory")
        memory_latencies.append((time.perf_counter() - started) * 1000)
        started = time.perf_counter()
        search_documents(client, "alice", "reset safety guard interlock")
        document_latencies.append((time.perf_counter() - started) * 1000)
    return {
        "runs": 15,
        "memory_p50_ms": round(statistics.median(memory_latencies), 3),
        "memory_p95_ms": round(percentile(memory_latencies, 95), 3),
        "document_p50_ms": round(statistics.median(document_latencies), 3),
        "document_p95_ms": round(percentile(document_latencies, 95), 3),
    }


def guarded(label: str, fn: Callable[[], Any]) -> dict[str, Any]:
    started = time.perf_counter()
    try:
        detail = fn()
        return {"name": label, "ok": True, "elapsed_ms": round((time.perf_counter() - started) * 1000, 3), "detail": detail}
    except Exception as exc:
        return {
            "name": label,
            "ok": False,
            "elapsed_ms": round((time.perf_counter() - started) * 1000, 3),
            "error": {"type": type(exc).__name__, "message": str(exc)[:1000]},
        }


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
    printing_press = parse_printing_press_version(os.environ.get("PRINTING_PRESS_VERSION_JSON", ""))
    event("SETUP", f"turingdb={__version__}; image={image}; scratch={scratch}")

    checks: list[dict[str, Any]] = []
    steps: list[dict[str, Any]] = []
    server = None
    logs_before_restart = ""
    logs_after_restart = ""
    cleanup: dict[str, Any] = {}

    add_check(checks, "cli_printing_press_installed_4_28_or_newer", bool(printing_press.get("ok")), printing_press)

    try:
        server = start_local_server(data_dir, free_port())
        step = guarded("seed_turingdb_graph_and_vectors", lambda: create_graph(client_for_local(server), data_dir))
        steps.append(step)
        add_check(checks, "turingdb_graph_and_vector_indexes_created", step["ok"], step)

        client = client_for_local(server)
        client.set_graph(GRAPH)

        alice_memory_edges = records(
            client.query('MATCH (:User {identifier: "alice"})-[:HAS_MEMORY]->(m:Memory) RETURN m.id, m.kind')
        )
        bob_memory_edges = records(
            client.query('MATCH (:User {identifier: "bob"})-[:HAS_MEMORY]->(m:Memory) RETURN m.id, m.kind')
        )
        add_check(
            checks,
            "agent_memory_scoped_graph_written",
            len(alice_memory_edges) == 3 and len(bob_memory_edges) == 1,
            {"alice": alice_memory_edges, "bob": bob_memory_edges},
        )

        alice_memory = search_memory(client, "alice", "espresso agent memory")
        add_check(
            checks,
            "agent_memory_alice_retrieves_exact_top1",
            bool(alice_memory) and alice_memory[0].get("m.id") == "mem-alice-espresso",
            {"query": "espresso agent memory", "rows": alice_memory},
        )

        bob_memory = search_memory(client, "bob", "espresso agent memory")
        no_bob_in_alice = all(row.get("m.user_id") == "alice" for row in alice_memory)
        bob_gets_bob = bool(bob_memory) and bob_memory[0].get("m.id") == "mem-bob-espresso"
        add_check(
            checks,
            "agent_memory_identity_scope_isolated",
            no_bob_in_alice and bob_gets_bob,
            {"alice_rows": alice_memory, "bob_rows": bob_memory},
        )

        doc_chunks = records(
            client.query('MATCH (:Document {id: "doc-machine-safety"})-[:HAS_CHUNK]->(c:Chunk) RETURN c.chunk_id, c.ordinal')
        )
        next_edges = records(
            client.query('MATCH (a:Chunk)-[:NEXT_CHUNK]->(b:Chunk) WHERE a.document_id = "doc-machine-safety" RETURN a.chunk_id, b.chunk_id')
        )
        add_check(
            checks,
            "document_ingest_graph_and_chunk_chain_written",
            len(doc_chunks) == 3 and len(next_edges) == 2,
            {"chunks": doc_chunks, "next_edges": next_edges},
        )

        doc_hits = search_documents(client, "alice", "reset safety guard interlock")
        add_check(
            checks,
            "document_retrieval_exact_top1",
            bool(doc_hits) and doc_hits[0].get("c.chunk_id") == "doc-machine-safety#1",
            {"query": "reset safety guard interlock", "rows": doc_hits},
        )

        context_rows = next_chunk_context(client, 2001)
        add_check(
            checks,
            "document_retrieval_expands_next_chunk_context",
            bool(context_rows) and context_rows[0].get("n.chunk_id") == "doc-machine-safety#2",
            {"winner_vector_id": 2001, "context": context_rows},
        )

        latency = run_latency_sample(client)
        add_check(
            checks,
            "memory_and_document_retrieval_p95_under_250ms",
            latency["memory_p95_ms"] <= 250.0 and latency["document_p95_ms"] <= 250.0,
            latency,
        )

        logs_before_restart = read_tail(server.log_path)
        stop_detail = stop_local_server(server)
        server = start_local_server(data_dir, free_port())
        restarted = client_for_local(server)
        restarted.load_graph(GRAPH, raise_if_loaded=False)
        restarted.set_graph(GRAPH)
        restart_memory = search_memory(restarted, "alice", "espresso agent memory")
        restart_docs = search_documents(restarted, "alice", "reset safety guard interlock")
        add_check(
            checks,
            "restart_durability_preserves_memory_and_document_vectors",
            bool(restart_memory)
            and restart_memory[0].get("m.id") == "mem-alice-espresso"
            and bool(restart_docs)
            and restart_docs[0].get("c.chunk_id") == "doc-machine-safety#1",
            {"stop": stop_detail, "memory": restart_memory, "documents": restart_docs},
        )
        logs_after_restart = read_tail(server.log_path)
    except Exception as exc:
        steps.append(
            {
                "name": "unhandled_probe_error",
                "ok": False,
                "error": {"type": type(exc).__name__, "message": str(exc)[:2000]},
            }
        )
        event("FAIL", f"unhandled_probe_error: {type(exc).__name__}: {exc}")
    finally:
        cleanup = stop_local_server(server)

    total_points = sum(float(item["points"]) for item in checks)
    earned_points = sum(float(item["points"]) for item in checks if item.get("ok"))
    score = round((earned_points / total_points) * 10.0, 3) if total_points else 0.0
    verdict = "VALIDATED_9_8" if score >= 9.8 else "FAILED_SCORE_GATE"
    payload = {
        "id": "094-turingdb-memory-doc-e2e",
        "timestamp": iso_now(),
        "client_package_version": __version__,
        "container_image": image,
        "printing_press": printing_press,
        "scratch": str(scratch),
        "score": score,
        "score_gate": ">=9.8/10",
        "verdict": verdict,
        "checks": checks,
        "steps": steps,
        "logs_before_restart_tail": logs_before_restart,
        "logs_after_restart_tail": logs_after_restart,
        "cleanup": cleanup,
    }
    out.write_text(json.dumps(payload, indent=2, sort_keys=True), encoding="utf-8")
    event("SUMMARY", f"verdict={verdict}; score={score}; results={out}")
    return 0 if verdict == "VALIDATED_9_8" else 1


if __name__ == "__main__":
    raise SystemExit(main())
