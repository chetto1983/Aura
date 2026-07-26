"""Structural regression cover for the cross-user data leak found live on 2026-07-25.

Every ``*_SCOPED`` query in ``queries.py`` is supposed to enforce ownership. Several of
them wrote the ownership predicate as a ``WHERE`` directly following an ``OPTIONAL
MATCH``, e.g.::

    MATCH (e:Entity {type: $type})
    OPTIONAL MATCH (:User {identifier: $user_identifier})-[ue:HAS_ENTITY]->(e)
    WHERE ue IS NOT NULL
    RETURN e

In Cypher, a ``WHERE`` that directly follows an ``OPTIONAL MATCH`` belongs to *that*
``OPTIONAL MATCH`` -- it constrains which optional pattern gets bound, it does not
filter the outer row stream. Rows whose optional pattern found nothing still flow
through with NULLs and get returned. So the ownership check did nothing, and every one
of these "scoped" reads returned other users' data.

Proved live against ``SEARCH_ENTITIES_BY_EMBEDDING_SCOPED``: querying as user
``claude-dogfood`` (who owns exactly one entity, "Marco Bianchi") returned "Davide" --
a different user's entity -- ranked ABOVE the caller's own entity by score.

The fix is a ``WITH`` carrying the needed variables between the ``OPTIONAL MATCH`` and
the ``WHERE``, turning the predicate back into a real row filter.

**2026-07-26 -- why this file was rewritten.** The first version of this guard
discovered its subjects with ``[n for n in dir(queries) if n.endswith("_SCOPED")]``.
Four more copies of the identical defect were living outside that net and survived the
sweep: ``_get_entity_neighbors`` (``mcp/_tools.py``) and three queries inside
``MemoryClient.get_graph`` (``__init__.py``). None is in ``queries.py``; none is named
``*_SCOPED``. The ``_tools.py`` one was the worst of the family -- it backs
``memory_get_entity(include_neighbors=True)``, which is default-on, and it is the only
reader in the codebase walking an *untyped, variable-length* path. Proved live against
the deployment graph: an identity owning nothing, asking for the neighbors of an entity
it did not own, got back another user's entity.

So discovery is now an AST walk over every Cypher string literal in the package, and
the rule is sharper. A ``WHERE`` glued to an ``OPTIONAL MATCH`` is not wrong in itself:
it is the correct way to constrain the optional pattern, and three queries here rely on
exactly that (``CREATE_MESSAGE`` picking the tail message with ``WHERE NOT
(last)-[:NEXT_MESSAGE]->()``, ``GET_SESSION_CONTEXT`` date-filtering ``p``,
``gds._communities_fallback`` filtering ``other``). A blanket ban would have to
"fix" those three by breaking them.

The rule that separates the six real defects from those three legitimate uses, in one
sentence: **a WHERE glued to an OPTIONAL MATCH may only constrain variables that same
OPTIONAL MATCH introduces.** The moment it mentions a variable bound earlier -- the
outer ``MATCH``, or a preceding ``OPTIONAL MATCH`` -- that variable's row survives the
predicate with the optional bindings NULLed, and the filter is dead.
"""

from __future__ import annotations

import ast
import pathlib
import re

import neo4j_agent_memory
from neo4j_agent_memory.graph import queries

_IDENT = r"[A-Za-z_][A-Za-z0-9_]*"
# Reject `$param` and `node.property` tails: neither is a bindable variable reference.
_NOT_A_REFERENCE = r"(?<![$\w.])"

_WHERE_RE = re.compile(r"^WHERE\b", re.IGNORECASE)
_OPTIONAL_MATCH_RE = re.compile(r"^OPTIONAL MATCH\b", re.IGNORECASE)
_COMMENT_RE = re.compile(r"^//")

# A variable is introduced by a pattern only where it directly follows `(` or `[`:
# `(u:User ...)` and `[ue:HAS_ENTITY]` bind, `(:User ...)` and `[:HAS_ENTITY]` do not.
_PATTERN_VAR_RE = re.compile(r"[(\[]\s*(" + _IDENT + r")")
_ALIAS_RE = re.compile(r"\bAS\s+(" + _IDENT + r")", re.IGNORECASE)

