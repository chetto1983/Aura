from __future__ import annotations

import asyncio
from datetime import datetime, timezone
from types import SimpleNamespace
from uuid import UUID

import pytest

from neo4j_agent_memory.integration import MemoryIntegration, SessionStrategy
from neo4j_agent_memory.memory.atomic import AtomicMutationError


class _Graph:
    async def execute_write_unit(self, work):
        return await work()


def _fact(suffix, subject, predicate, obj):
    return SimpleNamespace(
        id=UUID(f"30000000-0000-0000-0000-00000000000{suffix}"),
        subject=subject,
        predicate=predicate,
        object=obj,
        confidence=0.9,
    )


class _LongTerm:
    def __init__(self, exact_facts=None, semantic_facts=None):
        self.stored_preferences = []
        self.fact_calls = []
        self._exact_facts = exact_facts if exact_facts is not None else [
            _fact(1, "Davide", "lives_in", "Torino")
        ]
        self._semantic_facts = semantic_facts if semantic_facts is not None else [
            _fact(1, "Davide", "lives_in", "Torino"),
            _fact(2, "Davide", "works_on", "Aura"),
        ]

    async def add_preference(self, **kwargs):
        self.stored_preferences.append(kwargs)

    async def get_facts_about(self, subject, *, user_identifier=None, **_kwargs):
        self.fact_calls.append(("exact", subject, user_identifier))
        return list(self._exact_facts)

    async def search_facts(self, *, query, limit, threshold=0.7, user_identifier=None):
        self.fact_calls.append(("semantic", query, limit))
        return list(self._semantic_facts)

    async def search_entities(self, **_kwargs):
        return [
            SimpleNamespace(
                id=UUID("20000000-0000-0000-0000-000000000001"),
                name="Davide",
                canonical_name="David",
                type=SimpleNamespace(value="PERSON"),
                description="operator",
                full_type="PERSON",
            )
        ]

    async def search_preferences(self, **_kwargs):
        return [
            SimpleNamespace(
                id=UUID("10000000-0000-0000-0000-000000000001"),
                category="food",
                preference="pasta",
                context=None,
            )
        ]


class _ShortTerm:
    def __init__(self, fail=False):
        self.fail = fail

    async def add_message(self, **_kwargs):
        if self.fail:
            raise RuntimeError("store failed")
        return SimpleNamespace(id="message-1")

    async def search_messages(self, **_kwargs):
        return [
            SimpleNamespace(
                id="message-1",
                role=SimpleNamespace(value="user"),
                content="hello",
                created_at=datetime(2026, 7, 26, tzinfo=timezone.utc),
                metadata={"similarity": 0.9},
            )
        ]


class _Reasoning:
    async def get_similar_traces(self, **_kwargs):
        return [SimpleNamespace(id="trace-1", task="task", outcome="ok", success=True)]


def _integration(*, fail_store=False, strategy=SessionStrategy.PERSISTENT, long_term=None):
    long_term = long_term if long_term is not None else _LongTerm()
    client = SimpleNamespace(
        graph=_Graph(),
        long_term=long_term,
        short_term=_ShortTerm(fail_store),
        reasoning=_Reasoning(),
        _client=SimpleNamespace(read_corpus_epoch=lambda: None),
        _settings=SimpleNamespace(embedding=None),
    )
    integration = MemoryIntegration(
        client=client,
        session_strategy=strategy,
        user_id="owner-1",
    )
    return integration, long_term


def test_session_strategies_and_explicit_hint():
    conversation, _ = _integration(strategy=SessionStrategy.PER_CONVERSATION)
    first = conversation.resolve_session_id()
    assert first == conversation.resolve_session_id()
    assert conversation.resolve_session_id("explicit") == "explicit"

    daily, _ = _integration(strategy=SessionStrategy.PER_DAY)
    assert daily.resolve_session_id().startswith("owner-1-")

    persistent, _ = _integration(strategy=SessionStrategy.PERSISTENT)
    assert persistent.resolve_session_id() == "owner-1"


def test_store_message_completes_without_post_return_tasks_and_reports_errors():
    integration, _ = _integration()

    async def exercise():
        before = asyncio.all_tasks()
        result = await integration.store_message(
            "user",
            "I dislike olives",
            user_identifier="owner-1",
        )
        leaked = asyncio.all_tasks() - before
        for task in leaked:
            task.cancel()
        await asyncio.gather(*leaked, return_exceptions=True)
        return result, leaked

    result, leaked = asyncio.run(exercise())
    assert result["id"] == "message-1"
    assert leaked == set()

    failing, _ = _integration(fail_store=True)
    with pytest.raises(AtomicMutationError, match="memory mutation failed"):
        asyncio.run(failing.store_message("user", "hello"))


def test_search_all_types_and_error_path():
    integration, _ = _integration()
    result = asyncio.run(
        integration.search(
            "profile",
            memory_types=["messages", "entities", "preferences", "traces"],
            user_identifier="owner-1",
        )
    )
    assert list(result["results"]) == ["messages", "entities", "preferences", "traces"]
    assert result["results"]["entities"][0]["name"] == "Davide"
    assert result["results"]["entities"][0]["canonical_name"] == "David"

    default_result = asyncio.run(integration.search("profile"))
    assert set(default_result["results"]) == {"messages", "entities", "preferences", "facts"}

    assert "unknown memory_types ['nope']" in asyncio.run(
        integration.search("profile", memory_types=["nope"])
    )["error"]

    async def fail(**_kwargs):
        raise RuntimeError("search failed")

    integration.client.short_term.search_messages = fail
    assert "search failed" in asyncio.run(integration.search("profile"))["error"]


def test_facts_bucket_puts_exact_subject_first_and_only_tops_up_to_the_limit():
    integration, long_term = _integration()

    result = asyncio.run(
        integration.search(
            "Davide",
            memory_types=["facts"],
            limit=10,
            user_identifier="owner-1",
        )
    )["results"]["facts"]

    # Exact subject leg runs first and scoped; the semantic leg only asks for the
    # remaining slots, and the fact both legs return is not counted twice.
    assert long_term.fact_calls == [
        ("exact", "Davide", "owner-1"),
        ("semantic", "Davide", 9),
    ]
    assert [f["object"] for f in result] == ["Torino", "Aura"]
    assert result[0]["predicate"] == "lives_in"

    filled = _LongTerm(
        exact_facts=[
            _fact(1, "Davide", "lives_in", "Torino"),
            _fact(2, "Davide", "works_on", "Aura"),
        ]
    )
    integration, _ = _integration(long_term=filled)
    single = asyncio.run(integration.search("Davide", memory_types=["facts"], limit=1))
    # Exact already fills the limit: no semantic call at all, and the surplus is cut.
    assert [call[0] for call in filled.fact_calls] == ["exact"]
    assert [f["object"] for f in single["results"]["facts"]] == ["Torino"]


def test_missing_revisions_and_empty_long_term_are_usable_but_ineligible():
    integration, _ = _integration()

    async def epoch():
        return 9

    integration.client._client.read_corpus_epoch = epoch
    async def empty(*_args, **_kwargs):
        return []

    integration.client.long_term.search_preferences = empty
    integration.client.long_term.search_entities = empty
    result = asyncio.run(
        integration.get_context(
            query="none",
            include_short_term=False,
            include_long_term=True,
            include_reasoning=False,
        )
    )

    assert result["context"] == ""
    assert result["has_context"] is False
    assert result["recall_metadata"]["adaptive_eligible"] is False
