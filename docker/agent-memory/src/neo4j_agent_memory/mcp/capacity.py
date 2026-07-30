"""Quantitative capacity boundary for the Agent Memory MCP service."""

from __future__ import annotations

import asyncio
import copy
import json
import time
from collections import OrderedDict, deque
from dataclasses import dataclass, field
from typing import Any

from fastmcp.exceptions import ToolError
from fastmcp.server.dependencies import get_access_token
from fastmcp.server.middleware import Middleware, MiddlewareContext
from mcp.types import CallToolRequestParams, ListToolsRequest
from starlette.middleware import Middleware as ASGIMiddleware

from neo4j_agent_memory.mcp.outcomes import outcome_json

MAX_HTTP_BODY_BYTES = 256 * 1024
MAX_ARGUMENT_BYTES = 128 * 1024
MAX_RESPONSE_BYTES = 1024 * 1024

_MAX_ARGUMENT_PROPERTIES = 32
_MAX_COLLECTION_ITEMS = 64
_MAX_OBJECT_PROPERTIES = 64
_MAX_NESTING_DEPTH = 8
_MAX_ARGUMENT_NODES = 4096
_MAX_TENANT_STATES = 4096

_STRING_LIMITS = {
    "query": 4096,
    "content": 65536,
    "description": 8192,
    "context": 8192,
    "preference": 8192,
    "object_value": 8192,
    "subject": 512,
    "predicate": 256,
    "name": 512,
    "entity_type": 64,
    "subtype": 128,
    "category": 128,
    "role": 16,
    "node_type": 32,
    "node_id": 128,
    "source_id": 128,
    "target_id": 128,
    "relationship_type": 128,
    "session_id": 256,
    "user_identifier": 256,
    "valid_from": 64,
    "valid_until": 64,
}
_DEFAULT_STRING_BYTES = 8192
_LIMIT_FIELDS = {"limit", "max_items"}
_OFFSET_FIELDS = {"offset"}
_PROBABILITY_FIELDS = {"threshold", "confidence"}


class CapacityViolation(ValueError):
    """One quantitative request constraint was exceeded."""


@dataclass
class _TenantState:
    concurrency: asyncio.Semaphore
    requests: deque[float] = field(default_factory=deque)
    last_seen: float = 0.0
    in_flight: int = 0


class MemoryCapacityMiddleware(Middleware):
    """Bound schema, input, rate, concurrency, and output per authenticated tenant."""

    def __init__(
        self,
        *,
        max_requests: int = 120,
        window_seconds: float = 60,
        max_concurrent: int = 4,
        max_global_concurrent: int = 32,
        queue_timeout: float = 0.25,
    ):
        self._max_requests = max_requests
        self._window_seconds = window_seconds
        self._max_concurrent = max_concurrent
        self._queue_timeout = queue_timeout
        self._global = asyncio.Semaphore(max_global_concurrent)
        self._states: OrderedDict[str, _TenantState] = OrderedDict()
        self._states_lock = asyncio.Lock()

    async def on_list_tools(
        self,
        context: MiddlewareContext[ListToolsRequest],
        call_next: Any,
    ) -> list[Any]:
        tools = await call_next(context)
        return [_bounded_tool_schema(tool) for tool in tools]

    async def on_call_tool(
        self,
        context: MiddlewareContext[CallToolRequestParams],
        call_next: Any,
    ) -> Any:
        arguments = dict(context.message.arguments or {})
        try:
            _validate_arguments(arguments)
        except CapacityViolation as exc:
            raise _capacity_error(
                "capacity_input_exceeded",
                str(exc),
                effect="none",
            ) from exc

        tenant = _tenant_key(arguments)
        state = await self._state_for(tenant)
        await self._consume_rate(state)
        await self._pin(state)
        acquired_global = False
        acquired_tenant = False
        try:
            acquired_global = await self._acquire(self._global)
            acquired_tenant = await self._acquire(state.concurrency)
            result = await call_next(context)
            if _encoded_size(result) > MAX_RESPONSE_BYTES:
                raise _capacity_error(
                    "capacity_response_exceeded",
                    "memory tool response exceeds the 1048576-byte limit",
                    effect="unknown",
                )
            return result
        finally:
            if acquired_tenant:
                state.concurrency.release()
            if acquired_global:
                self._global.release()
            await self._unpin(state)

    async def _state_for(self, tenant: str) -> _TenantState:
        now = time.monotonic()
        async with self._states_lock:
            state = self._states.get(tenant)
            if state is None:
                if len(self._states) >= _MAX_TENANT_STATES:
                    evicted = False
                    for candidate, old in list(self._states.items()):
                        if old.in_flight == 0:
                            del self._states[candidate]
                            evicted = True
                            break
                    if not evicted:
                        raise _capacity_error(
                            "capacity_tenant_registry_exceeded",
                            "memory tool tenant capacity is exhausted",
                            effect="none",
                        )
                state = _TenantState(asyncio.Semaphore(self._max_concurrent))
                self._states[tenant] = state
            state.last_seen = now
            self._states.move_to_end(tenant)
            return state

    async def _pin(self, state: _TenantState) -> None:
        async with self._states_lock:
            state.in_flight += 1

    async def _unpin(self, state: _TenantState) -> None:
        async with self._states_lock:
            state.in_flight -= 1

    async def _consume_rate(self, state: _TenantState) -> None:
        now = time.monotonic()
        async with self._states_lock:
            cutoff = now - self._window_seconds
            while state.requests and state.requests[0] <= cutoff:
                state.requests.popleft()
            if len(state.requests) >= self._max_requests:
                raise _capacity_error(
                    "capacity_rate_exceeded",
                    "memory tool tenant rate limit exceeded",
                    effect="none",
                )
            state.requests.append(now)

    async def _acquire(self, semaphore: asyncio.Semaphore) -> bool:
        try:
            await asyncio.wait_for(semaphore.acquire(), timeout=self._queue_timeout)
            return True
        except TimeoutError as exc:
            raise _capacity_error(
                "capacity_concurrency_exceeded",
                "memory tool concurrency limit exceeded",
                effect="none",
            ) from exc


