"""Live Cypher proof that superseded preferences cannot win normal recall."""

from __future__ import annotations

import os
from uuid import uuid4

import pytest

from neo4j_agent_memory.graph import queries

pytestmark = pytest.mark.integration


def _env_or_skip() -> tuple[str, str, str, str]:
    uri = os.environ.get("NEO4J_TEST_URI") or os.environ.get("NEO4J_URI")
    password = os.environ.get("NEO4J_TEST_PASSWORD") or os.environ.get("NEO4J_PASSWORD")
    if not uri or not password:
        if os.environ.get("CI"):
            pytest.fail("Neo4j credentials are required for live active-preference recall")
        pytest.skip("set NEO4J_TEST_URI + NEO4J_TEST_PASSWORD for live preference recall")
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


async def test_scoped_category_recall_returns_only_the_active_replacement(graph):
    run = uuid4().hex
    owner = f"mem-002-owner-{run}"
    old_id = f"mem-002-old-{run}"
    new_id = f"mem-002-new-{run}"
    category = f"mem-002-{run}"
    await graph.execute_write(
        """
        CREATE (u:User {identifier: $owner})
        CREATE (old:Preference {
            id: $old_id, category: $category, preference: 'always use X',
            confidence: 1.0, created_at: datetime(), valid_from: datetime(),
            valid_until: datetime()
        })
        CREATE (new:Preference {
            id: $new_id, category: $category, preference: 'never use X',
            confidence: 0.5, created_at: datetime(), valid_from: datetime()
        })
        CREATE (u)-[:HAS_PREFERENCE]->(old)
        CREATE (u)-[:HAS_PREFERENCE]->(new)
        CREATE (old)-[:SUPERSEDED_BY]->(new)
        """,
        {
            "owner": owner,
            "old_id": old_id,
            "new_id": new_id,
            "category": category,
        },
    )
    try:
        rows = await graph.execute_read(
            queries.SEARCH_PREFERENCES_BY_CATEGORY_SCOPED,
            {"user_identifier": owner, "category": category, "limit": 10},
        )
        assert [row["p"]["id"] for row in rows] == [new_id]
    finally:
        await graph.execute_write(
            "MATCH (u:User {identifier: $owner}) OPTIONAL MATCH (u)-[:HAS_PREFERENCE]->(p) DETACH DELETE u, p",
            {"owner": owner},
        )
