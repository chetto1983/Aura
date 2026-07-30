"""Base embedder protocol and utilities.

.. deprecated:: 0.3.0
    The :class:`Embedder` Protocol defined in this module remains for
    backward compatibility, but new code should prefer
    :class:`neo4j_agent_memory.llm.protocol.EmbeddingProvider`.

    The two Protocols are structural-typing compatible — any class that
    implements ``Embedder`` (``dimensions`` + ``embed`` + ``embed_batch``)
    can be adapted to the new Protocol via :func:`adapt_to_embedding_provider`.
    The new Protocol uses ``embed`` for batch operations (instead of
    ``embed_batch``) and adds an ``embed_one`` convenience method.
"""

from __future__ import annotations

import asyncio
import hashlib
from abc import ABC, abstractmethod
from collections import OrderedDict
from collections.abc import Awaitable, Callable, Sequence
from typing import Protocol, runtime_checkable


@runtime_checkable
class Embedder(Protocol):
    """Protocol for embedding providers.

    .. deprecated:: 0.3.0
        Prefer :class:`neo4j_agent_memory.llm.protocol.EmbeddingProvider`.
        Existing code keeps working; the deprecation is documentation-only.
    """

    @property
    def dimensions(self) -> int:
        """Return the embedding dimensions."""
        ...

    async def embed(self, text: str) -> list[float]:
        """
        Generate embedding for a single text.

        Args:
            text: The text to embed

        Returns:
            Embedding vector as list of floats
        """
        ...

    async def embed_batch(self, texts: list[str]) -> list[list[float]]:
        """
        Generate embeddings for multiple texts efficiently.

        Args:
            texts: List of texts to embed

        Returns:
            List of embedding vectors
        """
        ...


class BaseEmbedder(ABC):
    """Abstract base class for embedder implementations.

    Concrete implementations live in sibling modules
    (:mod:`neo4j_agent_memory.embeddings.openai`, etc.). The new
    :mod:`neo4j_agent_memory.llm.adapters` wraps these for Protocol
    compliance — they retain their existing API.
    """

    @property
    @abstractmethod
    def dimensions(self) -> int:
        """Return the embedding dimensions."""

    @abstractmethod
    async def embed(self, text: str) -> list[float]:
        """Generate embedding for a single text."""

    async def embed_batch(self, texts: list[str]) -> list[list[float]]:
        """
        Generate embeddings for multiple texts.

        Default implementation calls embed() for each text.
        Subclasses should override for better performance.
        """
        return [await self.embed(text) for text in texts]


class _SingleEmbeddingCache:
    """Bounded LRU plus single-flight for deterministic single-text embeddings."""

    def __init__(self, max_size: int = 512) -> None:
        if max_size < 0:
            raise ValueError("cache size must be non-negative")
        self._max_size = max_size
        self._completed: OrderedDict[bytes, tuple[float, ...]] = OrderedDict()
        self._inflight: dict[bytes, asyncio.Task[list[float]]] = {}
        self._lock = asyncio.Lock()

    async def get_or_create(
        self,
        text: str,
        create: Callable[[], Awaitable[list[float]]],
    ) -> list[float]:
        if self._max_size == 0:
            return await create()

        key = hashlib.sha256(text.encode("utf-8")).digest()
        async with self._lock:
            cached = self._completed.get(key)
            if cached is not None:
                self._completed.move_to_end(key)
                return list(cached)
            task = self._inflight.get(key)
            if task is None:
                task = asyncio.create_task(create())
                self._inflight[key] = task

        try:
            vector = await task
        except BaseException:
            async with self._lock:
                if self._inflight.get(key) is task:
                    self._inflight.pop(key, None)
            raise

        async with self._lock:
            if self._inflight.get(key) is task:
                self._inflight.pop(key, None)
            self._completed[key] = tuple(vector)
            self._completed.move_to_end(key)
            while len(self._completed) > self._max_size:
                self._completed.popitem(last=False)
        return list(vector)


