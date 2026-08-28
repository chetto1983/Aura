package tools

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// xlsx (.xlsx) extraction — the spreadsheet half of document_extract.go, split out because the
// OOXML relationship-following (workbook -> rels -> sheet parts) plus row/column rendering is a
// distinct concern from the shared XML-tree walker and the docx/notebook extractors.

const spreadsheetNS = "http://schemas.openxmlformats.org/spreadsheetml/2006/main"
const packageRelNS = "http://schemas.openxmlformats.org/package/2006/relationships"

// maxXlsxRowsPerSheet / maxXlsxCols bound the rendered text: a sheet is read whole (openpyxl-style,
// no streaming), so an unbounded row/column count would spike RAM and pin a tool turn on a huge
// workbook. Ported 1:1 from read_extract.py's _MAX_XLSX_ROWS_PER_SHEET / _MAX_XLSX_COLS.
const (
	maxXlsxRowsPerSheet = 5000
	maxXlsxCols         = 256
)

func extractXlsx(raw []byte) (string, error) {
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return "", extractErr("not a valid XLSX: %v", err)
	}
	names := map[string]bool{}
	for _, f := range zr.File {
		names[f.Name] = true
	}

	budget := newOOXMLBudget()
	shared, err := xlsxSharedStrings(zr, names, budget)
	if err != nil {
		return "", err
	}
	sheets, err := xlsxWorkbookSheets(zr, budget)
	if err != nil {
		return "", err
	}
	rels, err := xlsxWorkbookRels(zr, names, budget)
	if err != nil {
		return "", err
	}

	var out []string
	outputBytes := 0
	for _, sheet := range sheets {
		if sheet.state == "hidden" || sheet.state == "veryHidden" {
			continue
		}
		part := xlsxSheetPart(rels[sheet.relID])
		if part == "" || !names[part] {
			continue
		}
		data, err := xlsxReadZipMember(zr, part, budget)
		if err != nil {
			if errors.Is(err, errOOXMLLimit) {
				return "", err
			}
			continue
		}
		root, err := parseXMLTree(data)
		if err != nil {
			if errors.Is(err, errOOXMLLimit) {
				return "", err
			}
			continue // malformed sheet XML: skip, matching read_extract's ET.ParseError swallow
		}
		rows := xlsxSheetRows(root, shared)
		header := fmt.Sprintf("# ── Sheet: %s ──", sheet.name)
		if err := addOOXMLOutput(&outputBytes, len(header)+1); err != nil {
			return "", err
		}
		out = append(out, header)
		for _, row := range rows {
			rowBytes := max(0, len(row)-1)
			for _, cell := range row {
				rowBytes += len(cell)
			}
			if err := addOOXMLOutput(&outputBytes, rowBytes+1); err != nil {
				return "", err
			}
			out = append(out, strings.Join(row, "\t"))
		}
		if len(rows) == 0 {
			if err := addOOXMLOutput(&outputBytes, len("(empty)")+1); err != nil {
				return "", err
			}
			out = append(out, "(empty)")
		}
		if err := addOOXMLOutput(&outputBytes, 1); err != nil {
			return "", err
		}
		out = append(out, "")
	}
	if len(out) == 0 {
		return "", extractErr("XLSX has no visible sheets with content")
	}
	return strings.TrimRight(strings.Join(out, "\n"), "\n") + "\n", nil
}

func xlsxReadZipMember(zr *zip.Reader, name string, budget *ooxmlBudget) ([]byte, error) {
	return budget.readMember(zr, name)
}

func xlsxSharedStrings(zr *zip.Reader, names map[string]bool, budget *ooxmlBudget) ([]string, error) {
	if !names["xl/sharedStrings.xml"] {
		return nil, nil
	}
	root, err := readZipXML(zr, "xl/sharedStrings.xml", budget)
	if err != nil {
		if errors.Is(err, errOOXMLLimit) {
			return nil, err
		}
		return nil, nil // malformed: matches read_extract's parse-error swallow (returns [])
	}
	var out []string
	for _, si := range xmlDescendants(root, spreadsheetNS, "si") {
		var b strings.Builder
		for _, t := range xmlDescendants(si, spreadsheetNS, "t") {
			_, _ = b.Write(t.text)
		}
		out = append(out, b.String())
	}
	return out, nil
}

type xlsxSheetMeta struct{ name, state, relID string }

