package filecard

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"
)

// headerProbeRows is how many leading rows are examined before deciding which
// one is the header. Fewer than 3% of real spreadsheets have a predefined data
// model — a banner row, a blank, then the real headers is the normal shape —
// so taking row 1 on faith mislabels every column in those files.
const headerProbeRows = 8

// maxSampleCandidates bounds the rows held back as sampling candidates. Rows are
// kept on a doubling stride, which spreads the candidates across everything
// scanned instead of clustering them at the top of the file.
const maxSampleCandidates = 64

// maxValueRunes truncates a value before it is counted or shown. A cell longer
// than this is a paragraph, not a category.
const maxValueRunes = 72

// tableBuilder accumulates one tabular surface: a worksheet or a delimited file.
type tableBuilder struct {
	name     string
	preamble []string
	headers  []string
	columns  []*columnStats

	probe          [][]string
	probePositions []int64
	headerFound    bool
	headerRow      int64
	rowsScanned    int
	// rowsSkipped counts the leading rows that are not data — the banner lines
	// above the header, plus the header itself — for formats that do not expose
	// physical row positions.
	rowsSkipped int

	candidates [][]string
	stride     int
	sinceKept  int
}

func newTable(name string) *tableBuilder {
	return &tableBuilder{name: name, stride: 1}
}

// addRow feeds one raw row. The first rows are buffered until the header row is
// identified; everything after it is counted, sampled and forgotten.
func (t *tableBuilder) addRow(values []string) {
	t.addRowAt(values, 0)
}

func (t *tableBuilder) addRowAt(values []string, position int64) {
	if !anyNonEmpty(values) {
		return
	}
	if !t.headerFound {
		t.probe = append(t.probe, clone(values))
		t.probePositions = append(t.probePositions, position)
		if len(t.probe) >= headerProbeRows {
			t.resolveHeader()
		}
		return
	}
	t.consume(values)
}

// done resolves a header that was never confirmed (a file shorter than the
// probe window) and returns the finished sheet. declaredTotal is the row count
// the file declares for itself, header and banner included; it is how a capped
// scan still reports the real size. Zero or less means "not declared".
func (t *tableBuilder) done(declaredTotal int64) Sheet {
	if !t.headerFound {
		t.resolveHeader()
	}
	rows := int64(t.rowsScanned)
	if declaredTotal > 0 {
		if t.headerRow > 0 {
			rows = max(declaredTotal-t.headerRow, 0)
		} else {
			rows = max(declaredTotal-int64(t.rowsSkipped), 0)
		}
	}
	sheet := Sheet{Name: t.name, Rows: rows, RowsScanned: t.rowsScanned, HeaderRow: t.headerRow}
	for _, col := range t.columns {
		sheet.Columns = append(sheet.Columns, col.column())
	}
	for _, row := range t.pickSamples() {
		sheet.Samples = append(sheet.Samples, t.sampleRow(row))
	}
	return sheet
}

// headerTextShare is how much of a candidate header row must be text. Headers
// are words; a row that is mostly numbers is data, however early it appears.
// The share is not 100% because real headers include years and quantities.
const headerTextShare = 0.7

// resolveHeader picks the header row out of the probe buffer: the first row as
// wide as the widest probed row that reads like labels. A banner ("Elenco
// fornitori 2025") is narrower than the table under it, so it is left as
// preamble; and when NO probed row reads like labels the sheet is treated as
// headerless rather than sacrificing its first data row, which is what turned a
// supplier list into a sheet whose columns were called "SIEMENS S.P.A." and "573".
func (t *tableBuilder) resolveHeader() {
	t.headerFound = true
	if len(t.probe) == 0 {
		return
	}
	widest := 0
	for _, row := range t.probe {
		if n := countNonEmpty(row); n > widest {
			widest = n
		}
	}
	pick := -1
	for i, row := range t.probe {
		if countNonEmpty(row) == widest && readsAsLabels(row) {
			pick = i
			break
		}
	}
	for i, row := range t.probe {
		if i < pick {
			if line := strings.TrimSpace(strings.Join(nonEmpty(row), " ")); line != "" {
				t.preamble = append(t.preamble, line)
			}
			t.rowsSkipped++
			continue
		}
		if i == pick {
			t.setHeaders(row)
			t.headerRow = t.probePositions[i]
			t.rowsSkipped++
			continue
		}
		t.consume(row)
	}
	t.probe = nil
	t.probePositions = nil
}

func (t *tableBuilder) setHeaders(row []string) {
	for i, value := range row {
		if i >= maxColumnsScanned {
			break
		}
		t.headers = append(t.headers, clean(value))
	}
}

