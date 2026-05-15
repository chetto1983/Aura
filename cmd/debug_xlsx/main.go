// debug_xlsx is the slice-15a smoke harness for the create_xlsx tool.
//
//	go run ./cmd/debug_xlsx                  # build + persist + open round-trip
//	go run ./cmd/debug_xlsx -out report.xlsx # additionally write the file to disk
//
// Hermetic: temp wiki dir, no LLM, no Telegram. Verifies:
//   - BuildXLSX produces a valid workbook excelize can re-open
//   - cell content survives the round trip
//   - Excel formula injection (= + - @) is neutralized with leading apostrophe
//   - source store dedups identical specs (sha256-keyed)
//   - tools.DocumentSender is invoked when deliver=true with a user context
//   - tools.DocumentSender is skipped when deliver=false
//   - filename sanitization strips path separators and forces .xlsx
//
// Run with -out to also drop the workbook to a real file you can open in
// Excel / LibreOffice for visual inspection.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/aura/aura/cmd/debug_common"
	"github.com/aura/aura/internal/agent/tools/registry"
	"github.com/aura/aura/internal/files"

	"github.com/xuri/excelize/v2"
)

func main() {
	var (
		outFile = flag.String("out", "", "if set, write the generated workbook to this path for visual inspection")
		keep    = flag.Bool("keep-wiki", false, "keep the temp wiki dir at exit (path printed)")
	)
	flag.Parse()

	h := debugcommon.New("debug_xlsx")
	store, _, cleanup := h.TempSourceStore("aura-debug-xlsx-*", *keep)
	defer cleanup()

	sender := &debugcommon.DocumentSender{}
	tool := tools.NewCreateXLSXTool(store, sender)
	if tool == nil {
		h.Fail("NewCreateXLSXTool returned nil")
	}

	h.Scenario("scenario 1: happy path with delivery", func() error {
		ctx := tools.WithUserID(context.Background(), "999")
		args := map[string]any{
			"filename": "demo-report",
			"sheets": []any{
				map[string]any{
					"name": "Q1",
					"rows": []any{
						[]any{"month", "revenue", "notes"},
						[]any{"jan", 100.0, "kickoff"},
						[]any{"feb", 120.5, "growth"},
						[]any{"mar", 145.0, ""},
					},
				},
				map[string]any{
					"name": "summary",
					"rows": []any{
						[]any{"metric", "value"},
						[]any{"total", 365.5},
						[]any{"avg", 121.83},
					},
				},
			},
			"caption": "Q1 numbers",
		}
		out, err := tool.Execute(ctx, args)
		if err != nil {
			return fmt.Errorf("execute: %w", err)
		}
		var resp map[string]any
		if err := json.Unmarshal([]byte(out), &resp); err != nil {
			return fmt.Errorf("unmarshal: %w", err)
		}
		h.MustEqual("filename", resp["filename"], "demo-report.xlsx")
		h.MustEqual("delivered", resp["delivered"], true)
		if len(sender.Calls) != 1 {
			return fmt.Errorf("sender called %d times, want 1", len(sender.Calls))
		}
		h.MustEqual("call.userID", sender.Calls[0].UserID, "999")
		h.MustEqual("call.filename", sender.Calls[0].Filename, "demo-report.xlsx")

		// Round-trip the persisted file.
		id, _ := resp["source_id"].(string)
		path := store.Path(id, "original.xlsx")
		body, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("read persisted: %w", err)
		}
		f, err := excelize.OpenReader(bytes.NewReader(body))
		if err != nil {
			return fmt.Errorf("OpenReader: %w", err)
		}
		defer f.Close()
		sheets := f.GetSheetList()
		if len(sheets) != 2 || sheets[0] != "Q1" || sheets[1] != "summary" {
			return fmt.Errorf("sheets = %v", sheets)
		}
		rows, err := f.GetRows("Q1")
		if err != nil {
			return fmt.Errorf("GetRows: %w", err)
		}
		if len(rows) != 4 || rows[1][0] != "jan" || rows[3][1] != "145" {
			return fmt.Errorf("Q1 rows wrong: %#v", rows)
		}

		if *outFile != "" {
			return debugcommon.WriteOptionalOutput(*outFile, body)
		}
		return nil
	})

	h.Scenario("scenario 2: formula injection neutralized", func() error {
		ctx := tools.WithUserID(context.Background(), "999")
		sender.Reset()
		args := map[string]any{
			"filename": "evil",
			"sheets": []any{
				map[string]any{
					"name": "evil",
					"rows": []any{
						[]any{"=cmd|'/c calc'", "+evil()", "@SUM(A1)", "-99"},
					},
				},
			},
			"deliver": false,
		}
		out, err := tool.Execute(ctx, args)
		if err != nil {
			return fmt.Errorf("execute: %w", err)
		}
		var resp map[string]any
		if err := json.Unmarshal([]byte(out), &resp); err != nil {
			return fmt.Errorf("unmarshal: %w", err)
		}
		h.MustEqual("delivered", resp["delivered"], false)
		if len(sender.Calls) != 0 {
			return fmt.Errorf("sender invoked despite deliver=false: %d calls", len(sender.Calls))
		}
		// Re-read and verify no cell starts with a trigger char.
		id, _ := resp["source_id"].(string)
		body, err := os.ReadFile(store.Path(id, "original.xlsx"))
		if err != nil {
			return fmt.Errorf("read: %w", err)
		}
		f, err := excelize.OpenReader(bytes.NewReader(body))
		if err != nil {
			return fmt.Errorf("OpenReader: %w", err)
		}
		defer f.Close()
		rows, err := f.GetRows("evil")
		if err != nil {
			return fmt.Errorf("GetRows: %w", err)
		}
		for i, cell := range rows[0] {
			if cell == "" {
				continue
			}
			if strings.ContainsRune("=+-@", rune(cell[0])) {
				return fmt.Errorf("cell %d %q still starts with trigger", i, cell)
			}
		}
		return nil
	})

	h.Scenario("scenario 3: dedup on identical spec", func() error {
		ctx := tools.WithUserID(context.Background(), "999")
		args := map[string]any{
			"filename": "dedup",
			"sheets":   []any{map[string]any{"name": "s", "rows": []any{[]any{"a", "b"}}}},
			"deliver":  false,
		}
		out1, err := tool.Execute(ctx, args)
		if err != nil {
			return fmt.Errorf("first: %w", err)
		}
		out2, err := tool.Execute(ctx, args)
		if err != nil {
			return fmt.Errorf("second: %w", err)
		}
		var r1, r2 map[string]any
		_ = json.Unmarshal([]byte(out1), &r1)
		_ = json.Unmarshal([]byte(out2), &r2)
		if r1["source_id"] != r2["source_id"] {
			return fmt.Errorf("source_id differs across identical calls")
		}
		h.MustEqual("duplicate (second)", r2["duplicate"], true)
		return nil
	})

	h.Scenario("scenario 4: filename sanitization (path traversal blocked)", func() error {
		got := files.SanitizeFilename(`../../etc/passwd`)
		h.MustEqual("traversal sanitized", got, "passwd.xlsx")
		got = files.SanitizeFilename(`C:\Users\evil\report`)
		h.MustEqual("windows path sanitized", got, "report.xlsx")
		return nil
	})

	h.Scenario("scenario 5: caps are enforced", func() error {
		// 17 sheets > MaxSheets=16
		too := make([]any, files.MaxSheets+1)
		for i := range too {
			too[i] = map[string]any{"name": fmt.Sprintf("s%d", i), "rows": []any{[]any{"a"}}}
		}
		ctx := tools.WithUserID(context.Background(), "999")
		args := map[string]any{
			"filename": "huge",
			"sheets":   too,
			"deliver":  false,
		}
		if _, err := tool.Execute(ctx, args); err == nil {
			return fmt.Errorf("expected cap error for %d sheets", files.MaxSheets+1)
		}
		return nil
	})

	fmt.Println("\nall scenarios passed.")
}