def _capacity_error(code: str, message: str, *, effect: str) -> ToolError:
    return ToolError(
        outcome_json(
            "rejected" if effect == "none" else "error",
            code=code,
            message=message,
            effect=effect,  # type: ignore[arg-type]
        )
    )


def _tenant_key(arguments: dict[str, Any]) -> str:
    try:
        token = get_access_token()
    except RuntimeError:
        token = None
    claims = token.claims if token is not None else {}
    subject = str(claims.get("sub", "")).strip()
    if subject:
        return subject[:256]
    return str(arguments.get("user_identifier") or "anonymous")[:256]


def _validate_arguments(arguments: dict[str, Any]) -> None:
    if len(arguments) > _MAX_ARGUMENT_PROPERTIES:
        raise CapacityViolation("memory tool accepts at most 32 arguments")
    state = {"bytes": 0, "nodes": 0}
    _validate_value(arguments, state, depth=0, field_name="")
    if state["bytes"] > MAX_ARGUMENT_BYTES:
        raise CapacityViolation("memory tool arguments exceed the 131072-byte limit")


def _validate_value(
    value: Any,
    state: dict[str, int],
    *,
    depth: int,
    field_name: str,
) -> None:
    state["nodes"] += 1
    if state["nodes"] > _MAX_ARGUMENT_NODES:
        raise CapacityViolation("memory tool arguments contain too many values")
    if depth > _MAX_NESTING_DEPTH:
        raise CapacityViolation("memory tool arguments exceed nesting depth 8")
    if isinstance(value, str):
        encoded = len(value.encode("utf-8", errors="replace"))
        state["bytes"] += encoded
        limit = _STRING_LIMITS.get(field_name, _DEFAULT_STRING_BYTES)
        if encoded > limit:
            raise CapacityViolation(f"{field_name or 'string'} exceeds its {limit}-byte limit")
        return
    if isinstance(value, dict):
        if len(value) > _MAX_OBJECT_PROPERTIES:
            raise CapacityViolation("memory tool object exceeds 64 properties")
        for key, item in value.items():
            if not isinstance(key, str):
                raise CapacityViolation("memory tool object keys must be strings")
            state["bytes"] += len(key.encode("utf-8", errors="replace"))
            _validate_value(item, state, depth=depth + 1, field_name=key)
        return
    if isinstance(value, list):
        if len(value) > _MAX_COLLECTION_ITEMS:
            raise CapacityViolation("memory tool collection exceeds 64 items")
        for item in value:
            _validate_value(item, state, depth=depth + 1, field_name=field_name)
        return
    if isinstance(value, int) and not isinstance(value, bool):
        if field_name in _LIMIT_FIELDS and not 1 <= value <= 100:
            raise CapacityViolation(f"{field_name} must be between 1 and 100")
        if field_name in _OFFSET_FIELDS and not 0 <= value <= 10_000:
            raise CapacityViolation(f"{field_name} must be between 0 and 10000")
    if isinstance(value, (int, float)) and field_name in _PROBABILITY_FIELDS:
        if not 0 <= value <= 1:
            raise CapacityViolation(f"{field_name} must be between 0 and 1")


