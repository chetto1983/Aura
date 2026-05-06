package source_test

import (
	"archive/zip"
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aura/aura/internal/sandbox"
	"github.com/aura/aura/internal/source"
	"github.com/xuri/excelize/v2"
)

func TestPyodideXLSXExtractor(t *testing.T) {
	requireLivePyodideExtraction(t)
	runner, err := sandbox.NewPyodideRunner(sandbox.PyodideRunnerConfig{
		RuntimeDir: filepath.Join("..", "..", "runtime", "pyodide"),
		Timeout:    60 * time.Second,
	})
	if err != nil {
		t.Fatalf("runner: %v", err)
	}
	if avail := runner.CheckAvailability(); !avail.Available {
		t.Skipf("pyodide unavailable: %s", avail.Detail)
	}
	body := makeTestWorkbook(t)
	res, err := source.ExtractWithPyodide(context.Background(), runner, source.ExtractInput{
		Source: &source.Source{ID: "src_0123456789abcdef", Kind: source.KindXLSX, Filename: "budget.xlsx"},
		Bytes:  body,
	})
	if err != nil {
		t.Fatalf("ExtractWithPyodide() error = %v", err)
	}
	if !strings.Contains(res.Markdown, "sandbox") || res.Metadata.SheetCount == 0 {
		t.Fatalf("result = %+v\n%s", res.Metadata, res.Markdown)
	}
}

func TestPyodideDOCXExtractor(t *testing.T) {
	requireLivePyodideExtraction(t)
	runner, err := sandbox.NewPyodideRunner(sandbox.PyodideRunnerConfig{
		RuntimeDir: filepath.Join("..", "..", "runtime", "pyodide"),
		Timeout:    60 * time.Second,
	})
	if err != nil {
		t.Fatalf("runner: %v", err)
	}
	if avail := runner.CheckAvailability(); !avail.Available {
		t.Skipf("pyodide unavailable: %s", avail.Detail)
	}
	body := makeTestDocument(t)
	res, err := source.ExtractWithPyodide(context.Background(), runner, source.ExtractInput{
		Source: &source.Source{ID: "src_0123456789abcdef", Kind: source.KindDOCX, Filename: "memo.docx"},
		Bytes:  body,
	})
	if err != nil {
		t.Fatalf("ExtractWithPyodide() error = %v", err)
	}
	if !strings.Contains(res.Markdown, "Aura should remember decisions") || res.Metadata.ExtractorName != "pyodide_docx" {
		t.Fatalf("result = %+v\n%s", res.Metadata, res.Markdown)
	}
}

func requireLivePyodideExtraction(t *testing.T) {
	t.Helper()
	if os.Getenv("AURA_SOURCE_PYODIDE_LIVE") != "1" {
		t.Skip("set AURA_SOURCE_PYODIDE_LIVE=1 to run live Pyodide extraction; it needs Node and a high-memory Docker engine")
	}
}

func makeTestWorkbook(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	f := excelize.NewFile()
	defer f.Close()
	if err := f.SetCellValue("Sheet1", "A1", "item"); err != nil {
		t.Fatalf("set A1: %v", err)
	}
	if err := f.SetCellValue("Sheet1", "B1", "cost"); err != nil {
		t.Fatalf("set B1: %v", err)
	}
	if err := f.SetCellValue("Sheet1", "A2", "sandbox"); err != nil {
		t.Fatalf("set A2: %v", err)
	}
	if err := f.SetCellValue("Sheet1", "B2", 12); err != nil {
		t.Fatalf("set B2: %v", err)
	}
	if err := f.Write(&buf); err != nil {
		t.Fatalf("write workbook: %v", err)
	}
	return buf.Bytes()
}

func makeTestDocument(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	writeZipFile(t, zw, "[Content_Types].xml", `<?xml version="1.0" encoding="UTF-8"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
  <Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
  <Default Extension="xml" ContentType="application/xml"/>
  <Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>
</Types>`)
	writeZipFile(t, zw, "_rels/.rels", `<?xml version="1.0" encoding="UTF-8"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
  <Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/>
</Relationships>`)
	writeZipFile(t, zw, "word/document.xml", `<?xml version="1.0" encoding="UTF-8"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
  <w:body>
    <w:p><w:r><w:t>Aura should remember decisions.</w:t></w:r></w:p>
    <w:p><w:r><w:t>Ship DOCX extraction safely.</w:t></w:r></w:p>
  </w:body>
</w:document>`)
	if err := zw.Close(); err != nil {
		t.Fatalf("close docx zip: %v", err)
	}
	return buf.Bytes()
}

func writeZipFile(t *testing.T, zw *zip.Writer, name, body string) {
	t.Helper()
	w, err := zw.Create(name)
	if err != nil {
		t.Fatalf("create %s: %v", name, err)
	}
	if _, err := w.Write([]byte(body)); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}
