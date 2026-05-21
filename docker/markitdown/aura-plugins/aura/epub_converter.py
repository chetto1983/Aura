"""
Per-chapter EPUB decomposition for Aura's wiki heading-subnode indexer.

Uses the same low-level EPUB shape as MarkItDown's built-in converter:
zipfile + META-INF/container.xml + OPF manifest/spine/TOC parsing. When an EPUB
has no usable TOC, delegates to MarkItDown's EpubConverter.
"""

import io
import posixpath
import re
import zipfile
from typing import Any, BinaryIO
from urllib.parse import unquote, urldefrag
from xml.etree import ElementTree as ET

from bs4 import BeautifulSoup
from markdownify import markdownify as html_to_markdown
from markitdown import DocumentConverter, DocumentConverterResult, StreamInfo
from markitdown.converters import EpubConverter

_EPUB_EXTENSIONS = {".epub"}
_EPUB_MIME_PREFIXES = ("application/epub+zip",)


def _yaml_scalar(s: str) -> str:
    """Always-quoted YAML scalar; avoids parsing ambiguity for arbitrary strings."""
    safe = str(s).replace("\\", "\\\\").replace('"', '\\"').replace("\n", "\\n")
    return f'"{safe}"'


def _local_name(tag: str) -> str:
    if "}" in tag:
        return tag.rsplit("}", 1)[1]
    return tag


def _first_text(root: ET.Element, local_name: str) -> str:
    for elem in root.iter():
        if _local_name(elem.tag) == local_name and elem.text:
            text = elem.text.strip()
            if text:
                return text
    return ""


def _all_text(root: ET.Element, local_name: str) -> list[str]:
    texts: list[str] = []
    for elem in root.iter():
        if _local_name(elem.tag) == local_name and elem.text:
            text = elem.text.strip()
            if text:
                texts.append(text)
    return texts


def _clean_lines(markdown: str) -> str:
    markdown = markdown.replace("\r\n", "\n").replace("\r", "\n")
    markdown = re.sub(r"[ \t]+\n", "\n", markdown)
    markdown = re.sub(r"\n{3,}", "\n\n", markdown)
    return markdown.strip()


def _join_epub_path(base_path: str, href: str) -> str:
    href, _fragment = urldefrag(href)
    href = unquote(href)
    if not href:
        return ""
    if base_path:
        return posixpath.normpath(posixpath.join(base_path, href))
    return posixpath.normpath(href)


