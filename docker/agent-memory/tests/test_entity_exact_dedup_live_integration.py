"""Live proof that the exact-name entity fallback matches what it must and nothing more.

Executed against a REAL Neo4j on purpose. The suite's in-memory fake clients never run
Cypher, which is exactly why this class of defect keeps shipping green: the query text can
be wrong in a way no fake can observe. FIND_SCOPED_ENTITY_BY_NAME_TYPE is asserted here by
running it.

The defect it closes: add_entity's only dedup was embedding similarity, so it decided
nothing whenever the embedder was off, the stored vectors came from a DIFFERENT model
(cross-model vectors are not comparable, so similarity is noise), or a real duplicate fell
below threshold. Observed on a live deployment after an embedder swap: "Pmsync" and
"PmSync" became two ORGANIZATION nodes for one company.

Both directions are asserted, because the risk runs both ways. Matching too little leaves
the duplicate; matching too much silently merges two different things — the operator's own
"David" exists as a PERSON and as an OBJECT, and those are two entities, not one.
"""

from __future__ import annotations

import os
from uuid import uuid4

import pytest

from neo4j_agent_memory.graph import queries

pytestmark = pytest.mark.integration


def _env_or_skip() -> tuple[str, str, str, str]:
    uri = os.environ.get("NEO4J_TEST_URI") or os.environ.get("NEO4J_URI")
    password = os.environ.get("NEO4J_TEST_PASSWORD") or os.environ.get("NEO4J_PASSWORD")
    if not uri or not password:
        pytest.skip("set NEO4J_TEST_URI + NEO4J_TEST_PASSWORD to run the live dedup test")
    user = os.environ.get("NEO4J_TEST_USER") or os.environ.get("NEO4J_USER") or "neo4j"
    database = os.environ.get("NEO4J_TEST_DATABASE") or os.environ.get("NEO4J_DATABASE") or "neo4j"
    return uri, user, password, database


@pytest.fixture
async def graph():
    from pydantic import SecretStr

    from neo4j_agent_memory.config.settings import Neo4jConfig
    from neo4j_agent_memory.graph.client import Neo4jClient

    uri, user, password, database = _env_or_skip()
    config = Neo4jConfig(uri=uri, username=user, password=SecretStr(password), database=database)
    async with Neo4jClient(config) as client:
        yield client


async def _find(client, identifier: str, name: str, etype: str, scope: str = "global"):
    rows = await client.execute_read(
        queries.FIND_SCOPED_ENTITY_BY_NAME_TYPE,
        {
            "user_identifier": identifier,
            "name": name,
            "type": etype,
            "deduplication_scope": scope,
        },
    )
    return rows[0]["id"] if rows else None


async def test_exact_name_fallback_matches_across_case_but_respects_type_and_owner(graph):
    run = uuid4().hex[:12]
    owner = f"dedup-test-{run}"
    stranger = f"dedup-stranger-{run}"
    org_id = f"org-{run}"
    person_id = f"person-{run}"

    # Fixture: the caller owns ORGANIZATION "<run>-Pmsync" and a same-named PERSON. A THIRD
    # entity, owned only by a stranger, tests the ownership fence.
    #
    # Names are run-scoped because :Entity carries a uniqueness constraint on
    # (name, type, deduplication_scope) — GLOBAL, not per user. That constraint is itself
    # part of the story: entities are shared nodes and ownership is the HAS_ENTITY edge, so
    # "the same organization owned twice" cannot exist, and a naive fixture that creates one
    # per user fails at write time. It is also why the original defect was possible at all —
    # the constraint is case-SENSITIVE, so "Pmsync" and "PmSync" are two legal nodes.
    org = f"{run}-Pmsync"
    other = f"{run}-Stranger-Co"
    await graph.execute_write(
        """
        CREATE (u:User {identifier: $owner})
        CREATE (s:User {identifier: $stranger})
        CREATE (o:Entity {id: $org_id, name: $org, type: 'ORGANIZATION',
                          deduplication_scope: 'global', created_at: datetime()})
        CREATE (p:Entity {id: $person_id, name: $org, type: 'PERSON',
                          deduplication_scope: 'global', created_at: datetime()})
        CREATE (x:Entity {id: $foreign_id, name: $other, type: 'ORGANIZATION',
                          deduplication_scope: 'global', created_at: datetime()})
        CREATE (u)-[:HAS_ENTITY]->(o)
        CREATE (u)-[:HAS_ENTITY]->(p)
        CREATE (s)-[:HAS_ENTITY]->(x)
        """,
        {
            "owner": owner,
            "stranger": stranger,
            "org_id": org_id,
            "person_id": person_id,
            "foreign_id": f"foreign-{run}",
            "org": org,
            "other": other,
        },
    )

    try:
        # The bug, exactly: a different capitalisation of the SAME organization must resolve
        # to the existing node instead of minting a second one.
        assert await _find(graph, owner, org.replace("Pmsync", "PmSync"), "ORGANIZATION") == org_id
        assert await _find(graph, owner, org.upper(), "ORGANIZATION") == org_id
        # Whitespace drift is the same mistake wearing a different hat.
        assert await _find(graph, owner, f"  {org}  ", "ORGANIZATION") == org_id

        # TYPE stays a real distinction: the same name as a PERSON is a different entity,
        # and merging them would fix a capitalisation bug by inventing a worse one.
        assert await _find(graph, owner, org.replace("Pmsync", "PmSync"), "PERSON") == person_id

        # Ownership fence: an entity the caller does not own is invisible to the caller's
        # dedup lookup, even by exact name. A lookup that crossed owners would be an
        # enumeration oracle over other people's entities.
        assert await _find(graph, owner, other, "ORGANIZATION") is None
        assert await _find(graph, stranger, other, "ORGANIZATION") == f"foreign-{run}"
        assert await _find(graph, stranger, org, "ORGANIZATION") is None

        # A genuinely new name still returns nothing, so the caller goes on to CREATE.
        assert await _find(graph, owner, f"{run}-Altra-Azienda", "ORGANIZATION") is None

        # Scope is part of the MERGE identity and is matched exactly, not normalised away.
        assert await _find(graph, owner, org, "ORGANIZATION", scope="session-x") is None
    finally:
        await graph.execute_write(
            """
            MATCH (u:User) WHERE u.identifier IN [$owner, $stranger]
            OPTIONAL MATCH (u)-[:HAS_ENTITY]->(e:Entity)
            DETACH DELETE u, e
            """,
            {"owner": owner, "stranger": stranger},
        )
