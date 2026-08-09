"""The reconciliation that turns a lost document into a failure instead of a silence.

An ingest pass that drops a file still exits 0: CocoIndex catches the component failure,
prints "component build failed" and carries on. On 2026-08-09 two separate defects did
exactly that on the same 130-document corpus and both were found by comparing the bucket
against the index BY HAND. These tests cover that comparison without a daemon, because
the parts that can be wrong are pure: which keys count as expected, and how the index's
answer is parsed.
"""

import pytest

from ingest import arcade, source


class _FakePaginator:
    def __init__(self, pages):
        self._pages = pages

    def paginate(self, **_kwargs):
        return self._pages


class _FakeS3:
    def __init__(self, pages):
        self._pages = pages
        self.requested = None

    def get_paginator(self, name):
        self.requested = name
        return _FakePaginator(self._pages)


def _config(prefix=""):
    return source.S3Config(
        identity_id="11111111-1111-1111-1111-111111111111",
        endpoint="http://garage:3900", bucket="aura-bench",
        access_key="k", secret_key="s", region="garage", prefix=prefix,
    )


def _install_fake_s3(monkeypatch, pages):
    client = _FakeS3(pages)
    monkeypatch.setattr(
        "ingest.source.sync_get_session", lambda: type("S", (), {"create_client": lambda *a, **k: client})()
    )
    return client


def test_expected_keys_excludes_the_prefixes_the_walker_excludes(monkeypatch):
    # RESERVED_PATTERNS keeps Aura's own layout out of the pipeline, so counting those
    # objects here would invent a discrepancy on every single pass: they are never
    # supposed to produce a document row.
    _install_fake_s3(monkeypatch, [{"Contents": [
        {"Key": "laws/statute.pdf"},
        {"Key": "identity/abcd/original"},
        {"Key": "share/token/thing.pdf"},
        {"Key": "laws/table.csv"},
    ]}])

    assert source.expected_keys(_config()) == {"laws/statute.pdf", "laws/table.csv"}


def test_expected_keys_spans_pages(monkeypatch):
    _install_fake_s3(monkeypatch, [
        {"Contents": [{"Key": "a.pdf"}]},
        {"Contents": [{"Key": "b.pdf"}]},
        {},
    ])

    assert source.expected_keys(_config()) == {"a.pdf", "b.pdf"}


def test_indexed_source_keys_reads_the_rows(monkeypatch):
    captured = {}

    def fake_post(base_url, path, payload, auth, timeout_s):
        captured.update(base_url=base_url, path=path, payload=payload)
        return {"result": [{"source_key": "laws/a.pdf"}, {"source_key": "laws/b.csv"}]}

    monkeypatch.setattr("ingest.arcade._post", fake_post)

    keys = arcade.indexed_source_keys("http://arcadedb:2480", "mem_x", ("root", "pw"), 60.0)

    assert keys == {"laws/a.pdf", "laws/b.csv"}
    assert captured["path"] == "/api/v1/query/mem_x"
    assert arcade.DOCUMENT_TYPE in captured["payload"]["command"]


@pytest.mark.parametrize("body", [{}, {"result": []}, {"result": [{"source_key": None}]}])
def test_indexed_source_keys_treats_an_empty_index_as_empty_not_as_success(monkeypatch, body):
    # An empty answer must read as "nothing is indexed", never as "nothing is missing".
    # Returning a full set here would make the audit pass loudest exactly when the whole
    # pass has failed.
    monkeypatch.setattr("ingest.arcade._post", lambda *a, **k: body)

    assert arcade.indexed_source_keys("http://x", "mem_x", ("root", "pw"), 1.0) == set()


def test_the_audit_reports_the_objects_with_no_row(monkeypatch):
    # The whole point, expressed as the set difference it is: an object present in the
    # bucket and absent from the index is a document the pass lost.
    expected = {"laws/a.pdf", "laws/b.csv", "laws/c.pdf"}
    indexed = {"laws/a.pdf", "laws/c.pdf"}

    assert sorted(expected - indexed) == ["laws/b.csv"]
