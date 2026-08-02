package filecard

import (
	"archive/zip"
	"encoding/xml"
	"fmt"
	"io"
	"path"
	"strconv"
	"strings"
)

// XLSX is a zip of XML parts (ECMA-376 / ISO-IEC 29500 SpreadsheetML). Reading
// it with archive/zip + encoding/xml rather than a spreadsheet library is what
// makes the read cap real: the reader stops mid-sheet after maxRowsScanned rows
// and the rest of a 100 MB worksheet is never decompressed.
//
// Cell types, from the ST_CellType enumeration: "s" shared-string index,
// "str" formula string, "inlineStr" literal <is><t>, "b" boolean, "e" error,
// "d" ISO date, absent/"n" number.
// https://learn.microsoft.com/en-us/dotnet/api/documentformat.openxml.spreadsheet.cellvalues
func buildXLSX(req Request, card Card) (Card, error) {
	reader, err := zip.OpenReader(req.Path)
	if err != nil {
		return card, fmt.Errorf("open workbook: %w", err)
	}
	defer func() { _ = reader.Close() }()

	card.Title = ooxmlTitle(&reader.Reader)
	shared := readSharedStrings(&reader.Reader)
	sheets, err := readWorkbookSheets(&reader.Reader)
	if err != nil {
		return card, err
	}
	if len(sheets) == 0 {
		card.Caveats = append(card.Caveats, "The workbook declares no worksheets.")
		return card, nil
	}

	names := make([]string, 0, len(sheets))
	for _, sheet := range sheets {
		names = append(names, sheet.name)
	}
	card.Facts = append(card.Facts, plural(len(sheets), "sheet", "sheets"))

	scanned := 0
	for _, sheet := range sheets {
		if scanned >= maxSheetsScanned {
			card.Caveats = append(card.Caveats, fmt.Sprintf(
				"Only the first %d of %d sheets were read; the others are named here but not described: %s.",
				maxSheetsScanned, len(sheets), strings.Join(names[maxSheetsScanned:], ", ")))
			break
		}
		file := zipEntry(&reader.Reader, sheet.target)
		if file == nil {
			continue
		}
		built, capped, err := readWorksheet(file, sheet.name, shared)
		if err != nil {
			card.Caveats = append(card.Caveats, fmt.Sprintf("Sheet %q could not be read (%v).", sheet.name, err))
			continue
		}
		scanned++
		if capped {
			card.Caveats = append(card.Caveats, fmt.Sprintf(
				"Sheet %q was sampled: the first %d rows were read out of %d.",
				sheet.name, built.RowsScanned, built.Rows))
		}
		card.Sheets = append(card.Sheets, built)
	}
	if len(card.Sheets) == 1 && len(card.Sheets[0].Columns) > 0 {
		card.Facts = append(card.Facts, fmt.Sprintf("%d rows x %d columns",
			card.Sheets[0].Rows, len(card.Sheets[0].Columns)))
	}
	if !anyCells(card.Sheets) {
		card.Caveats = append(card.Caveats,
			"No sheet in this workbook holds cell data; what it contains is drawings, images or charts, "+
				"which are not read at ingest.")
	}
	return card, nil
}

type workbookSheet struct {
	name   string
	target string
}

func anyCells(sheets []Sheet) bool {
	for _, sheet := range sheets {
		if len(sheet.Columns) > 0 {
			return true
		}
	}
	return false
}

