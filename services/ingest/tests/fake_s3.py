"""A stand-in for the bucket listing the walker and the audit read, shared by their tests.

`install` answers `pages` to the paginator source.py asks its S3 session for; `config` is the
bucket binding the listing is read under.
"""

from ingest import source


class _FakePaginator:
    def __init__(self, pages):
        self._pages = pages

    def paginate(self, **_kwargs):
        return self._pages


class _FakeS3:
    def __init__(self, pages):
        self._pages = pages

    def get_paginator(self, _name):
        return _FakePaginator(self._pages)


def config(prefix=""):
    return source.S3Config(
        identity_id="11111111-1111-1111-1111-111111111111",
        endpoint="http://garage:3900", bucket="aura-bench",
        access_key="k", secret_key="s", region="garage", prefix=prefix,
    )


def install(monkeypatch, pages):
    client = _FakeS3(pages)
    monkeypatch.setattr(
        "ingest.source.sync_get_session", lambda: type("S", (), {"create_client": lambda *a, **k: client})()
    )
