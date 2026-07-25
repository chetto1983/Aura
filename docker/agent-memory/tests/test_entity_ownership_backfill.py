"""Cover the retroactive half of the ownership fix shipped in `a79a8df43`.

That commit made extraction write `(:User)-[:HAS_ENTITY]->(:Entity)` at the point entities
are created — the right place, because the alternative on the table was widening
`DELETE_ENTITY_SCOPED`'s guard, which would have loosened a destructive verb to paper over a
gap in the write path. But a write-path fix does nothing for rows already written: on the live
graph the operator's two "David" duplicates still had no owner, so `memory_forget` kept
answering "not found or not owned by this user" about their own data (controprova round 3,
2026-07-25 — three of four criteria passed, and this was the fourth).
"""

from __future__ import annotations

import asyncio
import re

from neo4j_agent_memory.graph import queries
from neo4j_agent_memory.memory.long_term import LongTermMemory


def _run(coro):
    return asyncio.run(coro)


class _RecordingClient:
    """Records writes so a test can assert WHICH query ran, without a live Neo4j."""

    def __init__(self, rows=None):
        self.writes: list[tuple[str, dict]] = []
        self._rows = rows if rows is not None else [{"linked": 0}]

    async def execute_write(self, query, params=None):
        self.writes.append((query, params or {}))
        return self._rows


def test_backfill_runs_the_ownership_query_and_reports_the_count():
    client = _RecordingClient([{"linked": 2}])

    linked = _run(LongTermMemory(client).backfill_entity_ownership())

    assert linked == 2
    query, _ = client.writes[0]
    assert query == queries.BACKFILL_ENTITY_OWNERSHIP


def test_backfill_reports_zero_when_the_graph_returns_nothing():
    """A driver that returns no rows is 'nothing to repair', not an IndexError."""
    assert _run(LongTermMemory(_RecordingClient([])).backfill_entity_ownership()) == 0


def test_ownership_is_derived_from_messages_only():
    """The repair must apply the write path's rule, not the broader read-scope rule.

    ENTITY_IN_USER_SCOPE deliberately accepts MENTIONS, APPLIES_TO and TOUCHED, because
    seeing and correcting your own data is not the same permission as destroying it. If the
    backfill granted ownership from those too, every entity a reasoning step ever touched
    would become deletable — the exact widening of a destructive verb that was rejected when
    this bug was diagnosed.
    """
    query = queries.BACKFILL_ENTITY_OWNERSHIP

    assert "MENTIONS" in query
    assert "APPLIES_TO" not in query, "a preference target is not an owned entity"
    assert "TOUCHED" not in query, "a reasoning-step touch is not ownership"


def test_backfill_is_idempotent_by_construction():
    """Re-running must not depend on the caller: the query itself excludes repaired pairs."""
    query = queries.BACKFILL_ENTITY_OWNERSHIP

    assert re.search(r"WHERE\s+NOT\s+EXISTS\s*\{[^}]*HAS_ENTITY[^}]*\}", query), (
        "without the NOT EXISTS guard the count re-reports rows that were already owned"
    )
    assert "MERGE (u)-[:HAS_ENTITY]->(e)" in query, "CREATE would duplicate the edge on re-run"


def test_the_count_is_entities_not_mentions():
    """An entity mentioned in twelve messages is one ownership edge, not twelve."""
    assert "WITH DISTINCT u, e" in queries.BACKFILL_ENTITY_OWNERSHIP
