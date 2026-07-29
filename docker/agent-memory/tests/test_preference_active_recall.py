"""MEM-002 regression coverage for active-only preference recall."""

from __future__ import annotations

from neo4j_agent_memory.graph import queries


def test_every_normal_preference_query_requires_an_active_record():
    normal_queries = {
        "embedding": queries.SEARCH_PREFERENCES_BY_EMBEDDING,
        "embedding_scoped": queries.SEARCH_PREFERENCES_BY_EMBEDDING_SCOPED,
        "category": queries.SEARCH_PREFERENCES_BY_CATEGORY,
        "category_scoped": queries.SEARCH_PREFERENCES_BY_CATEGORY_SCOPED,
        "deduplication": queries.FIND_DUPLICATE_PREFERENCES,
    }

    for name, query in normal_queries.items():
        normalized = " ".join(query.split())
        assert ".valid_until IS NULL" in normalized, name
        assert "SUPERSEDED_BY" in normalized, name


def test_time_travel_accessor_remains_explicit_and_separate():
    """Normal query constants are active-only; as-of remains an explicit API."""
    normal = " ".join(queries.SEARCH_PREFERENCES_BY_CATEGORY_SCOPED.split())
    assert "$as_of" not in normal
