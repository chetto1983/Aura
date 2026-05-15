package docinspect

import (
	"archive/zip"
	"bytes"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"
)

func TestZipEntryHelpers(t *testing.T) {
	body := zipBody(t, map[string]string{
		"word/document.xml": `<w:document><w:t xml:space="preserve">hello</w:t><w:t>world</w:t></w:document>`,
		"notes.txt":         "plain",
	})

	names, err := ZipEntryNames(body)
	if err != nil {
		t.Fatalf("ZipEntryNames: %v", err)
	}
	if !ContainsString(names, "word/document.xml") {
		t.Fatalf("ZipEntryNames missing document.xml: %v", names)
	}
	entry, err := ZipEntryBytes(body, "notes.txt")
	if err != nil {
		t.Fatalf("ZipEntryBytes: %v", err)
	}
	if string(entry) != "plain" {
		t.Fatalf("ZipEntryBytes = %q", entry)
	}
	if _, err := ZipEntryBytes(body, "missing.xml"); err == nil {
		t.Fatal("ZipEntryBytes missing entry returned nil error")
	}
}

func TestXMLHelpers(t *testing.T) {
	body := []byte(`<w:document><w:t xml:space="preserve">hello</w:t><w:t>world</w:t></w:document>`)
	if err := XMLWellFormed(body); err != nil {
		t.Fatalf("XMLWellFormed valid XML: %v", err)
	}
	if err := XMLWellFormed([]byte(`<root><broken>`)); err == nil {
		t.Fatal("XMLWellFormed malformed XML returned nil error")
	}
	if got := XMLTagText(body, "w:t"); got != "hello world " {
		t.Fatalf("XMLTagText = %q", got)
	}

	zipBytes := zipBody(t, map[string]string{"word/document.xml": string(body)})
	got, err := TextFromZipEntry(zipBytes, "word/document.xml", "w:t")
	if err != nil {
		t.Fatalf("TextFromZipEntry: %v", err)
	}
	if got != "hello world " {
		t.Fatalf("TextFromZipEntry = %q", got)
	}
}

func TestOpenXLSXAndText(t *testing.T) {
	f := excelize.NewFile()
	t.Cleanup(func() { _ = f.Close() })
	if err := f.SetCellValue("Sheet1", "A1", "Citta"); err != nil {
		t.Fatalf("SetCellValue A1: %v", err)
	}
	if err := f.SetCellValue("Sheet1", "B1", "Milano"); err != nil {
		t.Fatalf("SetCellValue B1: %v", err)
	}
	buf, err := f.WriteToBuffer()
	if err != nil {
		t.Fatalf("WriteToBuffer: %v", err)
	}

	opened, err := OpenXLSX(buf.Bytes())
	if err != nil {
		t.Fatalf("OpenXLSX: %v", err)
	}
	defer opened.Close()
	text := XLSXText(opened)
	if !strings.Contains(text, "Citta") || !strings.Contains(text, "Milano") {
		t.Fatalf("XLSXText = %q", text)
	}
}

func TestEncodingChecks(t *testing.T) {
	if !HasMojibake("bad \uFFFD text") {
		t.Fatal("HasMojibake missed replacement character")
	}
	if HasMojibake("clean text") {
		t.Fatal("HasMojibake flagged clean text")
	}
	if !HasControlChars("bad\x00text") {
		t.Fatal("HasControlChars missed NUL")
	}
	if HasControlChars("tab\tnewline\ncarriage\rreturn") {
		t.Fatal("HasControlChars flagged allowed whitespace")
	}
}

func zipBody(t *testing.T, entries map[string]string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range entries {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("zip create %s: %v", name, err)
		}
		if _, err := w.Write([]byte(body)); err != nil {
			t.Fatalf("zip write %s: %v", name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	return buf.Bytes()
}
