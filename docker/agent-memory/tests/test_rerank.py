"""Unit tests for the fail-soft aura-rerank client.

Mirrors the Go ``internal/rerank/client_test.go`` contract: success reorders by
descending relevance; every failure mode (no base_url, non-2xx, malformed JSON,
length mismatch, out-of-range index) returns the input (identity) order. A local
threaded HTTP server stands in for the GPU sidecar (the Python analogue of Go's
httptest), so no live aura-rerank / GPU is required.
"""

from __future__ import annotations

import json
import threading
from http.server import BaseHTTPRequestHandler, HTTPServer

import pytest

from neo4j_agent_memory.rerank import (
    capture_rerank_evidence,
    rerank,
    summarize_rerank_evidence,
)


class _RerankHandler(BaseHTTPRequestHandler):
    """Canned /v1/rerank responder; status/body are set per test via _set_response.

    It also records the last request (parsed body + headers) so the cloud-path tests can
    assert what actually went ON THE WIRE — a model name and an Authorization header are
    only worth anything if the provider receives them.
    """

    status = 200
    body = b'{"results": []}'
    last_body: dict | None = None
    last_headers: dict | None = None

    def do_POST(self):  # noqa: N802 - BaseHTTPRequestHandler API
        length = int(self.headers.get("Content-Length", 0))
        raw = self.rfile.read(length)
        _RerankHandler.last_body = json.loads(raw) if raw else None
        _RerankHandler.last_headers = dict(self.headers)
        self.send_response(self.status)
        self.send_header("Content-Type", "application/json")
        self.end_headers()
        self.wfile.write(self.body)

    def log_message(self, *_args):  # silence the test server
        pass


def _set_response(status: int, body) -> None:
    _RerankHandler.status = status
    _RerankHandler.body = body if isinstance(body, bytes) else json.dumps(body).encode()
    _RerankHandler.last_body = None
    _RerankHandler.last_headers = None


def _ok_three() -> None:
    _set_response(
        200,
        {
            "results": [
                {"index": 0, "relevance_score": 0.10},
                {"index": 1, "relevance_score": 0.90},
                {"index": 2, "relevance_score": 0.50},
            ]
        },
    )


@pytest.fixture
def rerank_server():
    httpd = HTTPServer(("127.0.0.1", 0), _RerankHandler)
    thread = threading.Thread(target=httpd.serve_forever, daemon=True)
    thread.start()
    try:
        yield f"http://127.0.0.1:{httpd.server_address[1]}"
    finally:
        httpd.shutdown()


def test_rerank_reorders_by_score_descending(rerank_server):
    _set_response(
        200,
        {
            "results": [
                {"index": 0, "relevance_score": 0.10},
                {"index": 1, "relevance_score": 0.90},
                {"index": 2, "relevance_score": 0.50},
            ]
        },
    )
    assert rerank("q", ["a", "b", "c"], rerank_server) == [1, 2, 0]


def test_rerank_no_base_url_returns_identity():
    assert rerank("q", ["a", "b", "c"], "") == [0, 1, 2]


def test_rerank_http_503_returns_identity(rerank_server):
    _set_response(503, b'{"error": "unavailable"}')
    assert rerank("q", ["a", "b", "c"], rerank_server) == [0, 1, 2]


def test_rerank_malformed_json_returns_identity(rerank_server):
    _set_response(200, b"not json at all")
    assert rerank("q", ["a", "b", "c"], rerank_server) == [0, 1, 2]


def test_rerank_length_mismatch_returns_identity(rerank_server):
    _set_response(200, {"results": [{"index": 0, "relevance_score": 0.9}]})
    assert rerank("q", ["a", "b", "c"], rerank_server) == [0, 1, 2]


def test_rerank_out_of_range_index_returns_identity(rerank_server):
    _set_response(
        200,
        {
            "results": [
                {"index": 0, "relevance_score": 0.1},
                {"index": 9, "relevance_score": 0.9},
                {"index": 2, "relevance_score": 0.5},
            ]
        },
    )
    assert rerank("q", ["a", "b", "c"], rerank_server) == [0, 1, 2]


def test_rerank_empty_docs_returns_empty():
    assert rerank("q", [], "http://127.0.0.1:1") == []


def test_rerank_defaults_to_local_sidecar_unauthenticated(rerank_server, monkeypatch):
    """Unconfigured = the local sidecar contract, byte-for-byte: model "aura-rerank",
    no Authorization. An empty bearer would make the sidecar reject what it accepts."""
    monkeypatch.delenv("AURA_RERANK_MODEL", raising=False)
    monkeypatch.delenv("AURA_RERANK_API_KEY", raising=False)
    _ok_three()
    assert rerank("q", ["a", "b", "c"], rerank_server) == [1, 2, 0]
    assert _RerankHandler.last_body["model"] == "aura-rerank"
    assert "Authorization" not in _RerankHandler.last_headers


def test_rerank_cloud_model_and_key_reach_the_wire(rerank_server):
    """A hosted cross-encoder needs BOTH: the provider picks the model by name and
    rejects the call without a bearer. Explicit args win over the environment."""
    _ok_three()
    order = rerank(
        "q", ["a", "b", "c"], rerank_server, model="cohere/rerank-4-pro", api_key="sk-test-rr"
    )
    assert order == [1, 2, 0]
    assert _RerankHandler.last_body["model"] == "cohere/rerank-4-pro"
    assert _RerankHandler.last_headers["Authorization"] == "Bearer sk-test-rr"


def test_rerank_reads_model_and_key_from_env(rerank_server, monkeypatch):
    """compose sets the env and nobody threads arguments through the recall path, so the
    env fallback is the configuration that actually ships."""
    monkeypatch.setenv("AURA_RERANK_MODEL", "cohere/rerank-4-pro")
    monkeypatch.setenv("AURA_RERANK_API_KEY", "sk-env-rr")
    _ok_three()
    assert rerank("q", ["a", "b", "c"], rerank_server) == [1, 2, 0]
    assert _RerankHandler.last_body["model"] == "cohere/rerank-4-pro"
    assert _RerankHandler.last_headers["Authorization"] == "Bearer sk-env-rr"


def test_rerank_blank_env_falls_back_to_sidecar_defaults(rerank_server, monkeypatch):
    """compose renders an unset ${VAR:-} as an EMPTY string, not an absent variable —
    so blank must mean "unconfigured", or the base compose defaults would ship a
    model-less request and an empty bearer to the local sidecar."""
    monkeypatch.setenv("AURA_RERANK_MODEL", "")
    monkeypatch.setenv("AURA_RERANK_API_KEY", "")
    _ok_three()
    assert rerank("q", ["a", "b", "c"], rerank_server) == [1, 2, 0]
    assert _RerankHandler.last_body["model"] == "aura-rerank"
    assert "Authorization" not in _RerankHandler.last_headers


def test_rerank_evidence_reports_applied_disabled_and_degraded(rerank_server):
    _ok_three()
    with capture_rerank_evidence() as applied:
        rerank("q", ["a", "b", "c"], rerank_server, model="fixture-reranker")
    assert summarize_rerank_evidence(applied) == ("fixture-reranker", "applied")

    with capture_rerank_evidence() as disabled:
        rerank("q", ["a", "b"], "", model="fixture-reranker")
    assert summarize_rerank_evidence(disabled) == ("fixture-reranker", "disabled")

    _set_response(503, b'{"error": "unavailable"}')
    with capture_rerank_evidence() as degraded:
        rerank("q", ["a", "b"], rerank_server, model="fixture-reranker")
    assert summarize_rerank_evidence(degraded) == ("fixture-reranker", "degraded")
