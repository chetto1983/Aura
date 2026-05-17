// debug_ingest is the slice-9 natural-prompt smoke harness for the
// source / ingest / workspace-file / scheduler tools.
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
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aura/aura/cmd/debug_common"
	"github.com/aura/aura/internal/agent/tools/registry"
	"github.com/aura/aura/internal/conversation"
	"github.com/aura/aura/internal/conversation/summarizer"
	"github.com/aura/aura/internal/cron"
	"github.com/aura/aura/internal/llm"
	"github.com/aura/aura/internal/storage/memoryindex"
	"github.com/aura/aura/internal/storage/sources/ingest"
	"github.com/aura/aura/internal/storage/sources/store"
	"github.com/aura/aura/internal/wiki"
	"github.com/aura/aura/internal/workspace"
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

	apiKey := os.Getenv("LLM_API_KEY")
	if apiKey == "" {
		fmt.Println("FAIL: LLM_API_KEY is required")
		os.Exit(1)
	}
	baseURL := debugcommon.EnvDefault("LLM_BASE_URL", "https://api.openai.com/v1")
	model := debugcommon.EnvDefault("LLM_MODEL", "gpt-4")
	embeddingAPIKey := os.Getenv("EMBEDDING_API_KEY")
	if embeddingAPIKey == "" {
		fmt.Println("FAIL: EMBEDDING_API_KEY is required")
		os.Exit(1)
	}
	embeddingBaseURL := debugcommon.EnvDefault("EMBEDDING_BASE_URL", "https://api.mistral.ai/v1")
	embeddingModel := debugcommon.EnvDefault("EMBEDDING_MODEL", "mistral-embed")

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
	// debug_ingest exercises the ingest pipeline without a live Qdrant.
	// search.Repository is optional in the pipeline — passing nil disables
	// reindexing after each page write, which is fine for a single-shot
	// debug run.
	_ = embeddingBaseURL
	_ = embeddingAPIKey
	_ = embeddingModel
	pipeline, err := ingest.New(ingest.Config{
		Sources: srcStore,
		Wiki:    wikiStore,
		Search:  nil,
		Logger:  logger,
	})
	if err != nil {
		fmt.Printf("FAIL: ingest.New: %v\n", err)
		os.Exit(1)
	}
	workspaceRoot, err := workspace.New(wikiDir)
	if err != nil {
		fmt.Printf("FAIL: workspace.New: %v\n", err)
		os.Exit(1)
	}

	schedDB := filepath.Join(wikiDir, "scheduler.db")
	schedStore, err := cron.OpenStore(schedDB)
	if err != nil {
		fmt.Printf("FAIL: cron.OpenStore: %v\n", err)
		os.Exit(1)
	}
	defer schedStore.Close()
	summariesStore := summarizer.NewSummariesStore(schedStore.DB())
	issuesStore := cron.NewIssuesStore(schedStore.DB())
	archiveStore, err := conversation.NewArchiveStore(schedStore.DB())
	if err != nil {
		fmt.Printf("FAIL: conversation.NewArchiveStore: %v\n", err)
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
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
	memoryIndex, err := memoryindex.NewStore(schedStore.DB())
	if err != nil {
		fmt.Printf("FAIL: memoryindex.NewStore: %v\n", err)
		os.Exit(1)
	}
	if _, err := memoryindex.Rebuild(ctx, memoryIndex, memoryindex.RebuildInput{
		Sources:   srcStore,
		Archive:   archiveStore,
		Proposals: summariesStore,
	}); err != nil {
		fmt.Printf("FAIL: memoryindex.Rebuild: %v\n", err)
		os.Exit(1)
	}

	reg := tools.NewRegistry(logger)
	reg.Register(tools.NewStoreSourceTool(srcStore))
	reg.Register(tools.NewReadSourceTool(srcStore))
	reg.Register(tools.NewListSourcesTool(srcStore))
	reg.Register(tools.NewLintSourcesTool(srcStore))
	reg.Register(tools.NewIngestSourceTool(pipeline))
	if tool := tools.NewSearchMemoryTool(nil, memoryIndex); tool != nil {
		reg.Register(tool)
	}
	for _, tool := range tools.NewWorkspaceFileTools(workspaceRoot) {
		reg.Register(tool)
	}
	if tool := tools.NewTaskTool(schedStore, debugRunTaskNowRunner{store: schedStore}, time.Local); tool != nil {
		reg.Register(tool)
	}
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
			name:      "list_workspace_files_after_ingest",
			prompt:    "Use list_files on the workspace root to list markdown wiki files after ingest. Tell me the source page filename.",
			wantTools: []string{"list_files"},
			wantText:  []string{"source-aura-debug-ingest-fixture"},
		},
		{
			name:      "search_ingested_wiki_file",
			prompt:    "Use search_files over markdown files to find the ingested marker gold-742 and report the source page evidence.",
			wantTools: []string{"search_files"},
			wantText:  []string{"gold-742", "source-aura-debug-ingest-fixture"},
		},
		{
			name:      "write_audit_file",
			prompt:    "Use write_file to create audit/smoke-log.md with action \"smoke-test\", then read_file it back and report the action.",
			wantTools: []string{"write_file", "read_file"},
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
			wantTools: []string{"task"},
			wantText:  []string{"slice9-smoke"},
		},
		{
			name:      "schedule_task_every_minutes",
			prompt:    "Schedule a wiki maintenance pass every 60 minutes. Name it slice17-every-smoke.",
			wantTools: []string{"task"},
			wantText:  []string{"slice17-every-smoke", "every 60 minutes"},
		},
		{
			name:      "schedule_task_weekdays",
			prompt:    "Schedule a reminder every business day at 10:00 local time. Name it slice17-weekday-smoke and set the reminder text to weekday smoke.",
			wantTools: []string{"task"},
			wantText:  []string{"slice17-weekday-smoke", "mon,tue,wed,thu,fri"},
		},
		{
			name:      "schedule_agent_job",
			prompt:    "Schedule a propose-only agent job every 60 minutes. Name it slice17-agent-smoke. Its goal is to check Aura sources and propose useful wiki updates.",
			wantTools: []string{"task"},
			wantText:  []string{"slice17-agent-smoke", "agent_job", "every 60 minutes"},
		},
		{
			name:      "run_task_now",
			prompt:    "Esegui adesso il task schedulato slice17-agent-smoke. Usa l'azione run_now del tool task, non creare un nuovo swarm.",
			wantTools: []string{"task"},
			wantText:  []string{"slice17-agent-smoke", "completed", "debug run_task_now"},
		},
		{
			name:      "list_tasks",
			prompt:    "List every scheduled task you currently know about.",
			wantTools: []string{"task"},
			wantText:  []string{"slice9-smoke", "slice17-every-smoke", "slice17-weekday-smoke", "slice17-agent-smoke"},
		},
		{
			name:      "cancel_task",
			prompt:    "Cancel the slice9-smoke task we just scheduled.",
			wantTools: []string{"task"},
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
			debugcommon.ContainsTools(called, sc.wantTools) &&
			debugcommon.ContainsAllText(text, sc.wantText) &&
			debugcommon.ContainsNoText(text, append(sc.rejectText, "(tool error)"))

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
			fmt.Printf("  final: %s\n", debugcommon.SingleLine(final, 220))
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
	schedStore *cron.Store,
	summariesStore *summarizer.SummariesStore,
	issuesStore *cron.IssuesStore,
	archiveStore *conversation.ArchiveStore,
) error {
	now := time.Now().UTC()
	_, err := schedStore.Upsert(ctx, &cron.Task{
		Name:         "smoke-briefing-task",
		Kind:         cron.KindReminder,
		Payload:      "review today's Aura usefulness smoke test",
		ScheduleKind: cron.ScheduleAt,
		ScheduleAt:   now.Add(2 * time.Hour),
		NextRunAt:    now.Add(2 * time.Hour),
		Status:       cron.StatusActive,
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
	if err := issuesStore.Enqueue(ctx, cron.Issue{
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

type debugRunTaskNowRunner struct {
	store *cron.Store
}

func (r debugRunTaskNowRunner) RunTaskNow(ctx context.Context, name string) (tools.RunTaskNowResult, error) {
	task, err := r.store.GetByName(ctx, name)
	if err != nil {
		return tools.RunTaskNowResult{}, err
	}
	if task.Kind != cron.KindAgentJob {
		return tools.RunTaskNowResult{}, fmt.Errorf("debug run_task_now: task %q is kind %s", task.Name, task.Kind)
	}
	if err := r.store.RecordManualRun(ctx, task.ID, time.Now().UTC(), ""); err != nil {
		return tools.RunTaskNowResult{}, err
	}
	return tools.RunTaskNowResult{
		OK:        true,
		Name:      task.Name,
		Kind:      string(task.Kind),
		Status:    "completed",
		Summary:   "debug run_task_now completed for saved agent_job",
		LLMCalls:  1,
		ToolCalls: 1,
		ElapsedMS: 25,
		Notified:  true,
	}, nil
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
