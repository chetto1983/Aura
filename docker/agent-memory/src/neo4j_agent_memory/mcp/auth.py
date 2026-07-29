"""Authentication and tenant binding for Aura's Agent Memory MCP."""

from __future__ import annotations

from typing import Any

from fastmcp.exceptions import ToolError
from fastmcp.server.auth.providers.jwt import JWTVerifier
from fastmcp.server.dependencies import get_access_token
from fastmcp.server.middleware import Middleware, MiddlewareContext
from mcp.types import CallToolRequestParams

AURA_SERVICE_SUBJECT = "aura-service"
JWT_AUDIENCE = "agent-memory"
JWT_ISSUER = "aura"
JWT_SCOPE = "memory:access"


class AuraIdentityMiddleware(Middleware):
    """Replace payload scope with the identity authenticated by Aura."""

    async def on_call_tool(
        self,
        context: MiddlewareContext[CallToolRequestParams],
        call_next: Any,
    ) -> Any:
        token = get_access_token()
        claims = token.claims if token is not None else {}
        subject = str(claims.get("sub", "")).strip()
        if not subject or subject == AURA_SERVICE_SUBJECT:
            raise ToolError("an Aura tenant delegation token is required for memory tools")

        arguments = dict(context.message.arguments or {})
        arguments["user_identifier"] = subject
        message = context.message.model_copy(update={"arguments": arguments})
        return await call_next(context.copy(message=message))


def build_aura_auth(secret: str) -> tuple[JWTVerifier, AuraIdentityMiddleware]:
    """Build the fail-closed verifier and per-tool identity binder."""
    if len(secret.encode()) < 32:
        raise ValueError("Agent Memory auth secret must be at least 32 bytes")
    verifier = JWTVerifier(
        public_key=secret,
        algorithm="HS256",
        issuer=JWT_ISSUER,
        audience=JWT_AUDIENCE,
        required_scopes=[JWT_SCOPE],
    )
    return verifier, AuraIdentityMiddleware()
