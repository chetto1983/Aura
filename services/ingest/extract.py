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

import pathlib
import subprocess
import tempfile

from iscc_tika import Extractor

_LEGACY = {".doc": "docx", ".xls": "xlsx", ".ppt": "pptx"}

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


def extract_text(path: str) -> str:
    """Extract plain text from any office document, converting legacy formats first."""
    if needs_normalisation(path):
        with tempfile.TemporaryDirectory() as tmp:
            path = normalise(path, tmp)
            # extract_file_to_string returns (text, metadata); metadata is Tika's own
            # provenance (Content-Type, page count, producer) -- a later task consumes
            # it, this signature stays text-only.
            text, _metadata = _extractor.extract_file_to_string(path)
            return text
    text, _metadata = _extractor.extract_file_to_string(path)
    return text
