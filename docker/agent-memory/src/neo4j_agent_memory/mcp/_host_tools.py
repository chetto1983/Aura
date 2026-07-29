"""Host-only MCP operations used by Aura composition code."""

from __future__ import annotations

import json
from typing import TYPE_CHECKING, Any

from fastmcp import Context

from neo4j_agent_memory.mcp._common import get_integration

if TYPE_CHECKING:
    from fastmcp import FastMCP

_MAX_PROFILE_ITEMS = 128
_COMPLETION_PREDICATES = frozenset({"onboarding_completed", "onboarding_skipped"})


def _text(item: dict[str, Any], field: str) -> str:
    value = item.get(field)
    if not isinstance(value, str) or not value.strip():
        raise ValueError(f"profile {field} must be a non-empty string")
    return value


def _validate_profile(
    entities: list[dict[str, Any]],
    facts: list[dict[str, Any]],
    preferences: list[dict[str, Any]],
) -> None:
    if len(entities) + len(facts) + len(preferences) > _MAX_PROFILE_ITEMS:
        raise ValueError(f"profile exceeds {_MAX_PROFILE_ITEMS} items")
    for entity in entities:
        _text(entity, "name")
        _text(entity, "entity_type")
    for fact in facts:
        _text(fact, "subject")
        _text(fact, "predicate")
        _text(fact, "object_value")
    for preference in preferences:
        _text(preference, "category")
        _text(preference, "preference")


def register_host_tools(mcp: FastMCP) -> None:
    """Register operations served for trusted host callers but hidden by Aura."""

    @mcp.tool()
    async def memory_store_profile(
        ctx: Context,
        entities: list[dict[str, Any]],
        facts: list[dict[str, Any]],
        preferences: list[dict[str, Any]],
        completion_predicate: str,
        completion_value: str,
        user_identifier: str,
    ) -> str:
        """Atomically persist an Aura onboarding profile and completion marker."""
        if not user_identifier.strip():
            raise ValueError("profile user_identifier must be a non-empty string")
        if completion_predicate not in _COMPLETION_PREDICATES:
            raise ValueError("unsupported profile completion predicate")
        if not completion_value.strip():
            raise ValueError("profile completion value must be a non-empty string")
        _validate_profile(entities, facts, preferences)

        integration = get_integration(ctx)

        async def store() -> dict[str, Any]:
            for entity in entities:
                await integration.add_entity(
                    name=_text(entity, "name"),
                    entity_type=_text(entity, "entity_type"),
                    description=entity.get("description"),
                    aliases=entity.get("aliases"),
                    user_identifier=user_identifier,
                )
            for fact in facts:
                await integration.add_fact(
                    subject=_text(fact, "subject"),
                    predicate=_text(fact, "predicate"),
                    object_value=_text(fact, "object_value"),
                    user_identifier=user_identifier,
                )
            for preference in preferences:
                await integration.add_preference(
                    category=_text(preference, "category"),
                    preference=_text(preference, "preference"),
                    context=preference.get("context"),
                    user_identifier=user_identifier,
                )
            await integration.add_fact(
                subject=user_identifier,
                predicate=completion_predicate,
                object_value=completion_value,
                user_identifier=user_identifier,
            )
            return {
                "stored": True,
                "entities": len(entities),
                "facts": len(facts),
                "preferences": len(preferences),
                "completion_predicate": completion_predicate,
            }

        result = await integration.client.graph.execute_write_unit(store)
        return json.dumps(result)
