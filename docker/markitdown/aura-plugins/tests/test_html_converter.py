"""
Unit tests for AuraHtmlConverter.

Run with:
  python -m pytest docker/markitdown/aura-plugins/tests/test_html_converter.py -q
"""

import io
import os
import sys

sys.path.insert(0, os.path.join(os.path.dirname(__file__), ".."))

from markitdown import StreamInfo

from aura.html_converter import AuraHtmlConverter


def _stream_info(
    filename: str = "page.html",
    extension: str = ".html",
    mimetype: str = "text/html",
) -> StreamInfo:
    return StreamInfo(extension=extension, mimetype=mimetype, filename=filename)


def _convert(html: str, stream_info: StreamInfo | None = None) -> str:
    converter = AuraHtmlConverter()
    result = converter.convert(
        io.BytesIO(html.encode("utf-8")),
        stream_info or _stream_info(),
    )
    return result.markdown


def test_accepts_html_extension():
    converter = AuraHtmlConverter()
    assert converter.accepts(io.BytesIO(b""), _stream_info(extension=".html"))
    assert converter.accepts(io.BytesIO(b""), _stream_info(extension=".htm"))


def test_accepts_text_html_mime():
    converter = AuraHtmlConverter()
    assert converter.accepts(io.BytesIO(b""), StreamInfo(mimetype="text/html"))


def test_rejects_plain_text():
    converter = AuraHtmlConverter()
    assert not converter.accepts(
        io.BytesIO(b""),
        StreamInfo(extension=".txt", mimetype="text/plain"),
    )


def test_strips_navigation_and_unsafe_blocks():
    md = _convert(
        """
        <html>
          <head><title>Doc</title><style>.x{color:red}</style></head>
          <body>
            <nav>Menu</nav>
            <aside>Ad</aside>
            <h1>Main</h1>
            <p>Keep me.</p>
            <footer>Copyright</footer>
            <script>alert(1)</script>
          </body>
        </html>
        """
    )

    assert "Keep me." in md
    assert "### Main" in md
    assert "Menu" not in md
    assert "Ad" not in md
    assert "Copyright" not in md
    assert "alert" not in md


def test_collapses_all_heading_levels_to_h3_and_counts_original_levels():
    md = _convert(
        """
        <html><head><title>Headings</title></head><body>
          <h1>Top</h1>
          <h2>Middle</h2>
          <h6>Deep</h6>
        </body></html>
        """
    )

    assert "page_title: \"Headings\"" in md
    assert "  h1: 1" in md
    assert "  h2: 1" in md
    assert "  h6: 1" in md
    assert "### Top" in md
    assert "### Middle" in md
    assert "### Deep" in md
    lines = set(md.splitlines())
    assert "# Top" not in lines
    assert "## Middle" not in lines
    assert "###### Deep" not in lines


def test_unheaded_section_and_article_get_section_headings():
    md = _convert(
        """
        <html><body>
          <section><p>First body.</p></section>
          <article><p>Second body.</p></article>
        </body></html>
        """
    )

    assert "section_count: 2" in md
    assert "### Section 1" in md
    assert "First body." in md
    assert "### Section 2" in md
    assert "Second body." in md


def test_section_with_existing_heading_does_not_get_synthetic_heading():
    md = _convert(
        """
        <html><body>
          <section>
            <h2>Existing</h2>
            <p>Body.</p>
          </section>
        </body></html>
        """
    )

    assert "### Existing" in md
    assert "### Section 1" not in md


def test_renders_common_blocks_to_markdown():
    md = _convert(
        """
        <html><body>
          <h1>Blocks</h1>
          <p>Paragraph text.</p>
          <ul><li>One</li><li>Two</li></ul>
          <ol><li>First</li><li>Second</li></ol>
          <blockquote>Quoted text.</blockquote>
          <pre>alpha = 1</pre>
          <table>
            <tr><th>Name</th><th>Value</th></tr>
            <tr><td>A</td><td>42</td></tr>
          </table>
        </body></html>
        """
    )

    assert "Paragraph text." in md
    assert "* One" in md
    assert "* Two" in md
    assert "1. First" in md
    assert "2. Second" in md
    assert "> Quoted text." in md
    assert "alpha = 1" in md
    assert "| Name | Value |" in md
    assert "| A | 42 |" in md


def test_frontmatter_description_and_counts():
    md = _convert(
        """
        <html>
          <head>
            <title>Meta Title</title>
            <meta name="description" content="Short summary">
          </head>
          <body><h3>Only heading</h3><p>Body.</p></body>
        </html>
        """
    )

    assert md.startswith("---")
    assert "page_title: \"Meta Title\"" in md
    assert "description: \"Short summary\"" in md
    assert "heading_counts:" in md
    assert "section_count: 0" in md


def test_zero_headings_and_zero_sections_falls_back_to_builtin_html_converter():
    md = _convert(
        """
        <html>
          <head><title>Plain</title></head>
          <body><p>Plain paragraph.</p></body>
        </html>
        """
    )

    assert not md.startswith("---")
    assert "Plain paragraph." in md
    assert "page_title:" not in md
