package telegram

// Atomic tables layer.
//
// GFM pipe tables get a Telegram-friendly rendering BEFORE the rest of the
// markdown is handed to telegramify-markdown-go. The library renders tables
// as an ASCII grid wrapped in a `pre` (code-block) entity, which produces
// unreadable horizontal-scroll blocks on mobile clients. We replace each
// table with a placeholder, let tgmd convert the surrounding markdown, then
// splice in a hand-rendered version sized for the table's shape:
//
//   shapeKV         (2 cols)              → "• **key**: value" bullets
//   shapeComparison (3-4 cols, few rows)  → per-row blocks with bold title
//   shapeWide       (everything else)     → ASCII grid wrapped in an
//                                            expandable_blockquote entity
//                                            (telebot v4 EntityEBlockquote)
//
// The placeholder is a unique ASCII token on its own paragraph, so tgmd
// preserves it as plain text and never splits messages mid-token.

import (
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	tgmd "github.com/eekstunt/telegramify-markdown-go"
)

// tableShape decides how a parsed table is rendered.
type tableShape int

const (
	shapeKV tableShape = iota
	shapeComparison
	shapeWide
)

// extractedTable is a parsed GFM table.
type extractedTable struct {
	headers []string
	rows    [][]string
}

// tableMatch is a parsed table plus its byte span in the original markdown.
type tableMatch struct {
	startByte int // inclusive
	endByte   int // exclusive
	table     extractedTable
}

// atomicRendering is a hand-built (text, entities) pair that gets spliced
// into the tgmd output at the placeholder location.
type atomicRendering struct {
	text     string
	entities []tgmd.Entity
}

// Type string used for the expandable blockquote entity. telebot v4
// translates this directly to EntityEBlockquote. tgmd's own constant set
// doesn't include it, so we use the raw type string.
const expandableBlockquoteType tgmd.EntityType = "expandable_blockquote"

// Classification knobs. The thresholds are conservative on purpose — when
// in doubt, fall back to shapeWide (expandable blockquote) so we never make
// the output worse than the pre-existing pre/ASCII rendering.
const (
	maxKVRows                = 20
	maxComparisonCols        = 4
	maxComparisonRows        = 8
	maxComparisonCellRuneLen = 20
)

// rewriteAtomicTables scans md for GFM pipe tables and replaces each one
// with a unique placeholder. Returns the rewritten markdown plus a map
// from placeholder → parsed table for the post-tgmd splice step. If no
// tables are present, returns (md, nil) so the caller can short-circuit.
func rewriteAtomicTables(md string) (string, map[string]extractedTable) {
	matches := findTables(md)
	if len(matches) == 0 {
		return md, nil
	}
	tables := make(map[string]extractedTable, len(matches))
	var b strings.Builder
	cursor := 0
	for i, m := range matches {
		b.WriteString(md[cursor:m.startByte])
		ph := placeholderFor(i)
		// Wrap placeholder in blank lines so tgmd parses it as its own
		// paragraph and won't merge it with surrounding text.
		b.WriteString("\n\n")
		b.WriteString(ph)
		b.WriteString("\n\n")
		tables[ph] = m.table
		cursor = m.endByte
	}
	b.WriteString(md[cursor:])
	return b.String(), tables
}

// placeholderFor returns the unique placeholder token for the i-th table.
// Format chosen to be (a) ASCII so byte/rune/UTF-16 lengths match, (b)
// extremely unlikely to appear in natural LLM output, (c) easy to scan for
// after tgmd conversion.
func placeholderFor(i int) string {
	return fmt.Sprintf("AURATBL%04dEND", i)
}

// applyAtomicTables splices each placeholder in msg with the hand-rendered
// version of its table. Adjusts entity offsets so the resulting message is
// still valid for the Telegram Bot API.
func applyAtomicTables(msg tgmd.Message, tables map[string]extractedTable) tgmd.Message {
	if len(tables) == 0 {
		return msg
	}
	for ph, tbl := range tables {
		for {
			idx := strings.Index(msg.Text, ph)
			if idx < 0 {
				break
			}
			rendering := renderTable(tbl)
			msg = spliceRendering(msg, idx, len(ph), rendering)
		}
	}
	return msg
}

