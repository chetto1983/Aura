"""Recall must be ordered by relevance, not admitted by an absolute cosine threshold.

``memory_search`` returned the caller's ENTIRE entity and preference set for any query.
Measured on the live graph 2026-07-26, the query "kubernetes deployment yaml" scored
0.8863 / 0.8784 / 0.8760 / 0.8659 against four stored preferences — one of them a home
address — against a default threshold of 0.70. granite-embedding-311m compresses short-text
cosines into a narrow high band, so ``score >= $threshold`` admits everything and the DB's
``ORDER BY score DESC LIMIT`` is close to arbitrary.

Two defects produced that:

* ``integration_context.search`` threaded ``threshold`` into ``search_messages`` but dropped
  it for entities and preferences, so the documented tool parameter did nothing for two of
  the three default memory types (inherited from upstream ``integration.py``).
* Only messages were reranked. Entities and preferences went straight from the embedding
  order to the caller, with no stage that could tell them apart.

Both are covered here. Ordering comes from the Qwen3 cross-encoder, which separates by three
orders of magnitude where the embedding separates by 0.02 — verified live: with the rerank
text carrying preference AND context, "dove vive Davide" scores the Caraglio preference
0.2578 versus 0.0309 for the runner-up; with the preference text alone it scored 0.0002 and
ranked third, because that text never names Davide.
"""

from __future__ import annotations

import asyncio
from uuid import UUID

import pytest

from neo4j_agent_memory.graph import queries
from neo4j_agent_memory.integration_context import ContextSearchMixin
from neo4j_agent_memory.memory.long_term import (
    LongTermMemory,
    _entity_text,
    _preference_text,
    _rerank_pool,
)


class RecordingClient:
    """Fake Neo4jClient returning canned rows and recording every call."""

    def __init__(self, read_rows=None):
        self.read_rows = read_rows or []
        self.reads: list[tuple[str, dict]] = []
        self.writes: list[tuple[str, dict]] = []

    async def execute_read(self, query, params=None):
        self.reads.append((query, params or {}))
        return self.read_rows

    async def execute_write(self, query, params=None):
        self.writes.append((query, params or {}))
        return []


class StubEmbedder:
    async def embed(self, text):
        return [0.1, 0.2, 0.3]

    async def embed_batch(self, texts):
        return [[0.1, 0.2, 0.3] for _ in texts]


# The parsers coerce ids through UUID(), so fixtures must carry real ones.
P1 = "11111111-1111-4111-8111-111111111111"
P2 = "22222222-2222-4222-8222-222222222222"
P3 = "33333333-3333-4333-8333-333333333333"
E1 = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
E2 = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
E3 = "cccccccc-cccc-4ccc-8ccc-cccccccccccc"
F1 = "ffffffff-ffff-4fff-8fff-ffffffffffff"


def _preference_row(pref_id: str, category: str, text: str, context: str, score: float):
    return {
        "p": {
            "id": pref_id,
            "category": category,
            "preference": text,
            "context": context,
            "confidence": 1.0,
        },
        "score": score,
    }


def _reverse_reranker(monkeypatch):
    """Stand in for aura-rerank: reverses the input order and records what it saw."""
    seen: dict[str, list[str]] = {}

    def _fake_rerank(query, docs, base_url=None):
        seen["query"] = query
        seen["docs"] = list(docs)
        return list(reversed(range(len(docs))))

    monkeypatch.setattr("neo4j_agent_memory.rerank.rerank", _fake_rerank)
    return seen


async def test_preference_recall_is_reranked_and_truncated_to_limit(monkeypatch):
    """The reranker orders the recall, and it is fed a pool wider than the caller's limit."""
    seen = _reverse_reranker(monkeypatch)
    rows = [
        _preference_row(P1, "location", "Localita corrente: Caraglio", "Davide vive li", 0.876),
        _preference_row(P2, "style", "Tieni la memoria pulita", "Richiesto da Davide", 0.878),
        _preference_row(P3, "evidence", "MEMORY-EVIDENCE-019f", "projection proof", 0.866),
    ]
    memory = LongTermMemory(RecordingClient(rows), embedder=StubEmbedder())

    result = await memory.search_preferences("dove vive Davide", limit=2, user_identifier="u1")

    # The stub reverses, so the LAST row must now come first — proving rerank ran.
    assert [str(p.id) for p in result] == [P3, P2]
    # ...and that the caller's limit is applied AFTER reranking, not before.
    assert len(result) == 2
    # The rerank text carries preference AND context: the context is what names the subject,
    # and without it the cross-encoder correctly rejects the row that answers the question.
    assert "Davide vive li" in seen["docs"][0]


async def test_preference_recall_asks_the_database_for_a_wider_pool(monkeypatch):
    """A reranker can only reorder what it is given; the DB LIMIT must exceed the caller's."""
    _reverse_reranker(monkeypatch)
    client = RecordingClient([])
    memory = LongTermMemory(client, embedder=StubEmbedder())

    await memory.search_preferences("q", limit=10, user_identifier="u1")

    _, params = client.reads[-1]
    assert params["limit"] == _rerank_pool(10) > 10
    assert params["threshold"] == 0.7


