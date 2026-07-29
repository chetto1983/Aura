"""Unit tests for LongTermMemory.update_entity/update_preference/update_fact
and their MemoryIntegration wrappers -- the three node types memory_update
dispatches to, symmetric with memory_forget's 'entity' | 'preference' | 'fact'.

Regression coverage for a real production defect: add_entity resolves a name
against existing entities and merges into the matched node, but
CREATE_ENTITY's ON MATCH SET deliberately never touches e.name -- so a
misspelled entity (e.g. "David" instead of "Davide") could never be renamed
through add_entity, and delete-then-recreate was the only lever (destroying
the node's relationships). The same shape of bug applies to add_preference
(FIND_EXACT_PREFERENCE / embedding-similarity dedup) and add_fact
(FIND_DUPLICATE_FACTS): re-adding to fix wording just merges onto the old
node. update_entity/update_preference/update_fact all edit by id instead,
bypassing resolution/deduplication entirely. These tests exercise the
ownership-refusal contract, partial-update semantics, and embedding refresh
for all three node types without a live Neo4j -- following the
RecordingClient/fake-client style used elsewhere in this directory (see
test_user_scoped_graph_memory.py).
"""

import asyncio
import json
from uuid import uuid4

from memory_update_support import (
    FakeEmbedder,
    FakeGraphClient,
)
from memory_update_support import (
    StubMemoryClient as _StubMemoryClient,
)
from memory_update_support import (
    entity_row as _entity_row,
)
from memory_update_support import (
    fact_row as _fact_row,
)
from memory_update_support import (
    preference_row as _preference_row,
)

from neo4j_agent_memory.graph import queries
from neo4j_agent_memory.integration import MemoryIntegration
from neo4j_agent_memory.memory.long_term import LongTermMemory


def _run(coro):
    return asyncio.run(coro)


# =============================================================================
# entity
# =============================================================================


def test_update_entity_renames_and_refreshes_embedding():
    _run(_test_update_entity_renames_and_refreshes_embedding())


async def _test_update_entity_renames_and_refreshes_embedding():
    entity_id = uuid4()
    client = FakeGraphClient(
        entities={entity_id: _entity_row(entity_id, name="David", canonical_name="David")},
        entity_owned_ids=[entity_id],
    )
    embedder = FakeEmbedder()
    memory = LongTermMemory(client, embedder=embedder)

    result = await memory.update_entity(entity_id, user_identifier="identity-1", name="Davide")

    assert result is not None
    entity, fields = result
    assert fields == ["name"]
    assert entity.name == "Davide"
    # canonical_name must follow the correction -- Entity.display_name prefers
    # canonical_name, and a stale canonical_name is exactly what let the
    # resolver keep re-merging "Davide" back onto "David" in the first place.
    assert entity.canonical_name == "Davide"
    assert entity.display_name == "Davide"

    assert embedder.calls == ["Davide"]
    assert any(q == queries.UPDATE_ENTITY_EMBEDDING for q, _ in client.writes)


def test_update_entity_refuses_unowned_entity():
    _run(_test_update_entity_refuses_unowned_entity())


async def _test_update_entity_refuses_unowned_entity():
    entity_id = uuid4()
    client = FakeGraphClient(
        entities={entity_id: _entity_row(entity_id, name="David")},
        entity_owned_ids=[],  # exists, but not in this caller's scope
    )
    memory = LongTermMemory(client)

    result = await memory.update_entity(entity_id, user_identifier="identity-1", name="Davide")

    assert result is None
    # Refusal must be a hard stop, not a silent no-op that still writes.
    assert not any(q == queries.UPDATE_ENTITY_FIELDS for q, _ in client.writes)


def test_update_entity_refuses_unknown_id():
    _run(_test_update_entity_refuses_unknown_id())


async def _test_update_entity_refuses_unknown_id():
    client = FakeGraphClient()
    memory = LongTermMemory(client)

    result = await memory.update_entity(uuid4(), user_identifier="identity-1", name="Davide")

    assert result is None
    assert client.writes == []


