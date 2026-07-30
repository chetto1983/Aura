import asyncio
from uuid import uuid4

from neo4j_agent_memory.graph import queries
from neo4j_agent_memory.memory.long_term import LongTermMemory


class RecordingClient:
    def __init__(self, read_rows=None):
        self.read_rows = read_rows or []
        self.reads = []
        self.writes = []

    async def execute_read(self, query, params=None):
        self.reads.append((query, params or {}))
        return self.read_rows

    async def execute_write(self, query, params=None):
        params = params or {}
        self.writes.append((query, params))
        if query == queries.CREATE_FACT:
            return [{"f": {**params, "created_at": None}}]
        return []


def test_add_entity_links_entity_to_user_scope():
    asyncio.run(_test_add_entity_links_entity_to_user_scope())


async def _test_add_entity_links_entity_to_user_scope():
    client = RecordingClient()
    memory = LongTermMemory(client)

    entity, _ = await memory.add_entity(
        "Caraglio",
        "LOCATION",
        user_identifier="identity-1",
        generate_embedding=False,
    )

    assert any(
        "HAS_ENTITY" in query
        and params["user_identifier"] == "identity-1"
        and params["entity_id"] == str(entity.id)
        for query, params in client.writes
    )


def test_add_fact_links_fact_to_user_scope():
    asyncio.run(_test_add_fact_links_fact_to_user_scope())


async def _test_add_fact_links_fact_to_user_scope():
    client = RecordingClient()
    memory = LongTermMemory(client)

    fact = await memory.add_fact(
        "Davide",
        "risiede_a",
        "Caraglio",
        user_identifier="identity-1",
        generate_embedding=False,
    )

    assert any(
        "HAS_FACT" in query
        and params["user_identifier"] == "identity-1"
        and params["fact_id"] == str(fact.id)
        for query, params in client.writes
    )


def test_get_facts_about_uses_user_scope_when_identifier_is_supplied():
    asyncio.run(_test_get_facts_about_uses_user_scope_when_identifier_is_supplied())


async def _test_get_facts_about_uses_user_scope_when_identifier_is_supplied():
    fact_id = str(uuid4())
    client = RecordingClient(
        read_rows=[
            {
                "f": {
                    "id": fact_id,
                    "subject": "Davide",
                    "predicate": "risiede_a",
                    "object": "Caraglio",
                    "confidence": 1.0,
                }
            }
        ]
    )
    memory = LongTermMemory(client)

    facts = await memory.get_facts_about("Davide", user_identifier="identity-1")

    assert facts[0].id.hex == fact_id.replace("-", "")
    query, params = client.reads[0]
    assert "HAS_FACT" in query
    assert params["subject"] == "Davide"
    assert params["user_identifier"] == "identity-1"
