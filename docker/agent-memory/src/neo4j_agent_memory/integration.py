"""High-level convenience layer for memory operations.

Provides a simplified interface over MemoryClient that both the MCP server
and create-context-graph templates can consume. Handles session identity
resolution, automatic entity extraction, and preference detection.

Example:
    from neo4j_agent_memory import MemoryIntegration

    async with MemoryIntegration(
        neo4j_uri="bolt://localhost:7687",
        neo4j_password="password",
    ) as memory:
        await memory.store_message("user", "I love Italian food")
        context = await memory.get_context()
"""

from __future__ import annotations

import logging
from datetime import datetime
from typing import TYPE_CHECKING, Any

from neo4j_agent_memory.integration_context import (
    ContextSearchMixin,
    SessionStrategy,
    entity_name_fields,
)

if TYPE_CHECKING:
    from neo4j_agent_memory import MemoryClient
    from neo4j_agent_memory.mcp._observer import MemoryObserver

logger = logging.getLogger(__name__)


def _entity_ref_name(ref: Any) -> str:
    """Name of an applies_to target, whether it arrived as a string or an EntityRef.

    Echoed back so the caller can see which entities the preference was actually linked
    to, instead of having to trust that the link happened.
    """
    return ref if isinstance(ref, str) else str(getattr(ref, "name", ref))


def _parse_iso_datetime(value: str | None) -> datetime | None:
    if value is None:
        return None
    return datetime.fromisoformat(value.replace("Z", "+00:00"))