def test_update_entity_partial_update_leaves_other_fields_intact():
    _run(_test_update_entity_partial_update_leaves_other_fields_intact())


async def _test_update_entity_partial_update_leaves_other_fields_intact():
    entity_id = uuid4()
    client = FakeGraphClient(
        entities={
            entity_id: _entity_row(
                entity_id,
                name="Caraglio",
                subtype="ADDRESS",
                description="A comune in Piedmont",
                metadata={"aliases": ["Caraj"]},
            )
        },
        entity_owned_ids=[entity_id],
    )
    embedder = FakeEmbedder()
    memory = LongTermMemory(client, embedder=embedder)

    result = await memory.update_entity(
        entity_id,
        user_identifier="identity-1",
        description="A small comune in the province of Cuneo, Piedmont",
    )

    assert result is not None
    entity, fields = result
    assert fields == ["description"]
    assert entity.description == "A small comune in the province of Cuneo, Piedmont"
    # Untouched fields must survive the partial update unchanged.
    assert entity.name == "Caraglio"
    assert entity.subtype == "ADDRESS"
    assert entity.aliases == ["Caraj"]

    # description is one of the two fields that trigger a re-embed.
    assert embedder.calls == ["Caraglio"]


def test_update_entity_aliases_only_does_not_touch_name_or_description():
    _run(_test_update_entity_aliases_only_does_not_touch_name_or_description())


async def _test_update_entity_aliases_only_does_not_touch_name_or_description():
    entity_id = uuid4()
    client = FakeGraphClient(
        entities={
            entity_id: _entity_row(
                entity_id,
                name="Caraglio",
                description="A comune in Piedmont",
                metadata={"aliases": ["Caraj"]},
            )
        },
        entity_owned_ids=[entity_id],
    )
    embedder = FakeEmbedder()
    memory = LongTermMemory(client, embedder=embedder)

    result = await memory.update_entity(
        entity_id, user_identifier="identity-1", aliases=["Caraj", "Cravai"]
    )

    assert result is not None
    entity, fields = result
    assert fields == ["aliases"]
    assert entity.aliases == ["Caraj", "Cravai"]
    assert entity.name == "Caraglio"
    assert entity.description == "A comune in Piedmont"
    # Neither name nor description changed, so no re-embed is expected.
    assert embedder.calls == []


def test_update_entity_no_change_is_success_with_empty_fields_and_no_write():
    _run(_test_update_entity_no_change_is_success_with_empty_fields_and_no_write())


async def _test_update_entity_no_change_is_success_with_empty_fields_and_no_write():
    entity_id = uuid4()
    client = FakeGraphClient(
        entities={entity_id: _entity_row(entity_id, name="Davide")},
        entity_owned_ids=[entity_id],
    )
    memory = LongTermMemory(client)

    result = await memory.update_entity(entity_id, user_identifier="identity-1", name="Davide")

    assert result is not None
    entity, fields = result
    assert fields == []
    assert entity.name == "Davide"
    assert client.writes == []


def test_update_entity_without_embedder_skips_embedding_refresh():
    _run(_test_update_entity_without_embedder_skips_embedding_refresh())


async def _test_update_entity_without_embedder_skips_embedding_refresh():
    entity_id = uuid4()
    client = FakeGraphClient(
        entities={entity_id: _entity_row(entity_id, name="David")},
        entity_owned_ids=[entity_id],
    )
    memory = LongTermMemory(client)  # no embedder configured

    result = await memory.update_entity(entity_id, user_identifier="identity-1", name="Davide")

    assert result is not None
    entity, fields = result
    assert fields == ["name"]
    assert entity.name == "Davide"
    assert not any(q == queries.UPDATE_ENTITY_EMBEDDING for q, _ in client.writes)


def test_integration_update_entity_returns_changed_fields_and_entity_payload():
    _run(_test_integration_update_entity_returns_changed_fields_and_entity_payload())