// readWorkbookSheets resolves each sheet's part through the workbook
// relationships. Sheet order in the zip is not sheet order in the workbook, and
// the file named sheet1.xml is not necessarily the first tab.
func readWorkbookSheets(reader *zip.Reader) ([]workbookSheet, error) {
	workbook := zipEntry(reader, "xl/workbook.xml")
	if workbook == nil {
		return nil, fmt.Errorf("workbook part is missing")
	}
	rels := readRelationships(reader, "xl/_rels/workbook.xml.rels")
	body, err := zipEntryReader(workbook)
	if err != nil {
		return nil, err
	}
	defer func() { _ = body.Close() }()

	var sheets []workbookSheet
	decoder := xml.NewDecoder(body)
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return sheets, fmt.Errorf("read workbook: %w", err)
		}
		start, ok := token.(xml.StartElement)
		if !ok || start.Name.Local != "sheet" {
			continue
		}
		sheet := workbookSheet{}
		for _, attr := range start.Attr {
			switch attr.Name.Local {
			case "name":
				sheet.name = attr.Value
			case "id":
				sheet.target = rels[attr.Value]
			}
		}
		if sheet.target == "" {
			continue
		}
		sheets = append(sheets, sheet)
	}
	return sheets, nil
}

// readRelationships maps rId -> part name, resolved against the .rels file's own
// directory the way the OPC package spec defines it.
func readRelationships(reader *zip.Reader, relsPath string) map[string]string {
	out := map[string]string{}
	file := zipEntry(reader, relsPath)
	if file == nil {
		return out
	}
	body, err := zipEntryReader(file)
	if err != nil {
		return out
	}
	defer func() { _ = body.Close() }()
	base := path.Dir(path.Dir(relsPath))
	decoder := xml.NewDecoder(body)
	for {
		token, err := decoder.Token()
		if err != nil {
			return out
		}
		start, ok := token.(xml.StartElement)
		if !ok || start.Name.Local != "Relationship" {
			continue
		}
		var id, target string
		for _, attr := range start.Attr {
			switch attr.Name.Local {
			case "Id":
				id = attr.Value
			case "Target":
				target = attr.Value
			}
		}
		if id == "" || target == "" {
			continue
		}
		if absolute, rooted := strings.CutPrefix(target, "/"); rooted {
			out[id] = absolute
			continue
		}
		out[id] = path.Join(base, target)
	}
}

// readSharedStrings loads the workbook's string table. Cells hold an index into
// it, so without the table a spreadsheet reads as a grid of integers. The table
// is bounded: past maxSharedStringBytes the remaining indices resolve to empty
// rather than growing the heap with a file's worth of prose.
func readSharedStrings(reader *zip.Reader) []string {
	file := zipEntry(reader, "xl/sharedStrings.xml")
	if file == nil {
		return nil
	}
	body, err := zipEntryReader(file)
	if err != nil {
		return nil
	}
	defer func() { _ = body.Close() }()

	var strs []string
	decoder := xml.NewDecoder(io.LimitReader(body, maxSharedStringBytes))
	var current strings.Builder
	inItem := false
	for {
		token, err := decoder.Token()
		if err != nil {
			return strs
		}
		switch element := token.(type) {
		case xml.StartElement:
			if element.Name.Local == "si" {
				inItem = true
				current.Reset()
			}
		case xml.CharData:
			if inItem && current.Len() < maxValueRunes*4 {
				current.Write(element)
			}
		case xml.EndElement:
			if element.Name.Local == "si" {
				strs = append(strs, current.String())
				inItem = false
			}
		}
	}
}

// readWorksheet streams one worksheet into a table card. It reports whether the
// row cap bit, so the card can say the sheet was sampled instead of implying it
// was read whole.
func readWorksheet(file *zip.File, name string, shared []string) (Sheet, bool, error) {
	body, err := zipEntryReader(file)
	if err != nil {
		return Sheet{}, false, err
	}
	defer func() { _ = body.Close() }()

	table := newTable(name)
	decoder := xml.NewDecoder(body)
	declaredRows := int64(0)
	rows := 0
	capped := false
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			// A truncated part still described everything read so far, and a
			// half-read sheet beats no sheet.
			break
		}
		start, ok := token.(xml.StartElement)
		if !ok {
			continue
		}
		switch start.Name.Local {
		case "dimension":
			declaredRows = dimensionRows(start)
		case "row":
			if rows >= maxRowsScanned {
				capped = true
				break
			}
			values, err := readRow(decoder, shared)
			if err != nil {
				break
			}
			rows++
			table.addRow(values)
		}
		if capped {
			break
		}
	}
	sheet := table.done(declaredRows)
	return sheet, capped && sheet.Rows > int64(sheet.RowsScanned), nil
}

