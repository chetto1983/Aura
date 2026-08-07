"""Cross-language parity tests for search_document_id.

Vectors below were generated from internal/documents/ids.go's SearchDocumentID
on this machine — they are the ground truth, not something to recompute here.
"""

import pytest

from services.ingest.identity import search_document_id

VECTORS = [
    (
        "00000000-0000-0000-0000-000000000001",
        "s3",
        "bucket/a.pdf",
        "doc_cc9a5e20001236dcb53ed1fa784f4551",
    ),
    (
        # kind is case-insensitive: "S3" must fold to the same id as "s3"
        "00000000-0000-0000-0000-000000000001",
        "S3",
        "bucket/a.pdf",
        "doc_cc9a5e20001236dcb53ed1fa784f4551",
    ),
    (
        # key changes the id
        "00000000-0000-0000-0000-000000000001",
        "s3",
        "bucket/b.pdf",
        "doc_1e3cde4ba12e12c9136754e4aeb59fc0",
    ),
    (
        # identity scopes the id
        "00000000-0000-0000-0000-000000000002",
        "s3",
        "bucket/a.pdf",
        "doc_ce1b40d622ac03eaab5c1131fa4064b1",
    ),
    (
        # all three inputs are trimmed of surrounding whitespace
        "  id-with-spaces  ",
        "  S3  ",
        "  bucket/c.pdf  ",
        "doc_18ad2d4ff136a7d374836a4d1497ed3f",
    ),
]


@pytest.mark.parametrize("identity_id,source_kind,source_key,expected", VECTORS)
def test_matches_go_reference_vector(identity_id, source_kind, source_key, expected):
    assert search_document_id(identity_id, source_kind, source_key) == expected


def test_id_has_doc_prefix_and_36_chars():
    result = search_document_id("identity", "s3", "key")
    assert result.startswith("doc_")
    assert len(result) == 36


@pytest.mark.parametrize(
    "identity_id,source_kind,source_key",
    [
        ("", "s3", "bucket/a.pdf"),
        ("   ", "s3", "bucket/a.pdf"),
        ("identity", "", "bucket/a.pdf"),
        ("identity", "   ", "bucket/a.pdf"),
        ("identity", "s3", ""),
        ("identity", "s3", "   "),
    ],
)
def test_empty_field_after_trim_raises(identity_id, source_kind, source_key):
    with pytest.raises(ValueError):
        search_document_id(identity_id, source_kind, source_key)
