package files

// PDF generator. Pure-Go via go-pdf/fpdf — single dep, no Chrome, no
// external runtime. Same DSL shape as DOCXBlock (heading | paragraph |
// bullet | table) so the LLM can reuse one mental model across the
// three file formats.
//
// Layout: A4, 15 mm margins, Helvetica family. Headings render as
// bold + ramped sizes (H1=18pt down to H6=10pt) — visually mirrors the
// docx generator's heading sizing. Tables auto-size cell width across
// the available content width.
//
// fpdf is not zero-dep at runtime in the sense that it ships with no
// embedded fonts beyond the 14 PDF base fonts; using only Helvetica
// keeps the generated PDF self-contained (no font subset embedding).

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/go-pdf/fpdf"
)

// PDFSpec describes the document. Same shape as DOCXSpec for consistency
// — the LLM only has to learn one block grammar.
type PDFSpec struct {
	Filename string
	Title    string
	Blocks   []PDFBlock
}

// PDFBlock is one structural element. Discriminated by Kind:
//   - "heading":   H1–H6 via Level (clamped 1..6); Text is the heading text.
//   - "paragraph": plain paragraph; Text is the body.
//   - "bullet":    rendered with a "• " prefix on a normal paragraph.
//   - "table":     Rows is a 2-D slice; row[i][j] is the cell at row i, col j.
type PDFBlock struct {
	Kind  string
	Level int
	Text  string
	Rows  [][]string
}

const (
	MaxPDFBlocks    = 1000
	MaxPDFTableRows = 500
	MaxPDFTableCols = 20 // narrower than docx; PDF tables don't auto-wrap as gracefully
	MaxPDFTextLen   = 50000
	MaxPDFBytes     = 25 * 1024 * 1024
)

