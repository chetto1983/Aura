// Package docinspect provides byte-level helpers for probe/debug document checks.
package docinspect

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"strings"

	"github.com/ledongthuc/pdf"
	"github.com/xuri/excelize/v2"
)

// ZipEntryNames lists every entry name inside a ZIP archive.
func ZipEntryNames(body []byte) ([]string, error) {
	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		return nil, err
	}
	out := make([]string, 0, len(zr.File))
	for _, f := range zr.File {
		out = append(out, f.Name)
	}
	return out, nil
}

// ZipEntryBytes returns the bytes of one entry inside a ZIP archive.
func ZipEntryBytes(body []byte, entryName string) ([]byte, error) {
	zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		return nil, fmt.Errorf("zip reader: %w", err)
	}
	for _, f := range zr.File {
		if f.Name == entryName {
			rc, err := f.Open()
			if err != nil {
				return nil, fmt.Errorf("open %s: %w", entryName, err)
			}
			defer rc.Close()
			return io.ReadAll(rc)
		}
	}
	return nil, fmt.Errorf("entry %q not found", entryName)
}

// XMLWellFormed returns nil if body parses to a complete XML document.
func XMLWellFormed(body []byte) error {
	dec := xml.NewDecoder(bytes.NewReader(body))
	for {
		_, err := dec.Token()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

// XMLTagText returns concatenated text inside every <tag>...</tag> pair.
//
// This is a pragmatic probe helper, not a general XML query engine. It
// tolerates attributes on the open tag, such as <w:t xml:space="preserve">.
func XMLTagText(body []byte, tag string) string {
	xmlText := string(body)
	var out strings.Builder
	openMarker := "<" + tag
	closeMarker := "</" + tag + ">"
	i := 0
	for {
		start := strings.Index(xmlText[i:], openMarker)
		if start < 0 {
			break
		}
		afterOpen := strings.Index(xmlText[i+start:], ">")
		if afterOpen < 0 {
			break
		}
		textStart := i + start + afterOpen + 1
		end := strings.Index(xmlText[textStart:], closeMarker)
		if end < 0 {
			break
		}
		out.WriteString(xmlText[textStart : textStart+end])
		out.WriteString(" ")
		i = textStart + end + len(closeMarker)
	}
	return out.String()
}

// TextFromZipEntry extracts XML tag text from a named ZIP entry.
func TextFromZipEntry(body []byte, entryName, tag string) (string, error) {
	xmlBytes, err := ZipEntryBytes(body, entryName)
	if err != nil {
		return "", err
	}
	return XMLTagText(xmlBytes, tag), nil
}

// PDFPageCount returns the number of pages reported by the PDF parser.
func PDFPageCount(body []byte) (int, error) {
	reader, err := pdf.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		return 0, fmt.Errorf("pdf NewReader: %w", err)
	}
	return reader.NumPage(), nil
}

// PDFText returns the textual content of a PDF byte buffer.
func PDFText(body []byte) (string, error) {
	reader, err := pdf.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		return "", fmt.Errorf("pdf NewReader: %w", err)
	}
	var out strings.Builder
	for i := 1; i <= reader.NumPage(); i++ {
		page := reader.Page(i)
		if page.V.IsNull() {
			continue
		}
		text, perr := page.GetPlainText(nil)
		if perr != nil {
			continue
		}
		out.WriteString(text)
		out.WriteString("\n")
	}
	return out.String(), nil
}

// OpenXLSX opens an XLSX workbook from bytes.
func OpenXLSX(body []byte) (*excelize.File, error) {
	f, err := excelize.OpenReader(bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("excelize open: %w", err)
	}
	return f, nil
}

// XLSXText concatenates every cell value in a workbook.
func XLSXText(f *excelize.File) string {
	var sb strings.Builder
	for _, sheet := range f.GetSheetList() {
		rows, _ := f.GetRows(sheet)
		for _, row := range rows {
			for _, cell := range row {
				sb.WriteString(cell)
				sb.WriteString(" ")
			}
		}
	}
	return sb.String()
}

// HasMojibake reports whether s contains the U+FFFD replacement character.
func HasMojibake(s string) bool {
	return strings.ContainsRune(s, '\uFFFD')
}

// HasControlChars reports whether s contains unexpected control characters.
func HasControlChars(s string) bool {
	for _, r := range s {
		if r == '\t' || r == '\n' || r == '\r' {
			continue
		}
		if r < 0x20 || (r >= 0x7f && r <= 0x9f) {
			return true
		}
	}
	return false
}

// ContainsString reports whether xs contains want.
func ContainsString(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}
