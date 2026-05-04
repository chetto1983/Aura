// debug_agent_jobs proves the scheduled agent_job run -> skip -> mutate ->
// rerun wake-gate contract in a hermetic wiki and SQLite scheduler DB.
//
//	go run ./cmd/debug_agent_jobs
//	go run ./cmd/debug_agent_jobs -json
//	go run ./cmd/debug_agent_jobs -live-llm
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/aura/aura/internal/agent"
	"github.com/aura/aura/internal/llm"
	"github.com/aura/aura/internal/scheduler"
	"github.com/aura/aura/internal/tools"
	"github.com/aura/aura/internal/wiki"
)

const monitorSlug = "phase-19-monitor"

type options struct {
	JSON    bool
	Keep    bool
	LiveLLM bool
	Timeout time.Duration
}

type report struct {
	OK       bool        `json:"ok"`
	Mode     string      `json:"mode"`
	Model    string      `json:"model,omitempty"`
	WikiDir  string      `json:"wiki_dir"`
	DBPath   string      `json:"db_path"`
	Runs     []runReport `json:"runs"`
	Warnings []string    `json:"warnings,omitempty"`
}

type runReport struct {
	RunNumber            int    `json:"run_number"`
	Skipped              bool   `json:"skipped"`
	LLMCalls             int    `json:"llm_calls"`
	ToolCalls            int    `json:"tool_calls"`
	TokensPrompt         int    `json:"tokens_prompt"`
	TokensCompletion     int    `json:"tokens_completion"`
	TokensTotal          int    `json:"tokens_total"`
	ElapsedMS            int64  `json:"elapsed_ms"`
	WakeSignature        string `json:"wake_signature"`
	WakeSignatureChanged bool   `json:"wake_signature_changed"`
	LastOutputPreview    string `json:"last_output_preview"`
}

type harness struct {
	store  *scheduler.Store
	wiki   *wiki.Store
	runner *agent.Runner
}

func main() {
	opts := options{}
	flag.BoolVar(&opts.JSON, "json", false, "print machine-readable JSON only")
	flag.BoolVar(&opts.Keep, "keep", false, "keep the temporary wiki directory")
	flag.BoolVar(&opts.LiveLLM, "live-llm", false, "use LLM_API_KEY instead of the deterministic fake LLM")
	flag.DurationVar(&opts.Timeout, "timeout", 45*time.Second, "overall harness timeout")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), opts.Timeout)
	defer cancel()

	rep, cleanup, err := run(ctx, opts)
	if cleanup != nil && !opts.Keep {
		defer cleanup()
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "FAIL: %v\n", err)
		os.Exit(1)
	}
	if opts.JSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(rep)
	} else {
		printReport(rep)
	}
	if !rep.OK {
		os.Exit(1)
	}
}

