package files

// DOCX generator. Pure-Go OOXML zip writer — no third-party dep.
//
// Why hand-roll: a basic DOCX is just a ZIP with three small XML parts
// ([Content_Types].xml, _rels/.rels, word/document.xml). The libraries
// for richer DOCX work in Go are either commercial-licensed
// (unidoc/unioffice's free tier still requires a UNI Cloud API key for
// some operations) or template-driven, which doesn't fit Aura's "LLM
// authors structured content from a JSON spec" shape. ~200 LOC here
// gets us heading/paragraph/bullet/table without any dep risk.
//
// Visual styling for headings is done via direct run formatting (bold +
// font size in half-points) rather than referencing the default
// "Heading1" style, so we don't need a /word/styles.xml. Word still
// recognizes the result as semantic headings on copy/paste.

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"strings"
)

// DOCXSpec describes a document the LLM (or any caller) wants generated.
// Blocks render in slice order; an empty Title means no top-of-document
// heading.
type DOCXSpec struct {
	// Filename is the user-visible name. The generator sanitizes path
	// separators and forces a .docx suffix; callers can pass "report"
	// and get back "report.docx".
	Filename string
	// Title renders as an H1 at the top when non-empty; otherwise the
	// document opens with the first block.
	Title string
	// Blocks is the body. Empty docs are rejected since they're almost
	// always LLM mistakes.
	Blocks []DOCXBlock
}

// DOCXBlock is one structural element. Discriminated by Kind:
//   - "heading":   H1–H6 via Level (clamped 1..6); Text is the heading text.
//   - "paragraph": plain paragraph; Text is the body.
//   - "bullet":    rendered with a "• " prefix on a normal paragraph
//                  (avoids needing a /word/numbering.xml definition).
//   - "table":     Rows is a 2-D slice; row[i][j] is the cell at row i, col j.
type DOCXBlock struct {
	Kind  string     // heading | paragraph | bullet | table
	Level int        // for heading: 1..6
	Text  string     // for heading/paragraph/bullet
	Rows  [][]string // for table
}

// Generation caps. Picked the same way xlsx caps were — fits comfortably
// under Telegram's 50 MB document cap with headroom for prose docs.
const (
	MaxDOCXBlocks    = 1000
	MaxDOCXTableRows = 500
	MaxDOCXTableCols = 50
	MaxDOCXTextLen   = 50000 // per-block text cap; runaway LLM output guard
	MaxDOCXBytes     = 25 * 1024 * 1024
)

const (
	docContentTypes = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
<Default Extension="xml" ContentType="application/xml"/>
<Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>
</Types>`
	docRels = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/>
</Relationships>`
	docHeader = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>` +
		`<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">` +
		`<w:body>`
	docFooter = `<w:sectPr><w:pgSz w:w="12240" w:h="15840"/>` +
		`<w:pgMar w:top="1440" w:right="1440" w:bottom="1440" w:left="1440" w:header="720" w:footer="720" w:gutter="0"/>` +
		`</w:sectPr>` +
		`</w:body></w:document>`
)