async def _test_integration_update_entity_returns_changed_fields_and_entity_payload():
    entity_id = uuid4()
    client = FakeGraphClient(
        entities={entity_id: _entity_row(entity_id, name="David", entity_type="PERSON")},
        entity_owned_ids=[entity_id],
    )
    memory = LongTermMemory(client)
    integration = MemoryIntegration(client=_StubMemoryClient(memory))

    result = await integration.update_entity(
        str(entity_id), user_identifier="identity-1", name="Davide"
    )

    assert result["updated"] == str(entity_id)
    assert result["fields"] == ["name"]
    assert result["entity"]["name"] == "Davide"
    assert result["entity"]["id"] == str(entity_id)
    # Must be JSON-serializable end to end, exactly as the MCP tool returns it.
    json.dumps(result, default=str)


def test_integration_update_entity_refusal_is_not_silent():
    _run(_test_integration_update_entity_refusal_is_not_silent())


async def _test_integration_update_entity_refusal_is_not_silent():
    client = FakeGraphClient()
    memory = LongTermMemory(client)
    integration = MemoryIntegration(client=_StubMemoryClient(memory))

    result = await integration.update_entity(
        str(uuid4()), user_identifier="identity-1", name="Davide"
    )

    assert result == {"updated": None, "reason": "not found or not owned by this user"}


# =============================================================================
# preference
# =============================================================================


def test_update_preference_edits_text_and_refreshes_embedding():
    _run(_test_update_preference_edits_text_and_refreshes_embedding())


async def _test_update_preference_edits_text_and_refreshes_embedding():
    pref_id = uuid4()
    client = FakeGraphClient(
        preferences={
            pref_id: _preference_row(pref_id, category="food", preference="Loves pizza")
        },
        preference_owned_ids=[pref_id],
    )
    embedder = FakeEmbedder()
    memory = LongTermMemory(client, embedder=embedder)

    result = await memory.update_preference(
        pref_id, user_identifier="identity-1", preference="Loves sushi"
    )

    assert result is not None
    pref, fields = result
    assert fields == ["preference"]
    assert pref.preference == "Loves sushi"
    assert pref.category == "food"  # untouched

    # add_preference embeds "{category}: {preference}"; the merged post-update
    # text (old category + new preference) is what gets re-embedded.
    assert embedder.calls == ["food: Loves sushi"]


def test_update_preference_refuses_unowned_preference():
    _run(_test_update_preference_refuses_unowned_preference())


async def _test_update_preference_refuses_unowned_preference():
    pref_id = uuid4()
    client = FakeGraphClient(
        preferences={
            pref_id: _preference_row(pref_id, category="food", preference="Loves pizza")
        },
        preference_owned_ids=[],  # exists, but not in this caller's scope
    )
    memory = LongTermMemory(client)

    result = await memory.update_preference(
        pref_id, user_identifier="identity-1", preference="Loves sushi"
    )

    assert result is None
    assert not any(q == queries.UPDATE_PREFERENCE_FIELDS_SCOPED for q, _ in client.writes)


def test_update_preference_refuses_unknown_id():
    _run(_test_update_preference_refuses_unknown_id())


async def _test_update_preference_refuses_unknown_id():
    client = FakeGraphClient()
    memory = LongTermMemory(client)

    result = await memory.update_preference(
        uuid4(), user_identifier="identity-1", preference="Loves sushi"
    )

    assert result is None
    assert client.writes == []


def test_update_preference_partial_update_leaves_other_fields_intact():
    _run(_test_update_preference_partial_update_leaves_other_fields_intact())


