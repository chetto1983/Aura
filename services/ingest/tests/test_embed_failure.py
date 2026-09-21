"""An embedding failure must arrive carrying the two facts that diagnose it.

A bare HTTPError renders as "HTTP Error 500: Internal Server Error" and drops the body,
which is where llama.cpp names the actual fault. That silence cost two reproductions on
2026-08-09, and the overwhelmingly common cause -- a chunk over the model ceiling -- is
settled by a token count the error can carry itself.
"""

import asyncio
import io
import json
import urllib.error

import pytest

from ingest import app, chunk


async def _embed_texts(texts: list[str]) -> list[list[float]]:
    """Drive the batched function the way CocoIndex does: one awaited call per text,
    grouped by the decorator. Written out because the decorated object is a coroutine."""
    return list(await asyncio.gather(*(app._embed(text) for text in texts)))


def http_error(code: int, body: bytes | None) -> urllib.error.HTTPError:
    return urllib.error.HTTPError(
        url="http://embed/v1/embeddings", code=code, msg="Internal Server Error",
        hdrs=None, fp=io.BytesIO(body) if body is not None else None,
    )


def test_the_servers_own_explanation_survives():
    detail = app._embed_failure_detail(
        http_error(500, b"input is too large to process, increase the physical batch size"),
        "una perizia lunga",
    )

    assert "input is too large to process" in detail
    assert "500" in detail


def test_the_token_count_and_the_ceiling_are_both_reported():
    text = "una perizia lunga"
    detail = app._embed_failure_detail(http_error(500, b"boom"), text)

    # Counted the way the SERVER counts it: with the document prefix and the specials.
    expected = chunk.count_tokens(chunk.EMBED_DOC_PREFIX + text, add_special=True)
    assert f"{expected}-token" in detail
    assert str(chunk.MODEL_MAX_TOKENS) in detail


def test_an_empty_body_says_so_rather_than_reading_as_a_missing_message():
    detail = app._embed_failure_detail(http_error(500, b""), "x")

    assert "empty body" in detail


def test_a_body_that_cannot_be_read_does_not_replace_the_failure_with_its_own():
    # fp None is what an HTTPError built without a payload carries; reading it would raise
    # and lose the original failure entirely.
    detail = app._embed_failure_detail(http_error(503, None), "x")

    assert "503" in detail


def test_embed_raises_with_the_detail_instead_of_the_bare_http_error(monkeypatch):
    def boom(*_args, **_kwargs):
        raise http_error(500, b"input is too large to process")

    monkeypatch.setattr(app.urllib.request, "urlopen", boom)

    with pytest.raises(RuntimeError) as caught:
        app._embed_batch(["una perizia lunga"])

    assert "input is too large to process" in str(caught.value)
    # Chained, so the traceback still reaches the transport-level cause.
    assert isinstance(caught.value.__cause__, urllib.error.HTTPError)


def test_an_input_that_already_fits_is_never_trimmed():
    assert app._head_within_ceiling("una perizia corta") is None


def test_an_oversized_input_is_cut_to_something_the_model_takes():
    text = "Perizia tecnica dettagliata. " * 4000

    head = app._head_within_ceiling(text)

    assert head is not None and len(head) < len(text)
    assert chunk.count_tokens(
        chunk.EMBED_DOC_PREFIX + head, add_special=True
    ) <= chunk.MODEL_MAX_TOKENS