// SanitizeDOCXFilename mirrors SanitizeFilename but forces .docx.
func SanitizeDOCXFilename(in string) string {
	clean := strings.TrimSpace(in)
	if idx := strings.LastIndexAny(clean, `/\`); idx >= 0 {
		clean = clean[idx+1:]
	}
	clean = invalidFilenameChar.ReplaceAllString(clean, "")
	clean = strings.TrimSpace(clean)
	clean = strings.TrimRightFunc(clean, trimDotsAndSpaces)
	lower := strings.ToLower(clean)
	if !strings.HasSuffix(lower, ".docx") {
		clean = strings.TrimSuffix(clean, ".") + ".docx"
	}
	if clean == ".docx" || clean == "" {
		clean = "document.docx"
	}
	if len(clean) > MaxFilenameLen {
		stem := strings.TrimSuffix(clean, ".docx")
		keep := max(MaxFilenameLen-len(".docx"), 1)
		if len(stem) > keep {
			stem = stem[:keep]
		}
		clean = stem + ".docx"
	}
	return clean
}

// BuildDOCX renders spec into a docx byte stream. Returns the rendered
// bytes plus the canonicalized filename.
func BuildDOCX(spec DOCXSpec) ([]byte, string, error) {
	if spec.Title == "" && len(spec.Blocks) == 0 {
		return nil, "", errors.New("docx: spec must contain a title or at least one block")
	}
	if len(spec.Blocks) > MaxDOCXBlocks {
		return nil, "", fmt.Errorf("docx: too many blocks (%d > %d)", len(spec.Blocks), MaxDOCXBlocks)
	}

	var body strings.Builder
	body.Grow(4096)
	body.WriteString(docHeader)

	if spec.Title != "" {
		writeHeading(&body, 1, spec.Title)
	}

	for i, b := range spec.Blocks {
		if len(b.Text) > MaxDOCXTextLen {
			return nil, "", fmt.Errorf("docx: block %d text exceeds %d chars", i, MaxDOCXTextLen)
		}
		switch b.Kind {
		case "heading":
			lvl := min(max(b.Level, 1), 6)
			writeHeading(&body, lvl, b.Text)
		case "paragraph", "":
			writeParagraph(&body, b.Text)
		case "bullet":
			// Plain "• " prefix avoids needing a numbering definition.
			writeParagraph(&body, "• "+b.Text)
		case "table":
			if len(b.Rows) > MaxDOCXTableRows {
				return nil, "", fmt.Errorf("docx: block %d table has %d rows (max %d)", i, len(b.Rows), MaxDOCXTableRows)
			}
			for j, row := range b.Rows {
				if len(row) > MaxDOCXTableCols {
					return nil, "", fmt.Errorf("docx: block %d table row %d has %d cols (max %d)", i, j, len(row), MaxDOCXTableCols)
				}
			}
			writeTable(&body, b.Rows)
		default:
			return nil, "", fmt.Errorf("docx: block %d has unknown kind %q (want heading|paragraph|bullet|table)", i, b.Kind)
		}
	}

	body.WriteString(docFooter)

	zipped, err := zipDOCX(body.String())
	if err != nil {
		return nil, "", fmt.Errorf("docx: zip: %w", err)
	}
	if len(zipped) > MaxDOCXBytes {
		return nil, "", fmt.Errorf("docx: rendered %d bytes exceeds cap %d", len(zipped), MaxDOCXBytes)
	}
	return zipped, SanitizeDOCXFilename(spec.Filename), nil
}

// trimDotsAndSpaces is shared with the xlsx variant via SanitizeFilename
// but redeclared here so a future split into per-file packages doesn't
// require a refactor.
func trimDotsAndSpaces(r rune) bool {
	return r == '.' || r == ' ' || r == '\t' || r == '\n' || r == '\r'
}

// writeHeading emits a paragraph with bold + sized run formatting that
// visually mimics Word's default Heading1..6. Sizes are in half-points
// (Word convention): H1=36 (18pt) down to H6=22 (11pt).
func writeHeading(b *strings.Builder, level int, text string) {
	sizes := map[int]int{1: 36, 2: 32, 3: 28, 4: 26, 5: 24, 6: 22}
	sz := sizes[level]
	b.WriteString(`<w:p><w:pPr><w:pStyle w:val="Heading`)
	b.WriteString(itoa(level))
	b.WriteString(`"/></w:pPr><w:r><w:rPr><w:b/><w:sz w:val="`)
	b.WriteString(itoa(sz))
	b.WriteString(`"/></w:rPr><w:t xml:space="preserve">`)
	xmlEscape(b, text)
	b.WriteString(`</w:t></w:r></w:p>`)
}

// writeParagraph emits a plain text paragraph. Empty text still emits a
// paragraph (Word treats it as a blank line) so the LLM can space its
// content explicitly.
func writeParagraph(b *strings.Builder, text string) {
	b.WriteString(`<w:p><w:r><w:t xml:space="preserve">`)
	xmlEscape(b, text)
	b.WriteString(`</w:t></w:r></w:p>`)
}

// writeTable emits a basic bordered table. Cells contain a single
// paragraph each. Empty rows ([]) are rendered as a single empty cell to
// keep the underlying OOXML well-formed.
func writeTable(b *strings.Builder, rows [][]string) {
	if len(rows) == 0 {
		return
	}
	// Compute width per col by taking the widest row's column count.
	cols := 0
	for _, r := range rows {
		if len(r) > cols {
			cols = len(r)
		}
	}
	if cols == 0 {
		cols = 1
	}
	b.WriteString(`<w:tbl>`)
	b.WriteString(`<w:tblPr><w:tblW w:w="5000" w:type="pct"/>`)
	b.WriteString(`<w:tblBorders>`)
	for _, side := range []string{"top", "left", "bottom", "right", "insideH", "insideV"} {
		b.WriteString(`<w:`)
		b.WriteString(side)
		b.WriteString(` w:val="single" w:sz="4" w:space="0" w:color="auto"/>`)
	}
	b.WriteString(`</w:tblBorders></w:tblPr>`)
	b.WriteString(`<w:tblGrid>`)
	for i := 0; i < cols; i++ {
		b.WriteString(`<w:gridCol/>`)
	}
	b.WriteString(`</w:tblGrid>`)
	for _, row := range rows {
		b.WriteString(`<w:tr>`)
		for c := 0; c < cols; c++ {
			b.WriteString(`<w:tc><w:tcPr><w:tcW w:w="0" w:type="auto"/></w:tcPr>`)
			cell := ""
			if c < len(row) {
				cell = row[c]
			}
			writeParagraph(b, cell)
			b.WriteString(`</w:tc>`)
		}
		b.WriteString(`</w:tr>`)
	}
	b.WriteString(`</w:tbl>`)
}

// xmlEscape writes the text into b with XML reserved chars (& < > " ')
// escaped. Uses xml.EscapeText for correctness — DOCX consumers (Word,
// LibreOffice) will refuse to open a file with raw < or & in <w:t>
// content.
func xmlEscape(b *strings.Builder, text string) {
	var buf bytes.Buffer
	_ = xml.EscapeText(&buf, []byte(text))
	b.Write(buf.Bytes())
}

func itoa(n int) string {
	return fmt.Sprintf("%d", n)
}

// zipDOCX wraps the three OOXML parts into a docx (zip) container.
// Order of writes doesn't matter functionally; we follow the typical
// convention seen in real .docx files for forward-compat with older
// readers.
func zipDOCX(body string) ([]byte, error) {
	var out bytes.Buffer
	zw := zip.NewWriter(&out)
	parts := []struct {
		name, content string
	}{
		{"[Content_Types].xml", docContentTypes},
		{"_rels/.rels", docRels},
		{"word/document.xml", body},
	}
	for _, p := range parts {
		w, err := zw.Create(p.name)
		if err != nil {
			return nil, err
		}
		if _, err := w.Write([]byte(p.content)); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}
