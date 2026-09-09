"""A PDF usually carries its own table of contents. Anchoring it into the extracted text
is what gives a passage the section it belongs to, which is the field retrieval already
returns and nothing has ever filled."""
import pytest

from ingest import outline


def test_anchors_walk_the_outline_in_document_order():
    text = (
        "Chapter 1. Welcome\nbenvenuto\n\n"
        "1.1. Installazione\ncome si installa\n\n"
        "Chapter 2. SQL\nil linguaggio\n\n"
        "2.1. SELECT\nla proiezione\n"
    )
    entries = [
        (0, "Chapter 1. Welcome"), (1, "1.1. Installazione"),
        (0, "Chapter 2. SQL"), (1, "2.1. SELECT"),
    ]

    anchors = outline.anchors_in(text, entries)

    assert [a.offset for a in anchors] == [text.index(t) for _, t in entries]
    assert [a.heading_path for a in anchors] == [
        ("Chapter 1. Welcome",),
        ("Chapter 1. Welcome", "1.1. Installazione"),
        ("Chapter 2. SQL",),
        ("Chapter 2. SQL", "2.1. SELECT"),
    ]


def test_a_title_repeated_in_the_table_of_contents_anchors_at_its_section():
    """The scan is forward-only, which is what steps past the contents listing: the second
    occurrence of a title is its real heading, and every later title is found after it."""
    text = (
        "INDICE\n1.1. Installazione . . . 4\n2.1. SELECT . . . 9\n\n"
        "1.1. Installazione\ncome si installa\n\n"
        "2.1. SELECT\nla proiezione\n"
    )
    entries = [(1, "1.1. Installazione"), (1, "2.1. SELECT")]

    anchors = outline.anchors_in(text, entries)

    # rindex on both: the first occurrence of each is the contents line, which is exactly
    # what must not be anchored.
    assert anchors[0].offset == text.rindex("1.1. Installazione")
    assert anchors[1].offset == text.rindex("2.1. SELECT")


def test_a_title_the_text_does_not_carry_is_skipped_not_guessed():
    text = "Chapter 1. Welcome\nbenvenuto\n"
    entries = [(0, "Chapter 1. Welcome"), (1, "1.1. Assente"), (0, "Chapter 2. SQL")]

    assert [a.heading_path for a in outline.anchors_in(text, entries)] == [
        ("Chapter 1. Welcome",)
    ]


def test_a_deeper_level_than_its_parent_still_produces_a_path():
    """Outlines are not always well-formed: a jump from depth 0 to depth 2 must not crash
    and must not invent a level that does not exist."""
    text = "Parte I\ntesto\n\n1.1.1. Dettaglio\naltro testo\n"
    entries = [(0, "Parte I"), (2, "1.1.1. Dettaglio")]

    anchors = outline.anchors_in(text, entries)

    assert anchors[1].heading_path == ("Parte I", "1.1.1. Dettaglio")


@pytest.mark.parametrize("name", ["nota.txt", "foglio.xlsx", "pagina.html"])
def test_titles_from_a_non_pdf_are_empty(tmp_path, name):
    path = tmp_path / name
    path.write_bytes(b"not a pdf")

    assert outline.titles_of(str(path)) == []


def test_titles_of_a_pdf_without_an_outline_are_empty(tmp_path):
    path = tmp_path / "senza-indice.pdf"
    path.write_bytes(b"%PDF-1.4\nnot really a pdf either\n")

    assert outline.titles_of(str(path)) == []


def test_a_title_that_prefixes_a_longer_one_does_not_match_it():
    """Measured on the Italian Constitution: its outline offers "Titolo I", which occurs 16
    times in the text because it opens "Titolo II" and "Titolo III" too. Matching the prefix
    walks the cursor into the wrong section and drags every later anchor with it."""
    # Padded so the anchor sits early: this test is about the prefix, not about the
    # trailing-index guard, and a fixture short enough to trip that would test both.
    text = "Titolo II\nRapporti etico-sociali\n\nTitolo I\nRapporti civili\n" + "x" * 400
    entries = [(0, "Titolo I")]

    anchors = outline.anchors_in(text, entries)

    assert anchors[0].offset == text.index("Titolo I\n")