class AuraEpubConverter(DocumentConverter):
    """
    Per-chapter EPUB converter. Output shape:
      ---
      book_title: "..."
      author: "..."
      language: "it"
      chapter_count: 2
      chapter_titles:
        - "Chapter 1"
      publisher: "..."
      ---

      ### Chapter 1
      Chapter text...
    """

    def accepts(
        self,
        file_stream: BinaryIO,
        stream_info: StreamInfo,
        **kwargs: Any,
    ) -> bool:
        ext = (getattr(stream_info, "extension", "") or "").lower()
        mime = (getattr(stream_info, "mimetype", "") or "").lower()
        return ext in _EPUB_EXTENSIONS or any(
            mime.startswith(p) for p in _EPUB_MIME_PREFIXES
        )

    def convert(
        self,
        file_stream: BinaryIO,
        stream_info: StreamInfo,
        **kwargs: Any,
    ) -> DocumentConverterResult:
        data = file_stream.read()

        with zipfile.ZipFile(io.BytesIO(data), "r") as zf:
            opf_path = self._find_opf_path(zf)
            opf_root = ET.fromstring(zf.read(opf_path))
            base_path = posixpath.dirname(opf_path)

            manifest = self._manifest(opf_root)
            toc_items = self._toc_items(zf, opf_root, manifest, base_path)
            if not toc_items:
                return EpubConverter().convert(io.BytesIO(data), stream_info, **kwargs)

            metadata = self._metadata(opf_root, stream_info)
            chapters = self._chapters(zf, toc_items)

        lines: list[str] = ["---"]
        lines.append(f"book_title: {_yaml_scalar(metadata['book_title'])}")
        lines.append(f"author: {_yaml_scalar(metadata['author'])}")
        lines.append(f"language: {_yaml_scalar(metadata['language'])}")
        lines.append(f"chapter_count: {len(chapters)}")
        lines.append("chapter_titles:")
        for chapter in chapters:
            lines.append(f"  - {_yaml_scalar(chapter['title'])}")
        lines.append(f"publisher: {_yaml_scalar(metadata['publisher'])}")
        lines.append("---")
        lines.append("")

        for chapter in chapters:
            lines.append(f"### {chapter['title']}")
            if chapter["body"]:
                lines.append("")
                lines.append(chapter["body"])
            lines.append("")

        return DocumentConverterResult(
            markdown="\n".join(lines).rstrip(), title=metadata["book_title"]
        )

    def _find_opf_path(self, zf: zipfile.ZipFile) -> str:
        container = ET.fromstring(zf.read("META-INF/container.xml"))
        for elem in container.iter():
            if _local_name(elem.tag) == "rootfile":
                path = elem.attrib.get("full-path", "").strip()
                if path:
                    return path
        raise ValueError("EPUB container.xml does not contain a rootfile")

    def _metadata(self, opf_root: ET.Element, stream_info: StreamInfo) -> dict[str, str]:
        title = _first_text(opf_root, "title")
        authors = _all_text(opf_root, "creator")
        filename = (getattr(stream_info, "filename", "") or "").strip()
        return {
            "book_title": title or filename or "unknown",
            "author": ", ".join(authors) if authors else "unknown",
            "language": _first_text(opf_root, "language") or "unknown",
            "publisher": _first_text(opf_root, "publisher") or "unknown",
        }

    def _manifest(self, opf_root: ET.Element) -> dict[str, dict[str, str]]:
        manifest: dict[str, dict[str, str]] = {}
        for elem in opf_root.iter():
            if _local_name(elem.tag) != "item":
                continue
            item_id = elem.attrib.get("id", "").strip()
            href = elem.attrib.get("href", "").strip()
            if not item_id or not href:
                continue
            manifest[item_id] = {
                "href": href,
                "media_type": elem.attrib.get("media-type", "").strip(),
                "properties": elem.attrib.get("properties", "").strip(),
            }
        return manifest

    def _toc_items(
        self,
        zf: zipfile.ZipFile,
        opf_root: ET.Element,
        manifest: dict[str, dict[str, str]],
        base_path: str,
    ) -> list[dict[str, str]]:
        items = self._toc_items_from_ncx(zf, opf_root, manifest, base_path)
        if items:
            return items
        return self._toc_items_from_nav(zf, manifest, base_path)

    def _toc_items_from_ncx(
        self,
        zf: zipfile.ZipFile,
        opf_root: ET.Element,
        manifest: dict[str, dict[str, str]],
        base_path: str,
    ) -> list[dict[str, str]]:
        toc_id = ""
        for elem in opf_root.iter():
            if _local_name(elem.tag) == "spine":
                toc_id = elem.attrib.get("toc", "").strip()
                break

        ncx_href = ""
        if toc_id and toc_id in manifest:
            ncx_href = manifest[toc_id]["href"]
        if not ncx_href:
            for item in manifest.values():
                if item["media_type"] == "application/x-dtbncx+xml":
                    ncx_href = item["href"]
                    break
        if not ncx_href:
            return []

        ncx_path = _join_epub_path(base_path, ncx_href)
        if ncx_path not in zf.namelist():
            return []

        root = ET.fromstring(zf.read(ncx_path))
        items: list[dict[str, str]] = []
        for nav_point in root.iter():
            if _local_name(nav_point.tag) != "navPoint":
                continue
            title = ""
            href = ""
            for child in nav_point.iter():
                child_name = _local_name(child.tag)
                if child_name == "text" and child.text and not title:
                    title = child.text.strip()
                elif child_name == "content" and not href:
                    href = child.attrib.get("src", "").strip()
            if title and href:
                items.append(
                    {"title": title, "path": _join_epub_path(base_path, href)}
                )
        return self._dedupe_toc(items)

    def _toc_items_from_nav(
        self,
        zf: zipfile.ZipFile,
        manifest: dict[str, dict[str, str]],
        base_path: str,
    ) -> list[dict[str, str]]:
        nav_href = ""
        for item in manifest.values():
            props = set(item["properties"].split())
            if "nav" in props:
                nav_href = item["href"]
                break
        if not nav_href:
            return []

        nav_path = _join_epub_path(base_path, nav_href)
        if nav_path not in zf.namelist():
            return []

        soup = BeautifulSoup(zf.read(nav_path), "html.parser")
        nav = soup.find("nav", attrs={"epub:type": "toc"}) or soup.find("nav")
        if nav is None:
            return []

        items: list[dict[str, str]] = []
        for link in nav.find_all("a"):
            title = link.get_text(" ", strip=True)
            href = (link.get("href") or "").strip()
            if title and href:
                items.append(
                    {"title": title, "path": _join_epub_path(base_path, href)}
                )
        return self._dedupe_toc(items)

    def _dedupe_toc(self, items: list[dict[str, str]]) -> list[dict[str, str]]:
        seen: set[tuple[str, str]] = set()
        deduped: list[dict[str, str]] = []
        for item in items:
            key = (item["title"], item["path"])
            if key in seen:
                continue
            seen.add(key)
            deduped.append(item)
        return deduped

    def _chapters(
        self, zf: zipfile.ZipFile, toc_items: list[dict[str, str]]
    ) -> list[dict[str, str]]:
        chapters: list[dict[str, str]] = []
        names = set(zf.namelist())
        for item in toc_items:
            body = ""
            if item["path"] in names:
                body = self._html_to_markdown(zf.read(item["path"]))
            chapters.append({"title": item["title"], "body": body})
        return chapters

    def _html_to_markdown(self, html: bytes) -> str:
        text = html.decode("utf-8", errors="replace")
        soup = BeautifulSoup(text, "html.parser")
        for tag in soup(["script", "style", "head", "nav"]):
            tag.decompose()
        root = soup.body or soup
        markdown = html_to_markdown(str(root), heading_style="ATX")
        return _clean_lines(markdown)
