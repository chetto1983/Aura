"""Live fault injection for one public multi-step memory mutation."""

from __future__ import annotations

import os
from types import SimpleNamespace
from uuid import uuid4

import pytest
from pydantic import SecretStr

from neo4j_agent_memory.config.settings import Neo4jConfig
from neo4j_agent_memory.graph.client import Neo4jClient
from neo4j_agent_memory.integration import MemoryIntegration
from neo4j_agent_memory.memory.atomic import AtomicMutationError
from neo4j_agent_memory.memory.long_term import LongTermMemory

pytestmark = pytest.mark.integration


def _config_or_skip() -> Neo4jConfig:
    uri = os.environ.get("NEO4J_TEST_URI") or os.environ.get("NEO4J_URI")
    password = os.environ.get("NEO4J_TEST_PASSWORD") or os.environ.get("NEO4J_PASSWORD")
    if not uri or not password:
        if os.environ.get("CI"):
            pytest.fail("Neo4j credentials are required for live atomic mutation tests")
        pytest.skip("set NEO4J_TEST_URI + NEO4J_TEST_PASSWORD for live atomic mutation tests")
    user = os.environ.get("NEO4J_TEST_USER") or os.environ.get("NEO4J_USER") or "neo4j"
    database = os.environ.get("NEO4J_TEST_DATABASE") or os.environ.get("NEO4J_DATABASE") or "neo4j"
    return Neo4jConfig(
        uri=uri,
        username=user,
        password=SecretStr(password),
        database=database,
    )


@pytest.mark.parametrize("fail_after_write", [1, 2, 3])
async def test_preference_fault_rolls_back_node_validity_owner_and_epoch(fail_after_write):
    run = uuid4().hex
    owner = f"mem-001-owner-{run}"
    category = f"mem-001-category-{run}"

    async with Neo4jClient(_config_or_skip()) as graph:
        integration = MemoryIntegration(
            client=SimpleNamespace(graph=graph, long_term=LongTermMemory(graph))
        )
        original_write = graph.execute_write
        writes = 0

        async def fail_after(query, parameters=None):
            nonlocal writes
            rows = await original_write(query, parameters)
            writes += 1
            if writes == fail_after_write:
                raise RuntimeError(f"injected failure after write {writes}")
            return rows

        graph.execute_write = fail_after
        try:
            with pytest.raises(AtomicMutationError, match="memory mutation failed"):
                await integration.add_preference(
                    category,
                    "atomic rollback fixture",
                    user_identifier=owner,
                )
        finally:
            graph.execute_write = original_write

        rows = await graph.execute_read(
            """
            OPTIONAL MATCH (p:Preference {category: $category})
            WITH count(p) AS preferences
            OPTIONAL MATCH (u:User {identifier: $owner})
            RETURN preferences, count(u) AS users
            """,
            {"category": category, "owner": owner},
        )
        assert rows == [{"preferences": 0, "users": 0}]
