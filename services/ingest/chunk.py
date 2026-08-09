"""Token-bounded chunking for the embedding server's hard ceiling.

google/embeddinggemma-300m REFUSES an input past 2048 tokens with HTTP 500 --
that ceiling is the model's, not a tuning knob. cocoindex.ops.text exposes two
splitters and this module wraps both rather than hand-rolling one (inventory
before invention): RecursiveSplitter for syntax-aware boundaries, and
SeparatorSplitter -- configured as a fixed-width window -- for the one case
RecursiveSplitter can't cut at all: a run with no boundary anywhere.

Chunk carries cocoindex's own TextPosition (byte/char offset, line, column)
for start and end unmodified, alongside the plain character-offset ints this
module's own contract requires (.start/.end must slice the source text
directly: `text[c.start:c.end] == c.text`, so they can't themselves be
TextPosition objects) -- nothing from cocoindex's own offsets is discarded.

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
from cocoindex.resources.chunk import Chunk as CoreChunk
from cocoindex.resources.chunk import TextPosition

logger = logging.getLogger(__name__)
_EMBED_BASE_URL_ENV = "AURA_EMBED_BASE_URL"
_DEFAULT_EMBED_BASE_URL = "http://aura-llama-embed:8081"
_TOKENIZE_TIMEOUT_S = 2.0

# The model's own ceiling, not a tuning knob: past this the server answers HTTP 500.
MODEL_MAX_TOKENS = 2048
# EmbeddingGemma is asymmetric and the prefix is part of the model input, so the
# server is sent PREFIX + text and the ceiling applies to the SUM. It lives here,
# next to the budget that has to subtract it, because the two drifting apart is
# exactly the defect this module exists to prevent -- and it drifted once already.
# Must equal embeddings.UntitledDocumentPrefix (internal/embeddings/tasks.go), the
# Go side of the same contract.
EMBED_DOC_PREFIX = "title: none | text: "

# FALLBACK ONLY (see count_tokens). Measured 2026-08-06 via POST /tokenize on
# real Italian prose: embeddinggemma-300m's SentencePiece tokenizer averages
# ~5.32 chars/token (6400 chars -> 1202 tokens). Using 3 instead of ~5.3 always
# OVERSHOOTS the real count, so a chunk clearing this check is never actually
# longer than max_tokens once the real tokenizer counts it. Also sizes
# _window_split's fixed window below, for the same overshoot-is-safe reason.
CHARS_PER_TOKEN_FALLBACK = 3
# FALLBACK ONLY, same reason: with no server to ask, the BOS/EOS pair the embeddings
# endpoint always adds still has to be paid for. Measured on this build via POST
# /tokenize with add_special true and false on the same string: 10 tokens against 8,
# BOS id 2 prepended and EOS id 1 appended.
_SPECIAL_TOKENS = 2
# The measured ratio is an AVERAGE over the piece, so a window sized exactly at it
# can still overshoot where the text is locally denser. 0.9 buys that margin back.
_WINDOW_SAFETY = 0.9
# `.{N}` is expanded by the regex engine, so a large N stops compiling: measured on
# this build, 8000 compiles and 12000 raises "Compiled regex exceeds size limit of
# 10485760 bytes". Only the boundary-less path uses these windows, so capping there
# costs nothing worth having.
_MAX_WINDOW_CHARS = 8000
DEFAULT_OVERLAP_RATIO = 0.1  # verified directly against RecursiveSplitter: chunk_size=2000/chunk_overlap=200 keeps every offset exact

_splitter = RecursiveSplitter()
_server_reachable: bool | None = None  # None = unprobed; sticky True/False after the first attempt this process
FALLBACK_ACTIVE = False  # visible flag: True after the most recent count_tokens() call used the char estimate, not the real tokenizer

@dataclass(frozen=True, slots=True)
class Chunk:
    text: str
    start: int
    end: int
    start_pos: TextPosition
    end_pos: TextPosition
    heading_path: list[str] = field(default_factory=list)

def _tokenize_remote(text: str, add_special: bool = False) -> list[int] | None:
    global _server_reachable
    if _server_reachable is False:
        return None
    base = os.environ.get(_EMBED_BASE_URL_ENV, _DEFAULT_EMBED_BASE_URL)
    req = urllib.request.Request(
        f"{base.rstrip('/')}/tokenize",
        data=json.dumps({"content": text, "add_special": add_special}).encode("utf-8"),
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

def count_tokens(text: str, add_special: bool = False) -> int:
    """Tokens the embedding server sees. add_special counts what IT prepends and
    appends, which /tokenize omits by default -- see document_budget()."""
    global FALLBACK_ACTIVE
    if not text:
        FALLBACK_ACTIVE = False
        return 0
    tokens = _tokenize_remote(text, add_special)
    if tokens is not None:
        FALLBACK_ACTIVE = False
        return len(tokens)
    FALLBACK_ACTIVE = True
    estimate = math.ceil(len(text) / CHARS_PER_TOKEN_FALLBACK)
    return estimate + _SPECIAL_TOKENS if add_special else estimate

def document_budget() -> int:
    """Tokens a document chunk may occupy once EMBED_DOC_PREFIX is prepended.

    Measured with the server's own tokenizer rather than assumed, so changing the
    prefix moves the budget with it instead of silently overflowing the ceiling.

    add_special=True is the whole point of the second measurement. /tokenize omits
    BOS/EOS by default while the embeddings endpoint always adds them, so counting
    the prefix without them left a two-token hole -- and a hole of exactly two
    tokens is one a real document walks into. Measured 2026-08-09 on the 130-document
    Italian corpus: `epatite-di-origine-sconosciuta-nei-bambini.csv` chunked to 2047
    tokens by /tokenize, cleared the 2041 budget, and the server refused it with
    "input (2049 tokens) is too large to process" -- the document vanished from the
    index while the run still exited 0.
    """
    return MODEL_MAX_TOKENS - count_tokens(EMBED_DOC_PREFIX, add_special=True)


def _shift(base: TextPosition, rel: TextPosition) -> TextPosition:
    # `rel` is relative to a piece that (see _window_split) never crosses a
    # source line, so line is constant and column advances 1:1 with char_offset.
    return TextPosition(
        byte_offset=base.byte_offset + rel.byte_offset,
        char_offset=base.char_offset + rel.char_offset,
        line=base.line,
        column=base.column + rel.char_offset,
    )

def _window_split(piece: CoreChunk, max_tokens: int) -> list[Chunk]:
    # A run with no syntax boundary comes back from RecursiveSplitter whole,
    # at any size ("x" * 200000 at chunk_size=2000 still returns one chunk).
    # SeparatorSplitter covers that too: a fixed-width regex window IS the
    # separator. (?s) so '.' matches newlines (or a run containing one goes
    # uncut); keep_separator='left' (the default DISCARDS matched windows ->
    # zero chunks); trim=False (or edge whitespace is eaten and offsets stop
    # reconstructing the source). No newline exists inside `piece` (any would
    # already have been a RecursiveSplitter boundary), which is what makes
    # _shift's line/column arithmetic below valid.
    # The window is sized from THIS piece's measured chars-per-token, not from
    # CHARS_PER_TOKEN_FALLBACK. That constant is safe only where a character is
    # worth more than three tokens' worth of nothing -- i.e. Latin script. Han,
    # Kana and Hangul run near one character per token, so a 3x window would
    # produce chunks THREE TIMES over the ceiling, on the one path whose output
    # nothing downstream re-checks. Measuring the piece makes the sizing correct
    # for whatever script it actually contains.
    # Clamped, because both ends of the range are reachable. Degenerate input
    # ("x" repeated) compresses so well that the measured ratio explodes and the
    # resulting `.{N}` regex blows past the engine's 10 MB compile limit; dense
    # scripts sit near 1. Outside [1, 20] the measurement says more about the
    # tokenizer's handling of pathological text than about the window we want.
    tokens = max(count_tokens(piece.text), 1)
    chars_per_token = min(max(len(piece.text) / tokens, 1.0), 20.0)
    max_chars = min(max(1, int(max_tokens * chars_per_token * _WINDOW_SAFETY)), _MAX_WINDOW_CHARS)
    # Sized from an AVERAGE, then VERIFIED, because an average is not a bound. Where a
    # piece is locally denser than itself -- prose that turns into a formula block --
    # a window sized on the whole overshoots on that stretch, and _WINDOW_SAFETY is a
    # hoped-for margin, not a guarantee. Nothing downstream re-measures this path, so
    # an overshoot here is an HTTP 500 the reconciler prints and walks past, and the
    # document leaves no passage. Measured 2026-08-09 over 60 open_ragbench arXiv
    # papers: 2 of 1419 chunks came back at 2116 and 2350 tokens against a 2048
    # ceiling -- exactly the two the ingest run then dropped.
    #
    # The loop shrinks by the overshoot it just measured rather than by a fixed
    # factor, so the correction is the evidence. It terminates: while the worst chunk
    # exceeds max_tokens the multiplier is below _WINDOW_SAFETY < 1, so max_chars
    # strictly decreases to its floor of 1 character.
    while True:
        out = _windows(piece, max_chars)
        worst = max((count_tokens(chunk.text) for chunk in out), default=0)
        if worst <= max_tokens or max_chars <= 1:
            return out
        max_chars = max(1, int(max_chars * max_tokens / worst * _WINDOW_SAFETY))


def _windows(piece: CoreChunk, max_chars: int) -> list[Chunk]:
    splitter = SeparatorSplitter([rf"(?s).{{{max_chars}}}"], keep_separator="left", trim=False)
    out: list[Chunk] = []
    for rel in splitter.split(piece.text):
        start_pos, end_pos = _shift(piece.start, rel.start), _shift(piece.start, rel.end)
        out.append(Chunk(text=rel.text, start=start_pos.char_offset, end=end_pos.char_offset,
                          start_pos=start_pos, end_pos=end_pos))
    return out

def chunk(
    text: str, max_tokens: int = MODEL_MAX_TOKENS, overlap_ratio: float = DEFAULT_OVERLAP_RATIO
) -> list[Chunk]:
    """Split text so every chunk fits the embedding model's token ceiling.

    overlap_ratio drives RecursiveSplitter's native chunk_overlap: a fact
    straddling a chunk boundary is otherwise unreachable from either side.
    """
    if not text:
        return []
    # chunk_size/chunk_overlap are documented in BYTES; this module only has a
    # CHARACTER estimate. Passing chars where bytes are expected understates
    # true byte length for multi-byte text (bytes >= chars always), which
    # only makes RecursiveSplitter look for a boundary EARLIER -- a heuristic
    # GUIDE, not the ceiling, which count_tokens() enforces below (the real
    # tokenizer when reachable).
    guide_chars_per_token = 4
    budget_bytes = max_tokens * guide_chars_per_token
    overlap_bytes = round(budget_bytes * overlap_ratio)
    out: list[Chunk] = []
    for piece in _splitter.split(text, chunk_size=budget_bytes, chunk_overlap=overlap_bytes):
        if count_tokens(piece.text) <= max_tokens:
            out.append(Chunk(text=piece.text, start=piece.start.char_offset, end=piece.end.char_offset,
                              start_pos=piece.start, end_pos=piece.end))
        else:
            out.extend(_window_split(piece, max_tokens))
    return out
