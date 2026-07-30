"""Canonical preference/fact writes stay single-node under concurrency."""

from __future__ import annotations

import asyncio
import os
from uuid import UUID, uuid4

import pytest
from pydantic import SecretStr

from neo4j_agent_memory.config.settings import Neo4jConfig
from neo4j_agent_memory.graph import queries
from neo4j_agent_memory.graph.client import Neo4jClient
from neo4j_agent_memory.graph.schema import SchemaManager
from neo4j_agent_memory.memory.long_term import (
    LongTermMemory,
    _canonical_memory_key,
)


class _CanonicalWriteClient:
    """Return one already-existing row from each atomic MERGE."""

    def __init__(self) -> None:
        self.writes: list[tuple[str, dict]] = []
        self.existing_preference_id = uuid4()
        self.existing_fact_id = uuid4()

    async def execute_read(self, _query, _parameters=None):
        return []

    async def execute_write(self, query, parameters=None):
        parameters = parameters or {}
        self.writes.append((query, parameters))
        if query == queries.CREATE_PREFERENCE:
            return [
                {
                    "p": {
                        **parameters,
                        "id": str(self.existing_preference_id),
                        "created_at": None,
                    }
                }
            ]
        if query == queries.CREATE_FACT:
            return [
                {
                    "f": {
                        **parameters,
                        "id": str(self.existing_fact_id),
                        "object": parameters["object"],
                        "created_at": None,
                    }
                }
            ]
        return []


def test_canonical_key_normalizes_visual_text_but_keeps_field_boundaries():
    assert _canonical_memory_key(" Café ", "LOVES   Pasta") == _canonical_memory_key(
        "cafe\u0301", "loves pasta"
    )
    assert _canonical_memory_key("ab", "c") != _canonical_memory_key("a", "bc")


@pytest.mark.asyncio
async def test_preference_merge_returns_the_database_winner_and_owner_scope():
    graph = _CanonicalWriteClient()
    memory = LongTermMemory(graph)

    preference = await memory.add_preference(
        "Food",
        "Loves pasta",
        user_identifier="owner-1",
        generate_embedding=False,
    )

    assert preference.id == graph.existing_preference_id
    create = next(params for query, params in graph.writes if query == queries.CREATE_PREFERENCE)
    assert create["canonical_key"]
    assert create["deduplication_scope"] == '{"user_identifier":"owner-1"}'
    assert "MERGE (p:Preference" in queries.CREATE_PREFERENCE
    assert "ON CREATE SET" in queries.CREATE_PREFERENCE


@pytest.mark.asyncio
async def test_fact_merge_returns_the_database_winner():
    graph = _CanonicalWriteClient()
    memory = LongTermMemory(graph)

    fact = await memory.add_fact(
        "Davide",
        "vive_a",
        "Caraglio",
        user_identifier="owner-1",
        generate_embedding=False,
    )

    assert fact.id == graph.existing_fact_id
    create = next(params for query, params in graph.writes if query == queries.CREATE_FACT)
    assert create["canonical_key"]
    assert "MERGE (f:Fact" in queries.CREATE_FACT
    assert "ON CREATE SET" in queries.CREATE_FACT


def test_schema_has_canonical_preference_and_fact_uniqueness_constraints():
    constraints = {
        name: (label, properties)
        for name, label, properties in SchemaManager._COMPOSITE_CONSTRAINTS
    }
    assert constraints["preference_canonical_scope_unique"] == (
        "Preference",
        ("canonical_key", "deduplication_scope"),
    )
    assert constraints["fact_canonical_scope_unique"] == (
        "Fact",
        ("canonical_key", "deduplication_scope"),
    )


def _config_or_skip() -> Neo4jConfig:
    uri = os.environ.get("NEO4J_TEST_URI") or os.environ.get("NEO4J_URI")
    password = os.environ.get("NEO4J_TEST_PASSWORD") or os.environ.get("NEO4J_PASSWORD")
    if not uri or not password:
        if os.environ.get("CI"):
            pytest.fail("Neo4j credentials are required for canonical write concurrency tests")
        pytest.skip("set NEO4J_TEST_URI + NEO4J_TEST_PASSWORD")
    return Neo4jConfig(
        uri=uri,
        username=os.environ.get("NEO4J_TEST_USER") or os.environ.get("NEO4J_USER") or "neo4j",
        password=SecretStr(password),
        database=(
            os.environ.get("NEO4J_TEST_DATABASE")
            or os.environ.get("NEO4J_DATABASE")
            or "neo4j"
        ),
    )


@pytest.mark.integration
@pytest.mark.asyncio
async def test_fifty_barrier_writers_create_one_preference_and_one_fact():
    run = uuid4().hex
    owner = f"canonical-owner-{run}"
    category = f"canonical-category-{run}"
    predicate = f"canonical-predicate-{run}"
    release = asyncio.Event()

    async with Neo4jClient(_config_or_skip()) as graph:
        await SchemaManager(graph).setup_constraints()
        memory = LongTermMemory(graph)

        async def preference_writer():
            await release.wait()
            return await memory.add_preference(
                category,
                "same preference",
                user_identifier=owner,
                generate_embedding=False,
            )

        async def fact_writer():
            await release.wait()
            return await memory.add_fact(
                owner,
                predicate,
                "same object",
                user_identifier=owner,
                generate_embedding=False,
            )

        tasks = [
            asyncio.create_task(preference_writer()) for _ in range(50)
        ] + [
            asyncio.create_task(fact_writer()) for _ in range(50)
        ]
        release.set()
        results = await asyncio.gather(*tasks)

        preference_ids = {str(item.id) for item in results[:50]}
        fact_ids = {str(item.id) for item in results[50:]}
        assert len(preference_ids) == 1
        assert len(fact_ids) == 1
        assert all(UUID(item) for item in preference_ids | fact_ids)

        rows = await graph.execute_read(
            """
            MATCH (:User {identifier: $owner})-[:HAS_PREFERENCE]->(p:Preference {
                category: $category
            })
            WITH count(DISTINCT p) AS preferences
            MATCH (:User {identifier: $owner})-[:HAS_FACT]->(f:Fact {
                predicate: $predicate
            })
            RETURN preferences, count(DISTINCT f) AS facts
            """,
            {"owner": owner, "category": category, "predicate": predicate},
        )
        assert rows == [{"preferences": 1, "facts": 1}]

        await graph.execute_write(
            """
            MATCH (u:User {identifier: $owner})
            OPTIONAL MATCH (u)-[:HAS_PREFERENCE]->(p:Preference)
            OPTIONAL MATCH (u)-[:HAS_FACT]->(f:Fact)
            DETACH DELETE u, p, f
            """,
            {"owner": owner},
        )
