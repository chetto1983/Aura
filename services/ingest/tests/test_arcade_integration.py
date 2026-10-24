"""Real integration test against a running ArcadeDB -- no mocks.

Proves the actual claim ensure_schema exists for: declaring Passage.embedding
as ARRAY_OF_FLOATS (with its LSM_VECTOR index) BEFORE any write reaches it is
what lets a Bolt-side Cypher MERGE keep that type, instead of degrading to an
untyped list LSM_VECTOR then refuses to index.

Uses a single disposable database (aura_t5_it), dropped and recreated at
setup and dropped again at teardown -- even on failure -- via a fixture
`finally`. NEVER aura_memory or any mem_<uuid>: those are production
identities.

Run (rebuild first -- a new module is invisible to the image until then):
    docker build -t aura-ingest:local -f docker/aura-ingest/Dockerfile .
    PW=$(docker inspect aura-arcadedb --format '{{range .Config.Env}}{{println .}}{{end}}' \
        | grep -oP '(?<=rootPassword=)\\S+')
    MSYS_NO_PATHCONV=1 docker run --rm -i --network aura_default \
        -e ARCADEDB_PASSWORD="$PW" --entrypoint python aura-ingest:local \
        -m pytest /app/ingest/tests/test_arcade_integration.py -v
"""

from __future__ import annotations

import json
import os

import pytest
from neo4j import GraphDatabase

from ingest.arcade import DOCUMENT_TYPE, _post, ensure_schema, indexed_source_keys, remove_document
from ingest.tests import disposable
from ingest.tests.disposable import ARCADE_HTTP, AUTH

ARCADE_BOLT = os.environ.get("ARCADE_BOLT", "bolt://arcadedb:7687")

TEST_DB = "aura_t5_it"
DIMS = 768


def _command(database: str, statement: str) -> None:
    _post(
        ARCADE_HTTP, f"/api/v1/command/{database}",
        {"language": "sql", "command": statement}, AUTH, 30.0,
    )


@pytest.fixture
def disposable_database():
    disposable.drop(TEST_DB)  # clean slate from a prior failed run
    try:
        yield TEST_DB
    finally:
        disposable.drop(TEST_DB)


def test_ensure_schema_succeeds_on_a_fresh_database(disposable_database):
    ensure_schema(ARCADE_HTTP, disposable_database, AUTH, DIMS)


def test_ensure_schema_is_idempotent(disposable_database):
    ensure_schema(ARCADE_HTTP, disposable_database, AUTH, DIMS)
    ensure_schema(ARCADE_HTTP, disposable_database, AUTH, DIMS)  # must not raise


def test_ensure_schema_removes_retired_values_properties_and_types(disposable_database):
    ensure_schema(ARCADE_HTTP, disposable_database, AUTH, DIMS)
    for statement in (
        "CREATE PROPERTY Passage.document_id IF NOT EXISTS STRING",
        "CREATE PROPERTY Passage.active IF NOT EXISTS BOOLEAN",
        "CREATE PROPERTY Passage.self_ref IF NOT EXISTS STRING",
        "CREATE INDEX IF NOT EXISTS ON Passage (active, document_id) NOTUNIQUE",
        "CREATE VERTEX TYPE DocumentProjection IF NOT EXISTS",
        "CREATE EDGE TYPE HAS_PASSAGE IF NOT EXISTS",
        ("INSERT INTO Passage SET passage_key = 'retired', document_id = 'old', "
         "active = true, self_ref = '/old'"),
    ):
        _command(disposable_database, statement)

    ensure_schema(ARCADE_HTTP, disposable_database, AUTH, DIMS)

    schema = disposable.query(
        disposable_database,
        "SELECT name, properties FROM schema:types",
        {},
    )
    by_name = {row["name"]: row for row in schema}
    names = {
        prop["name"] if isinstance(prop, dict) else prop
        for prop in by_name["Passage"]["properties"]
    }
    assert not {"document_id", "active", "self_ref"} & names
    assert "DocumentProjection" not in by_name
    assert "HAS_PASSAGE" not in by_name

    rows = disposable.query(
        disposable_database,
        "SELECT FROM Passage WHERE passage_key = :key",
        {"key": "retired"},
    )
    assert len(rows) == 1
    assert not {"document_id", "active", "self_ref"} & rows[0].keys()


