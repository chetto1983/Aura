"""The route, the width and the space, as the supervisor hands them over.

internal/ingestsupervisor/route.go resolves the route and this process only obeys it. Each
test sets the environment the supervisor would and reloads the module, which reads it once at
import -- the lifetime a route has in a real child.
"""

import asyncio
import importlib
import io
import json
import os
import subprocess
import sys
import urllib.error

import cocoindex as coco
import pytest

from ingest import chunk, embed

HOSTED = {
    "AURA_EMBED_BASE_URL": "https://openrouter.ai/api",
    "AURA_EMBED_MODEL": "vendor/embed",
    "AURA_EMBED_API_KEY": "sk-test-key",
    "AURA_EMBED_INPUT_LIMIT": "64",
    "AURA_EMBED_DIMENSIONS": "4",
    "AURA_EMBED_SPACE": "es1-hosted0000000000",
}


@pytest.fixture(autouse=True)
def keep_the_tokenizer_state(monkeypatch):
    """A failure path asks the tokenizer, and an unreachable one is sticky for the process."""
    monkeypatch.setattr(chunk, "_server_reachable", chunk._server_reachable)


@pytest.fixture
def hosted(monkeypatch):
    for key, value in HOSTED.items():
        monkeypatch.setenv(key, value)
    yield importlib.reload(embed)
    monkeypatch.undo()
    importlib.reload(embed)


def _answer(vectors):
    return io.BytesIO(json.dumps(
        {"data": [{"index": i, "embedding": v} for i, v in enumerate(vectors)]}
    ).encode())


def _http_error(code):
    return urllib.error.HTTPError(
        url="http://embed/v1/embeddings", code=code, msg="refused", hdrs=None, fp=io.BytesIO(b"refused"),
    )


def _embeddings_only(answer):
    """Route /v1/embeddings to `answer`; the tokenizer is unreachable in these tests."""
    def urlopen(req, *args, **kwargs):
        if not req.full_url.endswith("/v1/embeddings"):
            raise urllib.error.URLError("no tokenizer in this test")
        return answer(req)
    return urlopen


def test_a_hosted_request_names_the_model_the_width_and_the_key(hosted, monkeypatch):
    sent = []

    def answer(req):
        sent.append(req)
        return _answer([[0.5, 0.5, 0.5, 0.5]])

    monkeypatch.setattr(hosted.urllib.request, "urlopen", _embeddings_only(answer))

    hosted._embed_batch(["ciao"])

    body = json.loads(sent[0].data)
    assert sent[0].full_url == "https://openrouter.ai/api/v1/embeddings"
    assert body["model"] == "vendor/embed" and body["dimensions"] == 4
    assert body["input"] == [chunk.EMBED_DOC_PREFIX + "ciao"]
    assert sent[0].get_header("Authorization") == "Bearer sk-test-key"


def test_the_local_route_sends_neither_a_key_nor_a_width(monkeypatch):
    sent = []

    def answer(req):
        sent.append(req)
        return _answer([[0.0] * embed.DIMENSIONS])

    monkeypatch.setattr(embed.urllib.request, "urlopen", _embeddings_only(answer))

    embed._embed_batch(["ciao"])

    assert "dimensions" not in json.loads(sent[0].data)
    assert sent[0].get_header("Authorization") is None


def test_a_wider_vector_is_cut_to_the_width_and_renormalised(hosted, monkeypatch):
    monkeypatch.setattr(hosted.urllib.request, "urlopen",
                        _embeddings_only(lambda _req: _answer([[3.0, 4.0, 0.0, 0.0, 9.0, 9.0]])))

    [vector] = hosted._embed_batch(["ciao"])

    assert vector == pytest.approx([0.6, 0.8, 0.0, 0.0])


def test_a_narrower_vector_is_refused_rather_than_stored(hosted, monkeypatch):
    monkeypatch.setattr(hosted.urllib.request, "urlopen", _embeddings_only(lambda _req: _answer([[1.0, 0.0]])))

    with pytest.raises(hosted.EmbedRequestError, match="dimension 2, want 4"):
        hosted._embed_batch(["ciao"])


