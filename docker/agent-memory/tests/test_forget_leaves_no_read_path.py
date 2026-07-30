"""Forget must remove the entity from the caller's READS, not just from their ownership.

Observed live on 2026-07-25 (controprova round 5): `memory_forget` returned
`{"deleted": "3700…", "removed_node": false}` and the entity was still in the caller's scope —
`owned=0, mentioned=1, scope_count=1`. Every scoped read goes through ENTITY_IN_USER_SCOPE,
which accepts MENTIONS / APPLIES_TO / TOUCHED as well as ownership, and MENTIONS is how
extraction-born entities exist at all. So the user was told "deleted" about something their
next turn could read back out of search, get_entity, get_context and the recalled-context
block. These tests pin the two halves: forget cuts every scope-granting edge of the CALLER,
and cuts nobody else's.
"""

from __future__ import annotations

import asyncio
import re
from uuid import uuid4

from neo4j_agent_memory.graph import queries
from neo4j_agent_memory.memory.long_term import LongTermMemory

# Every relationship ENTITY_IN_USER_SCOPE accepts. If a future edge type is added there and
# not here, this list is the thing to update — and the test below will say so.
SCOPE_GRANTING = ("HAS_ENTITY", "MENTIONS", "APPLIES_TO", "TOUCHED")


def _run(coro):
    return asyncio.run(coro)


class _RecordingClient:
    def __init__(self, rows):
        self.writes: list[tuple[str, dict]] = []
        self._rows = rows

    async def execute_write(self, query, params=None):
        self.writes.append((query, params or {}))
        return self._rows


class _RelationshipClient(_RecordingClient):
    async def execute_read(self, _query, _params=None):
        return [{"scope_count": 1}]


def test_forget_cuts_every_edge_that_grants_the_caller_read_scope():
    """The rule stated as a set relation, so a new scope edge cannot be silently missed."""
    granted = {rel for rel in SCOPE_GRANTING if rel in queries.ENTITY_IN_USER_SCOPE}
    cut = {rel for rel in SCOPE_GRANTING if rel in queries.DELETE_ENTITY_SCOPED}

    assert granted, "ENTITY_IN_USER_SCOPE matched none of the known edges — update SCOPE_GRANTING"
    assert granted <= cut, (
        f"forget leaves {sorted(granted - cut)} intact, so the entity stays readable to the "
        "caller it was just 'deleted' for"
    )


def test_forget_only_ever_cuts_the_callers_own_edges():
    """Non-cascading is the other half: someone else's memory is not the caller's to delete."""
    query = queries.DELETE_ENTITY_SCOPED

    assert "DETACH DELETE" not in query, "DETACH DELETE would take every user's edges at once"
    for clause in re.findall(r"OPTIONAL MATCH \((.*?)\)-", query):
        assert "u" in clause or "e" in clause, (
            f"unrooted OPTIONAL MATCH {clause!r}: every deletion must start at the caller"
        )
    # The node itself survives whenever anything still points at it.
    assert "count(rel) AS remaining" in query
    assert "CASE WHEN remaining = 0" in query


def test_forget_does_not_multiply_rows_per_mention():
    """OPTIONAL MATCH fans out one row per matched edge; DISTINCT keeps the work flat."""
    assert queries.DELETE_ENTITY_SCOPED.count("WITH DISTINCT") >= len(SCOPE_GRANTING) - 1


def test_delete_entity_reports_none_when_nothing_matched():
    """An entity the caller does not own is a refusal, never a silent success."""
    memory = LongTermMemory(_RecordingClient([]))

    assert _run(memory.delete_entity(uuid4(), user_identifier="identity-1")) is None


def test_delete_entity_reports_whether_the_node_survived():
    entity_id = uuid4()
    client = _RecordingClient([{"deleted_id": str(entity_id), "removed_node": False}])

    result = _run(LongTermMemory(client).delete_entity(entity_id, user_identifier="identity-1"))

    assert result == (str(entity_id), False)
    query, params = client.writes[0]
    assert query == queries.DELETE_ENTITY_SCOPED
    assert params == {"entity_id": str(entity_id), "user_identifier": "identity-1"}


def test_delete_relationship_matches_the_public_semantic_type():
    source_id = uuid4()
    target_id = uuid4()
    client = _RelationshipClient([{"removed": 1}])

    removed = _run(
        LongTermMemory(client).delete_relationship(
            source_id,
            target_id,
            "VISITED",
            user_identifier="identity-1",
        )
    )

    assert removed == 1
    query, params = client.writes[0]
    assert "type(r) = $relationship_type" in query
    assert "coalesce(r.type, r.relation_type)" in query
    assert params == {
        "source_id": str(source_id),
        "target_id": str(target_id),
        "relationship_type": "VISITED",
    }
