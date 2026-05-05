package source

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aura/aura/internal/sandbox"
	"github.com/xuri/excelize/v2"
)

func TestPyodideXLSXExtractor(t *testing.T) {
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
	res, err := ExtractWithPyodide(context.Background(), runner, ExtractInput{
		Source: &Source{ID: "src_0123456789abcdef", Kind: KindXLSX, Filename: "budget.xlsx"},
		Bytes:  body,
	})
	if err != nil {
		t.Fatalf("ExtractWithPyodide() error = %v", err)
	}
	if !strings.Contains(res.Markdown, "sandbox") || res.Metadata.SheetCount == 0 {
		t.Fatalf("result = %+v\n%s", res.Metadata, res.Markdown)
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