func run(ctx context.Context, opts options) (report, func(), error) {
	tempDir, err := os.MkdirTemp("", "aura-debug-agent-jobs-*")
	if err != nil {
		return report{}, nil, fmt.Errorf("create temp dir: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(tempDir) }
	wikiDir := filepath.Join(tempDir, "wiki")
	dbPath := filepath.Join(tempDir, "scheduler.db")

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	wikiStore, err := wiki.NewStore(wikiDir, logger)
	if err != nil {
		return report{}, cleanup, fmt.Errorf("wiki store: %w", err)
	}
	if err := seedWiki(ctx, wikiStore, "Initial monitored state."); err != nil {
		return report{}, cleanup, err
	}
	store, err := scheduler.OpenStore(dbPath)
	if err != nil {
		return report{}, cleanup, fmt.Errorf("scheduler store: %w", err)
	}

	reg := tools.NewRegistry(logger)
	reg.Register(tools.NewReadWikiTool(wikiStore))

	model := ""
	var client llm.Client = newScriptedLLM()
	if opts.LiveLLM {
		if err := loadDotEnv(".env"); err != nil && !errors.Is(err, os.ErrNotExist) {
			_ = store.Close()
			return report{}, cleanup, fmt.Errorf("load .env: %w", err)
		}
		apiKey := strings.TrimSpace(os.Getenv("LLM_API_KEY"))
		if apiKey == "" {
			_ = store.Close()
			return report{}, cleanup, fmt.Errorf("LLM_API_KEY is required for -live-llm")
		}
		model = envDefault("LLM_MODEL", "gpt-4")
		client = llm.NewOpenAIClient(llm.OpenAIConfig{
			APIKey:  apiKey,
			BaseURL: envDefault("LLM_BASE_URL", "https://api.openai.com/v1"),
			Model:   model,
		})
	}
	runner, err := agent.NewRunner(agent.Config{
		LLM:           client,
		Tools:         reg,
		Model:         model,
		MaxIterations: 4,
		Timeout:       opts.Timeout,
		ToolTimeout:   10 * time.Second,
		Logger:        logger,
	})
	if err != nil {
		_ = store.Close()
		return report{}, cleanup, err
	}
	h := &harness{store: store, wiki: wikiStore, runner: runner}
	if err := h.seedTask(ctx); err != nil {
		_ = store.Close()
		return report{}, cleanup, err
	}

	rep := report{
		Mode:    "fake-llm",
		Model:   model,
		WikiDir: wikiDir,
		DBPath:  dbPath,
		Runs:    make([]runReport, 0, 3),
	}
	if opts.LiveLLM {
		rep.Mode = "live-llm"
	}
	for i := 1; i <= 3; i++ {
		row, err := h.runOnce(ctx, i)
		if err != nil {
			_ = store.Close()
			return report{}, cleanup, err
		}
		rep.Runs = append(rep.Runs, row)
		if i == 2 {
			if err := mutateWiki(ctx, wikiStore); err != nil {
				_ = store.Close()
				return report{}, cleanup, err
			}
		}
	}
	_ = store.Close()
	rep.OK = score(rep)
	if !rep.OK {
		rep.Warnings = append(rep.Warnings, "agent_job E2E gate failed: want execute, skip with zero LLM calls, mutate, execute again with changed signature")
	}
	return rep, cleanup, nil
}

func (h *harness) seedTask(ctx context.Context) error {
	notify := false
	payload, err := scheduler.AgentJobPayload{
		Goal:            "Inspect the monitored phase 19 page and report only material changes.",
		EnabledToolsets: []string{"memory_read"},
		Skills:          []string{"aura-implementation"},
		ContextFrom:     []string{"[[" + monitorSlug + "]]"},
		WakeIfChanged:   []string{"wiki:" + monitorSlug},
		Notify:          &notify,
	}.JSON()
	if err != nil {
		return err
	}
	_, err = h.store.Upsert(ctx, &scheduler.Task{
		Name:                 "phase19-agent-job-e2e",
		Kind:                 scheduler.KindAgentJob,
		Payload:              payload,
		RecipientID:          "9001",
		ScheduleKind:         scheduler.ScheduleEvery,
		ScheduleEveryMinutes: 60,
		NextRunAt:            time.Now().UTC().Add(time.Hour).Truncate(time.Second),
	})
	return err
}

func (h *harness) runOnce(ctx context.Context, runNumber int) (runReport, error) {
	task, err := h.store.GetByName(ctx, "phase19-agent-job-e2e")
	if err != nil {
		return runReport{}, err
	}
	payload, err := scheduler.NormalizeAgentJobPayload(task.Payload)
	if err != nil {
		return runReport{}, err
	}
	signature, _ := scheduler.AgentJobWakeSignature(ctx, payload, scheduler.AgentJobWakeDeps{
		Wiki:  h.wiki,
		Tasks: h.store,
	})
	previousSignature := task.WakeSignature
	result := agent.Result{Content: "Agent job skipped: wake_if_changed signals unchanged."}
	skipped := previousSignature != "" && previousSignature == signature
	if !skipped {
		result, err = h.runner.Run(ctx, agent.Task{
			SystemPrompt:       debugAgentJobSystemPrompt(),
			Prompt:             debugAgentJobPrompt(payload),
			ToolAllowlist:      payload.ToolAllowlist,
			UserID:             task.RecipientID,
			Temperature:        llm.Float64Ptr(0),
			MaxToolCalls:       2,
			MaxToolResultChars: 4000,
		})
		if err != nil {
			return runReport{}, err
		}
	}
	if err := h.recordResult(ctx, task, result, skipped, signature); err != nil {
		return runReport{}, err
	}
	return runReport{
		RunNumber:            runNumber,
		Skipped:              skipped,
		LLMCalls:             result.LLMCalls,
		ToolCalls:            result.ToolCalls,
		TokensPrompt:         result.Tokens.PromptTokens,
		TokensCompletion:     result.Tokens.CompletionTokens,
		TokensTotal:          result.Tokens.TotalTokens,
		ElapsedMS:            result.Elapsed.Milliseconds(),
		WakeSignature:        signature,
		WakeSignatureChanged: previousSignature != "" && previousSignature != signature,
		LastOutputPreview:    preview(result.Content, 160),
	}, nil
}

func (h *harness) recordResult(ctx context.Context, task *scheduler.Task, result agent.Result, skipped bool, signature string) error {
	metrics := map[string]any{
		"skipped":           skipped,
		"llm_calls":         result.LLMCalls,
		"tool_calls":        result.ToolCalls,
		"tokens_prompt":     result.Tokens.PromptTokens,
		"tokens_completion": result.Tokens.CompletionTokens,
		"tokens_total":      result.Tokens.TotalTokens,
		"elapsed_ms":        result.Elapsed.Milliseconds(),
	}
	data, err := json.Marshal(metrics)
	if err != nil {
		return err
	}
	if err := h.store.RecordAgentJobResult(ctx, task.ID, result.Content, string(data), signature); err != nil {
		return err
	}
	return h.store.RecordManualRun(ctx, task.ID, time.Now().UTC(), "")
}

func seedWiki(ctx context.Context, store *wiki.Store, body string) error {
	now := time.Date(2026, 5, 4, 9, 0, 0, 0, time.UTC).Format(time.RFC3339)
	return store.WritePage(ctx, &wiki.Page{
		Title:         "Phase 19 Monitor",
		Category:      "system",
		Tags:          []string{"phase-19", "agent-job"},
		SchemaVersion: wiki.CurrentSchemaVersion,
		PromptVersion: "v1",
		CreatedAt:     now,
		UpdatedAt:     now,
		Body:          body,
	})
}

func mutateWiki(ctx context.Context, store *wiki.Store) error {
	page, err := store.ReadPage(monitorSlug)
	if err != nil {
		return err
	}
	page.Body += "\n\nMutation: wake gate should rerun after this change."
	page.UpdatedAt = time.Date(2026, 5, 4, 9, 5, 0, 0, time.UTC).Format(time.RFC3339)
	return store.WritePage(ctx, page)
}

func debugAgentJobSystemPrompt() string {
	return "You are Aura running a scheduled agent_job harness. Call read_wiki for the monitored page before answering when that tool is available. Do not mutate wiki pages, sources, skills, settings, tasks, files, or external state. Return one concise sentence with what changed and whether a proposal is needed."
}

func debugAgentJobPrompt(payload scheduler.AgentJobPayload) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "Goal: %s", payload.Goal)
	fmt.Fprintf(&sb, "\n\nContext anchors: %s", strings.Join(payload.ContextFrom, ", "))
	fmt.Fprintf(&sb, "\nWake-if-changed signals: %s", strings.Join(payload.WakeIfChanged, ", "))
	if len(payload.Skills) > 0 {
		fmt.Fprintf(&sb, "\nAttached skills: %s", strings.Join(payload.Skills, ", "))
	}
	return sb.String()
}