async def _test_update_preference_partial_update_leaves_other_fields_intact():
    pref_id = uuid4()
    client = FakeGraphClient(
        preferences={
            pref_id: _preference_row(
                pref_id,
                category="food",
                preference="Loves pizza",
                context="mentioned at dinner",
                confidence=0.8,
            )
        },
        preference_owned_ids=[pref_id],
    )
    embedder = FakeEmbedder()
    memory = LongTermMemory(client, embedder=embedder)

    result = await memory.update_preference(pref_id, user_identifier="identity-1", confidence=1.0)

    assert result is not None
    pref, fields = result
    assert fields == ["confidence"]
    assert pref.confidence == 1.0
    # Untouched fields must survive the partial update unchanged.
    assert pref.preference == "Loves pizza"
    assert pref.category == "food"
    assert pref.context == "mentioned at dinner"
    # confidence alone does not affect the embedded text -- no re-embed.
    assert embedder.calls == []


def test_update_preference_no_change_is_success_with_empty_fields_and_no_write():
    _run(_test_update_preference_no_change_is_success_with_empty_fields_and_no_write())


async def _test_update_preference_no_change_is_success_with_empty_fields_and_no_write():
    pref_id = uuid4()
    client = FakeGraphClient(
        preferences={
            pref_id: _preference_row(pref_id, category="food", preference="Loves pizza")
        },
        preference_owned_ids=[pref_id],
    )
    memory = LongTermMemory(client)

    result = await memory.update_preference(
        pref_id, user_identifier="identity-1", preference="Loves pizza"
    )

    assert result is not None
    pref, fields = result
    assert fields == []
    assert client.writes == []


def test_integration_update_preference_returns_changed_fields_and_payload():
    _run(_test_integration_update_preference_returns_changed_fields_and_payload())


async def _test_integration_update_preference_returns_changed_fields_and_payload():
    pref_id = uuid4()
    client = FakeGraphClient(
        preferences={
            pref_id: _preference_row(pref_id, category="food", preference="Loves pizza")
        },
        preference_owned_ids=[pref_id],
    )
    memory = LongTermMemory(client)
    integration = MemoryIntegration(client=_StubMemoryClient(memory))

    result = await integration.update_preference(
        str(pref_id), user_identifier="identity-1", preference="Loves sushi"
    )

    assert result["updated"] == str(pref_id)
    assert result["fields"] == ["preference"]
    assert result["preference"]["preference"] == "Loves sushi"
    json.dumps(result, default=str)


def test_integration_update_preference_refusal_is_not_silent():
    _run(_test_integration_update_preference_refusal_is_not_silent())


async def _test_integration_update_preference_refusal_is_not_silent():
    client = FakeGraphClient()
    memory = LongTermMemory(client)
    integration = MemoryIntegration(client=_StubMemoryClient(memory))

    result = await integration.update_preference(
        str(uuid4()), user_identifier="identity-1", preference="Loves sushi"
    )

    assert result == {"updated": None, "reason": "not found or not owned by this user"}


# =============================================================================
# fact
# =============================================================================


def test_update_fact_edits_object_and_refreshes_embedding():
    _run(_test_update_fact_edits_object_and_refreshes_embedding())


async def _test_update_fact_edits_object_and_refreshes_embedding():
    fact_id = uuid4()
    client = FakeGraphClient(
        facts={fact_id: _fact_row(fact_id, subject="Davide", predicate="risiede_a", obj="David")},
        fact_owned_ids=[fact_id],
    )
    embedder = FakeEmbedder()
    memory = LongTermMemory(client, embedder=embedder)

    result = await memory.update_fact(fact_id, user_identifier="identity-1", obj="Caraglio")

    assert result is not None
    fact, fields = result
    assert fields == ["object"]
    assert fact.object == "Caraglio"
    assert fact.subject == "Davide"  # untouched
    assert fact.predicate == "risiede_a"  # untouched

    # add_fact embeds "{subject} {predicate} {object}".
    assert embedder.calls == ["Davide risiede_a Caraglio"]


def test_update_fact_refuses_unowned_fact():
    _run(_test_update_fact_refuses_unowned_fact())


