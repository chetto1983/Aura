"""Execute forget against a REAL Neo4j, because the rest of the suite structurally cannot.

Every other test here drives fake clients that never execute Cypher. That is how a family of
defects shipped green: the `*_SCOPED` queries whose WHERE was glued to an OPTIONAL MATCH (so
they scoped nothing), and forget's own "deleted" that left the entity readable. A query string
can satisfy any number of assertions about its text and still not do what it says.

Runs only when a Neo4j is reachable; builds and destroys its own fixture under a unique
identifier and never reads or writes anything outside it.

    NEO4J_TEST_URI=bolt://127.0.0.1:7687 NEO4J_TEST_PASSWORD=... \
      python -m pytest tests/test_forget_live_scope_integration.py
"""

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
        pytest.skip("set NEO4J_TEST_URI + NEO4J_TEST_PASSWORD to run the live forget test")
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


async def _scope_count(client, entity_id: str, identifier: str) -> int:
    rows = await client.execute_read(
        queries.ENTITY_IN_USER_SCOPE,
        {"entity_id": entity_id, "user_identifier": identifier},
    )
    return int(rows[0]["scope_count"]) if rows else 0


async def test_forget_removes_the_entity_from_the_callers_read_scope(graph):
    """The live assertion the structural tests cannot make: after forget, scope is empty.

    The fixture is the shape that broke — an entity owned by the caller AND mentioned by the
    caller's message, which is every entity born from extraction. Before the fix, forget cut
    the ownership edge, reported "deleted", and left scope_count at 1 through MENTIONS.
    """
    run = uuid4().hex[:12]
    identifier = f"forget-scope-test-{run}"
    entity_id = f"entity-{run}"

    await graph.execute_write(
        """
        CREATE (u:User {identifier: $identifier})
        CREATE (c:Conversation {id: $conversation_id})
        CREATE (m:Message {id: $message_id, content: 'fixture'})
        CREATE (e:Entity {id: $entity_id, name: 'ForgetScopeFixture', type: 'PERSON'})
        CREATE (u)-[:HAS_CONVERSATION]->(c)
        CREATE (c)-[:HAS_MESSAGE]->(m)
        CREATE (u)-[:HAS_ENTITY]->(e)
        CREATE (m)-[:MENTIONS]->(e)
        """,
        {
            "identifier": identifier,
            "conversation_id": f"conversation-{run}",
            "message_id": f"message-{run}",
            "entity_id": entity_id,
        },
    )

    try:
        assert await _scope_count(graph, entity_id, identifier) > 0, "fixture is not in scope"

        rows = await graph.execute_write(
            queries.DELETE_ENTITY_SCOPED,
            {"entity_id": entity_id, "user_identifier": identifier},
        )

        assert rows, "forget matched nothing on an entity the caller owns"
        assert rows[0]["deleted_id"] == entity_id
        assert await _scope_count(graph, entity_id, identifier) == 0, (
            "the entity the caller was told was deleted is still inside their read scope"
        )
    finally:
        await graph.execute_write(
            """
            MATCH (u:User {identifier: $identifier})
            OPTIONAL MATCH (u)-[:HAS_CONVERSATION]->(c:Conversation)
            OPTIONAL MATCH (c)-[:HAS_MESSAGE]->(m:Message)
            OPTIONAL MATCH (e:Entity {id: $entity_id})
            DETACH DELETE u, c, m, e
            """,
            {"identifier": identifier, "entity_id": entity_id},
        )


async def test_forget_does_not_touch_another_users_edges(graph):
    """Non-cascading, stated where it counts: a shared entity survives for everyone else."""
    run = uuid4().hex[:12]
    caller = f"forget-scope-test-{run}-a"
    other = f"forget-scope-test-{run}-b"
    entity_id = f"entity-{run}"

    await graph.execute_write(
        """
        CREATE (a:User {identifier: $caller})
        CREATE (b:User {identifier: $other})
        CREATE (e:Entity {id: $entity_id, name: 'SharedFixture', type: 'PERSON'})
        CREATE (a)-[:HAS_ENTITY]->(e)
        CREATE (b)-[:HAS_ENTITY]->(e)
        """,
        {"caller": caller, "other": other, "entity_id": entity_id},
    )

    try:
        rows = await graph.execute_write(
            queries.DELETE_ENTITY_SCOPED,
            {"entity_id": entity_id, "user_identifier": caller},
        )

        assert rows[0]["removed_node"] is False, "a shared entity must survive the node deletion"
        assert await _scope_count(graph, entity_id, caller) == 0
        assert await _scope_count(graph, entity_id, other) == 1, "another user's memory was cut"
    finally:
        await graph.execute_write(
            """
            MATCH (u:User) WHERE u.identifier IN [$caller, $other]
            OPTIONAL MATCH (e:Entity {id: $entity_id})
            DETACH DELETE u, e
            """,
            {"caller": caller, "other": other, "entity_id": entity_id},
        )
