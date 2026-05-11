package sandbox

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/aura/aura/internal/source"
)

const (
	maxArtifacts                  = 10
	maxArtifactBytes              = 5 << 20
	maxXLSXEntries                = 512
	maxXLSXTotalUncompressedBytes = 20 << 20
	maxXLSXXMLPartBytes           = 5 << 20
	maxXLSXCompressionRatio       = 100
)

const trustedXLSXExtractorCode = `
import json
from pathlib import Path
import pandas as pd

MAX_XLSX_SHEETS = 10
MAX_XLSX_ROWS_PER_SHEET = 200

input_path = Path("workbook.xlsx")
output_dir = Path("/tmp/aura_out")
output_dir.mkdir(parents=True, exist_ok=True)

book = pd.ExcelFile(input_path, engine="calamine")
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
    for _, row in df.head(MAX_XLSX_ROWS_PER_SHEET).iterrows():
        lines.append("| " + " | ".join(clean_cell(row[c]) for c in df.columns) + " |")
    return "\n".join(lines)

for sheet in book.sheet_names[:MAX_XLSX_SHEETS]:
    df = book.parse(sheet, nrows=MAX_XLSX_ROWS_PER_SHEET)
    rows_total += int(len(df.index))
    sections.append("## Sheet: " + str(sheet))
    sections.append(markdown_table(df))

if len(book.sheet_names) > MAX_XLSX_SHEETS:
    warnings.append("workbook truncated to first 10 sheets")
if rows_total >= MAX_XLSX_ROWS_PER_SHEET:
    warnings.append("large workbook rendered with per-sheet row limits")

markdown = "\n\n".join(sections).strip() + "\n"
(output_dir / "extract.md").write_text(markdown, encoding="utf-8")
(output_dir / "extract.json").write_text(json.dumps({
    "extractor_name": "process_xlsx",
    "extractor_version": "process_xlsx_v1",
    "text_bytes": len(markdown.encode("utf-8")),
    "sheet_count": len(book.sheet_names),
    "row_count": rows_total,
    "warnings": warnings
}), encoding="utf-8")
print("xlsx extraction complete")
`

const trustedDOCXExtractorCode = `
import json
from pathlib import Path
import re
import xml.etree.ElementTree as ET
import zipfile

input_path = Path("document.docx")
output_dir = Path("/tmp/aura_out")
output_dir.mkdir(parents=True, exist_ok=True)
warnings = []

MAX_DOCX_ENTRIES = 512
MAX_DOCX_TOTAL_UNCOMPRESSED_BYTES = 20 * 1024 * 1024
MAX_DOCX_XML_PART_BYTES = 2 * 1024 * 1024
MAX_DOCX_COMPRESSION_RATIO = 100
MAX_DOCX_PARAGRAPHS = 5000
MAX_DOCX_TEXT_BYTES = 512 * 1024

WORD_NS = "http://schemas.openxmlformats.org/wordprocessingml/2006/main"
TEXT_TAG = "{" + WORD_NS + "}t"
PARA_TAG = "{" + WORD_NS + "}p"
TAB_TAG = "{" + WORD_NS + "}tab"
BR_TAG = "{" + WORD_NS + "}br"
CR_TAG = "{" + WORD_NS + "}cr"

def clean_text(text):
    text = text.replace("\r\n", "\n").replace("\r", "\n")
    lines = [re.sub(r"[ \t]+", " ", line).strip() for line in text.split("\n")]
    return "\n".join(line for line in lines if line).strip()

def paragraph_text(paragraph):
    parts = []
    for elem in paragraph.iter():
        if elem.tag == TEXT_TAG and elem.text:
            parts.append(elem.text)
        elif elem.tag == TAB_TAG:
            parts.append("\t")
        elif elem.tag in (BR_TAG, CR_TAG):
            parts.append("\n")
    return clean_text("".join(parts))

def part_paragraphs(zf, name):
    try:
        info = zf.getinfo(name)
    except KeyError:
        return []
    if info.file_size > MAX_DOCX_XML_PART_BYTES:
        warnings.append(f"skipped oversized XML part {name}")
        return []
    if info.compress_size > 0 and info.file_size > info.compress_size * MAX_DOCX_COMPRESSION_RATIO:
        warnings.append(f"skipped suspiciously compressed XML part {name}")
        return []
    raw = zf.read(info)
    try:
        root = ET.fromstring(raw)
    except ET.ParseError as exc:
        warnings.append(f"skipped malformed XML part {name}: {exc}")
        return []
    paragraphs = []
    for paragraph in root.iter(PARA_TAG):
        if len(paragraphs) >= MAX_DOCX_PARAGRAPHS:
            warnings.append(f"truncated paragraph extraction in {name}")
            break
        text = paragraph_text(paragraph)
        if text:
            paragraphs.append(text)
    return paragraphs

sections = []
extracted_text_bytes = 0
extracted_paragraphs = 0
truncated = False

def append_block(text, counts_as_paragraph=True):
    global extracted_text_bytes, extracted_paragraphs, truncated
    if counts_as_paragraph and extracted_paragraphs >= MAX_DOCX_PARAGRAPHS:
        if not truncated:
            warnings.append("document truncated at paragraph limit")
        truncated = True
        return False
    projected = extracted_text_bytes + len((text + "\n\n").encode("utf-8"))
    if projected > MAX_DOCX_TEXT_BYTES:
        if not truncated:
            warnings.append("document truncated at text byte limit")
        truncated = True
        return False
    sections.append(text)
    extracted_text_bytes = projected
    if counts_as_paragraph:
        extracted_paragraphs += 1
    return True

def append_part(label, paragraphs):
    if not paragraphs:
        return
    if label and not append_block(label, False):
        return
    for text in paragraphs:
        if not append_block(text):
            return

with zipfile.ZipFile(input_path) as zf:
    infos = zf.infolist()
    if len(infos) > MAX_DOCX_ENTRIES:
        raise RuntimeError("DOCX has too many ZIP entries")
    total_uncompressed = sum(info.file_size for info in infos)
    if total_uncompressed > MAX_DOCX_TOTAL_UNCOMPRESSED_BYTES:
        raise RuntimeError("DOCX uncompressed size exceeds limit")
    names = set(zf.namelist())
    if "word/document.xml" not in names:
        raise RuntimeError("DOCX missing word/document.xml")

    header_parts = sorted(name for name in names if re.match(r"word/header\d+\.xml$", name))
    footer_parts = sorted(name for name in names if re.match(r"word/footer\d+\.xml$", name))

    for name in header_parts:
        append_part("## Header", part_paragraphs(zf, name))

    append_part("", part_paragraphs(zf, "word/document.xml"))

    for name in footer_parts:
        append_part("## Footer", part_paragraphs(zf, name))

markdown = "\n\n".join(sections).strip()
if not markdown:
    warnings.append("document contained no extractable text")
markdown += "\n"

(output_dir / "extract.md").write_text(markdown, encoding="utf-8")
(output_dir / "extract.json").write_text(json.dumps({
    "extractor_name": "process_docx",
    "extractor_version": "process_docx_v1",
    "text_bytes": len(markdown.encode("utf-8")),
    "page_count": 0,
    "warnings": warnings
}), encoding="utf-8")
print("docx extraction complete")
`

