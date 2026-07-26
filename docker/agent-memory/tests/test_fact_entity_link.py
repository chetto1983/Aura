"""A fact must attach to the entity its subject names.

Before this, ``Fact{subject: "Davide"}`` and ``Entity{name: "Davide"}`` were two
unconnected nodes: the fact's subject was a string that happened to spell the entity's
name. Nothing could get from the person to what was known about them by structure, and
facts are excluded from ``memory_search`` entirely, so the only route to a fact was to
guess its subject wording exactly.

The edge is ``(:Fact)-[:ABOUT_SUBJECT]->(:Entity)``, and the tests below pin the three
decisions that make it safe rather than the fact that it exists:

* it is NOT added to ``ENTITY_IN_USER_SCOPE`` — a new scope-granting edge fed by
  LLM-generated free text would be an entity-enumeration oracle;
* the subject resolves through an OWNED-entity lookup, never a bare
  ``MERGE (e:Entity {name: $name})`` — that is how ``_link_preference_to_entity``
  attaches to whatever node already carries a name, and it must not be copied;
* an unresolvable subject creates nothing, because fact subjects are not always names
  (the onboarding sentinel writes facts whose subject is an identity UUID).
"""

from __future__ import annotations

from neo4j_agent_memory.graph import queries

OWNER = "e3c8eb3b-7f0c-4bbc-bd6c-3311f9623c3e"
OTHER = "019f9dd4-f2f6-7935-82a8-bb81af98361e"


class RecordingClient:
    """Fake Neo4j client that records writes and replays canned reads."""

    def __init__(self, read_rows: list[dict] | None = None):
        self.reads: list[tuple[str, dict]] = []
        self.writes: list[tuple[str, dict]] = []
        self._read_rows = read_rows or []

    async def execute_read(self, query, params=None):
        self.reads.append((query, params or {}))
        return self._read_rows

    async def execute_write(self, query, params=None):
        self.writes.append((query, params or {}))
        return []


def test_about_subject_grants_no_read_access_to_an_entity():
    """The scope predicate must not learn about the new edge.

    Every scoped entity read enumerates four typed patterns. Adding a fifth here would
    mean: write a fact whose subject spells a name, gain read access to whoever's entity
    carries it. The subject is LLM-generated free text, so that is an enumeration oracle.

    If this ever becomes desirable, it is not a one-line change: DELETE_ENTITY_SCOPED must
    cut the edge too, or `forget` starts lying again (tests/test_forget_leaves_no_read_path
    asserts granted-edges are a subset of cut-edges, and its SCOPE_GRANTING list is
    hardcoded, so it would pass vacuously).
    """
    for name in (
        "ENTITY_IN_USER_SCOPE",
        "SEARCH_ENTITIES_BY_EMBEDDING_SCOPED",
        "SEARCH_ENTITIES_BY_TYPE_SCOPED",
    ):
        assert "ABOUT_SUBJECT" not in getattr(queries, name), (
            f"{name} now accepts ABOUT_SUBJECT as proof of scope. See the docstring above: "
            "DELETE_ENTITY_SCOPED and the forget contract have to change with it."
        )


def test_forgetting_an_entity_cuts_the_callers_fact_edges():
    """Otherwise the untyped orphan probe never sees zero.

    DELETE_ENTITY_SCOPED ends with `OPTIONAL MATCH (e)-[rel]-()` and removes the node only
    when `remaining = 0`. That probe matches ANY relationship type, so one surviving
    ABOUT_SUBJECT edge pins the node forever and `removed_node` is permanently false.
    """
    assert "ABOUT_SUBJECT" in queries.DELETE_ENTITY_SCOPED
    cut = queries.DELETE_ENTITY_SCOPED.index("[about:ABOUT_SUBJECT]")
    probe = queries.DELETE_ENTITY_SCOPED.index("OPTIONAL MATCH (e)-[rel]-()")
    assert cut < probe, "the fact edges must be cut BEFORE the orphan probe counts them"
    # Only the caller's own facts: a shared entity survives for everyone else.
    assert "(u)-[:HAS_FACT]->(:Fact)-[about:ABOUT_SUBJECT]->(e)" in queries.DELETE_ENTITY_SCOPED


def test_reading_facts_from_an_entity_is_rooted_at_the_caller():
    """Traversing FROM the entity would return facts belonging to whoever shares it."""
    query = queries.GET_FACTS_ABOUT_ENTITY_SCOPED
    assert query.strip().startswith("MATCH (:User {identifier: $user_identifier})-[:HAS_FACT]->")
    assert "OPTIONAL MATCH" not in query, (
        "an OPTIONAL MATCH here would reopen the dead-ownership-predicate class; "
        "see tests/test_scoped_query_where_placement.py"
    )


def test_subject_resolution_is_exact_owned_and_never_merges():
    """The lookup must not create, and must not reach past what the caller owns."""
    query = queries.FIND_SCOPED_ENTITY_BY_NAME
    assert "MERGE" not in query, "resolution must never create or capture an entity node"
    assert "MENTIONS" not in query and "APPLIES_TO" not in query, (
        "a name-keyed lookup that accepted the weaker scope edges would attach the "
        "caller's fact to whatever node happens to carry that name"
    )
    assert "toLower(e.name) = toLower($name)" in query, (
        "resolution is exact; similarity would link a fact about 'Davide' to 'David' — "
        "the duplicate the operator is trying to remove"
    )


def test_link_writes_both_sides_by_id():
    assert "MERGE (f)-[r:ABOUT_SUBJECT]->(e)" in queries.LINK_FACT_TO_ENTITY
    assert "{name:" not in queries.LINK_FACT_TO_ENTITY
    assert queries.LINK_FACT_TO_ENTITY.count("MATCH") == 2


async def test_an_unresolvable_subject_links_nothing_and_creates_nothing():
    """Fact subjects are not always names — onboarding writes facts subject'd by UUID."""
    from neo4j_agent_memory.memory.long_term import LongTermMemory

    client = RecordingClient(read_rows=[])
    memory = LongTermMemory.__new__(LongTermMemory)
    memory._client = client

    await memory._link_fact_to_subject_entity(OWNER, "fact-1", "9f1b-not-a-name")

    assert client.writes == [], "no entity may be created for an unresolved subject"


async def test_a_resolved_subject_links_the_fact_to_that_entity():
    from neo4j_agent_memory.memory.long_term import LongTermMemory

    client = RecordingClient(read_rows=[{"id": "entity-42"}])
    memory = LongTermMemory.__new__(LongTermMemory)
    memory._client = client

    await memory._link_fact_to_subject_entity(OWNER, "fact-1", "Davide")

    assert len(client.reads) == 1
    read_query, read_params = client.reads[0]
    assert read_query is queries.FIND_SCOPED_ENTITY_BY_NAME
    assert read_params["user_identifier"] == OWNER, "resolution must be scoped to the caller"

    assert len(client.writes) == 1
    write_query, write_params = client.writes[0]
    assert write_query is queries.LINK_FACT_TO_ENTITY
    assert write_params == {"fact_id": "fact-1", "entity_id": "entity-42"}
