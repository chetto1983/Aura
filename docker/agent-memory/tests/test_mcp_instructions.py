"""The server instructions are a contract with two sides, and only one was ever checked.

``_instructions.py`` states its own invariant in its module docstring: *"every tool named
here is registered in ``_tools.py``"*. That invariant was written because it had already
been violated — an earlier revision taught five tools that do not exist
(``memory_start_trace``, ``memory_record_step``, ``memory_complete_trace``,
``memory_export_graph``, ``graph_query``), and, as the docstring puts it, a model told it
has a Cypher escape hatch stops looking for the real repair path. Nothing enforced the
invariant afterwards, so it could be violated again in either direction:

- a tool NAMED but not registered — the original failure, burns turns on calls that fail;
- a tool REGISTERED but not named — the silent one. ``memory_add_entity`` was registered
  and never mentioned, so the model never created the entity a fact's subject names.
  Measured on the live deployment 2026-07-30: 22 of 26 facts had no ``ABOUT_SUBJECT``
  edge, including two client codes the agent had just stored, and
  ``memory_get_entity("ZOPPI SRL")`` found nothing. The write path was correct
  throughout — ``_link_fact_to_subject_entity`` deliberately does not mint entities — so
  no amount of reading the writer would have found it. The defect was in the text.

These tests read both files as TEXT rather than importing the package, so they run with a
bare interpreter and no extras installed.
"""

from __future__ import annotations

import re
from pathlib import Path

_SRC = Path(__file__).resolve().parents[1] / "src" / "neo4j_agent_memory" / "mcp"
_INSTRUCTIONS = _SRC / "_instructions.py"
_TOOLS = _SRC / "_tools.py"

# Tools whose absence from the instructions is a deliberate choice, each with the reason.
# An entry here is a claim that the model does better WITHOUT being told, and it has to be
# argued — not a place to park a tool someone forgot to document.
_INTENTIONALLY_UNDOCUMENTED = {
    # SHORT-TERM surface. The module docstring scopes these instructions to long-term
    # memory on purpose: "It does not hold the conversation — the host already has that."
    # Aura's host persists every turn to Postgres itself, so teaching the model to
    # duplicate them here would buy a second copy and a second bill.
    "memory_store_message",
    "memory_get_conversation",
    "memory_list_sessions",
}


def _instruction_text() -> str:
    """The instruction strings only — not this module's own docstring or comments."""
    source = _INSTRUCTIONS.read_text(encoding="utf-8")
    return "\n".join(re.findall(r'"""(.*?)"""', source, flags=re.S)[1:])


def _tools_source() -> tuple[str, str]:
    """Split _tools.py at the backend gate: (always-registered, NAMS-Platinum-only)."""
    source = _TOOLS.read_text(encoding="utf-8")
    gate = source.index("def _register_platinum_tools")
    return source[:gate], source[gate:]


def _registered_tools() -> set[str]:
    """Tools this deployment actually exposes.

    NOT every ``async def memory_*`` in the file. ``_register_platinum_tools`` runs only
    when ``settings.backend == "nams"`` (server.py: ``register_platinum = settings is not
    None and settings.backend == "nams"``); Aura runs bolt against its own Neo4j, so those
    four are never registered here. Reading the file as a flat list of definitions was the
    first version of this helper and it was wrong in the most embarrassing possible way:
    the coverage test below went green while the instructions named three tools this
    deployment does not have — reintroducing, inside the test written to prevent it, the
    exact defect the module docstring of _instructions.py was written about.
    """
    always, _ = _tools_source()
    return set(re.findall(r"async def (memory_\w+)\s*\(", always))


def _platinum_tools() -> set[str]:
    _, platinum = _tools_source()
    return set(re.findall(r"async def (memory_\w+)\s*\(", platinum))


def test_every_tool_named_in_the_instructions_is_registered() -> None:
    """The original failure: instructions promising tools that do not exist."""
    named = set(re.findall(r"\bmemory_\w+", _instruction_text()))
    missing = named - _registered_tools()
    assert not missing, (
        f"the instructions promise tools that are not registered: {sorted(missing)} — "
        "a model told it has a capability stops looking for the real repair path"
    )


def test_every_registered_tool_is_named_in_the_instructions() -> None:
    """The silent failure: a capability the model is never told it has.

    This is the direction that produced the dangling-fact bug. It fails loudly the moment
    someone registers a tool without teaching it, which is the only moment the omission is
    cheap to notice.
    """
    undocumented = _registered_tools() - set(re.findall(r"\bmemory_\w+", _instruction_text()))
    undocumented -= _INTENTIONALLY_UNDOCUMENTED
    assert not undocumented, (
        f"registered but never taught: {sorted(undocumented)} — either name each in "
        "_instructions.py or justify it in _INTENTIONALLY_UNDOCUMENTED"
    )


def test_fact_subjects_are_taught_to_carry_an_entity() -> None:
    """A fact whose subject names something must be told to create that something.

    Pinned separately from the coverage test above because naming ``memory_add_entity``
    somewhere in the text is not the same as teaching WHEN to reach for it. The
    instructions already do exactly this for preferences (``applies_to`` … "so it is
    reachable by structure and not only by wording"); the asymmetry between the two
    writers is the whole defect.
    """
    text = _instruction_text()
    fact_paragraph = text[text.index("memory_add_fact") : text.index("memory_add_preference")]
    assert "memory_add_entity" in fact_paragraph, (
        "the fact instruction must point at memory_add_entity — without the entity, "
        "the fact is a string sitting beside the node it talks about, and "
        'memory_get_entity("ZOPPI SRL") returns nothing after storing facts about it'
    )


def test_platinum_tools_are_never_taught_on_this_deployment() -> None:
    """The four NAMS-Platinum tools must not appear in the instructions.

    ``_register_platinum_tools`` runs only when ``settings.backend == "nams"``. Aura runs
    bolt against its own Neo4j, so ``memory_set_entity_feedback``,
    ``memory_get_entity_history``, ``memory_get_entity_provenance`` and
    ``memory_get_reflections`` are never registered — 17 definitions, 13 exposed.

    This assertion is separate from the coverage pair on purpose. Coverage answers "is
    every exposed tool taught"; this answers "is anything taught that is not exposed",
    which is the direction that actually shipped a regression: three of these four were
    added to the instructions in this very change before the audit caught them.
    """
    taught = set(re.findall(r"\bmemory_\w+", _instruction_text()))
    leaked = taught & _platinum_tools()
    assert not leaked, (
        f"instructions name NAMS-Platinum tools this bolt deployment does not register: "
        f"{sorted(leaked)} — a model told it has a capability stops looking for the real "
        "repair path"
    )
