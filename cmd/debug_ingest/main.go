// debug_ingest is the slice-9 natural-prompt smoke harness for the
// source / ingest / wiki-maintenance / scheduler tools.
//
//	go run ./cmd/debug_ingest               # all default scenarios
//	go run ./cmd/debug_ingest -keep-wiki    # keep temp wiki for inspection
//
// Reads LLM_API_KEY + EMBEDDING_API_KEY from .env. Spins up a temp wiki
// + temp SQLite for the scheduler so the run is hermetic. Each scenario
// drives the LLM with a single user prompt and asserts:
//   - the expected tool(s) fired
//   - the final text or tool result contains/excludes the right strings
//
// Pre-seeds:
//   - one stored text source (kind=text, status=stored)
//   - one ocr_complete source with a hand-written ocr.md so
//     ingest_source has something to compile
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aura/aura/internal/conversation"
	"github.com/aura/aura/internal/conversation/summarizer"
	"github.com/aura/aura/internal/ingest"
	"github.com/aura/aura/internal/llm"
	"github.com/aura/aura/internal/scheduler"
	"github.com/aura/aura/internal/search"
	"github.com/aura/aura/internal/source"
	"github.com/aura/aura/internal/tools"
	"github.com/aura/aura/internal/wiki"
	"github.com/philippgille/chromem-go"
)

