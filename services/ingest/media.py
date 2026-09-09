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


def index_text(path: str, file_name: str) -> str:
    """Text for the index, degrading to "" when nothing in this deployment can read it.

    Returning "" still indexes the file: it keeps its card, its name and its row, so it
    stays findable and the operator can see it exists. Raising here would fail the whole
    component and the file would have no row at all -- which is how two PNGs sat
    unindexed through 470 retries while their OCR sidecar was deliberately switched off.
    """
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
