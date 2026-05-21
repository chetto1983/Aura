"""
HTML converter tuned for Aura wiki retrieval.

The built-in MarkItDown HTML converter is good for plain pages, but Aura's
heading subnode indexer benefits from predictable ### sections. This converter
normalizes document headings and gives unheaded section/article blocks a stable
section heading before rendering Markdown.
"""

import io
from typing import Any, BinaryIO

from bs4 import BeautifulSoup, Tag
from markdownify import markdownify as md
from markitdown import DocumentConverter, DocumentConverterResult, StreamInfo
from markitdown.converters._html_converter import HtmlConverter

_HTML_EXTENSIONS = {".html", ".htm"}
_HTML_MIME_PREFIXES = ("text/html",)
_DROP_TAGS = ("script", "style", "nav", "footer", "aside")
_HEADING_TAGS = ("h1", "h2", "h3", "h4", "h5", "h6")


def _yaml_scalar(value: str) -> str:
    safe = str(value).replace("\\", "\\\\").replace('"', '\\"').replace("\n", "\\n")
    return f'"{safe}"'


def _clean_markdown(text: str) -> str:
    lines = [line.rstrip() for line in text.strip().splitlines()]
    compact: list[str] = []
    blank = False
    for line in lines:
        is_blank = line == ""
        if is_blank and blank:
            continue
        compact.append(line)
        blank = is_blank
    return "\n".join(compact).strip()


class AuraHtmlConverter(DocumentConverter):
    """HTML converter that emits Aura-friendly ### retrieval sections."""

    def __init__(self) -> None:
        self._fallback = HtmlConverter()

    def accepts(
        self,
        file_stream: BinaryIO,
        stream_info: StreamInfo,
        **kwargs: Any,
    ) -> bool:
        ext = (getattr(stream_info, "extension", "") or "").lower()
        mime = (getattr(stream_info, "mimetype", "") or "").lower()
        return ext in _HTML_EXTENSIONS or any(
            mime.startswith(prefix) for prefix in _HTML_MIME_PREFIXES
        )

    def convert(
        self,
        file_stream: BinaryIO,
        stream_info: StreamInfo,
        **kwargs: Any,
    ) -> DocumentConverterResult:
        data = file_stream.read()
        encoding = "utf-8" if stream_info.charset is None else stream_info.charset
        soup = BeautifulSoup(data, "html.parser", from_encoding=encoding)

        page_title = ""
        if soup.title and soup.title.string:
            page_title = soup.title.string.strip()

        description = ""
        meta_description = soup.find("meta", attrs={"name": "description"})
        if meta_description is not None:
            description = (meta_description.get("content") or "").strip()

        for tag in soup.find_all(_DROP_TAGS):
            tag.decompose()

        heading_counts = {tag: len(soup.find_all(tag)) for tag in _HEADING_TAGS}
        original_section_count = len(soup.find_all(["section", "article"]))

        if sum(heading_counts.values()) == 0 and original_section_count == 0:
            return self._fallback.convert(
                file_stream=io.BytesIO(data),
                stream_info=stream_info,
                **kwargs,
            )

        for heading in soup.find_all(_HEADING_TAGS):
            heading.name = "h3"

        section_number = 1
        for block in soup.find_all(["section", "article"]):
            if block.find(_HEADING_TAGS + ("h3",)) is not None:
                continue
            heading = soup.new_tag("h3")
            heading.string = f"Section {section_number}"
            block.insert(0, heading)
            section_number += 1

        root = soup.find("body") or soup
        markdown = md(str(root), heading_style="ATX")
        markdown = _clean_markdown(markdown)

        lines: list[str] = ["---"]
        lines.append(f"page_title: {_yaml_scalar(page_title)}")
        lines.append(f"description: {_yaml_scalar(description)}")
        lines.append("heading_counts:")
        for tag in _HEADING_TAGS:
            lines.append(f"  {tag}: {heading_counts[tag]}")
        lines.append(f"section_count: {original_section_count}")
        lines.append("---")
        lines.append("")
        if markdown:
            lines.append(markdown)

        return DocumentConverterResult(
            markdown="\n".join(lines).rstrip(),
            title=page_title or None,
        )
