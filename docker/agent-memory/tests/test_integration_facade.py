from __future__ import annotations

import asyncio
from types import SimpleNamespace

import pytest

from neo4j_agent_memory.integration import MemoryIntegration


class _LongTerm:
    def __init__(self):
        self.fail = False

    def _result(self, value):
        if self.fail:
            raise RuntimeError("facade failed")
        return value

    async def add_entity(self, **_kwargs):
        entity = SimpleNamespace(
            id="entity-1",
            name="Davide",
            canonical_name="David",
            type=SimpleNamespace(value="PERSON"),
        )
        dedup = SimpleNamespace(action="created", matched_entity_name=None, similarity_score=0.0)
        return self._result((entity, dedup))

    async def add_preference(self, **_kwargs):
        return self._result(SimpleNamespace(id="preference-1"))

    async def add_fact(self, **_kwargs):
        return self._result(SimpleNamespace(id="fact-1"))

    async def delete_preference(self, *_args, **_kwargs):
        return self._result("preference-1")

    async def delete_fact(self, *_args, **_kwargs):
        return self._result("fact-1")

    async def delete_entity(self, *_args, **_kwargs):
        return self._result(("entity-1", True))


def test_lifecycle_with_borrowed_client_and_unconnected_guard():
    borrowed = SimpleNamespace(long_term=_LongTerm())
    integration = MemoryIntegration(client=borrowed)
    asyncio.run(integration.connect())
    asyncio.run(integration.close())
    assert integration.client is borrowed

    unconnected = MemoryIntegration()
    with pytest.raises(RuntimeError, match="not connected"):
        _ = unconnected.client


def test_add_and_delete_facades_cover_success_absence_and_error():
    long_term = _LongTerm()
    integration = MemoryIntegration(client=SimpleNamespace(long_term=long_term))

    assert asyncio.run(integration.add_entity("Davide", "PERSON"))["deduplication"][
        "action"
    ] == "created"
    assert asyncio.run(integration.add_preference("food", "pasta"))["id"] == "preference-1"
    assert asyncio.run(integration.add_fact("Aura", "is", "ready"))["id"] == "fact-1"
    assert asyncio.run(
        integration.delete_preference("preference-1", user_identifier="owner")
    )["deleted"] == "preference-1"
    assert asyncio.run(integration.delete_fact("fact-1", user_identifier="owner"))[
        "deleted"
    ] == "fact-1"
    assert asyncio.run(integration.delete_entity("entity-1", user_identifier="owner"))[
        "removed_node"
    ] is True

    long_term.delete_preference = lambda *_args, **_kwargs: _async_value(None)
    long_term.delete_fact = lambda *_args, **_kwargs: _async_value(None)
    long_term.delete_entity = lambda *_args, **_kwargs: _async_value(None)
    assert "reason" in asyncio.run(
        integration.delete_preference("missing", user_identifier="owner")
    )
    assert "reason" in asyncio.run(integration.delete_fact("missing", user_identifier="owner"))
    assert "reason" in asyncio.run(
        integration.delete_entity("missing", user_identifier="owner")
    )

    long_term.fail = True
    long_term.delete_preference = _raise_async
    long_term.delete_fact = _raise_async
    long_term.delete_entity = _raise_async
    assert "error" in asyncio.run(integration.add_entity("Davide", "PERSON"))
    assert "error" in asyncio.run(integration.add_preference("food", "pasta"))
    assert "error" in asyncio.run(integration.add_fact("Aura", "is", "ready"))
    assert "error" in asyncio.run(
        integration.delete_preference("preference-1", user_identifier="owner")
    )
    assert "error" in asyncio.run(
        integration.delete_fact("fact-1", user_identifier="owner")
    )
    assert "error" in asyncio.run(
        integration.delete_entity("entity-1", user_identifier="owner")
    )
    assert "error" in asyncio.run(
        integration.update_preference("preference-1", user_identifier="owner")
    )
    assert "error" in asyncio.run(integration.update_fact("fact-1", user_identifier="owner"))
    assert "error" in asyncio.run(
        integration.update_entity("entity-1", user_identifier="owner")
    )


async def _async_value(value):
    return value


async def _raise_async(*_args, **_kwargs):
    raise RuntimeError("facade failed")