// renderTable picks the right strategy and renders the table. Empty
// tables (no body rows) collapse to an empty rendering so the surrounding
// placeholder gets erased cleanly.
func renderTable(t extractedTable) atomicRendering {
	if len(t.rows) == 0 {
		return atomicRendering{}
	}
	switch classifyTable(t) {
	case shapeKV:
		return renderKV(t)
	case shapeComparison:
		return renderComparison(t)
	default:
		return renderWide(t)
	}
}

// classifyTable picks a rendering strategy based on the table's dimensions
// and cell sizes. The order is important: narrow tables get KV first, then
// short-cell comparison, then wide as the catch-all.
func classifyTable(t extractedTable) tableShape {
	cols := len(t.headers)
	rows := len(t.rows)
	if cols <= 2 && rows <= maxKVRows {
		return shapeKV
	}
	if cols <= maxComparisonCols && rows <= maxComparisonRows {
		maxCell := 0
		for _, row := range t.rows {
			for _, c := range row {
				if w := utf8.RuneCountInString(c); w > maxCell {
					maxCell = w
				}
			}
		}
		if maxCell <= maxComparisonCellRuneLen {
			return shapeComparison
		}
	}
	return shapeWide
}

// renderKV produces "• **key**: value" bullets. Headers are dropped — for
// 2-col tables they're almost always uninformative ("Name | Value",
// "Key | Description") and bullet output already self-documents.
func renderKV(t extractedTable) atomicRendering {
	var sb strings.Builder
	var entities []tgmd.Entity
	for _, row := range t.rows {
		if len(row) == 0 {
			continue
		}
		key := strings.TrimSpace(row[0])
		val := ""
		if len(row) > 1 {
			val = strings.TrimSpace(row[1])
		}
		if key == "" && val == "" {
			continue
		}
		sb.WriteString("• ")
		keyOffset := tgmd.UTF16Len(sb.String())
		sb.WriteString(key)
		entities = append(entities, tgmd.Entity{
			Type:   tgmd.Bold,
			Offset: keyOffset,
			Length: tgmd.UTF16Len(key),
		})
		if val != "" {
			sb.WriteString(": ")
			sb.WriteString(val)
		}
		sb.WriteString("\n")
	}
	return atomicRendering{
		text:     strings.TrimRight(sb.String(), "\n"),
		entities: entities,
	}
}

// renderComparison produces per-row blocks: first cell becomes a bold
// title, remaining cells become "header: value" lines indented under it.
func renderComparison(t extractedTable) atomicRendering {
	var sb strings.Builder
	var entities []tgmd.Entity
	for r, row := range t.rows {
		if len(row) == 0 {
			continue
		}
		if r > 0 {
			sb.WriteString("\n")
		}
		title := strings.TrimSpace(row[0])
		sb.WriteString("▸ ")
		titleOffset := tgmd.UTF16Len(sb.String())
		sb.WriteString(title)
		entities = append(entities, tgmd.Entity{
			Type:   tgmd.Bold,
			Offset: titleOffset,
			Length: tgmd.UTF16Len(title),
		})
		sb.WriteString("\n")
		for j := 1; j < len(t.headers) && j < len(row); j++ {
			header := strings.TrimSpace(t.headers[j])
			value := strings.TrimSpace(row[j])
			sb.WriteString("   ")
			hOffset := tgmd.UTF16Len(sb.String())
			sb.WriteString(header)
			entities = append(entities, tgmd.Entity{
				Type:   tgmd.Italic,
				Offset: hOffset,
				Length: tgmd.UTF16Len(header),
			})
			sb.WriteString(": ")
			sb.WriteString(value)
			sb.WriteString("\n")
		}
	}
	return atomicRendering{
		text:     strings.TrimRight(sb.String(), "\n"),
		entities: entities,
	}
}

