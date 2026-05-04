// debug_docx is the slice-15b smoke harness for the create_docx tool.
//
//	go run ./cmd/debug_docx                  # build + persist + delivery stub
//	go run ./cmd/debug_docx -out memo.docx   # additionally write the file to disk
//
// Hermetic: temp wiki dir, no LLM, no Telegram. Verifies:
//   - BuildDOCX produces a valid OOXML zip with the three required parts
//   - title + heading + paragraph + bullet + table all survive round trip
//   - XML reserved chars in user content are escaped (no <script> leak)
//   - source store dedups identical specs (sha256-keyed)
//   - tools.DocumentSender invoked when deliver=true with user context
//   - tools.DocumentSender skipped when deliver=false
//   - filename sanitization strips path separators and forces .docx
package main

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

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
		outFile = flag.String("out", "", "if set, write the generated document to this path for visual inspection")
		keep    = flag.Bool("keep-wiki", false, "keep the temp wiki dir at exit (path printed)")
	)
	flag.Parse()

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))

	wikiDir, err := os.MkdirTemp("", "aura-debug-docx-*")
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
	tool := tools.NewCreateDOCXTool(store, sender)
	if tool == nil {
		fail("NewCreateDOCXTool returned nil")
	}

	scenario("scenario 1: full document with delivery", func() error {
		ctx := tools.WithUserID(context.Background(), "999")
		args := map[string]any{
			"filename": "quarterly-memo",
			"title":    "Quarterly Report",
			"blocks": []any{
				map[string]any{"kind": "paragraph", "text": "Executive summary follows."},
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
			"caption": "Q1 memo",
		}
		out, err := tool.Execute(ctx, args)
		if err != nil {
			return fmt.Errorf("execute: %w", err)
		}
		var resp map[string]any
		if err := json.Unmarshal([]byte(out), &resp); err != nil {
			return fmt.Errorf("unmarshal: %w", err)
		}
		mustEqual("filename", resp["filename"], "quarterly-memo.docx")
		mustEqual("delivered", resp["delivered"], true)
		if len(sender.calls) != 1 {
			return fmt.Errorf("sender called %d times, want 1", len(sender.calls))
		}
		mustEqual("call.filename", sender.calls[0].filename, "quarterly-memo.docx")

		// Round-trip the persisted file: verify all three required OOXML
		// parts and that user content survived.
		id, _ := resp["source_id"].(string)
		body, err := os.ReadFile(store.Path(id, "original.docx"))
		if err != nil {
			return fmt.Errorf("read persisted: %w", err)
		}
		zr, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
		if err != nil {
			return fmt.Errorf("zip.NewReader: %w", err)
		}
		var (
			seenContentTypes, seenRels, seenDoc bool
			docXML                              string
		)
		for _, f := range zr.File {
			switch f.Name {
			case "[Content_Types].xml":
				seenContentTypes = true
			case "_rels/.rels":
				seenRels = true
			case "word/document.xml":
				seenDoc = true
				rc, _ := f.Open()
				b, _ := io.ReadAll(rc)
				rc.Close()
				docXML = string(b)
			}
		}
		if !seenContentTypes || !seenRels || !seenDoc {
			return fmt.Errorf("missing required parts: ct=%v rels=%v doc=%v", seenContentTypes, seenRels, seenDoc)
		}
		for _, want := range []string{"Quarterly Report", "Highlights", "• Revenue up 12%", "<w:tbl>"} {
			if !strings.Contains(docXML, want) {
				return fmt.Errorf("document.xml missing %q", want)
			}
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

	scenario("scenario 2: XML escape (script tag does not leak)", func() error {
		ctx := tools.WithUserID(context.Background(), "999")
		sender.calls = sender.calls[:0]
		args := map[string]any{
			"filename": "escape-test",
			"blocks": []any{
				map[string]any{"kind": "paragraph", "text": `<script>alert("xss")</script> & "quotes"`},
			},
			"deliver": false,
		}
		out, err := tool.Execute(ctx, args)
		if err != nil {
			return fmt.Errorf("execute: %w", err)
		}
		var resp map[string]any
		_ = json.Unmarshal([]byte(out), &resp)
		if len(sender.calls) != 0 {
			return fmt.Errorf("sender invoked despite deliver=false")
		}
		id, _ := resp["source_id"].(string)
		body, _ := os.ReadFile(store.Path(id, "original.docx"))
		zr, _ := zip.NewReader(bytes.NewReader(body), int64(len(body)))
		var docXML string
		for _, f := range zr.File {
			if f.Name == "word/document.xml" {
				rc, _ := f.Open()
				b, _ := io.ReadAll(rc)
				rc.Close()
				docXML = string(b)
				break
			}
		}
		if strings.Contains(docXML, "<script>") {
			return fmt.Errorf("raw <script> tag leaked into document.xml")
		}
		if !strings.Contains(docXML, "&lt;script&gt;") {
			return fmt.Errorf("expected escaped <script> in document.xml")
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

	scenario("scenario 4: filename sanitization", func() error {
		got := files.SanitizeDOCXFilename(`../../etc/passwd`)
		mustEqual("traversal sanitized", got, "passwd.docx")
		got = files.SanitizeDOCXFilename(`C:\Users\evil\report`)
		mustEqual("windows path sanitized", got, "report.docx")
		return nil
	})

	scenario("scenario 5: caps enforced", func() error {
		blocks := make([]any, files.MaxDOCXBlocks+1)
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
			return fmt.Errorf("expected cap error for %d blocks", files.MaxDOCXBlocks+1)
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
	fmt.Fprintf(os.Stderr, "debug_docx: "+format+"\n", args...)
	os.Exit(1)
}
