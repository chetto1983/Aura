package source

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"

	"github.com/aura/aura/internal/sandbox"
)

type pyodideExtractionOut struct {
	Markdown string         `json:"markdown"`
	Metadata ExtractionMeta `json:"metadata"`
}

func ExtractWithPyodide(ctx context.Context, runner interface {
	Execute(context.Context, string, bool) (*sandbox.Result, error)
}, in ExtractInput) (ExtractResult, error) {
	if in.Source == nil {
		return ExtractResult{}, fmt.Errorf("source: nil source")
	}
	var code string
	switch in.Source.Kind {
	case KindXLSX:
		code = buildXLSXExtractorCode(in.Bytes)
	default:
		return ExtractResult{}, fmt.Errorf("source: no Pyodide extractor for kind %s", in.Source.Kind)
	}
	res, err := runner.Execute(ctx, code, false)
	if err != nil {
		return ExtractResult{}, err
	}
	if !res.OK {
		return ExtractResult{}, fmt.Errorf("source: pyodide extraction failed: %s", res.Stderr)
	}
	var out pyodideExtractionOut
	if err := json.Unmarshal([]byte(res.Stdout), &out); err != nil {
		return ExtractResult{}, fmt.Errorf("source: parse pyodide extraction json: %w", err)
	}
	if out.Markdown == "" {
		return ExtractResult{}, fmt.Errorf("source: pyodide extraction returned empty markdown")
	}
	return ExtractResult{Markdown: out.Markdown, Metadata: out.Metadata}, nil
}

func buildXLSXExtractorCode(body []byte) string {
	encoded := base64.StdEncoding.EncodeToString(body)
	return fmt.Sprintf(`
import base64, io, json
import pandas as pd

raw = base64.b64decode(%q)
book = pd.ExcelFile(io.BytesIO(raw), engine="calamine")
sections = []
rows_total = 0
warnings = []

def clean_cell(value):
    text = "" if value is None else str(value)
    return text.replace("|", "\\|").replace("\n", " ").strip()

def markdown_table(df):
    df = df.fillna("")
    headers = [clean_cell(c) for c in list(df.columns)]
    lines = ["| " + " | ".join(headers) + " |"]
    lines.append("| " + " | ".join(["---"] * len(headers)) + " |")
    for _, row in df.head(200).iterrows():
        lines.append("| " + " | ".join(clean_cell(row[c]) for c in df.columns) + " |")
    return "\n".join(lines)

for sheet in book.sheet_names[:10]:
    df = book.parse(sheet)
    rows_total += int(len(df.index))
    sections.append("## Sheet: " + str(sheet))
    sections.append(markdown_table(df))

if len(book.sheet_names) > 10:
    warnings.append("workbook truncated to first 10 sheets")
if rows_total > 200:
    warnings.append("large workbook rendered with per-sheet row limits")

markdown = "\n\n".join(sections).strip() + "\n"
print(json.dumps({
    "markdown": markdown,
    "metadata": {
        "extractor_name": "pyodide_xlsx",
        "extractor_version": "pyodide_xlsx_v1",
        "text_bytes": len(markdown.encode("utf-8")),
        "sheet_count": len(book.sheet_names),
        "row_count": rows_total,
        "warnings": warnings
    }
}))
`, encoded)
}
