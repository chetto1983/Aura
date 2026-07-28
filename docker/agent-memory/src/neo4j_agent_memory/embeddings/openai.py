"""OpenAI embedding provider."""

import math
from typing import TYPE_CHECKING

from neo4j_agent_memory.core.exceptions import EmbeddingError
from neo4j_agent_memory.embeddings.base import BaseEmbedder

if TYPE_CHECKING:
    from openai import AsyncOpenAI


# Model dimensions mapping
MODEL_DIMENSIONS = {
    "text-embedding-3-small": 1536,
    "text-embedding-3-large": 3072,
    "text-embedding-ada-002": 1536,
}


class OpenAIEmbedder(BaseEmbedder):
    """OpenAI embedding provider."""

    def __init__(
        self,
        model: str = "text-embedding-3-small",
        *,
        api_key: str | None = None,
        dimensions: int | None = None,
        batch_size: int = 100,
    ):
        """
        Initialize OpenAI embedder.

        Args:
            model: OpenAI embedding model name
            api_key: OpenAI API key (uses OPENAI_API_KEY env var if not provided)
            dimensions: Optional dimension reduction (for text-embedding-3-* models)
            batch_size: Maximum texts per API call
        """
        self._model = model
        self._api_key = api_key
        self._requested_dimensions = dimensions
        self._batch_size = batch_size
        self._client: AsyncOpenAI | None = None

        # Determine dimensions
        if dimensions is not None:
            self._dimensions = dimensions
        else:
            self._dimensions = MODEL_DIMENSIONS.get(model, 1536)

    def _ensure_client(self) -> "AsyncOpenAI":
        """Ensure the OpenAI client is initialized."""
        if self._client is None:
            try:
                from openai import AsyncOpenAI
            except ImportError:
                raise EmbeddingError(
                    "OpenAI package not installed. Install with: pip install neo4j-agent-memory[openai]"
                )
            self._client = AsyncOpenAI(api_key=self._api_key)
        return self._client

    @property
    def dimensions(self) -> int:
        """Return the embedding dimensions."""
        return self._dimensions

    async def embed(self, text: str) -> list[float]:
        """Generate embedding for a single text."""
        client = self._ensure_client()

        try:
            kwargs: dict = {"input": text, "model": self._model}
            if self._requested_dimensions is not None:
                kwargs["dimensions"] = self._requested_dimensions

            response = await client.embeddings.create(**kwargs)
            return self._fit(response.data[0].embedding)
        except Exception as e:
            raise EmbeddingError(f"Failed to generate embedding: {e}") from e

    def _fit(self, vector: list[float]) -> list[float]:
        """Narrow a wider embedding to the configured width (Matryoshka truncation).

        Servers that truncate on their own honour the ``dimensions`` request
        parameter and return exactly what was asked for, in which case this is a
        no-op. Local OpenAI-compatible servers may ignore it entirely and always
        return the model's native width — llama.cpp does: asking for 256 returns the
        native size unchanged. Without this the caller silently stores vectors wider
        than the vector index was built for, and every write fails at the database.

        MRL-trained embedders are explicitly trained so a leading slice is itself a
        valid embedding; the slice is renormalised because it is no longer unit
        length and cosine over unnormalised vectors skews every score.

        A vector NARROWER than configured is not padded: that means the server is
        running a different model than the index expects, and hiding it would corrupt
        the store quietly.
        """
        width = self._dimensions
        if width is None or len(vector) == width:
            return vector
        if len(vector) < width:
            raise EmbeddingError(
                f"embedding server returned {len(vector)} dimensions, expected {width}"
            )
        head = vector[:width]
        norm = math.sqrt(sum(v * v for v in head))
        if norm == 0:
            raise EmbeddingError(f"embedding truncated to {width} all-zero components")
        return [v / norm for v in head]

    async def embed_batch(self, texts: list[str]) -> list[list[float]]:
        """Generate embeddings for multiple texts efficiently."""
        if not texts:
            return []

        client = self._ensure_client()
        all_embeddings: list[list[float]] = []

        try:
            # Process in batches
            for i in range(0, len(texts), self._batch_size):
                batch = texts[i : i + self._batch_size]
                kwargs: dict = {"input": batch, "model": self._model}
                if self._requested_dimensions is not None:
                    kwargs["dimensions"] = self._requested_dimensions

                response = await client.embeddings.create(**kwargs)
                # Sort by index to maintain order
                sorted_data = sorted(response.data, key=lambda x: x.index)
                all_embeddings.extend([self._fit(d.embedding) for d in sorted_data])

            return all_embeddings
        except Exception as e:
            raise EmbeddingError(f"Failed to generate embeddings: {e}") from e
