"""Read-after-write evidence returned by memory mutation facades."""

from __future__ import annotations

from typing import Any

_FACT_SUBJECT_ENTITY = """
MATCH (:User {identifier: $user_identifier})-[:HAS_FACT]->(f:Fact {id: $fact_id})
MATCH (f)-[:ABOUT_SUBJECT]->(e:Entity)
RETURN e.id AS id, e.name AS name
LIMIT 1
"""


async def fact_subject_entity(
    graph: Any,
    *,
    fact_id: str,
    user_identifier: str | None,
) -> dict[str, str] | None:
    """Return the exact subject entity linked to the caller's stored fact."""
    if user_identifier is None:
        return None
    rows = await graph.execute_read(
        _FACT_SUBJECT_ENTITY,
        {
            "fact_id": fact_id,
            "user_identifier": user_identifier,
        },
    )
    if not rows:
        return None
    return {
        "id": str(rows[0]["id"]),
        "name": str(rows[0]["name"]),
    }
