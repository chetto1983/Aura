"""The Aura deployment requires a complete 1024-dimensional vector schema."""

from __future__ import annotations

import asyncio

import pytest

from neo4j_agent_memory.core.exceptions import SchemaError
from neo4j_agent_memory.graph.schema import DEFAULT_VECTOR_DIMENSIONS, SchemaManager
from neo4j_agent_memory.llm.errors import EmbeddingDimensionMismatchError


class _VectorClient:
    def __init__(self, rows: list[dict] | None = None, schema_error: Exception | None = None):
        self.rows = rows or []
        self.schema_error = schema_error
        self.schema_queries: list[str] = []

    async def check_index_exists(self, _name: str) -> bool:
        return False

    async def execute_schema(self, query: str) -> list:
        if self.schema_error is not None:
            raise self.schema_error
        self.schema_queries.append(query)
        return []

    async def execute_read(self, _query: str) -> list[dict]:
        return self.rows


def _row(name: str, dimensions: object) -> dict:
    return {"name": name, "options": {"indexConfig": {"vector.dimensions": dimensions}}}


def _complete_rows(dimensions: int = DEFAULT_VECTOR_DIMENSIONS) -> list[dict]:
    return [_row(name, dimensions) for name, _, _ in SchemaManager._MANAGED_VECTOR_INDEXES]


def test_default_vector_schema_is_native_1024():
    client = _VectorClient()

    asyncio.run(SchemaManager(client).setup_vector_indexes())

    assert DEFAULT_VECTOR_DIMENSIONS == 1024
    assert len(client.schema_queries) == len(SchemaManager._MANAGED_VECTOR_INDEXES)
    assert all("`vector.dimensions`: 1024" in query for query in client.schema_queries)


def test_vector_index_creation_failure_is_fatal():
    client = _VectorClient(schema_error=RuntimeError("permission denied"))

    with pytest.raises(SchemaError, match="Failed to create vector index message_embedding_idx"):
        asyncio.run(SchemaManager(client).setup_vector_indexes())


@pytest.mark.parametrize(
    ("rows", "message"),
    [
        (_complete_rows()[:-1], "missing: step_embedding_idx"),
        (
            [
                _row(name, True if name == "task_embedding_idx" else DEFAULT_VECTOR_DIMENSIONS)
                for name, _, _ in SchemaManager._MANAGED_VECTOR_INDEXES
            ],
            "dimension unavailable: task_embedding_idx",
        ),
    ],
)
def test_incomplete_vector_manifest_is_fatal(rows: list[dict], message: str):
    with pytest.raises(SchemaError, match=message):
        asyncio.run(
            SchemaManager(_VectorClient(rows)).validate_vector_index_dimensions(
                DEFAULT_VECTOR_DIMENSIONS
            )
        )


def test_dimension_mismatch_is_fatal():
    rows = _complete_rows()
    rows[0] = _row("message_embedding_idx", 768)

    with pytest.raises(EmbeddingDimensionMismatchError) as exc_info:
        asyncio.run(
            SchemaManager(_VectorClient(rows)).validate_vector_index_dimensions(
                DEFAULT_VECTOR_DIMENSIONS
            )
        )

    assert exc_info.value.expected_dimensions == 1024
    assert exc_info.value.actual_dimensions == 768
    assert exc_info.value.index_name == "message_embedding_idx"