async def _test_update_fact_refuses_unowned_fact():
    fact_id = uuid4()
    client = FakeGraphClient(
        facts={fact_id: _fact_row(fact_id, subject="Davide", predicate="risiede_a", obj="David")},
        fact_owned_ids=[],  # exists, but not in this caller's scope
    )
    memory = LongTermMemory(client)

    result = await memory.update_fact(fact_id, user_identifier="identity-1", obj="Caraglio")

    assert result is None
    assert not any(q == queries.UPDATE_FACT_FIELDS_SCOPED for q, _ in client.writes)


def test_update_fact_refuses_unknown_id():
    _run(_test_update_fact_refuses_unknown_id())


async def _test_update_fact_refuses_unknown_id():
    client = FakeGraphClient()
    memory = LongTermMemory(client)

    result = await memory.update_fact(uuid4(), user_identifier="identity-1", obj="Caraglio")

    assert result is None
    assert client.writes == []


def test_update_fact_partial_update_leaves_other_fields_intact():
    _run(_test_update_fact_partial_update_leaves_other_fields_intact())


async def _test_update_fact_partial_update_leaves_other_fields_intact():
    fact_id = uuid4()
    client = FakeGraphClient(
        facts={
            fact_id: _fact_row(
                fact_id,
                subject="Davide",
                predicate="risiede_a",
                obj="David",
                confidence=0.6,
            )
        },
        fact_owned_ids=[fact_id],
    )
    embedder = FakeEmbedder()
    memory = LongTermMemory(client, embedder=embedder)

    result = await memory.update_fact(fact_id, user_identifier="identity-1", confidence=1.0)

    assert result is not None
    fact, fields = result
    assert fields == ["confidence"]
    assert fact.confidence == 1.0
    # Untouched fields must survive the partial update unchanged.
    assert fact.subject == "Davide"
    assert fact.predicate == "risiede_a"
    assert fact.object == "David"
    # confidence alone does not affect the embedded triple -- no re-embed.
    assert embedder.calls == []


def test_update_fact_no_change_is_success_with_empty_fields_and_no_write():
    _run(_test_update_fact_no_change_is_success_with_empty_fields_and_no_write())


async def _test_update_fact_no_change_is_success_with_empty_fields_and_no_write():
    fact_id = uuid4()
    client = FakeGraphClient(
        facts={fact_id: _fact_row(fact_id, subject="Davide", predicate="risiede_a", obj="David")},
        fact_owned_ids=[fact_id],
    )
    memory = LongTermMemory(client)

    result = await memory.update_fact(fact_id, user_identifier="identity-1", obj="David")

    assert result is not None
    fact, fields = result
    assert fields == []
    assert client.writes == []


def test_integration_update_fact_returns_changed_fields_and_payload():
    _run(_test_integration_update_fact_returns_changed_fields_and_payload())


async def _test_integration_update_fact_returns_changed_fields_and_payload():
    fact_id = uuid4()
    client = FakeGraphClient(
        facts={fact_id: _fact_row(fact_id, subject="Davide", predicate="risiede_a", obj="David")},
        fact_owned_ids=[fact_id],
    )
    memory = LongTermMemory(client)
    integration = MemoryIntegration(client=_StubMemoryClient(memory))

    result = await integration.update_fact(
        str(fact_id), user_identifier="identity-1", object_value="Caraglio"
    )

    assert result["updated"] == str(fact_id)
    assert result["fields"] == ["object"]
    assert result["fact"]["triple"] == "Davide -> risiede_a -> Caraglio"
    json.dumps(result, default=str)


def test_integration_update_fact_refusal_is_not_silent():
    _run(_test_integration_update_fact_refusal_is_not_silent())


async def _test_integration_update_fact_refusal_is_not_silent():
    client = FakeGraphClient()
    memory = LongTermMemory(client)
    integration = MemoryIntegration(client=_StubMemoryClient(memory))

    result = await integration.update_fact(
        str(uuid4()), user_identifier="identity-1", object_value="Caraglio"
    )

    assert result == {"updated": None, "reason": "not found or not owned by this user"}
