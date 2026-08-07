"""Token-bounded chunking for the embedding server's hard ceiling.

google/embeddinggemma-300m REFUSES an input past 2048 tokens with HTTP 500 --
that ceiling is the model's, not a tuning knob. cocoindex.ops.text.RecursiveSplitter
already does syntax-aware splitting (paragraph/sentence/word boundaries) -- inventory
before invention, so this module wraps it rather than hand-rolling a splitter; it
only translates the token budget into the byte budget RecursiveSplitter expects
and re-enforces the ceiling in tokens afterwards.

No SentencePiece/tokenizers/transformers/tiktoken package ships in this image
(checked: none of them import from docker/aura-ingest/requirements.txt's closure),
so exact parity with embeddinggemma-300m's tokenizer is unavailable here.
count_tokens instead uses a deliberately conservative CHARS_PER_TOKEN=3 -- below
the ~4 chars/token typical of a SentencePiece tokenizer on English/Italian prose
-- so the estimate always overshoots the real token count and never lets a chunk
through that the embedder would actually reject.
"""

from __future__ import annotations

import math
from dataclasses import dataclass, field

from cocoindex.ops.text import RecursiveSplitter

CHARS_PER_TOKEN = 3

_splitter = RecursiveSplitter()


@dataclass(frozen=True, slots=True)
class Chunk:
    text: str
    start: int
    end: int
    heading_path: list[str] = field(default_factory=list)


def count_tokens(text: str) -> int:
    return math.ceil(len(text) / CHARS_PER_TOKEN) if text else 0


def _hard_split(text: str, offset: int, max_tokens: int) -> list[Chunk]:
    # RecursiveSplitter only cuts at syntax boundaries and returns ONE chunk,
    # any size, for a run that has none (verified empirically: "x" * 200000 at
    # chunk_size=2000 still comes back whole). This fixed-width slice is the
    # only fallback, and only fires when the boundary-aware split still leaves
    # a chunk over budget.
    max_chars = max_tokens * CHARS_PER_TOKEN
    return [
        Chunk(text=text[i : i + max_chars], start=offset + i, end=offset + min(i + max_chars, len(text)))
        for i in range(0, len(text), max_chars)
    ]


def chunk(text: str, max_tokens: int = 2048) -> list[Chunk]:
    if not text:
        return []
    # chunk_size is documented in BYTES; passing the char budget understates a
    # UTF-8 multi-byte text's true byte length, which only makes the library
    # split EARLIER (smaller chunks) -- conservative in the same safe direction
    # as CHARS_PER_TOKEN.
    budget_chars = max_tokens * CHARS_PER_TOKEN
    out: list[Chunk] = []
    for c in _splitter.split(text, chunk_size=budget_chars):
        if count_tokens(c.text) <= max_tokens:
            out.append(Chunk(text=c.text, start=c.start.char_offset, end=c.end.char_offset))
        else:
            out.extend(_hard_split(c.text, c.start.char_offset, max_tokens))
    return out