def test_an_overflow_is_cut_BEFORE_the_request_not_after_it_fails(monkeypatch):
    """The failure that loses a document is the one worth recovering from.

    CocoIndex catches a component failure, prints "component build failed" and carries on,
    so a chunk that cannot embed takes the WHOLE FILE out of the index -- and live mode
    never runs audit_pass() to notice. Embedding the head is the small loss; losing the
    document is the large one.

    The mechanism changed on 2026-09-21 and the guarantee did not. It used to send the chunk,
    read a 500 and retry on the head. That cannot survive batching: one oversized chunk would
    fail every chunk travelling with it, so the loss would grow instead of shrink. The cut now
    happens before the request, which is what _head_within_ceiling already claimed to believe
    -- "The decision is the MEASUREMENT, never the provider's error string."
    """
    def never_called(*_args, **_kwargs):
        raise AssertionError("fitting must not need the server to fail first")

    monkeypatch.setattr(app.urllib.request, "urlopen", never_called)

    oversized = "Perizia tecnica dettagliata. " * 4000
    fitted, cost = app._fit_for_embedding(oversized)

    assert len(fitted) < len(oversized), "the oversized chunk was not cut"
    assert chunk.count_tokens(
        chunk.EMBED_DOC_PREFIX + fitted, add_special=True
    ) <= chunk.MODEL_MAX_TOKENS
    assert cost <= chunk.MODEL_MAX_TOKENS


def test_an_oversized_chunk_does_not_take_its_batch_mates_down_with_it(monkeypatch):
    """The failure mode batching introduces, and the reason the cut moved earlier."""
    sent: list[list[str]] = []

    def urlopen(req, *_args, **_kwargs):
        inputs = json.loads(req.data)["input"]
        sent.append(inputs)
        for text in inputs:
            if chunk.count_tokens(text, add_special=True) > chunk.MODEL_MAX_TOKENS:
                raise http_error(500, b"input is too large to process")
        return io.BytesIO(json.dumps({
            "data": [{"index": i, "embedding": [0.5, 0.5]} for i in range(len(inputs))]
        }).encode())

    monkeypatch.setattr(app.urllib.request, "urlopen", urlopen)

    vectors = asyncio.run(_embed_texts(["Perizia tecnica dettagliata. " * 4000, "corto"]))

    assert vectors == [[0.5, 0.5], [0.5, 0.5]], "the healthy chunk was lost with the oversized one"
    # Every input that reached the wire was already within the ceiling: nothing relied on a
    # 500 to discover it. Whether the two shared ONE request is CocoIndex's scheduling, not
    # this module's -- a bare asyncio.gather does not group, the flow does, and that is
    # verified end to end against a real document rather than asserted here.
    for inputs in sent:
        for text in inputs:
            assert chunk.count_tokens(text, add_special=True) <= chunk.MODEL_MAX_TOKENS


def test_vectors_are_placed_by_the_response_index_not_by_arrival(monkeypatch):
    """The endpoint does not promise order, and a misplaced vector is silently wrong:
    every vector is well-formed, it is simply attached to the wrong passage."""
    def urlopen(req, *_args, **_kwargs):
        inputs = json.loads(req.data)["input"]
        rows = [{"index": i, "embedding": [float(i)]} for i in range(len(inputs))]
        return io.BytesIO(json.dumps({"data": list(reversed(rows))}).encode())

    monkeypatch.setattr(app.urllib.request, "urlopen", urlopen)

    assert app._embed_batch(["a", "b", "c"]) == [[0.0], [1.0], [2.0]]


def test_a_request_is_bounded_by_tokens_as_well_as_by_count():
    """A count alone would put 32 ceiling-sized chunks behind one deadline."""
    big = app.EMBED_REQUEST_TOKEN_BUDGET // 2
    assert app._request_end([big, big, big], 0) == 2
    # An input over the budget on its own still goes, alone, rather than never.
    assert app._request_end([app.EMBED_REQUEST_TOKEN_BUDGET * 2, 1], 0) == 1


def test_a_failure_that_is_not_an_overflow_is_raised_rather_than_retried(monkeypatch):
    """Sending less does not fix a server that is restarting, and retrying would hide it."""
    calls = 0

    def urlopen(*_args, **_kwargs):
        nonlocal calls
        calls += 1
        raise http_error(503, b"model is loading")

    monkeypatch.setattr(app.urllib.request, "urlopen", urlopen)

    with pytest.raises(RuntimeError, match="503"):
        app._embed_batch(["una perizia corta"])
    assert calls == 1, "a non-overflow failure must not be retried"