// renderWide builds an ASCII grid and wraps the whole thing in an
// expandable_blockquote entity. Telegram shows the first ~3 lines and a
// "click to expand" affordance, keeping scrolling tractable.
func renderWide(t extractedTable) atomicRendering {
	grid := buildASCIIGrid(t)
	return atomicRendering{
		text: grid,
		entities: []tgmd.Entity{
			{
				Type:   expandableBlockquoteType,
				Offset: 0,
				Length: tgmd.UTF16Len(grid),
			},
		},
	}
}

// buildASCIIGrid renders a table as a uniform-width ASCII grid. Cell width
// is computed in runes — wide characters (CJK, emoji) may render slightly
// misaligned, but expandable_blockquote already softens that vs. the
// previous pre-block behavior.
func buildASCIIGrid(t extractedTable) string {
	cols := len(t.headers)
	if cols == 0 {
		return ""
	}
	colWidths := make([]int, cols)
	for j, h := range t.headers {
		colWidths[j] = utf8.RuneCountInString(h)
	}
	for _, row := range t.rows {
		for j := 0; j < cols && j < len(row); j++ {
			if w := utf8.RuneCountInString(row[j]); w > colWidths[j] {
				colWidths[j] = w
			}
		}
	}
	var sb strings.Builder
	writeRow := func(cells []string) {
		sb.WriteString("| ")
		for j := 0; j < cols; j++ {
			cell := ""
			if j < len(cells) {
				cell = cells[j]
			}
			pad := colWidths[j] - utf8.RuneCountInString(cell)
			if pad < 0 {
				pad = 0
			}
			sb.WriteString(cell)
			sb.WriteString(strings.Repeat(" ", pad))
			if j < cols-1 {
				sb.WriteString(" | ")
			}
		}
		sb.WriteString(" |\n")
	}
	writeRow(t.headers)
	sb.WriteString("|-")
	for j := 0; j < cols; j++ {
		sb.WriteString(strings.Repeat("-", colWidths[j]))
		if j < cols-1 {
			sb.WriteString("-+-")
		}
	}
	sb.WriteString("-|\n")
	for _, row := range t.rows {
		writeRow(row)
	}
	return strings.TrimRight(sb.String(), "\n")
}

// spliceRendering replaces a placeholder at byteIdx with rendering.text and
// shifts entity offsets accordingly. Entities that overlap the placeholder
// span (which shouldn't happen if the placeholder is preserved as plain
// text) are dropped defensively.
func spliceRendering(msg tgmd.Message, byteIdx, phByteLen int, rendering atomicRendering) tgmd.Message {
	prefix := msg.Text[:byteIdx]
	phText := msg.Text[byteIdx : byteIdx+phByteLen]
	suffix := msg.Text[byteIdx+phByteLen:]

	prefixUTF16 := tgmd.UTF16Len(prefix)
	phUTF16 := tgmd.UTF16Len(phText)
	renderingUTF16 := tgmd.UTF16Len(rendering.text)
	delta := renderingUTF16 - phUTF16

	newText := prefix + rendering.text + suffix
	newEntities := make([]tgmd.Entity, 0, len(msg.Entities)+len(rendering.entities))

	for _, e := range msg.Entities {
		end := e.Offset + e.Length
		if end <= prefixUTF16 {
			newEntities = append(newEntities, e)
			continue
		}
		if e.Offset >= prefixUTF16+phUTF16 {
			e.Offset += delta
			newEntities = append(newEntities, e)
			continue
		}
		// Overlap with placeholder span: drop. Should be unreachable
		// because the placeholder is a single ASCII paragraph token.
	}
	for _, e := range rendering.entities {
		e.Offset += prefixUTF16
		newEntities = append(newEntities, e)
	}
	return tgmd.Message{Text: newText, Entities: newEntities}
}

// --- GFM table parser -------------------------------------------------------

var (
	tableLineRE = regexp.MustCompile(`^\s*\|.*\|\s*$`)
	tableSepRE  = regexp.MustCompile(`^\s*\|(\s*:?-+:?\s*\|)+\s*$`)
)

