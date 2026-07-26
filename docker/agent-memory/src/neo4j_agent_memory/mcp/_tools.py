"""MCP tool implementations for Neo4j Agent Memory.

Tools are organized into two profiles:
- Core (8 tools): Essential read/write/forget/correct cycle for memory operations.
- Extended: entity management, relationships, and advanced graph queries.
  (Aura fork: the reasoning-trace tools + memory_export_graph were removed —
  Aura keeps its own reasoning store, so the agent-memory trace path was a
  redundant third path.)

Each tool follows goal-oriented design: high-level tools that orchestrate
extraction, resolution, and graph operations internally.
"""

from __future__ import annotations

import json
import logging
from typing import TYPE_CHECKING, Any

from fastmcp import Context

# ``_is_read_only_query`` was relocated to ``neo4j_agent_memory.core.query`` in
# v0.4 so the bolt and NAMS Cypher accessors share the same validator. The
# private alias below preserves backward compatibility for internal callers
# (including tests/unit/mcp/test_fastmcp_tools.py).
from neo4j_agent_memory.core.query import is_read_only_query as _is_read_only_query  # noqa: F401
from neo4j_agent_memory.integration import entity_name_fields
from neo4j_agent_memory.memory.long_term import _normalized_text
from neo4j_agent_memory.mcp._common import get_client, get_integration

if TYPE_CHECKING:
    from fastmcp import FastMCP

logger = logging.getLogger(__name__)

# How many same-name entities memory_get_entity looks at. It used to fetch exactly one, so a
# name with two entities behind it answered about whichever ranked first and said nothing about
# the other — the operator's "David" existed twice (PERSON and OBJECT) and an agent asked to
# clean up the duplicates was told about one of them, corrected it, and stopped (observed live
# 2026-07-25). Small on purpose: this is a lookup, not a search, and the extras are a heads-up
# that the name is ambiguous rather than a result set to page through.
_MAX_ENTITY_NAME_MATCHES = 5


def _names_collide(entity: Any, name: str) -> bool:
    """True when `entity` really is another entity CALLED `name`.

    Compared over the name, the resolver's canonical name and any alias, so a duplicate
    that was canonicalised onto a different spelling still counts as the collision it is.
    """
    wanted = _normalized_text(name)
    candidates = [
        getattr(entity, "name", None),
        getattr(entity, "canonical_name", None),
        *(getattr(entity, "aliases", None) or []),
    ]
    return any(_normalized_text(value) == wanted for value in candidates if value)


# ── Registration dispatcher ──────────────────────────────────────────


def register_tools(
    mcp: FastMCP,
    *,
    profile: str = "extended",
    register_platinum: bool = False,
) -> None:
    """Register MCP tools on the server based on the selected profile.

    Args:
        mcp: FastMCP server instance.
        profile: Tool profile - 'core' (8 tools) or 'extended'.
        register_platinum: When True (and profile == 'extended'), register
            four additional NAMS Platinum-tier tools:
            ``memory_set_entity_feedback``, ``memory_get_entity_history``,
            ``memory_get_entity_provenance``, ``memory_get_reflections``.
            Server startup should set this when the underlying MemoryClient
            uses ``backend == "nams"`` — see :func:`mcp.server.create_mcp_server`.
    """
    _register_core_tools(mcp)
    if profile == "extended":
        _register_extended_tools(mcp)
        if register_platinum:
            _register_platinum_tools(mcp)


# ── Core Profile (8 tools) ───────────────────────────────────────────


