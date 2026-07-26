from __future__ import annotations

import asyncio
from types import SimpleNamespace

import pytest
from neo4j.exceptions import AuthError, ServiceUnavailable

from neo4j_agent_memory.core.exceptions import ConnectionError
from neo4j_agent_memory.graph import client as client_module
from neo4j_agent_memory.graph.client import Neo4jClient


class _Result:
    def __init__(self, rows):
        self._rows = rows

    async def data(self):
        return self._rows


class _Transaction:
    def __init__(self, *, mutation_error: Exception | None = None, epoch_rows=None):
        self.mutation_error = mutation_error
        self.epoch_rows = [{"epoch": 2}] if epoch_rows is None else epoch_rows
        self.queries: list[tuple[str, dict]] = []

    async def run(self, query, parameters=None):
        self.queries.append((query, parameters or {}))
        if len(self.queries) == 1 and self.mutation_error is not None:
            raise self.mutation_error
        if len(self.queries) == 1:
            return _Result([{"stored": True}])
        return _Result(self.epoch_rows)


class _Session:
    def __init__(self, transaction: _Transaction):
        self.transaction = transaction
        self.committed = False

    async def __aenter__(self):
        return self

    async def __aexit__(self, *_args):
        return None

    async def execute_write(self, work):
        rows = await work(self.transaction)
        self.committed = True
        return rows

    async def execute_read(self, work):
        return await work(self.transaction)


class _Driver:
    def __init__(self, session: _Session):
        self._session = session

    def session(self, **_kwargs):
        return self._session


def _client(transaction: _Transaction) -> tuple[Neo4jClient, _Session]:
    client = Neo4jClient(
        SimpleNamespace(
            database="neo4j",
            uri="bolt://unused",
            username="neo4j",
            password=SimpleNamespace(get_secret_value=lambda: "unused"),
            max_connection_pool_size=1,
            connection_timeout=1.0,
            max_transaction_retry_time=1.0,
            max_connection_lifetime=1.0,
            liveness_check_timeout=1.0,
            keep_alive=True,
        )
    )
    session = _Session(transaction)
    client._driver = _Driver(session)
    return client, session


def test_mutation_and_epoch_increment_share_one_managed_transaction():
    transaction = _Transaction()
    client, session = _client(transaction)

    rows = asyncio.run(client.execute_write("CREATE (:Message {id: $id})", {"id": "m-1"}))

    assert rows == [{"stored": True}]
    assert len(transaction.queries) == 2
    assert "MemoryCorpusRevision" in transaction.queries[1][0]
    assert "epoch" in transaction.queries[1][0]
    assert session.committed


def test_mutation_failure_never_attempts_epoch_increment():
    transaction = _Transaction(mutation_error=RuntimeError("mutation failed"))
    client, session = _client(transaction)

    with pytest.raises(RuntimeError, match="mutation failed"):
        asyncio.run(client.execute_write("CREATE (:Message)"))

    assert len(transaction.queries) == 1
    assert not session.committed


def test_missing_epoch_singleton_rolls_back_the_mutation():
    transaction = _Transaction(epoch_rows=[])
    client, session = _client(transaction)

    with pytest.raises(RuntimeError, match="corpus epoch singleton"):
        asyncio.run(client.execute_write("CREATE (:Message)"))

    assert len(transaction.queries) == 2
    assert not session.committed


def test_reads_do_not_advance_the_epoch():
    transaction = _Transaction()
    client, _ = _client(transaction)

    asyncio.run(client.execute_read("MATCH (m:Message) RETURN m"))

    assert len(transaction.queries) == 1
    assert "MemoryCorpusRevision" not in transaction.queries[0][0]


def test_corpus_epoch_read_is_positive_or_missing():
    transaction = _Transaction()
    transaction.queries.append(("already", {}))
    client, _ = _client(transaction)

    assert asyncio.run(client.read_corpus_epoch()) == 2

    missing = _Transaction(epoch_rows=[])
    missing.queries.append(("already", {}))
    missing_client, _ = _client(missing)
    assert asyncio.run(missing_client.read_corpus_epoch()) is None


