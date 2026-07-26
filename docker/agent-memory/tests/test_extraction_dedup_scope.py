"""An extracted entity and a hand-recorded one must be the same node.

The entity MERGE key is ``{name, type, deduplication_scope}``. Message extraction
hardcoded ``"global"`` while ``LongTermMemory.add_entity`` derives a provenance scope from
the owner, so the two write paths could never converge: measured on the live graph
2026-07-26, two ``Davide`` PERSON nodes, identical except for the scope — one
``{"user_identifier":"e3c8…"}`` written by the agent, one ``global`` written by extraction
— un-mergeable by construction no matter how good deduplication got.

The fix is not a matching constant; it is a single shared derivation. A test that pinned a
literal ``'{"user_identifier":"..."}'`` on each side would pass while they drifted again,
so the assertion below compares the two producers against each other.
"""

from __future__ import annotations

from neo4j_agent_memory.extraction.base import ExtractedEntity, ExtractionResult
from neo4j_agent_memory.memory.long_term import _deduplication_scope, _metadata_with_user_scope
from neo4j_agent_memory.memory.short_term import (
    Message,
    MessageRole,
    ShortTermMemory,
    _extraction_scope,
)

OWNER = "e3c8eb3b-7f0c-4bbc-bd6c-3311f9623c3e"


class RecordingClient:
    def __init__(self):
        self.writes: list[tuple[str, dict]] = []

    async def execute_read(self, query, params=None):
        return []

    async def execute_write(self, query, params=None):
        self.writes.append((query, params or {}))
        return []


class FakeExtractor:
    async def extract(self, content):
        return ExtractionResult(
            entities=[ExtractedEntity(name="Davide", type="PERSON", confidence=0.9)],
            relations=[],
            source_text=content,
        )


def test_extraction_scope_is_the_same_derivation_the_agent_path_uses():
    """Compare the two producers, not a literal — a literal cannot catch drift."""
    assert _extraction_scope(OWNER) == _deduplication_scope(
        _metadata_with_user_scope(None, OWNER)
    )


def test_extraction_scope_falls_back_to_global_without_an_owner():
    """Never None: the scope is part of the MERGE key and Neo4j refuses to merge on null.

    Returning the derivation's bare None here made every ownerless store_message fail with
    "Cannot merge the following node because of null property value for
    'deduplication_scope'" — the CLI path broke outright.
    """
    assert _extraction_scope(None) == "global"


async def test_extracted_entity_carries_the_owner_scope_not_global():
    """The write that produced the duplicate Davide node."""
    client = RecordingClient()
    memory = ShortTermMemory(client, None, FakeExtractor())
    message = Message(role=MessageRole.USER, content="Davide vive a Caraglio")

    await memory._extract_and_link_entities(
        message, extract_relations=False, user_identifier=OWNER
    )

    scopes = [
        params["deduplication_scope"]
        for _query, params in client.writes
        if "deduplication_scope" in params
    ]
    assert scopes, "extraction wrote no entity"
    assert all(scope == _extraction_scope(OWNER) for scope in scopes)
    assert "global" not in scopes
