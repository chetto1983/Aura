"""Token-boundedness of chunk(): the embedding server (google/embeddinggemma-300m)
refuses anything past 2048 tokens with HTTP 500, so "no chunk exceeds the ceiling"
is a correctness test, not a style preference.
"""

from ingest.chunk import _window_split, chunk, count_tokens


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


def test_window_split_offsets_reconstruct_a_boundary_less_source():
    # "y" * 5000 has no paragraph/sentence/word boundary at all, so
    # RecursiveSplitter returns it whole and the fixed-window SeparatorSplitter
    # is what cuts it. The offsets it reports are relative to the text it was
    # given, so the rebasing onto the parent's position is ours and is exactly
    # what this asserts -- embedded at a nonzero offset, as chunk() calls it.
    prefix = "PREFIX " * 3
    body = "y" * 5000
    document = prefix + body
    out = _window_split(body, offset=len(prefix), max_tokens=64)

    assert len(out) > 1
    for c in out:
        assert document[c.start:c.end] == c.text
    assert out[0].start == len(prefix)
    assert out[-1].end == len(document)
    for prev, nxt in zip(out, out[1:]):
        assert prev.end == nxt.start, "window chunks must be contiguous, no gap or overlap"


def test_a_boundary_less_document_is_covered_exactly_by_chunk():
    text = "w" * 200000
    out = chunk(text, max_tokens=2048)

    assert len(out) > 1
    for c in out:
        assert text[c.start:c.end] == c.text
    assert out[0].start == 0
    assert out[-1].end == len(text)
    for prev, nxt in zip(out, out[1:]):
        assert prev.end == nxt.start, "chunks must be contiguous, no gap or overlap"
