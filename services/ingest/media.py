import pathlib
import os
import subprocess

import cocoindex as coco

from ingest import extract


_IMAGE_EXTENSIONS = frozenset({".gif", ".jpeg", ".jpg", ".png", ".webp"})
_AUDIO_EXTENSIONS = frozenset({
    ".aac", ".flac", ".m4a", ".mp3", ".mp4", ".oga", ".ogg", ".opus", ".wav", ".webm",
})
_PDF_EXTENSION = ".pdf"

# The bridge's own exit code for "no configured route accepts images" (see
# cmd/aura-media-index exitNoVisionRoute). Distinct from any other failure on purpose.
_EXIT_NO_VISION_ROUTE = 3

CONFIG_FINGERPRINT = coco.ContextKey[str]("media_config_fingerprint", detect_change=True)
_BINARY = "aura-media-index"


def kind(file_name: str) -> str | None:
    suffix = pathlib.PurePath(file_name).suffix.lower()
    if suffix in _IMAGE_EXTENSIONS:
        return "image"
    if suffix in _AUDIO_EXTENSIONS:
        return "audio"
    return None


class NoVisionRoute(RuntimeError):
    """No configured route can read an image, which is a fact about the configuration.

    Separate from every other failure because the answers differ: this one degrades the
    file to card-only, an unreachable endpoint keeps raising so the next cycle retries it.
    Degrading on an outage would let CocoIndex memoize a blank answer and the image would
    never be read again after the endpoint came back.
    """


def _run(args: list[str], timeout: float) -> str:
    try:
        done = subprocess.run(
            [_BINARY, *args], check=False, capture_output=True, timeout=timeout,
        )
    except (OSError, subprocess.SubprocessError) as exc:
        raise RuntimeError(f"media indexer could not run: {exc}") from exc
    if done.returncode != 0:
        detail = done.stderr.decode("utf-8", "replace").strip()
        if done.returncode == _EXIT_NO_VISION_ROUTE:
            raise NoVisionRoute(detail or "no configured route accepts images")
        raise RuntimeError(
            f"media indexer exited {done.returncode}" + (f": {detail}" if detail else "")
        )
    return done.stdout.decode("utf-8", "replace").strip()


def fingerprint() -> str:
    return _run(["-fingerprint"], timeout=30)


def derive(path: str, file_name: str) -> str:
    media_kind = kind(file_name or path)
    if media_kind is None:
        return ""
    request_timeout = max(1, int(os.environ.get("MULTIMODAL_TIMEOUT_SEC", "120")))
    return _run(
        ["-kind", media_kind, "-name", file_name, path],
        timeout=(3 * request_timeout) + 30,
    )


def derive_scanned_pdf(path: str, file_name: str) -> str:
    """Render and OCR an image-only PDF through the existing selected vision model."""
    request_timeout = max(1, int(os.environ.get("MULTIMODAL_TIMEOUT_SEC", "120")))
    return _run(
        ["-kind", "pdf", "-name", file_name, path],
        timeout=(3 * request_timeout) + 30,
    )


# A spreadsheet is queried, not read. The product contract already says an aggregate cannot
# come from a few passages and points at document_open for counts, sums, groupings and
# cross-column filters, so chunking one produces passages nothing should ever answer from.
#
# Measured 2026-09-09 on eight Italian reference tables (comuni, CAP, province, nazioni):
# 3.4M characters became 981 passages -- more than the entire live corpus -- and the Caraglio
# row landed in a 2,114-character chunk beside fourteen unrelated municipalities from
# Calabria to Piemonte, then collapsed into ONE vector whose meaning is their average. The
# question those files answer, "the CAP of Caraglio", is an exact lookup on a key.
#
# Legacy .xls/.ods arrive here already normalised to .xlsx; the originals are listed anyway
# so the routing does not depend on normalisation having run.
_SPREADSHEET_TYPES = {
    "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
    "application/vnd.ms-excel",
    "application/vnd.ms-excel.sheet.macroenabled.12",
    "application/vnd.oasis.opendocument.spreadsheet",
}
# The extension is the FALLBACK, not the rule: Garage answers the content type on the same
# HEAD the sweep already makes, so a file whose name lies is still routed correctly. An
# object stored without one degrades to octet-stream, and then the suffix is all there is.
_SPREADSHEET_EXTENSIONS = {".xlsx", ".xlsm", ".xls", ".ods"}


def _is_spreadsheet(path: str, content_type: str | None) -> bool:
    if content_type:
        normalised = content_type.split(";")[0].strip().lower()
        if normalised in _SPREADSHEET_TYPES:
            return True
        if normalised not in {"", "application/octet-stream", "binary/octet-stream"}:
            return False
    return pathlib.PurePath(path).suffix.lower() in _SPREADSHEET_EXTENSIONS


def index_text(path: str, file_name: str, content_type: str | None = None) -> str:
    """Text for the index, degrading to "" when nothing in this deployment can read it.

    Returning "" still indexes the file: it keeps its card, its name and its row, so it
    stays findable and the operator can see it exists. Raising here would fail the whole
    component and the file would have no row at all -- which is how two PNGs sat
    unindexed through 470 retries while their OCR sidecar was deliberately switched off.
    """
    if _is_spreadsheet(path, content_type):
        return ""
    try:
        if extract.extractable(path):
            text = extract.extract_text(path)
            # LibreChat gives configured OCR precedence over its document parser; Hermes
            # extracts text first and hands image-only pages to vision. Aura takes the narrow
            # latter boundary: digital PDFs stay on the fast local parser, while a PDF whose
            # complete text layer is blank uses the already-selected, DB-overlaid vision route.
            if pathlib.PurePath(path).suffix.lower() == _PDF_EXTENSION and not text.strip():
                return extract_scanned_pdf(path, file_name)
            return text
        return derive(path, file_name)
    except NoVisionRoute:
        return ""



@coco.fn(memo=True, version=1)
def extract_scanned_pdf(path: str, file_name: str) -> str:
    coco.use_context(CONFIG_FINGERPRINT)
    return derive_scanned_pdf(path, file_name)
