"""MCP server instructions for Neo4j Agent Memory.

Server instructions are sent during MCP initialization and injected into
the LLM's system context by the host. They teach the model how to use the
memory tools effectively.

These describe the LONG-TERM memory surface only, and every tool named here is
registered in ``_tools.py``. The previous text taught five tools that do not exist
(``memory_start_trace``, ``memory_record_step``, ``memory_complete_trace``,
``memory_export_graph``, ``graph_query``). That is worse than saying nothing: a model
told it has a Cypher escape hatch stops looking for the real repair path and burns
turns on calls that can only fail.
"""

CORE_INSTRUCTIONS = """\
You have access to a persistent graph memory backed by Neo4j.

This is LONG-TERM memory. It does not hold the conversation — the host already has
that — it holds what should outlive it: facts, preferences, and the entities they
concern. Write to it deliberately, not continuously.

WHEN you learn something that should outlive this conversation:
  A durable fact -> memory_add_fact with subject, predicate, object. When the SUBJECT
  names a person, place, organization, event or object, call memory_add_entity for it
  FIRST, with its POLE+O type. A fact attaches to the entity its subject names only if
  that entity already exists; nothing is minted on your behalf, because fact subjects
  are not always names — some are identity UUIDs, and one entity per UUID would fill
  the graph with nodes no one meant to create. So the choice is yours to make, and it
  is the difference between a fact that is reachable by structure and a string sitting
  beside the node it talks about: without the entity, memory_get_entity("ZOPPI SRL")
  finds nothing even though you just stored two facts about it.
  Something about how the user wants to be treated -> memory_add_preference with a
  category, and applies_to when the preference concerns a specific person, place or
  organization, so it is reachable by structure and not only by wording.
  Two entities that relate to each other -> memory_create_relationship with the two
  NAMES and an UPPER_SNAKE_CASE type. This is the only tool that builds an edge between
  entities, and edges are what make the graph answer questions a search cannot: "which
  clients did this person handle" is a traversal, not a phrase to match. A graph of
  entities with no relationships is a list.

  Do NOT record the passing shape of this conversation. If it would not matter in a
  month, it does not belong here.

WHEN you need to recall:
  memory_search for a free-text lookup across facts, preferences and entities.
  memory_get_entity when you have a NAME and want what is known about it: its facts,
  its neighbours in the graph, and — importantly — any duplicates recorded under the
  same name.

WHEN a memory is wrong, use the verb that matches the intention. They are not
interchangeable, and picking the wrong one loses information:

  memory_update   — the content is WRONG, the memory has a right to exist.
                    Corrects in place, keeps the id, relationships and history,
                    refreshes the embedding.

  memory_forget   — it should NEVER HAVE BEEN recorded: test residue, something
                    captured by mistake, or something the user asked you to erase.
                    It is removed.

  Note what is NOT a reason to forget: a memory that is merely out of date. Superseding
  is a different act from deleting, and deleting the old one throws away the fact that
  it was ever true.

NEVER correct by re-adding. add_fact and add_preference deduplicate against what is
already stored and merge into the closest match WITHOUT changing its text, so
re-adding a corrected version silently re-merges onto the old wording every time. Get
the node id — from the add_* response, from memory_search, or from memory_get_entity —
and use memory_update.

Entity types follow POLE+O: Person, Object, Location, Event, Organization, with
optional subtypes (OBJECT:VEHICLE, LOCATION:ADDRESS). Relationships use
UPPER_SNAKE_CASE (WORKS_AT, LIVES_IN, FOUNDED).
"""

EXTENDED_INSTRUCTIONS = CORE_INSTRUCTIONS


def get_instructions(profile: str = "extended") -> str:
    """Get server instructions for the given tool profile.

    Args:
        profile: Tool profile name ('core' or 'extended').

    Returns:
        Instructions text to send during MCP initialization.
    """
    if profile == "extended":
        return EXTENDED_INSTRUCTIONS
    return CORE_INSTRUCTIONS