def _register_core_tools(mcp: FastMCP) -> None:
    """Register the 8 core profile tools."""

    @mcp.tool()
    async def memory_search(
        ctx: Context,
        query: str,
        limit: int = 10,
        memory_types: list[str] | None = None,
        session_id: str | None = None,
        threshold: float = 0.7,
        user_identifier: str | None = None,
    ) -> str:
        """Search across all memory types using hybrid vector + graph search.

        Finds relevant messages, entities, preferences, and reasoning traces.
        Use this to recall stored information or find related context.

        Args:
            query: Natural language search query.
            limit: Maximum results per memory type (default: 10).
            memory_types: Types to search. Options: 'messages', 'entities',
                'preferences', 'traces'. Defaults to messages, entities, preferences.
            session_id: Filter message search to a specific session.
            threshold: Similarity threshold 0.0-1.0 (default: 0.7).
        """
        integration = get_integration(ctx)
        result = await integration.search(
            query,
            memory_types=memory_types,
            session_id=session_id,
            limit=limit,
            threshold=threshold,
            user_identifier=user_identifier,
        )
        return json.dumps(result, default=str)

    @mcp.tool()
    async def memory_get_context(
        ctx: Context,
        session_id: str | None = None,
        query: str | None = None,
        max_items: int = 10,
        include_short_term: bool = True,
        include_long_term: bool = True,
        include_reasoning: bool = True,
        user_identifier: str | None = None,
    ) -> str:
        """Get assembled context from all memory types for the current session.

        Returns conversation history, relevant entities/preferences, and similar
        past reasoning traces, formatted for LLM consumption. Call this at the
        start of each conversation to load relevant memories.

        Args:
            session_id: Session to get context for (uses current session if not set).
            query: Optional search query to focus context retrieval.
            max_items: Maximum items per memory type (default: 10).
            include_short_term: Include conversation history (default: true).
            include_long_term: Include entities and preferences (default: true).
            include_reasoning: Include similar reasoning traces (default: true).
        """
        integration = get_integration(ctx)
        result = await integration.get_context(
            session_id=session_id,
            query=query,
            max_items=max_items,
            include_short_term=include_short_term,
            include_long_term=include_long_term,
            include_reasoning=include_reasoning,
            user_identifier=user_identifier,
        )
        return json.dumps(result, default=str)

    @mcp.tool()
    async def memory_store_message(
        ctx: Context,
        content: str,
        role: str = "user",
        session_id: str | None = None,
        metadata: dict[str, Any] | None = None,
        user_identifier: str | None = None,
    ) -> str:
        """Store a message in conversation memory.

        Automatically triggers entity extraction, preference detection,
        and fact extraction from the message content. Use this to persist
        important user messages, assistant responses, or system events.

        Args:
            content: The message text content.
            role: Message role - 'user', 'assistant', or 'system' (default: 'user').
            session_id: Session ID (uses current session if not set).
            metadata: Optional metadata to attach (e.g., model, source).
        """
        integration = get_integration(ctx)
        result = await integration.store_message(
            role=role,
            content=content,
            session_id=session_id,
            metadata=metadata,
            user_identifier=user_identifier,
        )
        return json.dumps(result, default=str)

    @mcp.tool()
    async def memory_add_entity(
        ctx: Context,
        name: str,
        entity_type: str,
        subtype: str | None = None,
        description: str | None = None,
        aliases: list[str] | None = None,
        metadata: dict[str, Any] | None = None,
        user_identifier: str | None = None,
    ) -> str:
        """Create or update an entity in the knowledge graph.

        Runs entity resolution against existing entities to avoid duplicates.
        If a close match is found, the entity may be merged or flagged.

        Entity types follow POLE+O: PERSON, OBJECT, LOCATION, EVENT, ORGANIZATION.
        Each supports optional subtypes (e.g., OBJECT:VEHICLE, LOCATION:ADDRESS).

        Args:
            name: Entity name (e.g., 'John Smith', 'Acme Corp').
            entity_type: POLE+O type in UPPER_CASE.
            subtype: Optional subtype (e.g., 'VEHICLE', 'COMPANY').
            description: Entity description.
            aliases: Alternative names for the entity.
            metadata: Additional metadata.
        """
        integration = get_integration(ctx)
        result = await integration.add_entity(
            name=name,
            entity_type=entity_type,
            subtype=subtype,
            description=description,
            aliases=aliases,
            metadata=metadata,
            user_identifier=user_identifier,
        )
        return json.dumps(result, default=str)

    @mcp.tool()
    async def memory_add_preference(
        ctx: Context,
        category: str,
        preference: str,
        context: str | None = None,
        confidence: float = 1.0,
        metadata: dict[str, Any] | None = None,
        user_identifier: str | None = None,
        applies_to: list[str] | None = None,
    ) -> str:
        """Record a user preference for personalization.

        Store explicit or inferred user preferences with categorization.
        These are used to personalize future interactions.

        Args:
            category: Preference category (e.g., 'food', 'music', 'communication_style').
            preference: The preference text (e.g., 'Prefers dark mode').
            context: Optional context about when/why the preference was expressed.
            confidence: Confidence score 0.0-1.0 (default: 1.0).
            metadata: Additional provenance metadata.
            applies_to: Names of the entities this preference is ABOUT (e.g. ["Caraglio"]
                for a home location). Always pass them when the preference concerns a
                person, place or organization: this is what connects the preference into
                the knowledge graph instead of leaving it dangling off the user, and it
                lets recall reach it by structure rather than by wording alone.
        """
        integration = get_integration(ctx)
        result = await integration.add_preference(
            category=category,
            preference=preference,
            context=context,
            confidence=confidence,
            metadata=metadata,
            user_identifier=user_identifier,
            applies_to=applies_to,
        )
        return json.dumps(result, default=str)

    @mcp.tool()
    async def memory_add_fact(
        ctx: Context,
        subject: str,
        predicate: str,
        object_value: str,
        confidence: float = 1.0,
        valid_from: str | None = None,
        valid_until: str | None = None,
        metadata: dict[str, Any] | None = None,
        user_identifier: str | None = None,
    ) -> str:
        """Store a subject-predicate-object fact triple.

        Records declarative knowledge as a structured triple with optional
        temporal validity bounds and arbitrary metadata.

        Args:
            subject: The subject of the fact (e.g., 'Earth').
            predicate: The relationship (e.g., 'has_radius_km').
            object_value: The object/value (e.g., '6371').
            confidence: Confidence score 0.0-1.0 (default: 1.0).
            valid_from: ISO date string for when this fact becomes valid.
            valid_until: ISO date string for when this fact expires.
            metadata: Additional metadata (e.g., scope, stack_tags, temporality).
        """
        integration = get_integration(ctx)
        result = await integration.add_fact(
            subject=subject,
            predicate=predicate,
            object_value=object_value,
            confidence=confidence,
            valid_from=valid_from,
            valid_until=valid_until,
            metadata=metadata,
            user_identifier=user_identifier,
        )
        return json.dumps(result, default=str)

    @mcp.tool()
    async def memory_forget(
        ctx: Context,
        node_type: str,
        node_id: str,
        user_identifier: str | None = None,
    ) -> str:
        """Delete (forget) a stored memory the user owns, by id.

        Use this to remove an obsolete or wrong memory — e.g. after the operator
        corrects a preference, forget the old one. Ownership is enforced: a node the
        caller does not own (or that does not exist) is REFUSED, not silently ignored,
        and reported back so the caller knows nothing was removed. Forgetting an
        'entity' is deliberately NON-cascading: it unlinks your ownership and removes
        the entity node only if it is fully orphaned afterwards — a shared entity
        (referenced by other messages/preferences) is kept, just unlinked from you.

        Args:
            node_type: What to forget — 'preference', 'fact', or 'entity'.
            node_id: The id of the node to delete (from add_preference/add_fact/
                add_entity or a get_* / search result).

        Returns a JSON object: ``{"deleted": <id>}`` on success, or
        ``{"deleted": null, "reason": ...}`` when nothing was removed.
        """
        integration = get_integration(ctx)
        kind = node_type.strip().lower()
        if kind in ("preference", "pref"):
            result = await integration.delete_preference(
                node_id, user_identifier=user_identifier
            )
        elif kind == "fact":
            result = await integration.delete_fact(
                node_id, user_identifier=user_identifier
            )
        elif kind == "entity":
            result = await integration.delete_entity(
                node_id, user_identifier=user_identifier
            )
        else:
            result = {
                "deleted": None,
                "reason": f"unsupported node_type {node_type!r}; use 'preference', 'fact', or 'entity'",
            }
        return json.dumps(result, default=str)

    @mcp.tool()
    async def memory_update(
        ctx: Context,
        node_type: str,
        node_id: str,
        name: str | None = None,
        description: str | None = None,
        aliases: list[str] | None = None,
        subtype: str | None = None,
        preference: str | None = None,
        category: str | None = None,
        context: str | None = None,
        subject: str | None = None,
        predicate: str | None = None,
        object_value: str | None = None,
        confidence: float | None = None,
        metadata: dict[str, Any] | None = None,
        user_identifier: str | None = None,
    ) -> str:
        """Correct an existing memory the user owns, by id — the update half of add/forget.

        ``memory_add_entity``, ``memory_add_preference``, and ``memory_add_fact``
        all resolve/deduplicate what you pass against what already exists and
        merge into the closest match WITHOUT changing that match's stored
        text. So re-adding to fix a mistake (e.g. correcting the name 'David'
        to 'Davide') just re-merges onto the OLD wording every time — add can
        never rename or reword anything. The only other lever, forgetting and
        re-adding, throws away the node's id and relationships. This tool
        edits the existing node directly by id instead, bypassing resolution
        and deduplication entirely, so it is the correct way to fix a wrong
        name, description, category, wording, or triple on a memory that
        already exists — never delete-and-recreate for a correction.

        Get node_id the same way you would for memory_forget: from the
        relevant add_* tool's response ('id', or 'matched_entity_id' under
        'deduplication' when it merged), from a get_* tool, or from a
        memory_search result.

        Only the fields relevant to node_type are used; anything you omit
        keeps its current value. Changing an entity's name/description, a
        preference's preference/category/context, or a fact's
        subject/predicate/object also refreshes that node's embedding, so
        the matching vector index (entity_embedding_idx,
        preference_embedding_idx, fact_embedding_idx) doesn't keep returning
        results under stale wording.

        Args:
            node_type: What to correct — 'preference', 'fact', or 'entity'.
            node_id: The id of the node to correct (not its name/text).
            name: [entity] Corrected name, if wrong. Also keeps the entity's
                internal canonical_name in sync, so future add_entity calls
                resolve to the corrected spelling instead of the old one.
            description: [entity] Corrected description, if wrong.
            aliases: [entity] Replacement list of alternative names, if wrong.
            subtype: [entity] Corrected subtype, if wrong.
            preference: [preference] Corrected preference text, if wrong.
            category: [preference] Corrected category, if wrong.
            context: [preference] Corrected context, if wrong.
            subject: [fact] Corrected subject, if wrong.
            predicate: [fact] Corrected predicate, if wrong.
            object_value: [fact] Corrected object/value, if wrong.
            confidence: [preference, fact] Corrected confidence 0.0-1.0, if wrong.
            metadata: [preference, fact, entity] Metadata fields to merge in.

        Returns a JSON object: ``{"updated": <id>, "fields": [...], "entity"|
        "preference"|"fact": {...}}`` on success, or ``{"updated": null,
        "reason": ...}`` when the node does not exist or is outside the
        caller's scope — never a silent no-op.
        """
        integration = get_integration(ctx)
        kind = node_type.strip().lower()
        if kind in ("preference", "pref"):
            result = await integration.update_preference(
                node_id,
                user_identifier=user_identifier,
                preference=preference,
                category=category,
                context=context,
                confidence=confidence,
                metadata=metadata,
            )
        elif kind == "fact":
            result = await integration.update_fact(
                node_id,
                user_identifier=user_identifier,
                subject=subject,
                predicate=predicate,
                object_value=object_value,
                confidence=confidence,
                metadata=metadata,
            )
        elif kind == "entity":
            result = await integration.update_entity(
                node_id,
                user_identifier=user_identifier,
                name=name,
                description=description,
                aliases=aliases,
                subtype=subtype,
                metadata=metadata,
            )
        else:
            result = {
                "updated": None,
                "reason": f"unsupported node_type {node_type!r}; use 'preference', 'fact', or 'entity'",
            }
        return json.dumps(result, default=str)


