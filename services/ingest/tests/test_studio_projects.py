"""A Video Studio project is never a document.

The Studio saves its editor state as `<slug>.aura-video.json`; the object key keeps that suffix
whole (Go objectstore.StudioProjectSuffix). The key is all the walker's matcher sees -- the real
filename rides in metadata only a HEAD reads -- so the suffix in the key is what keeps a project
out of the index, while any other `.json` the operator uploads is still a document.

These run CocoIndex's own PatternFilePathMatcher (globset semantics), so `**/` at the bucket
root is measured here rather than assumed.
"""

import pathlib

import pytest

from ingest import source
from ingest.tests import fake_s3


@pytest.mark.parametrize(
    "key",
    [
        "chat/019f8a2b-0000-7000-8000-000000000001.aura-video.json",
        "reel.aura-video.json",  # the bucket root: `**/` matches zero folders too
        "progetti/2026/reel.aura-video.json",
    ],
)
def test_a_studio_project_is_not_offered_to_the_pipeline(key):
    assert not source.path_matcher().is_file_included(pathlib.PurePosixPath(key))


@pytest.mark.parametrize(
    "key",
    [
        "chat/019f8a2b-0000-7000-8000-000000000002.json",
        "dati/listino.json",
        "reel.aura-video.json.bak",  # the marker must END the name
        "reel-aura-video.json",
    ],
)
def test_any_other_json_is_still_a_document(key):
    assert source.path_matcher().is_file_included(pathlib.PurePosixPath(key))


def test_the_reserved_prefixes_stay_excluded():
    matcher = source.path_matcher()
    assert not matcher.is_file_included(pathlib.PurePosixPath("identity/abcd/original"))
    assert not matcher.is_file_included(pathlib.PurePosixPath("share/token/thing.pdf"))


def test_the_walker_filters_with_the_same_matcher(monkeypatch):
    seen = {}

    def list_objects(client, bucket, *, prefix, path_matcher):
        seen["matcher"] = path_matcher
        return iter(())

    monkeypatch.setattr(source.amazon_s3, "list_objects", list_objects)
    source.walk(object(), fake_s3.config())

    assert not seen["matcher"].is_file_included(pathlib.PurePosixPath("chat/x.aura-video.json"))
    assert seen["matcher"].is_file_included(pathlib.PurePosixPath("chat/x.json"))


def test_the_audit_does_not_expect_a_project(monkeypatch):
    # The audit compares the bucket with the index: expecting a row for a project it never
    # indexes would print a MISSING line on every cycle that can never clear.
    fake_s3.install(monkeypatch, [{"Contents": [
        {"Key": "chat/a.aura-video.json"},
        {"Key": "chat/b.json"},
    ]}])

    assert source.expected_keys(fake_s3.config()) == {"chat/b.json"}
