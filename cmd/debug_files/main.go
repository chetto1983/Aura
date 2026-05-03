// debug_files is the slice-15e natural-prompt smoke harness for Aura's
// file creation tools.
//
//	go run ./cmd/debug_files
//	go run ./cmd/debug_files -keep-wiki
//
// Reads LLM_API_KEY from .env, spins up a temp wiki/source store, registers
// create_xlsx/create_docx/create_pdf, and verifies that ordinary prompts make
// the model select the right tool and produce a persisted, deliverable file.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/aura/aura/internal/conversation"
	"github.com/aura/aura/internal/llm"
	"github.com/aura/aura/internal/source"
	"github.com/aura/aura/internal/tools"
)

type scenario struct {
	name      string
	prompt    string
	wantTool  string
	wantKind  source.Kind
	wantAsset string
	wantExt   string
}

type stubSender struct {
	calls []stubCall
}

type stubCall struct {
	userID, filename, caption string
	bodyLen                   int
}

func (s *stubSender) SendDocumentToUser(userID, filename string, body []byte, caption string) error {
	s.calls = append(s.calls, stubCall{
		userID:   userID,
		filename: filename,
		caption:  caption,
		bodyLen:  len(body),
	})
	return nil
}

func main() {
	keepWiki := flag.Bool("keep-wiki", false, "keep the temporary wiki directory after the run")
	flag.Parse()

	if err := loadDotEnv(".env"); err != nil && !errors.Is(err, os.ErrNotExist) {
		fmt.Printf("warning: could not load .env: %v\n", err)
	}

	apiKey := os.Getenv("LLM_API_KEY")
	if apiKey == "" {
		fmt.Println("FAIL: LLM_API_KEY is required")
		os.Exit(1)
	}
	baseURL := envDefault("LLM_BASE_URL", "https://api.openai.com/v1")
	model := envDefault("LLM_MODEL", "gpt-4")

	wikiDir, err := os.MkdirTemp("", "aura-debug-files-*")
	if err != nil {
		fail("create temp wiki: %v", err)
	}
	if !*keepWiki {
		defer os.RemoveAll(wikiDir)
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelWarn}))
	store, err := source.NewStore(wikiDir, logger)
	if err != nil {
		fail("source.NewStore: %v", err)
	}
	sender := &stubSender{}

	reg := tools.NewRegistry(logger)
	reg.Register(tools.NewCreateXLSXTool(store, sender))
	reg.Register(tools.NewCreateDOCXTool(store, sender))
	reg.Register(tools.NewCreatePDFTool(store, sender))

	client := llm.NewOpenAIClient(llm.OpenAIConfig{
		APIKey:  apiKey,
		BaseURL: baseURL,
		Model:   model,
	})

	scenarios := []scenario{
		{
			name: "xlsx_budget",
			prompt: "Create and send me an Excel spreadsheet named natural-budget with one sheet called Budget. " +
				"Include columns category, planned, actual and rows for rent 1200 1200, food 450 475, software 80 99.",
			wantTool:  "create_xlsx",
			wantKind:  source.KindXLSX,
			wantAsset: "original.xlsx",
			wantExt:   ".xlsx",
		},
		{
			name: "docx_memo",
			prompt: "Create and send me an editable Word memo named natural-status-memo. " +
				"Title it Weekly Status Memo. Include a short overview paragraph, two bullet highlights, and a small risks table.",
			wantTool:  "create_docx",
			wantKind:  source.KindDOCX,
			wantAsset: "original.docx",
			wantExt:   ".docx",
		},
		{
			name: "pdf_invoice",
			prompt: "Create and send me a PDF named natural-invoice-summary. " +
				"Title it Invoice Summary. Add a paragraph, two bullet notes, and a table with item and amount columns.",
			wantTool:  "create_pdf",
			wantKind:  source.KindPDFGen,
			wantAsset: "original.pdf",
			wantExt:   ".pdf",
		},
	}

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	fmt.Printf("Slice 15e natural file-creation smoke test\n")
	fmt.Printf("model=%s base_url=%s wiki=%s\n\n", model, baseURL, wikiDir)

	failures := 0
	for _, sc := range scenarios {
		before := len(sender.calls)
		called, final, toolResults, err := runScenario(ctx, client, reg, model, sc.prompt)
		resp, resultErr := parseToolResponse(toolResults, sc.wantTool)
		if resultErr != nil && err == nil {
			err = resultErr
		}

		ok := err == nil &&
			containsTool(called, sc.wantTool) &&
			validateResponse(store, sender, before, resp, sc) == nil

		status := "PASS"
		if !ok {
			status = "FAIL"
			failures++
		}
		fmt.Printf("[%s] %s\n", status, sc.name)
		fmt.Printf("  wanted: %s\n", sc.wantTool)
		fmt.Printf("  called: %s\n", strings.Join(called, ", "))
		if err != nil {
			fmt.Printf("  error: %v\n", err)
		} else if validateErr := validateResponse(store, sender, before, resp, sc); validateErr != nil {
			fmt.Printf("  error: %v\n", validateErr)
		}
		if final != "" {
			fmt.Printf("  final: %s\n", singleLine(final, 220))
		}
		if id, _ := resp["source_id"].(string); id != "" {
			fmt.Printf("  source: %s\n", id)
		}
		fmt.Println()
	}

	if *keepWiki {
		fmt.Printf("kept wiki: %s\n", wikiDir)
	}
	if failures > 0 {
		os.Exit(1)
	}
}