class _EmbedderToProviderAdapter:
    """Wraps an :class:`Embedder` to expose the
    :class:`~neo4j_agent_memory.llm.protocol.EmbeddingProvider` Protocol.

    The two Protocols are very similar — the only differences are:

    * ``Embedder.embed(text: str)`` returns a single vector; the new
      Protocol's ``embed`` takes a list and returns vectors. The new
      Protocol has a separate ``embed_one`` for the single-text case.
    * The new Protocol has a ``model`` attribute; the old Embedder
      does not. We synthesize a sensible default.

    This adapter is used internally by :meth:`MemoryClient._create_embedder`
    to accept either kind of object. End users should rarely need to
    use this directly.
    """

    def __init__(self, embedder: Embedder, *, model: str | None = None) -> None:
        self._embedder = embedder
        self.model = model or getattr(embedder, "model", None) or type(embedder).__name__

    @property
    def dimensions(self) -> int:
        return self._embedder.dimensions

    async def embed(self, texts: Sequence[str]) -> list[list[float]]:
        return await self._embedder.embed_batch(list(texts))

    async def embed_one(self, text: str) -> list[float]:
        return await self._embedder.embed(text)


def adapt_to_embedding_provider(embedder: object) -> object:
    """Return ``embedder`` as an :class:`EmbeddingProvider`.

    Accepts either an :class:`Embedder` (old Protocol) or an
    :class:`~neo4j_agent_memory.llm.protocol.EmbeddingProvider` (new
    Protocol). If ``embedder`` already satisfies the new Protocol it is
    returned unchanged; otherwise it is wrapped in
    :class:`_EmbedderToProviderAdapter`.

    This is the entry point used by :class:`MemoryClient` to accept user-
    supplied embedders of either shape.
    """
    # Lazy import to avoid circular import at module load
    from neo4j_agent_memory.llm.protocol import EmbeddingProvider

    if isinstance(embedder, EmbeddingProvider):
        return embedder
    if isinstance(embedder, Embedder):
        return _EmbedderToProviderAdapter(embedder)
    raise TypeError(
        f"{type(embedder).__name__} satisfies neither Embedder nor EmbeddingProvider. "
        f"It must expose `dimensions`, `embed`, and either `embed_batch` (old) or "
        f"`embed_one` (new)."
    )


class _ProviderToEmbedderAdapter:
    """Wraps an :class:`EmbeddingProvider` to expose the old :class:`Embedder` API.

    The old API used by memory/resolution/extraction modules across the
    library expects two methods:

    * ``await embedder.embed(text: str) -> list[float]`` — single text.
    * ``await embedder.embed_batch(texts: list[str]) -> list[list[float]]`` — batch.

    The new :class:`EmbeddingProvider` Protocol instead exposes
    ``embed(texts) -> list[list[float]]`` (batch) and
    ``embed_one(text) -> list[float]`` (single). This adapter bridges new
    Provider instances back to the old shape so downstream code does not
    need a migration sweep.

    Used internally by :meth:`MemoryClient._create_embedder` when the
    configured embedding is a new-protocol Provider.
    """

    def __init__(self, provider: object) -> None:
        self._provider = provider
        self._single_cache = _SingleEmbeddingCache()
        # Forward the canonical attributes downstream code reads.
        self.model = getattr(provider, "model", type(provider).__name__)

    @property
    def dimensions(self) -> int:
        return self._provider.dimensions  # type: ignore[attr-defined]

    async def embed(self, text: str) -> list[float]:
        return await self._single_cache.get_or_create(
            text,
            lambda: self._provider.embed_one(text),  # type: ignore[attr-defined]
        )

    async def embed_batch(self, texts: list[str]) -> list[list[float]]:
        return await self._provider.embed(texts)  # type: ignore[attr-defined]


def adapt_to_legacy_embedder(provider: object) -> object:
    """Return ``provider`` as a legacy :class:`Embedder`.

    Accepts either an :class:`Embedder` (returned unchanged) or an
    :class:`~neo4j_agent_memory.llm.protocol.EmbeddingProvider` (wrapped
    in :class:`_ProviderToEmbedderAdapter`). The wrapped object exposes
    the old ``embed(text)`` / ``embed_batch(texts)`` API consumed by the
    rest of the library.

    This is the inverse of :func:`adapt_to_embedding_provider`.
    """
    from neo4j_agent_memory.llm.protocol import EmbeddingProvider

    if isinstance(provider, Embedder):
        return provider
    if isinstance(provider, EmbeddingProvider):
        return _ProviderToEmbedderAdapter(provider)
    raise TypeError(f"{type(provider).__name__} satisfies neither Embedder nor EmbeddingProvider.")


__all__ = [
    "Embedder",
    "BaseEmbedder",
    "adapt_to_embedding_provider",
    "adapt_to_legacy_embedder",
]
