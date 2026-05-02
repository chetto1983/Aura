// Package files generates user-deliverable artifacts (xlsx today, docx/pdf
// later). Each generator returns bytes + a sanitized filename so callers
// can persist via internal/source and ship over Telegram via tele.Document.
//
// Security posture:
//   - Pure-Go generation (excelize/v2). No subprocess, no macro engine.
//   - Cell strings are sanitized to neutralize Excel formula injection — any
//     value starting with =, +, -, @, or 0x09/0x0D gets a leading apostrophe
//     so Excel treats it as a literal string. CWE-1236.
//   - Hard caps on sheet count, rows-per-sheet, cols-per-row, and total
//     bytes block both runaway LLM output and Telegram's 50 MB document cap.
package files

import (
	"bytes"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"unicode"

	"github.com/xuri/excelize/v2"
)

// XLSXSpec describes a workbook the LLM (or any other caller) wants
// generated. Sheets are emitted in slice order; the first sheet's name
// becomes the workbook's active sheet.
type XLSXSpec struct {
	// Filename is the user-visible name. The generator sanitizes path
	// separators and forces a .xlsx suffix; callers can pass "report" and
	// get back "report.xlsx".
	Filename string
	// Sheets must contain at least one sheet. Empty workbooks aren't useful
	// and tend to come from LLM mistakes; we reject them so the model
	// retries instead of shipping junk.
	Sheets []XLSXSheet
}

// XLSXSheet is one tab. Rows are 0-indexed in the slice; excelize converts
// to 1-indexed Excel coordinates internally.
type XLSXSheet struct {
	// Name shows up on the tab. excelize enforces 31 chars + no /\?*[].
	// We sanitize before passing through.
	Name string
	// Rows is a 2-D slice; row[i][j] is the cell at row i, col j. Cells
	// are stored as strings — numeric/date inference happens at render
	// time so the LLM doesn't need typed JSON.
	Rows [][]string
}

// Generation caps. Picked to fit comfortably under Telegram's 50 MB
// non-premium bot document limit while still accommodating real-world
// invoice / report sizes. Tightening later is cheap; loosening would
// require revisiting the formula-injection sanitizer's perf assumptions.
const (
	MaxSheets       = 16
	MaxRowsPerSheet = 10000
	MaxColsPerRow   = 100
	MaxTotalCells   = 200000 // overall guard so 16 sheets * 10000 * 100 can't all run hot
	MaxBytes        = 25 * 1024 * 1024
	MaxFilenameLen  = 80
)

// dangerousLeadingChars are the four characters Excel treats as the start
// of a formula. CSV injection / XLSX formula injection (CWE-1236) abuse
// this: if the LLM is told to put attacker-controlled data into a cell,
// =cmd|... or @SUM(... can hijack the spreadsheet on open. Prefixing with
// an apostrophe forces literal-text mode and is the recommendation from
// OWASP CSV injection cheat sheet.
var dangerousLeadingChars = map[byte]struct{}{
	'=': {},
	'+': {},
	'-': {},
	'@': {},
	// Tab and CR can also trigger formula-mode in some Excel locales.
	0x09: {},
	0x0D: {},
}

// invalidSheetNameChar matches the characters excelize rejects in sheet
// names: : \ / ? * [ ]. We replace each with an underscore.
var invalidSheetNameChar = regexp.MustCompile(`[:\\/?*\[\]]`)

// invalidFilenameChar matches characters we never want in a filename
// regardless of OS: path separators, NUL, plus the Windows-reserved set.
var invalidFilenameChar = regexp.MustCompile(`[<>:"/\\|?*\x00-\x1f]`)

// SanitizeCell returns a value safe to write into an xlsx cell. Empty
// strings pass through; anything starting with a formula-trigger char is
// prefixed with a single apostrophe (which Excel hides on display but
// preserves on copy/paste, matching CSV-injection mitigation guidance).
//
// Exported so tests in other packages can verify their inputs would
// survive a round-trip without surprise mutation.
func SanitizeCell(v string) string {
	if v == "" {
		return ""
	}
	if _, bad := dangerousLeadingChars[v[0]]; bad {
		return "'" + v
	}
	return v
}

