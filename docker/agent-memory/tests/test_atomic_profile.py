from __future__ import annotations

import asyncio
import json
from types import SimpleNamespace

import pytest
from fastmcp import FastMCP

from neo4j_agent_memory.mcp._host_tools import register_host_tools


class _Graph:
    def __init__(self):
        self.events: list[tuple[str, str]] = []
        self.units = 0

    async def execute_write_unit(self, work):
        self.units += 1
        snapshot = list(self.events)
        try:
            return await work()
        except Exception:
            self.events = snapshot
            raise


class _Integration:
    def __init__(self, *, fail_predicate: str | None = None):
        self.client = SimpleNamespace(graph=_Graph())
        self.fail_predicate = fail_predicate

    async def add_entity(self, *, name, **_kwargs):
        self.client.graph.events.append(("entity", name))

    async def add_fact(self, *, predicate, **_kwargs):
        self.client.graph.events.append(("fact", predicate))
        if predicate == self.fail_predicate:
            raise RuntimeError("injected profile failure")

    async def add_preference(self, *, category, **_kwargs):
        self.client.graph.events.append(("preference", category))


def _ctx(integration):
    return SimpleNamespace(
        request_context=SimpleNamespace(lifespan_context={"integration": integration})
    )


def _tool():
    mcp = FastMCP("host-profile")
    register_host_tools(mcp)
    return asyncio.run(mcp.get_tools())["memory_store_profile"]


def _args():
    return {
        "entities": [{"name": "Davide", "entity_type": "PERSON"}],
        "facts": [{"subject": "Davide", "predicate": "role", "object_value": "founder"}],
        "preferences": [{"category": "language", "preference": "Italian"}],
        "completion_predicate": "onboarding_completed",
        "completion_value": "2026-07-29T00:00:00Z",
        "user_identifier": "owner-1",
    }


def test_profile_uses_one_transaction_for_every_item_and_sentinel():
    integration = _Integration()

    result = json.loads(asyncio.run(_tool().fn(_ctx(integration), **_args())))

    assert result == {
        "stored": True,
        "entities": 1,
        "facts": 1,
        "preferences": 1,
        "completion_predicate": "onboarding_completed",
    }
    assert integration.client.graph.units == 1
    assert integration.client.graph.events == [
        ("entity", "Davide"),
        ("fact", "role"),
        ("preference", "language"),
        ("fact", "onboarding_completed"),
    ]


def test_profile_failure_rolls_back_every_prior_item_and_sentinel():
    integration = _Integration(fail_predicate="role")

    with pytest.raises(RuntimeError, match="injected profile failure"):
        asyncio.run(_tool().fn(_ctx(integration), **_args()))

    assert integration.client.graph.units == 1
    assert integration.client.graph.events == []


def test_profile_rejects_invalid_input_before_opening_a_transaction():
    integration = _Integration()
    args = _args()
    args["completion_predicate"] = "arbitrary_fact"

    with pytest.raises(ValueError, match="unsupported profile completion"):
        asyncio.run(_tool().fn(_ctx(integration), **args))

    assert integration.client.graph.units == 0
    assert integration.client.graph.events == []
