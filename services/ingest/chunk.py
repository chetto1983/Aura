"""Token-bounded chunking for the embedding server's hard ceiling.
google/embeddinggemma-300m REFUSES an input past 2048 tokens with HTTP 500 --
that ceiling is the model's, not a tuning knob. cocoindex.ops.text.RecursiveSplitter
already does syntax-aware splitting (paragraph/sentence/word boundaries) -- inventory
before invention, so this module wraps it rather than hand-rolling a splitter.
count_tokens counts with the embedding server's OWN tokenizer (POST /tokenize,
base URL from AURA_EMBED_BASE_URL) whenever reachable -- that is the tokenizer
the 2048 ceiling actually belongs to. The char-based estimate below is a
documented FALLBACK only, for when the server isn't reachable (e.g. the unit
tests, which run off the compose network and must stay green without one).
"""

from __future__ import annotations
import json
import logging
import math
import os
import urllib.error
import urllib.request
from dataclasses import dataclass, field
from cocoindex.ops.text import RecursiveSplitter, SeparatorSplitter

logger = logging.getLogger(__name__)
_EMBED_BASE_URL_ENV = "AURA_EMBED_BASE_URL"
_DEFAULT_EMBED_BASE_URL = "http://aura-llama-embed:8081"
_TOKENIZE_TIMEOUT_S = 2.0
# FALLBACK ONLY (see count_tokens). Measured 2026-08-06 via POST /tokenize on
# real Italian prose: embeddinggemma-300m's SentencePiece tokenizer averages
# ~5.32 chars/token (6400 chars -> 1202 tokens). Using 3 instead of ~5.3 always
# OVERSHOOTS the real count, so a chunk clearing this check is never actually
# longer than max_tokens once the real tokenizer counts it -- the cost is
# fragmentation, not correctness.
CHARS_PER_TOKEN_FALLBACK = 3
_splitter = RecursiveSplitter()
_server_reachable: bool | None = None  # None = unprobed; sticky True/False after first attempt this process
FALLBACK_ACTIVE = False  # visible flag: True after the most recent count_tokens() call used the char estimate, not the real tokenizer

@dataclass(frozen=True, slots=True)
class Chunk:
    text: str
    start: int
    end: int
    heading_path: list[str] = field(default_factory=list)

def _tokenize_remote(text: str) -> list[int] | None:
    global _server_reachable
    if _server_reachable is False:
        return None
    base = os.environ.get(_EMBED_BASE_URL_ENV, _DEFAULT_EMBED_BASE_URL)
    req = urllib.request.Request(
        f"{base.rstrip('/')}/tokenize",
        data=json.dumps({"content": text}).encode("utf-8"),
        headers={"Content-Type": "application/json"},
        method="POST",
    )
    try:
        with urllib.request.urlopen(req, timeout=_TOKENIZE_TIMEOUT_S) as resp:
            tokens = json.load(resp)["tokens"]
    except (urllib.error.URLError, OSError, ValueError, KeyError) as exc:
        if _server_reachable is None:
            logger.warning(
                "embedding tokenizer at %s unreachable (%s); falling back to a "
                "conservative %d chars/token estimate for the rest of this process",
                base, exc, CHARS_PER_TOKEN_FALLBACK,
            )
        _server_reachable = False
        return None
    _server_reachable = True
    return tokens

def count_tokens(text: str) -> int:
    global FALLBACK_ACTIVE
    if not text:
        FALLBACK_ACTIVE = False
        return 0
    tokens = _tokenize_remote(text)
    if tokens is not None:
        FALLBACK_ACTIVE = False
        return len(tokens)
    FALLBACK_ACTIVE = True
    return math.ceil(len(text) / CHARS_PER_TOKEN_FALLBACK)

def _window_split(text: str, offset: int, max_tokens: int) -> list[Chunk]:
    # A run with no syntax boundary comes back from RecursiveSplitter whole, at
    # any size ("x" * 200000 at chunk_size=2000 still returns one chunk). The
    # library covers that case too, so nothing here is hand-rolled: a
    # SeparatorSplitter whose separator IS a fixed-width window cuts it.
    #
    # Three options are load-bearing and each fails silently if wrong:
    #   (?s)                  so `.` matches newlines, or a run containing one is uncut
    #   keep_separator='left' or the matched windows are DISCARDED -> zero chunks
    #   trim=False            or edge whitespace is eaten and the offsets stop
    #                         reconstructing the source
    max_chars = max_tokens * CHARS_PER_TOKEN_FALLBACK
    splitter = SeparatorSplitter(
        separators_regex=[rf"(?s).{{1,{max_chars}}}"], keep_separator="left", trim=False
    )
    return [
        Chunk(text=c.text, start=offset + c.start.char_offset, end=offset + c.end.char_offset)
        for c in splitter.split(text)
    ]

def chunk(text: str, max_tokens: int = 2048, overlap_tokens: int = 128) -> list[Chunk]:
    """Split text so every chunk fits the embedding model's 2048-token ceiling.

    overlap_tokens exists because a fact that straddles a boundary is otherwise
    unreachable from either side. RecursiveSplitter implements it natively.
    """
    if not text:
        return []
    # chunk_size is documented in BYTES and used only as a heuristic GUIDE for
    # where RecursiveSplitter looks for a boundary -- the ceiling itself is
    # enforced below via count_tokens() (real tokenizer when reachable) plus
    # _window_split, so this guide doesn't need real-tokenizer precision. 4 sits
    # below the measured ~5.32 chars/token (see CHARS_PER_TOKEN_FALLBACK), and
    # UTF-8 text has >=1 byte/char, so undershooting chars/token also
    # undershoots true bytes/token: erring low only yields more fragments,
    # never a ceiling violation.
    guide_bytes_per_token = 4
    budget_bytes = max_tokens * guide_bytes_per_token
    out: list[Chunk] = []
    for c in _splitter.split(
        text, chunk_size=budget_bytes, chunk_overlap=overlap_tokens * guide_bytes_per_token
    ):
        if count_tokens(c.text) <= max_tokens:
            out.append(Chunk(text=c.text, start=c.start.char_offset, end=c.end.char_offset))
        else:
            out.extend(_window_split(c.text, c.start.char_offset, max_tokens))
    return out
