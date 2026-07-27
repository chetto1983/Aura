"""Schema DDL must never ride the corpus-epoch write path.

Neo4j refuses to mix statement types inside one transaction. ``execute_write`` appends a
``MATCH (revision:MemoryCorpusRevision …) SET …`` epoch bump to every call, so any DDL sent
through it aborts with::

    Neo.ClientError.Transaction.ForbiddenDueToTransactionType
    Tried to execute Write query after executing Schema modification

The bootstrap did exactly that, and it stayed invisible on every long-lived database:
``_create_constraint``/``_create_index`` short-circuit on ``check_*_exists``, so the DDL is
only ever emitted against an instance that lacks the schema. A fresh Neo4j — CI, or a new
deployment — was the only place the sidecar refused to boot.

These tests pin the invariant on the whole ``setup_all`` surface rather than on the one
constraint that happened to fail first, since the same mistake is available at each of the
seven DDL call sites.
"""

from __future__ import annotations

import asyncio

from neo4j_agent_memory.graph.schema import SchemaManager

_DDL_PREFIXES = ("CREATE CONSTRAINT", "CREATE INDEX", "CREATE VECTOR INDEX", "CREATE POINT INDEX", "DROP")


class _RecordingClient:
    """Records which transaction path each query was routed through."""

    def __init__(self) -> None:
        self.schema_queries: list[str] = []
        self.write_queries: list[str] = []

    async def check_constraint_exists(self, _name: str) -> bool:
        return False

    async def check_index_exists(self, _name: str) -> bool:
        return False

    async def execute_schema(self, query: str, parameters: dict | None = None) -> list:
        self.schema_queries.append(query)
        return []

    async def execute_write(self, query: str, parameters: dict | None = None) -> list:
        self.write_queries.append(query)
        return []

    async def execute_read(self, query: str, parameters: dict | None = None) -> list:
        return []


def _is_ddl(query: str) -> bool:
    return query.strip().upper().startswith(_DDL_PREFIXES)


def test_setup_all_sends_every_ddl_statement_down_the_schema_path():
    client = _RecordingClient()

    asyncio.run(SchemaManager(client).setup_all())

    assert client.schema_queries, "setup_all emitted no DDL against an empty database"
    assert all(_is_ddl(q) for q in client.schema_queries)
    misrouted = [q for q in client.write_queries if _is_ddl(q)]
    assert not misrouted, f"DDL routed through the epoch-bumping write path: {misrouted}"


def test_setup_constraints_creates_the_constraint_that_broke_the_bootstrap():
    client = _RecordingClient()

    asyncio.run(SchemaManager(client).setup_constraints())

    assert any("conversation_id" in q for q in client.schema_queries)
    assert client.write_queries == []


def test_drop_all_sends_its_ddl_down_the_schema_path():
    client = _RecordingClient()

    async def _show(query: str, parameters: dict | None = None) -> list:
        if "CONSTRAINT" in query.upper():
            return [{"name": "conversation_id"}]
        return [{"name": "message_timestamp_idx"}]

    client.execute_read = _show  # type: ignore[method-assign]

    asyncio.run(SchemaManager(client).drop_all())

    assert len(client.schema_queries) == 2
    assert all(q.strip().upper().startswith("DROP") for q in client.schema_queries)
    assert client.write_queries == []
