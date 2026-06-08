package telegram

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"strings"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gomono"
	"golang.org/x/image/font/gofont/gomonobold"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/math/fixed"
)

// errEmptyTable is returned by RenderTablePNG when the grid has no rows or no
// columns — the renderer (plan 13-05) treats it as "fall back to PreBlockTable",
// never a fatal error.
var errEmptyTable = errors.New("telegram: cannot render an empty table grid")

// Table-render constants (spike 018b winner). 28px = 14pt @2x survives
// Telegram's JPEG re-encode legibly; 16px cell padding; 1px black grid.
const (
	tableFontSize  = 28
	tableFontDPI   = 72
	cellPadX       = 16
	cellPadY       = 10
	gridLineWidth  = 1
	preBlockColGap = "  "
)

// ParseMarkdownTable detects a markdown table — a '|'-delimited header row, a
// '|---|' separator row, then N data rows — in s and returns the parsed cell
// grid (header row first, separator dropped). ok is false when s is not a
// well-formed table (no separator, fewer than one data row, or no '|' rows).
// Surrounding prose is ignored: the first contiguous header+separator+data run
// is parsed.
func ParseMarkdownTable(s string) (grid [][]string, ok bool) {
	lines := strings.Split(s, "\n")
	var rows [][]string
	sawSeparator := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if !strings.Contains(trimmed, "|") {
			if len(rows) > 0 {
				break // table ended; ignore trailing prose
			}
			continue
		}
		cells := splitRow(trimmed)
		if isSeparatorRow(cells) {
			sawSeparator = true
			continue
		}
		rows = append(rows, cells)
	}
	if !sawSeparator || len(rows) < 2 {
		// Need a separator and at least a header + one data row.
		return nil, false
	}
	return normalizeColumns(rows), true
}

// splitRow splits a '|'-delimited markdown row into trimmed cells, dropping the
// leading/trailing empty fields produced by the outer pipes.
func splitRow(line string) []string {
	parts := strings.Split(line, "|")
	if len(parts) > 0 && strings.TrimSpace(parts[0]) == "" {
		parts = parts[1:]
	}
	if len(parts) > 0 && strings.TrimSpace(parts[len(parts)-1]) == "" {
		parts = parts[:len(parts)-1]
	}
	cells := make([]string, len(parts))
	for i, p := range parts {
		cells[i] = strings.TrimSpace(p)
	}
	return cells
}

// isSeparatorRow reports whether every cell is a markdown separator cell
// (dashes with optional leading/trailing colons), e.g. "---", ":--", "--:".
func isSeparatorRow(cells []string) bool {
	if len(cells) == 0 {
		return false
	}
	for _, c := range cells {
		c = strings.TrimSpace(c)
		if c == "" {
			return false
		}
		body := strings.Trim(c, ":")
		if body == "" || strings.Trim(body, "-") != "" {
			return false
		}
	}
	return true
}

// normalizeColumns pads every row to the max column count so the grid is
// rectangular (a ragged model table never panics the renderer).
func normalizeColumns(rows [][]string) [][]string {
	cols := 0
	for _, r := range rows {
		if len(r) > cols {
			cols = len(r)
		}
	}
	for i, r := range rows {
		if len(r) < cols {
			padded := make([]string, cols)
			copy(padded, r)
			rows[i] = padded
		}
	}
	return rows
}

