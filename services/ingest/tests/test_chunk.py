"""Token-boundedness of chunk(): the embedding server (google/embeddinggemma-300m)
refuses anything past 2048 tokens with HTTP 500, so "no chunk exceeds the ceiling"
is a correctness test, not a style preference.
"""

from ingest.chunk import chunk, count_tokens


def test_no_chunk_exceeds_the_model_ceiling():
    text = "parola " * 20000
    for c in chunk(text, max_tokens=2048):
        assert count_tokens(c.text) <= 2048


def test_offsets_reconstruct_the_source():
    text = "alpha beta gamma delta " * 500
    for c in chunk(text, max_tokens=64):
        assert text[c.start:c.end] == c.text


def test_a_single_oversized_paragraph_is_split_not_dropped():
    text = "x" * 200000
    out = chunk(text, max_tokens=2048)
    assert len(out) > 1
    assert sum(len(c.text) for c in out) >= len(text) * 0.99


def test_empty_input_yields_no_chunks():
    assert chunk("", max_tokens=2048) == []
