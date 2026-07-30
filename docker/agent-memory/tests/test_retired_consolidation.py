from __future__ import annotations

import importlib

import pytest

from neo4j_agent_memory import MemoryClient


def test_unsafe_unowned_consolidation_surface_is_not_shipped():
    assert not hasattr(MemoryClient, "consolidation")
    with pytest.raises(ModuleNotFoundError):
        importlib.import_module("neo4j_agent_memory.memory.consolidation")
