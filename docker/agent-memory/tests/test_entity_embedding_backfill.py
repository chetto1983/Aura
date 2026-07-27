"""Tests for entity embedding on extraction, composite dedup constraint, and backfill.

Covers three related fixes:

* Entities created via message-extraction paths (``ShortTermMemory``) now
  embed on write instead of hard-coding ``embedding=None`` — mirroring
  ``LongTermMemory.add_entity`` — and degrade (log + continue) rather than
  raise when the embedder fails, since extraction is a background
  side-effect of a message write that has already succeeded.
* ``SchemaManager`` now backs the ``(name, type, deduplication_scope)``
  MERGE key used by entity creation with a composite uniqueness
  constraint, closing the concurrent-duplicate-creation race.
* ``LongTermMemory.backfill_entity_embeddings`` wires the previously
  orphaned ``GET_ENTITIES_WITHOUT_EMBEDDINGS`` / ``UPDATE_ENTITY_EMBEDDING``
  queries into a resumable, idempotent repair pass.
"""

from uuid import uuid4

from neo4j_agent_memory.extraction.base import ExtractedEntity, ExtractionResult
from neo4j_agent_memory.graph import queries
from neo4j_agent_memory.graph.schema import SchemaManager
from neo4j_agent_memory.memory.long_term import LongTermMemory
from neo4j_agent_memory.memory.short_term import Message, MessageRole, ShortTermMemory


class RecordingClient:
    """Fake Neo4jClient: records writes, returns a fixed read result."""

    def __init__(self, read_rows=None):
        self.read_rows = read_rows or []
        self.reads = []
        self.writes = []

    async def execute_read(self, query, params=None):
        self.reads.append((query, params or {}))
        return self.read_rows

    async def execute_write(self, query, params=None):
        self.writes.append((query, params or {}))
        return []


class FakeExtractor:
    """Fake EntityExtractor returning a fixed set of entities for any input."""

    def __init__(self, entities):
        self._entities = entities

    async def extract(self, content):
        return ExtractionResult(entities=self._entities, relations=[], source_text=content)


class RecordingEmbedder:
    """Fake Embedder that records what it was asked to embed."""

    def __init__(self):
        self.calls: list[str] = []

    async def embed(self, text):
        self.calls.append(text)
        return [1.0, 2.0, 3.0]

    async def embed_batch(self, texts):
        return [await self.embed(t) for t in texts]


class RaisingEmbedder:
    """Fake Embedder that always fails — simulates a transient outage."""

    async def embed(self, text):
        raise RuntimeError("embedder unavailable")

    async def embed_batch(self, texts):
        raise RuntimeError("embedder unavailable")


def _entity_create_params(client: RecordingClient) -> dict:
    """Find the single CREATE_ENTITY-shaped write (the one carrying 'embedding')."""
    matches = [params for _, params in client.writes if "embedding" in params]
    assert len(matches) == 1, f"expected exactly one entity-create write, got {len(matches)}"
    return matches[0]


def _make_message() -> Message:
    return Message(id=uuid4(), role=MessageRole.USER, content="David lives here", conversation_id=uuid4())


async def test_extract_and_link_entities_embeds_when_embedder_present():
    client = RecordingClient()
    embedder = RecordingEmbedder()
    extractor = FakeExtractor([ExtractedEntity(name="David", type="PERSON")])
    memory = ShortTermMemory(client, embedder=embedder, extractor=extractor)

    await memory._extract_and_link_entities(_make_message(), extract_relations=False)

    params = _entity_create_params(client)
    assert params["embedding"] == [1.0, 2.0, 3.0]
    assert embedder.calls == ["David"]


async def test_extract_and_link_entities_no_embedder_degrades_to_none():
    client = RecordingClient()
    extractor = FakeExtractor([ExtractedEntity(name="David", type="PERSON")])
    memory = ShortTermMemory(client, embedder=None, extractor=extractor)

    await memory._extract_and_link_entities(_make_message(), extract_relations=False)

    params = _entity_create_params(client)
    assert params["embedding"] is None


async def test_extract_and_link_entities_embedder_failure_degrades_to_none():
    client = RecordingClient()
    extractor = FakeExtractor([ExtractedEntity(name="David", type="PERSON")])
    memory = ShortTermMemory(client, embedder=RaisingEmbedder(), extractor=extractor)

    # Must not raise: the message this extraction runs against is already
    # persisted, so a broken embedder must degrade, not lose the write.
    await memory._extract_and_link_entities(_make_message(), extract_relations=False)

    params = _entity_create_params(client)
    assert params["embedding"] is None


async def test_extract_entities_from_session_embeds_when_embedder_present():
    message_id = str(uuid4())
    client = RecordingClient(read_rows=[{"id": message_id, "content": "David lives here"}])
    embedder = RecordingEmbedder()
    extractor = FakeExtractor([ExtractedEntity(name="David", type="PERSON")])
    memory = ShortTermMemory(client, embedder=embedder, extractor=extractor)

    stats = await memory.extract_entities_from_session("session-1", extract_relations=False)

    assert stats["entities_extracted"] == 1
    params = _entity_create_params(client)
    assert params["embedding"] == [1.0, 2.0, 3.0]


