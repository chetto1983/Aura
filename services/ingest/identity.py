"""Search-document identity, mirroring internal/documents/ids.go's SearchDocumentID.

The Go ingestion path and this Python sidecar must mint byte-identical ids for
the same (identity, source kind, source key) triple, or a passage minted here
becomes unresolvable by the Go-side document_open tool. Any drift in trimming,
casing, or hash framing silently breaks find-then-open.
"""

import hashlib

_DOMAIN = "aura.document.search.v1"


def search_document_id(identity_id: str, source_kind: str, source_key: str) -> str:
    """Derive the id a retrieved passage carries, scoped by identity and source."""
    identity_id = identity_id.strip()
    source_kind = source_kind.strip().lower()
    source_key = source_key.strip()
    if not identity_id or not source_kind or not source_key:
        raise ValueError(
            "document identity, source kind, and source key are required"
        )

    digest = hashlib.sha256()
    # Each part is null-PREFIXED (0x00 written before the part's UTF-8 bytes),
    # not null-separated: Go writes the 0x00 unconditionally ahead of every
    # part, including the first. Null-separating (0x00 only between parts)
    # produces a different digest and breaks parity with ids.go lines 92-96.
    for part in (_DOMAIN, identity_id, source_kind, source_key):
        digest.update(b"\x00")
        digest.update(part.encode("utf-8"))

    return "doc_" + digest.hexdigest()[:32]
