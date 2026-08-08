"""Text extraction, and the door for the formats Aura could not open at all.

iscc-tika (Apache Tika via a GraalVM native image) is the extractor: measured on a
real corpus it handled 15 formats of 16 with zero failures, 18x faster than Docling
on the same PDF. It is the maintained fork of extractous, which has not been pushed
since 2024-12-21. NEVER import both in one process -- two native images collide with
NoSuchMethodError ... TesseractOCRConfig.setSkipOcr (scripts/ingest_image_contract_test.sh
asserts extractous stays out of sys.modules for this reason).

LibreOffice is here for a narrower reason: internal/documents/filecard/build.go routes
only .xlsx and .xlsm, so an ordinary 2010 .xls has no way into Aura's ETL path at all.
`soffice --convert-to` preserves what filecard needs -- banner rows, the real header
below them, and numeric cells that stay numeric.
"""

import contextlib
import pathlib
import subprocess
import tempfile

from iscc_tika import Extractor

# Everything LibreOffice can turn into the OOXML that BOTH consumers understand.
#
# It started as the three legacy Microsoft formats, for extraction only. The ODF family
# and .rtf are here now because of what a card looked like without them: MEASURED
# 2026-08-08 across the fixture set, .doc/.xls/.ppt/.odt/.ods/.odp/.rtf each produced
# "sample.ods -- file, 12 KB" and nothing else, because filecard routes .xlsx/.docx/.pptx
# and falls through to a bare size for anything else. Converting first means one
# spreadsheet gets the same card whether it arrived as .xlsx, .xls or .ods.
_LEGACY = {
    ".doc": "docx", ".xls": "xlsx", ".ppt": "pptx",
    ".odt": "docx", ".ods": "xlsx", ".odp": "pptx",
    ".rtf": "docx",
}

# extract_file_to_string truncates SILENTLY at its default max length -- no
# exception, no marker, just a shorter string. Set the cap once, far above any
# real document, on one reused instance rather than per call.
_extractor = Extractor()
_extractor.set_extract_string_max_length(50_000_000)


def needs_normalisation(path: str) -> bool:
    return pathlib.Path(path).suffix.lower() in _LEGACY


def normalise(path: str, outdir: str) -> str:
    """Convert a legacy Office file to its OOXML equivalent. Returns the new path."""
    target = _LEGACY[pathlib.Path(path).suffix.lower()]
    subprocess.run(
        ["soffice", "--headless", "--convert-to", target, "--outdir", outdir, path],
        check=True, capture_output=True, timeout=300,
    )
    out = pathlib.Path(outdir) / (pathlib.Path(path).stem + "." + target)
    if not out.exists():
        raise RuntimeError(f"soffice produced no {target} for {path}")
    return str(out)


@contextlib.contextmanager
def prepared(path: str):
    """Yield the path every consumer should read: OOXML, converting once if needed.

    Both consumers want the same converted file -- the extractor for its text and
    aura-filecard for its card -- and converting twice would mean running LibreOffice
    twice on the same bytes for one document. Yielding the path instead of hiding the
    conversion inside extract_text is what lets the card share it.
    """
    if not needs_normalisation(path):
        yield path
        return
    with tempfile.TemporaryDirectory() as tmp:
        yield normalise(path, tmp)


def extract_text(path: str) -> str:
    """Extract plain text from any office document, converting legacy formats first."""
    with prepared(path) as ready:
        # extract_file_to_string returns (text, metadata); metadata is Tika's own
        # provenance (Content-Type, page count, producer) -- a later task consumes
        # it, this signature stays text-only.
        text, _metadata = _extractor.extract_file_to_string(ready)
        return text
