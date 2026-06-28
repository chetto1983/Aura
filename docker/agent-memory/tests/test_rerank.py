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

from neo4j_agent_memory.rerank import rerank


class _RerankHandler(BaseHTTPRequestHandler):
    """Canned /v1/rerank responder; status/body are set per test via _set_response."""

    status = 200
    body = b'{"results": []}'

    def do_POST(self):  # noqa: N802 - BaseHTTPRequestHandler API
        length = int(self.headers.get("Content-Length", 0))
        self.rfile.read(length)
        self.send_response(self.status)
        self.send_header("Content-Type", "application/json")
        self.end_headers()
        self.wfile.write(self.body)

    def log_message(self, *_args):  # silence the test server
        pass


def _set_response(status: int, body) -> None:
    _RerankHandler.status = status
    _RerankHandler.body = body if isinstance(body, bytes) else json.dumps(body).encode()


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
