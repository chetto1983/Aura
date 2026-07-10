from __future__ import annotations

import argparse
import json
import math
import os
import random
import shutil
import statistics
import sys
import time
from datetime import datetime, timezone
from pathlib import Path
from typing import Any

from turingdb import __version__

sys.path.insert(0, str(Path(__file__).resolve().parents[1]))
from _turingdb_docker import client_for_local, free_port, read_tail, start_local_server, stop_local_server  # noqa: E402


DIM = 384
DOCS = 256
QUERY_ID = 42
K = 5


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
    return [x / norm for x in vec]


def make_vectors() -> list[list[float]]:
    rng = random.Random(920092)
    vectors: list[list[float]] = []
    centers: list[list[float]] = []
    for cluster in range(4):
        base = [rng.gauss(0.0, 1.0) for _ in range(DIM)]
        base[cluster] += 4.0
        centers.append(normalize(base))
    for idx in range(DOCS):
        center = centers[idx % len(centers)]
        vec = [center[j] + rng.gauss(0.0, 0.06) for j in range(DIM)]
        vectors.append(normalize(vec))
    return vectors


def cosine(a: list[float], b: list[float]) -> float:
    return sum(x * y for x, y in zip(a, b))


def vector_literal(vec: list[float]) -> str:
    return "(" + ", ".join(f"{x:.8f}" for x in vec) + ")"


def seed_docs(client) -> None:
    client.create_graph("aura092")
    client.set_graph("aura092")
    client.new_change()
    for start in range(0, DOCS, 32):
        parts = []
        for idx in range(start, min(start + 32, DOCS)):
            cluster = idx % 4
            parts.append(f'(d{idx}:Document {{id: {idx}, title: "Doc {idx}", cluster: "cluster-{cluster}"}})')
        client.query("CREATE " + ", ".join(parts))
    client.query("CHANGE SUBMIT")
    client.checkout()


def write_vector_csv(path: Path, vectors: list[list[float]]) -> None:
    with path.open("w", encoding="utf-8", newline="\n") as handle:
        for idx, vec in enumerate(vectors):
            handle.write(str(idx))
            handle.write(",")
            handle.write(",".join(f"{x:.8f}" for x in vec))
            handle.write("\n")


def percentile(values: list[float], pct: float) -> float:
    if not values:
        return 0.0
    ordered = sorted(values)
    index = min(len(ordered) - 1, max(0, math.ceil((pct / 100.0) * len(ordered)) - 1))
    return ordered[index]


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
    data_subdir = data_dir / "data"
    data_subdir.mkdir(parents=True, exist_ok=True)
    image = os.environ.get("TURINGDB_CONTAINER_IMAGE", "aura-turingdb-spike:1.35")
    event("SETUP", f"client package {__version__}; container_image={image}; docs={DOCS}; dim={DIM}; data_dir={data_dir}")

    server = None
    payload: dict[str, Any]
    try:
        server = start_local_server(data_dir, free_port())
        client = client_for_local(server)
        vectors = make_vectors()
        query = vectors[QUERY_ID]
        expected = sorted(((idx, cosine(query, vec)) for idx, vec in enumerate(vectors)), key=lambda item: item[1], reverse=True)[:K]
        expected_ids = [idx for idx, _ in expected]

        timings: dict[str, float] = {}
        started = time.perf_counter()
        seed_docs(client)
        timings["seed_docs_ms"] = round((time.perf_counter() - started) * 1000, 3)
        event("PASS", f"seeded {DOCS} Document nodes")

        vector_csv = data_subdir / "doc_vectors.csv"
        write_vector_csv(vector_csv, vectors)
        started = time.perf_counter()
        client.query(f"CREATE VECTOR INDEX doc_vectors WITH DIMENSION {DIM} METRIC COSINE")
        client.query('LOAD VECTOR FROM "doc_vectors.csv" IN doc_vectors')
        timings["index_load_ms"] = round((time.perf_counter() - started) * 1000, 3)
        event("PASS", f"loaded {DOCS} vectors")

        literal = vector_literal(query)
        standalone_query = f"VECTOR SEARCH IN doc_vectors FOR {K} {literal} YIELD ids, score RETURN ids, score"
        composed_query = f"VECTOR SEARCH IN doc_vectors FOR {K} {literal} YIELD ids MATCH (d:Document) WHERE d.id = ids RETURN d.id, d.title, d.cluster"
        composed_with_score_query = f"VECTOR SEARCH IN doc_vectors FOR {K} {literal} YIELD ids, score MATCH (d:Document) WHERE d.id = ids RETURN d.id, d.title, d.cluster, score"

        standalone_rows = records(client.query(standalone_query))
        actual_ids = [int(row["ids"]) for row in standalone_rows]
        overlap = len(set(actual_ids) & set(expected_ids)) / K
        top1_ok = bool(actual_ids and actual_ids[0] == QUERY_ID)
        event("RESULT", f"standalone ids={actual_ids}; expected={expected_ids}; overlap@{K}={overlap:.2f}")

        composed_rows = records(client.query(composed_query))
        score_rows: list[dict[str, Any]] = []
        score_error = None
        try:
            score_rows = records(client.query(composed_with_score_query))
        except Exception as exc:
            score_error = {"type": type(exc).__name__, "message": str(exc).splitlines()[0][:500]}
            event("FAIL", f"composed query with score retention failed: {score_error['message']}")

        latencies: list[float] = []
        for _ in range(25):
            started = time.perf_counter()
            client.query(standalone_query)
            latencies.append((time.perf_counter() - started) * 1000)

        latency_summary = {
            "runs": len(latencies),
            "p50_ms": round(statistics.median(latencies), 3),
            "p95_ms": round(percentile(latencies, 95), 3),
            "min_ms": round(min(latencies), 3),
            "max_ms": round(max(latencies), 3),
        }
        verdict = "VALIDATED" if top1_ok and overlap >= 0.8 and len(composed_rows) == K else "PARTIAL"
        payload = {
            "id": "092-turingdb-vector-graphrag-parity",
            "timestamp": iso_now(),
            "client_package_version": __version__,
            "container_image": image,
            "scratch": str(scratch),
            "dimension": DIM,
            "documents": DOCS,
            "k": K,
            "query_id": QUERY_ID,
            "expected_ids": expected_ids,
            "actual_ids": actual_ids,
            "overlap_at_k": overlap,
            "top1_ok": top1_ok,
            "standalone_rows": standalone_rows,
            "composed_rows": composed_rows,
            "composed_with_score_rows": score_rows,
            "composed_with_score_error": score_error,
            "latency": latency_summary,
            "timings": timings,
            "daemon_logs_tail": read_tail(server.log_path),
            "verdict": verdict,
        }
    except Exception as exc:
        event("FAIL", f"vector probe: {type(exc).__name__}: {exc}")
        payload = {
            "id": "092-turingdb-vector-graphrag-parity",
            "timestamp": iso_now(),
            "client_package_version": __version__,
            "container_image": image,
            "scratch": str(scratch),
            "verdict": "INVALIDATED",
            "error": {"type": type(exc).__name__, "message": str(exc)},
            "daemon_logs_tail": read_tail(server.log_path) if server else "",
        }
    finally:
        payload["cleanup"] = stop_local_server(server)

    out.write_text(json.dumps(payload, indent=2, sort_keys=True), encoding="utf-8")
    event("SUMMARY", f"verdict={payload['verdict']}; results={out}")
    return 0 if payload["verdict"] in {"VALIDATED", "PARTIAL"} else 1


if __name__ == "__main__":
    raise SystemExit(main())
