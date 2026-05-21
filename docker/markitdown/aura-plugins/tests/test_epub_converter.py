"""
Unit tests for AuraEpubConverter.

Run with:  python -m pytest docker/markitdown/aura-plugins/tests/test_epub_converter.py -q
"""

import io
import os
import sys
import zipfile

# Allow running from repo root without installing the package
sys.path.insert(0, os.path.join(os.path.dirname(__file__), ".."))

from markitdown import DocumentConverterResult, StreamInfo

from aura import epub_converter
from aura.epub_converter import AuraEpubConverter


def _stream_info(filename: str = "book.epub") -> StreamInfo:
    return StreamInfo(
        extension=".epub",
        mimetype="application/epub+zip",
        filename=filename,
    )


def _write_epub(entries: dict[str, str | bytes]) -> bytes:
    buf = io.BytesIO()
    with zipfile.ZipFile(buf, "w") as zf:
        zf.writestr("mimetype", "application/epub+zip")
        for path, content in entries.items():
            zf.writestr(path, content)
    return buf.getvalue()


def _container() -> str:
    return """<?xml version="1.0"?>
<container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container">
  <rootfiles>
    <rootfile full-path="OEBPS/content.opf" media-type="application/oebps-package+xml"/>
  </rootfiles>
</container>
"""


def _chapter(title: str, body: str) -> str:
    return f"""<?xml version="1.0" encoding="utf-8"?>
<html xmlns="http://www.w3.org/1999/xhtml">
  <head><title>{title}</title><style>.x {{ color: red; }}</style></head>
  <body>
    <h1>{title}</h1>
    <p>{body}</p>
    <script>ignored()</script>
  </body>
</html>
"""


def _epub2_with_ncx() -> bytes:
    opf = """<?xml version="1.0" encoding="utf-8"?>
<package xmlns="http://www.idpf.org/2007/opf" unique-identifier="bookid" version="2.0">
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/">
    <dc:title>Sample Book</dc:title>
    <dc:creator>Ada Author</dc:creator>
    <dc:language>en</dc:language>
    <dc:publisher>Aura Press</dc:publisher>
  </metadata>
  <manifest>
    <item id="ncx" href="toc.ncx" media-type="application/x-dtbncx+xml"/>
    <item id="c1" href="chapter1.xhtml" media-type="application/xhtml+xml"/>
    <item id="c2" href="chapter2.xhtml" media-type="application/xhtml+xml"/>
  </manifest>
  <spine toc="ncx">
    <itemref idref="c1"/>
    <itemref idref="c2"/>
  </spine>
</package>
"""
    ncx = """<?xml version="1.0" encoding="utf-8"?>
<ncx xmlns="http://www.daisy.org/z3986/2005/ncx/">
  <navMap>
    <navPoint id="n1" playOrder="1">
      <navLabel><text>Opening Chapter</text></navLabel>
      <content src="chapter1.xhtml"/>
    </navPoint>
    <navPoint id="n2" playOrder="2">
      <navLabel><text>Second Chapter</text></navLabel>
      <content src="chapter2.xhtml#anchor"/>
    </navPoint>
  </navMap>
</ncx>
"""
    return _write_epub(
        {
            "META-INF/container.xml": _container(),
            "OEBPS/content.opf": opf,
            "OEBPS/toc.ncx": ncx,
            "OEBPS/chapter1.xhtml": _chapter(
                "Opening Chapter", "Citt\u00e0 and truth body text."
            ),
            "OEBPS/chapter2.xhtml": _chapter("Second Chapter", "Second body text."),
        }
    )