func (t *tableBuilder) consume(values []string) {
	t.rowsScanned++
	for i, value := range values {
		if i >= maxColumnsScanned {
			break
		}
		t.columnAt(i).add(value)
	}
	t.offerSample(values)
}

func (t *tableBuilder) columnAt(index int) *columnStats {
	for len(t.columns) <= index {
		header := ""
		if len(t.headers) > len(t.columns) {
			header = t.headers[len(t.columns)]
		}
		t.columns = append(t.columns, &columnStats{header: header, counts: map[string]int{}})
	}
	return t.columns[index]
}

// offerSample keeps rows on a doubling stride: every stride-th row is buffered,
// and when the buffer fills, every second entry is dropped and the stride
// doubles. What survives is spread across the whole scan, not the first page.
func (t *tableBuilder) offerSample(values []string) {
	t.sinceKept++
	if t.sinceKept < t.stride {
		return
	}
	t.sinceKept = 0
	t.candidates = append(t.candidates, clone(values))
	if len(t.candidates) < maxSampleCandidates {
		return
	}
	kept := t.candidates[:0]
	for i, row := range t.candidates {
		if i%2 == 0 {
			kept = append(kept, row)
		}
	}
	t.candidates = kept
	t.stride *= 2
}

func (t *tableBuilder) pickSamples() [][]string {
	if len(t.candidates) <= sampleRows {
		return t.candidates
	}
	out := make([][]string, 0, sampleRows)
	step := float64(len(t.candidates)) / float64(sampleRows)
	for i := range sampleRows {
		out = append(out, t.candidates[int(float64(i)*step)])
	}
	return out
}

func (t *tableBuilder) sampleRow(values []string) SampleRow {
	row := make(SampleRow, 0, len(values))
	for i, value := range values {
		if i >= maxColumnsScanned {
			break
		}
		value = clean(value)
		if value == "" {
			continue
		}
		header := ""
		if i < len(t.headers) {
			header = t.headers[i]
		}
		row = append(row, Cell{Header: header, Value: truncateRunes(value, maxValueRunes)})
	}
	return row
}

// columnStats narrates one column: how many values, how many distinct, which
// repeat, and for numbers the extent.
type columnStats struct {
	header   string
	nonEmpty int
	counts   map[string]int
	capped   bool
	numeric  int
	// padded counts values written with a leading zero on the integer part. Decimal
	// notation never pads, so such a value is a fixed-width CODE and the column that
	// holds it stores identifiers rather than quantities. Reading it as a number drops
	// the zeros that make the code valid, and the card then states a type and a range
	// that no cell in the file actually has.
	padded int
	hasNum bool
	minNum float64
	maxNum float64
}

func (c *columnStats) add(raw string) {
	value := clean(raw)
	if value == "" {
		return
	}
	c.nonEmpty++
	key := truncateRunes(value, maxValueRunes)
	if _, seen := c.counts[key]; seen || len(c.counts) < maxTrackedValues {
		c.counts[key]++
	} else {
		c.capped = true
	}
	if isZeroPadded(value) {
		// Deliberately BEFORE parseNumber, so a padded value feeds neither the numeric
		// share nor the extent: it is not a quantity and its magnitude is meaningless.
		c.padded++
		return
	}
	number, ok := parseNumber(value)
	if !ok {
		return
	}
	c.numeric++
	if !c.hasNum {
		c.minNum, c.maxNum, c.hasNum = number, number, true
		return
	}
	c.minNum = min(c.minNum, number)
	c.maxNum = max(c.maxNum, number)
}

func (c *columnStats) column() Column {
	col := Column{
		Header:         c.header,
		Type:           c.kind(),
		NonEmpty:       c.nonEmpty,
		Distinct:       len(c.counts),
		DistinctCapped: c.capped,
		Common:         c.common(),
	}
	// The numeric extent is the tail sketch: it covers the range the frequent
	// values do not, the way Postgres pairs a histogram with most_common_vals.
	if col.Type == "number" && c.hasNum {
		col.Min, col.Max = formatNumber(c.minNum), formatNumber(c.maxNum)
	}
	return col
}

