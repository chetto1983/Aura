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

import ast
import asyncio
import inspect
import json
from pathlib import Path
from types import SimpleNamespace
from uuid import uuid4

from fastmcp import FastMCP

from neo4j_agent_memory import integration as integration_module
from neo4j_agent_memory.graph import queries
from neo4j_agent_memory.integration import entity_name_fields
from neo4j_agent_memory.mcp._tools import register_tools
from neo4j_agent_memory.memory import long_term as long_term_module
from neo4j_agent_memory.memory import short_term as short_term_module
from neo4j_agent_memory.memory.long_term import Entity, LongTermMemory


def _run(coro):
    return asyncio.run(coro)


def _entity(name: str, canonical: str | None) -> Entity:
    return Entity(id=uuid4(), name=name, type="PERSON", canonical_name=canonical)


def test_projection_reports_the_stored_name_not_the_canonical():
    fields = entity_name_fields(_entity("Davide", "David"))

    assert fields["name"] == "Davide", "the API must report what was stored"
    assert fields["canonical_name"] == "David", "the canonical is additive, not a replacement"


def test_projection_omits_canonical_when_it_adds_nothing():
    assert entity_name_fields(_entity("Davide", "Davide")) == {"name": "Davide"}
    assert entity_name_fields(_entity("Davide", None)) == {"name": "Davide"}


# ── The class guard ──────────────────────────────────────────────────────────
#
# Three read paths were fixed, then a fourth (the recalled-context block) turned up, then a
# fifth (memory_get_entity) — each time because the guard checked a LITERAL from the site
# just fixed, so the next site, written differently, walked straight past it. What follows
# bans the mistake itself: no caller-facing read may reach for ``display_name`` at all.
#
# The file set is derived from the package directory, not listed here, so a read path added
# in a NEW module is covered the day it is written. ``integrations/`` is deliberately out of
# scope: those are upstream framework adapters (LangChain, CrewAI, Strands...) that Aura does
# not serve, and pulling them in would trade a rule that must hold for a backlog nobody owns.

_PACKAGE = Path(inspect.getfile(integration_module)).parent

CALLER_FACING_SOURCES = [
    _PACKAGE / "integration.py",
    _PACKAGE / "integration_context.py",
    *sorted((_PACKAGE / "mcp").glob("*.py")),
    *sorted((_PACKAGE / "memory").glob("*.py")),
]


def _display_name_reads(path: Path) -> list[str]:
    """Every ``<something>.display_name`` access in a module, as ``file:line``.

    Parsed, not grepped: the property's own definition is a FunctionDef and the word appears
    in comments and docstrings that explain the rule. A textual scan would flag all of those
    and the guard would be silenced as noisy — which is how guards die.
    """
    tree = ast.parse(path.read_text(encoding="utf-8"))
    return [
        f"{path.name}:{node.lineno}"
        for node in ast.walk(tree)
        if isinstance(node, ast.Attribute) and node.attr == "display_name"
    ]


def test_the_class_guard_is_actually_looking_at_something():
    """A guard that scans an empty file set passes forever. Pin what it must cover."""
    names = {p.name for p in CALLER_FACING_SOURCES}

    assert {
        "integration.py",
        "integration_context.py",
        "_tools.py",
        "_resources.py",
        "long_term.py",
    } <= names
    assert all(p.is_file() for p in CALLER_FACING_SOURCES)


def test_no_caller_facing_read_reaches_for_display_name():
    """display_name renders a label; it is never the data a caller asked for.

    Supersedes the two literal guards that preceded it — it catches the same two sites plus
    any other, in any module of the read surface, however it is spelled.
    """
    offenders = {
        path.name: hits for path in CALLER_FACING_SOURCES if (hits := _display_name_reads(path))
    }

    assert not offenders, (
        f"display_name read on a caller-facing path: {offenders}. "
        "Project names through integration.entity_name_fields(entity), which reports the "
        "STORED name and adds the canonical as its own field. If you genuinely want a "
        "rendered label, write `entity.canonical_name or entity.name` at the site so it "
        "cannot be mistaken for the data."
    )


def test_the_projection_helper_is_the_one_in_use():
    """Guard the other direction: the ban above is also satisfied by projecting no name."""
    users = [
        path.name
        for path in CALLER_FACING_SOURCES
        if "entity_name_fields(" in path.read_text(encoding="utf-8")
    ]

    assert {"integration.py", "integration_context.py", "_tools.py", "_resources.py"} <= set(users)


def _ctx(client):
    return SimpleNamespace(
        request_context=SimpleNamespace(lifespan_context={"client": client}),
    )


class _OneEntityClient:
    """Answers any entity search with the operator's node: stored "Davide", canonical "David"."""

    def __init__(self, entity):
        self.long_term = SimpleNamespace(search_entities=self._search)
        self._entity = entity

    async def _search(self, **_kwargs):
        return [self._entity]


def test_memory_get_entity_reports_the_stored_name():
    """Driven through the registered tool, because that is where the source guards missed it.

    Live on 2026-07-25, on one node in one moment: memory_search said "Davide" and
    memory_get_entity said "David". The agent believed the second, concluded its write had
    failed, and deleted its own correct data.
    """
    mcp = FastMCP("entity-name-projection-guard")
    register_tools(mcp, profile="extended")
    tool = _run(mcp.get_tools())["memory_get_entity"]

    payload = json.loads(
        _run(
            tool.fn(
                _ctx(_OneEntityClient(_entity("Davide", "David"))),
                name="Davide",
                include_neighbors=False,
            )
        )
    )

    assert payload["entity"]["name"] == "Davide", "the tool must report what was stored"
    assert payload["entity"]["canonical_name"] == "David", "the canonical is additive"


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


class _ManyMatchesClient:
    """Answers an entity search with several same-name matches, closest first."""

    def __init__(self, entities):
        self.long_term = SimpleNamespace(search_entities=self._search)
        self._entities = entities
        self.limits: list[int] = []

    async def _search(self, **kwargs):
        self.limits.append(kwargs.get("limit"))
        return self._entities


def _get_entity_tool():
    mcp = FastMCP("entity-lookup-guard")
    register_tools(mcp, profile="extended")
    return _run(mcp.get_tools())["memory_get_entity"]


def test_get_entity_discloses_the_other_entities_with_that_name():
    """A name with two entities behind it must not answer as if it had one.

    memory_get_entity fetched exactly one match, so the operator's "David" — which existed
    twice, PERSON and OBJECT — was reported as a single entity. An agent asked to clean up
    the duplicates was told about one, corrected it, and stopped (live, 2026-07-25).
    """
    duplicate = _entity("David", None)
    duplicate.type = "OBJECT"
    client = _ManyMatchesClient([_entity("Davide", "Davide"), duplicate])

    payload = json.loads(
        _run(_get_entity_tool().fn(_ctx(client), name="David", include_neighbors=False))
    )

    assert payload["entity"]["name"] == "Davide"
    assert [m["name"] for m in payload["other_matches"]] == ["David"]
    assert client.limits[0] > 1, "a limit of 1 cannot see a duplicate"


def test_get_entity_stays_quiet_when_the_name_is_unambiguous():
    """other_matches is a signal, not a field that is always there saying nothing."""
    client = _ManyMatchesClient([_entity("Davide", "Davide")])

    payload = json.loads(
        _run(_get_entity_tool().fn(_ctx(client), name="Davide", include_neighbors=False))
    )

    assert "other_matches" not in payload