_PROPERTY_ROOT_RE = re.compile(_NOT_A_REFERENCE + r"(" + _IDENT + r")\s*\.")
_NULL_CHECK_RE = re.compile(
    _NOT_A_REFERENCE + r"(" + _IDENT + r")\s+IS\s+(?:NOT\s+)?NULL", re.IGNORECASE
)
_BARE_NODE_RE = re.compile(r"\(\s*(" + _IDENT + r")\s*\)")


def _significant_lines(query: str) -> list[str]:
    """Non-blank, non-comment lines of a Cypher query template, in order."""
    return [
        stripped
        for raw_line in query.splitlines()
        if (stripped := raw_line.strip()) and not _COMMENT_RE.match(stripped)
    ]


def _variables_referenced(where_clause: str) -> set[str]:
    """Variables a predicate actually reads.

    Deliberately narrow -- property roots (`e.id`), null checks (`ue IS NOT NULL`) and
    bare node references (`(last)`). Every ownership predicate in this codebase is one
    of those three, and staying narrow keeps Cypher keywords and function names out
    without maintaining a keyword list that would rot.
    """
    return (
        {m.group(1) for m in _PROPERTY_ROOT_RE.finditer(where_clause)}
        | {m.group(1) for m in _NULL_CHECK_RE.finditer(where_clause)}
        | {m.group(1) for m in _BARE_NODE_RE.finditer(where_clause)}
    )


def _glued_where_lines(query: str) -> list[str]:
    """Every WHERE whose nearest preceding clause is an OPTIONAL MATCH, sound or not."""
    lines = _significant_lines(query)
    return [
        line
        for i, line in enumerate(lines)
        if _WHERE_RE.match(line) and i and _OPTIONAL_MATCH_RE.match(lines[i - 1])
    ]


def _dead_ownership_predicates(query: str) -> list[str]:
    """WHERE lines glued to an OPTIONAL MATCH that constrain an earlier binding.

    Such a predicate does not filter the outer row stream: rows survive with the
    optional variables NULLed and flow on to RETURN. This is the exact shape that let
    six ownership checks silently no-op.
    """
    lines = _significant_lines(query)

    # bound_before[k] = variables already bound when line k is reached. The lag matters:
    # measured against the accumulator *including* the OPTIONAL MATCH line, every
    # variable looks pre-bound, "introduces nothing", and all three legitimate queries
    # get reported.
    bound_before: list[set[str]] = []
    accumulated: set[str] = set()
    for line in lines:
        bound_before.append(set(accumulated))
        accumulated |= set(_PATTERN_VAR_RE.findall(line))
        accumulated |= set(_ALIAS_RE.findall(line))

    offenders = []
    for i, line in enumerate(lines):
        if not (_WHERE_RE.match(line) and i and _OPTIONAL_MATCH_RE.match(lines[i - 1])):
            continue
        introduced_here = set(_PATTERN_VAR_RE.findall(lines[i - 1])) - bound_before[i - 1]
        if _variables_referenced(line) - introduced_here:
            offenders.append(line)
    return offenders


def _string_literals(node: ast.AST) -> list[str]:
    if isinstance(node, ast.Constant) and isinstance(node.value, str):
        return [node.value]
    if isinstance(node, ast.JoinedStr):
        # f-string: keep the literal segments, drop the {placeholders}. Enough to see
        # the clause skeleton, which is all this analysis needs.
        return [
            "".join(
                part.value
                for part in node.values
                if isinstance(part, ast.Constant) and isinstance(part.value, str)
            )
        ]
    return []


def _cypher_literals() -> list[tuple[str, int, str]]:
    """Every (file, line, text) Cypher string in the package that has an OPTIONAL MATCH.

    An AST walk rather than a name convention: the four defects that survived the first
    sweep were all inline query strings, discoverable by no naming rule at all.
    """
    root = pathlib.Path(neo4j_agent_memory.__file__).parent
    found = []
    for path in sorted(root.rglob("*.py")):
        tree = ast.parse(path.read_text(encoding="utf-8"))
        for node in ast.walk(tree):
            for text in _string_literals(node):
                if "OPTIONAL MATCH" in text.upper():
                    found.append((path.name, node.lineno, text))
    return found


