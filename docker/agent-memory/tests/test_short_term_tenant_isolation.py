from __future__ import annotations

import asyncio
from uuid import UUID

import pytest

from neo4j_agent_memory.graph import queries
from neo4j_agent_memory.graph.schema import SchemaManager
from neo4j_agent_memory.memory.short_term import MessageRole, ShortTermMemory


class TenantGraph:
    def __init__(self) -> None:
        self.conversations: dict[tuple[str, str], dict] = {}
        self.messages: dict[str, list[str]] = {}
        self.writes: list[tuple[str, dict]] = []

    async def execute_read(self, query: str, params: dict | None = None) -> list[dict]:
        params = params or {}
        if query is queries.GET_CONVERSATION_BY_SESSION_SCOPED:
            conversation = self.conversations.get(
                (params["user_identifier"], params["session_id"])
            )
            return [{"c": conversation}] if conversation else []
        if query is queries.GET_CONVERSATION_SCOPED:
            conversation = next(
                (
                    value
                    for (owner, _session), value in self.conversations.items()
                    if owner == params["user_identifier"] and value["id"] == params["id"]
                ),
                None,
            )
            return [{"c": conversation}] if conversation else []
        return []

    async def execute_write(self, query: str, params: dict | None = None) -> list[dict]:
        params = params or {}
        self.writes.append((query, params))
        if query is queries.ENSURE_CONVERSATION_SCOPED:
            key = (params["user_identifier"], params["session_id"])
            conversation = self.conversations.setdefault(
                key,
                {
                    "id": params["id"],
                    "session_id": params["session_id"],
                    "user_identifier": params["user_identifier"],
                },
            )
            return [{"c": conversation}]
        if query is queries.CREATE_MESSAGE:
            self.messages.setdefault(params["conversation_id"], []).append(params["content"])
            return [{"m": {"id": params["id"]}}]
        return []


class SchemaRecorder:
    def __init__(self) -> None:
        self.schema_queries: list[str] = []

    async def check_constraint_exists(self, _name: str) -> bool:
        return False

    async def execute_schema(self, query: str, parameters: dict | None = None) -> list:
        self.schema_queries.append(query)
        return []


def test_same_session_id_creates_distinct_tenant_conversations():
    async def exercise():
        graph = TenantGraph()
        memory = ShortTermMemory(graph, multi_tenant=True)
        alice_message = await memory.add_message(
            "shared-session",
            MessageRole.USER,
            "alice secret",
            generate_embedding=False,
            extraction_mode="skip",
            user_identifier="alice",
        )
        bob_message = await memory.add_message(
            "shared-session",
            MessageRole.USER,
            "bob secret",
            generate_embedding=False,
            extraction_mode="skip",
            user_identifier="bob",
        )
        return graph, alice_message, bob_message

    graph, alice_message, bob_message = asyncio.run(exercise())

    assert alice_message.conversation_id != bob_message.conversation_id
    assert graph.messages[str(alice_message.conversation_id)] == ["alice secret"]
    assert graph.messages[str(bob_message.conversation_id)] == ["bob secret"]
    assert all("SET c.user_identifier" not in query for query, _params in graph.writes)


def test_explicit_foreign_conversation_id_cannot_be_appended():
    async def exercise():
        graph = TenantGraph()
        memory = ShortTermMemory(graph, multi_tenant=True)
        alice_message = await memory.add_message(
            "shared-session",
            MessageRole.USER,
            "alice secret",
            generate_embedding=False,
            extraction_mode="skip",
            user_identifier="alice",
        )
        with pytest.raises(PermissionError, match="not owned"):
            await memory.add_message(
                "shared-session",
                MessageRole.USER,
                "bob overwrite",
                conversation_id=UUID(str(alice_message.conversation_id)),
                generate_embedding=False,
                extraction_mode="skip",
                user_identifier="bob",
            )
        return graph, alice_message

    graph, alice_message = asyncio.run(exercise())

    assert graph.messages[str(alice_message.conversation_id)] == ["alice secret"]


def test_schema_constrains_the_tenant_session_merge_key():
    async def collect() -> SchemaRecorder:
        recorder = SchemaRecorder()
        await SchemaManager(recorder).setup_constraints()
        return recorder

    recorder = asyncio.run(collect())
    constraint = next(
        query
        for query in recorder.schema_queries
        if "conversation_user_session_unique" in query
    )
    assert "n.user_identifier" in constraint
    assert "n.session_id" in constraint
