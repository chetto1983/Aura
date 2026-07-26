from __future__ import annotations

import os
from uuid import uuid4

import pytest

pytestmark = pytest.mark.integration


def _env_or_skip() -> tuple[str, str, str, str]:
    uri = os.environ.get("NEO4J_TEST_URI") or os.environ.get("NEO4J_URI")
    password = os.environ.get("NEO4J_TEST_PASSWORD") or os.environ.get("NEO4J_PASSWORD")
    if not uri or not password:
        if os.environ.get("CI"):
            pytest.fail("Neo4j credentials are required for the live corpus-epoch tier")
        pytest.skip("set NEO4J_TEST_URI + NEO4J_TEST_PASSWORD for the live corpus-epoch tier")
    user = os.environ.get("NEO4J_TEST_USER") or os.environ.get("NEO4J_USER") or "neo4j"
    database = os.environ.get("NEO4J_TEST_DATABASE") or os.environ.get("NEO4J_DATABASE") or "neo4j"
    return uri, user, password, database


@pytest.fixture
async def graph():
    from pydantic import SecretStr

    from neo4j_agent_memory.config.settings import Neo4jConfig
    from neo4j_agent_memory.graph.client import Neo4jClient

    uri, user, password, database = _env_or_skip()
    config = Neo4jConfig(uri=uri, username=user, password=SecretStr(password), database=database)
    async with Neo4jClient(config) as client:
        yield client


async def test_live_mutation_increments_once_and_reads_do_not_increment(graph):
    fixture_id = f"epoch-test-{uuid4().hex}"
    before = await graph.read_corpus_epoch()
    assert before is not None

    await graph.execute_write(
        "CREATE (:MemoryEpochFixture {id: $id})",
        {"id": fixture_id},
    )
    after_write = await graph.read_corpus_epoch()
    assert after_write == before + 1

    await graph.execute_read(
        "MATCH (fixture:MemoryEpochFixture {id: $id}) RETURN fixture.id AS id",
        {"id": fixture_id},
    )
    assert await graph.read_corpus_epoch() == after_write

    await graph.execute_write(
        "MATCH (fixture:MemoryEpochFixture {id: $id}) DELETE fixture",
        {"id": fixture_id},
    )


async def test_live_failed_mutation_does_not_increment(graph):
    before = await graph.read_corpus_epoch()
    assert before is not None

    with pytest.raises(Exception):
        await graph.execute_write(
            """
            CREATE (:MemoryEpochFixture {id: $id})
            WITH 1 / 0 AS deliberate
            RETURN deliberate
            """,
            {"id": f"epoch-test-{uuid4().hex}"},
        )

    assert await graph.read_corpus_epoch() == before
