"""Regression cover for the two read-side defects found by driving the live MCP on 2026-07-25.

1. Every entity read projected ``Entity.display_name`` (``canonical_name or name``) under the
   key ``name``. An agent that stored "Davide" was told by add, get and search alike that the
   entity was called "David" — its resolver-assigned canonical — concluded its write had
   failed, deleted the node and retried. Four rounds, two and a half minutes, and the very
   first write had actually succeeded.
2. Canonical-name resolution drew its candidates from EVERY entity of that type in the
   database. A freshly created "Marco Bianchi" came back canonicalised onto another
   operator's "Davide": one user's data corrupting another's, and leaking the stranger's
   name back through the caller's own results.
"""

from __future__ import annotations

import asyncio
import inspect
from pathlib import Path
from uuid import uuid4

from neo4j_agent_memory import integration as integration_module
from neo4j_agent_memory.graph import queries
from neo4j_agent_memory.integration import _entity_name_fields
from neo4j_agent_memory.memory.long_term import Entity, LongTermMemory


def _run(coro):
    return asyncio.run(coro)


def _entity(name: str, canonical: str | None) -> Entity:
    return Entity(id=uuid4(), name=name, type="PERSON", canonical_name=canonical)


def test_projection_reports_the_stored_name_not_the_canonical():
    fields = _entity_name_fields(_entity("Davide", "David"))

    assert fields["name"] == "Davide", "the API must report what was stored"
    assert fields["canonical_name"] == "David", "the canonical is additive, not a replacement"


def test_projection_omits_canonical_when_it_adds_nothing():
    assert _entity_name_fields(_entity("Davide", "Davide")) == {"name": "Davide"}
    assert _entity_name_fields(_entity("Davide", None)) == {"name": "Davide"}


def test_no_read_path_projects_display_name_as_name():
    """The rule, pinned at the source: display_name renders a label, it is not the data.

    A unit test on the helper cannot stop a fourth read path from being added next to the
    three that were fixed, which is exactly how this shipped.
    """
    source = Path(inspect.getfile(integration_module)).read_text(encoding="utf-8")

    assert '"name": entity.display_name' not in source


class _RecordingClient:
    """Records reads so the test can assert WHICH query ran and with which parameters."""

    def __init__(self):
        self.reads: list[tuple[str, dict]] = []

    async def execute_read(self, query, params=None):
        self.reads.append((query, params or {}))
        return []


def test_canonical_candidates_are_scoped_to_the_caller():
    client = _RecordingClient()
    memory = LongTermMemory(client)

    _run(memory._get_existing_entity_names("PERSON", "identity-1"))

    query, params = client.reads[0]
    assert query == queries.SEARCH_ENTITIES_BY_TYPE_SCOPED
    assert params["user_identifier"] == "identity-1"


def test_canonical_candidates_stay_global_when_there_is_no_caller_scope():
    client = _RecordingClient()
    memory = LongTermMemory(client)

    _run(memory._get_existing_entity_names("PERSON"))

    query, _ = client.reads[0]
    assert query == queries.SEARCH_ENTITIES_BY_TYPE
