"""Regression coverage for query-embedding reuse on the live OpenAI-compatible path."""

from __future__ import annotations

import asyncio
from types import SimpleNamespace

from neo4j_agent_memory.embeddings.base import adapt_to_legacy_embedder
from neo4j_agent_memory.embeddings.openai import OpenAIEmbedder


class _RecordingEmbeddings:
    def __init__(self):
        self.calls = 0

    async def create(self, **_kwargs):
        self.calls += 1
        await asyncio.sleep(0)
        return SimpleNamespace(
            data=[SimpleNamespace(index=0, embedding=[3.0, 4.0, 0.0])]
        )


async def test_concurrent_and_warm_identical_embeddings_share_one_request():
    backend = _RecordingEmbeddings()
    embedder = OpenAIEmbedder(model="test", dimensions=2)
    embedder._client = SimpleNamespace(embeddings=backend)

    vectors = await asyncio.gather(*(embedder.embed("Davide") for _ in range(4)))
    warm = await embedder.embed("Davide")

    assert backend.calls == 1
    assert vectors == [warm] * 4
    assert vectors[0] is not vectors[1], "callers must not share a mutable vector"
    assert warm == [0.6, 0.8]


async def test_embedding_cache_is_bounded():
    backend = _RecordingEmbeddings()
    embedder = OpenAIEmbedder(model="test", dimensions=2, cache_size=2)
    embedder._client = SimpleNamespace(embeddings=backend)

    await embedder.embed("one")
    await embedder.embed("two")
    await embedder.embed("three")
    await embedder.embed("one")

    assert backend.calls == 4, "the oldest entry must be evicted"


class _ProtocolProvider:
    dimensions = 2
    model = "openai/test"

    def __init__(self):
        self.calls = 0

    async def embed(self, texts):
        self.calls += 1
        await asyncio.sleep(0)
        return [[0.6, 0.8] for _ in texts]

    async def embed_one(self, text):
        return (await self.embed([text]))[0]


async def test_live_provider_adapter_coalesces_identical_single_text_calls():
    provider = _ProtocolProvider()
    embedder = adapt_to_legacy_embedder(provider)

    vectors = await asyncio.gather(*(embedder.embed("Davide") for _ in range(4)))
    warm = await embedder.embed("Davide")

    assert provider.calls == 1
    assert vectors == [warm] * 4