def _bounded_tool_schema(tool: Any) -> Any:
    schema = copy.deepcopy(
        tool.parameters if hasattr(tool, "parameters") else tool.inputSchema
    )
    schema["maxProperties"] = _MAX_ARGUMENT_PROPERTIES
    for name, property_schema in schema.get("properties", {}).items():
        _bound_property(name, property_schema)
    field = "parameters" if hasattr(tool, "parameters") else "inputSchema"
    return tool.model_copy(update={field: schema})


def _bound_property(name: str, schema: dict[str, Any]) -> None:
    variants = schema.get("anyOf")
    if isinstance(variants, list):
        for variant in variants:
            if isinstance(variant, dict):
                _bound_property(name, variant)
    kind = schema.get("type")
    if kind == "string":
        schema["maxLength"] = _STRING_LIMITS.get(name, _DEFAULT_STRING_BYTES)
    elif kind == "array":
        schema["maxItems"] = _MAX_COLLECTION_ITEMS
    elif kind == "object":
        schema["maxProperties"] = _MAX_OBJECT_PROPERTIES
    elif kind == "integer":
        if name in _LIMIT_FIELDS:
            schema.update({"minimum": 1, "maximum": 100})
        elif name in _OFFSET_FIELDS:
            schema.update({"minimum": 0, "maximum": 10_000})
    elif kind == "number" and name in _PROBABILITY_FIELDS:
        schema.update({"minimum": 0, "maximum": 1})


def _encoded_size(value: Any) -> int:
    if hasattr(value, "model_dump"):
        value = value.model_dump(mode="json")
    elif hasattr(value, "content") or hasattr(value, "structured_content"):
        # FastMCP 2.x ToolResult is a plain Python object, while FastMCP 3.x
        # exposes a serializable model. Measuring repr(ToolResult) under 2.x
        # only counted the object address and let arbitrarily large responses
        # bypass the production cap.
        value = {
            "content": _jsonable(getattr(value, "content", None)),
            "structuredContent": _jsonable(
                getattr(value, "structured_content", None)
            ),
            "_meta": _jsonable(getattr(value, "meta", None)),
        }
    return len(json.dumps(value, default=str, separators=(",", ":")).encode("utf-8"))


def _jsonable(value: Any) -> Any:
    if hasattr(value, "model_dump"):
        return value.model_dump(mode="json")
    if isinstance(value, dict):
        return {str(key): _jsonable(item) for key, item in value.items()}
    if isinstance(value, (list, tuple)):
        return [_jsonable(item) for item in value]
    return value


class RequestBodyLimitMiddleware:
    """ASGI pre-parser cap that never buffers more than MAX_HTTP_BODY_BYTES."""

    def __init__(self, app: Any, max_bytes: int = MAX_HTTP_BODY_BYTES):
        self._app = app
        self._max_bytes = max_bytes

    async def __call__(self, scope: dict, receive: Any, send: Any) -> None:
        if scope.get("type") != "http" or scope.get("method") not in {
            "POST",
            "PUT",
            "PATCH",
        }:
            await self._app(scope, receive, send)
            return
        headers = {key.lower(): value for key, value in scope.get("headers", [])}
        try:
            content_length = int(headers.get(b"content-length", b"0"))
        except ValueError:
            await self._reject(send)
            return
        if content_length > self._max_bytes:
            await self._reject(send)
            return

        messages = []
        total = 0
        while True:
            message = await receive()
            messages.append(message)
            if message.get("type") == "http.request":
                total += len(message.get("body", b""))
                if total > self._max_bytes:
                    await self._reject(send)
                    return
                if not message.get("more_body", False):
                    break
            else:
                break

        async def replay() -> dict:
            if messages:
                return messages.pop(0)
            return await receive()

        await self._app(scope, replay, send)

    @staticmethod
    async def _reject(send: Any) -> None:
        body = b'{"error":"request body exceeds 262144-byte limit"}'
        await send(
            {
                "type": "http.response.start",
                "status": 413,
                "headers": [
                    (b"content-type", b"application/json"),
                    (b"content-length", str(len(body)).encode()),
                ],
            }
        )
        await send({"type": "http.response.body", "body": body})


def capacity_http_middleware() -> list[ASGIMiddleware]:
    return [ASGIMiddleware(RequestBodyLimitMiddleware, max_bytes=MAX_HTTP_BODY_BYTES)]
