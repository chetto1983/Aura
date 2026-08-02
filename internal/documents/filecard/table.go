package filecard

import (
	"sort"
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

	probe       [][]string
	headerFound bool
	rowsScanned int
	// rowsSkipped counts the leading rows that are not data — the banner lines
	// above the header, plus the header itself — so a declared total can be
	// turned into a data-row count.
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
	if !anyNonEmpty(values) {
		return
	}
	if !t.headerFound {
		t.probe = append(t.probe, clone(values))
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
		rows = max(declaredTotal-int64(t.rowsSkipped), 0)
	}
	sheet := Sheet{Name: t.name, Rows: rows, RowsScanned: t.rowsScanned}
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
			t.rowsSkipped++
			continue
		}
		t.consume(row)
	}
	t.probe = nil
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
	hasNum   bool
	minNum   float64
	maxNum   float64
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
	floor := max(2, c.nonEmpty/100)
	values := make([]Value, 0, len(c.counts))
	for text, count := range c.counts {
		if count < floor {
			continue
		}
		values = append(values, Value{Text: text, Count: count})
	}
	sort.Slice(values, func(i, j int) bool {
		if values[i].Count != values[j].Count {
			return values[i].Count > values[j].Count
		}
		return values[i].Text < values[j].Text
	})
	if len(values) > mcvPerColumn {
		values = values[:mcvPerColumn]
	}
	return values
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
