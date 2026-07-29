"""Context-facing operations for :mod:`neo4j_agent_memory.integration`."""

from __future__ import annotations

import asyncio
import logging
from datetime import datetime, timezone
from enum import Enum
from typing import TYPE_CHECKING, Any
from uuid import uuid4

if TYPE_CHECKING:
    from neo4j_agent_memory import MemoryClient
    from neo4j_agent_memory.mcp._observer import MemoryObserver

logger = logging.getLogger(__name__)

_RETRIEVER_REVISION = "neo4j-agent-memory-long-term-v1"
_RERANKER_REVISION = "none-v1"


def entity_name_fields(entity: Any) -> dict[str, Any]:
    """Expose the stored name and keep a distinct canonical alias when present."""
    fields: dict[str, Any] = {"name": entity.name}
    canonical = getattr(entity, "canonical_name", None)
    if canonical and canonical != entity.name:
        fields["canonical_name"] = canonical
    return fields


# The buckets memory_search can actually serve. Kept next to search() so adding a
# retrieval path and advertising it can never drift apart.
_SEARCHABLE = {"messages", "entities", "preferences", "facts", "traces"}


class SessionStrategy(str, Enum):
    """Strategy for resolving session IDs."""

    PER_CONVERSATION = "per_conversation"
    PER_DAY = "per_day"
    PERSISTENT = "persistent"