def _epub3_with_nav() -> bytes:
    opf = """<?xml version="1.0" encoding="utf-8"?>
<package xmlns="http://www.idpf.org/2007/opf" unique-identifier="bookid" version="3.0">
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/">
    <dc:title>Nav Book</dc:title>
  </metadata>
  <manifest>
    <item id="nav" href="nav.xhtml" media-type="application/xhtml+xml" properties="nav"/>
    <item id="c1" href="text/one.xhtml" media-type="application/xhtml+xml"/>
  </manifest>
  <spine>
    <itemref idref="c1"/>
  </spine>
</package>
"""
    nav = """<?xml version="1.0" encoding="utf-8"?>
<html xmlns="http://www.w3.org/1999/xhtml">
  <body>
    <nav epub:type="toc">
      <ol><li><a href="text/one.xhtml">Nav Chapter</a></li></ol>
    </nav>
  </body>
</html>
"""
    return _write_epub(
        {
            "META-INF/container.xml": _container(),
            "OEBPS/content.opf": opf,
            "OEBPS/nav.xhtml": nav,
            "OEBPS/text/one.xhtml": _chapter("Nav Chapter", "Navigation body."),
        }
    )


def _epub_without_toc() -> bytes:
    opf = """<?xml version="1.0" encoding="utf-8"?>
<package xmlns="http://www.idpf.org/2007/opf" unique-identifier="bookid" version="2.0">
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/">
    <dc:title>No Toc Book</dc:title>
  </metadata>
  <manifest>
    <item id="c1" href="chapter1.xhtml" media-type="application/xhtml+xml"/>
  </manifest>
  <spine>
    <itemref idref="c1"/>
  </spine>
</package>
"""
    return _write_epub(
        {
            "META-INF/container.xml": _container(),
            "OEBPS/content.opf": opf,
            "OEBPS/chapter1.xhtml": _chapter("No Toc", "Built in fallback body."),
        }
    )


def test_accepts_epub_extension():
    c = AuraEpubConverter()
    assert c.accepts(io.BytesIO(b""), StreamInfo(extension=".epub"))


def test_accepts_epub_mime():
    c = AuraEpubConverter()
    assert c.accepts(io.BytesIO(b""), StreamInfo(mimetype="application/epub+zip"))


def test_rejects_pdf():
    c = AuraEpubConverter()
    assert not c.accepts(
        io.BytesIO(b""), StreamInfo(extension=".pdf", mimetype="application/pdf")
    )


def test_convert_ncx_toc_emits_frontmatter_and_chapters():
    c = AuraEpubConverter()
    result = c.convert(io.BytesIO(_epub2_with_ncx()), _stream_info("sample.epub"))
    md = result.markdown

    assert md.startswith("---")
    assert 'book_title: "Sample Book"' in md
    assert 'author: "Ada Author"' in md
    assert 'language: "en"' in md
    assert 'publisher: "Aura Press"' in md
    assert "chapter_count: 2" in md
    assert '  - "Opening Chapter"' in md
    assert '  - "Second Chapter"' in md
    assert "### Opening Chapter" in md
    assert "### Second Chapter" in md
    assert "Citt\u00e0 and truth body text." in md
    assert "Second body text." in md
    assert "ignored()" not in md


def test_convert_nav_toc_emits_chapter():
    c = AuraEpubConverter()
    result = c.convert(io.BytesIO(_epub3_with_nav()), _stream_info("nav.epub"))
    md = result.markdown

    assert 'book_title: "Nav Book"' in md
    assert "chapter_count: 1" in md
    assert "### Nav Chapter" in md
    assert "Navigation body." in md


def test_empty_toc_falls_back_to_builtin(monkeypatch):
    calls = []

    class FakeEpubConverter:
        def convert(self, file_stream, stream_info, **kwargs):
            calls.append((file_stream.read(), stream_info.filename, kwargs))
            return DocumentConverterResult(markdown="fallback markdown", title="fallback")

    monkeypatch.setattr(epub_converter, "EpubConverter", FakeEpubConverter)

    c = AuraEpubConverter()
    result = c.convert(
        io.BytesIO(_epub_without_toc()), _stream_info("no-toc.epub"), keep=True
    )

    assert result.markdown == "fallback markdown"
    assert len(calls) == 1
    assert calls[0][1] == "no-toc.epub"
    assert calls[0][2] == {"keep": True}
