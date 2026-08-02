package filecard

import (
	"bufio"
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"strings"
)

// maxCSVBytes bounds a delimited read. The row cap alone is not a byte cap: one
// row of a machine-generated export can be a megabyte.
const maxCSVBytes = 64 << 20

// buildCSV describes a delimited file with the same table card a worksheet gets.
// The row count is exact only when the whole file was read; when the cap bites,
// the card says the file was sampled instead of reporting the sample as the total.
func buildCSV(req Request, card Card) (Card, error) {
	file, err := os.Open(req.Path)
	if err != nil {
		return card, fmt.Errorf("open table: %w", err)
	}
	defer func() { _ = file.Close() }()

	buffered := bufio.NewReaderSize(io.LimitReader(file, maxCSVBytes), 64<<10)
	head, _ := buffered.Peek(8 << 10)
	head = trimBOM(head)
	reader := csv.NewReader(buffered)
	reader.Comma = sniffDelimiter(string(head))
	// A real-world export is not RFC 4180: rows have different widths and quotes
	// appear inside unquoted fields. Refusing to read such a file would leave it
	// with no card at all, which is the outcome this whole change exists to end.
	reader.FieldsPerRecord = -1
	reader.LazyQuotes = true
	reader.ReuseRecord = true

	table := newTable(strings.TrimSuffix(req.FileName, ".csv"))
	rows := 0
	capped := false
	for {
		record, err := reader.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			if rows == 0 {
				return card, fmt.Errorf("read table: %w", err)
			}
			card.Caveats = append(card.Caveats, fmt.Sprintf("The file stopped parsing after %d rows (%v).", rows, err))
			break
		}
		if rows == 0 && len(record) > 0 {
			record[0] = string(trimBOM([]byte(record[0])))
		}
		rows++
		table.addRow(record)
		if rows >= maxRowsScanned+headerProbeRows {
			capped = true
			break
		}
	}
	if rows == 0 {
		card.Caveats = append(card.Caveats, "The file held no rows at ingest.")
		return card, nil
	}
	sheet := table.done(0)
	card.Sheets = append(card.Sheets, sheet)
	card.Facts = append(card.Facts, fmt.Sprintf("%d rows x %d columns", sheet.Rows, len(sheet.Columns)))
	if capped {
		card.Caveats = append(card.Caveats, fmt.Sprintf(
			"The file was sampled: the first %d rows were read, and it has more.", sheet.RowsScanned))
	}
	return card, nil
}

// sniffDelimiter picks the separator by counting candidates in the first block.
// Comma is not a safe default on this corpus: an Italian export written by Excel
// separates with semicolons, and read as commas every row becomes one column.
func sniffDelimiter(head string) rune {
	best, bestCount := ',', 0
	for _, candidate := range []rune{',', ';', '\t', '|'} {
		if count := strings.Count(head, string(candidate)); count > bestCount {
			best, bestCount = candidate, count
		}
	}
	return best
}

func trimBOM(data []byte) []byte {
	if len(data) >= 3 && data[0] == 0xEF && data[1] == 0xBB && data[2] == 0xBF {
		return data[3:]
	}
	return data
}
