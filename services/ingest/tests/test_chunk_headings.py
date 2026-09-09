"""Every chunk carries the section it starts in.

Stamping rather than cutting is deliberate: the chunk boundaries do not move, so every
char_start/char_end already issued as a citation stays valid and no document needs
re-ingesting to gain its headings. Cutting on section boundaries is a separate change with
its own measurement.
"""
from ingest import chunk, outline


def _stamped(text, entries, **kwargs):
    anchors = outline.anchors_in(text, entries)
    return chunk.chunk(text, anchors=anchors, **kwargs)


def test_a_chunk_carries_the_section_it_starts_in():
    body = "corpo del capitolo. " * 40
    text = "Chapter 1. Welcome\n" + body + "\nChapter 2. SQL\n" + body
    entries = [(0, "Chapter 1. Welcome"), (0, "Chapter 2. SQL")]

    pieces = _stamped(text, entries, max_tokens=64)

    assert pieces, "the fixture must produce chunks"
    for piece in pieces:
        expected = ["Chapter 2. SQL"] if piece.start >= text.index("Chapter 2. SQL") \
            else ["Chapter 1. Welcome"]
        assert piece.heading_path == expected, (piece.start, piece.heading_path)


def test_text_before_the_first_section_has_no_heading():
    text = "frontespizio senza titolo\n\n" + "Chapter 1. Welcome\n" + "corpo. " * 60
    entries = [(0, "Chapter 1. Welcome")]

    pieces = _stamped(text, entries, max_tokens=64)

    assert pieces[0].heading_path == []


def test_the_full_ancestry_is_carried_not_only_the_leaf():
    text = ("Chapter 6. Data Modeling\n" + "intro. " * 20
            + "\n6.4. Vector\n" + "vettori. " * 20
            + "\n6.4.19. Group-By Retrieval\n" + "gruppi. " * 40)
    entries = [(0, "Chapter 6. Data Modeling"), (1, "6.4. Vector"), (2, "6.4.19. Group-By Retrieval")]

    pieces = _stamped(text, entries, max_tokens=64)
    deepest = [p for p in pieces if p.start >= text.index("6.4.19.")]

    assert deepest
    assert deepest[0].heading_path == [
        "Chapter 6. Data Modeling", "6.4. Vector", "6.4.19. Group-By Retrieval",
    ]


def test_without_anchors_nothing_changes():
    text = "un testo qualunque. " * 40

    assert all(p.heading_path == [] for p in chunk.chunk(text, max_tokens=64))


def test_stamping_does_not_move_a_single_boundary():
    """The whole reason this stamps instead of cutting: a citation already issued against
    char_start/char_end must still point at the same bytes."""
    text = "Chapter 1. Welcome\n" + "corpo del capitolo. " * 60
    entries = [(0, "Chapter 1. Welcome")]

    plain = chunk.chunk(text, max_tokens=64)
    stamped = _stamped(text, entries, max_tokens=64)

    assert [(p.start, p.end) for p in plain] == [(p.start, p.end) for p in stamped]
