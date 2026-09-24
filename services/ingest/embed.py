"""The documents family's embedding: one route and one space per process (spec §6).

The supervisor resolves the route from aura.settings on every tick and restarts this
process when it moves (internal/ingestsupervisor/route.go). Nothing here derives a route or
a space: the environment names both, and every vector this module returns was produced in
SPACE, which app.py stamps beside it.
"""

import json
import math
import os
import urllib.error
import urllib.request

import cocoindex as coco

from ingest import chunk

BASE_URL = os.environ.get("AURA_EMBED_BASE_URL", "http://aura-llama-embed:8081")
DIMENSIONS = int(os.environ.get("AURA_EMBED_DIMENSIONS", "768"))
# Set only on a hosted route, and that is what makes a route hosted: Go's
# config.EmbedRouteKind reads the same field.
MODEL = os.environ.get("AURA_EMBED_MODEL", "").strip()
SPACE = os.environ.get("AURA_EMBED_SPACE", "").strip()
# Popped, not read: Tika, LibreOffice, aura-filecard and aura-media-index are children of this
# process, and none of them has any business holding a provider's key.
_API_KEY = os.environ.pop("AURA_EMBED_API_KEY", "").strip()
INPUT_LIMIT = int(os.environ.get("AURA_EMBED_INPUT_LIMIT") or "0")

# One request carries several chunks, bounded by BOTH a count and a token budget -- the two
# bounds internal/embeddings/fit.go:22-25 already applies, for the reason measured there: a
# 2048-token input took 5.7 s on the appliance sidecar, so 32 of them behind one deadline is
# a timeout, not a speed-up. A single input over the budget still goes alone.
EMBED_MAX_BATCH = 32
EMBED_REQUEST_TOKEN_BUDGET = 4096
# What the server prepends and appends to every input. chunk.count_tokens(add_special=True)
# measures it properly; this is the same two tokens, added to an estimate that never asks.
EMBED_SPECIAL_TOKENS = 2
# The document prefix travels inside every input, so a hosted byte budget pays for it too.
_PREFIX_BYTES = len(chunk.EMBED_DOC_PREFIX.encode("utf-8"))

# A vector without the space that produced it is the defect the stamp exists to end, so a
# process started without one embeds nothing rather than write rows nobody can place.
if not SPACE:
    raise RuntimeError("AURA_EMBED_SPACE is required: every stored vector is stamped with its space")
if MODEL and not _API_KEY:
    raise RuntimeError(f"AURA_EMBED_MODEL={MODEL} is a hosted route, and AURA_EMBED_API_KEY is empty")
if MODEL and INPUT_LIMIT <= EMBED_SPECIAL_TOKENS + _PREFIX_BYTES:
    raise RuntimeError(f"AURA_EMBED_INPUT_LIMIT={INPUT_LIMIT} leaves no room for a hosted input")


class EmbedRequestError(RuntimeError):
    """An embedding request that failed, with the HTTP status when there was one."""

    def __init__(self, message: str, status: int | None = None):
        super().__init__(message)
        self.status = status

    @property
    def rejects_input(self) -> bool:
        # Only these say something about a text (Go's embeddings.RejectsInput): 401, 403 and
        # 429 are about the route or the account, and a 5xx is about the server.
        return self.status in (400, 413, 422)


def _estimated_tokens(text: str) -> int:
    """A free upper bound on what the server will count for this chunk.

    chunk.CHARS_PER_TOKEN_FALLBACK is 3 where the tokenizer measured ~5.32, and chunk.py
    picked that ratio precisely because it always OVERSHOOTS -- so a text this bound clears
    is never actually longer. Using it here keeps the happy path free of round trips: asking
    /tokenize per chunk would trade N embedding requests for N tokenizing ones, which is the
    cost this batching exists to remove. The price is under-packed requests, and against a
    25.9x saving that is not a price worth optimising.
    """
    chars = len(chunk.EMBED_DOC_PREFIX + text)
    return math.ceil(chars / chunk.CHARS_PER_TOKEN_FALLBACK) + EMBED_SPECIAL_TOKENS


def _fit_for_embedding(text: str) -> tuple[str, int]:
    """The text as it will be sent, and what it is expected to cost.

    Cutting happens BEFORE the request, not in reaction to its failure. In a batch one
    oversized chunk would fail every other chunk travelling with it, so the reactive retry
    this replaced is not merely slower here -- it is wrong. It is also what
    _head_within_ceiling already says it believes: "The decision is the MEASUREMENT, never
    the provider's error string."
    """
    cost = _estimated_tokens(text)
    if cost <= chunk.MODEL_MAX_TOKENS:
        return text, cost
    head = _head_within_ceiling(text)
    if head is None:  # the real tokenizer disagrees with the overshooting estimate
        return text, cost
    # Losing the vector loses the WHOLE FILE: CocoIndex catches the component failure,
    # prints "component build failed" and carries on, so the document simply never reaches
    # the index -- and in live mode audit_pass() never runs to notice. Against that,
    # embedding the head of an over-long chunk is a small, honest degradation: the Passage
    # keeps its full text, so document_open and the card are unaffected, and only the vector
    # is computed from less than the whole.
    print(
        f"[embed] a chunk exceeds the {chunk.MODEL_MAX_TOKENS}-token ceiling; "
        f"embedding the head that fits", flush=True,
    )
    return head, _estimated_tokens(head)