def test_bolt_written_vector_is_typed_and_ann_retrievable(disposable_database):
    ensure_schema(ARCADE_HTTP, disposable_database, AUTH, DIMS)

    # Distinct DIRECTIONS, not scalar multiples: similarity is COSINE, which
    # ignores magnitude, so v and 2v are indistinguishable to the index --
    # this already produced one false failure while designing this test.
    v1 = [0.0] * DIMS
    v1[0] = 1.0
    v2 = [0.0] * DIMS
    v2[1] = 1.0

    driver = GraphDatabase.driver(ARCADE_BOLT, auth=AUTH)
    try:
        with driver.session(database=disposable_database) as session:
            # MERGE, not CREATE: this is the exact write shape CocoIndex's
            # stock Neo4j target issues for declare_record (amendment #118).
            for key, text, embedding in (("p1", "alpha passage", v1), ("p2", "beta passage", v2)):
                session.run(
                    "MERGE (p:Passage {passage_key: $k}) SET p.text = $t, p.embedding = $e",
                    k=key, t=text, e=embedding,
                ).consume()
        driver.verify_connectivity()
    finally:
        driver.close()

    rows = disposable.query(
        disposable_database,
        "SELECT embedding.type() AS t, embedding.size() AS n FROM Passage WHERE passage_key = :k",
        {"k": "p1"},
    )
    assert len(rows) == 1
    assert rows[0]["t"] == "ARRAY_OF_FLOATS", (
        "embedding kept an untyped list instead of ARRAY_OF_FLOATS -- schema ran after the write?"
    )
    assert rows[0]["n"] == DIMS

    ann = disposable.query(
        disposable_database,
        "SELECT expand(`vector.neighbors`('Passage[embedding]', :q, 3))",
        {"q": v1},
    )
    assert ann, "the vector index returned nothing -- schema ran after the write?"
    assert any(row.get("passage_key") == "p1" for row in ann), (
        "nearest neighbour of v1 did not include the passage written with v1"
    )


def test_rows_written_before_the_stamp_are_counted_through_its_index(disposable_database):
    """The upgrade adds the stamp to a database whose rows have none, and the documents gate
    must count every one of them. With the index's default null strategy it would count none
    (arcadedb-docs reference/sql/sql-indexes.adoc), so this builds the index over rows that
    already exist, the way the upgrade does."""
    ensure_schema(ARCADE_HTTP, disposable_database, AUTH, DIMS)
    _command(disposable_database, "DROP PROPERTY Passage.embed_space FORCE")
    _command(disposable_database, "INSERT INTO Passage SET passage_key = 'before', text = 'x'")

    ensure_schema(ARCADE_HTTP, disposable_database, AUTH, DIMS)
    _command(disposable_database,
             "INSERT INTO Passage SET passage_key = 'after', text = 'y', embed_space = 'es1-aaaaaaaaaaaaaaaa'")

    unstamped = disposable.query(disposable_database,
                                 "SELECT count(*) AS n FROM Passage WHERE embed_space IS NULL")
    stamped = disposable.query(disposable_database,
                               "SELECT count(*) AS n FROM Passage WHERE embed_space = :s",
                               {"s": "es1-aaaaaaaaaaaaaaaa"})
    assert unstamped[0]["n"] == 1, "the stamp index hides the rows written before it"
    assert stamped[0]["n"] == 1


def test_remove_document_leaves_only_the_other_keys_rows(disposable_database):
    ensure_schema(ARCADE_HTTP, disposable_database, AUTH, DIMS)
    driver = GraphDatabase.driver(ARCADE_BOLT, auth=AUTH)
    try:
        with driver.session(database=disposable_database) as session:
            # CocoIndex's own write shape, so the rows are the ones a lost state leaves behind.
            for key in ("gone.md", "kept.md"):
                session.run(
                    "MERGE (d:IndexedDocument {search_document_id: $id}) SET d.source_key = $k",
                    id=f"doc-{key}", k=key,
                ).consume()
                for ordinal in (0, 1):
                    session.run(
                        "MERGE (p:Passage {passage_key: $pk}) SET p.source_key = $k",
                        pk=f"{key}#{ordinal}", k=key,
                    ).consume()
    finally:
        driver.close()

    remove_document(ARCADE_HTTP, disposable_database, AUTH, "gone.md", 30.0)

    for type_name, kept in (("IndexedDocument", 1), ("Passage", 2)):
        rows = disposable.query(
            disposable_database,
            f"SELECT source_key, count(*) AS n FROM {type_name} GROUP BY source_key",
        )
        assert {row["source_key"]: row["n"] for row in rows} == {"kept.md": kept}, type_name


def test_indexed_source_keys_reads_past_the_http_default_limit(disposable_database):
    """A query that states no limit is cut at arcadedb.server.httpQueryDefaultLimit (20,000),
    flagged only by `truncated: true`, which the audit does not read: every key past the cut
    would be reported MISSING. Measured 2026-10-24 on 26.10.2: 30,000 rows came back as
    20,000 with HTTP 200."""
    ensure_schema(ARCADE_HTTP, disposable_database, AUTH, DIMS)
    rows = 20_001
    for start in range(0, rows, 1000):
        batch = json.dumps([
            {"search_document_id": f"doc-{i}", "source_key": f"k{i}"}
            for i in range(start, min(rows, start + 1000))
        ])
        _command(disposable_database, f"INSERT INTO {DOCUMENT_TYPE} CONTENT {batch}")

    assert len(indexed_source_keys(ARCADE_HTTP, disposable_database, AUTH, 60.0)) == rows
