from __future__ import annotations

import asyncio
import json
from types import SimpleNamespace

import pytest
from fastmcp import Client
from fastmcp.exceptions import ToolError
from fastmcp.tools.tool import ToolResult
from mcp.types import CallToolRequestParams, Tool
from starlette.applications import Starlette
from starlette.responses import PlainTextResponse
from starlette.routing import Route
from starlette.testclient import TestClient

from neo4j_agent_memory.mcp.capacity import (
    MAX_ARGUMENT_BYTES,
    MAX_HTTP_BODY_BYTES,
    MAX_RESPONSE_BYTES,
    MemoryCapacityMiddleware,
    RequestBodyLimitMiddleware,
    _encoded_size,
    capacity_http_middleware,
)
from neo4j_agent_memory.mcp.server import create_mcp_server


def _context(name: str, arguments: dict):
    message = CallToolRequestParams(name=name, arguments=arguments)
    return SimpleNamespace(message=message, copy=lambda **updates: SimpleNamespace(**updates))


def _envelope(error: ToolError) -> dict:
    return json.loads(str(error))


def test_capacity_advertises_quantitative_schema_limits():
    middleware = MemoryCapacityMiddleware()
    tool = Tool(
        name="memory_search",
        inputSchema={
            "type": "object",
            "properties": {
                "query": {"type": "string"},
                "limit": {"type": "integer"},
                "aliases": {"type": "array", "items": {"type": "string"}},
            },
        },
    )

    async def listed(_context):
        return [tool]

    bounded = asyncio.run(middleware.on_list_tools(SimpleNamespace(), listed))[0]
    assert bounded.inputSchema["maxProperties"] == 32
    assert bounded.inputSchema["properties"]["query"]["maxLength"] == 4096
    assert bounded.inputSchema["properties"]["limit"] == {
        "type": "integer",
        "minimum": 1,
        "maximum": 100,
    }
    assert bounded.inputSchema["properties"]["aliases"]["maxItems"] == 64


def test_server_lists_the_bounded_schema_over_the_real_mcp_path():
    async def inspect():
        server = create_mcp_server(None, profile="extended")
        async with Client(server) as client:
            tools = await client.list_tools()
        return next(tool for tool in tools if tool.name == "memory_search")

    tool = asyncio.run(inspect())
    assert tool.inputSchema["properties"]["query"]["maxLength"] == 4096
    assert tool.inputSchema["properties"]["limit"]["maximum"] == 100


def test_capacity_rejects_oversized_arguments_before_tool_execution():
    middleware = MemoryCapacityMiddleware()
    called = False

    async def call_next(_context):
        nonlocal called
        called = True
        return ToolResult("should not run")

    with pytest.raises(ToolError) as raised:
        asyncio.run(
            middleware.on_call_tool(
                _context(
                    "memory_store_message",
                    {"content": "x" * (MAX_ARGUMENT_BYTES + 1), "user_identifier": "alice"},
                ),
                call_next,
            )
        )

    assert called is False
    assert _envelope(raised.value)["code"] == "capacity_input_exceeded"


def test_capacity_rejects_oversized_results_with_unknown_effect():
    middleware = MemoryCapacityMiddleware()

    async def call_next(_context):
        return ToolResult("x" * (MAX_RESPONSE_BYTES + 1))

    with pytest.raises(ToolError) as raised:
        asyncio.run(
            middleware.on_call_tool(
                _context("memory_search", {"query": "q", "user_identifier": "alice"}),
                call_next,
            )
        )

    assert _envelope(raised.value)["code"] == "capacity_response_exceeded"
    assert _envelope(raised.value)["effect"] == "unknown"


def test_capacity_counts_fastmcp_two_tool_result_content():
    result = ToolResult("x" * (MAX_RESPONSE_BYTES + 1))

    assert _encoded_size(result) > MAX_RESPONSE_BYTES


def test_capacity_enforces_per_tenant_rate_limit():
    middleware = MemoryCapacityMiddleware(max_requests=1, window_seconds=60)

    async def call_next(_context):
        return ToolResult("ok")

    context = _context("memory_search", {"query": "q", "user_identifier": "alice"})
    asyncio.run(middleware.on_call_tool(context, call_next))
    with pytest.raises(ToolError) as raised:
        asyncio.run(middleware.on_call_tool(context, call_next))
    assert _envelope(raised.value)["code"] == "capacity_rate_exceeded"


def test_capacity_enforces_per_tenant_concurrency_without_leaking_a_slot():
    async def exercise():
        middleware = MemoryCapacityMiddleware(
            max_concurrent=1,
            max_global_concurrent=2,
            queue_timeout=0.01,
        )
        entered = asyncio.Event()
        release = asyncio.Event()

        async def slow(_context):
            entered.set()
            await release.wait()
            return ToolResult("ok")

        context = _context("memory_search", {"query": "q", "user_identifier": "alice"})
        first = asyncio.create_task(middleware.on_call_tool(context, slow))
        await entered.wait()
        with pytest.raises(ToolError) as raised:
            await middleware.on_call_tool(context, slow)
        assert _envelope(raised.value)["code"] == "capacity_concurrency_exceeded"
        release.set()
        await first

    asyncio.run(exercise())


def test_http_body_cap_rejects_content_length_before_route_execution():
    called = False

    async def endpoint(request):
        nonlocal called
        called = True
        return PlainTextResponse("ok")

    app = Starlette(
        routes=[Route("/mcp", endpoint, methods=["POST"])],
        middleware=capacity_http_middleware(),
    )
    with TestClient(app) as client:
        response = client.post("/mcp", content=b"x" * (MAX_HTTP_BODY_BYTES + 1))

    assert response.status_code == 413
    assert called is False


def test_http_body_cap_preserves_disconnect_after_replaying_the_body():
    upstream = [
        {"type": "http.request", "body": b"{}", "more_body": False},
        {"type": "http.disconnect"},
    ]
    upstream_calls = 0

    async def receive():
        nonlocal upstream_calls
        upstream_calls += 1
        return upstream.pop(0)

    async def downstream(_scope, wrapped_receive, _send):
        assert await wrapped_receive() == {
            "type": "http.request",
            "body": b"{}",
            "more_body": False,
        }
        assert await wrapped_receive() == {"type": "http.disconnect"}

    middleware = RequestBodyLimitMiddleware(downstream)
    asyncio.run(
        middleware(
            {
                "type": "http",
                "method": "POST",
                "headers": [(b"content-length", b"2")],
            },
            receive,
            lambda _message: None,
        )
    )

    assert upstream_calls == 2
