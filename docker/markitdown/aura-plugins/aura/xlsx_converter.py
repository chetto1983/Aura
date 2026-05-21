"""
Per-row XLSX decomposition for Aura's wiki heading-subnode indexer.

Replaces the built-in flat-table renderer so each data row becomes a
### Row N heading that the Wave-A B04 subnode parser can index as a
separate retrieval node.

Cell value normalisation:
  None         -> '(empty)'
  datetime     -> ISO 8601
  everything else -> str(), no precision loss for int/float
"""

import io
import warnings
from datetime import datetime
from typing import Any, BinaryIO

import openpyxl
from markitdown import DocumentConverter, DocumentConverterResult, StreamInfo

_XLSX_EXTENSIONS = {".xlsx"}
_XLSX_MIME_PREFIXES = (
    "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
)


def _cell_str(v) -> str:
    if v is None:
        return "(empty)"
    if isinstance(v, datetime):
        return v.isoformat()
    return str(v)


def _yaml_scalar(s: str) -> str:
    """Always-quoted YAML scalar; avoids parsing ambiguity for arbitrary strings."""
    safe = str(s).replace("\\", "\\\\").replace('"', '\\"').replace("\n", "\\n")
    return f'"{safe}"'


class AuraXlsxConverter(DocumentConverter):
    """
    Per-row XLSX converter.  Output shape:
      ---
      workbook_title: "..."
      sheets:
        - "Sheet1"
      sheet_row_counts:
        "Sheet1": 42
      author: "..."
      ---

      ## Sheet: Sheet1
      **Columns**: Name, Age, City
      ### Row 1
      - Name: Alice
      - Age: 30
      - City: (empty)
      ### Row 2
      ...
    """

    def accepts(
        self,
        file_stream: BinaryIO,
        stream_info: StreamInfo,
        **kwargs: Any,
    ) -> bool:
        ext = (getattr(stream_info, "extension", "") or "").lower()
        mime = (getattr(stream_info, "mimetype", "") or "").lower()
        return ext in _XLSX_EXTENSIONS or any(
            mime.startswith(p) for p in _XLSX_MIME_PREFIXES
        )

    def convert(
        self,
        file_stream: BinaryIO,
        stream_info: StreamInfo,
        **kwargs: Any,
    ) -> DocumentConverterResult:
        data = file_stream.read()

        with warnings.catch_warnings():
            warnings.simplefilter("ignore")
            wb = openpyxl.load_workbook(io.BytesIO(data), data_only=True)

        # Collect author from core properties
        author = "unknown"
        try:
            creator = (wb.properties.creator or "").strip()
            if creator:
                author = creator
        except Exception:
            pass

        sheet_names = wb.sheetnames

        # One pass per sheet: collect all rows as plain values
        sheet_data: dict = {}
        for sname in sheet_names:
            ws = wb[sname]
            sheet_data[sname] = list(ws.iter_rows(values_only=True))

        wb.close()

        # Compute data-row counts (all rows minus the header row)
        sheet_row_counts: dict = {
            sname: max(0, len(rows) - 1) for sname, rows in sheet_data.items()
        }

        # Derive filename for workbook_title
        filename = (getattr(stream_info, "filename", None) or "").strip()
        title = filename or "workbook"

        # Build YAML frontmatter
        lines: list = ["---"]
        lines.append(f"workbook_title: {_yaml_scalar(title)}")
        lines.append("sheets:")
        for s in sheet_names:
            lines.append(f"  - {_yaml_scalar(s)}")
        lines.append("sheet_row_counts:")
        for s, cnt in sheet_row_counts.items():
            lines.append(f"  {_yaml_scalar(s)}: {cnt}")
        lines.append(f"author: {_yaml_scalar(author)}")
        lines.append("---")
        lines.append("")

        # Emit per-sheet sections with ### Row headings
        for sname, rows in sheet_data.items():
            lines.append(f"## Sheet: {sname}")
            lines.append("")

            if not rows:
                continue

            header = rows[0]
            col_names = [_cell_str(h) for h in header]
            lines.append(f'**Columns**: {", ".join(col_names)}')
            lines.append("")

            for row_idx, row in enumerate(rows[1:], start=1):
                lines.append(f"### Row {row_idx}")
                for ci, col_name in enumerate(col_names):
                    cell_val = row[ci] if ci < len(row) else None
                    lines.append(f"- {col_name}: {_cell_str(cell_val)}")
                lines.append("")

        return DocumentConverterResult(markdown="\n".join(lines).rstrip())