# ── Extended Profile ─────────────────────────────────────────────────


def _register_extended_tools(mcp: FastMCP) -> None:
    """Register the extended profile tools."""

    @mcp.tool()
    async def memory_get_conversation(
        ctx: Context,
        session_id: str,
        limit: int = 50,
        include_metadata: bool = True,
        user_identifier: str | None = None,
    ) -> str:
        """Retrieve full conversation history for a session.

        Returns messages in chronological order with role, content, and timestamps.

        Args:
            session_id: The session ID to retrieve.
            limit: Maximum number of messages to return (default: 50).
            include_metadata: Include message metadata (default: true).
        """
        client = get_client(ctx)

        try:
            conversation = await client.short_term.get_conversation(
                session_id=session_id,
                limit=limit,
                user_identifier=user_identifier,
            )

            messages = []
            for msg in conversation.messages:
                msg_data: dict[str, Any] = {
                    "id": str(msg.id),
                    "role": msg.role.value if hasattr(msg.role, "value") else str(msg.role),
                    "content": msg.content,
                    "timestamp": msg.created_at.isoformat() if msg.created_at else None,
                }
                if include_metadata and msg.metadata:
                    msg_data["metadata"] = msg.metadata
                messages.append(msg_data)

            return json.dumps(
                {
                    "session_id": session_id,
                    "message_count": len(messages),
                    "messages": messages,
                },
                default=str,
            )

        except Exception as e:
            logger.error(f"Error in memory_get_conversation: {e}")
            return json.dumps({"error": str(e)})

    @mcp.tool()
    async def memory_list_sessions(
        ctx: Context,
        limit: int = 20,
        offset: int = 0,
        user_identifier: str | None = None,
    ) -> str:
        """List available conversation sessions with previews.

        Returns session IDs, message counts, and first/last message previews.
        Useful for browsing stored conversations.

        Args:
            limit: Maximum sessions to return (default: 20).
            offset: Offset for pagination (default: 0).
        """
        client = get_client(ctx)

        try:
            sessions = await client.short_term.list_sessions(
                limit=limit,
                offset=offset,
                user_identifier=user_identifier,
            )

            session_list = []
            for session in sessions:
                session_list.append(
                    {
                        "session_id": session.session_id,
                        "title": session.title,
                        "message_count": session.message_count,
                        "created_at": (
                            session.created_at.isoformat() if session.created_at else None
                        ),
                        "updated_at": (
                            session.updated_at.isoformat() if session.updated_at else None
                        ),
                        "first_message_preview": session.first_message_preview,
                        "last_message_preview": session.last_message_preview,
                    }
                )

            return json.dumps(
                {
                    "session_count": len(session_list),
                    "offset": offset,
                    "sessions": session_list,
                },
                default=str,
            )

        except Exception as e:
            logger.error(f"Error in memory_list_sessions: {e}")
            return json.dumps({"error": str(e)})

    @mcp.tool()
    async def memory_get_entity(
        ctx: Context,
        name: str,
        entity_type: str | None = None,
        include_neighbors: bool = True,
        max_hops: int = 1,
        user_identifier: str | None = None,
    ) -> str:
        """Get detailed entity information with graph relationships.

        Searches the knowledge graph for an entity by name and optionally
        traverses relationships to find connected entities.

        When several entities match the name, the closest is returned as ``entity`` and
        the rest are listed under ``other_matches`` (id, name, type). Duplicates are
        exactly what someone asking about an entity by name needs to see — reporting only
        the top hit is how a second copy stays invisible to the one tool whose job is to
        tell you about that name.

        Args:
            name: Entity name to look up.
            entity_type: Filter by POLE+O type (optional).
            include_neighbors: Traverse graph for related entities (default: true).
            max_hops: Relationship traversal depth, 1-3 (default: 1).
        """
        client = get_client(ctx)

        try:
            entities = await client.long_term.search_entities(
                query=name,
                entity_types=[entity_type] if entity_type else None,
                limit=_MAX_ENTITY_NAME_MATCHES,
                user_identifier=user_identifier,
            )

            if not entities:
                return json.dumps({"found": False, "name": name})

            entity = entities[0]
            result: dict[str, Any] = {
                "found": True,
                "entity": {
                    "id": str(entity.id),
                    **entity_name_fields(entity),
                    "type": (
                        entity.type.value if hasattr(entity.type, "value") else str(entity.type)
                    ),
                    "subtype": entity.subtype if hasattr(entity, "subtype") else None,
                    "description": entity.description,
                    "aliases": entity.aliases if hasattr(entity, "aliases") else [],
                },
            }
            # Only genuine NAME COLLISIONS belong here. The recall behind this is a vector
            # search, so `entities[1:]` is "whatever else the graph holds", not "other
            # entities called this": asking about "ProbeCorp" listed Davide and Caraglio as
            # other matches. That contradicts this tool's own contract and hands the caller
            # unrelated identities as if they were duplicates of the thing they asked about.
            other_matches = [
                {
                    "id": str(other.id),
                    **entity_name_fields(other),
                    "type": (
                        other.type.value if hasattr(other.type, "value") else str(other.type)
                    ),
                }
                for other in entities[1:]
                if _names_collide(other, name)
            ]
            if other_matches:
                result["other_matches"] = other_matches

            if include_neighbors:
                neighbors = await _get_entity_neighbors(
                    client,
                    str(entity.id),
                    max_hops,
                    user_identifier=user_identifier,
                )
                result["neighbors"] = neighbors

            return json.dumps(result, default=str)

        except Exception as e:
            logger.error(f"Error in memory_get_entity: {e}")
            return json.dumps({"error": str(e)})

    @mcp.tool()
    async def memory_get_facts(
        ctx: Context,
        subject: str | None = None,
        query: str | None = None,
        limit: int = 20,
        threshold: float = 0.7,
        user_identifier: str | None = None,
    ) -> str:
        """Retrieve facts by exact subject or semantic query.

        Use this instead of broad graph_query when an agent needs declarative
        subject-predicate-object facts.

        Args:
            subject: Exact fact subject to retrieve.
            query: Optional semantic query when subject is not known.
            limit: Maximum facts to return.
            threshold: Similarity threshold for semantic search.
        """
        client = get_client(ctx)
        try:
            if subject:
                facts = await client.long_term.get_facts_about(
                    subject,
                    limit=limit,
                    user_identifier=user_identifier,
                )
            elif query:
                facts = await client.long_term.search_facts(
                    query,
                    limit=limit,
                    threshold=threshold,
                    user_identifier=user_identifier,
                )
            else:
                return json.dumps({"error": "Provide subject or query."})

            return json.dumps(
                {
                    "fact_count": len(facts),
                    "facts": [
                        {
                            "id": str(f.id),
                            "subject": f.subject,
                            "predicate": f.predicate,
                            "object": f.object,
                            "confidence": f.confidence,
                            "metadata": f.metadata,
                        }
                        for f in facts
                    ],
                },
                default=str,
            )
        except Exception as e:
            logger.error(f"Error in memory_get_facts: {e}")
            return json.dumps({"error": str(e)})

    @mcp.tool()
    async def memory_create_relationship(
        ctx: Context,
        source_name: str,
        target_name: str,
        relationship_type: str,
        description: str | None = None,
        confidence: float = 1.0,
        user_identifier: str | None = None,
    ) -> str:
        """Create a typed relationship between two entities.

        Both entities are looked up by name. If either is not found,
        an error is returned.

        Args:
            source_name: Name of the source entity.
            target_name: Name of the target entity.
            relationship_type: Relationship type in UPPER_SNAKE_CASE
                (e.g., 'WORKS_AT', 'LIVES_IN', 'FOUNDED').
            description: Optional description of the relationship.
            confidence: Confidence score 0.0-1.0 (default: 1.0).
        """
        client = get_client(ctx)

        try:
            # Resolve source entity
            source_entities = await client.long_term.search_entities(
                query=source_name,
                limit=1,
                user_identifier=user_identifier,
            )
            if not source_entities:
                return json.dumps(
                    {"error": f"Source entity '{source_name}' not found in knowledge graph."}
                )

            # Resolve target entity
            target_entities = await client.long_term.search_entities(
                query=target_name,
                limit=1,
                user_identifier=user_identifier,
            )
            if not target_entities:
                return json.dumps(
                    {"error": f"Target entity '{target_name}' not found in knowledge graph."}
                )

            source = source_entities[0]
            target = target_entities[0]

            rel = await client.long_term.add_relationship(
                source=source,
                target=target,
                relationship_type=relationship_type,
                description=description,
                confidence=confidence,
            )

            return json.dumps(
                {
                    "stored": True,
                    "type": "relationship",
                    "id": str(rel.id) if hasattr(rel, "id") else None,
                    "source": source.name,
                    "target": target.name,
                    "relationship_type": relationship_type,
                },
                default=str,
            )

        except Exception as e:
            logger.error(f"Error in memory_create_relationship: {e}")
            return json.dumps({"error": str(e)})



