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

import os

import pytest
from neo4j import GraphDatabase

from ingest.arcade import ArcadeSchemaError, _post, ensure_schema

ARCADE_HTTP = os.environ.get("ARCADE_HTTP", "http://arcadedb:2480")
ARCADE_BOLT = os.environ.get("ARCADE_BOLT", "bolt://arcadedb:7687")
# ArcadeDB requires the rootPassword baked into the running container -- see
# this file's docstring for how to read it without hardcoding it.
ARCADE_PW = os.environ["ARCADEDB_PASSWORD"]
AUTH = ("root", ARCADE_PW)

TEST_DB = "aura_t5_it"
DIMS = 768


def _drop_database_ignoring_absence(database: str) -> None:
    try:
        _post(ARCADE_HTTP, "/api/v1/server", {"command": f"drop database {database}"}, AUTH, 30.0)
    except ArcadeSchemaError as exc:
        if "not exist" not in str(exc).lower():
            raise


def _query(database: str, statement: str, params: dict) -> list[dict]:
    body = _post(
        ARCADE_HTTP, f"/api/v1/query/{database}",
        {"language": "sql", "command": statement, "params": params}, AUTH, 30.0,
    )
    return body["result"]


@pytest.fixture
def disposable_database():
    _drop_database_ignoring_absence(TEST_DB)  # clean slate from a prior failed run
    try:
        yield TEST_DB
    finally:
        _drop_database_ignoring_absence(TEST_DB)


def test_ensure_schema_succeeds_on_a_fresh_database(disposable_database):
    ensure_schema(ARCADE_HTTP, disposable_database, AUTH, DIMS)


def test_ensure_schema_is_idempotent(disposable_database):
    ensure_schema(ARCADE_HTTP, disposable_database, AUTH, DIMS)
    ensure_schema(ARCADE_HTTP, disposable_database, AUTH, DIMS)  # must not raise


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

    rows = _query(
        disposable_database,
        "SELECT embedding.type() AS t, embedding.size() AS n FROM Passage WHERE passage_key = :k",
        {"k": "p1"},
    )
    assert len(rows) == 1
    assert rows[0]["t"] == "ARRAY_OF_FLOATS", (
        "embedding kept an untyped list instead of ARRAY_OF_FLOATS -- schema ran after the write?"
    )
    assert rows[0]["n"] == DIMS

    ann = _query(
        disposable_database,
        "SELECT expand(`vector.neighbors`('Passage[embedding]', :q, 3))",
        {"q": v1},
    )
    assert ann, "the vector index returned nothing -- schema ran after the write?"
    assert any(row.get("passage_key") == "p1" for row in ann), (
        "nearest neighbour of v1 did not include the passage written with v1"
    )
