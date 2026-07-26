"""A preference must be able to say what it is ABOUT.

``LongTermMemory.add_preference`` has always accepted ``applies_to`` and written
``(:Preference)-[:APPLIES_TO]->(:Entity)``, and the scoped read queries traverse those
edges in four places. No caller could reach it: the parameter stopped at the
``MemoryIntegration`` boundary and was absent from the MCP tool signature, so every
preference the agent stored landed unconnected.

Measured on the live graph 2026-07-26 before this fix: six relationship types, all of them
``HAS_*`` from the ``:User`` node, and every ``:Preference`` with zero outgoing edges. A star,
not a graph — while the read path kept OPTIONAL MATCHing ``APPLIES_TO`` and getting NULL.

This is the cheap half of making the memory a graph: it adds no volume, no extraction and no
retention change, because the structure comes from writes the agent already performs.
"""

from __future__ import annotations

from neo4j_agent_memory.memory.long_term import LongTermMemory, _NamedEntityRef


class RecordingClient:
    def __init__(self, read_rows=None):
        self.read_rows = read_rows or []
        self.reads: list[tuple[str, dict]] = []
        self.writes: list[tuple[str, dict]] = []

    async def execute_read(self, query, params=None):
        self.reads.append((query, params or {}))
        return self.read_rows

    async def execute_write(self, query, params=None):
        self.writes.append((query, params or {}))
        return []


async def test_link_accepts_a_plain_entity_name_from_an_mcp_caller():
    """An MCP caller can only send JSON, so applies_to arrives as bare names."""
    client = RecordingClient()
    memory = LongTermMemory(client, embedder=None)

    await memory._link_preference_to_entity("pref-1", "Caraglio")

    query, params = client.writes[-1]
    assert "APPLIES_TO" in query
    assert params == {"preference_id": "pref-1", "name": "Caraglio"}


async def test_link_still_honours_a_typed_entity_ref():
    """In-process callers pass EntityRef objects; both shapes must resolve."""
    client = RecordingClient()
    memory = LongTermMemory(client, embedder=None)

    await memory._link_preference_to_entity("pref-1", _NamedEntityRef("Caraglio", type="LOCATION"))

    query, params = client.writes[-1]
    assert "APPLIES_TO" in query
    assert params["type"] == "LOCATION"  # the typed branch, not the name-only one


async def test_link_prefers_an_explicit_id_over_the_name():
    """An id identifies exactly one node; a name can collide across types."""
    client = RecordingClient()
    memory = LongTermMemory(client, embedder=None)

    await memory._link_preference_to_entity("pref-1", _NamedEntityRef("Caraglio", id="e-9"))

    query, params = client.writes[-1]
    assert "MATCH (e:Entity {id: $entity_id})" in query
    assert params["entity_id"] == "e-9"
    assert "name" not in params  # never falls back to a fuzzier match when an id is given


async def test_add_preference_links_every_target_it_was_given():
    """The whole point: the preference does not land dangling off the user."""
    client = RecordingClient()
    memory = LongTermMemory(client, embedder=None)

    await memory.add_preference(
        "location",
        "Localita corrente: Caraglio",
        user_identifier="u1",
        generate_embedding=False,
        applies_to=["Caraglio", "Piemonte"],
    )

    linked = [params.get("name") for query, params in client.writes if "APPLIES_TO" in query]
    assert linked == ["Caraglio", "Piemonte"]
