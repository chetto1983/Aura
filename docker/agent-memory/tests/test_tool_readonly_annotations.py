"""Guard: every read tool advertises readOnlyHint, and no write tool claims it.

WHY this matters beyond tidiness. A client cannot tell a lookup from a mutation by
inspecting a tool name — the MCP annotation is the only channel. Aura derives its risk
tier straight from it (`spec.Mutating = !Annotations.ReadOnlyHint`, bridge.go), and its
gateway saturates upward: anything mutating is Risky, and Risky stops the turn for
operator approval.

So while these annotations were missing, asking the graph for the user's own name —
`memory_search` — required the operator to approve it. Observed live 2026-07-27 on the
appliance: the agent answered "la richiesta è in attesa di approvazione" to "come mi
chiamo?", and then, asked why, called its own behaviour absurd. The real damage is not the
one prompt: an approval gate that fires on harmless reads trains an operator to approve
without reading, which is precisely the habit the gate exists to prevent.

The test asserts BOTH directions on purpose. Only checking the reads would let a future
`readOnlyHint` pasted onto a write verb sail through, and that failure is silent and far
worse — a mutation that skips the gate.
"""

from __future__ import annotations

import asyncio

from fastmcp import FastMCP

from neo4j_agent_memory.mcp._tools import READ_ONLY_TOOLS, register_tools

# The mutating surface, spelled out rather than derived as "everything else": a new tool
# that is neither listed here nor in READ_ONLY_TOOLS fails the completeness check below,
# which is how an unclassified verb gets noticed at the moment it is added.
WRITE_TOOLS = frozenset(
    {
        "memory_store_message",
        "memory_add_entity",
        "memory_add_preference",
        "memory_add_fact",
        "memory_forget",
        "memory_update",
        "memory_create_relationship",
        "memory_set_entity_feedback",
    }
)


def _registered_tools():
    mcp = FastMCP("readonly-annotation-guard")
    register_tools(mcp, profile="extended", register_platinum=True)
    return asyncio.run(mcp.get_tools())


def _is_read_only(tool) -> bool:
    ann = getattr(tool, "annotations", None)
    if ann is None:
        return False
    # FastMCP may hand back the pydantic ToolAnnotations model or the raw dict.
    if isinstance(ann, dict):
        return bool(ann.get("readOnlyHint"))
    return bool(getattr(ann, "readOnlyHint", False))


def test_read_tools_advertise_read_only():
    tools = _registered_tools()
    missing = sorted(
        name for name in READ_ONLY_TOOLS if name in tools and not _is_read_only(tools[name])
    )
    assert not missing, (
        f"read tools without readOnlyHint: {missing}. "
        "A client cannot distinguish these from writes, so Aura's gateway treats them as "
        "mutating and demands operator approval for a lookup."
    )


def test_write_tools_do_not_claim_read_only():
    tools = _registered_tools()
    lying = sorted(
        name for name in WRITE_TOOLS if name in tools and _is_read_only(tools[name])
    )
    assert not lying, (
        f"write tools claiming readOnlyHint: {lying}. "
        "These would bypass the approval gate entirely."
    )


def test_every_registered_tool_is_classified():
    """A new verb must be declared read or write — never left to the default."""
    tools = set(_registered_tools())
    unclassified = sorted(tools - READ_ONLY_TOOLS - WRITE_TOOLS)
    assert not unclassified, (
        f"tools classified as neither read nor write: {unclassified}. "
        "Add each to READ_ONLY_TOOLS (and annotate it) or to WRITE_TOOLS here."
    )