type scenario struct {
	name       string
	prompt     string
	wantTools  []string
	wantText   []string
	rejectText []string
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
	embeddingAPIKey := os.Getenv("EMBEDDING_API_KEY")
	if embeddingAPIKey == "" {
		fmt.Println("FAIL: EMBEDDING_API_KEY is required")
		os.Exit(1)
	}
	embeddingBaseURL := envDefault("EMBEDDING_BASE_URL", "https://api.mistral.ai/v1")
	embeddingModel := envDefault("EMBEDDING_MODEL", "mistral-embed")

	wikiDir, err := os.MkdirTemp("", "aura-debug-ingest-*")
	if err != nil {
		fmt.Printf("FAIL: create temp wiki: %v\n", err)
		os.Exit(1)
	}
	if !*keepWiki {
		defer os.RemoveAll(wikiDir)
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelWarn}))

	wikiStore, err := wiki.NewStore(wikiDir, logger)
	if err != nil {
		fmt.Printf("FAIL: wiki.NewStore: %v\n", err)
		os.Exit(1)
	}
	srcStore, err := source.NewStore(wikiDir, logger)
	if err != nil {
		fmt.Printf("FAIL: source.NewStore: %v\n", err)
		os.Exit(1)
	}
	embedFn := chromem.NewEmbeddingFuncOpenAICompat(embeddingBaseURL, embeddingAPIKey, embeddingModel, ptrBool(true))
	engine, err := search.NewEngine(wikiDir, embedFn, logger)
	if err != nil {
		fmt.Printf("FAIL: search.NewEngine: %v\n", err)
		os.Exit(1)
	}
	pipeline, err := ingest.New(ingest.Config{
		Sources: srcStore,
		Wiki:    wikiStore,
		Search:  engine,
		Logger:  logger,
	})
	if err != nil {
		fmt.Printf("FAIL: ingest.New: %v\n", err)
		os.Exit(1)
	}

	schedDB := filepath.Join(wikiDir, "scheduler.db")
	schedStore, err := scheduler.OpenStore(schedDB)
	if err != nil {
		fmt.Printf("FAIL: scheduler.OpenStore: %v\n", err)
		os.Exit(1)
	}
	defer schedStore.Close()
	summariesStore := summarizer.NewSummariesStore(schedStore.DB())
	issuesStore := scheduler.NewIssuesStore(schedStore.DB())
	archiveStore, err := conversation.NewArchiveStore(schedStore.DB())
	if err != nil {
		fmt.Printf("FAIL: conversation.NewArchiveStore: %v\n", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	storedID, ocrID, err := seed(ctx, srcStore)
	if err != nil {
		fmt.Printf("FAIL: seed: %v\n", err)
		os.Exit(1)
	}
	if err := seedBriefing(ctx, schedStore, summariesStore, issuesStore, archiveStore); err != nil {
		fmt.Printf("FAIL: seed briefing: %v\n", err)
		os.Exit(1)
	}

	reg := tools.NewRegistry(logger)
	reg.Register(tools.NewStoreSourceTool(srcStore))
	reg.Register(tools.NewReadSourceTool(srcStore))
	reg.Register(tools.NewListSourcesTool(srcStore))
	reg.Register(tools.NewLintSourcesTool(srcStore))
	reg.Register(tools.NewIngestSourceTool(pipeline))
	if tool := tools.NewSearchMemoryTool(engine, srcStore, archiveStore); tool != nil {
		reg.Register(tool)
	}
	reg.Register(tools.NewListWikiTool(wikiStore))
	reg.Register(tools.NewLintWikiTool(wikiStore))
	reg.Register(tools.NewRebuildIndexTool(wikiStore))
	reg.Register(tools.NewAppendLogTool(wikiStore))
	reg.Register(tools.NewScheduleTaskTool(schedStore, time.Local))
	reg.Register(tools.NewListTasksTool(schedStore))
	reg.Register(tools.NewCancelTaskTool(schedStore))
	if tool := tools.NewDailyBriefingTool(schedStore, srcStore, summariesStore, issuesStore, archiveStore, time.Local); tool != nil {
		reg.Register(tool)
	}

	client := llm.NewOpenAIClient(llm.OpenAIConfig{
		APIKey:  apiKey,
		BaseURL: baseURL,
		Model:   model,
	})

	scenarios := []scenario{
		{
			name:      "list_sources",
			prompt:    "Use the source tools to show me every source currently stored. Don't filter.",
			wantTools: []string{"list_sources"},
			wantText:  []string{storedID, ocrID},
		},
		{
			name:      "read_source_metadata",
			prompt:    fmt.Sprintf("Read the metadata for source %s and tell me its filename.", storedID),
			wantTools: []string{"read_source"},
			wantText:  []string{"smoke-note.txt"},
		},
		{
			name:      "lint_sources",
			prompt:    "Run a lint pass over all sources and tell me which ones are awaiting OCR or awaiting ingest.",
			wantTools: []string{"lint_sources"},
			wantText:  []string{ocrID},
		},
		{
			name:      "ingest_source",
			prompt:    fmt.Sprintf("The source %s is ready to ingest. Compile it into a wiki page using the ingest tool.", ocrID),
			wantTools: []string{"ingest_source"},
			wantText:  []string{"compiled"},
		},
		{
			name:      "list_wiki_after_ingest",
			prompt:    "List every wiki page in the sources category.",
			wantTools: []string{"list_wiki"},
			wantText:  []string{"source-aura-debug-ingest-fixture"},
		},
		{
			name:      "lint_wiki",
			prompt:    "Run a wiki health check and report broken links or missing categories. Use the lint tool.",
			wantTools: []string{"lint_wiki"},
		},
		{
			name:      "append_log",
			prompt:    "Append a log entry with action \"smoke-test\" so we have a record this run happened.",
			wantTools: []string{"append_log"},
			wantText:  []string{"smoke-test"},
		},
		{
			name:      "daily_briefing",
			prompt:    "Dammi il briefing di oggi in 5 punti. Usa il briefing giornaliero se disponibile.",
			wantTools: []string{"daily_briefing"},
			wantText:  []string{"Daily briefing", "smoke-briefing", "briefing smoke issue", "aura-debug-ingest-fixture.pdf"},
		},
		{
			name:      "search_memory",
			prompt:    "Cerca nella memoria locale di Aura il marker gold-742 e dimmi quali evidenze trovi. Usa search_memory.",
			wantTools: []string{"search_memory"},
			wantText:  []string{"Memory evidence", "gold-742", ocrID, "page=1"},
		},
		{
			name:      "schedule_task_in",
			prompt:    "Schedule a wiki maintenance pass to run in 90 seconds. Use the relative-duration field. Name it slice9-smoke.",
			wantTools: []string{"schedule_task"},
			wantText:  []string{"slice9-smoke"},
		},
		{
			name:      "schedule_task_every_minutes",
			prompt:    "Schedule a wiki maintenance pass every 60 minutes. Name it slice17-every-smoke.",
			wantTools: []string{"schedule_task"},
			wantText:  []string{"slice17-every-smoke", "every 60 minutes"},
		},
		{
			name:      "schedule_task_weekdays",
			prompt:    "Schedule a reminder every business day at 10:00 local time. Name it slice17-weekday-smoke and set the reminder text to weekday smoke.",
			wantTools: []string{"schedule_task"},
			wantText:  []string{"slice17-weekday-smoke", "mon,tue,wed,thu,fri"},
		},
		{
			name:      "schedule_agent_job",
			prompt:    "Schedule a propose-only agent job every 60 minutes. Name it slice17-agent-smoke. Its goal is to check Aura sources and propose useful wiki updates.",
			wantTools: []string{"schedule_task"},
			wantText:  []string{"slice17-agent-smoke", "agent_job", "every 60 minutes"},
		},
		{
			name:      "list_tasks",
			prompt:    "List every scheduled task you currently know about.",
			wantTools: []string{"list_tasks"},
			wantText:  []string{"slice9-smoke", "slice17-every-smoke", "slice17-weekday-smoke", "slice17-agent-smoke"},
		},
		{
			name:      "cancel_task",
			prompt:    "Cancel the slice9-smoke task we just scheduled.",
			wantTools: []string{"cancel_task"},
			wantText:  []string{"slice9-smoke"},
		},
	}

	fmt.Printf("Slice 9 natural-prompt smoke test\n")
	fmt.Printf("model=%s base_url=%s wiki=%s\n", model, baseURL, wikiDir)
	fmt.Printf("seeded text source: %s\nseeded ocr_complete source: %s\n\n", storedID, ocrID)

	failures := 0
	for _, sc := range scenarios {
		started := time.Now()
		called, final, toolResults, err := runScenario(ctx, client, reg, model, sc.prompt)
		elapsed := time.Since(started)
		text := final + "\n" + strings.Join(toolResults, "\n")
		ok := err == nil &&
			containsTools(called, sc.wantTools) &&
			containsAllText(text, sc.wantText) &&
			containsNoText(text, append(sc.rejectText, "(tool error)"))

		status := "PASS"
		if !ok {
			status = "FAIL"
			failures++
		}
		fmt.Printf("[%s] %s\n", status, sc.name)
		fmt.Printf("  elapsed_ms: %d tool_calls: %d\n", elapsed.Milliseconds(), len(called))
		fmt.Printf("  wanted: %s\n", strings.Join(sc.wantTools, ", "))
		fmt.Printf("  called: %s\n", strings.Join(called, ", "))
		if len(sc.wantText) > 0 {
			fmt.Printf("  expected text: %s\n", strings.Join(sc.wantText, ", "))
		}
		if err != nil {
			fmt.Printf("  error: %v\n", err)
		}
		if final != "" {
			fmt.Printf("  final: %s\n", singleLine(final, 220))
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

// seed creates two sources: a plain text source (status=stored) and an
// ocr_complete PDF source with a hand-written ocr.md so ingest_source
// has something to compile without needing a live Mistral OCR call.
func seed(ctx context.Context, store *source.Store) (storedID, ocrID string, err error) {
	stored, _, err := store.Put(ctx, source.PutInput{
		Kind:     source.KindText,
		Filename: "smoke-note.txt",
		MimeType: "text/plain",
		Bytes:    []byte("Slice 9 stored-text source. Marker: cobalt-913."),
	})
	if err != nil {
		return "", "", fmt.Errorf("put text source: %w", err)
	}

	pdfBody := []byte("%PDF-1.4 fake aura-debug-ingest fixture")
	ocrSrc, _, err := store.Put(ctx, source.PutInput{
		Kind:     source.KindPDF,
		Filename: "aura-debug-ingest-fixture.pdf",
		MimeType: "application/pdf",
		Bytes:    pdfBody,
	})
	if err != nil {
		return "", "", fmt.Errorf("put pdf source: %w", err)
	}

	ocrMD := fmt.Sprintf("# Source OCR: %s\n\nSource ID: %s\nModel: fixture\n\n## Page 1\n\nThe slice 9 ingest fixture marker is gold-742. This text exists so ingest_source has a real preview to embed.\n",
		"aura-debug-ingest-fixture.pdf", ocrSrc.ID)
	if err := os.WriteFile(store.Path(ocrSrc.ID, "ocr.md"), []byte(ocrMD), 0o644); err != nil {
		return "", "", fmt.Errorf("write ocr.md: %w", err)
	}
	if _, err := store.Update(ocrSrc.ID, func(s *source.Source) error {
		s.Status = source.StatusOCRComplete
		s.OCRModel = "fixture"
		s.PageCount = 1
		return nil
	}); err != nil {
		return "", "", fmt.Errorf("flip ocr_complete: %w", err)
	}
	return stored.ID, ocrSrc.ID, nil
}

func seedBriefing(
	ctx context.Context,
	schedStore *scheduler.Store,
	summariesStore *summarizer.SummariesStore,
	issuesStore *scheduler.IssuesStore,
	archiveStore *conversation.ArchiveStore,
) error {
	now := time.Now().UTC()
	_, err := schedStore.Upsert(ctx, &scheduler.Task{
		Name:         "smoke-briefing-task",
		Kind:         scheduler.KindReminder,
		Payload:      "review today's Aura usefulness smoke test",
		ScheduleKind: scheduler.ScheduleAt,
		ScheduleAt:   now.Add(2 * time.Hour),
		NextRunAt:    now.Add(2 * time.Hour),
		Status:       scheduler.StatusActive,
	})
	if err != nil {
		return fmt.Errorf("upsert briefing task: %w", err)
	}
	if _, err := summariesStore.Propose(ctx, summarizer.ProposalInput{
		Fact:       "Create [[smoke-briefing]] to track whether daily briefings are useful.",
		Action:     string(summarizer.ActionNew),
		Similarity: 0.9,
	}); err != nil {
		return fmt.Errorf("proposal: %w", err)
	}
	if err := issuesStore.Enqueue(ctx, scheduler.Issue{
		Kind:     "missing_category",
		Severity: "medium",
		Slug:     "smoke-briefing",
		Message:  "briefing smoke issue: add category before this becomes real knowledge.",
	}); err != nil {
		return fmt.Errorf("issue: %w", err)
	}
	if err := archiveStore.Append(ctx, conversation.Turn{
		ChatID:    1,
		UserID:    1,
		TurnIndex: 1,
		Role:      "user",
		Content:   "We need Aura to answer real daily questions, not just pass technical demos.",
	}); err != nil {
		return fmt.Errorf("archive: %w", err)
	}
	return nil
}

func runScenario(ctx context.Context, client llm.Client, reg *tools.Registry, model, prompt string) ([]string, string, []string, error) {
	// Reminder reach into context isn't exercised here (the harness runs
	// outside Telegram), but threading a synthetic user ID lets the
	// reminder branch of schedule_task work uniformly. Wiki-maintenance
	// scheduling — the only kind we test — doesn't need it.
	ctx = tools.WithUserID(ctx, "debug-ingest-harness")

	messages := []llm.Message{
		{Role: "system", Content: conversation.RenderSystemPrompt(time.Now(), time.Local) +
			"\n\nFor this smoke test, use the tool that matches the user's request. Do not answer from memory when a tool can be used."},
		{Role: "user", Content: prompt},
	}

	var called []string
	var toolResults []string
	var lastToolResult string
	for range 8 {
		resp, err := client.Send(ctx, llm.Request{
			Messages:    messages,
			Model:       model,
			Temperature: llm.Float64Ptr(0),
			Tools:       reg.Definitions(),
		})
		if err != nil {
			return called, "", toolResults, err
		}
		if !resp.HasToolCalls {
			final := strings.TrimSpace(resp.Content)
			if final == "" {
				final = lastToolResult
			}
			return called, final, toolResults, nil
		}
		messages = append(messages, llm.Message{Role: "assistant", Content: resp.Content, ToolCalls: resp.ToolCalls})
		for _, tc := range resp.ToolCalls {
			called = append(called, tc.Name)
			result, execErr := reg.Execute(ctx, tc.Name, tc.Arguments)
			if execErr != nil {
				result = "(tool error) " + execErr.Error()
			}
			lastToolResult = result
			toolResults = append(toolResults, result)
			messages = append(messages, llm.Message{Role: "tool", Content: result, ToolCallID: tc.ID})
		}
	}
	return called, lastToolResult, toolResults, fmt.Errorf("max tool iterations reached")
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

func ptrBool(b bool) *bool { return &b }

func containsTools(called, wants []string) bool {
	seen := make(map[string]bool, len(called))
	for _, name := range called {
		seen[name] = true
	}
	for _, want := range wants {
		if !seen[want] {
			return false
		}
	}
	return true
}

func containsAllText(text string, wants []string) bool {
	text = strings.ToLower(text)
	for _, want := range wants {
		if !strings.Contains(text, strings.ToLower(want)) {
			return false
		}
	}
	return true
}

func containsNoText(text string, rejects []string) bool {
	text = strings.ToLower(text)
	for _, reject := range rejects {
		if strings.TrimSpace(reject) == "" {
			continue
		}
		if strings.Contains(text, strings.ToLower(reject)) {
			return false
		}
	}
	return true
}

func singleLine(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) <= max {
		return s
	}
	return s[:max] + "..."
}