async def test_entity_recall_pins_the_exact_name_match_ahead_of_the_reranker(monkeypatch):
    """An exact name hit is an explicit request, not a relevance guess — it stays first."""
    _reverse_reranker(monkeypatch)
    rows = [
        {"e": {"id": E2, "name": "Other", "type": "PERSON"}, "score": 0.88},
        {"e": {"id": E3, "name": "Another", "type": "PERSON"}, "score": 0.87},
    ]
    client = RecordingClient(rows)
    memory = LongTermMemory(client, embedder=StubEmbedder())

    exact = {"id": E1, "name": "Davide", "type": "PERSON"}
    monkeypatch.setattr(
        memory, "get_entity_by_name", lambda *_a, **_k: _async_value(memory._parse_entity(exact))
    )
    monkeypatch.setattr(memory, "_entity_in_user_scope", lambda *_a, **_k: _async_value(True))

    result = await memory.search_entities("Davide", limit=3, user_identifier="u1")

    assert result[0].name == "Davide"  # pinned, never demoted by the rerank pass
    assert [e.name for e in result[1:]] == ["Another", "Other"]  # tail reversed by the stub


async def test_context_search_forwards_the_threshold_to_every_memory_type():
    """The documented `threshold` parameter must not silently apply to messages only."""
    calls: dict[str, dict] = {}

    class _Recorder:
        def __init__(self, kind):
            self._kind = kind

        async def _record(self, **kwargs):
            calls[self._kind] = kwargs
            return []

    class _ShortTerm:
        async def search_messages(self, **kwargs):
            calls["messages"] = kwargs
            return []

    class _LongTerm:
        async def search_entities(self, **kwargs):
            calls["entities"] = kwargs
            return []

        async def search_preferences(self, **kwargs):
            calls["preferences"] = kwargs
            return []

    class _Client:
        short_term = _ShortTerm()
        long_term = _LongTerm()

    class _Integration(ContextSearchMixin):
        @property
        def client(self):
            return _Client()

    integration = _Integration()

    await integration.search("q", threshold=0.93, user_identifier="u1")

    assert calls["messages"]["threshold"] == 0.93
    assert calls["entities"]["threshold"] == 0.93
    assert calls["preferences"]["threshold"] == 0.93


async def test_context_search_starts_independent_default_buckets_concurrently():
    """One slow backend leg must not serialize all four default recall buckets."""
    started: set[str] = set()
    gate = asyncio.Event()

    async def wait_for_peers(kind: str):
        started.add(kind)
        if len(started) == 4:
            gate.set()
        await gate.wait()
        return []

    class _ShortTerm:
        async def search_messages(self, **_kwargs):
            return await wait_for_peers("messages")

    class _LongTerm:
        async def search_entities(self, **_kwargs):
            return await wait_for_peers("entities")

        async def search_preferences(self, **_kwargs):
            return await wait_for_peers("preferences")

        async def get_facts_about(self, *_args, **_kwargs):
            return await wait_for_peers("facts")

        async def search_facts(self, **_kwargs):
            return []

    class _Client:
        short_term = _ShortTerm()
        long_term = _LongTerm()

    class _Integration(ContextSearchMixin):
        @property
        def client(self):
            return _Client()

    result = await asyncio.wait_for(_Integration().search("q"), timeout=0.5)

    assert set(result["results"]) == {"messages", "entities", "preferences", "facts"}
    assert started == {"messages", "entities", "preferences", "facts"}


async def test_add_fact_deduplicates_identical_text_without_an_embedder():
    """Dedup was gated on `embedding is not None`, so an embedder-less write duplicated."""
    existing = {
        "id": F1,
        "subject": "Davide",
        "predicate": "vive_a",
        "object": "Caraglio",
        "confidence": 1.0,
    }
    client = RecordingClient([{"f": existing}])
    memory = LongTermMemory(client, embedder=None)

    fact = await memory.add_fact(
        "Davide", "vive_a", "Caraglio", user_identifier="u1", generate_embedding=False
    )

    assert fact.id == UUID(F1)
    assert fact.metadata["deduplicated"] is True
    assert client.writes == []  # the duplicate CREATE never ran
    lookup_query, lookup_params = client.reads[-1]
    assert lookup_query == queries.FIND_EXACT_FACT
    # Dedup stays inside the provenance boundary rather than collapsing to a global match,
    # so one tenant's identical fact can never be handed back to another.
    assert lookup_params["deduplication_scope"] == '{"user_identifier":"u1"}'
    assert lookup_params["user_identifier"] == "u1"


async def test_rerank_text_helpers_include_the_qualifying_context():
    """The subject usually lives in the context field, not in the headline text."""
    assert _preference_text(_Pref("Localita: Caraglio", "Davide vive li")) == (
        "Localita: Caraglio Davide vive li"
    )
    assert _entity_text(_Ent("Caraglio", "Comune in provincia di Cuneo")) == (
        "Caraglio Comune in provincia di Cuneo"
    )
    # A missing half must not leak "None" into the reranked document.
    assert _preference_text(_Pref("solo testo", None)) == "solo testo"
    assert _entity_text(_Ent("Davide", None)) == "Davide"


class _Pref:
    def __init__(self, preference, context):
        self.preference = preference
        self.context = context


class _Ent:
    def __init__(self, name, description):
        self.name = name
        self.description = description


def _async_value(value):
    async def _coro():
        return value

    return _coro()


if __name__ == "__main__":  # pragma: no cover
    raise SystemExit(pytest.main([__file__]))