func score(rep report) bool {
	if len(rep.Runs) != 3 {
		return false
	}
	first, second, third := rep.Runs[0], rep.Runs[1], rep.Runs[2]
	return !first.Skipped &&
		second.Skipped &&
		second.LLMCalls == 0 &&
		!third.Skipped &&
		third.WakeSignatureChanged &&
		first.WakeSignature != "" &&
		third.WakeSignature != "" &&
		first.WakeSignature != third.WakeSignature &&
		first.LastOutputPreview != "" &&
		second.LastOutputPreview != "" &&
		third.LastOutputPreview != ""
}

type scriptedLLM struct {
	calls int
}

func newScriptedLLM() *scriptedLLM {
	return &scriptedLLM{}
}

func (f *scriptedLLM) Send(_ context.Context, req llm.Request) (llm.Response, error) {
	f.calls++
	if !hasToolResult(req.Messages) && hasTool(req.Tools, "read_wiki") {
		return llm.Response{
			Content:      "Reading monitored page.",
			HasToolCalls: true,
			ToolCalls: []llm.ToolCall{{
				ID:        fmt.Sprintf("read-monitor-%d", f.calls),
				Name:      "read_wiki",
				Arguments: map[string]any{"slug": monitorSlug},
			}},
			Usage: llm.TokenUsage{PromptTokens: 40, CompletionTokens: 8, TotalTokens: 48},
		}, nil
	}
	return llm.Response{
		Content: "Checked the monitored page; no direct mutation performed and no proposal is needed.",
		Usage:   llm.TokenUsage{PromptTokens: 32, CompletionTokens: 13, TotalTokens: 45},
	}, nil
}

func (f *scriptedLLM) Stream(context.Context, llm.Request) (<-chan llm.Token, error) {
	ch := make(chan llm.Token)
	close(ch)
	return ch, nil
}

func hasTool(defs []llm.ToolDefinition, name string) bool {
	for _, def := range defs {
		if def.Name == name {
			return true
		}
	}
	return false
}

func hasToolResult(messages []llm.Message) bool {
	for _, msg := range messages {
		if msg.Role == "tool" {
			return true
		}
	}
	return false
}

func printReport(rep report) {
	status := "PASS"
	if !rep.OK {
		status = "FAIL"
	}
	fmt.Printf("%s debug_agent_jobs %s\n", status, rep.Mode)
	fmt.Printf("wiki_dir=%s db=%s\n", rep.WikiDir, rep.DBPath)
	for _, run := range rep.Runs {
		changed := "no"
		if run.WakeSignatureChanged {
			changed = "yes"
		}
		fmt.Printf("- run=%d skipped=%t llm_calls=%d tool_calls=%d tokens=%d elapsed_ms=%d wake_changed=%s output=%q\n",
			run.RunNumber,
			run.Skipped,
			run.LLMCalls,
			run.ToolCalls,
			run.TokensTotal,
			run.ElapsedMS,
			changed,
			run.LastOutputPreview,
		)
	}
	for _, warning := range rep.Warnings {
		fmt.Printf("warning: %s\n", warning)
	}
}

func preview(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	if max > 0 && len(s) > max {
		return s[:max] + "..."
	}
	return s
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
