from __future__ import annotations

import ast
from pathlib import Path

import neo4j_agent_memory

PACKAGE = Path(neo4j_agent_memory.__file__).parent
GRAPH_CLIENT = PACKAGE / "graph" / "client.py"
READ_ONLY_DIRECT_DRIVER = PACKAGE / "integrations" / "microsoft_agent" / "gds.py"
MUTATING_WORDS = {
    "create",
    "delete",
    "detach",
    "drop",
    "merge",
    "remove",
    "set",
}


def _attribute_chain(node: ast.AST) -> str:
    parts: list[str] = []
    while isinstance(node, ast.Attribute):
        parts.append(node.attr)
        node = node.value
    if isinstance(node, ast.Name):
        parts.append(node.id)
    return ".".join(reversed(parts))


def test_direct_driver_access_is_confined_to_client_and_read_only_gds():
    offenders: list[str] = []
    for path in PACKAGE.rglob("*.py"):
        if path in {GRAPH_CLIENT, READ_ONLY_DIRECT_DRIVER}:
            continue
        tree = ast.parse(path.read_text(encoding="utf-8"))
        for node in ast.walk(tree):
            if not isinstance(node, ast.Call):
                continue
            chain = _attribute_chain(node.func)
            if chain == "AsyncGraphDatabase.driver" or chain.endswith("._client._client.session"):
                offenders.append(f"{path.relative_to(PACKAGE)}:{node.lineno}")

    assert not offenders, f"direct driver access bypasses the corpus-epoch wrapper: {offenders}"


def test_gds_direct_driver_queries_remain_read_only():
    tree = ast.parse(READ_ONLY_DIRECT_DRIVER.read_text(encoding="utf-8"))
    offenders: list[str] = []
    for node in ast.walk(tree):
        if not isinstance(node, ast.Constant) or not isinstance(node.value, str):
            continue
        words = {word.strip("():,.;{}[]").lower() for word in node.value.split()}
        if words & MUTATING_WORDS:
            offenders.append(f"{READ_ONLY_DIRECT_DRIVER.name}:{node.lineno}")

    assert not offenders, f"GDS direct-driver allowance contains mutating Cypher: {offenders}"


def test_every_result_affecting_writer_family_routes_through_execute_write():
    families = {
        "message": PACKAGE / "memory" / "short_term.py",
        "entity_preference_fact_relationship_forget_backfill": (
            PACKAGE / "memory" / "long_term.py"
        ),
        "buffered": PACKAGE / "memory" / "buffered.py",
        "reasoning_touched_edges": PACKAGE / "memory" / "reasoning.py",
        "user": PACKAGE / "memory" / "users.py",
    }

    missing = [
        name
        for name, path in families.items()
        if ".execute_write(" not in path.read_text(encoding="utf-8")
    ]
    assert not missing, f"writer families bypass or lack the managed write seam: {missing}"


def test_context_split_and_name_projection_guard_cover_the_new_module():
    context_module = PACKAGE / "integration_context.py"
    assert context_module.is_file()

    projection_guard = (
        PACKAGE.parent.parent / "tests" / "test_entity_name_projection.py"
    ).read_text(encoding="utf-8")
    assert "integration_context.py" in projection_guard
