package telegram

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const fourColTable = `| Name | Qty | Price | Total |
|------|-----|-------|-------|
| Apple | 3 | 1.20 | 3.60 |
| Pear | 2 | 0.90 | 1.80 |`

const sixColTable = `| ID | Name | Dept | City | Role | Active |
|----|------|------|------|------|--------|
| 1 | Alice | Eng | Roma | Lead | yes |
| 2 | Bob | Ops | Milano | SRE | no |
| 3 | Carol | Eng | Torino | Dev | yes |`

func TestParseMarkdownTable(t *testing.T) {
	t.Run("four_col", func(t *testing.T) {
		grid, ok := ParseMarkdownTable(fourColTable)
		if !ok {
			t.Fatal("ParseMarkdownTable(fourColTable) ok = false, want true")
		}
		if len(grid) != 3 { // header + 2 data rows (separator dropped)
			t.Fatalf("grid rows = %d, want 3", len(grid))
		}
		if len(grid[0]) != 4 {
			t.Fatalf("grid cols = %d, want 4", len(grid[0]))
		}
		if grid[0][0] != "Name" || grid[1][0] != "Apple" || grid[2][3] != "1.80" {
			t.Errorf("unexpected cell contents: %v", grid)
		}
	})

	t.Run("six_col", func(t *testing.T) {
		grid, ok := ParseMarkdownTable(sixColTable)
		if !ok {
			t.Fatal("ParseMarkdownTable(sixColTable) ok = false, want true")
		}
		if len(grid) != 4 || len(grid[0]) != 6 {
			t.Fatalf("grid shape = %dx%d, want 4x6", len(grid), len(grid[0]))
		}
	})

	t.Run("not_a_table", func(t *testing.T) {
		for _, s := range []string{"", "plain text", "| only header |", "| h |\nno separator"} {
			if _, ok := ParseMarkdownTable(s); ok {
				t.Errorf("ParseMarkdownTable(%q) ok = true, want false", s)
			}
		}
	})
}

func decodePNG(t *testing.T, b []byte) image.Image {
	t.Helper()
	img, err := png.Decode(bytes.NewReader(b))
	if err != nil {
		t.Fatalf("png.Decode: %v", err)
	}
	return img
}

func nonBlankPixelCount(img image.Image) int {
	bnd := img.Bounds()
	white := color.RGBA{R: 255, G: 255, B: 255, A: 255}
	count := 0
	for y := bnd.Min.Y; y < bnd.Max.Y; y++ {
		for x := bnd.Min.X; x < bnd.Max.X; x++ {
			r, g, b, a := img.At(x, y).RGBA()
			rr := color.RGBA{R: uint8(r >> 8), G: uint8(g >> 8), B: uint8(b >> 8), A: uint8(a >> 8)}
			if rr != white {
				count++
			}
		}
	}
	return count
}

func TestRenderTablePNG_DeterministicDimsAndGrid(t *testing.T) {
	tests := []struct {
		name  string
		table string
	}{
		{name: "four_col", table: fourColTable},
		{name: "six_col", table: sixColTable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			grid, ok := ParseMarkdownTable(tt.table)
			if !ok {
				t.Fatalf("ParseMarkdownTable(%s) failed", tt.name)
			}

			png1, err := RenderTablePNG(grid)
			if err != nil {
				t.Fatalf("RenderTablePNG: %v", err)
			}
			png2, err := RenderTablePNG(grid)
			if err != nil {
				t.Fatalf("RenderTablePNG (second): %v", err)
			}
			// Determinism: same grid -> byte-identical PNG.
			if !bytes.Equal(png1, png2) {
				t.Error("RenderTablePNG is not deterministic: two renders differ")
			}

			img := decodePNG(t, png1)
			bnd := img.Bounds()

			// Golden dims stored in testdata; on first run (UPDATE_GOLDEN) write them.
			goldenPath := filepath.Join("testdata", "table_"+tt.name+"_dims.txt")
			wantDims := dims{w: bnd.Dx(), h: bnd.Dy()}
			if os.Getenv("UPDATE_GOLDEN") == "1" {
				writeGoldenDims(t, goldenPath, wantDims)
			}
			gotGolden := readGoldenDims(t, goldenPath)
			if gotGolden != wantDims {
				t.Errorf("dims = %dx%d, golden = %dx%d (set UPDATE_GOLDEN=1 to refresh after an intentional change)",
					wantDims.w, wantDims.h, gotGolden.w, gotGolden.h)
			}

			// Non-blank: a rendered table must have substantial dark pixels
			// (grid lines + glyphs), never an all-white blank.
			nb := nonBlankPixelCount(img)
			if nb < 500 {
				t.Errorf("only %d non-blank pixels — table render looks blank", nb)
			}

			// Grid presence: there must be at least one fully-dark vertical line
			// (a column separator) and one horizontal line (a row separator).
			if !hasVerticalLine(img) {
				t.Error("no vertical grid line detected")
			}
			if !hasHorizontalLine(img) {
				t.Error("no horizontal grid line detected")
			}
		})
	}
}

