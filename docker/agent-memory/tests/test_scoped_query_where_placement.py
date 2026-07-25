"""Structural regression cover for the cross-user data leak found live on 2026-07-25.

Every ``*_SCOPED`` query in ``queries.py`` is supposed to enforce ownership. Several of
them wrote the ownership predicate as a ``WHERE`` directly following an ``OPTIONAL
MATCH``, e.g.::

    MATCH (e:Entity {type: $type})
    OPTIONAL MATCH (:User {identifier: $user_identifier})-[ue:HAS_ENTITY]->(e)
    WHERE ue IS NOT NULL
    RETURN e

In Cypher, a ``WHERE`` that directly follows an ``OPTIONAL MATCH`` belongs to *that*
``OPTIONAL MATCH`` -- it constrains which optional pattern gets bound, it does not
filter the outer row stream. Rows whose optional pattern found nothing still flow
through with NULLs and get returned. So the ownership check did nothing, and every one
of these "scoped" reads returned other users' data.

Proved live against ``SEARCH_ENTITIES_BY_EMBEDDING_SCOPED``: querying as user
``claude-dogfood`` (who owns exactly one entity, "Marco Bianchi") returned "Davide" --
a different user's entity -- ranked ABOVE the caller's own entity by score.

The fix is a ``WITH`` carrying the needed variables between the ``OPTIONAL MATCH`` and
the ``WHERE``, turning the predicate back into a real row filter. The existing unit
tests in this directory use fake in-memory clients and cannot catch this: the bug is
pure Cypher semantics, invisible to anything that doesn't reason about the query text
itself. Hence this test parses ``queries.py`` structurally, with no database required.
"""

from __future__ import annotations

import re

from neo4j_agent_memory.graph import queries

_WHERE_RE = re.compile(r"^WHERE\b", re.IGNORECASE)
_OPTIONAL_MATCH_RE = re.compile(r"^OPTIONAL MATCH\b", re.IGNORECASE)
_COMMENT_RE = re.compile(r"^//")


def _scoped_query_names() -> list[str]:
    return [
        name
        for name in dir(queries)
        if name.endswith("_SCOPED") and isinstance(getattr(queries, name), str)
    ]


def _significant_lines(query: str) -> list[str]:
    """Non-blank, non-comment lines of a Cypher query template, in order."""
    lines = []
    for raw_line in query.splitlines():
        stripped = raw_line.strip()
        if not stripped or _COMMENT_RE.match(stripped):
            continue
        lines.append(stripped)
    return lines


def _where_clauses_glued_to_optional_match(query: str) -> list[str]:
    """WHERE lines whose nearest preceding statement is an OPTIONAL MATCH.

    Such a WHERE is parsed by Cypher as part of the OPTIONAL MATCH pattern, not as an
    outer row filter -- exactly the shape that let the ownership check silently no-op.
    """
    lines = _significant_lines(query)
    offenders = []
    for i, line in enumerate(lines):
        if not _WHERE_RE.match(line):
            continue
        previous = lines[i - 1] if i > 0 else ""
        if _OPTIONAL_MATCH_RE.match(previous):
            offenders.append(line)
    return offenders


def test_scoped_query_discovery_finds_the_known_queries():
    # Guards the discovery mechanism itself: if this drifts to zero, the test below
    # would silently check nothing.
    names = _scoped_query_names()
    assert "GET_CONVERSATION_SCOPED" in names
    assert "SEARCH_ENTITIES_BY_EMBEDDING_SCOPED" in names
    assert "SEARCH_ENTITIES_BY_TYPE_SCOPED" in names
    assert len(names) >= 7


def test_no_scoped_query_attaches_where_directly_to_optional_match():
    """Regression test for the cross-user leak.

    Any *_SCOPED query -- present or added in the future -- that puts its ownership
    (or any other) predicate in a WHERE directly glued to an OPTIONAL MATCH is
    filtering nothing on the outer rows. This must fail loudly instead of shipping.
    """
    broken = {
        name: offenders
        for name in _scoped_query_names()
        if (offenders := _where_clauses_glued_to_optional_match(getattr(queries, name)))
    }

    assert not broken, (
        "these _SCOPED queries have a WHERE clause directly attached to an "
        "OPTIONAL MATCH -- it constrains the optional pattern, not the outer rows, "
        "so the predicate does not filter anything. Insert a WITH (carrying the "
        "variables the predicate needs) between the OPTIONAL MATCH and the WHERE:\n"
        f"{broken}"
    )
