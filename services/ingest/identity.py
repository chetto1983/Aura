"""Search-document identity, mirroring internal/documents/ids.go's SearchDocumentID.

The Go ingestion path and this Python sidecar must mint byte-identical ids for
the same (identity, source kind, source key) triple, or a passage minted here
becomes unresolvable by the Go-side document_open tool. Any drift in trimming,
casing, or hash framing silently breaks find-then-open.
"""

import hashlib
import uuid as _uuid

_DOMAIN = "aura.document.search.v1"


def database_for(identity_id: str) -> str:
    """The ArcadeDB database holding one identity's passages.

    Mirrors internal/arcadedb/tenant.go's DatabaseFor. This is NOT cosmetic and NOT
    configurable: Aura's Go retriever opens `mem_<uuid>` for the identity it is
    answering for, so passages written anywhere else are invisible to it no matter how
    correct their contents are. Writing them to a single shared database is exactly how
    an ingestion run can look completely successful while retrieval returns nothing.

    Fails closed on a missing or malformed identity, like the Go original: an empty
    identity used to mean "the shared database", which is the hole that closes.
    """
    identity_id = identity_id.strip()
    if not identity_id:
        raise ValueError("identity_id is required: passages are per identity")
    try:
        parsed = _uuid.UUID(identity_id)
    except ValueError as exc:
        raise ValueError(f"identity_id {identity_id!r} is not an identity: {exc}") from exc
    # str(UUID) is canonical lowercase 8-4-4-4-12, so the mapping is total and injective.
    return "mem_" + str(parsed).replace("-", "_")


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