def test_a_title_inside_a_word_is_not_a_heading():
    text = "Sottotitolo I non conta\n\nTitolo I\nil vero\n" + "x" * 400
    entries = [(0, "Titolo I")]

    assert outline.anchors_in(text, entries)[0].offset == text.index("Titolo I\n")


def test_an_outline_that_only_matches_a_trailing_index_is_refused():
    """Fail closed. Measured 2026-09-09: the ArcadeDB manual's first anchor is at character 1
    of 1,944,898, while the Italian Constitution's first body anchor is at 96,414 of 96,987 —
    a 538-character index at the end — because its outline titles do not match the typography
    of its body. A heading_path taken from that index would be confidently wrong on every
    passage; empty is what every reader already handles."""
    body = "x" * 90_000
    text = body + "\nCapitolo I\nCapitolo II\nCapitolo III\n"
    entries = [(0, "Capitolo I"), (0, "Capitolo II"), (0, "Capitolo III")]

    assert outline.anchors_in(text, entries) == []


def test_an_outline_that_reaches_the_body_is_kept():
    text = "Capitolo I\n" + "x" * 40_000 + "\nCapitolo II\n" + "y" * 40_000 + "\nCapitolo III\n"
    entries = [(0, "Capitolo I"), (0, "Capitolo II"), (0, "Capitolo III")]

    assert len(outline.anchors_in(text, entries)) == 3


def test_markdown_headings_become_the_outline(tmp_path):
    """Markdown keeps its outline in the body, so the PDF branch found nothing at all:
    measured 2026-09-09, prd.md and aura-quality-snapshot.md came back with a character
    span and an empty heading_path while their passage text carried the heading line."""
    source = tmp_path / "prd.md"
    source.write_text(
        "# Aura

intro

## 1. Scope

lo scopo

### 1.1 Limiti

i limiti

"
        "## 2. Architettura

i livelli
",
        encoding="utf-8",
    )

    entries = outline.titles_of(str(source))

    assert entries == [
        (1, "Aura"), (2, "1. Scope"), (3, "1.1 Limiti"), (2, "2. Architettura"),
    ]
    # The hashes are left behind because anchors_in searches the EXTRACTED text, where the
    # line still reads "## 1. Scope": the bare title lands on the heading, hashes and all.
    text = source.read_text(encoding="utf-8")
    anchors = outline.anchors_in(text, entries)
    assert [a.heading_path for a in anchors] == [
        ("Aura",),
        ("Aura", "1. Scope"),
        ("Aura", "1. Scope", "1.1 Limiti"),
        ("Aura", "2. Architettura"),
    ]


def test_the_parser_reads_what_a_pattern_of_ours_would_lose(tmp_path):
    """Each of these is a case a hand-written ATX regex gets wrong or has to special-case:
    a '#' inside a fence is a shell comment, closing hashes are not part of the title, and
    a setext heading is underlined rather than prefixed."""
    source = tmp_path / "guide.md"
    source.write_text(
        "# Guida

```bash
# non e' una sezione
make quality
```

"
        "## Davvero ##

Sottotitolo
===========

| a | b |
|---|---|
",
        encoding="utf-8",
    )

    assert outline.titles_of(str(source)) == [
        (1, "Guida"), (2, "Davvero"), (1, "Sottotitolo"),
    ]


def test_a_format_neither_branch_reads_keeps_its_empty_outline(tmp_path):
    """An unreadable outline must leave the document indexed exactly as it is today."""
    source = tmp_path / "note.txt"
    source.write_text("# non markdown
", encoding="utf-8")

    assert outline.titles_of(str(source)) == []
    assert outline.titles_of(str(tmp_path / "assente.md")) == []