def _request_end(costs: list[int], start: int) -> int:
    """Where the request that begins at `start` stops. Mirrors requestEnd in fit.go."""
    end, tokens = start + 1, costs[start]
    while (
        end < len(costs)
        and end - start < EMBED_MAX_BATCH
        and tokens + costs[end] <= EMBED_REQUEST_TOKEN_BUDGET
    ):
        tokens += costs[end]
        end += 1
    return end


def _fit(text: str) -> tuple[str, int]:
    """What to send for one chunk, before its prefix, and what it is expected to cost."""
    return _fit_hosted(text) if MODEL else _fit_for_embedding(text)


def _fit_hosted(text: str) -> tuple[str, int]:
    """Cut to a hosted model's published limit in bytes, internal/embeddings fitInput's rule.

    There is no tokenizer for a hosted model here, and no token was ever shorter than one
    UTF-8 byte, so prefix + text within INPUT_LIMIT - 2 bytes always fits. It drops text a
    truncating provider would have kept; one that refuses instead (qwen3-embedding-8b answers
    400) would otherwise refuse the whole file.
    """
    budget = INPUT_LIMIT - EMBED_SPECIAL_TOKENS - _PREFIX_BYTES
    encoded = text.encode("utf-8")
    if len(encoded) > budget:
        text = encoded[:budget].decode("utf-8", "ignore")
    return text, _estimated_tokens(text)


def _embed_texts(texts: list[str]) -> list[list[float]]:
    """Embed a batch of chunks, one HTTP request per group rather than per chunk.

    CocoIndex groups concurrent calls itself (batching=True), so the call sites still pass
    ONE text and await ONE vector. Measured 2026-09-21 from the appliance: against the local
    sidecar this is 1.0x -- it is compute-bound at one slot -- but against a cloud embedder
    it is 25.9x (9.95 s -> 0.38 s for 32 chunks), because there each chunk was a network
    round trip. The token count, and therefore the bill, is identical either way.

    A request the provider refuses as input goes back to CocoIndex to split
    (RetryWithSmallerBatch). Batching groups chunks across files, so a plain raise failed
    every file sharing the request with one bad input (audit F1). Only 400, 413 and 422
    split: any other failure fails the same way at any size, and CocoIndex's own contract
    says not to split on it (cocoindex/_internal/batching.py).
    """
    fitted = [_fit(text) for text in texts]
    costs = [cost for _, cost in fitted]
    out: list[list[float]] = []
    start = 0
    try:
        while start < len(fitted):
            end = _request_end(costs, start)
            out.extend(_embed_batch([text for text, _ in fitted[start:end]]))
            start = end
    except EmbedRequestError as err:
        if err.rejects_input:
            raise coco.RetryWithSmallerBatch() from err
        raise
    return out


# deps is the space. A route change restarts this process with another one, the logic
# fingerprint moves, and CocoIndex re-runs every file that embedded under the old space.
# deps is snapshotted at import, which is exactly the lifetime of a space here.
embed_text = coco.fn.as_async(
    memo=True, batching=True, max_batch_size=EMBED_MAX_BATCH, deps=SPACE,
)(_embed_texts)


def _endpoint(base: str) -> str:
    """Where /v1/embeddings is, by internal/embeddings endpoint()'s rule."""
    base = base.strip().rstrip("/")
    if base.endswith("/embeddings"):
        return base
    if base.endswith("/v1"):
        return base + "/embeddings"
    return base + "/v1/embeddings"


def _to_width(vector: list[float]) -> list[float]:
    """The vector at DIMENSIONS, by internal/embeddings TruncateMRL's rule.

    Wider keeps the leading components and renormalises, which is valid only for a
    Matryoshka-trained model (the cockpit's preview warns about it). Narrower cannot fill the
    index at all, and nothing is stored rather than a vector ArcadeDB would refuse.
    """
    if len(vector) == DIMENSIONS:
        return vector
    if len(vector) < DIMENSIONS:
        raise EmbedRequestError(f"embedding has dimension {len(vector)}, want {DIMENSIONS}")
    head = vector[:DIMENSIONS]
    norm = math.sqrt(sum(value * value for value in head))
    if norm == 0 or not math.isfinite(norm):
        raise EmbedRequestError(f"embedding truncated to {DIMENSIONS} invalid components")
    return [value / norm for value in head]


