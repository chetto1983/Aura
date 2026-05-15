// debug_pdf is the slice-15c smoke harness for the create_pdf tool.
//
//	go run ./cmd/debug_pdf                  # build + persist + delivery stub
//	go run ./cmd/debug_pdf -out report.pdf  # additionally write the file to disk
//
// Hermetic: temp wiki dir, no LLM, no Telegram. Verifies:
//   - BuildPDF produces bytes starting with %PDF- and ending with %%EOF
//   - title + heading + paragraph + bullet + table all render
//   - Latin-1 sanitization handles curly quotes, em-dashes, ellipses
//   - source store dedups identical specs (sha256-keyed)
//   - tools.DocumentSender invoked when deliver=true with user context
//   - tools.DocumentSender skipped when deliver=false
//   - filename sanitization strips path separators and forces .pdf
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"

	"github.com/aura/aura/cmd/debug_common"
	"github.com/aura/aura/internal/agent/tools/registry"
	"github.com/aura/aura/internal/files"
)

func main() {
	var (
		outFile = flag.String("out", "", "if set, write the generated PDF to this path for visual inspection")
		keep    = flag.Bool("keep-wiki", false, "keep the temp wiki dir at exit (path printed)")
	)
	flag.Parse()

	h := debugcommon.New("debug_pdf")

	store, _, cleanup := h.TempSourceStore("aura-debug-pdf-*", *keep)
	defer cleanup()

	sender := &debugcommon.DocumentSender{}
	tool := tools.NewCreatePDFTool(store, sender)
	if tool == nil {
		h.Fail("NewCreatePDFTool returned nil")
	}

	h.Scenario("scenario 1: full document with delivery", func() error {
		ctx := tools.WithUserID(context.Background(), "999")
		args := map[string]any{
			"filename": "quarterly-report",
			"title":    "Quarterly Report",
			"blocks": []any{
				map[string]any{"kind": "paragraph", "text": "Executive summary follows below."},
				map[string]any{"kind": "heading", "level": 2.0, "text": "Highlights"},
				map[string]any{"kind": "bullet", "text": "Revenue up 12%"},
				map[string]any{"kind": "bullet", "text": "Two new customers signed"},
				map[string]any{"kind": "heading", "level": 2.0, "text": "Numbers"},
				map[string]any{"kind": "table", "rows": []any{
					[]any{"month", "revenue", "notes"},
					[]any{"jan", 100.0, "kickoff"},
					[]any{"feb", 120.5, "growth"},
				}},
			},
			"caption": "Q1 report",
		}
		out, err := tool.Execute(ctx, args)
		if err != nil {
			return fmt.Errorf("execute: %w", err)
		}
		var resp map[string]any
		if err := json.Unmarshal([]byte(out), &resp); err != nil {
			return fmt.Errorf("unmarshal: %w", err)
		}
		h.MustEqual("filename", resp["filename"], "quarterly-report.pdf")
		h.MustEqual("delivered", resp["delivered"], true)
		if len(sender.Calls) != 1 {
			return fmt.Errorf("sender called %d times, want 1", len(sender.Calls))
		}

		// Verify on-disk bytes are a valid PDF.
		id, _ := resp["source_id"].(string)
		body, err := os.ReadFile(store.Path(id, "original.pdf"))
		if err != nil {
			return fmt.Errorf("read persisted: %w", err)
		}
		if !bytes.HasPrefix(body, []byte("%PDF-")) {
			return fmt.Errorf("body not a valid PDF (no %%PDF- prefix)")
		}
		trimmed := bytes.TrimSpace(body)
		if !bytes.HasSuffix(trimmed, []byte("%%EOF")) {
			return fmt.Errorf("body not a valid PDF (no %%%%EOF tail)")
		}

		if *outFile != "" {
			return debugcommon.WriteOptionalOutput(*outFile, body)
		}
		return nil
	})

	h.Scenario("scenario 2: Latin-1 sanitization (curly quotes / em-dash / ellipsis)", func() error {
		ctx := tools.WithUserID(context.Background(), "999")
		sender.Reset()
		args := map[string]any{
			"filename": "fancy-quotes",
			"title":    `It’s a “smoke” test — ok? …`,
			"blocks": []any{
				map[string]any{"kind": "paragraph", "text": "Testing curly quotes and em-dashes that fpdf can't natively render."},
			},
			"deliver": false,
		}
		if _, err := tool.Execute(ctx, args); err != nil {
			return fmt.Errorf("execute: %w", err)
		}
		if len(sender.Calls) != 0 {
			return fmt.Errorf("sender invoked despite deliver=false")
		}
		return nil
	})

	h.Scenario("scenario 3: dedup on identical spec", func() error {
		ctx := tools.WithUserID(context.Background(), "999")
		args := map[string]any{
			"filename": "dedup",
			"title":    "stable",
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
		got := files.SanitizePDFFilename(`../../etc/passwd`)
		h.MustEqual("traversal sanitized", got, "passwd.pdf")
		got = files.SanitizePDFFilename(`C:\Users\evil\report`)
		h.MustEqual("windows path sanitized", got, "report.pdf")
		return nil
	})

	h.Scenario("scenario 5: caps enforced", func() error {
		blocks := make([]any, files.MaxPDFBlocks+1)
		for i := range blocks {
			blocks[i] = map[string]any{"kind": "paragraph", "text": "x"}
		}
		ctx := tools.WithUserID(context.Background(), "999")
		args := map[string]any{
			"filename": "huge",
			"blocks":   blocks,
			"deliver":  false,
		}
		if _, err := tool.Execute(ctx, args); err == nil {
			return fmt.Errorf("expected cap error for %d blocks", files.MaxPDFBlocks+1)
		}
		return nil
	})

	fmt.Println("\nall scenarios passed.")
}
