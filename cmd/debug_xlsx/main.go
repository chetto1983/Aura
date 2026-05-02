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
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/aura/aura/internal/files"
	"github.com/aura/aura/internal/source"
	"github.com/aura/aura/internal/tools"

	"github.com/xuri/excelize/v2"
)

type stubSender struct {
	calls []stubCall
}

type stubCall struct {
	userID, filename, caption string
	bodyLen                   int
}

func (s *stubSender) SendDocumentToUser(userID, filename string, body []byte, caption string) error {
	s.calls = append(s.calls, stubCall{userID, filename, caption, len(body)})
	return nil
}

func main() {
	var (
		outFile = flag.String("out", "", "if set, write the generated workbook to this path for visual inspection")
		keep    = flag.Bool("keep-wiki", false, "keep the temp wiki dir at exit (path printed)")
	)
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	wikiDir, err := os.MkdirTemp("", "aura-debug-xlsx-*")
	if err != nil {
		fail("mkdir temp wiki: %v", err)
	}
	if !*keep {
		defer os.RemoveAll(wikiDir)
	} else {
		fmt.Printf("temp wiki: %s\n", wikiDir)
	}

	store, err := source.NewStore(wikiDir, logger)
	if err != nil {
		fail("source.NewStore: %v", err)
	}

	sender := &stubSender{}
	tool := tools.NewCreateXLSXTool(store, sender)
	if tool == nil {
		fail("NewCreateXLSXTool returned nil")
	}

	scenario("scenario 1: happy path with delivery", func() error {
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
			return fmt.Errorf("Execute: %w", err)
		}
		var resp map[string]any
		if err := json.Unmarshal([]byte(out), &resp); err != nil {
			return fmt.Errorf("unmarshal: %w", err)
		}
		mustEqual("filename", resp["filename"], "demo-report.xlsx")
		mustEqual("delivered", resp["delivered"], true)
		if len(sender.calls) != 1 {
			return fmt.Errorf("sender called %d times, want 1", len(sender.calls))
		}
		mustEqual("call.userID", sender.calls[0].userID, "999")
		mustEqual("call.filename", sender.calls[0].filename, "demo-report.xlsx")

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
			abs, _ := filepath.Abs(*outFile)
			if err := os.WriteFile(abs, body, 0o644); err != nil {
				return fmt.Errorf("write -out: %w", err)
			}
			fmt.Printf("  wrote %d bytes to %s\n", len(body), abs)
		}
		return nil
	})

	scenario("scenario 2: formula injection neutralized", func() error {
		ctx := tools.WithUserID(context.Background(), "999")
		sender.calls = sender.calls[:0]
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
			return fmt.Errorf("Execute: %w", err)
		}
		var resp map[string]any
		if err := json.Unmarshal([]byte(out), &resp); err != nil {
			return fmt.Errorf("unmarshal: %w", err)
		}
		mustEqual("delivered", resp["delivered"], false)
		if len(sender.calls) != 0 {
			return fmt.Errorf("sender invoked despite deliver=false: %d calls", len(sender.calls))
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

	scenario("scenario 3: dedup on identical spec", func() error {
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
		mustEqual("duplicate (second)", r2["duplicate"], true)
		return nil
	})

	scenario("scenario 4: filename sanitization (path traversal blocked)", func() error {
		got := files.SanitizeFilename(`../../etc/passwd`)
		mustEqual("traversal sanitized", got, "passwd.xlsx")
		got = files.SanitizeFilename(`C:\Users\evil\report`)
		mustEqual("windows path sanitized", got, "report.xlsx")
		return nil
	})

	scenario("scenario 5: caps are enforced", func() error {
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

func scenario(name string, fn func() error) {
	fmt.Printf("→ %s\n", name)
	if err := fn(); err != nil {
		fail("  FAIL: %v", err)
	}
	fmt.Println("  ok")
}

func mustEqual(label string, got, want any) {
	if fmt.Sprintf("%v", got) != fmt.Sprintf("%v", want) {
		fail("  %s = %v, want %v", label, got, want)
	}
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "debug_xlsx: "+format+"\n", args...)
	os.Exit(1)
}