func runScenario(ctx context.Context, client llm.Client, reg *tools.Registry, model, prompt string) ([]string, string, []toolResult, error) {
	ctx = tools.WithUserID(ctx, "debug-files-harness")
	messages := []llm.Message{
		{Role: "system", Content: conversation.RenderSystemPrompt(time.Now(), time.Local) +
			"\n\nFor this smoke test, choose exactly the file creation tool that matches the requested output format. " +
			"Set deliver=true or omit it so the file is sent. Do not answer without creating the file."},
		{Role: "user", Content: prompt},
	}

	var called []string
	var results []toolResult
	var lastToolResult string
	for range 8 {
		resp, err := client.Send(ctx, llm.Request{
			Messages:    messages,
			Model:       model,
			Temperature: llm.Float64Ptr(0),
			Tools:       reg.Definitions(),
		})
		if err != nil {
			return called, "", results, err
		}
		if !resp.HasToolCalls {
			final := strings.TrimSpace(resp.Content)
			if final == "" {
				final = lastToolResult
			}
			return called, final, results, nil
		}
		messages = append(messages, llm.Message{Role: "assistant", Content: resp.Content, ToolCalls: resp.ToolCalls})
		for _, tc := range resp.ToolCalls {
			called = append(called, tc.Name)
			result, execErr := reg.Execute(ctx, tc.Name, tc.Arguments)
			if execErr != nil {
				result = "(tool error) " + execErr.Error()
			}
			lastToolResult = result
			results = append(results, toolResult{Name: tc.Name, Body: result})
			messages = append(messages, llm.Message{Role: "tool", Content: result, ToolCallID: tc.ID})
		}
	}
	return called, lastToolResult, results, fmt.Errorf("max tool iterations reached")
}

type toolResult struct {
	Name string
	Body string
}

func parseToolResponse(results []toolResult, name string) (map[string]any, error) {
	for _, r := range results {
		if r.Name != name {
			continue
		}
		if strings.HasPrefix(r.Body, "(tool error)") {
			return nil, errors.New(r.Body)
		}
		var resp map[string]any
		if err := json.Unmarshal([]byte(r.Body), &resp); err != nil {
			return nil, fmt.Errorf("parse %s result: %w", name, err)
		}
		return resp, nil
	}
	return nil, fmt.Errorf("%s result not found", name)
}

func validateResponse(store *source.Store, sender *stubSender, before int, resp map[string]any, sc scenario) error {
	if resp == nil {
		return errors.New("missing tool response")
	}
	id, _ := resp["source_id"].(string)
	if id == "" {
		return errors.New("missing source_id")
	}
	filename, _ := resp["filename"].(string)
	if !strings.HasSuffix(strings.ToLower(filename), sc.wantExt) {
		return fmt.Errorf("filename %q does not end with %s", filename, sc.wantExt)
	}
	if resp["delivered"] != true {
		return fmt.Errorf("delivered = %v, want true", resp["delivered"])
	}

	src, err := store.Get(id)
	if err != nil {
		return fmt.Errorf("read source %s: %w", id, err)
	}
	if src.Kind != sc.wantKind {
		return fmt.Errorf("kind = %q, want %q", src.Kind, sc.wantKind)
	}
	if src.Status != source.StatusIngested {
		return fmt.Errorf("status = %q, want %q", src.Status, source.StatusIngested)
	}
	body, err := os.ReadFile(store.Path(id, sc.wantAsset))
	if err != nil {
		return fmt.Errorf("read %s: %w", sc.wantAsset, err)
	}
	if len(body) == 0 {
		return fmt.Errorf("%s is empty", sc.wantAsset)
	}
	if len(sender.calls) != before+1 {
		return fmt.Errorf("sender calls = %d after scenario, want %d", len(sender.calls), before+1)
	}
	call := sender.calls[len(sender.calls)-1]
	if call.userID != "debug-files-harness" {
		return fmt.Errorf("sender userID = %q", call.userID)
	}
	if call.filename != filename {
		return fmt.Errorf("sender filename = %q, want %q", call.filename, filename)
	}
	if call.bodyLen == 0 {
		return errors.New("sender got empty body")
	}
	return nil
}

func loadDotEnv(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.Trim(strings.TrimSpace(value), `"'`)
		if key != "" {
			os.Setenv(key, value)
		}
	}
	return scanner.Err()
}

func envDefault(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}

func containsTool(called []string, want string) bool {
	for _, name := range called {
		if name == want {
			return true
		}
	}
	return false
}

func singleLine(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "debug_files: "+format+"\n", args...)
	os.Exit(1)
}
