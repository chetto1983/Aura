from __future__ import annotations

import asyncio
from datetime import datetime, timezone
from types import SimpleNamespace

from neo4j_agent_memory.integration import MemoryIntegration


class _LongTerm:
    def __init__(self):
        self.kwargs = None

    async def add_fact(self, **kwargs):
        self.kwargs = kwargs
        return SimpleNamespace(id="fact-1")


class _Graph:
    async def execute_write_unit(self, work):
        return await work()


def test_integration_parses_iso_fact_validity_before_storage():
    long_term = _LongTerm()
    integration = MemoryIntegration(client=SimpleNamespace(long_term=long_term, graph=_Graph()))

    result = asyncio.run(
        integration.add_fact(
            "Aura",
            "released",
            "today",
            valid_from="2026-07-26T10:00:00Z",
            valid_until="2026-07-27T10:00:00+00:00",
        )
    )

    assert result["stored"] is True
    assert long_term.kwargs["valid_from"] == datetime(2026, 7, 26, 10, 0, tzinfo=timezone.utc)
    assert long_term.kwargs["valid_until"] == datetime(2026, 7, 27, 10, 0, tzinfo=timezone.utc)
