"""Extraction coverage across all 12 fixture formats.

Assertions split by family (scripts/fixtures/document_pipeline_e2e/GROUND_TRUTH.txt):
prose files (pdf/docx/doc/odt/rtf/pptx/ppt/odp) carry the constitution sentence;
spreadsheet files (xlsx/xls/ods/csv) carry an order table and never contained that
sentence -- asserting it there would be the test lying about the extractor, not the
extractor failing. sample.pptx/sample.ppt split the sentence across two text boxes
on slide 1 on purpose (no extractor recovers that); slide 2 carries it whole, which
is what makes the canary assertion still hold for those two formats.

The prose canary check locates the canary as a word sequence (words joined by
whitespace) rather than a literal substring. sample.pdf is LibreOffice's justified
rendering of the docx master, and PDFBox -- which iscc-tika uses for PDF -- inserts
a "\\n" at the PDF's actual visual line boundary (a positioned content stream has
no paragraph text run, only lines), which happens to land between "limiti" and
"della" here. scripts/extractor_matrix_test.sh, already validated on this same
fixture set, sidesteps the same boundary by using a shorter canary that doesn't
cross the line -- a single clean newline at a real line break is not the bug this
suite exists to catch. That bug is MarkItDown reflowing a justified PDF into ~50
double spaces per 1k chars: plausible-looking text an exact-phrase search can no
longer find. The fixture's own filler paragraph (see GROUND_TRUTH.txt) is deliberately
over-justified to provoke exactly that, so the regression check below inspects the
canary's own matched span, not whole-document density -- the filler paragraph
justifying hard is expected and irrelevant to whether the canary itself reflowed.
"""

import pathlib
import re

import pytest

from ingest.extract import extractable, extract_text, needs_normalisation, normalise

FX = pathlib.Path("/fx")

CANARY = (
    "La sovranita appartiene al popolo che la esercita nelle forme e nei "
    "limiti della Costituzione."
)
_CANARY_PATTERN = re.compile(r"\s+".join(re.escape(w) for w in CANARY.split()))
SHEET_TOKENS = ("Codice", "Descrizione", "A9A26924")

PROSE_FORMATS = ["pdf", "docx", "doc", "odt", "rtf", "pptx", "ppt", "odp"]
SHEET_FORMATS = ["xlsx", "xls", "ods", "csv"]
ALL_FORMATS = PROSE_FORMATS + SHEET_FORMATS


@pytest.mark.parametrize("ext", ALL_FORMATS)
def test_every_office_format_yields_text(ext):
    text = extract_text(str(FX / f"sample.{ext}"))
    assert len(text) > 0
    assert "<?xml" not in text[:200] and "{\\rtf" not in text[:200], (
        "returned markup, not text"
    )


@pytest.mark.parametrize("ext", PROSE_FORMATS)
def test_prose_canary_survives_without_reflow(ext):
    text = extract_text(str(FX / f"sample.{ext}"))
    match = _CANARY_PATTERN.search(text)
    assert match is not None, "canary sentence not found, even word by word"
    span = match.group(0)
    assert "  " not in span, f"canary reflowed with a double space: {span!r}"


@pytest.mark.parametrize("ext", SHEET_FORMATS)
def test_spreadsheet_tokens_survive(ext):
    text = extract_text(str(FX / f"sample.{ext}"))
    for token in SHEET_TOKENS:
        assert token in text


def test_legacy_formats_are_flagged_for_normalisation():
    assert needs_normalisation("a.doc")
    assert needs_normalisation("a.xls")
    assert needs_normalisation("a.ppt")
    assert not needs_normalisation("a.docx")
    assert not needs_normalisation("a.xlsx")
    assert not needs_normalisation("a.pptx")
    assert not needs_normalisation("a.pdf")


def test_normalise_converts_legacy_doc_to_ooxml(tmp_path):
    out = normalise(str(FX / "sample.doc"), str(tmp_path))
    assert out.endswith(".docx")
    assert pathlib.Path(out).exists()


def test_extractable_routes_by_family():
    """A format Tika cannot parse must be decided BEFORE the call, not caught after it.

    The raise killed the whole CocoIndex component, so one .png in a bucket stopped that
    file being indexed at all. These are the families the measured matrix covers plus the
    ones LibreOffice normalises into them.
    """
    for name in ["a.pdf", "b.docx", "c.xlsx", "d.txt", "e.md", "f.csv", "G.PDF"]:
        assert extractable(name), name
    for name in ["a.doc", "b.xls", "c.ppt", "d.odt", "e.ods", "f.odp", "g.rtf"]:
        assert extractable(name), name
    for name in ["photo.png", "photo.jpg", "clip.mp3", "clip.wav", "archive.zip", "noext"]:
        assert not extractable(name), name
