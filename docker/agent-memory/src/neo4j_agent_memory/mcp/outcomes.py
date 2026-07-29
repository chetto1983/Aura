"""Bounded domain-outcome errors for the Aura MCP boundary."""

from __future__ import annotations

import json
import logging
from typing import Any, Literal

from fastmcp.exceptions import ToolError
from fastmcp.server.middleware import Middleware, MiddlewareContext
from mcp.types import CallToolRequestParams

from neo4j_agent_memory.memory.atomic import AtomicMutationError

logger = logging.getLogger(__name__)

Outcome = Literal["rejected", "partial", "error"]
Effect = Literal["none", "partial", "unknown"]

_MAX_CODE_BYTES = 96
_MAX_MESSAGE_BYTES = 1024


def _bounded(value: str, limit: int) -> str:
    encoded = value.strip().encode("utf-8", errors="replace")
    if len(encoded) <= limit:
        return encoded.decode("utf-8")
    return encoded[:limit].decode("utf-8", errors="ignore")


def outcome_json(
    outcome: Outcome,
    *,
    code: str,
    message: str,
    effect: Effect,
) -> str:
    """Return the stable, bounded JSON carried by MCP ``isError=true``."""
    return json.dumps(
        {
            "outcome": outcome,
            "code": _bounded(code, _MAX_CODE_BYTES),
            "message": _bounded(message, _MAX_MESSAGE_BYTES),
            "effect": effect,
        },
        separators=(",", ":"),
        sort_keys=True,
    )


class MemoryDomainError(RuntimeError):
    """A classified memory failure that the MCP boundary must not flatten."""

    def __init__(
        self,
        *,
        outcome: Outcome = "error",
        code: str = "memory_operation_failed",
        message: str = "memory operation failed",
        effect: Effect = "unknown",
    ):
        super().__init__(message)
        self.outcome = outcome
        self.code = code
        self.effect = effect

    def envelope(self) -> str:
        return outcome_json(
            self.outcome,
            code=self.code,
            message=str(self),
            effect=self.effect,
        )


def render_result(result: Any) -> str:
    """Serialize success, but convert legacy error dictionaries to failure."""
    if isinstance(result, dict) and result.get("error"):
        raise MemoryDomainError(
            code="memory_operation_failed",
            message="memory operation failed",
            effect="unknown",
        )
    return json.dumps(result, default=str)


def _legacy_error_payload(value: Any) -> bool:
    if isinstance(value, dict):
        return bool(value.get("error"))
    if not isinstance(value, str):
        return False
    try:
        parsed = json.loads(value)
    except (TypeError, ValueError):
        return False
    return isinstance(parsed, dict) and bool(parsed.get("error"))


def _legacy_error_result(result: Any) -> bool:
    if _legacy_error_payload(result):
        return True
    if _legacy_error_payload(getattr(result, "structured_content", None)):
        return True
    return any(
        _legacy_error_payload(getattr(block, "text", None))
        for block in (getattr(result, "content", None) or [])
    )


def _already_typed(message: str) -> bool:
    try:
        parsed = json.loads(message)
    except (TypeError, ValueError):
        return False
    return (
        isinstance(parsed, dict)
        and parsed.get("outcome") in {"rejected", "partial", "error"}
        and parsed.get("effect") in {"none", "partial", "unknown"}
    )


class AuraDomainOutcomeMiddleware(Middleware):
    """Make every tool-domain failure an MCP error with a stable envelope."""

    async def on_call_tool(
        self,
        context: MiddlewareContext[CallToolRequestParams],
        call_next: Any,
    ) -> Any:
        try:
            result = await call_next(context)
            if _legacy_error_result(result):
                raise ToolError(
                    outcome_json(
                        "error",
                        code="memory_operation_failed",
                        message="memory operation failed",
                        effect="unknown",
                    )
                )
            return result
        except AtomicMutationError as exc:
            raise ToolError(
                outcome_json(
                    "error",
                    code="memory_mutation_failed",
                    message="memory mutation failed",
                    effect="none",
                )
            ) from exc
        except MemoryDomainError as exc:
            raise ToolError(exc.envelope()) from exc
        except ToolError as exc:
            message = str(exc)
            if _already_typed(message):
                raise
            raise ToolError(
                outcome_json(
                    "rejected",
                    code="tool_rejected",
                    message=message,
                    effect="none",
                )
            ) from exc
        except ValueError as exc:
            raise ToolError(
                outcome_json(
                    "rejected",
                    code="invalid_argument",
                    message=str(exc),
                    effect="none",
                )
            ) from exc
        except Exception as exc:
            logger.exception("unclassified Agent Memory tool failure")
            raise ToolError(
                outcome_json(
                    "error",
                    code="memory_internal_error",
                    message="memory operation failed",
                    effect="unknown",
                )
            ) from exc
