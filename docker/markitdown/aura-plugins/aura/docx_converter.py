"""
Per-heading DOCX decomposition for Aura's wiki heading-subnode indexer.

Uses only stdlib ZIP/XML parsing so the sidecar keeps the markitdown[all]
dependency set. Documents without Heading 1/2/3 styled paragraphs fall back to
MarkItDown's built-in DOCX converter.
"""

import io
import zipfile
from typing import Any, BinaryIO
from xml.etree import ElementTree as ET

from markitdown import DocumentConverter, DocumentConverterResult, StreamInfo
from markitdown.converters import DocxConverter

_DOCX_EXTENSIONS = {".docx"}
_DOCX_MIME_PREFIXES = (
    "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
)

_W = "{http://schemas.openxmlformats.org/wordprocessingml/2006/main}"
_CP = "{http://schemas.openxmlformats.org/package/2006/metadata/core-properties}"
_DC = "{http://purl.org/dc/elements/1.1/}"
_DCTERMS = "{http://purl.org/dc/terms/}"


def _yaml_scalar(s: str) -> str:
    safe = str(s).replace("\\", "\\\\").replace('"', '\\"').replace("\n", "\\n")
    return f'"{safe}"'


def _normalise_text(s: str) -> str:
    return " ".join(str(s).split())


def _style_id(elem: ET.Element) -> str:
    pstyle = elem.find(f"{_W}pPr/{_W}pStyle")
    if pstyle is None:
        return ""
    return pstyle.attrib.get(f"{_W}val", "")


def _heading_level(style_id: str) -> int | None:
    norm = style_id.lower().replace(" ", "").replace("-", "").replace("_", "")
    if norm in {"heading1", "titre1"}:
        return 1
    if norm in {"heading2", "titre2"}:
        return 2
    if norm in {"heading3", "titre3"}:
        return 3
    return None


def _paragraph_text(elem: ET.Element) -> str:
    parts: list[str] = []
    for node in elem.iter():
        if node.tag == f"{_W}t" and node.text:
            parts.append(node.text)
        elif node.tag == f"{_W}tab":
            parts.append(" ")
        elif node.tag in {f"{_W}br", f"{_W}cr"}:
            parts.append("\n")
    return _normalise_text("".join(parts))


def _is_numbered_or_bullet(elem: ET.Element) -> bool:
    return elem.find(f"{_W}pPr/{_W}numPr") is not None


def _cell_text(elem: ET.Element) -> str:
    text = _normalise_text(" ".join(_paragraph_text(p) for p in elem.findall(f".//{_W}p")))
    if not text:
        return " "
    return text.replace("|", "\\|")


def _render_table(elem: ET.Element) -> list[str]:
    rows: list[list[str]] = []
    for tr in elem.findall(f"{_W}tr"):
        cells = [_cell_text(tc) for tc in tr.findall(f"{_W}tc")]
        if cells:
            rows.append(cells)
    if not rows:
        return []

    width = max(len(row) for row in rows)
    normalised = [row + [" "] * (width - len(row)) for row in rows]
    lines = [
        "| " + " | ".join(normalised[0]) + " |",
        "| " + " | ".join(["---"] * width) + " |",
    ]
    for row in normalised[1:]:
        lines.append("| " + " | ".join(row) + " |")
    return lines


def _core_props(zf: zipfile.ZipFile) -> tuple[str, str, str]:
    try:
        root = ET.fromstring(zf.read("docProps/core.xml"))
    except Exception:
        return "", "unknown", ""

    def find_text(tag: str) -> str:
        node = root.find(tag)
        if node is None or node.text is None:
            return ""
        return node.text.strip()

    title = find_text(f"{_DC}title")
    author = find_text(f"{_DC}creator") or "unknown"
    created = find_text(f"{_DCTERMS}created")
    return title, author, created


def _document_root(data: bytes) -> ET.Element:
    with zipfile.ZipFile(io.BytesIO(data), "r") as zf:
        return ET.fromstring(zf.read("word/document.xml"))


class AuraDocxConverter(DocumentConverter):
    """
    Per-heading DOCX converter. Output shape:
      ---
      document_title: "..."
      author: "..."
      created: "..."
      heading_counts:
        h1: 1
        h2: 2
        h3: 3
      ---

      ### Heading text
      Paragraph text under that heading.
    """

    def accepts(
        self,
        file_stream: BinaryIO,
        stream_info: StreamInfo,
        **kwargs: Any,
    ) -> bool:
        ext = (getattr(stream_info, "extension", "") or "").lower()
        mime = (getattr(stream_info, "mimetype", "") or "").lower()
        return ext in _DOCX_EXTENSIONS or any(
            mime.startswith(p) for p in _DOCX_MIME_PREFIXES
        )

    def convert(
        self,
        file_stream: BinaryIO,
        stream_info: StreamInfo,
        **kwargs: Any,
    ) -> DocumentConverterResult:
        data = file_stream.read()
        root = _document_root(data)
        body = root.find(f"{_W}body")
        if body is None:
            return DocxConverter().convert(io.BytesIO(data), stream_info, **kwargs)

        heading_counts = {"h1": 0, "h2": 0, "h3": 0}
        for elem in body:
            if elem.tag != f"{_W}p":
                continue
            level = _heading_level(_style_id(elem))
            if level is not None and _paragraph_text(elem):
                heading_counts[f"h{level}"] += 1

        if sum(heading_counts.values()) == 0:
            return DocxConverter().convert(io.BytesIO(data), stream_info, **kwargs)

        with zipfile.ZipFile(io.BytesIO(data), "r") as zf:
            doc_title, author, created = _core_props(zf)
        filename = (getattr(stream_info, "filename", None) or "").strip()
        title = doc_title or filename or "document"

        lines: list[str] = ["---"]
        lines.append(f"document_title: {_yaml_scalar(title)}")
        lines.append(f"author: {_yaml_scalar(author)}")
        lines.append(f"created: {_yaml_scalar(created)}")
        lines.append("heading_counts:")
        for key in ("h1", "h2", "h3"):
            lines.append(f"  {key}: {heading_counts[key]}")
        lines.append("---")
        lines.append("")

        for elem in body:
            if elem.tag == f"{_W}p":
                text = _paragraph_text(elem)
                if not text:
                    continue
                level = _heading_level(_style_id(elem))
                if level is not None:
                    lines.append(f"### {text}")
                elif _is_numbered_or_bullet(elem):
                    lines.append(f"- {text}")
                else:
                    lines.append(text)
                lines.append("")
            elif elem.tag == f"{_W}tbl":
                table_lines = _render_table(elem)
                if table_lines:
                    lines.extend(table_lines)
                    lines.append("")

        return DocumentConverterResult(markdown="\n".join(lines).rstrip(), title=title)