// readRow consumes one <row>, returning its cells positioned by their column
// reference so a gap in the middle does not shift every value left.
func readRow(decoder *xml.Decoder, shared []string) ([]string, error) {
	var values []string
	next := 0
	for {
		token, err := decoder.Token()
		if err != nil {
			return values, err
		}
		switch element := token.(type) {
		case xml.StartElement:
			if element.Name.Local != "c" {
				continue
			}
			index, cellType := cellPosition(element, next)
			value, err := readCell(decoder, cellType, shared)
			if err != nil {
				return values, err
			}
			if index >= maxColumnsScanned {
				next = index + 1
				continue
			}
			for len(values) <= index {
				values = append(values, "")
			}
			values[index] = value
			next = index + 1
		case xml.EndElement:
			if element.Name.Local == "row" {
				return values, nil
			}
		}
	}
}

func cellPosition(element xml.StartElement, fallback int) (int, string) {
	index, cellType := fallback, ""
	for _, attr := range element.Attr {
		switch attr.Name.Local {
		case "r":
			if column := columnIndex(attr.Value); column >= 0 {
				index = column
			}
		case "t":
			cellType = attr.Value
		}
	}
	return index, cellType
}

// readCell consumes one <c>. <v> holds the value for every type except
// inlineStr, which carries <is><t>; <f> holds a formula and is skipped.
func readCell(decoder *xml.Decoder, cellType string, shared []string) (string, error) {
	var value, inline strings.Builder
	inValue, inInline := false, false
	for {
		token, err := decoder.Token()
		if err != nil {
			return "", err
		}
		switch element := token.(type) {
		case xml.StartElement:
			switch element.Name.Local {
			case "v":
				inValue = true
			case "is":
				inInline = true
			}
		case xml.CharData:
			switch {
			case inValue:
				value.Write(element)
			case inInline:
				inline.Write(element)
			}
		case xml.EndElement:
			switch element.Name.Local {
			case "v":
				inValue = false
			case "is":
				inInline = false
			case "c":
				return cellValue(cellType, value.String(), inline.String(), shared), nil
			}
		}
	}
}

func cellValue(cellType, value, inline string, shared []string) string {
	switch cellType {
	case "s":
		index, err := strconv.Atoi(strings.TrimSpace(value))
		if err != nil || index < 0 || index >= len(shared) {
			return ""
		}
		return shared[index]
	case "inlineStr":
		return inline
	case "e":
		// An error cell says nothing about the file's subject.
		return ""
	default:
		return value
	}
}

// dimensionRows reads the last row number out of a <dimension ref="A1:G5890"/>.
// It is how a capped scan still reports a sheet's real size.
func dimensionRows(element xml.StartElement) int64 {
	for _, attr := range element.Attr {
		if attr.Name.Local != "ref" {
			continue
		}
		_, last, found := strings.Cut(attr.Value, ":")
		if !found {
			last = attr.Value
		}
		digits := strings.TrimLeft(last, "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz$")
		rows, err := strconv.ParseInt(digits, 10, 64)
		if err != nil {
			return 0
		}
		return rows
	}
	return 0
}

// columnIndex turns the letters of a cell reference into a zero-based column.
func columnIndex(ref string) int {
	index := 0
	seen := false
	for _, r := range ref {
		switch {
		case r >= 'A' && r <= 'Z':
			index = index*26 + int(r-'A') + 1
			seen = true
		case r >= 'a' && r <= 'z':
			index = index*26 + int(r-'a') + 1
			seen = true
		case r == '$':
		default:
			if !seen {
				return -1
			}
			return index - 1
		}
	}
	if !seen {
		return -1
	}
	return index - 1
}

func plural(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, one)
	}
	return fmt.Sprintf("%d %s", n, many)
}