class _ConnectDriver:
    def __init__(self, verify_error: Exception | None = None):
        self.verify_error = verify_error
        self.verified = False
        self.closed = False

    async def verify_connectivity(self):
        if self.verify_error:
            raise self.verify_error
        self.verified = True

    async def close(self):
        self.closed = True

    def session(self, **_kwargs):
        return SimpleNamespace()


@pytest.mark.parametrize(
    ("raised", "message"),
    [
        (AuthError("bad auth"), "Authentication failed"),
        (ServiceUnavailable("offline"), "Neo4j service unavailable"),
        (RuntimeError("boom"), "Failed to connect"),
    ],
)
def test_connect_classifies_driver_failures(monkeypatch, raised, message):
    client, _ = _client(_Transaction())
    client._driver = None
    monkeypatch.setattr(
        client_module.AsyncGraphDatabase,
        "driver",
        lambda *_args, **_kwargs: _ConnectDriver(raised),
    )

    with pytest.raises(ConnectionError, match=message):
        asyncio.run(client.connect())


def test_connect_close_context_and_connection_guard(monkeypatch):
    client, _ = _client(_Transaction())
    client._driver = None
    driver = _ConnectDriver()
    monkeypatch.setattr(
        client_module.AsyncGraphDatabase,
        "driver",
        lambda *_args, **_kwargs: driver,
    )

    async def exercise():
        assert not client.is_connected
        with pytest.raises(ConnectionError, match="Not connected"):
            client._ensure_connected()
        entered = await client.__aenter__()
        assert entered is client
        assert client.is_connected
        assert client._get_session() is not None
        await client.connect()
        await client.__aexit__(None, None, None)

    asyncio.run(exercise())
    assert driver.verified
    assert driver.closed
    assert not client.is_connected
    asyncio.run(client.close())


def test_query_helpers_project_parameters_and_empty_results():
    client, _ = _client(_Transaction())
    reads: list[tuple[str, dict]] = []
    writes: list[tuple[str, dict]] = []
    queued_reads = [
        [{"n": {"id": "n-1"}}],
        [],
        [{"count": 1}],
        [],
        [{"count": 1}],
        [],
        [{"name": "Neo4j"}],
        [],
        [{"score": 0.9}],
        [{"score": 0.8}],
    ]
    queued_writes = [[{"deleted": 1}], []]

    async def execute_read(query, parameters=None):
        reads.append((query, parameters or {}))
        return queued_reads.pop(0)

    async def execute_write(query, parameters=None):
        writes.append((query, parameters or {}))
        return queued_writes.pop(0)

    client.execute_read = execute_read
    client.execute_write = execute_write

    async def exercise():
        assert await client.get_node_by_id("Entity", "n-1") == {"id": "n-1"}
        assert await client.get_node_by_id("Entity", "missing") is None
        assert await client.check_index_exists("entity_embedding_idx")
        assert not await client.check_index_exists("missing")
        assert await client.check_constraint_exists("entity_id")
        assert not await client.check_constraint_exists("missing")
        assert await client.get_database_info() == {"name": "Neo4j"}
        assert await client.get_database_info() == {}
        assert await client.delete_node_by_id("Entity", "n-1")
        assert not await client.delete_node_by_id("Entity", "missing")
        assert await client.vector_search(
            "entity_embedding_idx",
            [1.0, 0.0],
            limit=2,
            threshold=0.5,
            return_properties=["id", "name"],
        ) == [{"score": 0.9}]
        assert await client.vector_search(
            "entity_embedding_idx",
            [1.0, 0.0],
        ) == [{"score": 0.8}]

    asyncio.run(exercise())
    assert "RETURN node.id AS id, node.name AS name, score" in reads[-2][0]
    assert "RETURN node, score" in reads[-1][0]
    assert writes[0][1] == {"id": "n-1"}