def _embed_batch(texts: list[str]) -> list[list[float]]:
    """One request for several chunks, restored to the caller's order.

    The response is placed by its own `index` rather than by arrival: the field exists
    because the order is not promised, and trusting arrival order would attach each vector
    to the wrong passage -- silently, since every vector is well-formed.
    """
    # Whether batching is actually happening is otherwise invisible: the vectors look the
    # same either way, and the only symptom of it silently degrading to one chunk per
    # request is a slow cloud ingest nobody can explain.
    print(f"[embed] {len(texts)} chunk(s) in one request", flush=True)
    body = {"input": [chunk.EMBED_DOC_PREFIX + text for text in texts], "model": MODEL or "embeddinggemma"}
    headers = {"Content-Type": "application/json"}
    if MODEL:
        # Only a hosted route truncates server-side; llama.cpp ignores the field, and
        # _to_width narrows whatever comes back either way.
        body["dimensions"] = DIMENSIONS
        headers["Authorization"] = f"Bearer {_API_KEY}"
    req = urllib.request.Request(_endpoint(BASE_URL), data=json.dumps(body).encode(), headers=headers)
    try:
        with urllib.request.urlopen(req, timeout=120) as resp:
            data = json.loads(resp.read())["data"]
    except urllib.error.HTTPError as exc:
        # The longest input is the one a size-related failure is about.
        raise EmbedRequestError(_embed_failure_detail(exc, max(texts, key=len)), exc.code) from exc
    if len(data) != len(texts):
        raise EmbedRequestError(f"embedding endpoint returned {len(data)} vectors for {len(texts)} inputs")
    out: list[list[float] | None] = [None] * len(texts)
    for item in data:
        index = item.get("index")
        if not isinstance(index, int) or not 0 <= index < len(out):
            raise EmbedRequestError(f"embedding response carries an unusable index {index!r}")
        if out[index] is not None:
            raise EmbedRequestError(f"embedding response repeats index {index}")
        out[index] = _to_width(item["embedding"])
    missing = [i for i, vector in enumerate(out) if vector is None]
    if missing:
        raise EmbedRequestError(f"embedding response is missing indexes {missing}")
    return out


def _head_within_ceiling(text: str) -> str | None:
    """The head of text the model can actually take, or None when text already fits.

    The decision is the MEASUREMENT, never the provider's error string: a message like
    "input is too large to process" is llama.cpp's wording and would be a different one
    behind any other server, while the token count is ours and means the same thing
    everywhere.

    chunk.chunk does the cutting because it is the same verified splitter that sized every
    other chunk in the pipeline -- it re-measures each window and shrinks by the overshoot
    it observes. A second sizing rule here could disagree with it, and the disagreement
    would only ever surface as this exact failure.

    Returning None when nothing smaller comes back is deliberate: a failure that is not an
    overflow (a server restarting, a model unloaded) is not made better by sending less, and
    pretending otherwise would hide it behind a retry.
    """
    if chunk.count_tokens(chunk.EMBED_DOC_PREFIX + text, add_special=True) <= chunk.MODEL_MAX_TOKENS:
        return None
    # No anchors here on purpose: this re-cuts ONE oversized chunk that already carries its
    # heading, so a second stamping pass would have nothing to add and no document to
    # position it against.
    pieces = chunk.chunk(text, max_tokens=chunk.document_budget())
    if not pieces or pieces[0].text == text:
        return None
    return pieces[0].text


def _embed_failure_detail(exc: urllib.error.HTTPError, text: str) -> str:
    """What the bare HTTPError does not say, and what it cost to not say it.

    An HTTPError raised out of urlopen renders as "HTTP Error 500: Internal Server Error"
    and nothing else, while the BODY is where llama.cpp names the actual fault -- "input is
    too large to process, increase the physical batch size" tells you the fix in one line.
    Losing it cost two full reproductions on 2026-08-09.

    The token count is measured, not assumed, and it is the second half of the diagnosis:
    the overwhelmingly common cause is a chunk that overflowed the model ceiling, and the
    count beside the ceiling settles that in the error itself rather than in a re-run.
    Counted WITH the prefix and the specials because that is what the server actually sees
    (the same arithmetic document_budget() does). count_tokens degrades to a character
    estimate when the server is unreachable, so measuring here cannot fail the failure path.
    """
    body = exc.read().decode("utf-8", "replace").strip() if exc.fp is not None else ""
    tokens = chunk.count_tokens(chunk.EMBED_DOC_PREFIX + text, add_special=True)
    return (
        f"embedding request failed: HTTP {exc.code} for a {tokens}-token input "
        f"(model ceiling {chunk.MODEL_MAX_TOKENS})"
        + (f": {body}" if body else " with an empty body")
    )
