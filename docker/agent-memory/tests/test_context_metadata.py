from __future__ import annotations

import asyncio
from types import SimpleNamespace
from uuid import UUID

from neo4j_agent_memory.integration import MemoryIntegration, SessionStrategy
from neo4j_agent_memory.memory.long_term import Entity, Preference
from neo4j_agent_memory.rerank import record_rerank_evidence

PREFERENCE_IDS = [
    UUID(f"10000000-0000-0000-0000-00000000000{index}") for index in range(1, 4)
]
ENTITY_IDS = [
    UUID(f"20000000-0000-0000-0000-00000000000{index}") for index in range(1, 4)
]


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
    def __init__(self, reranker_status="applied"):
        self.preference_calls = 0
        self.entity_calls = 0
        self.reranker_status = reranker_status
        self.preference_started = asyncio.Event()
        self.entity_started = asyncio.Event()
        self.preferences = [
            Preference(
                id=preference_id,
                category="food",
                preference=f"fresh pasta {index}",
                context="Sunday lunch [trusted]",
            )
            for index, preference_id in enumerate(PREFERENCE_IDS, start=1)
        ]
        self.entities = [
            Entity(
                id=entity_id,
                name=f"Davide {index}",
                type="PERSON",
                description="Aura\noperator",
            )
            for index, entity_id in enumerate(ENTITY_IDS, start=1)
        ]

    async def search_preferences(self, query, *, limit, user_identifier):
        self.preference_calls += 1
        assert query == "profile"
        assert limit == 4
        assert user_identifier == "owner-1"
        self.preference_started.set()
        await asyncio.wait_for(self.entity_started.wait(), timeout=0.25)
        record_rerank_evidence(self.reranker_status, "aura-rerank")
        return self.preferences

    async def search_entities(self, query, *, limit, user_identifier):
        self.entity_calls += 1
        assert query == "profile"
        assert limit == 4
        assert user_identifier == "owner-1"
        self.entity_started.set()
        await asyncio.wait_for(self.preference_started.wait(), timeout=0.25)
        record_rerank_evidence(self.reranker_status, "aura-rerank")
        return self.entities


class _MemoryClient:
    def __init__(self, epochs=(7, 7), reranker_status="applied"):
        self._client = _EpochReader(epochs)
        self.long_term = _LongTerm(reranker_status)
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


def test_long_term_only_context_is_concurrent_capped_and_provenance_exact():
    client = _MemoryClient()
    result = _long_term_only(client)
    assert client.long_term.preference_calls == 1
    assert client.long_term.entity_calls == 1
    metadata = result["recall_metadata"]
    assert len(metadata["results"]) == 4
    assert metadata["results"] == [
        {"kind": "memory_preference", "id": str(PREFERENCE_IDS[0]), "order": 0},
        {"kind": "memory_entity", "id": str(ENTITY_IDS[0]), "order": 1},
        {"kind": "memory_preference", "id": str(PREFERENCE_IDS[1]), "order": 2},
        {"kind": "memory_entity", "id": str(ENTITY_IDS[1]), "order": 3},
    ]
    markers = [
        line.split("]", 1)[0] + "]"
        for line in result["context"].splitlines()
        if line.startswith("- [memory_")
    ]
    assert markers == [
        f"- [memory_preference:{PREFERENCE_IDS[0]}]",
        f"- [memory_entity:{ENTITY_IDS[0]}]",
        f"- [memory_preference:{PREFERENCE_IDS[1]}]",
        f"- [memory_entity:{ENTITY_IDS[1]}]",
    ]
    assert "Sunday lunch {trusted}" in result["context"]
    assert "Aura operator" in result["context"]
    assert client.delegated_calls == 0


def test_long_term_only_context_emits_ordered_typed_safe_metadata():
    client = _MemoryClient()

    result = _long_term_only(client)

    metadata = result["recall_metadata"]
    assert metadata["results"] == [
        {"kind": "memory_preference", "id": str(PREFERENCE_IDS[0]), "order": 0},
        {"kind": "memory_entity", "id": str(ENTITY_IDS[0]), "order": 1},
        {"kind": "memory_preference", "id": str(PREFERENCE_IDS[1]), "order": 2},
        {"kind": "memory_entity", "id": str(ENTITY_IDS[1]), "order": 3},
    ]
    assert metadata["limits"] == {
        "memory_preference": {"requested_k": 4, "effective_k": 2, "count": 2},
        "memory_entity": {"requested_k": 4, "effective_k": 2, "count": 2},
    }
    assert metadata["revisions"] == {
        "retriever": "neo4j-agent-memory-long-term-v1",
        "reranker": "aura-rerank",
        "embedding": "openai/embedding-fixture-v1@3",
        "index": "entity_embedding_idx+preference_embedding_idx@3",
    }
    assert metadata["reranker_status"] == "applied"
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


def test_disabled_or_degraded_reranking_is_reported_and_ineligible():
    for status in ("disabled", "degraded"):
        result = _long_term_only(_MemoryClient(reranker_status=status))
        assert result["recall_metadata"]["reranker_status"] == status
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