func validateXLSXArchive(body []byte) error {
	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		return fmt.Errorf("source: invalid XLSX archive: %w", err)
	}
	if len(zr.File) > maxXLSXEntries {
		return errors.New("source: XLSX has too many ZIP entries")
	}
	var total uint64
	for _, f := range zr.File {
		total += f.UncompressedSize64
		if total > maxXLSXTotalUncompressedBytes {
			return errors.New("source: XLSX uncompressed size exceeds limit")
		}
		if strings.HasSuffix(strings.ToLower(f.Name), ".xml") && f.UncompressedSize64 > maxXLSXXMLPartBytes {
			return fmt.Errorf("source: XLSX XML part %s exceeds limit", f.Name)
		}
		if f.CompressedSize64 > 0 && f.UncompressedSize64 > f.CompressedSize64*maxXLSXCompressionRatio {
			return fmt.Errorf("source: XLSX ZIP entry %s has suspicious compression ratio", f.Name)
		}
	}
	return nil
}

func artifactBytes(artifacts []Artifact, name string) ([]byte, bool) {
	for _, artifact := range artifacts {
		if artifact.Name == name {
			return artifact.Bytes, true
		}
	}
	return nil, false
}

func extractMarkdownResult(res *Result, label string) (source.ExtractResult, error) {
	if res == nil {
		return source.ExtractResult{}, errors.New("source: extraction returned nil result")
	}
	if !res.OK {
		return source.ExtractResult{}, fmt.Errorf("source: %s extraction failed: %s", label, res.Stderr)
	}
	md, ok := artifactBytes(res.Artifacts, "extract.md")
	if !ok || len(md) == 0 {
		return source.ExtractResult{}, fmt.Errorf("source: %s extraction missing extract.md", label)
	}
	metaBytes, ok := artifactBytes(res.Artifacts, "extract.json")
	if !ok || len(metaBytes) == 0 {
		return source.ExtractResult{}, fmt.Errorf("source: %s extraction missing extract.json", label)
	}
	var meta source.ExtractionMeta
	if err := json.Unmarshal(metaBytes, &meta); err != nil {
		return source.ExtractResult{}, fmt.Errorf("source: parse %s extract metadata: %w", label, err)
	}
	return source.ExtractResult{Markdown: string(md), Metadata: meta}, nil
}

func clipOutput(s string, limit int) string {
	if limit <= 0 || len(s) <= limit {
		return s
	}
	return s[:limit] + "\n...[truncated]"
}

type limitedBuffer struct {
	data      bytes.Buffer
	limit     int64
	truncated bool
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if b.limit > 0 {
		remaining := b.limit - int64(b.data.Len())
		if remaining <= 0 {
			b.truncated = true
			return len(p), nil
		}
		if int64(len(p)) > remaining {
			_, _ = b.data.Write(p[:remaining])
			b.truncated = true
			return len(p), nil
		}
	}
	_, _ = b.data.Write(p)
	return len(p), nil
}

func (b *limitedBuffer) String() string {
	if b.truncated {
		return b.data.String() + "\n...[truncated]"
	}
	return b.data.String()
}