// RenderTablePNG renders the parsed grid to a deterministic gridded PNG: a
// gomonobold header row, gomono data cells, per-column width from
// font.Drawer.MeasureString + cellPadX padding, black 1px grid lines, white
// background, png.Encode. The same grid yields byte-identical bytes (embedded
// fonts, fixed metrics — no fontconfig, no CGO). This is the pure transform; the
// renderer (plan 13-05) decides PNG-vs-fallback and calls sendPhoto.
func RenderTablePNG(grid [][]string) ([]byte, error) {
	if len(grid) == 0 || len(grid[0]) == 0 {
		return nil, errEmptyTable
	}

	bodyFace, err := newFace(gomono.TTF)
	if err != nil {
		return nil, err
	}
	defer func() { _ = bodyFace.Close() }()
	headFace, err := newFace(gomonobold.TTF)
	if err != nil {
		return nil, err
	}
	defer func() { _ = headFace.Close() }()

	cols := len(grid[0])
	rows := len(grid)

	// Column widths: max measured advance across header (bold) + body cells.
	colWidth := make([]int, cols)
	for c := 0; c < cols; c++ {
		for r := 0; r < rows; r++ {
			f := bodyFace
			if r == 0 {
				f = headFace
			}
			w := measure(f, grid[r][c])
			if w > colWidth[c] {
				colWidth[c] = w
			}
		}
		colWidth[c] += 2 * cellPadX
	}

	// Row height from the face metrics, padded.
	metrics := bodyFace.Metrics()
	rowHeight := (metrics.Ascent + metrics.Descent).Ceil() + 2*cellPadY

	imgW := gridLineWidth
	for _, w := range colWidth {
		imgW += w + gridLineWidth
	}
	imgH := rows*(rowHeight+gridLineWidth) + gridLineWidth

	img := image.NewRGBA(image.Rect(0, 0, imgW, imgH))
	draw.Draw(img, img.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)

	black := image.NewUniform(color.Black)

	// Horizontal grid lines.
	for r := 0; r <= rows; r++ {
		y := r * (rowHeight + gridLineWidth)
		fillRect(img, 0, y, imgW, gridLineWidth, black)
	}
	// Vertical grid lines.
	x := 0
	fillRect(img, x, 0, gridLineWidth, imgH, black)
	for c := 0; c < cols; c++ {
		x += gridLineWidth + colWidth[c]
		fillRect(img, x, 0, gridLineWidth, imgH, black)
	}

	// Cell text.
	baselineOffset := metrics.Ascent.Ceil() + cellPadY
	for r := 0; r < rows; r++ {
		f := bodyFace
		if r == 0 {
			f = headFace
		}
		cellX := gridLineWidth
		cellY := r * (rowHeight + gridLineWidth)
		for c := 0; c < cols; c++ {
			d := &font.Drawer{
				Dst:  img,
				Src:  black,
				Face: f,
				Dot: fixed.Point26_6{
					X: fixed.I(cellX + cellPadX),
					Y: fixed.I(cellY + baselineOffset),
				},
			}
			d.DrawString(grid[r][c])
			cellX += colWidth[c] + gridLineWidth
		}
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func newFace(ttf []byte) (font.Face, error) {
	ft, err := opentype.Parse(ttf)
	if err != nil {
		return nil, err
	}
	return opentype.NewFace(ft, &opentype.FaceOptions{
		Size:    tableFontSize,
		DPI:     tableFontDPI,
		Hinting: font.HintingFull,
	})
}

func measure(f font.Face, s string) int {
	d := &font.Drawer{Face: f}
	return d.MeasureString(s).Ceil()
}

func fillRect(img *image.RGBA, x, y, w, h int, src image.Image) {
	draw.Draw(img, image.Rect(x, y, x+w, y+h), src, image.Point{}, draw.Src)
}

// PreBlockTable is the zero-dependency fallback: it pads each cell to its
// per-column rune width inside a ``` monospace fence. Readable up to ~56
// char/row on the operator's device (the on-device ceiling, spike 018a); wide
// tables degrade here rather than failing. Used when PNG rendering is
// unavailable or the channel prefers text.
func PreBlockTable(grid [][]string) string {
	if len(grid) == 0 {
		return "```\n```"
	}
	cols := len(grid[0])
	colWidth := make([]int, cols)
	for _, row := range grid {
		for c := 0; c < cols && c < len(row); c++ {
			if w := len([]rune(row[c])); w > colWidth[c] {
				colWidth[c] = w
			}
		}
	}

	var b strings.Builder
	b.WriteString("```\n")
	for _, row := range grid {
		for c := 0; c < cols; c++ {
			cell := ""
			if c < len(row) {
				cell = row[c]
			}
			b.WriteString(padRunes(cell, colWidth[c]))
			if c < cols-1 {
				b.WriteString(preBlockColGap)
			}
		}
		b.WriteByte('\n')
	}
	b.WriteString("```")
	return b.String()
}

func padRunes(s string, width int) string {
	n := width - len([]rune(s))
	if n <= 0 {
		return s
	}
	return s + strings.Repeat(" ", n)
}