# ── Platinum Profile (4 additional NAMS-only tools) ──────────────────


def _register_platinum_tools(mcp: FastMCP) -> None:
    """Register NAMS Platinum-tier tools.

    Only registered when the MemoryClient is configured with
    ``backend == "nams"``. These tools rely on Platinum operations
    (``set_entity_feedback``, ``get_entity_history``,
    ``get_entity_provenance``, ``get_reflections``) that the bolt
    backend does not implement.
    """

    @mcp.tool()
    async def memory_set_entity_feedback(
        ctx: Context,
        entity_id: str,
        feedback: str,
        user_identifier: str | None = None,
    ) -> str:
        """Record user feedback on an entity (NAMS Platinum).

        Use this when the user explicitly confirms or rejects an entity
        the agent surfaced — NAMS uses the signal to bias future
        retrieval and dedup decisions.

        Args:
            entity_id: Entity UUID (returned by memory_add_entity or
                memory_search).
            feedback: Free-form feedback — convention is "positive" or
                "negative".
            user_identifier: Optional per-user scoping (multi-tenant).
        """
        client = get_client(ctx)
        try:
            # Platinum-tier — present on NamsLongTermMemory at runtime.
            await client.long_term.set_entity_feedback(  # type: ignore[attr-defined]
                entity_id, feedback, user_identifier=user_identifier
            )
            return json.dumps({"status": "ok", "entity_id": entity_id, "feedback": feedback})
        except Exception as e:
            logger.error(f"Error in memory_set_entity_feedback: {e}")
            return json.dumps({"error": str(e)})

    @mcp.tool()
    async def memory_get_entity_history(
        ctx: Context,
        entity_id: str,
        limit: int = 50,
        user_identifier: str | None = None,
    ) -> str:
        """Get the conversation-and-mention history for an entity (NAMS Platinum).

        Returns a list of conversations where the entity has been
        mentioned, with mention counts and first/last seen timestamps.
        Useful when the agent needs to know "have I encountered this
        entity before?".

        Args:
            entity_id: Entity UUID.
            limit: Maximum history entries to return (default: 50).
        """
        if user_identifier is not None:
            return _scoped_unsupported("memory_get_entity_history")

        client = get_client(ctx)
        try:
            history = await client.long_term.get_entity_history(  # type: ignore[attr-defined]
                entity_id, limit=limit
            )
            return json.dumps({"entity_id": entity_id, "history": history}, default=str)
        except Exception as e:
            logger.error(f"Error in memory_get_entity_history: {e}")
            return json.dumps({"error": str(e)})

    @mcp.tool()
    async def memory_get_entity_provenance(
        ctx: Context,
        entity_id: str,
        user_identifier: str | None = None,
    ) -> str:
        """Get the provenance record for an entity (source messages + extractors).

        Returns ``{"sources": [...], "extractors": [...]}`` so the agent
        can cite where it learned an entity from.

        Args:
            entity_id: Entity UUID.
        """
        if user_identifier is not None:
            return _scoped_unsupported("memory_get_entity_provenance")

        client = get_client(ctx)
        try:
            prov = await client.long_term.get_entity_provenance(entity_id)  # type: ignore[arg-type]
            return json.dumps(prov, default=str)
        except Exception as e:
            logger.error(f"Error in memory_get_entity_provenance: {e}")
            return json.dumps({"error": str(e)})

    @mcp.tool()
    async def memory_get_reflections(
        ctx: Context,
        session_id: str,
        limit: int = 20,
        user_identifier: str | None = None,
    ) -> str:
        """Get LLM-generated reflections for a session (NAMS Platinum).

        Reflections are higher-level summaries the hosted service
        generates over a conversation — distinct from raw message
        history. Use to understand the agent's prior thinking about a
        session without re-scanning every message.

        Args:
            session_id: Session identifier.
            limit: Maximum reflections to return (default: 20).
        """
        client = get_client(ctx)
        try:
            if user_identifier is not None:
                conversation = await client.short_term.get_conversation(
                    session_id=session_id,
                    limit=1,
                    user_identifier=user_identifier,
                )
                if not conversation.messages:
                    return json.dumps({"session_id": session_id, "reflections": []})
            reflections = await client.short_term.get_reflections(  # type: ignore[attr-defined]
                session_id, limit=limit
            )
            return json.dumps({"session_id": session_id, "reflections": reflections}, default=str)
        except Exception as e:
            logger.error(f"Error in memory_get_reflections: {e}")
            return json.dumps({"error": str(e)})


