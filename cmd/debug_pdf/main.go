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
	"log/slog"
	"os"
	"path/filepath"

	"github.com/aura/aura/internal/files"
	"github.com/aura/aura/internal/source"
	"github.com/aura/aura/internal/tools"
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
		outFile = flag.String("out", "", "if set, write the generated PDF to this path for visual inspection")
		keep    = flag.Bool("keep-wiki", false, "keep the temp wiki dir at exit (path printed)")
	)
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	wikiDir, err := os.MkdirTemp("", "aura-debug-pdf-*")
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
	tool := tools.NewCreatePDFTool(store, sender)
	if tool == nil {
		fail("NewCreatePDFTool returned nil")
	}

	scenario("scenario 1: full document with delivery", func() error {
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
			return fmt.Errorf("Execute: %w", err)
		}
		var resp map[string]any
		if err := json.Unmarshal([]byte(out), &resp); err != nil {
			return fmt.Errorf("unmarshal: %w", err)
		}
		mustEqual("filename", resp["filename"], "quarterly-report.pdf")
		mustEqual("delivered", resp["delivered"], true)
		if len(sender.calls) != 1 {
			return fmt.Errorf("sender called %d times, want 1", len(sender.calls))
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
			abs, _ := filepath.Abs(*outFile)
			if err := os.WriteFile(abs, body, 0o644); err != nil {
				return fmt.Errorf("write -out: %w", err)
			}
			fmt.Printf("  wrote %d bytes to %s\n", len(body), abs)
		}
		return nil
	})

	scenario("scenario 2: Latin-1 sanitization (curly quotes / em-dash / ellipsis)", func() error {
		ctx := tools.WithUserID(context.Background(), "999")
		sender.calls = sender.calls[:0]
		args := map[string]any{
			"filename": "fancy-quotes",
			"title":    `It’s a “smoke” test — ok? …`,
			"blocks": []any{
				map[string]any{"kind": "paragraph", "text": "Testing curly quotes and em-dashes that fpdf can't natively render."},
			},
			"deliver": false,
		}
		if _, err := tool.Execute(ctx, args); err != nil {
			return fmt.Errorf("Execute: %w", err)
		}
		if len(sender.calls) != 0 {
			return fmt.Errorf("sender invoked despite deliver=false")
		}
		return nil
	})

	scenario("scenario 3: dedup on identical spec", func() error {
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
		mustEqual("duplicate (second)", r2["duplicate"], true)
		return nil
	})

	scenario("scenario 4: filename sanitization (path traversal blocked)", func() error {
		got := files.SanitizePDFFilename(`../../etc/passwd`)
		mustEqual("traversal sanitized", got, "passwd.pdf")
		got = files.SanitizePDFFilename(`C:\Users\evil\report`)
		mustEqual("windows path sanitized", got, "report.pdf")
		return nil
	})

	scenario("scenario 5: caps enforced", func() error {
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
	fmt.Fprintf(os.Stderr, "debug_pdf: "+format+"\n", args...)
	os.Exit(1)
}