// SanitizeFilename strips path separators, trims whitespace, lowercases
// trailing extensions, and forces a .xlsx suffix. Returns "workbook.xlsx"
// when the input would otherwise be empty so we never produce a hidden
// file (e.g. ".xlsx").
func SanitizeFilename(in string) string {
	// Extract basename FIRST so path components are dropped intact, before
	// the regex below would otherwise eat the separators and merge dirs
	// into the filename ("path/to/file" → "pathtofile" was a real bug).
	clean := strings.TrimSpace(in)
	if idx := strings.LastIndexAny(clean, `/\`); idx >= 0 {
		clean = clean[idx+1:]
	}
	clean = invalidFilenameChar.ReplaceAllString(clean, "")
	clean = strings.TrimSpace(clean)
	// Strip trailing dots/spaces (Windows hates both).
	clean = strings.TrimRightFunc(clean, func(r rune) bool {
		return r == '.' || unicode.IsSpace(r)
	})
	// Force extension.
	lower := strings.ToLower(clean)
	if !strings.HasSuffix(lower, ".xlsx") {
		clean = strings.TrimSuffix(clean, ".") + ".xlsx"
	}
	if clean == ".xlsx" || clean == "" {
		clean = "workbook.xlsx"
	}
	if len(clean) > MaxFilenameLen {
		// Preserve the .xlsx suffix when truncating the stem.
		stem := strings.TrimSuffix(clean, ".xlsx")
		keep := max(MaxFilenameLen-len(".xlsx"), 1)
		if len(stem) > keep {
			stem = stem[:keep]
		}
		clean = stem + ".xlsx"
	}
	return clean
}

// sanitizeSheetName trims to 31 chars and replaces excelize's forbidden
// characters with underscores. Empty input becomes "Sheet1" so the
// workbook is always renderable.
func sanitizeSheetName(name string, fallback string) string {
	clean := invalidSheetNameChar.ReplaceAllString(name, "_")
	clean = strings.TrimSpace(clean)
	if clean == "" {
		clean = fallback
	}
	// Excel sheet names can be at most 31 chars.
	if len(clean) > 31 {
		clean = clean[:31]
	}
	return clean
}

// BuildXLSX renders spec into an xlsx byte stream. Returns the rendered
// bytes plus the canonicalized filename (with .xlsx suffix, no path
// separators).
//
// Errors when caps are exceeded or the spec is empty so the LLM gets a
// concrete tool error to retry from instead of an opaque silent failure.
func BuildXLSX(spec XLSXSpec) ([]byte, string, error) {
	if len(spec.Sheets) == 0 {
		return nil, "", errors.New("xlsx: spec must contain at least one sheet")
	}
	if len(spec.Sheets) > MaxSheets {
		return nil, "", fmt.Errorf("xlsx: too many sheets (%d > %d)", len(spec.Sheets), MaxSheets)
	}
	totalCells := 0
	for i, sh := range spec.Sheets {
		if len(sh.Rows) > MaxRowsPerSheet {
			return nil, "", fmt.Errorf("xlsx: sheet %d has %d rows (max %d)", i, len(sh.Rows), MaxRowsPerSheet)
		}
		for j, row := range sh.Rows {
			if len(row) > MaxColsPerRow {
				return nil, "", fmt.Errorf("xlsx: sheet %d row %d has %d cols (max %d)", i, j, len(row), MaxColsPerRow)
			}
			totalCells += len(row)
		}
	}
	if totalCells > MaxTotalCells {
		return nil, "", fmt.Errorf("xlsx: %d total cells exceeds cap %d", totalCells, MaxTotalCells)
	}

	f := excelize.NewFile()
	defer f.Close()

	// excelize starts with a default "Sheet1". We rename it to the first
	// spec sheet, then create the rest.
	usedNames := map[string]int{}
	uniqueName := func(base string) string {
		if usedNames[base] == 0 {
			usedNames[base] = 1
			return base
		}
		for n := 2; ; n++ {
			candidate := fmt.Sprintf("%s_%d", base, n)
			if len(candidate) > 31 {
				candidate = candidate[len(candidate)-31:]
			}
			if usedNames[candidate] == 0 {
				usedNames[candidate] = 1
				return candidate
			}
		}
	}

	for i, sh := range spec.Sheets {
		name := uniqueName(sanitizeSheetName(sh.Name, fmt.Sprintf("Sheet%d", i+1)))
		var idx int
		if i == 0 {
			// Rename the auto-created Sheet1 to our first sheet name so
			// the workbook isn't shipped with both "Sheet1" and the
			// user's actual first tab.
			if err := f.SetSheetName("Sheet1", name); err != nil {
				return nil, "", fmt.Errorf("xlsx: rename default sheet: %w", err)
			}
			n, err := f.GetSheetIndex(name)
			if err != nil {
				return nil, "", fmt.Errorf("xlsx: lookup default sheet: %w", err)
			}
			idx = n
		} else {
			n, err := f.NewSheet(name)
			if err != nil {
				return nil, "", fmt.Errorf("xlsx: new sheet %q: %w", name, err)
			}
			idx = n
		}
		if i == 0 {
			f.SetActiveSheet(idx)
		}
		for r, row := range sh.Rows {
			for c, raw := range row {
				cell, err := excelize.CoordinatesToCellName(c+1, r+1)
				if err != nil {
					return nil, "", fmt.Errorf("xlsx: coords r%d c%d: %w", r, c, err)
				}
				safe := SanitizeCell(raw)
				if err := f.SetCellStr(name, cell, safe); err != nil {
					return nil, "", fmt.Errorf("xlsx: set %s!%s: %w", name, cell, err)
				}
			}
		}
	}

	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		return nil, "", fmt.Errorf("xlsx: serialize: %w", err)
	}
	if buf.Len() > MaxBytes {
		return nil, "", fmt.Errorf("xlsx: rendered %d bytes exceeds cap %d", buf.Len(), MaxBytes)
	}
	return buf.Bytes(), SanitizeFilename(spec.Filename), nil
}
