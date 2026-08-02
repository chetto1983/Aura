// Package filecard writes the card that says WHICH file this is.
//
// A card is built AT INGEST, from the file itself, with no LLM and no network.
// It exists because the alternative was a library that only knew about documents
// somebody had already found another way: the description used to be written by
// the agent through document_describe, after it had opened a file, so a file
// nobody ever opened was invisible to document_search forever. At tens of files
// an operator absorbs that; at thousands it is not a library.
//
// What earns a place in a card is settled by measurement, not taste (see
// docs/recon-2026-08-02-document-indexing-at-scale.md):
//   - a descriptive title is the single biggest lever (+0.42 recall@3, Table
//     Serialization Kitchen on TARGET) — we derive one mechanically where the file
//     carries one, and leave the LLM-written kind on the table on purpose;
//   - column narrations beat bare values (Pneuma, arXiv 2504.09207: 81.0% vs 67.9%);
//   - whole sampled rows as `header: value` beat a bag of cells, because "76"
//     alone is ambiguous and "age: 76" is not (Pneuma, r=5 by default);
//   - when a column has more values than fit, the established selection criterion
//     is FREQUENCY: Postgres' own ANALYZE keeps most_common_vals (top-N by
//     frequency) plus a disjoint sketch of the tail, and that is the model copied
//     here. Nothing in the retrieval literature selects values alphabetically,
//     which is exactly what the spike card builder did.
//
// A card is a proxy and says so in its own last line. The file is one
// document_open away and that is where computation happens.
package filecard

import (
	"fmt"
	"strings"
)

// MaxCardChars caps the rendered card. It matches the cap document_describe
// enforces on the agent's own note: a card is a filing-cabinet label, and the
// embedder we route with reads 2048 tokens, past which more content measurably
// HURTS short-context models. Richer has to mean better-selected, not bigger.
const MaxCardChars = 4000

// Kind is the one-word file kind a card leads with.
type Kind string

// The file kinds a card can carry. Anything unrecognized is KindFile.
const (
	KindSpreadsheet  Kind = "spreadsheet"
	KindTable        Kind = "table"
	KindDocument     Kind = "document"
	KindPresentation Kind = "presentation"
	KindBook         Kind = "book"
	KindPage         Kind = "web page"
	KindText         Kind = "text file"
	KindPDF          Kind = "PDF"
	KindImage        Kind = "image"
	KindFile         Kind = "file"
)

// Card is what the machine can say about a file without understanding it.
type Card struct {
	FileName  string
	Kind      Kind
	SizeBytes int64
	// Title is a description the FILE carries — OOXML core properties, PDF
	// metadata, a first heading. It is never the filename reworded: the filename
	// is already indexed at weight A and split into words by aura.searchable_text,
	// so repeating it buys nothing.
	Title string
	// Facts are short structural statements: sheet counts, row counts, pages.
	Facts []string
	// Sheets carry tabular structure for spreadsheets and delimited files.
	Sheets []Sheet
	// Text carries extracted prose: headings first, then opening words.
	Text []string
	// Caveats state what the card does NOT contain. A degraded card must say it
	// is degraded rather than look complete.
	Caveats []string
}

// Sheet is one tabular surface: a worksheet, or the single table in a CSV.
type Sheet struct {
	Name string
	// Rows is the sheet's declared data-row count, taken from the worksheet
	// dimension rather than from what was scanned, so a capped read still
	// reports the real size. Negative means unknown.
	Rows        int64
	RowsScanned int
	Columns     []Column
	Samples     []SampleRow
}

// Column is one column's narration: what kind of values it holds, how many
// distinct ones were seen, and the frequent ones.
type Column struct {
	Header   string
	Type     string
	NonEmpty int
	Distinct int
	// DistinctCapped reports that value tracking hit its per-column ceiling, so
	// Distinct is a floor rather than a count.
	DistinctCapped bool
	// Common holds the most frequent repeated values, top-N by count. A value
	// seen once is not a category and never lands here.
	Common []Value
	// Min and Max sketch the tail of a numeric column, the way a histogram
	// covers what Postgres' most_common_vals list does not.
	Min, Max string
}

// Value is one frequent value and how often it was seen.
type Value struct {
	Text  string
	Count int
}

// SampleRow is one whole row kept as header/value pairs.
type SampleRow []Cell

// Cell is one header/value pair inside a sampled row.
type Cell struct {
	Header string
	Value  string
}

const cardTrailer = "Written by Aura at ingest from the file itself: it describes the file and is not the file — " +
	"open it with document_open to read or compute on it."

// cardCutNotice is reserved out of the budget whether or not it is used. Sixty
// characters of a four-thousand-character card is a cheap way to make the cap a
// guarantee instead of an approximation — appending this line after spending the
// whole budget is how the card overran it.
const cardCutNotice = "The description was cut to fit; the file holds more than this."