type dims struct{ w, h int }

func writeGoldenDims(t *testing.T, path string, d dims) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatalf("mkdir testdata: %v", err)
	}
	content := []byte{}
	content = append(content, []byte(itoa(d.w))...)
	content = append(content, 'x')
	content = append(content, []byte(itoa(d.h))...)
	content = append(content, '\n')
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("write golden: %v", err)
	}
}

func readGoldenDims(t *testing.T, path string) dims {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v (run with UPDATE_GOLDEN=1 first)", path, err)
	}
	parts := strings.SplitN(strings.TrimSpace(string(b)), "x", 2)
	if len(parts) != 2 {
		t.Fatalf("malformed golden %s: %q", path, string(b))
	}
	return dims{w: atoi(t, parts[0]), h: atoi(t, parts[1])}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

func atoi(t *testing.T, s string) int {
	t.Helper()
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			t.Fatalf("non-numeric golden dim %q", s)
		}
		n = n*10 + int(c-'0')
	}
	return n
}

// hasVerticalLine scans for a column of mostly-dark pixels (a grid separator).
func hasVerticalLine(img image.Image) bool {
	bnd := img.Bounds()
	for x := bnd.Min.X; x < bnd.Max.X; x++ {
		dark := 0
		for y := bnd.Min.Y; y < bnd.Max.Y; y++ {
			if isDark(img.At(x, y)) {
				dark++
			}
		}
		if dark >= (bnd.Dy()*8)/10 {
			return true
		}
	}
	return false
}

// hasHorizontalLine scans for a row of mostly-dark pixels (a grid separator).
func hasHorizontalLine(img image.Image) bool {
	bnd := img.Bounds()
	for y := bnd.Min.Y; y < bnd.Max.Y; y++ {
		dark := 0
		for x := bnd.Min.X; x < bnd.Max.X; x++ {
			if isDark(img.At(x, y)) {
				dark++
			}
		}
		if dark >= (bnd.Dx()*8)/10 {
			return true
		}
	}
	return false
}

func isDark(c color.Color) bool {
	r, g, b, _ := c.RGBA()
	return r>>8 < 128 && g>>8 < 128 && b>>8 < 128
}

func TestPreBlockTable_FallbackPadsCells(t *testing.T) {
	grid, ok := ParseMarkdownTable(fourColTable)
	if !ok {
		t.Fatal("parse failed")
	}
	out := PreBlockTable(grid)

	// Fenced monospace block.
	if !strings.HasPrefix(out, "```") || !strings.HasSuffix(strings.TrimRight(out, "\n"), "```") {
		t.Errorf("pre-block must be ```-fenced; got:\n%s", out)
	}

	// Every content row pads to the same visual width (per-column rune widths).
	lines := strings.Split(strings.Trim(out, "`\n"), "\n")
	var contentLines []string
	for _, l := range lines {
		if strings.TrimSpace(l) != "" {
			contentLines = append(contentLines, l)
		}
	}
	if len(contentLines) < 2 {
		t.Fatalf("expected padded content lines, got %d", len(contentLines))
	}
	width := len([]rune(contentLines[0]))
	for i, l := range contentLines {
		if w := len([]rune(l)); w != width {
			t.Errorf("row %d width = %d, want %d (cells not padded to per-column width)\n%s", i, w, width, out)
		}
	}
}

func TestPreBlockTable_ColumnWidthMatchesWidestCell(t *testing.T) {
	// "Apple" (5) is the widest cell in column 0; the column must pad to >=5.
	grid, _ := ParseMarkdownTable(fourColTable)
	out := PreBlockTable(grid)
	if !strings.Contains(out, "Apple") || !strings.Contains(out, "Name ") {
		t.Errorf("expected 'Name' padded to align under 'Apple'; got:\n%s", out)
	}
}
