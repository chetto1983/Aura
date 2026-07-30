"""Regression tests for entity deduplication safety.

Embedding similarity alone is not identity evidence for proper names. The deployed
embedder scored unrelated Italian people above the old flag threshold, creating a
pending SAME_AS edge for every new person. Pending edges are still graph structure and
later maintenance can promote them, so lexical corroboration is mandatory.
"""

import asyncio
from difflib import SequenceMatcher
from types import SimpleNamespace
from unittest.mock import patch
from uuid import uuid4

from neo4j_agent_memory.memory.long_term import (
    DeduplicationConfig,
    LongTermMemory,
)


class _SimilarityClient:
    def __init__(self, candidate_name: str, score: float) -> None:
        self.candidate_id = uuid4()
        self.candidate_name = candidate_name
        self.score = score

    async def execute_read(self, _query, _params):
        return [
            {
                "e": {
                    "id": str(self.candidate_id),
                    "name": self.candidate_name,
                    "canonical_name": self.candidate_name,
                    "type": "PERSON",
                    "deduplication_scope": "global",
                },
                "score": self.score,
            }
        ]


def _dedupe(name: str, candidate: str, score: float):
    client = _SimilarityClient(candidate, score)
    memory = LongTermMemory(
        client,
        deduplication=DeduplicationConfig(
            flag_threshold=0.85,
            auto_merge_threshold=0.95,
            fuzzy_threshold=0.90,
        ),
    )
    fake_rapidfuzz = SimpleNamespace(
        fuzz=SimpleNamespace(
            ratio=lambda left, right: 100 * SequenceMatcher(None, left, right).ratio()
        )
    )
    with patch.dict("sys.modules", {"rapidfuzz": fake_rapidfuzz}):
        result = asyncio.run(
            memory._check_for_duplicates(
                name=name,
                entity_type="PERSON",
                embedding=[0.1, 0.2],
                deduplication_scope=None,
            )
        )
    return result, client


def test_embedding_similarity_alone_never_links_unrelated_people():
    result, _ = _dedupe("Maria Verdi", "Davide", 0.8915)

    assert result.action == "none"
    assert result.is_duplicate is False
    assert result.matched_entity_id is None


def test_embedding_plus_lexical_similarity_can_still_flag_a_name_variant():
    result, client = _dedupe("Davide Marcheto", "Davide Marchetto", 0.90)

    assert result.action == "flagged"
    assert result.is_duplicate is True
    assert result.matched_entity_id == client.candidate_id
    assert result.match_type == "both"
