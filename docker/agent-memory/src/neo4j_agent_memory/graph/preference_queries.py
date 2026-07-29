"""Canonical active-only Cypher for normal preference recall.

Time-travel remains in ``LongTermMemory.get_preferences_for(as_of=...)``. Every
query here backs current-state recall or deduplication and therefore excludes
both closed validity intervals and explicit supersession edges.
"""

_ACTIVE_NODE = """
  AND node.valid_until IS NULL
  AND NOT (node)-[:SUPERSEDED_BY]->(:Preference)
"""

_ACTIVE_PREFERENCE = """
WHERE p.valid_until IS NULL
  AND NOT (p)-[:SUPERSEDED_BY]->(:Preference)
"""

SEARCH_PREFERENCES_BY_EMBEDDING = f"""
CALL db.index.vector.queryNodes('preference_embedding_idx', $limit, $embedding)
YIELD node, score
WHERE score >= $threshold
{_ACTIVE_NODE}
RETURN node AS p, score
ORDER BY score DESC
"""

SEARCH_PREFERENCES_BY_EMBEDDING_SCOPED = f"""
CALL db.index.vector.queryNodes('preference_embedding_idx', $candidate_limit, $embedding)
YIELD node, score
MATCH (:User {{identifier: $user_identifier}})-[:HAS_PREFERENCE]->(node)
WHERE score >= $threshold
{_ACTIVE_NODE}
RETURN node AS p, score
ORDER BY score DESC
LIMIT $limit
"""

SEARCH_PREFERENCES_BY_CATEGORY = f"""
MATCH (p:Preference {{category: $category}})
{_ACTIVE_PREFERENCE}
RETURN p
ORDER BY p.confidence DESC, p.created_at DESC
LIMIT $limit
"""

SEARCH_PREFERENCES_BY_CATEGORY_SCOPED = f"""
MATCH (:User {{identifier: $user_identifier}})-[:HAS_PREFERENCE]->(
    p:Preference {{category: $category}}
)
{_ACTIVE_PREFERENCE}
RETURN p
ORDER BY p.confidence DESC, p.created_at DESC
LIMIT $limit
"""

FIND_DUPLICATE_PREFERENCES = f"""
CALL db.index.vector.queryNodes('preference_embedding_idx', $limit, $embedding)
YIELD node, score
WHERE score >= $threshold
  AND node.category = $category
  AND ($deduplication_scope IS NULL OR node.deduplication_scope = $deduplication_scope)
{_ACTIVE_NODE}
RETURN node AS p, score
ORDER BY score DESC
"""