def _scoped_query_names() -> list[str]:
    return [
        name
        for name in dir(queries)
        if name.endswith("_SCOPED") and isinstance(getattr(queries, name), str)
    ]


def test_cypher_literal_discovery_still_finds_queries():
    # Guards the discovery mechanism itself: an AST walk that silently stops matching
    # would make every assertion below vacuous -- which is precisely how the previous
    # `dir(queries)` version came to check 18 queries and miss the 4 that were broken.
    literals = _cypher_literals()
    files = {name for name, _line, _text in literals}
    assert len(literals) >= 30
    assert {"queries.py", "_tools.py", "__init__.py"} <= files


def test_the_analysis_flags_the_defect_it_exists_to_catch():
    """A guard that cannot detect the original bug is decoration.

    Left column is `_get_entity_neighbors` as it shipped; right column is the fix.
    """
    shipped_broken = """
    MATCH (e:Entity {id: $entity_id})-[r*1..2]-(neighbor:Entity)
    OPTIONAL MATCH (:User {identifier: $user_identifier})-[ue:HAS_ENTITY]->(neighbor)
    OPTIONAL MATCH (:User {identifier: $user_identifier})-[:HAS_TRACE]->(:ReasoningTrace)-[:HAS_STEP]->(rs:ReasoningStep)-[:TOUCHED]->(neighbor)
    WHERE neighbor.id <> $entity_id
      AND ($user_identifier IS NULL OR ue IS NOT NULL OR rs IS NOT NULL)
    RETURN neighbor.id AS id
    """
    assert _dead_ownership_predicates(shipped_broken)

    repaired = shipped_broken.replace(
        "    WHERE neighbor.id", "    WITH neighbor, r, ue, rs\n    WHERE neighbor.id"
    )
    assert not _dead_ownership_predicates(repaired)


def test_the_analysis_accepts_a_predicate_on_its_own_optional_match():
    """`CREATE_MESSAGE` picking the tail message -- a glued WHERE that is correct.

    Pinned because the obvious over-strict rule (ban every glued WHERE) would report
    this, and "fixing" it would break message chaining.
    """
    assert not _dead_ownership_predicates(queries.CREATE_MESSAGE)


def test_no_cypher_query_in_the_package_has_a_dead_ownership_predicate():
    """Regression test for the cross-user leak, package-wide.

    Any query -- in `queries.py` or inline, present or added in the future -- whose
    glued WHERE reaches for a variable bound before it is filtering nothing.
    """
    broken = {
        f"{name}:{line}": offenders
        for name, line, text in _cypher_literals()
        if (offenders := _dead_ownership_predicates(text))
    }

    assert not broken, (
        "these queries have a WHERE glued to an OPTIONAL MATCH that constrains a "
        "variable bound EARLIER -- it filters the optional pattern, not the outer "
        "rows, so the predicate does not filter anything. Insert a WITH (carrying "
        "the variables the predicate needs) between the OPTIONAL MATCH and the "
        f"WHERE:\n{broken}"
    )


def test_no_scoped_query_attaches_where_directly_to_optional_match():
    """`queries.py`'s ownership queries hold to the stricter house convention.

    None of them needs to constrain an optional pattern, so none of them should carry
    a glued WHERE at all -- a flat rule that leaves no room for the analysis above to
    be argued with on the queries that matter most.
    """
    broken = {
        name: glued
        for name in _scoped_query_names()
        if (glued := _glued_where_lines(getattr(queries, name)))
    }

    assert not broken, (
        "these _SCOPED queries have a WHERE clause directly attached to an "
        "OPTIONAL MATCH. Insert a WITH (carrying the variables the predicate "
        f"needs) between the OPTIONAL MATCH and the WHERE:\n{broken}"
    )
