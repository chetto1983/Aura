"""An embedding failure must arrive carrying the two facts that diagnose it.

A bare HTTPError renders as "HTTP Error 500: Internal Server Error" and drops the body,
which is where llama.cpp names the actual fault. That silence cost two reproductions on
2026-08-09, and the overwhelmingly common cause -- a chunk over the model ceiling -- is
settled by a token count the error can carry itself.
"""

import io
import urllib.error

import pytest

from ingest import app, chunk


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
        app._embed("una perizia lunga")

    assert "input is too large to process" in str(caught.value)
    # Chained, so the traceback still reaches the transport-level cause.
    assert isinstance(caught.value.__cause__, urllib.error.HTTPError)
