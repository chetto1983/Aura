from __future__ import annotations

import asyncio
import base64
import hashlib
import hmac
import json
import time

import pytest
from fastmcp import FastMCP
from starlette.testclient import TestClient

from neo4j_agent_memory.mcp.auth import build_aura_auth
from neo4j_agent_memory.mcp.server import create_mcp_server

SECRET = "tenant-auth-test-secret-that-is-at-least-32-bytes"


def _segment(value: dict) -> str:
    encoded = json.dumps(value, separators=(",", ":")).encode()
    return base64.urlsafe_b64encode(encoded).rstrip(b"=").decode()


def _token(secret: str = SECRET, **overrides: object) -> str:
    now = int(time.time())
    claims: dict[str, object] = {
        "sub": "aura-service",
        "iss": "aura",
        "aud": "agent-memory",
        "scope": "memory:access",
        "iat": now,
        "exp": now + 60,
    }
    claims.update(overrides)
    message = f"{_segment({'alg': 'HS256', 'typ': 'JWT'})}.{_segment(claims)}"
    signature = hmac.new(secret.encode(), message.encode(), hashlib.sha256).digest()
    return f"{message}.{base64.urlsafe_b64encode(signature).rstrip(b'=').decode()}"


def _headers(token: str) -> dict[str, str]:
    return {
        "Accept": "application/json, text/event-stream",
        "Authorization": f"Bearer {token}",
        "Content-Type": "application/json",
    }


def _initialize(client: TestClient, token: str):
    return client.post(
        "/mcp",
        headers=_headers(token),
        json={
            "jsonrpc": "2.0",
            "id": 1,
            "method": "initialize",
            "params": {
                "protocolVersion": "2025-06-18",
                "capabilities": {},
                "clientInfo": {"name": "tenant-auth-test", "version": "1"},
            },
        },
    )


def _call_headers(token: str) -> dict[str, str]:
    headers = _headers(token)
    headers["MCP-Protocol-Version"] = "2025-06-18"
    return headers


@pytest.mark.parametrize(
    "token",
    [
        "",
        "not-a-jwt",
        _token("wrong-signing-secret-that-is-also-at-least-32-bytes"),
        _token(iss="attacker"),
        _token(aud="other-service"),
        _token(scope="other:scope"),
        _token(exp=1),
    ],
)
def test_http_auth_rejects_invalid_bearers_before_initialization(token: str):
    auth, middleware = build_aura_auth(SECRET)
    server = FastMCP("tenant-auth-test", auth=auth, middleware=[middleware])

    with TestClient(server.http_app(path="/mcp", stateless_http=True)) as client:
        response = _initialize(client, token)

    assert response.status_code in {401, 403}
    assert "Mcp-Session-Id" not in response.headers


def test_tool_identity_is_bound_to_signed_subject_not_payload():
    auth, middleware = build_aura_auth(SECRET)
    server = FastMCP("tenant-auth-test", auth=auth, middleware=[middleware])

    @server.tool
    def who(user_identifier: str | None = None) -> str:
        return user_identifier or "missing"

    service_token = _token()
    with TestClient(server.http_app(path="/mcp", stateless_http=True)) as client:
        initialized = _initialize(client, service_token)
        assert initialized.status_code == 200

        service_call = client.post(
            "/mcp",
            headers=_call_headers(service_token),
            json={
                "jsonrpc": "2.0",
                "id": 2,
                "method": "tools/call",
                "params": {"name": "who", "arguments": {"user_identifier": "mallory"}},
            },
        )
        alice_call = client.post(
            "/mcp",
            headers=_call_headers(_token(sub="alice")),
            json={
                "jsonrpc": "2.0",
                "id": 3,
                "method": "tools/call",
                "params": {"name": "who", "arguments": {"user_identifier": "mallory"}},
            },
        )

    assert service_call.status_code == 200
    assert '"isError":true' in service_call.text
    assert alice_call.status_code == 200
    assert '"text":"alice"' in alice_call.text
    assert '"text":"mallory"' not in alice_call.text


def test_authenticated_agent_memory_server_has_no_unscoped_resources():
    async def inspect_server() -> tuple[list, list, list]:
        server = create_mcp_server(None, profile="extended", auth_secret=SECRET)
        return (
            await server.get_resources(),
            await server.get_resource_templates(),
            await server.get_tools(),
        )

    resources, templates, tools = asyncio.run(inspect_server())
    assert resources == {}
    assert templates == {}
    assert len(tools) == 13


def test_auth_secret_must_be_cryptographically_nontrivial():
    with pytest.raises(ValueError, match="at least 32 bytes"):
        build_aura_auth("too-short")
