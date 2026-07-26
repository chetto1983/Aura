from __future__ import annotations

import asyncio
from types import SimpleNamespace
from uuid import UUID

from neo4j_agent_memory.integration import MemoryIntegration, SessionStrategy
from neo4j_agent_memory.memory.long_term import Entity, LongTermMemory, Preference

PREFERENCE_ID = UUID("10000000-0000-0000-0000-000000000001")
ENTITY_ID = UUID("20000000-0000-0000-0000-000000000002")


class _EpochReader:
    def __init__(self, epochs):
        self.epochs = list(epochs)
        self.calls = 0

    async def read_corpus_epoch(self):
        self.calls += 1
        value = self.epochs.pop(0)
        if isinstance(value, Exception):
            raise value
        return value


class _LongTerm:
    def __init__(self):
        self.preference_calls = 0
        self.entity_calls = 0
        self.preferences = [
            Preference(
                id=PREFERENCE_ID,
                category="food",
                preference="fresh pasta",
                context="Sunday lunch",
            )
        ]
        self.entities = [
            Entity(
                id=ENTITY_ID,
                name="Davide",
                type="PERSON",
                description="Aura operator",
            )
        ]

    async def search_preferences(self, query, *, limit, user_identifier):
        self.preference_calls += 1
        assert query == "profile"
        assert limit == 4
        assert user_identifier == "owner-1"
        return self.preferences

    async def search_entities(self, query, *, limit, user_identifier):
        self.entity_calls += 1
        assert query == "profile"
        assert limit == 4
        assert user_identifier == "owner-1"
        return self.entities


class _MemoryClient:
    def __init__(self, epochs=(7, 7)):
        self._client = _EpochReader(epochs)
        self.long_term = _LongTerm()
        self.delegated_calls = 0
        self._settings = SimpleNamespace(
            embedding=SimpleNamespace(
                provider=SimpleNamespace(value="openai"),
                model="embedding-fixture-v1",
                dimensions=3,
            )
        )

    async def get_context(self, **_kwargs):
        self.delegated_calls += 1
        return "delegated context"


def _long_term_only(client: _MemoryClient):
    integration = MemoryIntegration(
        client=client,
        user_id="owner-1",
        session_strategy=SessionStrategy.PERSISTENT,
    )
    return asyncio.run(
        integration.get_context(
            query="profile",
            max_items=4,
            include_short_term=False,
            include_long_term=True,
            include_reasoning=False,
            user_identifier="owner-1",
        )
    )


def test_long_term_only_context_is_byte_compatible_and_retrieved_once():
    client = _MemoryClient()
    result = _long_term_only(client)
    assert client.long_term.preference_calls == 1
    assert client.long_term.entity_calls == 1

    direct_source = _LongTerm()
    direct = LongTermMemory(SimpleNamespace())
    direct.search_preferences = direct_source.search_preferences
    direct.search_entities = direct_source.search_entities
    expected_long_term = asyncio.run(
        direct.get_context("profile", max_items=4, user_identifier="owner-1")
    )

    assert result["context"] == f"## Relevant Knowledge\n{expected_long_term}"
    assert direct_source.preference_calls == 1
    assert direct_source.entity_calls == 1
    assert client.delegated_calls == 0


def test_long_term_only_context_emits_ordered_typed_safe_metadata():
    client = _MemoryClient()

    result = _long_term_only(client)

    metadata = result["recall_metadata"]
    assert metadata["results"] == [
        {"kind": "memory_preference", "id": str(PREFERENCE_ID), "order": 0},
        {"kind": "memory_entity", "id": str(ENTITY_ID), "order": 1},
    ]
    assert metadata["limits"] == {
        "memory_preference": {"requested_k": 4, "effective_k": 4, "count": 1},
        "memory_entity": {"requested_k": 4, "effective_k": 4, "count": 1},
    }
    assert metadata["revisions"] == {
        "retriever": "neo4j-agent-memory-long-term-v1",
        "reranker": "none-v1",
        "embedding": "openai/embedding-fixture-v1@3",
        "index": "entity_embedding_idx+preference_embedding_idx@3",
    }
    assert metadata["corpus_epoch_before"] == 7
    assert metadata["corpus_epoch_after"] == 7
    assert metadata["coherent"] is True
    assert metadata["adaptive_eligible"] is True
    assert client.long_term.preference_calls == 1
    assert client.long_term.entity_calls == 1

    serialized = repr(metadata).lower()
    for forbidden in ("query", "content", "fingerprint", "fresh pasta", "davide"):
        assert forbidden not in serialized


def test_unequal_or_missing_epochs_keep_static_context_but_are_ineligible():
    for epochs in ((7, 8), (None, None), (RuntimeError("unavailable"), 7)):
        result = _long_term_only(_MemoryClient(epochs))

        assert result["has_context"] is True
        assert "fresh pasta" in result["context"]
        assert result["recall_metadata"]["coherent"] is False
        assert result["recall_metadata"]["adaptive_eligible"] is False


def test_short_term_or_reasoning_requests_delegate_unchanged():
    client = _MemoryClient()
    integration = MemoryIntegration(
        client=client,
        user_id="owner-1",
        session_strategy=SessionStrategy.PERSISTENT,
    )

    result = asyncio.run(
        integration.get_context(
            query="profile",
            include_short_term=True,
            include_long_term=True,
            include_reasoning=False,
            user_identifier="owner-1",
        )
    )

    assert result == {
        "session_id": "owner-1",
        "context": "delegated context",
        "has_context": True,
    }
    assert client.delegated_calls == 1
    assert client.long_term.preference_calls == 0
    assert client.long_term.entity_calls == 0