def test_a_hosted_input_is_cut_to_the_published_limit_in_bytes(hosted):
    # Two bytes each, so a cut in the middle of one would not decode.
    fitted, _ = hosted._fit("è" * 100)

    sent = (chunk.EMBED_DOC_PREFIX + fitted).encode("utf-8")
    assert len(sent) + hosted.EMBED_SPECIAL_TOKENS <= int(HOSTED["AURA_EMBED_INPUT_LIMIT"])
    assert fitted and set(fitted) == {"è"}


def test_a_hosted_input_that_fits_is_sent_unchanged(hosted):
    assert hosted._fit("ciao")[0] == "ciao"


def test_the_key_never_reaches_a_child_process(hosted):
    assert "AURA_EMBED_API_KEY" not in os.environ
    child = subprocess.run(
        [sys.executable, "-c", "import os; print(os.environ.get('AURA_EMBED_API_KEY', ''))"],
        capture_output=True, text=True, check=True,
    )
    assert child.stdout.strip() == ""


def _import_embed(changes):
    env = dict(os.environ)
    for key, value in changes.items():
        if value is None:
            env.pop(key, None)
        else:
            env[key] = value
    return subprocess.run([sys.executable, "-c", "import ingest.embed"], env=env, capture_output=True, text=True)


def test_a_child_without_a_space_refuses_to_start():
    done = _import_embed({"AURA_EMBED_SPACE": None})

    assert done.returncode != 0 and "AURA_EMBED_SPACE is required" in done.stderr


def test_a_hosted_child_without_a_key_refuses_to_start():
    done = _import_embed({"AURA_EMBED_MODEL": "vendor/embed", "AURA_EMBED_API_KEY": None,
                          "AURA_EMBED_INPUT_LIMIT": "8192"})

    assert done.returncode != 0 and "AURA_EMBED_API_KEY is empty" in done.stderr


def test_a_refused_input_asks_cocoindex_to_split_the_batch(monkeypatch):
    def refuse(_req):
        raise _http_error(400)

    monkeypatch.setattr(embed.urllib.request, "urlopen", _embeddings_only(refuse))

    with pytest.raises(coco.RetryWithSmallerBatch) as caught:
        embed._embed_texts(["uno", "due"])

    assert isinstance(caught.value.__cause__, embed.EmbedRequestError)
    assert caught.value.__cause__.status == 400


@pytest.mark.parametrize("code", [401, 403, 429, 500, 503])
def test_a_route_failure_fails_the_batch_without_splitting_it(monkeypatch, code):
    def fail(_req):
        raise _http_error(code)

    monkeypatch.setattr(embed.urllib.request, "urlopen", _embeddings_only(fail))

    with pytest.raises(embed.EmbedRequestError) as caught:
        embed._embed_texts(["uno"])

    assert caught.value.status == code


def test_one_refused_input_fails_only_its_own_caller(monkeypatch):
    refused: list[int] = []

    def answer(req):
        inputs = json.loads(req.data)["input"]
        if any("RIFIUTATO" in text for text in inputs):
            refused.append(len(inputs))
            raise _http_error(400)
        return _answer([[0.0] * embed.DIMENSIONS for _ in inputs])

    monkeypatch.setattr(embed.urllib.request, "urlopen", _embeddings_only(answer))

    async def embed_all():
        texts = ["uno", "RIFIUTATO", "tre", "quattro"]
        return await asyncio.gather(*(embed.embed_text(text) for text in texts), return_exceptions=True)

    results = asyncio.run(embed_all())

    # A refused text sent alone fails alone whether or not a batch is split, so the
    # outcome below proves the split only if the refused text shared a request.
    assert max(refused) > 1, f"the refused text never shared a request: {refused}"
    assert isinstance(results[1], embed.EmbedRequestError)
    assert all(isinstance(result, list) for index, result in enumerate(results) if index != 1)