class FakeEntityGraphClient:
    """Fake Neo4jClient simulating the WHERE embedding IS NULL semantics.

    Real enough to catch a skip-drift pagination bug: rows fixed by
    UPDATE_ENTITY_EMBEDDING actually leave the `embedding IS NULL` result
    set on the next read, matching real Cypher semantics.
    """

    def __init__(self, entities: list[dict]):
        self.entities = entities
        self.write_calls: list[tuple[str, dict]] = []

    async def execute_read(self, query, params=None):
        params = params or {}
        if query is queries.COUNT_ENTITIES_WITHOUT_EMBEDDINGS:
            return [{"count": sum(1 for e in self.entities if e["embedding"] is None)}]
        if query is queries.GET_ENTITIES_WITHOUT_EMBEDDINGS:
            missing = [e for e in self.entities if e["embedding"] is None]
            skip, limit = params["skip"], params["limit"]
            page = missing[skip : skip + limit]
            return [
                {"id": e["id"], "name": e["name"], "type": e["type"], "description": None}
                for e in page
            ]
        raise AssertionError(f"unexpected read query: {query!r}")

    async def execute_write(self, query, params=None):
        params = params or {}
        self.write_calls.append((query, params))
        assert query is queries.UPDATE_ENTITY_EMBEDDING
        for e in self.entities:
            if e["id"] == params["id"]:
                e["embedding"] = params["embedding"]
        return []


async def test_backfill_entity_embeddings_fixes_only_missing_and_is_idempotent():
    already_embedded = {"id": "e-0", "name": "Existing", "type": "PERSON", "embedding": [9.0]}
    entities = [
        already_embedded,
        {"id": "e-1", "name": "David", "type": "PERSON", "embedding": None},
        {"id": "e-2", "name": "Caraglio", "type": "LOCATION", "embedding": None},
    ]
    client = FakeEntityGraphClient(entities)
    embedder = RecordingEmbedder()
    memory = LongTermMemory(client, embedder=embedder)

    fixed = await memory.backfill_entity_embeddings(batch_size=1)

    assert fixed == 2
    assert already_embedded["embedding"] == [9.0]  # untouched
    assert {e["id"] for e in entities if e["embedding"] is not None} == {"e-0", "e-1", "e-2"}
    assert sorted(embedder.calls) == ["Caraglio", "David"]  # embedded by name, not id

    # Idempotent: re-running touches nothing further.
    embedder.calls.clear()
    client.write_calls.clear()
    fixed_again = await memory.backfill_entity_embeddings(batch_size=1)
    assert fixed_again == 0
    assert client.write_calls == []
    assert embedder.calls == []


async def test_backfill_entity_embeddings_noop_without_embedder():
    client = FakeEntityGraphClient([{"id": "e-1", "name": "David", "type": "PERSON", "embedding": None}])
    memory = LongTermMemory(client, embedder=None)

    fixed = await memory.backfill_entity_embeddings()

    assert fixed == 0
    assert client.write_calls == []


def test_create_composite_constraint_query_shape():
    query = queries.create_composite_constraint_query(
        "entity_name_type_scope_unique", "Entity", ["name", "type", "deduplication_scope"]
    )
    assert "CREATE CONSTRAINT entity_name_type_scope_unique IF NOT EXISTS" in query
    assert "FOR (n:Entity)" in query
    assert "REQUIRE (n.name, n.type, n.deduplication_scope) IS UNIQUE" in query


class FakeSchemaClient:
    def __init__(self):
        self.existing_constraints: set[str] = set()
        self.created: list[str] = []

    async def check_constraint_exists(self, name):
        return name in self.existing_constraints

    async def check_index_exists(self, name):
        return True  # not under test here — keep setup_constraints() focused on constraints

    async def execute_schema(self, query, params=None):
        # DDL goes through the schema path, never execute_write: the latter appends the
        # corpus-epoch bump, and Neo4j rejects a write in the same transaction as a schema
        # modification (see tests/test_schema_ddl_transaction_type.py).
        self.created.append(query)
        return []


async def test_setup_constraints_includes_composite_entity_constraint():
    client = FakeSchemaClient()
    manager = SchemaManager(client)

    await manager.setup_constraints()

    composite = [
        q for q in client.created if "IS UNIQUE" in q and "n.name, n.type, n.deduplication_scope" in q
    ]
    assert len(composite) == 1
    assert "entity_name_type_scope_unique" in composite[0]


async def test_create_composite_constraint_skips_when_already_exists():
    client = FakeSchemaClient()
    client.existing_constraints.add("entity_name_type_scope_unique")
    manager = SchemaManager(client)

    await manager._create_composite_constraint(
        "entity_name_type_scope_unique", "Entity", ["name", "type", "deduplication_scope"]
    )

    assert client.created == []