// Render turns a card into the text the digest index holds.
//
// The budget is divided BETWEEN sheets rather than spent in file order. A
// workbook with seven sheets otherwise spends the whole card on the first two
// and never mentions the other five, which is the failure mode this card exists
// to end: a file the index cannot describe is a file the index cannot find.
// Within a sheet the order is column names, then column narrations, then sampled
// rows, so a tight allowance loses examples before it loses structure.
func (c Card) Render() string {
	budget := MaxCardChars - len(cardTrailer) - len(cardCutNotice) - 2
	head := c.headLines()
	tail := append(append([]string(nil), c.Text...), c.Caveats...)

	head, headSpent, headCut := takeLines(head, budget)
	tail, tailSpent, tailCut := takeLines(tail, budget-headSpent)
	room := budget - headSpent - tailSpent
	truncated := headCut || tailCut

	var sheets []string
	for i, sheet := range c.Sheets {
		lines := sheet.lines()
		allowance := room
		if left := len(c.Sheets) - i; left > 1 {
			allowance = max(room/left, min(room, len(lines[0])+1))
		}
		kept, spent, cut := takeLines(lines, allowance)
		sheets = append(sheets, kept...)
		room -= spent
		truncated = truncated || cut
	}

	var b strings.Builder
	for _, line := range append(append(head, sheets...), tail...) {
		b.WriteString(line)
		b.WriteByte('\n')
	}
	if truncated {
		b.WriteString(cardCutNotice)
		b.WriteByte('\n')
	}
	b.WriteString(cardTrailer)
	return b.String()
}

// takeLines keeps as many leading lines as fit in budget, reporting the bytes
// spent and whether anything was left behind.
func takeLines(lines []string, budget int) (kept []string, spent int, truncated bool) {
	for _, line := range lines {
		if spent+len(line)+1 > budget {
			return kept, spent, true
		}
		kept = append(kept, line)
		spent += len(line) + 1
	}
	return kept, spent, false
}

func (c Card) headLines() []string {
	lines := []string{c.headline()}
	if c.Title != "" {
		lines = append(lines, "Titled: "+c.Title)
	}
	return lines
}

func (c Card) headline() string {
	parts := []string{string(c.Kind)}
	if size := humanBytes(c.SizeBytes); size != "" {
		parts = append(parts, size)
	}
	parts = append(parts, c.Facts...)
	return fmt.Sprintf("%s — %s.", c.FileName, strings.Join(parts, ", "))
}

func (s Sheet) lines() []string {
	if len(s.Columns) == 0 {
		return []string{fmt.Sprintf("Sheet %q — no cell data.", s.Name)}
	}
	headers := make([]string, 0, len(s.Columns))
	for _, col := range s.Columns {
		if col.Header != "" {
			headers = append(headers, col.Header)
		}
	}
	head := fmt.Sprintf("Sheet %q — %d rows x %d columns", s.Name, s.Rows, len(s.Columns))
	if len(headers) > 0 {
		head += "; columns: " + strings.Join(headers, ", ")
	} else {
		head += "; the sheet has no header row, so its columns are unnamed"
	}
	lines := []string{head + "."}
	for i, col := range s.Columns {
		if line := col.line(i); line != "" {
			lines = append(lines, line)
		}
	}
	for _, sample := range s.Samples {
		if rendered := sample.line(); rendered != "" {
			lines = append(lines, "  row: "+rendered)
		}
	}
	return lines
}

func (c Column) line(index int) string {
	if c.NonEmpty == 0 {
		return ""
	}
	header := c.Header
	if header == "" {
		header = fmt.Sprintf("column %d", index+1)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "  %s: %s", header, c.Type)
	if c.DistinctCapped {
		fmt.Fprintf(&b, ", %d+ distinct values", c.Distinct)
	} else {
		fmt.Fprintf(&b, ", %s", plural(c.Distinct, "distinct value", "distinct values"))
	}
	if len(c.Common) > 0 {
		frequent := make([]string, 0, len(c.Common))
		for _, value := range c.Common {
			frequent = append(frequent, fmt.Sprintf("%s (%d)", value.Text, value.Count))
		}
		fmt.Fprintf(&b, ", most common %s", strings.Join(frequent, ", "))
	}
	if c.Min != "" && c.Max != "" {
		fmt.Fprintf(&b, ", from %s to %s", c.Min, c.Max)
	}
	return b.String() + "."
}

func (r SampleRow) line() string {
	pairs := make([]string, 0, len(r))
	for _, cell := range r {
		if cell.Value == "" {
			continue
		}
		if cell.Header == "" {
			pairs = append(pairs, cell.Value)
			continue
		}
		pairs = append(pairs, cell.Header+": "+cell.Value)
	}
	return strings.Join(pairs, " | ")
}

func humanBytes(size int64) string {
	switch {
	case size <= 0:
		return ""
	case size < 1024:
		return fmt.Sprintf("%d bytes", size)
	case size < 1024*1024:
		return fmt.Sprintf("%d KB", size/1024)
	default:
		return fmt.Sprintf("%d MB", size/(1024*1024))
	}
}
