"""
Unit tests for AuraXlsxConverter.

Run with:  python -m pytest docker/markitdown/aura-plugins/tests/
"""

import io
import sys
import os

# Allow running from repo root without installing the package
sys.path.insert(0, os.path.join(os.path.dirname(__file__), ".."))

import openpyxl
import pytest
from markitdown import StreamInfo

from aura.xlsx_converter import AuraXlsxConverter


def _make_xlsx(sheets: dict) -> bytes:
    """
    Build an in-memory xlsx.

    sheets = {"SheetName": [header_row, data_row, ...]}
    """
    wb = openpyxl.Workbook()
    first = True
    for sname, rows in sheets.items():
        if first:
            ws = wb.active
            ws.title = sname
            first = False
        else:
            ws = wb.create_sheet(sname)
        for row in rows:
            ws.append(row)
    buf = io.BytesIO()
    wb.save(buf)
    return buf.getvalue()


def _stream_info(filename: str = "test.xlsx") -> StreamInfo:
    return StreamInfo(
        extension=".xlsx",
        mimetype="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
        filename=filename,
    )


# ---------------------------------------------------------------------------
# accepts() tests
# ---------------------------------------------------------------------------


def test_accepts_xlsx_extension():
    c = AuraXlsxConverter()
    assert c.accepts(io.BytesIO(b""), _stream_info())


def test_accepts_xlsx_mime():
    c = AuraXlsxConverter()
    si = StreamInfo(
        mimetype="application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
    )
    assert c.accepts(io.BytesIO(b""), si)


def test_rejects_docx():
    c = AuraXlsxConverter()
    si = StreamInfo(
        extension=".docx",
        mimetype="application/vnd.openxmlformats-officedocument.wordprocessingml.document",
    )
    assert not c.accepts(io.BytesIO(b""), si)


# ---------------------------------------------------------------------------
# convert() shape tests
# ---------------------------------------------------------------------------


def test_convert_basic_structure():
    data = _make_xlsx(
        {
            "Products": [
                ["Name", "Price", "Category"],
                ["Mozzarella", 2.50, "Dairy"],
                ["Prosciutto", 8.99, "Meat"],
            ]
        }
    )
    c = AuraXlsxConverter()
    result = c.convert(io.BytesIO(data), _stream_info("products.xlsx"))
    md = result.markdown

    # YAML frontmatter present
    assert md.startswith("---"), f"expected frontmatter, got: {md[:60]}"
    assert "sheet_row_counts:" in md
    assert "workbook_title:" in md

    # Sheet heading
    assert "## Sheet: Products" in md

    # Column declaration
    assert "**Columns**: Name, Price, Category" in md

    # Row headings
    assert "### Row 1" in md
    assert "### Row 2" in md

    # Cell bullets
    assert "- Name: Mozzarella" in md
    assert "- Name: Prosciutto" in md
    assert "- Category: Dairy" in md


def test_convert_empty_cell_renders_as_empty():
    data = _make_xlsx(
        {
            "Sheet1": [
                ["Name", "Note"],
                ["Alice", None],
            ]
        }
    )
    c = AuraXlsxConverter()
    result = c.convert(io.BytesIO(data), _stream_info())
    md = result.markdown
    assert "- Note: (empty)" in md, f"expected (empty) for None cell, got:\n{md}"


def test_convert_int_cell_no_precision_loss():
    data = _make_xlsx({"Sheet1": [["ID"], [42]]})
    c = AuraXlsxConverter()
    result = c.convert(io.BytesIO(data), _stream_info())
    assert "- ID: 42" in result.markdown


def test_convert_row_count_in_frontmatter():
    data = _make_xlsx(
        {
            "Data": [
                ["Col1", "Col2"],
                ["a", "b"],
                ["c", "d"],
                ["e", "f"],
            ]
        }
    )
    c = AuraXlsxConverter()
    result = c.convert(io.BytesIO(data), _stream_info("data.xlsx"))
    md = result.markdown
    # 3 data rows (excluding header)
    assert '"Data": 3' in md or "Data: 3" in md, f"row count not in frontmatter:\n{md}"
    assert "### Row 3" in md


def test_convert_multi_sheet():
    data = _make_xlsx(
        {
            "Alpha": [["X"], [1], [2]],
            "Beta": [["Y"], [10]],
        }
    )
    c = AuraXlsxConverter()
    result = c.convert(io.BytesIO(data), _stream_info("multi.xlsx"))
    md = result.markdown
    assert "## Sheet: Alpha" in md
    assert "## Sheet: Beta" in md
    assert "### Row 2" in md  # Alpha has 2 data rows


def test_convert_empty_sheet_no_crash():
    data = _make_xlsx({"Empty": []})
    c = AuraXlsxConverter()
    result = c.convert(io.BytesIO(data), _stream_info())
    # Should not raise; just produce frontmatter with count=0
    assert "sheet_row_counts:" in result.markdown


# ---------------------------------------------------------------------------
# Integration: use bench fixture if present
# ---------------------------------------------------------------------------


def test_convert_bench_fixture_if_present():
    fixture = os.path.join(
        os.path.dirname(__file__),
        "../../../../docs/quality-bench/fixtures/tika-testEXCEL.xlsx",
    )
    if not os.path.exists(fixture):
        pytest.skip("bench fixture not present")

    with open(fixture, "rb") as f:
        data = f.read()

    c = AuraXlsxConverter()
    result = c.convert(
        io.BytesIO(data),
        StreamInfo(extension=".xlsx", filename="tika-testEXCEL.xlsx"),
    )
    md = result.markdown
    assert "### Row" in md, "no row headings in bench fixture output"
    assert "sheet_row_counts:" in md