func xlsxWorkbookSheets(zr *zip.Reader, budget *ooxmlBudget) ([]xlsxSheetMeta, error) {
	root, err := readZipXML(zr, "xl/workbook.xml", budget)
	if err != nil {
		return nil, err
	}
	var out []xlsxSheetMeta
	for _, sheet := range xmlDescendants(root, spreadsheetNS, "sheet") {
		name := sheet.attrs["name"]
		if name == "" {
			name = "Sheet"
		}
		state := sheet.attrs["state"]
		if state == "" {
			state = "visible"
		}
		out = append(out, xlsxSheetMeta{name: name, state: state, relID: sheet.attrs["id"]})
	}
	return out, nil
}

func xlsxWorkbookRels(zr *zip.Reader, names map[string]bool, budget *ooxmlBudget) (map[string]string, error) {
	const relsPath = "xl/_rels/workbook.xml.rels"
	if !names[relsPath] {
		return map[string]string{}, nil
	}
	root, err := readZipXML(zr, relsPath, budget)
	if err != nil {
		if errors.Is(err, errOOXMLLimit) {
			return nil, err
		}
		return map[string]string{}, nil
	}
	out := map[string]string{}
	for _, rel := range xmlDescendants(root, packageRelNS, "Relationship") {
		if id := rel.attrs["Id"]; id != "" {
			out[id] = rel.attrs["Target"]
		}
	}
	return out, nil
}

func xlsxSheetPart(target string) string {
	target = strings.TrimPrefix(target, "/")
	if !strings.HasPrefix(target, "xl/") {
		target = "xl/" + target
	}
	// Normalize ".." / "." segments the way posixpath.normpath does — worksheets are always
	// referenced relative to xl/, but a defensive normalize avoids a malformed relationship
	// target escaping the prefix check above.
	parts := strings.Split(target, "/")
	var cleaned []string
	for _, p := range parts {
		switch p {
		case "", ".":
			continue
		case "..":
			if len(cleaned) > 0 {
				cleaned = cleaned[:len(cleaned)-1]
			}
		default:
			cleaned = append(cleaned, p)
		}
	}
	return strings.Join(cleaned, "/")
}

func xlsxSheetRows(root *xmlNode, shared []string) [][]string {
	var rows [][]string
	for _, row := range xmlDescendants(root, spreadsheetNS, "row") {
		if len(rows) >= maxXlsxRowsPerSheet {
			break
		}
		cells := map[int]string{}
		maxCol := -1
		for _, c := range xmlDescendants(row, spreadsheetNS, "c") {
			col := maxCol + 1
			if ref := c.attrs["r"]; ref != "" {
				col = colIndex(ref)
			}
			if col >= maxXlsxCols {
				continue
			}
			cells[col] = xlsxCellValue(c, shared)
			if col > maxCol {
				maxCol = col
			}
		}
		if maxCol < 0 {
			rows = append(rows, nil)
			continue
		}
		line := make([]string, maxCol+1)
		for i := range line {
			line[i] = cells[i]
		}
		rows = append(rows, line)
	}
	for len(rows) > 0 && rowIsBlank(rows[len(rows)-1]) {
		rows = rows[:len(rows)-1]
	}
	return rows
}

func rowIsBlank(row []string) bool {
	for _, v := range row {
		if strings.TrimSpace(v) != "" {
			return false
		}
	}
	return true
}

func xlsxCellValue(c *xmlNode, shared []string) string {
	value := ""
	for _, v := range c.children {
		if v.space == spreadsheetNS && v.local == "v" {
			value = string(v.text)
			break
		}
	}
	switch c.attrs["t"] {
	case "s":
		if idx, err := strconv.Atoi(value); err == nil && idx >= 0 && idx < len(shared) {
			return shared[idx]
		}
		return ""
	case "inlineStr":
		for _, is := range c.children {
			if is.space == spreadsheetNS && is.local == "is" {
				var b strings.Builder
				for _, t := range xmlDescendants(is, spreadsheetNS, "t") {
					_, _ = b.Write(t.text)
				}
				return b.String()
			}
		}
		return ""
	case "b":
		if strings.TrimSpace(value) == "1" || strings.EqualFold(value, "true") {
			return "TRUE"
		}
		return "FALSE"
	case "e":
		if value == "" {
			return "#ERROR"
		}
		return value
	default:
		return value
	}
}
