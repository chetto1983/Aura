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
from neo4j_agent_memory.memory import long_term as long_term_module
from neo4j_agent_memory.memory import short_term as short_term_module
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


def test_recalled_context_names_the_entity_as_stored():
    """The same rule where it bites hardest: the block injected into the model every turn.

    With display_name here, the live profile read "- David (PERSON): ... Preferisce essere
    chiamato Davide." — the agent was handed a contradiction about its own operator on every
    single turn, and no amount of correcting the record could clear it.
    """
    source = Path(inspect.getfile(long_term_module)).read_text(encoding="utf-8")

    assert "{entity.display_name} ({type_str})" not in source
    assert "{entity.name} ({type_str})" in source


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


def test_every_message_entity_link_also_links_the_owner():
    """Ownership must be written wherever a message-entity link is, or delete refuses it.

    Only add_entity ever wrote (:User)-[:HAS_ENTITY]->(:Entity), while DELETE_ENTITY_SCOPED
    requires exactly that edge — so entities born from extraction were visible and updatable
    but undeletable, and memory_forget told the operator their own data was "not found or not
    owned by this user". A behavioural test on one call site would not stop a fourth from being
    added without the ownership write, which is how this happened.
    """
    source = Path(inspect.getfile(short_term_module)).read_text(encoding="utf-8")

    linked = source.count("queries.LINK_MESSAGE_TO_ENTITY")
    owned = source.count("queries.LINK_MESSAGE_OWNER_TO_ENTITY")

    assert linked > 0, "no message-entity link sites found — the guard is checking nothing"
    assert owned == linked, (
        f"{linked} message-entity link site(s) but {owned} ownership write(s): "
        "every extracted entity must be attributed to the message's owner"
    )