// SanitizePDFFilename forces .pdf suffix; same shape as
// SanitizeFilename / SanitizeDOCXFilename.
func SanitizePDFFilename(in string) string {
	clean := strings.TrimSpace(in)
	if idx := strings.LastIndexAny(clean, `/\`); idx >= 0 {
		clean = clean[idx+1:]
	}
	clean = invalidFilenameChar.ReplaceAllString(clean, "")
	clean = strings.TrimSpace(clean)
	clean = strings.TrimRightFunc(clean, trimDotsAndSpaces)
	lower := strings.ToLower(clean)
	if !strings.HasSuffix(lower, ".pdf") {
		clean = strings.TrimSuffix(clean, ".") + ".pdf"
	}
	if clean == ".pdf" || clean == "" {
		clean = "document.pdf"
	}
	if len(clean) > MaxFilenameLen {
		stem := strings.TrimSuffix(clean, ".pdf")
		keep := max(MaxFilenameLen-len(".pdf"), 1)
		if len(stem) > keep {
			stem = stem[:keep]
		}
		clean = stem + ".pdf"
	}
	return clean
}

// BuildPDF renders spec into a PDF byte stream. Returns the rendered
// bytes plus the canonicalized filename.
func BuildPDF(spec PDFSpec) ([]byte, string, error) {
	if spec.Title == "" && len(spec.Blocks) == 0 {
		return nil, "", errors.New("pdf: spec must contain a title or at least one block")
	}
	if len(spec.Blocks) > MaxPDFBlocks {
		return nil, "", fmt.Errorf("pdf: too many blocks (%d > %d)", len(spec.Blocks), MaxPDFBlocks)
	}

	pdf := fpdf.New("P", "mm", "A4", "")
	pdf.SetMargins(15, 15, 15)
	pdf.SetAutoPageBreak(true, 15)
	pdf.AddPage()
	pdf.SetFont("Helvetica", "", 11)

	// fpdf's standard fonts are Latin-1 (cp1252). Anything outside that
	// blows up at write time. Replace the most common offenders with
	// ASCII equivalents so the generator doesn't surprise-fail on
	// realistic LLM output (curly quotes, em-dashes, ellipses, NBSP).
	clean := func(s string) string { return latin1Sanitize(s) }

	if spec.Title != "" {
		writePDFHeading(pdf, 1, clean(spec.Title))
	}

	for i, b := range spec.Blocks {
		if utf8.RuneCountInString(b.Text) > MaxPDFTextLen {
			return nil, "", fmt.Errorf("pdf: block %d text exceeds %d chars", i, MaxPDFTextLen)
		}
		switch b.Kind {
		case "heading":
			lvl := min(max(b.Level, 1), 6)
			writePDFHeading(pdf, lvl, clean(b.Text))
		case "paragraph", "":
			writePDFParagraph(pdf, clean(b.Text))
		case "bullet":
			writePDFParagraph(pdf, "• "+clean(b.Text))
		case "table":
			if len(b.Rows) > MaxPDFTableRows {
				return nil, "", fmt.Errorf("pdf: block %d table has %d rows (max %d)", i, len(b.Rows), MaxPDFTableRows)
			}
			for j, row := range b.Rows {
				if len(row) > MaxPDFTableCols {
					return nil, "", fmt.Errorf("pdf: block %d table row %d has %d cols (max %d)", i, j, len(row), MaxPDFTableCols)
				}
			}
			cleaned := make([][]string, len(b.Rows))
			for r, row := range b.Rows {
				cleaned[r] = make([]string, len(row))
				for c, cell := range row {
					cleaned[r][c] = clean(cell)
				}
			}
			writePDFTable(pdf, cleaned)
		default:
			return nil, "", fmt.Errorf("pdf: block %d has unknown kind %q (want heading|paragraph|bullet|table)", i, b.Kind)
		}
	}

	if err := pdf.Error(); err != nil {
		return nil, "", fmt.Errorf("pdf: render: %w", err)
	}

	var buf bytes.Buffer
	if err := pdf.Output(&buf); err != nil {
		return nil, "", fmt.Errorf("pdf: serialize: %w", err)
	}
	if buf.Len() > MaxPDFBytes {
		return nil, "", fmt.Errorf("pdf: rendered %d bytes exceeds cap %d", buf.Len(), MaxPDFBytes)
	}
	return buf.Bytes(), SanitizePDFFilename(spec.Filename), nil
}

// writePDFHeading emits a heading line. Sizes ramp 18→10pt for H1→H6.
func writePDFHeading(pdf *fpdf.Fpdf, level int, text string) {
	sizes := map[int]float64{1: 18, 2: 16, 3: 14, 4: 13, 5: 12, 6: 10}
	sz := sizes[level]
	pdf.Ln(2)
	pdf.SetFont("Helvetica", "B", sz)
	pdf.MultiCell(0, sz*0.55, text, "", "L", false)
	pdf.SetFont("Helvetica", "", 11)
	pdf.Ln(1)
}

// writePDFParagraph emits a body paragraph with line wrapping.
func writePDFParagraph(pdf *fpdf.Fpdf, text string) {
	pdf.SetFont("Helvetica", "", 11)
	if text == "" {
		// Empty input still emits a vertical gap so the LLM can space
		// content explicitly with empty paragraph blocks.
		pdf.Ln(5)
		return
	}
	pdf.MultiCell(0, 5, text, "", "L", false)
	pdf.Ln(1)
}

// writePDFTable emits a basic bordered table. Column widths are
// computed from the page's printable width / column count.
func writePDFTable(pdf *fpdf.Fpdf, rows [][]string) {
	if len(rows) == 0 {
		return
	}
	cols := 0
	for _, r := range rows {
		if len(r) > cols {
			cols = len(r)
		}
	}
	if cols == 0 {
		cols = 1
	}
	pageW, _ := pdf.GetPageSize()
	left, _, right, _ := pdf.GetMargins()
	usable := pageW - left - right
	cw := usable / float64(cols)

	pdf.SetFont("Helvetica", "", 10)
	for r, row := range rows {
		// First row gets a subtle bold treatment as a header.
		if r == 0 {
			pdf.SetFont("Helvetica", "B", 10)
		}
		for c := 0; c < cols; c++ {
			cell := ""
			if c < len(row) {
				cell = row[c]
			}
			pdf.CellFormat(cw, 7, cell, "1", 0, "L", false, 0, "")
		}
		pdf.Ln(7)
		if r == 0 {
			pdf.SetFont("Helvetica", "", 10)
		}
	}
	pdf.SetFont("Helvetica", "", 11)
	pdf.Ln(2)
}

// latin1Sanitize replaces non-cp1252 runes with safe ASCII equivalents.
// fpdf's standard fonts only support Latin-1, so a curly quote or em
// dash from typical LLM output would either render as garbage or crash
// at output time. We map the most common offenders explicitly and drop
// anything else still outside cp1252 to "?". Bullet (•) is preserved
// because it's in cp1252.
func latin1Sanitize(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch r {
		case '‘', '’', '‚', '‛': // curly singles
			b.WriteByte('\'')
		case '“', '”', '„', '‟': // curly doubles
			b.WriteByte('"')
		case '–', '—': // en/em dash
			b.WriteByte('-')
		case '…': // horizontal ellipsis
			b.WriteString("...")
		case ' ', ' ', ' ': // various non-breaking/thin spaces
			b.WriteByte(' ')
		case '\t':
			b.WriteString("    ")
		default:
			if r < 0x100 {
				b.WriteRune(r)
			} else {
				b.WriteByte('?')
			}
		}
	}
	return b.String()
}