# ── Helpers ───────────────────────────────────────────────────────────


def _scoped_unsupported(tool_name: str) -> str:
    return json.dumps(
        {
            "error": f"{tool_name} is disabled for user-scoped sessions because "
            "it cannot enforce per-user graph isolation. Use memory_store_message "
            "or a scoped retrieval tool instead."
        }
    )


async def _trace_in_user_scope(client: Any, trace_id: str, user_identifier: str) -> bool:
    try:
        records = await client.query.cypher(
            """
            MATCH (rt:ReasoningTrace {id: $trace_id})
            OPTIONAL MATCH (u:User {identifier: $user_identifier})-[:HAS_TRACE]->(rt)
            RETURN (rt.user_identifier = $user_identifier OR u IS NOT NULL) AS ok
            LIMIT 1
            """,
            {"trace_id": trace_id, "user_identifier": user_identifier},
        )
        return bool(records and records[0].get("ok"))
    except Exception:
        return False


async def _get_entity_neighbors(
    client: Any,
    entity_id: str,
    max_hops: int = 1,
    *,
    user_identifier: str | None = None,
) -> list[dict[str, Any]]:
    """Get neighboring entities via graph traversal.

    Args:
        client: MemoryClient instance.
        entity_id: Starting entity ID.
        max_hops: Maximum relationship depth (clamped to 1-3).

    Returns:
        List of neighboring entities with relationships.
    """
    max_hops = min(max(max_hops, 1), 3)
    query = f"""
    MATCH (e:Entity {{id: $entity_id}})-[r*1..{max_hops}]-(neighbor:Entity)
    OPTIONAL MATCH (:User {{identifier: $user_identifier}})-[ue:HAS_ENTITY]->(neighbor)
    OPTIONAL MATCH (:User {{identifier: $user_identifier}})-[:HAS_CONVERSATION]->(:Conversation)-[:HAS_MESSAGE]->(m:Message)-[:MENTIONS]->(neighbor)
    OPTIONAL MATCH (:User {{identifier: $user_identifier}})-[:HAS_PREFERENCE]->(p:Preference)-[:APPLIES_TO]->(neighbor)
    OPTIONAL MATCH (:User {{identifier: $user_identifier}})-[:HAS_TRACE]->(:ReasoningTrace)-[:HAS_STEP]->(rs:ReasoningStep)-[:TOUCHED]->(neighbor)
    WHERE neighbor.id <> $entity_id
      AND ($user_identifier IS NULL OR ue IS NOT NULL OR m IS NOT NULL OR p IS NOT NULL OR rs IS NOT NULL)
    WITH DISTINCT neighbor, r
    RETURN neighbor.id AS id,
           neighbor.name AS name,
           neighbor.type AS type,
           neighbor.description AS description
    LIMIT 20
    """

    try:
        # v0.4: portable read-only Cypher accessor (works on bolt + NAMS).
        records = await client.query.cypher(
            query,
            {"entity_id": entity_id, "user_identifier": user_identifier},
        )
        return [
            {
                "id": r["id"],
                "name": r["name"],
                "type": r["type"],
                "description": r["description"],
            }
            for r in records
        ]
    except Exception as e:
        logger.debug(f"Error getting neighbors: {e}")
        return []