// findTables scans md line-by-line for GFM pipe tables. A table is at
// least a header row + a separator row matching tableSepRE with the same
// column count; any number of body rows follow until the first non-pipe
// line. Returns matches in source order.
func findTables(md string) []tableMatch {
	if !strings.Contains(md, "|") {
		return nil
	}
	lines := strings.SplitAfter(md, "\n")
	var matches []tableMatch

	// Pre-compute byte offsets per line for accurate (start, end) reporting.
	offsets := make([]int, len(lines)+1)
	for i, ln := range lines {
		offsets[i+1] = offsets[i] + len(ln)
	}

	i := 0
	for i < len(lines) {
		headerLine := stripNewline(lines[i])
		if !tableLineRE.MatchString(headerLine) {
			i++
			continue
		}
		if i+1 >= len(lines) {
			i++
			continue
		}
		sepLine := stripNewline(lines[i+1])
		if !tableSepRE.MatchString(sepLine) {
			i++
			continue
		}
		headers := parsePipeRow(headerLine)
		sepCols := countPipeCells(sepLine)
		if len(headers) == 0 || len(headers) != sepCols {
			i++
			continue
		}
		// Collect body rows.
		j := i + 2
		var rows [][]string
		for j < len(lines) {
			line := stripNewline(lines[j])
			if !tableLineRE.MatchString(line) {
				break
			}
			// Defensive: don't swallow a second header/separator pair as
			// a data row — if we hit another separator-like line, stop.
			if tableSepRE.MatchString(line) {
				break
			}
			row := parsePipeRow(line)
			// Pad short rows so callers don't index-out-of-range.
			for len(row) < len(headers) {
				row = append(row, "")
			}
			if len(row) > len(headers) {
				row = row[:len(headers)]
			}
			rows = append(rows, row)
			j++
		}
		// Header + separator with no body is still consumed (and erased
		// from the markdown) — otherwise tgmd would render the leftover
		// header/separator lines as a pre code block.
		matches = append(matches, tableMatch{
			startByte: offsets[i],
			endByte:   offsets[j],
			table: extractedTable{
				headers: headers,
				rows:    rows,
			},
		})
		i = j
	}
	return matches
}

// parsePipeRow splits a GFM table row into trimmed cells. Leading/trailing
// pipes are dropped; empty leading/trailing cells produced by surrounding
// pipes are discarded; escaped pipes `\|` survive as literal `|` inside
// cells.
func parsePipeRow(line string) []string {
	line = strings.TrimSpace(line)
	// Split on unescaped `|`, then trim. Walk byte-by-byte because escape
	// processing is cleaner than a regex here.
	var cells []string
	var cur strings.Builder
	for i := 0; i < len(line); i++ {
		c := line[i]
		if c == '\\' && i+1 < len(line) && line[i+1] == '|' {
			cur.WriteByte('|')
			i++
			continue
		}
		if c == '|' {
			cells = append(cells, strings.TrimSpace(cur.String()))
			cur.Reset()
			continue
		}
		cur.WriteByte(c)
	}
	if cur.Len() > 0 {
		cells = append(cells, strings.TrimSpace(cur.String()))
	}
	// Drop the empty cells produced by leading/trailing pipes (the GFM
	// convention is `| a | b |` which yields ["", "a", "b", ""]).
	if len(cells) > 0 && cells[0] == "" {
		cells = cells[1:]
	}
	if len(cells) > 0 && cells[len(cells)-1] == "" {
		cells = cells[:len(cells)-1]
	}
	return cells
}

// countPipeCells counts cells in a separator-like row by reusing parsePipeRow.
// Separator rows can't contain escaped pipes but we treat them uniformly.
func countPipeCells(line string) int {
	return len(parsePipeRow(line))
}

// stripNewline trims a trailing \n (and optional \r) from a line as
// produced by SplitAfter. Keeps interior whitespace intact.
func stripNewline(line string) string {
	line = strings.TrimSuffix(line, "\n")
	line = strings.TrimSuffix(line, "\r")
	return line
}
