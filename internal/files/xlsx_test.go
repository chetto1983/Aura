package files

import (
	"bytes"
	"strings"
	"testing"

	"github.com/xuri/excelize/v2"
)

func TestSanitizeCell(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"", ""},
		{"hello", "hello"},
		{"=cmd|'/c calc'", "'=cmd|'/c calc'"},
		{"+1+1", "'+1+1"},
		{"-2", "'-2"},
		{"@SUM(A1)", "'@SUM(A1)"},
		{"\tinjected", "'\tinjected"},
		{"\rinjected", "'\rinjected"},
		// Genuine numbers should not be quoted (they don't start with one
		// of the dangerous chars; Excel will still parse them as text via
		// SetCellStr but at least we don't leak the apostrophe).
		{"42", "42"},
		// Negative number stored as string still gets the prefix because
		// the leading '-' is the formula trigger; this is the safe call.
		{"-42", "'-42"},
	}
	for _, tc := range cases {
		got := SanitizeCell(tc.in)
		if got != tc.want {
			t.Errorf("SanitizeCell(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestSanitizeFilename(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"report", "report.xlsx"},
		{"report.xlsx", "report.xlsx"},
		{"REPORT.XLSX", "REPORT.XLSX"},
		{"  spaced  ", "spaced.xlsx"},
		{"path/to/file", "file.xlsx"},
		{`C:\Users\evil.xlsx`, "evil.xlsx"},
		{"foo.bar.xlsx", "foo.bar.xlsx"},
		{"foo<>:|?*.xlsx", "foo.xlsx"},
		{"", "workbook.xlsx"},
		{".xlsx", "workbook.xlsx"},
		{strings.Repeat("a", 200), strings.Repeat("a", 75) + ".xlsx"},
		// Newline / control char in middle gets stripped.
		{"line\x01break", "linebreak.xlsx"},
	}
	for _, tc := range cases {
		got := SanitizeFilename(tc.in)
		if got != tc.want {
			t.Errorf("SanitizeFilename(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestBuildXLSX_HappyPath(t *testing.T) {
	spec := XLSXSpec{
		Filename: "demo",
		Sheets: []XLSXSheet{{
			Name: "Q1",
			Rows: [][]string{
				{"month", "revenue"},
				{"jan", "100"},
				{"feb", "120"},
			},
		}},
	}
	body, name, err := BuildXLSX(spec)
	if err != nil {
		t.Fatalf("BuildXLSX: %v", err)
	}
	if name != "demo.xlsx" {
		t.Errorf("filename = %q, want demo.xlsx", name)
	}
	if len(body) == 0 {
		t.Fatal("empty body")
	}

	// Round-trip through excelize to verify cell content survived.
	f, err := excelize.OpenReader(bytes.NewReader(body))
	if err != nil {
		t.Fatalf("OpenReader: %v", err)
	}
	defer f.Close()
	rows, err := f.GetRows("Q1")
	if err != nil {
		t.Fatalf("GetRows: %v", err)
	}
	if len(rows) != 3 || rows[1][0] != "jan" || rows[2][1] != "120" {
		t.Errorf("unexpected rows: %#v", rows)
	}
}

func TestBuildXLSX_FormulaInjectionSanitized(t *testing.T) {
	body, _, err := BuildXLSX(XLSXSpec{
		Filename: "evil",
		Sheets: []XLSXSheet{{
			Name: "evil",
			Rows: [][]string{{"=cmd|'/c calc'", "+evil", "@SUM(A1)"}},
		}},
	})
	if err != nil {
		t.Fatalf("BuildXLSX: %v", err)
	}
	f, err := excelize.OpenReader(bytes.NewReader(body))
	if err != nil {
		t.Fatalf("OpenReader: %v", err)
	}
	defer f.Close()
	rows, err := f.GetRows("evil")
	if err != nil {
		t.Fatalf("GetRows: %v", err)
	}
	// Excel hides the leading apostrophe on display but it's still on
	// the underlying string; the key invariant is that no cell starts
	// with the formula trigger char in the read-back data.
	for _, cell := range rows[0] {
		if cell == "" {
			continue
		}
		if _, bad := dangerousLeadingChars[cell[0]]; bad {
			t.Errorf("cell %q still starts with formula trigger", cell)
		}
	}
}

func TestBuildXLSX_RejectsEmpty(t *testing.T) {
	if _, _, err := BuildXLSX(XLSXSpec{Filename: "x"}); err == nil {
		t.Error("expected error for empty sheet list, got nil")
	}
}

func TestBuildXLSX_TooManySheets(t *testing.T) {
	sheets := make([]XLSXSheet, MaxSheets+1)
	for i := range sheets {
		sheets[i] = XLSXSheet{Name: "s", Rows: [][]string{{"a"}}}
	}
	if _, _, err := BuildXLSX(XLSXSpec{Filename: "x", Sheets: sheets}); err == nil {
		t.Error("expected error for too many sheets")
	}
}

func TestBuildXLSX_TooManyRows(t *testing.T) {
	rows := make([][]string, MaxRowsPerSheet+1)
	for i := range rows {
		rows[i] = []string{"a"}
	}
	_, _, err := BuildXLSX(XLSXSpec{
		Filename: "x",
		Sheets:   []XLSXSheet{{Name: "s", Rows: rows}},
	})
	if err == nil {
		t.Error("expected error for too many rows")
	}
}

func TestBuildXLSX_TooManyCols(t *testing.T) {
	row := make([]string, MaxColsPerRow+1)
	_, _, err := BuildXLSX(XLSXSpec{
		Filename: "x",
		Sheets:   []XLSXSheet{{Name: "s", Rows: [][]string{row}}},
	})
	if err == nil {
		t.Error("expected error for too many cols")
	}
}

func TestBuildXLSX_DuplicateSheetNamesGetSuffixed(t *testing.T) {
	body, _, err := BuildXLSX(XLSXSpec{
		Filename: "dup",
		Sheets: []XLSXSheet{
			{Name: "data", Rows: [][]string{{"a"}}},
			{Name: "data", Rows: [][]string{{"b"}}},
		},
	})
	if err != nil {
		t.Fatalf("BuildXLSX: %v", err)
	}
	f, err := excelize.OpenReader(bytes.NewReader(body))
	if err != nil {
		t.Fatalf("OpenReader: %v", err)
	}
	defer f.Close()
	names := f.GetSheetList()
	if len(names) != 2 {
		t.Fatalf("got %d sheets, want 2: %v", len(names), names)
	}
	if names[0] != "data" || names[1] != "data_2" {
		t.Errorf("sheet names = %v, want [data data_2]", names)
	}
}

func TestBuildXLSX_SanitizesSheetName(t *testing.T) {
	body, _, err := BuildXLSX(XLSXSpec{
		Filename: "x",
		Sheets: []XLSXSheet{
			{Name: "bad/name?[1]", Rows: [][]string{{"a"}}},
		},
	})
	if err != nil {
		t.Fatalf("BuildXLSX: %v", err)
	}
	f, err := excelize.OpenReader(bytes.NewReader(body))
	if err != nil {
		t.Fatalf("OpenReader: %v", err)
	}
	defer f.Close()
	names := f.GetSheetList()
	if names[0] != "bad_name__1_" {
		t.Errorf("sanitized sheet name = %q, want bad_name__1_", names[0])
	}
}