class ContextSearchMixin:
    """Session, message, context, and search operations for MemoryIntegration."""

    _strategy: SessionStrategy
    _conversation_session_id: str | None
    _user_id: str | None
    _auto_extract: bool
    _auto_preferences: bool
    _preference_detector: Any
    _observer: MemoryObserver | None

    @property
    def client(self) -> MemoryClient:
        raise NotImplementedError

    def resolve_session_id(self, hint: str | None = None) -> str:
        if hint:
            return hint
        if self._strategy == SessionStrategy.PER_CONVERSATION:
            if self._conversation_session_id is None:
                self._conversation_session_id = str(uuid4())
            return self._conversation_session_id
        if self._strategy == SessionStrategy.PER_DAY:
            user = self._user_id or "default"
            return f"{user}-{datetime.now(tz=timezone.utc):%Y-%m-%d}"
        return self._user_id or "default"

    def _get_preference_detector(self):
        if self._preference_detector is None:
            from neo4j_agent_memory.mcp._preference_detector import PreferenceDetector

            self._preference_detector = PreferenceDetector()
        return self._preference_detector

    async def _detect_and_store_preferences(
        self,
        content: str,
        session_id: str,
        user_identifier: str | None = None,
    ) -> None:
        try:
            detector = self._get_preference_detector()
            for pref in detector.detect(content):
                sentiment_prefix = "" if pref.sentiment == "positive" else "Dislikes: "
                await self.client.long_term.add_preference(
                    category=pref.category,
                    preference=f"{sentiment_prefix}{pref.preference}",
                    context=f"Auto-detected from session {session_id}: {pref.source_text[:200]}",
                    confidence=pref.confidence,
                    generate_embedding=True,
                    user_identifier=user_identifier,
                )
                logger.debug(
                    "Auto-detected preference: [%s] %s: %s",
                    pref.category,
                    pref.sentiment,
                    pref.preference,
                )
        except Exception as exc:
            logger.warning("Error in background preference detection: %s", exc)

    async def store_message(
        self,
        role: str,
        content: str,
        *,
        session_id: str | None = None,
        metadata: dict[str, Any] | None = None,
        user_identifier: str | None = None,
    ) -> dict[str, Any]:
        sid = self.resolve_session_id(session_id)

        async def store():
            return await self.client.short_term.add_message(
                session_id=sid,
                role=role,
                content=content,
                metadata=metadata,
                extract_entities=self._auto_extract,
                generate_embedding=True,
                user_identifier=user_identifier,
            )

        try:
            message = await self.client.graph.execute_write_unit(store)
            if self._auto_preferences and role == "user":
                asyncio.create_task(
                    self._detect_and_store_preferences(content, sid, user_identifier)
                )
            if self._observer is not None:
                asyncio.create_task(
                    self._observer.on_message_stored(
                        session_id=sid,
                        content=content,
                        message_id=str(message.id),
                        role=role,
                    )
                )
            return {
                "stored": True,
                "type": "message",
                "id": str(message.id),
                "session_id": sid,
            }
        except Exception as exc:
            logger.error("Error storing message: %s", exc)
            from neo4j_agent_memory.memory.atomic import AtomicMutationError

            raise AtomicMutationError() from exc

    async def get_context(
        self,
        *,
        session_id: str | None = None,
        query: str | None = None,
        max_items: int = 10,
        include_short_term: bool = True,
        include_long_term: bool = True,
        include_reasoning: bool = True,
        user_identifier: str | None = None,
    ) -> dict[str, Any]:
        sid = self.resolve_session_id(session_id)
        try:
            if include_long_term and not include_short_term and not include_reasoning:
                return await self._long_term_context(
                    sid=sid,
                    query=query or "",
                    max_items=max_items,
                    user_identifier=user_identifier,
                )
            context = await self.client.get_context(
                query=query or "",
                session_id=sid,
                include_short_term=include_short_term,
                include_long_term=include_long_term,
                include_reasoning=include_reasoning,
                max_items=max_items,
                user_identifier=user_identifier,
            )
            return {"session_id": sid, "context": context, "has_context": bool(context)}
        except Exception as exc:
            logger.error("Error getting context: %s", exc)
            return {"error": str(exc)}

    async def _long_term_context(
        self,
        *,
        sid: str,
        query: str,
        max_items: int,
        user_identifier: str | None,
    ) -> dict[str, Any]:
        before = await self._read_corpus_epoch()
        preferences = await self.client.long_term.search_preferences(
            query,
            limit=max_items,
            user_identifier=user_identifier,
        )
        entities = await self.client.long_term.search_entities(
            query,
            limit=max_items,
            user_identifier=user_identifier,
        )
        after = await self._read_corpus_epoch()

        long_term_context = self._format_long_term(preferences, entities)
        context = f"## Relevant Knowledge\n{long_term_context}" if long_term_context else ""
        revisions = self._recall_revisions()
        coherent = before is not None and before == after
        metadata = {
            "results": [
                *[
                    {"kind": "memory_preference", "id": str(pref.id), "order": order}
                    for order, pref in enumerate(preferences)
                ],
                *[
                    {
                        "kind": "memory_entity",
                        "id": str(entity.id),
                        "order": len(preferences) + order,
                    }
                    for order, entity in enumerate(entities)
                ],
            ],
            "limits": {
                "memory_preference": {
                    "requested_k": max_items,
                    "effective_k": max_items,
                    "count": len(preferences),
                },
                "memory_entity": {
                    "requested_k": max_items,
                    "effective_k": max_items,
                    "count": len(entities),
                },
            },
            "revisions": revisions,
            "corpus_epoch_before": before,
            "corpus_epoch_after": after,
            "coherent": coherent,
            "adaptive_eligible": coherent and all(revisions.values()),
        }
        return {
            "session_id": sid,
            "context": context,
            "has_context": bool(context),
            "recall_metadata": metadata,
        }

    async def _read_corpus_epoch(self) -> int | None:
        graph_client = getattr(self.client, "_client", None)
        reader = getattr(graph_client, "read_corpus_epoch", None)
        if reader is None:
            return None
        try:
            return await reader()
        except Exception:
            logger.warning("Memory corpus epoch read failed", exc_info=True)
            return None

    def _recall_revisions(self) -> dict[str, str | None]:
        settings = getattr(self.client, "_settings", None)
        embedding = getattr(settings, "embedding", None)
        model = getattr(embedding, "model", None)
        dimensions = getattr(embedding, "dimensions", None)
        provider = getattr(embedding, "provider", None)
        provider_value = getattr(provider, "value", provider)
        if isinstance(model, str) and model and "/" not in model and provider_value:
            model = f"{provider_value}/{model}"
        valid_dimensions = isinstance(dimensions, int) and dimensions > 0
        embedding_revision = f"{model}@{dimensions}" if model and valid_dimensions else None
        index_revision = (
            f"entity_embedding_idx+preference_embedding_idx@{dimensions}"
            if valid_dimensions
            else None
        )
        return {
            "retriever": _RETRIEVER_REVISION,
            "reranker": _RERANKER_REVISION,
            "embedding": embedding_revision,
            "index": index_revision,
        }

    @staticmethod
    def _format_long_term(preferences: list[Any], entities: list[Any]) -> str:
        parts: list[str] = []
        if preferences:
            parts.append("### User Preferences")
            for pref in preferences:
                line = f"- [{pref.category}] {pref.preference}"
                if pref.context:
                    line += f" (context: {pref.context})"
                parts.append(line)
        if entities:
            parts.append("\n### Relevant Entities")
            for entity in entities:
                line = f"- {entity.name} ({entity.full_type})"
                if entity.description:
                    line += f": {entity.description}"
                parts.append(line)
        return "\n".join(parts)

    async def search(
        self,
        query: str,
        *,
        memory_types: list[str] | None = None,
        session_id: str | None = None,
        limit: int = 10,
        threshold: float = 0.7,
        user_identifier: str | None = None,
    ) -> dict[str, Any]:
        if memory_types is None:
            memory_types = ["messages", "entities", "preferences", "facts"]
        unknown = [t for t in memory_types if t not in _SEARCHABLE]
        if unknown:
            # An unrecognised bucket used to return {} — no error, no results, no way
            # to tell "nothing matched" from "that bucket does not exist". Facts were
            # unreachable through search for exactly this reason and it stayed
            # invisible.
            return {
                "error": f"unknown memory_types {unknown}; valid: {sorted(_SEARCHABLE)}",
                "query": query,
            }
        results: dict[str, list[dict[str, Any]]] = {}
        try:
            if "messages" in memory_types:
                messages = await self.client.short_term.search_messages(
                    query=query,
                    session_id=session_id,
                    limit=limit,
                    threshold=threshold,
                    user_identifier=user_identifier,
                )
                results["messages"] = [
                    {
                        "id": str(msg.id),
                        "role": msg.role.value if hasattr(msg.role, "value") else str(msg.role),
                        "content": msg.content,
                        "timestamp": msg.created_at.isoformat() if msg.created_at else None,
                        "similarity": msg.metadata.get("similarity") if msg.metadata else None,
                    }
                    for msg in messages
                ]
            if "entities" in memory_types:
                # `threshold` reached search_messages but was dropped here and for
                # preferences, so the documented tool parameter did nothing for two of the
                # three default memory types. Inherited from upstream integration.py.
                entities = await self.client.long_term.search_entities(
                    query=query,
                    limit=limit,
                    threshold=threshold,
                    user_identifier=user_identifier,
                )
                results["entities"] = [
                    {
                        "id": str(entity.id),
                        **entity_name_fields(entity),
                        "type": (
                            entity.type.value if hasattr(entity.type, "value") else str(entity.type)
                        ),
                        "description": entity.description,
                    }
                    for entity in entities
                ]
            if "preferences" in memory_types:
                preferences = await self.client.long_term.search_preferences(
                    query=query,
                    limit=limit,
                    threshold=threshold,
                    user_identifier=user_identifier,
                )
                results["preferences"] = [
                    {
                        "id": str(pref.id),
                        "category": pref.category,
                        "preference": pref.preference,
                        "context": pref.context,
                    }
                    for pref in preferences
                ]
            if "facts" in memory_types:
                # Exact subject FIRST, then semantic. A fact's subject is usually a
                # name the caller already knows ("Davide", "Progetto Zeta"), and the
                # semantic leg cannot be trusted to find it: on short strings this
                # embedder scores unrelated pairs within a few hundredths of a real
                # match, so an exact subject would sit indistinguishable in the noise.
                facts = await self.client.long_term.get_facts_about(
                    query, user_identifier=user_identifier
                )
                seen = {str(f.id) for f in facts}
                if len(facts) < limit:
                    for fact in await self.client.long_term.search_facts(
                        query=query,
                        limit=limit - len(facts),
                        threshold=threshold,
                        user_identifier=user_identifier,
                    ):
                        if str(fact.id) not in seen:
                            facts.append(fact)
                            seen.add(str(fact.id))
                results["facts"] = [
                    {
                        "id": str(fact.id),
                        "subject": fact.subject,
                        "predicate": fact.predicate,
                        "object": fact.object,
                        "confidence": fact.confidence,
                    }
                    for fact in facts[:limit]
                ]
            if "traces" in memory_types:
                traces = await self.client.reasoning.get_similar_traces(
                    task=query,
                    limit=limit,
                    user_identifier=user_identifier,
                )
                results["traces"] = [
                    {
                        "id": str(trace.id),
                        "task": trace.task,
                        "outcome": trace.outcome,
                        "success": trace.success,
                    }
                    for trace in traces
                ]
        except Exception as exc:
            logger.error("Error in search: %s", exc)
            return {"error": str(exc)}
        return {"results": results, "query": query}
