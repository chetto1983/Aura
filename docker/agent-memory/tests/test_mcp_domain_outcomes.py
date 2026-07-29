from __future__ import annotations

import asyncio
import json
from types import SimpleNamespace

import pytest
from fastmcp.exceptions import ToolError
from fastmcp.tools.tool import ToolResult

from neo4j_agent_memory.mcp.outcomes import (
    AuraDomainOutcomeMiddleware,
    MemoryDomainError,
    render_result,
)


def _middleware_context():
    return SimpleNamespace()


def test_legacy_error_dictionary_is_not_serialized_as_success():
    with pytest.raises(MemoryDomainError) as raised:
        render_result({"error": "database password must not cross the boundary"})

    assert raised.value.effect == "unknown"
    assert "password" not in raised.value.envelope()


@pytest.mark.parametrize(
    ("error", "outcome", "effect"),
    [
        (ValueError("bad field"), "rejected", "none"),
        (RuntimeError("secret internal detail"), "error", "unknown"),
    ],
)
def test_middleware_emits_bounded_typed_tool_errors(error, outcome, effect):
    middleware = AuraDomainOutcomeMiddleware()

    async def fail(_context):
        raise error

    with pytest.raises(ToolError) as raised:
        asyncio.run(middleware.on_call_tool(_middleware_context(), fail))

    envelope = json.loads(str(raised.value))
    assert envelope["outcome"] == outcome
    assert envelope["effect"] == effect
    assert set(envelope) == {"code", "effect", "message", "outcome"}
    assert len(str(raised.value).encode()) < 2048
    if outcome == "error":
        assert "secret internal detail" not in envelope["message"]


def test_middleware_converts_legacy_error_tool_result_to_unknown_failure():
    middleware = AuraDomainOutcomeMiddleware()

    async def legacy_failure(_context):
        return ToolResult('{"error":"database password must not cross the boundary"}')

    with pytest.raises(ToolError) as raised:
        asyncio.run(middleware.on_call_tool(_middleware_context(), legacy_failure))

    envelope = json.loads(str(raised.value))
    assert envelope == {
        "code": "memory_operation_failed",
        "effect": "unknown",
        "message": "memory operation failed",
        "outcome": "error",
    }
