from __future__ import annotations

import asyncio
from datetime import datetime, timezone
from types import SimpleNamespace
from uuid import UUID

from neo4j_agent_memory.integration import MemoryIntegration, SessionStrategy


class _LongTerm:
    def __init__(self):
        self.stored_preferences = []

    async def add_preference(self, **kwargs):
        self.stored_preferences.append(kwargs)

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


class _Observer:
    def __init__(self):
        self.messages = []

    async def on_message_stored(self, **kwargs):
        self.messages.append(kwargs)


class _Detector:
    def detect(self, _content):
        return [
            SimpleNamespace(
                sentiment="negative",
                category="food",
                preference="olives",
                source_text="I dislike olives",
                confidence=0.8,
            )
        ]


class _Reasoning:
    async def get_similar_traces(self, **_kwargs):
        return [SimpleNamespace(id="trace-1", task="task", outcome="ok", success=True)]


def _integration(*, fail_store=False, strategy=SessionStrategy.PERSISTENT):
    long_term = _LongTerm()
    client = SimpleNamespace(
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
        auto_preferences=True,
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


def test_store_message_runs_existing_background_paths_and_reports_errors():
    integration, long_term = _integration()
    integration._preference_detector = _Detector()
    observer = _Observer()
    integration.observer = observer

    async def exercise():
        result = await integration.store_message(
            "user",
            "I dislike olives",
            user_identifier="owner-1",
        )
        await asyncio.sleep(0)
        return result

    result = asyncio.run(exercise())
    assert result["id"] == "message-1"
    assert long_term.stored_preferences[0]["preference"] == "Dislikes: olives"
    assert observer.messages[0]["message_id"] == "message-1"
    assert integration.observer is observer

    failing, _ = _integration(fail_store=True)
    assert "store failed" in asyncio.run(failing.store_message("user", "hello"))["error"]


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
    assert set(default_result["results"]) == {"messages", "entities", "preferences"}

    async def fail(**_kwargs):
        raise RuntimeError("search failed")

    integration.client.short_term.search_messages = fail
    assert "search failed" in asyncio.run(integration.search("profile"))["error"]


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