class MemoryIntegration(ContextSearchMixin):
    """High-level convenience wrapper over MemoryClient.

    Provides a simplified interface for the three most common operations:
    store a message (with automatic extraction), retrieve assembled context,
    and manage entities/preferences/facts.

    Can be constructed with either a pre-connected MemoryClient or connection
    parameters (in which case it manages the client lifecycle internally).

    Args:
        client: Pre-connected MemoryClient instance. If provided, the caller
            is responsible for its lifecycle.
        neo4j_uri: Neo4j connection URI (used if client is None).
        neo4j_password: Neo4j password (used if client is None).
        neo4j_user: Neo4j username (default: "neo4j").
        neo4j_database: Neo4j database name (default: "neo4j").
        session_strategy: How to resolve session IDs.
        user_id: User identifier for per_day and persistent strategies.
        auto_extract: Whether to extract entities from stored messages.
        auto_preferences: Whether to detect preferences from user messages.
    """

    def __init__(
        self,
        client: MemoryClient | None = None,
        *,
        neo4j_uri: str | None = None,
        neo4j_password: str | None = None,
        neo4j_user: str = "neo4j",
        neo4j_database: str = "neo4j",
        session_strategy: SessionStrategy | str = SessionStrategy.PER_CONVERSATION,
        user_id: str | None = None,
        auto_extract: bool = True,
        auto_preferences: bool = True,
    ):
        self._client = client
        self._owns_client = client is None
        self._neo4j_uri = neo4j_uri
        self._neo4j_password = neo4j_password
        self._neo4j_user = neo4j_user
        self._neo4j_database = neo4j_database

        if isinstance(session_strategy, str):
            session_strategy = SessionStrategy(session_strategy)
        self._strategy = session_strategy
        self._user_id = user_id
        self._auto_extract = auto_extract
        self._auto_preferences = auto_preferences

        # Per-conversation session ID (generated on first resolve)
        self._conversation_session_id: str | None = None

        # Preference detector (lazy-initialized)
        self._preference_detector = None

        # Observer (set externally by the MCP server lifespan)
        self._observer: MemoryObserver | None = None

    @property
    def client(self) -> MemoryClient:
        """Access the underlying MemoryClient.

        Raises:
            RuntimeError: If not connected.
        """
        if self._client is None:
            raise RuntimeError(
                "MemoryIntegration not connected. Use 'async with' or call connect()."
            )
        return self._client

    async def connect(self) -> None:
        """Connect to Neo4j (only needed if no client was provided)."""
        if self._client is not None:
            return

        from pydantic import SecretStr

        from neo4j_agent_memory import MemoryClient as _MemoryClient
        from neo4j_agent_memory import MemorySettings
        from neo4j_agent_memory.config.settings import Neo4jConfig

        settings = MemorySettings(
            neo4j=Neo4jConfig(
                uri=self._neo4j_uri or "bolt://localhost:7687",
                username=self._neo4j_user,
                password=SecretStr(self._neo4j_password or ""),
                database=self._neo4j_database,
            )
        )
        self._client = _MemoryClient(settings)
        await self._client.connect()

    async def close(self) -> None:
        """Close the connection (only if we own the client)."""
        if self._owns_client and self._client is not None:
            await self._client.close()
            self._client = None

    async def __aenter__(self) -> MemoryIntegration:
        await self.connect()
        return self

    async def __aexit__(self, exc_type, exc_val, exc_tb) -> None:
        await self.close()

    @property
    def observer(self) -> MemoryObserver | None:
        """Access the observer (set by MCP server lifespan)."""
        return self._observer

    @observer.setter
    def observer(self, value: MemoryObserver | None) -> None:
        self._observer = value

    # ── Core Operations ──────────────────────────────────────────────

    async def add_entity(
        self,
        name: str,
        entity_type: str,
        *,
        subtype: str | None = None,
        description: str | None = None,
        aliases: list[str] | None = None,
        metadata: dict[str, Any] | None = None,
        user_identifier: str | None = None,
    ) -> dict[str, Any]:
        """Create or update an entity with POLE+O typing.

        Runs entity resolution against existing graph to avoid duplicates.

        Args:
            name: Entity name.
            entity_type: POLE+O type (PERSON, OBJECT, LOCATION, EVENT, ORGANIZATION).
            subtype: Optional subtype (e.g., VEHICLE, ADDRESS).
            description: Entity description.
            aliases: Alternative names.
            metadata: Additional metadata.
            user_identifier: Optional user scope.

        Returns:
            Dict with entity info and deduplication result.
        """
        try:
            entity, dedup_result = await self.client.long_term.add_entity(
                name=name,
                entity_type=entity_type,
                subtype=subtype,
                description=description,
                aliases=aliases,
                metadata=metadata,
                generate_embedding=True,
                user_identifier=user_identifier,
            )
            result: dict[str, Any] = {
                "stored": True,
                "type": "entity",
                "id": str(entity.id),
                **entity_name_fields(entity),
                "entity_type": (
                    entity.type.value if hasattr(entity.type, "value") else str(entity.type)
                ),
            }
            if dedup_result:
                result["deduplication"] = {
                    "action": dedup_result.action,
                    "matched_entity_name": dedup_result.matched_entity_name,
                    "similarity_score": dedup_result.similarity_score,
                }
            return result
        except Exception as e:
            logger.error(f"Error adding entity: {e}")
            return {"error": str(e)}

    async def add_preference(
        self,
        category: str,
        preference: str,
        *,
        context: str | None = None,
        confidence: float = 1.0,
        metadata: dict[str, Any] | None = None,
        user_identifier: str | None = None,
        applies_to: list | None = None,
    ) -> dict[str, Any]:
        """Record a user preference.

        Args:
            category: Preference category (e.g., 'food', 'music').
            preference: The preference text.
            context: Optional context about when/why this preference was expressed.
            confidence: Confidence score (0.0-1.0).
            metadata: Additional metadata.
            applies_to: Entities this preference is about — names or EntityRefs. The
                storage layer has always supported this and written
                ``(:Preference)-[:APPLIES_TO]->(:Entity)``, but no caller could reach
                it: the parameter stopped at this boundary, so every preference landed
                unconnected and the memory stayed a star around the user instead of a
                graph, with the read queries traversing APPLIES_TO edges that nothing
                wrote.

        Returns:
            Dict with stored preference info.
        """
        try:
            pref = await self.client.long_term.add_preference(
                category=category,
                preference=preference,
                context=context,
                confidence=confidence,
                metadata=metadata,
                generate_embedding=True,
                user_identifier=user_identifier,
                applies_to=applies_to,
            )
            return {
                "stored": True,
                "type": "preference",
                "id": str(pref.id),
                "category": category,
                "applies_to": [_entity_ref_name(ref) for ref in applies_to or []],
            }
        except Exception as e:
            logger.error(f"Error adding preference: {e}")
            return {"error": str(e)}

    async def add_fact(
        self,
        subject: str,
        predicate: str,
        object_value: str,
        *,
        confidence: float = 1.0,
        valid_from: str | None = None,
        valid_until: str | None = None,
        metadata: dict[str, Any] | None = None,
        user_identifier: str | None = None,
    ) -> dict[str, Any]:
        """Store a subject-predicate-object fact triple.

        Args:
            subject: Fact subject.
            predicate: Relationship/predicate.
            object_value: Fact object.
            confidence: Confidence score (0.0-1.0).
            valid_from: ISO date string for temporal validity start.
            valid_until: ISO date string for temporal validity end.
            metadata: Additional metadata.
            user_identifier: Optional user scope.

        Returns:
            Dict with stored fact info.
        """
        try:
            fact = await self.client.long_term.add_fact(
                subject=subject,
                predicate=predicate,
                obj=object_value,
                confidence=confidence,
                valid_from=_parse_iso_datetime(valid_from),
                valid_until=_parse_iso_datetime(valid_until),
                generate_embedding=True,
                metadata=metadata,
                user_identifier=user_identifier,
            )
            return {
                "stored": True,
                "type": "fact",
                "id": str(fact.id) if hasattr(fact, "id") else None,
                "triple": f"{subject} -> {predicate} -> {object_value}",
            }
        except Exception as e:
            logger.error(f"Error adding fact: {e}")
            return {"error": str(e)}

    async def delete_preference(
        self,
        preference_id: str,
        *,
        user_identifier: str,
    ) -> dict[str, Any]:
        """Forget a preference the caller owns (ownership-enforced by the store).

        Returns ``{"deleted": <id>}`` on success or ``{"deleted": None, "reason": ...}``
        when the preference is absent or out of the caller's scope.
        """
        try:
            removed = await self.client.long_term.delete_preference(
                preference_id, user_identifier=user_identifier
            )
        except Exception as e:
            logger.error(f"Error deleting preference: {e}")
            return {"deleted": None, "error": str(e)}
        if removed is None:
            return {"deleted": None, "reason": "not found or not owned by this user"}
        return {"deleted": removed, "type": "preference"}

    async def update_preference(
        self,
        preference_id: str,
        *,
        user_identifier: str,
        preference: str | None = None,
        category: str | None = None,
        context: str | None = None,
        confidence: float | None = None,
        metadata: dict[str, Any] | None = None,
    ) -> dict[str, Any]:
        """Correct an existing preference by id (see
        :meth:`long_term.LongTermMemory.update_preference`). Ownership is the
        same direct HAS_PREFERENCE edge :meth:`delete_preference` enforces."""
        try:
            result = await self.client.long_term.update_preference(
                preference_id,
                user_identifier=user_identifier,
                preference=preference,
                category=category,
                context=context,
                confidence=confidence,
                metadata=metadata,
            )
        except Exception as e:
            logger.error(f"Error updating preference: {e}")
            return {"updated": None, "error": str(e)}
        if result is None:
            return {"updated": None, "reason": "not found or not owned by this user"}
        pref, fields = result
        return {
            "updated": str(pref.id),
            "type": "preference",
            "fields": fields,
            "preference": {
                "id": str(pref.id),
                "category": pref.category,
                "preference": pref.preference,
                "context": pref.context,
                "confidence": pref.confidence,
            },
        }

    async def delete_fact(
        self,
        fact_id: str,
        *,
        user_identifier: str,
    ) -> dict[str, Any]:
        """Forget a fact the caller owns (see :meth:`delete_preference`)."""
        try:
            removed = await self.client.long_term.delete_fact(
                fact_id, user_identifier=user_identifier
            )
        except Exception as e:
            logger.error(f"Error deleting fact: {e}")
            return {"deleted": None, "error": str(e)}
        if removed is None:
            return {"deleted": None, "reason": "not found or not owned by this user"}
        return {"deleted": removed, "type": "fact"}

    async def update_fact(
        self,
        fact_id: str,
        *,
        user_identifier: str,
        subject: str | None = None,
        predicate: str | None = None,
        object_value: str | None = None,
        confidence: float | None = None,
        metadata: dict[str, Any] | None = None,
    ) -> dict[str, Any]:
        """Correct an existing fact triple by id (see
        :meth:`long_term.LongTermMemory.update_fact`). Ownership is the same
        direct HAS_FACT edge :meth:`delete_fact` enforces."""
        try:
            result = await self.client.long_term.update_fact(
                fact_id,
                user_identifier=user_identifier,
                subject=subject,
                predicate=predicate,
                obj=object_value,
                confidence=confidence,
                metadata=metadata,
            )
        except Exception as e:
            logger.error(f"Error updating fact: {e}")
            return {"updated": None, "error": str(e)}
        if result is None:
            return {"updated": None, "reason": "not found or not owned by this user"}
        fact, fields = result
        return {
            "updated": str(fact.id),
            "type": "fact",
            "fields": fields,
            "fact": {
                "id": str(fact.id),
                "triple": f"{fact.subject} -> {fact.predicate} -> {fact.object}",
                "confidence": fact.confidence,
            },
        }

    async def delete_entity(
        self,
        entity_id: str,
        *,
        user_identifier: str,
    ) -> dict[str, Any]:
        """Forget an entity the caller owns (NON-cascading — see
        :meth:`long_term.delete_entity`). A shared entity is only unlinked from the
        caller; a fully-private one is removed."""
        try:
            result = await self.client.long_term.delete_entity(
                entity_id, user_identifier=user_identifier
            )
        except Exception as e:
            logger.error(f"Error deleting entity: {e}")
            return {"deleted": None, "error": str(e)}
        if result is None:
            return {"deleted": None, "reason": "not found or not owned by this user"}
        removed_id, removed_node = result
        return {
            "deleted": removed_id,
            "type": "entity",
            "removed_node": removed_node,
            "note": (
                "entity node removed (was private to you)"
                if removed_node
                else "removed from your memory; the node survives for another user"
            ),
        }

    async def update_entity(
        self,
        entity_id: str,
        *,
        user_identifier: str,
        name: str | None = None,
        description: str | None = None,
        aliases: list[str] | None = None,
        subtype: str | None = None,
        metadata: dict[str, Any] | None = None,
    ) -> dict[str, Any]:
        """Correct an existing entity's fields by id (see
        :meth:`long_term.LongTermMemory.update_entity` for the rename-without-
        resolver contract). Ownership uses the same broad scope check as
        search — direct ownership, mentions, applied preferences, or
        reasoning-touched all count."""
        try:
            result = await self.client.long_term.update_entity(
                entity_id,
                user_identifier=user_identifier,
                name=name,
                description=description,
                aliases=aliases,
                subtype=subtype,
                metadata=metadata,
            )
        except Exception as e:
            logger.error(f"Error updating entity: {e}")
            return {"updated": None, "error": str(e)}
        if result is None:
            return {"updated": None, "reason": "not found or not owned by this user"}
        entity, fields = result
        return {
            "updated": str(entity.id),
            "type": "entity",
            "fields": fields,
            "entity": {
                "id": str(entity.id),
                **entity_name_fields(entity),
                "type": (entity.type.value if hasattr(entity.type, "value") else str(entity.type)),
                "subtype": entity.subtype,
                "description": entity.description,
                "aliases": entity.aliases,
            },
        }