func (c *columnStats) kind() string {
	switch {
	case c.nonEmpty == 0:
		return "empty"
	// One padded value settles the column: padding is a property of how the column is
	// written, not of the single cell, so the rest of it holds codes that merely happen
	// not to need a zero. A share test would call such a column a number whenever the
	// padded minority is small enough, which is exactly when the error is hardest to see.
	//
	// "code" and not "text" because the reader has to DO something different with it.
	// Measured 2026-09-09 against the live agent: told the column was text, it still ran
	// pandas.read_excel with inferred dtypes and answered ISTAT 4040 for a cell reading
	// 004040. A name it cannot read as prose is the first half of saying so; codeColumns
	// below is the second.
	case c.padded > 0:
		return "code"
	case c.numeric*10 >= c.nonEmpty*9:
		return "number"
	case c.numeric*10 <= c.nonEmpty:
		return "text"
	default:
		return "mixed"
	}
}

// common returns the values that describe the column. A value must repeat, and
// it must cover at least a hundredth of the column: a company name that happens
// to appear twice among 5,889 is not what the column is about, and six such
// names are the alphabetical-cap mistake wearing a new hat. Where nothing clears
// the floor the column narration says only how many distinct values there are,
// and the sampled rows carry the examples.
func (c *columnStats) common() []Value {
	return topValues(c.counts, max(2, c.nonEmpty/100), mcvPerColumn)
}

func clean(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

func clone(values []string) []string {
	return append([]string(nil), values...)
}

func anyNonEmpty(values []string) bool {
	return countNonEmpty(values) > 0
}

func countNonEmpty(values []string) int {
	n := 0
	for _, value := range values {
		if clean(value) != "" {
			n++
		}
	}
	return n
}

func nonEmpty(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if cleaned := clean(value); cleaned != "" {
			out = append(out, cleaned)
		}
	}
	return out
}

func readsAsLabels(values []string) bool {
	filled, text := 0, 0
	for _, value := range values {
		cleaned := clean(value)
		if cleaned == "" {
			continue
		}
		filled++
		if _, isNumber := parseNumber(cleaned); !isNumber {
			text++
		}
	}
	return filled > 0 && float64(text) >= float64(filled)*headerTextShare
}

// parseNumber accepts both decimal conventions. A spreadsheet cell arrives
// canonicalised ("1234.56"), but a CSV written in Italy says "1.234,56", and
// reading that as text would put every amount in the wrong column narration.
// isZeroPadded reports whether value writes its integer part with a leading zero, as
// 004040 or 03 do and as 0, 0.5 and -0.75 do not.
func isZeroPadded(value string) bool {
	digits := strings.TrimLeft(value, "+-")
	if len(digits) < 2 || digits[0] != '0' {
		return false
	}
	return digits[1] >= '0' && digits[1] <= '9'
}

func parseNumber(value string) (float64, bool) {
	if number, err := strconv.ParseFloat(value, 64); err == nil {
		return number, true
	}
	if strings.Count(value, ",") == 1 {
		candidate := strings.ReplaceAll(value, ".", "")
		candidate = strings.Replace(candidate, ",", ".", 1)
		if number, err := strconv.ParseFloat(candidate, 64); err == nil {
			return number, true
		}
	}
	return 0, false
}

func formatNumber(value float64) string {
	return strconv.FormatFloat(value, 'f', -1, 64)
}

func truncateRunes(value string, limit int) string {
	if utf8.RuneCountInString(value) <= limit {
		return value
	}
	runes := []rune(value)
	return strings.TrimSpace(string(runes[:limit])) + "…"
}

// codeColumns names the columns whose values are fixed-width codes, in sheet order.
func codeColumns(sheet Sheet) []string {
	var names []string
	for _, column := range sheet.Columns {
		if column.Type == "code" && column.Header != "" {
			names = append(names, column.Header)
		}
	}
	return names
}

// tableCaveats states what a reader has to do differently with this sheet, as opposed to
// what it contains. Both come from what the scan already measured, and both were watched
// costing a live agent an answer on 2026-09-09: it converted a code column with pandas'
// inferred dtypes and lost the leading zeros, and it took three tries to find the header
// under a one-line banner.
func tableCaveats(sheet Sheet) []string {
	var caveats []string
	if names := codeColumns(sheet); len(names) > 0 {
		caveats = append(caveats, fmt.Sprintf(
			"Read %s as text: they hold fixed-width codes, and any numeric conversion "+
				"drops the leading zeros that make them valid.", strings.Join(names, ", ")))
	}
	if sheet.HeaderRow > 1 {
		above := "the line above them is a banner"
		if sheet.HeaderRow > 2 {
			above = fmt.Sprintf("the %d lines above them are a banner", sheet.HeaderRow-1)
		}
		caveats = append(caveats, fmt.Sprintf(
			"The column names are on row %d; %s, not data.", sheet.HeaderRow, above))
	}
	return caveats
}
